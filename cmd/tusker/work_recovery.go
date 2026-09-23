package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// contextRecoveryPreflight reads canonical facts without changing them.
func contextRecoveryPreflight(store *RuntimeStore, wave Note, run RunStatus) (*RunAttempt, error, string) {
	if !run.Terminal || projectedAttemptOutcome(run.AttemptOutcome, run.LastError) != AttemptOutcomeUnknown {
		return nil, nil, "context recovery requires an unknown terminal outcome"
	}
	if runProcessGroupAlive(run) || isDispatchingLeaseState(run.LeaseState) {
		return nil, nil, "a live owner still holds this task; settle it before context recovery"
	}
	if strings.TrimSpace(stringField(wave.Data, "id")) == "" ||
		!strings.EqualFold(strings.TrimSpace(stringField(wave.Data, "authorization")), "armed") ||
		strings.TrimSpace(stringField(wave.Data, "authorization_fingerprint")) == "" ||
		strings.TrimSpace(stringField(wave.Data, "authorized_at")) == "" {
		return nil, nil, "the existing wave execution window is not current"
	}
	if vault := v7VaultPathForLandingAudit(wave); vault != "" {
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			return nil, nil, "cannot verify wave material before context recovery: " + loadErr.Error()
		}
		if auth := waveAuthorizationProjection(vault, idx, wave); boolFromAny(auth["stale"]) {
			return nil, nil, "wave material changed since authorization; re-authorize before context recovery"
		}
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return nil, err, ""
	}
	var parent RunAttempt
	for _, attempt := range attempts {
		if projectedAttemptOutcome(attempt.Outcome, attempt.LastError) == AttemptOutcomeUnknown {
			parent = attempt
			break
		}
	}
	if parent.AttemptID == "" {
		return nil, nil, "the uncertain parent attempt cannot be identified"
	}
	for _, attempt := range attempts {
		if attempt.ParentAttemptID == parent.AttemptID && attempt.ChildType == "recovery" {
			return nil, nil, "one recovery attempt already exists; human review is required"
		}
	}

	return &parent, nil, ""
}

// queueOutcomeUnknownContextRecovery records an explicit context recovery for
// an uncertain attempt. It keeps the original attempt/session rows for audit,
// but clears the run's session reference before queuing so the next attempt
// must create a different native session. The existing attempt budget is
// deliberately retained; this action is a recovery decision, not a redrive
// that silently opens a new budget window.
func queueOutcomeUnknownContextRecovery(store *RuntimeStore, task Note, wave Note, run RunStatus, actor string, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "recover_context", TaskID: stringField(task.Data, "id"), Lane: runLaneExecute}
	if store == nil {
		return result, tuskerError(errorInvalidArg, "context recovery requires a runtime store")
	}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	if current == nil {
		result.Refused, result.Reason = true, "run not found"
		return result, nil
	}
	run = *current
	parent, preflightErr, reason := contextRecoveryPreflight(store, wave, run)
	if preflightErr != nil {
		return result, preflightErr
	}
	if reason != "" {
		result.Refused, result.Reason = true, reason
		return result, nil
	}

	previous := run
	oldSession := strings.TrimSpace(run.SessionRef)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	run.LeaseState = string(LeaseStateRetryQueued)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.NextRetryAt = now.Format(time.RFC3339)
	run.LastError = outcomeUnknownContextRecoveryReasonPrefix + parent.AttemptID
	run.LastEventAt = now.Format(time.RFC3339)
	run.UpdatedAt = now.Format(time.RFC3339)
	run.Terminal = false
	run.SessionRef = ""
	clearActiveExecution(&run)
	priorIntent, err := loadRunSessionControlIntent(store, run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	recoveryIntent := runSessionControlIntent{
		Action: runSessionControlContextRecovery, State: runSessionControlQueued,
		ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
		Actor: actor, Reason: "explicit context recovery", LeaseGeneration: run.LeaseGeneration,
		AttemptID: parent.AttemptID, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	saved, err := saveRunSessionControlIntentIfPrior(store, recoveryIntent, priorIntent)
	if err != nil {
		return result, err
	}
	if !saved {
		result.Refused, result.Reason = true, "run control intent changed while context recovery was being queued; reload and retry"
		return result, nil
	}
	updated, err := store.UpsertRunIfSnapshot(previous, run)
	if err != nil {
		_ = clearFailedContextRecoveryIntent(store, recoveryIntent, priorIntent)
		return result, err
	}
	if !updated {
		_ = clearFailedContextRecoveryIntent(store, recoveryIntent, priorIntent)
		result.Refused, result.Reason = true, "run changed while context recovery was being queued; reload and retry"
		return result, nil
	}

	queued, err := store.QueueRunDirective(RunDirective{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Actor: actor,
		Reason:    outcomeUnknownContextRecoveryReasonPrefix + parent.AttemptID,
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(directRunDirectiveTTL).Format(time.RFC3339Nano),
		WaveID: stringField(wave.Data, "id"), AuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), WaveAuthorizedAt: stringField(wave.Data, "authorized_at"),
	})
	if err != nil {
		if matches, matchErr := store.RunMatchesSnapshot(run); matchErr == nil && matches {
			if restored, restoreErr := store.UpsertRunIfSnapshot(run, previous); restoreErr == nil && restored {
				_ = clearFailedContextRecoveryIntent(store, recoveryIntent, priorIntent)
			}
		}
		return result, err
	}
	if !queued {
		if directive, directiveErr := store.RunDirective(run.ProjectID, run.RecordID); directiveErr == nil && directive != nil && (directive.State == "queued" || directive.State == "consumed") {
			result.OK, result.Reason = true, "context recovery is already queued"
			return result, nil
		}
		result.Refused, result.Reason = true, "run changed before context recovery could be admitted; reload and retry"
		return result, nil
	}
	if oldSession != "" {
		_ = store.MarkSessionState(run.ProjectID, oldSession, sessionStateForLeaseState(LeaseStateReleased), now.Format(time.RFC3339), "explicit context recovery selected", false)
	}
	_, _ = store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner,
		WorkRevision: run.WorkRevision, AttemptID: parent.AttemptID, ParentAttemptID: parent.AttemptID,
		ParentSessionRef: oldSession, Kind: string(SupervisorDecisionForkThread),
		Reason:        "explicit context recovery from uncertain attempt; completed effects stay unreplayed",
		WorkspacePath: run.WorkspacePath, LeaseState: run.LeaseState, ContextSignal: "explicit_context_recovery",
		CreatedAt: now.Format(time.RFC3339),
	})
	result.OK, result.Admitted, result.Reason = true, true, "explicit context recovery queued; the next attempt will use a fresh native session"
	return result, nil
}

