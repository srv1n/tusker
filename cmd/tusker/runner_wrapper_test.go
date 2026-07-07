package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWrapperDetached(t *testing.T) {
	dir := t.TempDir()
	stateRoot := filepath.Join(dir, "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	helperPath := filepath.Join(dir, "wrapper-helper.sh")
	helperPIDPath := filepath.Join(dir, "wrapper-helper.pid")
	if err := writeText(helperPath, "#!/bin/sh\n"+"echo \"$$\" > \"$TUSKER_HELPER_PID\"\n"+"echo wrapper-helper-stdout\n"+"echo wrapper-helper-stderr >&2\n"+"sleep 2\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(helperPath, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_WRAPPER_EXE", helperPath)
	t.Setenv("TUSKER_HELPER_PID", helperPIDPath)

	req, err := runnerWrapperRequestForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	req.Start.Command = "codex app-server"
	result, err := (&CodexAppServerRunner{}).Start(context.Background(), req.Start)
	if err != nil {
		t.Fatal(err)
	}
	if result.PGID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-result.PGID, syscall.SIGTERM) })
	}
	helperPID := waitForPIDFile(t, helperPIDPath)
	if helperPID != result.PID {
		t.Fatalf("wrapper result pid should be helper pid: result=%d helper=%d", result.PID, helperPID)
	}
	if result.PGID != result.PID {
		t.Fatalf("detached wrapper should own its process group: pid=%d pgid=%d", result.PID, result.PGID)
	}
	if strings.TrimSpace(result.ProcessStart) == "" {
		t.Fatalf("expected wrapper process start time: %#v", result)
	}
	if !processIdentityMatches(RunStatus{ProcessPID: result.PID, ProcessPGID: result.PGID, ProcessStartedAt: result.ProcessStart}) {
		t.Fatalf("recorded wrapper identity does not match live process: %#v", result)
	}
	waitForFileText(t, runnerWrapperLogPath(req.Start), "wrapper-helper-stdout")
	waitForFileText(t, runnerWrapperLogPath(req.Start), "wrapper-helper-stderr")
	request, err := readRunnerWrapperRequest(req.Start.StatusPath + ".wrapper-request.json")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(RunnerCodexAppServer), request.Runner, "wrapper request runner")
}

func TestWrapperStopSignal(t *testing.T) {
	store, req := setupRunnerWrapperRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runRunnerWrapper(ctx, req) }()
	waitForWrapperHeartbeat(t, store, req)
	cancel()
	if err := waitForWrapperDone(t, done); err != nil {
		t.Fatal(err)
	}
	assertRunnerStatusExitCode(t, req.Start.StatusPath, 130)
	waitForFileText(t, req.Start.RawLogPath, "runner wrapper stopping:")
}

