package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMirrorApplyInputsIntoWorkspaceCopiesOnlyCurrentInput(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	artifactPath := filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "fix.patch")
	if err := writeText(artifactPath, "diff --git a/README.md b/README.md\n"); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "stale.patch")
	if err := writeText(stalePath, "old cycle patch\n"); err != nil {
		t.Fatal(err)
	}
	hash, err := sha256Path(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.UpsertApplyInput(RuntimeApplyInput{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "chatgpt-browser", JobID: "cgpt", Path: artifactPath, RelPath: "architect/APP-T-0001/fix.patch", Sha256: hash, Kind: "patch"})
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := mirrorApplyInputsIntoWorkspace(store, project, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", CloudTaskID: "cgpt"}, workspace); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(workspace, "architect", "APP-T-0001", "fix.patch"))
	if fileExists(filepath.Join(workspace, "architect", "APP-T-0001", "stale.patch")) {
		t.Fatal("mirror copied an unbound prior-cycle artifact")
	}
}

func installFakeExternalCollectFetcher(t *testing.T, artifactDir string, files []string) {
	t.Helper()
	previous := runExternalCollectFetch
	runExternalCollectFetch = func(ctx context.Context, req externalFetchRequest) (externalFetchResult, error) {
		return externalFetchResult{JobID: req.JobID, ArtifactDir: artifactDir, Files: files}, nil
	}
	t.Cleanup(func() { runExternalCollectFetch = previous })
}

func writeExternalFetchFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
