package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedriveCASDoesNotClearConcurrentClaimOrResetBudget(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 10, 5, 0, 0, 0, time.UTC)
	original := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateReleased), WorkRevision: 4, AttemptCount: 3,
		AttemptOutcome: string(AttemptOutcomeFailed), UpdatedAt: now.Add(-time.Minute).Format(time.RFC3339),
	}
	if err := store.UpsertRun(original); err != nil {
		t.Fatal(err)
	}
	oldReset, err := store.RecordBudgetRedrive(original.ProjectID, original.RecordID, "human:old", "old window", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	run := original
	var claimErr error
	_, err = redriveRuntimeRunWithHook(store, &run, "human:new", "new window", now, func() {
		var claimed bool
		claimed, claimErr = store.ClaimRunLease(original.ProjectID, original.RecordID, "concurrent-attempt", 1, defaultRunLeaseTTL, now, true, false, RuntimeLeaseClaimPrecondition{
			ExpectedLeaseState: LeaseStateReleased, ExpectedOwner: "", ExpectedLeaseGeneration: 0, ExpectedWorkRevision: original.WorkRevision,
		})
		if claimErr == nil && !claimed {
			claimErr = errors.New("concurrent lease claim did not match original snapshot")
		}
	})
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	var typed *TuskerError
	if !errors.As(err, &typed) || typed.Code != "CAS_CONFLICT" {
		t.Fatalf("expected redrive snapshot conflict, got %v", err)
	}
	stored, err := store.FindRun(original.RecordID)
	if err != nil || stored == nil {
		t.Fatalf("load concurrently claimed run: %#v %v", stored, err)
	}
	assertEqual(t, string(LeaseStateClaimed), stored.LeaseState, "redrive does not clear concurrent claim")
	assertEqual(t, "concurrent-attempt", stored.LeaseOwner, "redrive preserves concurrent owner")
	assertEqual(t, 1, stored.LeaseGeneration, "redrive preserves concurrent generation")
	window, err := store.BudgetWindowStart(original.ProjectID, original.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, oldReset, window, "failed redrive leaves budget window unchanged")
}