func clearFailedContextRecoveryIntent(store *RuntimeStore, recovery runSessionControlIntent, prior *runSessionControlIntent) error {
	recovery.Schema = runSessionControlSchema
	raw, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	key := runSessionControlSettingKey(recovery.ProjectID, recovery.RecordID)
	if prior == nil {
		return store.DeleteSettingIfValue(key, string(raw))
	}
	previous, err := json.Marshal(prior)
	if err != nil {
		return err
	}
	_, err = store.SetSettingIfValue(key, string(raw), string(previous))
	return err
}

// nativeContinuationPreflight is shared by the read model and the mutating path.
func nativeContinuationPreflight(store *RuntimeStore, project RegisteredProject, wave Note, run RunStatus) (*RunnerSession, error, string) {
	if run.Lane == runLaneReview {
		return nil, nil, "native continuation is unavailable for review runs"
	}
	if runProcessGroupAlive(run) || isDispatchingLeaseState(run.LeaseState) {
		return nil, nil, "a live owner still holds this task; reconnect or stop it before continuing"
	}
	if projectedAttemptOutcome(run.AttemptOutcome, run.LastError) == AttemptOutcomeSucceeded {
		return nil, nil, "a completed attempt cannot be continued; recover only unresolved work"
	}
	if strings.TrimSpace(run.SessionRef) == "" {
		return nil, nil, "native continuation requires a saved session reference"
	}
	capability := nativeResumeRunnerCapabilities(RunnerName(strings.TrimSpace(run.Runner)))
	if !capability.ResumeSession {
		return nil, nil, "runner does not support native session continuation; request explicit context recovery"
	}
	session, err := store.FindSessionByRef(run.ProjectID, run.SessionRef)
	if err != nil {
		return nil, err, ""
	}
	if reason := incompatibleResumeSessionReason(project, run, session); reason != "" {
		return nil, nil, reason
	}
	if strings.TrimSpace(session.CurrentItemID) != "" && strings.TrimSpace(run.ItemID) != "" && session.CurrentItemID != run.ItemID {
		return nil, nil, "stored session item_id does not match run item_id"
	}
	// Dispatch will validate the same fingerprint after it allocates the child
	// attempt. Clear only the future active-attempt slot for this preflight so a
	// session whose last attempt is still the current terminal attempt can be
	// checked against its retained prompt rather than rejected as self-equal.
	probe := run
	probe.ActiveAttemptID = ""
	if reason := (&Daemon{store: store}).resumeContextFingerprintMismatch(probe, session); reason != "" {
		return nil, nil, reason
	}
	if strings.TrimSpace(stringField(wave.Data, "id")) == "" ||
		!strings.EqualFold(strings.TrimSpace(stringField(wave.Data, "authorization")), "armed") ||
		strings.TrimSpace(stringField(wave.Data, "authorization_fingerprint")) == "" ||
		strings.TrimSpace(stringField(wave.Data, "authorized_at")) == "" {
		return nil, nil, "the existing wave execution window is not current"
	}
	if vault := v7VaultPathForLandingAudit(wave); vault != "" {
		idx, loadErr := loadV7Index(vault)
		if loadErr != nil {
			return nil, nil, "cannot verify wave material before continuation: " + loadErr.Error()
		}
		if auth := waveAuthorizationProjection(vault, idx, wave); boolFromAny(auth["stale"]) {
			return nil, nil, "wave material changed since authorization; re-authorize before continuation"
		}
	}
	if directive, directiveErr := store.RunDirective(run.ProjectID, run.RecordID); directiveErr != nil {
		return nil, directiveErr, ""
	} else if directive != nil && directive.State == "queued" {
		return nil, nil, "native continuation is already queued"
	}
	return session, nil, ""
}

