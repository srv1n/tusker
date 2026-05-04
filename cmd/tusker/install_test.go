package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallSkillPayloadRemovesStaleFiles(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "tusker")
	stale := filepath.Join(destination, "assets", "templates", "story.md")
	if err := writeText(stale, "---\nschema_version: 2\n---\n"); err != nil {
		t.Fatal(err)
	}

	if err := installSkillPayload(destination); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file still exists after skill update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "SKILL.md")); err != nil {
		t.Fatalf("expected skill payload to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "assets", "templates", "task.md")); err != nil {
		t.Fatalf("expected current task template to be installed: %v", err)
	}
}

func TestInitHonorsExplicitNewVaultInsideExistingRepo(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := ensureDir(repo); err != nil {
		t.Fatal(err)
	}
	existingVault := filepath.Join(repo, "tusker")
	if err := bootstrap(Args{"vault": existingVault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	explicitVault := filepath.Join(root, "explicit", "tracker")
	if err := initCmd(Args{"vault": explicitVault, "yes": "true", "vault-only": "true", "no-mount": "true"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(explicitVault, "_system", "views", "Tasks.base"))
	assertExists(t, filepath.Join(explicitVault, "_system", "templates", "task.md"))
}
