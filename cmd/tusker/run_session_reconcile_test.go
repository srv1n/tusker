package main

import (
	"os"
	"testing"
	"time"
)

func TestRunSessionReconcileLiveOwnerReopensWithoutLaunching(t *testing.T) {
	stateRoot := t.TempDir()
	store, run, identity := seedRunSessionReconcile(t, stateRoot, true)
	t0 := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	if _, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: identity, Kind: "completed", Milestone: "checkpoint-1", Status: "completed", OccurredAt: t0.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: identity, Kind: "progress", Milestone: "checkpoint-2", NextMilestone: "checkpoint-3", Status: "working", OccurredAt: t0.Add(time.Minute).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EvaluateWorkerAttention(identity, time.Now().UTC().Add(time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	// seedWorkerRun creates the immutable execution-ledger identity. Add the
	// mutable attempt receipt after reopening so the legacy execution-ledger
	// backfill cannot project the same attempt a second time on this fixture.
	if err := reopened.SaveAttempt(RunAttempt{AttemptID: identity.AttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, WorkspacePath: run.WorkspacePath, SessionRef: identity.NativeSessionID, Outcome: string(AttemptOutcomeNone), StartedAt: run.StartedAt}); err != nil {
		t.Fatal(err)
	}
	launchChecks := 0
	reconciled, err := reopened.ReconcileRunSessionWithProbe(run, func(observed RunStatus) bool {
		launchChecks++
		return observed.ProcessPID == os.Getpid() && observed.ActiveAttemptID == identity.AttemptID
	})
	if err != nil {
		t.Fatal(err)
	}
	if launchChecks != 1 {
		t.Fatalf("reopen should perform one identity observation, got %d", launchChecks)
	}
	if reconciled.State != runSessionStateLive || reconciled.Process.State != "live" || !reconciled.Process.Verified {
		t.Fatalf("live owner was not reconstructed: %#v", reconciled)
	}
	if !reconciled.Reconnectable || reconciled.Identity == nil || reconciled.Identity.AttemptID != identity.AttemptID || reconciled.Session == nil || reconciled.Session.SessionRef != identity.NativeSessionID {
		t.Fatalf("current attempt/session identity was not retained: %#v", reconciled)
	}
	if reconciled.LastCompletedCheckpoint == nil || reconciled.LastCompletedCheckpoint.Milestone != "checkpoint-1" {
		t.Fatalf("last completed checkpoint was not retained: %#v", reconciled.LastCompletedCheckpoint)
	}
	if reconciled.UnresolvedOperation == nil || reconciled.UnresolvedOperation.Milestone != "checkpoint-2" {
		t.Fatalf("unfinished operation was not retained: %#v", reconciled.UnresolvedOperation)
	}
	if reconciled.Attention == nil || !reconciled.Attention.AttentionRequired {
		t.Fatalf("persisted worker attention was not retained: %#v", reconciled.Attention)
	}
	if !reconciled.Capability.Supported || reconciled.Capability.Command != "" || reconciled.Capability.Reason == "" {
		t.Fatalf("native resume capability was not reconstructed: %#v", reconciled.Capability)
	}
	attempts, err := reopened.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("observation must not create a worker attempt: %#v", attempts)
	}
}

func TestRunSessionReconcileTerminalReceiptAndUnknownOwner(t *testing.T) {
	t.Run("dead owner with receipt is terminal", func(t *testing.T) {
		store, run, _ := seedRunSessionReconcile(t, t.TempDir(), false)
		defer store.Close()
		run.AttemptOutcome = string(AttemptOutcomeSucceeded)
		run.Terminal = false
		run.ProcessPID = 999999
		run.ProcessPGID = 999999
		run.ProcessStartedAt = "2020-01-01T00:00:00Z"
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveAttempt(RunAttempt{AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, WorkRevision: run.WorkRevision, Outcome: string(AttemptOutcomeSucceeded), StartedAt: run.StartedAt, FinishedAt: "2026-09-22T10:01:00Z"}); err != nil {
			t.Fatal(err)
		}
		reconciled, err := store.ReconcileRunSessionWithProbe(run, func(RunStatus) bool { return false })
		if err != nil {
			t.Fatal(err)
		}
		if reconciled.State != runSessionStateTerminal || !reconciled.Terminal || reconciled.TerminalOutcome != string(AttemptOutcomeSucceeded) || reconciled.Reconnectable {
			t.Fatalf("terminal receipt was not authoritative after owner death: %#v", reconciled)
		}
	})

	t.Run("missing receipt and reused pid remain unknown", func(t *testing.T) {
		store, run, _ := seedRunSessionReconcile(t, t.TempDir(), false)
		defer store.Close()
		run.ProcessPID = os.Getpid()
		run.ProcessPGID = processGroupID(run.ProcessPID)
		run.ProcessStartedAt = "1900-01-01T00:00:00Z"
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		reconciled, err := store.ReconcileRunSession(run)
		if err != nil {
			t.Fatal(err)
		}
		if reconciled.State != runSessionStateUnknown || reconciled.Terminal || reconciled.Reconnectable {
			t.Fatalf("missing receipt or reused pid became runnable: %#v", reconciled)
		}
	})

	t.Run("terminal flag contradicting a live owner remains unknown", func(t *testing.T) {
		store, run, _ := seedRunSessionReconcile(t, t.TempDir(), true)
		defer store.Close()
		run.Terminal = true
		run.AttemptOutcome = string(AttemptOutcomeSucceeded)
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		reconciled, err := store.ReconcileRunSessionWithProbe(run, func(RunStatus) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		if reconciled.State != runSessionStateUnknown || reconciled.Reconnectable {
			t.Fatalf("contradictory live/terminal evidence became runnable: %#v", reconciled)
		}
	})
}

func TestRunSessionReconcileExcludesStaleGenerationEvidence(t *testing.T) {
	stateRoot := t.TempDir()
	store, _, oldIdentity := seedRunSessionReconcile(t, stateRoot, false)
	if _, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: oldIdentity, Kind: "progress", Milestone: "stale-generation", OccurredAt: "2026-09-22T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	currentIdentity := seedWorkerRun(t, store, "attempt-2", 2, 1, "devin", "devin-2")
	currentRun, err := store.FindRunScoped("project-1", "TASK-1")
	if err != nil || currentRun == nil {
		t.Fatalf("load current run: %#v %v", currentRun, err)
	}
	currentRun.Runner = string(RunnerCodexExec)
	currentRun.LeaseOwner = "owner-2"
	currentRun.SessionRef = currentIdentity.NativeSessionID
	currentRun.ProcessPID = 999999
	currentRun.ProcessPGID = 999999
	currentRun.ProcessStartedAt = "2020-01-01T00:00:00Z"
	if err := store.UpsertRun(*currentRun); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: currentIdentity, Kind: "progress", Milestone: "current-generation", OccurredAt: "2026-09-22T10:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	currentRun, err = reopened.FindRunScoped("project-1", "TASK-1")
	if err != nil || currentRun == nil {
		t.Fatalf("reload current run: %#v %v", currentRun, err)
	}
	reconciled, err := reopened.ReconcileRunSessionWithProbe(*currentRun, func(RunStatus) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.UnresolvedOperation == nil || reconciled.UnresolvedOperation.Milestone != "current-generation" {
		t.Fatalf("stale generation evidence leaked into the reopened projection: %#v", reconciled.UnresolvedOperation)
	}
	if reconciled.UnresolvedOperation.Identity.AttemptID != currentIdentity.AttemptID || reconciled.UnresolvedOperation.Stale {
		t.Fatalf("projection returned a stale identity: %#v", reconciled.UnresolvedOperation)
	}
}

func TestDaemonConsumesPendingStopWithoutSchedulingRetry(t *testing.T) {
	t.Run("settles dead exact owner", func(t *testing.T) {
		store, run, _ := seedRunSessionReconcile(t, t.TempDir(), false)
		defer store.Close()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := saveRunSessionControlIntent(store, runSessionControlIntent{
			ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
			Action: runSessionControlStop, State: runSessionControlUnknown,
			Reason: "stop acknowledgement is unresolved", LeaseGeneration: run.LeaseGeneration,
			LeaseOwner: run.LeaseOwner, AttemptID: run.ActiveAttemptID,
			ProcessPID: run.ProcessPID, ProcessPGID: run.ProcessPGID, ProcessStarted: run.ProcessStartedAt,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		d := &Daemon{store: store}
		reconciled, suppressRetry, changed, err := d.reconcilePersistedStopIntent(run)
		if err != nil {
			t.Fatal(err)
		}
		if !suppressRetry || !changed || reconciled.LeaseState != string(LeaseStateInterrupted) || reconciled.AttemptOutcome != string(AttemptOutcomeCancelled) || reconciled.NextRetryAt != "" {
			t.Fatalf("dead exact owner was not settled as interrupted: suppress=%v changed=%v run=%#v", suppressRetry, changed, reconciled)
		}
		settled, suppressRetry, changed, err := d.reconcilePersistedStopIntent(reconciled)
		if err != nil {
			t.Fatal(err)
		}
		if !suppressRetry || changed || settled.LeaseState != string(LeaseStateInterrupted) {
			t.Fatalf("interrupted run did not retain retry suppression: suppress=%v changed=%v run=%#v", suppressRetry, changed, settled)
		}
		intent, err := loadRunSessionControlIntent(store, run.ProjectID, run.RecordID)
		if err != nil || intent == nil || intent.State != runSessionControlSettledState {
			t.Fatalf("stop intent was not consumed: %#v %v", intent, err)
		}
	})

	t.Run("live exact owner remains untouched", func(t *testing.T) {
		store, run, _ := seedRunSessionReconcile(t, t.TempDir(), true)
		defer store.Close()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := saveRunSessionControlIntent(store, runSessionControlIntent{
			ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID,
			Action: runSessionControlStop, State: runSessionControlPending,
			Reason: "stop accepted; waiting for exact-owner settlement", LeaseGeneration: run.LeaseGeneration,
			LeaseOwner: run.LeaseOwner, AttemptID: run.ActiveAttemptID,
			ProcessPID: run.ProcessPID, ProcessPGID: run.ProcessPGID, ProcessStarted: run.ProcessStartedAt,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		d := &Daemon{store: store}
		reconciled, suppressRetry, changed, err := d.reconcilePersistedStopIntent(run)
		if err != nil {
			t.Fatal(err)
		}
		if !suppressRetry || changed || reconciled.LeaseState != run.LeaseState || reconciled.ActiveAttemptID != run.ActiveAttemptID {
			t.Fatalf("live exact owner was mutated or not suppressed: suppress=%v changed=%v run=%#v", suppressRetry, changed, reconciled)
		}
	})
}

func seedRunSessionReconcile(t *testing.T, stateRoot string, live bool) (*RuntimeStore, RunStatus, WorkerAttemptIdentity) {
	t.Helper()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	run, err := store.FindRunScoped("project-1", "TASK-1")
	if err != nil || run == nil {
		t.Fatalf("seed run lookup: %#v %v", run, err)
	}
	now := time.Now().UTC()
	run.Runner = string(RunnerCodexExec)
	run.LeaseState = string(LeaseStateRunning)
	run.LeaseOwner = "owner-1"
	run.LeaseExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	run.SessionRef = identity.NativeSessionID
	run.StartedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	run.UpdatedAt = now.Format(time.RFC3339Nano)
	if live {
		run.ProcessPID = os.Getpid()
		run.ProcessPGID = processGroupID(run.ProcessPID)
		run.ProcessStartedAt = recordedProcessStartTime(run.ProcessPID, now.Format(time.RFC3339))
	} else {
		run.ProcessPID = 999999
		run.ProcessPGID = 999999
		run.ProcessStartedAt = "2020-01-01T00:00:00Z"
	}
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: identity.NativeSessionID, WorkspacePath: run.WorkspacePath, CurrentItemID: run.ItemID, WorkRevision: run.WorkRevision, LastAttemptID: identity.AttemptID, State: "running", Resumable: true, StartedAt: run.StartedAt, LastSeenAt: run.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	return store, *run, identity
}
