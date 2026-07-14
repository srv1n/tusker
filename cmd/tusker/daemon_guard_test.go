package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDaemonSingleInstanceGuardRejectsSecondStartBeforeStoreOpen(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	guard, err := acquireDaemonGuard(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()

	err = daemonRunCmd(Args{})
	if err == nil {
		t.Fatal("expected second daemon start to fail")
	}
	var alreadyRunning *daemonAlreadyRunningError
	if !errors.As(err, &alreadyRunning) {
		t.Fatalf("expected daemonAlreadyRunningError, got %T: %v", err, err)
	}
	if alreadyRunning.PID != os.Getpid() {
		t.Fatalf("expected incumbent pid %d, got %d", os.Getpid(), alreadyRunning.PID)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("expected error to name incumbent pid, got %q", err.Error())
	}
	if _, err := os.Stat(runtimeStoreDBPath(stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second start touched runtime store path: %v", err)
	}
}

func TestDaemonStalePidGuardIsReclaimed(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	if err := ensureDir(stateRoot); err != nil {
		t.Fatal(err)
	}
	stale := daemonPIDFile{PID: 99999999, StartedAt: "2026-07-06T00:00:00Z", StateRoot: stateRoot}
	raw, err := json.Marshal(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, daemonPIDFileName), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	guard, err := acquireDaemonGuard(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	pidFile, ok, err := readDaemonPIDFile(filepath.Join(stateRoot, daemonPIDFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected pid file to be replaced")
	}
	if pidFile.PID != os.Getpid() {
		t.Fatalf("expected pidfile to be reclaimed by current process, got %#v", pidFile)
	}
}

func TestRuntimeStoreSqliteBusyRetrySurvivesHeldWriteLock(t *testing.T) {
	oldTimeout := runtimeStoreBusyTimeout
	oldLimit := runtimeStoreBusyRetryLimit
	oldBackoff := runtimeStoreBusyRetryBackoff
	runtimeStoreBusyTimeout = 5 * time.Millisecond
	runtimeStoreBusyRetryLimit = 500 * time.Millisecond
	runtimeStoreBusyRetryBackoff = []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}
	t.Cleanup(func() {
		runtimeStoreBusyTimeout = oldTimeout
		runtimeStoreBusyRetryLimit = oldLimit
		runtimeStoreBusyRetryBackoff = oldBackoff
	})

	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var busyTimeout int
	if err := store.queryRowScan(`PRAGMA busy_timeout`, nil, &busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != int((5 * time.Millisecond).Milliseconds()) {
		t.Fatalf("expected busy_timeout pragma to be 5ms, got %d", busyTimeout)
	}

	locker, err := sql.Open("sqlite", runtimeStoreSQLiteDSN(runtimeStoreDBPath(stateRoot), time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	locker.SetMaxOpenConns(1)
	defer locker.Close()
	if _, err := locker.Exec(`BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	committed := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		_, err := locker.Exec(`COMMIT`)
		committed <- err
	}()

	started := time.Now()
	err = store.UpsertProject(RegisteredProject{
		ProjectID:    "project-1",
		ProjectKey:   "APP",
		Name:         "App",
		RepoRoot:     filepath.Join(stateRoot, "repo"),
		VaultRoot:    filepath.Join(stateRoot, "repo", ".tusker"),
		WorkflowPath: filepath.Join(stateRoot, "repo", ".tusker", "WORKFLOW.md"),
		Enabled:      true,
	})
	if commitErr := <-committed; commitErr != nil {
		t.Fatal(commitErr)
	}
	if err != nil {
		t.Fatalf("expected runtime store write to retry through transient SQLITE_BUSY, got %v", err)
	}
	if time.Since(started) < 50*time.Millisecond {
		t.Fatal("expected write to wait for held SQLite write lock")
	}
}

func TestAutomationStatusReportsDaemonPidUptimeAndRuntimeStorePath(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	guard, err := acquireDaemonGuard(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()

	output := captureStdout(t, func() {
		if err := automationStatusCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK     bool                   `json:"ok"`
		Status automationStatusReport `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK {
		t.Fatalf("expected ok payload, got %s", output)
	}
	if !payload.Status.DaemonAlive {
		t.Fatalf("expected daemon alive in status, got %#v", payload.Status)
	}
	if payload.Status.DaemonPID != os.Getpid() {
		t.Fatalf("expected daemon pid %d, got %d", os.Getpid(), payload.Status.DaemonPID)
	}
	if payload.Status.DaemonUptimeSeconds < 0 {
		t.Fatalf("expected non-negative uptime, got %d", payload.Status.DaemonUptimeSeconds)
	}
	if payload.Status.RuntimeStorePath != runtimeStoreDBPath(stateRoot) {
		t.Fatalf("expected runtime store path %q, got %q", runtimeStoreDBPath(stateRoot), payload.Status.RuntimeStorePath)
	}
}
