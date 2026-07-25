package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	t.Run("old handback replay never rewinds a newer review", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		defer daemon.Close()
		result.Verdict, result.Findings = "changes_requested", []string{"old review finding"}
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
				return errors.New("injected old handback crash")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected old handback crash")
		}
		setAutomationV7TaskFields(t, vault, result.TaskID, map[string]any{
			"status": "review", "readiness": "waiting_on_review", "work_revision": 2,
			"source_sha": "new-reviewed-source", "proof_status": "satisfied",
		})
		if err := daemon.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: result.TaskID, ItemID: result.TaskID,
			Runner: "codex", RunnerProfile: "review", Lane: runLaneReview,
			LeaseState: string(LeaseStateRunning), ActiveAttemptID: "review-new",
			AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: 2,
		}); err != nil {
			t.Fatal(err)
		}
		completionReactorCrashHook = nil
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "review" || intField(note.Data, "work_revision") != 2 || stringField(note.Data, "source_sha") != "new-reviewed-source" {
			t.Fatalf("old handback rewound newer review: %#v", note.Data)
		}
		run, err := daemon.store.FindRun(result.TaskID)
		if err != nil || run == nil || run.ActiveAttemptID != "review-new" || run.LeaseState != string(LeaseStateRunning) {
			t.Fatalf("old handback released newer review owner: run=%#v err=%v", run, err)
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("old handback bookkeeping did not converge: transaction=%#v err=%v", transaction, err)
		}
	})

	t.Run("older same-work result cannot suppress later active handback", func(t *testing.T) {
		vault, project, daemon, older := completionReactorFixture(t, false)
		defer daemon.Close()
		older.Verdict, older.Findings, older.CreatedAt = "changes_requested", []string{"obsolete finding"}, "2026-07-25T10:00:00Z"
		older.ResultRevision = reviewResultFingerprint(older)
		later := older
		later.AttemptID = "review-2"
		later.Findings = []string{"current finding"}
		later.CreatedAt = "2026-07-25T11:00:00Z"
		later.ResultRevision = reviewResultFingerprint(later)
		if _, err := daemon.store.SaveReviewResult(older); err != nil {
			t.Fatal(err)
		}
		if _, err := daemon.store.SaveReviewResult(later); err != nil {
			t.Fatal(err)
		}
		for _, attempt := range []RunAttempt{
			{AttemptID: older.AttemptID, ProjectID: project.ProjectID, RecordID: older.TaskID, ItemID: older.TaskID, Runner: "codex", Lane: runLaneReview, WorkRevision: older.WorkRevision, StartedAt: "2026-07-25T09:55:00Z"},
			{AttemptID: later.AttemptID, ProjectID: project.ProjectID, RecordID: later.TaskID, ItemID: later.TaskID, Runner: "codex", Lane: runLaneReview, WorkRevision: later.WorkRevision, StartedAt: "2026-07-25T10:55:00Z"},
		} {
			if err := daemon.store.SaveAttempt(attempt); err != nil {
				t.Fatal(err)
			}
		}
		if err := daemon.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: later.TaskID, ItemID: later.TaskID,
			Runner: "codex", RunnerProfile: "review", Lane: runLaneReview,
			LeaseState: string(LeaseStateRunning), ActiveAttemptID: later.AttemptID,
			AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: later.WorkRevision,
		}); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		task, err := resolveV7Note(vault, later.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		finding := generatedReviewerFindingContent(task.Body)
		if stringField(task.Data, "status") != "rework" || !strings.Contains(finding, "current finding") || strings.Contains(finding, "obsolete finding") {
			t.Fatalf("later handback lost same-work ordering: status=%s finding=%q", stringField(task.Data, "status"), finding)
		}
		laterTransaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, later.TaskID, later.ResultRevision)
		if err != nil || laterTransaction == nil || laterTransaction.Phase != completionPhaseTerminal {
			t.Fatalf("later handback did not terminalize: transaction=%#v err=%v", laterTransaction, err)
		}
	})

	t.Run("pre-marker terminal state finishes failure bookkeeping monotonically", func(t *testing.T) {
		for _, terminalStatus := range []string{"done", "cancelled"} {
			t.Run(terminalStatus, func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, false)
				defer daemon.Close()
				result.Verdict, result.Findings = "changes_requested", []string{"superseded finding"}
				result.ResultRevision = reviewResultFingerprint(result)
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				oldHook := completionReactorCrashHook
				t.Cleanup(func() { completionReactorCrashHook = oldHook })
				completionReactorCrashHook = func(point string, _ *completionTransaction) error {
					if point == "failure_intent" {
						return errors.New("injected pre-marker crash")
					}
					return nil
				}
				wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
				if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
					t.Fatal("expected failure-intent crash")
				}
				setAutomationV7TaskFields(t, vault, result.TaskID, map[string]any{"status": terminalStatus, "readiness": terminalStatus})
				completionReactorCrashHook = nil
				if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
					t.Fatalf("terminal replay poisoned reconciliation: %v", err)
				}
				task, err := resolveV7Note(vault, result.TaskID, "task")
				if err != nil {
					t.Fatal(err)
				}
				if stringField(task.Data, "status") != terminalStatus {
					t.Fatalf("old handback rewound pre-marker terminal state: %#v", task.Data)
				}
				transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
					t.Fatalf("terminal bookkeeping did not converge: transaction=%#v err=%v", transaction, err)
				}
				if strings.Contains(task.Body, completionHandbackMarker(transaction.ID)) {
					t.Fatal("terminal replay wrote a stale handback marker")
				}
			})
		}
	})

	t.Run("partial same revision handback finishes its own status flip", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		defer daemon.Close()
		result.Verdict, result.Findings = "changes_requested", []string{"partial finding"}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "failure_intent" {
				return errors.New("injected crash before handback")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected failure-intent crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil {
			t.Fatalf("missing handback intent: transaction=%#v err=%v", transaction, err)
		}
		taskPath := filepath.Join(vault, "work", "tasks", result.TaskID+".md")
		data, body, err := parseFrontmatterMustRead(taskPath)
		if err != nil {
			t.Fatal(err)
		}
		section := reviewerFindingGeneratedMarker + "\n\n" + completionHandbackMarker(transaction.ID) + "\n\n" + transaction.Failure
		body = upsertGeneratedReviewerFindingSection(body, section)
		if _, err := saveV7DocumentCAS(taskPath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
			t.Fatal(err)
		}
		completionReactorCrashHook = nil
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "rework" {
			t.Fatalf("partial owned handback did not finish: %#v", note.Data)
		}
		transaction, err = daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("partial handback replay did not converge: transaction=%#v err=%v", transaction, err)
		}
	})

	t.Run("old handback fails closed on unrelated same revision drift", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		defer daemon.Close()
		result.Verdict, result.Findings = "changes_requested", []string{"fenced finding"}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "failure_intent" {
				return errors.New("injected crash after failure intent")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected failure-intent crash")
		}
		setAutomationV7TaskFields(t, vault, result.TaskID, map[string]any{"next_action": "unrelated operator edit"})
		completionReactorCrashHook = nil
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil || !strings.Contains(err.Error(), "same-revision drift") {
			t.Fatalf("unrelated same-revision drift was not fenced: %v", err)
		}
		note, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(note.Data, "status") != "review" || generatedReviewerFindingContent(note.Body) != "" {
			t.Fatal("fenced handback mutated unrelated same-revision state")
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
		if stringField(note.Data, "status") != "done" || !strings.Contains(note.Body, "[tusker-review-result:"+result.ResultRevision+"]") {
			t.Fatalf("canonical task was not projected from staged completion: %#v", note.Data)
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil {
			t.Fatalf("missing completion transaction: transaction=%#v err=%v", transaction, err)
		}
		wave, err := resolveV7Note(vault, transaction.WaveID, "wave")
		if err != nil {
			t.Fatal(err)
		}
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

	t.Run("canonical pass unlocks hard successor in another armed wave", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 2, "true")
		waveOne, err := resolveV7Note(vault, "W-0001", "wave")
		if err != nil {
			t.Fatal(err)
		}
		waveData, waveBody, err := parseFrontmatterMustRead(waveOne.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		waveData["members"] = []string{"APP-T-0001"}
		if _, err := saveV7DocumentCAS(waveOne.AbsolutePath, waveData, waveBody, v7FrontmatterOrder["wave"], stringField(waveData, "state_rev")); err != nil {
			t.Fatal(err)
		}
		setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{
			"wave": "", "status": "ready", "readiness": "blocked_by_dependency",
			"dependencies": []string{"APP-T-0001:hard"}, "next_owner": "blocked_dependency",
			"next_source": "dependency", "next_ref": "APP-T-0001",
		})
		if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Dependent batch", "_pos1": "APP-T-0002"}); err != nil {
			t.Fatal(err)
		}
		dependent, err := resolveV7Note(vault, "APP-T-0002", "task")
		if err != nil || stringField(dependent.Data, "wave") != "W-0002" {
			t.Fatalf("dependent was not isolated in wave B: task=%#v err=%v", dependent.Data, err)
		}
		source := commitLandBranch(t, repo, "source/APP-T-0001", "integration/W-0001", map[string]string{"predecessor.txt": "reviewed\n"})
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied",
			"source_sha": source, "work_revision": 1,
		})
		armScheduledPromotionWaveForTest(t, vault, "W-0001")
		armScheduledPromotionWaveForTest(t, vault, "W-0002")

		project := newRegisteredProject(repo, vault)
		stateRoot := filepath.Join(t.TempDir(), "state")
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		daemon, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer daemon.Close()
		reviewed, err := resolveV7Note(vault, "APP-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}
		proof, gates, err := reviewObjectiveSnapshots(vault, reviewed)
		if err != nil {
			t.Fatal(err)
		}
		result := ReviewResult{
			Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: "APP-T-0001",
			TaskStateRev: stringField(reviewed.Data, "state_rev"), WorkRevision: 1,
			ImplementationSHA: source, AttemptID: "review-predecessor", Actor: "reviewer:agent",
			Runner: "codex", RunnerProfile: "review", Covers: []string{"A1"},
			ProofFingerprint: proof, GateFingerprint: gates, Verdict: "pass", Summary: "predecessor objective pass",
		}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		canonical, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil || stringField(canonical.Data, "status") != "done" {
			t.Fatalf("predecessor canonical state did not close: task=%#v err=%v", canonical.Data, err)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		waveTwo := idx.Waves["W-0002"]
		snapshot := buildArmedWaveSnapshot(vault, idx, waveTwo, nil, time.Unix(0, 0).UTC())
		frontierCount := 0
		for _, id := range snapshot.Frontier {
			if id == "APP-T-0002" {
				frontierCount++
			}
		}
		if frontierCount != 1 {
			t.Fatalf("hard successor frontier count=%d, want exactly one: %#v", frontierCount, snapshot)
		}
		claimed := buildArmedWaveSnapshot(vault, idx, waveTwo, map[string]RunStatus{
			"APP-T-0002": {ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", LeaseState: string(LeaseStateRunning)},
		}, time.Unix(0, 0).UTC())
		if containsString(claimed.Frontier, "APP-T-0002") {
			t.Fatalf("claimed cross-wave successor remained dispatchable: %#v", claimed)
		}
	})

	t.Run("standalone review binds singleton before immutable result", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 1, "true")
		clearWaveBackpointer(t, vault, "APP-T-0001")
		setSingletonPromotionMode(t, vault, scheduledPromotionStage)
		source := commitLandBranch(t, repo, "source/APP-T-0001", "integration/W-0001", map[string]string{"standalone.txt": "reviewed\n"})
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied",
			"source_sha": source, "work_revision": 1,
		})
		before, err := resolveV7Note(vault, "APP-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(before.Data, "wave") != "" {
			t.Fatal("fixture is not standalone before review handoff")
		}
		if err := requestV7ReviewAfterHandoff(vault, "APP-T-0001", Args{"vault": vault, "quiet": "true", "by": "agent:test"}); err != nil {
			t.Fatal(err)
		}
		reviewed, err := resolveV7Note(vault, "APP-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(reviewed.Data, "wave") == "" || stringField(reviewed.Data, "state_rev") == stringField(before.Data, "state_rev") {
			t.Fatal("review handoff did not freeze singleton binding before reviewer snapshot")
		}
		wave, ok := completionWaveForReviewedTask(vault, reviewed)
		if !ok || !v7ImplicitDeliveryUnit(wave) {
			t.Fatal("bound standalone task is not an authorized implicit completion unit")
		}
		proof, gates, err := reviewObjectiveSnapshots(vault, reviewed)
		if err != nil {
			t.Fatal(err)
		}
		project := newRegisteredProject(repo, vault)
		stateRoot := filepath.Join(t.TempDir(), "state")
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		daemon, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer daemon.Close()
		result := ReviewResult{
			Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: "APP-T-0001",
			TaskStateRev: stringField(reviewed.Data, "state_rev"), WorkRevision: 1,
			ImplementationSHA: source, AttemptID: "review-standalone", Actor: "reviewer:agent",
			Runner: "codex", RunnerProfile: "review", Covers: []string{"A1"},
			ProofFingerprint: proof, GateFingerprint: gates, Verdict: "pass", Summary: "standalone objective pass",
		}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		integration := "refs/heads/" + v7WaveIntegrationBranch(wave)
		got, err := gitOutputTrim(repo, "rev-parse", integration)
		if err != nil {
			t.Fatal(err)
		}
		if !gitMergeBaseAncestor(repo, source, got) {
			t.Fatal("standalone pass did not merge exact reviewed SHA")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal || transaction.WaveID != stringField(wave.Data, "id") {
			t.Fatalf("standalone completion did not terminalize on its frozen singleton: transaction=%#v err=%v", transaction, err)
		}
		canonical, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(canonical.Data, "status") != "done" ||
			strings.Count(canonical.Body, "[tusker-review-result:"+result.ResultRevision+"]") != 1 ||
			strings.Contains(canonical.Body, completionHandbackMarker(transaction.ID)) {
			t.Fatalf("standalone canonical projection is not exactly one pass: %#v", canonical.Data)
		}
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		projected := v7ProjectedTaskState(vault, canonical, idx)
		if stringField(projected, "readiness") != "done" {
			t.Fatalf("standalone effective state is not done: %#v", projected)
		}
		auditedWave, err := resolveV7Note(vault, transaction.WaveID, "wave")
		if err != nil {
			t.Fatal(err)
		}
		passes, failures := 0, 0
		for _, row := range normalizeLandingAudit(auditedWave.Data["landings"]) {
			if stringField(row, "task") != result.TaskID {
				continue
			}
			switch stringField(row, "gate_result") {
			case "pass":
				passes++
			case "fail":
				failures++
			}
		}
		if passes != 1 || failures != 0 {
			t.Fatalf("standalone landing audit passes=%d failures=%d, want 1/0", passes, failures)
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

	t.Run("phase crash matrix converges on one staged commit", func(t *testing.T) {
		for _, point := range []string{"staging_commit", "staging_ref", "gate", "ref_commit", "canonical_projection", "audit", "wake"} {
			t.Run(point, func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, true)
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				oldHook := completionReactorCrashHook
				t.Cleanup(func() { completionReactorCrashHook = oldHook })
				capturedSHA := ""
				injected := false
				completionReactorCrashHook = func(got string, transaction *completionTransaction) error {
					if got == point && !injected {
						injected = true
						capturedSHA = transaction.StagedSHA
						return errors.New("injected completion phase crash")
					}
					return nil
				}
				wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
				if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
					t.Fatalf("%s did not inject a crash", point)
				}
				transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || transaction == nil || transaction.Phase == completionPhaseTerminal {
					t.Fatalf("%s lost resumable phase: transaction=%#v err=%v", point, transaction, err)
				}
				if capturedSHA == "" {
					t.Fatalf("%s crash did not expose staged candidate", point)
				}
				if point == "staging_commit" {
					if _, err := gitCombined(project.RepoRoot, "config", "user.name", "Changed Identity"); err != nil {
						t.Fatal(err)
					}
					if _, err := gitCombined(project.RepoRoot, "config", "user.email", "changed@example.invalid"); err != nil {
						t.Fatal(err)
					}
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
					t.Fatalf("%s replay failed: %v", point, err)
				}
				transaction, err = restarted.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
					t.Fatalf("%s replay did not terminalize last: transaction=%#v err=%v", point, transaction, err)
				}
				if transaction.StagedSHA != capturedSHA {
					t.Fatalf("%s recreated a different staging commit: first=%s replay=%s", point, capturedSHA, transaction.StagedSHA)
				}
				stagingSHA, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.StagingRef)
				if err != nil || stagingSHA != capturedSHA {
					t.Fatalf("%s staging ref lost candidate: got=%s err=%v", point, stagingSHA, err)
				}
				wave, err := resolveV7Note(vault, transaction.WaveID, "wave")
				if err != nil {
					t.Fatal(err)
				}
				integrated, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/"+v7WaveIntegrationBranch(wave))
				if err != nil || integrated != capturedSHA {
					t.Fatalf("%s integration candidate=%s, want %s err=%v", point, integrated, capturedSHA, err)
				}
				commits, err := gitOutputTrim(project.RepoRoot, "rev-list", "--all", "--grep=Tusker-Completion: "+transaction.ID)
				if err != nil || len(strings.Fields(commits)) != 1 {
					t.Fatalf("%s duplicate completion commits visible: %q err=%v", point, commits, err)
				}
				parents, err := gitOutputTrim(project.RepoRoot, "rev-list", "--parents", "-n", "1", capturedSHA)
				parentFields := strings.Fields(parents)
				if err != nil || len(parentFields) != 3 || parentFields[2] != result.ImplementationSHA {
					t.Fatalf("%s staged parent contract=%q err=%v", point, parents, err)
				}
			})
		}
	})

	t.Run("tampered staging ref is classified without integration movement", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "staging_intent" {
				return errors.New("injected crash after staging intent")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected staging-intent crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseStaging {
			t.Fatalf("missing staging intent: transaction=%#v err=%v", transaction, err)
		}
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		wave, _, ok := armedWaveForTask(vault, task)
		if !ok {
			t.Fatal("missing armed fixture wave")
		}
		integrationRef := "refs/heads/" + v7WaveIntegrationBranch(wave)
		before, err := gitOutputTrim(project.RepoRoot, "rev-parse", integrationRef)
		if err != nil {
			t.Fatal(err)
		}
		if err := updateGitRef(project.RepoRoot, transaction.StagingRef, result.ImplementationSHA, strings.Repeat("0", 40)); err != nil {
			t.Fatal(err)
		}
		completionReactorCrashHook = nil
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err = daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal ||
			!strings.Contains(transaction.Failure, "only parents") {
			t.Fatalf("tampered ref was not classified: transaction=%#v err=%v", transaction, err)
		}
		after, err := gitOutputTrim(project.RepoRoot, "rev-parse", integrationRef)
		if err != nil || after != before {
			t.Fatalf("tampered ref moved integration: before=%s after=%s err=%v", before, after, err)
		}
	})

	t.Run("exact-parent staging ref with extra tree content is refused", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "staging_intent" {
				return errors.New("injected crash before deterministic staging")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected staging-intent crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseStaging {
			t.Fatalf("missing staging intent: transaction=%#v err=%v", transaction, err)
		}
		integrationBefore, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := buildExactReviewCompletionCandidate(vault, project.RepoRoot, transaction.IntegrationBase, result, transaction)
		if err != nil {
			t.Fatal(err)
		}
		worktree := filepath.Join(t.TempDir(), "forged-staging")
		runGitDir(t, project.RepoRoot, "worktree", "add", "--detach", worktree, expected)
		if err := writeText(filepath.Join(worktree, "smuggled.txt"), "not reviewed\n"); err != nil {
			t.Fatal(err)
		}
		runGitDir(t, worktree, "add", "--", "smuggled.txt")
		tree := strings.TrimSpace(gitDirOutput(t, worktree, "write-tree"))
		runGitDir(t, project.RepoRoot, "worktree", "remove", "--force", worktree)
		message := "Complete reviewed task " + result.TaskID + "\n\nTusker-Completion: " + transaction.ID
		forged, err := gitOutputTrim(project.RepoRoot, "commit-tree", tree,
			"-p", transaction.IntegrationBase, "-p", result.ImplementationSHA, "-m", message)
		if err != nil {
			t.Fatal(err)
		}
		parents, err := gitOutputTrim(project.RepoRoot, "rev-list", "--parents", "-n", "1", forged)
		if err != nil || len(strings.Fields(parents)) != 3 {
			t.Fatalf("forged test commit lost exact parent shape: %q err=%v", parents, err)
		}
		if err := updateGitRef(project.RepoRoot, transaction.StagingRef, forged, strings.Repeat("0", 40)); err != nil {
			t.Fatal(err)
		}
		completionReactorCrashHook = nil
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err = daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal ||
			!strings.Contains(transaction.Failure, "deterministic reviewed completion object") {
			t.Fatalf("forged exact-parent tree was not classified: transaction=%#v err=%v", transaction, err)
		}
		integrationAfter, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil || integrationAfter != integrationBefore {
			t.Fatalf("forged exact-parent tree moved integration: before=%s after=%s err=%v", integrationBefore, integrationAfter, err)
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
