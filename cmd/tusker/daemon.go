package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Daemon struct {
	stateRoot             string
	store                 *RuntimeStore
	guard                 *daemonGuard
	dispatchRefusalReason string
	stream                *serveStreamBroker
	pollTaskStatuses      map[string]string
}

const (
	runLaneExecute = "execute"
	runLaneReview  = "review"

	daemonFirstEventDeadline     = 5 * time.Minute
	daemonHeartbeatDeadThreshold = 120 * time.Second
	tuskerSignsWarnLineLimit     = 60
)

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
	if once && strings.TrimSpace(d.dispatchRefusalReason) == "" {
		d.dispatchRefusalReason = oneShotDispatchRefusal("tusker daemon run --once")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
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
			case "stop":
				cancel()
				return daemonControlResponse{OK: true, Message: "daemon stop requested"}
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
		return d.PollOnce(runCtx)
	}
	serveServer, err := d.startServe(runCtx)
	if err != nil {
		return err
	}
	if serveServer != nil {
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = serveServer.Close(shutdownCtx)
		}()
	}
	for {
		if err := d.PollOnce(runCtx); err != nil {
			if errors.Is(err, context.Canceled) && runCtx.Err() != nil {
				return nil
			}
			return err
		}
		interval := d.nextPollInterval()
		select {
		case <-runCtx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func (d *Daemon) nextPollInterval() time.Duration {
	projects, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{})
	if err != nil || len(projects) == 0 {
		return 30 * time.Second
	}
	minInterval := 30 * time.Second
	found := false
	for _, project := range projects {
		if !project.Loadable() {
			continue
		}
		ms := project.Workflow.Data.Runtime.PollIntervalMS
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
		if run.ProcessPID > 0 && processIdentityMatches(*run) {
			return interruptRunProcess(d.store, run)
		}
		reason := "interrupt requested by operator; live runner handle not found and process is not running"
		return finishRuntimeRun(d.store, run, LeaseStateInterrupted, AttemptOutcomeCancelled, 130, reason, false)
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
		_ = d.store.MarkSessionState(run.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateInterrupted), "", run.LastError, run.Lane != runLaneReview)
	}
	return nil
}

func (d *Daemon) ReleaseRun(ctx context.Context, identity string) error {
	_ = ctx
	run, err := d.store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	if run.ProcessPID > 0 && processIdentityMatches(*run) {
		return tuskerError(errorInvalidTransition, "run process is still running; use tusker runs interrupt before release", withContext(map[string]any{"pid": run.ProcessPID}))
	}
	return finishRuntimeRun(d.store, run, LeaseStateReleased, AttemptOutcomeAbandoned, 0, "released dead run by operator", false)
}

func interruptRunProcess(store *RuntimeStore, run *RunStatus) error {
	if run == nil {
		return tuskerError(errorNotFound, "run not found")
	}
	if run.ProcessPID > 0 {
		pgid := processSignalGroup(*run)
		if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil && !strings.Contains(err.Error(), "no such process") {
			return err
		}
		for i := 0; i < 6 && processExists(run.ProcessPID); i++ {
			time.Sleep(150 * time.Millisecond)
		}
		if processExists(run.ProcessPID) {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			for i := 0; i < 4 && processExists(run.ProcessPID); i++ {
				time.Sleep(150 * time.Millisecond)
			}
		}
		if processExists(run.ProcessPID) {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
	return finishRuntimeRun(store, run, LeaseStateInterrupted, AttemptOutcomeCancelled, 130, "interrupt requested by operator", true)
}

// killSpawnedRunProcess terminates the process group of a run whose child was
// already spawned but whose lease was lost (revoked by a concurrent operator
// stop/interrupt) before the post-spawn row write landed. The stop's own
// interrupt saw ProcessPID=0 in the store and killed nothing, so without this
// the spawned group would run forever while the store shows the run stopped.
// It MUST be called with the local run value that still carries the live
// ProcessPID/ProcessPGID, never the store-fetched row (which has PID=0).
// Signalling an already-exited group is harmless (ESRCH).
func killSpawnedRunProcess(run RunStatus) {
	if run.ProcessPID <= 0 {
		return
	}
	pgid := processSignalGroup(run)
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	for i := 0; i < 4 && processExists(run.ProcessPID); i++ {
		time.Sleep(150 * time.Millisecond)
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

func finishRuntimeRun(store *RuntimeStore, run *RunStatus, state LeaseState, outcome AttemptOutcome, exitCode int, reason string, resumable bool) error {
	if store == nil || run == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	updateRunAttemptFromRun(store, *run, outcome, exitCode, reason, now)
	run.LeaseState = string(state)
	run.AttemptOutcome = string(outcome)
	run.NextRetryAt = ""
	run.LastError = reason
	run.UpdatedAt = now
	run.Terminal = false
	clearActiveExecution(run)
	if strings.TrimSpace(run.SessionRef) != "" {
		endedAt := ""
		if state == LeaseStateReleased {
			endedAt = now
		}
		_ = store.MarkSessionState(run.ProjectID, run.SessionRef, sessionStateForLeaseState(state), endedAt, reason, resumable)
	}
	return store.UpsertRun(*run)
}

func (d *Daemon) PollOnce(ctx context.Context) error {
	previousPollAt, err := d.store.GetSetting("daemon_last_poll_at")
	if err != nil {
		return err
	}
	nowPoll := time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.store.SetSetting("daemon_watchdog_beat_at", nowPoll); err != nil {
		return err
	}
	projects, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{Notes: true})
	if err != nil {
		return err
	}
	allRuns, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	sentinelProjects := []runtimeSentinelProjectSnapshot{}
	runsByProject := map[string]map[string]RunStatus{}
	for _, run := range allRuns {
		if runsByProject[run.ProjectID] == nil {
			runsByProject[run.ProjectID] = map[string]RunStatus{}
		}
		runsByProject[run.ProjectID][run.RecordID] = run
	}
	globalActiveRuns := countDispatchCapacityRuns(allRuns)
	globalLimit, err := d.globalActiveRunLimit()
	if err != nil {
		return err
	}

	for _, loaded := range projects {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !loaded.Loadable() {
			continue
		}

		project := loaded.Project
		wfFile := loaded.Workflow
		if _, err := d.refreshBudgetCircuitStatus(wfFile.Data, time.Now().UTC()); err != nil {
			return err
		}
		notes := loaded.Notes
		sortDispatchCandidates(notes)
		noteStatusByRecord := map[string]string{}
		notesByID, notesByRecordID := daemonNoteMaps(notes)
		keep := map[string]struct{}{}
		for _, note := range notes {
			if daemonNoteKind(note) != "task" {
				continue
			}
			recordID := trackerRecordID(note)
			if recordID == "" {
				continue
			}
			d.observeTaskStatusForStream(project.ProjectID, recordID, stringField(note.Data, "status"))
			noteStatusByRecord[recordID] = stringField(note.Data, "status")
			keep[recordID] = struct{}{}
		}
		sentinelProjects = append(sentinelProjects, runtimeSentinelProjectSnapshot{
			Project:         project,
			Workflow:        wfFile.Data,
			NotesByID:       notesByID,
			NotesByRecordID: notesByRecordID,
		})

		projectRuns := runsByProject[project.ProjectID]
		if projectRuns == nil {
			projectRuns = map[string]RunStatus{}
		}
		projectActiveRuns := countDispatchCapacityProjectRuns(projectRuns)
		now := time.Now().UTC()
		for recordID, current := range projectRuns {
			if note, ok := notesByRecordID[recordID]; ok {
				normalized, changed, err := d.normalizeDeadRetryQueuedRun(ctx, project, wfFile, current, note)
				if err != nil {
					return err
				}
				if changed {
					if err := d.upsertRunWithStream(current, normalized); err != nil {
						return err
					}
					projectRuns[recordID] = normalized
					current = normalized
				}
			}
			reconciled, changed, err := d.reconcileRunWithTracker(ctx, project, wfFile, current, notesByRecordID[recordID], notesByID, notesByRecordID)
			if err != nil {
				return err
			}
			if changed {
				if err := d.upsertRunWithStream(current, reconciled); err != nil {
					return err
				}
				projectRuns[recordID] = reconciled
				globalActiveRuns += dispatchCapacityRunDelta(current, reconciled)
				projectActiveRuns += dispatchCapacityRunDelta(current, reconciled)
				current = reconciled
			}
			capped, capChanged := d.parkRetryQueuedRunAtAttemptCap(project, wfFile.Data, current, "queued continuation retry would exceed cap")
			if capChanged {
				if err := d.upsertRunWithStream(current, capped); err != nil {
					return err
				}
				projectRuns[recordID] = capped
				globalActiveRuns += dispatchCapacityRunDelta(current, capped)
				projectActiveRuns += dispatchCapacityRunDelta(current, capped)
			}
			if note, ok := notesByRecordID[recordID]; ok {
				beforeAuto := projectRuns[recordID]
				autoAdvanced, autoChanged, err := d.autoAdvanceExternalLoop(ctx, project, wfFile, notes, note, beforeAuto)
				if err != nil {
					return err
				}
				if autoChanged {
					if err := d.upsertRunWithStream(beforeAuto, autoAdvanced); err != nil {
						return err
					}
					projectRuns[recordID] = autoAdvanced
					globalActiveRuns += dispatchCapacityRunDelta(beforeAuto, autoAdvanced)
					projectActiveRuns += dispatchCapacityRunDelta(beforeAuto, autoAdvanced)
				}
				beforeApplyAuto := projectRuns[recordID]
				applyAdvanced, applyChanged, err := d.autoAdvanceExternalApplyResult(ctx, project, wfFile, notes, note, beforeApplyAuto)
				if err != nil {
					return err
				}
				if applyChanged {
					if err := d.upsertRunWithStream(beforeApplyAuto, applyAdvanced); err != nil {
						return err
					}
					projectRuns[recordID] = applyAdvanced
					globalActiveRuns += dispatchCapacityRunDelta(beforeApplyAuto, applyAdvanced)
					projectActiveRuns += dispatchCapacityRunDelta(beforeApplyAuto, applyAdvanced)
				}
			}
		}
		stateActiveRuns := countDispatchCapacityProjectRunsByState(projectRuns, noteStatusByRecord)

		for _, note := range notes {
			if daemonNoteKind(note) != "task" {
				continue
			}
			status := stringField(note.Data, "status")
			if !containsString(wfFile.Data.Tracker.ActiveStates, status) {
				continue
			}
			recordID := trackerRecordID(note)
			if recordID == "" {
				continue
			}

			current := projectRuns[recordID]
			current.ProjectID = project.ProjectID
			current.RecordID = recordID
			current.ItemID = stringField(note.Data, "id")
			resolvedRunner := resolveRunnerForNote(note, wfFile.Data)
			if isDispatchingLeaseState(current.LeaseState) {
				current.Runner = firstNonEmpty(current.Runner, resolvedRunner, wfFile.Data.Agents.Default)
			} else {
				current.Runner = firstNonEmpty(resolvedRunner, current.Runner, wfFile.Data.Agents.Default)
			}
			current.Lane = firstNonEmpty(current.Lane, runLaneExecute)
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
				current.FirstEventAt = ""
				current.LastHeartbeatAt = ""
				current.Lane = runLaneExecute
				current.Terminal = false
				clearRunCloudRefs(&current)
				clearActiveExecution(&current)
			}
			if current.Lane == runLaneReview && LeaseState(current.LeaseState) == LeaseStateReleased {
				current = prepareRunForLaneDispatch(current, runLaneExecute, current.Runner)
				current.UpdatedAt = now.Format(time.RFC3339)
			}
			if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
				return err
			}
			projectRuns[recordID] = current

			if shouldDispatchRun(current, now) {
				if reason := daemonDispatchBlockedReason(project.VaultRoot, note, notesByID, notesByRecordID); reason != "" {
					current.LastError = reason
					current.UpdatedAt = now.Format(time.RFC3339)
					if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
						return err
					}
					projectRuns[recordID] = current
					continue
				}
				if dispatchCapacityLimitReached(globalActiveRuns, globalLimit, current) {
					continue
				}
				if dispatchCapacityLimitReached(projectActiveRuns, projectActiveRunLimit(wfFile.Data), current) {
					continue
				}
				beforeBudget := current
				budgeted, budgetReason, budgetChanged, err := d.budgetDispatchBlocker(project, wfFile.Data, note, current, now)
				if err != nil {
					return err
				}
				if budgetReason != "" {
					current = budgeted
					current.LastError = budgetReason
					current.UpdatedAt = now.Format(time.RFC3339)
					if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
						return err
					}
					projectRuns[recordID] = current
					if budgetChanged {
						globalActiveRuns += dispatchCapacityRunDelta(beforeBudget, current)
						projectActiveRuns += dispatchCapacityRunDelta(beforeBudget, current)
					}
					continue
				}
				invariantReason, err := d.invariantDispatchBlocker()
				if err != nil {
					return err
				}
				if invariantReason != "" {
					current.LastError = invariantReason
					current.UpdatedAt = now.Format(time.RFC3339)
					if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
						return err
					}
					projectRuns[recordID] = current
					continue
				}
				if stateDispatchCapReachedForRun(status, stateActiveRuns, wfFile.Data, current) {
					current.LastError = fmt.Sprintf("dispatch blocked: state %q concurrency cap reached", status)
					current.UpdatedAt = now.Format(time.RFC3339)
					if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
						return err
					}
					projectRuns[recordID] = current
					continue
				}
				if reason := strings.TrimSpace(d.dispatchRefusalReason); reason != "" {
					return tuskerError(errorInvalidTransition, reason, withContext(map[string]any{"task": recordID, "lane": runLaneExecute}))
				}
				updated, persisted, err := d.dispatchRun(ctx, project, wfFile, note, current, runLaneExecute)
				if !persisted {
					if err != nil {
						updated = d.scheduleRetry(updated, wfFile.Data, err.Error())
					}
					if err := d.upsertRunWithStream(current, updated); err != nil {
						return err
					}
				} else if err != nil {
					return err
				} else {
					d.emitLeaseTransitionStreamEvent(current, updated)
				}
				d.emitDispatchStreamEvent(updated)
				projectRuns[recordID] = updated
				globalActiveRuns += dispatchCapacityRunDelta(current, updated)
				projectActiveRuns += dispatchCapacityRunDelta(current, updated)
				stateActiveRuns[status] += dispatchCapacityRunDelta(current, updated)
			}
		}

		for _, note := range notes {
			if daemonNoteKind(note) != "task" {
				continue
			}
			status := stringField(note.Data, "status")
			if !containsString(wfFile.Data.Tracker.ReviewStates, status) {
				continue
			}
			recordID := trackerRecordID(note)
			if recordID == "" {
				continue
			}
			if externalLoopCloseTaskRecorded(d.store, project.ProjectID, recordID) {
				continue
			}
			current := projectRuns[recordID]
			if !reviewDispatchAllowed(project.VaultRoot, note, wfFile.Data, current) {
				continue
			}
			reviewerRunner := firstNonEmpty(wfFile.Data.Reviewer.Runner, wfFile.Data.Agents.Default)
			current.ProjectID = project.ProjectID
			current.RecordID = recordID
			current.ItemID = stringField(note.Data, "id")
			current.WorkRevision = intField(note.Data, "work_revision")
			current = prepareRunForLaneDispatch(current, runLaneReview, reviewerRunner)
			current.UpdatedAt = now.Format(time.RFC3339)
			if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
				return err
			}
			d.emitStreamEvent(serveStreamKindReviewBatch, "review:batch", "needs", "tasks", "runs", "projects")
			projectRuns[recordID] = current
			if !shouldDispatchRun(current, now) {
				continue
			}
			if dispatchCapacityLimitReached(globalActiveRuns, globalLimit, current) {
				continue
			}
			if dispatchCapacityLimitReached(projectActiveRuns, projectActiveRunLimit(wfFile.Data), current) {
				continue
			}
			budgeted, budgetReason, _, err := d.budgetDispatchBlocker(project, wfFile.Data, note, current, now)
			if err != nil {
				return err
			}
			if budgetReason != "" {
				current = budgeted
				current.LastError = budgetReason
				current.UpdatedAt = now.Format(time.RFC3339)
				if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
					return err
				}
				projectRuns[recordID] = current
				continue
			}
			invariantReason, err := d.invariantDispatchBlocker()
			if err != nil {
				return err
			}
			if invariantReason != "" {
				current.LastError = invariantReason
				current.UpdatedAt = now.Format(time.RFC3339)
				if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
					return err
				}
				projectRuns[recordID] = current
				continue
			}
			if stateDispatchCapReachedForRun(status, stateActiveRuns, wfFile.Data, current) {
				current.LastError = fmt.Sprintf("dispatch blocked: state %q concurrency cap reached", status)
				current.UpdatedAt = now.Format(time.RFC3339)
				if err := d.upsertRunWithStream(projectRuns[recordID], current); err != nil {
					return err
				}
				projectRuns[recordID] = current
				continue
			}
			if reason := strings.TrimSpace(d.dispatchRefusalReason); reason != "" {
				return tuskerError(errorInvalidTransition, reason, withContext(map[string]any{"task": recordID, "lane": runLaneReview}))
			}
			updated, persisted, err := d.dispatchRun(ctx, project, wfFile, note, current, runLaneReview)
			if !persisted {
				if err != nil {
					updated = d.scheduleRetry(updated, wfFile.Data, err.Error())
				}
				if err := d.upsertRunWithStream(current, updated); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				d.emitLeaseTransitionStreamEvent(current, updated)
			}
			d.emitDispatchStreamEvent(updated)
			projectRuns[recordID] = updated
			globalActiveRuns += dispatchCapacityRunDelta(current, updated)
			projectActiveRuns += dispatchCapacityRunDelta(current, updated)
			stateActiveRuns[status] += dispatchCapacityRunDelta(current, updated)
		}

		if err := d.store.DeleteRunsNotIn(project.ProjectID, keep); err != nil {
			return err
		}
		if err := d.store.TouchProjectPoll(project.ProjectID); err != nil {
			return err
		}
	}
	currentPollAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := d.store.SetSetting("daemon_last_poll_at", currentPollAt); err != nil {
		return err
	}
	finalRuns, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	tokenTotals, err := d.store.RunTokenTotalsByRun()
	if err != nil {
		return err
	}
	if _, err := d.refreshInvariantCircuitStatus(runtimeSentinelSnapshot{
		Projects:       sentinelProjects,
		Runs:           finalRuns,
		TokenTotals:    tokenTotals,
		PreviousPollAt: previousPollAt,
		CurrentPollAt:  currentPollAt,
		Now:            time.Now().UTC(),
		Liveness:       processIdentityMatches,
	}); err != nil {
		return err
	}
	d.emitPollTickStreamEvent()
	return nil
}

func projectActiveRunLimit(wf Workflow) int {
	if wf.Runtime.MaxActiveRunsPerProject > 0 {
		return wf.Runtime.MaxActiveRunsPerProject
	}
	return 1
}

