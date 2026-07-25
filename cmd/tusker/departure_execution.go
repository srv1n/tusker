package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const departureHeldError = "DEPARTURE_HELD"

var errDepartureExecutionDeferred = errors.New("departure execution deferred")

const (
	departureExecutionErrorLimit = 480
	departureExecutionErrorCap   = 1_000_000
)

var departureExecutionLog = func(projectID, runID, state, message string) {
	fmt.Fprintf(os.Stderr, "departure executor project=%s run=%s state=%s error=%s\n", projectID, runID, state, message)
}

// startPendingDepartureExecutions is the resident-daemon handoff from durable
// scheduling to execution. Long landing and gate work runs outside the poll so
// one project cannot stall reconciliation or independent work in another.
func (d *Daemon) startPendingDepartureExecutions(ctx context.Context, project RegisteredProject, wf Workflow) error {
	if d == nil || d.store == nil || d.departureExecutionDisabled || !wf.ScheduledPromotion.Effective.Observe {
		return nil
	}
	recoveries, err := d.store.ReconcileDepartureRunsForProject(project, d.activeDepartureExecutionIDs()...)
	if err != nil {
		return err
	}
	policyID := departurePolicyID(wf.ScheduledPromotion.Effective)
	for _, recovery := range recoveries {
		run := recovery.Run
		if recovery.Disposition != DepartureRecoveryResumable ||
			run.PolicyID != policyID ||
			!departureExecutionState(run.State) ||
			(run.State == DepartureStatePromoted && wf.ScheduledPromotion.Effective.Release) {
			continue
		}
		workerCtx, claimed := d.claimDepartureExecutionContext(ctx, run.ID)
		if !claimed {
			continue
		}
		go func(runID string, workerCtx context.Context) {
			defer d.releaseDepartureExecution(runID)
			executeErr := d.executeDeparture(workerCtx, project, wf, runID)
			if departureExecutionContextCanceled(executeErr) {
				return
			}
			if executeErr != nil {
				persisted, persistErr := d.recordDepartureExecutionError(runID, executeErr)
				message := safePacketText(executeErr.Error(), departureExecutionErrorLimit)
				if persistErr != nil {
					message += "; persistence failed: " + safePacketText(persistErr.Error(), 180)
				}
				departureExecutionLog(project.ProjectID, runID, firstNonEmpty(string(persisted.State), "unknown"), safePacketText(message, departureExecutionErrorLimit+200))
				return
			}
			_ = d.clearDepartureExecutionError(runID)
		}(run.ID, workerCtx)
	}
	return nil
}

func departureExecutionState(state DepartureState) bool {
	switch state {
	case DepartureStateDue, DepartureStateEvaluating, DepartureStateStaging, DepartureStateGating, DepartureStatePromoted:
		return true
	default:
		return false
	}
}

func (d *Daemon) claimDepartureExecution(runID string) bool {
	_, claimed := d.claimDepartureExecutionContext(context.Background(), runID)
	return claimed
}

func (d *Daemon) claimDepartureExecutionContext(parent context.Context, runID string) (context.Context, bool) {
	if parent == nil {
		parent = context.Background()
	}
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	if d.departureActive == nil {
		d.departureActive = map[string]struct{}{}
	}
	if d.departureCancels == nil {
		d.departureCancels = map[string]context.CancelFunc{}
	}
	if d.departureClosing {
		return nil, false
	}
	if _, exists := d.departureActive[runID]; exists {
		return nil, false
	}
	workerCtx, cancel := context.WithCancel(parent)
	d.departureActive[runID] = struct{}{}
	d.departureCancels[runID] = cancel
	d.departureWorkers.Add(1)
	return workerCtx, true
}

func (d *Daemon) releaseDepartureExecution(runID string) {
	d.departureMu.Lock()
	cancel := d.departureCancels[runID]
	delete(d.departureActive, runID)
	delete(d.departureCancels, runID)
	d.departureMu.Unlock()
	if cancel != nil {
		cancel()
	}
	d.departureWorkers.Done()
}