func TestWrapperStopSignalDaemonAbsenceContinuesHeartbeat(t *testing.T) {
	store, req := setupRunnerWrapperRuntime(t)
	if err := writeText(req.Start.NotePath, "---\nid: APP-T-0001\nstatus: review\n---\n"); err != nil {
		t.Fatal(err)
	}
	decision := runnerWrapperBeat(store, req.Start)
	if !decision.Continue {
		t.Fatalf("daemon absence/non-dispatchable note must not stop wrapper heartbeat: %#v", decision)
	}
	run, err := store.FindRun(req.Start.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.LeaseState != string(LeaseStateRunning) || strings.TrimSpace(run.LastHeartbeatAt) == "" {
		t.Fatalf("wrapper should renew its own lease heartbeat, got %#v", run)
	}
}

func TestWrapperStopSignalOwnerChangeStopsWrapper(t *testing.T) {
	store, req := setupRunnerWrapperRuntime(t)
	run, err := store.FindRun(req.Start.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	run.LeaseOwner = "other-attempt"
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	decision := runnerWrapperBeat(store, req.Start)
	if decision.Continue || !strings.Contains(decision.StopReason, "lease owner changed") {
		t.Fatalf("expected owner-change stop signal, got %#v", decision)
	}
}

func TestWrapperStopSignalLeaseStateStopsWrapper(t *testing.T) {
	store, req := setupRunnerWrapperRuntime(t)
	run, err := store.FindRun(req.Start.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	run.LeaseState = string(LeaseStateInterrupted)
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	decision := runnerWrapperBeat(store, req.Start)
	if decision.Continue || !strings.Contains(decision.StopReason, "lease state changed to interrupted") {
		t.Fatalf("expected lease-state stop signal, got %#v", decision)
	}
}

func TestWrapperFenced(t *testing.T) {
	store, req := setupRunnerWrapperRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runRunnerWrapper(ctx, req) }()
	run := waitForWrapperHeartbeat(t, store, req)
	if run.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("wrapper heartbeat should move claimed run to running: %#v", run)
	}
	run.LeaseGeneration++
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := waitForWrapperDone(t, done); err != nil {
		t.Fatal(err)
	}
	assertRunnerStatusExitCode(t, req.Start.StatusPath, 130)
	waitForFileText(t, req.Start.RawLogPath, "lease generation advanced")
}

func TestDaemonStopDrain(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cmd := exec.Command("sleep", "5")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	closeStdin := attachDevNullStdin(cmd)
	defer closeStdin()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	pgid := processGroupID(pid)
	startedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	defer func() {
		if processExists(pid) {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		}
		_ = cmd.Wait()
	}()
	if err := store.UpsertRun(RunStatus{
		ProjectID:        "project-1",
		RecordID:         "APP-T-0001",
		ItemID:           "APP-T-0001",
		Runner:           string(RunnerCodexAppServer),
		Lane:             runLaneExecute,
		LeaseState:       string(LeaseStateRunning),
		LeaseOwner:       "attempt-wrapper",
		LeaseGeneration:  1,
		AttemptOutcome:   string(AttemptOutcomeNone),
		ActiveAttemptID:  "attempt-wrapper",
		ProcessPID:       pid,
		ProcessPGID:      pgid,
		ProcessStartedAt: startedAt,
		AttemptCount:     1,
	}); err != nil {
		t.Fatal(err)
	}
	drained, err := waitForWrapperDrain(store, 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if drained {
		t.Fatal("expected drain to time out while wrapper process is live")
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	_ = cmd.Wait()
	drained, err = waitForWrapperDrain(store, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !drained {
		t.Fatal("expected drain to complete after wrapper process exits")
	}
}

func setupRunnerWrapperRuntime(t *testing.T) (*RuntimeStore, runnerWrapperRequest) {
	t.Helper()
	dir := t.TempDir()
	stateRoot := filepath.Join(dir, "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("TUSKER_WRAPPER_HEARTBEAT_MS", "50")
	t.Setenv("TUSKER_WRAPPER_STOP_TIMEOUT_MS", "200")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	req, err := runnerWrapperRequestForTest(dir)
	if err != nil {
		t.Fatal(err)
	}
	req.Start.Command = "sh -c 'sleep 30'"
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       req.Start.ProjectID,
		RecordID:        req.Start.RecordID,
		ItemID:          req.Start.ItemID,
		Runner:          req.Runner,
		Lane:            req.Start.Lane,
		LeaseState:      string(LeaseStateClaimed),
		LeaseOwner:      req.Start.AttemptID,
		LeaseGeneration: req.Start.LeaseGeneration,
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: req.Start.AttemptID,
		WorkspacePath:   req.Start.WorkspacePath,
		PromptPath:      req.Start.PromptPath,
		EventSinkPath:   req.Start.EventSinkPath,
		RawLogPath:      req.Start.RawLogPath,
		StatusPath:      req.Start.StatusPath,
		WorkRevision:    req.Start.WorkRevision,
		AttemptCount:    1,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
	return store, req
}

func waitForWrapperHeartbeat(t *testing.T, store *RuntimeStore, req runnerWrapperRequest) RunStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.FindRun(req.Start.RecordID)
		if err == nil && run != nil && strings.TrimSpace(run.LastHeartbeatAt) != "" && run.ProcessPID > 0 {
			return *run
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for wrapper heartbeat for %s", req.Start.RecordID)
	return RunStatus{}
}

func waitForWrapperDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for wrapper to stop")
		return nil
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := readText(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(raw))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file: %s", path)
	return 0
}

func assertRunnerStatusExitCode(t *testing.T, statusPath string, expected int) {
	t.Helper()
	waitForStatusFile(t, statusPath)
	status, err := readRunnerProcessStatus(statusPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.ExitCode != expected {
		t.Fatalf("expected runner exit code %d, got %#v", expected, status)
	}
}
