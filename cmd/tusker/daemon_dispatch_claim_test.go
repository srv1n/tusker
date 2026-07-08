package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestClaimRunLeaseDispatchCASPreconditions proves the store-level compare-and-swap
// the daemon dispatch path relies on (RUN-T-0043 A1/A2/A3): an uncontended snapshot
// claims atomically, while any concurrent mutation that changed the row since the
// poll snapshot (operator interrupt/park, or a second daemon advancing the
// generation) makes the claim back off without overwriting the winner. A released
// row still claims so the external-loop re-dispatch path keeps working.
func TestClaimRunLeaseDispatchCASPreconditions(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	seed := func(state LeaseState, owner string, gen, wr int) {
		if err := store.UpsertRun(RunStatus{
			ProjectID:       "p",
			RecordID:        "APP-T-0001",
			ItemID:          "APP-T-0001",
			LeaseState:      string(state),
			LeaseOwner:      owner,
			LeaseGeneration: gen,
			WorkRevision:    wr,
			AttemptOutcome:  string(AttemptOutcomeNone),
		}); err != nil {
			t.Fatal(err)
		}
	}
	guard := func(gen int) RunLeaseClaimGuard {
		return RunLeaseClaimGuard{Enforce: true, ExpectGeneration: gen}
	}
	find := func() RunStatus {
		row, err := store.FindRun("APP-T-0001")
		if err != nil || row == nil {
			t.Fatalf("find run: %v", err)
		}
		return *row
	}

	// A3/A1: an uncontended snapshot claims atomically and stamps owner/generation.
	seed(LeaseStateUnclaimed, "", 0, 3)
	ok, err := store.ClaimRunLease("p", "APP-T-0001", "owner-A", 1, defaultRunLeaseTTL, now, true, guard(0))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, ok, "uncontended claim")
	row := find()
	assertEqual(t, string(LeaseStateClaimed), row.LeaseState, "claimed lease state")
	assertEqual(t, "owner-A", row.LeaseOwner, "claim owner")
	assertEqual(t, 1, row.LeaseGeneration, "claim generation")

	// A2: an operator interrupt landed between poll and claim -> no overwrite.
	seed(LeaseStateInterrupted, "", 0, 3)
	ok, err = store.ClaimRunLease("p", "APP-T-0001", "owner-B", 1, defaultRunLeaseTTL, now, true, guard(0))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, ok, "claim backs off over interrupt")
	row = find()
	assertEqual(t, string(LeaseStateInterrupted), row.LeaseState, "interrupt preserved")
	assertEqual(t, "", row.LeaseOwner, "interrupt owner untouched")

	// A2: a supervisor park landed between poll and claim -> no overwrite.
	seed(LeaseStateParkedBudget, "", 0, 3)
	ok, err = store.ClaimRunLease("p", "APP-T-0001", "owner-P", 1, defaultRunLeaseTTL, now, true, guard(0))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, ok, "claim backs off over park")
	assertEqual(t, string(LeaseStateParkedBudget), find().LeaseState, "park preserved")

	// A2: a second daemon already advanced the generation -> stale snapshot backs off.
	seed(LeaseStateUnclaimed, "", 5, 3)
	ok, err = store.ClaimRunLease("p", "APP-T-0001", "owner-C", 6, defaultRunLeaseTTL, now, true, guard(4))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, ok, "stale generation backs off")
	assertEqual(t, 5, find().LeaseGeneration, "generation untouched")

	// External-loop path: a released row with a matching snapshot is still claimable.
	seed(LeaseStateReleased, "", 2, 3)
	ok, err = store.ClaimRunLease("p", "APP-T-0001", "owner-E", 3, defaultRunLeaseTTL, now, true, guard(2))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, ok, "released row remains claimable")
	assertEqual(t, string(LeaseStateClaimed), find().LeaseState, "released row claimed")
}

