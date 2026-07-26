package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestOneShotDispatchRefusesWithDaemonRequirement(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "One shot", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	registerAutomationTestProject(t, vault)

	for name, run := range map[string]func() error{
		"automation dispatch": func() error { return automationDispatchCmd(Args{"vault": vault, "id": "APP-T-0001"}) },
		"daemon run --once":   func() error { return daemonRunCmd(Args{"once": "true"}) },
		"refresh":             func() error { return refreshCmd(Args{"quiet": "true"}) },
	} {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "resident daemon") || !strings.Contains(err.Error(), "tusker daemon run") {
			t.Fatalf("%s: expected resident daemon refusal, got %v", name, err)
		}
	}
}

func TestEnsureDispatchedV7AttemptBindsRuntimeIdentityIdempotently(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Bound daemon attempt", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	workspace := filepath.Join(t.TempDir(), "workspace")
	worktreeVault := runnerWorktreeVaultPath(workspace, vault)
	if err := copyDirContents(vault, worktreeVault); err != nil {
		t.Fatal(err)
	}
	if err := attemptV7StartCmd(Args{"vault": worktreeVault, "quiet": "true", "id": "APP-T-0001", "attempt-id": "APP-T-0001-A-0001"}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		id, err := ensureDispatchedV7Attempt(vault, "APP-T-0001", "01KXGPRUNTIME0000000000000", runLaneExecute, "codex_exec", workspace, "task/app-t-0001")
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "APP-T-0001-A-0002", id, "bound V7 attempt id")
	}
	attemptDir := filepath.Join(worktreeVault, "attempts", "APP-T-0001")
	entries, err := os.ReadDir(attemptDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[1].Name() != "APP-T-0001-A-0002.md" {
		t.Fatalf("expected prior attempt plus one idempotent daemon binding, got %#v", entries)
	}
	boundPath := filepath.Join(attemptDir, "APP-T-0001-A-0002.md")
	data, body, err := parseFrontmatterMustRead(boundPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "01KXGPRUNTIME0000000000000", stringField(data, "runtime_attempt_id"), "runtime attempt binding")
	assertEqual(t, workspace, stringField(data, "workspace_path"), "attempt workspace")

	data["id"] = "APP-T-0001-A-0003"
	data["state_rev"] = v7StateRev(data, body)
	duplicate, err := serializeDocument(data, body, v7FrontmatterOrder["attempt"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(attemptDir, "APP-T-0001-A-0003.md"), duplicate); err != nil {
		t.Fatal(err)
	}
	_, err = ensureDispatchedV7Attempt(vault, "APP-T-0001", "01KXGPRUNTIME0000000000000", runLaneExecute, "codex_exec", workspace, "task/app-t-0001")
	if err == nil || !strings.Contains(err.Error(), "multiple V7 attempts") {
		t.Fatalf("expected duplicate runtime binding to be rejected, got %v", err)
	}
}

func TestReviewerControlMutationRequiresBoundReviewAttempt(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Reviewer authority", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	workspace := filepath.Join(t.TempDir(), "workspace")
	worktreeVault := runnerWorktreeVaultPath(workspace, vault)
	if err := copyDirContents(vault, worktreeVault); err != nil {
		t.Fatal(err)
	}
	runtimeID := "01KXGPREVIEW00000000000000"
	branch := "task/APP-T-0001"
	if _, err := ensureDispatchedV7Attempt(vault, "APP-T-0001", runtimeID, runLaneReview, "codex_exec", workspace, branch); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", runtimeID)
	t.Setenv("TUSKER_WORKSPACE", workspace)
	t.Setenv("TUSKER_RUN_LANE", runLaneReview)
	args := Args{"id": "APP-T-0001", "by": "reviewer:e2e"}
	if !v7ReviewerControlMutationAllowed(worktreeVault, args, branch) {
		t.Fatal("expected bound review attempt to authorize reviewer control mutation")
	}

	for name, mutate := range map[string]func(Args) (Args, string){
		"wrong actor": func(next Args) (Args, string) { next["by"] = "agent:e2e"; return next, branch },
		"wrong task":  func(next Args) (Args, string) { next["id"] = "APP-T-9999"; return next, branch },
		"wrong branch": func(next Args) (Args, string) {
			return next, "task/APP-T-9999"
		},
	} {
		t.Run(name, func(t *testing.T) {
			next := Args{"id": args["id"], "by": args["by"]}
			next, nextBranch := mutate(next)
			if v7ReviewerControlMutationAllowed(worktreeVault, next, nextBranch) {
				t.Fatal("unexpected reviewer authority")
			}
		})
	}
	t.Setenv("TUSKER_RUN_LANE", runLaneExecute)
	if v7ReviewerControlMutationAllowed(worktreeVault, args, branch) {
		t.Fatal("execute lane must not receive reviewer control authority")
	}
	t.Setenv("TUSKER_RUN_LANE", runLaneReview)
	t.Setenv("TUSKER_ATTEMPT_ID", "01KXGPMISMATCH000000000000")
	if v7ReviewerControlMutationAllowed(worktreeVault, args, branch) {
		t.Fatal("mismatched runtime attempt must not receive reviewer control authority")
	}
}

func TestDispatchConsultsPlanBeforeExecute(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Blocked readiness", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":     "ready",
		"readiness":  "blocked_by_dependency",
		"next_owner": "blocked_dependency",
	})
	project := registerAutomationTestProject(t, vault)
	setAllEligibleDispatchScopeForAutomationTest(t, vault)

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateUnclaimed), run.LeaseState, "blocked task lease state")
	if !strings.Contains(run.LastError, "readiness is blocked_by_dependency") || !strings.Contains(run.LastError, "next_owner is blocked_dependency") {
		t.Fatalf("expected automation-plan blockers in run error, got %#v", run)
	}
}

