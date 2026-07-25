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
				transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
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
		note := assertCompletionTerminalProjection(t, vault, result)
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

	t.Run("typed review summary is rendered as one escaped verification cell", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		result.Summary = "objective \\| pass\n## Injected heading\r\n| forged | row |"
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		task := assertCompletionTerminalProjection(t, vault, result)
		if strings.Count(task.Body, "## Verification") != 1 ||
			strings.Contains(task.Body, "\n## Injected heading") ||
			!strings.Contains(task.Body, `objective \\\| pass ## Injected heading \| forged \| row \|`) {
			t.Fatalf("adversarial review summary escaped the canonical table: %s", task.Body)
		}
		matches := 0
		for _, row := range parseV7VerificationRows(task.Body) {
			if strings.Contains(row.Notes, "[tusker-review-result:"+result.ResultRevision+"]") {
				matches++
				if row.Notes != "[tusker-review-result:"+result.ResultRevision+"] objective \\| pass ## Injected heading | forged | row |" {
					t.Fatalf("rendered review notes changed semantic content: %q", row.Notes)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("review verification row count=%d, want 1", matches)
		}
	})

	t.Run("ref intent replay accepts authenticated same-wave descendant", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 2, "true")
		sourceZ := commitLandBranch(t, repo, "source/APP-T-0002-z", "integration/W-0001", map[string]string{"z-reviewed.txt": "z\n"})
		sourceA := commitLandBranch(t, repo, "source/APP-T-0001-a", "integration/W-0001", map[string]string{"a-reviewed.txt": "a\n"})
		for id, source := range map[string]string{"APP-T-0001": sourceA, "APP-T-0002": sourceZ} {
			recordCompletionTestProof(t, vault, id)
			setAutomationV7TaskFields(t, vault, id, map[string]any{
				"status": "review", "readiness": "waiting_on_review",
				"source_sha": source, "work_revision": 1,
			})
		}
		armScheduledPromotionWaveForTest(t, vault, "W-0001")
		project := newRegisteredProject(repo, vault)
		stateRoot := filepath.Join(t.TempDir(), "state")
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		daemon, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		zResult := completionResultForReviewedTask(t, vault, project, "APP-T-0002", "review-z", "Z passed")
		aResult := completionResultForReviewedTask(t, vault, project, "APP-T-0001", "review-a", "A passed")
		for _, result := range []ReviewResult{zResult, aResult} {
			if _, err := daemon.store.SaveReviewResult(result); err != nil {
				t.Fatal(err)
			}
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		crashed := false
		completionReactorCrashHook = func(point string, transaction *completionTransaction) error {
			if point == "ref_commit" && transaction.TaskID == zResult.TaskID && !crashed {
				crashed = true
				return errors.New("injected Z crash after integration CAS")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reactToReviewResult(project, wf, zResult, completionReactorModeAuthoritative); err == nil {
			t.Fatal("expected Z ref-commit crash")
		}
		zTransaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, zResult.TaskID, zResult.ResultRevision)
		if err != nil || zTransaction == nil || zTransaction.Phase != completionPhaseRefIntent {
			t.Fatalf("Z did not retain ref intent after CAS crash: transaction=%#v err=%v", zTransaction, err)
		}
		crashedTip, err := gitOutputTrim(repo, "rev-parse", zTransaction.IntegrationRef)
		if err != nil || crashedTip != zTransaction.StagedSHA {
			t.Fatalf("Z CAS was not observable before restart: tip=%s transaction=%#v err=%v", crashedTip, zTransaction, err)
		}
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
		aTransaction, err := restarted.store.CompletionTransactionForResult(project.ProjectID, aResult.TaskID, aResult.ResultRevision)
		if err != nil || aTransaction == nil || aTransaction.Phase != completionPhaseTerminal {
			t.Fatalf("lexically earlier A did not terminalize: transaction=%#v err=%v", aTransaction, err)
		}
		zTransaction, err = restarted.store.CompletionTransactionForResult(project.ProjectID, zResult.TaskID, zResult.ResultRevision)
		if err != nil || zTransaction == nil || zTransaction.Phase != completionPhaseTerminal {
			t.Fatalf("descended Z replay did not terminalize: transaction=%#v err=%v", zTransaction, err)
		}
		if aTransaction.IntegrationBase != zTransaction.StagedSHA {
			t.Fatalf("A did not stage from Z's committed tip: A base=%s Z staged=%s", aTransaction.IntegrationBase, zTransaction.StagedSHA)
		}
		integrated, err := gitOutputTrim(repo, "rev-parse", aTransaction.IntegrationRef)
		if err != nil || integrated != aTransaction.StagedSHA ||
			!gitMergeBaseAncestor(repo, zTransaction.StagedSHA, integrated) {
			t.Fatalf("A did not advance integration as an authenticated Z descendant: tip=%s err=%v", integrated, err)
		}
		for _, result := range []ReviewResult{aResult, zResult} {
			task, err := resolveV7Note(vault, result.TaskID, "task")
			if err != nil {
				t.Fatal(err)
			}
			if stringField(task.Data, "status") != "done" ||
				strings.Count(task.Body, "[tusker-review-result:"+result.ResultRevision+"]") != 1 ||
				generatedReviewerFindingContent(task.Body) != "" {
				t.Fatalf("%s did not project exactly one canonical pass: %#v", result.TaskID, task.Data)
			}
		}
		wave, err := resolveV7Note(vault, "W-0001", "wave")
		if err != nil {
			t.Fatal(err)
		}
		passCounts, failureCounts := map[string]int{}, map[string]int{}
		for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
			switch stringField(row, "gate_result") {
			case "pass":
				passCounts[stringField(row, "task")]++
			case "fail":
				failureCounts[stringField(row, "task")]++
			}
		}
		for _, result := range []ReviewResult{aResult, zResult} {
			if passCounts[result.TaskID] != 1 || failureCounts[result.TaskID] != 0 {
				t.Fatalf("%s audit pass/fail=%d/%d, want 1/0", result.TaskID, passCounts[result.TaskID], failureCounts[result.TaskID])
			}
		}
		beforeReplay := integrated
		if err := restarted.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		if afterReplay, _ := gitOutputTrim(repo, "rev-parse", aTransaction.IntegrationRef); afterReplay != beforeReplay {
			t.Fatalf("terminal replay moved descendant integration: before=%s after=%s", beforeReplay, afterReplay)
		}
	})

	t.Run("proof-green soft dependency unblocks execution but not close", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 2, "true")
		for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
			recordCompletionTestProof(t, vault, id)
		}
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "work_revision": 1,
		})
		commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")
		source := commitLandBranch(t, repo, "source/APP-T-0002-soft-dependency", "integration/W-0001", map[string]string{"dependent-reviewed.txt": "reviewed\n"})
		setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "work_revision": 1,
			"source_sha": source, "dependencies": []string{"APP-T-0001:soft"},
		})
		armScheduledPromotionWaveForTest(t, vault, "W-0001")

		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		dependent := idx.Tasks["APP-T-0002"]
		if blocker, blocked := v7BlockingDependencyForReadiness(dependent, idx); blocked {
			t.Fatalf("proof-green soft dependency should unblock execution readiness: %#v", blocker)
		}
		if blocker, blocked := v7UnclosedDependency(dependent, idx); !blocked || blocker.ID != "APP-T-0001" {
			t.Fatalf("soft dependency must remain a close blocker until done: blocker=%#v blocked=%v", blocker, blocked)
		}

		project := newRegisteredProject(repo, vault)
		stateRoot := filepath.Join(t.TempDir(), "state")
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		daemon, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer daemon.Close()
		result := completionResultForReviewedTask(t, vault, project, "APP-T-0002", "review-soft-dependent", "dependent proof passed")
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		before, err := gitOutputTrim(repo, "rev-parse", "refs/heads/integration/W-0001")
		if err != nil {
			t.Fatal(err)
		}
		err = daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}})
		if err == nil || !strings.Contains(err.Error(), "unfinished dependency APP-T-0001") {
			t.Fatalf("completion close did not enforce canonical dependency semantics: %v", err)
		}
		assertCompletionPolicyRefusedWithoutCAS(t, vault, project, daemon, result, before)
		if err := closeV7Cmd(Args{
			"vault": vault, "quiet": "true", "local": "true",
			"id": "APP-T-0001", "by": "reviewer:agent",
		}); err != nil {
			t.Fatal(err)
		}
		commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0001")
		armScheduledPromotionWaveForTest(t, vault, "W-0001")
		afterDependency, err := gitOutputTrim(repo, "rev-parse", "refs/heads/integration/W-0001")
		if err != nil {
			t.Fatal(err)
		}
		err = daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}})
		if err == nil || !strings.Contains(err.Error(), "close preflight task revision drifted") {
			t.Fatalf("dependency reconciliation did not invalidate the old typed review: %v", err)
		}
		assertCompletionPolicyRefusedWithoutCAS(t, vault, project, daemon, result, afterDependency)

		refreshed := completionResultForReviewedTask(t, vault, project, "APP-T-0002", "review-soft-dependent-refreshed", "dependent proof refreshed after dependency closure")
		if refreshed.TaskStateRev == result.TaskStateRev || refreshed.ResultRevision == result.ResultRevision {
			t.Fatalf("refreshed review did not bind the reconciled task projection: old=%#v refreshed=%#v", result, refreshed)
		}
		if _, err := daemon.store.SaveReviewResult(refreshed); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reactToReviewResult(project, wf, refreshed, completionReactorModeAuthoritative); err != nil {
			t.Fatalf("freshly reviewed dependent did not close after its integrated dependency became done: %v", err)
		}
		assertCompletionTerminalProjection(t, vault, refreshed)
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
		recordCompletionTestProof(t, vault, "APP-T-0001")
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review",
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
			CreatedAt: "2026-07-25T10:00:00Z",
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
		recordCompletionTestProof(t, vault, "APP-T-0001")
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review",
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
			CreatedAt: "2026-07-25T10:00:00Z",
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
		canonical := assertCompletionTerminalProjection(t, vault, result)
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
				if transaction.StagedTaskBlob == "" || transaction.StagedTaskMode != "100644" {
					t.Fatalf("%s lost staged task entry attestation: %#v", point, transaction)
				}
				closedEvents, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", result.TaskID+"--*--close-*.json"))
				if err != nil || len(closedEvents) != 1 {
					t.Fatalf("%s closed-event count=%d, want 1: paths=%#v err=%v", point, len(closedEvents), closedEvents, err)
				}
			})
		}
	})

	t.Run("ref commit crash ignores post-CAS disarm material and gate drift", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		crashed := false
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "ref_commit" && !crashed {
				crashed = true
				return errors.New("injected ref-commit crash before phase persistence")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected ref-commit crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseRefIntent {
			t.Fatalf("ref-commit crash did not retain intent: transaction=%#v err=%v", transaction, err)
		}
		if transaction.WaveAuthorityKind != "armed" || transaction.WaveAuthorizationFP == "" ||
			transaction.WaveAuthorizationFP != transaction.WaveMaterialFP {
			t.Fatalf("ref-commit transaction did not freeze exact wave authority: %#v", transaction)
		}
		integrated, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil || integrated != transaction.StagedSHA {
			t.Fatalf("ref-commit crash did not leave staged integration observable: tip=%s err=%v", integrated, err)
		}

		waveBeforeDrift, err := resolveV7Note(vault, transaction.WaveID, "wave")
		if err != nil {
			t.Fatal(err)
		}
		waveData, waveBody, err := parseFrontmatterMustRead(waveBeforeDrift.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		waveData["authorization"] = "disarmed"
		delete(waveData, "authorization_fingerprint")
		waveData["members"] = []string{}
		if _, err := saveV7DocumentCAS(waveBeforeDrift.AbsolutePath, waveData, waveBody, v7FrontmatterOrder["wave"], stringField(waveData, "state_rev")); err != nil {
			t.Fatal(err)
		}

		gateID := "APP-G-9999"
		gateBody := "# " + gateID + " · post-CAS drift\n"
		gateData := map[string]any{
			"schema": "tusker.gate/v1", "kind": "gate", "id": gateID, "project": "app",
			"title": "Post-CAS drift", "gate_kind": "verification", "status": "open",
			"owner": "reviewer", "priority": "p2", "blocking": false,
			"blocks": []string{result.TaskID}, "covers": []string{"A1"},
			"action": "Recheck after the immutable result.", "verification": "Post-CAS gate exists.",
			"created_at": "2026-07-25T12:00:00Z", "created_by": "agent:test",
			"updated_at": "2026-07-25T12:00:00Z", "updated_by": "agent:test",
		}
		gateData["state_rev"] = v7StateRev(gateData, gateBody)
		gateRaw, err := serializeDocument(gateData, gateBody, v7FrontmatterOrder["gate"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(vault, "work", "gates", gateID+".md"), gateRaw); err != nil {
			t.Fatal(err)
		}
		reviewed, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(reviewed.Data, "state_rev") != result.TaskStateRev {
			t.Fatal("gate drift fixture unexpectedly mutated the canonical reviewed task")
		}
		_, driftedGates, err := reviewObjectiveSnapshots(vault, reviewed)
		if err != nil {
			t.Fatal(err)
		}
		if driftedGates == result.GateFingerprint {
			t.Fatal("gate drift fixture did not invalidate the frozen fingerprint")
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
			t.Fatalf("post-CAS gate drift replay failed: %v", err)
		}
		if err := restarted.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatalf("post-CAS terminal replay failed: %v", err)
		}
		transaction, err = restarted.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal || transaction.Failure != "" {
			t.Fatalf("post-CAS gate drift did not terminalize as pass: transaction=%#v err=%v", transaction, err)
		}
		canonical := assertCompletionTerminalProjection(t, vault, result)
		if stringField(canonical.Data, "status") != "done" ||
			!strings.Contains(canonical.Body, "[tusker-review-result:"+result.ResultRevision+"]") ||
			generatedReviewerFindingContent(canonical.Body) != "" {
			t.Fatalf("post-CAS drift created integration/canonical split brain: %#v", canonical.Data)
		}
		wave, err := resolveV7Note(vault, transaction.WaveID, "wave")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(wave.Data, "authorization") != "disarmed" ||
			containsString(normalizeList(wave.Data["members"]), result.TaskID) {
			t.Fatalf("post-CAS replay rewrote human wave drift instead of using frozen authority: %#v", wave.Data)
		}
		passes, failures := 0, 0
		for _, row := range normalizeLandingAudit(wave.Data["landings"]) {
			if stringField(row, "task") != result.TaskID {
				continue
			}
			if stringField(row, "gate_result") == "pass" {
				passes++
			}
			if stringField(row, "gate_result") == "fail" {
				failures++
			}
		}
		if passes != 1 || failures != 0 {
			t.Fatalf("post-CAS drift audit pass/fail=%d/%d, want 1/0", passes, failures)
		}
	})

	t.Run("missing post-CAS wave is a repair conflict without handback", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "ref_commit" {
				return errors.New("injected ref-commit crash")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected ref-commit crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseRefIntent {
			t.Fatalf("ref-commit crash did not retain intent: transaction=%#v err=%v", transaction, err)
		}
		wave, err := resolveV7Note(vault, transaction.WaveID, "wave")
		if err != nil {
			t.Fatal(err)
		}
		missingPath := wave.AbsolutePath + ".missing"
		if err := os.Rename(wave.AbsolutePath, missingPath); err != nil {
			t.Fatal(err)
		}
		invalidateCachedNote(wave.AbsolutePath)
		t.Cleanup(func() {
			if fileExists(missingPath) {
				_ = os.Rename(missingPath, wave.AbsolutePath)
				invalidateCachedNote(wave.AbsolutePath)
			}
		})
		completionReactorCrashHook = nil
		replayErr := daemon.reconcileReviewCompletion(project, wf)
		var typed *TuskerError
		if !errors.As(replayErr, &typed) || typed.Code != completionRepairRequiredError {
			t.Fatalf("missing committed wave error=%v, want %s", replayErr, completionRepairRequiredError)
		}
		transaction, err = daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseCanonicalDone || transaction.Failure != "" {
			t.Fatalf("repair conflict rewrote committed pass as failure: transaction=%#v err=%v", transaction, err)
		}
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if stringField(task.Data, "status") != "done" || generatedReviewerFindingContent(task.Body) != "" {
			t.Fatalf("repair conflict handed integrated work back: %#v", task.Data)
		}

		if err := os.Rename(missingPath, wave.AbsolutePath); err != nil {
			t.Fatal(err)
		}
		invalidateCachedNote(wave.AbsolutePath)
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatalf("restored frozen wave did not repair completion: %v", err)
		}
		transaction, err = daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("restored frozen wave did not terminalize: transaction=%#v err=%v", transaction, err)
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
		taskRel, err := completionTaskRepoRelativePath(project.RepoRoot, vault, result.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		tamperedEntry, err := completionGitTreeEntryAt(project.RepoRoot, result.ImplementationSHA, taskRel)
		if err != nil {
			t.Fatal(err)
		}
		transaction.StagedSHA, transaction.StagedTaskBlob, transaction.StagedTaskMode =
			result.ImplementationSHA, tamperedEntry.OID, tamperedEntry.Mode
		if err := daemon.store.SaveCompletionTransaction(transaction); err != nil {
			t.Fatal(err)
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

	t.Run("same-blob symlink descendant cannot authenticate committed task entry", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		oldHook := completionReactorCrashHook
		t.Cleanup(func() { completionReactorCrashHook = oldHook })
		crashed := false
		completionReactorCrashHook = func(point string, _ *completionTransaction) error {
			if point == "ref_commit" && !crashed {
				crashed = true
				return errors.New("injected type-change crash after CAS")
			}
			return nil
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
			t.Fatal("expected ref-commit crash")
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseRefIntent ||
			transaction.StagedTaskBlob == "" || transaction.StagedTaskMode != "100644" {
			t.Fatalf("type-change fixture did not retain exact entry attestation: transaction=%#v err=%v", transaction, err)
		}
		taskRel, err := completionTaskRepoRelativePath(project.RepoRoot, vault, result.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		worktree := filepath.Join(t.TempDir(), "same-blob-symlink")
		runGitDir(t, project.RepoRoot, "worktree", "add", "--detach", worktree, transaction.StagedSHA)
		runGitDir(t, worktree, "update-index", "--cacheinfo", "120000", transaction.StagedTaskBlob, taskRel)
		tree := strings.TrimSpace(gitDirOutput(t, worktree, "write-tree"))
		runGitDir(t, project.RepoRoot, "worktree", "remove", "--force", worktree)
		descendant, err := gitOutputTrim(project.RepoRoot, "commit-tree", tree, "-p", transaction.StagedSHA, "-m", "same blob, symlink mode")
		if err != nil {
			t.Fatal(err)
		}
		rawEntry, err := gitOutputTrim(project.RepoRoot, "ls-tree", descendant, "--", taskRel)
		if err != nil || !strings.HasPrefix(rawEntry, "120000 blob "+transaction.StagedTaskBlob) {
			t.Fatalf("type-change fixture is not a same-blob symlink: %q err=%v", rawEntry, err)
		}
		if err := updateGitRef(project.RepoRoot, transaction.IntegrationRef, descendant, transaction.StagedSHA); err != nil {
			t.Fatal(err)
		}
		completionReactorCrashHook = nil
		replayErr := daemon.reconcileReviewCompletion(project, wf)
		if replayErr == nil || errorToIssue(replayErr).Code != "CAS_CONFLICT" {
			t.Fatalf("same-blob symlink descendant authenticated: %v", replayErr)
		}
		replayed, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || replayed == nil || replayed.Phase != completionPhaseRefIntent || replayed.Failure != "" {
			t.Fatalf("type-change rejection rewrote transaction: transaction=%#v err=%v", replayed, err)
		}
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil || stringField(task.Data, "status") != "review" {
			t.Fatalf("type-change rejection projected canonical close: task=%#v err=%v", task.Data, err)
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
		transaction.StagedSHA, transaction.StagedTaskBlob, transaction.StagedTaskMode =
			expected.SHA, expected.TaskBlob, expected.TaskMode
		if err := daemon.store.SaveCompletionTransaction(transaction); err != nil {
			t.Fatal(err)
		}
		worktree := filepath.Join(t.TempDir(), "forged-staging")
		runGitDir(t, project.RepoRoot, "worktree", "add", "--detach", worktree, expected.SHA)
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
		parentFields := strings.Fields(parents)
		if err != nil || len(parentFields) != 3 ||
			parentFields[1] != transaction.IntegrationBase || parentFields[2] != result.ImplementationSHA {
			t.Fatalf("forged test commit lost exact parent shape: %q err=%v", parents, err)
		}
		taskRel, err := completionTaskRepoRelativePath(project.RepoRoot, vault, result.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		forgedTask, err := gitOutputTrim(project.RepoRoot, "show", forged+":"+taskRel)
		if err != nil || !strings.Contains(forgedTask, "[tusker-review-result:"+result.ResultRevision+"]") {
			t.Fatalf("forged test commit lost reviewed task projection: err=%v", err)
		}
		if smuggled, err := gitOutputTrim(project.RepoRoot, "show", forged+":smuggled.txt"); err != nil || smuggled != "not reviewed" {
			t.Fatalf("forged test commit does not contain extra tree content: content=%q err=%v", smuggled, err)
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

	t.Run("completion candidate cannot rewind another done task", func(t *testing.T) {
		repo, vault := newLandTestRepo(t, 2, "true")
		recordCompletionTestProof(t, vault, "APP-T-0002")
		setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "work_revision": 1,
		})
		if err := closeV7Cmd(Args{
			"vault": vault, "quiet": "true", "local": "true",
			"id": "APP-T-0002", "by": "reviewer:agent",
		}); err != nil {
			t.Fatal(err)
		}
		commitCanonicalTaskStateToIntegration(t, repo, vault, "APP-T-0002")
		doneTask, err := resolveV7Note(vault, "APP-T-0002", "task")
		if err != nil {
			t.Fatal(err)
		}
		staleData := cloneNoteData(doneTask.Data)
		staleData["status"], staleData["readiness"] = "review", "waiting_on_review"
		staleData["next_owner"], staleData["next_source"] = "reviewer:agent", "status"
		staleData["next_ref"], staleData["next_action"] = "review", "Review the task."
		for _, field := range []string{"accepted_by", "accepted_at", "closed_at", "close_authority"} {
			delete(staleData, field)
		}
		staleData["state_rev"] = v7StateRev(staleData, doneTask.Body)
		staleRaw, err := serializeDocument(staleData, doneTask.Body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		source := commitLandBranch(t, repo, "source/APP-T-0001-rewinds-done", "integration/W-0001", map[string]string{
			".tusker/work/tasks/APP-T-0002.md": staleRaw,
			"reviewed.txt":                     "exact\n",
		})
		recordCompletionTestProof(t, vault, "APP-T-0001")
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review",
			"source_sha": source, "work_revision": 1,
		})
		armScheduledPromotionWaveForTest(t, vault, "W-0001")

		project := newRegisteredProject(repo, vault)
		stateRoot := filepath.Join(t.TempDir(), "state")
		t.Setenv("TUSKER_STATE_ROOT", stateRoot)
		daemon, err := NewDaemon(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer daemon.Close()
		result := completionResultForReviewedTask(t, vault, project, "APP-T-0001", "review-rewind", "review passed")
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		before, err := gitOutputTrim(repo, "rev-parse", "refs/heads/integration/W-0001")
		if err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal ||
			!strings.Contains(transaction.Failure, "terminal task state rewind refused for APP-T-0002") {
			t.Fatalf("done-task rewind was not rejected by landing safeguards: transaction=%#v err=%v", transaction, err)
		}
		after, err := gitOutputTrim(repo, "rev-parse", "refs/heads/integration/W-0001")
		if err != nil || after != before {
			t.Fatalf("done-task rewind moved integration: before=%s after=%s err=%v", before, after, err)
		}
		integratedB, ok, err := v7GitFrontmatterAtRef(repo, "integration/W-0001", ".tusker/work/tasks/APP-T-0002.md")
		if err != nil || !ok || stringField(integratedB, "status") != "done" {
			t.Fatalf("done task was rewound in integration: task=%#v ok=%v err=%v", integratedB, ok, err)
		}
	})

	t.Run("staging commit plumbing bypasses mutating pre-commit hook", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		hooksDir := filepath.Join(t.TempDir(), "hooks")
		if err := ensureDir(hooksDir); err != nil {
			t.Fatal(err)
		}
		hookMarker := filepath.Join(t.TempDir(), "pre-commit-invoked")
		hook := "#!/bin/sh\n" +
			"printf invoked > " + shellSingleQuote(hookMarker) + "\n" +
			"printf smuggled > smuggled.txt\n" +
			"git add -- smuggled.txt\n"
		hookPath := filepath.Join(hooksDir, "pre-commit")
		if err := writeText(hookPath, hook); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(hookPath, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := gitCombined(project.RepoRoot, "config", "core.hooksPath", hooksDir); err != nil {
			t.Fatal(err)
		}
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("hook-free completion did not terminalize: transaction=%#v err=%v", transaction, err)
		}
		if fileExists(hookMarker) {
			t.Fatal("completion staging invoked the repository pre-commit hook")
		}
		integrated, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			t.Fatal(err)
		}
		for label, commit := range map[string]string{"staging": transaction.StagedSHA, "integration": integrated} {
			if _, err := gitCombined(project.RepoRoot, "cat-file", "-e", commit+":smuggled.txt"); err == nil {
				t.Fatalf("%s tree contains pre-commit hook smuggled content", label)
			}
		}
		canonical, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil || stringField(canonical.Data, "status") != "done" {
			t.Fatalf("hook-free completion lost canonical done projection: task=%#v err=%v", canonical.Data, err)
		}
		assertCompletionTerminalProjection(t, vault, result)
	})

	t.Run("staging task blob bypasses mutating clean filter", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		filterDir := t.TempDir()
		filterMarker := filepath.Join(filterDir, "clean-filter-invoked")
		filterPath := filepath.Join(filterDir, "mutating-clean-filter")
		filter := "#!/bin/sh\n" +
			"printf invoked > " + shellSingleQuote(filterMarker) + "\n" +
			"cat\n" +
			"printf '\\nclean-filter-smuggled\\n'\n"
		if err := writeText(filterPath, filter); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filterPath, 0o755); err != nil {
			t.Fatal(err)
		}
		attributesPath := filepath.Join(filterDir, "attributes")
		if err := writeText(attributesPath, "*.md filter=completion-mutator\n"); err != nil {
			t.Fatal(err)
		}
		for key, value := range map[string]string{
			"core.attributesFile":                attributesPath,
			"filter.completion-mutator.clean":    shellSingleQuote(filterPath),
			"filter.completion-mutator.smudge":   "cat",
			"filter.completion-mutator.required": "true",
		} {
			if _, err := gitCombined(project.RepoRoot, "config", key, value); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal ||
			transaction.StagedTaskBlob == "" || transaction.StagedTaskMode != "100644" {
			t.Fatalf("filter-free completion did not terminalize: transaction=%#v err=%v", transaction, err)
		}
		// Merge bookkeeping may consult the configured clean filter while
		// comparing paths. Reset its marker here; the invariant under test is
		// that final task staging installs the raw blob into both trees.
		if err := os.Remove(filterMarker); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		taskRel, err := completionTaskRepoRelativePath(project.RepoRoot, vault, result.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		integrated, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			t.Fatal(err)
		}
		var generatedRaw string
		for _, committed := range []struct {
			label    string
			revision string
		}{
			{label: "staging", revision: transaction.StagedSHA},
			{label: "integration", revision: integrated},
		} {
			entry, err := completionGitTreeEntryAt(project.RepoRoot, committed.revision, taskRel)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := gitCombined(project.RepoRoot, "cat-file", "blob", entry.OID)
			if err != nil {
				t.Fatal(err)
			}
			if generatedRaw == "" {
				generatedRaw = raw
			}
			if entry.OID != transaction.StagedTaskBlob || entry.Mode != transaction.StagedTaskMode ||
				entry.Type != "blob" || raw != generatedRaw || strings.Contains(raw, "clean-filter-smuggled") {
				t.Fatalf("%s commit tree does not retain the transaction's exact generated task entry: entry=%#v want=%s/%s", committed.label, entry, transaction.StagedTaskMode, transaction.StagedTaskBlob)
			}
		}
		rawBlob, err := gitCommandInput(project.RepoRoot, generatedRaw, "hash-object", "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(rawBlob) != transaction.StagedTaskBlob {
			t.Fatalf("committed task bytes hash to %s, want generated blob %s", strings.TrimSpace(rawBlob), transaction.StagedTaskBlob)
		}
		filteredBlob, err := gitCommandInput(project.RepoRoot, generatedRaw,
			"hash-object", "--filters", "--path="+taskRel, "--stdin")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(filteredBlob) == transaction.StagedTaskBlob || !fileExists(filterMarker) {
			t.Fatal("clean-filter control did not mutate the generated task blob")
		}
		assertCompletionTerminalProjection(t, vault, result)
	})

	t.Run("legacy transaction missing frozen authority requires repair on both sides of CAS", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			crash     string
			wantPhase string
			committed bool
		}{
			{name: "pre-CAS", crash: "gate", wantPhase: completionPhaseStaged},
			{name: "post-CAS", crash: "ref_commit", wantPhase: completionPhaseRefIntent, committed: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, true)
				defer daemon.Close()
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				oldHook := completionReactorCrashHook
				t.Cleanup(func() { completionReactorCrashHook = oldHook })
				crashed := false
				completionReactorCrashHook = func(point string, _ *completionTransaction) error {
					if point == tc.crash && !crashed {
						crashed = true
						return errors.New("injected legacy transaction crash")
					}
					return nil
				}
				wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
				if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
					t.Fatal("expected injected crash")
				}
				transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || transaction == nil || transaction.Phase != tc.wantPhase {
					t.Fatalf("crash fixture phase=%#v err=%v, want %s", transaction, err, tc.wantPhase)
				}
				transaction.WaveAuthorityKind = ""
				transaction.WaveAuthorizationFP = ""
				transaction.WaveMaterialFP = ""
				if err := daemon.store.SaveCompletionTransaction(transaction); err != nil {
					t.Fatal(err)
				}
				completionReactorCrashHook = nil
				replayErr := daemon.reconcileReviewCompletion(project, wf)
				var typed *TuskerError
				if !errors.As(replayErr, &typed) || typed.Code != completionRepairRequiredError {
					t.Fatalf("legacy replay error=%v, want %s", replayErr, completionRepairRequiredError)
				}
				replayed, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || replayed == nil || replayed.Phase != tc.wantPhase || replayed.Failure != "" {
					t.Fatalf("legacy replay mutated transaction: transaction=%#v err=%v", replayed, err)
				}
				task, err := resolveV7Note(vault, result.TaskID, "task")
				if err != nil {
					t.Fatal(err)
				}
				if stringField(task.Data, "status") != "review" ||
					strings.Contains(task.Body, "[tusker-review-result:"+result.ResultRevision+"]") ||
					generatedReviewerFindingContent(task.Body) != "" {
					t.Fatalf("legacy replay handed back or closed task: %#v", task.Data)
				}
				tip, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
				if err != nil {
					t.Fatal(err)
				}
				if tc.committed && tip != transaction.StagedSHA {
					t.Fatalf("post-CAS legacy fixture lost integrated SHA: tip=%s staged=%s", tip, transaction.StagedSHA)
				}
				if !tc.committed && tip != transaction.IntegrationBase {
					t.Fatalf("pre-CAS legacy replay moved integration: tip=%s base=%s", tip, transaction.IntegrationBase)
				}
			})
		}
	})

	t.Run("terminal replay authenticates persisted authority before no-op", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
		if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("terminal authority fixture failed: transaction=%#v err=%v", transaction, err)
		}
		taskPath := filepath.Join(vault, "work", "tasks", result.TaskID+".md")
		beforeTask, err := os.ReadFile(taskPath)
		if err != nil {
			t.Fatal(err)
		}
		beforeTip, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			t.Fatal(err)
		}
		transaction.Schema = "tusker.completion-transaction/v2"
		transaction.WaveMaterialFP = ""
		if err := daemon.store.SaveCompletionTransaction(transaction); err != nil {
			t.Fatal(err)
		}
		replayErr := daemon.reconcileReviewCompletion(project, wf)
		var typed *TuskerError
		if !errors.As(replayErr, &typed) || typed.Code != completionRepairRequiredError {
			t.Fatalf("legacy terminal replay error=%v, want %s", replayErr, completionRepairRequiredError)
		}
		afterTask, err := os.ReadFile(taskPath)
		if err != nil {
			t.Fatal(err)
		}
		afterTip, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
		if err != nil {
			t.Fatal(err)
		}
		if string(afterTask) != string(beforeTask) || afterTip != beforeTip {
			t.Fatalf("legacy terminal replay mutated durable state: task_changed=%v tip=%s want=%s", string(afterTask) != string(beforeTask), afterTip, beforeTip)
		}
	})

	t.Run("staged task blob and mode attestations are mandatory through terminal", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			terminal bool
			clear    func(*completionTransaction)
		}{
			{name: "staged missing blob", clear: func(transaction *completionTransaction) { transaction.StagedTaskBlob = "" }},
			{name: "staged missing mode", clear: func(transaction *completionTransaction) { transaction.StagedTaskMode = "" }},
			{name: "terminal missing blob", terminal: true, clear: func(transaction *completionTransaction) { transaction.StagedTaskBlob = "" }},
			{name: "terminal missing mode", terminal: true, clear: func(transaction *completionTransaction) { transaction.StagedTaskMode = "" }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, true)
				defer daemon.Close()
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				oldHook := completionReactorCrashHook
				t.Cleanup(func() { completionReactorCrashHook = oldHook })
				if !tc.terminal {
					completionReactorCrashHook = func(point string, _ *completionTransaction) error {
						if point == "gate" {
							return errors.New("injected attestation crash")
						}
						return nil
					}
				}
				wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
				err := daemon.reconcileReviewCompletion(project, wf)
				if !tc.terminal && err == nil {
					t.Fatal("expected staged attestation crash")
				}
				if tc.terminal && err != nil {
					t.Fatal(err)
				}
				transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				wantPhase := completionPhaseStaged
				if tc.terminal {
					wantPhase = completionPhaseTerminal
				}
				if err != nil || transaction == nil || transaction.Phase != wantPhase ||
					transaction.StagedTaskBlob == "" || transaction.StagedTaskMode != "100644" {
					t.Fatalf("attestation fixture=%#v err=%v, want phase %s", transaction, err, wantPhase)
				}
				tc.clear(transaction)
				if err := daemon.store.SaveCompletionTransaction(transaction); err != nil {
					t.Fatal(err)
				}
				completionReactorCrashHook = nil
				replayErr := daemon.reconcileReviewCompletion(project, wf)
				var typed *TuskerError
				if !errors.As(replayErr, &typed) || typed.Code != completionRepairRequiredError {
					t.Fatalf("missing attestation replay error=%v, want %s", replayErr, completionRepairRequiredError)
				}
				replayed, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || replayed == nil || replayed.Phase != wantPhase {
					t.Fatalf("missing attestation replay mutated phase: transaction=%#v err=%v", replayed, err)
				}
				task, err := resolveV7Note(vault, result.TaskID, "task")
				if err != nil {
					t.Fatal(err)
				}
				wantStatus := "review"
				if tc.terminal {
					wantStatus = "done"
				}
				if stringField(task.Data, "status") != wantStatus {
					t.Fatalf("missing attestation replay changed task status: got=%s want=%s", stringField(task.Data, "status"), wantStatus)
				}
			})
		}
	})

	t.Run("close policy eligibility is enforced before integration CAS", func(t *testing.T) {
		t.Run("legacy human acceptor policy cannot authorize reviewer closure", func(t *testing.T) {
			vault, project, daemon, result := completionReactorFixture(t, true)
			defer daemon.Close()
			appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: human\n")
			if _, err := daemon.store.SaveReviewResult(result); err != nil {
				t.Fatal(err)
			}
			before, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/integration/W-0001")
			if err != nil {
				t.Fatal(err)
			}
			err = daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}})
			if err == nil || errorToIssue(err).Code != errorConfigInvalid || !strings.Contains(err.Error(), "required_acceptor: human") {
				t.Fatalf("custom human acceptor policy was not enforced: %v", err)
			}
			assertCompletionPolicyRefusedWithoutCAS(t, vault, project, daemon, result, before)
		})

		t.Run("required evidence blocks absent accepted evidence", func(t *testing.T) {
			vault, project, daemon, result := completionReactorFixture(t, true)
			defer daemon.Close()
			appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n")
			if _, err := daemon.store.SaveReviewResult(result); err != nil {
				t.Fatal(err)
			}
			before, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/integration/W-0001")
			if err != nil {
				t.Fatal(err)
			}
			err = daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}})
			if err == nil || errorToIssue(err).Code != errorEvidenceGate || !strings.Contains(err.Error(), "automated_test") {
				t.Fatalf("required evidence policy was not enforced: %v", err)
			}
			assertCompletionPolicyRefusedWithoutCAS(t, vault, project, daemon, result, before)
		})

		t.Run("required gate blocks absent gate kind", func(t *testing.T) {
			vault, project, daemon, _ := completionReactorFixture(t, true)
			defer daemon.Close()
			addCompletionTestEvidence(t, vault, "APP-T-0001")
			armScheduledPromotionWaveForTest(t, vault, "W-0001")
			result := completionResultForReviewedTask(t, vault, project, "APP-T-0001", "review-policy-gate-missing", "policy gate check")
			appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n    required_gates: [security]\n")
			if _, err := daemon.store.SaveReviewResult(result); err != nil {
				t.Fatal(err)
			}
			before, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/integration/W-0001")
			if err != nil {
				t.Fatal(err)
			}
			err = daemon.reconcileReviewCompletion(project, Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}})
			if err == nil || errorToIssue(err).Code != errorInvalidTransition || !strings.Contains(err.Error(), "security gate") {
				t.Fatalf("required gate-kind policy was not enforced: %v", err)
			}
			assertCompletionPolicyRefusedWithoutCAS(t, vault, project, daemon, result, before)
		})

		t.Run("accepted evidence and satisfied gate authorize closure", func(t *testing.T) {
			vault, project, daemon, _ := completionReactorFixture(t, true)
			defer daemon.Close()
			addCompletionTestEvidence(t, vault, "APP-T-0001")
			addCompletionTestSecurityGate(t, vault, "APP-T-0001")
			armScheduledPromotionWaveForTest(t, vault, "W-0001")
			result := completionResultForReviewedTask(t, vault, project, "APP-T-0001", "review-policy-satisfied", "policy evidence and gate satisfied")
			appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n    required_gates: [security]\n")
			if _, err := daemon.store.SaveReviewResult(result); err != nil {
				t.Fatal(err)
			}
			wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
			if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
				t.Fatal(err)
			}
			transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
			if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal || transaction.CloseAuthorityFP == "" {
				t.Fatalf("eligible close policy did not freeze and terminalize: transaction=%#v err=%v", transaction, err)
			}
			assertCompletionTerminalProjection(t, vault, result)
		})

		t.Run("policy drift after freeze but before CAS requires repair", func(t *testing.T) {
			vault, project, daemon, result := completionReactorFixture(t, true)
			defer daemon.Close()
			if _, err := daemon.store.SaveReviewResult(result); err != nil {
				t.Fatal(err)
			}
			oldHook := completionReactorCrashHook
			t.Cleanup(func() { completionReactorCrashHook = oldHook })
			crashed := false
			completionReactorCrashHook = func(point string, _ *completionTransaction) error {
				if point == "gate" && !crashed {
					crashed = true
					return errors.New("injected pre-CAS policy crash")
				}
				return nil
			}
			wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
			if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
				t.Fatal("expected gate crash")
			}
			transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
			if err != nil || transaction == nil || transaction.Phase != completionPhaseStaged {
				t.Fatalf("pre-CAS policy fixture did not retain staged phase: transaction=%#v err=%v", transaction, err)
			}
			appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n")
			completionReactorCrashHook = nil
			replayErr := daemon.reconcileReviewCompletion(project, wf)
			var typed *TuskerError
			if !errors.As(replayErr, &typed) || typed.Code != completionRepairRequiredError {
				t.Fatalf("pre-CAS policy drift error=%v, want %s", replayErr, completionRepairRequiredError)
			}
			replayed, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
			if err != nil || replayed == nil || replayed.Phase != completionPhaseStaged || replayed.Failure != "" {
				t.Fatalf("pre-CAS policy drift mutated transaction: transaction=%#v err=%v", replayed, err)
			}
			task, err := resolveV7Note(vault, result.TaskID, "task")
			if err != nil {
				t.Fatal(err)
			}
			if stringField(task.Data, "status") != "review" || generatedReviewerFindingContent(task.Body) != "" {
				t.Fatalf("pre-CAS policy drift handed task back: %#v", task.Data)
			}
			tip, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
			if err != nil || tip != transaction.IntegrationBase {
				t.Fatalf("pre-CAS policy drift moved integration: tip=%s base=%s err=%v", tip, transaction.IntegrationBase, err)
			}
		})
	})

	t.Run("post-CAS replay uses frozen close authority across policy changes", func(t *testing.T) {
		for _, tc := range []struct {
			name            string
			strongBeforeCAS bool
		}{
			{name: "policy strengthens after CAS"},
			{name: "policy weakens after CAS", strongBeforeCAS: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				vault, project, daemon, result := completionReactorFixture(t, true)
				originalConfig, err := os.ReadFile(filepath.Join(project.RepoRoot, "tusker.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if tc.strongBeforeCAS {
					addCompletionTestEvidence(t, vault, result.TaskID)
					addCompletionTestSecurityGate(t, vault, result.TaskID)
					armScheduledPromotionWaveForTest(t, vault, "W-0001")
					result = completionResultForReviewedTask(t, vault, project, result.TaskID, "review-policy-strong", "strong policy satisfied")
					appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n    required_gates: [security]\n")
				}
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				oldHook := completionReactorCrashHook
				t.Cleanup(func() { completionReactorCrashHook = oldHook })
				crashed := false
				completionReactorCrashHook = func(point string, _ *completionTransaction) error {
					if point == "ref_commit" && !crashed {
						crashed = true
						return errors.New("injected policy crash after CAS")
					}
					return nil
				}
				wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
				if err := daemon.reconcileReviewCompletion(project, wf); err == nil {
					t.Fatal("expected ref-commit crash")
				}
				transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || transaction == nil || transaction.Phase != completionPhaseRefIntent || transaction.CloseAuthorityFP == "" {
					t.Fatalf("policy crash did not retain frozen ref intent: transaction=%#v err=%v", transaction, err)
				}
				if tc.strongBeforeCAS {
					if err := writeText(filepath.Join(project.RepoRoot, "tusker.yaml"), string(originalConfig)); err != nil {
						t.Fatal(err)
					}
				} else {
					appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n    required_gates: [security]\n")
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
					t.Fatalf("post-CAS policy replay consulted mutable policy: %v", err)
				}
				transaction, err = restarted.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
				if err != nil || transaction == nil || transaction.Phase != completionPhaseTerminal || transaction.Failure != "" {
					t.Fatalf("post-CAS policy replay did not terminalize frozen authority: transaction=%#v err=%v", transaction, err)
				}
				task := assertCompletionTerminalProjection(t, vault, result)
				if stringField(task.Data, "status") != "done" ||
					stringField(task.Data, "accepted_by") != result.Actor ||
					stringField(task.Data, "accepted_at") != completionResultTimestamp(result) ||
					generatedReviewerFindingContent(task.Body) != "" {
					t.Fatalf("post-CAS policy replay lost authoritative closure: %#v", task.Data)
				}
			})
		}
	})

	t.Run("invalid close audit cannot bypass strengthened policy validation", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, true)
		defer daemon.Close()
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		wf := Workflow{CompletionReactor: completionReactorModeProjection{Effective: string(completionReactorModeAuthoritative)}}
		if err := daemon.reconcileReviewCompletion(project, wf); err != nil {
			t.Fatal(err)
		}
		appendCompletionTestClosePolicy(t, project.RepoRoot, "  low:\n    required_acceptor: reviewer_agent\n    required_evidence: [automated_test]\n")
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		authority := cloneMap(mapField(data, "close_authority"))
		authority["binding_fingerprint"] = "sha256:" + strings.Repeat("0", 64)
		data["close_authority"] = authority
		if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
			t.Fatal(err)
		}
		tampered, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		issues, _ := validateV7Note(tampered, validationContext{VaultPath: vault, RelativePath: tampered.RelativePath}, tampered.RelativePath)
		codes := map[string]bool{}
		for _, current := range issues {
			codes[current.Code] = true
		}
		if !codes["DONE_TASK_CLOSE_AUTHORITY_INVALID"] || !codes["CLOSE_POLICY_EVIDENCE_MISSING"] {
			t.Fatalf("invalid frozen audit bypassed mutable close policy: issues=%#v", issues)
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
	recordCompletionTestProof(t, vault, "APP-T-0001")
	if exactSource {
		sha := commitLandBranch(t, repo, "source/APP-T-0001", "integration/W-0001", map[string]string{"reviewed.txt": "exact\n"})
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "source_sha": sha, "work_revision": 1})
	} else {
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "source_sha": "deadbeef", "work_revision": 1})
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
	result := ReviewResult{Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: "APP-T-0001", TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1, ImplementationSHA: stringField(note.Data, "source_sha"), AttemptID: "review-1", Actor: "reviewer:agent", Runner: "codex", RunnerProfile: "review", Covers: []string{"A1"}, ProofFingerprint: proof, GateFingerprint: gates, Verdict: "pass", Summary: "objective pass", CreatedAt: "2026-07-25T10:00:00Z"}
	result.ResultRevision = reviewResultFingerprint(result)
	return vault, project, daemon, result
}

