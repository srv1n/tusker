package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestServeWaveExecuteQueuesOnlyExactArmedWaveWithAutomationOff(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	vault, _, wave := waveExecuteTestFixture(t)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	single := wave
	single.Data = cloneMap(wave.Data)
	single.Data["members"] = []string{normalizeList(wave.Data["members"])[0]}
	env := greenWaveEnvironment()
	env.IsolatedWorkspace = false
	env.IntegrationClean = false
	if got := buildWavePreflight(vault, idx, single, env); !got.Checks["workspaceIsolation"] {
		t.Fatalf("single-task wave required integration workspace machinery: %#v", got.Blockers)
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Next", "_pos1": "APP-T-0007"}); err == nil {
		t.Fatal("fixture unexpectedly had APP-T-0007 before it was created")
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Next wave task", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0007")
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Next", "_pos1": "APP-T-0007"}); err != nil {
		t.Fatal(err)
	}
	if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}, "disarmed", nil); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: v7RepoRoot(vault), VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: false, Health: projectHealthDisabled}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	guard, err := acquireDaemonGuard(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()

	server := newServeServer(vault, project.RepoRoot, defaultServeAddr, store, nil)
	server.operatorActor = "human:test"
	fingerprint := stringField(wave.Data, "authorization_fingerprint")
	var first serveWaveExecuteResult
	servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &first)
	if !first.OK || first.Refused || first.Execution == nil || len(first.Execution.QueuedTaskIDs) != len(normalizeList(wave.Data["members"])) {
		t.Fatalf("exact armed wave was not queued: %#v", first)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 || !projects[0].Enabled {
		t.Fatalf("explicit Execute Wave did not opt this registered project into polling: %#v err=%v", projects, err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil || wf.Data.AutomationEnabled {
		t.Fatalf("Execute Wave changed broad project automation: enabled=%v err=%v", wf.Data.AutomationEnabled, err)
	}
	armedIdx, err := loadV7Index(vault)
	if err != nil || stringField(waveAuthorizationProjection(vault, armedIdx, armedIdx.Waves["W-0001"]), "state") != "armed" {
		t.Fatalf("Execute Wave did not atomically authorize its selected wave: err=%v", err)
	}
	for _, taskID := range first.Execution.QueuedTaskIDs {
		directive, err := store.RunDirective("app", taskID)
		if err != nil || directive == nil || directive.WaveID != "W-0001" || directive.AuthorizationFingerprint != fingerprint || directive.WaveAuthorizedAt == "" {
			t.Fatalf("%s directive was not bound to exact wave authority: %#v err=%v", taskID, directive, err)
		}
	}
	if directive, err := store.RunDirective("app", "APP-T-0007"); err != nil || directive != nil {
		t.Fatalf("next-wave task was admitted: %#v err=%v", directive, err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", true); err != nil {
		t.Fatal(err)
	}
	server.invalidateProjectSnapshot("app")

	var replay serveWaveExecuteResult
	servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &replay)
	if !replay.OK || replay.Execution == nil || len(replay.Execution.QueuedTaskIDs) != 0 || len(replay.Execution.AlreadyQueuedTaskIDs) != len(first.Execution.QueuedTaskIDs) {
		t.Fatalf("repeated execute with project automation enabled was not idempotent: %#v", replay)
	}

	writeArmedWaveTestFields(t, vault, map[string]any{"authorization_fingerprint": "sha256:stale"})
	server.invalidateProjectSnapshot("app")
	var stale serveWaveExecuteResult
	servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &stale)
	if !stale.OK || stale.Refused || stale.Execution == nil {
		t.Fatalf("stale wave was not refreshed and queued: %#v", stale)
	}
}

func TestWavePreflightAllowsOnlySerializedSharedWorkspace(t *testing.T) {
	vault, _, wave := waveExecuteTestFixture(t)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf.Data.Workspace.Strategy = string(WorkspaceStrategyShared)
	wf.Data.Runtime.MaxActiveRunsPerProject = 1
	env := greenWaveEnvironment()
	applyWaveWorkflowEnvironment(&env, wave, wf.Data)
	if got := buildWavePreflight(vault, idx, wave, env); !got.Checks["workspaceIsolation"] {
		t.Fatalf("serialized shared workspace was rejected: %#v", got.Blockers)
	}
	wf.Data.Runtime.MaxActiveRunsPerProject = 2
	applyWaveWorkflowEnvironment(&env, wave, wf.Data)
	if got := buildWavePreflight(vault, idx, wave, env); got.Checks["workspaceIsolation"] {
		t.Fatal("concurrent shared workspace passed preflight")
	}
}

func TestWaveDirectiveRequiresCurrentTaskAuthorization(t *testing.T) {
	vault, idx, wave := waveExecuteTestFixture(t)
	now := time.Now().UTC()
	directive := &RunDirective{State: "queued", ExpiresAt: now.Add(waveExecuteDirectiveTTL).Format(time.RFC3339Nano), WaveID: "W-0001", AuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), WaveAuthorizedAt: stringField(wave.Data, "authorized_at")}
	if !runDirectiveMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], directive, now) {
		t.Fatal("current exact-wave directive was rejected")
	}
	directive.AuthorizationFingerprint = "sha256:stale"
	if runDirectiveMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], directive, now) {
		t.Fatal("stale exact-wave directive was admitted")
	}
	directive.AuthorizationFingerprint = stringField(wave.Data, "authorization_fingerprint")
	directive.WaveAuthorizedAt = "2026-01-01T00:00:00Z"
	if runDirectiveMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], directive, now) {
		t.Fatal("directive from an earlier authorization generation was admitted")
	}
	directive.WaveAuthorizedAt = stringField(wave.Data, "authorized_at")
	if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}, "disarmed", nil); err != nil {
		t.Fatal(err)
	}
	if runDirectiveMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], directive, now) {
		t.Fatal("disarmed wave left its execution directive active")
	}
}

