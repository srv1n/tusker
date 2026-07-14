package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateWorkflowRejectsUnknownPerStateCap(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Agents.MaxConcurrentAgentsByState = map[string]int{"made_up": 1}
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "unknown tracker state") {
		t.Fatalf("expected unknown tracker state error, got %v", err)
	}
}

func TestValidateWorkflowRejectsReviewerRiskOverlap(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Reviewer.AutoCloseRisks = []string{"low", "high"}
	wfFile.Data.Reviewer.HumanRequiredRisks = []string{"high", "critical"}
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "appears in both") {
		t.Fatalf("expected reviewer risk overlap error, got %v", err)
	}
}

func TestValidateWorkflowRejectsReviewerRunnerOutsideEnabledAgents(t *testing.T) {
	wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
	wfFile.Data = defaultWorkflow()
	wfFile.Data.Reviewer.Runner = "opencode"
	err := validateWorkflowFile(wfFile)
	if err == nil || !strings.Contains(err.Error(), "reviewer.runner must be listed in agents.enabled") {
		t.Fatalf("expected reviewer runner enabled-agents error, got %v", err)
	}
}

func TestValidateWorkflowRejectsWorkspaceRootOutsideSharedRuntime(t *testing.T) {
	for _, root := range []string{"../.tusker-worktrees", "/tmp/tusker-worktrees", "workspaces-old"} {
		wfFile := WorkflowFile{Path: "WORKFLOW.md", Body: defaultWorkflowMarkdown()}
		wfFile.Data = defaultWorkflow()
		wfFile.Data.Workspace.Strategy = string(WorkspaceStrategyWorktree)
		wfFile.Data.Workspace.Root = root
		err := validateWorkflowFile(wfFile)
		if err == nil || !strings.Contains(err.Error(), "workspace.root must be workspaces") {
			t.Fatalf("workspace root %q should be rejected, got %v", root, err)
		}
	}
}

func TestDefaultWorkflowEnablesRiskAwareReviewer(t *testing.T) {
	wf := defaultWorkflow()
	if !wf.Reviewer.Enabled {
		t.Fatal("expected reviewer policy to be enabled by default")
	}
	if !reviewerMayAutoCloseRisk(wf.Reviewer, "medium") {
		t.Fatal("expected medium risk to be reviewer auto-closeable")
	}
	if !reviewerRequiresHumanRisk(wf.Reviewer, "high") {
		t.Fatal("expected high risk to require human close")
	}
	if wf.Reviewer.Actor != "agent:reviewer/codex" {
		t.Fatalf("expected normalized reviewer actor, got %q", wf.Reviewer.Actor)
	}
	if wf.Reviewer.MaxCycles != 3 {
		t.Fatalf("expected three bounded reviewer cycles, got %d", wf.Reviewer.MaxCycles)
	}
	if !strings.Contains(wf.Reviewer.Prompt, "independent Tusker reviewer") {
		t.Fatalf("expected reviewer prompt to be substantive, got %q", wf.Reviewer.Prompt)
	}
	if !strings.Contains(wf.Reviewer.Prompt, "Risk alone does not justify a human gate") || !strings.Contains(wf.Reviewer.Prompt, "already settled by the task/spec") {
		t.Fatalf("expected reviewer prompt to reserve human attention, got %q", wf.Reviewer.Prompt)
	}
}

func TestCodexPolicyForReviewLaneForcesReadOnlyNever(t *testing.T) {
	policy := codexPolicyForLane(CodexPolicy{
		ApprovalPolicy:    "on-failure",
		ThreadSandbox:     "workspace-write",
		TurnSandboxPolicy: "workspace-write",
	}, runLaneReview)
	if policy.ApprovalPolicy != "never" {
		t.Fatalf("expected reviewer approval policy never, got %q", policy.ApprovalPolicy)
	}
	if policy.ThreadSandbox != "read-only" || policy.TurnSandboxPolicy != "read-only" {
		t.Fatalf("expected reviewer sandbox read-only, got thread=%q turn=%q", policy.ThreadSandbox, policy.TurnSandboxPolicy)
	}

	executePolicy := codexPolicyForLane(CodexPolicy{
		ApprovalPolicy:    "on-failure",
		ThreadSandbox:     "workspace-write",
		TurnSandboxPolicy: "workspace-write",
	}, runLaneExecute)
	if executePolicy.ApprovalPolicy != "on-failure" || executePolicy.TurnSandboxPolicy != "workspace-write" {
		t.Fatalf("execute lane policy should be preserved, got %#v", executePolicy)
	}
}

func TestDefaultWorkflowPromptIncludesCommandBudget(t *testing.T) {
	body := defaultWorkflowMarkdown()
	for _, expected := range []string{
		"## Command budget",
		"path-scoped status/search",
		"build-lock/status commands",
		"command + PASS/FAIL",
		"do not paste raw transcripts",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("default workflow missing %q:\n%s", expected, body)
		}
	}
}

func TestTuskerYamlAutomationOverridesWorkflowControlPlane(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), strings.TrimSpace(`
schema: tusker.config/v1
project_id: app
automation:
  trigger_states: [ready, rework]
  default_runner: codex_exec
  enabled_runners: [codex_exec]
  workspace:
    strategy: clone
  concurrency:
    max_active_runs: 4
    max_active_runs_per_project: 2
    max_continuation_retries: 2
    max_concurrent_by_state:
      rework: 1
  runners:
    codex_exec:
      kind: codex_exec
      command: codex exec --json --skip-git-repo-check -
`)+"\n"); err != nil {
		t.Fatal(err)
	}

	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"ready", "rework"}, wf.Data.Tracker.ActiveStates, "trigger states")
	assertEqual(t, "codex_exec", wf.Data.Agents.Default, "default runner")
	assertEqual(t, []string{"codex_exec"}, wf.Data.Agents.Enabled, "enabled runners")
	assertEqual(t, "clone", wf.Data.Workspace.Strategy, "workspace strategy")
	assertEqual(t, 4, wf.Data.Agents.MaxConcurrentAgents, "global concurrency")
	assertEqual(t, 2, wf.Data.Runtime.MaxActiveRunsPerProject, "project concurrency")
	assertEqual(t, 2, wf.Data.Runtime.MaxContinuationRetries, "continuation retries")
	assertEqual(t, 1, wf.Data.Agents.MaxConcurrentAgentsByState["rework"], "state concurrency")
}

func TestTuskerYamlAutomationRejectsLegacyActiveWithoutProfile(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "tusker.yaml"), "schema: tusker.config/v1\nproject_id: app\nautomation:\n  trigger_states: [active, rework]\n"); err != nil {
		t.Fatal(err)
	}

	_, err := loadWorkflow(vault)
	if err == nil || !strings.Contains(err.Error(), "automation.trigger_states must not include legacy active") {
		t.Fatalf("expected legacy active automation error, got %v", err)
	}
}