func maxContinuationRetries(wf Workflow) int {
	if wf.Runtime.MaxContinuationRetries > 0 {
		return wf.Runtime.MaxContinuationRetries
	}
	return 3
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

// countDispatchCapacityRuns counts the runs that consume active-run capacity:
// leases in claimed or running state (matching RuntimeStore.CountProjectActiveRuns).
// retry_queued, released, interrupted, parked_*, and unclaimed rows wait for
// slots without consuming them; counting queued rows live-locks dispatch once
// the queue is longer than the cap (RUN-T-0036).
func countDispatchCapacityRuns(runs []RunStatus) int {
	count := 0
	for _, run := range runs {
		if isDispatchingLeaseState(run.LeaseState) {
			count++
		}
	}
	return count
}

func countDispatchCapacityProjectRuns(runs map[string]RunStatus) int {
	count := 0
	for _, run := range runs {
		if isDispatchingLeaseState(run.LeaseState) {
			count++
		}
	}
	return count
}

func countDispatchCapacityProjectRunsByState(runs map[string]RunStatus, stateByRecord map[string]string) map[string]int {
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

func stateDispatchCapReachedForRun(state string, activeByState map[string]int, wf Workflow, run RunStatus) bool {
	state = strings.TrimSpace(state)
	capValue := wf.Agents.MaxConcurrentAgentsByState[state]
	if capValue <= 0 {
		return false
	}
	return dispatchCapacityCountExcludingRun(activeByState[state], run) >= capValue
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

func daemonDispatchBlockedReason(vaultPath string, note Note, notesByID map[string]Note, notesByRecordID map[string]Note) string {
	var blockers []string
	if isV7TaskNote(note) {
		blockers = append(blockers, v7TaskDispatchBlockers(vaultPath, note)...)
	} else {
		status := stringField(note.Data, "status")
		if status != "ready" && status != "rework" {
			blockers = append(blockers, "status is "+fallback(status, "(missing)"))
		}
	}
	if reason := dispatchEligibilityReason(note, notesByID, notesByRecordID); reason != "" {
		blockers = append(blockers, strings.TrimPrefix(reason, "dispatch blocked: "))
	}
	blockers = uniqueStrings(blockers)
	if len(blockers) == 0 {
		return ""
	}
	return "dispatch blocked: " + strings.Join(blockers, "; ")
}

func trackerRecordID(note Note) string {
	return firstNonEmpty(stringField(note.Data, "record_id"), stringField(note.Data, "id"))
}

func dispatchEligibilityReason(note Note, notesByID map[string]Note, notesByRecordID map[string]Note) string {
	if strings.EqualFold(stringField(note.Data, "risk"), "critical") {
		return "dispatch blocked: critical risk requires explicit human dispatch"
	}
	if reason := unresolvedBlockerReason(note, notesByID, notesByRecordID); reason != "" {
		return "dispatch blocked: " + reason
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

// dispatchCapacityRunDelta reports how a lease-state transition changes the
// in-tick active-run tally. Only claimed/running rows consume capacity, so a
// successful dispatch of a queued row (retry_queued -> claimed) increments the
// tally by one and a single tick cannot dispatch past the cap (RUN-T-0036).
func dispatchCapacityRunDelta(before, after RunStatus) int {
	delta := 0
	if isDispatchingLeaseState(after.LeaseState) {
		delta++
	}
	if isDispatchingLeaseState(before.LeaseState) {
		delta--
	}
	return delta
}

func oneShotDispatchRefusal(command string) string {
	return strings.TrimSpace(command) + " cannot dispatch local runners from a one-shot CLI process; start the resident daemon with `tusker daemon run` and let it pick up ready/rework tasks"
}

func isDispatchingLeaseState(state string) bool {
	switch LeaseState(strings.TrimSpace(state)) {
	case LeaseStateClaimed, LeaseStateRunning:
		return true
	default:
		return false
	}
}

// isDispatchCapacityLeaseState reports whether a run row still holds live
// attempt/session state that the daemon must reconcile, budget-meter, or
// release, and that the sentinel treats as a held lease: a dispatching lease
// (claimed/running) or a queued retry that still owns its attempt refs.
// Despite the name it is NOT the active-run capacity predicate: capacity
// counts only claimed/running via isDispatchingLeaseState, so retry_queued
// rows wait for slots without consuming them (RUN-T-0036).
func isDispatchCapacityLeaseState(state string) bool {
	switch LeaseState(strings.TrimSpace(state)) {
	case LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued:
		return true
	default:
		return false
	}
}

func dispatchCapacityCountExcludingRun(count int, run RunStatus) int {
	if isDispatchingLeaseState(run.LeaseState) && count > 0 {
		return count - 1
	}
	return count
}

func dispatchCapacityLimitReached(active, limit int, run RunStatus) bool {
	return limit > 0 && dispatchCapacityCountExcludingRun(active, run) >= limit
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
	case LeaseStateParkedNoProgress, LeaseStateParkedBudget:
		return false
	default:
		return false
	}
}

func prepareRunForLaneDispatch(run RunStatus, lane, runner string) RunStatus {
	if strings.TrimSpace(runner) != "" {
		run.Runner = runner
	}
	if strings.TrimSpace(lane) != "" {
		run.Lane = lane
	}
	run.LeaseState = string(LeaseStateUnclaimed)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.NextRetryAt = ""
	run.LastError = ""
	run.SessionRef = ""
	run.Terminal = false
	clearRunCloudRefs(&run)
	clearActiveExecution(&run)
	return run
}

func reviewDispatchAllowed(vaultPath string, note Note, wf Workflow, run RunStatus) bool {
	if !wf.Reviewer.Enabled {
		return false
	}
	if stringField(note.Data, "status") != "review" {
		return false
	}
	if v7TerminalHumanWait(vaultPath, note, wf) {
		return false
	}
	if stringField(note.Data, "verified_at") != "" || stringField(note.Data, "closed_at") != "" {
		return false
	}
	risk := strings.ToLower(strings.TrimSpace(stringField(note.Data, "risk")))
	if !reviewerPolicyCoversRisk(wf.Reviewer, risk) {
		return false
	}
	if isDispatchingLeaseState(run.LeaseState) {
		return false
	}
	workRevision := intField(note.Data, "work_revision")
	if run.Lane == runLaneReview && run.WorkRevision == workRevision && run.AttemptCount > 0 {
		return false
	}
	return true
}

func daemonNoteKind(note Note) string {
	return firstNonEmpty(stringField(note.Data, "type"), stringField(note.Data, "kind"))
}

func v7TerminalHumanWait(vaultPath string, note Note, wf Workflow) bool {
	_ = wf
	if !isV7TaskNote(note) {
		return false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false
	}
	if task, ok := idx.Tasks[stringField(note.Data, "id")]; ok {
		note = task
	}
	_, ok := v7LatestValidTerminalCloseout(vaultPath, note, idx)
	return ok
}

func v7MachineCompleteWaitingForHuman(vaultPath string, note Note) (bool, string) {
	if !isV7TaskNote(note) {
		return false, ""
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return false, ""
	}
	task, ok := idx.Tasks[stringField(note.Data, "id")]
	if ok {
		note = task
	}
	if _, ok := v7LatestValidTerminalCloseout(vaultPath, note, idx); ok {
		report := computeV7ProofReport(vaultPath, note, idx)
		return true, v7HumanWaitReason(note, &report)
	}
	report := computeV7ProofReport(vaultPath, note, idx)
	report, terminalWait := v7CloseoutTerminalReport(vaultPath, note, report)
	if terminalWait {
		return false, v7HumanWaitReason(note, &report)
	}
	return false, ""
}

func isV7TaskNote(note Note) bool {
	return stringField(note.Data, "kind") == "task" && strings.HasSuffix(stringField(note.Data, "schema"), "/v7")
}

func v7HumanWaitReason(note Note, report *v7ProofReport) string {
	taskID := stringField(note.Data, "id")
	var parts []string
	if report != nil {
		if len(report.HumanMissing) > 0 {
			parts = append(parts, "human proof missing: "+strings.Join(report.HumanMissing, ", "))
		}
		if len(report.OpenHumanGates) > 0 {
			parts = append(parts, "open human gates: "+strings.Join(report.OpenHumanGates, ", "))
		}
	}
	if len(parts) == 0 {
		if refs := strings.Join(normalizeList(note.Data["next_ref"]), ", "); refs != "" {
			parts = append(parts, "pending human refs: "+refs)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "pending human response")
	}
	return taskID + ": machine work complete; " + strings.Join(parts, "; ")
}

func (d *Daemon) reconcileRunWithTracker(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, run RunStatus, note Note, notesByID map[string]Note, notesByRecordID map[string]Note) (RunStatus, bool, error) {
	trackerState := strings.TrimSpace(stringField(note.Data, "status"))
	if canonicalStatusRetiresRuntimeRows(wfFile.Data, trackerState) && !runnerStatusReadyForReconcile(run) {
		return d.retireCanonicalRuntimeRun(ctx, project, run, trackerState, "daemon:reconcile", "")
	}
	if !isDispatchCapacityLeaseState(run.LeaseState) {
		return d.reconcileRun(ctx, project, wfFile, run)
	}
	_ = d.ingestCodexExecRawLog(run)
	if strings.TrimSpace(note.AbsolutePath) != "" {
		if budgeted, changed, err := d.enforceBudgetForRun(ctx, project, wfFile.Data, note, run); err != nil || changed {
			return budgeted, changed, err
		}
		if turnCapped, changed, err := d.enforceTurnCapForRun(ctx, project, wfFile.Data, run); err != nil || changed {
			return turnCapped, changed, err
		}
	}
	if run.Lane == runLaneReview && containsString(wfFile.Data.Tracker.ReviewStates, trackerState) {
		return d.reconcileRun(ctx, project, wfFile, run)
	}
	if completedReviewHandoffCanReconcile(wfFile.Data, run, trackerState) {
		return d.reconcileRun(ctx, project, wfFile, run)
	}
	if activeReviewHandoffCanReconcile(wfFile.Data, run, trackerState) {
		return d.reconcileRun(ctx, project, wfFile, run)
	}
	if strings.TrimSpace(note.AbsolutePath) == "" {
		reason := fmt.Sprintf("tracker state %q is not dispatchable; daemon released run", firstNonEmpty(trackerState, "missing"))
		return d.releaseIneligibleRun(ctx, project, run, reason)
	}
	if reason, finished, ok := completedRunnerDispatchDecline(run); ok {
		return d.finishDispatchDeclinedRun(project, wfFile.Data, note, run, reason, finished)
	}
	if reason := daemonDispatchBlockedReason(project.VaultRoot, note, notesByID, notesByRecordID); reason != "" {
		return d.releaseIneligibleRun(ctx, project, run, reason+"; daemon released run")
	}
	return d.reconcileRun(ctx, project, wfFile, run)
}

func completedReviewHandoffCanReconcile(wf Workflow, run RunStatus, trackerState string) bool {
	if !isDispatchingLeaseState(run.LeaseState) {
		return false
	}
	if !containsString(wf.Tracker.ReviewStates, strings.TrimSpace(trackerState)) {
		return false
	}
	statusPath := strings.TrimSpace(run.StatusPath)
	return statusPath != "" && fileExists(statusPath)
}

func activeReviewHandoffCanReconcile(wf Workflow, run RunStatus, trackerState string) bool {
	if run.Lane == runLaneReview || !isDispatchingLeaseState(run.LeaseState) {
		return false
	}
	if !containsString(wf.Tracker.ReviewStates, strings.TrimSpace(trackerState)) {
		return false
	}
	return run.ProcessPID > 0 && processIdentityMatches(run)
}

func (d *Daemon) releaseIneligibleRun(ctx context.Context, project RegisteredProject, run RunStatus, reason string) (RunStatus, bool, error) {
	if !isDispatchCapacityLeaseState(run.LeaseState) {
		return run, false, nil
	}
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	interrupted := false
	if isDispatchingLeaseState(run.LeaseState) {
		var interruptErr error
		interrupted, interruptErr = d.stopRunExecution(ctx, run)
		if interruptErr != nil {
			reason = reason + ": " + interruptErr.Error()
		}
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
	run.Terminal = false
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

func (d *Daemon) runLeaseRenewalDispatchable(project RegisteredProject, wf Workflow, run RunStatus) bool {
	note, err := resolveNote(project.VaultRoot, run.RecordID)
	if err != nil {
		return false
	}
	status := strings.TrimSpace(stringField(note.Data, "status"))
	if run.Lane == runLaneReview {
		return containsString(wf.Tracker.ReviewStates, status)
	}
	return containsString(wf.Tracker.ActiveStates, status)
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
	sessionResumable := runSessionResumable(wfFile.Data, run)

	if d.ingestCodexExecRawLog(run) {
		changed = true
	}
	if strings.TrimSpace(run.RawLogPath) != "" {
		if sessionRef := extractSessionRef(run.RawLogPath); sessionRef != "" && sessionRef != run.SessionRef {
			run.SessionRef = sessionRef
			changed = true
		}
	}
	if lastEventAt, ok := latestObservedRunEventAt(run); ok {
		formatted := lastEventAt.Format(time.RFC3339)
		if formatted != run.LastEventAt {
			run.LastEventAt = formatted
			changed = true
		}
		if strings.TrimSpace(run.FirstEventAt) == "" {
			run.FirstEventAt = formatted
			changed = true
		}
		if formatted != run.LastHeartbeatAt {
			run.LastHeartbeatAt = formatted
			changed = true
		}
	}
	if err := recordRunBoundaryTraces(project, run); err != nil {
		return run, changed, err
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
			Resumable:      sessionResumable,
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
		finished := runnerProcessFinishedAt(status)
		run.ProcessPID = 0
		run.UpdatedAt = finished
		note, err := resolveNote(project.VaultRoot, run.RecordID)
		if err != nil {
			return run, changed, err
		}
		classification := classifyRunnerProcessExit(run, status, note, project.VaultRoot, wfFile.Data.Tracker.ActiveStates)
		if canonicalStatusRetiresRuntimeRows(wfFile.Data, classification.trackerState) {
			outcome := classification.outcome
			if status.ExitCode != 0 {
				outcome = AttemptOutcomeFailed
			}
			run.AttemptOutcome = string(outcome)
			run.LastError = classification.reason
			updateRunAttemptFromRun(d.store, run, outcome, status.ExitCode, classification.reason, finished)
			return d.retireCanonicalRuntimeRun(ctx, project, run, classification.trackerState, "daemon:reconcile", "status-ready")
		}
		if status.ExitCode == 0 {
			if classification.outcome == AttemptOutcomeTurnCapExhausted {
				reason := classification.reason
				updateRunAttemptFromRun(d.store, run, AttemptOutcomeTurnCapExhausted, 0, reason, finished)
				run = d.scheduleRetry(run, wfFile.Data, reason)
				if strings.TrimSpace(run.SessionRef) != "" {
					_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, sessionResumable)
				}
				run.UpdatedAt = finished
				return run, true, nil
			}
			if run.Lane != runLaneReview {
				if reason, ok := runnerStatusDispatchDeclineReason(status, run.RawLogPath); ok {
					return d.finishDispatchDeclinedRun(project, wfFile.Data, note, run, reason, finished)
				}
			}
			if run.Lane != runLaneReview {
				if reason, ok := completedRunnerReviewRequest(run, project.VaultRoot, wfFile.Data); ok {
					return d.finishReviewCompleteRun(project, run, reason, finished)
				}
			}
			if classification.outcome == AttemptOutcomeWaitingForHuman {
				reason := classification.reason
				parentAttemptID := run.ActiveAttemptID
				parentSessionRef := run.SessionRef
				run.LeaseState = string(LeaseStateReleased)
				run.AttemptOutcome = string(AttemptOutcomeWaitingForHuman)
				run.NextRetryAt = ""
				run.LastError = reason
				run.UpdatedAt = finished
				run.Terminal = false
				updateRunAttemptFromRun(d.store, run, AttemptOutcomeWaitingForHuman, 0, reason, finished)
				if strings.TrimSpace(run.SessionRef) != "" {
					_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateReleased), "", reason, false)
				}
				d.emitSupervisorDecision(SupervisorDecision{
					ProjectID:        project.ProjectID,
					RecordID:         run.RecordID,
					AttemptID:        parentAttemptID,
					SessionRef:       parentSessionRef,
					Kind:             string(SupervisorDecisionStopForHuman),
					Reason:           reason,
					ParentAttemptID:  parentAttemptID,
					ParentSessionRef: parentSessionRef,
					WorkspacePath:    run.WorkspacePath,
				})
				clearActiveExecution(&run)
				return run, true, nil
			}
			if classification.outcome == AttemptOutcomeEarlyExit {
				parentAttemptID := run.ActiveAttemptID
				parentSessionRef := run.SessionRef
				reason := classification.reason
				updateRunAttemptFromRun(d.store, run, AttemptOutcomeEarlyExit, 0, reason, finished)
				run, queued := d.scheduleContinuationRetry(run, wfFile.Data, reason)
				if strings.TrimSpace(run.SessionRef) != "" {
					_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", run.LastError, queued && sessionResumable)
				}
				decisionKind := string(SupervisorDecisionContinueAttempt)
				if !queued {
					updateRunAttemptFromRun(d.store, run, AttemptOutcomeBlocked, 0, run.LastError, finished)
					decisionKind = string(SupervisorDecisionStopForAudit)
				}
				d.emitSupervisorDecision(SupervisorDecision{
					ProjectID:        project.ProjectID,
					RecordID:         run.RecordID,
					AttemptID:        parentAttemptID,
					SessionRef:       parentSessionRef,
					Kind:             decisionKind,
					Reason:           run.LastError,
					ParentAttemptID:  parentAttemptID,
					ParentSessionRef: parentSessionRef,
					WorkspacePath:    run.WorkspacePath,
					LeaseState:       run.LeaseState,
				})
				run.UpdatedAt = finished
				return run, true, nil
			}
			if run.Lane == runLaneReview {
				if reason := reviewerWorkspaceDirtyReason(run.WorkspacePath); reason != "" {
					parentAttemptID := run.ActiveAttemptID
					parentSessionRef := run.SessionRef
					updateRunAttemptFromRun(d.store, run, AttemptOutcomeBlocked, 1, reason, finished)
					run.LeaseState = string(LeaseStateReleased)
					run.AttemptOutcome = string(AttemptOutcomeBlocked)
					run.NextRetryAt = ""
					run.LastError = reason
					run.UpdatedAt = finished
					run.Terminal = false
					if strings.TrimSpace(run.SessionRef) != "" {
						_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateReleased), "", reason, false)
					}
					d.emitSupervisorDecision(SupervisorDecision{
						ProjectID:        project.ProjectID,
						RecordID:         run.RecordID,
						AttemptID:        parentAttemptID,
						SessionRef:       parentSessionRef,
						Kind:             string(SupervisorDecisionStopForAudit),
						Reason:           reason,
						ParentAttemptID:  parentAttemptID,
						ParentSessionRef: parentSessionRef,
						WorkspacePath:    run.WorkspacePath,
					})
					clearActiveExecution(&run)
					return run, true, nil
				}
			}
			noteStatus := classification.trackerState
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
			run.Terminal = false
			updateRunAttemptFromRun(d.store, run, AttemptOutcomeSucceeded, 0, "", finished)
			if strings.TrimSpace(run.SessionRef) != "" {
				_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForOutcome(AttemptOutcomeSucceeded), "", "", sessionResumable)
			}
			clearActiveExecution(&run)
			return run, true, nil
		}
		reason := classification.reason
		updateRunAttemptFromRun(d.store, run, AttemptOutcomeFailed, classification.exitCode, reason, finished)
		run = d.scheduleRetry(run, wfFile.Data, reason)
		if strings.TrimSpace(run.SessionRef) != "" {
			_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, sessionResumable)
		}
		clearActiveExecution(&run)
		run.UpdatedAt = finished
		return run, true, nil
	}

	if recovered, recoveredChanged := recoverWrapperLeaseIdentity(run); recoveredChanged {
		run = recovered
		changed = true
	}

	if run.ProcessPID > 0 && processIdentityMatches(run) {
		if run.LeaseGeneration > 0 && strings.TrimSpace(firstNonEmpty(run.LeaseOwner, run.ActiveAttemptID)) != "" {
			renewDispatchable := d.runLeaseRenewalDispatchable(project, wfFile.Data, run)
			if renewed, err := d.store.RenewRunLease(RuntimeLeaseRenewal{
				ProjectID:      project.ProjectID,
				RecordID:       run.RecordID,
				Owner:          firstNonEmpty(run.LeaseOwner, run.ActiveAttemptID),
				Generation:     run.LeaseGeneration,
				TTL:            defaultRunLeaseTTL,
				Now:            nowTime,
				Dispatchable:   renewDispatchable,
				ProcessPID:     run.ProcessPID,
				ProcessPGID:    run.ProcessPGID,
				ProcessStarted: run.ProcessStartedAt,
			}); err != nil {
				return run, changed, err
			} else if renewed {
				run.LeaseExpiresAt = nowTime.Add(defaultRunLeaseTTL).Format(time.RFC3339)
				run.LeaseHost = runtimeLeaseHost()
				if strings.TrimSpace(run.LeaseOwner) == "" {
					run.LeaseOwner = run.ActiveAttemptID
				}
				changed = true
			}
		}
		if stalled, reason := runStallReason(run, wfFile.Data, nowTime); stalled {
			if _, err := d.stopRunExecution(ctx, run); err != nil {
				reason = reason + ": " + err.Error()
			}
			updateRunAttemptFromRun(d.store, run, AttemptOutcomeFailed, 124, reason, now)
			run = d.scheduleRetry(run, wfFile.Data, reason)
			run.UpdatedAt = now
			if strings.TrimSpace(run.SessionRef) != "" {
				_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, sessionResumable)
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

	if run.ProcessPID > 0 {
		reason := fmt.Sprintf("runner process no longer matches recorded identity pid=%d pgid=%d start_time=%s", run.ProcessPID, run.ProcessPGID, firstNonEmpty(run.ProcessStartedAt, "unknown"))
		parentAttemptID := run.ActiveAttemptID
		parentSessionRef := run.SessionRef
		workspacePath := run.WorkspacePath
		updateRunAttemptFromRun(d.store, run, AttemptOutcomeCancelled, 130, reason, now)
		if capped, capReached := d.enforceAttemptCreationCap(wfFile.Data, run, attemptCreationReclaim, reason+"; reclaim would create another attempt"); capReached {
			run = capped
			if strings.TrimSpace(parentSessionRef) != "" {
				_ = d.store.MarkSessionState(project.ProjectID, parentSessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", run.LastError, false)
			}
			d.emitSupervisorDecision(SupervisorDecision{
				ProjectID:        project.ProjectID,
				RecordID:         run.RecordID,
				Kind:             string(SupervisorDecisionStopForAudit),
				Reason:           run.LastError,
				ParentAttemptID:  parentAttemptID,
				ParentSessionRef: parentSessionRef,
				WorkspacePath:    workspacePath,
				LeaseState:       run.LeaseState,
			})
			return run, true, nil
		}
		run = d.scheduleRetry(run, wfFile.Data, reason)
		if strings.TrimSpace(run.SessionRef) != "" {
			_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", reason, sessionResumable)
		}
		return run, true, nil
	}

	runner, _, err := runnerForName(run.Runner, wfFile.Data)
	if err != nil {
		return run, changed, err
	}
	result, err := runner.Reconcile(ctx, ReconcileRequest{
		Runner: run.Runner, ProjectID: project.ProjectID, RecordID: run.RecordID, AttemptID: run.ActiveAttemptID, SessionRef: run.SessionRef, CloudTaskID: run.CloudTaskID,
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
	applyReconcileResultCloud(&run, result)
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(result.LeaseState), "", result.Reason, result.Outcome != AttemptOutcomeAbandoned && sessionResumable)
	}
	if result.LeaseState == LeaseStateClaimed || result.LeaseState == LeaseStateRunning {
		run.AttemptOutcome = string(AttemptOutcomeNone)
		return run, true, nil
	}
	if result.Outcome != AttemptOutcomeNone || result.LeaseState == LeaseStateReleased {
		updateRunAttemptFromRun(d.store, run, result.Outcome, exitCodeForOutcome(result.Outcome), result.Reason, now)
	}
	if result.LeaseState == LeaseStateRetryQueued && result.Outcome == AttemptOutcomeNone {
		run, queued := d.scheduleContinuationRetry(run, wfFile.Data, firstNonEmpty(result.Reason, "runner requested continuation retry"))
		if strings.TrimSpace(run.SessionRef) != "" {
			_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseState(run.LeaseState)), "", run.LastError, queued && sessionResumable)
		}
		decisionKind := string(SupervisorDecisionContinueAttempt)
		if !queued {
			updateRunAttemptFromRun(d.store, run, AttemptOutcomeBlocked, 0, run.LastError, now)
			decisionKind = string(SupervisorDecisionStopForAudit)
		}
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        project.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        parentAttemptID,
			SessionRef:       parentSessionRef,
			Kind:             decisionKind,
			Reason:           run.LastError,
			ParentAttemptID:  parentAttemptID,
			ParentSessionRef: parentSessionRef,
			WorkspacePath:    run.WorkspacePath,
			LeaseState:       run.LeaseState,
		})
		return run, true, nil
	}
	clearActiveExecution(&run)
	return run, true, nil
}

