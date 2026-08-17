package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	legacySkills := []string{
		filepath.Join(home, ".agents", "skills", "obsidian-vault-tracker"),
		filepath.Join(home, ".codex", "skills", "obsidian-vault-tracker"),
		filepath.Join(home, ".claude", "skills", "obsidian-vault-tracker"),
	}
	config := filepath.Join(configHome, "tusker", "config.yaml")
	stateMarker := filepath.Join(stateRoot, "runs", "keep.txt")
	if err := writeText(filepath.Join(skill, "SKILL.md"), "skill\n"); err != nil {
		t.Fatal(err)
	}
	for _, legacySkill := range legacySkills {
		if err := writeText(filepath.Join(legacySkill, "SKILL.md"), "legacy\n"); err != nil {
			t.Fatal(err)
		}
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
	for _, legacySkill := range legacySkills {
		if _, err := os.Lstat(legacySkill); !os.IsNotExist(err) {
			t.Fatalf("legacy skill remains at %s: %v", legacySkill, err)
		}
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

func TestGlobalUninstallStateRequiresTuskerMarker(t *testing.T) {
	home := t.TempDir()
	stateRoot := filepath.Join(t.TempDir(), "empty-state")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	err := tuskerGlobalUninstallCmd(Args{"state": "true", "force-state": "true", "yes": "true", "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "not a Tusker state root") {
		t.Fatalf("expected missing-marker refusal, got %v", err)
	}
	assertExists(t, stateRoot)
}

func TestGlobalUninstallStateRefusesHomeEvenWithMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TUSKER_STATE_ROOT", home)
	if err := writeText(filepath.Join(home, "daemon.db"), "marker\n"); err != nil {
		t.Fatal(err)
	}
	err := tuskerGlobalUninstallCmd(Args{"state": "true", "force-state": "true", "yes": "true", "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "unsafe Tusker state root") {
		t.Fatalf("expected home-root refusal, got %v", err)
	}
	assertExists(t, filepath.Join(home, "daemon.db"))
}

func TestGlobalUninstallStateLockIsHeldUntilCallerCloses(t *testing.T) {
	home := t.TempDir()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeText(runtimeStoreDBPath(stateRoot), "marker\n"); err != nil {
		t.Fatal(err)
	}
	lock, busy, err := globalUninstallStateRootBusy(stateRoot)
	if err != nil || busy || lock == nil {
		t.Fatalf("acquire state lock: lock=%v busy=%t err=%v", lock, busy, err)
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()
	second, busy, err := globalUninstallStateRootBusy(stateRoot)
	if err != nil || !busy || second != nil {
		t.Fatalf("second lock acquisition raced prompt: lock=%v busy=%t err=%v", second, busy, err)
	}
}

func TestGlobalUninstallJSONReportsApplyErrors(t *testing.T) {
	home := t.TempDir()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	launchAgents := filepath.Join(home, "Library", "LaunchAgents")
	plist := filepath.Join(launchAgents, daemonServiceLabel+".plist")
	if err := ensureDir(launchAgents); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "plist-target")
	if err := writeText(target, "plist\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, plist); err != nil {
		t.Fatal(err)
	}
	oldGOOS := daemonServiceGOOS
	daemonServiceGOOS = "darwin"
	t.Cleanup(func() { daemonServiceGOOS = oldGOOS })
	var output string
	var commandErr error
	output = captureStdout(t, func() {
		commandErr = tuskerGlobalUninstallCmd(Args{"yes": "true", "json": "true"})
	})
	if commandErr == nil {
		t.Fatal("expected the symlink service action to fail")
	}
	var payload struct {
		OK       bool                     `json:"ok"`
		Outcomes []globalUninstallOutcome `json:"outcomes"`
	}
	if decodeErr := json.Unmarshal([]byte(output), &payload); decodeErr != nil {
		t.Fatalf("expected JSON envelope, got %q: %v", output, decodeErr)
	}
	if payload.OK {
		t.Fatalf("apply error reported success: %#v", payload)
	}
	if len(payload.Outcomes) == 0 {
		t.Fatalf("apply error omitted per-action outcomes: %#v", payload)
	}
}
