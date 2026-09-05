package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestFactoryExecutionControl is intentionally in package main. The public CLI
// does not expose daemon-free entry points for the frontier index, global fair
// scheduler, or completion transaction. Keeping this fixture beside those
// seams lets it exercise the production implementations with temp stores,
// temp Git repositories, a fake clock, and an in-process fake runner. It never
// starts a daemon process or a model runner.
func TestFactoryExecutionControl(t *testing.T) {
	t.Run("two_project_DAG_ownership_and_fairness_timeline", testFactoryExecutionTimeline)
	t.Run("opt_in_admission_and_interactive_work_start", testFactoryOptInAdmission)
	t.Run("review_and_failure_matrix", testFactoryFailureMatrix)
	t.Run("crash_and_replay_exactly_once", testFactoryCrashReplay)
	t.Run("incremental_recovery_and_compatibility", testFactoryIncrementalCompatibility)
}

type factoryExecutionTimelineStep struct {
	Seam    string
	Project string
	Outcome string
}

func testFactoryExecutionTimeline(t *testing.T) {
	timeline := make([]factoryExecutionTimelineStep, 0, 12)
	record := func(seam, project, outcome string) {
		timeline = append(timeline, factoryExecutionTimelineStep{Seam: seam, Project: project, Outcome: outcome})
	}

	alphaNotes := []Note{
		frontierTestNote("ALPHA-WAVE", "wave", map[string]any{
			"authorization": "armed",
			"members":       []any{"ALPHA-ROOT", "ALPHA-SOFT", "ALPHA-HARD", "ALPHA-INDEPENDENT"},
		}),
		frontierTestNote("ALPHA-ROOT", "task", map[string]any{
			"status": "review", "proof_status": "satisfied", "wave": "ALPHA-WAVE",
		}),
		frontierTestNote("ALPHA-SOFT", "task", map[string]any{
			"status": "ready", "dependencies": []any{"ALPHA-ROOT:soft"}, "wave": "ALPHA-WAVE",
		}),
		frontierTestNote("ALPHA-HARD", "task", map[string]any{
			"status": "ready", "dependencies": []any{"ALPHA-ROOT:hard"}, "wave": "ALPHA-WAVE",
		}),
		frontierTestNote("ALPHA-INDEPENDENT", "task", map[string]any{
			"status": "ready", "wave": "ALPHA-WAVE",
		}),
	}
	betaNotes := []Note{
		frontierTestNote("BETA-WAVE", "wave", map[string]any{
			"authorization": "armed",
			"members":       []any{"BETA-ROOT", "BETA-HARD", "BETA-INDEPENDENT"},
		}),
		frontierTestNote("BETA-ROOT", "task", map[string]any{
			"status": "ready", "wave": "BETA-WAVE",
		}),
		frontierTestNote("BETA-HARD", "task", map[string]any{
			"status": "ready", "dependencies": []any{"BETA-ROOT:hard"}, "wave": "BETA-WAVE",
		}),
		frontierTestNote("BETA-INDEPENDENT", "task", map[string]any{
			"status": "ready", "wave": "BETA-WAVE",
		}),
	}

	alpha := newProjectFrontierIndex("alpha")
	alpha.rebuild(alphaNotes)
	assertFactoryStringSet(t, alpha.Frontier, "ALPHA-INDEPENDENT", "ALPHA-SOFT")
	record("frontier.rebuild", "alpha", "soft premise provisionally unlocks while hard successor stays blocked")

	beta := newProjectFrontierIndex("beta")
	beta.rebuild(betaNotes)
	assertFactoryStringSet(t, beta.Frontier, "BETA-INDEPENDENT", "BETA-ROOT")
	record("frontier.rebuild", "beta", "independent and root branches are parallel")

	// Global fairness uses the real scheduler and the same run-ownership CAS
	// used by daemon dispatch. The fake runner does nothing except claim.
	store := fairDispatchTestStore(t)
	ownership := newRunOwnershipService(store)
	ownership.projectConcurrencyLimit = 8
	ownership.now = func() time.Time {
		return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	}
	candidates := []daemonDispatchCandidate{
		fairDispatchTestCandidate("alpha", "ALPHA-INDEPENDENT", "p1", ""),
		fairDispatchTestCandidate("alpha", "ALPHA-SOFT", "p1", ""),
		fairDispatchTestCandidate("beta", "BETA-INDEPENDENT", "p1", ""),
		fairDispatchTestCandidate("beta", "BETA-ROOT", "p1", ""),
	}
	for index := range candidates {
		candidates[index].Run.WorkspacePath = filepath.Join(t.TempDir(), candidates[index].Run.ItemID)
		if err := store.UpsertRun(candidates[index].Run); err != nil {
			t.Fatal(err)
		}
	}
	var claims []string
	daemon := &Daemon{store: store, stateRoot: store.stateRoot}
	daemon.fairDispatchRun = func(
		_ context.Context,
		project RegisteredProject,
		_ WorkflowFile,
		_ Note,
		run RunStatus,
		_ string,
		attemptID string,
	) (RunStatus, bool, bool, error) {
		result, err := ownership.claimExistingWithAuthorization(run, attemptID, RunAuthorization{
			Source: "daemon_auto", Actor: "daemon:fixture", Trigger: "fair_poll",
			ProjectAutomationEnabled: true,
		}, RunAttempt{})
		if err != nil {
			return run, false, false, err
		}
		if !result.Claimed || result.Run == nil {
			return run, true, false, nil
		}
		claims = append(claims, project.ProjectID+"/"+run.ItemID)
		return *result.Run, true, true, nil
	}
	if err := daemon.dispatchFairCandidates(context.Background(), candidates, 4); err != nil {
		t.Fatal(err)
	}
	assertFairDispatchOrder(t, []string{
		"alpha/ALPHA-INDEPENDENT",
		"beta/BETA-INDEPENDENT",
		"alpha/ALPHA-SOFT",
		"beta/BETA-ROOT",
	}, claims)
	record("scheduler.dispatchFairCandidates + ownership.claimExisting", "alpha,beta", "one turn per project before either repeats")

	// Replaying the original planning snapshots after a scheduler restart must
	// observe the existing leases and invoke no second worker claim.
	restarted := &Daemon{store: store, stateRoot: store.stateRoot, fairDispatchRun: daemon.fairDispatchRun}
	if err := restarted.dispatchFairCandidates(context.Background(), candidates, 4); err != nil {
		t.Fatal(err)
	}
	if len(claims) != 4 {
		t.Fatalf("scheduler replay duplicated a claim: %#v", claims)
	}
	record("scheduler restart", "alpha,beta", "stale candidate snapshots cannot duplicate live claims")

	// A reviewer change relocks the soft edge but cannot stop the independent
	// branch. This is an actual incremental reverse-closure update.
	alphaRework := frontierTestNote("ALPHA-ROOT", "task", map[string]any{
		"status": "rework", "proof_status": "", "wave": "ALPHA-WAVE",
	})
	counters := alpha.apply([]Note{alphaRework})
	assertFactoryStringSet(t, alpha.Frontier, "ALPHA-INDEPENDENT", "ALPHA-ROOT")
	if counters.GraphRecomputed != 4 {
		t.Fatalf("alpha rework recomputed %d nodes, want the four-task wave closure", counters.GraphRecomputed)
	}
	record("frontier.apply", "alpha", "changes requested relocks soft and hard closure only")

	alphaReviewed := frontierTestNote("ALPHA-ROOT", "task", map[string]any{
		"status": "review", "proof_status": "satisfied", "wave": "ALPHA-WAVE",
	})
	alpha.apply([]Note{alphaReviewed})
	assertFactoryStringSet(t, alpha.Frontier, "ALPHA-INDEPENDENT", "ALPHA-SOFT")
	record("frontier.apply", "alpha", "re-reviewed proof restores provisional soft unlock")

	alphaDone := frontierTestNote("ALPHA-ROOT", "task", map[string]any{
		"status": "done", "proof_status": "satisfied", "wave": "ALPHA-WAVE",
	})
	alpha.apply([]Note{alphaDone})
	assertFactoryStringSet(t, alpha.Frontier, "ALPHA-HARD", "ALPHA-INDEPENDENT", "ALPHA-SOFT")
	record("frontier.apply", "alpha", "integrated done is the first state that unlocks the hard successor")

	betaDone := frontierTestNote("BETA-ROOT", "task", map[string]any{
		"status": "done", "proof_status": "satisfied", "wave": "BETA-WAVE",
	})
	beta.apply([]Note{betaDone})
	assertFactoryStringSet(t, beta.Frontier, "BETA-HARD", "BETA-INDEPENDENT")
	record("frontier.apply", "beta", "sibling project drains independently")

	alphaCold := newProjectFrontierIndex("alpha")
	alphaCold.rebuild(alpha.notes())
	betaCold := newProjectFrontierIndex("beta")
	betaCold.rebuild(beta.notes())
	if !reflect.DeepEqual(alpha.Eligibility, alphaCold.Eligibility) ||
		!reflect.DeepEqual(alpha.Frontier, alphaCold.Frontier) ||
		!reflect.DeepEqual(beta.Eligibility, betaCold.Eligibility) ||
		!reflect.DeepEqual(beta.Frontier, betaCold.Frontier) {
		t.Fatal("incremental final graph differs from cold rebuild")
	}
	record("frontier cold rebuild", "alpha,beta", "final eligibility and ordered frontiers match")

	// The interactive path and daemon path share runOwnershipService. A
	// disabled-project interactive claim creates one attempt; a daemon replay of
	// its stale snapshot loses the same lease CAS.
	manual := fairDispatchTestRun("manual-project", "MANUAL-T-0001")
	manual.WorkspacePath = t.TempDir()
	if err := store.UpsertRun(manual); err != nil {
		t.Fatal(err)
	}
	hand, err := ownership.claimWorkSessionWithAuthorization(
		manual,
		"agent:interactive",
		RunAuthorization{
			Source: "codex", Actor: "agent:interactive", Trigger: "work_start",
			ProjectAutomationEnabled: false,
		},
		RunIdentityMetadata{
			RepoRoot: t.TempDir(), WorkspacePath: manual.WorkspacePath,
			WorkspaceMode: "shared", Runner: manual.Runner, Branch: "fixture/manual",
		},
	)
	if err != nil || !hand.Claimed || hand.Authorization == nil || hand.Authorization.ProjectAutomationEnabled {
		t.Fatalf("interactive disabled-project claim failed or widened authority: result=%#v err=%v", hand, err)
	}
	loser, err := ownership.claimExistingWithAuthorization(manual, "daemon:late", RunAuthorization{
		Source: "daemon_auto", Actor: "daemon:late", Trigger: "poll",
		ProjectAutomationEnabled: true,
	}, RunAttempt{})
	if err != nil || loser.Claimed {
		t.Fatalf("daemon duplicated interactive ownership: result=%#v err=%v", loser, err)
	}
	attempts, err := store.ListAttemptsForRun(manual.ProjectID, manual.RecordID)
	if err != nil || len(attempts) != 1 {
		t.Fatalf("interactive claim attempt count=%d, want one: %#v err=%v", len(attempts), attempts, err)
	}
	record("ownership.claimWorkSessionWithAuthorization", "manual-project", "automation-off work owns one lease and one attempt")

	if len(timeline) < 10 {
		t.Fatalf("fixture timeline is unexpectedly shallow: %#v", timeline)
	}
}

