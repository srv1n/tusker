package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRegisteredProjectsMetadataOnlyDoesNotReadProjectContract(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	project := RegisteredProject{
		ProjectID:    "protected-project",
		ProjectKey:   "APP",
		Name:         "Protected project",
		RepoRoot:     filepath.Join(t.TempDir(), "missing-repo"),
		VaultRoot:    filepath.Join(t.TempDir(), "missing-vault"),
		WorkflowPath: filepath.Join(t.TempDir(), "missing-vault", "WORKFLOW.md"),
		Enabled:      true,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Project.ProjectID != project.ProjectID {
		t.Fatalf("metadata-only projects = %#v", loaded)
	}
	if loaded[0].LoadError != nil {
		t.Fatalf("metadata-only load touched the project contract: %v", loaded[0].LoadError)
	}
}

func TestResolveRegisteredProjectAcceptsIDKeyOrName(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{ProjectID: "project-123", ProjectKey: "backend", Name: "Backend", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{project.ProjectID, project.ProjectKey, project.Name} {
		loaded, err := resolveLoadedRegisteredProject(store, Args{"id": selector}, registeredProjectLoadOptions{MetadataOnly: true})
		if err != nil || loaded.Project.ProjectID != project.ProjectID {
			t.Fatalf("selector %q resolved %#v, err=%v", selector, loaded, err)
		}
	}
}

func TestProjectLoadSkipsMissingTrackerRootWithoutRepeatQuarantineWrite(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	project := RegisteredProject{
		ProjectID:    "missing-project",
		ProjectKey:   "missing",
		Name:         "Missing project",
		RepoRoot:     t.TempDir(),
		VaultRoot:    filepath.Join(t.TempDir(), "missing-vault"),
		WorkflowPath: filepath.Join(t.TempDir(), "missing-vault", "WORKFLOW.md"),
		Enabled:      true,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}

	for tick := 0; tick < 2; tick++ {
		loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded) != 1 || loaded[0].LoadError == nil {
			t.Fatalf("missing tracker root must be skipped and quarantined: %#v", loaded)
		}
		if !strings.Contains(loaded[0].LoadError.Error(), "tracker root is missing") {
			t.Fatalf("missing tracker root error = %v", loaded[0].LoadError)
		}
	}
	stored, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, projectHealthError, stored[0].Health, "missing root health")
	loadErr := requireRegisteredProjectTrackerRoot(project)
	if registeredProjectLoadErrorNeedsQuarantineWrite(stored[0], loadErr) {
		t.Fatalf("identical missing-root quarantine must not write again")
	}
}