func completionResultForReviewedTask(t *testing.T, vault string, project RegisteredProject, taskID, attemptID, summary string) ReviewResult {
	t.Helper()
	task, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	proof, gates, err := reviewObjectiveSnapshots(vault, task)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{
		Schema: reviewResultSchema, ProjectID: project.ProjectID, TaskID: taskID,
		TaskStateRev: stringField(task.Data, "state_rev"), WorkRevision: intField(task.Data, "work_revision"),
		ImplementationSHA: firstNonEmpty(stringField(task.Data, "source_sha"), stringField(task.Data, "source_commit")),
		AttemptID:         attemptID, Actor: "reviewer:agent", Runner: "codex", RunnerProfile: "review",
		Covers: []string{"A1"}, ProofFingerprint: proof, GateFingerprint: gates,
		Verdict: "pass", Summary: summary,
		CreatedAt: "2026-07-25T10:00:00Z",
	}
	result.ResultRevision = reviewResultFingerprint(result)
	return result
}

func recordCompletionTestProof(t *testing.T, vault, taskID string) {
	t.Helper()
	rows := strings.Join([]string{
		"A1|go test ./cmd/tusker -run '^TestDeterministicReviewCompletion$' -count=1|pass|Focused completion proof passed.",
		"A1|go test ./cmd/tusker -count=1|pass|Broad command proof passed.",
	}, "\n")
	if err := verifyV7AddCmd(Args{
		"vault": vault, "quiet": "true", "id": taskID, "rows": rows, "by": "agent:test",
	}); err != nil {
		t.Fatal(err)
	}
	report, err := loadV7ProofReport(vault, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "satisfied" || len(report.Missing) != 0 || len(report.ModeMissing) != 0 {
		t.Fatalf("completion fixture proof is not objectively satisfied: %#v", report)
	}
}

func appendCompletionTestClosePolicy(t *testing.T, repoRoot, policy string) {
	t.Helper()
	path := filepath.Join(repoRoot, "tusker.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "\nclose_policy:") {
		t.Fatal("completion test config already has close_policy")
	}
	next := strings.TrimRight(string(raw), "\n") + "\nclose_policy:\n" + policy
	if err := writeText(path, next); err != nil {
		t.Fatal(err)
	}
}

func addCompletionTestEvidence(t *testing.T, vault, taskID string) {
	t.Helper()
	if err := evidenceV7AddCmd(Args{
		"vault": vault, "quiet": "true", "id": taskID,
		"kind": "automated_test", "status": "accepted", "accepted-by": "reviewer:independent",
		"covers": "A1", "external-url": "https://example.test/completion-proof",
		"summary": "Accepted automated completion proof.",
	}); err != nil {
		t.Fatal(err)
	}
}

func addCompletionTestSecurityGate(t *testing.T, vault, taskID string) {
	t.Helper()
	if err := newV7Gate(Args{
		"vault": vault, "quiet": "true", "blocks": taskID, "kind": "security",
		"owner": "reviewer:security", "covers": "A1",
		"action":       "Perform the objective security verification.",
		"verification": "The independent security check passes.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := gateV7Transition(Args{
		"vault": vault, "quiet": "true", "id": "APP-G-0001", "by": "reviewer:security",
		"evidence": "Independent security verification passed.",
	}, "satisfied"); err != nil {
		t.Fatal(err)
	}
}

func assertCompletionPolicyRefusedWithoutCAS(t *testing.T, vault string, project RegisteredProject, daemon *Daemon, result ReviewResult, before string) {
	t.Helper()
	after, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/integration/W-0001")
	if err != nil || after != before {
		t.Fatalf("close-policy refusal moved integration: before=%s after=%s err=%v", before, after, err)
	}
	transaction, err := daemon.store.CompletionTransactionForResult(project.ProjectID, result.TaskID, result.ResultRevision)
	if err != nil || transaction != nil {
		t.Fatalf("close-policy refusal persisted executable authority: transaction=%#v err=%v", transaction, err)
	}
	task, err := resolveV7Note(vault, result.TaskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if stringField(task.Data, "status") != "review" ||
		strings.Contains(task.Body, "[tusker-review-result:"+result.ResultRevision+"]") ||
		generatedReviewerFindingContent(task.Body) != "" {
		t.Fatalf("close-policy refusal handed back or closed task: %#v", task.Data)
	}
}

func assertCompletionTerminalProjection(t *testing.T, vault string, result ReviewResult) Note {
	t.Helper()
	task, err := resolveV7Note(vault, result.TaskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	stamp := completionResultTimestamp(result)
	proofStatus := stringField(task.Data, "proof_status")
	if stringField(task.Data, "status") != "done" ||
		stringField(task.Data, "readiness") != "done" ||
		(proofStatus != "satisfied" && proofStatus != "waived") ||
		stringField(task.Data, "source_sha") != result.ImplementationSHA ||
		stringField(task.Data, "accepted_by") != result.Actor ||
		stringField(task.Data, "accepted_at") != stamp ||
		stringField(task.Data, "closed_at") != stamp ||
		stringField(task.Data, "updated_by") != result.Actor {
		t.Fatalf("completion terminal provenance is incomplete or synthetic: %#v", task.Data)
	}
	if stringField(task.Data, "next_owner") != "none" ||
		stringField(task.Data, "next_source") != "status" ||
		stringField(task.Data, "next_ref") != "" ||
		stringField(task.Data, "next_action") != "" {
		t.Fatalf("completion terminal routing was not cleared: %#v", task.Data)
	}
	report, err := loadV7ProofReport(vault, result.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "satisfied" || len(report.Missing) != 0 || len(report.ModeMissing) != 0 {
		t.Fatalf("completion projection did not preserve accepted proof: %#v", report)
	}
	authority, authenticated, err := authenticatedV7TaskCloseAuthority(task, v7ProjectID(vault))
	if err != nil || !authenticated ||
		authority.TaskID != result.TaskID ||
		authority.ReviewResultRevision != result.ResultRevision ||
		authority.ReviewedTaskStateRev != result.TaskStateRev {
		t.Fatalf("completion close authority is not authenticated: authority=%#v authenticated=%v err=%v", authority, authenticated, err)
	}
	issues, _ := validateV7Note(task, validationContext{VaultPath: vault, RelativePath: task.RelativePath}, task.RelativePath)
	if len(issues) != 0 {
		t.Fatalf("completion projection is not schema-valid: %#v", issues)
	}
	eventIssues, _, _ := validateV7Events(vault)
	if len(eventIssues) != 0 {
		t.Fatalf("completion closed audit event is not schema-valid: %#v", eventIssues)
	}
	eventPaths, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", result.TaskID+"--*--close-*.json"))
	if err != nil || len(eventPaths) != 1 {
		t.Fatalf("completion closed audit count=%d, want 1: paths=%#v err=%v", len(eventPaths), eventPaths, err)
	}
	eventRaw, err := os.ReadFile(eventPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(eventRaw, &event); err != nil {
		t.Fatal(err)
	}
	eventAuthority, ok := v7TaskCloseAuthorityFromAny(mapField(mapField(event, "payload"), "close_authority"))
	if !ok || eventAuthority.BindingFingerprint != authority.BindingFingerprint {
		t.Fatalf("task/event close authority mismatch: task=%#v event=%#v", authority, eventAuthority)
	}
	return task
}
