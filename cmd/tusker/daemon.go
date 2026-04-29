package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Daemon struct {
	stateRoot string
	store     *RuntimeStore
}

func DefaultStateRoot() string {
	if explicit := strings.TrimSpace(os.Getenv("TUSKER_STATE_ROOT")); explicit != "" {
		return explicit
	}
	if home := userHomeDir(); home != "" {
		return filepath.Join(home, "Library", "Application Support", "tusker")
	}
	return filepath.Join(os.TempDir(), "tusker")
}

func NewDaemon(stateRoot string) (*Daemon, error) {
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return nil, err
	}
	return &Daemon{stateRoot: stateRoot, store: store}, nil
}

func (d *Daemon) Close() error {
	if d == nil || d.store == nil {
		return nil
	}
	return d.store.Close()
}

func (d *Daemon) Run(ctx context.Context, once bool) error {
	var control *daemonControlServer
	var err error
	if !once {
		control, err = startDaemonControlServer(d.stateRoot, func(reqCtx context.Context, req daemonControlRequest) daemonControlResponse {
			switch req.Command {
			case "interrupt":
				if err := d.InterruptRun(reqCtx, req.Identity); err != nil {
					return daemonControlResponse{OK: false, Message: err.Error()}
				}
				return daemonControlResponse{OK: true}
			default:
				return daemonControlResponse{OK: false, Message: "unknown control command"}
			}
		})
		if err != nil {
			return err
		}
		defer control.Close()
	}
	if once {
		return d.PollOnce(ctx)
	}
	for {
		if err := d.PollOnce(ctx); err != nil {
			return err
		}
		interval := d.nextPollInterval()
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func (d *Daemon) nextPollInterval() time.Duration {
	projects, err := d.store.ListProjects()
	if err != nil || len(projects) == 0 {
		return 30 * time.Second
	}
	minInterval := 30 * time.Second
	found := false
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		wf, err := loadWorkflow(project.VaultRoot)
		if err != nil {
			continue
		}
		ms := wf.Data.Runtime.PollIntervalMS
		if ms <= 0 {
			continue
		}
		dur := time.Duration(ms) * time.Millisecond
		if !found || dur < minInterval {
			minInterval = dur
			found = true
		}
	}
	if !found {
		return 30 * time.Second
	}
	return minInterval
}

func (d *Daemon) InterruptRun(ctx context.Context, identity string) error {
	run, err := d.store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	handle := liveRegistry.Find(firstNonEmpty(run.ActiveAttemptID, identity))
	if handle == nil {
		return errLiveHandleNotFound
	}
	if err := handle.Interrupt(ctx); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	updateRunAttemptFromRun(d.store, *run, AttemptOutcomeCancelled, 130, "interrupt requested by operator", now)
	run.LeaseState = string(LeaseStateInterrupted)
	run.AttemptOutcome = string(AttemptOutcomeCancelled)
	run.NextRetryAt = ""
	run.LastError = "interrupt requested by operator"
	run.UpdatedAt = now
	clearActiveExecution(run)
	if err := d.store.UpsertRun(*run); err != nil {
		return err
	}
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = d.store.MarkSessionState(run.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateInterrupted), "", run.LastError, true)
	}
	return nil
}

func (d *Daemon) PollOnce(ctx context.Context) error {
	projects, err := d.store.ListProjects()
	if err != nil {
		return err
	}
	allRuns, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	runsByProject := map[string]map[string]RunStatus{}
	for _, run := range allRuns {
		if runsByProject[run.ProjectID] == nil {
			runsByProject[run.ProjectID] = map[string]RunStatus{}
		}
		runsByProject[run.ProjectID][run.RecordID] = run
	}
	globalActiveRuns := countDispatchingRuns(allRuns)
	globalLimit, err := d.globalActiveRunLimit()
	if err != nil {
		return err
	}

	for _, project := range projects {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !project.Enabled {
			continue
		}

		wfFile, err := loadWorkflow(project.VaultRoot)
		if err != nil {
			_ = d.store.UpsertProject(RegisteredProject{
				ProjectID: project.ProjectID, ProjectKey: project.ProjectKey, Name: project.Name,
				RepoRoot: project.RepoRoot, VaultRoot: project.VaultRoot, WorkflowPath: project.WorkflowPath,
				Enabled: project.Enabled, Health: projectHealthError, LastError: err.Error(), LastPollAt: project.LastPollAt,
			})
			continue
		}
		notes, err := listAllNotes(project.VaultRoot)
		if err != nil {
			return err
		}
		sortDispatchCandidates(notes)
		noteStatusByRecord := map[string]string{}
		notesByID := map[string]Note{}
		notesByRecordID := map[string]Note{}
		keep := map[string]struct{}{}
		for _, note := range notes {
			noteType := stringField(note.Data, "type")
			if noteType != "story" && noteType != "bug" {
				continue
			}
			if id := stringField(note.Data, "id"); id != "" {
				notesByID[id] = note
			}
			recordID := stringField(note.Data, "record_id")
			if recordID == "" {
				continue
			}
			notesByRecordID[recordID] = note
			noteStatusByRecord[recordID] = stringField(note.Data, "status")
			keep[recordID] = struct{}{}
		}

		projectRuns := runsByProject[project.ProjectID]
		if projectRuns == nil {
			projectRuns = map[string]RunStatus{}
		}
		projectActiveRuns := countDispatchingProjectRuns(projectRuns)
		now := time.Now().UTC()
		for recordID, current := range projectRuns {
			reconciled, changed, err := d.reconcileRunWithTracker(ctx, project, wfFile, current, noteStatusByRecord[recordID])
			if err != nil {
				return err
			}
			if changed {
				if err := d.store.UpsertRun(reconciled); err != nil {
					return err
				}
				projectRuns[recordID] = reconciled
				globalActiveRuns += dispatchingRunDelta(current, reconciled)
				projectActiveRuns += dispatchingRunDelta(current, reconciled)
			}
		}
		stateActiveRuns := countDispatchingProjectRunsByState(projectRuns, noteStatusByRecord)

		for _, note := range notes {
			noteType := stringField(note.Data, "type")
			if noteType != "story" && noteType != "bug" {
				continue
			}
			status := stringField(note.Data, "status")
			if !containsString(wfFile.Data.Tracker.ActiveStates, status) {
				continue
			}
			recordID := stringField(note.Data, "record_id")
			if recordID == "" {
				continue
			}

			current := projectRuns[recordID]
			current.ProjectID = project.ProjectID
			current.RecordID = recordID
			current.ItemID = stringField(note.Data, "id")
			current.Runner = firstNonEmpty(resolveRunnerForNote(note, wfFile.Data), current.Runner, wfFile.Data.Agents.Default)
			current.UpdatedAt = now.Format(time.RFC3339)
			if current.LeaseState == "" {
				current.LeaseState = string(LeaseStateUnclaimed)
			}

			workRevision := intField(note.Data, "work_revision")
			if current.WorkRevision != workRevision {
				oldRun := current
				d.emitSupervisorDecision(SupervisorDecision{
					ProjectID:        project.ProjectID,
					RecordID:         recordID,
					Kind:             string(SupervisorDecisionNewRevision),
					Reason:           fmt.Sprintf("work_revision changed from %d to %d; old session refs cleared", oldRun.WorkRevision, workRevision),
					ParentAttemptID:  oldRun.ActiveAttemptID,
					ParentSessionRef: oldRun.SessionRef,
					WorkspacePath:    oldRun.WorkspacePath,
				})
				current.WorkRevision = workRevision
				current.AttemptCount = 0
				current.AttemptOutcome = string(AttemptOutcomeNone)
				current.LeaseState = string(LeaseStateUnclaimed)
				current.NextRetryAt = ""
				current.LastError = ""
				current.SessionRef = ""
				current.StartedAt = ""
				current.LastEventAt = ""
				clearActiveExecution(&current)
			}
			if err := d.store.UpsertRun(current); err != nil {
				return err
			}
			projectRuns[recordID] = current

			if shouldDispatchRun(current, now) {
				if !dispatchEligibilityAllows(note, notesByID, notesByRecordID) {
					current.LastError = dispatchEligibilityReason(note, notesByID, notesByRecordID)
					current.UpdatedAt = now.Format(time.RFC3339)
					if err := d.store.UpsertRun(current); err != nil {
						return err
					}
					projectRuns[recordID] = current
					continue
				}
				if globalLimit > 0 && globalActiveRuns >= globalLimit {
					continue
				}
				if projectActiveRuns >= projectActiveRunLimit(wfFile.Data) {
					continue
				}
				if stateDispatchCapReached(status, stateActiveRuns, wfFile.Data) {
					current.LastError = fmt.Sprintf("dispatch blocked: state %q concurrency cap reached", status)
					current.UpdatedAt = now.Format(time.RFC3339)
					if err := d.store.UpsertRun(current); err != nil {
						return err
					}
					projectRuns[recordID] = current
					continue
				}
				updated, err := d.dispatchRun(ctx, project, wfFile, note, current)
				if err != nil {
					updated = d.scheduleRetry(updated, wfFile.Data, err.Error())
				}
				if err := d.store.UpsertRun(updated); err != nil {
					return err
				}
				projectRuns[recordID] = updated
				globalActiveRuns += dispatchingRunDelta(current, updated)
				projectActiveRuns += dispatchingRunDelta(current, updated)
				stateActiveRuns[status] += dispatchingRunDelta(current, updated)
			}
		}

		if err := d.store.DeleteRunsNotIn(project.ProjectID, keep); err != nil {
			return err
		}
		if err := d.store.TouchProjectPoll(project.ProjectID); err != nil {
			return err
		}
	}
	return nil
}

