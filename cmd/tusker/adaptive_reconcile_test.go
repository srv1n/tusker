package main

import (
	"testing"
	"time"
)

func TestProjectInteractionKeepsProjectActiveForWindow(t *testing.T) {
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	d := &Daemon{}
	if d.recentProjectActivity("app", now) || d.adaptiveReconcileStatus("app").Tier != "idle" {
		t.Fatal("a project nobody touched must be idle")
	}
	d.noteProjectActivity("app", "cli_mutation", now)
	status := d.adaptiveReconcileStatus("app")
	if !d.recentProjectActivity("app", now.Add(projectInteractionWindow-time.Second)) ||
		status.Tier != "active" || status.CadenceMS != reconcileLiveCadence.Milliseconds() || status.NextDueAt != "" {
		t.Fatalf("interaction did not make the project active: %#v", status)
	}
	if d.recentProjectActivity("app", now.Add(projectInteractionWindow)) {
		t.Fatal("interaction kept the project active past its window")
	}
}

func TestRuntimeRunMakesProjectActive(t *testing.T) {
	for _, state := range []LeaseState{LeaseStateUnclaimed, LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued, LeaseStateInterrupted} {
		if !runtimeRunNeedsHotReconcileAt(RunStatus{LeaseState: string(state)}, time.Now().UTC()) {
			t.Fatalf("%s must keep its project active", state)
		}
	}
	if runtimeRunNeedsHotReconcileAt(RunStatus{LeaseState: string(LeaseStateUnclaimed), LastError: "automation plan do_not_dispatch: disabled"}, time.Now().UTC()) {
		t.Fatal("policy-blocked unclaimed work must not keep its project active")
	}
	if !runtimeRunNeedsHotReconcileAt(RunStatus{
		Lane: runLaneReview, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeWaitingForReview),
		LastError: "review dispatch blocked: dependency APP-T-0002 has not completed objective review (status review)",
	}, time.Now().UTC()) {
		t.Fatal("a review handoff waiting on an upstream DAG edge must keep its project active")
	}
	if !runtimeRunNeedsHotReconcileAt(RunStatus{
		Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeWaitingForReview),
		LastError: "review dispatch blocked: dependency APP-T-0002 has not completed objective review (status review)",
	}, time.Now().UTC()) {
		t.Fatal("a released review handoff waiting on an upstream DAG edge must keep its project active")
	}
	if !runtimeRunNeedsHotReconcileAt(RunStatus{
		Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded),
		LastError: "typed review result recorded; awaiting review reactor",
	}, time.Now().UTC()) {
		t.Fatal("a stored typed review result must keep the completion reactor's project active")
	}
	for _, state := range []LeaseState{LeaseStateReleased, LeaseStateParkedBudget, LeaseStateParkedNoProgress} {
		if runtimeRunNeedsHotReconcileAt(RunStatus{LeaseState: string(state)}, time.Now().UTC()) {
			t.Fatalf("%s must not keep its project active", state)
		}
	}
}

func TestRunWaitsForAdmissionOnlyForQueuedCapacityWaits(t *testing.T) {
	queued := RunStatus{LeaseState: string(LeaseStateUnclaimed)}
	for reason, want := range map[string]bool{
		"automation plan do_not_dispatch: waiting: machine busy (load 5.9 per CPU)": true,
		fairDispatchReasonPrefix + "project capacity reached (4/4)":                 true,
		"automation plan do_not_dispatch: wave W-0002 authorization is disarmed":    false,
		"automation plan do_not_dispatch: global active run limit reached (8/8)":    true,
		"": false,
	} {
		queued.LastError = reason
		if got := runWaitsForAdmission(queued); got != want {
			t.Fatalf("runWaitsForAdmission(%q) = %v, want %v", reason, got, want)
		}
	}
	queued.LastError = fairDispatchReasonPrefix + "x"
	queued.Terminal = true
	if runWaitsForAdmission(queued) {
		t.Fatal("terminal run must not keep its project hot")
	}
}
