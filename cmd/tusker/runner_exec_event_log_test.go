package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunnerExecAttemptStartedEventFailureDoesNotLaunchProcess(t *testing.T) {
	req := runnerExecEventRequestForTest(t)
	launchMarker := filepath.Join(req.WorkspacePath, "launched")
	commandPath := filepath.Join(req.WorkspacePath, "mark-launched")
	if err := os.WriteFile(commandPath, []byte(fmt.Sprintf("#!/bin/sh\necho launched > %q\nsleep 30\n", launchMarker)), 0o755); err != nil {
		t.Fatal(err)
	}
	req.Command = commandPath
	if err := os.WriteFile(req.EventSinkPath, []byte("{\"seq\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := executeRunnerCommand(context.Background(), RunnerCodexExec, req, RunnerCapabilities{})
	if err == nil || !strings.Contains(err.Error(), "attempt_started") || !strings.Contains(err.Error(), "partial trailing record") {
		t.Fatalf("expected actionable attempt_started append error, got result=%#v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("pre-spawn append failure returned a live result: %#v", result)
	}
	if _, statErr := os.Stat(launchMarker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("command launched despite attempt_started append failure: %v", statErr)
	}
}

func TestRunnerExecAttemptSpawnedEventFailureTerminatesAndReapsProcessGroup(t *testing.T) {
	req := runnerExecEventRequestForTest(t)
	pidPath := filepath.Join(req.WorkspacePath, "runner-pids")
	commandPath := filepath.Join(req.WorkspacePath, "spawn-child")
	if err := os.WriteFile(commandPath, []byte(fmt.Sprintf("#!/bin/sh\nsleep 30 &\nchild=$!\necho \"$$ $child\" > %q\nwait \"$child\"\n", pidPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	req.Command = commandPath
	sentinel := errors.New("injected attempt_spawned append failure")
	eventLog := NewEventLog(req.EventSinkPath)
	writeCount := 0
	eventLog.writeFn = func(file *os.File, raw []byte) (int, error) {
		writeCount++
		if writeCount == 1 {
			return file.Write(raw)
		}
		// Process startup can exceed two seconds on a loaded hosted macOS runner;
		// the assertion is about durable spawn-event failure cleanup, not a
		// scheduler-latency budget.
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if pids, err := os.ReadFile(pidPath); err == nil && len(strings.Fields(string(pids))) == 2 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		return 0, sentinel
	}

	result, err := executeRunnerCommandWithEventLog(context.Background(), RunnerCodexExec, req, RunnerCapabilities{}, eventLog)
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "attempt_spawned") || !strings.Contains(err.Error(), "terminated") {
		t.Fatalf("expected actionable attempt_spawned append error, got result=%#v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("post-spawn append failure returned a live result: %#v", result)
	}
	pids := readRunnerExecPIDs(t, pidPath)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	for _, pid := range pids {
		deadline := time.Now().Add(time.Second)
		for processExists(pid) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if processExists(pid) {
			t.Fatalf("runner process-group member %d survived attempt_spawned append failure", pid)
		}
	}
	events, readErr := os.ReadFile(req.EventSinkPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Count(string(events), `"kind":"attempt_started"`) != 1 || strings.Contains(string(events), `"kind":"attempt_spawned"`) {
		t.Fatalf("unexpected event history after failed spawn append: %s", events)
	}
}

func TestRunnerStatusPublishFailureIsDurableInfrastructureEvent(t *testing.T) {
	dir := t.TempDir()
	blockedParent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("blocked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := runnerExecRequest{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		AttemptID: "attempt-status-failure", StatusPath: filepath.Join(blockedParent, "status.json"),
	}
	eventPath := filepath.Join(dir, "events.jsonl")
	publishRunnerTerminalStatus(NewEventLog(eventPath), RunnerCodexExec, req, 0, AttemptOutcomeNone, "", 0)
	raw, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"kind":"attempt_status_publish_failed"`) ||
		!strings.Contains(text, `"attempt_id":"attempt-status-failure"`) ||
		!strings.Contains(text, `"project_id":"project-1"`) {
		t.Fatalf("status publication failure was not durably attributed: %s", text)
	}
}

func TestRemovePrivateRunnerStatusIfExists(t *testing.T) {
	status := filepath.Join(t.TempDir(), "attempt", "status.json")
	if err := removePrivateFileIfExists(status); err != nil {
		t.Fatalf("absent stale status cleanup: %v", err)
	}
	if err := os.WriteFile(status, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removePrivateFileIfExists(status); err != nil {
		t.Fatalf("remove stale status: %v", err)
	}
	if _, err := os.Lstat(status); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale status still exists: %v", err)
	}
}

func TestRunnerFilesRejectSymlinkAuthorityPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("do not touch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rawLog := filepath.Join(dir, "raw.log")
	if err := os.Symlink(target, rawLog); err != nil {
		t.Fatal(err)
	}
	if file, err := openPrivateRunnerAppendFile(rawLog); err == nil {
		_ = file.Close()
		t.Fatal("runner raw log followed a symlink")
	}
	status := filepath.Join(dir, "status.json")
	if err := os.Symlink(target, status); err != nil {
		t.Fatal(err)
	}
	if err := removePrivateFileIfExists(status); err == nil {
		t.Fatal("stale runner status cleanup followed or removed a symlink")
	}
	if _, err := writeRunnerStatusFileIfAbsentWithOutcome(status, 0, AttemptOutcomeNone, "", 0); err == nil {
		t.Fatal("runner status treated an existing symlink as an authoritative terminal record")
	}
	if _, err := readRunnerProcessStatus(status); err == nil {
		t.Fatal("runner status reader followed a symlink")
	}
	contents, err := os.ReadFile(target)
	if err != nil || string(contents) != "do not touch\n" {
		t.Fatalf("symlink target changed: contents=%q err=%v", contents, err)
	}
}

func TestRunnerAuthorityFilesRejectSymlinkedParent(t *testing.T) {
	dir := t.TempDir()
	safeRoot := filepath.Join(dir, "safe")
	outside := filepath.Join(dir, "outside")
	if err := os.Mkdir(safeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	redirect := filepath.Join(safeRoot, "redirect")
	if err := os.Symlink(outside, redirect); err != nil {
		t.Fatal(err)
	}

	if file, err := openPrivateRunnerAppendFile(filepath.Join(redirect, "raw.log")); err == nil {
		_ = file.Close()
		t.Fatal("runner raw log followed a symlinked parent")
	}
	if _, err := writeRunnerStatusFileIfAbsentWithOutcome(filepath.Join(redirect, "status.json"), 0, AttemptOutcomeNone, "", 0); err == nil {
		t.Fatal("runner status followed a symlinked parent")
	}
	if err := removePrivateFileIfExists(filepath.Join(redirect, "status.json")); err == nil {
		t.Fatal("stale runner status cleanup followed a symlinked parent")
	}
	if err := NewEventLog(filepath.Join(redirect, "events.jsonl")).Append("attempt_started", "attempt-1", RunnerCodexExec, nil); err == nil {
		t.Fatal("event log followed a symlinked parent")
	}
	if err := writeRunnerWrapperRequest(filepath.Join(redirect, "request.json"), runnerWrapperRequest{}); err == nil {
		t.Fatal("wrapper request followed a symlinked parent")
	}

	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink target received authority files: %#v", entries)
	}
}

func runnerExecEventRequestForTest(t *testing.T) runnerExecRequest {
	t.Helper()
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := ensureDir(workspace); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(dir, "prompt.md")
	if err := writeText(promptPath, "test prompt\n"); err != nil {
		t.Fatal(err)
	}
	return runnerExecRequest{
		ProjectID:     "project-1",
		RecordID:      "APP-T-0001",
		ItemID:        "APP-T-0001",
		AttemptID:     "attempt-runner-exec",
		Lane:          runLaneExecute,
		WorkspacePath: workspace,
		RepoRoot:      workspace,
		PromptPath:    promptPath,
		EventSinkPath: filepath.Join(dir, "events.jsonl"),
		RawLogPath:    filepath.Join(dir, "raw.log"),
		StatusPath:    filepath.Join(dir, "status.json"),
		Command:       "sleep 30",
	}
}

func readRunnerExecPIDs(t *testing.T, path string) []int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		t.Fatalf("expected runner and child pids, got %q", raw)
	}
	pids := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			t.Fatalf("invalid runner pid %q", field)
		}
		pids = append(pids, pid)
	}
	return pids
}