// queueNativeSessionContinuation queues one new attempt while retaining the
// exact native session reference. The daemon remains the only process that
// consumes the directive and calls Runner.Resume; this helper only admits the
// operator decision after the same identity and prompt-context fences used by
// dispatch have passed.
func queueNativeSessionContinuation(store *RuntimeStore, project RegisteredProject, task Note, wave Note, run RunStatus, actor string, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "continue", TaskID: stringField(task.Data, "id"), Lane: runLaneExecute}
	if store == nil {
		return result, tuskerError(errorInvalidArg, "native continuation requires a runtime store")
	}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	if current == nil {
		result.Refused, result.Reason = true, "run not found"
		return result, nil
	}
	run = *current
	session, preflightErr, reason := nativeContinuationPreflight(store, project, wave, run)
	if preflightErr != nil {
		return result, preflightErr
	}
	if reason != "" {
		if reason == "native continuation is already queued" {
			result.OK, result.Reason = true, reason
		} else {
			result.Refused, result.Reason = true, reason
		}
		return result, nil
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	previous := run
	run.LeaseState = string(LeaseStateRetryQueued)
	run.AttemptOutcome = string(AttemptOutcomeNone)
	run.NextRetryAt = now.Format(time.RFC3339)
	run.LastError = "native continuation requested by " + firstNonEmpty(strings.TrimSpace(actor), defaultActorName()) + ": " + session.SessionRef
	run.LastEventAt = now.Format(time.RFC3339)
	run.UpdatedAt = now.Format(time.RFC3339)
	run.Terminal = false
	clearActiveExecution(&run)
	updated, err := store.UpsertRunIfSnapshot(previous, run)
	if err != nil {
		return result, err
	}
	if !updated {
		result.Refused, result.Reason = true, "run changed while native continuation was being queued; reload and retry"
		return result, nil
	}
	queued, err := store.QueueRunDirective(RunDirective{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Actor: actor,
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(directRunDirectiveTTL).Format(time.RFC3339Nano),
		WaveID: stringField(wave.Data, "id"), AuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), WaveAuthorizedAt: stringField(wave.Data, "authorized_at"),
	})
	if err != nil {
		if matches, matchErr := store.RunMatchesSnapshot(run); matchErr == nil && matches {
			_, _ = store.UpsertRunIfSnapshot(run, previous)
		}
		return result, err
	}
	if !queued {
		if directive, directiveErr := store.RunDirective(run.ProjectID, run.RecordID); directiveErr == nil && directive != nil && (directive.State == "queued" || directive.State == "consumed") {
			result.OK, result.Reason = true, "native continuation is already queued"
			return result, nil
		}
		result.Refused, result.Reason = true, "run changed before native continuation could be admitted; reload and retry"
		return result, nil
	}
	_, _ = store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner,
		WorkRevision: run.WorkRevision, AttemptID: firstNonEmpty(run.ActiveAttemptID, session.LastAttemptID),
		ParentAttemptID: session.LastAttemptID, SessionRef: session.SessionRef, ParentSessionRef: session.SessionRef,
		Kind: string(SupervisorDecisionContinueThread), Reason: "operator requested native continuation of the compatible saved session",
		WorkspacePath: run.WorkspacePath, LeaseState: run.LeaseState, ContextSignal: "native_session_continuation",
		CreatedAt: now.Format(time.RFC3339),
	})
	result.OK, result.Admitted, result.Reason = true, true, "native continuation queued; the daemon will resume the saved session"
	return result, nil
}

func nativeResumeRunnerCapabilities(name RunnerName) RunnerCapabilities {
	switch name {
	case RunnerCodexExec:
		return (&CodexExecRunner{}).Capabilities()
	case RunnerClaude:
		return (&ClaudeRunner{}).Capabilities()
	case RunnerMuse:
		return (&MuseRunner{}).Capabilities()
	case RunnerDevin:
		return (&ACPRunner{runner: RunnerDevin}).Capabilities()
	default:
		return RunnerCapabilities{}
	}
}

