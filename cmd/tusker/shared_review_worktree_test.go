package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A shared-checkout review runs in a detached worktree of the submitted
// commit, so another task's uncommitted edits in the checkout neither block
// the reviewer nor leak into what it builds.
func TestSharedCheckoutReviewRunsInWorktreeAtSubmission(t *testing.T) {
	shared, stateRoot := t.TempDir(), t.TempDir()
	runGitDir(t, shared, "init", "-q")
	runGitDir(t, shared, "config", "user.email", "t@example.com")
	runGitDir(t, shared, "config", "user.name", "t")
	write := func(rel, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(shared, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shared, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/a.go", "package src\n")
	runGitDir(t, shared, "add", "-A")
	runGitDir(t, shared, "commit", "-q", "-m", "base")
	// The submission commit exists, but its files stay dirty in the checkout.
	write("src/a.go", "package src // submitted\n")
	runGitDir(t, shared, "commit", "-q", "-am", "submission")
	sha, err := gitOutputTrim(shared, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	runGitDir(t, shared, "reset", "-q", "--soft", "HEAD~1")
	write("other/b.go", "package other // another task, half done\n")
	write(".tusker/workspace.json", `{"strategy":"shared"}`)
	if reviewerWorkspaceDirtyReason(shared) == "" {
		t.Fatal("fixture: shared checkout should be dirty")
	}

	req := WorkspacePrepareRequest{
		ProjectID: "P1", ProjectKey: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", RepoRoot: shared, StateRoot: stateRoot,
		Strategy: WorkspaceStrategyShared, BranchName: "task/app-t-0001",
	}
	path, err := prepareSharedReviewWorktree(NewWorkspaceManager(), req, sha)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, filepath.Join(stateRoot, "workspaces", "app")+string(filepath.Separator)) {
		t.Fatalf("review worktree %q is not under the project worktree root", path)
	}
	if head, _ := gitOutputTrim(path, "rev-parse", "HEAD"); head != sha {
		t.Fatalf("review worktree HEAD = %q, want submission %q", head, sha)
	}
	if fileExists(filepath.Join(path, "other", "b.go")) {
		t.Fatal("another task's uncommitted edit leaked into the review worktree")
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	parent := RunAttempt{AttemptID: "EXEC-1", ProjectID: "P1", RecordID: "APP-T-0001", Lane: runLaneExecute, WorkspacePath: shared,
		EndState: RunEndState{HeadSHA: sha, MaterialScope: []string{"src"}}}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "REV-1", ProjectID: "P1", RecordID: "APP-T-0001", Lane: runLaneReview, WorkspacePath: path, ParentAttemptID: parent.AttemptID}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: "P1", RecordID: "APP-T-0001", Lane: runLaneReview, ActiveAttemptID: "REV-1", WorkspacePath: shared}
	reviewPath, err := sharedReviewWorkspace(store, run)
	if err != nil || reviewPath != path {
		t.Fatalf("review workspace = %q err=%v, want %q", reviewPath, err, path)
	}
	// The daemon's reviewer-exit check reads the review worktree, not the checkout.
	if reason := reviewerWorkspaceDirtyReason(firstNonEmpty(reviewPath, run.WorkspacePath)); reason != "" {
		t.Fatalf("dirty shared checkout blocked the reviewer: %s", reason)
	}
	if err := reviewWorktreeMatchesSubmission(reviewPath, parent); err != nil {
		t.Fatal(err)
	}
	if env := sharedReviewCargoTargetEnv(nil, reviewPath, shared); runnerEnvValue(env, "CARGO_TARGET_DIR") != filepath.Join(shared, "target") {
		t.Fatalf("review worker does not share the checkout build cache: %v", env)
	}
	if env := sharedReviewCargoTargetEnv([]string{"CARGO_TARGET_DIR=/x"}, reviewPath, shared); runnerEnvValue(env, "CARGO_TARGET_DIR") != "/x" {
		t.Fatalf("explicit CARGO_TARGET_DIR overridden: %v", env)
	}
	if err := os.WriteFile(filepath.Join(reviewPath, "src", "a.go"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reviewWorktreeMatchesSubmission(reviewPath, parent); err == nil {
		t.Fatal("changed review worktree material still bound to the submission")
	}

	// A retry must not reuse a dirty leftover worktree.
	if again, err := prepareSharedReviewWorktree(NewWorkspaceManager(), req, sha); err != nil || again != reviewPath {
		t.Fatalf("re-prepare = %q err=%v, want %q", again, err, reviewPath)
	}
	if err := reviewWorktreeMatchesSubmission(reviewPath, parent); err != nil {
		t.Fatalf("dirty leftover review worktree was reused: %v", err)
	}
	if reason := reviewerWorkspaceDirtyReason(reviewPath); reason != "" {
		t.Fatalf("recreated review worktree is dirty: %s", reason)
	}

	// Finalizing the review attempt through the daemon removes the worktree
	// and leaves the attempt pointing at it, not at the shared checkout.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	run.LeaseState, run.AttemptOutcome, run.ProcessPID, run.AttemptCount = string(LeaseStateRunning), string(AttemptOutcomeNone), dead.Process.Pid, 1
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	updated, _, err := daemon.reconcileRun(context.Background(), RegisteredProject{ProjectID: "P1"}, WorkflowFile{Data: defaultWorkflow()}, run)
	if err != nil {
		t.Fatal(err)
	}
	if isDispatchingLeaseState(updated.LeaseState) && updated.ActiveAttemptID == run.ActiveAttemptID {
		t.Fatalf("fixture: review attempt was not finalized: %#v", updated)
	}
	attempts, err := store.ListAttemptsForRun("P1", "APP-T-0001")
	if err != nil || len(attempts) != 1 || attempts[0].WorkspacePath != reviewPath || attempts[0].Outcome == string(AttemptOutcomeNone) {
		t.Fatalf("finalized review attempt = %#v err=%v, want outcome recorded on %q", attempts, err, reviewPath)
	}
	if fileExists(reviewPath) {
		t.Fatal("review worktree survived its finalized attempt")
	}
	if list, _ := gitOutputTrim(shared, "worktree", "list"); strings.Contains(list, reviewPath) {
		t.Fatalf("git still lists the removed review worktree:\n%s", list)
	}
}

// Command verification and the review binding accept the same scope: the
// task's declared scope plus the strays a shared-checkout submission committed.
func TestSubmittedMaterialScopeIncludesStrays(t *testing.T) {
	parent := RunAttempt{EndState: RunEndState{MaterialScope: []string{"notes.txt", "src"}, StrayPaths: []string{"notes.txt"}}}
	if !submittedMaterialScopeMatches([]string{"src"}, parent) {
		t.Fatal("submission with strays refused")
	}
	parent.EndState.StrayPaths = nil
	if submittedMaterialScopeMatches([]string{"src"}, parent) {
		t.Fatal("scope that no longer matches the submission accepted")
	}
}
