package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSkillPayloadRemovesStaleFiles(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "tusker")
	stale := filepath.Join(destination, "assets", "templates", "story.md")
	if err := writeText(stale, "---\nschema_version: 2\n---\n"); err != nil {
		t.Fatal(err)
	}
	staleReference := filepath.Join(destination, "references", "COMMANDS.md")
	if err := writeText(staleReference, "stale commands\n"); err != nil {
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
	referenceContent, err := os.ReadFile(staleReference)
	if err != nil {
		t.Fatalf("expected current commands reference to be installed: %v", err)
	}
	if strings.Contains(string(referenceContent), "stale commands") || !strings.Contains(string(referenceContent), "# Tusker Commands") {
		t.Fatalf("expected stale reference content to be replaced, got:\n%s", string(referenceContent))
	}
}

func TestInstallCommandRefreshesExistingUserSkillsByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	destinations := []string{
		filepath.Join(home, ".agents", "skills", "tusker"),
		filepath.Join(home, ".codex", "skills", "tusker"),
		filepath.Join(home, ".claude", "skills", "tusker"),
	}
	for _, destination := range destinations {
		if err := writeText(filepath.Join(destination, "assets", "templates", "story.md"), "stale\n"); err != nil {
			t.Fatal(err)
		}
	}

	if err := installCmd(Args{"no-bin": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	for _, destination := range destinations {
		assertExists(t, filepath.Join(destination, "SKILL.md"))
		assertExists(t, filepath.Join(destination, "references", "COMMANDS.md"))
		if _, err := os.Stat(filepath.Join(destination, "assets", "templates", "story.md")); !os.IsNotExist(err) {
			t.Fatalf("stale file still exists after default install refresh at %s: %v", destination, err)
		}
	}
}

func TestInstallCommandStillInstallsExplicitUserSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := installCmd(Args{"codex-user": "true", "claude-user": "true", "no-bin": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(home, ".agents", "skills", "tusker", "SKILL.md"))
	assertExists(t, filepath.Join(home, ".claude", "skills", "tusker", "SKILL.md"))
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "tusker")); !os.IsNotExist(err) {
		t.Fatalf("unselected .codex skill install was created: %v", err)
	}
}

func TestUpdateCommandRefreshesRepoSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	userDestination := filepath.Join(home, ".agents", "skills", "tusker")
	if err := writeText(filepath.Join(userDestination, "assets", "templates", "story.md"), "stale user\n"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	destinations := []string{
		filepath.Join(repo, ".agents", "skills", "tusker"),
		filepath.Join(repo, ".claude", "skills", "tusker"),
	}
	for _, destination := range destinations {
		if err := writeText(filepath.Join(destination, "assets", "templates", "story.md"), "stale\n"); err != nil {
			t.Fatal(err)
		}
	}
	stalePointer := strings.Join([]string{
		tuskerPointerBegin,
		"## Progressive Tusker context",
		"",
		"Start with `tusker list --type epic` to see the short epic roster.",
		"Progressive drill-down: `tusker list --epic <ACR> --type task --open`.",
		tuskerPointerEnd,
	}, "\n")
	if err := writeText(filepath.Join(repo, "AGENTS.md"), "Human repo rule.\n\n"+stalePointer+"\n\nKeep this rule.\n"); err != nil {
		t.Fatal(err)
	}

	if err := updateCmd(Args{"repo": repo, "repo-only": "true", "no-bin": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	for _, destination := range destinations {
		assertExists(t, filepath.Join(destination, "SKILL.md"))
		if _, err := os.Stat(filepath.Join(destination, "assets", "templates", "story.md")); !os.IsNotExist(err) {
			t.Fatalf("stale repo-local file still exists after update at %s: %v", destination, err)
		}
	}
	if _, err := os.Stat(filepath.Join(userDestination, "assets", "templates", "story.md")); err != nil {
		t.Fatalf("repo-only update touched user skill install: %v", err)
	}
	agents, err := readText(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, agents, "Human repo rule.")
	assertContainsIndexTest(t, agents, "Keep this rule.")
	assertContainsIndexTest(t, agents, "Use Tusker for tracked repo work.")
	assertContainsIndexTest(t, agents, "Task mechanics live in the installed `tusker` skill.")
	assertContainsIndexTest(t, agents, "Project knowledge starts at `.tusker/SKILL.md`.")
	assertContainsIndexTest(t, agents, "`tusker next`")
	assertContainsIndexTest(t, agents, "`tusker show <TASK-ID> --capsule`")
	assertContainsIndexTest(t, agents, "Do not read `.tusker/events`, `_generated`, `attempts`, `evidence`, `Attachments`, raw logs, or full task files")
	assertContainsIndexTest(t, agents, "Keep proof compact")
	assertContainsIndexTest(t, agents, "command + PASS/FAIL summaries")
	assertNotContainsIndexTest(t, agents, "Progressive Tusker context")
	assertNotContainsIndexTest(t, agents, "epic roster")
	if count := strings.Count(agents, tuskerPointerBegin); count != 1 {
		t.Fatalf("expected one managed Tusker pointer block, got %d:\n%s", count, agents)
	}
	claude, err := readText(filepath.Join(repo, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, claude, "Use Tusker for tracked repo work.")
}

func TestSyncRepoContractDoesNotUseUnrelatedCwdVaultForPointers(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	repo := t.TempDir()
	other := t.TempDir()
	if err := bootstrap(Args{"vault": filepath.Join(other, "vault"), "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatal(err)
	}

	if err := syncRepoContract(Args{"repo": repo}); err != nil {
		t.Fatal(err)
	}
	agents, err := readText(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(agents, other) {
		t.Fatalf("repo pointer leaked unrelated cwd vault path:\n%s", agents)
	}
	assertContainsIndexTest(t, agents, "`.tusker/SKILL.md`")
}

func TestInstallCommandInstallsRepoSkillsWithoutBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	userDestination := filepath.Join(home, ".agents", "skills", "tusker")
	if err := writeText(filepath.Join(userDestination, "assets", "templates", "story.md"), "stale user\n"); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := installCmd(Args{"repo": repo, "no-bin": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(repo, ".agents", "skills", "tusker", "SKILL.md"))
	assertExists(t, filepath.Join(repo, ".claude", "skills", "tusker", "SKILL.md"))
	assertIsSymlink(t, filepath.Join(repo, ".agents", "skills", "tusker"))
	assertIsSymlink(t, filepath.Join(repo, ".claude", "skills", "tusker"))
	agents, err := readText(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, agents, "Use Tusker for tracked repo work.")
	assertContainsIndexTest(t, agents, "Project knowledge starts at `.tusker/SKILL.md`.")
	assertContainsIndexTest(t, agents, "Keep proof compact")
	assertNotContainsIndexTest(t, agents, "Progressive Tusker context")
	assertExists(t, filepath.Join(repo, "CLAUDE.md"))
	if _, err := os.Stat(filepath.Join(userDestination, "assets", "templates", "story.md")); err != nil {
		t.Fatalf("repo install touched user skill install: %v", err)
	}
}

func TestInstallCommandCopyModeMaterializesRepoSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()

	if err := installCmd(Args{"repo": repo, "no-bin": "true", "skill-mode": "copy", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(repo, ".agents", "skills", "tusker")
	assertExists(t, filepath.Join(destination, "SKILL.md"))
	if info, err := os.Lstat(destination); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copy mode produced a symlink at %s", destination)
	}
}

func TestSkillSyncDefaultsToRepoSymlinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()

	if err := skillSyncCmd(Args{"repo": repo, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	assertIsSymlink(t, filepath.Join(repo, ".agents", "skills", "tusker"))
	assertIsSymlink(t, filepath.Join(repo, ".claude", "skills", "tusker"))
}

func TestSkillSyncSymlinkAcceptsExplicitSourceOutsideCheckout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot, err := findRepoRoot(previousWD)
	if err != nil {
		t.Fatal(err)
	}
	if sourceRoot == "" {
		t.Fatal("expected test to run inside Tusker checkout")
	}
	outside := t.TempDir()
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()

	if err := skillSyncCmd(Args{"repo": repo, "source": sourceRoot, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(repo, ".agents", "skills", "tusker")
	assertIsSymlink(t, destination)
	target, err := os.Readlink(destination)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, filepath.Join(sourceRoot, "skill"), target, "skill symlink target")
}

func TestSkillBundleMaterializesPortableCopies(t *testing.T) {
	repo := t.TempDir()
	out := filepath.Join(repo, "bundle")

	if err := skillBundleCmd(Args{"repo": repo, "out": out, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	for _, destination := range []string{
		filepath.Join(out, ".agents", "skills", "tusker"),
		filepath.Join(out, ".claude", "skills", "tusker"),
	} {
		assertExists(t, filepath.Join(destination, "SKILL.md"))
		if info, err := os.Lstat(destination); err != nil {
			t.Fatal(err)
		} else if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("portable bundle should materialize copy, got symlink at %s", destination)
		}
	}
}

func TestPurgeTuskerStateRemovesOnlyGeneratedTuskerState(t *testing.T) {
	repo := t.TempDir()
	if err := writeText(filepath.Join(repo, ".tusker", "SKILL.md"), "vault\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "apps", "admin-web", ".tusker", "scratch", "x.md"), "scratch\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, ".agents", "skills", "tusker", "SKILL.md"), "skill\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "src", "main.rs"), "fn main() {}\n"); err != nil {
		t.Fatal(err)
	}
	block := strings.Join([]string{
		"Human rule.",
		"",
		tuskerPointerBegin,
		"## Tusker",
		"",
		"Use Tusker for tracked repo work.",
		tuskerPointerEnd,
		"",
		"Keep me.",
	}, "\n")
	if err := writeText(filepath.Join(repo, "AGENTS.md"), block); err != nil {
		t.Fatal(err)
	}

	if err := tuskerPurgeCmd(Args{"repo": repo, "only-tusker-state": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(repo, ".tusker", "SKILL.md"))

	if err := tuskerPurgeCmd(Args{"repo": repo, "only-tusker-state": "true", "yes": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		filepath.Join(repo, ".tusker"),
		filepath.Join(repo, "apps", "admin-web", ".tusker"),
		filepath.Join(repo, ".agents", "skills", "tusker"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("expected Tusker state removed at %s, got %v", path, err)
		}
	}
	assertExists(t, filepath.Join(repo, "src", "main.rs"))
	agents, err := readText(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, agents, "Human rule.")
	assertContainsIndexTest(t, agents, "Keep me.")
	assertNotContainsIndexTest(t, agents, tuskerPointerBegin)
}

func TestInstallHelpExplainsInstallerFlags(t *testing.T) {
	output := captureStdout(t, printInstallHelp)
	for _, expected := range []string{
		"tusker install [--bin-dir <path>]",
		"refreshes already-installed user skills in ~/.agents, ~/.codex, and ~/.claude",
		"--codex-user installs ~/.agents/skills/tusker",
		"--claude-user installs ~/.claude/skills/tusker",
		"repo-local installs default to symlink mode",
		"refreshes the managed AGENTS.md/CLAUDE.md Tusker bootstrap block",
		"compact proof/context guidance",
		"--refresh-existing-user-skills also refreshes existing user skills when --repo is used",
		"--source points symlink mode at a canonical Tusker checkout or skill dir",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("install help missing %q:\n%s", expected, output)
		}
	}
}

func assertIsSymlink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("expected symlink at %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s, got mode %s", path, info.Mode())
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
	if err := ensureDir(filepath.Join(existingVault, "work")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	explicitVault := filepath.Join(root, "explicit", "tracker")
	if err := initCmd(Args{"vault": explicitVault, "yes": "true", "vault-only": "true", "no-mount": "true"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(explicitVault, "SKILL.md"))
	assertExists(t, filepath.Join(explicitVault, "work", "tasks"))
	assertExists(t, filepath.Join(explicitVault, "knowledge", "domains", "project", "INDEX.md"))
	if _, err := os.Stat(filepath.Join(explicitVault, "_config", "docs-map.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected V7 init not to create legacy docs map: %v", err)
	}
}

func TestInitDefaultsToDotTuskerVault(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	repo := t.TempDir()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := initCmd(Args{"yes": "true", "vault-only": "true", "no-mount": "true"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(repo, ".tusker", "SKILL.md"))
	assertExists(t, filepath.Join(repo, ".tusker", "work", "tasks"))
	assertExists(t, filepath.Join(repo, ".tusker", "Dashboard.md"))
	assertExists(t, filepath.Join(repo, ".tusker", "dashboards", "human-actions.md"))
	assertExists(t, filepath.Join(repo, ".tusker", "_generated", "bases", "tasks.base"))
	assertExists(t, filepath.Join(repo, ".tusker", "_generated", "indexes", "summary.json"))
	if _, err := os.Stat(filepath.Join(repo, "tusker")); !os.IsNotExist(err) {
		t.Fatalf("default init created legacy visible tusker directory: %v", err)
	}
	config, err := readText(filepath.Join(repo, "tusker.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, config, "root: .tusker")
	assertContainsIndexTest(t, config, "events_root: .tusker/events")
}

func TestMigrateVaultRootMovesLegacyVaultAndUpdatesPointers(t *testing.T) {
	repo := t.TempDir()
	legacyVault := filepath.Join(repo, "tusker")
	if err := bootstrap(Args{"vault": legacyVault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	if err := migrateVaultRootCmd(Args{"vault": legacyVault, "to": ".tusker"}); err != nil {
		t.Fatal(err)
	}

	assertExists(t, filepath.Join(repo, ".tusker", "SKILL.md"))
	if _, err := os.Stat(legacyVault); !os.IsNotExist(err) {
		t.Fatalf("legacy vault still exists after migration: %v", err)
	}
	config, err := readText(filepath.Join(repo, "tusker.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, config, "root: .tusker")
	assertContainsIndexTest(t, config, "generated_root: .tusker/_generated")
	agents, err := readText(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, agents, "Project knowledge starts at `.tusker/SKILL.md`.")
	assertContainsIndexTest(t, agents, "Do not read `.tusker/events`")
}
