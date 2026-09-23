package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func serveRunHistory(s *serveServer, snap serveSnapshot, taskID string) []serveRunSummary {
	out := []serveRunSummary{}
	for _, run := range snap.runs {
		if run.ItemID == taskID || run.RecordID == taskID {
			out = append(out, s.runSummary(snap, run))
		}
	}
	return out
}

func (s *serveServer) runSummary(snap serveSnapshot, run RunStatus) serveRunSummary {
	result, _ := s.runSummaryChecked(snap, run)
	return result
}

func (s *serveServer) runSummaryChecked(snap serveSnapshot, run RunStatus) (serveRunSummary, error) {
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	taskTitle := taskID
	if task, ok := snap.notesByID[taskID]; ok {
		taskTitle = firstNonEmpty(stringField(task.Data, "title"), taskID)
	}
	turns, _, err := s.store.ListTurnsForRunPage(run.ProjectID, run.RecordID, 200)
	if err != nil {
		return serveRunSummary{}, err
	}
	identity, err := s.store.RunIdentity(run.ProjectID, run.RecordID)
	if err != nil {
		return serveRunSummary{}, err
	}
	workspacePath, workspaceMode := run.WorkspacePath, "shared"
	if identity != nil {
		workspacePath, workspaceMode = identity.WorkspacePath, identity.WorkspaceMode
	}
	attention, err := s.store.WorkerAttentionForRun(run)
	if err != nil {
		return serveRunSummary{}, err
	}
	return serveRunSummary{
		TaskID:               taskID,
		TaskTitle:            taskTitle,
		ProjectID:            firstNonEmpty(run.ProjectID, snap.projectID),
		Runner:               serveRunner(run.Runner),
		RunnerName:           run.Runner,
		RunnerProfile:        run.RunnerProfile,
		RunnerHarness:        run.RunnerHarness,
		Model:                run.RunnerModel,
		RunnerEffort:         run.RunnerEffort,
		RunnerFallbackReason: run.RunnerFallbackReason,
		Lane:                 serveLane(run.Lane),
		LeaseState:           serveLeaseState(run.LeaseState),
		LeaseStateRaw:        run.LeaseState,
		HandRun:              runHandRunOrigin(run, snap.project.VaultRoot),
		ProcessRunning:       runProcessGroupAlive(run),
		Outcome:              serveRunOutcome(run, s.now()),
		ElapsedSec:           serveRunElapsedSec(run, s.now()),
		SinceLastEventSec:    serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), s.now()),
		Liveness:             serveRunLiveness(run, s.now()),
		AttemptCount:         maxInt(run.AttemptCount, len(turnsByAttempt(turns))),
		ActiveAttemptID:      run.ActiveAttemptID,
		Terminal:             run.Terminal,
		Error:                nullIfBlank(run.LastError),
		Infrastructure:       run.Infrastructure,
		LastHeartbeatAt:      nullIfBlank(run.LastHeartbeatAt),
		NextWakeAt:           nullIfBlank(run.NextRetryAt),
		WorkspacePath:        workspacePath,
		WorkspaceMode:        workspaceMode,
		StartedAt:            run.StartedAt,
		UpdatedAt:            run.UpdatedAt,
		Attention:            attention,
	}, nil
}

func serveFindRun(runs []RunStatus, id string) (RunStatus, bool) {
	for _, run := range runs {
		if run.RecordID == id {
			return run, true
		}
	}
	for _, run := range runs {
		if run.ItemID == id {
			return run, true
		}
	}
	return RunStatus{}, false
}

func serveRunner(runner string) string {
	switch {
	case strings.Contains(runner, "claude"):
		return "claude"
	default:
		return "codex"
	}
}

func serveLane(lane string) string {
	if strings.Contains(strings.ToLower(lane), "review") {
		return "review"
	}
	return "execute"
}

func serveLeaseState(state string) string {
	if strings.TrimSpace(state) == runnerInfrastructureBlockedState {
		return runnerInfrastructureBlockedState
	}
	switch LeaseState(strings.TrimSpace(state)) {
	case LeaseStateUnclaimed, LeaseStateRetryQueued:
		return "unclaimed"
	case LeaseStateReleased, LeaseStateInterrupted, LeaseStateParkedBudget:
		return "released"
	case LeaseStateParkedNoProgress:
		return "parked"
	default:
		return "held"
	}
}

