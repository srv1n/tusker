package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// wmlLandRepo is a landing fixture with no wave integration branch: per-task
// projects never create one.
func wmlLandRepo(t *testing.T, tasks int, gate, strategy string) (string, string) {
	t.Helper()
	repo, vault := newLandTestRepo(t, tasks, gate)
	runGitDir(t, repo, "branch", "-D", "integration/W-0001")
	wf := defaultWorkflow()
	wf.Workspace.Strategy = strategy
	if strategy == string(WorkspaceStrategyWorktree) {
		wf.Workspace.Root = "workspaces"
	}
	_, body, _ := strings.Cut(strings.TrimPrefix(defaultWorkflowMarkdown(), "---\n"), "\n---\n")
	writeWorkflowForPreflightTest(t, vault, wf, body)
	runGitDir(t, repo, "add", "-A")
	runGitDir(t, repo, "commit", "-m", "workflow")
	return repo, vault
}

func wmlRefs(t *testing.T, repo string) []string {
	t.Helper()
	return strings.Fields(gitDirOutput(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads"))
}

// M1-M3: a shared-checkout task lands in the project checkout on main. No
// task, review or wave branch is created, no worktree is left behind, the
// landing commit changes only the task's owned files, and other agents'
// uncommitted work in the checkout is untouched.
func TestSharedModeLandsInCheckout(t *testing.T) {
	repo, vault := wmlLandRepo(t, 1, "test -f src/a.txt", string(WorkspaceStrategyShared))
	if shared, err := sharedCheckoutStrategy(vault); err != nil || !shared {
		t.Fatalf("fixture is not a shared checkout: %v %v", shared, err)
	}
	before := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	// The worker edits its owned file in place; another agent has work in progress.
	if err := writeText(filepath.Join(repo, "src", "a.txt"), "shared work\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, "other-wip.txt"), "someone else's work\n"); err != nil {
		t.Fatal(err)
	}
	source, err := materializeWorkerSubmissionCommit(RunStatus{RecordID: "APP-T-0001", WorkspacePath: repo}, []string{"src/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := landV7CmdAsReviewPass(context.Background(), Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"},
		map[string]string{"APP-T-0001": source}, map[string]string{"APP-T-0001": source}); err != nil {
		t.Fatal(err)
	}
	after := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	if after == before || !gitMergeBaseAncestor(repo, source, "main") {
		t.Fatal("the shared task did not land on main")
	}
	assertEqual(t, after, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD")), "the checkout stays on main and moves with it")
	assertEqual(t, "shared work\n", gitShowFile(t, repo, "main", "src/a.txt"), "landed owned file")
	assertEqual(t, "src/a.txt", strings.TrimSpace(gitDirOutput(t, repo, "diff", "--name-only", before, after, "--", ".", ":!.tusker")), "the landing commits only the task's owned files")
	assertEqual(t, []string{"main"}, wmlRefs(t, repo), "no task, review or wave branch")
	if worktrees := v7ListWorktrees(repo); len(worktrees) != 1 {
		t.Fatalf("shared landing left worktrees behind: %#v", worktrees)
	}
	assertEqual(t, "someone else's work\n", mustReadIndexTest(t, filepath.Join(repo, "other-wip.txt")), "other agents' work is untouched")
	if status := strings.TrimSpace(gitDirOutput(t, repo, "status", "--porcelain", "--", "src/a.txt")); status != "" {
		t.Fatalf("landed file should be clean in the checkout, got %q", status)
	}
}

// M4: in worktree mode each task merges into main as soon as it lands. When
// main has moved, the task is merged onto current main and the landing check
// runs on the merged result; a real conflict is refused without moving main.
func TestWorktreeModeMergesEachTaskToMain(t *testing.T) {
	repo, vault := wmlLandRepo(t, 3, "test -f README.md", string(WorkspaceStrategyWorktree))
	base := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	commitLandBranch(t, repo, "task/APP-T-0001", "main", map[string]string{"src/a.txt": "a\n"})
	commitLandBranch(t, repo, "task/APP-T-0002", "main", map[string]string{"src/b.txt": "b\n"})
	commitLandBranch(t, repo, "task/APP-T-0003", "main", map[string]string{"README.md": "task three\n"})

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "a\n", gitShowFile(t, repo, "main", "src/a.txt"), "first task merged into main")
	if gitBranchExists(repo, "integration/W-0001") {
		t.Fatal("per-task landing created a wave integration branch")
	}

	// Main moves underneath the remaining tasks.
	if err := writeText(filepath.Join(repo, "README.md"), "operator edit\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "commit", "-am", "operator commit on main")

	if err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0002"}); err != nil {
		t.Fatalf("a task based on an older main must still land: %v", err)
	}
	assertEqual(t, "b\n", gitShowFile(t, repo, "main", "src/b.txt"), "second task merged onto moved main")
	assertEqual(t, "operator edit\n", gitShowFile(t, repo, "main", "README.md"), "main's own commit kept")
	if !gitMergeBaseAncestor(repo, base, "main") {
		t.Fatal("main history was rewritten")
	}

	mainBefore := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main"))
	err := landV7Cmd(Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0003"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("a real conflict must be refused naming the file: %v", err)
	}
	assertEqual(t, mainBefore, strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "main")), "a conflict never moves main")
	for _, ref := range wmlRefs(t, repo) {
		if strings.HasPrefix(ref, "integration/") {
			t.Fatalf("wave branch %s exists", ref)
		}
	}
}