func projectActiveRunLimit(wf Workflow) int {
	if wf.Runtime.MaxActiveRunsPerProject > 0 {
		return wf.Runtime.MaxActiveRunsPerProject
	}
	return 1
}

func (d *Daemon) globalActiveRunLimit() (int, error) {
	if d != nil && d.store != nil {
		if value, err := d.store.GlobalActiveRunLimit(); err != nil {
			return 0, err
		} else if value > 0 {
			return value, nil
		}
	}
	return 2, nil
}

func countDispatchingRuns(runs []RunStatus) int {
	count := 0
	for _, run := range runs {
		if isDispatchingLeaseState(run.LeaseState) {
			count++
		}
	}
	return count
}

func countDispatchingProjectRuns(runs map[string]RunStatus) int {
	count := 0
	for _, run := range runs {
		if isDispatchingLeaseState(run.LeaseState) {
			count++
		}
	}
	return count
}

func countDispatchingProjectRunsByState(runs map[string]RunStatus, stateByRecord map[string]string) map[string]int {
	counts := map[string]int{}
	for recordID, run := range runs {
		if !isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		state := strings.TrimSpace(stateByRecord[recordID])
		if state == "" {
			continue
		}
		counts[state]++
	}
	return counts
}

func stateDispatchCapReached(state string, activeByState map[string]int, wf Workflow) bool {
	capValue := wf.Agents.MaxConcurrentAgentsByState[strings.TrimSpace(state)]
	return capValue > 0 && activeByState[strings.TrimSpace(state)] >= capValue
}

func sortDispatchCandidates(notes []Note) {
	sort.SliceStable(notes, func(i, j int) bool {
		left, right := notes[i], notes[j]
		if lp, rp := priorityRank(stringField(left.Data, "priority")), priorityRank(stringField(right.Data, "priority")); lp != rp {
			return lp < rp
		}
		if ld, rd := stringField(left.Data, "due"), stringField(right.Data, "due"); ld != rd {
			if ld == "" {
				return false
			}
			if rd == "" {
				return true
			}
			return ld < rd
		}
		if lr, rr := riskRank(stringField(left.Data, "risk")), riskRank(stringField(right.Data, "risk")); lr != rr {
			return lr < rr
		}
		return stringField(left.Data, "id") < stringField(right.Data, "id")
	})
}

func priorityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "p0":
		return 0
	case "p1":
		return 1
	case "p2":
		return 2
	case "p3":
		return 3
	default:
		return 9
	}
}

func riskRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	default:
		return 9
	}
}

func dispatchEligibilityAllows(note Note, notesByID map[string]Note, notesByRecordID map[string]Note) bool {
	return dispatchEligibilityReason(note, notesByID, notesByRecordID) == ""
}

func dispatchEligibilityReason(note Note, notesByID map[string]Note, notesByRecordID map[string]Note) string {
	if strings.EqualFold(stringField(note.Data, "risk"), "critical") {
		return "dispatch blocked: critical risk requires explicit human dispatch"
	}
	for _, blocker := range normalizeList(note.Data["blocked_by"]) {
		target := wikiTarget(blocker)
		if target == "" {
			continue
		}
		blockingNote, ok := notesByID[target]
		if !ok {
			return "dispatch blocked: unresolved blocker " + target
		}
		if !blockerResolved(blockingNote) {
			return "dispatch blocked: waiting on " + target
		}
	}
	for _, recordID := range normalizeList(note.Data["blocked_by_record_ids"]) {
		recordID = strings.TrimSpace(recordID)
		if recordID == "" {
			continue
		}
		blockingNote, ok := notesByRecordID[recordID]
		if !ok {
			return "dispatch blocked: unresolved blocker record " + recordID
		}
		if !blockerResolved(blockingNote) {
			return "dispatch blocked: waiting on " + firstNonEmpty(stringField(blockingNote.Data, "id"), recordID)
		}
	}
	return ""
}

func blockerResolved(note Note) bool {
	switch stringField(note.Data, "status") {
	case "done", "cancelled":
		return true
	default:
		return false
	}
}

func dispatchingRunDelta(before, after RunStatus) int {
	delta := 0
	if isDispatchingLeaseState(after.LeaseState) {
		delta++
	}
	if isDispatchingLeaseState(before.LeaseState) {
		delta--
	}
	return delta
}

func isDispatchingLeaseState(state string) bool {
	switch LeaseState(strings.TrimSpace(state)) {
	case LeaseStateClaimed, LeaseStateRunning:
		return true
	default:
		return false
	}
}

func shouldDispatchRun(run RunStatus, now time.Time) bool {
	switch LeaseState(run.LeaseState) {
	case LeaseStateUnclaimed:
		return true
	case LeaseStateRetryQueued:
		if strings.TrimSpace(run.NextRetryAt) == "" {
			return true
		}
		due, err := time.Parse(time.RFC3339, run.NextRetryAt)
		return err == nil && !due.After(now)
	case LeaseStateInterrupted:
		return false
	default:
		return false
	}
}

func (d *Daemon) reconcileRunWithTracker(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, run RunStatus, trackerState string) (RunStatus, bool, error) {
	if isDispatchingLeaseState(run.LeaseState) && !containsString(wfFile.Data.Tracker.ActiveStates, trackerState) {
		reason := fmt.Sprintf("tracker state %q is not active; daemon released run", firstNonEmpty(trackerState, "missing"))
		return d.releaseIneligibleRun(ctx, project, run, reason)
	}
	return d.reconcileRun(ctx, project, wfFile, run)
}

