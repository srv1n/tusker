package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationBranchCut(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "true")

	mainRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	integrationRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "integration/W-0001"))
	assertEqual(t, mainRev, integrationRev, "integration branch cut from main")

	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "integration/W-0001", stringField(waveData, "integration_branch"), "wave integration branch")
}

func TestWorktreeFromIntegration(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "test -f sibling.txt")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"sibling.txt": "landed sibling\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}

	manager := NewWorkspaceManager()
	workspace, err := manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "app", ProjectKey: "app", RecordID: "APP-T-0002", ItemID: "APP-T-0002",
		BranchName: "task/APP-T-0002", BranchBase: "integration/W-0001",
		RepoRoot: repo, StateRoot: t.TempDir(), Strategy: WorkspaceStrategyWorktree,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Cleanup(workspace.Path) }()
	assertEqual(t, "landed sibling\n", mustReadIndexTest(t, filepath.Join(workspace.Path, "sibling.txt")), "worktree sees landed sibling content")
	assertEqual(t, "task/APP-T-0002", strings.TrimSpace(gitDirOutput(t, workspace.Path, "rev-parse", "--abbrev-ref", "HEAD")), "worktree task branch")
}

func TestArmedWaveLanding(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "test -f a.txt && test -f b.txt")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"a.txt": "a\n"})
	commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{"b.txt": "b\n"})

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "_pos1": "APP-T-0002"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "a\n", gitShowFile(t, repo, "integration/W-0001", "a.txt"), "integration branch has first batch file")
	assertEqual(t, "b\n", gitShowFile(t, repo, "integration/W-0001", "b.txt"), "integration branch has second batch file")
	if gitShowFileOK(repo, "main", "a.txt") {
		t.Fatal("green task batch should land to integration, not main")
	}

	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	landings := normalizeLandingAudit(waveData["landings"])
	if len(landings) != 2 {
		t.Fatalf("expected two landing audit rows, got %#v", landings)
	}
	for _, row := range landings {
		assertEqual(t, "pass", stringField(row, "gate_result"), "green batch audit result")
		if stringField(row, "branch") == "" || stringField(row, "timestamp") == "" {
			t.Fatalf("landing audit missing branch/timestamp: %#v", row)
		}
	}
}

func TestLandTerminalStateMonotonic(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	taskRel := ".tusker/work/tasks/APP-T-0001.md"
	reviewContent, err := readText(filepath.Join(repo, taskRel))
	if err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-08T02:30:00Z")
	runGitDir(t, repo, "add", taskRel)
	runGitDir(t, repo, "commit", "-m", "close task")
	runGitDir(t, repo, "branch", "-f", "integration/W-0001", "main")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{
		taskRel:     reviewContent,
		"stale.txt": "stale branch content\n",
	})

	err = landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected terminal task rewind to fail landing")
	}
	if !strings.Contains(err.Error(), "terminal task state rewind refused") {
		t.Fatalf("expected terminal rewind conflict, got %v", err)
	}
	integrationData, _, err := parseFrontmatter(gitShowFile(t, repo, "integration/W-0001", taskRel))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(integrationData, "status"), "integration task status")
	canonicalData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(canonicalData, "status"), "canonical task status")
	assertEqual(t, "2026-07-08T02:30:00Z", stringField(canonicalData, "closed_at"), "canonical closed_at")
}

func TestArmedWaveBisect(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "if [ -f good.txt ] && [ -f bad.txt ]; then echo semantic conflict; exit 1; fi")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"good.txt": "good\n"})
	commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{"bad.txt": "bad\n"})

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "_pos1": "APP-T-0002"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "good\n", gitShowFile(t, repo, "integration/W-0001", "good.txt"), "good branch landed")
	if gitShowFileOK(repo, "integration/W-0001", "bad.txt") {
		t.Fatal("bad branch should not land into integration")
	}

	taskData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "rework", stringField(taskData, "status"), "bad task status")
	if !strings.Contains(stringField(taskData, "next_action"), "semantic conflict") {
		t.Fatalf("expected failing gate output attached to next_action, got %q", stringField(taskData, "next_action"))
	}
	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	landings := normalizeLandingAudit(waveData["landings"])
	if len(landings) != 2 || stringField(landings[1], "gate_result") != "fail" {
		t.Fatalf("expected pass+fail audit rows, got %#v", landings)
	}
}

