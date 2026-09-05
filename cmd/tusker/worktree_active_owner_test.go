package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeCapPreservesStaleWorkspaceWhenRuntimeStateUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		corruptDB bool
	}{
		{name: "missing"},
		{name: "corrupt", corruptDB: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stateRoot := t.TempDir()
			if tc.corruptDB {
				if err := os.WriteFile(filepath.Join(stateRoot, "daemon.db"), []byte("not sqlite"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			manager := NewWorkspaceManager()
			req := WorkspacePrepareRequest{
				ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
				RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy,
			}
			prepared, err := manager.Prepare(req)
			if err != nil {
				t.Fatal(err)
			}
			markWorkspaceMetadataPIDDead(t, prepared.Path)
			sentinel := filepath.Join(prepared.Path, "keep.txt")
			if err := os.WriteFile(sentinel, []byte("keep me\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			req.RecordID = "APP-T-0002"
			req.ItemID = "APP-T-0002"
			req.MaxLiveWorktrees = 1
			if _, err := manager.Prepare(req); err == nil || !strings.Contains(err.Error(), "another live work copy") {
				t.Fatalf("missing or corrupt runtime state must refuse conservatively, got %v", err)
			}
			if content, err := os.ReadFile(sentinel); err != nil || string(content) != "keep me\n" {
				t.Fatalf("stale workspace was removed or changed: content=%q err=%v", content, err)
			}
		})
	}
}

func TestWorktreeCapPreservesWorkspaceWithActiveRuntimeOwner(t *testing.T) {
	stateRoot := t.TempDir()
	manager := NewWorkspaceManager()
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy,
	}
	prepared, err := manager.Prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	markWorkspaceMetadataPIDDead(t, prepared.Path)
	sentinel := filepath.Join(prepared.Path, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec),
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1", WorkspacePath: prepared.Path,
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	req.RecordID = "APP-T-0002"
	req.ItemID = "APP-T-0002"
	req.MaxLiveWorktrees = 1
	if _, err := manager.Prepare(req); err == nil || !strings.Contains(err.Error(), "another live work copy") {
		t.Fatalf("active runtime owner must keep stale workspace counted, got %v", err)
	}
	if content, err := os.ReadFile(sentinel); err != nil || string(content) != "keep me\n" {
		t.Fatalf("active workspace was removed or changed: content=%q err=%v", content, err)
	}
}

func markWorkspaceMetadataPIDDead(t *testing.T, path string) {
	t.Helper()
	metadataPath := filepath.Join(path, ".tusker", "workspace.json")
	text, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata WorkspaceMetadata
	if err := json.Unmarshal(text, &metadata); err != nil {
		t.Fatal(err)
	}
	metadata.PID = 2147483000
	raw, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