type serveInterruptResult struct {
	OK             bool   `json:"ok"`
	Refused        bool   `json:"refused,omitempty"`
	Interrupted    bool   `json:"interrupted"`
	Reason         string `json:"reason"`
	TaskID         string `json:"taskId"`
	LeaseState     string `json:"leaseState,omitempty"`
	LeaseStateRaw  string `json:"leaseStateRaw,omitempty"`
	ProcessRunning bool   `json:"processRunning"`
}

// handleRunInterrupt shares the exact runtime transition used by
// `tusker runs interrupt`, then returns canonical store readback so the UI does
// not enable Redrive based only on a successful HTTP response.
func (s *serveServer) handleRunInterrupt(w http.ResponseWriter, r *http.Request, taskID string) {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	// Interrupt is a mutating, cross-project-dangerous action: without an explicit
	// project a same-ID run in any other project could be stopped instead.
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID == "" {
		serveJSON(w, http.StatusBadRequest, serveInterruptResult{Refused: true, Reason: "project query parameter is required to interrupt a run", TaskID: taskID})
		return
	}
	if snap, err := s.loadSnapshotForProject(projectID); err != nil {
		serveJSON(w, http.StatusNotFound, serveInterruptResult{Refused: true, Reason: "run not found in project", TaskID: taskID})
		return
	} else if _, ok := serveFindRun(snap.runs, taskID); !ok {
		serveJSON(w, http.StatusNotFound, serveInterruptResult{Refused: true, Reason: "run not found in project", TaskID: taskID})
		return
	}
	run, _, err := interruptRuntimeRunScoped(DefaultStateRoot(), s.store, projectID, taskID)
	if err != nil {
		issue := errorToIssue(err)
		reason := issue.Message
		if issue.Hint != "" {
			reason += " Hint: " + issue.Hint
		}
		serveJSON(w, http.StatusOK, serveInterruptResult{Refused: true, Reason: reason, TaskID: taskID})
		return
	}
	// The preflight snapshot load above warms the cache pre-mutation; drop it so
	// follow-up reads see the interrupted lease instead of the stale snapshot.
	s.dropProjectSnapshot(projectID)
	processRunning := runProcessGroupAlive(*run)
	confirmed := LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateInterrupted && !processRunning
	result := serveInterruptResult{
		OK:             confirmed,
		Refused:        !confirmed,
		Interrupted:    confirmed,
		TaskID:         firstNonEmpty(run.ItemID, taskID),
		LeaseState:     serveLeaseState(run.LeaseState),
		LeaseStateRaw:  run.LeaseState,
		ProcessRunning: processRunning,
	}
	if confirmed {
		result.Reason = "run interrupted; canonical lease is interrupted and no process is running"
	} else {
		result.Reason = "interrupt returned before canonical lease/process state confirmed the stop"
	}
	s.refreshProjectSnapshot(firstNonEmpty(run.ProjectID, projectID))
	serveJSON(w, http.StatusOK, result)
}

func serveRunOutcome(run RunStatus, now time.Time) string {
	if strings.TrimSpace(run.LeaseState) == runnerInfrastructureBlockedState || (run.Infrastructure != nil && run.Infrastructure.State == runnerInfrastructureBlockedState) {
		return runnerInfrastructureBlockedState
	}
	projectedOutcome := projectedAttemptOutcome(run.AttemptOutcome, run.LastError)
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateUnclaimed:
		if run.AttemptCount > 0 || projectedOutcome != "" && projectedOutcome != AttemptOutcomeNone {
			return serveRunOutcomeFromAttempt(string(projectedOutcome), run.LeaseState)
		}
		return "idle"
	case LeaseStateParkedNoProgress:
		return "parked-no-progress"
	case LeaseStateParkedBudget:
		return "parked-budget"
	case LeaseStateRetryQueued:
		return "retry-queued"
	case LeaseStateClaimed, LeaseStateRunning:
		if serveRunHeartbeatFresh(run, now) {
			return "running"
		}
		return "stale"
	case LeaseStateReleased:
		if run.Terminal {
			switch projectedOutcome {
			case AttemptOutcomeSucceeded, AttemptOutcomeFailed, AttemptOutcomeUnknown, AttemptOutcomeBlocked, AttemptOutcomeEarlyExit, AttemptOutcomeDispatchDeclined, AttemptOutcomeTurnCapExhausted, AttemptOutcomeBudgetExceeded, AttemptOutcomeCancelled:
				outcome := serveRunOutcomeFromAttempt(string(projectedOutcome), run.LeaseState)
				return outcome
			}
			return "terminal"
		}
	}
	return serveRunOutcomeFromAttempt(string(projectedOutcome), run.LeaseState)
}

