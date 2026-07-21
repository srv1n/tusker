package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func retryTestStore(t *testing.T) *RuntimeStore {
	t.Helper()
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func failedRun(recordID string, rev int) RunStatus {
	return RunStatus{
		ProjectID:       "project-1",
		RecordID:        recordID,
		ItemID:          recordID,
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeFailed),
		ActiveAttemptID: "attempt-old",
		SessionRef:      "session-old",
		WorkRevision:    rev,
		AttemptCount:    3,
		Terminal:        true,
	}
}

// A1: retrying a failed piece starts a fresh attempt for that piece alone.
func TestRetryScopesToSingleTask(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	if err := store.UpsertRun(failedRun("FAC-T-0001", 2)); err != nil {
		t.Fatal(err)
	}

	res, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, res.Requeued, "target requeued")
	assertEqual(t, false, res.AlreadyLive, "target not treated as already live")

	stored, err := store.FindRun("FAC-T-0001")
	if err != nil || stored == nil {
		t.Fatalf("load retried run: %#v %v", stored, err)
	}
	assertEqual(t, string(LeaseStateRetryQueued), stored.LeaseState, "retried lease")
	assertEqual(t, 0, stored.AttemptCount, "retried attempt count reset")
	assertEqual(t, string(AttemptOutcomeNone), stored.AttemptOutcome, "retried outcome reset")
	assertEqual(t, false, stored.Terminal, "retried run no longer terminal")
	assertEqual(t, "", stored.ActiveAttemptID, "retried run clears prior attempt")
	if blocker := automationRunBlocker(*stored, now.Add(time.Second)); blocker != "" {
		t.Fatalf("expected retried run dispatchable, got blocker %q", blocker)
	}
}

// A2: running the same retry request twice does not create duplicate competing
// attempts; the second call is a no-op that returns the live attempt.
func TestRetryIsIdempotent(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	if err := store.UpsertRun(failedRun("FAC-T-0001", 2)); err != nil {
		t.Fatal(err)
	}

	first, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, first.Requeued, "first retry requeues")

	afterFirst, err := store.FindRun("FAC-T-0001")
	if err != nil || afterFirst == nil {
		t.Fatalf("load after first retry: %#v %v", afterFirst, err)
	}

	second, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, second.AlreadyLive, "second retry is a no-op")
	assertEqual(t, false, second.Requeued, "second retry does not requeue again")

	afterSecond, err := store.FindRun("FAC-T-0001")
	if err != nil || afterSecond == nil {
		t.Fatalf("load after second retry: %#v %v", afterSecond, err)
	}
	if !reflect.DeepEqual(*afterFirst, *afterSecond) {
		t.Fatalf("second retry mutated the run:\n first=%#v\nsecond=%#v", *afterFirst, *afterSecond)
	}
}

// A3: neighbouring pieces are not restarted by the retry.
func TestRetryLeavesNeighborsAlone(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	target := failedRun("FAC-T-0002", 2)

	doneSibling := RunStatus{
		ProjectID: "project-1", RecordID: "FAC-T-0001", ItemID: "FAC-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded),
		ActiveAttemptID: "attempt-done", WorkRevision: 2, AttemptCount: 1, Terminal: true,
	}
	runningSibling := RunStatus{
		ProjectID: "project-1", RecordID: "FAC-T-0003", ItemID: "FAC-T-0003",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-live",
		ActiveAttemptID: "attempt-live", WorkRevision: 2, AttemptCount: 1,
	}
	for _, run := range []RunStatus{target, doneSibling, runningSibling} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}

	beforeDone, err := store.FindRun("FAC-T-0001")
	if err != nil || beforeDone == nil {
		t.Fatal(err)
	}
	beforeRunning, err := store.FindRun("FAC-T-0003")
	if err != nil || beforeRunning == nil {
		t.Fatal(err)
	}

	if _, err := retryFailedRun(store, "FAC-T-0002", "human:sarav", "operator retry", now); err != nil {
		t.Fatal(err)
	}

	afterDone, err := store.FindRun("FAC-T-0001")
	if err != nil || afterDone == nil {
		t.Fatal(err)
	}
	afterRunning, err := store.FindRun("FAC-T-0003")
	if err != nil || afterRunning == nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*beforeDone, *afterDone) {
		t.Fatalf("retry restarted succeeded neighbour:\nbefore=%#v\n after=%#v", *beforeDone, *afterDone)
	}
	if !reflect.DeepEqual(*beforeRunning, *afterRunning) {
		t.Fatalf("retry disturbed running neighbour:\nbefore=%#v\n after=%#v", *beforeRunning, *afterRunning)
	}
}
