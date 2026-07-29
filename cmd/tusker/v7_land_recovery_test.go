package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV7LandingLockRecoversOnlyProvenStaleOwner(t *testing.T) {
	t.Run("records owner identity and excludes a live owner", func(t *testing.T) {
		vault := t.TempDir()
		release, err := acquireV7LandingLock(vault)
		if err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(vault, "_system", "land.lock")
		owner := readV7LandingLockOwnerForTest(t, lockPath)
		if owner.Schema != v7LandingLockSchema ||
			owner.Token == "" ||
			owner.PID != os.Getpid() ||
			owner.Host != runtimeLeaseHost() ||
			!owner.HostVerified ||
			owner.ProcessStartedAt == "" ||
			owner.AcquiredAt == "" {
			t.Fatalf("landing lock owner identity is incomplete: %#v", owner)
		}
		if _, err := acquireV7LandingLock(vault); err == nil || !strings.Contains(err.Error(), "already running") {
			t.Fatalf("live landing owner was not excluded: %v", err)
		}
		release()
		if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned landing lock was not released: %v", err)
		}
		if _, err := os.Stat(lockPath + ".guard"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("landing recovery guard polluted the vault: %v", err)
		}
	})

	t.Run("does not steal malformed metadata", func(t *testing.T) {
		vault, lockPath := newV7LandingLockFixture(t)
		legacy := "2026-07-25T00:00:00Z\n"
		if err := writeText(lockPath, legacy); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * v7LandingLockRecoveryGrace)
		if err := os.Chtimes(lockPath, old, old); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireV7LandingLock(vault); err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("malformed landing lock was not rejected: %v", err)
		}
		if got := mustReadIndexTest(t, lockPath); got != legacy {
			t.Fatalf("malformed landing lock was stolen: %q", got)
		}
	})

	t.Run("does not steal a fresh dead-owner lock", func(t *testing.T) {
		vault, lockPath := newV7LandingLockFixture(t)
		owner := v7LandingLockOwnerForTest(time.Now().UTC(), 999999999, "dead-process-start")
		writeV7LandingLockOwnerForTest(t, lockPath, owner)
		if _, err := acquireV7LandingLock(vault); err == nil || !strings.Contains(err.Error(), "too fresh") {
			t.Fatalf("fresh landing lock was not rejected: %v", err)
		}
		if got := readV7LandingLockOwnerForTest(t, lockPath); got.Token != owner.Token {
			t.Fatalf("fresh landing lock was stolen: before=%s after=%s", owner.Token, got.Token)
		}
	})

	t.Run("does not steal an old live-owner lock", func(t *testing.T) {
		vault, lockPath := newV7LandingLockFixture(t)
		startedAt, ok := processStartTime(os.Getpid())
		if !ok {
			t.Skip("process start identity is unavailable")
		}
		old := time.Now().UTC().Add(-2 * v7LandingLockRecoveryGrace)
		owner := v7LandingLockOwnerForTest(old, os.Getpid(), startedAt)
		writeV7LandingLockOwnerForTest(t, lockPath, owner)
		if err := os.Chtimes(lockPath, old, old); err != nil {
			t.Fatal(err)
		}
		if _, err := acquireV7LandingLock(vault); err == nil || !strings.Contains(err.Error(), "still alive") {
			t.Fatalf("live landing owner was not preserved: %v", err)
		}
		if got := readV7LandingLockOwnerForTest(t, lockPath); got.Token != owner.Token {
			t.Fatalf("live landing lock was stolen: before=%s after=%s", owner.Token, got.Token)
		}
	})

	t.Run("recovers an old dead-owner lock", func(t *testing.T) {
		vault, lockPath := newV7LandingLockFixture(t)
		old := time.Now().UTC().Add(-2 * v7LandingLockRecoveryGrace)
		stale := v7LandingLockOwnerForTest(old, 999999999, "dead-process-start")
		writeV7LandingLockOwnerForTest(t, lockPath, stale)
		if err := os.Chtimes(lockPath, old, old); err != nil {
			t.Fatal(err)
		}
		release, err := acquireV7LandingLock(vault)
		if err != nil {
			t.Fatalf("recover stale landing lock: %v", err)
		}
		defer release()
		current := readV7LandingLockOwnerForTest(t, lockPath)
		if current.Token == stale.Token || current.PID != os.Getpid() {
			t.Fatalf("stale landing owner was not replaced safely: stale=%#v current=%#v", stale, current)
		}
	})

	t.Run("recognizes pid reuse from the process start identity", func(t *testing.T) {
		vault, lockPath := newV7LandingLockFixture(t)
		old := time.Now().UTC().Add(-2 * v7LandingLockRecoveryGrace)
		stale := v7LandingLockOwnerForTest(old, os.Getpid(), "1900-01-01T00:00:00Z")
		writeV7LandingLockOwnerForTest(t, lockPath, stale)
		if err := os.Chtimes(lockPath, old, old); err != nil {
			t.Fatal(err)
		}
		release, err := acquireV7LandingLock(vault)
		if err != nil {
			t.Fatalf("recover reused-pid landing lock: %v", err)
		}
		defer release()
		if current := readV7LandingLockOwnerForTest(t, lockPath); current.Token == stale.Token {
			t.Fatalf("reused-pid landing owner was not replaced: %#v", current)
		}
	})

	t.Run("release cannot remove a successor identity", func(t *testing.T) {
		vault := t.TempDir()
		release, err := acquireV7LandingLock(vault)
		if err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(vault, "_system", "land.lock")
		successor := v7LandingLockOwnerForTest(time.Now().UTC(), os.Getpid(), "successor-start")
		writeV7LandingLockOwnerForTest(t, lockPath, successor)
		release()
		if got := readV7LandingLockOwnerForTest(t, lockPath); got.Token != successor.Token {
			t.Fatalf("former owner removed its successor: %#v", got)
		}
	})
}

