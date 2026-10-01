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

func TestProjectsPruneCommandDefaultsToPreviewAndRequiresApply(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	missingRoot := filepath.Join(t.TempDir(), "gone", ".tusker")
	dead := RegisteredProject{ProjectID: "dead-default", ProjectKey: "dead-default", Name: "dead-default", RepoRoot: filepath.Dir(missingRoot), VaultRoot: missingRoot, WorkflowPath: workflowPath(missingRoot), Enabled: true}
	if err := store.UpsertProject(dead); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := projectsPruneCmd(Args{}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("default prune removed registry entry: %#v", projects)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := projectsPruneCmd(Args{"apply": "true"}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, err = store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("--apply left registry entry: %#v", projects)
	}
}

func TestProjectsPrunePreservesRuntimeHistory(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	missingRoot := filepath.Join(t.TempDir(), "gone", ".tusker")
	dead := RegisteredProject{ProjectID: "dead-history", ProjectKey: "dead-history", Name: "dead-history", RepoRoot: filepath.Dir(missingRoot), VaultRoot: missingRoot, WorkflowPath: workflowPath(missingRoot), Enabled: true}
	if err := store.UpsertProject(dead); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`INSERT INTO runs (project_id, record_id, item_id) VALUES (?, ?, ?)`, dead.ProjectID, "record-1", "task-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := pruneMissingRegisteredProjects(store, false); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ProjectID != dead.ProjectID {
		t.Fatalf("prune deleted runtime history: %#v", runs)
	}
}

func TestRemoveDanglingWorkspaceMountRestatsTrackerRoot(t *testing.T) {
	root := t.TempDir()
	trackerRoot := filepath.Join(root, "tracker")
	if err := os.MkdirAll(trackerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(root, "mount")
	if err := os.Symlink(trackerRoot, mount); err != nil {
		t.Fatal(err)
	}
	removed, err := removeDanglingWorkspaceMount(mount, trackerRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Fatal("mount was removed after tracker root reappeared")
	}
	assertSymlinkTarget(t, mount, trackerRoot)
}

func TestSweepRegisteredProjectsRemovesDeadAndClearsStaleErrors(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	staleVault := pickupV7TestVault(t)
	writeDaemonServeWorkflow(t, staleVault, false, "")
	brokenVault := filepath.Join(t.TempDir(), ".tusker")
	if err := os.MkdirAll(brokenVault, 0o755); err != nil {
		t.Fatal(err)
	}
	goneVault := filepath.Join(t.TempDir(), "gone", ".tusker")
	register := func(id, vault string) {
		project := RegisteredProject{ProjectID: id, ProjectKey: id, Name: id, RepoRoot: filepath.Dir(vault), VaultRoot: vault, WorkflowPath: workflowPath(vault), Health: projectHealthError, LastError: "registered project tracker root is missing: " + vault}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
	}
	register("stale", staleVault)
	register("broken", brokenVault)
	register("gone", goneVault)
	register("gone-busy", filepath.Join(t.TempDir(), "busy", ".tusker"))
	if _, err := store.exec(`INSERT INTO runs (project_id, record_id, item_id, lease_state, workspace_path) VALUES (?, ?, ?, ?, ?)`, "gone", "r1", "t1", "retry_queued", "/tmp/leftover"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`INSERT INTO runs (project_id, record_id, item_id, lease_state, lease_owner) VALUES (?, ?, ?, ?, ?)`, "gone-busy", "r2", "t2", "retry_queued", "daemon-1"); err != nil {
		t.Fatal(err)
	}

	sweepRegisteredProjects(store)

	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]RegisteredProject{}
	for _, project := range projects {
		byID[project.ProjectID] = project
	}
	if _, ok := byID["gone"]; ok {
		t.Fatal("registration with a missing tracker root was kept")
	}
	if _, ok := byID["gone-busy"]; !ok {
		t.Fatal("registration with a live run was removed")
	}
	assertEqual(t, projectHealthDisabled, byID["stale"].Health, "stale error cleared")
	assertEqual(t, "", byID["stale"].LastError, "stale error text cleared")
	assertEqual(t, false, byID["stale"].Enabled, "heal does not enable")
	assertEqual(t, projectHealthError, byID["broken"].Health, "real load error kept")
}
