package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateRefusesSelfReferentialBinaryWithoutMutation(t *testing.T) {
	binDir := t.TempDir()
	target := filepath.Join(binDir, "tusker")
	original := []byte{0x7f, 'T', 'U', 'S', 'K', 'E', 'R', 0x00, 0xff}
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}

	err = installBinarySymlinkFrom(Args{"bin-dir": binDir}, target)
	if err == nil {
		t.Fatal("expected self-referential binary update to be refused")
	}
	issue := errorToIssue(err)
	if !strings.Contains(issue.Message, "release-installed Tusker binaries") {
		t.Fatalf("expected release-install explanation, got %q", issue.Message)
	}
	if !strings.Contains(issue.Hint, "scripts/install.sh") || !strings.Contains(issue.Hint, "tusker update --no-bin") {
		t.Fatalf("expected actionable release-install guidance, got %q", issue.Hint)
	}

	after, err := os.Lstat(target)
	if err != nil {
		t.Fatalf("installed binary was removed: %v", err)
	}
	if after.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("installed binary was replaced by a symlink: %s", after.Mode())
	}
	if before.Mode() != after.Mode() {
		t.Fatalf("installed binary mode changed: before=%s after=%s", before.Mode(), after.Mode())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("installed binary content changed: got %v want %v", got, original)
	}
}

func TestBinaryInstallPlanRefusesHardLinkedSourceAndTarget(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("release binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "tusker")
	if err := os.Link(source, target); err != nil {
		t.Fatal(err)
	}

	_, err := binaryInstallPlanForSource(Args{"bin-dir": binDir}, source)
	if err == nil {
		t.Fatal("expected hard-linked binary source and target to be refused")
	}
	if !strings.Contains(errorToIssue(err).Message, "same file") {
		t.Fatalf("expected same-file error, got %v", err)
	}
}

func TestUpdatePreflightsBinaryBeforeRefreshingSkillsOrRepoPointers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := t.TempDir()
	skillDestination := filepath.Join(repo, ".agents", "skills", "tusker")
	staleSkill := filepath.Join(skillDestination, "stale.txt")
	if err := writeText(staleSkill, "must survive refusal\n"); err != nil {
		t.Fatal(err)
	}
	pointerPath := filepath.Join(repo, "AGENTS.md")
	if err := writeText(pointerPath, "existing operator instructions\n"); err != nil {
		t.Fatal(err)
	}

	preflightErr := tuskerError(errorInvalidArg, "refusing to update: source and target are the same file")
	err := updateCmdWithBinaryPreflight(Args{"repo": repo, "repo-only": "true", "quiet": "true"}, func(Args) error {
		return preflightErr
	})
	if !errors.Is(err, preflightErr) {
		t.Fatalf("expected preflight error, got %v", err)
	}
	if got, err := readText(staleSkill); err != nil || got != "must survive refusal\n" {
		t.Fatalf("skill destination changed before refusal: text=%q err=%v", got, err)
	}
	if got, err := readText(pointerPath); err != nil || got != "existing operator instructions\n" {
		t.Fatalf("repo pointer changed before refusal: text=%q err=%v", got, err)
	}
}

func TestInstallBinaryReplacementIsAtomic(t *testing.T) {
	root := t.TempDir()
	binarySource := filepath.Join(root, "checkout", "dist", "tusker")
	if err := os.MkdirAll(filepath.Dir(binarySource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binarySource, []byte("new checkout binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedSource, err := filepath.EvalSymlinks(binarySource)
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "tusker")
	oldBinary := []byte("old installed binary")
	if err := os.WriteFile(target, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}

	commitObserved := false
	err = installBinarySymlinkFromWithRename(Args{"bin-dir": binDir}, binarySource, func(staged, destination string) error {
		commitObserved = true
		if destination != target {
			t.Fatalf("rename destination = %s, want %s", destination, target)
		}
		current, err := os.ReadFile(destination)
		if err != nil {
			t.Fatalf("destination disappeared before atomic commit: %v", err)
		}
		if !bytes.Equal(current, oldBinary) {
			t.Fatalf("destination changed before atomic commit: got %q want %q", current, oldBinary)
		}
		stagedTarget, err := os.Readlink(staged)
		if err != nil {
			t.Fatalf("staged replacement is not a symlink: %v", err)
		}
		if stagedTarget != resolvedSource {
			t.Fatalf("staged symlink target = %s, want %s", stagedTarget, resolvedSource)
		}
		return os.Rename(staged, destination)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !commitObserved {
		t.Fatal("atomic rename was not attempted")
	}
	installedSource, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("installed destination is not a symlink: %v", err)
	}
	if installedSource != resolvedSource {
		t.Fatalf("installed symlink target = %s, want %s", installedSource, resolvedSource)
	}
	stages, err := filepath.Glob(filepath.Join(binDir, ".tusker-install-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 0 {
		t.Fatalf("staging directories were not cleaned up: %v", stages)
	}
}

func TestInstallBinaryAllowsExistingSymlinkToSource(t *testing.T) {
	root := t.TempDir()
	binarySource := filepath.Join(root, "checkout", "tusker")
	if err := os.MkdirAll(filepath.Dir(binarySource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binarySource, []byte("checkout binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "tusker")
	if err := os.Symlink(binarySource, target); err != nil {
		t.Fatal(err)
	}

	if err := installBinarySymlinkFrom(Args{"bin-dir": binDir}, target); err != nil {
		t.Fatalf("existing checkout symlink was treated as self-source: %v", err)
	}
	installedSource, err := os.Readlink(target)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSource, err := filepath.EvalSymlinks(binarySource)
	if err != nil {
		t.Fatal(err)
	}
	if installedSource != resolvedSource {
		t.Fatalf("installed symlink target = %s, want %s", installedSource, resolvedSource)
	}
}

func TestInstallBinaryAtomicRenameFailurePreservesDestination(t *testing.T) {
	root := t.TempDir()
	binarySource := filepath.Join(root, "checkout", "tusker")
	if err := os.MkdirAll(filepath.Dir(binarySource), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binarySource, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(binDir, "tusker")
	original := []byte("existing binary")
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatal(err)
	}

	commitErr := errors.New("rename refused")
	err := installBinarySymlinkFromWithRename(Args{"bin-dir": binDir}, binarySource, func(_, _ string) error {
		return commitErr
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected rename error, got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("destination disappeared after rename failure: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("destination changed after rename failure: got %q want %q", got, original)
	}
	if info, err := os.Lstat(target); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("destination became a symlink after rename failure: %s", info.Mode())
	}
	stages, err := filepath.Glob(filepath.Join(binDir, ".tusker-install-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 0 {
		t.Fatalf("staging directories were not cleaned up after rename failure: %v", stages)
	}
}

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
	if strings.Contains(string(referenceContent), "stale commands") || !strings.Contains(string(referenceContent), "# Compatibility redirect") {
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
	sourceRoot := filepath.Join(t.TempDir(), "canonical-skill")
	if err := installSkillPayloadCopy(sourceRoot); err != nil {
		t.Fatal(err)
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
	resolvedSource, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, resolvedSource, target, "skill symlink target")
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
