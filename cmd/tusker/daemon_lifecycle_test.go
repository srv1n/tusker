package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestOneShotDispatchRefusesWithDaemonRequirement(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "One shot", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	for name, run := range map[string]func() error{
		"automation dispatch": func() error { return automationDispatchCmd(Args{"vault": vault, "id": "APP-T-0001"}) },
		"daemon run --once":   func() error { return daemonRunCmd(Args{"once": "true"}) },
		"refresh":             func() error { return refreshCmd(Args{"quiet": "true"}) },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "resident daemon") || !strings.Contains(err.Error(), "tusker daemon run") {
			t.Fatalf("%s: expected resident daemon refusal, got %v", name, err)
		}
	}
}

func TestDispatchConsultsPlanBeforeExecute(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked readiness", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":     "ready",
		"readiness":  "blocked_by_dependency",
		"next_owner": "blocked_dependency",
	})
	project := registerAutomationTestProject(t, vault)

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateUnclaimed), run.LeaseState, "blocked task lease state")
	if !strings.Contains(run.LastError, "readiness is blocked_by_dependency") || !strings.Contains(run.LastError, "next_owner is blocked_dependency") {
		t.Fatalf("expected automation-plan blockers in run error, got %#v", run)
	}
}

func TestStaleLeaseReleaseForReviewState(t *testing.T) {
	vault := automationTestVault(t)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Reviewer.Enabled = false
	raw, err := yaml.Marshal(wfFile.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+wfFile.Body); err != nil {
		t.Fatal(err)
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review stale run", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":     "review",
		"readiness":  "waiting_on_review",
		"next_owner": "reviewer",
	})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRetryQueued),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-review",
		SessionRef:      "session-review",
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "review stale lease state")
	assertEqual(t, string(AttemptOutcomeAbandoned), run.AttemptOutcome, "review stale outcome")
	if !strings.Contains(run.LastError, "status is review") {
		t.Fatalf("expected review blocker release reason, got %#v", run)
	}
	count := countDispatchCapacityProjectRuns(map[string]RunStatus{"APP-T-0001": run})
	assertEqual(t, 0, count, "released review run capacity")
}

func TestRetryLeaseDeadPidReleasedByPollTick(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dead retry", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRetryQueued),
		AttemptOutcome:  string(AttemptOutcomeFailed),
		ActiveAttemptID: "attempt-dead",
		ProcessPID:      deadPIDForTest(),
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "dead retry lease state")
	assertEqual(t, 0, run.ProcessPID, "dead retry pid")
	if !strings.Contains(run.LastError, "released dead retry lease") {
		t.Fatalf("expected release reason, got %#v", run)
	}
	count, err := daemon.store.CountProjectActiveRuns(project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, count, "active project run count")
	status, err := daemon.store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, intFromAny(status["activeRuns"]), "daemon active run count")
}