func (d *Daemon) activeDepartureExecutionIDs() []string {
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	active := make([]string, 0, len(d.departureActive))
	for runID := range d.departureActive {
		active = append(active, runID)
	}
	return active
}

func (d *Daemon) recordDepartureExecutionError(runID string, cause error) (DepartureRun, error) {
	if d == nil || d.store == nil || cause == nil {
		return DepartureRun{}, fmt.Errorf("record departure execution error requires a store, run, and cause")
	}
	message := safePacketText(cause.Error(), departureExecutionErrorLimit)
	if message == "" {
		message = "departure execution failed"
	}
	for attempt := 0; attempt < 4; attempt++ {
		run, err := d.store.FindDepartureRun(runID)
		if err != nil {
			return DepartureRun{}, err
		}
		if run == nil {
			return DepartureRun{}, tuskerError(errorNotFound, "departure run not found: "+runID)
		}
		next := *run
		next.ExecutionLastError = message
		next.ExecutionLastErrorAt = departureNow()
		if next.ExecutionErrorCount < departureExecutionErrorCap {
			next.ExecutionErrorCount++
		}
		changed, err := d.store.TransitionDepartureRun(next, run.StateRevision)
		if err != nil {
			return DepartureRun{}, err
		}
		if changed {
			next.StateRevision++
			next.UpdatedAt = departureNow()
			return next, nil
		}
	}
	return DepartureRun{}, fmt.Errorf("departure execution error CAS did not converge for %s", runID)
}

func (d *Daemon) clearDepartureExecutionError(runID string) error {
	if d == nil || d.store == nil {
		return nil
	}
	for attempt := 0; attempt < 4; attempt++ {
		run, err := d.store.FindDepartureRun(runID)
		if err != nil || run == nil {
			return err
		}
		if run.ExecutionLastError == "" && run.ExecutionLastErrorAt == "" {
			return nil
		}
		next := *run
		next.ExecutionLastError = ""
		next.ExecutionLastErrorAt = ""
		changed, err := d.store.TransitionDepartureRun(next, run.StateRevision)
		if err != nil {
			return err
		}
		if changed {
			return nil
		}
	}
	return fmt.Errorf("departure execution error clear CAS did not converge for %s", runID)
}

// executeDeparture is the deterministic synchronous phase runner used by the
// resident worker and focused tests. Each phase reloads durable truth, and a
// lost CAS simply follows the winning writer on the next loop.
func (d *Daemon) executeDeparture(ctx context.Context, project RegisteredProject, wf Workflow, runID string) error {
	if d == nil || d.store == nil {
		return fmt.Errorf("departure execution requires a runtime store")
	}
	for step := 0; step < 16; step++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		run, err := d.store.FindDepartureRun(runID)
		if err != nil {
			return err
		}
		if run == nil || departureTerminal(run.State) {
			return nil
		}
		if run.ProjectID != project.ProjectID {
			return fmt.Errorf("departure %s belongs to project %s, not %s", run.ID, run.ProjectID, project.ProjectID)
		}
		if run.PolicyID != departurePolicyID(wf.ScheduledPromotion.Effective) {
			return d.blockDepartureRun(*run, "departure recompute required: policy_drift")
		}

		switch run.State {
		case DepartureStateDue:
			window, parseErr := departureExecutionWindow(run.ScheduledWindow)
			if parseErr != nil {
				if blockErr := d.blockDepartureRun(*run, "departure refusal: invalid scheduled window: "+parseErr.Error()); blockErr != nil {
					return blockErr
				}
				continue
			}
			err = d.evaluateScheduledDepartureRun(project, wf, *run, window, time.Now().UTC())
		case DepartureStateEvaluating:
			err = d.executeDepartureEvaluation(project, wf, *run)
		case DepartureStateStaging:
			err = d.executeDepartureStaging(project, wf, *run)
		case DepartureStateGating:
			err = d.executeDepartureGating(ctx, project, wf, *run)
		case DepartureStatePromoted:
			err = d.executeDeparturePromoted(ctx, project, wf, *run)
		case DepartureStateRepairing, DepartureStateReleasing:
			// Durable red routing and release have separate owners.
			return nil
		default:
			err = d.blockDepartureRun(*run, "departure refusal: unsupported state "+string(run.State))
		}
		if errors.Is(err, errDepartureExecutionDeferred) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	return fmt.Errorf("departure %s did not converge after 16 durable phase reloads", runID)
}

