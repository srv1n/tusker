package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHandRunNeverReclaimedByExpiry(t *testing.T) {
	now := time.Now().UTC()
	store, run := ownershipStoreFixture(t, "APP-T-HAND-1")
	run.LeaseState = string(LeaseStateClaimed)
	run.LeaseOwner = "agent:editor"
	run.LeaseGeneration = 1
	run.LeaseExpiresAt = now.Add(-10 * time.Minute).Format(time.RFC3339)
	run.ActiveAttemptID = "work-hand-1"
	run.ProcessPID, run.ProcessPGID = 99999999, 99999999
	run.ProcessStartedAt = now.Add(-11 * time.Minute).Format(time.RFC3339)
	run.HandRun = true
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	// UpsertRun preserves claim origin; emulate the stamp written by ClaimRunLease.
	if _, err := store.db.Exec(`UPDATE runs SET hand_run = 1 WHERE project_id = ? AND record_id = ?`, run.ProjectID, run.RecordID); err != nil {
		t.Fatal(err)
	}
	load := func() RunStatus {
		t.Helper()
		current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
		if err != nil || current == nil {
			t.Fatalf("load hand run: %#v %v", current, err)
		}
		return *current
	}
	assertHeld := func(label string) {
		t.Helper()
		current := load()
		if current.LeaseState != run.LeaseState || current.LeaseOwner != run.LeaseOwner || current.LeaseGeneration != run.LeaseGeneration || current.ActiveAttemptID != run.ActiveAttemptID {
			t.Fatalf("%s changed hand-run owner: %#v", label, current)
		}
	}
	current := load()
	if !current.HandRun || !current.HandRunStamped {
		t.Fatalf("hand_run not persisted: %#v", current)
	}
	if changed, err := store.ReclaimExpiredRunLease(run.ProjectID, run.RecordID, now, defaultRunLeaseTTL, "expired"); err != nil || changed {
		t.Fatalf("shared reclaim changed hand run: changed=%t err=%v", changed, err)
	}
	// A caller with an old or forged non-hand snapshot cannot bypass the SQL guard.
	forged := current
	forged.HandRun = false
	if changed, err := store.reclaimExpiredRunLeaseIfSnapshot(forged, now, defaultRunLeaseTTL, "forged snapshot"); err != nil || changed {
		t.Fatalf("stale snapshot changed hand run: changed=%t err=%v", changed, err)
	}
	assertHeld("store reclaim")

	daemon := &Daemon{store: store, stateRoot: store.stateRoot}
	project := RegisteredProject{ProjectID: run.ProjectID}
	wf := WorkflowFile{Data: defaultWorkflow()}
	note := Note{Data: map[string]any{"id": run.ItemID, "status": "backlog"}}
	if after, changed, err := daemon.reconcileExecuteRunWithPlan(context.Background(), project, wf, nil, note, current); err != nil || changed || after.LeaseOwner != run.LeaseOwner {
		t.Fatalf("plan reconciliation changed hand run: after=%#v changed=%t err=%v", after, changed, err)
	}
	if after, changed, err := daemon.reconcileRunWithTracker(context.Background(), project, wf, current, note, nil, nil); err != nil || changed || after.LeaseOwner != run.LeaseOwner {
		t.Fatalf("tracker reconciliation changed hand run: after=%#v changed=%t err=%v", after, changed, err)
	}
	assertHeld("daemon reconciliation")
	for i := 0; i < 3; i++ {
		if changed, err := daemon.reclaimExpiredDispatchCapacity([]RunStatus{load()}, now); err != nil || changed {
			t.Fatalf("poll %d reclaimed hand run: changed=%t err=%v", i, changed, err)
		}
	}
	assertHeld("capacity reclaim")

	holder := Note{Data: map[string]any{"id": run.ItemID, "owned_paths": []string{"cmd/tusker"}}}
	candidate := Note{Data: map[string]any{"id": "APP-T-HAND-2", "owned_paths": []string{"cmd/tusker/daemon.go"}}}
	if conflict, found := ownedPathConflict(candidate, map[string]Note{run.ItemID: holder}, []RunStatus{load()}, now); !found || conflict["liveness"] != "quiet_interactive" {
		t.Fatalf("quiet owner not diagnosed: found=%t conflict=%#v", found, conflict)
	}
	if taken, err := reclaimDeadOwnedPathHolders(store, "", candidate, map[string]Note{run.ItemID: holder}, []RunStatus{load()}, now); err != nil || len(taken) != 0 {
		t.Fatalf("owned-path takeover: taken=%v err=%v", taken, err)
	}
	assertHeld("owned-path takeover")
	blocker := workSessionOwnerBlocker(run.ItemID, run.LeaseOwner, "The expired holder process is still alive.")
	if blocker.Kind != ReadinessBlockerInteractiveOwner || !strings.Contains(blocker.Reason, "Quiet — held by interactive session agent:editor") || !strings.Contains(blocker.Remedy, "--break-glass") {
		t.Fatalf("quiet owner remedy: %#v", blocker)
	}

	// Explicit attributed release is still the operator escape hatch.
	current = load()
	if err := finishRuntimeRunIfSnapshot(store, &current, LeaseStateReleased, AttemptOutcomeAbandoned, 0, "break_glass actor=human:sarav reason=incident", false); err != nil {
		t.Fatal(err)
	}
	if released := load(); released.LeaseState != string(LeaseStateReleased) || !strings.Contains(released.LastError, "actor=human:sarav") {
		t.Fatalf("break-glass release lost attribution: %#v", released)
	}

	// Pre-migration NULL hand_run rows retain legacy expiry reclaim behavior.
	legacy := run
	legacy.RecordID, legacy.ItemID, legacy.ActiveAttemptID = "APP-T-LEGACY-1", "APP-T-LEGACY-1", "attempt-legacy-1"
	legacy.HandRun = false
	if err := store.UpsertRun(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE runs SET hand_run = NULL WHERE project_id = ? AND record_id = ?`, legacy.ProjectID, legacy.RecordID); err != nil {
		t.Fatal(err)
	}
	old, err := store.FindRunScoped(legacy.ProjectID, legacy.RecordID)
	if err != nil || old == nil || old.HandRunStamped {
		t.Fatalf("legacy NULL origin: %#v %v", old, err)
	}
	if changed, err := store.ReclaimExpiredRunLease(legacy.ProjectID, legacy.RecordID, now, defaultRunLeaseTTL, "legacy expiry"); err != nil || !changed {
		t.Fatalf("legacy reclaim: changed=%t err=%v", changed, err)
	}
}
