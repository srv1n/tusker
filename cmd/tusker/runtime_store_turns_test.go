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
		BranchName:          "story/record-1",
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

	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodex),
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		SessionRef:      "session-1",
		AttemptCount:    1,
		StartedAt:       "2026-04-28T01:00:00Z",
		UpdatedAt:       "2026-04-28T01:02:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{
		AttemptID:   "attempt-1",
		ProjectID:   "project-1",
		RecordID:    "record-1",
		TurnID:      "turn-1",
		TurnIndex:   0,
		SessionRef:  "session-1",
		Status:      "completed",
		TotalTokens: 20,
		LastEventAt: "2026-04-28T01:01:00Z",
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
		Turns               []RunTurn                   `json:"turns"`
		SupervisorDecisions []RuntimeSupervisorDecision `json:"supervisor_decisions"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(payload.Turns), "inspect json turn count")
	assertEqual(t, "turn-1", payload.Turns[0].TurnID, "inspect json turn id")
	assertEqual(t, 1, len(payload.SupervisorDecisions), "inspect json supervisor decision count")
	assertEqual(t, string(SupervisorDecisionResumeSession), payload.SupervisorDecisions[0].Kind, "inspect json supervisor decision kind")
	assertEqual(t, "session-0", payload.SupervisorDecisions[0].ParentSessionRef, "inspect json parent session")

	textOutput := captureStdout(t, func() {
		if err := runsInspectCmd(Args{"id": "ITEM-1"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(textOutput, "turns=1 latest=turn-1 status=completed") {
		t.Fatalf("expected non-json inspect to summarize latest turn, got:\n%s", textOutput)
	}
	if !strings.Contains(textOutput, "latest supervisor decision=resume_session reason=\"daemon restart with compatible native session\"") {
		t.Fatalf("expected non-json inspect to summarize latest supervisor decision, got:\n%s", textOutput)
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
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(payload.Events), "event tail count")
	assertEqual(t, "turn_completed", stringValue(payload.Events[0]["kind"]), "event tail kind")
}