func testFactoryOptInAdmission(t *testing.T) {
	t.Run("public_work_start_works_with_automation_off", func(t *testing.T) {
		t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
		vault := automationTestVault(t)
		mustRunPickupTest(t, Args{
			"vault": vault, "quiet": "true", "epic": "APP",
			"title": "Factory manual work", "risk": "low", "priority": "p0", "v7": "true",
		}, newV7Task)
		makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
		initializeOrchestrationGitRepo(t, filepath.Dir(vault))
		project := registerAutomationTestProject(t, vault)
		if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
			t.Fatal(err)
		}
		store, err := OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		project.Enabled, project.Health = false, projectHealthDisabled
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		output := captureStdout(t, func() {
			if err := workSessionStartCmd(Args{
				"vault": vault, "id": "APP-T-0001",
				"by": "agent:fixture", "source": "codex",
			}); err != nil {
				t.Fatal(err)
			}
		})
		var packet workSessionPacket
		if err := json.Unmarshal([]byte(output), &packet); err != nil {
			t.Fatal(err)
		}
		store, err = OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run, err := store.FindRun("APP-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		auth, err := store.LatestRunAuthorization(project.ProjectID, "APP-T-0001")
		if err != nil {
			t.Fatal(err)
		}
		if run == nil || !run.HandRun || auth == nil || auth.ProjectAutomationEnabled ||
			auth.Source != "codex" || packet.Branch == "" || packet.Head == "" {
			t.Fatalf("public work start lost disabled-project ownership identity: run=%#v auth=%#v packet=%#v", run, auth, packet)
		}
	})

	t.Run("armed_wave_daemon_admission_is_exact", func(t *testing.T) {
		t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
		vault, idx, _ := armedWaveTestFixture(t)
		task := idx.Tasks["APP-T-0001"]
		wf := defaultWorkflow()
		wf.DispatchScope = defaultAutomationDispatchScope()
		wf.Workspace.Strategy = string(WorkspaceStrategyWorktree)
		if reason := automationDispatchScopeBlocker(vault, task, wf, nil); reason != "" {
			t.Fatalf("exact armed frontier fixture is not admissible: %s", reason)
		}

		store := fairDispatchTestStore(t)
		run := fairDispatchTestRun("armed-project", "APP-T-0001")
		run.WorkspacePath = t.TempDir()
		run.WorkRevision = intField(task.Data, "work_revision")
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		project := RegisteredProject{
			ProjectID: "armed-project", ProjectKey: "armed-project",
			RepoRoot: filepath.Dir(vault), VaultRoot: vault, Enabled: true,
		}
		candidate := daemonDispatchCandidate{
			Project: project, Workflow: WorkflowFile{Data: wf}, Note: task,
			Run: run, Lane: runLaneExecute, Status: stringField(task.Data, "status"),
			ProjectLimit: 2,
		}
		service := newRunOwnershipService(store)
		service.projectConcurrencyLimit = 2
		service.now = func() time.Time {
			return time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC)
		}
		claims := 0
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		daemon.fairDispatchRun = func(
			_ context.Context,
			_ RegisteredProject,
			_ WorkflowFile,
			_ Note,
			run RunStatus,
			_ string,
			attemptID string,
		) (RunStatus, bool, bool, error) {
			result, err := service.claimExistingWithAuthorization(run, attemptID, RunAuthorization{
				Source: "daemon_auto", Actor: "daemon:fixture", Trigger: "armed_poll",
				ProjectAutomationEnabled: true,
			}, RunAttempt{})
			if err != nil || !result.Claimed || result.Run == nil {
				return run, result.Run != nil, false, err
			}
			claims++
			return *result.Run, true, true, nil
		}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 1); err != nil {
			t.Fatal(err)
		}
		if claims != 1 {
			t.Fatalf("armed exact frontier claims=%d, want one", claims)
		}

		unrelated := Note{Data: map[string]any{"id": "APP-T-UNRELATED", "status": "ready"}}
		if got := automationDispatchScopeBlocker(vault, unrelated, wf, nil); !strings.Contains(got, "requires task membership") {
			t.Fatalf("unrelated ready task admission=%q", got)
		}
		missingWave := Note{Data: map[string]any{"id": "APP-T-DISARMED", "status": "ready", "wave": "W-MISSING"}}
		if got := automationDispatchScopeBlocker(vault, missingWave, wf, nil); got != "wave is not durably armed" {
			t.Fatalf("disarmed wave admission=%q", got)
		}
		writeArmedWaveTestFields(t, vault, map[string]any{"authorization_fingerprint": "sha256:stale"})
		staleTask, err := resolveV7Note(vault, "APP-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}
		if got := automationDispatchScopeBlocker(vault, staleTask, wf, nil); !strings.Contains(got, "durably armed") {
			t.Fatalf("stale fingerprint admission=%q", got)
		}
	})

	t.Run("disabled_sibling_is_not_polled", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		project := RegisteredProject{
			ProjectID: "disabled-project", ProjectKey: "disabled-project",
			RepoRoot: t.TempDir(), VaultRoot: filepath.Join(t.TempDir(), ".tusker"),
			Enabled: false, Health: projectHealthDisabled,
		}
		if err := store.UpsertProject(project); err != nil {
			t.Fatal(err)
		}
		daemon := &Daemon{
			store: store, stateRoot: store.stateRoot,
			frontiers: map[string]*projectFrontierIndex{}, frontierHints: map[string][]daemonControlChange{},
		}
		calls := 0
		daemon.fairDispatchRun = func(
			context.Context, RegisteredProject, WorkflowFile, Note, RunStatus, string, string,
		) (RunStatus, bool, bool, error) {
			calls++
			return RunStatus{}, false, false, nil
		}
		if err := daemon.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatalf("disabled project reached runner seam %d times", calls)
		}
	})
}