func TestDaemonQuarantineBrokenProject(t *testing.T) {
	vault := automationTestVault(t)
	healthyRoot := filepath.Dir(vault)
	brokenRoot := filepath.Join(t.TempDir(), "broken")
	brokenVault := filepath.Join(brokenRoot, ".tusker")
	if err := ensureDir(brokenVault); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	broken := RegisteredProject{ProjectID: "aaa-broken", ProjectKey: "broken", Name: "broken", RepoRoot: brokenRoot, VaultRoot: brokenVault, WorkflowPath: workflowPath(brokenVault), Enabled: true, Health: projectHealthHealthy}
	healthy := RegisteredProject{ProjectID: "zzz-healthy", ProjectKey: "healthy", Name: "healthy", RepoRoot: healthyRoot, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(broken); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(healthy); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	target, enabled, err := daemon.serveTarget()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, enabled, "serve target enabled")
	assertEqual(t, healthy.ProjectID, target.project.ProjectID, "serve skips broken project")
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]RegisteredProject{}
	for _, project := range projects {
		byID[project.ProjectID] = project
	}
	assertEqual(t, projectHealthError, byID[broken.ProjectID].Health, "broken project quarantined")
	if !strings.Contains(byID[broken.ProjectID].LastError, "WORKFLOW.md not found") {
		t.Fatalf("expected workflow load error, got %#v", byID[broken.ProjectID])
	}
	assertEqual(t, projectHealthHealthy, byID[healthy.ProjectID].Health, "healthy project remains healthy")
	if strings.TrimSpace(byID[healthy.ProjectID].LastPollAt) == "" {
		t.Fatalf("healthy project should be polled: %#v", byID[healthy.ProjectID])
	}
	status, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	projectHealth, ok := status["project_health"].([]RegisteredProject)
	if !ok || len(projectHealth) != 2 {
		t.Fatalf("daemon status must expose project health rows, got %#v", status["project_health"])
	}
	server := &serveServer{vaultPath: vault, repoRoot: healthyRoot, addr: defaultServeAddr, store: store, now: time.Now}
	response := httptest.NewRecorder()
	server.handleDaemon(response, httptest.NewRequest("GET", "/api/daemon", nil))
	if response.Code != 200 {
		t.Fatalf("/api/daemon status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Projects []RegisteredProject `json:"projects"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Projects) != 2 {
		t.Fatalf("/api/daemon must expose project health rows, got %#v", payload)
	}
}

func TestResumeQuarantinesBrokenRegistration(t *testing.T) {
	vault := automationTestVault(t)
	healthyRoot := filepath.Dir(vault)
	brokenRoot := filepath.Join(t.TempDir(), "broken")
	brokenVault := filepath.Join(brokenRoot, ".tusker")
	if err := ensureDir(brokenVault); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	broken := RegisteredProject{ProjectID: "aaa-broken", ProjectKey: "broken", Name: "broken", RepoRoot: brokenRoot, VaultRoot: brokenVault, WorkflowPath: workflowPath(brokenVault), Enabled: true, Health: projectHealthHealthy}
	healthy := RegisteredProject{ProjectID: "zzz-healthy", ProjectKey: "healthy", Name: "healthy", RepoRoot: healthyRoot, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(broken); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(healthy); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatalf("resume should skip unrelated broken registration: %v", err)
	}

	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]RegisteredProject{}
	for _, project := range projects {
		byID[project.ProjectID] = project
	}
	assertEqual(t, projectHealthError, byID[broken.ProjectID].Health, "broken project quarantined by resume")
	if !strings.Contains(byID[broken.ProjectID].LastError, "WORKFLOW.md not found") {
		t.Fatalf("expected recorded workflow load error, got %#v", byID[broken.ProjectID])
	}
	assertEqual(t, projectHealthHealthy, byID[healthy.ProjectID].Health, "healthy project remains healthy")

	output := captureStdout(t, func() {
		if err := projectsListCmd(Args{}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, broken.ProjectID) || !strings.Contains(output, "error") || !strings.Contains(output, "WORKFLOW.md not found") {
		t.Fatalf("projects list must surface quarantined error, got:\n%s", output)
	}
}

func TestSharedProjectLoaderAllEntryPoints(t *testing.T) {
	requiredSites := []string{
		"automation_commands.go",
		"commands_index.go",
		"commands_open_print.go",
		"daemon.go",
		"daemon_serve.go",
		"sentinel.go",
		"serve_command.go",
	}
	for _, path := range requiredSites {
		body, err := readText(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "loadRegisteredProjects(") {
			t.Fatalf("%s must route registration loading through loadRegisteredProjects", path)
		}
	}

	allowedRawListProjects := map[string]bool{
		"project_loader.go": true,
		"project_prune.go":  true,
		"runtime_store.go":  true,
	}
	if err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		base := filepath.Base(path)
		body, err := readText(path)
		if err != nil {
			return err
		}
		if !allowedRawListProjects[base] && strings.Contains(body, "ListProjects(") {
			t.Fatalf("%s directly enumerates project registrations instead of using loadRegisteredProjects", path)
		}
		if base != "project_loader.go" && strings.Contains(body, "loadWorkflow(project.VaultRoot)") {
			t.Fatalf("%s directly loads a registered project workflow instead of using loadRegisteredProjects", path)
		}
		if base != "project_loader.go" && strings.Contains(body, "listAllNotes(project.VaultRoot)") {
			t.Fatalf("%s directly loads registered project notes instead of using loadRegisteredProjects", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAutoLandArmedWaveRejectsRetargetedProjectRegistration(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	replacementVault := pickupV7TestVault(t)
	if err := writeDefaultWorkflow(replacementVault); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	replacement := project
	replacement.RepoRoot = filepath.Dir(replacementVault)
	replacement.VaultRoot = replacementVault
	replacement.WorkflowPath = workflowPath(replacementVault)
	if err := store.UpsertProject(replacement); err != nil {
		t.Fatal(err)
	}

	_, err = (&Daemon{store: store}).autoLandArmedWaveReviewComplete(project, Note{}, RunStatus{})
	if err == nil || !strings.Contains(err.Error(), "registered project identity changed") {
		t.Fatalf("retargeted registration must fail closed before wave landing, got %v", err)
	}
}

func TestDaemonProjectSelfHeal(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	root := filepath.Join(t.TempDir(), "repo")
	vault := filepath.Join(root, ".tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, projectHealthError, loaded[0].Health, "missing workflow quarantines")
	if err := ensureDir(filepath.Join(vault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(vault, "work", "epics")); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(vault, "work", "gates")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, projectHealthHealthy, loaded[0].Health, "restored workflow heals project")
	assertEqual(t, "", loaded[0].LastError, "healed project clears error")
}

func TestStaleLeaseReleaseForReviewState(t *testing.T) {
	vault := automationTestVault(t)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Reviewer.Enabled = false
	raw, err := yaml.Marshal(wfFile.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+wfFile.Body); err != nil {
		t.Fatal(err)
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review stale run", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":     "review",
		"readiness":  "waiting_on_review",
		"next_owner": "reviewer",
	})
	project := registerAutomationTestProject(t, vault)
	setAllEligibleDispatchScopeForAutomationTest(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRetryQueued),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-review",
		SessionRef:      "session-review",
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "review stale lease state")
	assertEqual(t, string(AttemptOutcomeAbandoned), run.AttemptOutcome, "review stale outcome")
	if !strings.Contains(run.LastError, "status is review") {
		t.Fatalf("expected review blocker release reason, got %#v", run)
	}
	count := countDispatchCapacityProjectRuns(map[string]RunStatus{"APP-T-0001": run})
	assertEqual(t, 0, count, "released review run capacity")
}

func TestRetryLeaseDeadPidReleasedByPollTick(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dead retry", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRetryQueued),
		AttemptOutcome:  string(AttemptOutcomeFailed),
		ActiveAttemptID: "attempt-dead",
		ProcessPID:      deadPIDForTest(),
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "dead retry lease state")
	assertEqual(t, 0, run.ProcessPID, "dead retry pid")
	if !strings.Contains(run.LastError, "released dead retry lease") {
		t.Fatalf("expected release reason, got %#v", run)
	}
	count, err := daemon.store.CountProjectActiveRuns(project.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, count, "active project run count")
	status, err := daemon.store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, intFromAny(status["activeRuns"]), "daemon active run count")
}

func TestDispatchConsultsPlanBeforeExecuteContinuation(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Plan gated", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setV7TaskStateForDaemonTest(t, vault, "APP-T-0001", "review", "waiting_on_review", "reviewer")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         string(RunnerCodex),
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateRetryQueued),
		AttemptOutcome: string(AttemptOutcomeNone),
		SessionRef:     "session-plan-gated",
		AttemptCount:   1,
		NextRetryAt:    now,
		UpdatedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "plan-gated review continuation lease")
	if !strings.Contains(run.LastError, "automation plan do_not_dispatch") || !strings.Contains(run.LastError, "status is review") {
		t.Fatalf("expected automation-plan skip reason, got %#v", run)
	}
}

