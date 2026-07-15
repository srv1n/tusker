package main

import (
	"encoding/json"
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
	writeWorkspaceRecordForLandTest(t, worktree, "APP-T-0001")
	if err := writeText(filepath.Join(worktree, "recovered.txt"), "recovered\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", "recovered.txt")
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
	commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")
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
	writeWorkspaceRecordForLandTest(t, worktree, "APP-T-0001")
	if err := writeText(filepath.Join(worktree, "recovered.txt"), "recovered\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", "recovered.txt")
	runGitDir(t, worktree, "commit", "-m", "runner completed work")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": worktree}); err != nil {
		t.Fatalf("recovery land with --from failed: %v", err)
	}
	if !gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("--from should have created task/APP-T-0001")
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has the recovered work")
}

func TestLandFromRefRequiresExactWorkspaceRecordID(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	wrong := filepath.Join(t.TempDir(), "APP-T-0001-wrong-record")
	runGitDir(t, repo, "worktree", "add", "--detach", wrong, "integration/W-0001")
	writeWorkspaceRecordForLandTest(t, wrong, "APP-T-0002")
	if err := writeText(filepath.Join(wrong, "recovered.txt"), "wrong\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, wrong, "add", "recovered.txt")
	runGitDir(t, wrong, "commit", "-m", "wrong task work")

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": wrong})
	if err == nil {
		t.Fatal("expected --from worktree with mismatched record_id to be refused")
	}
	msg := err.Error()
	for _, want := range []string{"refused --from", "record_id", "APP-T-0001"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected actionable provenance refusal containing %q, got %v", want, err)
		}
	}
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("mismatched --from source must not create task branch")
	}

	matching := filepath.Join(t.TempDir(), "APP-T-0001-matching-record")
	runGitDir(t, repo, "worktree", "add", "--detach", matching, "integration/W-0001")
	writeWorkspaceRecordForLandTest(t, matching, "APP-T-0001")
	if err := writeText(filepath.Join(matching, "recovered.txt"), "matching\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, matching, "add", "recovered.txt")
	runGitDir(t, matching, "commit", "-m", "matching task work")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": matching}); err != nil {
		t.Fatalf("matching --from source should land: %v", err)
	}
	assertEqual(t, "matching\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "matching worktree source landed")
}