func testFactoryFailureMatrix(t *testing.T) {
	t.Run("changes_requested_reworks_without_completion", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		defer daemon.Close()
		result.Verdict = "changes_requested"
		result.Summary = "objective changes"
		result.Findings = []string{"repair the exact regression"}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		if err := daemon.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err != nil {
			t.Fatal(err)
		}
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		transaction := factoryCompletionTransaction(t, daemon.store, project.ProjectID, result)
		if stringField(task.Data, "status") != "rework" ||
			transaction.Phase != completionPhaseTerminal ||
			transaction.Disposition != "rework" ||
			strings.Count(generatedReviewerFindingContent(task.Body), "repair the exact regression") != 1 {
			t.Fatalf("changes-requested outcome is not one terminal handback: task=%#v transaction=%#v", task.Data, transaction)
		}
	})

	t.Run("infrastructure_block_parks_review", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		defer daemon.Close()
		result.Verdict, result.Blocker = "blocked", "infrastructure"
		result.Summary = "fixture build host unavailable"
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		if err := daemon.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: result.TaskID, ItemID: result.TaskID,
			Runner: string(RunnerCodexExec), RunnerProfile: result.RunnerProfile, Lane: runLaneReview,
			LeaseState: string(LeaseStateRunning), ActiveAttemptID: result.AttemptID,
			AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: result.WorkRevision,
		}); err != nil {
			t.Fatal(err)
		}
		if err := daemon.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err != nil {
			t.Fatal(err)
		}
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		run, err := daemon.store.FindRun(result.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		transaction := factoryCompletionTransaction(t, daemon.store, project.ProjectID, result)
		if stringField(task.Data, "status") != "review" || run == nil ||
			run.LeaseState != string(LeaseStateParkedNoProgress) ||
			transaction.Disposition != "park" || transaction.Phase != completionPhaseTerminal {
			t.Fatalf("infrastructure blocker falsely completed or reworked: task=%#v run=%#v transaction=%#v", task.Data, run, transaction)
		}
	})

	t.Run("reviewer_exit_without_result_retries_then_caps", func(t *testing.T) {
		vault := automationTestVault(t)
		mustRunPickupTest(t, Args{
			"vault": vault, "quiet": "true", "epic": "APP",
			"title": "Factory reviewer exit", "risk": "low", "priority": "p0", "v7": "true",
		}, newV7Task)
		setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
			"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer",
			"source_sha": "abc123", "work_revision": 2,
		})
		project := registerAutomationTestProject(t, vault)
		statusPath := filepath.Join(t.TempDir(), "reviewer.status.json")
		if err := writeRunnerStatusFile(statusPath, 0); err != nil {
			t.Fatal(err)
		}
		daemon, err := NewDaemon(DefaultStateRoot())
		if err != nil {
			t.Fatal(err)
		}
		defer daemon.Close()
		wfFile, err := loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		run := RunStatus{
			ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: string(RunnerCodexExec), RunnerProfile: "review-fixture", Lane: runLaneReview,
			LeaseState: string(LeaseStateRunning), ActiveAttemptID: "review-1", WorkRevision: 2,
			StatusPath: statusPath, WorkspacePath: t.TempDir(), AttemptCount: 1,
		}
		updated, changed, err := daemon.reconcileRun(context.Background(), project, wfFile, run)
		if err != nil {
			t.Fatal(err)
		}
		if !changed || updated.LeaseState != string(LeaseStateReleased) ||
			updated.AttemptOutcome != string(AttemptOutcomeFailed) {
			t.Fatalf("result-less review exit was accepted: %#v", updated)
		}
		attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, run.RecordID)
		if err != nil {
			t.Fatal(err)
		}
		note, err := resolveV7Note(vault, run.RecordID, "task")
		if err != nil {
			t.Fatal(err)
		}
		if !reviewDispatchAllowed(vault, note, wfFile.Data, updated, reviewerAttemptCount(attempts)) {
			t.Fatal("result-less reviewer exit was not retryable below its cap")
		}
		wfFile.Data.Reviewer.MaxCycles = 1
		if reviewDispatchAllowed(vault, note, wfFile.Data, updated, reviewerAttemptCount(attempts)) {
			t.Fatal("result-less reviewer exit escaped its cycle cap")
		}
	})

	t.Run("merge_conflict_and_red_gate_rework_without_ref_movement", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			gate     string
			conflict bool
		}{
			{name: "merge_conflict", gate: "true", conflict: true},
			{name: "red_gate", gate: "false"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				vault, project, daemon, result := factoryFailingPassFixture(t, tc.gate, tc.conflict)
				defer daemon.Close()
				before, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/integration/W-0001")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := daemon.store.SaveReviewResult(result); err != nil {
					t.Fatal(err)
				}
				if err := daemon.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err != nil {
					t.Fatal(err)
				}
				after, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/integration/W-0001")
				if err != nil {
					t.Fatal(err)
				}
				task, err := resolveV7Note(vault, result.TaskID, "task")
				if err != nil {
					t.Fatal(err)
				}
				transaction := factoryCompletionTransaction(t, daemon.store, project.ProjectID, result)
				if before != after || stringField(task.Data, "status") != "rework" ||
					transaction.Phase != completionPhaseTerminal || transaction.Disposition != "rework" {
					t.Fatalf("%s falsely completed or moved integration: before=%s after=%s task=%#v transaction=%#v", tc.name, before, after, task.Data, transaction)
				}
			})
		}
	})

	t.Run("human_block_requires_a_real_human_gate", func(t *testing.T) {
		vault, base := reviewResultCommandFixture(t)
		blocked := cloneReviewArgs(base)
		blocked["verdict"] = "blocked"
		blocked["finding"] = ""
		blocked["blocker"] = "human"
		if err := reviewSubmitCmd(blocked); err == nil {
			t.Fatal("human blocker without a human-owned gate was accepted")
		}
		if err := newV7Gate(Args{
			"vault": vault, "quiet": "true", "blocks": "APP-T-0001",
			"kind": "signoff", "owner": "human:product",
			"action":           "Review the subjective fixture artifact.",
			"verification":     "Product records its decision.",
			"why-agent-cannot": "The contract reserves subjective acceptance for the product owner.",
			"covers":           "A1",
		}); err != nil {
			t.Fatal(err)
		}
		if err := reviewSubmitCmd(refreshReviewArgs(t, vault, blocked)); err != nil {
			t.Fatalf("genuine human wait rejected: %v", err)
		}
	})

	t.Run("attempt_cap_parks_without_new_attempt", func(t *testing.T) {
		wf := defaultWorkflow()
		wf.Retry.MaxAttempts = 2
		run := fairDispatchTestRun("cap-project", "CAP-T-0001")
		run.AttemptCount = 2
		got, capped := (&Daemon{}).enforceAttemptCreationCap(
			wf, run, attemptCreationRetry, "fixture retry would create another attempt",
		)
		if !capped || got.LeaseState != string(LeaseStateParkedNoProgress) ||
			!strings.Contains(got.LastError, "attempt cap reached (2)") {
			t.Fatalf("attempt cap outcome=%#v capped=%v", got, capped)
		}
	})

	t.Run("named_resource_contention_blocks_only_the_waiter", func(t *testing.T) {
		store := fairDispatchTestStore(t)
		first := fairDispatchTestCandidate("resource-a", "A-T-0001", "p1", "fixture-host")
		second := fairDispatchTestCandidate("resource-b", "B-T-0001", "p1", "fixture-host")
		for _, candidate := range []*daemonDispatchCandidate{&first, &second} {
			candidate.Run.WorkspacePath = t.TempDir()
			if err := store.UpsertRun(candidate.Run); err != nil {
				t.Fatal(err)
			}
		}
		service := newRunOwnershipService(store)
		service.projectConcurrencyLimit = 2
		daemon := &Daemon{store: store, stateRoot: store.stateRoot}
		var claims []string
		daemon.fairDispatchRun = func(
			_ context.Context,
			project RegisteredProject,
			_ WorkflowFile,
			_ Note,
			run RunStatus,
			_ string,
			attemptID string,
		) (RunStatus, bool, bool, error) {
			result, err := service.claimExistingWithAuthorization(run, attemptID, RunAuthorization{
				Source: "daemon_auto", Actor: "daemon:fixture", Trigger: "resource_poll",
				ProjectAutomationEnabled: true,
			}, RunAttempt{})
			if err != nil || !result.Claimed || result.Run == nil {
				return run, result.Run != nil, false, err
			}
			claims = append(claims, project.ProjectID+"/"+run.ItemID)
			return *result.Run, true, true, nil
		}
		if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{first, second}, 2); err != nil {
			t.Fatal(err)
		}
		assertFairDispatchOrder(t, []string{"resource-a/A-T-0001"}, claims)
		waiter := fairDispatchFindRun(t, store, "resource-b", "B-T-0001")
		if !strings.Contains(waiter.LastError, `named resource "fixture-host" is held by project resource-a`) {
			t.Fatalf("resource waiter reason=%q", waiter.LastError)
		}
	})
}

