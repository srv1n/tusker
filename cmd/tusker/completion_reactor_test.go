package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// This matrix stays intentionally small but exercises the authority boundary:
// legacy/disabled do not consume an immutable result, shadow persists only a
// plan, authoritative returns findings without a model, and an exact reviewed
// SHA can be staged and CASed into integration.
func TestDeterministicReviewCompletion(t *testing.T) {
	t.Run("mode compatibility", func(t *testing.T) {
		for _, tc := range []struct {
			mode      completionReactorMode
			wantPhase string
		}{
			{completionReactorModeLegacy, ""},
			{completionReactorModeDisabled, ""},
			{completionReactorModeShadow, completionPhasePlanned},
		} {
			t.Run(string(tc.mode), func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, false)
				defer daemon.Close()
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				if err := daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(tc.mode)}}); err != nil {
					t.Fatal(err)
				}
				note, err := resolveV7Note(vault, result.TaskID, "task")
				if err != nil {
					t.Fatal(err)
				}
				if tc.wantPhase == "" {
					if stringField(note.Data, "status") != "review" {
						t.Fatalf("%s mutated review task", tc.mode)
					}
					return
				}
				wave, _, ok := armedWaveForTask(vault, note)
				if !ok {
					t.Fatal("fixture lost wave")
				}
				base, _ := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/"+v7WaveIntegrationBranch(wave))
				transaction, err := daemon.store.CompletionTransaction(completionTransactionID(project.ProjectID, result, base))
				if err != nil || transaction == nil || transaction.Phase != tc.wantPhase {
					t.Fatalf("transaction=%#v err=%v", transaction, err)
				}
			})
		}
	})

	t.Run("changes requested crash after handback resumes release and audit", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		result.Verdict, result.Blocker, result.Findings = "changes_requested", "", []string{"fix the exact acceptance regression"}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		if err := daemon.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: result.TaskID, ItemID: result.TaskID,
			Runner: "codex", RunnerProfile: "review", Lane: runLaneReview,
			LeaseState: string(LeaseStateRunning), ActiveAttemptID: result.AttemptID,
			AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: result.WorkRevision,
		}); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		crashed := false
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "failure_handback" && !crashed {
				crashed = true
				return errors.New("injected crash after handback")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected injected handback crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseFailureIntent {
			t.Fatalf("crash persisted terminal or lost intent: transaction=%#v err=%v", transaction, err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "rework" {
			t.Fatal("handback side effect did not happen before crash")
		}
		stateRoot := daemon.stateRoot
		if err := daemon.Close(); err != nil {
			t.Fatal(err)
		}
		completionReactorCrashHook = nil
		restarted, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer restarted.Close()
		if err := restarted.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		if err := restarted.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err = restarted.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("replay did not reach terminal last: transaction=%#v err=%v", transaction, err)
		}
		note, err = resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "rework" || !strings.Contains(generatedReviewerFindingContent(note.Body), "fix the exact acceptance regression") {
			t.Fatalf("finding was not durably handed back: %#v", note.Data)
		}
		if strings.Count(generatedReviewerFindingContent(note.Body), "fix the exact acceptance regression") != 1 {
			t.Fatal("replay duplicated reviewer finding")
		}
		run, err := restarted.store.FindRun(result.TaskID)
		if err != nil || run == nil || run.LeaseState != string(LeaseStateReleased) {
			t.Fatalf("review ownership was not released: run=%#v err=%v", run, err)
		}
		wave, err := resolveV7Note(vault, stringField(note.Data, "wave"), "wave")
		if err != nil {
			t.Fatal(err)
		}
		defects := 0
		for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
			if stringField(row, "defect_id") == transaction.ID {
				defects++
			}
		}
		if defects != 1 {
			t.Fatalf("failure audit count=%d, want 1", defects)
		}
	})

	t.Run("blocked crash after audit stays parked until terminal replay", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		result.Verdict, result.Blocker, result.Summary = "blocked", "infrastructure", "CI host unavailable"
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		if err := daemon.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: result.TaskID, ItemID: result.TaskID,
			Runner: "codex", RunnerProfile: "review", Lane: runLaneReview,
			LeaseState: string(LeaseStateRunning), ActiveAttemptID: result.AttemptID,
			AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: result.WorkRevision,
		}); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		crashed := false
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "failure_audit" && !crashed {
				crashed = true
				return errors.New("injected crash after failure audit")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected injected audit crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseFailureReleased {
			t.Fatalf("audit crash advanced terminal: transaction=%#v err=%v", transaction, err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "review" {
			t.Fatalf("blocked result falsely changed task status: %s", stringField(note.Data, "status"))
		}
		run, err := daemon.store.FindRun(result.TaskID)
		if err != nil || run == nil || run.LeaseState != string(LeaseStateParkedNoProgress) {
			t.Fatalf("blocked review was not truthfully parked: run=%#v err=%v", run, err)
		}
		completionReactorCrashHook = nil
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err = daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("blocked replay did not terminalize last: transaction=%#v err=%v", transaction, err)
		}
		if err := daemon.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("pass freezes exact sha and integration CAS", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		wave, _, _ := armedWaveForTask(vault, note)
		integration := "refs/heads/" + v7WaveIntegrationBranch(wave)
		got, err := gitOutputTrim(project.RepoRoot, "rev-parse", integration)
		if err != nil {
			t.Fatal(err)
		}
		if !gitMergeBaseAncestor(project.RepoRoot, result.ImplementationSHA, got) {
			t.Fatalf("integration did not merge exact reviewed sha %s", result.ImplementationSHA)
		}
		data, ok, err := v7GitFrontmatterAtRef(project.RepoRoot, strings.TrimPrefix(integration, "refs/heads/"), filepath.ToSlash(filepath.Join(".tusker", "work", "tasks", result.TaskID+".md")))
		if err != nil || !ok || stringField(data, "status") != "done" {
			t.Fatalf("staged close missing data=%#v err=%v", data, err)
		}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		if replay, _ := gitOutputTrim(project.RepoRoot, "rev-parse", integration); replay != got {
			t.Fatalf("replay moved integration %s -> %s", got, replay)
		}
	})

	t.Run("first completion creates a ref-less integration lane by CAS", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		wave, _, ok := armedWaveForTask(vault, task)
		if !ok {
			t.Fatal("missing fixture wave")
		}
		base, err := gitOutputTrim(project.RepoRoot, "rev-parse", v7WaveIntegrationBranch(wave))
		if err != nil {
			t.Fatal(err)
		}
		waveData, waveBody, err := parseFrontmatterMustRead(wave.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		waveData["integration_base_sha"] = base
		if _, err := saveV7DocumentCAS(wave.AbsolutePath, waveData, waveBody, v7FrontmatterOrder["wave"], stringField(waveData, "state_rev")); err != nil {
			t.Fatal(err)
		}
		if _, err := gitCombined(project.RepoRoot, "update-ref", "-d", "refs/heads/"+v7WaveIntegrationBranch(wave)); err != nil {
			t.Fatal(err)
		}
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		ref := "refs/heads/" + v7WaveIntegrationBranch(wave)
		if !gitRefExists(project.RepoRoot, ref) {
			t.Fatal("reactor did not CAS-create integration ref")
		}
		got, err := gitOutputTrim(project.RepoRoot, "rev-parse", ref)
		if err != nil {
			t.Fatal(err)
		}
		if !gitMergeBaseAncestor(project.RepoRoot, result.ImplementationSHA, got) {
			t.Fatal("ref-less integration omitted reviewed source")
		}
	})
}

func completionReactorFixture(t *testing.T, exactSource bool) (string, RegisteredProject, *Daemon, ReviewResult) {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, "true")
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", state)
	project := newRegisteredProject(repo, vault)
	daemon, err := NewDaemon(state)
	if err != nil {
		t.Fatal(err)
	}
	if exactSource {
		sha := commitLandBranch(t, repo, "source/APP-T-0001", "integration/W-0001", map[string]string{"reviewed.txt": "exact\n"})
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied", "source_sha": sha, "work_revision": 1})
	} else {
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied", "source_sha": "deadbeef", "work_revision": 1})
	}
	armScheduledPromotionWaveForTest(t, vault, "W-0001")
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, note)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: "APP-T-0001", TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1, ImplementationSHA: stringField(note.Data, "source_sha"), AttemptID: "review-1", Actor: "reviewer:agent", Runner: "codex", RunnerProfile: "review", Covers: []string{"A1"}, ProofFingerprint: proof, GateFingerprint: gates, Verdict: "pass", Summary: "objective pass"}
	result.ResultRevision = reviewResultFingerprint(result)
	return vault, project, daemon, result
}
