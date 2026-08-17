package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalUninstallDryRunListsItemsWithoutDeleting(t *testing.T) {
	home := t.TempDir()
	stateRoot := filepath.Join(t.TempDir(), "state")
	configHome := filepath.Join(t.TempDir(), "config")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TUSKER_CONFIG", "")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)

	skill := filepath.Join(home, ".agents", "skills", currentSkillInstallDir)
	config := filepath.Join(configHome, "tusker", "config.yaml")
	stateMarker := filepath.Join(stateRoot, "daemon.db")
	if err := writeText(filepath.Join(skill, "SKILL.md"), "skill\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(config, "tier: one\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(stateMarker, "history\n"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := tuskerGlobalUninstallCmd(Args{}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{"global uninstall dry-run", skill, config, "[exists]", "Registered projects", "tusker purge --repo <r>"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("dry-run output missing %q:\n%s", expected, output)
		}
	}
	assertExists(t, skill)
	assertExists(t, config)
	assertExists(t, stateMarker)
}

func TestGlobalUninstallYesRemovesSkillsAndConfigButNotState(t *testing.T) {
	home := t.TempDir()
	stateRoot := filepath.Join(t.TempDir(), "state")
	configHome := filepath.Join(t.TempDir(), "config")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("TUSKER_CONFIG", "")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)

	skill := filepath.Join(home, ".codex", "skills", currentSkillInstallDir)
	config := filepath.Join(configHome, "tusker", "config.yaml")
	stateMarker := filepath.Join(stateRoot, "runs", "keep.txt")
	if err := writeText(filepath.Join(skill, "SKILL.md"), "skill\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(config, "tier: one\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(stateMarker, "history\n"); err != nil {
		t.Fatal(err)
	}

	if err := tuskerGlobalUninstallCmd(Args{"yes": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(skill); !os.IsNotExist(err) {
		t.Fatalf("skill install remains: %v", err)
	}
	if _, err := os.Lstat(config); !os.IsNotExist(err) {
		t.Fatalf("config remains: %v", err)
	}
	assertExists(t, stateMarker)
}

func TestGlobalUninstallRefusesNonTuskerBinSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	binDir := filepath.Join(home, ".local", "bin")
	other := filepath.Join(t.TempDir(), "other-tool")
	if err := writeText(other, "#!/bin/sh\nexit 0\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(other, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(binDir, "tusker")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}

	for _, action := range planGlobalUninstall() {
		if action.Path == link {
			t.Fatalf("non-Tusker binary was included in uninstall plan: %#v", action)
		}
	}
	if err := tuskerGlobalUninstallCmd(Args{"yes": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, link)
}