func testFactoryCrashReplay(t *testing.T) {
	t.Run("claim_and_result_CAS_are_idempotent", func(t *testing.T) {
		store, run := ownershipStoreFixture(t, "CAS-T-0001")
		service := newRunOwnershipService(store)
		service.projectConcurrencyLimit = 2
		service.now = func() time.Time {
			return time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
		}
		first, err := service.claimExistingWithAuthorization(run, "attempt-first", RunAuthorization{
			Source: "daemon_auto", Actor: "daemon:first", Trigger: "claim_crash",
			ProjectAutomationEnabled: true,
		}, RunAttempt{})
		if err != nil || !first.Claimed {
			t.Fatalf("first claim failed: result=%#v err=%v", first, err)
		}
		replay, err := service.claimExistingWithAuthorization(run, "attempt-replay", RunAuthorization{
			Source: "daemon_auto", Actor: "daemon:replay", Trigger: "claim_restart",
			ProjectAutomationEnabled: true,
		}, RunAttempt{})
		if err != nil || replay.Claimed || replay.OwnerRun == nil ||
			replay.OwnerRun.LeaseOwner != "attempt-first" {
			t.Fatalf("claim replay did not retain first owner: result=%#v err=%v", replay, err)
		}

		result := validStoredReviewResult()
		insertReplay, err := store.SaveReviewResult(result)
		if err != nil || insertReplay {
			t.Fatalf("first result save replay=%v err=%v", insertReplay, err)
		}
		exactReplay, err := store.SaveReviewResult(result)
		if err != nil || !exactReplay {
			t.Fatalf("exact result replay=%v err=%v", exactReplay, err)
		}
	})

	t.Run("rework_crash_replays_one_handback_and_audit", func(t *testing.T) {
		vault, project, daemon, result := completionReactorFixture(t, false)
		result.Verdict = "changes_requested"
		result.Summary = "crash handback"
		result.Findings = []string{"one durable crash finding"}
		result.ResultRevision = reviewResultFingerprint(result)
		if _, err := daemon.store.SaveReviewResult(result); err != nil {
			t.Fatal(err)
		}
		if err := daemon.store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: result.TaskID, ItemID: result.TaskID,
			Runner: string(RunnerCodexExec), RunnerProfile: result.RunnerProfile, Lane: runLaneReview,
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
				return errors.New("fixture crash after rework handback")
			}
			return nil
		}
		if err := daemon.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err == nil {
			t.Fatal("rework crash was not injected")
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
		for replay := 0; replay < 2; replay++ {
			if err := restarted.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err != nil {
				t.Fatal(err)
			}
		}
		task, err := resolveV7Note(vault, result.TaskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		transaction := factoryCompletionTransaction(t, restarted.store, project.ProjectID, result)
		if transaction.Phase != completionPhaseTerminal ||
			strings.Count(generatedReviewerFindingContent(task.Body), "one durable crash finding") != 1 {
			t.Fatalf("rework replay duplicated or lost handback: task=%#v transaction=%#v", task.Data, transaction)
		}
		wave, err := resolveV7Note(vault, transaction.WaveID, "wave")
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
			t.Fatalf("rework audit count=%d, want one", defects)
		}
	})

	for _, point := range []string{"staging_commit", "ref_commit", "canonical_projection", "audit", "wake"} {
		point := point
		t.Run("passing_completion_"+point, func(t *testing.T) {
			factoryAssertPassingCrashReplay(t, point)
		})
	}
}