func (d *Daemon) releaseIneligibleRun(ctx context.Context, project RegisteredProject, run RunStatus, reason string) (RunStatus, bool, error) {
	if !isDispatchingLeaseState(run.LeaseState) {
		return run, false, nil
	}
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	interrupted, interruptErr := d.stopRunExecution(ctx, run)
	if interruptErr != nil {
		reason = reason + ": " + interruptErr.Error()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	outcome := AttemptOutcomeAbandoned
	exitCode := 0
	if interrupted {
		outcome = AttemptOutcomeCancelled
		exitCode = 130
		run.LeaseState = string(LeaseStateInterrupted)
	} else {
		run.LeaseState = string(LeaseStateReleased)
	}
	run.AttemptOutcome = string(outcome)
	run.NextRetryAt = ""
	run.LastError = reason
	run.UpdatedAt = now
	updateRunAttemptFromRun(d.store, run, outcome, exitCode, reason, now)
	clearActiveExecution(&run)
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, false)
	}
	d.emitSupervisorDecision(SupervisorDecision{
		ProjectID:        project.ProjectID,
		RecordID:         run.RecordID,
		Kind:             string(SupervisorDecisionStopForAudit),
		Reason:           reason,
		ParentAttemptID:  parentAttemptID,
		ParentSessionRef: parentSessionRef,
		WorkspacePath:    run.WorkspacePath,
	})
	return run, true, nil
}

func (d *Daemon) reconcileRun(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, run RunStatus) (RunStatus, bool, error) {
	switch LeaseState(run.LeaseState) {
	case LeaseStateClaimed, LeaseStateRunning:
	default:
		return run, false, nil
	}

	changed := false
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339)

	if strings.TrimSpace(run.RawLogPath) != "" {
		if sessionRef := extractSessionRef(run.RawLogPath); sessionRef != "" && sessionRef != run.SessionRef {
			run.SessionRef = sessionRef
			changed = true
		}
	}
	if lastEventAt, ok := latestRunEventAt(run); ok {
		formatted := lastEventAt.Format(time.RFC3339)
		if formatted != run.LastEventAt {
			run.LastEventAt = formatted
			changed = true
		}
	}
	messageRef := ""
	if strings.TrimSpace(run.RawLogPath) != "" {
		messageRef = extractMessageRef(run.RawLogPath)
	}
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = d.store.SaveSession(RunnerSession{
			ProjectID:      project.ProjectID,
			RecordID:       run.RecordID,
			Runner:         run.Runner,
			SessionRef:     run.SessionRef,
			LastMessageRef: messageRef,
			WorkspacePath:  run.WorkspacePath,
			CurrentItemID:  run.ItemID,
			WorkRevision:   run.WorkRevision,
			LastAttemptID:  run.ActiveAttemptID,
			State:          sessionStateForLeaseState(LeaseState(run.LeaseState)),
			Resumable:      true,
			StartedAt:      run.StartedAt,
			LastSeenAt:     now,
			LastError:      run.LastError,
		})
	}

	if strings.TrimSpace(run.StatusPath) != "" && fileExists(run.StatusPath) {
		status, err := readRunnerProcessStatus(run.StatusPath)
		if err != nil {
			return run, changed, err
		}
		finished := firstNonEmpty(status.CompletedAt, now)
		run.ProcessPID = 0
		run.UpdatedAt = finished
		if status.ExitCode == 0 {
			note, err := resolveNote(project.VaultRoot, run.RecordID)
			if err != nil {
				return run, changed, err
			}
			noteStatus := stringField(note.Data, "status")
			updateRunAttemptFromRun(d.store, run, AttemptOutcomeSucceeded, 0, "", finished)
			if containsString(wfFile.Data.Tracker.ActiveStates, noteStatus) {
				parentAttemptID := run.ActiveAttemptID
				parentSessionRef := run.SessionRef
				run = scheduleContinuationRetry(run, "runner exited cleanly while tracker state remained active; queued continuation retry")
				if strings.TrimSpace(run.SessionRef) != "" {
					_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateRetryQueued), "", run.LastError, true)
				}
				d.emitSupervisorDecision(SupervisorDecision{
					ProjectID:        project.ProjectID,
					RecordID:         run.RecordID,
					AttemptID:        parentAttemptID,
					SessionRef:       parentSessionRef,
					Kind:             string(SupervisorDecisionContinueThread),
					Reason:           run.LastError,
					ParentAttemptID:  parentAttemptID,
					ParentSessionRef: parentSessionRef,
					WorkspacePath:    run.WorkspacePath,
				})
				run.UpdatedAt = finished
				return run, true, nil
			}
			if err := writeReviewPacketEvidence(project.VaultRoot, note, run, d.store); err != nil {
				return run, changed, err
			}
			if shouldDaemonPromoteCleanExitToReview(noteStatus) {
				if err := markNoteReadyForReview(project.VaultRoot, note.AbsolutePath); err != nil {
					return run, changed, err
				}
			}
			run.LeaseState = string(LeaseStateReleased)
			run.AttemptOutcome = string(AttemptOutcomeSucceeded)
			run.NextRetryAt = ""
			run.LastError = ""
			if strings.TrimSpace(run.SessionRef) != "" {
				_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForOutcome(AttemptOutcomeSucceeded), "", "", true)
			}
			clearActiveExecution(&run)
			return run, true, nil
		}
		reason := fmt.Sprintf("runner exited with code %d", status.ExitCode)
		updateRunAttemptFromRun(d.store, run, AttemptOutcomeFailed, status.ExitCode, reason, finished)
		run = d.scheduleRetry(run, wfFile.Data, reason)
		if strings.TrimSpace(run.SessionRef) != "" {
			_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, true)
		}
		clearActiveExecution(&run)
		run.UpdatedAt = finished
		return run, true, nil
	}

	if run.ProcessPID > 0 && processExists(run.ProcessPID) {
		if stalled, reason := runStallReason(run, wfFile.Data, nowTime); stalled {
			if _, err := d.stopRunExecution(ctx, run); err != nil {
				reason = reason + ": " + err.Error()
			}
			updateRunAttemptFromRun(d.store, run, AttemptOutcomeFailed, 124, reason, now)
			run = d.scheduleRetry(run, wfFile.Data, reason)
			run.UpdatedAt = now
			if strings.TrimSpace(run.SessionRef) != "" {
				_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, true)
			}
			return run, true, nil
		}
		if LeaseState(run.LeaseState) != LeaseStateRunning {
			run.LeaseState = string(LeaseStateRunning)
			run.UpdatedAt = now
			return run, true, nil
		}
		return run, changed, nil
	}

	if run.ProcessPID > 0 && strings.TrimSpace(run.StatusPath) != "" && !fileExists(run.StatusPath) {
		for i := 0; i < 10 && !fileExists(run.StatusPath); i++ {
			time.Sleep(50 * time.Millisecond)
		}
		if fileExists(run.StatusPath) {
			return d.reconcileRun(ctx, project, wfFile, run)
		}
	}

	runner, _, err := runnerForName(run.Runner, wfFile.Data)
	if err != nil {
		return run, changed, err
	}
	result, err := runner.Reconcile(ctx, ReconcileRequest{
		Runner: run.Runner, ProjectID: project.ProjectID, RecordID: run.RecordID, AttemptID: "", SessionRef: run.SessionRef,
	})
	if err != nil {
		return run, changed, err
	}
	if result == nil {
		return run, changed, nil
	}
	run.LeaseState = string(result.LeaseState)
	run.AttemptOutcome = string(result.Outcome)
	run.LastError = result.Reason
	run.UpdatedAt = now
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	clearActiveExecution(&run)
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(result.LeaseState), "", result.Reason, result.Outcome != AttemptOutcomeAbandoned)
	}
	if result.LeaseState == LeaseStateRetryQueued && result.Outcome == AttemptOutcomeNone {
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        project.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        parentAttemptID,
			SessionRef:       parentSessionRef,
			Kind:             string(SupervisorDecisionContinueThread),
			Reason:           result.Reason,
			ParentAttemptID:  parentAttemptID,
			ParentSessionRef: parentSessionRef,
			WorkspacePath:    run.WorkspacePath,
		})
	}
	return run, true, nil
}