func departureExecutionWindow(raw string) (time.Time, error) {
	window, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err == nil {
		return window, nil
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(raw))
}

func (d *Daemon) executeDepartureEvaluation(project RegisteredProject, wf Workflow, run DepartureRun) error {
	if hold, err := d.store.departureHold(project.ProjectID, false); err != nil {
		return err
	} else if hold != nil {
		return d.blockDepartureRun(run, departureHoldBlockReason(hold))
	}
	decision, err := d.planScheduledDeparture(project, wf)
	if err != nil {
		return err
	}
	switch decision.Disposition {
	case "ready", "already_gated":
		// Continue below.
	case "empty", "disabled":
		return d.blockDepartureRun(run, "departure recompute required: "+departureDecisionCode(decision))
	default:
		return d.blockDepartureRun(run, departureDecisionReason(decision))
	}
	if drift := departurePlanningDrift(run, decision); drift != "" {
		return d.blockDepartureRun(run, "departure recompute required: "+drift+"_drift")
	}
	policy := wf.ScheduledPromotion.Effective
	if !policy.Stage {
		if policy.Mode == scheduledPromotionShadow {
			_, err = d.transitionDepartureRun(run, func(next *DepartureRun) {
				next.State = DepartureStatePassed
				next.Candidate, next.Gate = decision.Candidate, decision.GateIntent
				next.BlockReason = ""
			})
			return err
		}
		return d.blockDepartureRun(run, "departure refusal: mode "+policy.Mode+" cannot stage cargo")
	}
	if policy.Promote && len(decision.Candidate.WaveIDs) != 1 {
		return d.blockDepartureRun(run, fmt.Sprintf("departure refusal: promote mode requires exactly one cargo wave; found %d", len(decision.Candidate.WaveIDs)))
	}
	if len(decision.Candidate.CargoTaskIDs) == 0 {
		return d.blockDepartureRun(run, "departure recompute required: cargo_drift")
	}
	_, err = d.transitionDepartureRun(run, func(next *DepartureRun) {
		next.State = DepartureStateStaging
		next.Candidate, next.Gate = decision.Candidate, decision.GateIntent
		next.BlockReason = ""
	})
	return err
}

func departureDecisionCode(decision DepartureDecision) string {
	if len(decision.Reasons) > 0 && strings.TrimSpace(decision.Reasons[0].Code) != "" {
		return strings.TrimSpace(decision.Reasons[0].Code)
	}
	return firstNonEmpty(strings.TrimSpace(decision.Disposition), "indeterminate")
}

