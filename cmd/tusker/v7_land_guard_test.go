package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorkspaceRecordID drops a minimal .tusker/workspace.json into a worktree
// so landing provenance can bind the source to a task exactly the way a real
// runner workspace does.
func writeWorkspaceRecordID(t *testing.T, worktreePath, recordID string) {
	t.Helper()
	dir := filepath.Join(worktreePath, ".tusker")
	if err := ensureDir(dir); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(dir, "workspace.json"), fmt.Sprintf("{\"record_id\":%q}\n", recordID)); err != nil {
		t.Fatal(err)
	}
}

// landGuardWorktree materializes a detached completed worktree from the
// integration base, drops a file, commits it, and stamps it with a workspace
// record id. Returns the worktree path.
func landGuardWorktree(t *testing.T, repo, dir, recordID, file, content string) string {
	t.Helper()
	worktree := filepath.Join(t.TempDir(), dir)
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, "integration/W-0001")
	if err := writeText(filepath.Join(worktree, file), content); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", ".")
	runGitDir(t, worktree, "commit", "-m", dir)
	writeWorkspaceRecordID(t, worktree, recordID)
	return worktree
}

// OPS-T-0007 A1: `land --from <worktree>` is refused when the worktree's
// workspace record_id names a different task; a matching source still lands.
func TestLandFromWrongSourceRefused(t *testing.T) {
	repo, vault := newLandTestRepo(t, 2, "test -f recovered.txt")
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")

	// A worktree that actually belongs to APP-T-0002. Landing APP-T-0001 from it
	// must be refused, not silently landed with the wrong task's diff.
	wrong := landGuardWorktree(t, repo, "APP-T-0002__task", "APP-T-0002", "recovered.txt", "recovered\n")

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": wrong})
	if err == nil {
		t.Fatal("expected refusal landing APP-T-0001 from an APP-T-0002 worktree")
	}
	for _, want := range []string{"APP-T-0001", "APP-T-0002", "record_id"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("wrong-source refusal must be actionable; missing %q in %q", want, err.Error())
		}
	}
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("wrong-source land must not create task/APP-T-0001")
	}
	if gitShowFileOK(repo, "integration/W-0001", "recovered.txt") {
		t.Fatal("wrong-source content must never reach integration")
	}

	// The correct worktree (record_id APP-T-0001) still lands.
	right := landGuardWorktree(t, repo, "APP-T-0001__task", "APP-T-0001", "recovered.txt", "recovered\n")
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": right}); err != nil {
		t.Fatalf("matching-source land should succeed: %v", err)
	}
	if !gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("matching source should have created task/APP-T-0001")
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has the recovered work")
}

// OPS-T-0007 A1 (bare ref): a raw commit ref with no workspace metadata is
// refused without --trust-from, and accepted (logged) with it.
func TestLandFromBareRefRequiresTrust(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")

	// A bare commit with no workspace.json to prove ownership.
	worktree := filepath.Join(t.TempDir(), "detached")
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, "integration/W-0001")
	if err := writeText(filepath.Join(worktree, "recovered.txt"), "recovered\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", ".")
	runGitDir(t, worktree, "commit", "-m", "unverified work")
	commit := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": commit})
	if err == nil {
		t.Fatal("expected refusal landing from a bare commit with no workspace provenance")
	}
	for _, want := range []string{"APP-T-0001", "workspace.json", "--trust-from"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("bare-ref refusal must name the override; missing %q in %q", want, err.Error())
		}
	}
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("unverified bare ref must not create task/APP-T-0001")
	}

	// With the explicit, logged override the same source lands.
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": commit, "trust-from": "true"}); err != nil {
		t.Fatalf("--trust-from override should land the bare ref: %v", err)
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has the trusted work")
}

