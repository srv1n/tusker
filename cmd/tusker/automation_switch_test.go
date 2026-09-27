package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalAutomationSwitch(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fairDispatchPollProject(t, store, "project-a", "AAA", 1)
	worker := RunStatus{
		ProjectID: "project-other", RecordID: "OTH-T-0001", ItemID: "OTH-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1", ProcessPID: deadPIDForTest(), AttemptCount: 1,
		SessionRef: "native-session-1",
	}
	mustUpsertRun(t, store, worker)

	t.Setenv("CLAUDECODE", "1")
	if err := automationOffCmd(Args{"json": "true"}); err == nil || !strings.Contains(err.Error(), "automation off is owner-only") {
		t.Fatalf("agent session automation off error = %v", err)
	}
	if enabled, _ := store.GlobalAutomationEnabled(); !enabled {
		t.Fatal("refused agent call flipped the switch")
	}
	t.Setenv("CLAUDECODE", "")

	captureStdout(t, func() { err = automationOffCmd(Args{"json": "true"}) })
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := store.FindRun("OTH-T-0001")
	if err != nil || stopped == nil {
		t.Fatalf("worker row: %#v %v", stopped, err)
	}
	assertEqual(t, string(LeaseStateInterrupted), stopped.LeaseState, "off interrupts running worker")
	assertEqual(t, "native-session-1", stopped.SessionRef, "interrupt keeps native session")
	audit, err := store.ListProjectAutomationAudit(globalAutomationAuditScope)
	if err != nil || len(audit) != 1 || !audit[0].BeforeEnabled || audit[0].AfterEnabled || audit[0].Source != "cli" {
		t.Fatalf("global switch audit = %#v %v", audit, err)
	}
	status, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, boolFromAny(status["automation_global_enabled"]), "serve-readable switch state")

	daemon := &Daemon{
		store: store, stateRoot: stateRoot, frontiers: map[string]*projectFrontierIndex{},
		frontierHints: map[string][]daemonControlChange{},
	}
	var order []string
	daemon.fairDispatchRun = fairDispatchRecorder(&order)
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Fatalf("automation off dispatched %v", order)
	}
	blocked := latestRunForRecord(t, store, "project-a", "AAA-T-0001")
	if !strings.Contains(blocked.LastError, "automation is off globally") {
		t.Fatalf("expected global switch blocker, got %#v", blocked)
	}

	captureStdout(t, func() { err = automationOnCmd(Args{"json": "true"}) })
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFairDispatchOrder(t, []string{"project-a/AAA-T-0001"}, order)
	still, _ := store.FindRun("OTH-T-0001")
	assertEqual(t, string(LeaseStateInterrupted), still.LeaseState, "on does not auto-continue interrupted runs")
}