func recoverWrapperLeaseIdentity(run RunStatus) (RunStatus, bool) {
	wrapper, ok := wrapperLeaseIdentityFromEvents(run)
	if !ok {
		return run, false
	}
	if run.ProcessPID == wrapper.ProcessPID &&
		run.ProcessPGID == wrapper.ProcessPGID &&
		strings.TrimSpace(run.ProcessStartedAt) == strings.TrimSpace(wrapper.ProcessStartedAt) {
		return run, false
	}
	run.ProcessPID = wrapper.ProcessPID
	run.ProcessPGID = wrapper.ProcessPGID
	run.ProcessStartedAt = wrapper.ProcessStartedAt
	return run, true
}

func wrapperLeaseIdentityFromEvents(run RunStatus) (RunStatus, bool) {
	path := strings.TrimSpace(run.EventSinkPath)
	if path == "" {
		return RunStatus{}, false
	}
	text, err := readText(path)
	if err != nil {
		return RunStatus{}, false
	}
	attemptID := strings.TrimSpace(run.ActiveAttemptID)
	var latest RunStatus
	found := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if strings.TrimSpace(stringValue(event["kind"])) != "attempt_wrapper_spawned" {
			continue
		}
		if attemptID != "" && strings.TrimSpace(stringValue(event["attempt_id"])) != attemptID {
			continue
		}
		payload, _ := event["payload"].(map[string]any)
		pid := intFromAny(payload["pid"])
		processStart := strings.TrimSpace(stringValue(payload["process_start"]))
		if pid <= 0 || processStart == "" {
			continue
		}
		candidate := RunStatus{
			ProcessPID:       pid,
			ProcessPGID:      intFromAny(payload["pgid"]),
			ProcessStartedAt: processStart,
		}
		if !processIdentityMatches(candidate) {
			continue
		}
		latest = candidate
		found = true
	}
	return latest, found
}

func (d *Daemon) normalizeDeadRetryQueuedRun(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, run RunStatus, note Note) (RunStatus, bool, error) {
	_ = ctx
	if LeaseState(strings.TrimSpace(run.LeaseState)) != LeaseStateRetryQueued {
		return run, false, nil
	}
	if run.ProcessPID <= 0 || processIdentityMatches(run) {
		return run, false, nil
	}
	reason := fmt.Sprintf("retry_queued run pid %d is dead", run.ProcessPID)
	if capped, capReached := d.enforceAttemptCreationCap(wfFile.Data, run, attemptCreationReclaim, reason+"; reclaim would create another attempt"); capReached {
		parentAttemptID := run.ActiveAttemptID
		parentSessionRef := run.SessionRef
		run = capped
		if strings.TrimSpace(parentSessionRef) != "" {
			_ = d.store.MarkSessionState(project.ProjectID, parentSessionRef, sessionStateForLeaseState(LeaseStateParkedNoProgress), "", run.LastError, false)
		}
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        project.ProjectID,
			RecordID:         run.RecordID,
			Kind:             string(SupervisorDecisionStopForAudit),
			Reason:           run.LastError,
			ParentAttemptID:  parentAttemptID,
			ParentSessionRef: parentSessionRef,
			WorkspacePath:    run.WorkspacePath,
			LeaseState:       run.LeaseState,
		})
		return run, true, nil
	}
	if strings.TrimSpace(run.SessionRef) != "" {
		runner, _, err := runnerForName(run.Runner, wfFile.Data)
		if err != nil {
			return run, false, err
		}
		if runner.Capabilities().ResumeSession {
			if session, err := d.store.FindSessionByRef(project.ProjectID, run.SessionRef); err != nil {
				return run, false, err
			} else if session != nil && incompatibleResumeSessionReason(project, run, session) == "" {
				run.NextRetryAt = time.Now().UTC().Format(time.RFC3339)
				run.LastError = reason + "; queued resident daemon resume"
				run.UpdatedAt = run.NextRetryAt
				clearActiveExecution(&run)
				_ = d.store.MarkSessionState(project.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateRetryQueued), "", run.LastError, run.Lane != runLaneReview)
				d.emitSupervisorDecision(SupervisorDecision{
					ProjectID:        project.ProjectID,
					RecordID:         run.RecordID,
					Kind:             string(SupervisorDecisionContinueAttempt),
					Reason:           run.LastError,
					ParentAttemptID:  session.LastAttemptID,
					ParentSessionRef: run.SessionRef,
					WorkspacePath:    run.WorkspacePath,
				})
				return run, true, nil
			}
		}
	}
	_ = note
	if err := finishRuntimeRun(d.store, &run, LeaseStateReleased, AttemptOutcomeAbandoned, 0, reason+"; released dead retry lease", false); err != nil {
		return run, false, err
	}
	return run, true, nil
}

func (d *Daemon) scheduleContinuationRetry(run RunStatus, wf Workflow, reason string) (RunStatus, bool) {
	if capped, capReached := d.enforceAttemptCreationCap(wf, run, attemptCreationContinuation, reason); capReached {
		run = capped
		return run, false
	}
	now := time.Now().UTC()
	run.LeaseState = string(LeaseStateRetryQueued)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.LastError = reason
	run.NextRetryAt = now.Add(time.Second).Format(time.RFC3339)
	run.UpdatedAt = now.Format(time.RFC3339)
	run.Terminal = false
	clearActiveExecution(&run)
	return run, true
}

const runnerEarlyExitActiveTrackerReason = "runner early exit while tracker state remained active"

const runnerReviewCompleteAwaitingLandReason = "runner requested review; work complete in worktree, awaiting land"

// completedRunnerReviewRequest reports whether a clean-exited runner flipped its
// worktree-local tracker into a review/terminal state during this attempt — the
// review-request signal the runner itself controls at exit via
// `tusker finish --request-review`. This is the write-side mirror of RUN-T-0037:
// the worktree tracker is attempt-local scratch that has not yet reached the
// canonical vault, so the daemon must read the runner's own worktree copy to see
// the completion signal instead of requiring the flip to land in canonical
// first. When the workspace is the canonical repo (in-place), the flip is already
// visible via canonical state and the normal success path handles it, so this
// returns false to avoid double-handling.
func completedRunnerReviewRequest(run RunStatus, canonicalVaultPath string, wf Workflow) (string, bool) {
	if run.Lane == runLaneReview {
		return "", false
	}
	statusPath := strings.TrimSpace(run.StatusPath)
	if statusPath == "" || !fileExists(statusPath) {
		return "", false
	}
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil || status.ExitCode != 0 {
		return "", false
	}
	worktreeVault := runnerWorktreeVaultPath(run.WorkspacePath, canonicalVaultPath)
	if worktreeVault == "" || workspacePathsCompatible(worktreeVault, canonicalVaultPath) {
		return "", false
	}
	note, err := resolveNote(worktreeVault, run.RecordID)
	if err != nil {
		return "", false
	}
	worktreeStatus := strings.TrimSpace(stringField(note.Data, "status"))
	if worktreeStatus == "" {
		return "", false
	}
	if !containsString(wf.Tracker.ReviewStates, worktreeStatus) && !trackerStateTerminal(wf, worktreeStatus) {
		return "", false
	}
	return fmt.Sprintf("%s (worktree tracker=%s)", runnerReviewCompleteAwaitingLandReason, worktreeStatus), true
}

// runnerWorktreeVaultPath resolves the worktree-local Tusker vault directory for
// a runner workspace given the canonical vault path. The vault sits at the same
// repo-relative location inside the runner's worktree as the canonical vault does
// inside the control repo (typically `.tusker`).
func runnerWorktreeVaultPath(workspacePath, canonicalVaultPath string) string {
	workspacePath = strings.TrimSpace(workspacePath)
	canonicalVaultPath = strings.TrimSpace(canonicalVaultPath)
	if workspacePath == "" || canonicalVaultPath == "" {
		return ""
	}
	rel := relativeFromRepo(v7RepoRoot(canonicalVaultPath), canonicalVaultPath)
	if rel == "" || rel == "." || filepath.IsAbs(rel) {
		rel = filepath.Base(canonicalVaultPath)
	}
	if strings.TrimSpace(rel) == "" {
		rel = defaultRepoVaultDir
	}
	return filepath.Join(workspacePath, filepath.FromSlash(rel))
}

func completedRunnerDispatchDecline(run RunStatus) (string, string, bool) {
	statusPath := strings.TrimSpace(run.StatusPath)
	if statusPath == "" || !fileExists(statusPath) {
		return "", "", false
	}
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil || status.ExitCode != 0 {
		return "", "", false
	}
	reason, ok := runnerStatusDispatchDeclineReason(status, run.RawLogPath)
	if !ok {
		return "", "", false
	}
	return reason, firstNonEmpty(status.CompletedAt, time.Now().UTC().Format(time.RFC3339)), true
}

func runnerStatusDispatchDeclineReason(status runnerProcessStatus, rawLogPath string) (string, bool) {
	if AttemptOutcome(strings.TrimSpace(status.Outcome)) == AttemptOutcomeDispatchDeclined {
		return firstNonEmpty(strings.TrimSpace(status.Reason), "runner dispatch declined"), true
	}
	return runnerRawLogDispatchDeclineReason(rawLogPath)
}

func runnerRawLogDispatchDeclineReason(rawLogPath string) (string, bool) {
	rawLogPath = strings.TrimSpace(rawLogPath)
	if rawLogPath == "" {
		return "", false
	}
	text, err := readText(rawLogPath)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		command, output, ok := rawLogCommandExecution(line)
		if !ok {
			continue
		}
		if !strings.Contains(command, "tusker automation plan") {
			continue
		}
		if reason, ok := automationPlanDeclineReason(output); ok {
			return reason, true
		}
	}
	return "", false
}

func rawLogCommandExecution(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return "", "", false
	}
	item, itemOK := rawLogMapStringAny(payload["item"])
	if !itemOK {
		if params, paramsOK := rawLogMapStringAny(payload["params"]); paramsOK {
			item, itemOK = rawLogMapStringAny(params["item"])
		}
	}
	if !itemOK {
		return "", "", false
	}
	command := strings.TrimSpace(rawLogStringField(item, "command"))
	output := firstNonEmpty(rawLogStringField(item, "aggregated_output"), rawLogStringField(item, "aggregatedOutput"), rawLogStringField(item, "output"))
	if command == "" || strings.TrimSpace(output) == "" {
		return "", "", false
	}
	return command, output, true
}

func rawLogStringField(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return stringValue(value)
}

func rawLogMapStringAny(value any) (map[string]any, bool) {
	out, ok := value.(map[string]any)
	return out, ok
}

func automationPlanDeclineReason(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}
	for _, candidate := range jsonObjectCandidates(output) {
		var payload struct {
			Plan struct {
				Decision string   `json:"decision"`
				Blockers []string `json:"blockers"`
			} `json:"plan"`
		}
		if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
			continue
		}
		if strings.TrimSpace(payload.Plan.Decision) == "do_not_dispatch" {
			return dispatchDeclinedReason(payload.Plan.Blockers), true
		}
	}
	if strings.Contains(output, `"decision":"do_not_dispatch"`) || strings.Contains(output, `"decision": "do_not_dispatch"`) {
		return "runner dispatch declined", true
	}
	return "", false
}

func jsonObjectCandidates(text string) []string {
	text = strings.TrimSpace(text)
	candidates := []string{text}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(text[start : end+1])
		if candidate != text {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func dispatchDeclinedReason(blockers []string) string {
	blockers = uniqueStrings(blockers)
	if len(blockers) == 0 {
		return "runner dispatch declined"
	}
	return "runner dispatch declined: " + strings.Join(blockers, "; ")
}

func (d *Daemon) finishDispatchDeclinedRun(project RegisteredProject, wf Workflow, note Note, run RunStatus, reason, finished string) (RunStatus, bool, error) {
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	reason = firstNonEmpty(strings.TrimSpace(reason), "runner dispatch declined")
	finished = firstNonEmpty(strings.TrimSpace(finished), time.Now().UTC().Format(time.RFC3339))
	updateRunAttemptFromRun(d.store, run, AttemptOutcomeDispatchDeclined, 0, reason, finished)
	run.LeaseState = string(LeaseStateReleased)
	run.AttemptOutcome = string(AttemptOutcomeDispatchDeclined)
	run.NextRetryAt = ""
	run.LastError = reason
	run.UpdatedAt = finished
	run.Terminal = trackerStateTerminal(wf, stringField(note.Data, "status"))
	clearActiveExecution(&run)
	if strings.TrimSpace(parentSessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, parentSessionRef, sessionStateForLeaseState(LeaseStateReleased), finished, reason, false)
	}
	d.emitSupervisorDecision(SupervisorDecision{
		ProjectID:        project.ProjectID,
		RecordID:         run.RecordID,
		AttemptID:        parentAttemptID,
		SessionRef:       parentSessionRef,
		Kind:             string(SupervisorDecisionStopForAudit),
		Reason:           reason,
		ParentAttemptID:  parentAttemptID,
		ParentSessionRef: parentSessionRef,
		WorkspacePath:    run.WorkspacePath,
		LeaseState:       run.LeaseState,
	})
	return run, true, nil
}

// finishReviewCompleteRun terminates a run whose runner completed its work and
// requested review in its worktree. The completion signal is the runner's own
// review flip (see completedRunnerReviewRequest), so the attempt scores a
// terminal review-complete outcome — never early_exit — and the lease releases
// without queuing a continuation. Re-dispatch is prevented because
// shouldDispatchRun declines released runs, so the row waits for the landing
// lane instead of churning to the park guard.
func (d *Daemon) finishReviewCompleteRun(project RegisteredProject, run RunStatus, reason, finished string) (RunStatus, bool, error) {
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	reason = firstNonEmpty(strings.TrimSpace(reason), runnerReviewCompleteAwaitingLandReason)
	finished = firstNonEmpty(strings.TrimSpace(finished), time.Now().UTC().Format(time.RFC3339))
	updateRunAttemptFromRun(d.store, run, AttemptOutcomeWaitingForReview, 0, reason, finished)
	run.LeaseState = string(LeaseStateReleased)
	run.AttemptOutcome = string(AttemptOutcomeWaitingForReview)
	run.NextRetryAt = ""
	run.LastError = reason
	run.UpdatedAt = finished
	run.Terminal = false
	clearActiveExecution(&run)
	if strings.TrimSpace(parentSessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, parentSessionRef, sessionStateForLeaseState(LeaseStateReleased), finished, reason, false)
	}
	d.emitSupervisorDecision(SupervisorDecision{
		ProjectID:        project.ProjectID,
		RecordID:         run.RecordID,
		AttemptID:        parentAttemptID,
		SessionRef:       parentSessionRef,
		Kind:             string(SupervisorDecisionStopForHuman),
		Reason:           reason,
		ParentAttemptID:  parentAttemptID,
		ParentSessionRef: parentSessionRef,
		WorkspacePath:    run.WorkspacePath,
		LeaseState:       run.LeaseState,
	})
	return run, true, nil
}

