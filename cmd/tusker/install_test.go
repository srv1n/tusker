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
	if strings.Contains(string(referenceContent), "stale commands") || !strings.Contains(string(referenceContent), "# Commands") {
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
	assertContainsIndexTest(t, agents, "Project knowledge starts at `tusker/SKILL.md`.")
	assertContainsIndexTest(t, agents, "`tusker next`")
	assertContainsIndexTest(t, agents, "`tusker show <TASK-ID> --capsule`")
	assertContainsIndexTest(t, agents, "Do not read `tusker/events`, `_generated`, `attempts`, `evidence`, `Attachments`, raw logs, or full task files")
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
	assertContainsIndexTest(t, agents, "`tusker/SKILL.md`")
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
	agents, err := readText(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, agents, "Use Tusker for tracked repo work.")
	assertContainsIndexTest(t, agents, "Project knowledge starts at `tusker/SKILL.md`.")
	assertContainsIndexTest(t, agents, "Keep proof compact")
	assertNotContainsIndexTest(t, agents, "Progressive Tusker context")
	assertExists(t, filepath.Join(repo, "CLAUDE.md"))
	if _, err := os.Stat(filepath.Join(userDestination, "assets", "templates", "story.md")); err != nil {
		t.Fatalf("repo install touched user skill install: %v", err)
	}
}

func TestInstallHelpExplainsInstallerFlags(t *testing.T) {
	output := captureStdout(t, printInstallHelp)
	for _, expected := range []string{
		"tusker install [--bin-dir <path>]",
		"refreshes already-installed user skills in ~/.agents, ~/.codex, and ~/.claude",
		"--codex-user installs ~/.agents/skills/tusker",
		"--claude-user installs ~/.claude/skills/tusker",
		"refreshes the managed AGENTS.md/CLAUDE.md Tusker bootstrap block",
		"compact proof/context guidance",
		"--refresh-existing-user-skills also refreshes existing user skills when --repo is used",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("install help missing %q:\n%s", expected, output)
		}
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
	if err := bootstrapLegacy(Args{"vault": existingVault, "quiet": "true"}); err != nil {
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
