package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRunnerHonorsAgentNextOwner(t *testing.T) {
	wf := defaultWorkflow()
	wf.Agents.Default = string(RunnerCodexExec)
	wf.Agents.Enabled = []string{string(RunnerCodexExec), "chatgpt-browser"}
	note := Note{Data: map[string]any{
		"id":         "APP-T-0001",
		"next_owner": "agent:chatgpt-browser",
	}}

	assertEqual(t, "chatgpt-browser", resolveRunnerForNote(note, wf), "daemon runner resolution")
	assertEqual(t, "chatgpt-browser", automationResolveRunner(note, wf), "automation runner resolution")
}

func TestAutomationExplainJSONReportsBlockers(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human held", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"readiness":  "held",
		"next_owner": "human:pm",
	})
	registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.dispatch_scope", "all_eligible"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := automationExplainCmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK          bool                      `json:"ok"`
		Explanation automationTaskExplanation `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "json ok")
	assertEqual(t, "APP-T-0001", payload.Explanation.ID, "explain id")
	assertEqual(t, false, payload.Explanation.Dispatchable, "dispatchability")
	for _, expected := range []string{"readiness is held", "next_owner is human:pm"} {
		if !containsString(payload.Explanation.Blockers, expected) {
			t.Fatalf("expected blocker %q, got %#v", expected, payload.Explanation.Blockers)
		}
	}
	if payload.Explanation.Runner == "" || payload.Explanation.WorkspacePath == "" {
		t.Fatalf("expected runner and workspace in explanation, got %#v", payload.Explanation)
	}
}

func TestAutomationQueueJSONSplitsEligibleAndBlockedWithoutMutatingLifecycle(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Runnable", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Held", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{
		"readiness":  "held",
		"next_owner": "human:pm",
	})
	registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.dispatch_scope", "all_eligible"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := automationQueueCmd(Args{"vault": vault, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK    bool                  `json:"ok"`
		Queue automationQueueReport `json:"queue"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "json ok")
	if len(payload.Queue.Eligible) != 1 || payload.Queue.Eligible[0].ID != "APP-T-0001" {
		t.Fatalf("expected APP-T-0001 eligible, got %#v", payload.Queue.Eligible)
	}
	if len(payload.Queue.Blocked) != 1 || payload.Queue.Blocked[0].ID != "APP-T-0002" {
		t.Fatalf("expected APP-T-0002 blocked, got %#v", payload.Queue.Blocked)
	}
	if !containsString(payload.Queue.Blocked[0].Blockers, "readiness is held") {
		t.Fatalf("expected held readiness blocker, got %#v", payload.Queue.Blocked[0].Blockers)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "ready", stringField(data, "status"), "queue does not mutate eligible task status")
}

func TestAutomationQueueTextShowsEligibleAndBlocked(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Runnable", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Held", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{
		"readiness":  "held",
		"next_owner": "human:pm",
	})
	registerAutomationTestProject(t, vault)

	output := captureStdout(t, func() {
		if err := automationQueueCmd(Args{"vault": vault}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{"Eligible:", "APP-T-0001", "Blocked:", "APP-T-0002", "readiness is held"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("queue text missing %q:\n%s", expected, output)
		}
	}
}

func TestPlanGateUsesCanonicalStateFromRunnerWorkspace(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Canonical ready", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	workspace := t.TempDir()
	staleVault := filepath.Join(workspace, ".tusker")
	if err := ensureDir(filepath.Join(staleVault, "work", "tasks")); err != nil {
		t.Fatal(err)
	}
	workflow, err := readText(workflowPath(vault))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(staleVault, "WORKFLOW.md"), workflow); err != nil {
		t.Fatal(err)
	}
	staleData, staleBody, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	staleData["status"] = "review"
	staleData["readiness"] = "waiting_on_review"
	staleData["next_owner"] = "reviewer"
	staleData["state_rev"] = v7StateRev(staleData, staleBody)
	staleTask, err := serializeDocument(staleData, staleBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(staleVault, "work", "tasks", "APP-T-0001.md"), staleTask); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
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
		ActiveAttemptID: "attempt-self",
		WorkspacePath:   workspace,
		AttemptCount:    1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatal(err)
		}
	}()
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-self")
	t.Setenv("TUSKER_WORKSPACE", workspace)

	output := captureStdout(t, func() {
		if err := automationPlanCmd(Args{"id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK   bool                   `json:"ok"`
		Plan automationDispatchPlan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "canonical plan ok")
	assertEqual(t, "dispatch", payload.Plan.Decision, "canonical plan decision")
	assertEqual(t, canonicalProjectPath(vault), payload.Plan.Project.VaultRoot, "canonical vault")
	assertEqual(t, true, payload.Plan.Project.Registered, "registered project")
	for _, blocker := range payload.Plan.Blockers {
		if strings.Contains(blocker, "status is review") || strings.Contains(blocker, "waiting_on_review") || strings.Contains(blocker, "existing run is running") {
			t.Fatalf("plan used stale worktree/self-run blocker: %#v", payload.Plan.Blockers)
		}
	}
}

func TestAutomationExplainShowsFanoutPolicyAndConflicts(t *testing.T) {
	vault := automationTestVault(t)
	if err := writeText(managedTuskerConfigPath(vault), strings.TrimSpace(`
schema: tusker.config/v1
project_id: app
automation:
  trigger_states: [ready, rework]
  fanout:
    enabled: true
    max_children: 2
    allowed_child_types: [explorer, worker]
    merge_rule: manual_review
`)+"\n"); err != nil {
		t.Fatal(err)
	}
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Fanout task", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodex), Lane: "fanout:explorer", LeaseState: string(LeaseStateRunning), ActiveAttemptID: "child-1", WorkspacePath: "/tmp/fanout-work"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001#child-2", ItemID: "APP-T-0001", Runner: string(RunnerCodex), Lane: "fanout:worker", LeaseState: string(LeaseStateRunning), ActiveAttemptID: "child-2", WorkspacePath: "/tmp/fanout-work"}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	output := captureStdout(t, func() {
		if err := automationExplainCmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK          bool                      `json:"ok"`
		Explanation automationTaskExplanation `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.Explanation.Fanout.Enabled, "fanout enabled")
	assertEqual(t, 2, payload.Explanation.Fanout.MaxChildren, "fanout max")
	assertEqual(t, "manual_review", payload.Explanation.Fanout.MergeRule, "merge rule")
	if len(payload.Explanation.Fanout.Blockers) == 0 || !strings.Contains(payload.Explanation.Fanout.Blockers[0], "workspace conflict") {
		t.Fatalf("expected fanout workspace conflict, got %#v", payload.Explanation.Fanout.Blockers)
	}
}

func automationTestVault(t *testing.T) string {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	vault := pickupV7TestVault(t)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	return vault
}

func registerAutomationTestProject(t *testing.T, vault string) RegisteredProject {
	t.Helper()
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", true); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(filepath.Dir(vault), vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalActiveRunLimit(10); err != nil {
		t.Fatal(err)
	}
	return project
}

// Legacy automation fixtures exercise behavior other than admission policy.
// Make their broad authority explicit instead of relying on the old implicit
// all-eligible default; production's generated configuration stays armed_waves.
func setAllEligibleDispatchScopeForAutomationTest(t *testing.T, vault string) {
	t.Helper()
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.dispatch_scope", "all_eligible"); err != nil {
		t.Fatal(err)
	}
}

func setAutomationV7TaskFields(t *testing.T, vault, id string, fields map[string]any) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		data[key] = value
	}
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}