func serveRunOutcomeFromAttempt(outcome, lease string) string {
	if strings.TrimSpace(lease) == runnerInfrastructureBlockedState {
		return runnerInfrastructureBlockedState
	}
	switch AttemptOutcome(strings.TrimSpace(outcome)) {
	case AttemptOutcomeSucceeded:
		return "succeeded"
	case AttemptOutcomeUnknown:
		return "outcome-unknown"
	case AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeAbandoned, AttemptOutcomeEarlyExit, AttemptOutcomeTurnCapExhausted, AttemptOutcomeBudgetExceeded:
		return "failed"
	case AttemptOutcomeDispatchDeclined:
		return "dispatch-declined"
	case AttemptOutcomeCancelled:
		return "interrupted"
	case AttemptOutcomeWaitingForReview:
		return "review-complete"
	case AttemptOutcomeWaitingForHuman:
		return "awaiting-human"
	default:
		if LeaseState(lease) == LeaseStateUnclaimed {
			return "idle"
		}
		if LeaseState(lease) == LeaseStateParkedNoProgress {
			return "parked-no-progress"
		}
		if LeaseState(lease) == LeaseStateParkedBudget {
			return "parked-budget"
		}
		if LeaseState(lease) == LeaseStateRetryQueued {
			return "retry-queued"
		}
		return "released"
	}
}

// serveAttemptOutcome keeps the active attempt aligned with the canonical run
// liveness. An attempt row is intentionally durable before it has a terminal
// outcome, so mapping an active "none" outcome through the lease fallback
// would incorrectly paint a live reviewer as released.
func (s *serveServer) serveAttemptOutcome(run RunStatus, attempt RunAttempt) string {
	if attempt.AttemptID == run.ActiveAttemptID {
		current := serveRunOutcome(run, s.now())
		if current == "running" || current == "stale" {
			return current
		}
	}
	return serveRunOutcomeFromAttempt(string(projectedAttemptOutcome(attempt.Outcome, attempt.LastError)), run.LeaseState)
}

func serveRunHeartbeatFresh(run RunStatus, now time.Time) bool {
	heartbeatAt, ok := parseRunTimestamp(run.LastHeartbeatAt)
	return ok && now.Sub(heartbeatAt) <= daemonHeartbeatDeadThreshold
}

func serveRunHiddenByDefault(run RunStatus) bool {
	return LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateUnclaimed && run.AttemptCount == 0
}

func serveRunLiveness(run RunStatus, now time.Time) string {
	age := serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), now)
	switch {
	case age < 60:
		return "fresh"
	case age < 120:
		return "stale"
	default:
		return "dead"
	}
}

func serveWorstLiveness(current any, next string) any {
	rank := map[string]int{"fresh": 0, "stale": 1, "dead": 2}
	if current == nil {
		return next
	}
	if rank[next] > rank[fmt.Sprint(current)] {
		return next
	}
	return current
}

func serveSinceSec(value string, now time.Time) int {
	if value == "" {
		return 0
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return maxInt(0, int(now.Sub(ts).Seconds()))
	}
	return 0
}

func serveRunElapsedSec(run RunStatus, now time.Time) int {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning:
		return serveDurationSec(run.StartedAt, "", now)
	default:
		return serveDurationSec(run.StartedAt, firstNonEmpty(run.UpdatedAt, run.LastEventAt, run.LastHeartbeatAt), now)
	}
}

