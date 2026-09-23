package main

import (
	"encoding/json"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestRunSessionControlsPauseRequiresProviderAcknowledgement(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if err := server.store.UpsertRun(RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseGeneration: 3,
		LeaseOwner: "attempt-1", ActiveAttemptID: "attempt-1",
	}); err != nil {
		t.Fatal(err)
	}

	var result runSessionControlResult
	servePost(t, server, "/api/runs/APP-T-0001/control?project=app", `{"action":"pause"}`, &result)
	if result.OK || !result.Refused || result.Supported || result.Alternative != runSessionControlStop {
		t.Fatalf("pause must refuse without negotiated provider support: %#v", result)
	}
	if !strings.Contains(result.Reason, "no negotiated pause capability") {
		t.Fatalf("pause refusal was not truthful: %q", result.Reason)
	}
	if _, err := loadRunSessionControlIntent(server.store, "app", "APP-T-0001"); err != nil {
		t.Fatalf("unsupported pause must not leave a durable control intent: %v", err)
	}
}

func TestRunSessionControlsStopKeepsStaleOwnerUnknown(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	pgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseGeneration: 7,
		LeaseOwner: "attempt-stale", ActiveAttemptID: "attempt-stale",
		ProcessPID: os.Getpid(), ProcessPGID: pgid, ProcessStartedAt: "2000-01-01T00:00:00Z",
	}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}

	var result runSessionControlResult
	servePost(t, server, "/api/runs/APP-T-0001/control?project=app", `{"action":"stop"}`, &result)
	if result.OK || !result.Refused || !result.Unknown || result.State != runSessionControlUnknown {
		t.Fatalf("stale owner stop must remain unresolved: %#v", result)
	}
	intent, err := loadRunSessionControlIntent(server.store, "app", run.RecordID)
	if err != nil || intent == nil {
		t.Fatalf("durable stop intent missing: %#v %v", intent, err)
	}
	if intent.State != runSessionControlUnknown || intent.LeaseGeneration != run.LeaseGeneration {
		t.Fatalf("stale stop intent lost owner identity: %#v", intent)
	}
	stored, err := server.store.FindRun(run.RecordID)
	if err != nil || stored == nil {
		t.Fatalf("read stale run: %#v %v", stored, err)
	}
	if stored.LeaseState != run.LeaseState || stored.ProcessPID != run.ProcessPID {
		t.Fatalf("stale PID refusal mutated the run: before=%#v after=%#v", run, stored)
	}
}

func TestRunSessionControlsStartFreshClearsNativeSessionAfterSettlement(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	run := RunStatus{
		ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateInterrupted), AttemptOutcome: string(AttemptOutcomeCancelled),
		LeaseGeneration: 9, ActiveAttemptID: "attempt-old", SessionRef: "native-old",
		AttemptCount: 2, WorkRevision: 4,
	}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SaveAttempt(RunAttempt{AttemptID: "attempt-old", ProjectID: run.ProjectID, RecordID: run.RecordID, ItemID: run.ItemID, SessionRef: run.SessionRef, Outcome: string(AttemptOutcomeCancelled)}); err != nil {
		t.Fatal(err)
	}

	var result runSessionControlResult
	servePost(t, server, "/api/runs/APP-T-0001/control?project=app", `{"action":"start_fresh"}`, &result)
	if !result.OK || result.Refused || result.State != "queued" || result.SessionRef != "" {
		t.Fatalf("start_fresh did not queue a new session: %#v", result)
	}
	stored, err := server.store.FindRun(run.RecordID)
	if err != nil || stored == nil {
		t.Fatalf("read fresh run: %#v %v", stored, err)
	}
	if stored.LeaseState != string(LeaseStateRetryQueued) || stored.SessionRef != "" || stored.AttemptCount != run.AttemptCount {
		t.Fatalf("start_fresh lost durable lineage or session separation: %#v", stored)
	}
	decisions, err := server.store.ListSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) == 0 || decisions[len(decisions)-1].Kind != string(SupervisorDecisionNewBranch) {
		t.Fatalf("start_fresh missing new-session decision: %#v", decisions)
	}
	intentRaw, err := server.store.GetSetting(runSessionControlSettingKey(run.ProjectID, run.RecordID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(intentRaw) != "" {
		var intent map[string]any
		if err := json.Unmarshal([]byte(intentRaw), &intent); err != nil {
			t.Fatal(err)
		}
		if intent["action"] == runSessionControlStop && intent["state"] != runSessionControlSettledState {
			t.Fatalf("fresh session bypassed an unsettled stop intent: %#v", intent)
		}
	}
}