func TestStaleLeaseReleaseForNonDispatchableTaskStates(t *testing.T) {
	cases := []struct {
		name      string
		status    string
		readiness string
		owner     string
	}{
		{name: "done", status: "done", readiness: "done", owner: "human"},
		{name: "backlog", status: "backlog", readiness: "not_ready", owner: "human"},
		{name: "blocked", status: "ready", readiness: "blocked_dependency", owner: "agent:codex"},
		{name: "review reviewer", status: "review", readiness: "waiting_on_review", owner: "reviewer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vault := automationTestVault(t)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stale lease", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
			setV7TaskStateForDaemonTest(t, vault, "APP-T-0001", tc.status, tc.readiness, tc.owner)
			project := registerAutomationTestProject(t, vault)
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpsertRun(RunStatus{
				ProjectID:       project.ProjectID,
				RecordID:        "APP-T-0001",
				ItemID:          "APP-T-0001",
				Runner:          string(RunnerCodex),
				Lane:            runLaneExecute,
				LeaseState:      string(LeaseStateRunning),
				AttemptOutcome:  string(AttemptOutcomeNone),
				ActiveAttemptID: "attempt-stale",
				AttemptCount:    1,
			}); err != nil {
				t.Fatal(err)
			}
			_ = store.Close()

			daemon, err := NewDaemon(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			if err := daemon.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
			if isDispatchCapacityLeaseState(run.LeaseState) {
				t.Fatalf("expected stale %s lease to leave capacity, got %#v", tc.name, run)
			}
			if !strings.Contains(run.LastError, "automation plan do_not_dispatch") {
				t.Fatalf("expected stale release to name automation plan, got %#v", run)
			}
		})
	}
}

func TestRunsReleaseClearsDeadRun(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		ProcessPID:      deadPIDForTest(),
		AttemptCount:    1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	output := captureStdout(t, func() {
		if err := runsReleaseCmd(Args{"id": "ITEM-1", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK       bool `json:"ok"`
		Released bool `json:"released"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "release ok")
	assertEqual(t, true, payload.Released, "release flag")
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRun("ITEM-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "released run state")
	assertEqual(t, 0, run.ProcessPID, "released pid")
	assertEqual(t, "", run.ActiveAttemptID, "released active attempt")
}

func TestRunsRetireTerminalizesStaleOverCapRunAndClearsCircuit(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Retire stale", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	eventPath := filepath.Join(t.TempDir(), "events.jsonl")
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "attempt-retire", EventSinkPath: eventPath, AttemptCount: 99, UpdatedAt: now.Format(time.RFC3339)}
	mustUpsertRun(t, store, run)
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	status, err := daemon.refreshInvariantCircuitStatus(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckAttemptCountWithinCaps}, "", "2026-07-06T12:00:01Z", now, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, status.Open, "circuit open before retire")
	if !strings.Contains(invariantCircuitSummary(status), "tusker runs retire") {
		t.Fatalf("circuit summary must name retire repair, got %q", invariantCircuitSummary(status))
	}
	_ = store.Close()

	output := captureStdout(t, func() {
		if err := runsRetireCmd(Args{"id": "APP-T-0001", "by": "human:sarav", "reason": "legacy run exceeded retry cap", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK             bool   `json:"ok"`
		Retired        bool   `json:"retired"`
		AttemptOutcome string `json:"attempt_outcome"`
		Terminal       bool   `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "retire ok")
	assertEqual(t, true, payload.Retired, "retired flag")
	assertEqual(t, string(AttemptOutcomeAbandoned), payload.AttemptOutcome, "default retired outcome")
	assertEqual(t, true, payload.Terminal, "retired terminal")

	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	retired, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateReleased), retired.LeaseState, "retired lease")
	assertEqual(t, string(AttemptOutcomeAbandoned), retired.AttemptOutcome, "retired outcome")
	assertEqual(t, true, retired.Terminal, "retired terminal record")
	assertEqual(t, 0, retired.ProcessPID, "retired pid cleared")
	eventsText, err := readText(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eventsText, "run_retired") || !strings.Contains(eventsText, "human:sarav") || !strings.Contains(eventsText, "legacy run exceeded retry cap") {
		t.Fatalf("retire event missing actor/reason: %s", eventsText)
	}
	daemon = &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatal(err)
	}
	closed, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, closed.Open, "circuit closed after retire")
}