// recoverVerificationChecks is the one recovery transition shared by Serve
// and the CLI. It verifies the submitted execute workspace, then reopens and
// queues only the independent review lane; attempts stay in the runtime log.
func recoverVerificationChecks(vault string, store *RuntimeStore, projectID, taskID, actor string, maxAttempts int, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "rerun_checks", TaskID: taskID}
	task, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	run, err := store.FindRunScoped(projectID, trackerRecordID(task))
	if err != nil {
		return result, err
	}
	if run == nil {
		result.Refused, result.Reason = true, "run not found"
		return result, nil
	}
	workspace, err := recoveryCommandVerificationWorkspace(store, vault, task, *run)
	if err != nil {
		result.Refused, result.Lane, result.Reason = true, "verify", err.Error()
		return result, nil
	}
	waveID := strings.TrimSpace(stringField(task.Data, "wave"))
	wave, err := resolveV7Note(vault, waveID, "wave")
	if err != nil {
		return result, err
	}
	if reason := reviewRecoveryOperationalBlocker(wave, *run, maxAttempts); reason != "" {
		result.Refused, result.Lane, result.Reason = true, runLaneReview, reason
		return result, nil
	}
	// Proof is recorded before the task reopens for review: a failed command
	// must leave the member at its prior status so the projection keeps
	// offering rerun_checks instead of a misleading awaiting-review dead end.
	if v7VerificationReceiptInvalidationForWorkspace(vault, task, workspace) != nil {
		_, report, failures, err := executeV7CommandVerificationRowsInWorkspace(vault, task, Args{"rerun-invalid": "true"}, actor, true, workspace)
		if err != nil {
			result.Refused, result.Lane, result.Reason = true, "verify", err.Error()
			return result, nil
		}
		if len(failures) > 0 || report.Status != "satisfied" {
			reason := "verification did not pass"
			if len(failures) > 0 {
				reason = firstNonEmpty(failures[0].Message, reason)
			}
			result.Refused, result.Lane, result.Reason = true, "verify", reason
			return result, nil
		}
		task, err = resolveV7Note(vault, taskID, "task")
		if err != nil {
			return result, err
		}
	}
	if err := statusV7CmdAsInternalActor(Args{
		"vault": vault, "quiet": "true", "id": taskID, "status": "review",
		"task-rev": stringField(task.Data, "state_rev"),
		"reason":   "verification recovery passed; independent review required",
	}, "tusker:recovery"); err != nil {
		return result, err
	}
	task, err = resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	run, err = store.FindRunScoped(projectID, trackerRecordID(task))
	if err != nil || run == nil {
		if err == nil {
			err = tuskerError(errorNotFound, "project-scoped run not found after verification")
		}
		return result, err
	}
	review, err := queueReviewRecovery(store, task, wave, *run, actor, maxAttempts, now, true)
	if err != nil {
		return result, err
	}
	if !review.OK {
		return result, tuskerError(errorInvalidTransition, "checks passed, but independent review could not be queued: "+review.Reason)
	}
	result.OK, result.Admitted, result.Lane = true, review.Admitted, runLaneReview
	result.Reason = "current verification commands passed and were recorded; independent review queued"
	return result, nil
}

// adoptCompletedImplementationSource is the only truthful implementation
// provenance an adoption may record: the work already exists in the checkout
// and the adopter never claims to know which provider, conversation, or
// attempt produced it.
const adoptCompletedImplementationSource = "external/unknown"

// taskAdoptedExternalImplementation reports whether the task carries the
// durable adoption marker written by adopt_completed.
func taskAdoptedExternalImplementation(task Note) bool {
	return strings.EqualFold(strings.TrimSpace(stringField(task.Data, "implementation_source")), adoptCompletedImplementationSource) &&
		strings.TrimSpace(stringField(task.Data, "adopted_at")) != ""
}

// adoptCompletedSnapshot pins every dimension the adoption is decided against;
// any drift before commit refuses atomically.
type adoptCompletedSnapshot struct {
	repoRoot     string
	scope        []string
	generated    []string
	contract     string
	workRevision int
	source       string
	stateRev     string
	material     string
	dependencies string
}

var adoptCompletedBeforeCommitHook func()
var adoptCompletedBeforeWriteHook func()

func adoptCompletedDependencyFingerprint(task Note, idx v7Index) string {
	dependencies := make([]v7CloseDependencyAuthority, 0, len(v7TaskDependencyEdges(task, idx)))
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		dependency := idx.Tasks[edge.ID]
		dependencies = append(dependencies, v7CloseDependencyAuthority{ID: edge.ID, Hardness: edge.Hardness, Status: stringField(dependency.Data, "status"), StateRev: stringField(dependency.Data, "state_rev")})
	}
	raw, _ := json.Marshal(dependencies)
	return v7Fingerprint(raw)
}

func (snap adoptCompletedSnapshot) verifyCurrent(vault, taskID string) error {
	current, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return err
	}
	data, body, err := parseFrontmatterMustRead(current.AbsolutePath)
	if err != nil {
		return err
	}
	if stringField(data, "id") != taskID {
		return tuskerError(errorInvalidTransition, "adopt_completed task identity changed during verification")
	}
	if rev := stringField(data, "state_rev"); rev != snap.stateRev || !v7StateRevMatches(data, body, rev) {
		return tuskerError("CAS_CONFLICT", taskID+": adopt_completed task state changed during verification")
	}
	if directWaveTaskContractFingerprint(data, body) != snap.contract {
		return tuskerError(errorInvalidTransition, taskID+": adopt_completed task contract changed during verification")
	}
	if intField(data, "work_revision") != snap.workRevision {
		return tuskerError(errorInvalidTransition, taskID+": adopt_completed work revision changed during verification")
	}
	if firstNonEmpty(stringField(data, "source_sha"), stringField(data, "source_commit")) != snap.source {
		return tuskerError(errorInvalidTransition, taskID+": adopt_completed source identity changed during verification")
	}
	material, err := workspaceTreeStateHashForPaths(snap.repoRoot, snap.scope, snap.generated)
	if err != nil {
		return err
	}
	if material != snap.material {
		return tuskerError(errorEvidenceGate, taskID+": adopt_completed scoped material changed during verification")
	}
	return nil
}

