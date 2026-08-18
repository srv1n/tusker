package main

import (
	"testing"
	"time"
)

func TestRuntimeBudgetConfigMigratesEnabledConfigurationToDisabled(t *testing.T) {
	legacy := RuntimeBudgetConfig{Enabled: true, DailyInputTokens: 1}
	budget := withDefaultRuntimeBudgetConfig(legacy)
	assertEqual(t, false, budget.Enabled, "legacy enabled configuration is disabled")
	assertEqual(t, defaultBudgetDailyOutputTokens, budget.DailyOutputTokens, "legacy configuration remains structurally valid")
}

func TestLegacyBudgetParkIsReleasedForNormalDispatch(t *testing.T) {
	parked := releaseLegacyBudgetPark(RunStatus{LeaseState: string(LeaseStateParkedBudget), AttemptOutcome: string(AttemptOutcomeBudgetExceeded), Terminal: true, ActiveAttemptID: "attempt-1"}, time.Now())
	assertEqual(t, string(LeaseStateUnclaimed), parked.LeaseState, "legacy budget park becomes dispatchable")
	assertEqual(t, false, parked.Terminal, "legacy budget park is not terminal")
	assertEqual(t, string(AttemptOutcomeNone), parked.AttemptOutcome, "new dispatch starts without a budget outcome")
}