func newV7LandingLockFixture(t *testing.T) (string, string) {
	t.Helper()
	vault := t.TempDir()
	lockPath := filepath.Join(vault, "_system", "land.lock")
	if err := ensureDir(filepath.Dir(lockPath)); err != nil {
		t.Fatal(err)
	}
	return vault, lockPath
}

func v7LandingLockOwnerForTest(acquiredAt time.Time, pid int, processStartedAt string) v7LandingLockOwner {
	host, hostVerified := v7LandingLockHostIdentity()
	return v7LandingLockOwner{
		Schema:               v7LandingLockSchema,
		Token:                "lock-owner-" + strings.ToLower(newRecordID()),
		PID:                  pid,
		Host:                 host,
		HostVerified:         hostVerified,
		ProcessStartedAt:     processStartedAt,
		ProcessStartVerified: true,
		AcquiredAt:           acquiredAt.Format(time.RFC3339Nano),
	}
}

func writeV7LandingLockOwnerForTest(t *testing.T, path string, owner v7LandingLockOwner) {
	t.Helper()
	raw, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, string(raw)+"\n"); err != nil {
		t.Fatal(err)
	}
}

func readV7LandingLockOwnerForTest(t *testing.T, path string) v7LandingLockOwner {
	t.Helper()
	var owner v7LandingLockOwner
	if err := json.Unmarshal([]byte(mustReadIndexTest(t, path)), &owner); err != nil {
		t.Fatal(err)
	}
	return owner
}

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

func TestLandImportsCompletedCommitFromCloneWorkspace(t *testing.T) {
	repo, vault := newLandTestRepo(t, 1, "test -f recovered.txt")
	setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")

	workspace := filepath.Join(t.TempDir(), "APP-T-0001__copy")
	runGitDir(t, t.TempDir(), "clone", "--quiet", repo, workspace)
	runGitDir(t, workspace, "config", "user.email", "copy@example.com")
	runGitDir(t, workspace, "config", "user.name", "Copy Workspace")
	runGitDir(t, workspace, "checkout", "--quiet", "integration/W-0001")
	writeWorkspaceRecordForLandTest(t, workspace, "APP-T-0001")
	if err := writeText(filepath.Join(workspace, "recovered.txt"), "copied workspace\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, workspace, "add", ".")
	runGitDir(t, workspace, "commit", "-m", "copy workspace completed work")
	completed := strings.TrimSpace(gitDirOutput(t, workspace, "rev-parse", "HEAD"))
	if _, err := gitOutputTrim(repo, "rev-parse", completed+"^{commit}"); err == nil {
		t.Fatal("precondition: canonical repository unexpectedly has the copy-only commit")
	}

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "from": workspace}); err != nil {
		t.Fatalf("clone workspace recovery land failed: %v", err)
	}
	assertEqual(t, completed, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "task/APP-T-0001")), "task branch imported exact clone commit")
	assertEqual(t, "copied workspace\n", gitShowFile(t, repo, "integration/W-0001", "recovered.txt"), "integration has cloned workspace work")
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