func trackerStateTerminal(wf Workflow, status string) bool {
	status = strings.TrimSpace(status)
	if status == "" {
		return false
	}
	if containsString(wf.Tracker.TerminalStates, status) {
		return true
	}
	switch status {
	case "done", "cancelled", "superseded":
		return true
	default:
		return false
	}
}

func (d *Daemon) parkRetryQueuedRunAtAttemptCap(project RegisteredProject, wf Workflow, run RunStatus, reason string) (RunStatus, bool) {
	if LeaseState(strings.TrimSpace(run.LeaseState)) != LeaseStateRetryQueued {
		return run, false
	}
	kind := attemptCreationKindForDispatch(run)
	capped, capReached := d.enforceAttemptCreationCap(wf, run, kind, reason)
	if !capReached {
		return run, false
	}
	parentAttemptID := run.ActiveAttemptID
	parentSessionRef := run.SessionRef
	run = capped
	if strings.TrimSpace(parentSessionRef) != "" {
		_ = d.store.MarkSessionState(project.ProjectID, parentSessionRef, sessionStateForLeaseState(LeaseStateParkedNoProgress), "", run.LastError, false)
	}
	d.emitSupervisorDecision(SupervisorDecision{
		ProjectID:        project.ProjectID,
		RecordID:         run.RecordID,
		Kind:             string(SupervisorDecisionStopForAudit),
		Reason:           run.LastError,
		ParentAttemptID:  parentAttemptID,
		ParentSessionRef: parentSessionRef,
		WorkspacePath:    run.WorkspacePath,
		LeaseState:       run.LeaseState,
	})
	return run, true
}

type attemptCreationKind string

const (
	attemptCreationFreshDispatch attemptCreationKind = "fresh dispatch"
	attemptCreationContinuation  attemptCreationKind = "continuation"
	attemptCreationReclaim       attemptCreationKind = "reclaim"
	attemptCreationRedrive       attemptCreationKind = "redrive"
	attemptCreationRetry         attemptCreationKind = "retry"
)

func (d *Daemon) enforceAttemptCreationCap(wf Workflow, run RunStatus, kind attemptCreationKind, reason string) (RunStatus, bool) {
	if kind == attemptCreationContinuation {
		limit := maxContinuationRetries(wf)
		if limit > 0 && d.continuationRetryCount(run) >= limit {
			return parkNoProgressRun(run, fmt.Sprintf("continuation retry cap reached (%d): %s", limit, firstNonEmpty(reason, "continuation would create another attempt"))), true
		}
	}
	limit := attemptCreationCap(wf, kind)
	if limit <= 0 || run.AttemptCount < limit {
		return run, false
	}
	return parkNoProgressRun(run, fmt.Sprintf("attempt cap reached (%d): %s", limit, attemptCreationCapReason(kind, reason))), true
}

func attemptCreationCap(wf Workflow, kind attemptCreationKind) int {
	if kind == attemptCreationContinuation {
		return maxContinuationRetries(wf)
	}
	return wf.Retry.MaxAttempts
}

func attemptCreationCapReason(kind attemptCreationKind, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = string(kind) + " would create another attempt"
	}
	return reason
}

func attemptCreationKindForDispatch(run RunStatus) attemptCreationKind {
	if LeaseState(strings.TrimSpace(run.LeaseState)) != LeaseStateRetryQueued {
		return attemptCreationFreshDispatch
	}
	if strings.HasPrefix(strings.TrimSpace(run.LastError), "redriven by ") ||
		(run.AttemptCount == 0 && strings.TrimSpace(run.ActiveAttemptID) == "") {
		return attemptCreationRedrive
	}
	if strings.TrimSpace(run.SessionRef) != "" || strings.Contains(strings.ToLower(run.LastError), "continuation") {
		return attemptCreationContinuation
	}
	return attemptCreationRetry
}

func runSessionResumable(wf Workflow, run RunStatus) bool {
	if run.Lane == runLaneReview {
		return false
	}
	runner, _, err := runnerForName(run.Runner, wf)
	if err != nil {
		return false
	}
	return runner.Capabilities().ResumeSession
}

func (d *Daemon) continuationRetryCount(run RunStatus) int {
	if d == nil || d.store == nil {
		if run.AttemptCount > 0 {
			return run.AttemptCount - 1
		}
		return 0
	}
	decisions, err := d.store.ListSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		if run.AttemptCount > 0 {
			return run.AttemptCount - 1
		}
		return 0
	}
	attemptID := strings.TrimSpace(run.ActiveAttemptID)
	sessionRef := strings.TrimSpace(run.SessionRef)
	count := 0
	for _, decision := range decisions {
		if decision.Kind != string(SupervisorDecisionContinueThread) && decision.Kind != string(SupervisorDecisionContinueAttempt) {
			continue
		}
		if attemptID != "" && (decision.AttemptID == attemptID || decision.TargetAttemptID == attemptID || decision.ParentAttemptID == attemptID) {
			count++
			continue
		}
		if sessionRef != "" && (decision.SessionRef == sessionRef || decision.TargetSessionRef == sessionRef || decision.ParentSessionRef == sessionRef) {
			count++
		}
	}
	if attemptDerived := run.AttemptCount - 1; attemptDerived > count {
		count = attemptDerived
	}
	return count
}

func parkNoProgressRun(run RunStatus, reason string) RunStatus {
	now := time.Now().UTC()
	run.LeaseState = string(LeaseStateParkedNoProgress)
	run.AttemptOutcome = string(AttemptOutcomeBlocked)
	run.LastError = reason
	run.NextRetryAt = ""
	run.UpdatedAt = now.Format(time.RFC3339)
	run.Terminal = true
	clearActiveExecution(&run)
	return run
}

func shouldDaemonPromoteCleanExitToReview(noteStatus string) bool {
	return strings.TrimSpace(noteStatus) == ""
}

func resolveRunnerForNote(note Note, wf Workflow) string {
	nextOwner := strings.TrimSpace(stringField(note.Data, "next_owner"))
	if strings.HasPrefix(nextOwner, "agent:") {
		candidate := strings.TrimSpace(strings.TrimPrefix(nextOwner, "agent:"))
		if containsString(wf.Agents.Enabled, candidate) {
			return candidate
		}
	}
	assignee := strings.TrimSpace(stringField(note.Data, "assignee"))
	for _, candidate := range wf.Agents.Enabled {
		if assignee == candidate {
			return candidate
		}
	}
	return wf.Agents.Default
}

func (d *Daemon) dispatchRun(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, note Note, run RunStatus, lane string) (RunStatus, bool, error) {
	lane = firstNonEmpty(strings.TrimSpace(lane), runLaneExecute)
	if reason := strings.TrimSpace(d.dispatchRefusalReason); reason != "" {
		return run, false, tuskerError(errorInvalidTransition, reason, withContext(map[string]any{"task": run.RecordID, "lane": lane}))
	}
	if capped, capReached := d.enforceAttemptCreationCap(wfFile.Data, run, attemptCreationKindForDispatch(run), "dispatch would create another attempt"); capReached {
		return capped, false, nil
	}
	previousRun := run
	runner, command, err := runnerForName(run.Runner, wfFile.Data)
	if err != nil {
		return run, false, err
	}
	workspaceManager := NewWorkspaceManager()
	workspaceStrategy := d.workspaceStrategyForDispatch(project, wfFile.Data, run)
	branchName, branchBase, err := v7WorkspaceBranchForTask(project.VaultRoot, note)
	if err != nil {
		return run, false, err
	}

	ordinal := run.AttemptCount + 1
	attemptID := newRecordID()
	started := time.Now().UTC()
	startedAt := started.Format(time.RFC3339)
	leaseGeneration := run.LeaseGeneration + 1
	claimed, err := d.store.ClaimRunLease(project.ProjectID, run.RecordID, attemptID, leaseGeneration, defaultRunLeaseTTL, started, true, RuntimeLeaseClaimPrecondition{
		ExpectedOwner:           run.LeaseOwner,
		ExpectedLeaseGeneration: run.LeaseGeneration,
		ExpectedWorkRevision:    run.WorkRevision,
	})
	if err != nil {
		return run, false, err
	}
	if !claimed {
		latest, err := d.latestDispatchRun(run)
		return latest, true, err
	}

	run.LeaseState = string(LeaseStateClaimed)
	run.LeaseOwner = attemptID
	run.LeaseGeneration = leaseGeneration
	run.LeaseExpiresAt = started.Add(defaultRunLeaseTTL).Format(time.RFC3339)
	run.LeaseHost = runtimeLeaseHost()
	run.Lane = lane
	run.AttemptCount = ordinal
	run.ActiveAttemptID = attemptID
	run.SessionRef = ""
	run.ProcessPID = 0
	run.ProcessPGID = 0
	run.ProcessStartedAt = ""
	run.PromptPath = ""
	run.EventSinkPath = ""
	run.RawLogPath = ""
	run.StatusPath = ""
	run.FirstEventAt = ""
	run.LastHeartbeatAt = ""
	run.Terminal = false
	clearRunCloudRefs(&run)
	run.UpdatedAt = startedAt
	run.LastEventAt = startedAt

	workspace, err := workspaceManager.Prepare(WorkspacePrepareRequest{
		ProjectID:     project.ProjectID,
		ProjectKey:    project.ProjectKey,
		RecordID:      run.RecordID,
		ItemID:        run.ItemID,
		BranchName:    branchName,
		BranchBase:    branchBase,
		RepoRoot:      project.RepoRoot,
		StateRoot:     d.stateRoot,
		WorkspaceRoot: wfFile.Data.Workspace.Root,
		Strategy:      workspaceStrategy,
		WorkRevision:  run.WorkRevision,
	})
	if err != nil {
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}
	if err := mirrorApplyInputsIntoWorkspace(d.store, project, run, workspace.Path); err != nil {
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}

	runDir := filepath.Join(d.stateRoot, "runs", project.ProjectKey, run.RecordID)
	if err := ensureDir(runDir); err != nil {
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}
	attemptStem := fmt.Sprintf("rev-%02d-%s-attempt-%04d-%s", run.WorkRevision, lane, ordinal, strings.ToLower(attemptID))
	promptPath := filepath.Join(runDir, attemptStem+".prompt.md")
	eventSinkPath := filepath.Join(runDir, attemptStem+".events.jsonl")
	rawLogPath := filepath.Join(runDir, attemptStem+".raw.log")
	statusPath := filepath.Join(runDir, attemptStem+".status.json")
	attempt := RunAttempt{
		AttemptID: attemptID, ProjectID: project.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
		Runner: run.Runner, Lane: lane, WorkRevision: run.WorkRevision, WorkspacePath: workspace.Path,
		BranchName: branchName,
		PromptPath: promptPath, EventSinkPath: eventSinkPath, RawLogPath: rawLogPath, StatusPath: statusPath,
		ParentAttemptID: previousRun.ActiveAttemptID,
		StartedAt:       startedAt,
	}
	run.WorkspacePath = workspace.Path
	run.PromptPath = promptPath
	run.EventSinkPath = eventSinkPath
	run.RawLogPath = rawLogPath
	run.StatusPath = statusPath
	updated, persisted, err := d.updateDispatchRunIfLease(run, attemptID, leaseGeneration)
	if err != nil || !persisted {
		return updated, true, err
	}
	run = updated

	prompt, err := renderAttemptPrompt(project, wfFile, note, workspace.Path, ordinal, attemptID, lane, run, previousRun, d.store)
	if err != nil {
		attempt.Outcome = string(AttemptOutcomeFailed)
		attempt.LastError = err.Error()
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_, _ = d.store.SaveAttemptIfRunLease(attempt, attemptID, leaseGeneration)
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}
	if err := writeText(promptPath, prompt); err != nil {
		attempt.Outcome = string(AttemptOutcomeFailed)
		attempt.LastError = err.Error()
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_, _ = d.store.SaveAttemptIfRunLease(attempt, attemptID, leaseGeneration)
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}
	if ok, err := d.store.SaveAttemptIfRunLease(attempt, attemptID, leaseGeneration); err != nil {
		return run, true, err
	} else if !ok {
		latest, err := d.latestDispatchRun(run)
		return latest, true, err
	}

	var start *StartResult
	codexPolicy := codexPolicyForLane(codexPolicyFromWorkflow(wfFile.Data), lane)
	externalLaunch := ExternalLoopLaunchContext{}
	if externalLoopRunnerRequiresCollect(wfFile.Data, run.Runner) {
		externalLaunch = d.externalLoopLaunchContext(project.ProjectID, run.RecordID)
	}
	if previousRunHasStructuredOutcome(previousRun) {
		if ok, err := d.store.RunLeaseMatches(project.ProjectID, run.RecordID, attemptID, leaseGeneration); err != nil {
			return run, true, err
		} else if !ok {
			latest, err := d.latestDispatchRun(run)
			return latest, true, err
		}
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        project.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        attemptID,
			Kind:             string(SupervisorDecisionContinueAttempt),
			Reason:           previousStructuredOutcomeReason(previousRun),
			ParentAttemptID:  previousRun.ActiveAttemptID,
			ParentSessionRef: previousRun.SessionRef,
			TargetAttemptID:  attemptID,
			WorkspacePath:    workspace.Path,
		})
	}
	startReq := StartRequest{
		ProjectID:       project.ProjectID,
		RecordID:        run.RecordID,
		ItemID:          run.ItemID,
		AttemptID:       attemptID,
		Lane:            lane,
		WorkRevision:    run.WorkRevision,
		LeaseGeneration: run.LeaseGeneration,
		ActiveStates:    wfFile.Data.Tracker.ActiveStates,
		WorkingDir:      workspace.Path,
		WorkspacePath:   workspace.Path,
		RepoRoot:        project.RepoRoot,
		PromptPath:      promptPath,
		EventSinkPath:   eventSinkPath,
		RawLogPath:      rawLogPath,
		StatusPath:      statusPath,
		Command:         command,
		NotePath:        note.AbsolutePath,
		VaultPath:       project.VaultRoot,
		CodexPolicy:     codexPolicy,
		ExternalLoop:    externalLaunch,
	}
	resumeSession := resolvedResumeSession{}
	if runner.Capabilities().ResumeSession {
		resumeRun := run
		resumeRun.SessionRef = previousRun.SessionRef
		resumeRun.AttemptOutcome = previousRun.AttemptOutcome
		resumeRun.LastError = previousRun.LastError
		resumeRun.LeaseState = previousRun.LeaseState
		resumeSession, err = d.resolveResumeSession(project, note, resumeRun)
		if err != nil {
			return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
		}
	}
	if ok, err := d.store.RunLeaseMatches(project.ProjectID, run.RecordID, attemptID, leaseGeneration); err != nil {
		return run, true, err
	} else if !ok {
		latest, err := d.latestDispatchRun(run)
		return latest, true, err
	}
	if strings.TrimSpace(resumeSession.SessionRef) != "" {
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        project.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        attemptID,
			Kind:             resumeSession.DecisionKind,
			Reason:           resumeSession.Reason,
			ParentAttemptID:  resumeSession.ParentAttemptID,
			ParentSessionRef: resumeSession.SessionRef,
			TargetAttemptID:  attemptID,
			TargetSessionRef: resumeSession.SessionRef,
			WorkspacePath:    workspace.Path,
		})
		start, err = runner.Resume(ctx, ResumeRequest{
			ProjectID:       startReq.ProjectID,
			RecordID:        startReq.RecordID,
			ItemID:          startReq.ItemID,
			AttemptID:       startReq.AttemptID,
			Lane:            startReq.Lane,
			WorkRevision:    startReq.WorkRevision,
			LeaseGeneration: startReq.LeaseGeneration,
			ActiveStates:    startReq.ActiveStates,
			SessionRef:      resumeSession.SessionRef,
			MessageRef:      resumeSession.MessageRef,
			WorkingDir:      startReq.WorkingDir,
			WorkspacePath:   startReq.WorkspacePath,
			RepoRoot:        startReq.RepoRoot,
			PromptPath:      startReq.PromptPath,
			EventSinkPath:   startReq.EventSinkPath,
			RawLogPath:      startReq.RawLogPath,
			StatusPath:      startReq.StatusPath,
			Command:         startReq.Command,
			NotePath:        startReq.NotePath,
			VaultPath:       startReq.VaultPath,
			CodexPolicy:     startReq.CodexPolicy,
			ExternalLoop:    startReq.ExternalLoop,
		})
	} else {
		start, err = runner.Start(ctx, startReq)
	}
	if err != nil {
		attempt.Outcome = string(AttemptOutcomeFailed)
		attempt.LastError = err.Error()
		attempt.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_, _ = d.store.SaveAttemptIfRunLease(attempt, attemptID, leaseGeneration)
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}
	if strings.TrimSpace(start.SessionRef) == "" && strings.TrimSpace(resumeSession.SessionRef) != "" {
		start.SessionRef = resumeSession.SessionRef
		start.MessageRef = resumeSession.MessageRef
	}

	run.SessionRef = start.SessionRef
	applyStartResultCloud(&run, start)
	run.StartedAt = start.StartedAt
	run.ProcessPID = start.PID
	run.ProcessPGID = start.PGID
	run.ProcessStartedAt = firstNonEmpty(start.ProcessStart, start.StartedAt)
	run.StatusPath = firstNonEmpty(start.StatusPath, statusPath)
	run.UpdatedAt = firstNonEmpty(start.StartedAt, time.Now().UTC().Format(time.RFC3339))
	run.LastEventAt = firstNonEmpty(run.LastEventAt, run.UpdatedAt)

	attempt.SessionRef = start.SessionRef
	applyStartResultCloudToAttempt(&attempt, start)
	attempt.ProcessPID = start.PID
	attempt.Outcome = string(start.Outcome)
	attempt.ExitCode = start.ExitCode
	attempt.LastError = start.Reason
	attempt.StartedAt = start.StartedAt
	attempt.FinishedAt = start.FinishedAt
	_, _ = d.store.SaveAttemptIfRunLease(attempt, attemptID, leaseGeneration)
	if strings.TrimSpace(start.SessionRef) != "" {
		if ok, err := d.store.RunLeaseMatches(project.ProjectID, run.RecordID, attemptID, leaseGeneration); err == nil && ok {
			_ = d.store.SaveSession(RunnerSession{
				ProjectID: project.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: start.SessionRef,
				LastMessageRef: start.MessageRef,
				WorkspacePath:  workspace.Path, CurrentItemID: run.ItemID, WorkRevision: run.WorkRevision, LastAttemptID: attemptID,
				State: sessionStateForOutcome(start.Outcome), Resumable: runner.Capabilities().ResumeSession && lane != runLaneReview,
				StartedAt: firstNonEmpty(run.StartedAt, start.StartedAt), LastSeenAt: time.Now().UTC().Format(time.RFC3339),
				EndedAt: firstNonEmpty(start.FinishedAt, ""), LastError: start.Reason,
			})
		}
	}

	run.LeaseState = string(LeaseStateRunning)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.NextRetryAt = ""
	run.LastError = ""
	updated, persisted, err = d.updateDispatchRunIfLease(run, attemptID, leaseGeneration)
	if err != nil || !persisted {
		if err == nil && !persisted {
			// The lease was revoked by a concurrent operator stop/interrupt
			// between claim and this write. That interrupt ran while the stored
			// row still had ProcessPID=0, so it killed nothing; reap the child we
			// just spawned (whose live PGID lives on the local run, not on the
			// store-fetched `updated`) so it is not orphaned.
			killSpawnedRunProcess(run)
		}
		return updated, true, err
	}
	run = updated
	reconciled, changed, err := d.reconcileRun(ctx, project, wfFile, run)
	if err != nil {
		return d.persistClaimedDispatchFailure(project, wfFile.Data, run, attemptID, leaseGeneration, err)
	}
	if changed {
		reconciledRun, reconciledPersisted, reconciledErr := d.updateDispatchRunIfLease(reconciled, attemptID, leaseGeneration)
		if reconciledErr == nil && !reconciledPersisted {
			// Same lease-lost fence after reconcile: kill from the local
			// `reconciled` run that carries the live PGID (the returned row has
			// ProcessPID=0). Killing an already-exited group is harmless (ESRCH).
			killSpawnedRunProcess(reconciled)
		}
		return reconciledRun, reconciledPersisted, reconciledErr
	}
	return run, true, nil
}

