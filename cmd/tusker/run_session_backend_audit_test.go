package main

import (
	"os"
	"testing"
)

func TestRunSessionReconcileUsesCanonicalProcessIdentity(t *testing.T) {
	store, run, _ := seedRunSessionReconcile(t, t.TempDir(), true)
	defer store.Close()
	canonical := run
	canonical.ProcessPID = 999999
	canonical.ProcessPGID = 999999
	canonical.ProcessStartedAt = "2000-01-01T00:00:00Z"
	if err := store.UpsertRun(canonical); err != nil {
		t.Fatal(err)
	}
	reconciled, err := store.ReconcileRunSessionWithProbe(run, func(observed RunStatus) bool {
		return observed.ProcessPID == os.Getpid()
	})
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != runSessionStateUnknown || reconciled.Process.State != "unknown" {
		t.Fatalf("stale input process identity was treated as canonical: %#v", reconciled)
	}
}

func TestRunSessionReconcileRequiresSessionAttemptIdentity(t *testing.T) {
	store, run, identity := seedRunSessionReconcile(t, t.TempDir(), true)
	defer store.Close()
	if err := store.SaveSession(RunnerSession{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner,
		SessionRef: identity.NativeSessionID, WorkspacePath: run.WorkspacePath,
		CurrentItemID: run.ItemID, WorkRevision: run.WorkRevision,
		State: "running", Resumable: true, StartedAt: run.StartedAt,
	}); err != nil {
		t.Fatal(err)
	}
	reconciled, err := store.ReconcileRunSessionWithProbe(run, func(RunStatus) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.Session != nil || reconciled.Reconnectable {
		t.Fatalf("session without exact attempt lineage became reconnectable: %#v", reconciled)
	}
}

func TestResolveResumeSessionHonorsFreshSessionDecisions(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, marker := range []string{outcomeUnknownContextRecoveryReasonPrefix + "attempt-old", runSessionControlFreshReasonPrefix + "human:test"} {
		if err := store.SaveSession(RunnerSession{
			ProjectID: "project-1", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec),
			SessionRef: "session-old", WorkspacePath: t.TempDir(), WorkRevision: 1,
			LastAttemptID: "attempt-old", State: "released", Resumable: true,
		}); err != nil {
			t.Fatal(err)
		}
		resolved, err := (&Daemon{store: store}).resolveResumeSession(
			RegisteredProject{ProjectID: "project-1"},
			Note{Data: map[string]any{"status": "ready"}},
			RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", Runner: string(RunnerCodexExec),
				LeaseState: string(LeaseStateRetryQueued), AttemptCount: 1, WorkRevision: 1,
				LastError: marker},
		)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.SessionRef != "" {
			t.Fatalf("%q silently reused historical session: %#v", marker, resolved)
		}
	}
}

func TestRunSessionControlStopDoesNotRewriteTerminalRun(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	run := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded),
		LeaseGeneration: 4, Terminal: true,
	}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	var result runSessionControlResult
	servePost(t, server, "/api/runs/APP-T-0001/control?project=app", `{"action":"stop"}`, &result)
	if !result.Refused || !result.Settled || result.State != runSessionControlSettledState {
		t.Fatalf("terminal stop should be an explicit no-op: %#v", result)
	}
	stored, err := server.store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.AttemptOutcome != string(AttemptOutcomeSucceeded) || !stored.Terminal || stored.LeaseState != run.LeaseState {
		t.Fatalf("terminal stop rewrote canonical outcome: %#v", stored)
	}
}
