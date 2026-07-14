package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	taskTitle := taskID
	if task, ok := snap.notesByID[taskID]; ok {
		taskTitle = firstNonEmpty(stringField(task.Data, "title"), taskID)
	}
	turns, _ := s.store.ListTurnsForRun(run.ProjectID, run.RecordID)
	identity, _ := s.store.RunIdentity(run.ProjectID, run.RecordID)
	workspacePath, workspaceMode := run.WorkspacePath, "shared"
	if identity != nil {
		workspacePath, workspaceMode = identity.WorkspacePath, identity.WorkspaceMode
	}
	return serveRunSummary{
		TaskID:            taskID,
		TaskTitle:         taskTitle,
		ProjectID:         firstNonEmpty(run.ProjectID, snap.projectID),
		Runner:            serveRunner(run.Runner),
		RunnerName:        run.Runner,
		RunnerProfile:     run.RunnerProfile,
		Model:             nil,
		Lane:              serveLane(run.Lane),
		LeaseState:        serveLeaseState(run.LeaseState),
		LeaseStateRaw:     run.LeaseState,
		ProcessRunning:    runProcessGroupAlive(run),
		Outcome:           serveRunOutcome(run, s.now()),
		ElapsedSec:        serveRunElapsedSec(run, s.now()),
		SinceLastEventSec: serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), s.now()),
		Liveness:          serveRunLiveness(run, s.now()),
		AttemptCount:      maxInt(run.AttemptCount, len(turnsByAttempt(turns))),
		Terminal:          run.Terminal,
		Error:             nullIfBlank(run.LastError),
		LastHeartbeatAt:   nullIfBlank(run.LastHeartbeatAt),
		NextWakeAt:        nullIfBlank(run.NextRetryAt),
		WorkspacePath:     workspacePath,
		WorkspaceMode:     workspaceMode,
		StartedAt:         run.StartedAt,
		UpdatedAt:         run.UpdatedAt,
	}
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
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID != "" {
		if snap, err := s.loadSnapshotForProject(projectID); err != nil {
			serveJSON(w, http.StatusNotFound, serveInterruptResult{Refused: true, Reason: "run not found in project", TaskID: taskID})
			return
		} else if _, ok := serveFindRun(snap.runs, taskID); !ok {
			serveJSON(w, http.StatusNotFound, serveInterruptResult{Refused: true, Reason: "run not found in project", TaskID: taskID})
			return
		}
	}
	run, _, err := interruptRuntimeRun(DefaultStateRoot(), s.store, taskID)
	if err != nil {
		issue := errorToIssue(err)
		reason := issue.Message
		if issue.Hint != "" {
			reason += " Hint: " + issue.Hint
		}
		serveJSON(w, http.StatusOK, serveInterruptResult{Refused: true, Reason: reason, TaskID: taskID})
		return
	}
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
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateUnclaimed:
		if run.AttemptCount > 0 || strings.TrimSpace(run.AttemptOutcome) != "" && AttemptOutcome(strings.TrimSpace(run.AttemptOutcome)) != AttemptOutcomeNone {
			return serveRunOutcomeFromAttempt(run.AttemptOutcome, run.LeaseState)
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
			switch AttemptOutcome(strings.TrimSpace(run.AttemptOutcome)) {
			case AttemptOutcomeSucceeded, AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeEarlyExit, AttemptOutcomeDispatchDeclined, AttemptOutcomeTurnCapExhausted, AttemptOutcomeBudgetExceeded, AttemptOutcomeCancelled:
				outcome := serveRunOutcomeFromAttempt(run.AttemptOutcome, run.LeaseState)
				return outcome
			}
			return "terminal"
		}
	}
	return serveRunOutcomeFromAttempt(run.AttemptOutcome, run.LeaseState)
}

func serveRunOutcomeFromAttempt(outcome, lease string) string {
	switch AttemptOutcome(strings.TrimSpace(outcome)) {
	case AttemptOutcomeSucceeded:
		return "succeeded"
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
	eventPath := bestRunEventPath(run, attempts)
	if eventPath == "" {
		return []serveRunEvent{}
	}
	file, err := os.Open(eventPath)
	if err != nil {
		return []serveRunEvent{}
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 256*1024))
	if err != nil {
		return []serveRunEvent{}
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	out := []serveRunEvent{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(line), &payload) != nil {
			continue
		}
		out = append(out, serveRunEventFromPayload(payload))
	}
	return out
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
		TS:    field("at", "ts", "timestamp", "time"),
		Kind:  firstNonEmpty(field("kind", "event"), "event"),
		Text:  field("text", "message", "summary", "detail"),
		Level: field("level", "severity"),
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
	TaskID          string `json:"taskId"`
	CanonicalStatus string `json:"canonicalStatus"`
	LeaseState      string `json:"leaseState"`
}

// serveRedriveRefusal decides whether a redrive is meaningless for the task's
// canonical (frontmatter) status. Redrive resets the attempt window and
// requeues so the daemon spawns a fresh attempt; when the task is already in
// review or done there is no execution to redrive — the daemon would refuse and
// (the observed bug) silently retire the run behind a stale badge. We surface
// that refusal synchronously instead of requeuing into a silent retire.
func serveRedriveRefusal(rawStatus string, run RunStatus) (bool, string) {
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