func scheduleContinuationRetry(run RunStatus, reason string) RunStatus {
	now := time.Now().UTC()
	run.LeaseState = string(LeaseStateRetryQueued)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.LastError = reason
	run.NextRetryAt = now.Add(time.Second).Format(time.RFC3339)
	run.UpdatedAt = now.Format(time.RFC3339)
	clearActiveExecution(&run)
	return run
}

func shouldDaemonPromoteCleanExitToReview(noteStatus string) bool {
	return strings.TrimSpace(noteStatus) == ""
}

func resolveRunnerForNote(note Note, wf Workflow) string {
	assignee := strings.TrimSpace(stringField(note.Data, "assignee"))
	for _, candidate := range wf.Agents.Enabled {
		if assignee == candidate {
			return candidate
		}
	}
	return wf.Agents.Default
}

func (d *Daemon) dispatchRun(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, note Note, run RunStatus) (RunStatus, error) {
	runner, command, err := runnerForName(run.Runner, wfFile.Data)
	if err != nil {
		return run, err
	}
	workspaceManager := NewWorkspaceManager()
	workspace, err := workspaceManager.Prepare(WorkspacePrepareRequest{
		ProjectID:    project.ProjectID,
		ProjectKey:   project.ProjectKey,
		RecordID:     run.RecordID,
		ItemID:       run.ItemID,
		RepoRoot:     project.RepoRoot,
		StateRoot:    d.stateRoot,
		Strategy:     workspaceStrategyFromWorkflow(wfFile.Data.Workspace.Strategy),
		WorkRevision: run.WorkRevision,
	})
	if err != nil {
		return run, err
	}

	ordinal := run.AttemptCount + 1
	attemptID := newRecordID()
	runDir := filepath.Join(d.stateRoot, "runs", project.ProjectKey, run.RecordID)
	if err := ensureDir(runDir); err != nil {
		return run, err
	}
	attemptStem := fmt.Sprintf("rev-%02d-attempt-%04d-%s", run.WorkRevision, ordinal, strings.ToLower(attemptID))
	promptPath := filepath.Join(runDir, attemptStem+".prompt.md")
	eventSinkPath := filepath.Join(runDir, attemptStem+".events.jsonl")
	rawLogPath := filepath.Join(runDir, attemptStem+".raw.log")
	statusPath := filepath.Join(runDir, attemptStem+".status.json")
	startedAt := time.Now().UTC().Format(time.RFC3339)
	attempt := RunAttempt{
		AttemptID: attemptID, ProjectID: project.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
		Runner: run.Runner, WorkRevision: run.WorkRevision, WorkspacePath: workspace.Path,
		PromptPath: promptPath, EventSinkPath: eventSinkPath, RawLogPath: rawLogPath, StatusPath: statusPath,
		StartedAt: startedAt,
	}
	run.LeaseState = string(LeaseStateClaimed)
	run.AttemptCount = ordinal
	run.WorkspacePath = workspace.Path
	run.ActiveAttemptID = attemptID
	run.ProcessPID = 0
	run.PromptPath = promptPath
	run.EventSinkPath = eventSinkPath
	run.RawLogPath = rawLogPath
	run.StatusPath = statusPath
	run.UpdatedAt = attempt.StartedAt
	run.LastEventAt = attempt.StartedAt

	prompt, err := renderAttemptPrompt(project, wfFile, note, workspace.Path, ordinal)
	if err != nil {
		attempt.Outcome = string(AttemptOutcomeFailed)
		attempt.LastError = err.Error()
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = d.store.SaveAttempt(attempt)
		return run, err
	}
	if err := writeText(promptPath, prompt); err != nil {
		attempt.Outcome = string(AttemptOutcomeFailed)
		attempt.LastError = err.Error()
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = d.store.SaveAttempt(attempt)
		return run, err
	}
	if err := d.store.SaveAttempt(attempt); err != nil {
		return run, err
	}

	if err := d.store.UpsertRun(run); err != nil {
		return run, err
	}

	resume, err := d.resolveResumeSession(project, note, run)
	if err != nil {
		return run, err
	}
	var start *StartResult
	if resume.SessionRef != "" && runner.Capabilities().ResumeSession {
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        project.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        attemptID,
			SessionRef:       resume.SessionRef,
			Kind:             resume.DecisionKind,
			Reason:           resume.Reason,
			ParentAttemptID:  resume.ParentAttemptID,
			ParentSessionRef: resume.SessionRef,
			WorkspacePath:    workspace.Path,
		})
		start, err = runner.Resume(ctx, ResumeRequest{
			ProjectID: project.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, AttemptID: attemptID,
			WorkRevision: run.WorkRevision, ActiveStates: wfFile.Data.Tracker.ActiveStates, SessionRef: resume.SessionRef, MessageRef: resume.MessageRef, WorkingDir: workspace.Path, WorkspacePath: workspace.Path,
			RepoRoot:   project.RepoRoot,
			PromptPath: promptPath, EventSinkPath: eventSinkPath, RawLogPath: rawLogPath, StatusPath: statusPath, Command: command,
			NotePath: note.AbsolutePath, VaultPath: project.VaultRoot, CodexPolicy: codexPolicyFromWorkflow(wfFile.Data),
		})
	} else {
		start, err = runner.Start(ctx, StartRequest{
			ProjectID:     project.ProjectID,
			RecordID:      run.RecordID,
			ItemID:        run.ItemID,
			AttemptID:     attemptID,
			WorkRevision:  run.WorkRevision,
			ActiveStates:  wfFile.Data.Tracker.ActiveStates,
			WorkingDir:    workspace.Path,
			WorkspacePath: workspace.Path,
			RepoRoot:      project.RepoRoot,
			PromptPath:    promptPath,
			EventSinkPath: eventSinkPath,
			RawLogPath:    rawLogPath,
			StatusPath:    statusPath,
			Command:       command,
			NotePath:      note.AbsolutePath,
			VaultPath:     project.VaultRoot,
			CodexPolicy:   codexPolicyFromWorkflow(wfFile.Data),
		})
	}
	if err != nil {
		attempt.Outcome = string(AttemptOutcomeFailed)
		attempt.LastError = err.Error()
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = d.store.SaveAttempt(attempt)
		return run, err
	}

	run.SessionRef = start.SessionRef
	run.StartedAt = start.StartedAt
	run.ProcessPID = start.PID
	run.StatusPath = firstNonEmpty(start.StatusPath, statusPath)
	run.UpdatedAt = firstNonEmpty(start.StartedAt, time.Now().UTC().Format(time.RFC3339))
	run.LastEventAt = firstNonEmpty(run.LastEventAt, run.UpdatedAt)

	attempt.SessionRef = start.SessionRef
	attempt.ProcessPID = start.PID
	attempt.Outcome = string(start.Outcome)
	attempt.ExitCode = start.ExitCode
	attempt.LastError = start.Reason
	attempt.StartedAt = start.StartedAt
	attempt.FinishedAt = start.FinishedAt
	_ = d.store.SaveAttempt(attempt)
	if strings.TrimSpace(start.SessionRef) != "" {
		_ = d.store.SaveSession(RunnerSession{
			ProjectID: project.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: start.SessionRef,
			LastMessageRef: start.MessageRef,
			WorkspacePath:  workspace.Path, CurrentItemID: run.ItemID, WorkRevision: run.WorkRevision, LastAttemptID: attemptID,
			State: sessionStateForOutcome(start.Outcome), Resumable: runner.Capabilities().ResumeSession,
			StartedAt: firstNonEmpty(run.StartedAt, start.StartedAt), LastSeenAt: time.Now().UTC().Format(time.RFC3339),
			EndedAt: firstNonEmpty(start.FinishedAt, ""), LastError: start.Reason,
		})
	}

	run.LeaseState = string(LeaseStateRunning)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.NextRetryAt = ""
	run.LastError = ""
	reconciled, changed, err := d.reconcileRun(ctx, project, wfFile, run)
	if err != nil {
		return run, err
	}
	if changed {
		return reconciled, nil
	}
	return run, nil
}