func TestConsumedWaveDirectiveAuthorizationKeepsClaimedRunAuthorized(t *testing.T) {
	vault, idx, wave := waveExecuteTestFixture(t)
	run := RunStatus{LeaseGeneration: 3}
	auth := &RunAuthorization{Source: "human_run_directive", LeaseGeneration: 3, DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), DirectiveWaveAuthorizedAt: stringField(wave.Data, "authorized_at")}
	if !runDirectiveAuthorizationMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], run, auth) {
		t.Fatal("current consumed directive authorization was not retained for its claimed run")
	}
	auth.LeaseGeneration++
	if runDirectiveAuthorizationMatchesTaskAuthority(vault, idx.Tasks["APP-T-0001"], run, auth) {
		t.Fatal("authorization from another lease generation was accepted")
	}
}

func TestServeWaveExecuteFencesDaemonClaimsToSelectedWave(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	installCodexSleepShimForTest(t)
	vault, _, _ := waveExecuteTestFixture(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Other wave task", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0007")
	otherTask, err := resolveNote(vault, "APP-T-0007")
	if err != nil {
		t.Fatal(err)
	}
	otherTask.Data["artifact_contract"] = map[string]any{"kind": "diff_summary", "path": "cmd/tusker", "summary": "Focused test artifact."}
	otherTask.Data["state_rev"] = v7StateRev(otherTask.Data, otherTask.Body)
	otherTaskText, err := serializeDocument(otherTask.Data, otherTask.Body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(otherTask.AbsolutePath, otherTaskText); err != nil {
		t.Fatal(err)
	}
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Other", "_pos1": "APP-T-0007"}); err != nil {
		t.Fatal(err)
	}
	green := greenWaveEnvironment()
	if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0002", "by": "human:test", "quiet": "true"}, "armed", &green); err != nil {
		t.Fatal(err)
	}

	store, project, server := waveExecuteDaemonTestRuntime(t, vault)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	otherWave := idx.Waves["W-0002"]
	now := time.Now().UTC()
	if _, _, err := store.QueueWaveRunDirectives(project.ProjectID, "W-0002", stringField(otherWave.Data, "authorization_fingerprint"), stringField(otherWave.Data, "authorized_at"), "human:test", []string{"APP-T-0007"}, now, waveExecuteDirectiveTTL, false); err != nil {
		t.Fatal(err)
	}
	var result serveWaveExecuteResult
	servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &result)
	if !result.OK {
		t.Fatalf("selected wave execute failed: %#v", result)
	}

	note, err := resolveNote(vault, "APP-T-0007")
	if err != nil {
		t.Fatal(err)
	}
	run, wfFile := waveExecuteDispatchInput(t, store, project, note)
	reachedClaim := false
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, beforeRunLeaseClaim: func(RunStatus) { reachedClaim = true }}
	if _, _, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute); err != nil {
		t.Fatal(err)
	}
	if !reachedClaim {
		t.Fatal("other-wave directive did not exercise the daemon claim boundary")
	}
	assertWaveExecuteNoClaim(t, store, project.ProjectID, run.RecordID)
	directive, err := store.RunDirective(project.ProjectID, run.RecordID)
	if err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("other-wave directive was consumed: %#v err=%v", directive, err)
	}
}

