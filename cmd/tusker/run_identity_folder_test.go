package main

import (
	"os"
	"path/filepath"
	"testing"
)

func runIdentityFolderFixture(t *testing.T) (*RuntimeStore, string) {
	t.Helper()
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var secondRepo string
	for _, id := range []string{"project-1", "project-2"} {
		repo := t.TempDir()
		vault := filepath.Join(repo, ".tusker")
		if err := os.MkdirAll(vault, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(managedTuskerConfigPath(vault), []byte("version: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertProject(RegisteredProject{ProjectID: id, Name: id, RepoRoot: repo, VaultRoot: vault}); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRun(fairDispatchTestRun(id, "SHARED-T-0001")); err != nil {
			t.Fatal(err)
		}
		secondRepo = repo
	}
	return store, secondRepo
}

func TestRunIdentityPrefersFolderProject(t *testing.T) {
	store, repo := runIdentityFolderFixture(t)
	t.Chdir(repo)
	run, err := findRunForCLI(store, Args{}, "SHARED-T-0001")
	if err != nil || run == nil || run.ProjectID != "project-2" {
		t.Fatalf("folder lookup = %#v, %v; want project-2", run, err)
	}
	run, err = findRunForCLI(store, Args{"project": "project-1"}, "SHARED-T-0001")
	if err != nil || run == nil || run.ProjectID != "project-1" {
		t.Fatalf("explicit project lookup = %#v, %v; want project-1", run, err)
	}
}

func TestRunIdentityAmbiguousOutsideProject(t *testing.T) {
	store, _ := runIdentityFolderFixture(t)
	t.Chdir(t.TempDir())
	if _, err := findRunForCLI(store, Args{}, "SHARED-T-0001"); workSessionErrorCode(err) != "RUN_IDENTITY_AMBIGUOUS" {
		t.Fatalf("lookup outside a project = %v, want RUN_IDENTITY_AMBIGUOUS", err)
	}
}
