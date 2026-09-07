package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPilotRunnerLifecycle(t *testing.T) {
	t.Run("real process publishes success", func(t *testing.T) {
		req := pilotRunnerRequest(t, "printf pilot-ok")
		result, err := executeRunnerCommand(context.Background(), RunnerCodexExec, req, RunnerCapabilities{})
		if err != nil {
			t.Fatal(err)
		}
		waitForStatusFile(t, req.StatusPath)
		status, err := readRunnerProcessStatus(req.StatusPath)
		if err != nil {
			t.Fatal(err)
		}
		if status.ExitCode != 0 || status.Outcome != "" {
			t.Fatalf("successful process published %#v", status)
		}
		raw, err := os.ReadFile(req.RawLogPath)
		if err != nil || string(raw) != "pilot-ok" {
			t.Fatalf("runner output=%q err=%v", raw, err)
		}
		if processExists(result.PID) {
			t.Fatalf("successful process %d still exists after terminal status", result.PID)
		}
	})

	t.Run("cancellation cannot publish late success", func(t *testing.T) {
		req := pilotRunnerRequest(t, "sleep 30")
		ctx, cancel := context.WithCancel(context.Background())
		result, err := executeRunnerCommand(ctx, RunnerCodexExec, req, RunnerCapabilities{})
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		waitForStatusFile(t, req.StatusPath)
		status, err := readRunnerProcessStatus(req.StatusPath)
		if err != nil {
			t.Fatal(err)
		}
		if status.ExitCode != 130 || status.Outcome != string(AttemptOutcomeInterrupted) || !strings.Contains(status.Reason, "cancelled") {
			t.Fatalf("cancelled process published %#v", status)
		}
		deadline := time.Now().Add(time.Second)
		for processGroupExists(result.PGID) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if processGroupExists(result.PGID) {
			t.Fatalf("cancelled process group %d survived", result.PGID)
		}
	})

	t.Run("stale containment refuses before launch", func(t *testing.T) {
		req := pilotRunnerRequest(t, "printf launched > marker")
		req.ContainmentPGID = processGroupID(os.Getpid()) + 1
		result, err := executeRunnerCommand(context.Background(), RunnerCodexExec, req, RunnerCapabilities{})
		if err == nil || !strings.Contains(err.Error(), "containment is stale") {
			t.Fatalf("expected stale containment refusal, got result=%#v err=%v", result, err)
		}
		if result != nil {
			t.Fatalf("stale containment returned a process: %#v", result)
		}
		if _, err := os.Stat(filepath.Join(req.WorkspacePath, "marker")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("command ran outside claimed containment: %v", err)
		}
		if _, err := os.Stat(req.EventSinkPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("refused launch emitted an attempt event: %v", err)
		}
	})
}

func pilotRunnerRequest(t *testing.T, command string) runnerExecRequest {
	t.Helper()
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.Mkdir(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(prompt, []byte("pilot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return runnerExecRequest{
		ProjectID:     "pilot-project",
		RecordID:      "PILOT-T-0001",
		ItemID:        "PILOT-T-0001",
		AttemptID:     "pilot-attempt",
		Lane:          runLaneExecute,
		WorkspacePath: workspace,
		RepoRoot:      workspace,
		PromptPath:    prompt,
		EventSinkPath: filepath.Join(dir, "events.jsonl"),
		RawLogPath:    filepath.Join(dir, "raw.log"),
		StatusPath:    filepath.Join(dir, "status.json"),
		Command:       command,
	}
}