func (d *Daemon) latestDispatchRun(fallback RunStatus) (RunStatus, error) {
	if d == nil || d.store == nil || strings.TrimSpace(fallback.RecordID) == "" {
		return fallback, nil
	}
	latest, err := d.store.FindRun(fallback.RecordID)
	if err != nil || latest == nil {
		return fallback, err
	}
	return *latest, nil
}

func (d *Daemon) updateDispatchRunIfLease(run RunStatus, owner string, generation int) (RunStatus, bool, error) {
	ok, err := d.store.UpdateRunIfLease(run, owner, generation)
	if err != nil {
		return run, true, err
	}
	if !ok {
		latest, err := d.latestDispatchRun(run)
		return latest, false, err
	}
	return run, true, nil
}

func (d *Daemon) persistClaimedDispatchFailure(project RegisteredProject, wf Workflow, run RunStatus, owner string, generation int, cause error) (RunStatus, bool, error) {
	if cause == nil {
		return run, true, nil
	}
	if ok, err := d.store.RunLeaseMatches(run.ProjectID, run.RecordID, owner, generation); err != nil {
		return run, true, err
	} else if !ok {
		latest, err := d.latestDispatchRun(run)
		return latest, true, err
	}
	run = d.scheduleRetry(run, wf, cause.Error())
	updated, persisted, err := d.updateDispatchRunIfLease(run, owner, generation)
	if err != nil || !persisted {
		return updated, true, err
	}
	return updated, true, nil
}

func (d *Daemon) workspaceStrategyForDispatch(project RegisteredProject, wf Workflow, run RunStatus) WorkspaceStrategy {
	configured := workspaceStrategyFromWorkflow(wf.Workspace.Strategy)
	if configured != WorkspaceStrategyInPlace || d == nil || d.store == nil {
		return configured
	}
	runs, err := d.store.ListRuns()
	if err != nil {
		return configured
	}
	return workspaceStrategyForRun(wf, project, run, runs)
}

type resolvedResumeSession struct {
	SessionRef      string
	MessageRef      string
	DecisionKind    string
	Reason          string
	ParentAttemptID string
}

func (d *Daemon) resolveResumeSession(project RegisteredProject, note Note, run RunStatus) (resolvedResumeSession, error) {
	if run.Lane == runLaneReview {
		return resolvedResumeSession{}, nil
	}
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
	if AttemptOutcome(strings.TrimSpace(run.AttemptOutcome)) == AttemptOutcomeBudgetExceeded || strings.Contains(strings.ToLower(run.LastError), "budget") {
		return "prior attempt was budget-killed"
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
	var session RunnerSession
	var resumable int
	err := s.queryRowScan(`SELECT project_id, record_id, runner, session_ref, last_message_ref, workspace_path, current_item_id, work_revision, last_attempt_id, state, resumable, started_at, last_seen_at, ended_at, last_error
		FROM sessions
		WHERE project_id = ? AND session_ref = ?
		LIMIT 1`, []any{projectID, sessionRef}, &session.ProjectID, &session.RecordID, &session.Runner, &session.SessionRef, &session.LastMessageRef, &session.WorkspacePath, &session.CurrentItemID, &session.WorkRevision, &session.LastAttemptID, &session.State, &resumable, &session.StartedAt, &session.LastSeenAt, &session.EndedAt, &session.LastError)
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
	if !classification.retryable {
		run.LeaseState = string(LeaseStateReleased)
		run.NextRetryAt = ""
		run.Terminal = true
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
		return run
	}
	if capped, capReached := d.enforceAttemptCreationCap(wf, run, attemptCreationRetry, reason); capReached {
		d.emitSupervisorDecision(SupervisorDecision{
			ProjectID:        run.ProjectID,
			RecordID:         run.RecordID,
			Kind:             string(SupervisorDecisionStopForAudit),
			Reason:           capped.LastError,
			ParentAttemptID:  parentAttemptID,
			ParentSessionRef: parentSessionRef,
			WorkspacePath:    run.WorkspacePath,
			LeaseState:       capped.LeaseState,
		})
		return capped
	}
	if len(wf.Retry.BackoffMS) == 0 {
		run.LeaseState = string(LeaseStateRetryQueued)
		run.NextRetryAt = time.Now().UTC().Format(time.RFC3339)
		run.Terminal = false
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
	run.Terminal = false
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
	if strings.Contains(text, "turn cap exhausted") {
		return retryFailureClassification{retryable: true, outcome: AttemptOutcomeTurnCapExhausted}
	}
	if strings.Contains(text, "runner process no longer matches recorded identity") {
		return retryFailureClassification{retryable: true, outcome: AttemptOutcomeCancelled}
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

func reviewerWorkspaceDirtyReason(workspacePath string) string {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return "reviewer workspace cleanliness could not be verified: missing workspace path"
	}
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "-C", workspacePath, "status", "--porcelain=v1", "--untracked-files=all")
		output, err := cmd.Output()
		if err == nil {
			if dirty := reviewerDirtyStatusLines(string(output)); len(dirty) > 0 {
				return "reviewer workspace dirty after review run: " + strings.Join(limitStrings(dirty, 3), "; ")
			}
			return ""
		}
	}
	entries, err := os.ReadDir(workspacePath)
	if err != nil {
		return "reviewer workspace cleanliness could not be verified: " + err.Error()
	}
	var dirty []string
	for _, entry := range entries {
		name := entry.Name()
		if name == ".tusker" {
			continue
		}
		dirty = append(dirty, name)
	}
	if len(dirty) > 0 {
		sort.Strings(dirty)
		return "reviewer workspace dirty after review run: " + strings.Join(limitStrings(dirty, 3), "; ")
	}
	return ""
}

func reviewerDirtyStatusLines(output string) []string {
	var dirty []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		path := reviewerStatusPath(line)
		if path == ".tusker" || strings.HasPrefix(path, ".tusker/") {
			continue
		}
		dirty = append(dirty, line)
	}
	return dirty
}

func reviewerStatusPath(line string) string {
	if len(line) > 3 {
		line = line[3:]
	}
	line = strings.TrimSpace(line)
	if idx := strings.LastIndex(line, " -> "); idx >= 0 {
		line = line[idx+4:]
	}
	return strings.Trim(line, `"`)
}

func limitStrings(values []string, max int) []string {
	if max <= 0 || len(values) <= max {
		return values
	}
	return values[:max]
}

func clearActiveExecution(run *RunStatus) {
	run.ActiveAttemptID = ""
	run.LeaseOwner = ""
	run.LeaseExpiresAt = ""
	run.ProcessPID = 0
	run.ProcessPGID = 0
	run.ProcessStartedAt = ""
	run.PromptPath = ""
	run.EventSinkPath = ""
	run.RawLogPath = ""
	run.StatusPath = ""
}

func clearRunCloudRefs(run *RunStatus) {
	run.CloudTaskID = ""
	run.CloudStatus = ""
	run.CloudEnvironmentID = ""
	run.CloudAttemptNumber = 0
	run.PullRequestURL = ""
	run.ApplyRef = ""
	run.LogsSummary = ""
	run.FinalSummary = ""
}

func applyStartResultCloud(run *RunStatus, start *StartResult) {
	if run == nil || start == nil {
		return
	}
	run.CloudTaskID = start.CloudTaskID
	run.CloudStatus = start.CloudStatus
	run.CloudEnvironmentID = start.CloudEnvironmentID
	run.CloudAttemptNumber = start.CloudAttemptNumber
	run.PullRequestURL = start.PullRequestURL
	run.ApplyRef = start.ApplyRef
	run.LogsSummary = start.LogsSummary
	run.FinalSummary = start.FinalSummary
}

func applyStartResultCloudToAttempt(attempt *RunAttempt, start *StartResult) {
	if attempt == nil || start == nil {
		return
	}
	attempt.CloudTaskID = start.CloudTaskID
	attempt.CloudStatus = start.CloudStatus
	attempt.CloudEnvironmentID = start.CloudEnvironmentID
	attempt.CloudAttemptNumber = start.CloudAttemptNumber
	attempt.PullRequestURL = start.PullRequestURL
	attempt.ApplyRef = start.ApplyRef
	attempt.LogsSummary = start.LogsSummary
	attempt.FinalSummary = start.FinalSummary
}

func applyReconcileResultCloud(run *RunStatus, result *ReconcileResult) {
	if run == nil || result == nil {
		return
	}
	if strings.TrimSpace(result.CloudTaskID) != "" {
		run.CloudTaskID = result.CloudTaskID
	}
	if strings.TrimSpace(result.CloudStatus) != "" {
		run.CloudStatus = result.CloudStatus
	}
	if strings.TrimSpace(result.CloudEnvironmentID) != "" {
		run.CloudEnvironmentID = result.CloudEnvironmentID
	}
	if result.CloudAttemptNumber > 0 {
		run.CloudAttemptNumber = result.CloudAttemptNumber
	}
	if strings.TrimSpace(result.PullRequestURL) != "" {
		run.PullRequestURL = result.PullRequestURL
	}
	if strings.TrimSpace(result.ApplyRef) != "" {
		run.ApplyRef = result.ApplyRef
	}
	if strings.TrimSpace(result.LogsSummary) != "" {
		run.LogsSummary = result.LogsSummary
	}
	if strings.TrimSpace(result.FinalSummary) != "" {
		run.FinalSummary = result.FinalSummary
	}
}

func exitCodeForOutcome(outcome AttemptOutcome) int {
	switch outcome {
	case AttemptOutcomeSucceeded, AttemptOutcomeNone, AttemptOutcomeEarlyExit, AttemptOutcomeDispatchDeclined:
		return 0
	case AttemptOutcomeCancelled:
		return 130
	case AttemptOutcomeTurnCapExhausted:
		return 124
	case AttemptOutcomeBudgetExceeded:
		return 75
	default:
		return 1
	}
}

func updateRunAttemptFromRun(store *RuntimeStore, run RunStatus, outcome AttemptOutcome, exitCode int, lastError, finishedAt string) {
	if store == nil || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return
	}
	_ = store.SaveAttempt(RunAttempt{
		AttemptID:          run.ActiveAttemptID,
		ProjectID:          run.ProjectID,
		RecordID:           run.RecordID,
		ItemID:             run.ItemID,
		Runner:             run.Runner,
		Lane:               run.Lane,
		WorkRevision:       run.WorkRevision,
		WorkspacePath:      run.WorkspacePath,
		SessionRef:         run.SessionRef,
		CloudTaskID:        run.CloudTaskID,
		CloudStatus:        run.CloudStatus,
		CloudEnvironmentID: run.CloudEnvironmentID,
		CloudAttemptNumber: run.CloudAttemptNumber,
		PullRequestURL:     run.PullRequestURL,
		ApplyRef:           run.ApplyRef,
		LogsSummary:        run.LogsSummary,
		FinalSummary:       run.FinalSummary,
		ProcessPID:         run.ProcessPID,
		Outcome:            string(outcome),
		ExitCode:           exitCode,
		TurnsUsed:          turnsUsedForAttempt(store, run.ActiveAttemptID),
		PromptPath:         run.PromptPath,
		EventSinkPath:      run.EventSinkPath,
		RawLogPath:         run.RawLogPath,
		StatusPath:         run.StatusPath,
		LastError:          lastError,
		StartedAt:          run.StartedAt,
		FinishedAt:         finishedAt,
	})
}

func turnsUsedForAttempt(store *RuntimeStore, attemptID string) int {
	if store == nil || strings.TrimSpace(attemptID) == "" {
		return 0
	}
	turns, err := store.ListTurnsForAttempt(attemptID)
	if err != nil {
		return 0
	}
	return len(turns)
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
	facts := collectReviewPacketFacts(run)
	if idx, err := loadV7Index(vaultPath); err == nil {
		facts.SoftDependencyDependents = v7SoftDependencyDependentLines(idx, itemID)
	}
	if err := writeText(packetPath, renderReviewPacket(note, run, turns, supervisorDecisions, facts)); err != nil {
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

type reviewPacketFacts struct {
	ChangedFiles                  []string
	ChangedFilesStatement         string
	DiffSummary                   []string
	DiffSummaryStatement          string
	CommandSummaries              []string
	CommandSummariesStatement     string
	VerificationCommands          []string
	VerificationCommandsStatement string
	ValidationSummaries           []string
	ValidationSummariesStatement  string
	SessionRefs                   []string
	TurnIDs                       []string
	EventTokenTotals              runtimeTokenTotals
	RuntimeSummaries              []string
	OpenRisks                     []string
	SoftDependencyDependents      []string
}

func renderReviewPacket(note Note, run RunStatus, turns []RunTurn, supervisorDecisions []RuntimeSupervisorDecision, facts reviewPacketFacts) string {
	var out []string
	tokenTotals := tokenTotalsForTurns(turns)
	if tokenTotals.TotalTokens == 0 && facts.EventTokenTotals.TotalTokens > 0 {
		tokenTotals = facts.EventTokenTotals
	}
	out = append(out, "# Review packet")
	out = append(out, "")
	out = append(out, fmt.Sprintf("- Item: %s - %s", stringField(note.Data, "id"), stringField(note.Data, "title")))
	out = append(out, fmt.Sprintf("- Record: %s", run.RecordID))
	out = append(out, fmt.Sprintf("- Attempt: %s", run.ActiveAttemptID))
	out = append(out, fmt.Sprintf("- Runner: %s", run.Runner))
	out = append(out, fmt.Sprintf("- Lane: %s", firstNonEmpty(run.Lane, runLaneExecute)))
	out = append(out, fmt.Sprintf("- Work revision: %d", run.WorkRevision))
	out = append(out, fmt.Sprintf("- Turns: %d", len(turns)))
	out = append(out, fmt.Sprintf("- Token totals: total=%d input=%d output=%d", tokenTotals.TotalTokens, tokenTotals.InputTokens, tokenTotals.OutputTokens))
	out = append(out, fmt.Sprintf("- Workspace: %s", run.WorkspacePath))
	out = append(out, fmt.Sprintf("- Session: %s", run.SessionRef))
	out = append(out, fmt.Sprintf("- Started: %s", run.StartedAt))
	out = append(out, fmt.Sprintf("- Last event: %s", run.LastEventAt))
	out = append(out, "")
	out = append(out, "## Runtime summary", "")
	if len(facts.RuntimeSummaries) == 0 {
		out = append(out, "- No normalized runtime summary was recorded for this attempt.")
	} else {
		for _, summary := range facts.RuntimeSummaries {
			out = append(out, "- "+summary)
		}
	}
	out = append(out, "")
	out = append(out, "## Soft dependency blast radius", "")
	if len(facts.SoftDependencyDependents) == 0 {
		out = append(out, "- No soft-edge dependents were found for this task.")
	} else {
		out = append(out, facts.SoftDependencyDependents...)
	}
	out = append(out, "")
	out = append(out, "## Runtime artifacts", "")
	for _, artifact := range []struct {
		label string
		path  string
	}{
		{"prompt", run.PromptPath},
		{"events", run.EventSinkPath},
		{"raw log pointer", run.RawLogPath},
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
			out = append(out, fmt.Sprintf("- #%d `%s` session=%s status=%s tokens=%d input=%d output=%d last_event=%s error=%s",
				turn.TurnIndex, turn.TurnID, firstNonEmpty(turn.SessionRef, "none"), turn.Status, turn.TotalTokens, turn.InputTokens, turn.OutputTokens, turn.LastEventAt, firstNonEmpty(turn.LastError, "none")))
		}
	}
	out = append(out, "", "## Sessions and turns", "")
	sessionRefs := sessionRefsForPacket(run, turns, facts)
	turnIDs := turnIDsForPacket(turns, facts)
	if len(sessionRefs) == 0 {
		out = append(out, "- Session refs: none observed.")
	} else {
		out = append(out, "- Session refs: "+backtickList(sessionRefs))
	}
	if len(turnIDs) == 0 {
		out = append(out, "- Turn ids: none observed.")
	} else {
		out = append(out, "- Turn ids: "+backtickList(turnIDs))
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
	out = append(out, "", "## Changed files", "")
	if len(facts.ChangedFiles) == 0 {
		out = append(out, "- "+firstNonEmpty(facts.ChangedFilesStatement, "No changed files were observed in normalized events or workspace status."))
	} else {
		for _, file := range facts.ChangedFiles {
			out = append(out, "- "+file)
		}
	}
	out = append(out, "", "### Diff summary", "")
	if len(facts.DiffSummary) == 0 {
		out = append(out, "- "+firstNonEmpty(facts.DiffSummaryStatement, "No diff summary was observed in normalized events or workspace status."))
	} else {
		for _, summary := range facts.DiffSummary {
			out = append(out, "- "+summary)
		}
	}
	out = append(out, "", "## Commands and tests", "")
	if len(facts.CommandSummaries) == 0 {
		out = append(out, "- "+firstNonEmpty(facts.CommandSummariesStatement, "No command or test summaries were observed in normalized events."))
	} else {
		for _, command := range facts.CommandSummaries {
			out = append(out, "- "+command)
		}
	}
	out = append(out, "", "## Verification", "")
	if len(facts.VerificationCommands) == 0 {
		out = append(out, "- "+firstNonEmpty(facts.VerificationCommandsStatement, "No verification commands were observed in normalized events."))
	} else {
		for _, command := range facts.VerificationCommands {
			out = append(out, "- "+command)
		}
	}
	out = append(out, "", "## Validation", "")
	if len(facts.ValidationSummaries) == 0 {
		out = append(out, "- "+firstNonEmpty(facts.ValidationSummariesStatement, "No validation results were observed in normalized events."))
	} else {
		for _, validation := range facts.ValidationSummaries {
			out = append(out, "- "+validation)
		}
	}
	out = append(out, "", "## Open risks", "")
	risks := openRisksForPacket(run, turns, supervisorDecisions, facts)
	if len(risks) == 0 {
		out = append(out, "- No open risks were observed in normalized events or runtime status.")
	} else {
		for _, risk := range risks {
			out = append(out, "- "+risk)
		}
	}
	out = append(out, "- Reviewer must still check claims against the current tree before approval.")
	out = append(out, "- This packet summarizes daemon-observed runtime facts. It does not embed raw logs or full transcripts.")
	return strings.Join(out, "\n") + "\n"
}

func collectReviewPacketFacts(run RunStatus) reviewPacketFacts {
	events := readReviewPacketEvents(run.EventSinkPath)
	changed := append([]string{}, changedFilesFromEvents(events)...)
	diffSummary := append([]string{}, diffSummariesFromEvents(events)...)
	verification := verificationCommandsFromEvents(events)
	commandSummaries := commandSummariesFromEvents(events)
	validationSummaries := validationSummariesFromEvents(events)
	openRisks := openRisksFromEvents(events)
	openRisks = append(openRisks, openRisksFromRun(run)...)
	gitChanged, gitStatement := changedFilesFromWorkspace(run.WorkspacePath)
	gitDiffSummary, gitDiffStatement := diffSummaryFromWorkspace(run.WorkspacePath)
	changed = append(changed, gitChanged...)
	diffSummary = append(diffSummary, gitDiffSummary...)
	changed = dedupeSortedStrings(changed)
	diffSummary = dedupeSortedStrings(diffSummary)
	statement := "No changed files were observed in normalized events or workspace status."
	if gitStatement != "" {
		statement = gitStatement
	}
	diffStatement := "No diff summary was observed in normalized events or workspace status."
	if gitDiffStatement != "" {
		diffStatement = gitDiffStatement
	}
	return reviewPacketFacts{
		ChangedFiles:                  changed,
		ChangedFilesStatement:         statement,
		DiffSummary:                   diffSummary,
		DiffSummaryStatement:          diffStatement,
		CommandSummaries:              dedupeSortedStrings(commandSummaries),
		CommandSummariesStatement:     "No command or test summaries were observed in normalized events.",
		VerificationCommands:          dedupeSortedStrings(verification),
		VerificationCommandsStatement: "No verification commands were observed in normalized events.",
		ValidationSummaries:           dedupeSortedStrings(validationSummaries),
		ValidationSummariesStatement:  "No validation results were observed in normalized events.",
		SessionRefs:                   sessionRefsFromEvents(events),
		TurnIDs:                       turnIDsFromEvents(events),
		EventTokenTotals:              tokenTotalsFromEvents(events),
		RuntimeSummaries:              runtimeSummariesFromRun(run),
		OpenRisks:                     dedupeSortedStrings(openRisks),
	}
}

func readReviewPacketEvents(path string) []map[string]any {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	content, err := readText(path)
	if err != nil {
		return nil
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if strings.HasPrefix(content, "[") {
		var events []map[string]any
		if json.Unmarshal([]byte(content), &events) == nil {
			return events
		}
	}
	if strings.HasPrefix(content, "{") {
		var event map[string]any
		if json.Unmarshal([]byte(content), &event) == nil {
			return []map[string]any{event}
		}
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

func changedFilesFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		if payload == nil {
			continue
		}
		if !strings.Contains(kind, "file") && !strings.Contains(kind, "change") && payload["changed_files"] == nil && payload["files"] == nil {
			continue
		}
		for _, path := range payloadPathValues(payload) {
			out = append(out, fmt.Sprintf("`%s` (event:%s)", path, firstNonEmpty(kind, "file_change")))
		}
	}
	return out
}

func diffSummariesFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		if payload == nil {
			continue
		}
		for _, key := range []string{"diff_summary", "diff_stat", "diffstat", "stat"} {
			if summary := safePacketText(stringValue(payload[key]), 240); summary != "" {
				out = append(out, fmt.Sprintf("%s (event:%s)", summary, firstNonEmpty(kind, "diff_summary")))
			}
		}
		out = append(out, fileDiffSummaries(payload["changed_files"], kind)...)
		out = append(out, fileDiffSummaries(payload["files"], kind)...)
		path := firstNonEmpty(pathValuesJoined(payload["path"]), pathValuesJoined(payload["file"]))
		if path != "" && (payload["insertions"] != nil || payload["deletions"] != nil || payload["additions"] != nil) {
			insertions := reviewPacketInt(firstNonEmptyAny(payload["insertions"], payload["additions"], payload["added"]))
			deletions := reviewPacketInt(firstNonEmptyAny(payload["deletions"], payload["removals"], payload["removed"]))
			out = append(out, fmt.Sprintf("`%s` +%d -%d (event:%s)", path, insertions, deletions, firstNonEmpty(kind, "diff_summary")))
		}
	}
	return dedupeSortedStrings(out)
}

func payloadPathValues(payload map[string]any) []string {
	var out []string
	for _, key := range []string{"path", "file", "file_path", "filename", "relative_path", "target_path", "changed_files", "files", "paths"} {
		out = append(out, pathValues(payload[key])...)
	}
	return dedupeSortedStrings(out)
}

func pathValues(value any) []string {
	switch typed := value.(type) {
	case string:
		path := safePacketText(typed, 240)
		if path == "" {
			return nil
		}
		return []string{path}
	case []string:
		var out []string
		for _, item := range typed {
			out = append(out, pathValues(item)...)
		}
		return out
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, pathValues(item)...)
		}
		return out
	case map[string]any:
		return payloadPathValues(typed)
	default:
		return nil
	}
}

func verificationCommandsFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		if payload == nil {
			continue
		}
		command := firstNonEmpty(
			stringValue(payload["check"]),
			stringValue(payload["command"]),
			stringValue(payload["cmd"]),
			stringValue(payload["argv"]),
		)
		if command == "" || (!strings.Contains(kind, "verification") && !strings.Contains(kind, "command") && !strings.Contains(kind, "test")) {
			continue
		}
		command = safePacketText(command, 260)
		result := safePacketText(firstNonEmpty(stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]), "observed"), 80)
		exitCode := firstNonEmpty(stringValue(payload["exit_code"]), stringValue(payload["exitCode"]))
		at := firstNonEmpty(stringValue(event["at"]), stringValue(payload["at"]), stringValue(payload["normalized_at"]))
		turnID := firstNonEmpty(stringValue(payload["turn_id"]), stringValue(payload["turnId"]))
		detail := fmt.Sprintf("`%s` result=%s", command, result)
		if exitCode != "" {
			detail += " exit_code=" + exitCode
		}
		if turnID != "" {
			detail += " turn=" + turnID
		}
		if at != "" {
			detail += " at=" + at
		}
		out = append(out, detail)
	}
	return out
}

func commandSummariesFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		if payload == nil {
			continue
		}
		command := firstNonEmpty(
			stringValue(payload["check"]),
			stringValue(payload["command"]),
			stringValue(payload["cmd"]),
			stringValue(payload["argv"]),
		)
		if command == "" {
			continue
		}
		if !strings.Contains(kind, "attempt") && !strings.Contains(kind, "command") && !strings.Contains(kind, "test") && !strings.Contains(kind, "verification") && !strings.Contains(kind, "validation") {
			continue
		}
		command = safePacketText(command, 260)
		result := safePacketText(firstNonEmpty(stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]), "observed"), 80)
		if strings.Contains(kind, "attempt_started") {
			result = "started"
		}
		parts := []string{fmt.Sprintf("`%s` kind=%s result=%s", command, firstNonEmpty(kind, "command"), result)}
		if exitCode := firstNonEmpty(stringValue(payload["exit_code"]), stringValue(payload["exitCode"])); exitCode != "" {
			parts = append(parts, "exit_code="+exitCode)
		}
		if duration := commandDurationText(payload); duration != "" {
			parts = append(parts, "duration="+duration)
		}
		if turnID := firstNonEmpty(stringValue(payload["turn_id"]), stringValue(payload["turnId"])); turnID != "" {
			parts = append(parts, "turn="+safePacketText(turnID, 120))
		}
		if sessionRef := sessionRefFromEvent(event); sessionRef != "" {
			parts = append(parts, "session="+sessionRef)
		}
		if summary := safePacketText(firstNonEmpty(stringValue(payload["summary"]), stringValue(payload["note"])), 180); summary != "" {
			parts = append(parts, "summary="+summary)
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

func validationSummariesFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		if payload == nil {
			continue
		}
		command := firstNonEmpty(
			stringValue(payload["validation_command"]),
			stringValue(payload["check"]),
			stringValue(payload["command"]),
			stringValue(payload["cmd"]),
		)
		result := firstNonEmpty(stringValue(payload["validation_result"]), stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]))
		if !strings.Contains(kind, "validation") && !strings.Contains(strings.ToLower(command), "tusker validate") && payload["validation_result"] == nil {
			continue
		}
		command = safePacketText(command, 260)
		if command == "" {
			command = firstNonEmpty(kind, "validation")
		}
		parts := []string{fmt.Sprintf("`%s` result=%s", command, safePacketText(firstNonEmpty(result, "observed"), 80))}
		if exitCode := firstNonEmpty(stringValue(payload["exit_code"]), stringValue(payload["exitCode"])); exitCode != "" {
			parts = append(parts, "exit_code="+exitCode)
		}
		if summary := safePacketText(firstNonEmpty(stringValue(payload["summary"]), stringValue(payload["note"])), 180); summary != "" {
			parts = append(parts, "summary="+summary)
		}
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

func changedFilesFromWorkspace(workspacePath string) ([]string, string) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return nil, "No changed files were observed; workspace path was not recorded."
	}
	info, err := os.Stat(workspacePath)
	if err != nil || !info.IsDir() {
		return nil, "No changed files were observed; workspace path was unavailable."
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "status", "--short", "--untracked-files=all")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, "No changed files were observed; git status timed out."
	}
	if err != nil {
		return nil, "No changed files were observed; git status was unavailable."
	}
	changed := parseGitStatusShort(string(output))
	if len(changed) == 0 {
		return nil, "No changed files were observed by git status in the workspace."
	}
	return changed, ""
}

func diffSummaryFromWorkspace(workspacePath string) ([]string, string) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return nil, "No diff summary was observed; workspace path was not recorded."
	}
	info, err := os.Stat(workspacePath)
	if err != nil || !info.IsDir() {
		return nil, "No diff summary was observed; workspace path was unavailable."
	}
	var summaries []string
	for _, args := range [][]string{
		{"diff", "--stat", "--no-ext-diff"},
		{"diff", "--cached", "--stat", "--no-ext-diff"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspacePath}, args...)...)
		output, runErr := cmd.CombinedOutput()
		timedOut := ctx.Err() != nil
		cancel()
		if timedOut {
			return nil, "No diff summary was observed; git diff timed out."
		}
		if runErr != nil {
			return nil, "No diff summary was observed; git diff was unavailable."
		}
		summaries = append(summaries, parseGitDiffStat(string(output))...)
	}
	if len(summaries) == 0 {
		return nil, "No diff summary was observed by git diff in the workspace."
	}
	return dedupeSortedStrings(summaries), ""
}

func parseGitStatusShort(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		status := strings.TrimSpace(line)
		path := ""
		if len(line) >= 3 {
			status = strings.TrimSpace(line[:2])
			path = strings.TrimSpace(line[3:])
		}
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = strings.TrimSpace(parts[len(parts)-1])
		}
		if path == "" {
			path = strings.TrimSpace(line)
		}
		out = append(out, fmt.Sprintf("`%s` (%s)", path, firstNonEmpty(status, "changed")))
	}
	return dedupeSortedStrings(out)
}

func parseGitDiffStat(output string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, "`"+safePacketText(line, 240)+"`")
	}
	return dedupeSortedStrings(out)
}

func reviewPacketEventKind(event map[string]any) string {
	return strings.ToLower(strings.TrimSpace(firstNonEmpty(
		stringValue(event["kind"]),
		stringValue(event["event_kind"]),
		stringValue(event["action"]),
		stringValue(event["type"]),
	)))
}

func reviewPacketEventPayload(event map[string]any) map[string]any {
	if payload, ok := event["payload"].(map[string]any); ok {
		return payload
	}
	if data, ok := event["data"].(map[string]any); ok {
		return data
	}
	return event
}

func fileDiffSummaries(value any, kind string) []string {
	var out []string
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			out = append(out, fileDiffSummaries(item, kind)...)
		}
	case []map[string]any:
		for _, item := range typed {
			out = append(out, fileDiffSummaries(item, kind)...)
		}
	case map[string]any:
		path := firstNonEmpty(pathValuesJoined(typed["path"]), pathValuesJoined(typed["file"]), pathValuesJoined(typed["filename"]))
		if path == "" {
			return nil
		}
		insertions := reviewPacketInt(firstNonEmptyAny(typed["insertions"], typed["additions"], typed["added"]))
		deletions := reviewPacketInt(firstNonEmptyAny(typed["deletions"], typed["removals"], typed["removed"]))
		status := safePacketText(firstNonEmpty(stringValue(typed["status"]), stringValue(typed["change_type"]), "changed"), 80)
		if insertions == 0 && deletions == 0 {
			out = append(out, fmt.Sprintf("`%s` %s (event:%s)", path, status, firstNonEmpty(kind, "diff_summary")))
		} else {
			out = append(out, fmt.Sprintf("`%s` %s +%d -%d (event:%s)", path, status, insertions, deletions, firstNonEmpty(kind, "diff_summary")))
		}
	}
	return out
}

func pathValuesJoined(value any) string {
	values := pathValues(value)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if strings.TrimSpace(stringValue(value)) != "" {
			return value
		}
	}
	return nil
}

func sessionRefsFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		if ref := sessionRefFromEvent(event); ref != "" {
			out = append(out, ref)
		}
	}
	return dedupeSortedStrings(out)
}

func sessionRefFromEvent(event map[string]any) string {
	payload := reviewPacketEventPayload(event)
	return safePacketText(firstNonEmpty(
		stringValue(payload["session_ref"]),
		stringValue(payload["session_id"]),
		stringValue(payload["sessionId"]),
		stringValue(payload["thread_id"]),
		stringValue(payload["threadId"]),
		stringValue(event["session_ref"]),
		stringValue(event["session_id"]),
		stringValue(event["thread_id"]),
	), 120)
}

func turnIDsFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		payload := reviewPacketEventPayload(event)
		turnID := safePacketText(firstNonEmpty(
			stringValue(payload["turn_id"]),
			stringValue(payload["turnId"]),
			stringValue(event["turn_id"]),
			stringValue(event["turnId"]),
		), 120)
		if turnID != "" {
			out = append(out, turnID)
		}
	}
	return dedupeSortedStrings(out)
}

func tokenTotalsFromEvents(events []map[string]any) runtimeTokenTotals {
	byTurn := map[string]runtimeTokenTotals{}
	for i, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		turnID := firstNonEmpty(stringValue(payload["turn_id"]), stringValue(payload["turnId"]))
		if turnID == "" && !strings.Contains(kind, "turn") {
			continue
		}
		totals := runtimeTokenTotals{
			InputTokens:  reviewPacketInt(payload["input_tokens"]),
			OutputTokens: reviewPacketInt(payload["output_tokens"]),
			TotalTokens:  reviewPacketInt(payload["total_tokens"]),
		}
		if totals.TotalTokens == 0 && (totals.InputTokens > 0 || totals.OutputTokens > 0) {
			totals.TotalTokens = totals.InputTokens + totals.OutputTokens
		}
		if totals.TotalTokens == 0 && totals.InputTokens == 0 && totals.OutputTokens == 0 {
			continue
		}
		key := firstNonEmpty(turnID, fmt.Sprintf("event-%d", i))
		byTurn[key] = totals
	}
	var out runtimeTokenTotals
	for _, totals := range byTurn {
		out.InputTokens += totals.InputTokens
		out.OutputTokens += totals.OutputTokens
		out.TotalTokens += totals.TotalTokens
	}
	if out.TotalTokens == 0 && (out.InputTokens > 0 || out.OutputTokens > 0) {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	return out
}

func runtimeSummariesFromRun(run RunStatus) []string {
	var out []string
	parts := []string{}
	if run.LeaseState != "" {
		parts = append(parts, "lease="+run.LeaseState)
	}
	if run.AttemptOutcome != "" {
		parts = append(parts, "outcome="+run.AttemptOutcome)
	}
	if run.ProcessPID > 0 {
		parts = append(parts, fmt.Sprintf("pid=%d", run.ProcessPID))
	}
	status, statusOK := readReviewPacketRunnerStatus(run.StatusPath)
	if statusOK {
		parts = append(parts, fmt.Sprintf("exit_code=%d", status.ExitCode))
		if status.CompletedAt != "" {
			parts = append(parts, "completed_at="+status.CompletedAt)
		}
	}
	if duration := runDurationSummary(run.StartedAt, firstNonEmpty(status.CompletedAt, run.LastEventAt, run.UpdatedAt)); duration != "" {
		parts = append(parts, "runtime="+duration)
	}
	if len(parts) > 0 {
		out = append(out, strings.Join(parts, " "))
	}
	return out
}

func readReviewPacketRunnerStatus(path string) (runnerProcessStatus, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return runnerProcessStatus{}, false
	}
	status, err := readRunnerProcessStatus(path)
	return status, err == nil
}

func runDurationSummary(startedAt, finishedAt string) string {
	started, err := time.Parse(time.RFC3339, strings.TrimSpace(startedAt))
	if err != nil {
		return ""
	}
	finished, err := time.Parse(time.RFC3339, strings.TrimSpace(finishedAt))
	if err != nil || finished.Before(started) {
		return ""
	}
	return finished.Sub(started).Round(time.Second).String()
}

func openRisksFromEvents(events []map[string]any) []string {
	var out []string
	for _, event := range events {
		kind := reviewPacketEventKind(event)
		payload := reviewPacketEventPayload(event)
		for _, key := range []string{"open_risks", "risks"} {
			for _, risk := range normalizeList(payload[key]) {
				if safe := safePacketText(risk, 220); safe != "" {
					out = append(out, safe)
				}
			}
		}
		if risk := safePacketText(stringValue(payload["risk"]), 220); risk != "" && (strings.Contains(kind, "risk") || strings.Contains(kind, "blocked")) {
			out = append(out, risk)
		}
		result := strings.ToLower(strings.TrimSpace(firstNonEmpty(stringValue(payload["result"]), stringValue(payload["status"]), stringValue(payload["outcome"]))))
		if result == "fail" || result == "failed" || result == "error" || result == "blocked" {
			command := safePacketText(firstNonEmpty(stringValue(payload["check"]), stringValue(payload["command"]), stringValue(payload["cmd"])), 180)
			if command != "" {
				out = append(out, fmt.Sprintf("`%s` result=%s", command, result))
			}
		}
		if strings.Contains(kind, "denied") || strings.Contains(kind, "error") || strings.Contains(kind, "blocked") || strings.Contains(kind, "failed") {
			reason := safePacketText(firstNonEmpty(stringValue(payload["reason"]), stringValue(payload["error"]), stringValue(payload["last_error"]), stringValue(event["message"])), 220)
			if reason != "" {
				out = append(out, fmt.Sprintf("%s: %s", firstNonEmpty(kind, "runtime"), reason))
			}
		}
	}
	return dedupeSortedStrings(out)
}

