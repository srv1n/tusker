package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRuntimeBudgetConfigMigratesEnabledConfigurationToDisabled(t *testing.T) {
	legacy := RuntimeBudgetConfig{Enabled: true, DailyInputTokens: 1}
	budget := withDefaultRuntimeBudgetConfig(legacy)
	assertEqual(t, false, budget.Enabled, "legacy enabled configuration is disabled")
	assertEqual(t, defaultBudgetDailyOutputTokens, budget.DailyOutputTokens, "legacy configuration remains structurally valid")
}

func TestBudgetTelemetryCannotBlockDispatchOrEraseHistory(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err := store.SaveTurn(RunTurn{ProjectID: "project-1", RecordID: "APP-T-0001", AttemptID: "attempt-1", TurnID: "turn-1", InputTokens: int(^uint(0) >> 1), OutputTokens: int(^uint(0) >> 1), LastEventAt: now.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{store: store}
	wf := defaultWorkflow()
	wf.Runtime.Budget.Enabled = true
	run := RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1"}
	updated, changed, err := daemon.enforceBudgetForRun(context.Background(), RegisteredProject{ProjectID: "project-1"}, wf, Note{}, run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, changed, "telemetry does not pause a live run")
	assertEqual(t, run.LeaseState, updated.LeaseState, "live run remains live")
	_, reason, _, err := daemon.budgetDispatchBlocker(RegisteredProject{ProjectID: "project-1"}, wf, Note{}, run, now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", reason, "telemetry does not block dispatch")
	turns, err := store.ListTurnsForAttempt("attempt-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(turns), "raw usage history is preserved")
	status, err := store.BudgetCircuitStatus(wf, now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, status.Open, "legacy counters cannot open a circuit")
}

func TestLegacyBudgetParkIsReleasedForNormalDispatch(t *testing.T) {
	parked := releaseLegacyBudgetPark(RunStatus{LeaseState: string(LeaseStateParkedBudget), AttemptOutcome: string(AttemptOutcomeBudgetExceeded), Terminal: true, ActiveAttemptID: "attempt-1"}, time.Now())
	assertEqual(t, string(LeaseStateUnclaimed), parked.LeaseState, "legacy budget park becomes dispatchable")
	assertEqual(t, false, parked.Terminal, "legacy budget park is not terminal")
	assertEqual(t, string(AttemptOutcomeNone), parked.AttemptOutcome, "new dispatch starts without a budget outcome")
}
