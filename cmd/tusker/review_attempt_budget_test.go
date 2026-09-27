package main

import (
	"context"
	"strings"
	"testing"
)

func TestReviewDispatchGetsFreshAttemptBudgetAfterExecuteCap(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	setDirectEmergencyProfileForAutomationTest(t, env.vault)
	setAllEligibleDispatchScopeForAutomationTest(t, env.vault)
	if _, err := setProjectLocalConfigWithReadback(env.vault, "automation.enabled", true); err != nil {
		t.Fatal(err)
	}
	installCodexSleepShimForTest(t)

	run := env.run
	run.Lane = runLaneExecute
	run.Runner = string(RunnerCodexExec)
	run.RunnerProfile = ""
	run.RunnerHarness = ""
	run.LeaseState = string(LeaseStateReleased)
	run.AttemptOutcome = string(AttemptOutcomeWaitingForReview)
	run.ActiveAttemptID = "exec-1"
	run.AttemptCount = env.wfFile.Data.Retry.MaxAttempts
	if err := env.daemon.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := env.daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := env.latestRun(t)
	defer killRunProcess(after)
	if after.Lane != runLaneReview || after.AttemptCount != 1 || !isDispatchingLeaseState(after.LeaseState) || strings.Contains(after.LastError, "attempt cap reached") {
		t.Fatalf("execute cap blocked review dispatch: %#v", after)
	}
}