func TestWaveLandToMain(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f shipped.txt")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"shipped.txt": "shipped\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T02:00:00Z")
	commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatal(err)
	}
	parents := strings.Fields(gitDirOutput(t, repo, "rev-list", "--parents", "-n", "1", "main"))
	if len(parents) != 3 {
		t.Fatalf("expected one two-parent wave merge commit on main, got %v", parents)
	}
	assertEqual(t, "Wave W-0001: Landing batch", strings.TrimSpace(gitDirOutput(t, repo, "log", "-1", "--format=%s", "main")), "wave merge title")
	if gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("integration branch should be deleted after wave landing")
	}
	assertEqual(t, "shipped\n", gitShowFile(t, repo, "main", "shipped.txt"), "main has wave content")
}

func TestWaveLandCheckedOutCleanMainSyncsWorktree(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "checked-out.txt", "checked out\n")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatalf("wave land with checked-out clean main failed: %v", err)
	}
	mainRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	headRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD"))
	assertEqual(t, mainRev, headRev, "checked-out main HEAD must advance with branch ref")
	assertEqual(t, "checked out\n", mustReadIndexTest(t, filepath.Join(repo, "checked-out.txt")), "checked-out main worktree has landed file")
	dirty, err := inPlaceDirtyPaths(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirty) > 0 {
		t.Fatalf("checked-out main should not be stale or dirty outside .tusker after land: %v", dirty)
	}
	if gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("integration branch should be deleted after checked-out main land")
	}
}

func TestWaveLandCheckedOutDirtyMainRefusesBeforeMove(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "dirty-main.txt", "dirty main land\n")
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	if err := writeText(filepath.Join(repo, "operator.txt"), "operator local change\n"); err != nil {
		t.Fatal(err)
	}

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"})
	if err == nil {
		t.Fatal("expected dirty checked-out main to refuse wave land")
	}
	msg := err.Error()
	for _, want := range []string{"main is checked out", "dirty paths", "operator.txt"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("dirty main refusal missing %q in %v", want, err)
		}
	}
	assertEqual(t, mainBefore, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")), "dirty refusal must not move main ref")
	if !gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("dirty refusal must leave integration branch intact")
	}
}

func TestWaveLandDefaultBranchNotCheckedOutUsesUpdateRefFastPath(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "fast-path.txt", "fast path\n")
	runGitDir(t, repo, "checkout", "-b", "operator-work", "main")
	operatorHead := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD"))

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatalf("wave land with main not checked out failed: %v", err)
	}
	assertEqual(t, operatorHead, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD")), "non-main checkout must not be reset by fast path")
	assertEqual(t, "operator-work", strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "--abbrev-ref", "HEAD")), "operator branch remains checked out")
	assertEqual(t, "fast path\n", gitShowFile(t, repo, "main", "fast-path.txt"), "main advanced through update-ref fast path")
	if gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("integration branch should be deleted after fast-path land")
	}
}

func TestWaveLandSummaryStillReportsMainMove(t *testing.T) {
	repo, vault := newLandReadyForMainAdvanceTest(t, "summary-main.txt", "summary\n")

	var landErr error
	output := captureStdout(t, func() {
		landErr = landV7Cmd(Args{"vault": vault, "_pos0": "W-0001"})
	})
	if landErr != nil {
		t.Fatal(landErr)
	}
	for _, want := range []string{"main: moved to", "Wave W-0001: Landing batch"} {
		if !strings.Contains(output, want) {
			t.Fatalf("wave landing summary missing %q in:\n%s", want, output)
		}
	}
	assertEqual(t, "summary\n", gitShowFile(t, repo, "main", "summary-main.txt"), "summary path still lands content")
}

func newLandReadyForMainAdvanceTest(t *testing.T, fileName, content string) (string, string) {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, "test -f "+yamlQuoteForShellTest(fileName))
	sourceSHA := commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{fileName: content})
	if err := landV7CmdWithFrozenSources(
		Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"},
		map[string]string{"APP-T-0001": sourceSHA},
	); err != nil {
		t.Fatalf("task land setup failed: %v", err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T02:00:00Z")
	commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")
	return repo, vault
}

func yamlQuoteForShellTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestLandIdempotent(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f shipped.txt")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"shipped.txt": "shipped\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T02:00:00Z")
	commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")

	mainRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	integrationRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "integration/W-0001"))
	mergeCommit := strings.TrimSpace(gitDirOutput(t, repo, "commit-tree", "integration/W-0001^{tree}", "-p", mainRev, "-p", integrationRev, "-m", "Wave W-0001: Landing batch"))
	runGitDir(t, repo, "update-ref", "refs/heads/main", mergeCommit, mainRev)

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, mergeCommit, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")), "idempotent rerun keeps existing merge commit")
	if gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("idempotent rerun should clean up integration branch")
	}
}

