package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reviewPassEnv is a real review hand-off: a V7 task at work revision 0 in
// review, its implementation committed on task/APP-T-0001 in a live execute
// worktree, a succeeded execute attempt that captured that material, and a
// running review attempt from a full-access Claude reviewer.
type reviewPassEnv struct {
	repo, vault, worktree, source string
	project                       RegisteredProject
	daemon                        *Daemon
	wfFile                        WorkflowFile
	run                           RunStatus
}

func newReviewPassEnv(t *testing.T, files map[string]string, owned []string) *reviewPassEnv {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, "true")
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", "-A")
	runGitDir(t, repo, "commit", "-m", "workflow")
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", state)
	project := newRegisteredProject(repo, vault)
	project.Enabled, project.Health = true, projectHealthHealthy
	daemon, err := NewDaemon(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Close() })
	if err := daemon.store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}

	worktree := filepath.Join(t.TempDir(), "execute")
	runGitDir(t, repo, "worktree", "add", "-b", v7TaskBranchName("APP-T-0001"), worktree, "integration/W-0001")
	for path, content := range files {
		if err := writeText(filepath.Join(worktree, path), content); err != nil {
			t.Fatal(err)
		}
	}
	runGitDir(t, worktree, "add", "-A")
	runGitDir(t, worktree, "commit", "-m", "implementation")
	source := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))

	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "owned_paths": owned})
	task, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	// One pending command row: the daemon must run it in the worktree.
	body = replaceSection(body, "## Verification", renderV7VerificationTable([]v7VerificationRow{
		{CoverText: "A1", Check: "command: test -f reviewed.txt", Result: "pending"},
	}))
	data["contract_fingerprint"] = directWaveTaskContractFingerprint(data, body)
	if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
	task, err = resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	scope, err := canonicalTaskMaterialScope(vault, task)
	if err != nil {
		t.Fatal(err)
	}
	material, err := workspaceTreeStateHashForPaths(worktree, scope)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := daemon.store.SaveAttempt(RunAttempt{AttemptID: "exec-1", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute, WorkRevision: 0, WorkspacePath: worktree, Outcome: string(AttemptOutcomeSucceeded),
		StartedAt: now.Add(-time.Hour).Format(time.RFC3339),
		EndState:  RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: source, WorktreePath: worktree, MaterialFingerprint: material, MaterialScope: scope}}); err != nil {
		t.Fatal(err)
	}
	env := &reviewPassEnv{repo: repo, vault: vault, worktree: worktree, source: source, project: project, daemon: daemon}
	env.wfFile, err = loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	env.startReview(t, "review-1", now.Add(-30*time.Minute))
	return env
}

// startReview records a running review attempt and makes it the run's owner.
func (env *reviewPassEnv) startReview(t *testing.T, attemptID string, at time.Time) {
	t.Helper()
	runDir := filepath.Join(env.daemon.stateRoot, "runs", env.project.ProjectKey, "APP-T-0001")
	if err := ensureDir(runDir); err != nil {
		t.Fatal(err)
	}
	if err := env.daemon.store.SaveAttempt(RunAttempt{AttemptID: attemptID, ProjectID: env.project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerClaude), Lane: runLaneReview, WorkRevision: 0, WorkspacePath: env.worktree, ParentAttemptID: "exec-1",
		StartedAt: at.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	generation := env.run.LeaseGeneration + 1
	env.run = RunStatus{
		ProjectID: env.project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerClaude), RunnerProfile: "claude-opus-high", RunnerHarness: string(RunnerClaude),
		Lane: runLaneReview, LeaseState: string(LeaseStateRunning), LeaseOwner: attemptID, ActiveAttemptID: attemptID,
		LeaseGeneration: generation, WorkRevision: 0, WorkspacePath: env.worktree, AttemptCount: 1,
		RawLogPath: filepath.Join(runDir, attemptID+".raw.log"), StatusPath: filepath.Join(runDir, attemptID+".status.json"),
	}
	if err := env.daemon.store.UpsertRun(env.run); err != nil {
		t.Fatal(err)
	}
	if err := env.daemon.store.SaveRunAuthorization(RunAuthorization{ProjectID: env.project.ProjectID, RecordID: "APP-T-0001", LeaseGeneration: generation,
		Source: "daemon_auto", Actor: "agent:reviewer", Trigger: "review", ProjectAutomationEnabled: true}); err != nil {
		t.Fatal(err)
	}
}