func TestRunsReleaseClearsDeadRun(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		ProcessPID:      deadPIDForTest(),
		AttemptCount:    1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	output := captureStdout(t, func() {
		if err := runsReleaseCmd(Args{"id": "ITEM-1", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK       bool `json:"ok"`
		Released bool `json:"released"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "release ok")
	assertEqual(t, true, payload.Released, "release flag")
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("ITEM-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "released run state")
	assertEqual(t, 0, run.ProcessPID, "released pid")
	assertEqual(t, "", run.ActiveAttemptID, "released active attempt")
}

func TestInterruptDeadRunWithoutLiveHandleReleases(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		ProcessPID:      deadPIDForTest(),
		AttemptCount:    1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	daemon, err := NewDaemon(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.InterruptRun(context.Background(), "ITEM-1"); err != nil {
		t.Fatal(err)
	}
	run, err := daemon.store.FindRun("ITEM-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateInterrupted), run.LeaseState, "interrupted dead run state")
	assertEqual(t, 0, run.ProcessPID, "interrupted dead pid")
	if !strings.Contains(run.LastError, "live runner handle not found") {
		t.Fatalf("expected missing handle reason, got %#v", run)
	}
}

func TestWorkspaceStrategyInPlaceDefaultUsesRepoRootAndExemptsTuskerBookkeeping(t *testing.T) {
	wf := defaultWorkflow()
	assertEqual(t, string(WorkspaceStrategyInPlace), wf.Workspace.Strategy, "default workspace strategy")

	repo := t.TempDir()
	if _, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(repo, ".tusker", "work")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, ".tusker", "work", "runtime.md"), "state churn\n"); err != nil {
		t.Fatal(err)
	}
	manager := NewWorkspaceManager()
	result, err := manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: repo, StateRoot: filepath.Join(t.TempDir(), "state"), Strategy: WorkspaceStrategyInPlace,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, repo, result.Path, "in_place path")
	assertEqual(t, string(WorkspaceStrategyInPlace), result.Metadata.Strategy, "in_place metadata")
	assertExists(t, filepath.Join(repo, ".tusker", "work", "runtime.md"))

	dirtyRepo := t.TempDir()
	if _, err := exec.Command("git", "-C", dirtyRepo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(dirtyRepo, "main.go"), "package main\n"); err != nil {
		t.Fatal(err)
	}
	_, err = manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0002", ItemID: "APP-T-0002",
		RepoRoot: dirtyRepo, StateRoot: filepath.Join(t.TempDir(), "state"), Strategy: WorkspaceStrategyInPlace,
	})
	if err == nil || !strings.Contains(err.Error(), "clean working tree outside .tusker") {
		t.Fatalf("expected dirty in_place refusal, got %v", err)
	}
}

func TestWorkspaceRootHonoredForWorktreeStrategy(t *testing.T) {
	repo := t.TempDir()
	if _, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "configured-workspaces")
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: repo, StateRoot: filepath.Join(t.TempDir(), "state"), WorkspaceRoot: root, Strategy: WorkspaceStrategyWorktree,
	}
	workspacePath, workspaceRoot, err := workspacePathForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, filepath.Join(root, "APP"), workspaceRoot, "configured workspace root")
	assertEqual(t, filepath.Join(root, "APP", "APP-T-0001"), workspacePath, "configured workspace path")
}

func TestDispatchRecordsProcessIdentity(t *testing.T) {
	vault := automationTestVault(t)
	writeCodexSleepWorkflowForCapacityTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Identity", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	defer killRunProcess(run)
	if run.ProcessPID <= 0 {
		t.Fatalf("expected pid, got %#v", run)
	}
	if run.ProcessPGID <= 0 {
		t.Fatalf("expected pgid, got %#v", run)
	}
	if strings.TrimSpace(run.ProcessStartedAt) == "" {
		t.Fatalf("expected process start time, got %#v", run)
	}
	if !processIdentityMatches(run) {
		t.Fatalf("expected recorded process identity to match live process: %#v", run)
	}
}

func TestFirstEventDeadlineInterruptsNeverStartedRunner(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodex),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		ProcessPID:      cmd.Process.Pid,
		ProcessPGID:     processGroupID(cmd.Process.Pid),
		AttemptCount:    1,
		StartedAt:       time.Now().UTC().Add(-daemonFirstEventDeadline - time.Minute).Format(time.RFC3339),
		UpdatedAt:       time.Now().UTC().Add(-daemonFirstEventDeadline - time.Minute).Format(time.RFC3339),
	}
	defer killRunProcess(run)

	reconciled, changed, err := daemon.reconcileRun(context.Background(), RegisteredProject{ProjectID: "project-1"}, WorkflowFile{Data: defaultWorkflow()}, run)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected first-event deadline to change run")
	}
	if reconciled.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("expected never-started runner to be retry queued, got %#v", reconciled)
	}
	if !strings.Contains(reconciled.LastError, "never started") {
		t.Fatalf("expected never-started error, got %#v", reconciled)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("expected first-event watchdog to stop process")
	}
}

func TestRetryCircuitBreakerMarksTerminal(t *testing.T) {
	daemon := &Daemon{}
	wf := defaultWorkflow()
	wf.Retry.MaxAttempts = 3
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodex),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-3",
		AttemptCount:    3,
	}
	retried := daemon.scheduleRetry(run, wf, "runner exited with code 1")
	if retried.LeaseState != string(LeaseStateReleased) {
		t.Fatalf("expected retry cap to release run, got %#v", retried)
	}
	if !retried.Terminal {
		t.Fatalf("expected terminal retry circuit breaker, got %#v", retried)
	}
	if retried.LastError != "runner exited with code 1" {
		t.Fatalf("expected terminal error to be preserved, got %#v", retried)
	}
}

func TestContinuationRetryCapParksNoProgress(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	wf := defaultWorkflow()
	wf.Runtime.MaxContinuationRetries = 2
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		SessionRef:      "session-1",
		AttemptCount:    1,
	}
	for i := 0; i < 2; i++ {
		if _, err := store.SaveSupervisorDecision(SupervisorDecision{
			ProjectID:        run.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        run.ActiveAttemptID,
			SessionRef:       run.SessionRef,
			Kind:             string(SupervisorDecisionContinueThread),
			Reason:           "session is resumable",
			ParentAttemptID:  run.ActiveAttemptID,
			ParentSessionRef: run.SessionRef,
		}); err != nil {
			t.Fatal(err)
		}
	}
	parked, queued := daemon.scheduleContinuationRetry(run, wf, "session is resumable")
	assertEqual(t, false, queued, "continuation queued")
	assertEqual(t, string(LeaseStateParkedNoProgress), parked.LeaseState, "parked lease state")
	assertEqual(t, string(AttemptOutcomeBlocked), parked.AttemptOutcome, "parked outcome")
	assertEqual(t, true, parked.Terminal, "parked terminal")
	assertEqual(t, false, isDispatchCapacityLeaseState(parked.LeaseState), "parked capacity")
	if !strings.Contains(parked.LastError, "continuation retry cap reached") {
		t.Fatalf("expected cap reason, got %#v", parked)
	}
	if err := store.UpsertRun(parked); err != nil {
		t.Fatal(err)
	}
	status, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, intFromAny(status["parkedNoProgressRuns"]), "parked status count")
}

func TestDaemonStopCommandStopsResidentDaemon(t *testing.T) {
	stateRoot, err := os.MkdirTemp("", "tusker-stop-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := pickupV7TestVault(t)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	registerAutomationTestProject(t, vault)
	done := make(chan error, 1)
	go func() {
		done <- daemonRunCmd(Args{})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if readDaemonLiveness(stateRoot, time.Now().UTC()).Alive && fileExists(daemonSocketPath(stateRoot)) {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("daemon exited before start: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	output := captureStdout(t, func() {
		if err := daemonStopCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK      bool `json:"ok"`
		Stopped bool `json:"stopped"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "stop ok")
	assertEqual(t, true, payload.Stopped, "stop stopped")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon stop did not end run loop")
	}
	if readDaemonLiveness(stateRoot, time.Now().UTC()).Alive {
		t.Fatal("daemon still alive after stop")
	}
}

func deadPIDForTest() int {
	for pid := 999999; pid > 900000; pid-- {
		if !processExists(pid) {
			return pid
		}
	}
	return 999999
}
