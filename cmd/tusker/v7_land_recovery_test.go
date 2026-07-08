package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A1: a task with no wave membership gets an actionable refusal that names the
// exact wave command to run.
func TestLandNoWaveRefusal(t *testing.T) {
	_, vault := newLandTestRepo(t, 1, "true")
	mustWave(t, Args{
		"vault": vault, "quiet": "true", "epic": "APP",
		"title": "Wave-less task", "risk": "low", "priority": "p2", "v7": "true",
	}, newV7Task)

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0002"})
	if err == nil {
		t.Fatal("expected a refusal when landing a task with no wave")
	}
	msg := err.Error()
	for _, want := range []string{"not in a wave", "tusker wave create", "APP-T-0002"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("no-wave refusal must be actionable; missing %q in %q", want, msg)
		}
	}
}

// A2: a detached completed worktree with no task/<ID> branch and nothing to
// branch from gets a refusal that prints the exact branch command before retry.
func TestLandDetachedBranchRefusal(t *testing.T) {
	_, vault := newLandTestRepo(t, 1, "true")

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected a refusal when the task branch is missing and no worktree can be found")
	}
	msg := err.Error()
	for _, want := range []string{"no task/APP-T-0001 branch", "git branch task/APP-T-0001", "--from"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("branchification refusal must name the exact branch command; missing %q in %q", want, msg)
		}
	}
}

// A3: the live-style recovery path. Start from a review/proof-satisfied task and
// a detached completed worktree with no task/<ID> branch; tusker land must
// branchify, merge the work into integration for real, and (once done) move main.
func TestLandDetachedRecovery(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	// Live shakedown state: task sitting in review with proof satisfied.
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")

	// A detached worktree carrying the runner's real completed work; note the
	// worktree path carries the record id like real runner workspaces do.
	worktree := filepath.Join(t.TempDir(), "APP-T-0001__task")
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, "integration/W-0001")
	if err := writeText(filepath.Join(worktree, "recovered.txt"), "recovered\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", ".")
	runGitDir(t, worktree, "commit", "-m", "runner completed work")
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("precondition: task/APP-T-0001 must not exist yet")
	}

	// Land: discover the detached worktree, branchify, and merge for real.
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatalf("recovery land failed: %v", err)
	}
	if !gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("land should have created task/APP-T-0001 from the detached worktree")
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has the recovered work")

	// Complete the task and land the wave; main must move with the recovered work.
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-08T02:00:00Z")
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatalf("wave land to main failed: %v", err)
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "main", "recovered.txt"), "main moved with the recovered work")
}

// A3 (flag path): the same recovery works when the operator points land at the
// detached worktree explicitly with --from.
func TestLandDetachedRecoveryFromFlag(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")

	worktree := filepath.Join(t.TempDir(), "unrelated-name")
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, "integration/W-0001")
	if err := writeText(filepath.Join(worktree, "recovered.txt"), "recovered\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", ".")
	runGitDir(t, worktree, "commit", "-m", "runner completed work")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": worktree}); err != nil {
		t.Fatalf("recovery land with --from failed: %v", err)
	}
	if !gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("--from should have created task/APP-T-0001")
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has the recovered work")
}

// A4: a successful land prints a concise summary naming task, source branch,
// target, gate result, commit, and whether main moved or is waiting.
func TestLandSummary(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"s.txt": "s\n"})

	var landErr error
	output := captureStdout(t, func() {
		landErr = landV7Cmd(Args{"vault": vault, "_pos0": "APP-T-0001"})
	})
	if landErr != nil {
		t.Fatal(landErr)
	}
	for _, want := range []string{"Landed 1 task", "APP-T-0001", "task/APP-T-0001", "integration/W-0001", "gate:pass", "main: waiting"} {
		if !strings.Contains(output, want) {
			t.Fatalf("landing summary missing %q in:\n%s", want, output)
		}
	}
}

// A5: a batch where nothing lands exits non-zero with an unmistakable failed
// batch summary and does not apply any rework transitions.
func TestLandFailedBatch(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "exit 1")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"a.txt": "a\n"})
	commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{"b.txt": "b\n"})
	mainRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "_pos1": "APP-T-0002"})
	if err == nil {
		t.Fatal("expected a non-zero exit when no requested task lands")
	}
	if !strings.Contains(err.Error(), "0 of 2 task") {
		t.Fatalf("expected an unmistakable failed-batch summary, got %v", err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		data, _, e := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", id+".md"))
		if e != nil {
			t.Fatal(e)
		}
		if got := stringField(data, "status"); got == "rework" {
			t.Fatalf("%s must not be moved to rework on total failure, got %q", id, got)
		}
	}
	assertEqual(t, mainRev, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "integration/W-0001")), "integration must stay at main on total failure")
}

// A6: a failed merge summary skips Auto-merging progress chatter and preserves
// the first actionable conflict line and path in the audit and task next_action.
func TestLandConflictSummary(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "true")

	// Seed a shared file into the integration base so two task branches can
	// conflict on it.
	seed := filepath.Join(t.TempDir(), "seed")
	runGitDir(t, repo, "worktree", "add", "--detach", seed, "integration/W-0001")
	if err := writeText(filepath.Join(seed, "conflict.txt"), "base\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, seed, "add", ".")
	runGitDir(t, seed, "commit", "-m", "seed conflict base")
	seedRev := strings.TrimSpace(gitDirOutput(t, seed, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", seed)
	runGitDir(t, repo, "branch", "-f", "integration/W-0001", seedRev)

	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"conflict.txt": "alpha\n"})
	commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{"conflict.txt": "beta\n"})

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "_pos1": "APP-T-0002"}); err != nil {
		t.Fatalf("partial batch should land the clean task and rework the conflicted one: %v", err)
	}

	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "rework", stringField(data, "status"), "conflicted task returned to rework")
	nextAction := stringField(data, "next_action")
	if !strings.Contains(nextAction, "CONFLICT") || !strings.Contains(nextAction, "conflict.txt") {
		t.Fatalf("next_action must preserve the actionable conflict line and path, got %q", nextAction)
	}
	if strings.Contains(nextAction, "Auto-merging") {
		t.Fatalf("next_action must not carry Auto-merging chatter, got %q", nextAction)
	}

	waveData, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	failSummary := ""
	for _, row := range normalizeLandingAudit(waveData["landings"]) {
		if stringField(row, "gate_result") == "fail" {
			failSummary = stringField(row, "gate_summary")
		}
	}
	if !strings.Contains(failSummary, "CONFLICT") || !strings.Contains(failSummary, "conflict.txt") {
		t.Fatalf("audit gate_summary must carry the actionable conflict line and path, got %q", failSummary)
	}
	if strings.Contains(failSummary, "Auto-merging") {
		t.Fatalf("audit gate_summary must not carry Auto-merging chatter, got %q", failSummary)
	}
}
