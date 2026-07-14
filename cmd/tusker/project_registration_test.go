package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistrationIsIdempotentAcrossCanonicalPaths(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}

	first := newRegisteredProject(repo, vault)
	registered, created, err := store.RegisterProject(first)
	if err != nil || !created {
		t.Fatalf("first registration: created=%v err=%v", created, err)
	}
	duplicate := newRegisteredProject(alias, filepath.Join(alias, ".tusker"))
	existing, created, err := store.RegisterProject(duplicate)
	if err != nil || created {
		t.Fatalf("duplicate registration: created=%v err=%v", created, err)
	}
	if existing.ProjectID != registered.ProjectID {
		t.Fatalf("duplicate returned %s, want %s", existing.ProjectID, registered.ProjectID)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registrations: err=%v projects=%#v", err, projects)
	}
}

func TestReconcileDuplicateProjectsKeepsAuthoritativeMetadataAndHistory(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.exec(`DROP INDEX projects_repo_root_unique`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP INDEX projects_vault_root_unique`); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	disabled := newRegisteredProject(repo, vault)
	disabled.ProjectID = "duplicate-disabled"
	disabled.Enabled = false
	disabled.Health = projectHealthDisabled
	enabled := newRegisteredProject(repo, vault)
	enabled.ProjectID = "authoritative-enabled"
	enabled.Enabled = true
	enabled.Health = projectHealthHealthy
	enabled.LastPollAt = "2026-07-14T00:00:00Z"
	for _, project := range []RegisteredProject{disabled, enabled} {
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.exec(`INSERT INTO runs (project_id, record_id, item_id) VALUES (?, ?, ?)`, disabled.ProjectID, "record-1", "task-1"); err != nil {
		t.Fatal(err)
	}

	removed, err := store.ReconcileDuplicateProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != disabled.ProjectID {
		t.Fatalf("removed=%#v", removed)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 || projects[0].ProjectID != enabled.ProjectID {
		t.Fatalf("authoritative registration: err=%v projects=%#v", err, projects)
	}
	runs, err := store.ListRuns()
	if err != nil || len(runs) != 1 || runs[0].ProjectID != disabled.ProjectID {
		t.Fatalf("historical rows were not preserved: err=%v runs=%#v", err, runs)
	}
}

func TestOpenRuntimeStoreAutomaticallyReconcilesDuplicateProjects(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP INDEX projects_repo_root_unique`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP INDEX projects_vault_root_unique`); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	first := newRegisteredProject(repo, vault)
	first.ProjectID = "older-disabled"
	first.Health = projectHealthDisabled
	second := newRegisteredProject(repo, vault)
	second.ProjectID = "newer-enabled"
	second.Enabled = true
	second.Health = projectHealthHealthy
	for _, project := range []RegisteredProject{first, second} {
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 || projects[0].ProjectID != second.ProjectID {
		t.Fatalf("startup reconciliation: err=%v projects=%#v", err, projects)
	}
}
