package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	fairDispatchLedgerSetting = "fair_dispatch_ledger_v1"
	fairDispatchReasonPrefix  = "dispatch waiting: "
	fairDispatchResourceKind  = "task-dispatch:"
)

// daemonDispatchCandidate is produced only after the existing project-local
// eligibility checks have passed. The scheduler does not replace those checks;
// it arbitrates the remaining globally contended capacity before the existing
// atomic claim path runs.
type daemonDispatchCandidate struct {
	Project      RegisteredProject
	Workflow     WorkflowFile
	Note         Note
	NotesByID    map[string]Note
	Run          RunStatus
	Lane         string
	Status       string
	StateActive  int
	ProjectLimit int
	StateLimit   int
	RunnerLimit  int
}

type fairDispatchLedger struct {
	Schema       string            `json:"schema"`
	Sequence     uint64            `json:"sequence"`
	ProjectTurns map[string]uint64 `json:"project_turns"`
}

func loadFairDispatchLedger(store *RuntimeStore) (fairDispatchLedger, error) {
	ledger := fairDispatchLedger{
		Schema:       "tusker.fair-dispatch-ledger/v1",
		ProjectTurns: map[string]uint64{},
	}
	if store == nil {
		return ledger, nil
	}
	raw, err := store.GetSetting(fairDispatchLedgerSetting)
	if err != nil {
		return ledger, err
	}
	if strings.TrimSpace(raw) == "" {
		return ledger, nil
	}
	if err := json.Unmarshal([]byte(raw), &ledger); err != nil {
		// The ledger is an ordering hint, never ownership authority. A damaged
		// hint falls back to stable IDs; successful dispatch rewrites it.
		return fairDispatchLedger{Schema: "tusker.fair-dispatch-ledger/v1", ProjectTurns: map[string]uint64{}}, nil
	}
	if ledger.ProjectTurns == nil {
		ledger.ProjectTurns = map[string]uint64{}
	}
	for _, turn := range ledger.ProjectTurns {
		if turn > ledger.Sequence {
			ledger.Sequence = turn
		}
	}
	ledger.Schema = "tusker.fair-dispatch-ledger/v1"
	return ledger, nil
}

func (l *fairDispatchLedger) recordSuccessfulTurn(store *RuntimeStore, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	if l.ProjectTurns == nil {
		l.ProjectTurns = map[string]uint64{}
	}
	l.Sequence++
	l.ProjectTurns[projectID] = l.Sequence
	if store == nil {
		return nil
	}
	raw, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return store.SetSetting(fairDispatchLedgerSetting, string(raw))
}

func fairDispatchLess(left, right daemonDispatchCandidate, ledger fairDispatchLedger) bool {
	if lp, rp := priorityRank(stringField(left.Note.Data, "priority")), priorityRank(stringField(right.Note.Data, "priority")); lp != rp {
		return lp < rp
	}
	if lt, rt := ledger.ProjectTurns[left.Project.ProjectID], ledger.ProjectTurns[right.Project.ProjectID]; lt != rt {
		return lt < rt
	}
	if left.Project.ProjectID != right.Project.ProjectID {
		return left.Project.ProjectID < right.Project.ProjectID
	}
	if left.Run.ItemID != right.Run.ItemID {
		return left.Run.ItemID < right.Run.ItemID
	}
	return left.Lane < right.Lane
}

func sortFairDispatchCandidates(candidates []daemonDispatchCandidate, ledger fairDispatchLedger) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return fairDispatchLess(candidates[i], candidates[j], ledger)
	})
}

func fairDispatchRunKey(projectID, recordID string) string {
	return strings.TrimSpace(projectID) + "\x00" + strings.TrimSpace(recordID)
}

func fairDispatchRunnerLimit(wf Workflow) int {
	// max_concurrent_agents is the resolved runner budget inherited by legacy
	// workflows. Project and global caps remain separate checks.
	return wf.Agents.MaxConcurrentAgents
}

func fairDispatchNamedResources(note Note) []string {
	resources := append([]string(nil), normalizeList(note.Data["resource_refs"])...)
	if group := strings.TrimSpace(stringField(note.Data, "concurrency_group")); group != "" {
		resources = append(resources, group)
	}
	return sortedStrings(uniqueStrings(resources))
}

func fairDispatchProjectRuns(runs map[string]RunStatus, projectID string) (map[string]RunStatus, []RunStatus) {
	byRecord := map[string]RunStatus{}
	list := make([]RunStatus, 0)
	for _, run := range runs {
		if run.ProjectID != projectID {
			continue
		}
		byRecord[run.RecordID] = run
		list = append(list, run)
	}
	return byRecord, list
}

