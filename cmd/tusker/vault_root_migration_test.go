package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVaultRootMigrationRejectsUndiscoverableDestinationBeforeMutation(t *testing.T) {
	repo, source := newVaultRootMigrationFixture(t)
	destination := filepath.Join(repo, "tracker")
	if err := migrateVaultRootCmd(Args{"vault": source, "to": destination}); err == nil || !strings.Contains(err.Error(), "supported discoverable") {
		t.Fatalf("undiscoverable destination accepted: %v", err)
	}
	assertExists(t, filepath.Join(source, "SKILL.md"))
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("undiscoverable destination was created: %v", err)
	}
}

func TestVaultRootMigrationValidatesBeforeMoving(t *testing.T) {
	repo, source := newVaultRootMigrationFixture(t)
	configPath := managedTuskerConfigPath(source)
	if err := writeText(configPath, "storage: [\n"); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(repo, defaultRepoVaultDir)
	if err := migrateVaultRootCmd(Args{"vault": source, "to": defaultRepoVaultDir}); err == nil {
		t.Fatal("invalid config was accepted")
	}
	assertExists(t, filepath.Join(source, "SKILL.md"))
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("invalid preflight moved vault: %v", err)
	}
}

func TestVaultRootMigrationRollsBackWriteFailure(t *testing.T) {
	repo, source := newVaultRootMigrationFixture(t)
	configBefore := mustReadIndexTest(t, managedTuskerConfigPath(source))
	if err := writeText(filepath.Join(repo, "AGENTS.md"), "keep agents\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "CLAUDE.md"), "keep claude\n"); err != nil {
		t.Fatal(err)
	}
	agentsBefore := mustReadIndexTest(t, filepath.Join(repo, "AGENTS.md"))
	claudeBefore := mustReadIndexTest(t, filepath.Join(repo, "CLAUDE.md"))

	originalWrite := vaultRootMigrationWrite
	failOnce := true
	vaultRootMigrationWrite = func(path string, content []byte, mode os.FileMode) error {
		if path == filepath.Join(repo, "CLAUDE.md") && failOnce {
			failOnce = false
			return errors.New("injected pointer write failure")
		}
		return originalWrite(path, content, mode)
	}
	t.Cleanup(func() { vaultRootMigrationWrite = originalWrite })

	if err := migrateVaultRootCmd(Args{"vault": source, "to": defaultRepoVaultDir}); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("write failure did not report a clean rollback: %v", err)
	}
	assertExists(t, filepath.Join(source, "SKILL.md"))
	if _, err := os.Stat(filepath.Join(repo, defaultRepoVaultDir)); !os.IsNotExist(err) {
		t.Fatalf("failed migration retained destination: %v", err)
	}
	if got := mustReadIndexTest(t, managedTuskerConfigPath(source)); got != configBefore {
		t.Fatalf("rollback changed config:\n%s", got)
	}
	if got := mustReadIndexTest(t, filepath.Join(repo, "AGENTS.md")); got != agentsBefore {
		t.Fatalf("rollback changed AGENTS.md:\n%s", got)
	}
	if got := mustReadIndexTest(t, filepath.Join(repo, "CLAUDE.md")); got != claudeBefore {
		t.Fatalf("rollback changed CLAUDE.md:\n%s", got)
	}
	discovered, err := discoverVault(repo)
	if err != nil || !samePath(discovered, source) {
		t.Fatalf("rollback discovery=%q err=%v, want %q", discovered, err, source)
	}
}

func TestVaultRootMigrationPostconditionDiscoversDestination(t *testing.T) {
	repo, source := newVaultRootMigrationFixture(t)
	destination := filepath.Join(repo, defaultRepoVaultDir)
	if err := migrateVaultRootCmd(Args{"vault": source, "to": defaultRepoVaultDir}); err != nil {
		t.Fatal(err)
	}
	discovered, err := discoverVault(repo)
	if err != nil || !samePath(discovered, destination) {
		t.Fatalf("discovery=%q err=%v, want %q", discovered, err, destination)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source remained after migration: %v", err)
	}
	config := mustReadIndexTest(t, managedTuskerConfigPath(destination))
	if !strings.Contains(config, "root: .tusker") {
		t.Fatalf("destination config did not describe discoverable root:\n%s", config)
	}
}

func newVaultRootMigrationFixture(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	source := filepath.Join(repo, legacyRepoVaultDir)
	if err := writeText(filepath.Join(source, "SKILL.md"), "# fixture\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(source), "schema: tusker.config/v1\nstorage:\n  root: tusker\n"); err != nil {
		t.Fatal(err)
	}
	return repo, source
}
