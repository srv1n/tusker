package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnerPreclaimHealth(t *testing.T) {
	goodDir := t.TempDir()
	badDir := t.TempDir()
	good := writeRunnerPreflightScript(t, goodDir, "codex", "#!/bin/sh\necho codex-test 1.0\n")
	bad := writeRunnerPreflightScript(t, badDir, "codex", "#!/bin/sh\necho broken runner >&2\nexit 127\n")
	nonExecutable := filepath.Join(t.TempDir(), "codex")
	if err := writeText(nonExecutable, "#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		command    string
		searchPath string
		check      string
		contains   []string
	}{
		{name: "missing", command: "codex exec -", searchPath: t.TempDir(), check: "executable", contains: []string{"not found"}},
		{name: "non executable", command: nonExecutable + " exec -", searchPath: t.TempDir(), check: "permission", contains: []string{"not executable"}},
		{name: "non executable discovered alternate", command: "codex exec -", searchPath: filepath.Dir(nonExecutable) + string(os.PathListSeparator) + goodDir, check: "permission", contains: []string{"not executable", "discovered alternate", good}},
		{name: "version", command: bad + " exec -", searchPath: badDir, check: "version", contains: []string{"failed health check", "broken runner"}},
		{name: "malformed", command: "codex 'unterminated", searchPath: goodDir, check: "command_shape", contains: []string{"unterminated"}},
		{name: "shell control", command: "codex exec -; whoami", searchPath: goodDir, check: "command_shape", contains: []string{"shell control"}},
		{name: "discovered alternate", command: "codex exec -", searchPath: badDir + string(os.PathListSeparator) + goodDir, check: "version", contains: []string{"discovered alternate", good}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			health := runnerPreclaimHealthWithSearchPath(RunnerCodexExec, tc.command, tc.searchPath)
			if health.Block == nil {
				t.Fatalf("expected pre-claim block, got %#v", health)
			}
			assertEqual(t, runnerInfrastructureBlockedState, health.Block.State, "block state")
			assertEqual(t, tc.check, health.Block.FailedCheck, "failed check")
			if health.Block.Remedy == "" || health.Block.PathProvenance == "" {
				t.Fatalf("block omitted durable remedy/path provenance: %#v", health.Block)
			}
			for _, want := range tc.contains {
				if !strings.Contains(health.Block.Reason, want) {
					t.Fatalf("block reason %q omitted %q", health.Block.Reason, want)
				}
			}
		})
	}

	t.Run("explicit executable", func(t *testing.T) {
		health := runnerPreclaimHealthWithSearchPath(RunnerCodexExec, good+" exec -", badDir)
		if health.Block != nil {
			t.Fatalf("explicit executable was refused: %#v", health.Block)
		}
		assertEqual(t, good, health.Preflight.ResolvedExecutable, "explicit executable")
	})

	t.Run("explicit machine prefix", func(t *testing.T) {
		t.Setenv(runnerPathPrefixEnv, goodDir)
		t.Setenv("PATH", badDir)
		previousLoginPath := runnerLoginShellPath
		runnerLoginShellPath = func() string { return "" }
		t.Cleanup(func() { runnerLoginShellPath = previousLoginPath })
		health := runnerPreclaimHealth(RunnerCodexExec, "codex exec -")
		if health.Block != nil {
			t.Fatalf("explicit machine alternate was refused: %#v", health.Block)
		}
		assertEqual(t, good, health.Preflight.ResolvedExecutable, "machine alternate executable")
	})
}

func TestRunnerInfrastructureBlockedProjection(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	block := &RunnerInfrastructureBlock{
		State: runnerInfrastructureBlockedState, Runner: string(RunnerCodexExec), Command: "codex exec -",
		Executable: "codex", PathProvenance: "/daemon/bin", FailedCheck: "version",
		Reason: "version failed", Remedy: "Repair the authorized codex_exec runner command or its daemon PATH, then redrive the task.",
	}
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), UpdatedAt: "2026-07-28T00:00:00Z"}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	blocked, claimed, err := d.persistRunnerInfrastructureBlock(run, block)
	if err != nil || claimed {
		t.Fatalf("pre-claim infrastructure block unexpectedly claimed work: run=%#v claimed=%t err=%v", blocked, claimed, err)
	}
	assertEqual(t, 0, blocked.AttemptCount, "pre-claim attempt count")
	assertEqual(t, "", blocked.ActiveAttemptID, "pre-claim active attempt")
	stored, err := store.FindRun("APP-T-0001")
	if err != nil || stored == nil {
		t.Fatalf("read infrastructure-blocked run: run=%#v err=%v", stored, err)
	}
	if stored.Infrastructure == nil || stored.Infrastructure.Remedy != block.Remedy || stored.Infrastructure.PathProvenance != block.PathProvenance {
		t.Fatalf("typed infrastructure projection was not durable: %#v", stored)
	}
	if got := automationRunBlocker(*stored, time.Now().UTC()); !strings.Contains(got, runnerInfrastructureBlockedState) || !strings.Contains(got, block.Remedy) {
		t.Fatalf("queue collapsed infrastructure block: %q", got)
	}
	if got := serveRunOutcome(*stored, time.Now().UTC()); got != runnerInfrastructureBlockedState {
		t.Fatalf("serve collapsed infrastructure block into %q", got)
	}
	attempts, err := store.ListAttemptsForRun("app", "APP-T-0001")
	if err != nil || len(attempts) != 0 {
		t.Fatalf("pre-claim block created an attempt: attempts=%#v err=%v", attempts, err)
	}
	if _, err := redriveRuntimeRun(store, stored, "human:operator", "runner repaired", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateRetryQueued), stored.LeaseState, "redrive lease state")
	assertEqual(t, (*RunnerInfrastructureBlock)(nil), stored.Infrastructure, "redrive clears infrastructure receipt")
	if blocker := automationRunBlocker(*stored, time.Now().UTC().Add(time.Second)); blocker != "" {
		t.Fatalf("repaired/redriven runner remained blocked: %q", blocker)
	}
}