func fairDispatchOwnedPathBlocker(candidate daemonDispatchCandidate, projectRuns []RunStatus, now time.Time) string {
	if len(candidate.NotesByID) == 0 {
		return ""
	}
	notes := orchestrationOwnedPathNotes(candidate.NotesByID, candidate.Workflow.Data)
	note := candidate.Note
	if projected, ok := notes[stringField(candidate.Note.Data, "id")]; ok {
		note = projected
	}
	conflict, found := ownedPathConflict(note, notes, projectRuns, now)
	if !found {
		return ""
	}
	return fmt.Sprintf(
		"owned path conflict: %s holds %s for task %s (candidate %s; liveness %s)",
		conflict["holder"], conflict["holder_path"], conflict["task_id"],
		conflict["candidate_path"], conflict["liveness"],
	)
}

func fairDispatchResourcePurpose(recordID string) string {
	return fairDispatchResourceKind + strings.TrimSpace(recordID)
}

func fairDispatchResourceRecordID(lease ResourceLease) (string, bool) {
	if !strings.HasPrefix(lease.Purpose, fairDispatchResourceKind) {
		return "", false
	}
	recordID := firstNonEmpty(strings.TrimSpace(lease.DepartureID), strings.TrimPrefix(lease.Purpose, fairDispatchResourceKind))
	return recordID, recordID != ""
}

func (d *Daemon) reconcileFairDispatchResourceLeases(runs map[string]RunStatus, now time.Time) error {
	leases, err := d.store.ListResourceLeases()
	if err != nil {
		return err
	}
	for _, lease := range leases {
		recordID, owned := fairDispatchResourceRecordID(lease)
		if !owned || lease.State != resourceLeaseHeld {
			continue
		}
		run, live := runs[fairDispatchRunKey(lease.ProjectID, recordID)]
		live = live && runConsumesDispatchCapacity(run) && run.LeaseOwner == lease.Owner
		if !live {
			if _, err := d.store.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, "task dispatch no longer owns a live run", now); err != nil {
				return err
			}
			continue
		}
		renewed, err := d.store.RenewResourceLease(ResourceLeaseRenewal{
			Name: lease.Name, Owner: lease.Owner, Generation: lease.Generation,
			TTL: defaultResourceLeaseTTL, Now: now,
		})
		if err != nil {
			return err
		}
		if renewed {
			continue
		}
		// A daemon restart can observe a still-live task after its resource TTL
		// elapsed. Reacquire only for the exact run owner; a concurrent takeover
		// remains fenced by AcquireResourceLease.
		if _, _, err := d.store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: lease.Name, Owner: lease.Owner, Purpose: lease.Purpose,
			ProjectID: lease.ProjectID, DepartureID: recordID,
			TTL: defaultResourceLeaseTTL, Now: now,
		}); err != nil {
			var typed *TuskerError
			if errors.As(err, &typed) && typed.Code == resourceLeaseRefusal {
				continue
			}
			return err
		}
	}
	return nil
}

func (d *Daemon) releaseFairDispatchResources(leases []ResourceLease, reason string, now time.Time) error {
	var releaseErr error
	for _, lease := range leases {
		if _, err := d.store.ReleaseResourceLease(lease.Name, lease.Owner, lease.Generation, reason, now); err != nil {
			releaseErr = errors.Join(releaseErr, err)
		}
	}
	return releaseErr
}

func (d *Daemon) reserveFairDispatchResources(candidate daemonDispatchCandidate, attemptID string, now time.Time) ([]ResourceLease, string, error) {
	reserved := make([]ResourceLease, 0)
	for _, resource := range fairDispatchNamedResources(candidate.Note) {
		lease, acquired, err := d.store.AcquireResourceLease(ResourceLeaseAcquireInput{
			Name: resource, Owner: attemptID,
			Purpose:   fairDispatchResourcePurpose(candidate.Run.RecordID),
			ProjectID: candidate.Project.ProjectID, DepartureID: candidate.Run.RecordID,
			TTL: defaultResourceLeaseTTL, Now: now,
		})
		if err != nil {
			var typed *TuskerError
			if !errors.As(err, &typed) || typed.Code != resourceLeaseRefusal {
				releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch resource reservation failed", now)
				return nil, "", errors.Join(err, releaseErr)
			}
			current, loadErr := d.store.FindResourceLease(resource)
			if loadErr != nil {
				releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch resource reservation lost", now)
				return nil, "", errors.Join(loadErr, releaseErr)
			}
			if err := d.store.RegisterResourceLeaseWaiter(resource, candidate.Project.ProjectID); err != nil {
				releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch waiter registration failed", now)
				return nil, "", errors.Join(err, releaseErr)
			}
			if err := d.releaseFairDispatchResources(reserved, "task dispatch blocked by another resource holder", now); err != nil {
				return nil, "", err
			}
			if current == nil {
				return nil, fmt.Sprintf("named resource %q changed ownership during selection", resource), nil
			}
			return nil, fmt.Sprintf("named resource %q is held by project %s owner %s", resource, current.ProjectID, current.Owner), nil
		}
		if !acquired {
			err := tuskerError(errorInvalidTransition, "named resource reservation returned without ownership")
			releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch resource reservation was not acquired", now)
			return nil, "", errors.Join(err, releaseErr)
		}
		reserved = append(reserved, lease)
		if err := d.store.ClearResourceLeaseWaiter(resource, candidate.Project.ProjectID); err != nil {
			releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch waiter cleanup failed", now)
			return nil, "", errors.Join(err, releaseErr)
		}
	}
	return reserved, "", nil
}

