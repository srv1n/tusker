package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateProjectStorageBoundaryRequiresRepoLocalVault(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".tusker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateProjectStorageBoundary(repo, filepath.Join(repo, ".tusker")); err != nil {
		t.Fatalf("repo-local vault should pass: %v", err)
	}
	for _, vault := range []string{filepath.Join(root, "shared-vault"), filepath.Join(root, "app-old", ".tusker")} {
		err := validateProjectStorageBoundary(repo, vault)
		if err == nil || !strings.Contains(err.Error(), "must live inside") {
			t.Fatalf("outside vault %q should fail: %v", vault, err)
		}
	}
}

func TestValidateProjectStorageBoundaryRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "app")
	outside := filepath.Join(root, "outside-vault")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	vaultLink := filepath.Join(repo, ".tusker")
	if err := os.Symlink(outside, vaultLink); err != nil {
		t.Fatal(err)
	}
	if err := validateProjectStorageBoundary(repo, vaultLink); err == nil {
		t.Fatal("vault symlink escaping the repository should fail")
	}
}

func TestValidateProjectStorageBoundaryRejectsSymlinkEscapeWithMissingVault(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(repo, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	err := validateProjectStorageBoundary(repo, filepath.Join(link, "not-created-yet"))
	if err == nil || !strings.Contains(err.Error(), "must live inside") {
		t.Fatalf("expected missing-tail symlink escape rejection, got %v", err)
	}
}
