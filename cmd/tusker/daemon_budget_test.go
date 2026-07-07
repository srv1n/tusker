package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBudgetAttemptBreach(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	wf := defaultWorkflow()
	wf.Runtime.Budget.PerAttemptInputTokens = 10
	wf.Runtime.Budget.PerAttemptOutputTokens = 10
	wf.Runtime.Budget.PerTaskInputTokens = 1000
	wf.Runtime.Budget.PerTaskOutputTokens = 1000
	project := RegisteredProject{ProjectID: "project-1"}
	note := Note{Data: map[string]any{"id": "APP-T-0001"}}
	run := RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-budget",
		AttemptCount:    1,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Outcome: string(AttemptOutcomeNone)}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{ProjectID: run.ProjectID, RecordID: run.RecordID, AttemptID: run.ActiveAttemptID, TurnID: "turn-1", InputTokens: 11, OutputTokens: 1, TotalTokens: 12, LastEventAt: "2026-07-06T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	updated, changed, err := daemon.enforceBudgetForRun(context.Background(), project, wf, note, run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, changed, "attempt budget changed")
	assertEqual(t, string(LeaseStateReleased), updated.LeaseState, "attempt budget lease")
	assertEqual(t, string(AttemptOutcomeBudgetExceeded), updated.AttemptOutcome, "attempt budget outcome")
	assertEqual(t, true, updated.Terminal, "attempt budget terminal")
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(AttemptOutcomeBudgetExceeded), attempts[0].Outcome, "attempt persisted budget outcome")
}

func TestBudgetTaskParkedBudgetRedrive(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	wf := defaultWorkflow()
	wf.Runtime.Budget.PerAttemptInputTokens = 1000
	wf.Runtime.Budget.PerAttemptOutputTokens = 1000
	wf.Runtime.Budget.PerTaskInputTokens = 20
	wf.Runtime.Budget.PerTaskOutputTokens = 20
	project := RegisteredProject{ProjectID: "project-1"}
	note := Note{Data: map[string]any{"id": "APP-T-0001"}}
	run := RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-budget",
		AttemptCount:    2,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{ProjectID: run.ProjectID, RecordID: run.RecordID, AttemptID: "attempt-1", TurnID: "turn-1", InputTokens: 15, TotalTokens: 15, LastEventAt: "2026-07-06T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{ProjectID: run.ProjectID, RecordID: run.RecordID, AttemptID: "attempt-2", TurnID: "turn-2", InputTokens: 10, TotalTokens: 10, LastEventAt: "2026-07-06T12:05:00Z"}); err != nil {
		t.Fatal(err)
	}
	parked, changed, err := daemon.enforceBudgetForRun(context.Background(), project, wf, note, run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, changed, "task budget changed")
	assertEqual(t, string(LeaseStateParkedBudget), parked.LeaseState, "task budget parked")
	assertEqual(t, string(AttemptOutcomeBudgetExceeded), parked.AttemptOutcome, "task budget outcome")
	assertEqual(t, true, parked.Terminal, "task budget terminal")
	if err := store.UpsertRun(parked); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	output := captureStdout(t, func() {
		if err := redriveCmd(Args{"id": "APP-T-0001", "by": "human:sarav", "reason": "approved more spend", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK    bool                `json:"ok"`
		Reset BudgetRedriveRecord `json:"reset"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "redrive ok")
	assertEqual(t, "human:sarav", payload.Reset.Actor, "redrive actor")
	assertEqual(t, "approved more spend", payload.Reset.Reason, "redrive reason")
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	redriven, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateRetryQueued), redriven.LeaseState, "redrive lease")
	reset, err := store.BudgetWindowStart(project.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if reset.ResetAt == "" {
		t.Fatalf("expected redrive reset timestamp")
	}
}

func TestBudgetCircuitOpen(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	wf := defaultWorkflow()
	wf.Runtime.Budget.DailyInputTokens = 20
	wf.Runtime.Budget.DailyOutputTokens = 20
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if err := store.SaveTurn(RunTurn{ProjectID: "project-1", RecordID: "APP-T-0001", AttemptID: "attempt-1", TurnID: "turn-1", InputTokens: 21, TotalTokens: 21, LastEventAt: now.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	status, err := store.BudgetCircuitStatus(wf, now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, status.Open, "budget circuit open")
	if !strings.Contains(status.Reason, "daily input token budget exceeded") {
		t.Fatalf("expected daily budget reason, got %#v", status)
	}
	if err := store.SetBudgetCircuitStatus(status); err != nil {
		t.Fatal(err)
	}
	daemonStatus, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, boolFromAny(daemonStatus["budget_circuit_open"]), "daemon status circuit")
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	run := RunStatus{ProjectID: "project-1", RecordID: "APP-T-0002", ItemID: "APP-T-0002", LeaseState: string(LeaseStateUnclaimed)}
	_, reason, changed, err := daemon.budgetDispatchBlocker(RegisteredProject{ProjectID: "project-1"}, wf, Note{Data: map[string]any{"id": "APP-T-0002"}}, run, now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, changed, "circuit does not park")
	if !strings.Contains(reason, "budget circuit open") {
		t.Fatalf("expected circuit dispatch blocker, got %q", reason)
	}
}

func TestBudgetSingleEnforcementSiteBudgetOverrideCap(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Budget override", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"budget": map[string]any{
			"per_attempt_input_tokens": 9999999999,
			"per_task_input_tokens":    9999999999,
		},
	})
	project := registerAutomationTestProject(t, vault)
	ctx, err := loadAutomationCommandContext(Args{"vault": vault})
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Close()
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	budget := resolveRunBudget(ctx.Workflow.Data, note)
	assertEqual(t, ctx.Workflow.Data.Runtime.Budget.PerAttemptInputTokens*4, budget.PerAttemptInputTokens, "attempt override capped")
	assertEqual(t, ctx.Workflow.Data.Runtime.Budget.PerTaskInputTokens*4, budget.PerTaskInputTokens, "task override capped")
	if err := ctx.Store.SaveTurn(RunTurn{ProjectID: project.ProjectID, RecordID: "APP-T-0001", AttemptID: "attempt-1", TurnID: "turn-1", InputTokens: budget.PerTaskInputTokens + 1, TotalTokens: budget.PerTaskInputTokens + 1, LastEventAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	explanation := ctx.explainTask(note)
	if explanation.Dispatchable {
		t.Fatalf("expected automation plan to block on shared budget path: %#v", explanation)
	}
	if !strings.Contains(strings.Join(explanation.Blockers, "; "), "task input token budget exceeded") {
		t.Fatalf("expected budget blocker in automation explanation, got %#v", explanation.Blockers)
	}
}

func TestBudgetRestartNoDoubleCount(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{ProjectID: "project-1", RecordID: "APP-T-0001", AttemptID: "attempt-1", TurnID: "turn-1", InputTokens: 10, OutputTokens: 2, TotalTokens: 12, LastEventAt: "2026-07-06T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurn(RunTurn{ProjectID: "project-1", RecordID: "APP-T-0001", AttemptID: "attempt-1", TurnID: "turn-1", InputTokens: 10, OutputTokens: 2, TotalTokens: 12, LastEventAt: "2026-07-06T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	first, err := store.SumRunTokens("project-1", "APP-T-0001", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	second, err := store.SumRunTokens("project-1", "APP-T-0001", "")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, first.InputTokens, second.InputTokens, "restart input tokens")
	assertEqual(t, 10, second.InputTokens, "idempotent turn upsert")
	assertEqual(t, 2, second.OutputTokens, "idempotent output tokens")
}