func TestWaveDirectiveClaimRechecksAuthorizationUnderLock(t *testing.T) {
	for _, target := range []string{"disarmed", "stale"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
			installCodexSleepShimForTest(t)
			vault, _, _ := waveExecuteTestFixture(t)
			store, project, server := waveExecuteDaemonTestRuntime(t, vault)
			var result serveWaveExecuteResult
			servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &result)
			if !result.OK || result.Execution == nil || len(result.Execution.QueuedTaskIDs) == 0 {
				t.Fatalf("selected wave execute failed: %#v", result)
			}
			taskID := result.Execution.QueuedTaskIDs[0]
			note, err := resolveNote(vault, taskID)
			if err != nil {
				t.Fatal(err)
			}
			run, wfFile := waveExecuteDispatchInput(t, store, project, note)
			reachedClaim := false
			daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
			daemon.beforeRunLeaseClaim = func(RunStatus) {
				reachedClaim = true
				if target == "disarmed" {
					if err := mutateWaveAuthorization(Args{"vault": vault, "_pos0": "W-0001", "by": "human:test", "quiet": "true"}, "disarmed", nil); err != nil {
						t.Fatal(err)
					}
					return
				}
				writeArmedWaveTestFields(t, vault, map[string]any{"authorization_fingerprint": "sha256:stale"})
			}
			if _, _, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute); err != nil {
				t.Fatal(err)
			}
			if !reachedClaim {
				t.Fatal("authorization mutation did not execute at the daemon claim boundary")
			}
			assertWaveExecuteNoClaim(t, store, project.ProjectID, run.RecordID)
			directive, err := store.RunDirective(project.ProjectID, run.RecordID)
			if err != nil || directive == nil || directive.State != "queued" {
				t.Fatalf("stale directive was consumed: %#v err=%v", directive, err)
			}
		})
	}
}

func TestWaveDescendantIsDurablyReadyBeforeLeaseClaim(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	installCodexSleepShimForTest(t)
	vault, _, _ := waveExecuteTestFixture(t)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "done", "readiness": "done", "proof_status": "satisfied"})
	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"status": "backlog", "readiness": "blocked_by_dependency", "next_owner": "blocked_dependency"})
	store, project, server := waveExecuteDaemonTestRuntime(t, vault)
	var result serveWaveExecuteResult
	servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &result)
	canonical, err := resolveNote(vault, "APP-T-0002")
	if err != nil {
		t.Fatal(err)
	}
	note, _, ok, err := armedWaveDispatchTaskProjection(vault, canonical)
	if err != nil || !ok {
		t.Fatalf("project descendant: ok=%t err=%v", ok, err)
	}
	run, wfFile := waveExecuteDispatchInput(t, store, project, note)
	observed := ""
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, beforeRunLeaseClaim: func(RunStatus) {
		current, resolveErr := resolveNote(vault, "APP-T-0002")
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		observed = stringField(current.Data, "status")
	}}
	if _, _, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", observed, "canonical status at lease claim")
}