// adoptCompletedOwnershipFence rechecks runtime ownership at final commit
// under the owned-path claim exclusion shared with run admission. It reloads
// the candidate task and the overlapping run holders, refuses on a live
// same-task run or an overlapping live holder, and returns the exclusion held
// so the caller retains it through the canonical transition. A refusal
// releases the exclusion before returning. A disjoint holder never refuses.
func adoptCompletedOwnershipFence(vault string, store *RuntimeStore, projectID, taskID string) (func(), string, error) {
	noop := func() {}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return noop, "", err
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return noop, "", err
	}
	fresh, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return noop, "", err
	}
	claimNotes := orchestrationOwnedPathNotes(idx.Tasks, wf.Data)
	candidate := fresh
	if overlaid, ok := claimNotes[taskID]; ok {
		candidate = overlaid
	}
	service := newRunOwnershipService(store).withOwnedPathContext(vault, candidate, claimNotes)
	unlock, err := service.lockOwnedPathClaims()
	if err != nil {
		return noop, "", err
	}
	if run, err := store.FindRunScoped(projectID, trackerRecordID(fresh)); err != nil {
		unlock()
		return noop, "", err
	} else if run != nil && (isDispatchingLeaseState(run.LeaseState) || runProcessGroupAlive(*run)) {
		reason := "a live run holds " + taskID + "; release or interrupt it before adopt_completed"
		unlock()
		return noop, reason, nil
	}
	runs, err := store.ListRuns()
	if err != nil {
		unlock()
		return noop, "", err
	}
	if conflict, found := ownedPathConflict(candidate, claimNotes, runs, time.Now().UTC()); found {
		reason := fmt.Sprintf("an active run holds %s for %s (lease age %s; liveness %s)", conflict["holder_path"], conflict["task_id"], conflict["lease_age"], conflict["liveness"])
		unlock()
		return noop, reason, nil
	}
	return unlock, "", nil
}