func openRisksFromRun(run RunStatus) []string {
	if risk := safePacketText(run.LastError, 220); risk != "" {
		return []string{"runtime last_error: " + risk}
	}
	return nil
}

func openRisksForPacket(run RunStatus, turns []RunTurn, supervisorDecisions []RuntimeSupervisorDecision, facts reviewPacketFacts) []string {
	risks := append([]string{}, facts.OpenRisks...)
	for _, risk := range openRisksFromRun(run) {
		risks = append(risks, risk)
	}
	for _, turn := range turns {
		if risk := safePacketText(turn.LastError, 220); risk != "" {
			risks = append(risks, fmt.Sprintf("turn `%s`: %s", turn.TurnID, risk))
		}
	}
	for _, decision := range supervisorDecisions {
		kind := strings.ToLower(strings.TrimSpace(decision.Kind))
		if strings.Contains(kind, "stop") || strings.Contains(kind, "human") || strings.Contains(kind, "audit") {
			reason := safePacketText(firstNonEmpty(decision.Reason, decision.ValidationDelta, decision.ContextSignal), 220)
			if reason != "" {
				risks = append(risks, fmt.Sprintf("supervisor `%s`: %s", decision.Kind, reason))
			}
		}
	}
	return dedupeSortedStrings(risks)
}

func sessionRefsForPacket(run RunStatus, turns []RunTurn, facts reviewPacketFacts) []string {
	refs := append([]string{}, facts.SessionRefs...)
	if ref := safePacketText(run.SessionRef, 120); ref != "" {
		refs = append(refs, ref)
	}
	for _, turn := range turns {
		if ref := safePacketText(turn.SessionRef, 120); ref != "" {
			refs = append(refs, ref)
		}
	}
	return dedupeSortedStrings(refs)
}

func turnIDsForPacket(turns []RunTurn, facts reviewPacketFacts) []string {
	ids := append([]string{}, facts.TurnIDs...)
	for _, turn := range turns {
		if id := safePacketText(turn.TurnID, 120); id != "" {
			ids = append(ids, id)
		}
	}
	return dedupeSortedStrings(ids)
}

func backtickList(values []string) string {
	var out []string
	for _, value := range values {
		if safe := safePacketText(value, 120); safe != "" {
			out = append(out, "`"+safe+"`")
		}
	}
	return strings.Join(out, ", ")
}

func commandDurationText(payload map[string]any) string {
	if value := reviewPacketInt(firstNonEmptyAny(payload["duration_ms"], payload["elapsed_ms"])); value > 0 {
		return (time.Duration(value) * time.Millisecond).Round(time.Millisecond).String()
	}
	if value := reviewPacketInt(firstNonEmptyAny(payload["duration_seconds"], payload["elapsed_seconds"])); value > 0 {
		return (time.Duration(value) * time.Second).String()
	}
	return ""
}

func reviewPacketInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	case string:
		return atoiSafe(strings.TrimSpace(typed))
	default:
		return 0
	}
}

func safePacketText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.Join(strings.Fields(value), " ")
	for _, pattern := range []string{
		`(?i)(authorization:\s*bearer\s+)[^\s]+`,
		`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret)=)[^\s]+`,
		`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret):\s*)[^\s]+`,
	} {
		value = regexp.MustCompile(pattern).ReplaceAllString(value, "${1}[redacted]")
	}
	if limit > 0 && len(value) > limit {
		value = strings.TrimSpace(value[:limit]) + "..."
	}
	return value
}

func dedupeSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func latestObservedRunEventAt(run RunStatus) (time.Time, bool) {
	var latest time.Time
	found := false
	if eventAt, ok := latestRunnerEventSinkAt(run.EventSinkPath); ok {
		latest = eventAt
		found = true
	}
	if rawAt, ok := latestNonEmptyFileModTime(run.RawLogPath); ok && (!found || rawAt.After(latest)) {
		latest = rawAt
		found = true
	}
	if found {
		return latest, true
	}
	return time.Time{}, false
}

func latestRunnerEventSinkAt(path string) (time.Time, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return time.Time{}, false
	}
	text, err := readText(path)
	if err != nil {
		return time.Time{}, false
	}
	var latest time.Time
	found := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		kind := strings.TrimSpace(stringValue(payload["kind"]))
		if daemonOwnedEventKind(kind) {
			continue
		}
		at, ok := parseEventTimestamp(stringValue(payload["at"]))
		if !ok {
			continue
		}
		if !found || at.After(latest) {
			latest = at
			found = true
		}
	}
	return latest, found
}

func daemonOwnedEventKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "", "attempt_started", "attempt_spawned", "supervisor_decision":
		return true
	default:
		return false
	}
}

func parseEventTimestamp(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func latestNonEmptyFileModTime(path string) (time.Time, bool) {
	info, err := os.Stat(strings.TrimSpace(path))
	if err != nil || info.Size() == 0 {
		return time.Time{}, false
	}
	return info.ModTime().UTC(), true
}

func latestRunEventAt(run RunStatus) (time.Time, bool) {
	if latest, ok := latestObservedRunEventAt(run); ok {
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
	runner, _, err := runnerForName(run.Runner, wf)
	heartbeatCapable := err == nil && runner.Capabilities().Heartbeats
	timeout := heartbeatDeadThresholdForRun(run, wf)
	if strings.TrimSpace(run.FirstEventAt) == "" {
		startedAt, startedOK := parseRunTimestamp(firstNonEmpty(run.ProcessStartedAt, run.StartedAt, run.UpdatedAt))
		if heartbeatCapable {
			if lastHeartbeatAt, heartbeatOK := parseRunTimestamp(run.LastHeartbeatAt); heartbeatOK {
				if now.Sub(lastHeartbeatAt) <= timeout {
					return false, ""
				}
				if !startedOK || lastHeartbeatAt.After(startedAt) || lastHeartbeatAt.Equal(startedAt) {
					return true, fmt.Sprintf("runner heartbeat dead before first event: no heartbeat since %s", lastHeartbeatAt.Format(time.RFC3339))
				}
			}
		}
		deadline := firstEventDeadlineForRun(run, wf)
		if startedOK && now.Sub(startedAt) > deadline {
			return true, fmt.Sprintf("runner never started: no first event within %s of spawn", deadline)
		}
		return false, ""
	}
	if !heartbeatCapable {
		return false, ""
	}
	lastHeartbeatAt, ok := parseRunTimestamp(firstNonEmpty(run.LastHeartbeatAt, run.LastEventAt, run.FirstEventAt))
	if !ok {
		return false, ""
	}
	if now.Sub(lastHeartbeatAt) <= timeout {
		return false, ""
	}
	if RunnerName(run.Runner) == RunnerCodexExec {
		if commandStartedAt, ok := codexExecInFlightCommandStartedAt(run, lastHeartbeatAt); ok {
			commandTimeout := codexExecInFlightCommandTimeout(wf)
			if !now.After(commandStartedAt.Add(commandTimeout)) {
				return false, ""
			}
			return true, fmt.Sprintf("runner in-flight command exceeded cap %s: command started %s; no events since %s", commandTimeout, commandStartedAt.Format(time.RFC3339), lastHeartbeatAt.Format(time.RFC3339))
		}
	}
	return true, fmt.Sprintf("runner heartbeat dead (idle): no events since %s", lastHeartbeatAt.Format(time.RFC3339))
}

func firstEventDeadlineForRun(run RunStatus, wf Workflow) time.Duration {
	if wf.Codex.StallTimeoutMS > 0 && RunnerName(run.Runner) == RunnerCodex {
		return time.Duration(wf.Codex.StallTimeoutMS) * time.Millisecond
	}
	return daemonFirstEventDeadline
}

func heartbeatDeadThresholdForRun(run RunStatus, wf Workflow) time.Duration {
	if wf.Codex.StallTimeoutMS > 0 && RunnerName(run.Runner) == RunnerCodex {
		return time.Duration(wf.Codex.StallTimeoutMS) * time.Millisecond
	}
	return daemonHeartbeatDeadThreshold
}

func codexExecInFlightCommandTimeout(wf Workflow) time.Duration {
	if wf.Codex.TurnTimeoutMS > 0 {
		return time.Duration(wf.Codex.TurnTimeoutMS) * time.Millisecond
	}
	defaults := defaultWorkflow()
	if defaults.Codex.TurnTimeoutMS > 0 {
		return time.Duration(defaults.Codex.TurnTimeoutMS) * time.Millisecond
	}
	return 10 * time.Minute
}

func parseRunTimestamp(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func (d *Daemon) stopRunExecution(ctx context.Context, run RunStatus) (bool, error) {
	if handle := liveRegistry.Find(firstNonEmpty(run.ActiveAttemptID, run.ItemID, run.RecordID)); handle != nil {
		return true, handle.Interrupt(ctx)
	}
	if run.ProcessPID <= 0 || !processIdentityMatches(run) {
		return false, nil
	}
	pgid := processSignalGroup(run)
	if err := syscall.Kill(-pgid, syscall.SIGINT); err != nil && !strings.Contains(err.Error(), "no such process") {
		return false, err
	}
	for i := 0; i < 6 && processExists(run.ProcessPID); i++ {
		time.Sleep(150 * time.Millisecond)
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		for i := 0; i < 4 && processExists(run.ProcessPID); i++ {
			time.Sleep(150 * time.Millisecond)
		}
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	return true, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	if err != nil {
		return true
	}
	return !processIsZombie(pid)
}

func processIsZombie(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(string(out)), "Z")
}

func processGroupID(pid int) int {
	if pid <= 0 {
		return 0
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid <= 0 {
		return pid
	}
	return pgid
}

func processSignalGroup(run RunStatus) int {
	if run.ProcessPGID > 0 {
		return run.ProcessPGID
	}
	return run.ProcessPID
}

func recordedProcessStartTime(pid int, fallback string) string {
	if startedAt, ok := processStartTime(pid); ok {
		return startedAt
	}
	return fallback
}

func processStartTime(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return "", false
	}
	text := strings.Join(strings.Fields(string(out)), " ")
	if text == "" {
		return "", false
	}
	if parsed, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", text, time.Local); err == nil {
		return parsed.UTC().Format(time.RFC3339), true
	}
	return text, true
}

func processIdentityMatches(run RunStatus) bool {
	if run.ProcessPID <= 0 || !processExists(run.ProcessPID) {
		return false
	}
	if run.ProcessPGID > 0 && processGroupID(run.ProcessPID) != run.ProcessPGID {
		return false
	}
	if expected := strings.TrimSpace(run.ProcessStartedAt); expected != "" {
		if actual, ok := processStartTime(run.ProcessPID); ok && actual != expected {
			return false
		}
	}
	return true
}

func runnerForName(name string, wf Workflow) (Runner, string, error) {
	definition, hasDefinition := wf.Runners[strings.TrimSpace(name)]
	runnerKind := RunnerName(name)
	command := ""
	if hasDefinition {
		command = strings.TrimSpace(definition.Command)
		if strings.TrimSpace(definition.Kind) != "" {
			runnerKind = RunnerName(strings.TrimSpace(definition.Kind))
		}
	}
	switch runnerKind {
	case RunnerCodex:
		return &CodexRunner{}, wf.Codex.Command, nil
	case RunnerCodexAppServer:
		return &CodexAppServerRunner{}, firstNonEmpty(command, wf.Codex.Command, "codex app-server"), nil
	case RunnerCodexExec:
		return &CodexExecRunner{}, firstNonEmpty(command, defaultCodexExecCommand()), nil
	case RunnerCodexCloud:
		config := wf.CodexCloud
		if hasDefinition {
			config = codexCloudConfigFromRunnerDefinition(definition, config)
		}
		return &CodexCloudRunner{Config: config}, firstNonEmpty(command, config.Command), nil
	case RunnerClaude:
		return &ClaudeRunner{}, firstNonEmpty(command, wf.Claude.Command), nil
	default:
		return nil, "", tuskerError(errorConfigInvalid, "unsupported runner: "+name)
	}
}

func codexCloudConfigFromRunnerDefinition(definition RunnerDefinition, fallbackConfig CodexCloudConfig) CodexCloudConfig {
	config := fallbackConfig
	if strings.TrimSpace(definition.Command) != "" {
		config.Command = definition.Command
	}
	if strings.TrimSpace(definition.StatusCommand) != "" {
		config.StatusCommand = definition.StatusCommand
	}
	if strings.TrimSpace(definition.CollectCommand) != "" {
		config.CollectCommand = definition.CollectCommand
	}
	if strings.TrimSpace(definition.EnvironmentID) != "" {
		config.EnvironmentID = definition.EnvironmentID
	}
	if strings.TrimSpace(definition.ApplyMode) != "" {
		config.ApplyMode = definition.ApplyMode
	}
	if strings.TrimSpace(definition.PRMode) != "" {
		config.PRMode = definition.PRMode
	}
	if definition.ExternalCollect {
		config.ExternalCollect = true
	}
	return config
}

func sessionStateForOutcome(outcome AttemptOutcome) string {
	switch outcome {
	case AttemptOutcomeSucceeded:
		return "open"
	case AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeCancelled:
		return "open"
	case AttemptOutcomeAbandoned:
		return "abandoned"
	case AttemptOutcomeDispatchDeclined:
		return "closed"
	case AttemptOutcomeWaitingForHuman:
		return "closed"
	case AttemptOutcomeWaitingForReview:
		return "closed"
	case AttemptOutcomeBudgetExceeded:
		return "closed"
	case AttemptOutcomeTurnCapExhausted:
		return "open"
	default:
		return "open"
	}
}

func sessionStateForLeaseState(state LeaseState) string {
	switch state {
	case LeaseStateReleased, LeaseStateParkedNoProgress, LeaseStateParkedBudget:
		return "closed"
	case LeaseStateRetryQueued, LeaseStateClaimed, LeaseStateRunning, LeaseStateUnclaimed, LeaseStateInterrupted:
		return "open"
	default:
		return "open"
	}
}

func workspaceStrategyFromWorkflow(value string) WorkspaceStrategy {
	switch strings.TrimSpace(value) {
	case "", string(WorkspaceStrategyInPlace):
		return WorkspaceStrategyInPlace
	case string(WorkspaceStrategyWorktree):
		return WorkspaceStrategyWorktree
	case string(WorkspaceStrategyClone):
		return WorkspaceStrategyClone
	case string(WorkspaceStrategyCopy):
		return WorkspaceStrategyCopy
	default:
		return WorkspaceStrategyInPlace
	}
}

func workspaceStrategyForRun(wf Workflow, project RegisteredProject, run RunStatus, runs []RunStatus) WorkspaceStrategy {
	configured := workspaceStrategyFromWorkflow(wf.Workspace.Strategy)
	if configured != WorkspaceStrategyInPlace {
		return configured
	}
	for _, other := range runs {
		if project.ProjectID != "" && other.ProjectID != project.ProjectID {
			continue
		}
		if other.RecordID == run.RecordID {
			continue
		}
		if isDispatchingLeaseState(other.LeaseState) {
			return WorkspaceStrategyWorktree
		}
	}
	return configured
}

var workflowTemplatePlaceholder = regexp.MustCompile(`{{\s*([A-Za-z0-9_.]+)\s*}}`)

func renderAttemptPrompt(project RegisteredProject, wfFile WorkflowFile, note Note, workspacePath string, attemptNumber int, attemptID, lane string, run RunStatus, previousRun RunStatus, store *RuntimeStore) (string, error) {
	values := map[string]string{
		"project.name":                project.Name,
		"project.id":                  project.ProjectID,
		"project.key":                 project.ProjectKey,
		"vault.path":                  project.VaultRoot,
		"repo.root":                   project.RepoRoot,
		"workspace.path":              workspacePath,
		"workflow.path":               wfFile.Path,
		"note.id":                     stringField(note.Data, "id"),
		"note.record_id":              trackerRecordID(note),
		"note.title":                  stringField(note.Data, "title"),
		"note.status":                 stringField(note.Data, "status"),
		"note.type":                   stringField(note.Data, "type"),
		"note.risk":                   stringField(note.Data, "risk"),
		"attempt.number":              strconv.Itoa(attemptNumber),
		"attempt.id":                  attemptID,
		"reviewer.actor":              reviewerActorForNote(wfFile.Data.Reviewer.Actor, note),
		"reviewer.auto_close_allowed": yesNo(reviewerMayAutoCloseRisk(wfFile.Data.Reviewer, stringField(note.Data, "risk"))),
		"reviewer.human_required":     yesNo(reviewerRequiresHumanRisk(wfFile.Data.Reviewer, stringField(note.Data, "risk"))),
	}
	values["reviewer.verify_command"] = reviewerVerifyCommandForNote(note, values["reviewer.actor"])
	values["reviewer.close_command"] = fmt.Sprintf("tusker close %s --by %s --reason \"agent review accepted\"", stringField(note.Data, "id"), values["reviewer.actor"])
	template := wfFile.Body
	if lane == runLaneReview {
		template = firstNonEmpty(wfFile.Data.Reviewer.Prompt, defaultReviewerPrompt())
	}
	rendered, err := renderStrictWorkflowTemplate(template, values)
	if err != nil {
		return "", tuskerError(errorConfigInvalid, err.Error(), withPath(wfFile.Path))
	}
	if ralphContext, err := renderRalphAttemptPromptContext(project, wfFile, note, attemptNumber, attemptID, lane, previousRun); err != nil {
		return "", err
	} else if ralphContext != "" {
		rendered = strings.TrimSpace(rendered) + "\n\n" + ralphContext
	}
	if runtimeContext := renderExternalLoopRuntimePromptContext(store, project.ProjectID, trackerRecordID(note), run); runtimeContext != "" {
		rendered = strings.TrimSpace(rendered) + "\n\n" + runtimeContext
	}
	return strings.TrimSpace(rendered) + "\n", nil
}

type taskPlanSnapshot struct {
	Path     string
	Display  string
	Contents string
	Created  bool
}

func renderRalphAttemptPromptContext(project RegisteredProject, wfFile WorkflowFile, note Note, attemptNumber int, attemptID, lane string, previousRun RunStatus) (string, error) {
	if !isV7TaskNote(note) || strings.TrimSpace(project.VaultRoot) == "" || !fileExists(project.VaultRoot) {
		return "", nil
	}
	taskID := stringField(note.Data, "id")
	if taskID == "" {
		return "", nil
	}
	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		return "", err
	}
	plan, err := ensureTaskPlanFile(project.VaultRoot, taskID, stringField(note.Data, "title"))
	if err != nil {
		return "", err
	}
	signs, signsLines, signsPresent, err := readTuskerSigns(project.VaultRoot)
	if err != nil {
		return "", err
	}
	audience := "agent"
	if lane == runLaneReview {
		audience = "reviewer"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Tusker Attempt Context\n\n")
	fmt.Fprintf(&b, "- Attempt: %d (%s)\n", attemptNumber, attemptID)
	fmt.Fprintf(&b, "- Fresh context rule: this attempt is a new runner session/thread. Do not query or append to predecessor transcripts.\n")
	fmt.Fprintf(&b, "- Durable state rule: use the plan file below as disposable cross-attempt state; do not cite it as proof or add it to task markdown.\n\n")
	fmt.Fprintf(&b, "### Task Packet\n\n%s\n", strings.TrimSpace(v7Packet(project.VaultRoot, note, idx, audience)))
	fmt.Fprintf(&b, "\n### Durable Plan File\n\n")
	fmt.Fprintf(&b, "- Path: `%s`\n", plan.Display)
	if plan.Created {
		fmt.Fprintf(&b, "- Lifecycle: created for this first attempt; it survives retries and is removed when the task closes.\n")
	} else {
		fmt.Fprintf(&b, "- Lifecycle: existing file loaded from a prior attempt; update it before finishing or stopping.\n")
	}
	fmt.Fprintf(&b, "\n```markdown\n%s\n```\n", strings.TrimSpace(plan.Contents))
	fmt.Fprintf(&b, "\n### Previous Structured Outcome\n\n%s\n", renderPreviousStructuredOutcome(previousRun))
	fmt.Fprintf(&b, "\n### Backpressure\n\n")
	fmt.Fprintf(&b, "- Source: %s\n", backpressureCommandSource(project.VaultRoot))
	fmt.Fprintf(&b, "- Run validation serially. One build/test invocation at a time; no parallel validation storms.\n\n")
	fmt.Fprintf(&b, "```bash\n%s\n```\n", strings.Join(backpressureCommands(project.VaultRoot), "\n"))
	fmt.Fprintf(&b, "\n### Ralph Rules\n\n")
	fmt.Fprintf(&b, "- Work exactly one task contract in this attempt.\n")
	fmt.Fprintf(&b, "- Search before implementing; do not create duplicate implementations because a first `rg` missed something.\n")
	fmt.Fprintf(&b, "- Do not add placeholder, stub, or fake-simple implementations to satisfy a compiler.\n")
	fmt.Fprintf(&b, "- Read the plan, do the next undone item, update the plan, verify, then finish or park with a concrete reason.\n")
	if signsPresent {
		fmt.Fprintf(&b, "\n### Repo Signs\n\n")
		if signsLines > tuskerSignsWarnLineLimit {
			fmt.Fprintf(&b, "- Warning: `%s` has %d lines; cap is %d. Trim it before adding more signs.\n\n", tuskerSignsDisplayPath(project.VaultRoot), signsLines, tuskerSignsWarnLineLimit)
		}
		fmt.Fprintf(&b, "```markdown\n%s\n```\n", strings.TrimSpace(signs))
	}
	return strings.TrimSpace(b.String()), nil
}

func ensureTaskPlanFile(vaultPath, taskID, title string) (taskPlanSnapshot, error) {
	path := taskPlanPath(vaultPath, taskID)
	display := taskPlanDisplayPath(taskID)
	if strings.TrimSpace(path) == "" {
		return taskPlanSnapshot{}, nil
	}
	created := false
	if !fileExists(path) {
		if err := ensureDir(filepath.Dir(path)); err != nil {
			return taskPlanSnapshot{}, err
		}
		if err := writeText(path, defaultTaskPlanContents(taskID, title)); err != nil {
			return taskPlanSnapshot{}, err
		}
		created = true
	}
	contents, err := readText(path)
	if err != nil {
		return taskPlanSnapshot{}, err
	}
	return taskPlanSnapshot{Path: path, Display: display, Contents: contents, Created: created}, nil
}

func taskPlanPath(vaultPath, taskID string) string {
	vaultPath = strings.TrimSpace(vaultPath)
	taskID = strings.TrimSpace(taskID)
	if vaultPath == "" || taskID == "" {
		return ""
	}
	return filepath.Join(vaultPath, "scratch", taskID, "PLAN.md")
}

func taskPlanDisplayPath(taskID string) string {
	return filepath.ToSlash(filepath.Join(".tusker", "scratch", strings.TrimSpace(taskID), "PLAN.md"))
}

func defaultTaskPlanContents(taskID, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = taskID
	}
	return fmt.Sprintf(`# %s Plan

- [ ] Read this plan, the task packet, and the previous structured outcome.
- [ ] Search for existing implementation before editing.
- [ ] Do the next undone implementation or verification item.
- [ ] Update this plan before finishing, parking, or responding to a stop signal.
- [ ] Run the configured backpressure commands and record concise proof.
`, title)
}

func removeTaskPlanFile(vaultPath, taskID string) error {
	path := taskPlanPath(vaultPath, taskID)
	if strings.TrimSpace(path) == "" || !fileExists(path) {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Dir(path))
	return nil
}

func renderPreviousStructuredOutcome(run RunStatus) string {
	if !previousRunHasStructuredOutcome(run) {
		return "- Previous attempt: none"
	}
	lines := []string{
		"- Previous attempt: " + fallback(strings.TrimSpace(run.ActiveAttemptID), "unknown"),
		"- Previous lease state: " + fallback(strings.TrimSpace(run.LeaseState), "unknown"),
		"- Previous outcome: " + fallback(strings.TrimSpace(run.AttemptOutcome), "unknown"),
	}
	if reason := previousStructuredOutcomeReason(run); reason != "" {
		lines = append(lines, "- Previous reason: "+reason)
	}
	return strings.Join(lines, "\n")
}

func previousRunHasStructuredOutcome(run RunStatus) bool {
	return run.AttemptCount > 0 ||
		strings.TrimSpace(run.ActiveAttemptID) != "" ||
		strings.TrimSpace(run.SessionRef) != "" ||
		strings.TrimSpace(run.LastError) != "" ||
		strings.TrimSpace(run.FinalSummary) != "" ||
		strings.TrimSpace(run.LogsSummary) != ""
}

func previousStructuredOutcomeReason(run RunStatus) string {
	return safePacketText(firstNonEmpty(run.LastError, run.FinalSummary, run.LogsSummary), 480)
}

func backpressureCommands(vaultPath string) []string {
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err == nil {
		if commands := normalizeList(cfg.Automation.Validation.Commands); len(commands) > 0 {
			return commands
		}
	}
	return []string{"go build ./...", "go vet ./...", "go test ./... -count=1"}
}

func backpressureCommandSource(vaultPath string) string {
	cfg, _, err := readV7TuskerConfig(vaultPath)
	if err == nil && len(normalizeList(cfg.Automation.Validation.Commands)) > 0 {
		return "`tusker.yaml` automation.validation.commands"
	}
	return "built-in default gate"
}

func readTuskerSigns(vaultPath string) (string, int, bool, error) {
	path := filepath.Join(strings.TrimSpace(vaultPath), "signs.md")
	if strings.TrimSpace(vaultPath) == "" || !fileExists(path) {
		return "", 0, false, nil
	}
	text, err := readText(path)
	if err != nil {
		return "", 0, false, err
	}
	return text, countNonEmptyLines(text), true, nil
}

func tuskerSignsDisplayPath(vaultPath string) string {
	_ = vaultPath
	return ".tusker/signs.md"
}

func renderExternalLoopRuntimePromptContext(store *RuntimeStore, projectID, recordID string, run RunStatus) string {
	if store == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return ""
	}
	inputs, _ := store.ListApplyInputsForRun(projectID, recordID)
	events, _ := store.ListExternalLoopEvents(projectID, recordID)
	if len(inputs) == 0 && len(events) == 0 && strings.TrimSpace(run.LastError) == "" {
		return ""
	}
	var lines []string
	lines = append(lines, "## Tusker Runtime Context")
	if len(events) > 0 {
		latest := events[len(events)-1]
		lines = append(lines, fmt.Sprintf("- Latest external-loop event: stage=%s action=%s status=%s runner=%s job=%s", latest.Stage, latest.Action, latest.Status, fallback(latest.Runner, "-"), fallback(latest.JobID, "-")))
		if strings.TrimSpace(latest.Reason) != "" {
			lines = append(lines, "- Latest external-loop reason: "+latest.Reason)
		}
	}
	if strings.TrimSpace(run.LastError) != "" {
		lines = append(lines, "- Previous attempt error: "+run.LastError)
	}
	if len(inputs) > 0 {
		var paths []string
		for _, input := range inputs {
			paths = append(paths, firstNonEmpty(input.RelPath, input.Path))
		}
		lines = append(lines, "- External apply inputs: "+strings.Join(paths, ", "))
	}
	switch firstNonEmpty(strings.TrimSpace(run.Lane), runLaneExecute) {
	case runLaneReview:
		lines = append(lines, "- Current external-loop role: review the applied result. Return review notes and only include a patch if concrete rework is required.")
	default:
		for i := len(events) - 1; i >= 0; i-- {
			if normalizeExternalLoopAction(events[i].Action) == externalLoopActionContinueThreadOnFailure {
				lines = append(lines, "- Current external-loop role: repair continuation. Use the failure context to return a corrected patch or explicit blocker notes.")
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func reviewerActorForNote(configured string, note Note) string {
	configured = strings.TrimSpace(configured)
	if isV7TaskNote(note) {
		switch configured {
		case "", "agent-reviewer", defaultReviewerActor:
			return "reviewer:agent"
		default:
			return configured
		}
	}
	switch configured {
	case "", "reviewer:agent", defaultReviewerActor:
		return "agent-reviewer"
	default:
		return configured
	}
}

func reviewerVerifyCommandForNote(note Note, actor string) string {
	if isV7TaskNote(note) {
		covers := strings.Join(v7AcceptanceIDs(note.Body), ",")
		if covers == "" {
			covers = "ALL"
		}
		return fmt.Sprintf("tusker verify add %s --by %s --covers %s --check \"review: acceptance, evidence, gates, and docs\" --result pass --note \"<what you verified>\"", stringField(note.Data, "id"), actor, covers)
	}
	return fmt.Sprintf("tusker verify %s --by %s --summary \"<what you verified>\"", stringField(note.Data, "id"), actor)
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
	data["status"] = "review"
	data["review_requested_at"] = now
	data["updated"] = date
	appendTransition(data, orderedTransition(now, "status", prevStatus, "review", "daemon", "runner attempt succeeded"))
	body = appendWorkLogBullet(body, fmt.Sprintf("%s — daemon — implementation pass completed; review requested", date))
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
	stateRoot := DefaultStateRoot()
	guard, err := acquireDaemonGuard(stateRoot)
	if err != nil {
		return err
	}
	defer guard.Close()
	daemon, err := NewDaemon(stateRoot)
	if err != nil {
		return err
	}
	daemon.guard = guard
	defer daemon.Close()
	if args.Bool("once") {
		daemon.dispatchRefusalReason = oneShotDispatchRefusal("tusker daemon run --once")
	}
	return daemon.Run(context.Background(), args.Bool("once"))
}

func daemonStatusCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := loadRegisteredProjects(store, registeredProjectLoadOptions{}); err != nil {
		return err
	}
	status, err := store.DaemonStatus()
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "status": status})
		return nil
	}
	fmt.Printf("Daemon state root: %s\n", status["state_root"])
	fmt.Printf("Runtime store: %s\n", status["runtime_store_path"])
	if boolFromAny(status["daemon_alive"]) {
		fmt.Printf("Daemon pid: %v uptime=%s\n", status["daemon_pid"], (time.Duration(int64FromAny(status["daemon_uptime_seconds"])) * time.Second).String())
	} else {
		fmt.Println("Daemon pid: none")
	}
	fmt.Printf("Registered projects: %v\n", status["projects"])
	if projects, ok := status["project_health"].([]RegisteredProject); ok {
		for _, project := range projects {
			state := "enabled"
			if !project.Enabled {
				state = "disabled"
			}
			line := fmt.Sprintf("  %-12s %-8s %-8s %s", firstNonEmpty(project.ProjectKey, project.ProjectID), state, project.Health, project.RepoRoot)
			if strings.TrimSpace(project.LastError) != "" {
				line += " (" + project.LastError + ")"
			}
			fmt.Println(line)
		}
	}
	fmt.Printf("Active runs: %v / %v\n", status["activeRuns"], status["max_active_runs"])
	fmt.Printf("Parked no-progress runs: %v\n", status["parkedNoProgressRuns"])
	fmt.Printf("Parked budget runs: %v\n", status["parkedBudgetRuns"])
	if boolFromAny(status["invariant_circuit_open"]) {
		fmt.Printf("Invariant circuit: open (%s)\n", stringValue(status["invariant_circuit_reason"]))
	}
	if boolFromAny(status["budget_circuit_open"]) {
		fmt.Printf("Budget circuit: open until %s (%s)\n", stringValue(status["budget_circuit_reset_at"]), stringValue(status["budget_circuit_reason"]))
	}
	return nil
}

func daemonResumeCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	status, err := daemon.ResumeInvariantCircuit()
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "resumed": true, "status": status})
		return nil
	}
	fmt.Println("Invariant circuit closed; daemon dispatch may resume.")
	return nil
}

func daemonLimitsCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	if _, err := loadRegisteredProjects(store, registeredProjectLoadOptions{}); err != nil {
		return err
	}
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

func daemonStopCmd(args Args) error {
	stateRoot := DefaultStateRoot()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return err
	}
	if _, err := loadRegisteredProjects(store, registeredProjectLoadOptions{}); err != nil {
		_ = store.Close()
		return err
	}
	_ = store.Close()
	drain := args.Bool("drain")
	before := readDaemonLiveness(stateRoot, time.Now().UTC())
	if !before.Alive {
		if drain {
			drained, err := daemonDrainWrappers(stateRoot, daemonStopDrainTimeout(args))
			if err != nil {
				return err
			}
			if args.Bool("json") {
				emitJSON(map[string]any{"ok": true, "stopped": false, "drained": drained, "message": "daemon is not running"})
				return nil
			}
			if drained {
				fmt.Println("Daemon is not running; wrapper drain complete")
			} else {
				fmt.Println("Daemon is not running; wrapper drain timed out")
			}
			return nil
		}
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "stopped": false, "message": "daemon is not running"})
			return nil
		}
		fmt.Println("Daemon is not running")
		return nil
	}
	resp, err := sendDaemonControl(stateRoot, daemonControlRequest{Command: "stop"})
	if err != nil {
		return tuskerError(errorInvalidTransition, "daemon stop failed: "+err.Error(), withHint("check `tusker daemon status`; if the control socket is stale, stop pid "+strconv.Itoa(before.PID)+" manually"))
	}
	if !resp.OK {
		return tuskerError(errorInvalidTransition, "daemon stop refused: "+firstNonEmpty(resp.Message, "unknown error"))
	}
	deadline := time.Now().UTC().Add(5 * time.Second)
	for {
		if !readDaemonLiveness(stateRoot, time.Now().UTC()).Alive {
			drained := false
			if drain {
				var drainErr error
				drained, drainErr = daemonDrainWrappers(stateRoot, daemonStopDrainTimeout(args))
				if drainErr != nil {
					return drainErr
				}
			}
			if args.Bool("json") {
				payload := map[string]any{"ok": true, "stopped": true, "pid": before.PID, "message": firstNonEmpty(resp.Message, "daemon stopped")}
				if drain {
					payload["drained"] = drained
				}
				emitJSON(payload)
				return nil
			}
			if drain && !drained {
				fmt.Printf("Daemon stopped pid %d; wrapper drain timed out\n", before.PID)
			} else if drain {
				fmt.Printf("Daemon stopped pid %d; wrapper drain complete\n", before.PID)
			} else {
				fmt.Printf("Daemon stopped pid %d\n", before.PID)
			}
			return nil
		}
		if time.Now().UTC().After(deadline) {
			return tuskerError(errorInvalidTransition, "daemon stop timed out", withContext(map[string]any{"pid": before.PID}))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func daemonStopDrainTimeout(args Args) time.Duration {
	if ms := atoiSafe(strings.TrimSpace(args.String("drain-timeout-ms"))); ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 30 * time.Second
}

func daemonDrainWrappers(stateRoot string, timeout time.Duration) (bool, error) {
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		return false, err
	}
	defer store.Close()
	return waitForWrapperDrain(store, timeout)
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
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{})
	if err != nil {
		return err
	}
	projects := loadedRegisteredProjects(loaded)
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
		line := fmt.Sprintf("%s %-8s %-8s %s", project.ProjectID, state, project.Health, project.RepoRoot)
		if strings.TrimSpace(project.LastError) != "" {
			line += " (" + project.LastError + ")"
		}
		fmt.Println(line)
	}
	return nil
}

func projectsLimitsCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	loaded, err := resolveLoadedRegisteredProject(store, args, registeredProjectLoadOptions{})
	if err != nil {
		return err
	}
	project := loaded.Project
	if raw := strings.TrimSpace(args.String("max-active-runs")); raw != "" {
		limit := atoiSafe(raw)
		if limit <= 0 {
			return tuskerError(errorInvalidArg, "--max-active-runs must be > 0", withContext(map[string]any{"arg": "--max-active-runs", "value": raw}))
		}
		wfFile, err := setWorkflowProjectRunLimit(project.VaultRoot, limit)
		if err != nil {
			return err
		}
		loaded.Workflow = wfFile
		loaded.LoadError = nil
	} else if loaded.LoadError != nil {
		return projectQuarantinedError(project)
	}
	limit := projectActiveRunLimit(loaded.Workflow.Data)
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
	daemon.dispatchRefusalReason = oneShotDispatchRefusal("tusker refresh")
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
	loaded, err := resolveLoadedRegisteredProject(store, args, registeredProjectLoadOptions{})
	if err != nil {
		return nil, err
	}
	project := loaded.Project
	return &project, nil
}

func resolveLoadedRegisteredProject(store *RuntimeStore, args Args, opts registeredProjectLoadOptions) (*loadedRegisteredProject, error) {
	loaded, err := loadRegisteredProjects(store, opts)
	if err != nil {
		return nil, err
	}
	if len(loaded) == 0 {
		return nil, tuskerError(errorNotFound, "no registered projects")
	}
	if projectID := strings.TrimSpace(args.String("id")); projectID != "" {
		for _, project := range loaded {
			if project.Project.ProjectID == projectID {
				copy := project
				return &copy, nil
			}
		}
		return nil, tuskerError(errorNotFound, "project not found: "+projectID)
	}

	targets := []string{}
	explicitPathSelector := strings.TrimSpace(args.String("repo")) != "" || strings.TrimSpace(args.String("vault")) != ""
	cwdTarget := ""
	for i, raw := range []string{args.String("repo"), args.String("vault"), mustGetwd()} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		targets = append(targets, abs)
		if i == 2 {
			cwdTarget = abs
		}
	}
	runs, _ := store.ListRuns()

	bestIndex := -1
	bestScore := -1
	ambiguous := false
	for i, project := range loaded {
		score := 0
		for _, target := range targets {
			score = maxInt(score, projectPathMatchScore(project.Project, target))
		}
		if !explicitPathSelector && cwdTarget != "" {
			score = maxInt(score, projectWorkspaceMatchScore(project.Project, runs, cwdTarget))
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
	copy := loaded[bestIndex]
	return &copy, nil
}

func projectWorkspaceMatchScore(project RegisteredProject, runs []RunStatus, target string) int {
	score := 0
	for _, run := range runs {
		if strings.TrimSpace(run.ProjectID) != strings.TrimSpace(project.ProjectID) {
			continue
		}
		workspace := strings.TrimSpace(run.WorkspacePath)
		if workspace == "" {
			continue
		}
		if pathWithin(workspace, target) {
			score = maxInt(score, len(canonicalPath(workspace)))
		}
	}
	return score
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
