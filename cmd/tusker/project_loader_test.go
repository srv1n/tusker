package main

import (
	"path/filepath"
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
