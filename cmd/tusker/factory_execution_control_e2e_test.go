package main

import (
	"context"
	"encoding/json"
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
		}, RunAttempt{AttemptID: attemptID})
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
	}, RunAttempt{AttemptID: "daemon:late"})
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
			"title": "Factory manual work", "risk": "low", "priority": "p0", "v7": "true", "owned-paths": "src"}, newV7Task)
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
			}, RunAttempt{AttemptID: attemptID})
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

func testFactoryIncrementalCompatibility(t *testing.T) {
	t.Run("canonical_hint_raw_edit_fallback_and_cold_equivalence", func(t *testing.T) {
		vault := automationTestVault(t)
		mustRunPickupTest(t, Args{
			"vault": vault, "quiet": "true", "epic": "APP",
			"title": "Adaptive fixture", "risk": "low", "priority": "p0", "v7": "true", "owned-paths": "src"}, newV7Task)
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

	t.Run("explicit_all_eligible_remains_compatible", func(t *testing.T) {
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
