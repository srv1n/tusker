package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveVaultPathExplainsMissingRegisteredVault(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := newRegisteredProject(repo, filepath.Join(repo, defaultRepoVaultDir))
	project.ProjectID = "01VANISHEDULID"
	project.ProjectKey = "cinta"
	project.Enabled = true
	if err := store.UpsertProject(project); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	_, err = resolveVaultPath(Args{}, false)
	if err == nil || !strings.Contains(err.Error(), "that vault is missing") || !strings.Contains(err.Error(), "01VANISHEDULID") {
		t.Fatalf("vanished vault not explained: %v", err)
	}
	if typed, ok := err.(*TuskerError); !ok || !strings.Contains(typed.Hint, "tusker init") {
		t.Fatalf("vanished vault error omitted init hint: %#v", err)
	}
}

func TestBareStatusPrintsVaultAndEmptyEpicSummary(t *testing.T) {
	vault := filepath.Join(t.TempDir(), defaultRepoVaultDir)
	if err := bootstrapV7Profile(vault, ""); err != nil {
		t.Fatal(err)
	}
	var statusErr error
	output := captureStdout(t, func() { statusErr = statusCmd(Args{"vault": vault}) })
	if statusErr != nil {
		t.Fatalf("bare status failed: %v", statusErr)
	}
	if !strings.Contains(output, "Vault: "+vault) || !strings.Contains(output, "No epics in this vault (try --type task).") {
		t.Fatalf("bare status output:\n%s", output)
	}
	var listErr error
	output = captureStdout(t, func() { listErr = listCmd(Args{"vault": vault, "type": "task"}) })
	if listErr != nil || !strings.Contains(output, "(no matches)") {
		t.Fatalf("filtered empty list: err=%v output:\n%s", listErr, output)
	}
}