func TestRedriveResetsAttemptWindow(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	overCap := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeFailed),
		ActiveAttemptID: "attempt-4",
		SessionRef:      "session-old",
		AttemptCount:    4,
		NextRetryAt:     time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		LastError:       "retry cap reached",
		Terminal:        true,
	}
	if err := store.UpsertRun(overCap); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := redriveCmd(Args{"id": "APP-T-0001", "by": "human:sarav", "reason": "fresh operator review", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK         bool   `json:"ok"`
		LeaseState string `json:"lease_state"`
		Reset      struct {
			Actor  string `json:"actor"`
			Reason string `json:"reason"`
		} `json:"reset"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "redrive ok")
	assertEqual(t, string(LeaseStateRetryQueued), payload.LeaseState, "payload lease")
	assertEqual(t, "human:sarav", payload.Reset.Actor, "payload actor")
	assertEqual(t, "fresh operator review", payload.Reset.Reason, "payload reason")

	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	redriven, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if redriven == nil {
		t.Fatal("expected redriven run")
	}
	assertEqual(t, 0, redriven.AttemptCount, "redrive attempt count")
	assertEqual(t, string(LeaseStateRetryQueued), redriven.LeaseState, "redrive lease")
	assertEqual(t, string(AttemptOutcomeNone), redriven.AttemptOutcome, "redrive outcome")
	assertEqual(t, false, redriven.Terminal, "redrive terminal")
	assertEqual(t, "", redriven.ActiveAttemptID, "active attempt cleared")
	assertEqual(t, "", redriven.StatusPath, "status path cleared")
	if blocker := automationRunBlocker(*redriven, time.Now().UTC().Add(time.Second)); blocker != "" {
		t.Fatalf("expected redriven run to be dispatchable, got blocker %q", blocker)
	}
	violations := sentinelAttemptCountWithinCaps(runtimeSentinelProjectSnapshot{Workflow: defaultWorkflow()}, []RunStatus{*redriven})
	if len(violations) != 0 {
		t.Fatalf("expected redriven run to stay inside attempt cap, got %#v", violations)
	}
}

func TestRedrivePreservesHistory(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeFailed),
		ActiveAttemptID: "attempt-15",
		SessionRef:      "session-old",
		AttemptCount:    15,
		Terminal:        true,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []RunAttempt{
		{AttemptID: "attempt-14", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, Outcome: string(AttemptOutcomeFailed), StartedAt: "2026-07-06T12:00:00Z", FinishedAt: "2026-07-06T12:05:00Z"},
		{AttemptID: "attempt-15", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, Lane: run.Lane, Outcome: string(AttemptOutcomeFailed), StartedAt: "2026-07-06T12:10:00Z", FinishedAt: "2026-07-06T12:15:00Z"},
	} {
		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID:       run.ProjectID,
		RecordID:        run.RecordID,
		ItemID:          run.ItemID,
		Runner:          run.Runner,
		AttemptID:       "attempt-15",
		Kind:            string(SupervisorDecisionStopForAudit),
		Reason:          "retry cap reached",
		LeaseState:      string(LeaseStateReleased),
		ContextSignal:   "pre_redrive",
		ParentAttemptID: "attempt-14",
		CreatedAt:       "2026-07-06T12:20:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	_ = captureStdout(t, func() {
		if err := redriveCmd(Args{"id": "APP-T-0001", "by": "human:sarav", "reason": "approved another window", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})

	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(attempts), "attempt history count")
	if attempts[0].AttemptID != "attempt-15" || attempts[1].AttemptID != "attempt-14" {
		t.Fatalf("expected original attempts in reverse chronology, got %#v", attempts)
	}
	decisions, err := store.ListRuntimeSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(decisions), "decision history count")
	redrive := decisions[len(decisions)-1]
	assertEqual(t, string(SupervisorDecisionRedrive), redrive.Kind, "redrive decision kind")
	assertEqual(t, "approved another window", redrive.Reason, "redrive reason")
	assertEqual(t, "operator_redrive", redrive.ContextSignal, "redrive signal")
	if !strings.Contains(redrive.ValidationDelta, `"actor":"human:sarav"`) || !strings.Contains(redrive.ValidationDelta, `"previous_attempt_count":15`) {
		t.Fatalf("expected redrive audit payload to include actor and previous count, got %s", redrive.ValidationDelta)
	}
	reset, err := store.BudgetWindowStart(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "human:sarav", reset.Actor, "budget redrive actor")
	assertEqual(t, "approved another window", reset.Reason, "budget redrive reason")
}

func TestRedrivenRunParksAtFreshContinuationCap(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	wf := defaultWorkflow()
	wf.Runtime.MaxContinuationRetries = 2
	redriven := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-fresh",
		SessionRef:      "session-fresh",
		AttemptCount:    1,
	}
	if err := store.UpsertRun(redriven); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID:       redriven.ProjectID,
		RecordID:        redriven.RecordID,
		AttemptID:       "attempt-before-redrive",
		SessionRef:      "session-before-redrive",
		Kind:            string(SupervisorDecisionContinueThread),
		Reason:          "old window continuation",
		ParentAttemptID: "attempt-before-redrive",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := store.SaveSupervisorDecision(SupervisorDecision{
			ProjectID:        redriven.ProjectID,
			RecordID:         redriven.RecordID,
			AttemptID:        redriven.ActiveAttemptID,
			SessionRef:       redriven.SessionRef,
			Kind:             string(SupervisorDecisionContinueThread),
			Reason:           "fresh window continuation",
			ParentAttemptID:  redriven.ActiveAttemptID,
			ParentSessionRef: redriven.SessionRef,
		}); err != nil {
			t.Fatal(err)
		}
	}
	parked, queued := daemon.scheduleContinuationRetry(redriven, wf, "fresh window still loops")
	assertEqual(t, false, queued, "fresh window queued")
	assertEqual(t, string(LeaseStateParkedNoProgress), parked.LeaseState, "fresh window parked")
	assertEqual(t, string(AttemptOutcomeBlocked), parked.AttemptOutcome, "fresh window outcome")
	assertEqual(t, true, parked.Terminal, "fresh window terminal")
	if !strings.Contains(parked.LastError, "continuation retry cap reached (2)") {
		t.Fatalf("expected fresh continuation cap reason, got %q", parked.LastError)
	}
}
