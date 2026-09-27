package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProjectRepoSelectorIgnoresCwdAndNamesCandidates(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	// Same-length roots: path-length scores tie, like side/tusker and side/kurpod.
	named, standing := filepath.Join(root, "kurpod"), filepath.Join(root, "tusker")
	for _, dir := range []string{named, standing} {
		if err := os.MkdirAll(filepath.Join(dir, ".tusker"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for id, dir := range map[string]string{"project-kurpod": named, "project-tusker": standing} {
		if err := store.UpsertProject(RegisteredProject{ProjectID: id, ProjectKey: id, Name: id, RepoRoot: dir, VaultRoot: filepath.Join(dir, ".tusker"), Enabled: true, Health: projectHealthHealthy}); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(standing)
	opts := registeredProjectLoadOptions{LoadDisabled: true, MetadataOnly: true}
	loaded, err := resolveLoadedRegisteredProject(store, Args{"repo": named}, opts)
	if err != nil {
		t.Fatalf("--repo from inside another project: %v", err)
	}
	assertEqual(t, "project-kurpod", loaded.Project.ProjectID, "--repo selects the named project")

	// A genuine tie (a stale row registered through a symlink alias of the
	// same repo, like /tmp vs /private/tmp) names the candidates.
	alias := filepath.Join(root, "kurpod-link")
	if err := os.Symlink(named, alias); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(RegisteredProject{ProjectID: "project-kurpod-alias", ProjectKey: "project-kurpod-alias", Name: "alias", RepoRoot: alias, VaultRoot: filepath.Join(alias, ".tusker"), Enabled: false, Health: projectHealthDisabled}); err != nil {
		t.Fatal(err)
	}
	_, err = resolveLoadedRegisteredProject(store, Args{"repo": named}, opts)
	if err == nil || !strings.Contains(err.Error(), "project-kurpod (") || !strings.Contains(err.Error(), "project-kurpod-alias (") {
		t.Fatalf("ambiguous --repo must list candidate ids, got %v", err)
	}
}
