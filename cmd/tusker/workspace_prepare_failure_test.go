package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspacePrepareFailsClosedOnWorktreeMaterializationError(t *testing.T) {
	repo := newWorkspacePrepareGitRepo(t)
	stateRoot := t.TempDir()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		BranchName: "task/APP-T-0001", BranchBase: "missing-base", RepoRoot: repo, StateRoot: stateRoot,
		Strategy: WorkspaceStrategyWorktree,
	}

	_, err := NewWorkspaceManager().Prepare(req)
	if err == nil || !strings.Contains(err.Error(), "materialize worktree workspace") {
		t.Fatalf("expected Git worktree preparation failure, got %v", err)
	}
	workspacePath, _, pathErr := workspacePathForRequest(req)
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if _, statErr := os.Stat(filepath.Join(workspacePath, ".tusker", "workspace.json")); !os.IsNotExist(statErr) {
		t.Fatalf("failed preparation must not report workspace metadata, stat error = %v", statErr)
	}
}

func TestWorkspacePreparePreservesPreexistingPathOnWorktreeMaterializationError(t *testing.T) {
	repo := newWorkspacePrepareGitRepo(t)
	stateRoot := t.TempDir()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		BranchName: "task/APP-T-0001", BranchBase: "missing-base", RepoRoot: repo, StateRoot: stateRoot,
		Strategy: WorkspaceStrategyWorktree,
	}
	workspacePath, _, err := workspacePathForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(workspacePath, "unrelated.txt")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := NewWorkspaceManager().Prepare(req); err == nil {
		t.Fatal("expected Git worktree preparation failure")
	}
	content, readErr := os.ReadFile(sentinel)
	if readErr != nil {
		t.Fatalf("preexisting path was not preserved: %v", readErr)
	}
	if string(content) != "keep me\n" {
		d := string(content)
		t.Fatalf("preexisting path changed to %q", d)
	}
}

func TestWorkspacePrepareFailsClosedOnCloneMaterializationError(t *testing.T) {
	repo := newWorkspacePrepareGitRepo(t)
	stateRoot := t.TempDir()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: repo, StateRoot: stateRoot, Strategy: WorkspaceStrategyClone,
	}
	workspacePath, _, err := workspacePathForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(workspacePath, "unrelated.txt")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := NewWorkspaceManager().Prepare(req); err == nil || !strings.Contains(err.Error(), "materialize clone workspace") {
		t.Fatalf("expected Git clone preparation failure, got %v", err)
	}
	content, readErr := os.ReadFile(sentinel)
	if readErr != nil {
		t.Fatalf("preexisting path was not preserved: %v", readErr)
	}
	if string(content) != "keep me\n" {
		t.Fatalf("preexisting path changed to %q", content)
	}
}

func TestWorkspacePrepareMaterializesValidGitWorkspaces(t *testing.T) {
	for _, strategy := range []WorkspaceStrategy{WorkspaceStrategyWorktree, WorkspaceStrategyClone} {
		t.Run(string(strategy), func(t *testing.T) {
			repo := newWorkspacePrepareGitRepo(t)
			req := WorkspacePrepareRequest{
				ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
				RepoRoot: repo, StateRoot: t.TempDir(), Strategy: strategy,
			}
			if strategy == WorkspaceStrategyWorktree {
				req.BranchName = "task/APP-T-0001"
				req.BranchBase = "HEAD"
			}

			workspace, err := NewWorkspaceManager().Prepare(req)
			if err != nil {
				t.Fatalf("valid Git %s preparation failed: %v", strategy, err)
			}
			t.Cleanup(func() { _ = NewWorkspaceManager().Cleanup(workspace.Path) })
			if _, err := os.Stat(filepath.Join(workspace.Path, ".tusker", "workspace.json")); err != nil {
				t.Fatalf("valid Git %s preparation did not write metadata: %v", strategy, err)
			}
		})
	}
}

func newWorkspacePrepareGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Tusker Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("workspace test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", repo, "add", "README.md")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, output)
	}
	cmd = exec.Command("git", "-C", repo, "commit", "-m", "seed")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, output)
	}
	return repo
}
