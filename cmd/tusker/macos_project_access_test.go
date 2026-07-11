package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSProtectedProjectsDetectsProtectedRootsWithoutPrefixFalsePositives(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "Users", "example")
	projects := []RegisteredProject{
		{ProjectID: "p-downloads", Name: "downloads", RepoRoot: filepath.Join(home, "Downloads", "app"), Enabled: true},
		{ProjectID: "p-desktop", Name: "desktop", RepoRoot: filepath.Join(home, "Desktop", "app"), Enabled: true},
		{ProjectID: "p-icloud", Name: "icloud", RepoRoot: filepath.Join(home, "Library", "Mobile Documents", "Drive", "app"), Enabled: true},
		{ProjectID: "p-safe", Name: "safe", RepoRoot: filepath.Join(home, "Developer", "app"), Enabled: true},
		{ProjectID: "p-cloud", Name: "cloud advisory", RepoRoot: filepath.Join(home, "Library", "CloudStorage", "Drive", "app"), Enabled: true},
		{ProjectID: "p-external", Name: "external advisory", RepoRoot: filepath.Join(string(os.PathSeparator), "Volumes", "Work", "app"), Enabled: true},
		{ProjectID: "p-prefix", Name: "prefix", RepoRoot: filepath.Join(home, "Downloads-old", "app"), Enabled: true},
		{ProjectID: "p-case", Name: "case-sensitive sibling", RepoRoot: filepath.Join(home, "downloads", "app"), Enabled: true},
		{ProjectID: "p-disabled", Name: "disabled", RepoRoot: filepath.Join(home, "Documents", "app"), Enabled: false},
	}
	issues := macOSProtectedProjects(projects, home)
	if len(issues) != 3 {
		t.Fatalf("protected project count = %d, want 3: %#v", len(issues), issues)
	}
	wantLocations := map[string]string{
		"p-desktop": "Desktop", "p-downloads": "Downloads", "p-icloud": "iCloud Drive",
	}
	for _, issue := range issues {
		if want := wantLocations[issue.ProjectID]; issue.Location != want {
			t.Fatalf("project %s location = %q, want %q", issue.ProjectID, issue.Location, want)
		}
	}
}

func TestMacOSProtectedProjectsResolvesSymlinkedRepoRoot(t *testing.T) {
	home := t.TempDir()
	protectedRepo := filepath.Join(home, "Downloads", "project")
	if err := os.MkdirAll(protectedRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(home, "Developer", "project-link")
	if err := os.MkdirAll(filepath.Dir(linkRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(protectedRepo, linkRoot); err != nil {
		t.Fatal(err)
	}
	issues := macOSProtectedProjects([]RegisteredProject{{ProjectID: "p1", RepoRoot: linkRoot, Enabled: true}}, home)
	if len(issues) != 1 || issues[0].Location != "Downloads" {
		t.Fatalf("symlinked protected project not detected: %#v", issues)
	}
}

func TestMacOSProtectedProjectsKeepsLexicallyProtectedSymlink(t *testing.T) {
	home := t.TempDir()
	safeRepo := filepath.Join(home, "Developer", "project")
	if err := os.MkdirAll(safeRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	protectedLink := filepath.Join(home, "Downloads", "project-link")
	if err := os.MkdirAll(filepath.Dir(protectedLink), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(safeRepo, protectedLink); err != nil {
		t.Fatal(err)
	}
	issues := macOSProtectedProjects([]RegisteredProject{{ProjectID: "p1", RepoRoot: protectedLink, Enabled: true}}, home)
	if len(issues) != 1 || issues[0].Location != "Downloads" {
		t.Fatalf("lexically protected symlink not detected: %#v", issues)
	}
}

func TestRequireDaemonServiceProjectAccessBlocksBeforeLaunchUnlessExplicitlyAllowed(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	home := filepath.Join(t.TempDir(), "home")
	repo := filepath.Join(home, "Downloads", "app")
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := RegisteredProject{
		ProjectID: "project-1", ProjectKey: "app", Name: "app",
		RepoRoot: repo, VaultRoot: vault, WorkflowPath: workflowPath(vault),
		Enabled: true, Health: projectHealthHealthy,
	}
	if err := store.UpsertProject(project); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	config := daemonServiceConfig{StateRoot: stateRoot, Home: home, Executable: filepath.Join(stateRoot, "bin", "tusker-daemon")}
	err = requireDaemonServiceProjectAccess(Args{}, config)
	var typed *TuskerError
	if err == nil || !errors.As(err, &typed) || !strings.Contains(err.Error(), "macOS-protected Downloads") || !strings.Contains(typed.Hint, "--allow-protected-projects") {
		t.Fatalf("expected actionable protected-project refusal, got %v", err)
	}
	if err := requireDaemonServiceProjectAccess(Args{"allow-protected-projects": "true"}, config); err != nil {
		t.Fatalf("explicit protected-project override should pass: %v", err)
	}
}

func TestDaemonServiceProtectedProjectsIgnoresDisabledRegistrations(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	home := filepath.Join(t.TempDir(), "home")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(RegisteredProject{
		ProjectID: "project-1", Name: "disabled", RepoRoot: filepath.Join(home, "Documents", "app"), Enabled: false, Health: projectHealthDisabled,
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	issues, err := daemonServiceProtectedProjects(daemonServiceConfig{StateRoot: stateRoot, Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("disabled registration should not block service startup: %#v", issues)
	}
}

func TestRequireDaemonServiceProjectAccessValidatesSafeProjectWorkflow(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	home := filepath.Join(t.TempDir(), "home")
	repo := filepath.Join(home, "Developer", "app")
	vault := filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(RegisteredProject{
		ProjectID: "project-1", Name: "app", RepoRoot: repo, VaultRoot: vault,
		WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy,
	}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	err = requireDaemonServiceProjectAccess(Args{}, daemonServiceConfig{StateRoot: stateRoot, Home: home})
	if err == nil || !strings.Contains(err.Error(), "invalid workflow contract") {
		t.Fatalf("missing safe-project workflow should fail before launch: %v", err)
	}
}
