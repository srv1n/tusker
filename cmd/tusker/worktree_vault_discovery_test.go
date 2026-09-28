package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkedWorktreeUsesRegisteredCanonicalVaultAndRefusesImplicitDuplicate(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "tusker-test@example.invalid")
	runGitDir(t, repo, "config", "user.name", "Tusker Test")
	canonicalVault := filepath.Join(repo, defaultRepoVaultDir)
	if err := ensureDir(canonicalVault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(canonicalVault), "schema: tusker.config/v1\nproject_id: backend\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", defaultRepoVaultDir)
	runGitDir(t, repo, "commit", "-m", "project config")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := newRegisteredProject(repo, canonicalVault)
	project.ProjectID = "01PROJECTULID"
	project.ProjectKey = "backend"
	project.Enabled = true
	if err := store.UpsertProject(project); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	feature := filepath.Join(t.TempDir(), "feature")
	runGitDir(t, repo, "worktree", "add", "-b", "feature", feature, "main")
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(feature); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	t.Setenv("TUSKER_PROJECT_ID", "")
	t.Setenv("TUSKER_CANONICAL_VAULT", "")
	t.Setenv("TUSKER_VAULT", "")

	if _, err := resolveVaultPath(Args{}, false); err == nil {
		t.Fatal("missing-vault route unexpectedly succeeded")
	} else if !strings.Contains(err.Error(), "canonical vault-owning checkout") {
		t.Fatalf("missing-vault route did not explain canonical project vault: %v", err)
	} else if typed, ok := err.(*TuskerError); !ok || !strings.Contains(typed.Hint, "--use-project-vault") {
		t.Fatalf("missing-vault route omitted use-project-vault hint: %#v", err)
	}
	t.Setenv("TUSKER_PROJECT_ID", project.ProjectID)
	t.Setenv("TUSKER_CANONICAL_VAULT", canonicalVault)
	if _, err := resolveVaultPath(Args{}, false); err == nil || !strings.Contains(err.Error(), "canonical vault-owning checkout") {
		t.Fatalf("vault env without attempt unexpectedly succeeded: %v", err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	if got, err := resolveVaultPath(Args{}, false); err != nil || got != project.VaultRoot {
		t.Fatalf("dispatched attempt resolved %q, %v; want %q", got, err, project.VaultRoot)
	}
	t.Setenv("TUSKER_PROJECT_ID", "other-project")
	if _, err := resolveVaultPath(Args{}, false); err == nil || !strings.Contains(err.Error(), "canonical vault-owning checkout") {
		t.Fatalf("mismatched project ID unexpectedly succeeded: %v", err)
	}
	t.Setenv("TUSKER_PROJECT_ID", project.ProjectID)
	t.Setenv("TUSKER_CANONICAL_VAULT", filepath.Join(feature, defaultRepoVaultDir))
	if _, err := resolveVaultPath(Args{}, false); err == nil || !strings.Contains(err.Error(), "canonical vault-owning checkout") {
		t.Fatalf("mismatched canonical vault unexpectedly succeeded: %v", err)
	}
	t.Setenv("TUSKER_CANONICAL_VAULT", "")
	t.Setenv("TUSKER_VAULT", canonicalVault)
	if got, err := resolveVaultPath(Args{}, false); err != nil || got != project.VaultRoot {
		t.Fatalf("dispatched attempt fallback resolved %q, %v; want %q", got, err, project.VaultRoot)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	got, err := resolveVaultPath(Args{"use-project-vault": "true"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonicalProjectPath(canonicalVault) {
		t.Fatalf("use-project-vault resolved %q, want %q", got, canonicalProjectPath(canonicalVault))
	}

	dbPath := runtimeStoreDBPath(stateRoot)
	dbBefore, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := initCmd(Args{"yes": "true"}); err == nil || !strings.Contains(err.Error(), "second Tusker vault") {
		t.Fatalf("implicit init did not refuse duplicate project graph: %v", err)
	} else if typed, ok := err.(*TuskerError); !ok || !strings.Contains(typed.Hint, "--isolated-vault") {
		t.Fatalf("implicit init omitted isolated-vault hint: %#v", err)
	}
	dbAfter, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(dbAfter) != string(dbBefore) {
		t.Fatal("duplicate init refusal mutated runtime state before the guard")
	}
	if fileExists(workflowPath(filepath.Join(feature, defaultRepoVaultDir))) {
		t.Fatal("implicit init created a duplicate worktree vault")
	}
	if err := initCmd(Args{"yes": "true", "isolated-vault": "true"}); err != nil {
		t.Fatalf("explicit isolated-vault init failed: %v", err)
	}
	if !isVaultDir(filepath.Join(feature, defaultRepoVaultDir)) {
		t.Fatal("explicit isolated-vault init did not create the requested feature vault")
	}
	brokenState := t.TempDir()
	brokenDB := runtimeStoreDBPath(brokenState)
	if err := writeText(brokenDB, "not a sqlite database\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(brokenDB, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_STATE_ROOT", brokenState)
	if err := initCmd(Args{"yes": "true"}); err == nil || !strings.Contains(err.Error(), "cannot inspect the Tusker project registry") {
		t.Fatalf("init did not fail closed when project registry lookup was unavailable: %v", err)
	}
	malformedRepo := t.TempDir()
	runGitDir(t, malformedRepo, "init", "-b", "main")
	if err := writeText(managedTuskerConfigPath(filepath.Join(malformedRepo, defaultRepoVaultDir)), "project_id: [broken\n"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := discoverRegisteredProjectVault(malformedRepo); err == nil || !strings.Contains(err.Error(), "cannot resolve Tusker project config") {
		t.Fatalf("malformed project config did not fail closed: %v", err)
	}
}
