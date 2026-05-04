package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeStoreSavesAndListsTurns(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SaveTurn(RunTurn{
		AttemptID:    "attempt-1",
		ProjectID:    "project-1",
		RecordID:     "record-1",
		TurnID:       "turn-1",
		TurnIndex:    0,
		SessionRef:   "session-1",
		Status:       "running",
		InputTokens:  12,
		OutputTokens: 3,
		TotalTokens:  15,
		StartedAt:    "2026-04-28T01:00:00Z",
		LastEventAt:  "2026-04-28T01:00:01Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{
		AttemptID:    "attempt-1",
		ProjectID:    "project-1",
		RecordID:     "record-1",
		TurnID:       "turn-1",
		TurnIndex:    0,
		Status:       "completed",
		OutputTokens: 8,
		TotalTokens:  20,
		CompletedAt:  "2026-04-28T01:01:00Z",
		LastEventAt:  "2026-04-28T01:01:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	turns, err := store.ListTurnsForRun("project-1", "record-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(turns), "turn count")
	assertEqual(t, "turn-1", turns[0].TurnID, "turn id")
	assertEqual(t, "completed", turns[0].Status, "turn status")
	assertEqual(t, 12, turns[0].InputTokens, "preserved input tokens")
	assertEqual(t, 8, turns[0].OutputTokens, "updated output tokens")
	assertEqual(t, "session-1", turns[0].SessionRef, "preserved session ref")
	assertEqual(t, "2026-04-28T01:01:00Z", turns[0].CompletedAt, "completed at")

	next, err := store.NextTurnIndex("project-1", "record-1", "attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, next, "next turn index")
}

func TestRuntimeStoreSavesAndListsSupervisorDecisions(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	decision, err := store.SaveRuntimeSupervisorDecision(RuntimeSupervisorDecision{
		DecisionID:          "decision-1",
		ProjectID:           "project-1",
		RecordID:            "record-1",
		AttemptID:           "attempt-2",
		ParentAttemptID:     "attempt-1",
		SessionRef:          "session-2",
		ParentSessionRef:    "session-1",
		Kind:                string(SupervisorDecisionForkThread),
		Reason:              "context pressure crossed threshold",
		BranchName:          "task/record-1",
		WorkspacePath:       "/tmp/workspace",
		ValidationDelta:     "run parser-specific regression suite",
		MergeRule:           "merge winning branch only after review",
		ContextSignal:       "context_pressure",
		InputTokens:         1000,
		OutputTokens:        250,
		TotalTokens:         1250,
		ContextWindowTokens: 2000,
		CreatedAt:           "2026-04-28T01:03:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "decision-1", decision.DecisionID, "decision id")

	runDecisions, err := store.ListRuntimeSupervisorDecisionsForRun("project-1", "record-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(runDecisions), "run decision count")
	assertEqual(t, string(SupervisorDecisionForkThread), runDecisions[0].Kind, "decision kind")
	assertEqual(t, "attempt-1", runDecisions[0].ParentAttemptID, "parent attempt")
	assertEqual(t, "session-1", runDecisions[0].ParentSessionRef, "parent session")
	assertEqual(t, "run parser-specific regression suite", runDecisions[0].ValidationDelta, "validation delta")
	assertEqual(t, "merge winning branch only after review", runDecisions[0].MergeRule, "merge rule")
	assertEqual(t, "context_pressure", runDecisions[0].ContextSignal, "context signal")
	assertEqual(t, 1250, runDecisions[0].TotalTokens, "total tokens")
	assertEqual(t, 2000, runDecisions[0].ContextWindowTokens, "context window tokens")

	attemptDecisions, err := store.ListRuntimeSupervisorDecisionsForAttempt("attempt-2")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(attemptDecisions), "attempt decision count")
	assertEqual(t, "decision-1", attemptDecisions[0].DecisionID, "attempt decision id")
}

func TestRunsInspectIncludesTurns(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eventPath := filepath.Join(t.TempDir(), "attempt.events.jsonl")
	rawLogPath := filepath.Join(t.TempDir(), "attempt.raw.log")
	promptPath := filepath.Join(t.TempDir(), "attempt.prompt.md")
	statusPath := filepath.Join(t.TempDir(), "attempt.status.json")
	if err := NewEventLog(eventPath).Append("turn_started", "attempt-1", RunnerCodex, map[string]any{"turn_id": "turn-1"}); err != nil {
		t.Fatal(err)
	}
	if err := NewEventLog(eventPath).Append("turn_completed", "attempt-1", RunnerCodex, map[string]any{"turn_id": "turn-1"}); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodex),
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeBlocked),
		ActiveAttemptID: "attempt-1",
		WorkspacePath:   "/workspace",
		SessionRef:      "session-1",
		PromptPath:      promptPath,
		EventSinkPath:   eventPath,
		RawLogPath:      rawLogPath,
		StatusPath:      statusPath,
		WorkRevision:    2,
		AttemptCount:    1,
		LastError:       "authentication failed: invalid token",
		StartedAt:       "2026-04-28T01:00:00Z",
		UpdatedAt:       "2026-04-28T01:02:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID:     "attempt-1",
		ProjectID:     "project-1",
		RecordID:      "record-1",
		ItemID:        "ITEM-1",
		Runner:        string(RunnerCodex),
		WorkRevision:  2,
		WorkspacePath: "/workspace",
		SessionRef:    "session-1",
		Outcome:       string(AttemptOutcomeBlocked),
		PromptPath:    promptPath,
		EventSinkPath: eventPath,
		RawLogPath:    rawLogPath,
		StatusPath:    statusPath,
		LastError:     "authentication failed: invalid token",
		StartedAt:     "2026-04-28T01:00:00Z",
		FinishedAt:    "2026-04-28T01:02:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{
		AttemptID:    "attempt-1",
		ProjectID:    "project-1",
		RecordID:     "record-1",
		TurnID:       "turn-1",
		TurnIndex:    0,
		SessionRef:   "session-1",
		Status:       "completed",
		InputTokens:  12,
		OutputTokens: 8,
		TotalTokens:  20,
		LastEventAt:  "2026-04-28T01:01:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{
		AttemptID:   "attempt-1",
		ProjectID:   "project-1",
		RecordID:    "record-1",
		TurnID:      "turn-2",
		TurnIndex:   1,
		SessionRef:  "session-1",
		Status:      "running",
		InputTokens: 5,
		TotalTokens: 5,
		LastEventAt: "2026-04-28T01:02:30Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{
		ProjectID:      "project-1",
		RecordID:       "record-1",
		Runner:         string(RunnerCodex),
		SessionRef:     "session-1",
		LastMessageRef: "msg-1",
		WorkspacePath:  "/workspace",
		CurrentItemID:  "ITEM-1",
		WorkRevision:   2,
		LastAttemptID:  "attempt-1",
		State:          "blocked",
		Resumable:      true,
		StartedAt:      "2026-04-28T01:00:00Z",
		LastSeenAt:     "2026-04-28T01:02:00Z",
		LastError:      "authentication failed: invalid token",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveRuntimeSupervisorDecision(RuntimeSupervisorDecision{
		DecisionID:       "decision-1",
		ProjectID:        "project-1",
		RecordID:         "record-1",
		AttemptID:        "attempt-1",
		ParentAttemptID:  "attempt-0",
		SessionRef:       "session-1",
		ParentSessionRef: "session-0",
		Kind:             string(SupervisorDecisionResumeSession),
		Reason:           "daemon restart with compatible native session",
		ContextSignal:    "restart_recovery",
		TotalTokens:      20,
		CreatedAt:        "2026-04-28T01:03:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	jsonOutput := captureStdout(t, func() {
		if err := runsInspectCmd(Args{"id": "ITEM-1", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Run                      *RunStatus                  `json:"run"`
		Attempts                 []RunAttempt                `json:"attempts"`
		Turns                    []RunTurn                   `json:"turns"`
		ActiveTurn               *RunTurn                    `json:"active_turn"`
		LatestTurn               *RunTurn                    `json:"latest_turn"`
		Sessions                 []RunnerSession             `json:"sessions"`
		LatestSession            *RunnerSession              `json:"latest_session"`
		SupervisorDecisions      []RuntimeSupervisorDecision `json:"supervisor_decisions"`
		LatestSupervisorDecision *RuntimeSupervisorDecision  `json:"latest_supervisor_decision"`
		LatestEvent              map[string]any              `json:"latest_event"`
		TokenTotals              runtimeTokenTotals          `json:"token_totals"`
		FailureClass             string                      `json:"failure_class"`
		Paths                    runtimeArtifactPaths        `json:"paths"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ITEM-1", payload.Run.ItemID, "inspect json run")
	assertEqual(t, 1, len(payload.Attempts), "inspect json attempt count")
	assertEqual(t, 2, len(payload.Turns), "inspect json turn count")
	assertEqual(t, "turn-1", payload.Turns[0].TurnID, "inspect json turn id")
	assertEqual(t, "turn-2", payload.ActiveTurn.TurnID, "inspect json active turn")
	assertEqual(t, "turn-2", payload.LatestTurn.TurnID, "inspect json latest turn")
	assertEqual(t, 1, len(payload.Sessions), "inspect json session count")
	assertEqual(t, "session-1", payload.LatestSession.SessionRef, "inspect json latest session")
	assertEqual(t, 25, payload.TokenTotals.TotalTokens, "inspect json token totals")
	assertEqual(t, "auth", payload.FailureClass, "inspect json failure class")
	assertEqual(t, eventPath, payload.Paths.Events, "inspect json event path")
	assertEqual(t, rawLogPath, payload.Paths.RawLog, "inspect json raw log path")
	assertEqual(t, "turn_completed", stringValue(payload.LatestEvent["kind"]), "inspect json latest event")
	assertEqual(t, 1, len(payload.SupervisorDecisions), "inspect json supervisor decision count")
	assertEqual(t, string(SupervisorDecisionResumeSession), payload.SupervisorDecisions[0].Kind, "inspect json supervisor decision kind")
	assertEqual(t, "session-0", payload.SupervisorDecisions[0].ParentSessionRef, "inspect json parent session")
	assertEqual(t, "decision-1", payload.LatestSupervisorDecision.DecisionID, "inspect json latest supervisor decision")

	textOutput := captureStdout(t, func() {
		if err := runsInspectCmd(Args{"id": "ITEM-1"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(textOutput, "turns=2 latest=turn-2 status=running") {
		t.Fatalf("expected non-json inspect to summarize latest turn, got:\n%s", textOutput)
	}
	if !strings.Contains(textOutput, "tokens total=25 input=17 output=8") || !strings.Contains(textOutput, "failure_class=auth") {
		t.Fatalf("expected non-json inspect to summarize token totals and failure class, got:\n%s", textOutput)
	}
	if !strings.Contains(textOutput, "latest supervisor decision=resume_session reason=\"daemon restart with compatible native session\"") {
		t.Fatalf("expected non-json inspect to summarize latest supervisor decision, got:\n%s", textOutput)
	}
	if !strings.Contains(textOutput, "events="+eventPath) || !strings.Contains(textOutput, "latest event=turn_completed") {
		t.Fatalf("expected non-json inspect to summarize event path and latest event, got:\n%s", textOutput)
	}
}

func TestRunsEventsTailsStructuredEventSink(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eventPath := filepath.Join(t.TempDir(), "attempt.events.jsonl")
	if err := NewEventLog(eventPath).Append("turn_started", "attempt-1", RunnerCodex, map[string]any{"turn_id": "turn-1"}); err != nil {
		t.Fatal(err)
	}
	if err := NewEventLog(eventPath).Append("turn_completed", "attempt-1", RunnerCodex, map[string]any{"turn_id": "turn-1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodex),
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeSucceeded),
		ActiveAttemptID: "attempt-1",
		EventSinkPath:   eventPath,
		AttemptCount:    1,
		StartedAt:       "2026-04-28T01:00:00Z",
		UpdatedAt:       "2026-04-28T01:02:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	jsonOutput := captureStdout(t, func() {
		if err := runsEventsCmd(Args{"id": "ITEM-1", "lines": "1", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Run         *RunStatus       `json:"run"`
		EventPath   string           `json:"event_path"`
		LatestEvent map[string]any   `json:"latest_event"`
		Events      []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ITEM-1", payload.Run.ItemID, "events json run")
	assertEqual(t, eventPath, payload.EventPath, "events json event path")
	assertEqual(t, "turn_completed", stringValue(payload.LatestEvent["kind"]), "events json latest event")
	assertEqual(t, 1, len(payload.Events), "event tail count")
	assertEqual(t, "turn_completed", stringValue(payload.Events[0]["kind"]), "event tail kind")
}