func testFactoryIncrementalCompatibility(t *testing.T) {
	t.Run("canonical_hint_raw_edit_fallback_and_cold_equivalence", func(t *testing.T) {
		vault := automationTestVault(t)
		mustRunPickupTest(t, Args{
			"vault": vault, "quiet": "true", "epic": "APP",
			"title": "Adaptive fixture", "risk": "low", "priority": "p0", "v7": "true",
		}, newV7Task)
		makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
		notes, err := listOperationalNotes(vault)
		if err != nil {
			t.Fatal(err)
		}
		projectID := "adaptive-project"
		daemon := &Daemon{
			frontiers:     map[string]*projectFrontierIndex{},
			frontierHints: map[string][]daemonControlChange{},
		}
		daemon.rebuildFrontier(projectID, notes)
		project := RegisteredProject{ProjectID: projectID, VaultRoot: vault}
		task, err := resolveV7Note(vault, "APP-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}

		// A canonical notification with the exact task identity takes the warm
		// incremental seam.
		warm, ok := daemon.applyFrontierHint(project, []daemonControlChange{{
			ID: "APP-T-0001", Kind: "task",
		}})
		if !ok || len(warm) != len(notes) {
			t.Fatalf("canonical warm hint fell back: ok=%v notes=%d want=%d", ok, len(warm), len(notes))
		}

		// A raw edit has no trustworthy resulting revision. A mismatched hint
		// must refuse the warm path; the caller then performs the adaptive scan.
		raw, err := readText(task.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(task.AbsolutePath, raw+"\nraw fixture edit\n"); err != nil {
			t.Fatal(err)
		}
		invalidateCachedNote(task.AbsolutePath)
		if _, ok := daemon.applyFrontierHint(project, []daemonControlChange{{
			ID: "APP-T-0001", Kind: "task", Revision: "sha256:not-the-raw-edit",
		}}); ok {
			t.Fatal("raw edit with a mismatched revision incorrectly used the warm projection")
		}
		rescanned, err := listOperationalNotes(vault)
		if err != nil {
			t.Fatal(err)
		}
		daemon.rebuildFrontier(projectID, rescanned)
		cold := newProjectFrontierIndex(projectID)
		cold.rebuild(rescanned)
		warmIndex := daemon.frontiers[projectID]
		if warmIndex == nil ||
			!reflect.DeepEqual(warmIndex.Eligibility, cold.Eligibility) ||
			!reflect.DeepEqual(warmIndex.Frontier, cold.Frontier) {
			t.Fatalf("adaptive recovery differs from cold rebuild: warm=%#v cold=%#v", warmIndex, cold)
		}

		// The operations brief is a separate read-only projection of the same
		// recovered canonical state. Compose it through the production seam with
		// a fixed clock so this parity check cannot be hidden by generated_at.
		workflow, err := loadWorkflow(vault)
		if err != nil {
			t.Fatal(err)
		}
		briefFacts := func(index v7Index) factoryOperationsFacts {
			return factoryOperationsFacts{
				VaultPath: vault, RepoRoot: filepath.Dir(vault), Project: project,
				Workflow: workflow.Data, Index: index, Runs: map[string]RunStatus{},
				Completions: map[string]factoryOperationsCompletionFact{},
				WaveFacts:   map[string]factoryOperationsWaveFact{},
				Now:         time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
			}
		}
		warmBriefIndex, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		coldBriefIndex, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		warmBrief := composeFactoryOperations(briefFacts(warmBriefIndex))
		coldBrief := composeFactoryOperations(briefFacts(coldBriefIndex))
		if warmBrief.Schema != factoryOperationsSchema {
			t.Fatalf("operations brief schema=%q, want %q", warmBrief.Schema, factoryOperationsSchema)
		}
		if !reflect.DeepEqual(warmBrief, coldBrief) {
			t.Fatalf("adaptive recovery operations brief differs from cold rebuild: warm=%#v cold=%#v", warmBrief, coldBrief)
		}
	})

	t.Run("explicit_all_eligible_and_V1_import_remain_compatible", func(t *testing.T) {
		resolved := resolvedTuskerConfig{Layers: []tuskerConfigLayer{{
			Name: configSourceProject, Path: ".tusker/config.yaml", Present: true,
			Raw: map[string]any{"automation": map[string]any{"dispatch_scope": "all_eligible"}},
		}}}
		scope, err := resolveAutomationDispatchScope(resolved, true)
		if err != nil {
			t.Fatal(err)
		}
		wf := defaultWorkflow()
		wf.DispatchScope = scope
		if scope.Effective != string(automationDispatchScopeAllEligible) ||
			automationDispatchScopeBlocker("", Note{Data: map[string]any{
				"id": "LEGACY-T-0001", "status": "ready",
			}}, wf, nil) != "" {
			t.Fatalf("explicit all_eligible compatibility=%#v", scope)
		}

		vault := deliveryTestVault(t)
		path := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
		if err := deliveryImportCmd(Args{
			"vault": vault, "plan": path, "dry-run": "true", "quiet": "true",
		}); err != nil {
			t.Fatalf("V1 dry-run regressed: %v", err)
		}
		if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
			t.Fatalf("V1 import regressed: %v", err)
		}
		if !fileExists(filepath.Join(vault, "work", "tasks", "APP-T-0001.md")) {
			t.Fatal("V1 import did not preserve task allocation")
		}
	})

	t.Run("direct_singleton_replay_remains_disarmed", func(t *testing.T) {
		_, vault := newLandTestRepo(t, 1, "true")
		clearWaveBackpointer(t, vault, "APP-T-0001")
		setSingletonPromotionMode(t, vault, scheduledPromotionStage)
		setWaveTaskState(t, vault, "APP-T-0001", "review", "review", "")
		unit, created, err := ensureV7ImplicitSingletonDeliveryUnit(
			vault, "APP-T-0001", Args{"vault": vault, "quiet": "true"},
		)
		if err != nil || !created || unit == "" {
			t.Fatalf("direct singleton creation: unit=%q created=%v err=%v", unit, created, err)
		}
		data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", unit+".md"))
		if err != nil {
			t.Fatal(err)
		}
		if !v7ImplicitDeliveryUnit(Note{Data: data}) ||
			stringField(data, "authorization") != "disarmed" ||
			boolFromAny(data["release_authorized"]) {
			t.Fatalf("direct singleton widened authority: %#v", data)
		}
		replayed, recreated, err := ensureV7ImplicitSingletonDeliveryUnit(
			vault, "APP-T-0001", Args{"vault": vault, "quiet": "true"},
		)
		if err != nil || recreated || replayed != unit {
			t.Fatalf("singleton replay drift: unit=%q recreated=%v err=%v", replayed, recreated, err)
		}
	})
}

func factoryFailingPassFixture(t *testing.T, gate string, conflict bool) (string, RegisteredProject, *Daemon, ReviewResult) {
	t.Helper()
	repo, vault := newLandTestRepo(t, 1, gate)
	source := commitLandBranch(t, repo, "source/APP-T-0001", "integration/W-0001", map[string]string{
		"reviewed.txt": "exact reviewed source\n",
	})
	if conflict {
		source = commitLandBranch(t, repo, "source/APP-T-0001-conflict", "integration/W-0001", map[string]string{
			"collision.txt": "reviewed side\n",
		})
		factoryCommitIntegrationFile(t, repo, "collision.txt", "integration side\n")
	}
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
	result := completionResultForReviewedTask(
		t, vault, project, "APP-T-0001", "review-failure-fixture", "objective pass before deterministic landing",
	)
	return vault, project, daemon, result
}

func factoryCommitIntegrationFile(t *testing.T, repo, path, content string) {
	t.Helper()
	const branch = "integration/W-0001"
	old := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", branch))
	worktree := filepath.Join(t.TempDir(), "factory-integration-collision")
	runGitDir(t, repo, "worktree", "add", "--detach", worktree, old)
	if err := writeText(filepath.Join(worktree, path), content); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, worktree, "add", "--", path)
	runGitDir(t, worktree, "commit", "-m", "integration collision")
	next := strings.TrimSpace(gitDirOutput(t, worktree, "rev-parse", "HEAD"))
	runGitDir(t, repo, "worktree", "remove", "--force", worktree)
	runGitDir(t, repo, "update-ref", "refs/heads/"+branch, next, old)
}