// M5: commits to main (including edits to cited specs) never stale a wave's
// authorization; a change to a member's promise does, and Start then succeeds
// while that member's attempt runs, the new authorization applying to its
// next attempt.
func TestReapprovalOnlyForPromiseChanges(t *testing.T) {
	vault, store, project := authorityFixture(t)
	repoRoot := v7RepoRoot(vault)
	specPath := filepath.Join(repoRoot, "docs", "specs", "promise.md")
	if err := writeText(specPath, "# Promise\n"); err != nil {
		t.Fatal(err)
	}
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"spec_refs": []any{"docs/specs/promise.md"}, "owned_paths": []any{"src/a"}})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	first, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatal(err)
	}
	directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001")
	if directive == nil {
		t.Fatal("start queued no directive")
	}
	// The member's attempt is running under this authorization.
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-1", LeaseGeneration: 1, ActiveAttemptID: "attempt-1", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{ProjectID: project.ProjectID, RecordID: "APP-T-0001", Source: "human_run_directive", Actor: "human:sarav", LeaseGeneration: 1, AttemptID: "attempt-1", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: first.MaterialFingerprint, DirectiveWaveAuthorizedAt: directive.WaveAuthorizedAt}); err != nil {
		t.Fatal(err)
	}

	// A commit to main rewrites the cited spec: no re-approval.
	if err := writeText(specPath, "# Promise\n\nReworded on main.\n"); err != nil {
		t.Fatal(err)
	}
	if auth := waveAuthorizationState(t, vault, "W-0001"); boolFromAny(auth["stale"]) || stringField(auth, "state") != "armed" {
		t.Fatalf("a commit to main staled the wave: %#v", auth)
	}

	// The member's promise changes: re-approval is needed.
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["owned_paths"] = []any{"src/a", "src/b"}
		return data, body
	})
	if auth := waveAuthorizationState(t, vault, "W-0001"); !boolFromAny(auth["stale"]) {
		t.Fatalf("a promise change did not need re-approval: %#v", auth)
	}

	// Re-approval succeeds while the attempt runs.
	second, err := directWaveStart(vault, store, "W-0001", "human:sarav")
	if err != nil {
		t.Fatalf("start refused while a member attempt runs: %v", err)
	}
	if second.MaterialFingerprint == first.MaterialFingerprint {
		t.Fatal("re-approval kept the old fingerprint")
	}
	if auth := waveAuthorizationState(t, vault, "W-0001"); boolFromAny(auth["stale"]) || stringField(auth, "state") != "armed" {
		t.Fatalf("re-approval did not arm the new promise: %#v", auth)
	}
	run, _ := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if run == nil || run.ActiveAttemptID != "attempt-1" || run.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("re-approval disturbed the running attempt: %#v", run)
	}

	// The attempt ends; the member's next attempt runs under the new authorization.
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateReleased), Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	next, _ := store.RunDirective(project.ProjectID, "APP-T-0001")
	if next == nil || next.State != "queued" || next.AuthorizationFingerprint != second.MaterialFingerprint {
		t.Fatalf("next attempt is not bound to the new authorization: %#v", next)
	}
}