func serveDurationSec(start, end string, now time.Time) int {
	if start == "" {
		return 0
	}
	startAt, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return 0
	}
	endAt := now
	if end != "" {
		if parsed, err := time.Parse(time.RFC3339, end); err == nil {
			endAt = parsed
		}
	}
	return maxInt(0, int(endAt.Sub(startAt).Seconds()))
}

func turnsByAttempt(turns []RunTurn) map[string]struct{} {
	out := map[string]struct{}{}
	for _, turn := range turns {
		if turn.AttemptID != "" {
			out[turn.AttemptID] = struct{}{}
		}
	}
	return out
}

func serveRunEvents(run RunStatus, attempts []RunAttempt) []serveRunEvent {
	var latest RunAttempt
	for _, attempt := range attempts {
		if run.ActiveAttemptID != "" {
			if attempt.AttemptID == run.ActiveAttemptID {
				latest = attempt
				break
			}
		} else if latest.AttemptID == "" || attempt.StartedAt > latest.StartedAt {
			latest = attempt
		}
	}
	attempts = []RunAttempt{latest}
	out := []serveRunEvent{}
	for _, payload := range runActivityTail(bestRunEventPath(run, attempts)) {
		out = appendRunActivity(out, serveRunEventFromPayload(payload))
	}
	for _, payload := range runActivityTail(bestRunLogPath(run, attempts)) {
		for _, event := range cliRunActivity(payload) {
			out = appendRunActivity(out, event)
		}
	}
	// Keep diagnostics from displacing the recent messages with heartbeat noise.
	activityCount, diagnosticCount := 0, 0
	keep := make([]serveRunEvent, 0, 100)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Activity {
			activityCount++
			if activityCount > 50 {
				continue
			}
		} else {
			diagnosticCount++
			if diagnosticCount > 50 {
				continue
			}
		}
		keep = append(keep, out[i])
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	return keep
}

// serveRunEventFromPayload flattens one JSONL event line into the shape the run
// event tail reads. The runtime EventLog (see event_log.go) writes the
// timestamp under the top-level "at" key (RFC3339) and nests the human-readable
// fields under a "payload" object. The UI parser only knew "ts"/"timestamp",
// so every row rendered NaN:NaN:NaN — read "at" first (the field the API
// actually emits) and dig into the nested payload for text/level.
func serveRunEventFromPayload(payload map[string]any) serveRunEvent {
	nested, _ := payload["payload"].(map[string]any)
	field := func(keys ...string) string {
		for _, k := range keys {
			if v := toString(payload[k]); v != "" {
				return v
			}
			if nested != nil {
				if v := toString(nested[k]); v != "" {
					return v
				}
			}
		}
		return ""
	}
	return serveRunEvent{
		TS:       field("at", "ts", "timestamp", "time"),
		ID:       field("message_id"),
		Kind:     firstNonEmpty(field("kind", "event"), "event"),
		Text:     runActivityText(field("text", "message", "summary", "detail")),
		Level:    field("level", "severity"),
		Activity: nested != nil && nested["activity"] == true,
	}
}

// serveRedriveResult is the response body for POST /api/runs/:taskId/redrive.
// It always carries a human-readable reason so the UI can surface both a
// successful requeue and a refusal — a redrive must never be silent.
type serveRedriveResult struct {
	OK              bool   `json:"ok"`
	Refused         bool   `json:"refused"`
	Requeued        bool   `json:"requeued"`
	Reason          string `json:"reason"`
	Issue           *Issue `json:"issue,omitempty"`
	TaskID          string `json:"taskId"`
	CanonicalStatus string `json:"canonicalStatus"`
	LeaseState      string `json:"leaseState"`
}

type serveRecoveryResult struct {
	OK       bool   `json:"ok"`
	Refused  bool   `json:"refused"`
	Admitted bool   `json:"admitted"`
	Action   string `json:"action"`
	TaskID   string `json:"taskId"`
	Lane     string `json:"lane,omitempty"`
	Reason   string `json:"reason"`
}

const outcomeUnknownRecoveryReasonPrefix = "recover outcome unknown from "
const outcomeUnknownContextRecoveryReasonPrefix = "recover context from "