func departurePlanningDrift(run DepartureRun, decision DepartureDecision) string {
	expected, actual := run.Candidate, decision.Candidate
	taskIDs := expected.CargoTaskIDs
	if len(taskIDs) == 0 {
		// Pre-cargo-identity rows still froze task/source maps. Preserve every
		// pinned fact before hydrating the richer identity on the next CAS.
		taskIDs = departurePinnedTaskIDs(expected)
	} else {
		if !sameDepartureStrings(expected.CargoTaskIDs, actual.CargoTaskIDs) {
			return "cargo"
		}
		if !sameDepartureStrings(expected.WaveIDs, actual.WaveIDs) {
			return "wave"
		}
	}
	for _, taskID := range taskIDs {
		if stateRev, pinned := expected.TaskStateRevisions[taskID]; pinned && stateRev != actual.TaskStateRevisions[taskID] {
			return "task"
		}
		if sourceSHA, pinned := expected.TaskSourceSHAs[taskID]; pinned && sourceSHA != actual.TaskSourceSHAs[taskID] {
			return "source"
		}
	}
	if expected.IntegrationBaseSHA != actual.IntegrationBaseSHA {
		return "integration"
	}
	if expected.ExpectedDefaultBranchSHA != actual.ExpectedDefaultBranchSHA {
		return "default_ref"
	}
	if run.Gate.Command != decision.GateIntent.Command ||
		run.Gate.Profile != decision.GateIntent.Profile ||
		run.Gate.Toolchain != decision.GateIntent.Toolchain ||
		run.Gate.TreeHash != decision.GateIntent.TreeHash {
		return "gate"
	}
	return ""
}

func departurePinnedTaskIDs(candidate DepartureCandidate) []string {
	taskIDs := make([]string, 0, len(candidate.TaskStateRevisions)+len(candidate.TaskSourceSHAs))
	for taskID := range candidate.TaskStateRevisions {
		taskIDs = append(taskIDs, taskID)
	}
	for taskID := range candidate.TaskSourceSHAs {
		taskIDs = append(taskIDs, taskID)
	}
	return uniqueDepartureStrings(taskIDs)
}

func (d *Daemon) executeDepartureStaging(project RegisteredProject, wf Workflow, run DepartureRun) error {
	if hold, err := d.store.departureHold(project.ProjectID, false); err != nil {
		return err
	} else if hold != nil {
		return d.blockDepartureRun(run, departureHoldBlockReason(hold))
	}
	policy := wf.ScheduledPromotion.Effective
	if !policy.Stage {
		return d.blockDepartureRun(run, "departure refusal: mode "+policy.Mode+" cannot stage cargo")
	}
	if policy.Promote && len(run.Candidate.WaveIDs) != 1 {
		return d.blockDepartureRun(run, fmt.Sprintf("departure refusal: promote mode requires exactly one cargo wave; found %d", len(run.Candidate.WaveIDs)))
	}
	if len(run.Candidate.CargoTaskIDs) == 0 {
		return d.blockDepartureRun(run, "departure refusal: durable cargo task identity is missing")
	}
	unstaged, err := departureUnstagedCargo(project, run.Candidate)
	if err != nil {
		return d.blockDepartureRun(run, "departure staging refusal: "+err.Error())
	}
	if len(unstaged) > 0 {
		if err := stageScheduledTasks(project.VaultRoot, unstaged, run.Candidate.TaskSourceSHAs, "daemon:departure:"+run.ID); err != nil {
			if departureExecutionTransient(err) {
				return err
			}
			return d.blockDepartureRun(run, "departure staging failed: "+err.Error())
		}
	}
	if !policy.Promote {
		_, err := d.transitionDepartureRun(run, func(next *DepartureRun) {
			next.State = DepartureStatePassed
			next.BlockReason = ""
		})
		return err
	}
	if hold, err := d.store.departureHold(project.ProjectID, false); err != nil {
		return err
	} else if hold != nil {
		return d.blockDepartureRun(run, departureHoldBlockReason(hold))
	}
	snapshot, err := departurePromotionSnapshotAfterStaging(project, wf, run.Candidate.WaveIDs[0])
	if err != nil {
		if departureExecutionTransient(err) {
			return err
		}
		return d.blockDepartureRun(run, "departure promotion candidate refusal: "+err.Error())
	}
	if drift := departurePostStagingDrift(run, snapshot); drift != "" {
		return d.blockDepartureRun(run, "departure recompute required: "+drift+"_drift")
	}
	if hold, err := d.store.departureHold(project.ProjectID, false); err != nil {
		return err
	} else if hold != nil {
		return d.blockDepartureRun(run, departureHoldBlockReason(hold))
	}
	_, err = d.transitionDepartureRun(run, func(next *DepartureRun) {
		next.State = DepartureStateGating
		next.Candidate, next.Gate = snapshot.Candidate, snapshot.Gate
		next.BlockReason = ""
	})
	return err
}