func TestRunsRetireLiveGuard(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	pid := os.Getpid()
	if err := store.UpsertRun(RunStatus{
		ProjectID:        "project-1",
		RecordID:         "record-live",
		ItemID:           "ITEM-LIVE",
		Runner:           string(RunnerCodexAppServer),
		Lane:             runLaneExecute,
		LeaseState:       string(LeaseStateRunning),
		AttemptOutcome:   string(AttemptOutcomeFailed),
		ActiveAttemptID:  "attempt-live",
		ProcessPID:       pid,
		ProcessPGID:      processGroupID(pid),
		ProcessStartedAt: recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339)),
		LastHeartbeatAt:  time.Now().UTC().Format(time.RFC3339),
		AttemptCount:     1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	err = runsRetireCmd(Args{"id": "ITEM-LIVE", "by": "human:sarav", "reason": "operator override"})
	if err == nil || !strings.Contains(err.Error(), "tusker runs interrupt") {
		t.Fatalf("expected live guard with interrupt hint, got %v", err)
	}
	output := captureStdout(t, func() {
		if err := runsRetireCmd(Args{"id": "ITEM-LIVE", "by": "human:sarav", "reason": "operator override", "force": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK             bool   `json:"ok"`
		Retired        bool   `json:"retired"`
		AttemptOutcome string `json:"attempt_outcome"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "force retire ok")
	assertEqual(t, true, payload.Retired, "force retired")
	assertEqual(t, string(AttemptOutcomeFailed), payload.AttemptOutcome, "force retire preserves outcome")
	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	retired, err := store.FindRun("ITEM-LIVE")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, retired.Terminal, "force retired terminal")
	assertEqual(t, 0, retired.ProcessPID, "force retired pid cleared")
}

func TestInterruptDeadRunWithoutLiveHandleReleases(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       "project-1",
		RecordID:        "record-1",
		ItemID:          "ITEM-1",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		ProcessPID:      deadPIDForTest(),
		AttemptCount:    1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	daemon, err := NewDaemon(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.InterruptRun(context.Background(), "ITEM-1"); err != nil {
		t.Fatal(err)
	}
	run, err := daemon.store.FindRun("ITEM-1")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateInterrupted), run.LeaseState, "interrupted dead run state")
	assertEqual(t, 0, run.ProcessPID, "interrupted dead pid")
	assertEqual(t, "interrupt requested by operator; live runner handle not found and process is not running", run.LastError, "interrupted dead run reason")
}

func TestWorkspaceStrategySharedDefaultUsesRepoRootAndExemptsTuskerBookkeeping(t *testing.T) {
	wf := defaultWorkflow()
	assertEqual(t, string(WorkspaceStrategyShared), wf.Workspace.Strategy, "default workspace strategy")

	repo := t.TempDir()
	if _, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if err := ensureDir(filepath.Join(repo, ".tusker", "work")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(repo, ".tusker", "work", "runtime.md"), "state churn\n"); err != nil {
		t.Fatal(err)
	}
	manager := NewWorkspaceManager()
	result, err := manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: repo, StateRoot: filepath.Join(t.TempDir(), "state"), Strategy: WorkspaceStrategyShared,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, repo, result.Path, "shared path")
	assertEqual(t, string(WorkspaceStrategyShared), result.Metadata.Strategy, "shared metadata")
	assertExists(t, filepath.Join(repo, ".tusker", "work", "runtime.md"))

	dirtyRepo := t.TempDir()
	if _, err := exec.Command("git", "-C", dirtyRepo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(dirtyRepo, "main.go"), "package main\n"); err != nil {
		t.Fatal(err)
	}
	_, err = manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0002", ItemID: "APP-T-0002",
		RepoRoot: dirtyRepo, StateRoot: filepath.Join(t.TempDir(), "state"), Strategy: WorkspaceStrategyShared,
	})
	if err == nil || !strings.Contains(err.Error(), "clean working tree outside .tusker") {
		t.Fatalf("expected dirty shared refusal, got %v", err)
	}

	assertEqual(t, WorkspaceStrategyShared, normalizeWorkspaceStrategy(WorkspaceStrategyInPlace), "legacy in_place migrates to shared")
}

func TestWorkspaceRootHonoredForWorktreeStrategy(t *testing.T) {
	repo := t.TempDir()
	if _, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	stateRoot := filepath.Join(t.TempDir(), "state")
	root := filepath.Join(stateRoot, "workspaces", "configured")
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: repo, StateRoot: stateRoot, WorkspaceRoot: root, Strategy: WorkspaceStrategyWorktree,
	}
	workspacePath, workspaceRoot, err := workspacePathForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, filepath.Join(root, "APP"), workspaceRoot, "configured workspace root")
	assertEqual(t, filepath.Join(root, "APP", "APP-T-0001"), workspacePath, "configured workspace path")
}

func TestDispatchRecordsProcessIdentity(t *testing.T) {
	vault := automationTestVault(t)
	installCodexSleepShimForTest(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Identity", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	setAllEligibleDispatchScopeForAutomationTest(t, vault)

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	defer killRunProcess(run)
	if run.ProcessPID <= 0 {
		t.Fatalf("expected pid, got %#v", run)
	}
	if run.ProcessPGID <= 0 {
		t.Fatalf("expected pgid, got %#v", run)
	}
	if strings.TrimSpace(run.ProcessStartedAt) == "" {
		t.Fatalf("expected process start time, got %#v", run)
	}
	if !processIdentityMatches(run) {
		t.Fatalf("expected recorded process identity to match live process: %#v", run)
	}
}

func TestFirstEventDeadlineInterruptsNeverStartedRunner(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}

	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodex),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		ProcessPID:      cmd.Process.Pid,
		ProcessPGID:     processGroupID(cmd.Process.Pid),
		AttemptCount:    1,
		StartedAt:       time.Now().UTC().Add(-daemonFirstEventDeadline - time.Minute).Format(time.RFC3339),
		UpdatedAt:       time.Now().UTC().Add(-daemonFirstEventDeadline - time.Minute).Format(time.RFC3339),
	}
	defer killRunProcess(run)

	reconciled, changed, err := daemon.reconcileRun(context.Background(), RegisteredProject{ProjectID: "project-1"}, WorkflowFile{Data: defaultWorkflow()}, run)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected first-event deadline to change run")
	}
	if reconciled.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("expected never-started runner to be retry queued, got %#v", reconciled)
	}
	if !strings.Contains(reconciled.LastError, "never started") {
		t.Fatalf("expected never-started error, got %#v", reconciled)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("expected first-event watchdog to stop process")
	}
}

func TestRetryCircuitBreakerMarksTerminal(t *testing.T) {
	daemon := &Daemon{}
	wf := defaultWorkflow()
	wf.Retry.MaxAttempts = 3
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodex),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-3",
		AttemptCount:    3,
	}
	retried := daemon.scheduleRetry(run, wf, "runner exited with code 1")
	if retried.LeaseState != string(LeaseStateParkedNoProgress) {
		t.Fatalf("expected retry cap to park run, got %#v", retried)
	}
	if !retried.Terminal {
		t.Fatalf("expected terminal retry circuit breaker, got %#v", retried)
	}
	if !strings.Contains(retried.LastError, "attempt cap reached (3): runner exited with code 1") {
		t.Fatalf("expected terminal error to be preserved, got %#v", retried)
	}
}

func TestReclaimParksAtCap(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Dead capped retry", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	workspacePath := t.TempDir()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:        project.ProjectID,
		RecordID:         "APP-T-0001",
		ItemID:           "APP-T-0001",
		Runner:           string(RunnerCodexAppServer),
		Lane:             runLaneExecute,
		LeaseState:       string(LeaseStateRetryQueued),
		AttemptOutcome:   string(AttemptOutcomeNone),
		ActiveAttemptID:  "attempt-3",
		SessionRef:       "session-3",
		WorkspacePath:    workspacePath,
		ProcessPID:       deadPIDForTest(),
		ProcessStartedAt: "1900-01-01T00:00:00Z",
		AttemptCount:     3,
		NextRetryAt:      "2026-07-06T00:00:00Z",
		UpdatedAt:        "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{
		ProjectID:     project.ProjectID,
		RecordID:      "APP-T-0001",
		Runner:        string(RunnerCodexAppServer),
		SessionRef:    "session-3",
		WorkspacePath: workspacePath,
		WorkRevision:  0,
		LastAttemptID: "attempt-3",
		State:         string(LeaseStateRetryQueued),
		Resumable:     true,
		StartedAt:     "2026-07-06T00:00:00Z",
		LastSeenAt:    "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateParkedNoProgress), run.LeaseState, "reclaim cap lease")
	assertEqual(t, string(AttemptOutcomeBlocked), run.AttemptOutcome, "reclaim cap outcome")
	assertEqual(t, true, run.Terminal, "reclaim cap terminal")
	assertEqual(t, 3, run.AttemptCount, "reclaim must not create attempt")
	if !strings.Contains(run.LastError, "attempt cap reached (3)") || !strings.Contains(run.LastError, "reclaim would create another attempt") {
		t.Fatalf("expected reclaim cap reason, got %#v", run)
	}
	violations := sentinelAttemptCountWithinCaps(runtimeSentinelProjectSnapshot{Project: project, Workflow: defaultWorkflow()}, []RunStatus{run})
	if len(violations) != 0 {
		t.Fatalf("expected sentinel to stay green, got %#v", violations)
	}
	decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) == 0 || decisions[len(decisions)-1].Kind != string(SupervisorDecisionStopForAudit) {
		t.Fatalf("expected stop-for-audit decision, got %#v", decisions)
	}
}

func TestSingleCapCheckAllPaths(t *testing.T) {
	raw, err := os.ReadFile("daemon.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	assertContainsIndexTest(t, source, "func (d *Daemon) enforceAttemptCreationCap(")
	for _, marker := range []string{
		"attemptCreationKindForDispatch(run), \"dispatch would create another attempt\"",
		"attemptCreationReclaim, reason+\"; reclaim would create another attempt\"",
		"attemptCreationContinuation, reason",
		"attemptCreationRetry, reason",
		"attemptCreationRedrive",
	} {
		if !strings.Contains(source, marker) {
			t.Fatalf("missing shared cap check marker %q", marker)
		}
	}
	if strings.Contains(source, "run.AttemptCount >= wf.Retry.MaxAttempts") ||
		strings.Contains(source, "run.AttemptCount < wfFile.Data.Retry.MaxAttempts") {
		t.Fatalf("attempt creation cap checks must route through enforceAttemptCreationCap")
	}
}

func TestContinuationRetryCapParksNoProgress(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	wf := defaultWorkflow()
	wf.Runtime.MaxContinuationRetries = 2
	run := RunStatus{
		ProjectID:       "project-1",
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-1",
		SessionRef:      "session-1",
		AttemptCount:    1,
	}
	for i := 0; i < 2; i++ {
		if _, err := store.SaveSupervisorDecision(SupervisorDecision{
			ProjectID:        run.ProjectID,
			RecordID:         run.RecordID,
			AttemptID:        run.ActiveAttemptID,
			SessionRef:       run.SessionRef,
			Kind:             string(SupervisorDecisionContinueThread),
			Reason:           "session is resumable",
			ParentAttemptID:  run.ActiveAttemptID,
			ParentSessionRef: run.SessionRef,
		}); err != nil {
			t.Fatal(err)
		}
	}
	parked, queued := daemon.scheduleContinuationRetry(run, wf, "session is resumable")
	assertEqual(t, false, queued, "continuation queued")
	assertEqual(t, string(LeaseStateParkedNoProgress), parked.LeaseState, "parked lease state")
	assertEqual(t, string(AttemptOutcomeBlocked), parked.AttemptOutcome, "parked outcome")
	assertEqual(t, true, parked.Terminal, "parked terminal")
	assertEqual(t, false, isDispatchCapacityLeaseState(parked.LeaseState), "parked capacity")
	if !strings.Contains(parked.LastError, "continuation retry cap reached") {
		t.Fatalf("expected cap reason, got %#v", parked)
	}
	if err := store.UpsertRun(parked); err != nil {
		t.Fatal(err)
	}
	status, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, intFromAny(status["parkedNoProgressRuns"]), "parked status count")
}

func TestCleanFinishReleasesLeaseBothRunnerLanes(t *testing.T) {
	for _, tc := range []struct {
		name string
		lane string
	}{
		{name: "execute handoff to review", lane: runLaneExecute},
		{name: "review handoff", lane: runLaneReview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := automationTestVault(t)
			disableReviewerForTest(t, vault)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Clean finish", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
			if _, err := upsertV7Verification(vault, "APP-T-0001", v7VerificationRow{CoverText: "A1", Check: "command: true", Result: "pass", Notes: "fixture proof"}, "agent:test"); err != nil {
				t.Fatal(err)
			}
			setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
				"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer",
				"source_sha": "fixture-source", "work_revision": 1,
			})
			project := registerAutomationTestProject(t, vault)
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			statusPath := filepath.Join(t.TempDir(), "runner.status.json")
			if err := writeRunnerStatusFile(statusPath, 0); err != nil {
				t.Fatal(err)
			}
			workspacePath := t.TempDir()
			if tc.lane != runLaneReview {
				workspacePath = orchestrationGitRepo(t)
			}
			if err := store.UpsertRun(RunStatus{
				ProjectID:       project.ProjectID,
				RecordID:        "APP-T-0001",
				ItemID:          "APP-T-0001",
				Runner:          string(RunnerClaude),
				RunnerProfile:   "review-fixture",
				Lane:            tc.lane,
				LeaseState:      string(LeaseStateRunning),
				AttemptOutcome:  string(AttemptOutcomeNone),
				ActiveAttemptID: "attempt-clean",
				SessionRef:      "session-clean",
				WorkspacePath:   workspacePath,
				StatusPath:      statusPath,
				WorkRevision:    1,
				AttemptCount:    1,
				UpdatedAt:       "2026-07-06T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
			if tc.lane == runLaneReview {
				note, err := resolveV7Note(vault, "APP-T-0001", "task")
				if err != nil {
					t.Fatal(err)
				}
				proof, gates, err := reviewObjectiveSnapshots(vault, note)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.SaveAttempt(RunAttempt{
					AttemptID: "attempt-clean", ProjectID: project.ProjectID,
					RecordID: "APP-T-0001", ItemID: "APP-T-0001",
					Runner: string(RunnerClaude), Lane: runLaneReview, WorkRevision: 1,
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := store.SaveReviewResult(ReviewResult{
					Schema: reviewResultSchemaV2, ProjectID: project.ProjectID, TaskID: "APP-T-0001",
					TaskStateRev: stringField(note.Data, "state_rev"), WorkRevision: 1,
					ImplementationSHA: "fixture-source", AttemptID: "attempt-clean",
					Actor: "reviewer:agent", Runner: string(RunnerClaude), RunnerProfile: "review-fixture",
					ProofFingerprint: proof, GateFingerprint: gates,
					Verdict: "changes_requested", Summary: "generic typed review transport",
					Findings:  []string{"fixture finding"},
					CreatedAt: "2026-07-25T10:00:00Z",
				}); err != nil {
					t.Fatal(err)
				}
			}
			_ = store.Close()
			daemon, err := NewDaemon(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			if err := daemon.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
			assertEqual(t, string(LeaseStateReleased), run.LeaseState, "clean finish lease state")
			assertEqual(t, string(AttemptOutcomeSucceeded), run.AttemptOutcome, "clean finish outcome")
			assertEqual(t, "", run.NextRetryAt, "clean finish retry")
			decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
			if err != nil {
				t.Fatal(err)
			}
			for _, decision := range decisions {
				if decision.Kind == string(SupervisorDecisionContinueAttempt) || decision.Kind == string(SupervisorDecisionContinueThread) {
					t.Fatalf("clean finish queued continuation: %#v", decision)
				}
			}
		})
	}
}

func TestMalformedRunnerStatusFailsOnlyTheAttempt(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Malformed runner status", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeText(statusPath, "{\"exit_code\":1}\n}\n"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-malformed-status",
		WorkspacePath:   t.TempDir(),
		StatusPath:      statusPath,
		AttemptCount:    1,
		UpdatedAt:       "2026-07-17T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatalf("malformed runner status crashed daemon poll: %v", err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if run.LeaseState == string(LeaseStateRunning) || run.LeaseState == string(LeaseStateClaimed) {
		t.Fatalf("malformed runner status left a live lease: %#v", run)
	}
	if !strings.Contains(run.LastError, "runner status is malformed") {
		t.Fatalf("malformed status reason was not preserved: %#v", run)
	}
}

func TestEarlyExitClassification(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Early exit", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	wf := defaultWorkflow()
	wf.Runtime.MaxContinuationRetries = 1
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n\n## Routing\n\nTest.\n\n## Prompt\n\nTest prompt.\n\n## Retry policy\n\nRetry transient failures.\n\n## Human override policy\n\nHumans may override.\n"); err != nil {
		t.Fatal(err)
	}
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	workspacePath := t.TempDir()
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-early",
		SessionRef:      "session-early",
		WorkspacePath:   workspacePath,
		StatusPath:      statusPath,
		AttemptCount:    2,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateParkedNoProgress), run.LeaseState, "early exit cap lease")
	assertEqual(t, string(AttemptOutcomeBlocked), run.AttemptOutcome, "early exit cap outcome")
	assertEqual(t, true, run.Terminal, "early exit cap terminal")
	if !strings.Contains(run.LastError, "continuation retry cap reached (1): "+runnerEarlyExitActiveTrackerReason) {
		t.Fatalf("expected early-exit cap reason, got %#v", run)
	}
	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected one attempt, got %#v", attempts)
	}
	assertEqual(t, "attempt-early", attempts[0].AttemptID, "early exit attempt id")
	assertEqual(t, string(AttemptOutcomeEarlyExit), attempts[0].Outcome, "early exit attempt outcome")
	assertEqual(t, 0, attempts[0].ExitCode, "early exit process code")
	if attempts[0].LastError != runnerEarlyExitActiveTrackerReason {
		t.Fatalf("expected early-exit attempt reason, got %#v", attempts[0])
	}
	decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) == 0 || decisions[len(decisions)-1].Kind != string(SupervisorDecisionStopForAudit) {
		t.Fatalf("expected cap to stop for audit, got %#v", decisions)
	}
	if !strings.Contains(decisions[len(decisions)-1].Reason, runnerEarlyExitActiveTrackerReason) {
		t.Fatalf("expected early-exit decision reason, got %#v", decisions[len(decisions)-1])
	}
}

func TestDispatchDeclinedOutcomeReleasesWithoutContinuation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		fields        map[string]any
		blockers      []string
		wantTerminal  bool
		wantInLastErr string
	}{
		{
			name:          "active task",
			blockers:      []string{"status is review", "readiness is waiting_on_review", "next_owner is reviewer"},
			wantInLastErr: "status is review",
		},
		{
			name: "canonical terminal",
			fields: map[string]any{
				"status":     "done",
				"readiness":  "done",
				"next_owner": "none",
			},
			blockers:      []string{"status is done", "readiness is done", "next_owner is none"},
			wantTerminal:  true,
			wantInLastErr: "status is done",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vault := automationTestVault(t)
			disableReviewerForTest(t, vault)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Declined", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
			if len(tc.fields) > 0 {
				setAutomationV7TaskFields(t, vault, "APP-T-0001", tc.fields)
			}
			project := registerAutomationTestProject(t, vault)
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			runDir := t.TempDir()
			statusPath := filepath.Join(runDir, "runner.status.json")
			rawLogPath := filepath.Join(runDir, "runner.raw.log")
			if err := writeRunnerStatusFile(statusPath, 0); err != nil {
				t.Fatal(err)
			}
			writeAutomationPlanDeclineRawLog(t, rawLogPath, "APP-T-0001", tc.blockers)
			rawLine, err := readText(rawLogPath)
			if err != nil {
				t.Fatal(err)
			}
			command, output, ok := rawLogCommandExecution(strings.TrimSpace(rawLine))
			if !ok {
				t.Fatalf("decline fixture command did not parse: line=%s", strings.TrimSpace(rawLine))
			}
			if !strings.Contains(command, "tusker automation plan") || !strings.Contains(output, "do_not_dispatch") {
				t.Fatalf("decline fixture command/output mismatch: command=%q output=%q", command, output)
			}
			if reason, ok := runnerRawLogDispatchDeclineReason(rawLogPath); !ok || !strings.Contains(reason, tc.wantInLastErr) {
				t.Fatalf("decline fixture did not parse: ok=%t reason=%q", ok, reason)
			}
			if err := store.UpsertRun(RunStatus{
				ProjectID:       project.ProjectID,
				RecordID:        "APP-T-0001",
				ItemID:          "APP-T-0001",
				Runner:          string(RunnerCodexExec),
				Lane:            runLaneExecute,
				LeaseState:      string(LeaseStateRunning),
				AttemptOutcome:  string(AttemptOutcomeNone),
				ActiveAttemptID: "attempt-declined",
				SessionRef:      "session-declined",
				WorkspacePath:   t.TempDir(),
				StatusPath:      statusPath,
				RawLogPath:      rawLogPath,
				AttemptCount:    1,
				UpdatedAt:       "2026-07-06T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
			_ = store.Close()

			daemon, err := NewDaemon(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			if err := daemon.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
			assertEqual(t, string(LeaseStateReleased), run.LeaseState, "declined lease")
			assertEqual(t, string(AttemptOutcomeDispatchDeclined), run.AttemptOutcome, "declined outcome")
			assertEqual(t, "", run.NextRetryAt, "declined retry")
			assertEqual(t, 1, run.AttemptCount, "declined attempt count")
			assertEqual(t, tc.wantTerminal, run.Terminal, "declined terminal")
			if !strings.Contains(run.LastError, tc.wantInLastErr) {
				t.Fatalf("expected blocker text %q in run error, got %#v", tc.wantInLastErr, run)
			}
			attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) != 1 {
				t.Fatalf("expected one declined attempt, got %#v", attempts)
			}
			assertEqual(t, string(AttemptOutcomeDispatchDeclined), attempts[0].Outcome, "declined attempt outcome")
			assertEqual(t, 0, attempts[0].ExitCode, "declined exit code")
			if !strings.Contains(attempts[0].LastError, tc.wantInLastErr) {
				t.Fatalf("expected blocker text %q in attempt error, got %#v", tc.wantInLastErr, attempts[0])
			}
			decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
			if err != nil {
				t.Fatal(err)
			}
			if len(decisions) == 0 || decisions[len(decisions)-1].Kind != string(SupervisorDecisionStopForAudit) {
				t.Fatalf("expected declined stop-for-audit decision, got %#v", decisions)
			}
			for _, decision := range decisions {
				if decision.Kind == string(SupervisorDecisionContinueAttempt) || decision.Kind == string(SupervisorDecisionContinueThread) {
					t.Fatalf("declined dispatch queued continuation: %#v", decisions)
				}
			}
		})
	}
}

func writeAutomationPlanDeclineRawLog(t *testing.T, rawLogPath, taskID string, blockers []string) {
	t.Helper()
	planPayload := map[string]any{
		"ok": false,
		"plan": map[string]any{
			"schema":    automationPlanSchema,
			"task":      taskID,
			"record_id": taskID,
			"decision":  "do_not_dispatch",
			"blockers":  blockers,
		},
	}
	planRaw, err := json.Marshal(planPayload)
	if err != nil {
		t.Fatal(err)
	}
	linePayload := map[string]any{
		"method": "item/completed",
		"params": map[string]any{
			"item": map[string]any{
				"type":             "commandExecution",
				"command":          "/bin/zsh -lc 'rtk tusker automation plan " + taskID + " --json'",
				"aggregatedOutput": string(planRaw) + "\n",
				"exitCode":         0,
				"status":           "completed",
			},
		},
	}
	lineRaw, err := json.Marshal(linePayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(rawLogPath, string(lineRaw)+"\n"); err != nil {
		t.Fatal(err)
	}
}

func TestContinuationCapParksAllLanes(t *testing.T) {
	for _, runner := range []RunnerName{RunnerCodexAppServer, RunnerCodexExec, RunnerClaude} {
		t.Run(string(runner), func(t *testing.T) {
			vault := automationTestVault(t)
			disableReviewerForTest(t, vault)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Continuation cap", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
			project := registerAutomationTestProject(t, vault)
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.UpsertRun(RunStatus{
				ProjectID:       project.ProjectID,
				RecordID:        "APP-T-0001",
				ItemID:          "APP-T-0001",
				Runner:          string(runner),
				Lane:            runLaneExecute,
				LeaseState:      string(LeaseStateRetryQueued),
				AttemptOutcome:  string(AttemptOutcomeNone),
				ActiveAttemptID: "attempt-4",
				SessionRef:      "session-4",
				AttemptCount:    4,
				NextRetryAt:     "2026-07-06T00:00:00Z",
				UpdatedAt:       "2026-07-06T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
			_ = store.Close()
			daemon, err := NewDaemon(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			if err := daemon.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
			assertEqual(t, string(LeaseStateParkedNoProgress), run.LeaseState, "cap lease state")
			assertEqual(t, string(AttemptOutcomeBlocked), run.AttemptOutcome, "cap outcome")
			assertEqual(t, true, run.Terminal, "cap terminal")
			if !strings.Contains(run.LastError, "continuation retry cap reached (3)") {
				t.Fatalf("expected cap reason, got %#v", run)
			}
			decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
			if err != nil {
				t.Fatal(err)
			}
			if len(decisions) == 0 || decisions[len(decisions)-1].Kind != string(SupervisorDecisionStopForAudit) {
				t.Fatalf("expected stop-for-audit decision, got %#v", decisions)
			}
		})
	}
}

func TestDaemonStopCommandStopsResidentDaemon(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	stateRoot, err := os.MkdirTemp("", "tusker-stop-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := pickupV7TestVault(t)
	writeDaemonServeWorkflow(t, vault, false, defaultServeAddr)
	registerAutomationTestProject(t, vault)
	done := make(chan error, 1)
	go func() {
		done <- daemonRunCmd(Args{})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if readDaemonLiveness(stateRoot, time.Now().UTC()).Alive && fileExists(daemonSocketPath(stateRoot)) {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("daemon exited before start: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	output := captureStdout(t, func() {
		if err := daemonStopCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK      bool `json:"ok"`
		Stopped bool `json:"stopped"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "stop ok")
	assertEqual(t, true, payload.Stopped, "stop stopped")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon stop did not end run loop")
	}
	if readDaemonLiveness(stateRoot, time.Now().UTC()).Alive {
		t.Fatal("daemon still alive after stop")
	}
}

func deadPIDForTest() int {
	for pid := 999999; pid > 900000; pid-- {
		if !processExists(pid) {
			return pid
		}
	}
	return 999999
}

func setV7TaskStateForDaemonTest(t *testing.T, vault, taskID, status, readiness, owner string) {
	t.Helper()
	taskPath := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = status
	data["readiness"] = readiness
	data["next_owner"] = owner
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	autoReindex(vault)
}

// writeWorktreeReviewFlipForTest builds a worktree-local Tusker vault at the same
// repo-relative location the daemon resolves for a runner workspace, and flips
// the task to review there — mimicking a runner that ran
// `tusker finish --request-review` in its worktree while the canonical vault
// still reads ready (RUN-T-0042). Returns the worktree vault path.
func writeWorktreeReviewFlipForTest(t *testing.T, canonicalVault, workspace, taskID string) string {
	t.Helper()
	worktreeVault := runnerWorktreeVaultPath(workspace, canonicalVault)
	if err := ensureDir(filepath.Join(worktreeVault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	workflow, err := readText(workflowPath(canonicalVault))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(worktreeVault), workflow); err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(filepath.Join(canonicalVault, "work", "tasks", taskID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "review"
	data["readiness"] = "waiting_on_review"
	data["next_owner"] = "reviewer"
	data["proof_status"] = "satisfied"
	data["review_requested_at"] = "2026-07-08T00:00:00Z"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(worktreeVault, "work", "tasks", taskID+".md"), content); err != nil {
		t.Fatal(err)
	}
	return worktreeVault
}

func writeReviewCompleteWorkflowForTest(t *testing.T, vault string, maxContinuation int) {
	t.Helper()
	wf := defaultWorkflow()
	wf.Runtime.MaxContinuationRetries = maxContinuation
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n\n## Routing\n\nTest.\n\n## Prompt\n\nTest prompt.\n\n## Retry policy\n\nRetry transient failures.\n\n## Human override policy\n\nHumans may override.\n"); err != nil {
		t.Fatal(err)
	}
}

// TestReviewCompleteExitTerminal covers RUN-T-0042 A1: a runner that exits clean
// after flipping its worktree tracker to review while canonical is still ready
// scores a terminal review-complete outcome (not early_exit) and queues no
// continuation, even sitting at/above the continuation cap.
func TestReviewCompleteExitTerminal(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review complete", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	writeReviewCompleteWorkflowForTest(t, vault, 1)
	project := registerAutomationTestProject(t, vault)

	workspace := t.TempDir()
	writeWorktreeReviewFlipForTest(t, vault, workspace, "APP-T-0001")

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-review",
		SessionRef:      "session-review",
		WorkspacePath:   workspace,
		StatusPath:      statusPath,
		AttemptCount:    2,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "review-complete lease released")
	assertEqual(t, string(AttemptOutcomeWaitingForReview), run.AttemptOutcome, "review-complete outcome")
	assertEqual(t, "", run.NextRetryAt, "review-complete no retry")
	assertEqual(t, false, run.Terminal, "review-complete not terminal")
	if strings.Contains(run.LastError, runnerEarlyExitActiveTrackerReason) {
		t.Fatalf("review-complete run misclassified as early exit: %#v", run)
	}
	if !strings.Contains(run.LastError, "awaiting land") {
		t.Fatalf("expected awaiting-land reason, got %#v", run)
	}

	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected one attempt, got %#v", attempts)
	}
	assertEqual(t, string(AttemptOutcomeWaitingForReview), attempts[0].Outcome, "review-complete attempt outcome")
	assertEqual(t, 0, attempts[0].ExitCode, "review-complete attempt exit code")
	if attempts[0].Outcome == string(AttemptOutcomeEarlyExit) {
		t.Fatalf("review-complete attempt scored early_exit: %#v", attempts[0])
	}

	decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range decisions {
		if decision.Kind == string(SupervisorDecisionContinueAttempt) || decision.Kind == string(SupervisorDecisionContinueThread) {
			t.Fatalf("review-complete queued a continuation: %#v", decisions)
		}
	}
	if len(decisions) == 0 || decisions[len(decisions)-1].Kind != string(SupervisorDecisionStopForHuman) {
		t.Fatalf("expected review-complete stop-for-human decision, got %#v", decisions)
	}
}

// TestPendingReviewNotRedispatched covers RUN-T-0042 A2: a task whose worktree
// holds a pending, unlanded review flip releases its lease terminally and is not
// re-dispatched across subsequent polls, so the row never churns to the park
// guard while canonical stays ready.
func TestPendingReviewNotRedispatched(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Pending review", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	writeReviewCompleteWorkflowForTest(t, vault, 1)
	project := registerAutomationTestProject(t, vault)

	workspace := t.TempDir()
	writeWorktreeReviewFlipForTest(t, vault, workspace, "APP-T-0001")

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-pending",
		SessionRef:      "session-pending",
		WorkspacePath:   workspace,
		StatusPath:      statusPath,
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	for i := 0; i < 3; i++ {
		if err := daemon.PollOnce(context.Background()); err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
		run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
		assertEqual(t, string(LeaseStateReleased), run.LeaseState, "pending-review lease stays released")
		assertEqual(t, string(AttemptOutcomeWaitingForReview), run.AttemptOutcome, "pending-review outcome stays terminal")
		if run.LeaseState == string(LeaseStateParkedNoProgress) || run.LeaseState == string(LeaseStateParkedBudget) {
			t.Fatalf("pending-review row churned to park guard on poll %d: %#v", i, run)
		}
		assertEqual(t, 1, run.AttemptCount, "pending-review attempt count unchanged")
	}

	attempts, err := daemon.store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected exactly one attempt with no re-dispatch, got %#v", attempts)
	}
	decisions, err := daemon.store.ListSupervisorDecisionsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range decisions {
		switch decision.Kind {
		case string(SupervisorDecisionContinueAttempt), string(SupervisorDecisionContinueThread), string(SupervisorDecisionResumeSession), string(SupervisorDecisionRedrive):
			t.Fatalf("pending-review row was re-dispatched/continued: %#v", decisions)
		}
	}
}