// adoptCompletedWork reconciles work already present in the registered
// checkout without claiming implementation provenance: it reruns the current
// verification contract bound to the canonical repo root, then closes the task
// or queues the independent review lane per the authored proof contract.
func adoptCompletedWork(vault string, store *RuntimeStore, projectID, taskID, actor string, maxAttempts int, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "adopt_completed", TaskID: taskID}
	refuse := func(reason string) (serveRecoveryResult, error) {
		result.Refused, result.Reason = true, reason
		return result, nil
	}
	task, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	status := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	// Idempotent replay: a prior completed adoption reports its recorded
	// outcome without writing duplicate receipts, markers, or directives.
	if taskAdoptedExternalImplementation(task) {
		switch status {
		case "done":
			result.OK, result.Reason = true, "Checks passed. The task is complete."
			return result, nil
		case "review":
			result.OK, result.Lane, result.Reason = true, runLaneReview, "Checks passed. Independent review is next."
			return result, nil
		}
	}
	switch status {
	case "done", "cancelled", "superseded":
		return refuse("task " + taskID + " is already " + status)
	case "review":
		return refuse("task " + taskID + " is already in review without adopt_completed provenance")
	}
	if reason := directWaveTaskContractStaleReason(task); reason != "" {
		return refuse(reason)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		return result, err
	}
	// Authorization: humans and operators may adopt directly; every other
	// actor needs a current durable authorization bound to this material.
	if reason := adoptCompletedAuthorizationRefusal(vault, store, projectID, task, idx, actor, now); reason != "" {
		return refuse(reason)
	}
	// Workspace safety: the registered checkout must be the canonical repo
	// root, the declared material scope must be non-empty and readable, and
	// no live run may hold the task or conflict on its owned paths.
	repoRoot, err := canonicalV7VerificationWorkspaceRoot(v7RepoRoot(vault))
	if err != nil {
		return refuse(err.Error())
	}
	scope, err := canonicalTaskMaterialScope(vault, task)
	if err != nil {
		return refuse("declared material scope is unsafe: " + err.Error())
	}
	generated, err := taskGeneratedOutputScope(task)
	if err != nil {
		return refuse("declared generated output scope is unsafe: " + err.Error())
	}
	if len(scope) == 0 && len(generated) == 0 {
		return refuse("adopt_completed requires a non-empty declared material scope (owned_paths, generated_outputs, or spec_refs)")
	}
	for _, rel := range append(append([]string{}, scope...), generated...) {
		if _, statErr := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(rel))); statErr != nil {
			return refuse("declared material scope path is unreadable or missing: " + rel)
		}
	}
	recordID := trackerRecordID(task)
	run, err := store.FindRunScoped(projectID, recordID)
	if err != nil {
		return result, err
	}
	if run != nil && (isDispatchingLeaseState(run.LeaseState) || runProcessGroupAlive(*run)) {
		return refuse("a live run holds " + taskID + "; release or interrupt it before adopt_completed")
	}
	runs, err := store.ListRuns()
	if err != nil {
		return result, err
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return result, err
	}
	claimNotes := orchestrationOwnedPathNotes(idx.Tasks, wf.Data)
	if conflict, found := ownedPathConflict(task, claimNotes, runs, now); found {
		return refuse(fmt.Sprintf("an active run holds %s for %s (lease age %s; liveness %s)", conflict["holder_path"], conflict["task_id"], conflict["lease_age"], conflict["liveness"]))
	}
	material, err := workspaceTreeStateHashForPaths(repoRoot, scope, generated)
	if err != nil {
		return refuse("cannot read scoped implementation material: " + err.Error())
	}
	snapshot := adoptCompletedSnapshot{
		repoRoot: repoRoot, scope: scope, generated: generated,
		contract:     directWaveTaskContractFingerprint(task.Data, task.Body),
		workRevision: intField(task.Data, "work_revision"),
		source:       firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")),
		stateRev:     stringField(task.Data, "state_rev"),
		material:     material,
		dependencies: adoptCompletedDependencyFingerprint(task, idx),
	}
	if snapshot.stateRev == "" || !v7StateRevMatches(task.Data, task.Body, snapshot.stateRev) {
		return refuse(taskID + ": task state_rev does not match current bytes; reconcile the record before adopt_completed")
	}
	workspace := &v7VerificationWorkspace{
		Path:                repoRoot,
		MaterialFingerprint: snapshot.material,
		Verify:              func() error { return snapshot.verifyCurrent(vault, taskID) },
	}
	fresh, report, failures, err := executeV7CommandVerificationRowsInWorkspace(vault, task, Args{"rerun-invalid": "true"}, actor, true, workspace)
	if err != nil {
		return result, err
	}
	if len(failures) > 0 {
		return refuse("verification did not pass: " + firstNonEmpty(failures[0].Message, "command failed"))
	}
	task = fresh
	snapshot.stateRev = stringField(task.Data, "state_rev")
	idx, err = loadV7Index(vault)
	if err != nil {
		return result, err
	}
	// Proof gate: machine-class requirements must be satisfied by the fresh
	// receipts; human/external-owned gaps and open blocking gates refuse.
	needsReview := false
	for _, required := range v7TaskProofRequired(task) {
		if required == "independent_review" {
			needsReview = true
			continue
		}
		if owner := classifyProofRequirement(required, task, idx); owner == "human" || owner == "external" || owner == "reviewer" {
			return refuse("adopt_completed cannot satisfy proof requirement " + required + ": it is owned by " + owner)
		}
	}
	if len(report.OpenGates) > 0 {
		return refuse("blocked by open gate(s): " + strings.Join(report.OpenGates, ", "))
	}
	if len(report.ExternalBlockers) > 0 {
		return refuse("verification rows are blocked externally: " + strings.Join(report.ExternalBlockers, "; "))
	}
	if len(report.MachineMissing) > 0 {
		return refuse("machine proof is incomplete: " + strings.Join(report.MachineMissing, ", "))
	}
	if len(report.HumanMissing) > 0 || len(report.ExternalMissing) > 0 {
		return refuse("proof requires human/external evidence: " + strings.Join(append(append([]string{}, report.HumanMissing...), report.ExternalMissing...), ", "))
	}
	for _, gap := range report.ReviewerMissing {
		if gap != "proof_required:independent_review" {
			return refuse("proof requires reviewer-owned evidence: " + gap)
		}
	}
	if !needsReview && report.Status != "satisfied" {
		return refuse("proof report is " + fallback(report.Status, "incomplete") + " for the current contract")
	}
	proofSnapshot, gateSnapshot, err := reviewObjectiveSnapshots(vault, task)
	if err != nil {
		return result, err
	}
	if adoptCompletedBeforeCommitHook != nil {
		adoptCompletedBeforeCommitHook()
	}
	materialLock, err := acquireV7MaterialEpochLock(vault)
	if err != nil {
		return result, err
	}
	defer func() { _ = materialLock.Close() }()
	// Final ownership fence: ownership arriving during verification must
	// refuse. Hold the same owned-path claim exclusion used by run
	// admission and recheck fresh holders through the canonical transition
	// below. Lock ordering is material-epoch before owned-path-claims,
	// matching daemon admission, so the two interleavings terminate
	// instead of deadlocking. The exclusion is only taken here, never
	// across verification commands.
	ownershipUnlock, ownershipRefusal, err := adoptCompletedOwnershipFence(vault, store, projectID, taskID)
	if err != nil {
		return result, err
	}
	defer ownershipUnlock()
	if ownershipRefusal != "" {
		return refuse(ownershipRefusal)
	}
	if err := snapshot.verifyCurrent(vault, taskID); err != nil {
		return result, err
	}
	current, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	currentProof, currentGates, err := reviewObjectiveSnapshots(vault, current)
	if err != nil {
		return result, err
	}
	if currentProof != proofSnapshot || currentGates != gateSnapshot {
		return result, tuskerError(errorInvalidTransition, taskID+": proof, dependency, or gate state changed before adopt_completed commit")
	}
	currentIndex, err := loadV7Index(vault)
	if err != nil {
		return result, err
	}
	if adoptCompletedDependencyFingerprint(current, currentIndex) != snapshot.dependencies {
		return result, tuskerError(errorInvalidTransition, taskID+": dependency state changed during adopt_completed verification")
	}
	task = current
	if !needsReview {
		idx, err = loadV7Index(vault)
		if err != nil {
			return result, err
		}
		preflight, err := v7ClosePreflight(vault, task, idx, v7ClosePreflightRequest{
			Args:              Args{"vault": vault, "quiet": "true"},
			Actor:             actor,
			Action:            "close",
			ExpectedTaskID:    taskID,
			ExpectedStateRev:  stringField(task.Data, "state_rev"),
			ObjectiveAdoption: true,
		})
		if err != nil {
			return result, err
		}
		data, body := preflight.Task.Data, preflight.Task.Body
		baseRev := stringField(data, "state_rev")
		prev := stringField(data, "status")
		nowStr := now.UTC().Format(time.RFC3339)
		prior := cloneNoteData(data)
		applyAdoptionMarker(data, actor, now, snapshot.material)
		applyV7TaskCloseProjection(data, actor, nowStr, nil)
		if err := commitAdoptionTransition(vault, task.AbsolutePath, taskID, data, prior, body, baseRev, snapshot); err != nil {
			return result, err
		}
		_ = materialLock.Close()
		if err := emitV7TaskClosedEvent(vault, taskID, actor, nowStr, prev, "adopt_completed: checks passed; external implementation adopted", nil); err != nil {
			return result, err
		}
		if _, err := retireCanonicalRuntimeRowsForTask(vault, taskID, "done", actor, "adopt_completed"); err != nil {
			return result, err
		}
		affected, err := v7TaskIDsForTaskControl(vault, taskID)
		if err != nil {
			return result, err
		}
		if _, err := reconcileV7ControlProjections(vault, affected, actor, "task:"+taskID); err != nil {
			return result, err
		}
		result.OK, result.Admitted, result.Reason = true, true, "Checks passed. The task is complete."
		return result, nil
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		return result, err
	}
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "status") == "open" && boolField(gate.Data, "blocking") && v7GateTouchesTask(gate, taskID) {
			return result, tuskerError(errorInvalidTransition, taskID+": adopt_completed blocked by open gate "+stringField(gate.Data, "id"))
		}
	}
	if dep, blocked := v7UnclosedDependency(task, idx); blocked {
		return result, tuskerError(errorInvalidTransition, taskID+": adopt_completed blocked by unfinished dependency "+dep.ID)
	}
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		return result, err
	}
	baseRev := stringField(data, "state_rev")
	prior := cloneNoteData(data)
	applyAdoptionMarker(data, actor, now, snapshot.material)
	data["status"], data["updated_at"], data["updated_by"] = "review", now.UTC().Format(time.RFC3339), "tusker:recovery"
	delete(data, "accepted_by")
	delete(data, "accepted_at")
	delete(data, "closed_at")
	delete(data, "close_authority")
	if err := commitAdoptionTransition(vault, task.AbsolutePath, taskID, data, prior, body, baseRev, snapshot); err != nil {
		return result, err
	}
	_ = materialLock.Close()
	if err := emitV7Event(vault, taskID, "task", "status_changed", "tusker:recovery", map[string]any{"from": stringField(task.Data, "status"), "to": "review", "reason": "adopt_completed: checks passed; external implementation queued for independent review"}); err != nil {
		return result, err
	}
	affected, err := v7TaskIDsForTaskControl(vault, taskID)
	if err != nil {
		return result, err
	}
	if _, err := reconcileV7ControlProjections(vault, affected, "tusker:recovery", "task:"+taskID); err != nil {
		return result, err
	}
	task, err = resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	result.OK, result.Admitted, result.Lane = true, true, runLaneReview
	result.Reason = "Checks passed. Independent review is next."
	// Queue the review lane only through durable runtime state: a wave member
	// with a stored run gets the bound one-shot directive; without a run the
	// review status itself is the claimable queue for `tusker work review`.
	if waveID := strings.TrimSpace(stringField(task.Data, "wave")); waveID != "" && run != nil {
		if wave, waveErr := resolveV7Note(vault, waveID, "wave"); waveErr == nil {
			queued, queueErr := queueReviewRecovery(store, task, wave, *run, actor, maxAttempts, now, true)
			if queueErr == nil && queued.Refused {
				result.Reason = "Checks passed. Independent review is next. (review lane not queued: " + queued.Reason + ")"
			}
		}
	}
	return result, nil
}

