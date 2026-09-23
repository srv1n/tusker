package main

import (
	"fmt"
	"strings"
	"time"
)

const defaultRunQuietAfter = 120 * time.Second

type runOperatorReason struct {
	Code      string `json:"code"`
	Class     string `json:"class"`
	Guidance  string `json:"guidance"`
	Retryable bool   `json:"retryable"`
	Source    string `json:"source"`
}

type runOperatorEvidence struct {
	OwnerAlive      bool   `json:"owner_alive"`
	LastActivityAt  string `json:"last_activity_at,omitempty"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
	ToolInFlight    bool   `json:"tool_in_flight"`
	OpenQuestionID  string `json:"open_question_id,omitempty"`
}

type runOperatorState struct {
	State         string              `json:"state"`
	Since         string              `json:"since"`
	Reason        *runOperatorReason  `json:"reason"`
	Evidence      runOperatorEvidence `json:"evidence"`
	QuietAfterSec int64               `json:"quiet_after_sec"`
}

type runOperatorFacts struct {
	LeaseState      string
	Outcome         string
	Terminal        bool
	OwnerAlive      bool
	LastActivityAt  string
	LastHeartbeatAt string
	ToolInFlight    bool
	OpenQuestionID  string
	PermissionWait  bool
	Reason          *runOperatorReason
	StartedAt       string
	UpdatedAt       string
}

// deriveRunOperatorState has no store, process or provider reads. Callers
// supply observed facts; a wrapper heartbeat is evidence, never activity.
func deriveRunOperatorState(f runOperatorFacts, now time.Time, quietAfter time.Duration) runOperatorState {
	if quietAfter <= 0 {
		quietAfter = defaultRunQuietAfter
	}
	now = now.UTC()
	state := runOperatorState{
		Evidence:      runOperatorEvidence{f.OwnerAlive, f.LastActivityAt, f.LastHeartbeatAt, f.ToolInFlight, f.OpenQuestionID},
		QuietAfterSec: int64(quietAfter / time.Second),
	}
	set := func(name, since string, reason *runOperatorReason) runOperatorState {
		state.State, state.Since, state.Reason = name, firstNonEmpty(since, f.UpdatedAt, f.StartedAt), reason
		return state
	}
	activeLease := f.LeaseState == string(LeaseStateClaimed) || f.LeaseState == string(LeaseStateRunning)
	if f.OpenQuestionID != "" || f.PermissionWait || f.Outcome == string(AttemptOutcomeWaitingForHuman) {
		return set("waiting_on_you", firstNonEmpty(f.LastActivityAt, f.UpdatedAt), nil)
	}
	if activeLease && f.OwnerAlive {
		activityAt, valid := parseRunTimestamp(f.LastActivityAt)
		if f.ToolInFlight || valid && now.Sub(activityAt) <= quietAfter {
			return set("working", firstNonEmpty(f.LastActivityAt, f.StartedAt), nil)
		}
		return set("quiet", firstNonEmpty(f.LastActivityAt, f.StartedAt), nil)
	}
	if f.Outcome == string(AttemptOutcomeUnknown) || f.Reason != nil && f.Reason.Class == "lost" || activeLease && !f.Terminal {
		return set("lost", f.UpdatedAt, f.Reason)
	}
	if !f.Terminal && (f.LeaseState == string(LeaseStateRetryQueued) || f.LeaseState == string(LeaseStateUnclaimed)) {
		return set("queued", f.UpdatedAt, nil)
	}
	if f.Outcome == string(AttemptOutcomeSucceeded) || f.Outcome == string(AttemptOutcomeWaitingForReview) {
		return set("finished", f.UpdatedAt, nil)
	}
	if f.Outcome == string(AttemptOutcomeCancelled) || f.Outcome == string(AttemptOutcomeInterrupted) || f.Reason != nil && f.Reason.Code == "cancelled" {
		return set("stopped", f.UpdatedAt, f.Reason)
	}
	if f.Reason != nil && f.Reason.Class == "blocked" {
		return set("blocked", f.UpdatedAt, f.Reason)
	}
	return set("failed", f.UpdatedAt, f.Reason)
}

func runOperatorStateLine(state runOperatorState, now time.Time) string {
	label := strings.ReplaceAll(state.State, "_", " ")
	if label != "" {
		label = strings.ToUpper(label[:1]) + label[1:]
	}
	if state.State == "quiet" {
		if at, ok := parseRunTimestamp(state.Evidence.LastActivityAt); ok {
			age := max(time.Duration(0), now.Sub(at))
			return fmt.Sprintf("State: %s — no agent activity for %s, no tool running", label, age.Truncate(time.Second))
		}
		return "State: Quiet — no recorded agent activity, no tool running"
	}
	if state.Reason != nil && state.Reason.Guidance != "" {
		return fmt.Sprintf("State: %s — %s", label, state.Reason.Guidance)
	}
	return "State: " + label
}

func runOperatorStateForRun(store *RuntimeStore, run RunStatus, now time.Time) (runOperatorState, error) {
	return runOperatorStateForRunWithQuietAfter(store, run, now, defaultRunQuietAfter)
}

func runOperatorStateForRunWithQuietAfter(store *RuntimeStore, run RunStatus, now time.Time, quietAfter time.Duration) (runOperatorState, error) {
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return runOperatorState{}, err
	}
	messages, err := store.ListAgentMessagesForTask(run.ProjectID, run.ItemID)
	if err != nil {
		return runOperatorState{}, err
	}
	facts := runOperatorFacts{
		LeaseState: run.LeaseState, Outcome: string(projectedAttemptOutcome(run.AttemptOutcome, run.LastError)),
		Terminal: run.Terminal, OwnerAlive: runProcessGroupAlive(run),
		LastHeartbeatAt: run.LastHeartbeatAt, StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt,
	}
	for _, message := range messages {
		if message.Kind == "question" && message.AnsweredAt == "" && message.OriginTaskID == run.ItemID {
			facts.OpenQuestionID = message.ID
			break
		}
	}
	if code := inspectedReasonCode(run, attempts); code != "" && inspectedReasonSource(run, attempts) == "driver" {
		if spec, ok := runFailureReason(RunFailureReasonCode(code)); ok {
			facts.Reason = &runOperatorReason{code, spec.Class, spec.Guidance, spec.Retryable, inspectedReasonSource(run, attempts)}
		}
	} else if run.Terminal && facts.Outcome != string(AttemptOutcomeSucceeded) && facts.Outcome != string(AttemptOutcomeWaitingForReview) {
		spec, _ := runFailureReason(RunFailureUnknown)
		facts.Reason = &runOperatorReason{string(RunFailureUnknown), spec.Class, spec.Guidance, spec.Retryable, "legacy_text"}
	}
	for _, event := range serveRunEventsAll(run, attempts) {
		if event.Kind == "permission_wait" || event.Kind == "permission_request" {
			facts.PermissionWait = true
		}
		if !event.Activity && event.Kind != "agent_message" && event.Kind != "message" && event.Kind != "tool_call" && event.Kind != "tool_result" && event.Kind != "tool_progress" && event.Kind != "plan" {
			continue
		}
		if at, ok := parseRunTimestamp(event.TS); ok {
			if latest, valid := parseRunTimestamp(facts.LastActivityAt); !valid || at.After(latest) {
				facts.LastActivityAt = at.Format(time.RFC3339Nano)
			}
		}
	}
	if _, inFlight := codexExecInFlightCommandStartedAt(run, now); inFlight {
		facts.ToolInFlight = true
	}
	if !facts.ToolInFlight {
		facts.ToolInFlight = runToolInFlight(run, attempts)
	}
	return deriveRunOperatorState(facts, now, quietAfter), nil
}

func runToolInFlight(run RunStatus, attempts []RunAttempt) bool {
	active := map[string]bool{}
	for _, row := range runActivityTail(bestRunLogPath(run, attempts)) {
		switch stringValue(row["type"]) {
		case "item.started", "item.completed":
			item, _ := row["item"].(map[string]any)
			kind := stringValue(item["type"])
			if kind != "command_execution" && kind != "mcp_tool_call" {
				continue
			}
			if id := stringValue(item["id"]); id != "" {
				active[id] = stringValue(row["type"]) == "item.started"
			}
		case "assistant", "user":
			message, _ := row["message"].(map[string]any)
			blocks, _ := message["content"].([]any)
			for _, raw := range blocks {
				block, _ := raw.(map[string]any)
				switch stringValue(block["type"]) {
				case "tool_use":
					if id := stringValue(block["id"]); id != "" {
						active[id] = true
					}
				case "tool_result":
					if id := stringValue(block["tool_use_id"]); id != "" {
						delete(active, id)
					}
				}
			}
		}
	}
	for _, row := range runActivityTail(bestRunEventPath(run, attempts)) {
		if stringValue(row["kind"]) != "tool_call" {
			continue
		}
		payload, _ := row["payload"].(map[string]any)
		id := stringValue(payload["tool_call_id"])
		if id == "" {
			continue
		}
		text := strings.ToLower(stringValue(payload["text"]))
		active[id] = !strings.Contains(text, "completed") && !strings.Contains(text, "failed") && !strings.Contains(text, "cancelled")
	}
	for _, inFlight := range active {
		if inFlight {
			return true
		}
	}
	return false
}