// submitProposal writes the reviewer's proposal and lets the daemon settle the
// review run: it validates the proposal, runs the pending command row in the
// execute worktree, and stores the result.
func (env *reviewPassEnv) submitProposal(t *testing.T, verdict string) {
	t.Helper()
	task, err := resolveV7Note(env.vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(env.vault, task)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := resolveV7ProjectID(env.vault)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{
		Schema: reviewResultSchemaV2, ProjectID: projectID, TaskID: "APP-T-0001",
		TaskStateRev: stringField(task.Data, "state_rev"), WorkRevision: 0, ImplementationSHA: env.source,
		AttemptID: env.run.ActiveAttemptID, Actor: reviewerActorForNote(env.wfFile.Data.Reviewer.Actor, task),
		Covers: sortedUniqueStrings(v7AcceptanceIDs(task.Body)), ProofFingerprint: proof, GateFingerprint: gates,
		Verdict: verdict, Summary: "reviewed", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if verdict == "changes_requested" {
		result.Covers = []string{}
		result.Findings = []string{"Fix the greeting."}
	}
	raw, err := json.Marshal(reviewProposal{Schema: reviewProposalSchema, AttemptID: env.run.ActiveAttemptID, Result: result})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(env.run.RawLogPath, reviewProposalMarker+string(raw)+"\n"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(env.run.RawLogPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRunnerStatusFile(env.run.StatusPath, 0); err != nil {
		t.Fatal(err)
	}
	updated, _, err := env.daemon.reconcileRun(context.Background(), env.project, env.wfFile, env.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.daemon.store.UpsertRun(updated); err != nil {
		t.Fatal(err)
	}
	if updated.ReasonCode != "" || !strings.Contains(updated.LastError, "typed review result recorded") {
		t.Fatalf("daemon did not record the proposal: %#v", updated)
	}
	env.run = updated
}

func (env *reviewPassEnv) passHandler(t *testing.T) {
	t.Helper()
	if err := env.daemon.reconcileReviewCompletion(env.project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}); err != nil {
		t.Fatal(err)
	}
}

func (env *reviewPassEnv) task(t *testing.T) Note {
	t.Helper()
	task, err := resolveV7Note(env.vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func (env *reviewPassEnv) landed() bool {
	return gitMergeBaseAncestor(env.repo, env.source, "integration/W-0001")
}

func (env *reviewPassEnv) latestRun(t *testing.T) RunStatus {
	t.Helper()
	run, err := env.daemon.store.FindRunScoped(env.project.ProjectID, "APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("load run: %#v %v", run, err)
	}
	return *run
}

// S4: a passing review from a full-access Claude reviewer goes through real
// proposal validation and command verification, lands exactly the reviewed
// commit, and closes the task.
func TestReviewPassLandsAndClosesForAnyHarness(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	rows := parseV7VerificationRows(env.task(t).Body)
	if len(rows) == 0 || rows[len(rows)-1].Result != "pass" {
		t.Fatalf("the daemon did not run the command row: %#v", rows)
	}
	env.passHandler(t)
	if !env.landed() {
		t.Fatal("reviewed commit did not land on the integration branch")
	}
	if task := env.task(t); stringField(task.Data, "status") != "done" || stringField(task.Data, "source_sha") != env.source {
		t.Fatalf("closed task status/source = %q/%q, want done/%q", stringField(task.Data, "status"), stringField(task.Data, "source_sha"), env.source)
	}
	env.passHandler(t) // idempotent
}

func TestReviewPassDepartureRecoversLandingReceipt(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	env.passHandler(t)
	assertReviewPassDepartureRecovery(t, env)
}

func assertReviewPassDepartureRecovery(t *testing.T, env *reviewPassEnv) {
	t.Helper()
	if !env.landed() {
		t.Fatal("review-pass source was not integrated")
	}
	candidate := DepartureCandidate{CargoTaskIDs: []string{"APP-T-0001"}, WaveIDs: []string{"W-0001"},
		TaskStateRevisions: map[string]string{"APP-T-0001": stringField(env.task(t).Data, "state_rev")},
		TaskSourceSHAs:     map[string]string{"APP-T-0001": env.source}}
	if unstaged, err := departureUnstagedCargo(env.project, candidate, env.daemon.store); err != nil || len(unstaged) != 0 {
		t.Fatalf("review-pass landing is not authenticated for departure: unstaged=%v err=%v", unstaged, err)
	}
	wrongStore, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "unrelated-store"))
	if err != nil {
		t.Fatal(err)
	}
	defer wrongStore.Close()
	if unstaged, err := departureUnstagedCargo(env.project, candidate, wrongStore); err != nil || len(unstaged) != 1 {
		t.Fatalf("unrelated store authenticated review-pass landing: unstaged=%v err=%v", unstaged, err)
	}
	run, _, err := env.daemon.store.GetOrCreateDepartureRun(DepartureRun{ProjectID: env.project.ProjectID,
		PolicyID: "review-pass-departure", ScheduledWindow: time.Now().UTC().Format(time.RFC3339Nano),
		State: DepartureStateStaging, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := env.daemon.issueV7LandingAuthority(env.project, env.wfFile.Data, run, candidate, "integration/W-0001")
	if err != nil {
		t.Fatal(err)
	}
	setScheduledPromotionPolicyForTest(t, env.vault, scheduledPromotionStage)
	before := departureLandingAuditCount(t, env.vault, "W-0001", "APP-T-0001")
	removeDepartureTaskLandingAudit(t, env.vault, "W-0001", "APP-T-0001", env.source)
	if err := stageScheduledTasksWithAuthority(env.vault, []string{"APP-T-0001"}, candidate.TaskSourceSHAs, "daemon:departure:"+run.ID, authority); err != nil {
		t.Fatalf("departure staging did not recover review-pass receipt: %v", err)
	}
	if after := departureLandingAuditCount(t, env.vault, "W-0001", "APP-T-0001"); after != before {
		t.Fatalf("departure recovery duplicated landing audit: before=%d after=%d", before, after)
	}
}

func TestReviewPassLandsDaemonSubmissionButCLIRefusesIt(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "original\n"}, []string{"reviewed.txt"})
	runGitDir(t, env.worktree, "switch", "--detach")
	runGitDir(t, env.repo, "branch", "-D", v7TaskBranchName("APP-T-0001"))
	if err := writeText(filepath.Join(env.worktree, "reviewed.txt"), "daemon submission\n"); err != nil {
		t.Fatal(err)
	}
	source, err := materializeWorkerSubmissionCommit(RunStatus{RecordID: "APP-T-0001", WorkspacePath: env.worktree}, []string{"reviewed.txt"})
	if err != nil {
		t.Fatal(err)
	}
	env.source = source
	runGitDir(t, env.worktree, "reset", "--hard", source)
	attempts, err := env.daemon.store.ListAttemptsForRun(env.project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range attempts {
		if attempt.AttemptID != "exec-1" {
			continue
		}
		attempt.EndState.HeadSHA = source
		attempt.EndState.Dirty = true
		attempt.EndState.MaterialFingerprint, err = workspaceTreeStateHashForPaths(env.worktree, attempt.EndState.MaterialScope)
		if err != nil {
			t.Fatal(err)
		}
		attempt.EndStateJSON = ""
		if err := env.daemon.store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	if err := landV7Cmd(Args{"vault": env.vault, "quiet": "true", "_pos0": "APP-T-0001", "from": source}); err == nil || !strings.Contains(err.Error(), "lacks task-owned provenance") {
		t.Fatalf("plain land must refuse the daemon submission without --trust-from: %v", err)
	}
	env.submitProposal(t, "pass")
	env.passHandler(t)
	if !env.landed() || stringField(env.task(t).Data, "status") != "done" {
		t.Fatalf("daemon submission did not land and close: run %#v", env.latestRun(t))
	}
	for _, row := range env.waveLandingRows(t) {
		if stringField(row, "task") == "APP-T-0001" && stringField(row, "source_provenance") == "daemon_submission" {
			assertReviewPassDepartureRecovery(t, env)
			return
		}
	}
	t.Fatalf("landing audit lacks daemon_submission provenance: %#v", env.waveLandingRows(t))
}

// Only an explicit authoritative mode turns the handler on; an existing
// project with no mode keeps landing by hand.
func TestReviewPassHandlerNeedsExplicitAuthoritativeMode(t *testing.T) {
	if reviewPassHandlerEnabled(Workflow{CompletionReactor: defaultCompletionReactorMode()}) {
		t.Fatal("the fresh default must not auto-land")
	}
	if reviewPassHandlerEnabled(Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeLegacy)}}) {
		t.Fatal("legacy absent-mode projects must not auto-land")
	}
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	if err := env.daemon.reconcileReviewCompletion(env.project, Workflow{CompletionReactor: defaultCompletionReactorMode()}); err != nil {
		t.Fatal(err)
	}
	if env.landed() || stringField(env.task(t).Data, "status") != "review" {
		t.Fatal("default mode landed or closed the task")
	}
}

// A crash after verification (result stored, nothing landed) and a crash after
// the ref update (landed, not closed) both finish on the next poll.
func TestReviewPassResumesAfterStops(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass") // stop after verification: the handler has not run
	crash := errors.New("injected stop after land")
	injectReviewPassCrash = func(string) error { return crash }
	t.Cleanup(func() { injectReviewPassCrash = nil })
	env.passHandler(t)
	if !env.landed() || stringField(env.task(t).Data, "status") != "review" {
		t.Fatal("the stop after the ref update should leave the task landed but in review")
	}
	injectReviewPassCrash = nil
	env.passHandler(t)
	if status := stringField(env.task(t).Data, "status"); status != "done" {
		t.Fatalf("resume after the ref update did not close: %q", status)
	}
}

// The branch moved after review: only the reviewed commit may land.
func TestReviewPassLandsOnlyTheReviewedCommit(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	later := filepath.Join(t.TempDir(), "later")
	runGitDir(t, env.repo, "worktree", "add", later, env.source+"^0")
	if err := writeText(filepath.Join(later, "unreviewed.txt"), "later\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, later, "add", "-A")
	runGitDir(t, later, "commit", "-m", "after review")
	advanced := strings.TrimSpace(gitDirOutput(t, later, "rev-parse", "HEAD"))
	runGitDir(t, env.repo, "update-ref", "refs/heads/"+v7TaskBranchName("APP-T-0001"), advanced)
	env.passHandler(t)
	if gitMergeBaseAncestor(env.repo, advanced, "integration/W-0001") {
		t.Fatal("an unreviewed commit landed")
	}
	if env.landed() {
		// The reviewed commit may land; the later one must not.
		return
	}
	if run := env.latestRun(t); !reviewPassHoldCode(run.ReasonCode) {
		t.Fatalf("a refused landing must hold the task for the owner: %#v", run)
	}
}

// A newer review attempt with no result makes the older pass stale.
func TestReviewPassIgnoresPassFromOlderAttempt(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	env.startReview(t, "review-2", time.Now().UTC())
	release := env.run
	release.LeaseState, release.ActiveAttemptID, release.LeaseOwner = string(LeaseStateReleased), "", ""
	if err := env.daemon.store.UpsertRun(release); err != nil {
		t.Fatal(err)
	}
	env.passHandler(t)
	if env.landed() || stringField(env.task(t).Data, "status") != "review" {
		t.Fatal("a pass from an older review attempt landed")
	}
}

// The execute worktree changed after review: the pass is stale.
func TestReviewPassRefusesMaterialChangedAfterReview(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	if err := writeText(filepath.Join(env.worktree, "reviewed.txt"), "edited after review\n"); err != nil {
		t.Fatal(err)
	}
	env.passHandler(t)
	if env.landed() {
		t.Fatal("stale material landed")
	}
	run := env.latestRun(t)
	if run.ReasonCode != string(RunFailureLandingFailed) || !strings.Contains(run.LastError, "stale") {
		t.Fatalf("stale pass must hold with a reason: %#v", run)
	}
}

func TestReviewPassSendsStrayFilesBackToWorker(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n", "elsewhere.txt": "stray\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	env.passHandler(t)
	task := env.task(t)
	if env.landed() || stringField(task.Data, "status") != "rework" || !strings.Contains(task.Body, "elsewhere.txt") {
		t.Fatalf("stray file must return the task to rework naming it; status=%q", stringField(task.Data, "status"))
	}
}

// A rename from outside owned_paths into it is a delete outside the scope.
func TestReviewPassChecksBothSidesOfARename(t *testing.T) {
	stray, undeclared, err := func() ([]string, bool, error) {
		env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
		runGitDir(t, env.worktree, "mv", "README.md", "reviewed-readme.txt")
		runGitDir(t, env.worktree, "commit", "-m", "rename into scope")
		source := strings.TrimSpace(gitDirOutput(t, env.worktree, "rev-parse", "HEAD"))
		wave, ok := reviewPassWave(env.vault, env.task(t))
		if !ok {
			t.Fatal("fixture task is not a wave member")
		}
		return reviewPassStrayPaths(env.project, setTaskOwned(t, env, []string{"reviewed.txt", "reviewed-readme.txt"}), wave, source)
	}()
	if err != nil || undeclared || strings.Join(stray, ",") != "README.md" {
		t.Fatalf("rename source outside owned_paths must be reported: stray=%v undeclared=%v err=%v", stray, undeclared, err)
	}
}

func setTaskOwned(t *testing.T, env *reviewPassEnv, owned []string) Note {
	t.Helper()
	task := env.task(t)
	task.Data["owned_paths"] = owned
	return task
}

// A task with no owned_paths never auto-lands repository changes.
func TestReviewPassHoldsTaskWithoutOwnedPaths(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	wave, _ := reviewPassWave(env.vault, env.task(t))
	_, undeclared, err := reviewPassStrayPaths(env.project, setTaskOwned(t, env, nil), wave, env.source)
	if err != nil || !undeclared {
		t.Fatalf("empty owned_paths must hold for the owner: undeclared=%v err=%v", undeclared, err)
	}
}

// The task points at a wave that no longer lists it.
func TestReviewPassRequiresTwoWayWaveMembership(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	wave, ok := reviewPassWave(env.vault, env.task(t))
	if !ok {
		t.Fatal("fixture task is not a wave member")
	}
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	data["members"] = []string{}
	if _, err := saveV7DocumentCAS(wave.AbsolutePath, data, body, v7FrontmatterOrder["wave"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
	env.passHandler(t)
	if env.landed() || stringField(env.task(t).Data, "status") != "review" {
		t.Fatal("a task removed from its wave landed")
	}
}

func TestReviewPassMergeConflictHoldsInReview(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	other := commitLandBranch(t, env.repo, "other", "integration/W-0001", map[string]string{"reviewed.txt": "conflicting\n"})
	runGitDir(t, env.repo, "branch", "-f", "integration/W-0001", other)
	env.passHandler(t)
	if stringField(env.task(t).Data, "status") != "review" {
		t.Fatal("merge conflict must keep the task in review")
	}
	run := env.latestRun(t)
	if run.ReasonCode != string(RunFailureMergeConflict) {
		t.Fatalf("run must record merge_conflict: %#v", run)
	}
	state := deriveTaskState(taskStateFacts{ID: "APP-T-0001", Status: "review", Run: taskStateRunFor(run, false, false)})
	if state.State != "in_review" || state.ReasonCode != "merge_conflict" {
		t.Fatalf("task state = %#v, want In review merge_conflict", state)
	}
}

func TestReviewChangesRequestedReturnsTaskToWorker(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "changes_requested")
	env.passHandler(t)
	task := env.task(t)
	if stringField(task.Data, "status") != "rework" || !strings.Contains(task.Body, "Fix the greeting.") {
		t.Fatalf("changes_requested must return the findings to the worker; status=%q", stringField(task.Data, "status"))
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

// stopAfterLand runs the handler with a stop between the ref update and the
// close, leaving the reviewed commit landed and the task in review.
func (env *reviewPassEnv) stopAfterLand(t *testing.T) {
	t.Helper()
	env.submitProposal(t, "pass")
	injectReviewPassCrash = func(string) error { return errors.New("injected stop after land") }
	env.passHandler(t)
	injectReviewPassCrash = nil
	if !env.landed() || stringField(env.task(t).Data, "status") != "review" {
		t.Fatal("the stop after the ref update should leave the task landed but in review")
	}
}

func (env *reviewPassEnv) waveLandingRows(t *testing.T) []map[string]any {
	t.Helper()
	wave, ok := reviewPassWave(env.vault, env.task(t))
	if !ok {
		t.Fatal("fixture task is not a wave member")
	}
	return normalizeLandingAudit(wave.Data["landings"])
}

// The ref moved but the wave's landing audit was never written: the resume
// recovers the audit row from this task's landing receipt, then closes.
func TestReviewPassRecoversMissingLandingAuditAfterStop(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	t.Cleanup(func() { injectReviewPassCrash = nil })
	env.stopAfterLand(t)
	wave, _ := reviewPassWave(env.vault, env.task(t))
	data, body, err := parseFrontmatterMustRead(wave.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	delete(data, "landings")
	if _, err := saveV7DocumentCAS(wave.AbsolutePath, data, body, v7FrontmatterOrder["wave"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
	env.passHandler(t)
	if status := stringField(env.task(t).Data, "status"); status != "done" {
		t.Fatalf("resume did not close: %q (run %#v)", status, env.latestRun(t))
	}
	found := false
	for _, row := range env.waveLandingRows(t) {
		if stringField(row, "task") == "APP-T-0001" && strings.EqualFold(stringField(row, "source_sha"), env.source) && stringField(row, "receipt_fingerprint") != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("resume did not recover the landing audit row: %#v", env.waveLandingRows(t))
	}
}

// Without this task's landing receipt, a commit already on the integration
// branch is not proof of this review's landing: hold for the owner.
func TestReviewPassHoldsWithoutLandingReceiptAfterStop(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	t.Cleanup(func() { injectReviewPassCrash = nil })
	env.stopAfterLand(t)
	if err := os.RemoveAll(filepath.Join(DefaultStateRoot(), "landing-cache")); err != nil {
		t.Fatal(err)
	}
	env.passHandler(t)
	if status := stringField(env.task(t).Data, "status"); status != "review" {
		t.Fatalf("a landing without a receipt must not close, got %q", status)
	}
	if run := env.latestRun(t); run.ReasonCode != string(RunFailureLandingFailed) || !strings.Contains(run.LastError, "no landing receipt") {
		t.Fatalf("missing receipt must hold with a reason: %#v", run)
	}
}

// The pass went stale while the handler was stopped: the resume must not close.
func TestReviewPassRevalidatesStaleStateAfterStop(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	t.Cleanup(func() { injectReviewPassCrash = nil })
	env.stopAfterLand(t)
	if err := writeText(filepath.Join(env.worktree, "reviewed.txt"), "edited after the stop\n"); err != nil {
		t.Fatal(err)
	}
	env.passHandler(t)
	if status := stringField(env.task(t).Data, "status"); status != "review" {
		t.Fatalf("a stale pass closed after the stop: %q", status)
	}
	if run := env.latestRun(t); run.ReasonCode != string(RunFailureLandingFailed) || !strings.Contains(run.LastError, "stale") {
		t.Fatalf("stale pass after the stop must hold with a reason: %#v", run)
	}
}