// TestOutcomeTextDistinguishesAwaitingLand covers RUN-T-0042 A3: the failure
// classifier separates a review-complete-awaiting-land exit from a genuine
// no-progress early_exit so the UI and post-mortems never read "finished" work
// as "failed".
func TestOutcomeTextDistinguishesAwaitingLand(t *testing.T) {
	reviewRun := RunStatus{
		AttemptOutcome: string(AttemptOutcomeWaitingForReview),
		LastError:      runnerReviewCompleteAwaitingLandReason + " (worktree tracker=review)",
	}
	earlyRun := RunStatus{
		AttemptOutcome: string(AttemptOutcomeEarlyExit),
		LastError:      runnerEarlyExitActiveTrackerReason,
	}

	reviewClass := runtimeFailureClass(reviewRun, nil, nil)
	earlyClass := runtimeFailureClass(earlyRun, nil, nil)

	assertEqual(t, "review_complete", reviewClass, "review-complete failure class")
	assertEqual(t, "runner_early_exit", earlyClass, "early-exit failure class")
	if reviewClass == earlyClass {
		t.Fatalf("review-complete and early_exit share a failure class: %q", reviewClass)
	}
	if reviewRun.LastError == earlyRun.LastError {
		t.Fatalf("review-complete and early_exit share reason text: %q", reviewRun.LastError)
	}
	if strings.Contains(reviewRun.LastError, "early exit") {
		t.Fatalf("review-complete reason still reads as early exit: %q", reviewRun.LastError)
	}
}