func departureUnstagedCargo(project RegisteredProject, candidate DepartureCandidate) ([]string, error) {
	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		return nil, err
	}
	var unstaged []string
	for _, taskID := range candidate.CargoTaskIDs {
		task, ok := idx.Tasks[taskID]
		if !ok {
			return nil, tuskerError(errorNotFound, "V7 task not found: "+taskID)
		}
		sourceSHA := strings.TrimSpace(candidate.TaskSourceSHAs[taskID])
		if sourceSHA == "" {
			return nil, tuskerError(errorInvalidTransition, "cargo task source is missing: "+taskID)
		}
		waveID := strings.TrimSpace(stringField(task.Data, "wave"))
		if waveID == "" {
			unstaged = append(unstaged, taskID)
			continue
		}
		wave, ok := idx.Waves[waveID]
		if !ok {
			unstaged = append(unstaged, taskID)
			continue
		}
		integrationBranch := v7WaveIntegrationBranch(wave)
		if !gitMergeBaseAncestor(project.RepoRoot, sourceSHA, integrationBranch) ||
			!departureExactLandingAudited(wave, taskID, integrationBranch, sourceSHA) {
			unstaged = append(unstaged, taskID)
		}
	}
	return unstaged, nil
}

func departureExactLandingAudited(wave Note, taskID, integrationBranch, sourceSHA string) bool {
	for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
		if stringField(row, "task") == taskID &&
			stringField(row, "target") == integrationBranch &&
			stringField(row, "gate_result") == "pass" &&
			stringField(row, "source_sha") == sourceSHA {
			return true
		}
	}
	return false
}

func departurePromotionSnapshotAfterStaging(project RegisteredProject, wf Workflow, waveID string) (scheduledPromotionCandidateSnapshot, error) {
	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		return scheduledPromotionCandidateSnapshot{}, err
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return scheduledPromotionCandidateSnapshot{}, tuskerError(errorNotFound, "V7 wave not found: "+waveID)
	}
	if err := syncV7WaveControlStateToIntegration(project.VaultRoot, wave, v7WaveIntegrationBranch(wave)); err != nil {
		return scheduledPromotionCandidateSnapshot{}, err
	}
	return scheduledPromotionSnapshot(project.VaultRoot, project.ProjectID, waveID, wf)
}

func departurePostStagingDrift(run DepartureRun, snapshot scheduledPromotionCandidateSnapshot) string {
	if len(run.Candidate.WaveIDs) != 1 || snapshot.WaveID != run.Candidate.WaveIDs[0] {
		return "wave"
	}
	if !sameDepartureStrings(run.Candidate.CargoTaskIDs, snapshot.Candidate.CargoTaskIDs) {
		return "cargo"
	}
	for _, taskID := range run.Candidate.CargoTaskIDs {
		if run.Candidate.TaskStateRevisions[taskID] != snapshot.Candidate.TaskStateRevisions[taskID] {
			return "task"
		}
		if run.Candidate.TaskSourceSHAs[taskID] != snapshot.Candidate.TaskSourceSHAs[taskID] {
			return "source"
		}
	}
	if run.Candidate.ExpectedDefaultBranchSHA != snapshot.Candidate.ExpectedDefaultBranchSHA {
		return "default_ref"
	}
	if run.Gate.Command != snapshot.Gate.Command ||
		run.Gate.Profile != snapshot.Gate.Profile ||
		run.Gate.Toolchain != snapshot.Gate.Toolchain {
		return "gate"
	}
	return ""
}