func TestServeWaveExecuteDoesNotAuthorizeNewTaskWithoutDirective(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	installCodexSleepShimForTest(t)
	vault, _, _ := waveExecuteTestFixture(t)
	store, project, server := waveExecuteDaemonTestRuntime(t, vault)
	var result serveWaveExecuteResult
	servePost(t, server, "/api/waves/W-0001/execute?project=app", `{}`, &result)
	if !result.OK {
		t.Fatalf("selected wave execute failed: %#v", result)
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Created after execute", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0007")
	note, err := resolveNote(vault, "APP-T-0007")
	if err != nil {
		t.Fatal(err)
	}
	run, wfFile := waveExecuteDispatchInput(t, store, project, note)
	reachedClaim := false
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, beforeRunLeaseClaim: func(RunStatus) { reachedClaim = true }}
	if _, _, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute); err != nil {
		t.Fatal(err)
	}
	if reachedClaim {
		t.Fatal("new task without a directive reached the lease claim")
	}
	assertWaveExecuteNoClaim(t, store, project.ProjectID, run.RecordID)
}

func waveExecuteDaemonTestRuntime(t *testing.T, vault string) (*RuntimeStore, RegisteredProject, *serveServer) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: v7RepoRoot(vault), VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: false, Health: projectHealthDisabled}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	guard, err := acquireDaemonGuard(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = guard.Close() })
	server := newServeServer(vault, project.RepoRoot, defaultServeAddr, store, nil)
	server.operatorActor = "human:test"
	project.Enabled, project.Health = true, projectHealthHealthy
	return store, project, server
}

func waveExecuteDispatchInput(t *testing.T, store *RuntimeStore, project RegisteredProject, note Note) (RunStatus, WorkflowFile) {
	t.Helper()
	wfFile, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Agents.Default = "test-wave-execute"
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, "test-wave-execute")
	wfFile.Data.Runners["test-wave-execute"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: defaultCodexExecCommand()}
	wfFile.Data.Workspace.Root = filepath.Join(DefaultStateRoot(), "workspaces", "wave-execute", newRecordID())
	run := RunStatus{ProjectID: project.ProjectID, RecordID: stringField(note.Data, "id"), ItemID: stringField(note.Data, "id"), Runner: "test-wave-execute", Lane: runLaneExecute, LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone), WorkRevision: intField(note.Data, "work_revision")}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	return run, wfFile
}

func assertWaveExecuteNoClaim(t *testing.T, store *RuntimeStore, projectID, taskID string) {
	t.Helper()
	run := latestRunForRecord(t, store, projectID, taskID)
	if run.LeaseState != string(LeaseStateUnclaimed) || run.LeaseOwner != "" {
		t.Fatalf("unauthorized task acquired a lease: %#v", run)
	}
	attempts, err := store.ListAttemptsForRun(projectID, taskID)
	if err != nil || len(attempts) != 0 {
		t.Fatalf("unauthorized task created attempts: %#v err=%v", attempts, err)
	}
}

func waveExecuteTestFixture(t *testing.T) (string, v7Index, Note) {
	t.Helper()
	vault := deliveryTestVault(t)
	initDispatchGitRepoForTest(t, v7RepoRoot(vault))
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf.Data.Runtime.MaxActiveRunsPerProject = 2
	wf.Data.Workspace.Strategy = string(WorkspaceStrategyWorktree)
	wf.Data.Codex.ApprovalPolicy = "never"
	writeWorkflowForPreflightTest(t, vault, wf.Data, wf.Body)
	if _, err := setProjectLocalConfigWithReadback(vault, "runtime.max_active_runs_per_project", 2); err != nil {
		t.Fatal(err)
	}

	plan := validDeliveryPlan()
	plan.Concurrency = 2
	base := plan.Tasks[0]
	plan.Tasks = []deliveryPlanTask{
		base,
		armedWavePlanTask(base, "parallel-a", []deliveryDependency{{Task: "schema", Kind: "hard"}}),
		armedWavePlanTask(base, "parallel-b", []deliveryDependency{{Task: "schema", Kind: "hard"}}),
		armedWavePlanTask(base, "soft-child", []deliveryDependency{{Task: "parallel-a", Kind: "soft"}}),
		armedWavePlanTask(base, "hard-child", []deliveryDependency{{Task: "parallel-b", Kind: "hard"}}),
		armedWavePlanTask(base, "independent", nil),
	}
	path := writeDeliveryTestPlan(t, vault, plan)
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK {
		t.Fatalf("wave execute fixture is operationally unsafe: %#v", report.Findings)
	}
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "wave": "Drain", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	armWaveForTest(t, vault)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return vault, idx, idx.Waves["W-0001"]
}