func outcomeUnknownRecoveryParent(lastError string) string {
	for _, prefix := range []string{outcomeUnknownRecoveryReasonPrefix, outcomeUnknownContextRecoveryReasonPrefix} {
		marker := strings.Index(lastError, prefix)
		if marker >= 0 {
			return strings.TrimSpace(lastError[marker+len(prefix):])
		}
	}
	return ""
}

// The directive is the durable queued intent; the redrive record covers older
// recoveries whose directive lacks a reason. Both survive capacity waits.
func pendingOutcomeUnknownRecoveryParent(store *RuntimeStore, run RunStatus) (string, error) {
	parent := outcomeUnknownRecoveryParent(run.LastError)
	if store == nil {
		return parent, nil
	}
	directive, err := store.RunDirective(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	record, err := store.BudgetWindowStart(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	if directive != nil && (directive.State == "queued" || directive.State == "consumed") {
		if durable := outcomeUnknownRecoveryParent(directive.Reason); durable != "" {
			parent = durable
		}
	}
	// A later ordinary redrive supersedes an unclaimed recovery directive.
	if directive != nil && record.ResetAt != "" && outcomeUnknownRecoveryParent(record.Reason) == "" {
		redriveAt, redriveErr := time.Parse(time.RFC3339Nano, record.ResetAt)
		directiveAt, directiveErr := time.Parse(time.RFC3339Nano, directive.CreatedAt)
		if redriveErr == nil && directiveErr == nil && redriveAt.After(directiveAt) {
			return "", nil
		}
	}
	if parent == "" {
		parent = outcomeUnknownRecoveryParent(record.Reason)
	}
	if parent == "" {
		return "", nil
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	for _, attempt := range attempts {
		if attempt.ParentAttemptID == parent && attempt.ChildType == "recovery" {
			return "", nil
		}
	}
	return parent, nil
}

func queueOutcomeUnknownRecovery(store *RuntimeStore, task Note, wave Note, run RunStatus, actor string, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "recover_unknown", TaskID: stringField(task.Data, "id"), Lane: runLaneExecute}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	if current == nil {
		result.Refused, result.Reason = true, "run not found"
		return result, nil
	}
	run = *current
	if !run.Terminal || projectedAttemptOutcome(run.AttemptOutcome, run.LastError) != AttemptOutcomeUnknown {
		result.Refused, result.Reason = true, "recovery is only available when the prior outcome is unknown"
		return result, nil
	}
	if runProcessGroupAlive(run) || isDispatchingLeaseState(run.LeaseState) {
		result.Refused, result.Reason = true, "a live owner still holds this task; wait for it to finish or become stale"
		return result, nil
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	var parent RunAttempt
	for _, attempt := range attempts {
		if projectedAttemptOutcome(attempt.Outcome, attempt.LastError) == AttemptOutcomeUnknown {
			parent = attempt
			break
		}
	}
	if parent.AttemptID == "" {
		result.Refused, result.Reason = true, "the uncertain parent attempt cannot be identified"
		return result, nil
	}
	for _, attempt := range attempts {
		if attempt.ParentAttemptID == parent.AttemptID && attempt.ChildType == "recovery" {
			result.Refused, result.Reason = true, "one recovery attempt already exists; human review is required"
			return result, nil
		}
	}
	// Older detached ACP attempts retained the bound session in their event
	// ledger but did not copy it into the run row. Preserve that exact identity
	// before redrive so the normal native-resume checks can use it.
	if run.Runner == string(RunnerDevin) && parent.SessionRef == "" {
		if ref := acpSessionRefFromAttemptEvents(parent.EventSinkPath, parent.AttemptID, parent.Runner); ref != "" {
			parent.SessionRef = ref
			if err := store.SaveAttempt(parent); err != nil {
				return result, err
			}
			if err := store.SaveSession(RunnerSession{
				ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: ref,
				WorkspacePath: parent.WorkspacePath, CurrentItemID: run.ItemID, WorkRevision: parent.WorkRevision,
				LastAttemptID: parent.AttemptID, State: sessionStateForLeaseState(LeaseStateReleased),
				Resumable: true, StartedAt: parent.StartedAt, LastSeenAt: now.UTC().Format(time.RFC3339),
			}); err != nil {
				return result, err
			}
		}
	}
	previousRun := run
	previousBudget, err := store.GetSetting(budgetRedriveSettingKey(run.ProjectID, run.RecordID))
	if err != nil {
		return result, err
	}
	if _, err := redriveRuntimeRun(store, &run, actor, outcomeUnknownRecoveryReasonPrefix+parent.AttemptID, now); err != nil {
		return result, err
	}
	queued, err := store.QueueRunDirective(RunDirective{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Actor: actor,
		Reason:    outcomeUnknownRecoveryReasonPrefix + parent.AttemptID,
		CreatedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: now.UTC().Add(directRunDirectiveTTL).Format(time.RFC3339Nano),
		WaveID: stringField(wave.Data, "id"), AuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), WaveAuthorizedAt: stringField(wave.Data, "authorized_at"),
	})
	if err != nil {
		// Redrive and directive insertion are separate store operations. If the
		// directive write fails, restore the exact pre-recovery snapshot only if
		// no daemon/operator write won the race in the meantime.
		if matches, matchErr := store.RunMatchesSnapshot(run); matchErr == nil && matches {
			if rollbackErr := store.UpsertRun(previousRun); rollbackErr != nil {
				return result, fmt.Errorf("recovery directive failed: %w; rollback failed: %v", err, rollbackErr)
			}
			if previousBudget == "" {
				_, _ = store.exec(`DELETE FROM daemon_settings WHERE key = ?`, budgetRedriveSettingKey(run.ProjectID, run.RecordID))
			} else {
				_ = store.SetSetting(budgetRedriveSettingKey(run.ProjectID, run.RecordID), previousBudget)
			}
		}
		return result, err
	}
	if !queued {
		result.OK, result.Reason = true, "recovery is already queued"
		return result, nil
	}
	result.OK, result.Admitted, result.Reason = true, true, "verification queued; the worker will inspect and preserve existing work before continuing"
	return result, nil
}

// queueReviewRecovery reuses the one-shot directive consumed by the normal
// daemon. It never resets AttemptCount: automatic recovery remains inside the
// originally authorized retry window.
func queueReviewRecovery(store *RuntimeStore, task Note, wave Note, run RunStatus, actor string, maxAttempts int, now time.Time, allowLaneChange ...bool) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "retry_review", TaskID: stringField(task.Data, "id"), Lane: runLaneReview}
	if current, err := store.FindRunScoped(run.ProjectID, run.RecordID); err != nil {
		return result, err
	} else if current != nil {
		run = *current
	}
	canChangeLane := len(allowLaneChange) > 0 && allowLaneChange[0]
	if !strings.EqualFold(stringField(task.Data, "status"), "review") || (run.Lane != runLaneReview && !canChangeLane) {
		result.Refused, result.Reason = true, "retry review is only available for a failed independent-review lane"
		return result, nil
	}
	if reason := reviewRecoveryOperationalBlocker(wave, run, maxAttempts); reason != "" {
		result.Refused, result.Reason = true, reason
		return result, nil
	}
	fingerprint := stringField(wave.Data, "authorization_fingerprint")
	authorizedAt := stringField(wave.Data, "authorized_at")
	prepared := prepareRunForLaneDispatch(run, runLaneReview, run.Runner)
	prepared.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	if err := store.UpsertRun(prepared); err != nil {
		return result, err
	}
	queued, err := store.QueueRunDirective(RunDirective{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Actor: actor,
		CreatedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: now.UTC().Add(directRunDirectiveTTL).Format(time.RFC3339Nano),
		WaveID: stringField(wave.Data, "id"), AuthorizationFingerprint: fingerprint, WaveAuthorizedAt: authorizedAt,
	})
	if err != nil {
		return result, err
	}
	if !queued {
		result.OK, result.Reason = true, "review recovery is already queued or owned"
		return result, nil
	}
	result.OK, result.Admitted, result.Reason = true, true, "review retry queued on the existing review lane; implementation will not rerun"
	return result, nil
}

