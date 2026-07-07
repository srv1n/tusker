package main

import (
	"context"
	"encoding/json"
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
	if !strings.Contains(run.LastError, "live runner handle not found") {
		t.Fatalf("expected missing handle reason, got %#v", run)
	}
}

func TestWorkspaceStrategyInPlaceDefaultUsesRepoRootAndExemptsTuskerBookkeeping(t *testing.T) {
	wf := defaultWorkflow()
	assertEqual(t, string(WorkspaceStrategyInPlace), wf.Workspace.Strategy, "default workspace strategy")

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
		RepoRoot: repo, StateRoot: filepath.Join(t.TempDir(), "state"), Strategy: WorkspaceStrategyInPlace,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, repo, result.Path, "in_place path")
	assertEqual(t, string(WorkspaceStrategyInPlace), result.Metadata.Strategy, "in_place metadata")
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
		RepoRoot: dirtyRepo, StateRoot: filepath.Join(t.TempDir(), "state"), Strategy: WorkspaceStrategyInPlace,
	})
	if err == nil || !strings.Contains(err.Error(), "clean working tree outside .tusker") {
		t.Fatalf("expected dirty in_place refusal, got %v", err)
	}
}

func TestWorkspaceRootHonoredForWorktreeStrategy(t *testing.T) {
	repo := t.TempDir()
	if _, err := exec.Command("git", "-C", repo, "init").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "configured-workspaces")
	req := WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "APP", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		RepoRoot: repo, StateRoot: filepath.Join(t.TempDir(), "state"), WorkspaceRoot: root, Strategy: WorkspaceStrategyWorktree,
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
	writeCodexSleepWorkflowForCapacityTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Identity", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

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
	if retried.LeaseState != string(LeaseStateReleased) {
		t.Fatalf("expected retry cap to release run, got %#v", retried)
	}
	if !retried.Terminal {
		t.Fatalf("expected terminal retry circuit breaker, got %#v", retried)
	}
	if retried.LastError != "runner exited with code 1" {
		t.Fatalf("expected terminal error to be preserved, got %#v", retried)
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
			setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
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
				Runner:          string(RunnerClaude),
				Lane:            tc.lane,
				LeaseState:      string(LeaseStateRunning),
				AttemptOutcome:  string(AttemptOutcomeNone),
				ActiveAttemptID: "attempt-clean",
				SessionRef:      "session-clean",
				WorkspacePath:   workspacePath,
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