func factoryAssertPassingCrashReplay(t *testing.T, point string) {
	t.Helper()
	vault, project, daemon, result := completionReactorFixture(t, true)
	mainBefore, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/main")
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := daemon.store.SaveReviewResult(result); err != nil || replay {
		t.Fatalf("first result persistence replay=%v err=%v", replay, err)
	}
	if replay, err := daemon.store.SaveReviewResult(result); err != nil || !replay {
		t.Fatalf("exact result persistence replay=%v err=%v", replay, err)
	}

	oldHook := completionReactorCrashHook
	t.Cleanup(func() { completionReactorCrashHook = oldHook })
	crashed := false
	capturedSHA := ""
	completionReactorCrashHook = func(got string, transaction *completionTransaction) error {
		if got == point && !crashed {
			crashed = true
			capturedSHA = transaction.StagedSHA
			return errors.New("factory completion crash at " + point)
		}
		return nil
	}
	if err := daemon.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err == nil {
		t.Fatalf("%s crash was not injected", point)
	}
	beforeRestart := factoryCompletionTransaction(t, daemon.store, project.ProjectID, result)
	if beforeRestart.Phase == completionPhaseTerminal || capturedSHA == "" {
		t.Fatalf("%s lost resumable state: transaction=%#v staged=%q", point, beforeRestart, capturedSHA)
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
	for replay := 0; replay < 2; replay++ {
		if err := restarted.reconcileReviewCompletion(project, completionAuthorityTestWorkflow()); err != nil {
			t.Fatalf("%s replay %d failed: %v", point, replay+1, err)
		}
	}
	transaction := factoryCompletionTransaction(t, restarted.store, project.ProjectID, result)
	if transaction.Phase != completionPhaseTerminal || transaction.StagedSHA != capturedSHA {
		t.Fatalf("%s did not converge on one candidate: transaction=%#v captured=%s", point, transaction, capturedSHA)
	}
	integration, err := gitOutputTrim(project.RepoRoot, "rev-parse", transaction.IntegrationRef)
	if err != nil || integration != capturedSHA {
		t.Fatalf("%s integration=%s want=%s err=%v", point, integration, capturedSHA, err)
	}
	mainAfter, err := gitOutputTrim(project.RepoRoot, "rev-parse", "refs/heads/main")
	if err != nil || mainAfter != mainBefore {
		t.Fatalf("%s moved default ref: before=%s after=%s err=%v", point, mainBefore, mainAfter, err)
	}
	commits, err := gitOutputTrim(
		project.RepoRoot, "rev-list", "--all", "--grep=Tusker-Completion: "+transaction.ID,
	)
	if err != nil || len(strings.Fields(commits)) != 1 {
		t.Fatalf("%s completion commit count=%d: %q err=%v", point, len(strings.Fields(commits)), commits, err)
	}
	closedEvents, err := filepath.Glob(filepath.Join(
		vault, "events", "*", "*", result.TaskID+"--*--close-*.json",
	))
	if err != nil || len(closedEvents) != 1 {
		t.Fatalf("%s close event count=%d paths=%#v err=%v", point, len(closedEvents), closedEvents, err)
	}
	task, err := resolveV7Note(vault, result.TaskID, "task")
	if err != nil {
		t.Fatal(err)
	}
	if stringField(task.Data, "status") != "done" ||
		strings.Count(task.Body, "[tusker-review-result:"+result.ResultRevision+"]") != 1 {
		t.Fatalf("%s duplicated or lost terminal projection: %#v", point, task.Data)
	}
}

func factoryCompletionTransaction(t *testing.T, store *RuntimeStore, projectID string, result ReviewResult) *completionTransaction {
	t.Helper()
	transaction, err := store.CompletionTransactionForResult(projectID, result.TaskID, result.ResultRevision)
	if err != nil || transaction == nil {
		t.Fatalf("completion transaction missing: result=%#v transaction=%#v err=%v", result, transaction, err)
	}
	return transaction
}

func assertFactoryStringSet(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings=%#v, want %#v", got, want)
	}
	remaining := make(map[string]int, len(want))
	for _, value := range want {
		remaining[value]++
	}
	for _, value := range got {
		remaining[value]--
	}
	for value, count := range remaining {
		if count != 0 {
			t.Fatalf("strings=%#v, want %#v (mismatch %s=%d)", got, want, value, count)
		}
	}
}