// TestDispatchRunAbortsWithoutSideEffectsWhenClaimLost proves RUN-T-0043 A1/A2 at the
// dispatch level: when a concurrent control action (operator interrupt) changes the
// run row between the poll snapshot and the claim, dispatchRun's atomic claim loses
// the compare-and-swap and aborts BEFORE any external side effect — no workspace
// prep, no attempt persisted, no spawn — and it does not clobber the interrupt with
// its stale snapshot.
func TestDispatchRunAbortsWithoutSideEffectsWhenClaimLost(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Claim race", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	runner := firstNonEmpty(wfFile.Data.Agents.Default, string(RunnerCodexAppServer))
	stamp := time.Now().UTC().Format(time.RFC3339)
	snapshot := RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         runner,
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   intField(note.Data, "work_revision"),
		UpdatedAt:      stamp,
	}
	if err := daemon.store.UpsertRun(snapshot); err != nil {
		t.Fatal(err)
	}

	// Simulate the concurrent operator interrupt that lands after the poll read the
	// dispatchable snapshot but before dispatchRun claims (same generation/revision,
	// only the lease_state moved to interrupted).
	cancelled := snapshot
	cancelled.LeaseState = string(LeaseStateInterrupted)
	cancelled.AttemptOutcome = string(AttemptOutcomeCancelled)
	cancelled.LastError = "interrupt requested by operator"
	cancelled.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := daemon.store.UpsertRun(cancelled); err != nil {
		t.Fatal(err)
	}

	_, dispatchErr := daemon.dispatchRun(context.Background(), project, wfFile, note, snapshot, runLaneExecute)
	if !errors.Is(dispatchErr, errDispatchClaimLost) {
		t.Fatalf("expected errDispatchClaimLost on lost claim, got %v", dispatchErr)
	}

	row := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateInterrupted), row.LeaseState, "interrupt not clobbered")
	assertEqual(t, "", row.LeaseOwner, "stale claim did not stamp owner")
	assertEqual(t, "", row.ActiveAttemptID, "no attempt refs written")

	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected no attempt persisted on aborted dispatch, got %d", len(attempts))
	}

	runDir := filepath.Join(daemon.stateRoot, "runs", project.ProjectKey, "APP-T-0001")
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected no run dir (no workspace/attempt side effects) at %s, stat err=%v", runDir, statErr)
	}
}

// TestPollDispatchBacksOffWhenInterruptLandsAfterSnapshot drives the RUN-T-0043
// invariant through the *real* poll path (PollOnce), not dispatchRun in isolation.
// A dispatchable run is snapshotted at poll start; an operator interrupt then
// lands in the ListRuns -> pre-dispatch-upsert window (modeled by a hook that
// fires right after the snapshot is read). The poll's pre-dispatch upsert must
// NOT re-arm the interrupted row from the stale snapshot, so the dispatch claim
// CAS backs off: the operator's stop survives, no lease is stamped, no attempt
// row/run dir/process is created. This FAILS against the pre-fix code (the plain
// pre-dispatch UpsertRun clobbered the stop back to unclaimed, letting the claim
// succeed and dispatch over the interrupt) and PASSES with the lease-preserving
// pre-dispatch upsert.
func TestPollDispatchBacksOffWhenInterruptLandsAfterSnapshot(t *testing.T) {
	vault := automationTestVault(t)
	// A python-sleep runner keeps any (buggy) fail-before dispatch to a local,
	// killable process instead of an external binary.
	writeCodexSleepWorkflowForCapacityTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Poll claim race", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	runner := firstNonEmpty(wfFile.Data.Agents.Default, string(RunnerCodex))
	// Seed the dispatchable snapshot the poll will read at ListRuns: unclaimed,
	// generation 0, matching the note's work revision (no work_revision reset).
	snapshot := RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         runner,
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   intField(note.Data, "work_revision"),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := daemon.store.UpsertRun(snapshot); err != nil {
		t.Fatal(err)
	}

	// The concurrent operator interrupt: it lands AFTER the poll reads its
	// snapshot but BEFORE the per-task pre-dispatch upsert. Same generation, only
	// the lease_state moves to interrupted (plus the operator's stop reason).
	fired := false
	daemon.afterPollSnapshotHook = func() {
		if fired {
			return
		}
		fired = true
		cancelled := snapshot
		cancelled.LeaseState = string(LeaseStateInterrupted)
		cancelled.AttemptOutcome = string(AttemptOutcomeCancelled)
		cancelled.LastError = "interrupt requested by operator"
		cancelled.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := daemon.store.UpsertRun(cancelled); err != nil {
			t.Errorf("hook interrupt write failed: %v", err)
		}
	}

	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce returned error: %v", err)
	}

	row := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	// Reap any process a regression would have spawned; no-op on the fixed path.
	defer killRunProcess(row)
	if !fired {
		t.Fatal("post-snapshot hook never fired; poll path did not reach the snapshot seam")
	}

	assertEqual(t, string(LeaseStateInterrupted), row.LeaseState, "operator interrupt survives the poll")
	assertEqual(t, "", row.LeaseOwner, "no lease owner stamped over the stop")
	assertEqual(t, "", row.ActiveAttemptID, "no attempt refs written over the stop")
	if row.ProcessPID != 0 {
		t.Fatalf("expected no spawned process over the stop, got pid %d", row.ProcessPID)
	}

	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 {
		t.Fatalf("expected no attempt persisted (dispatch backed off), got %d", len(attempts))
	}

	runDir := filepath.Join(daemon.stateRoot, "runs", project.ProjectKey, "APP-T-0001")
	if _, statErr := os.Stat(runDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected no run dir (no workspace/attempt side effects) at %s, stat err=%v", runDir, statErr)
	}
}