type resolvedResumeSession struct {
	SessionRef      string
	MessageRef      string
	DecisionKind    string
	Reason          string
	ParentAttemptID string
}

func (d *Daemon) resolveResumeSession(project RegisteredProject, note Note, run RunStatus) (resolvedResumeSession, error) {
	if strings.TrimSpace(run.SessionRef) != "" {
		session, err := d.store.FindSessionByRef(project.ProjectID, run.SessionRef)
		if err != nil || session == nil {
			return resolvedResumeSession{}, err
		}
		if reason := incompatibleResumeSessionReason(project, run, session); reason != "" {
			return resolvedResumeSession{}, nil
		}
		kind := string(SupervisorDecisionResumeSession)
		reason := "resuming compatible stored session"
		if LeaseState(run.LeaseState) == LeaseStateRetryQueued {
			kind = string(SupervisorDecisionContinueThread)
			reason = "continuing same stored session after queued continuation retry"
		}
		reason = appendRunnerContinuityCaveat(run.Runner, reason)
		return resolvedResumeSession{SessionRef: run.SessionRef, MessageRef: session.LastMessageRef, DecisionKind: kind, Reason: reason, ParentAttemptID: session.LastAttemptID}, nil
	}
	status := stringField(note.Data, "status")
	if status != "rework" && run.AttemptCount == 0 && LeaseState(run.LeaseState) != LeaseStateRetryQueued {
		return resolvedResumeSession{}, nil
	}
	session, err := d.store.LatestSession(project.ProjectID, run.RecordID, run.Runner)
	if err != nil || session == nil || !session.Resumable || strings.TrimSpace(session.SessionRef) == "" {
		return resolvedResumeSession{}, err
	}
	if reason := incompatibleResumeSessionReason(project, run, session); reason != "" {
		return resolvedResumeSession{}, nil
	}
	reason := appendRunnerContinuityCaveat(run.Runner, "resolved compatible stored session for same-ticket resume")
	return resolvedResumeSession{
		SessionRef: session.SessionRef, MessageRef: session.LastMessageRef, DecisionKind: string(SupervisorDecisionResumeSession),
		Reason: reason, ParentAttemptID: session.LastAttemptID,
	}, nil
}

func incompatibleResumeSessionReason(project RegisteredProject, run RunStatus, session *RunnerSession) string {
	if session == nil {
		return "missing stored session"
	}
	if session.ProjectID != project.ProjectID {
		return "stored session project_id does not match"
	}
	if session.RecordID != run.RecordID {
		return "stored session record_id does not match"
	}
	if session.Runner != run.Runner {
		return "stored session runner does not match"
	}
	if session.WorkRevision != run.WorkRevision {
		return fmt.Sprintf("stored session work_revision %d does not match run work_revision %d", session.WorkRevision, run.WorkRevision)
	}
	if !session.Resumable {
		return "stored session is not resumable"
	}
	if !workspacePathsCompatible(session.WorkspacePath, run.WorkspacePath) {
		return "stored session workspace_path does not match run workspace_path"
	}
	return ""
}

func workspacePathsCompatible(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	if resolved, err := filepath.EvalSymlinks(left); err == nil {
		left = resolved
	}
	if resolved, err := filepath.EvalSymlinks(right); err == nil {
		right = resolved
	}
	return left == right
}

func appendRunnerContinuityCaveat(runner, reason string) string {
	if RunnerName(strings.TrimSpace(runner)) == RunnerClaude {
		return reason + "; claude-code continuity is best-effort because the adapter only has a session ref"
	}
	return reason
}

func (d *Daemon) emitSupervisorDecision(decision SupervisorDecision) {
	if d == nil || d.store == nil || strings.TrimSpace(decision.Kind) == "" {
		return
	}
	saved, err := d.store.SaveSupervisorDecision(decision)
	if err != nil {
		return
	}
	run, err := d.store.FindRun(firstNonEmpty(saved.RecordID, saved.ItemID))
	if err != nil || run == nil || strings.TrimSpace(run.EventSinkPath) == "" {
		return
	}
	_ = NewEventLog(run.EventSinkPath).Append("supervisor_decision", saved.AttemptID, RunnerName(saved.Runner), map[string]any{
		"decision_id":           saved.DecisionID,
		"project_id":            saved.ProjectID,
		"record_id":             saved.RecordID,
		"item_id":               saved.ItemID,
		"runner":                saved.Runner,
		"work_revision":         saved.WorkRevision,
		"attempt_id":            saved.AttemptID,
		"parent_attempt_id":     saved.ParentAttemptID,
		"session_ref":           saved.SessionRef,
		"parent_session_ref":    saved.ParentSessionRef,
		"target_attempt_id":     saved.TargetAttemptID,
		"target_session_ref":    saved.TargetSessionRef,
		"kind":                  saved.Kind,
		"reason":                saved.Reason,
		"branch_name":           saved.BranchName,
		"workspace_path":        saved.WorkspacePath,
		"validation_delta":      saved.ValidationDelta,
		"merge_rule":            saved.MergeRule,
		"lease_state":           saved.LeaseState,
		"context_signal":        saved.ContextSignal,
		"input_tokens":          saved.InputTokens,
		"output_tokens":         saved.OutputTokens,
		"total_tokens":          saved.TotalTokens,
		"context_window_tokens": saved.ContextWindowTokens,
		"created_at":            saved.CreatedAt,
	})
}