// recordAdoptionMarker persists the truthful provenance marker with a CAS on
// the task's current state_rev. It never invents provider, conversation,
// attempt, or duration facts.
func applyAdoptionMarker(data map[string]any, actor string, now time.Time, material string) {
	data["implementation_source"] = adoptCompletedImplementationSource
	data["adopted_material_fingerprint"] = material
	data["adopted_by"] = actor
	data["adopted_at"] = now.UTC().Format(time.RFC3339)
	data["updated_at"] = now.UTC().Format(time.RFC3339)
	data["updated_by"] = actor
}

func commitAdoptionTransition(vault, path, taskID string, next, prior map[string]any, body, baseRev string, snapshot adoptCompletedSnapshot) error {
	if adoptCompletedBeforeWriteHook != nil {
		adoptCompletedBeforeWriteHook()
	}
	if err := snapshot.verifyCurrent(vault, taskID); err != nil {
		return err
	}
	nextRev, err := saveV7DocumentCASUnderMaterialLock(path, next, body, v7FrontmatterOrder["task"], baseRev)
	if err != nil {
		return err
	}
	material, err := workspaceTreeStateHashForPaths(snapshot.repoRoot, snapshot.scope, snapshot.generated)
	if err == nil && material == snapshot.material {
		return nil
	}
	if _, rollbackErr := saveV7DocumentCASUnderMaterialLock(path, prior, body, v7FrontmatterOrder["task"], nextRev); rollbackErr != nil {
		return tuskerError(errorInvalidTransition, taskID+": material changed during adopt_completed commit and rollback failed: "+rollbackErr.Error())
	}
	return tuskerError(errorEvidenceGate, taskID+": scoped implementation material changed during adopt_completed commit")
}

