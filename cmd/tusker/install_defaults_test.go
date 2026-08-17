package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallWithoutBinDoesNotCreateBinarySymlink(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := installCmd(Args{"bin-dir": binDir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(binDir, "tusker")); !os.IsNotExist(err) {
		t.Fatalf("install without --bin created a binary link: %v", err)
	}
}

func TestInitYesWritesVaultOnly(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(repo, "state"))
	withWorkingDirectory(t, repo)

	if err := initCmd(Args{"yes": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".tusker")); err != nil {
		t.Fatalf("init --yes did not create the vault: %v", err)
	}
	for _, path := range []string{
		filepath.Join(repo, "AGENTS.md"),
		filepath.Join(repo, "CLAUDE.md"),
		filepath.Join(repo, ".github"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("init --yes wrote opt-in path %s: %v", path, err)
		}
	}
}

func TestInitWithPointersWritesPointerBlock(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(repo, "state"))
	withWorkingDirectory(t, repo)

	if err := initCmd(Args{"yes": "true", "with-pointers": "true"}); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), tuskerPointerBegin) || !strings.Contains(string(content), tuskerPointerEnd) {
		t.Fatalf("pointer block missing from AGENTS.md:\n%s", content)
	}
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}
