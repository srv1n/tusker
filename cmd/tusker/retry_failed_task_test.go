package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
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
	// The requeued run is parked for retry with no live attempt, so the honest
	// no-op is "already queued" (not "already live") and it names no attempt id.
	assertEqual(t, true, second.AlreadyQueued, "second retry is a queued no-op")
	assertEqual(t, false, second.AlreadyLive, "queued run is not reported as live")
	assertEqual(t, "", second.AttemptID, "queued no-op claims no attempt id")
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

// A4: a Running lease whose TTL has expired is a crashed worker's leftover, not
// a live attempt. With nothing else to reap it (interactive / daemon-off), a
// retry must see through the stale lease and requeue.
func TestRetrySeesThroughStaleLease(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	stale := failedRun("FAC-T-0001", 2)
	stale.LeaseState = string(LeaseStateRunning)
	stale.LeaseOwner = "crashed-attempt"
	stale.ActiveAttemptID = "crashed-attempt"
	stale.Terminal = false
	stale.ProcessPID = 0
	stale.LeaseExpiresAt = now.Add(-time.Hour).Format(time.RFC3339) // expired
	if err := store.UpsertRun(stale); err != nil {
		t.Fatal(err)
	}

	res, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, res.Requeued, "stale-lease run requeued")
	assertEqual(t, false, res.AlreadyLive, "stale lease not treated as live")

	stored, err := store.FindRun("FAC-T-0001")
	if err != nil || stored == nil {
		t.Fatalf("load run: %#v %v", stored, err)
	}
	assertEqual(t, string(LeaseStateRetryQueued), stored.LeaseState, "requeued lease")
	assertEqual(t, "", stored.ActiveAttemptID, "stale attempt cleared")
}

// A4b: an unexpired Running lease is still a live attempt and blocks a retry.
func TestRetryRespectsUnexpiredLease(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	live := failedRun("FAC-T-0001", 2)
	live.LeaseState = string(LeaseStateRunning)
	live.ActiveAttemptID = "attempt-live"
	live.Terminal = false
	live.ProcessPID = 0
	live.LeaseExpiresAt = now.Add(time.Hour).Format(time.RFC3339) // unexpired
	if err := store.UpsertRun(live); err != nil {
		t.Fatal(err)
	}

	res, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, res.AlreadyLive, "unexpired lease reports live")
	assertEqual(t, "attempt-live", res.AttemptID, "live no-op names the attempt")
	assertEqual(t, false, res.Requeued, "unexpired lease not requeued")
}

// A5: a released run whose recorded PID no longer matches must not be kept alive
// by a bare PGID probe against a reused / other-user process group. Identity
// mismatch means dead, so the retry proceeds.
func TestRetrySeesThroughMismatchedIdentity(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	run := failedRun("FAC-T-0001", 2)
	run.LeaseState = string(LeaseStateReleased)
	// A PID that does not exist, paired with a PGID that IS live (this test
	// process's own group). Identity cannot match, so the live PGID must not be
	// mistaken for our attempt.
	run.ProcessPID = 2000000000
	run.ProcessPGID = syscall.Getpgrp()
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}

	res, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, res.Requeued, "identity-mismatch run requeued despite live pgid")
	assertEqual(t, false, res.AlreadyLive, "reused/foreign pgid not treated as live")
}

// A6: a genuinely live process (verified identity) blocks the retry and the
// no-op names the live attempt.
func TestRetryReportsLiveAttemptWithID(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	run := failedRun("FAC-T-0001", 2)
	run.LeaseState = string(LeaseStateRunning)
	run.ActiveAttemptID = "attempt-live"
	run.Terminal = false
	run.ProcessPID = os.Getpid() // verifiably alive: this test process
	run.ProcessPGID = 0          // skip pgid check
	run.ProcessStartedAt = ""    // skip start-time check
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}

	res, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, res.AlreadyLive, "verified-live process reports live")
	assertEqual(t, false, res.AlreadyQueued, "live is not queued")
	assertEqual(t, "attempt-live", res.AttemptID, "live no-op names the attempt")
}

// A7: an explicit operator retry of a run parked in backoff (RetryQueued with a
// future NextRetryAt) expedites it: NextRetryAt advances to now, attempt
// counters are preserved, and the result reports "expedited" not "already".
func TestRetryExpeditesFutureBackoff(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	parked := failedRun("FAC-T-0001", 2)
	parked.LeaseState = string(LeaseStateRetryQueued)
	parked.AttemptOutcome = string(AttemptOutcomeNone)
	parked.ActiveAttemptID = ""
	parked.Terminal = false
	parked.AttemptCount = 2
	parked.NextRetryAt = now.Add(30 * time.Minute).Format(time.RFC3339)
	if err := store.UpsertRun(parked); err != nil {
		t.Fatal(err)
	}

	res, err := retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, res.Expedited, "future-backoff retry expedites")
	assertEqual(t, false, res.AlreadyQueued, "expedite is an action, not a no-op")
	assertEqual(t, false, res.Requeued, "expedite does not reset the window")

	stored, err := store.FindRun("FAC-T-0001")
	if err != nil || stored == nil {
		t.Fatalf("load run: %#v %v", stored, err)
	}
	assertEqual(t, now.Format(time.RFC3339), stored.NextRetryAt, "next retry pulled to now")
	assertEqual(t, 2, stored.AttemptCount, "expedite keeps attempt counters")
	assertEqual(t, string(LeaseStateRetryQueued), stored.LeaseState, "expedite keeps queued state")
	if blocker := automationRunBlocker(*stored, now.Add(time.Second)); blocker != "" {
		t.Fatalf("expected expedited run dispatchable, got blocker %q", blocker)
	}
}

// A8: two concurrent retries of the same failed run both return clean results
// (one requeues, the loser gets a friendly no-op) rather than a raw CAS error.
func TestRetryConcurrentPairBothClean(t *testing.T) {
	store := retryTestStore(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)

	if err := store.UpsertRun(failedRun("FAC-T-0001", 2)); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make([]retryFailedTaskResult, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = retryFailedRun(store, "FAC-T-0001", "human:sarav", "operator retry", now)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent retry %d surfaced an error: %v", i, errs[i])
		}
	}
	requeued := 0
	noop := 0
	for _, r := range results {
		switch {
		case r.Requeued:
			requeued++
		case r.AlreadyQueued || r.AlreadyLive:
			noop++
		default:
			t.Fatalf("concurrent retry produced an ambiguous result: %#v", r)
		}
	}
	assertEqual(t, 1, requeued, "exactly one concurrent retry requeues")
	assertEqual(t, 1, noop, "the loser gets a clean no-op")

	stored, err := store.FindRun("FAC-T-0001")
	if err != nil || stored == nil {
		t.Fatalf("load run: %#v %v", stored, err)
	}
	assertEqual(t, string(LeaseStateRetryQueued), stored.LeaseState, "run ends queued exactly once")
}