func fairDispatchReasonIsOwned(reason string) bool {
	return strings.HasPrefix(strings.TrimSpace(reason), fairDispatchReasonPrefix)
}

func (d *Daemon) persistFairDispatchReason(runs map[string]RunStatus, candidate daemonDispatchCandidate, reason string) error {
	key := fairDispatchRunKey(candidate.Project.ProjectID, candidate.Run.RecordID)
	current, ok := runs[key]
	if !ok {
		current = candidate.Run
	}
	if runConsumesDispatchCapacity(current) {
		return nil
	}
	reason = fairDispatchReasonPrefix + strings.TrimSpace(reason)
	if current.LastError == reason {
		return nil
	}
	updated := current
	updated.LastError = reason
	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := d.upsertRunWithStream(current, updated); err != nil {
		var typed *TuskerError
		if errors.As(err, &typed) && typed.Code == "CAS_CONFLICT" {
			return nil
		}
		return err
	}
	runs[key] = updated
	return nil
}

func (d *Daemon) runFairDispatchCandidate(ctx context.Context, candidate daemonDispatchCandidate, run RunStatus, attemptID string) (RunStatus, bool, bool, error) {
	if d.fairDispatchRun != nil {
		return d.fairDispatchRun(ctx, candidate.Project, candidate.Workflow, candidate.Note, run, candidate.Lane, attemptID)
	}
	updated, persisted, err := d.dispatchRunWithAttemptID(ctx, candidate.Project, candidate.Workflow, candidate.Note, run, candidate.Lane, attemptID)
	claimed := strings.TrimSpace(updated.LeaseOwner) == attemptID && updated.LeaseGeneration > run.LeaseGeneration
	if !claimed {
		allRuns, loadErr := d.store.ListRuns()
		if loadErr != nil && err == nil {
			return updated, persisted, false, loadErr
		}
		for _, current := range allRuns {
			if current.ProjectID != candidate.Project.ProjectID || current.RecordID != candidate.Run.RecordID {
				continue
			}
			if strings.TrimSpace(current.LeaseOwner) == attemptID && current.LeaseGeneration > run.LeaseGeneration {
				updated, persisted, claimed = current, true, true
			}
			break
		}
	}
	return updated, persisted, claimed, err
}

