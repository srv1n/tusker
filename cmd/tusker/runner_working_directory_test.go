package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexWorkingDirectorySharedAndWorktree(t *testing.T) {
	testRunnerWorkingDirectories(t, RunnerCodexExec)
}

func TestClaudeWorkingDirectorySharedAndWorktree(t *testing.T) {
	testRunnerWorkingDirectories(t, RunnerClaude)
}

func testRunnerWorkingDirectories(t *testing.T, runner RunnerName) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	shared := filepath.Join(home, "registered repo with spaces")
	worktree := filepath.Join(home, "isolated worktree with spaces")
	for _, path := range []string{shared, worktree} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		resolved, err := runnerWorkspaceCWD(runner, path)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, expected, resolved, string(runner)+" cwd with spaces")
		if err := assertRunnerCommandDir(runner, resolved, path); err != nil {
			t.Fatal(err)
		}
	}

	tildePath := filepath.Join(home, "registered repo with spaces")
	resolved, err := runnerWorkspaceCWD(runner, "~/registered repo with spaces")
	if err != nil {
		t.Fatal(err)
	}
	expectedTilde, err := filepath.EvalSymlinks(tildePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, expectedTilde, resolved, string(runner)+" tilde cwd")
}
