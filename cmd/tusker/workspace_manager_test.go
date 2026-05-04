package main

import (
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
