package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// reviewPassFixture is a V7 task at work revision 0 in review, with its
// implementation on task/APP-T-0001, a released review run from a
// full-access Claude reviewer, and a stored v2 review result.
func reviewPassFixture(t *testing.T, verdict string, files map[string]string) (string, string, RegisteredProject, *Daemon, string) {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, "true")
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", state)
	project := newRegisteredProject(repo, vault)
	daemon, err := NewDaemon(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Close() })
	if err := daemon.store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	sha := commitLandBranch(t, repo, v7TaskBranchName("APP-T-0001"), "integration/W-0001", files)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status": "review", "readiness": "waiting_on_review", "owned_paths": []string{"reviewed.txt"},
	})
	recordReviewPassTestProof(t, vault, "APP-T-0001")
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.store.UpsertRun(RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerClaude), RunnerProfile: "claude-opus-high", Lane: runLaneReview,
		LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 0,
	}); err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{
		Schema: reviewResultSchemaV2, ProjectID: project.ProjectID, TaskID: "APP-T-0001",
		TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 0, ImplementationSHA: sha,
		AttemptID: "review-1", Actor: "reviewer:agent", Runner: "proposal", RunnerProfile: "proposal",
		Covers: []string{"A1"}, ProofFingerprint: proof, GateFingerprint: gates,
		Verdict: verdict, Summary: "reviewed", CreatedAt: "2026-09-27T10:00:00Z",
	}
	if verdict == "changes_requested" {
		result.Findings = []string{"Fix the greeting."}
	}
	if _, err := daemon.store.SaveReviewResult(result); err != nil {
		t.Fatal(err)
	}
	return repo, vault, project, daemon, sha
}

func recordReviewPassTestProof(t *testing.T, vault, taskID string) {
	t.Helper()
	if err := v7TestVerificationMutation(Args{
		"vault": vault, "quiet": "true", "id": taskID, "by": "agent:test",
		"rows": "A1|test -n ok|pass|Focused proof passed.\nA1|true|pass|Broad proof passed.",
	}); err != nil {
		t.Fatal(err)
	}
}

func reviewPassWorkflow(t *testing.T, vault, mode string) Workflow {
	t.Helper()
	return Workflow{CompletionReactor: completionReactorModeProjection{Effective: mode}}
}

func reviewPassTaskStatus(t *testing.T, vault string) string {
	t.Helper()
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	return stringField(note.Data, "status")
}

// S4: a passing review from any harness and access preset lands to the wave's
// integration branch and closes the task, with no worker-policy authority.
func TestReviewPassLandsAndClosesForAnyHarness(t *testing.T) {
	repo, vault, project, daemon, sha := reviewPassFixture(t, "pass", map[string]string{"reviewed.txt": "reviewed\n"})
	wf := reviewPassWorkflow(t, vault, string(completionReactorModeAuthoritative))
	if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
		t.Fatal(err)
	}
	if !gitMergeBaseAncestor(repo, sha, "integration/W-0001") {
		t.Fatal("reviewed source did not land on the integration branch")
	}
	if status := reviewPassTaskStatus(t, vault); status != "done" {
		t.Fatalf("task status = %q, want done", status)
	}
	// Idempotent: a second poll finds the task closed and does nothing.
	if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
		t.Fatal(err)
	}
}

func TestReviewPassHandlerIsDefaultAndDisabledLeavesTaskForOwner(t *testing.T) {
	if !reviewPassHandlerEnabled(Workflow{CompletionReactor: defaultCompletionReactorMode()}) {
		t.Fatal("the pass handler must be on by default")
	}
	_, vault, project, daemon, _ := reviewPassFixture(t, "pass", map[string]string{"reviewed.txt": "reviewed\n"})
	if err := daemon.reconcileReviewCompletion(project, reviewPassWorkflow(t, vault, string(completionReactorModeDisabled))); err != nil {
		t.Fatal(err)
	}
	if status := reviewPassTaskStatus(t, vault); status != "review" {
		t.Fatalf("disabled mode changed task status to %q", status)
	}
}

func TestReviewPassSendsStrayFilesBackToWorker(t *testing.T) {
	repo, vault, project, daemon, sha := reviewPassFixture(t, "pass", map[string]string{"reviewed.txt": "reviewed\n", "elsewhere.txt": "stray\n"})
	if err := daemon.reconcileReviewCompletion(project, reviewPassWorkflow(t, vault, string(completionReactorModeAuthoritative))); err != nil {
		t.Fatal(err)
	}
	if gitMergeBaseAncestor(repo, sha, "integration/W-0001") {
		t.Fatal("a diff outside owned_paths must not land")
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if status := stringField(note.Data, "status"); status != "rework" || !strings.Contains(note.Body, "elsewhere.txt") {
		t.Fatalf("stray files must return the task to rework naming the file; status=%q body=%q", status, note.Body)
	}
}

func TestReviewPassMergeConflictHoldsInReview(t *testing.T) {
	repo, vault, project, daemon, _ := reviewPassFixture(t, "pass", map[string]string{"reviewed.txt": "reviewed\n"})
	// Someone else landed a different reviewed.txt on the integration branch.
	other := commitLandBranch(t, repo, "other", "integration/W-0001", map[string]string{"reviewed.txt": "conflicting\n"})
	runGitDir(t, repo, "branch", "-f", "integration/W-0001", other)
	wf := reviewPassWorkflow(t, vault, string(completionReactorModeAuthoritative))
	if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
		t.Fatal(err)
	}
	if status := reviewPassTaskStatus(t, vault); status != "review" {
		t.Fatalf("merge conflict must keep the task in review, got %q", status)
	}
	run, err := daemon.store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || run == nil || run.ReasonCode != string(RunFailureMergeConflict) {
		t.Fatalf("run must record merge_conflict: %#v %v", run, err)
	}
	state := deriveTaskState(taskStateFacts{ID: "APP-T-0001", Status: "review", Run: taskStateRunFor(*run, false, false)})
	if state.State != "in_review" || state.ReasonCode != "merge_conflict" {
		t.Fatalf("task state = %#v, want In review merge_conflict", state)
	}
}

func TestReviewChangesRequestedReturnsTaskToWorker(t *testing.T) {
	_, vault, project, daemon, _ := reviewPassFixture(t, "changes_requested", map[string]string{"reviewed.txt": "reviewed\n"})
	if err := daemon.reconcileReviewCompletion(project, reviewPassWorkflow(t, vault, string(completionReactorModeAuthoritative))); err != nil {
		t.Fatal(err)
	}
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	if status := stringField(note.Data, "status"); status != "rework" || !strings.Contains(note.Body, "Fix the greeting.") {
		t.Fatalf("changes_requested must return the findings to the worker; status=%q", status)
	}
}

// F41: Tusker refusing a worker's output is not a harness crash.
func TestPolicyRefusalShowsNotAllowed(t *testing.T) {
	run := RunStatus{Lane: runLaneReview, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked),
		AttemptCount: 3, ReasonCode: string(RunFailurePolicyRefused), LastError: "review proposal rejected: example"}
	state := deriveTaskState(taskStateFacts{ID: "APP-T-0001", Status: "review", Run: taskStateRunFor(run, false, false)})
	if state.State != "blocked" || state.ReasonCode != "not_allowed" {
		t.Fatalf("policy refusal state = %#v, want Blocked not_allowed", state)
	}
}