func TestWaveMemberPreparationFinishesByExactRefOutcome(t *testing.T) {
	t.Run("third ref race restores original canonical bytes", func(t *testing.T) {
		repo, vault := newLandReadyForMainAdvanceTest(t, "third-ref-preparation.txt", "candidate\n")
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0001"]
		expected := gitRevisionForTest(t, repo, "main")
		candidate := gitRevisionForTest(t, repo, "integration/W-0001")
		intended := strings.TrimSpace(gitDirOutput(t, repo, "commit-tree", candidate+"^{tree}", "-p", expected, "-p", candidate, "-m", "intended promotion"))
		original := departureWaveMemberBytes(t, vault, "W-0001", "APP-T-0001")

		preparation, err := prepareV7WaveMembersForDefaultAdvance(repo, vault, "main", wave)
		if err != nil {
			t.Fatal(err)
		}
		prepared := departureWaveMemberBytes(t, vault, "W-0001", "APP-T-0001")
		if sameStringMap(original, prepared) {
			t.Fatal("fixture did not produce live canonical bytes for preparation to protect")
		}
		third := strings.TrimSpace(gitDirOutput(t, repo, "commit-tree", expected+"^{tree}", "-p", expected, "-m", "external third ref"))
		runGitDir(t, repo, "update-ref", "refs/heads/main", third, expected)

		if err := preparation.finishAfterRefAttempt(repo, "main", expected, intended); err != nil {
			t.Fatal(err)
		}
		assertDepartureWaveMemberBytes(t, vault, original)
	})

	t.Run("intended ref ownership does not restore the preimage", func(t *testing.T) {
		repo, vault := newLandReadyForMainAdvanceTest(t, "intended-ref-preparation.txt", "candidate\n")
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		wave := idx.Waves["W-0001"]
		expected := gitRevisionForTest(t, repo, "main")
		candidate := gitRevisionForTest(t, repo, "integration/W-0001")
		intended := strings.TrimSpace(gitDirOutput(t, repo, "commit-tree", candidate+"^{tree}", "-p", expected, "-p", candidate, "-m", "intended promotion"))
		original := departureWaveMemberBytes(t, vault, "W-0001", "APP-T-0001")

		preparation, err := prepareV7WaveMembersForDefaultAdvance(repo, vault, "main", wave)
		if err != nil {
			t.Fatal(err)
		}
		prepared := departureWaveMemberBytes(t, vault, "W-0001", "APP-T-0001")
		if sameStringMap(original, prepared) {
			t.Fatal("fixture did not produce live canonical bytes for preparation to protect")
		}
		runGitDir(t, repo, "update-ref", "refs/heads/main", intended, expected)

		if err := preparation.finishAfterRefAttempt(repo, "main", expected, intended); err != nil {
			t.Fatal(err)
		}
		assertDepartureWaveMemberBytes(t, vault, prepared)
		if sameStringMap(original, departureWaveMemberBytes(t, vault, "W-0001", "APP-T-0001")) {
			t.Fatal("intended ref observation incorrectly restored the preimage")
		}
	})
}

