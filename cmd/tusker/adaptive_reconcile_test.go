package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAdaptiveReconcileCadenceBacksOffAndActivityResetsHot(t *testing.T) {
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	d := &Daemon{}
	d.recordProjectPoll("app", now, false)
	assertAdaptiveState(t, d, "app", "hot", reconcileFullCadence, now.Add(reconcileFullCadence))

	d.recordProjectPoll("app", now.Add(time.Minute), false)
	assertAdaptiveState(t, d, "app", "warm", reconcileWarmCadence, now.Add(time.Minute+reconcileWarmCadence))

	d.recordProjectPoll("app", now.Add(6*time.Minute), false)
	assertAdaptiveState(t, d, "app", "cool", reconcileFullCadence, now.Add(6*time.Minute+reconcileFullCadence))

	d.recordProjectPoll("app", now.Add(16*time.Minute), false)
	assertAdaptiveState(t, d, "app", "cold", reconcileFullCadence, now.Add(16*time.Minute+reconcileFullCadence))

	d.noteProjectActivity("app", "cli_mutation", now.Add(20*time.Minute))
	assertAdaptiveState(t, d, "app", "hot", reconcileFullCadence, now.Add(25*time.Minute))
}

func TestAdaptiveReconcileLiveRuntimeNeverGoesCold(t *testing.T) {
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	d := &Daemon{}
	d.recordProjectPoll("app", now, false)
	d.recordProjectPoll("app", now.Add(24*time.Hour), true)
	assertAdaptiveState(t, d, "app", "live", reconcileLiveCadence, now.Add(24*time.Hour+reconcileLiveCadence))

	for _, state := range []LeaseState{LeaseStateUnclaimed, LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued, LeaseStateInterrupted} {
		if !runtimeRunNeedsHotReconcileAt(RunStatus{LeaseState: string(state)}, time.Now().UTC()) {
			t.Fatalf("%s must keep adaptive reconciliation live", state)
		}
	}
	if runtimeRunNeedsHotReconcileAt(RunStatus{LeaseState: string(LeaseStateUnclaimed), LastError: "automation plan do_not_dispatch: disabled"}, time.Now().UTC()) {
		t.Fatal("policy-blocked unclaimed work must be allowed to back off")
	}
	if !runtimeRunNeedsHotReconcileAt(RunStatus{
		Lane: runLaneReview, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeWaitingForReview),
		LastError: "review dispatch blocked: dependency APP-T-0002 has not completed objective review (status review)",
	}, time.Now().UTC()) {
		t.Fatal("a review handoff waiting on an upstream DAG edge must keep reconciliation live")
	}
	if !runtimeRunNeedsHotReconcileAt(RunStatus{
		Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeWaitingForReview),
		LastError: "review dispatch blocked: dependency APP-T-0002 has not completed objective review (status review)",
	}, time.Now().UTC()) {
		t.Fatal("a released review handoff waiting on an upstream DAG edge must keep reconciliation live")
	}
	if !runtimeRunNeedsHotReconcileAt(RunStatus{
		Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded),
		LastError: "typed review result recorded; awaiting review reactor",
	}, time.Now().UTC()) {
		t.Fatal("a stored typed review result must keep the completion reactor live")
	}
	for _, state := range []LeaseState{LeaseStateReleased, LeaseStateParkedBudget, LeaseStateParkedNoProgress} {
		if runtimeRunNeedsHotReconcileAt(RunStatus{LeaseState: string(state)}, time.Now().UTC()) {
			t.Fatalf("%s must be allowed to back off", state)
		}
	}
}

func TestAdaptiveReconcilePreservesGlobalHotIntervalOverride(t *testing.T) {
	t.Setenv(daemonPollIntervalEnv, "45000")
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	d := &Daemon{}
	d.recordProjectPoll("app", now, false)
	assertAdaptiveState(t, d, "app", "hot", 45*time.Second, now.Add(45*time.Second))
}

func TestHintedPollKeepsFullScanDeadlineAndOverdueReadsBackOff(t *testing.T) {
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	d := &Daemon{frontierScannedAt: map[string]time.Time{"app": now}}
	d.recordProjectPoll("app", now, false)
	hintedAt := now.Add(4 * time.Minute)
	d.noteProjectActivity("app", "cli_mutation", hintedAt)
	assertAdaptiveState(t, d, "app", "hot", reconcileFullCadence, now.Add(reconcileFullCadence))
	d.recordProjectPoll("app", hintedAt, false)
	assertAdaptiveState(t, d, "app", "hot", reconcileFullCadence, now.Add(reconcileFullCadence))
	d.recordProjectPoll("app", hintedAt, true)
	assertAdaptiveState(t, d, "app", "live", reconcileLiveCadence, hintedAt.Add(reconcileLiveCadence))

	// A failed/skipped full read leaves its old scan timestamp intact. It must
	// retain bounded retries rather than repeatedly return an already-due time.
	overdueAt := now.Add(reconcileFullCadence + time.Second)
	d.noteProjectActivity("app", "cli_mutation", overdueAt)
	d.recordProjectPoll("app", overdueAt, false)
	assertAdaptiveState(t, d, "app", "hot", reconcileFullCadence, overdueAt.Add(reconcileFullCadence))
	d.recordProjectPoll("app", overdueAt, true)
	assertAdaptiveState(t, d, "app", "live", reconcileLiveCadence, overdueAt.Add(reconcileLiveCadence))
}

func TestAdaptiveProjectsDueIsIndependentPerProjectAndRestartStartsHot(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []string{"alpha", "beta"} {
		if err := store.UpsertProject(RegisteredProject{ProjectID: id, ProjectKey: id, Name: id, RepoRoot: filepath.Join(t.TempDir(), id), VaultRoot: filepath.Join(t.TempDir(), id, ".tusker"), Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	d := &Daemon{store: store}
	d.recordProjectPoll("alpha", now, false)
	d.recordProjectPoll("beta", now, false)
	d.noteProjectActivity("beta", "serve_attention", now.Add(30*time.Second))

	due, wait, err := d.adaptiveProjectsDue(now.Add(reconcileFullCadence))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0] != "alpha" {
		t.Fatalf("expected only alpha due, got %#v", due)
	}
	if wait != 0 {
		t.Fatalf("due project should produce zero wait, got %s", wait)
	}

	restarted := &Daemon{store: store}
	due, wait, err = restarted.adaptiveProjectsDue(now.Add(24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 0 || wait != reconcileFullCadence {
		t.Fatalf("restart must initialize projects hot, due=%#v wait=%s", due, wait)
	}
}

func assertAdaptiveState(t *testing.T, d *Daemon, projectID, tier string, cadence time.Duration, next time.Time) {
	t.Helper()
	status := d.adaptiveReconcileStatus(projectID)
	if status.Tier != tier || status.CadenceMS != cadence.Milliseconds() || status.NextDueAt != next.Format(time.RFC3339Nano) {
		t.Fatalf("adaptive state mismatch: %#v", status)
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
