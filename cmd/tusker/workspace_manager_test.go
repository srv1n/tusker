package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceManagerRejectsMismatchedExistingMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy, WorkRevision: 0,
	}
	if _, err := manager.Prepare(req); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(stateRoot, "workspaces", "MEM", "record-1", ".tusker", "workspace.json")
	if err := writeText(metadataPath, `{"project_id":"other-project","record_id":"record-1","created_at":"2026-04-28T00:00:00Z"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Prepare(req)
	if err == nil || !strings.Contains(err.Error(), "project_id does not match") {
		t.Fatalf("expected metadata mismatch error, got %v", err)
	}
}

func TestAssertWorkspaceWithinRootRejectsEscapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces", "MEM")
	err := assertWorkspaceWithinRoot(filepath.Join(root, "..", "OTHER", "record-1"), root)
	if err == nil || !strings.Contains(err.Error(), "escapes workspace root") {
		t.Fatalf("expected workspace root escape error, got %v", err)
	}
}

func TestWorkspaceRootRejectsSharedRuntimeEscapes(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	for _, configured := range []string{"../repo-worktrees", filepath.Join(t.TempDir(), "absolute-worktrees"), "workspaces-old"} {
		_, _, err := workspacePathForRequest(WorkspacePrepareRequest{
			ProjectKey: "APP", RecordID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot,
			WorkspaceRoot: configured, Strategy: WorkspaceStrategyWorktree,
		})
		if err == nil || !strings.Contains(err.Error(), "shared runtime workspace directory") {
			t.Fatalf("workspace root %q should be rejected: %v", configured, err)
		}
	}
	path, root, err := workspacePathForRequest(WorkspacePrepareRequest{
		ProjectKey: "APP", RecordID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot,
		WorkspaceRoot: filepath.Join("workspaces", "team"), Strategy: WorkspaceStrategyWorktree,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(stateRoot, "workspaces", "team", "APP")
	if root != wantRoot || path != filepath.Join(wantRoot, "APP-T-0001") {
		t.Fatalf("workspace path/root = %q / %q, want %q", path, root, wantRoot)
	}
}

func TestWorkspaceRootRejectsSymlinkEscapeWithMissingTail(t *testing.T) {
	stateRoot := t.TempDir()
	sharedRoot := filepath.Join(stateRoot, "workspaces")
	if err := os.MkdirAll(sharedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(sharedRoot, "linked")); err != nil {
		t.Fatal(err)
	}
	_, _, err := workspacePathForRequest(WorkspacePrepareRequest{
		ProjectKey: "APP", RecordID: "APP-T-0001", RepoRoot: t.TempDir(), StateRoot: stateRoot,
		WorkspaceRoot: filepath.Join("workspaces", "linked", "not-created-yet"), Strategy: WorkspaceStrategyWorktree,
	})
	if err == nil || !strings.Contains(err.Error(), "shared runtime workspace directory") {
		t.Fatalf("expected missing-tail workspace symlink escape rejection, got %v", err)
	}
}

func TestWorkspaceManagerSeparatesBranchLineageForSameRecord(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	baseReq := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy, WorkRevision: 0,
	}
	base, err := manager.Prepare(baseReq)
	if err != nil {
		t.Fatal(err)
	}
	branchReq := baseReq
	branchReq.BranchName = "try/parser rewrite"
	branch, err := manager.Prepare(branchReq)
	if err != nil {
		t.Fatal(err)
	}
	if base.Path == branch.Path {
		t.Fatalf("expected branch workspace to use a separate path, got %s", base.Path)
	}
	if !strings.Contains(branch.Path, "record-1__try-parser-rewrite") {
		t.Fatalf("expected sanitized branch workspace key, got %s", branch.Path)
	}
	assertEqual(t, "try/parser rewrite", branch.Metadata.BranchName, "branch metadata")
}

func TestWorkspacePrepareReportsOnlyFirstMaterialization(t *testing.T) {
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		RepoRoot: t.TempDir(), StateRoot: t.TempDir(), Strategy: WorkspaceStrategyCopy,
	}
	first, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.NewlyMaterialized {
		t.Fatal("first workspace preparation must report a new materialization")
	}
	second, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	if second.NewlyMaterialized {
		t.Fatal("reused workspace must not report a new materialization")
	}
}

func TestWorkspaceManagerRejectsMismatchedBranchMetadata(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: "record-1", ItemID: "MEM-T-0001",
		BranchName: "branch-a", RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy, WorkRevision: 0,
	}
	if _, err := manager.Prepare(req); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(stateRoot, "workspaces", "MEM", "record-1__branch-a", ".tusker", "workspace.json")
	if err := writeText(metadataPath, `{"project_id":"project-1","record_id":"record-1","branch_name":"other","created_at":"2026-04-28T00:00:00Z"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	_, err := manager.Prepare(req)
	if err == nil || !strings.Contains(err.Error(), "branch_name does not match") {
		t.Fatalf("expected branch metadata mismatch error, got %v", err)
	}
}