func (d *Daemon) executeDepartureGating(ctx context.Context, project RegisteredProject, wf Workflow, run DepartureRun) error {
	if hold, err := d.store.departureHold(project.ProjectID, false); err != nil {
		return err
	} else if hold != nil {
		return d.blockDepartureRun(run, departureHoldBlockReason(hold))
	}
	if !wf.ScheduledPromotion.Effective.Promote || len(run.Candidate.WaveIDs) != 1 {
		return d.blockDepartureRun(run, "departure refusal: scheduled promotion requires exactly one durable cargo wave")
	}
	if err := d.store.ClearResourceLeaseWaiter("gate:full", project.ProjectID); err != nil {
		return err
	}
	durable := run
	_, err := promoteScheduledWaveContext(ctx, project.VaultRoot, project.ProjectID, run.Candidate.WaveIDs[0], wf, d.store, &durable, "daemon:departure:"+run.ID)
	if err == nil {
		if run.Promotion.CommittedRef != "" && run.Promotion.CommittedSHA != "" {
			_, err = d.transitionDepartureRun(run, func(next *DepartureRun) {
				next.State = DepartureStatePromoted
				next.BlockReason = ""
			})
		}
		return nil
	}
	var typed *TuskerError
	if errors.As(err, &typed) && typed.Code == resourceLeaseRefusal {
		if waitErr := d.store.RegisterResourceLeaseWaiter("gate:full", project.ProjectID); waitErr != nil {
			return waitErr
		}
		return errDepartureExecutionDeferred
	}
	latest, loadErr := d.store.FindDepartureRun(run.ID)
	if loadErr != nil {
		return loadErr
	}
	if latest != nil && (latest.State == DepartureStateRepairing || latest.State == DepartureStateFailed) {
		return nil
	}
	if errors.As(err, &typed) && typed.Code == departureHeldError {
		hold, holdErr := d.store.departureHold(project.ProjectID, false)
		if holdErr != nil {
			return holdErr
		}
		reason := err.Error()
		if hold != nil {
			reason = departureHoldBlockReason(hold)
		}
		return d.blockDepartureRun(run, reason)
	}
	if departureExecutionTransient(err) {
		return err
	}
	if errors.As(err, &typed) {
		return d.blockDepartureRun(run, err.Error())
	}
	return err
}

func (d *Daemon) executeDeparturePromoted(ctx context.Context, project RegisteredProject, wf Workflow, run DepartureRun) error {
	if wf.ScheduledPromotion.Effective.Release {
		return errDepartureExecutionDeferred
	}
	if len(run.Candidate.WaveIDs) != 1 {
		return d.blockDepartureRun(run, "departure refusal: promoted row is missing its durable cargo wave")
	}
	durable := run
	if _, err := promoteScheduledWaveContext(ctx, project.VaultRoot, project.ProjectID, run.Candidate.WaveIDs[0], wf, d.store, &durable, "daemon:departure:"+run.ID); err != nil {
		if departureExecutionContextCanceled(err) {
			return err
		}
		if departureExecutionTransient(err) {
			return err
		}
		return d.blockDepartureRun(run, err.Error())
	}
	_, err := d.transitionDepartureRun(run, func(next *DepartureRun) {
		next.State = DepartureStatePassed
		next.BlockReason = ""
	})
	return err
}

func (d *Daemon) transitionDepartureRun(run DepartureRun, mutate func(*DepartureRun)) (bool, error) {
	next := run
	mutate(&next)
	return d.store.TransitionDepartureRun(next, run.StateRevision)
}

func (d *Daemon) blockDepartureRun(run DepartureRun, reason string) error {
	_, err := d.transitionDepartureRun(run, func(next *DepartureRun) {
		next.State = DepartureStateBlocked
		next.BlockReason = strings.TrimSpace(reason)
	})
	return err
}

func departureExecutionTransient(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "landing lane is already running")
}

func departureExecutionContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func sameDepartureStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func departureHoldError(hold *DepartureHold) error {
	if hold == nil {
		return tuskerError(departureHeldError, "departure held")
	}
	return tuskerError(departureHeldError, departureHoldBlockReason(hold), withHint(hold.ResumeAction))
}