// OPS-T-0007 A2: auto-discovery selects only an exact record_id match; a decoy
// whose path contains the id as a prefix (APP-T-0001x) and whose record id is a
// different task must never be picked.
func TestLandAutoDiscoverExactRecordIDOnly(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")

	// Decoy: path is APP-T-0001x... (superset substring) and record id is a
	// different task. The old strings.Contains match would have chosen it.
	landGuardWorktree(t, repo, "APP-T-0001x__scratch", "APP-T-0001X", "wrong.txt", "wrong\n")

	// No worktree carries record_id APP-T-0001, so auto-discovery must refuse
	// with the branchification guidance rather than pick the decoy.
	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"})
	if err == nil {
		t.Fatal("expected refusal: no worktree with an exact APP-T-0001 record id")
	}
	if !strings.Contains(err.Error(), "no task/APP-T-0001 branch") {
		t.Fatalf("expected branchification refusal, got %v", err)
	}
	if gitBranchExists(repo, "task/APP-T-0001") {
		t.Fatal("auto-discovery must not branchify from the substring/decoy worktree")
	}

	// Give the real worktree an exact record id (at an unrelated path); it is now
	// the only match and auto-discovery selects it.
	landGuardWorktree(t, repo, "somewhere-else", "APP-T-0001", "recovered.txt", "recovered\n")
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatalf("exact record-id auto-discovery should land: %v", err)
	}
	assertEqual(t, "recovered\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has the exact-match work")
	if gitShowFileOK(repo, "integration/W-0001", "wrong.txt") {
		t.Fatal("decoy worktree content must never land")
	}
}

// OPS-T-0008 A1: when the default branch is checked out and clean, a successful
// wave land leaves that worktree's HEAD/index synced to the advanced ref (the
// landed file is present in the working tree, not just the ref).
func TestWaveLandSyncsCheckedOutMain(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f shipped.txt")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"shipped.txt": "shipped\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T02:00:00Z")

	// main is checked out in the repo-root worktree.
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatalf("wave land with clean checked-out main should succeed: %v", err)
	}
	// The working tree — not just the ref — must carry the landed file.
	assertExists(t, filepath.Join(repo, "shipped.txt"))
	head := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD"))
	mainRef := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	assertEqual(t, mainRef, head, "repo-root HEAD synced to advanced main ref")
	if status := strings.TrimSpace(gitDirOutput(t, repo, "status", "--porcelain", "shipped.txt")); status != "" {
		t.Fatalf("shipped.txt must be synced/clean in the checked-out main worktree, got status %q", status)
	}
	assertEqual(t, "shipped\n", gitShowFile(t, repo, "main", "shipped.txt"), "main advanced with wave content")
}

// OPS-T-0008 A2: when the default branch is checked out with an uncommitted
// change the advance would overwrite, the land refuses BEFORE moving the ref.
func TestWaveLandRefusesDirtyCheckedOutMain(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "true")
	// The landed work modifies README.md, which is tracked in the checked-out
	// main worktree, so a conflicting local edit blocks a safe advance.
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"README.md": "landed change\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T02:00:00Z")

	// Uncommitted local edit to README.md in the checked-out main worktree.
	if err := writeText(filepath.Join(repo, "README.md"), "operator local edit\n"); err != nil {
		t.Fatal(err)
	}
	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))

	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"})
	if err == nil {
		t.Fatal("expected refusal: main checked out with a conflicting uncommitted change")
	}
	for _, want := range []string{"main", "checked out", "W-0001"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("dirty checked-out refusal must be actionable; missing %q in %q", want, err.Error())
		}
	}
	// No partial/wedged state: the ref did not move and integration survives.
	assertEqual(t, mainBefore, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")), "main ref must not move on refusal")
	if !gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("integration branch must survive a refused wave land")
	}
	assertEqual(t, "operator local edit\n", mustReadIndexTest(t, filepath.Join(repo, "README.md")), "operator's uncommitted change preserved")
}

// OPS-T-0008 A3: when the default branch is not checked out anywhere, the plain
// update-ref fast path is unchanged.
func TestWaveLandNotCheckedOutFastPath(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f shipped.txt")
	commitLandBranch(t, repo, "task/APP-T-0001", "integration/W-0001", map[string]string{"shipped.txt": "shipped\n"})
	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-07-07T02:00:00Z")

	// Detach the repo-root HEAD so main is not checked out in any worktree.
	runGitDir(t, repo, "checkout", "--detach")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "W-0001"}); err != nil {
		t.Fatalf("wave land with main not checked out should use the update-ref fast path: %v", err)
	}
	parents := strings.Fields(gitDirOutput(t, repo, "rev-list", "--parents", "-n", "1", "main"))
	if len(parents) != 3 {
		t.Fatalf("expected a two-parent wave merge commit on main, got %v", parents)
	}
	assertEqual(t, "shipped\n", gitShowFile(t, repo, "main", "shipped.txt"), "main advanced via update-ref")
	if gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("integration branch should be deleted after wave landing")
	}
}