func (s *RuntimeStore) FindSessionByRef(projectID, sessionRef string) (*RunnerSession, error) {
	if s == nil || s.db == nil || strings.TrimSpace(sessionRef) == "" {
		return nil, nil
	}
	row := s.db.QueryRow(`SELECT project_id, record_id, runner, session_ref, last_message_ref, workspace_path, current_item_id, work_revision, last_attempt_id, state, resumable, started_at, last_seen_at, ended_at, last_error
		FROM sessions
		WHERE project_id = ? AND session_ref = ?
		LIMIT 1`, projectID, sessionRef)
	var session RunnerSession
	var resumable int
	err := row.Scan(&session.ProjectID, &session.RecordID, &session.Runner, &session.SessionRef, &session.LastMessageRef, &session.WorkspacePath, &session.CurrentItemID, &session.WorkRevision, &session.LastAttemptID, &session.State, &resumable, &session.StartedAt, &session.LastSeenAt, &session.EndedAt, &session.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	session.Resumable = resumable != 0
	return &session, nil
}

func (d *Daemon) scheduleRetry(run RunStatus, wf Workflow, reason string) RunStatus {
	classification := classifyRetryFailure(reason)
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	run.AttemptOutcome = string(classification.outcome)
	run.LastError = reason
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	clearActiveExecution(&run)
	if !classification.retryable || run.AttemptCount >= wf.Retry.MaxAttempts {
		run.LeaseState = string(LeaseStateReleased)
		run.NextRetryAt = ""
		if !classification.retryable {
			decisionKind := string(SupervisorDecisionStopForAudit)
			contextSignal := ""
			if failureReasonIndicatesContextPressure(reason) {
				decisionKind = string(SupervisorDecisionForkThread)
				contextSignal = "context_pressure"
			}
			d.emitSupervisorDecision(SupervisorDecision{
				ProjectID:        run.ProjectID,
				RecordID:         run.RecordID,
				Kind:             decisionKind,
				Reason:           reason,
				ParentAttemptID:  parentAttemptID,
				ParentSessionRef: parentSessionRef,
				WorkspacePath:    run.WorkspacePath,
				ContextSignal:    contextSignal,
			})
		}
		return run
	}
	if len(wf.Retry.BackoffMS) == 0 {
		run.LeaseState = string(LeaseStateRetryQueued)
		run.NextRetryAt = time.Now().UTC().Format(time.RFC3339)
		return run
	}
	index := run.AttemptCount - 1
	if index < 0 {
		index = 0
	}
	if index >= len(wf.Retry.BackoffMS) {
		index = len(wf.Retry.BackoffMS) - 1
	}
	run.LeaseState = string(LeaseStateRetryQueued)
	run.NextRetryAt = time.Now().UTC().Add(time.Duration(wf.Retry.BackoffMS[index]) * time.Millisecond).Format(time.RFC3339)
	return run
}

type retryFailureClassification struct {
	retryable bool
	outcome   AttemptOutcome
}

func classifyRetryFailure(reason string) retryFailureClassification {
	text := strings.ToLower(strings.TrimSpace(reason))
	if text == "" {
		return retryFailureClassification{retryable: true, outcome: AttemptOutcomeFailed}
	}
	nonRetryable := []string{
		"auth", "authentication", "authorization", "unauthorized", "forbidden", "permission denied",
		"api key", "token expired", "invalid token", "login required", "not logged in",
		"sandbox", "approval denied", "approval rejected", "requires approval", "human approval",
		"config", "configuration", "unsupported runner", "command is empty", "invalid workflow",
		"deterministic", "validation failed", "invalid request", "bad request",
		"budget", "quota", "spend limit", "rate limit budget",
		"context window", "context-window", "context length", "maximum context", "context limit",
	}
	for _, marker := range nonRetryable {
		if strings.Contains(text, marker) {
			return retryFailureClassification{retryable: false, outcome: AttemptOutcomeBlocked}
		}
	}
	return retryFailureClassification{retryable: true, outcome: AttemptOutcomeFailed}
}

func failureReasonIndicatesContextPressure(reason string) bool {
	text := strings.ToLower(strings.TrimSpace(reason))
	for _, marker := range []string{"context window", "context-window", "context length", "maximum context", "context limit"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func clearActiveExecution(run *RunStatus) {
	run.ActiveAttemptID = ""
	run.ProcessPID = 0
	run.PromptPath = ""
	run.EventSinkPath = ""
	run.RawLogPath = ""
	run.StatusPath = ""
}

func updateRunAttemptFromRun(store *RuntimeStore, run RunStatus, outcome AttemptOutcome, exitCode int, lastError, finishedAt string) {
	if store == nil || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return
	}
	_ = store.SaveAttempt(RunAttempt{
		AttemptID:     run.ActiveAttemptID,
		ProjectID:     run.ProjectID,
		RecordID:      run.RecordID,
		ItemID:        run.ItemID,
		Runner:        run.Runner,
		WorkRevision:  run.WorkRevision,
		WorkspacePath: run.WorkspacePath,
		SessionRef:    run.SessionRef,
		ProcessPID:    run.ProcessPID,
		Outcome:       string(outcome),
		ExitCode:      exitCode,
		PromptPath:    run.PromptPath,
		EventSinkPath: run.EventSinkPath,
		RawLogPath:    run.RawLogPath,
		StatusPath:    run.StatusPath,
		LastError:     lastError,
		StartedAt:     run.StartedAt,
		FinishedAt:    finishedAt,
	})
}

func writeReviewPacketEvidence(vaultPath string, note Note, run RunStatus, store *RuntimeStore) error {
	itemID := stringField(note.Data, "id")
	if itemID == "" || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return nil
	}
	packetRel := filepath.ToSlash(filepath.Join("Attachments", itemID, "review-packet-"+strings.ToLower(run.ActiveAttemptID)+".md"))
	packetPath := filepath.Join(vaultPath, filepath.FromSlash(packetRel))
	turns := []RunTurn{}
	if store != nil {
		if loaded, err := store.ListTurnsForAttempt(run.ActiveAttemptID); err == nil {
			turns = loaded
		}
	}
	supervisorDecisions := []RuntimeSupervisorDecision{}
	if store != nil {
		if loaded, err := store.ListRuntimeSupervisorDecisionsForAttempt(run.ActiveAttemptID); err == nil {
			supervisorDecisions = loaded
		}
	}
	if err := writeText(packetPath, renderReviewPacket(note, run, turns, supervisorDecisions)); err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		return err
	}
	if !strings.Contains(body, packetRel) {
		date := todayISO()
		body = appendSectionBullet(body, "## Evidence", fmt.Sprintf("- %s - review-packet: [[%s]] - generated daemon proof packet for attempt %s", date, packetRel, run.ActiveAttemptID), false)
		body = appendWorkLogBullet(body, fmt.Sprintf("%s - daemon - generated review packet for attempt %s", date, run.ActiveAttemptID))
		data["updated"] = date
		content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
		if err != nil {
			return err
		}
		return writeText(note.AbsolutePath, content)
	}
	return nil
}