// adoptCompletedAuthorizationRefusal is the authorization half of the
// transition: humans and operators carry their own authority, while agent
// actors must present a current durable authorization whose material still
// matches the task they adopt.
func adoptCompletedAuthorizationRefusal(vault string, store *RuntimeStore, projectID string, task Note, idx v7Index, actor string, now time.Time) string {
	if strings.HasPrefix(actor, "human:") || strings.HasPrefix(actor, "operator:") {
		return ""
	}
	recordID := trackerRecordID(task)
	if waveID := strings.TrimSpace(stringField(task.Data, "wave")); waveID != "" {
		if wave, ok := idx.Waves[waveID]; ok {
			fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
			if len(issues) == 0 && directWaveAuthorizationCurrent(wave.Data, "armed", fingerprint) {
				return ""
			}
		}
		return "an inert task with no matching authorization refuses an agent invocation"
	}
	if directive, err := store.RunDirective(projectID, recordID); err == nil && directive != nil {
		if directive.State == "queued" && runDirectiveMatchesTaskAuthority(vault, task, directive, now) {
			return ""
		}
		if run, runErr := store.FindRunScoped(projectID, recordID); runErr == nil && run != nil {
			if auth, authErr := store.LatestRunAuthorization(projectID, recordID); authErr == nil && auth != nil {
				if consumedRunDirectiveMatchesTaskAuthority(vault, task, *run, directive, auth, now) ||
					runDirectiveAuthorizationMatchesTaskAuthority(vault, task, *run, auth) {
					return ""
				}
			}
		}
	}
	return "an inert task with no matching authorization refuses an agent invocation"
}

func workRecoverCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	action := strings.TrimSpace(args.String("action"))
	var actor string
	switch action {
	case "rerun_checks":
		actor, err = directStartActor(args, "work recover")
	case "adopt_completed":
		// Unlike rerun_checks, adopt_completed accepts any qualified actor;
		// agent invocations are authorized inside the transition by durable
		// wave/task authorization bound to the current material.
		actor = strings.TrimSpace(firstNonEmpty(args.String("by"), args.String("actor")))
		if actor == "" {
			err = tuskerError(errorMissingArg, "work recover --action adopt_completed requires --by <actor>")
		}
	default:
		return tuskerError(errorInvalidArg, "work recover requires --action rerun_checks or adopt_completed")
	}
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vault)
	if err != nil {
		return err
	}
	if !registered {
		return tuskerError(errorNotFound, "work recover requires a registered project for the selected vault")
	}
	if requested := strings.TrimSpace(args.String("project")); requested != "" && requested != projectID {
		return tuskerError(errorInvalidTransition, "work recover project does not match the selected vault")
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	var result serveRecoveryResult
	if action == "adopt_completed" {
		result, err = adoptCompletedWork(vault, store, projectID, id, actor, wf.Data.Retry.MaxAttempts, time.Now().UTC())
	} else {
		result, err = recoverVerificationChecks(vault, store, projectID, id, actor, wf.Data.Retry.MaxAttempts, time.Now().UTC())
	}
	if err != nil {
		return err
	}
	if result.Admitted {
		_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: "verification_recovery", Changes: []daemonControlChange{{ID: id, Kind: "run"}}}, 250*time.Millisecond)
	}
	emitJSON(result)
	return nil
}