func reviewRecoveryOperationalBlocker(wave Note, run RunStatus, maxAttempts int) string {
	if runProcessGroupAlive(run) || isDispatchingLeaseState(run.LeaseState) {
		return "an owner is already active for this review"
	}
	if maxAttempts > 0 && run.AttemptCount >= maxAttempts {
		return fmt.Sprintf("review attempt limit exhausted (%d); authorize one new bounded execution window", maxAttempts)
	}
	if strings.EqualFold(stringField(wave.Data, "authorization"), "paused") {
		return "wave is paused; recovery will not secretly resume it"
	}
	if !strings.EqualFold(stringField(wave.Data, "authorization"), "armed") || stringField(wave.Data, "authorization_fingerprint") == "" || stringField(wave.Data, "authorized_at") == "" {
		return "the existing wave execution window is not current"
	}
	// The queued review directive is bound to the stored authorization
	// fingerprint; if wave material drifted since then it can never match
	// runDirectiveMatchesTaskAuthority and would sit until TTL expiry. Refuse
	// up front with the real recovery action instead of admitting silently.
	if vault := v7VaultPathForLandingAudit(wave); vault != "" {
		idx, err := loadV7Index(vault)
		if err != nil {
			return "cannot verify wave material before retrying review: " + err.Error()
		}
		if auth := waveAuthorizationProjection(vault, idx, wave); boolFromAny(auth["stale"]) {
			return "wave material changed since authorization; re-authorize with `tusker wave start` before retrying review"
		}
	}
	return ""
}