func renderReviewPacket(note Note, run RunStatus, turns []RunTurn, supervisorDecisions []RuntimeSupervisorDecision) string {
	var out []string
	out = append(out, "# Review packet")
	out = append(out, "")
	out = append(out, fmt.Sprintf("- Item: %s - %s", stringField(note.Data, "id"), stringField(note.Data, "title")))
	out = append(out, fmt.Sprintf("- Record: %s", run.RecordID))
	out = append(out, fmt.Sprintf("- Attempt: %s", run.ActiveAttemptID))
	out = append(out, fmt.Sprintf("- Runner: %s", run.Runner))
	out = append(out, fmt.Sprintf("- Work revision: %d", run.WorkRevision))
	out = append(out, fmt.Sprintf("- Workspace: %s", run.WorkspacePath))
	out = append(out, fmt.Sprintf("- Session: %s", run.SessionRef))
	out = append(out, fmt.Sprintf("- Started: %s", run.StartedAt))
	out = append(out, fmt.Sprintf("- Last event: %s", run.LastEventAt))
	out = append(out, "")
	out = append(out, "## Runtime artifacts", "")
	for _, artifact := range []struct {
		label string
		path  string
	}{
		{"prompt", run.PromptPath},
		{"events", run.EventSinkPath},
		{"raw log", run.RawLogPath},
		{"status", run.StatusPath},
	} {
		if strings.TrimSpace(artifact.path) != "" {
			out = append(out, fmt.Sprintf("- %s: `%s`", artifact.label, artifact.path))
		}
	}
	out = append(out, "", "## Turns", "")
	if len(turns) == 0 {
		out = append(out, "- No normalized turns were recorded for this attempt.")
	} else {
		for _, turn := range turns {
			out = append(out, fmt.Sprintf("- #%d `%s` status=%s tokens=%d input=%d output=%d last_event=%s error=%s",
				turn.TurnIndex, turn.TurnID, turn.Status, turn.TotalTokens, turn.InputTokens, turn.OutputTokens, turn.LastEventAt, firstNonEmpty(turn.LastError, "none")))
		}
	}
	out = append(out, "", "## Supervisor decisions", "")
	if len(supervisorDecisions) == 0 {
		out = append(out, "- No supervisor decisions were recorded for this attempt.")
	} else {
		for _, decision := range supervisorDecisions {
			out = append(out, fmt.Sprintf("- `%s` reason=%s parent_attempt=%s parent_session=%s branch=%s workspace=%s signal=%s tokens=%d at=%s",
				decision.Kind, firstNonEmpty(decision.Reason, "none"), firstNonEmpty(decision.ParentAttemptID, "none"), firstNonEmpty(decision.ParentSessionRef, "none"), firstNonEmpty(decision.BranchName, "none"), firstNonEmpty(decision.WorkspacePath, "none"), firstNonEmpty(decision.ContextSignal, "none"), decision.TotalTokens, decision.CreatedAt))
			if decision.ValidationDelta != "" || decision.MergeRule != "" {
				out = append(out, fmt.Sprintf("  validation_delta=%s merge_rule=%s", firstNonEmpty(decision.ValidationDelta, "none"), firstNonEmpty(decision.MergeRule, "none")))
			}
		}
	}
	out = append(out, "", "## Verification", "")
	out = append(out, "- Daemon observed a zero exit status for the runner process.")
	out = append(out, "- Human/verifier must still check claims against the current tree before approval.")
	out = append(out, "", "## Open risks", "")
	out = append(out, "- This packet summarizes daemon-observed runtime facts. It does not prove product correctness by itself.")
	return strings.Join(out, "\n") + "\n"
}

func latestRunEventAt(run RunStatus) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, path := range []string{run.EventSinkPath, run.RawLogPath} {
		info, err := os.Stat(strings.TrimSpace(path))
		if err != nil || info.Size() == 0 {
			continue
		}
		modTime := info.ModTime().UTC()
		if !found || modTime.After(latest) {
			latest = modTime
			found = true
		}
	}
	if found {
		return latest, true
	}
	for _, raw := range []string{run.LastEventAt, run.StartedAt, run.UpdatedAt} {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func runStallReason(run RunStatus, wf Workflow, now time.Time) (bool, string) {
	if RunnerName(run.Runner) != RunnerCodex || wf.Codex.StallTimeoutMS <= 0 {
		return false, ""
	}
	lastEventAt, ok := latestRunEventAt(run)
	if !ok {
		return false, ""
	}
	timeout := time.Duration(wf.Codex.StallTimeoutMS) * time.Millisecond
	if now.Sub(lastEventAt) <= timeout {
		return false, ""
	}
	return true, fmt.Sprintf("runner stalled: no codex events since %s", lastEventAt.Format(time.RFC3339))
}

func (d *Daemon) stopRunExecution(ctx context.Context, run RunStatus) (bool, error) {
	if handle := liveRegistry.Find(firstNonEmpty(run.ActiveAttemptID, run.ItemID, run.RecordID)); handle != nil {
		return true, handle.Interrupt(ctx)
	}
	if run.ProcessPID <= 0 || !processExists(run.ProcessPID) {
		return false, nil
	}
	if err := syscall.Kill(-run.ProcessPID, syscall.SIGINT); err != nil && !strings.Contains(err.Error(), "no such process") {
		return false, err
	}
	for i := 0; i < 6 && processExists(run.ProcessPID); i++ {
		time.Sleep(150 * time.Millisecond)
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-run.ProcessPID, syscall.SIGTERM)
		for i := 0; i < 4 && processExists(run.ProcessPID); i++ {
			time.Sleep(150 * time.Millisecond)
		}
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-run.ProcessPID, syscall.SIGKILL)
	}
	return true, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || !errors.Is(err, syscall.ESRCH)
}

func runnerForName(name string, wf Workflow) (Runner, string, error) {
	switch RunnerName(name) {
	case RunnerCodex:
		return &CodexRunner{}, wf.Codex.Command, nil
	case RunnerClaude:
		return &ClaudeRunner{}, wf.Claude.Command, nil
	default:
		return nil, "", tuskerError(errorConfigInvalid, "unsupported runner: "+name)
	}
}

func sessionStateForOutcome(outcome AttemptOutcome) string {
	switch outcome {
	case AttemptOutcomeSucceeded:
		return "open"
	case AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeCancelled:
		return "open"
	case AttemptOutcomeAbandoned:
		return "abandoned"
	default:
		return "open"
	}
}

func sessionStateForLeaseState(state LeaseState) string {
	switch state {
	case LeaseStateReleased:
		return "closed"
	case LeaseStateRetryQueued, LeaseStateClaimed, LeaseStateRunning, LeaseStateUnclaimed, LeaseStateInterrupted:
		return "open"
	default:
		return "open"
	}
}

func workspaceStrategyFromWorkflow(value string) WorkspaceStrategy {
	switch strings.TrimSpace(value) {
	case string(WorkspaceStrategyClone):
		return WorkspaceStrategyClone
	case string(WorkspaceStrategyCopy):
		return WorkspaceStrategyCopy
	default:
		return WorkspaceStrategyWorktree
	}
}

var workflowTemplatePlaceholder = regexp.MustCompile(`{{\s*([A-Za-z0-9_.]+)\s*}}`)

func renderAttemptPrompt(project RegisteredProject, wfFile WorkflowFile, note Note, workspacePath string, attemptNumber int) (string, error) {
	values := map[string]string{
		"project.name":   project.Name,
		"project.id":     project.ProjectID,
		"project.key":    project.ProjectKey,
		"vault.path":     project.VaultRoot,
		"repo.root":      project.RepoRoot,
		"workspace.path": workspacePath,
		"workflow.path":  wfFile.Path,
		"note.id":        stringField(note.Data, "id"),
		"note.record_id": stringField(note.Data, "record_id"),
		"note.title":     stringField(note.Data, "title"),
		"note.status":    stringField(note.Data, "status"),
		"note.type":      stringField(note.Data, "type"),
		"attempt.number": strconv.Itoa(attemptNumber),
	}
	rendered, err := renderStrictWorkflowTemplate(wfFile.Body, values)
	if err != nil {
		return "", tuskerError(errorConfigInvalid, err.Error(), withPath(wfFile.Path))
	}
	return strings.TrimSpace(rendered) + "\n", nil
}

func renderStrictWorkflowTemplate(template string, values map[string]string) (string, error) {
	var unknown []string
	rendered := workflowTemplatePlaceholder.ReplaceAllStringFunc(template, func(match string) string {
		parts := workflowTemplatePlaceholder.FindStringSubmatch(match)
		if len(parts) != 2 {
			unknown = append(unknown, match)
			return match
		}
		key := parts[1]
		value, ok := values[key]
		if !ok {
			unknown = append(unknown, key)
			return match
		}
		return value
	})
	if len(unknown) > 0 {
		return "", fmt.Errorf("WORKFLOW.md prompt template has unknown placeholder %q", unknown[0])
	}
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		return "", fmt.Errorf("WORKFLOW.md prompt template has malformed placeholder")
	}
	return rendered, nil
}

