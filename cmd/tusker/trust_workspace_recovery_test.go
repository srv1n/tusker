package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTrustWorkspaceRecovery(t *testing.T) {
	t.Run("restart reuses dirty Git workspace", func(t *testing.T) {
		repo, stateRoot := newWorkspacePrepareGitRepo(t), t.TempDir()
		req := WorkspacePrepareRequest{ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001", BranchName: "task/APP-T-0001", BranchBase: "HEAD", RepoRoot: repo, StateRoot: stateRoot, Strategy: WorkspaceStrategyWorktree}
		first, err := NewWorkspaceManager().Prepare(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = NewWorkspaceManager().Cleanup(first.Path) })
		checkpoint := filepath.Join(first.Path, "checkpoint.txt")
		if err := os.WriteFile(checkpoint, []byte("unlanded work\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		second, err := NewWorkspaceManager().Prepare(req)
		if err != nil || second.NewlyMaterialized || second.Path != first.Path {
			t.Fatalf("restart did not reuse workspace: %#v err=%v", second, err)
		}
		if got, err := os.ReadFile(checkpoint); err != nil || string(got) != "unlanded work\n" {
			t.Fatalf("restart lost checkpoint: %q err=%v", got, err)
		}
	})

	t.Run("cap preserves a durable active owner and Git failure launches nothing", func(t *testing.T) {
		stateRoot := t.TempDir()
		manager := NewWorkspaceManager()
		activeReq := WorkspacePrepareRequest{ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy}
		active, err := manager.Prepare(activeReq)
		if err != nil {
			t.Fatal(err)
		}
		markWorkspaceMetadataPIDDead(t, active.Path)
		checkpoint := filepath.Join(active.Path, "owned.txt")
		if err := os.WriteFile(checkpoint, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1", WorkspacePath: active.Path}); err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		_ = store.Close()
		blocked := activeReq
		blocked.RecordID, blocked.ItemID, blocked.MaxLiveWorktrees = "APP-T-0002", "APP-T-0002", 1
		if _, err := manager.Prepare(blocked); err == nil || !strings.Contains(err.Error(), "another live work copy") {
			t.Fatalf("active workspace escaped cap: %v", err)
		}
		if got, err := os.ReadFile(checkpoint); err != nil || string(got) != "keep\n" {
			t.Fatalf("cap cleanup lost active work: %q err=%v", got, err)
		}

		repo := newWorkspacePrepareGitRepo(t)
		bad := WorkspacePrepareRequest{ProjectID: "project-1", ProjectKey: "BAD", RecordID: "BAD-T-0001", ItemID: "BAD-T-0001", BranchName: "task/bad", BranchBase: "missing-base", RepoRoot: repo, StateRoot: t.TempDir(), Strategy: WorkspaceStrategyWorktree}
		if _, err := manager.Prepare(bad); err == nil {
			t.Fatal("failed Git materialization was accepted")
		}
		path, _, err := workspacePathForRequest(bad)
		if err != nil {
			t.Fatal(err)
		}
		if fileExists(filepath.Join(path, ".tusker", "workspace.json")) {
			t.Fatal("failed Git setup created a launchable workspace receipt")
		}
	})

	t.Run("shared Git setup serializes while independent worktrees open", func(t *testing.T) {
		repo, stateRoot := newWorkspacePrepareGitRepo(t), t.TempDir()
		requests := []WorkspacePrepareRequest{
			{ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001", BranchName: "task/APP-T-0001", BranchBase: "HEAD", RepoRoot: repo, StateRoot: stateRoot, Strategy: WorkspaceStrategyWorktree, MaxLiveWorktrees: 2},
			{ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0002", ItemID: "APP-T-0002", BranchName: "task/APP-T-0002", BranchBase: "HEAD", RepoRoot: repo, StateRoot: stateRoot, Strategy: WorkspaceStrategyWorktree, MaxLiveWorktrees: 2},
		}
		results := make([]WorkspacePrepareResult, len(requests))
		errs := make([]error, len(requests))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range requests {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				results[i], errs[i] = NewWorkspaceManager().Prepare(requests[i])
			}(i)
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("Git worktree %d failed: %v", i, err)
			}
			t.Cleanup(func() { _ = NewWorkspaceManager().Cleanup(results[i].Path) })
		}
		if results[0].Path == results[1].Path || !fileExists(filepath.Join(results[0].Path, ".git")) || !fileExists(filepath.Join(results[1].Path, ".git")) {
			t.Fatalf("concurrent Git setup did not create independent worktrees: %#v", results)
		}
	})
}