// serveRedriveRefusal decides whether a redrive is meaningless for the task's
// canonical (frontmatter) status. Redrive resets the attempt window and
// requeues so the daemon spawns a fresh attempt; when the task is already in
// review or done there is no execution to redrive — the daemon would refuse and
// (the observed bug) silently retire the run behind a stale badge. We surface
// that refusal synchronously instead of requeuing into a silent retire.
func serveRedriveRefusal(rawStatus string, run RunStatus) (bool, string) {
	if projectedAttemptOutcome(run.AttemptOutcome, run.LastError) == AttemptOutcomeUnknown {
		return true, "this work needs recovery — use Verify and continue so the next worker inspects and preserves existing work"
	}
	switch strings.ToLower(strings.TrimSpace(rawStatus)) {
	case "review":
		return true, "task is in review — no execution to redrive; use the review/land lane, not retry"
	case "done", "closed":
		return true, "task is done — no execution to redrive; nothing to retry"
	}
	if LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateRetryQueued {
		return true, "redrive is already queued; wait for the daemon to claim it or interrupt the queued run first"
	}
	if runProcessGroupAlive(run) {
		return true, "run is still executing; interrupt it before redrive"
	}
	return false, ""
}

// serveRedriveRun mirrors the state transition of `tusker redrive` (redriveCmd)
// using the shared runtime store: it resets the budget/attempt window and
// requeues the run so the daemon spawns a fresh codex-exec attempt. The
// attempt-window rules themselves are owned by RUN-T-0028; keep this in sync
// with redriveCmd.
func serveRedriveRun(store *RuntimeStore, run *RunStatus, actor, reason string, now time.Time) error {
	previousAttemptID := run.ActiveAttemptID
	previousSessionRef := run.SessionRef
	previousWorkspacePath := run.WorkspacePath
	if _, err := redriveRuntimeRun(store, run, actor, reason, now); err != nil {
		return err
	}
	_, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID:        run.ProjectID,
		RecordID:         run.RecordID,
		ItemID:           run.ItemID,
		Runner:           run.Runner,
		WorkRevision:     run.WorkRevision,
		AttemptID:        previousAttemptID,
		ParentAttemptID:  previousAttemptID,
		SessionRef:       previousSessionRef,
		ParentSessionRef: previousSessionRef,
		Kind:             string(SupervisorDecisionRedrive),
		Reason:           reason,
		WorkspacePath:    previousWorkspacePath,
		LeaseState:       run.LeaseState,
		ContextSignal:    "operator_redrive_serve",
		CreatedAt:        now.Format(time.RFC3339),
	})
	return err
}