func markNoteReadyForReview(vaultPath, notePath string) error {
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		return err
	}
	if err := assertEvidenceGate(data, body, stringField(data, "id")); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	date := todayISO()
	prevStatus := stringField(data, "status")
	prevReview := stringField(data, "review_state")
	data["status"] = "in_review"
	data["review_state"] = "verification_requested"
	data["review_requested_at"] = now
	data["updated"] = date
	appendTransition(data, orderedTransition(now, "status", prevStatus, "in_review", "daemon", "runner attempt succeeded"))
	appendTransition(data, orderedTransition(now, "review", prevReview, "verification_requested", "daemon", "implementation pass completed"))
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — daemon — implementation pass completed; verification requested", date))
	content, err := serializeDocument(data, body, frontmatterOrderForType(stringField(data, "type")))
	if err != nil {
		return err
	}
	if err := writeText(notePath, content); err != nil {
		return err
	}
	autoReindex(vaultPath)
	return nil
}

func daemonRunCmd(args Args) error {
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer daemon.Close()
	return daemon.Run(context.Background(), args.Bool("once"))
}

func daemonStatusCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	status, err := store.DaemonStatus()
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "status": status})
		return nil
	}
	fmt.Printf("Daemon state root: %s\n", status["state_root"])
	fmt.Printf("Registered projects: %v\n", status["projects"])
	fmt.Printf("Active runs: %v / %v\n", status["activeRuns"], status["max_active_runs"])
	return nil
}

func daemonLimitsCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	if raw := strings.TrimSpace(args.String("max-active-runs")); raw != "" {
		limit := atoiSafe(raw)
		if limit <= 0 {
			return tuskerError(errorInvalidArg, "--max-active-runs must be > 0", withContext(map[string]any{"arg": "--max-active-runs", "value": raw}))
		}
		if err := store.SetGlobalActiveRunLimit(limit); err != nil {
			return err
		}
	}
	limit, err := store.GlobalActiveRunLimit()
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = 2
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "max_active_runs": limit})
		return nil
	}
	fmt.Printf("Daemon max active runs: %d\n", limit)
	return nil
}

func projectsAddCmd(args Args) error {
	repoRoot, err := filepath.Abs(firstNonEmpty(args.String("repo"), mustGetwd()))
	if err != nil {
		return err
	}
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if _, err := loadWorkflow(vaultPath); err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	project := newRegisteredProject(repoRoot, vaultPath)
	if err := store.UpsertProject(project); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "project": project})
		return nil
	}
	fmt.Printf("Registered project %s (%s)\n", project.Name, project.ProjectID)
	return nil
}

func projectsListCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "count": len(projects), "projects": projects})
		return nil
	}
	if len(projects) == 0 {
		fmt.Println("(no registered projects)")
		return nil
	}
	for _, project := range projects {
		state := "enabled"
		if !project.Enabled {
			state = "disabled"
		}
		fmt.Printf("%s %-8s %-8s %s\n", project.ProjectID, state, project.Health, project.RepoRoot)
	}
	return nil
}

func projectsLimitsCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	project, err := resolveRegisteredProject(store, args)
	if err != nil {
		return err
	}
	if raw := strings.TrimSpace(args.String("max-active-runs")); raw != "" {
		limit := atoiSafe(raw)
		if limit <= 0 {
			return tuskerError(errorInvalidArg, "--max-active-runs must be > 0", withContext(map[string]any{"arg": "--max-active-runs", "value": raw}))
		}
		if _, err := setWorkflowProjectRunLimit(project.VaultRoot, limit); err != nil {
			return err
		}
	}
	wfFile, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		return err
	}
	limit := projectActiveRunLimit(wfFile.Data)
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok": true,
			"project": map[string]any{
				"project_id":      project.ProjectID,
				"name":            project.Name,
				"repo_root":       project.RepoRoot,
				"vault_root":      project.VaultRoot,
				"max_active_runs": limit,
			},
		})
		return nil
	}
	fmt.Printf("Project %s max active runs: %d\n", project.Name, limit)
	return nil
}

func projectsEnableCmd(args Args) error {
	return setProjectEnabledCmd(args, true)
}

func projectsDisableCmd(args Args) error {
	return setProjectEnabledCmd(args, false)
}

func setProjectEnabledCmd(args Args, enabled bool) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	project, err := resolveRegisteredProject(store, args)
	if err != nil {
		if !enabled {
			if typed, ok := err.(*TuskerError); ok && typed.Code == errorNotFound {
				if args.Bool("json") {
					emitJSON(map[string]any{"ok": true, "already_disabled": true, "message": "project is not registered; daemon execution is already off"})
					return nil
				}
				fmt.Println("Project is not registered; daemon execution is already off.")
				return nil
			}
		}
		return err
	}
	activeRuns, err := store.CountProjectActiveRuns(project.ProjectID)
	if err != nil {
		return err
	}
	if err := store.SetProjectEnabled(project.ProjectID, enabled); err != nil {
		return err
	}
	updated := *project
	updated.Enabled = enabled
	if enabled {
		updated.Health = projectHealthHealthy
	} else {
		updated.Health = projectHealthDisabled
		updated.LastError = ""
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":               true,
			"project":          updated,
			"active_run_count": activeRuns,
		})
		return nil
	}
	verb := "Disabled"
	if enabled {
		verb = "Enabled"
	}
	fmt.Printf("%s project %s (%s)\n", verb, updated.Name, updated.ProjectID)
	if !enabled && activeRuns > 0 {
		fmt.Printf("Warning: %d active run(s) still exist for this project. They will not be redispatched, but they are still in runtime state.\n", activeRuns)
	}
	return nil
}

func projectsRemoveCmd(args Args) error {
	projectID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.RemoveProject(projectID); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "removed": projectID})
		return nil
	}
	fmt.Printf("Removed project %s\n", projectID)
	return nil
}

func refreshCmd(args Args) error {
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Println("Refresh complete")
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true})
	}
	return nil
}

func resolveRegisteredProject(store *RuntimeStore, args Args) (*RegisteredProject, error) {
	projects, err := store.ListProjects()
	if err != nil {
		return nil, err
	}
	if len(projects) == 0 {
		return nil, tuskerError(errorNotFound, "no registered projects")
	}
	if projectID := strings.TrimSpace(args.String("id")); projectID != "" {
		for _, project := range projects {
			if project.ProjectID == projectID {
				copy := project
				return &copy, nil
			}
		}
		return nil, tuskerError(errorNotFound, "project not found: "+projectID)
	}

	targets := []string{}
	for _, raw := range []string{args.String("repo"), args.String("vault"), mustGetwd()} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		targets = append(targets, abs)
	}

	bestIndex := -1
	bestScore := -1
	ambiguous := false
	for i, project := range projects {
		score := 0
		for _, target := range targets {
			score = maxInt(score, projectPathMatchScore(project, target))
		}
		if score == 0 {
			continue
		}
		if score > bestScore {
			bestIndex = i
			bestScore = score
			ambiguous = false
			continue
		}
		if score == bestScore {
			ambiguous = true
		}
	}
	if bestIndex == -1 {
		return nil, tuskerError(errorNotFound, "no registered project matches the current path; use --id or --repo")
	}
	if ambiguous {
		return nil, tuskerError(errorInvalidArg, "multiple registered projects match this path; use --id")
	}
	copy := projects[bestIndex]
	return &copy, nil
}

func projectPathMatchScore(project RegisteredProject, target string) int {
	score := 0
	for _, root := range []string{project.RepoRoot, project.VaultRoot} {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if pathWithin(root, target) {
			score = maxInt(score, len(canonicalPath(root)))
		}
	}
	return score
}

func pathWithin(root, target string) bool {
	root = canonicalPath(root)
	target = canonicalPath(target)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil && strings.TrimSpace(resolved) != "" {
		return filepath.Clean(resolved)
	}
	return path
}