// refreshFairExecuteCandidate closes the gap between project-local candidate
// collection and global fair selection. The completion reactor runs in that
// gap and may relock a proof-green soft dependent. Scope admission alone is
// insufficient in all_eligible mode, so execute and integrator candidates
// reload canonical notes and rerun both dependency eligibility and the full
// automation plan immediately before selection.
func (d *Daemon) refreshFairExecuteCandidate(candidate daemonDispatchCandidate, run RunStatus, projectRuns map[string]RunStatus) (daemonDispatchCandidate, string, error) {
	// PollOnce always supplies both canonical paths. Empty paths identify
	// scheduler-only synthetic candidates, which have no tracker source to
	// refresh.
	if (candidate.Lane != runLaneExecute && candidate.Lane != runLaneIntegrator) ||
		strings.TrimSpace(candidate.Note.AbsolutePath) == "" ||
		strings.TrimSpace(candidate.Workflow.Path) == "" {
		return candidate, "", nil
	}
	notes, err := listOperationalNotes(candidate.Project.VaultRoot)
	if err != nil {
		return candidate, "", err
	}
	notesByID, notesByRecordID := daemonNoteMaps(notes)
	note, ok := notesByRecordID[candidate.Run.RecordID]
	if !ok {
		return candidate, "post-reactor task projection is missing", nil
	}
	dispatchNote := note
	dispatchNotes := notes
	dispatchNotesByID := notesByID
	dispatchNotesByRecordID := notesByRecordID
	if projected, projectedIdx, projectedOK, projectionErr := armedWaveDispatchTaskProjection(candidate.Project.VaultRoot, note); projectionErr != nil {
		return candidate, "", projectionErr
	} else if projectedOK {
		dispatchNote = projected
		dispatchNotes = append([]Note(nil), notes...)
		dispatchNotesByID = projectedNoteMap(notesByID, projectedIdx)
		dispatchNotesByRecordID = projectedNoteMap(notesByRecordID, projectedIdx)
		for i, current := range dispatchNotes {
			if projectedTask, exists := projectedIdx.Tasks[trackerRecordID(current)]; exists {
				dispatchNotes[i] = projectedTask
			}
		}
	}
	candidate.Note = dispatchNote
	candidate.NotesByID = dispatchNotesByID
	candidate.Run = run
	candidate.Status = stringField(dispatchNote.Data, "status")
	candidate.Lane = runLaneExecute
	if stringField(dispatchNote.Data, "work_kind") == "integrator" {
		candidate.Lane = runLaneIntegrator
	}
	if !containsString(candidate.Workflow.Data.Tracker.ActiveStates, candidate.Status) {
		return candidate, "post-reactor tracker status is " + fallback(candidate.Status, "(missing)"), nil
	}
	if currentWork := intField(dispatchNote.Data, "work_revision"); run.WorkRevision != currentWork {
		return candidate, fmt.Sprintf("post-reactor work revision changed from %d to %d", run.WorkRevision, currentWork), nil
	}
	if reason, scopeErr := d.scopeDispatchBlocker(candidate.Project, dispatchNote, candidate.Workflow.Data, projectRuns); scopeErr != nil {
		return candidate, "", scopeErr
	} else if reason != "" {
		return candidate, "post-reactor dispatch scope or wave constraint: " + reason, nil
	}
	if reason := daemonDispatchBlockedReason(candidate.Project.VaultRoot, dispatchNote, dispatchNotesByID, dispatchNotesByRecordID); reason != "" {
		return candidate, "post-reactor " + strings.TrimPrefix(reason, "dispatch blocked: "), nil
	}
	if blocked, planErr := d.executePlanBlockedReason(candidate.Project, candidate.Workflow, dispatchNotes, dispatchNote, run); planErr != nil {
		return candidate, "", planErr
	} else if blocked != "" {
		return candidate, "post-reactor automation plan do_not_dispatch: " + blocked, nil
	}
	return candidate, "", nil
}