func TestWaveMemberPreparationRestoreCAS(t *testing.T) {
	t.Run("restores exact preimage when checkout bytes differ from index blob", func(t *testing.T) {
		_, _, path, preparation, before, _ := newWaveMemberPreparationCASFixture(t)
		if err := preparation.restore(); err != nil {
			t.Fatal(err)
		}
		restored, err := v7PreparedWorktreeState(path)
		if err != nil {
			t.Fatal(err)
		}
		if !sameV7PreparedWaveMemberExactState(restored, before) {
			t.Fatalf("restored state = bytes %q mode %s, want bytes %q mode %s",
				string(restored.Bytes), restored.Mode, string(before.Bytes), before.Mode)
		}
	})

	t.Run("preserves a concurrent operator worktree edit", func(t *testing.T) {
		_, _, path, preparation, _, _ := newWaveMemberPreparationCASFixture(t)
		if err := writeText(path, "operator worktree edit\n"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o604); err != nil {
			t.Fatal(err)
		}
		err := preparation.restore()
		if err == nil || !strings.Contains(err.Error(), "worktree changed after default-advance preparation") {
			t.Fatalf("concurrent worktree edit was not fenced: %v", err)
		}
		current, stateErr := v7PreparedWorktreeState(path)
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		if string(current.Bytes) != "operator worktree edit\n" || current.Mode.Perm() != 0o604 {
			t.Fatalf("operator edit was overwritten: bytes=%q mode=%s", string(current.Bytes), current.Mode)
		}
	})

	t.Run("preserves a concurrent index owner", func(t *testing.T) {
		repo, relative, path, preparation, _, preparedWorktree := newWaveMemberPreparationCASFixture(t)
		if err := writeText(path, "concurrent index owner\n"); err != nil {
			t.Fatal(err)
		}
		runGitDir(t, repo, "add", "--", relative)
		if err := os.WriteFile(path, preparedWorktree.Bytes, preparedWorktree.Mode.Perm()); err != nil {
			t.Fatal(err)
		}
		err := preparation.restore()
		if err == nil || !strings.Contains(err.Error(), "index changed after default-advance preparation") {
			t.Fatalf("concurrent index owner was not fenced: %v", err)
		}
		current, stateErr := v7PreparedWorktreeState(path)
		if stateErr != nil {
			t.Fatal(stateErr)
		}
		if !sameV7PreparedWaveMemberExactState(current, preparedWorktree) {
			t.Fatalf("worktree owned by changed index was overwritten: bytes=%q mode=%s", string(current.Bytes), current.Mode)
		}
		indexBytes := gitDirOutput(t, repo, "show", ":"+relative)
		if indexBytes != "concurrent index owner\n" {
			t.Fatalf("concurrent index blob was overwritten: %q", indexBytes)
		}
	})
}

func newWaveMemberPreparationCASFixture(t *testing.T) (repo, relative, path string, preparation *v7WaveMemberPreparation, before, preparedWorktree v7PreparedWaveMemberState) {
	t.Helper()
	repo = t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "test@example.com")
	runGitDir(t, repo, "config", "user.name", "Test User")
	relative = ".tusker/work/waves/W-0001.md"
	path = filepath.Join(repo, filepath.FromSlash(relative))
	if err := writeText(path, "raw index representation\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "--", relative)
	runGitDir(t, repo, "commit", "-m", "seed index representation")
	preparedIndex, tracked, err := v7PreparedIndexState(repo, relative)
	if err != nil || !tracked {
		t.Fatalf("capture prepared index: tracked=%v err=%v", tracked, err)
	}
	before = v7PreparedWaveMemberState{
		Exists: true,
		Mode:   0o600,
		Bytes:  []byte("exact live preimage\n"),
	}
	if err := os.WriteFile(path, []byte("checkout-filtered worktree representation\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	preparedWorktree, err = v7PreparedWorktreeState(path)
	if err != nil {
		t.Fatal(err)
	}
	if sameV7PreparedWaveMemberBytes(preparedIndex, preparedWorktree) {
		t.Fatal("fixture requires distinct index and post-checkout worktree representations")
	}
	preparation = &v7WaveMemberPreparation{paths: []v7PreparedWaveMemberPath{{
		Absolute:         path,
		WorkDir:          repo,
		Relative:         relative,
		Before:           before,
		PreparedIndex:    preparedIndex,
		PreparedWorktree: preparedWorktree,
	}}}
	return repo, relative, path, preparation, before, preparedWorktree
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