func TestLandFromRawCommitRequiresTaskTrackerProvenance(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f raw.txt")
	unowned := filepath.Join(t.TempDir(), "raw-unowned")
	runGitDir(t, repo, "worktree", "add", "--detach", unowned, "integration/W-0001")
	if err := writeText(filepath.Join(unowned, "raw.txt"), "unowned\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, unowned, "add", "raw.txt")
	runGitDir(t, unowned, "commit", "-m", "unowned raw commit")
	unownedRev := strings.TrimSpace(gitDirOutput(t, unowned, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", unowned)

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": unownedRev})
	if err == nil {
		t.Fatal("expected raw commit with no task provenance to be refused")
	}
	msg := err.Error()
	for _, want := range []string{"source commit lacks task-owned provenance", "--trust-from", "APP-T-0001.md"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("raw commit refusal missing %q in %v", want, err)
		}
	}
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("unowned raw commit must not create task branch")
	}

	owned := filepath.Join(t.TempDir(), "raw-owned")
	runGitDir(t, repo, "worktree", "add", "--detach", owned, "integration/W-0001")
	if err := writeText(filepath.Join(owned, "raw.txt"), "owned\n"); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(owned, ".tusker", "work", "tasks", "APP-T-0001.md")
	taskContent, err := readText(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, taskContent+"\nProvenance touch.\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, owned, "add", "raw.txt", ".tusker/work/tasks/APP-T-0001.md")
	runGitDir(t, owned, "commit", "-m", "owned raw commit")
	ownedRev := strings.TrimSpace(gitDirOutput(t, owned, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", owned)

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": ownedRev}); err != nil {
		t.Fatalf("raw commit touching the task tracker should land: %v", err)
	}
	assertEqual(t, "owned\n", gitShowFile(t, repo, "integration/W-0001", "raw.txt"), "task-local raw commit landed")
}

func TestLandAutoDiscoveryRequiresExactRecordIDNotSubstringPath(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	substring := filepath.Join(t.TempDir(), "APP-T-0001x-scratch")
	runGitDir(t, repo, "worktree", "add", "--detach", substring, "integration/W-0001")
	writeWorkspaceRecordForLandTest(t, substring, "APP-T-0001X")
	if err := writeText(filepath.Join(substring, "recovered.txt"), "substring\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, substring, "add", "recovered.txt")
	runGitDir(t, substring, "commit", "-m", "substring path work")

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected substring path worktree to be ignored")
	}
	msg := err.Error()
	for _, want := range []string{"no task/APP-T-0001 branch", "no detached completed worktree"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected auto-discovery refusal containing %q, got %v", want, err)
		}
	}
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("substring path must not create task branch")
	}

	exact := filepath.Join(t.TempDir(), "unrelated-path")
	runGitDir(t, repo, "worktree", "add", "--detach", exact, "integration/W-0001")
	writeWorkspaceRecordForLandTest(t, exact, "APP-T-0001")
	if err := writeText(filepath.Join(exact, "recovered.txt"), "exact\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, exact, "add", "recovered.txt")
	runGitDir(t, exact, "commit", "-m", "exact record work")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatalf("exact record_id auto-discovery should land: %v", err)
	}
	assertEqual(t, "exact\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "exact record_id source landed")
}

// F1: advancing the default branch in a checked-out worktree must NOT discard
// local work under .tusker/*. Those paths pass the dirty guard (they are treated
// as tusker bookkeeping), so a staged conflicting change to a tracked .tusker/*
// file reaches the branch-advance step. `git reset --merge` silently overwrote
// such staged work; `git merge --ff-only` must refuse (safe) and leave it intact.
func TestAdvanceDefaultBranchFfOnlyPreservesStagedTuskerWork(t *testing.T) {
	repo, _ := newLandTestRepo(t, 1, "true")

	// Seed a tracked file under .tusker/ on main that newRev will also modify.
	probeRel := ".tusker/land-ff-probe.txt"
	if err := writeText(filepath.Join(repo, probeRel), "base\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", probeRel)
	runGitDir(t, repo, "commit", "-m", "seed ff probe")
	oldRev := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	// newRev: a descendant of main (valid fast-forward) that modifies the same
	// tracked .tusker/* file, anchored by branch probe/ff-new so it stays live.
	newRev := commitLandBranch(t, repo, "probe/ff-new", "main", map[string]string{probeRel: "newrev-content\n"})

	// Free main from the coordination root so it can be checked out in a worktree.
	runGitDir(t, repo, "checkout", "-b", "coord")
	wt := filepath.Join(t.TempDir(), "main-wt")
	runGitDir(t, repo, "worktree", "add", wt, "main")

	// Stage a CONFLICTING modification to the tracked .tusker/* file. It differs
	// from HEAD and from newRev, so the advance is a real conflict; it passes the
	// dirty guard because it lives under .tusker/.
	probeInWt := filepath.Join(wt, probeRel)
	if err := writeText(probeInWt, "local-work\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, wt, "add", probeRel)

	err := advanceV7DefaultBranch(repo, "main", newRev, oldRev)
	if err == nil {
		t.Fatal("advanceV7DefaultBranch must refuse a conflicting fast-forward instead of discarding staged .tusker work")
	}
	if !strings.Contains(err.Error(), "failed to advance checked-out main worktree") {
		t.Fatalf("expected a checked-out advance-failure refusal, got %v", err)
	}
	got, readErr := readText(probeInWt)
	if readErr != nil {
		t.Fatal(readErr)
	}
	assertEqual(t, "local-work\n", got, "staged .tusker work must survive a refused default-branch advance")
	// The worktree branch must not have moved onto newRev.
	if head := strings.TrimSpace(gitDirOutput(t, wt, "rev-parse", "HEAD")); head == strings.TrimSpace(newRev) {
		t.Fatalf("refused advance must leave the worktree at %s, not newRev %s", oldRev, newRev)
	}
}

func writeWorkspaceRecordForLandTest(t *testing.T, worktree, recordID string) {
	t.Helper()
	metaDir := filepath.Join(worktree, ".tusker")
	if err := ensureDir(metaDir); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"record_id": recordID})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(metaDir, "workspace.json"), string(raw)+"\n"); err != nil {
		t.Fatal(err)
	}
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

func TestLandRegeneratesParallelDashboardConflicts(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "true")
	projection := ".tusker/dashboards/review-queue.md"
	epicPath := ".tusker/work/epics/APP.md"
	epic := gitShowFile(t, repo, "integration/W-0001", epicPath)
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{
		"one.txt":                "one\n",
		projection:               "branch one derived projection\n",
		epicPath:                 replaceSection(epic, "## Active work", "branch one managed projection"),
		".tusker/workspace.json": "{\"record_id\":\"APP-T-0001\"}\n",
	})
	commitLandBranch(t, repo, "task/APP-T-0002", "integration/W-0001", map[string]string{
		"two.txt":                "two\n",
		projection:               "branch two derived projection\n",
		epicPath:                 replaceSection(epic, "## Active work", "branch two managed projection"),
		".tusker/workspace.json": "{\"record_id\":\"APP-T-0002\"}\n",
	})

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "_pos1": "APP-T-0002"}); err != nil {
		t.Fatalf("generated dashboard conflicts must not block independent task work: %v", err)
	}
	for _, path := range []string{"one.txt", "two.txt"} {
		if !gitShowFileOK(repo, "integration/W-0001", path) {
			data, _, _ := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0002.md"))
			t.Fatalf("integration branch is missing %s: %s", path, stringField(data, "next_action"))
		}
	}
	dashboard := gitShowFile(t, repo, "integration/W-0001", projection)
	if !strings.Contains(dashboard, "tusker:generated:file") || strings.Contains(dashboard, "branch one") || strings.Contains(dashboard, "branch two") {
		t.Fatalf("landing did not regenerate the derived dashboard:\n%s", dashboard)
	}
	landedEpic := gitShowFile(t, repo, "integration/W-0001", epicPath)
	if strings.Contains(landedEpic, "branch one") || strings.Contains(landedEpic, "branch two") {
		t.Fatalf("landing did not regenerate epic managed blocks:\n%s", landedEpic)
	}
	if gitShowFileOK(repo, "integration/W-0001", ".tusker/workspace.json") {
		t.Fatal("workspace-local identity metadata leaked into integration")
	}
}