func (d *Daemon) dispatchFairCandidates(ctx context.Context, candidates []daemonDispatchCandidate, globalLimit int) error {
	allRuns, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	runs := make(map[string]RunStatus, len(allRuns))
	projectActive := map[string]int{}
	runnerActive := map[string]int{}
	globalActive := 0
	for _, run := range allRuns {
		runs[fairDispatchRunKey(run.ProjectID, run.RecordID)] = run
		if !runConsumesDispatchCapacity(run) {
			continue
		}
		globalActive++
		projectActive[run.ProjectID]++
		runnerActive[run.Runner]++
	}
	if err := d.reconcileFairDispatchResourceLeases(runs, time.Now().UTC()); err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}
	ledger, err := loadFairDispatchLedger(d.store)
	if err != nil {
		return err
	}
	stateSelected := map[string]int{}

	for len(candidates) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		sortFairDispatchCandidates(candidates, ledger)
		if globalLimit > 0 && globalActive >= globalLimit {
			for _, candidate := range candidates {
				reason := fmt.Sprintf("global capacity reached (%d/%d); fair order is priority %s then least-recent project turn", globalActive, globalLimit, fallback(stringField(candidate.Note.Data, "priority"), "unspecified"))
				if err := d.persistFairDispatchReason(runs, candidate, reason); err != nil {
					return err
				}
			}
			break
		}

		candidate := candidates[0]
		candidates = candidates[1:]
		key := fairDispatchRunKey(candidate.Project.ProjectID, candidate.Run.RecordID)
		run := candidate.Run
		if current, ok := runs[key]; ok {
			run = current
		}
		if runConsumesDispatchCapacity(run) || !shouldDispatchRun(run, time.Now().UTC()) {
			continue
		}
		projectRunsByRecord, projectRuns := fairDispatchProjectRuns(runs, candidate.Project.ProjectID)
		candidate, refreshReason, refreshErr := d.refreshFairExecuteCandidate(candidate, run, projectRunsByRecord)
		if refreshErr != nil {
			return refreshErr
		}
		if refreshReason != "" {
			if err := d.persistFairDispatchReason(runs, candidate, refreshReason); err != nil {
				return err
			}
			continue
		}

		projectLimit := candidate.ProjectLimit
		if projectLimit > 0 && projectActive[candidate.Project.ProjectID] >= projectLimit {
			if err := d.persistFairDispatchReason(runs, candidate, fmt.Sprintf("project capacity reached (%d/%d)", projectActive[candidate.Project.ProjectID], projectLimit)); err != nil {
				return err
			}
			continue
		}
		runnerLimit := candidate.RunnerLimit
		if runnerLimit > 0 && runnerActive[run.Runner] >= runnerLimit {
			if err := d.persistFairDispatchReason(runs, candidate, fmt.Sprintf("runner %s capacity reached (%d/%d)", run.Runner, runnerActive[run.Runner], runnerLimit)); err != nil {
				return err
			}
			continue
		}
		stateKey := candidate.Project.ProjectID + "\x00" + candidate.Status
		if candidate.StateLimit > 0 && candidate.StateActive+stateSelected[stateKey] >= candidate.StateLimit {
			if err := d.persistFairDispatchReason(runs, candidate, fmt.Sprintf("state %q concurrency cap reached (%d/%d)", candidate.Status, candidate.StateActive+stateSelected[stateKey], candidate.StateLimit)); err != nil {
				return err
			}
			continue
		}
		projectRunsByRecord, projectRuns = fairDispatchProjectRuns(runs, candidate.Project.ProjectID)
		if reason, err := d.scopeDispatchBlocker(candidate.Project, candidate.Note, candidate.Workflow.Data, projectRunsByRecord); err != nil {
			return err
		} else if reason != "" {
			if err := d.persistFairDispatchReason(runs, candidate, "dispatch scope or wave constraint: "+reason); err != nil {
				return err
			}
			continue
		}
		if reason := fairDispatchOwnedPathBlocker(candidate, projectRuns, time.Now().UTC()); reason != "" {
			if err := d.persistFairDispatchReason(runs, candidate, reason); err != nil {
				return err
			}
			continue
		}
		attemptID := newRecordID()
		reserved, reason, err := d.reserveFairDispatchResources(candidate, attemptID, time.Now().UTC())
		if err != nil {
			return err
		}
		if reason != "" {
			if err := d.persistFairDispatchReason(runs, candidate, reason); err != nil {
				return err
			}
			continue
		}

		before := run
		if fairDispatchReasonIsOwned(run.LastError) {
			run.LastError = ""
		}
		updated, persisted, claimed, dispatchErr := d.runFairDispatchCandidate(ctx, candidate, run, attemptID)
		if !persisted {
			if dispatchErr != nil {
				updated = d.scheduleRetry(updated, candidate.Workflow.Data, dispatchErr.Error())
			}
			if err := d.upsertRunWithStream(before, updated); err != nil {
				releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch persistence failed", time.Now().UTC())
				return errors.Join(err, releaseErr)
			}
		} else if dispatchErr != nil {
			var typed *TuskerError
			if errors.As(dispatchErr, &typed) && typed.Code == "CAS_CONFLICT" {
				if !claimed {
					if err := d.releaseFairDispatchResources(reserved, "task dispatch claim lost", time.Now().UTC()); err != nil {
						return err
					}
				}
				continue
			}
			if !claimed {
				releaseErr := d.releaseFairDispatchResources(reserved, "task dispatch failed before claim", time.Now().UTC())
				return errors.Join(dispatchErr, releaseErr)
			}
			return dispatchErr
		} else {
			d.emitLeaseTransitionStreamEvent(before, updated)
		}
		d.emitDispatchStreamEvent(updated)
		runs[key] = updated

		if delta := dispatchCapacityRunDelta(before, updated); delta > 0 {
			globalActive += delta
			projectActive[candidate.Project.ProjectID] += delta
			runnerActive[updated.Runner] += delta
			stateSelected[stateKey] += delta
		}
		keptReservation := claimed && dispatchCapacityRunDelta(before, updated) > 0
		if !keptReservation {
			if err := d.releaseFairDispatchResources(reserved, "task dispatch did not retain capacity", time.Now().UTC()); err != nil {
				return err
			}
		}
		if keptReservation {
			if err := ledger.recordSuccessfulTurn(d.store, candidate.Project.ProjectID); err != nil {
				return err
			}
		}
	}
	return nil
}