func TestMainPushGuard(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	if err := hookInstallCmd(Args{"vault": vault, "quiet": "true", "_pos0": "pre-push"}); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(repo, ".git", "hooks", "pre-push")
	hook := mustReadIndexTest(t, hookPath)
	for _, want := range []string{"tusker:pre-push-main-guard", "TUSKER_LAND_MAIN_OK", "use tusker land"} {
		if !strings.Contains(hook, want) {
			t.Fatalf("pre-push hook missing %q:\n%s", want, hook)
		}
	}
	direct := exec.Command(hookPath, "origin", "git@example.invalid:app.git")
	direct.Stdin = strings.NewReader("refs/heads/main 1111111111111111111111111111111111111111 refs/heads/main 0000000000000000000000000000000000000000\n")
	if err := direct.Run(); err == nil {
		t.Fatal("expected direct main push to be blocked")
	}
	guarded := exec.Command(hookPath, "origin", "git@example.invalid:app.git")
	guarded.Env = append(os.Environ(), tuskerLandMainGuardEnv+"=1")
	guarded.Stdin = strings.NewReader("refs/heads/main 1111111111111111111111111111111111111111 refs/heads/main 0000000000000000000000000000000000000000\n")
	if output, err := guarded.CombinedOutput(); err != nil {
		t.Fatalf("expected land env guard to pass, got %v\n%s", err, string(output))
	}
	if !strings.Contains(defaultWorkflowMarkdown(), "Do not push or merge directly to the default branch/main") ||
		!strings.Contains(defaultWorkflowMarkdown(), "tusker land {{ note.id }}") {
		t.Fatal("default runner prompt is missing merge-lane guard text")
	}
}

func newLandTestRepo(t *testing.T, tasks int, gate string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nbranches:\n  default_branch: main\n  control:\n    - main\nruntime:\n  mutation_mode: single_user_local\nautomation:\n  validation:\n    commands:\n      - "+yamlQuoteForTest(gate)+"\n"); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(repo, ".tusker")
	mustWave(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustWave(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Landing tests.", "v7": "true"}, newV7Epic)
	for i := 1; i <= tasks; i++ {
		mustWave(t, Args{
			"vault": vault, "quiet": "true", "epic": "APP",
			"title": "Task " + padNumber(i), "risk": "low", "priority": "p2", "v7": "true",
		}, newV7Task)
	}
	if err := writeText(filepath.Join(repo, "README.md"), "seed\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "seed")
	waveArgs := Args{"vault": vault, "quiet": "true", "_pos0": "Landing batch"}
	for i := 1; i <= tasks; i++ {
		waveArgs[fmt.Sprintf("_pos%d", i)] = "APP-T-" + padNumber(i)
	}
	mustWave(t, waveArgs, waveV7CreateCmd)
	runGitDir(t, repo, "add", "-A")
	runGitDir(t, repo, "commit", "-m", "record landing wave")
	runGitDir(t, repo, "branch", "-f", "integration/W-0001", "main")
	return repo, vault
}

func yamlQuoteForTest(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func commitLandBranch(t *testing.T, repo, branch, base string, files map[string]string) string {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), sanitizeWorkspaceKey(branch))
	runGitDir(t, repo, "worktree", "add", "-b", branch, worktree, base)
	for path, content := range files {
		full := filepath.Join(worktree, path)
		if err := ensureDir(filepath.Dir(full)); err != nil {
			t.Fatal(err)
		}
		if err := writeText(full, content); err != nil {
			t.Fatal(err)
		}
	}
	runGitDir(t, worktree, "add", ".")
	runGitDir(t, worktree, "commit", "-m", branch)
	rev := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	return rev
}

func commitCanonicalTaskStateToIntegration(t *testing.T, repo, vault, taskID string) {
	t.Helper()
	branch := "integration/W-0001"
	old := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", branch))
	worktree := filepath.Join(t.TempDir(), "integration-state")
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, branch)
	rel := filepath.ToSlash(filepath.Join(".tusker", "work", "tasks", taskID+".md"))
	content := mustReadIndexTest(t, filepath.Join(vault, "work", "tasks", taskID+".md"))
	if err := writeText(filepath.Join(worktree, rel), content); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", "--", rel)
	runGitDir(t, worktree, "commit", "-m", "integrate task state "+taskID)
	next := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	runGitDir(t, repo, "update-ref", "refs/heads/"+branch, next, old)
}

func gitShowFile(t *testing.T, repo, ref, path string) string {
	t.Helper()
	return gitDirOutput(t, repo, "show", ref+":"+path)
}

func gitShowFileOK(repo, ref, path string) bool {
	return exec.Command("git", "-C", repo, "show", ref+":"+path).Run() == nil
}
