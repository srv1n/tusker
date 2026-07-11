package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectsPruneRemovesMissingRegistrationsAndDanglingMounts(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	root := t.TempDir()
	aliveRoot := filepath.Join(root, "alive", ".tusker")
	missingRoot := filepath.Join(root, "gone", ".tusker")
	if err := os.MkdirAll(aliveRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	alive := RegisteredProject{ProjectID: "alive", ProjectKey: "alive", Name: "alive", RepoRoot: filepath.Dir(aliveRoot), VaultRoot: aliveRoot, WorkflowPath: workflowPath(aliveRoot), Enabled: true}
	dead := RegisteredProject{ProjectID: "dead", ProjectKey: "dead", Name: "dead", RepoRoot: filepath.Dir(missingRoot), VaultRoot: missingRoot, WorkflowPath: workflowPath(missingRoot), Enabled: true}
	for _, project := range []RegisteredProject{alive, dead} {
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
	}

	obsidian := filepath.Join(root, "obsidian")
	if err := os.MkdirAll(obsidian, 0o755); err != nil {
		t.Fatal(err)
	}
	aliveMount := filepath.Join(obsidian, "alive")
	deadMount := filepath.Join(obsidian, "dead")
	if err := os.Symlink(aliveRoot, aliveMount); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(missingRoot, deadMount); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkspaceVaultConfig(WorkspaceVaultConfig{ObsidianVault: obsidian, Projects: []WorkspaceProject{
		{ProjectID: alive.ProjectID, TrackerRoot: alive.VaultRoot, MountName: "alive"},
		{ProjectID: dead.ProjectID, TrackerRoot: dead.VaultRoot, MountName: "dead"},
	}}); err != nil {
		t.Fatal(err)
	}

	report, err := pruneMissingRegisteredProjects(store, false)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{dead.ProjectID}, report.RemovedProjects, "pruned project")
	assertEqual(t, []string{deadMount}, report.RemovedMounts, "pruned mount")
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(projects), "remaining registrations")
	assertEqual(t, alive.ProjectID, projects[0].ProjectID, "live registration remains")
	assertSymlinkTarget(t, aliveMount, aliveRoot)
	if _, err := os.Lstat(deadMount); !os.IsNotExist(err) {
		t.Fatalf("expected dead mount removal, got %v", err)
	}
	workspace, err := loadWorkspaceVaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(workspace.Projects), "remaining workspace projects")
	assertEqual(t, alive.ProjectID, workspace.Projects[0].ProjectID, "live workspace mount remains")
}

func TestProjectsPruneDryRunLeavesRegistryAndMountsUntouched(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	missingRoot := filepath.Join(t.TempDir(), "gone", ".tusker")
	dead := RegisteredProject{ProjectID: "dead", ProjectKey: "dead", Name: "dead", RepoRoot: filepath.Dir(missingRoot), VaultRoot: missingRoot, WorkflowPath: workflowPath(missingRoot), Enabled: true}
	if err := store.UpsertProject(dead); err != nil {
		t.Fatal(err)
	}
	obsidian := t.TempDir()
	mount := filepath.Join(obsidian, "dead")
	if err := os.Symlink(missingRoot, mount); err != nil {
		t.Fatal(err)
	}
	if err := saveWorkspaceVaultConfig(WorkspaceVaultConfig{ObsidianVault: obsidian, Projects: []WorkspaceProject{{ProjectID: dead.ProjectID, TrackerRoot: dead.VaultRoot, MountName: "dead"}}}); err != nil {
		t.Fatal(err)
	}

	report, err := pruneMissingRegisteredProjects(store, true)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, report.DryRun, "dry run")
	assertEqual(t, []string{dead.ProjectID}, report.RemovedProjects, "dry-run project report")
	assertEqual(t, []string{mount}, report.RemovedMounts, "dry-run mount report")
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(projects), "dry-run registration remains")
	if _, err := os.Lstat(mount); err != nil {
		t.Fatalf("dry-run mount should remain: %v", err)
	}
}
