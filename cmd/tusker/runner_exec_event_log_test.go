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
	req.Command = fmt.Sprintf("echo launched > %q; sleep 30", launchMarker)
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
	req.Command = fmt.Sprintf("sleep 30 & child=$!; echo \"$$ $child\" > %q; wait \"$child\"", pidPath)
	sentinel := errors.New("injected attempt_spawned append failure")
	eventLog := NewEventLog(req.EventSinkPath)
	writeCount := 0
	eventLog.writeFn = func(file *os.File, raw []byte) (int, error) {
		writeCount++
		if writeCount == 1 {
			return file.Write(raw)
		}
		deadline := time.Now().Add(2 * time.Second)
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
