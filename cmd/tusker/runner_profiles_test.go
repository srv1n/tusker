package main

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedCodexProfileArgumentsPassInstalledCLIParser(t *testing.T) {
	_, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("codex CLI is not installed")
	}
	selected := ResolvedRunnerProfile{Definition: RunnerProfileDefinition{
		Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "medium",
	}}
	command := strings.TrimSuffix(commandForRunnerProfile(defaultCodexExecCommand(), selected), " -") + " --help"
	if output, err := exec.Command("sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("generated codex arguments failed the installed CLI parser: %v\n%s", err, output)
	}
}

func TestProfileConfigParsesAndRejectsUnknownValues(t *testing.T) {
	vault := automationTestVault(t)
	root := filepath.Dir(vault)
	if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), strings.TrimSpace(`
schema: tusker.config/v1
project_id: app
automation:
  profiles:
    docs-fast:
      harness: codex_exec
      model: gpt-5.x
      effort: low
      sandbox:
        mode: workspace-write
        network: true
      subagents:
        allowed: false
        max_concurrent: 0
  default_profile: docs-fast
`)+"\n"); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	profile := wf.Data.RunnerProfiles["docs-fast"]
	assertEqual(t, string(RunnerCodexExec), profile.Harness, "profile harness")
	assertEqual(t, "gpt-5.x", profile.Model, "profile model")
	if profile.Sandbox.Network == nil || !*profile.Sandbox.Network {
		t.Fatalf("expected profile network access true, got %#v", profile.Sandbox.Network)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{"harness", "harness: opencode\n      model: gpt-5.x\n      effort: low", "harness"},
		{"retired app server", "harness: codex_app_server\n      model: gpt-5.x\n      effort: low", "retired"},
		{"model", "harness: codex_exec\n      model: mystery-model\n      effort: low", "model"},
		{"effort", "harness: codex_exec\n      model: gpt-5.x\n      effort: turbo", "effort"},
		{"unenforced guarded preset", "harness: codex_exec\n      model: gpt-5.x\n      effort: high\n      permission_preset: guarded-yolo", "permission_preset"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vault := automationTestVault(t)
			root := filepath.Dir(vault)
			if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), "schema: tusker.config/v1\nproject_id: app\nautomation:\n  profiles:\n    bad:\n      "+tc.body+"\n      sandbox:\n        mode: workspace-write\n"); err != nil {
				t.Fatal(err)
			}
			_, err := loadWorkflow(vault)
			if err == nil {
				t.Fatal("expected invalid profile error")
			}
			typed, ok := err.(*TuskerError)
			if !ok {
				t.Fatalf("expected TuskerError, got %T %v", err, err)
			}
			if !strings.Contains(typed.Message, tc.want) || !strings.Contains(typed.Path, ".tusker/config.yaml") {
				t.Fatalf("expected legible %s error with source path, got message=%q path=%q", tc.want, typed.Message, typed.Path)
			}
		})
	}
}

func TestProfileResolvePrecedenceAndDispatchCommand(t *testing.T) {
	wf := defaultWorkflow()
	wf.Agents.Enabled = []string{string(RunnerCodexExec), string(RunnerCodexAppServer), string(RunnerClaude)}
	wf.RunnerProfiles = map[string]RunnerProfileDefinition{
		"default-cheap": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "low",
			Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)},
		},
		"lane-frontier": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.1", Effort: "medium",
			Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(true)},
		},
		"task-yolo": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.2", Effort: "high", PermissionPreset: "danger-full-access",
			Sandbox: RunnerSandboxDefinition{Mode: "danger-full-access", Network: boolPtr(true)},
		},
		"risk-review": {
			Harness: string(RunnerClaude), Model: "claude-opus-4-8", Effort: "high",
			Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)},
		},
	}
	wf.RunnerDefaultProfile = "default-cheap"
	wf.RunnerLaneProfiles = map[string]string{runLaneExecute: "lane-frontier"}
	wf.RunnerRouting = []RunnerRoutingRule{{Name: "high-risk", Profile: "risk-review", Match: RunnerRoutingMatch{Risk: "high"}}}

	lowRisk := Note{Data: map[string]any{"id": "APP-T-0001", "title": "ordinary", "risk": "low"}}
	selected, err := resolveRunnerProfileForNote(lowRisk, wf, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "lane-frontier", selected.Name, "lane beats default")

	highRisk := Note{Data: map[string]any{"id": "APP-T-0002", "title": "risky", "risk": "high"}}
	selected, err = resolveRunnerProfileForNote(highRisk, wf, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "risk-review", selected.Name, "routing beats lane")
	assertEqual(t, "high-risk", selected.RuleName, "routing rule logged")

	override := Note{Data: map[string]any{"id": "APP-T-0003", "title": "override", "risk": "high", "runner_profile": "task-yolo"}}
	selected, err = resolveRunnerProfileForNote(override, wf, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "task-yolo", selected.Name, "task override beats routing")
	command := commandForRunnerProfile("codex exec --skip-git-repo-check -", selected)
	if !strings.Contains(command, "--model gpt-5.2") || !strings.Contains(command, `-c 'model_reasoning_effort="high"'`) {
		t.Fatalf("expected codex exec command to include model and effort, got %q", command)
	}
	policy := codexPolicyForResolvedProfile(codexPolicyFromWorkflow(wf), runLaneExecute, selected)
	sandbox := codexTurnSandboxPolicy(policy, "/tmp/workspace")
	assertEqual(t, "dangerFullAccess", stringValue(sandbox["type"]), "full access sandbox")
	env := runnerEnv(runnerLaunchEnv{RunnerProfile: selected.Name, RunnerHarness: selected.Definition.Harness, RunnerModel: selected.Definition.Model, RunnerEffort: selected.Definition.Effort, CodexPolicy: policy})
	assertContainsEnv(t, env, "TUSKER_RUNNER_PROFILE=task-yolo")
	assertContainsEnv(t, env, "TUSKER_RUNNER_MODEL=gpt-5.2")
}

func TestProfileLaneExecuteAndReviewCanDiffer(t *testing.T) {
	wf := defaultWorkflow()
	wf.RunnerProfiles = map[string]RunnerProfileDefinition{
		"execute-cheap":   {Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "low", Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(true)}},
		"review-frontier": {Harness: string(RunnerClaude), Model: "claude-opus-4-8", Effort: "high", Sandbox: RunnerSandboxDefinition{Mode: "read-only", Network: boolPtr(false)}},
	}
	wf.RunnerLaneProfiles = map[string]string{runLaneExecute: "execute-cheap", runLaneReview: "review-frontier"}
	note := Note{Data: map[string]any{"id": "APP-T-0001", "title": "same task"}}
	execute, err := resolveRunnerProfileForNote(note, wf, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	review, err := resolveRunnerProfileForNote(note, wf, runLaneReview)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "execute-cheap", execute.Name, "execute profile")
	assertEqual(t, "review-frontier", review.Name, "review profile")
	if execute.Definition.Harness == review.Definition.Harness || execute.Definition.Model == review.Definition.Model {
		t.Fatalf("expected different lane harness/model, got execute=%#v review=%#v", execute, review)
	}
}

func TestProfileRunRecordAndReviewPacketIncludeProfileHarnessModel(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec),
		RunnerProfile: "execute-cheap", RunnerHarness: string(RunnerCodexExec), RunnerModel: "gpt-5.x", RunnerEffort: "low",
		Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1",
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one run, got %#v", runs)
	}
	assertEqual(t, "execute-cheap", runs[0].RunnerProfile, "run profile")
	assertEqual(t, string(RunnerCodexExec), runs[0].RunnerHarness, "run harness")
	assertEqual(t, "gpt-5.x", runs[0].RunnerModel, "run model")
	output := captureStdout(t, func() {
		if err := runsInspectCmd(Args{"id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var inspected runInspection
	if err := json.Unmarshal([]byte(output), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Run == nil {
		t.Fatal("expected inspected run")
	}
	assertEqual(t, "execute-cheap", inspected.Run.RunnerProfile, "runs inspect profile")
	assertEqual(t, "gpt-5.x", inspected.Run.RunnerModel, "runs inspect model")

	packet := renderReviewPacket(Note{Data: map[string]any{"id": "APP-T-0001", "title": "Profiles"}}, run, nil, nil, reviewPacketFacts{})
	for _, expected := range []string{"Runner profile: execute-cheap", "Harness: codex_exec", "Model: gpt-5.x", "Effort: low"} {
		if !strings.Contains(packet, expected) {
			t.Fatalf("review packet missing %q:\n%s", expected, packet)
		}
	}
}

func TestSetterReadbackProjectsLimitsWritesLocalAndRejectsNoop(t *testing.T) {
	vault := automationTestVault(t)
	root := filepath.Dir(vault)
	if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), strings.TrimSpace(`
schema: tusker.config/v1
project_id: app
automation:
  concurrency:
    max_active_runs_per_project: 1
`)+"\n"); err != nil {
		t.Fatal(err)
	}
	registerAutomationTestProject(t, vault)
	output := captureStdout(t, func() {
		if err := projectsLimitsCmd(Args{"vault": vault, "max-active-runs": "4", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK      bool `json:"ok"`
		Project struct {
			MaxActiveRuns int    `json:"max_active_runs"`
			Source        string `json:"source"`
		} `json:"project"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "projects limits ok")
	assertEqual(t, 4, payload.Project.MaxActiveRuns, "project max active runs")
	assertEqual(t, configSourceLocal, payload.Project.Source, "project setter winning source")
	if !fileExists(filepath.Join(vault, "config.local.yaml")) {
		t.Fatal("expected project setter to write managed config.local.yaml")
	}
	if fileExists(managedTuskerLocalConfigPath(filepath.Join(root, defaultRepoVaultDir))) {
		t.Fatal("project setter must not create a root local config")
	}
	if err := projectsLimitsCmd(Args{"vault": vault, "max-active-runs": "4", "json": "true"}); err == nil {
		t.Fatal("expected repeat setter to fail as a no-op")
	}
}

func TestConfigResolverPreservesExplicitZeroFalseAndEmptyCollections(t *testing.T) {
	vault := automationTestVault(t)
	root := filepath.Dir(vault)
	if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), `automation:
  concurrency:
    max_active_runs: 7
    max_concurrent_by_state:
      ready: 3
  fanout:
    enabled: true
    max_children: 9
    allowed_child_types: [task]
  validation:
    commands: [go test ./...]
  denylist:
    - id: inherited
      pattern: inherited
`); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "config.yaml"), `automation:
  concurrency:
    max_active_runs: 0
    max_concurrent_by_state: {}
  fanout:
    enabled: false
    max_children: 0
    allowed_child_types: []
  validation:
    commands: []
  denylist: []
`); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config.Automation.Concurrency.MaxActiveRuns != 0 || resolved.Config.Automation.Fanout.Enabled || resolved.Config.Automation.Fanout.MaxChildren != 0 {
		t.Fatalf("explicit scalar overrides lost: %#v", resolved.Config.Automation)
	}
	if len(resolved.Config.Automation.Fanout.AllowedChildTypes) != 0 || len(resolved.Config.Automation.Validation.Commands) != 0 || len(resolved.Config.Automation.Denylist) != 0 || len(resolved.Config.Automation.Concurrency.MaxConcurrentByState) != 0 {
		t.Fatalf("explicit empty collection did not clear inherited policy: %#v", resolved.Config.Automation)
	}
	report, err := configResolve(vault, "automation.fanout.enabled")
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := report.Value.(bool); !ok || value || report.Path != filepath.Join(vault, "config.yaml") || report.Source != configSourceProject {
		t.Fatalf("effective value and provenance diverged: %#v", report)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.Fanout.Enabled || wf.Data.Fanout.MaxChildren != 0 || len(wf.Data.Fanout.AllowedChildTypes) != 0 || wf.Data.Agents.MaxConcurrentAgents != 0 || len(wf.Data.Agents.MaxConcurrentAgentsByState) != 0 {
		t.Fatalf("workflow consumer ignored explicit cleared policy: %#v", wf.Data)
	}
}

func TestManagedConfigAugmentsLegacyClosePolicyWithoutMaskingIt(t *testing.T) {
	vault := automationTestVault(t)
	root := filepath.Dir(vault)
	if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), `close_policy:
  high:
    required_acceptor: reviewer_agent
    required_evidence: [automated_test]
    required_gates: [security]
branches:
  default_branch: release
mutation_mode: protected
`); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "config.yaml"), `automation:
  enabled: false
branches:
  state_branch: state
`); err != nil {
		t.Fatal(err)
	}
	policy, err := v7ClosePolicyFor(vault, "high")
	if err != nil {
		t.Fatal(err)
	}
	if policy.RequiredAcceptor != "reviewer_agent" || strings.Join(policy.RequiredEvidence, ",") != "automated_test" || strings.Join(policy.RequiredGates, ",") != "security" {
		t.Fatalf("managed partial config masked legacy close policy: %#v", policy)
	}
	cfg, _, err := readV7TuskerConfig(vault)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Branches.DefaultBranch != "release" || cfg.Branches.StateBranch != "state" || cfg.MutationMode != "protected" {
		t.Fatalf("full-schema merge lost safety authority: %#v", cfg)
	}
}

func TestProjectSetterUsesExactLegacyVaultAndRollsBackNoop(t *testing.T) {
	repo := t.TempDir()
	vault := filepath.Join(repo, "tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(filepath.Join(repo, defaultRepoVaultDir)), "automation:\n  concurrency:\n    max_active_runs: 2\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.concurrency.max_active_runs", 4); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(vault, "config.local.yaml")
	if !fileExists(managed) || fileExists(filepath.Join(repo, ".tusker", "config.local.yaml")) {
		t.Fatalf("setter did not use exact supplied vault: managed=%t default=%t", fileExists(managed), fileExists(filepath.Join(repo, ".tusker", "config.local.yaml")))
	}
	before, err := readText(managed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.concurrency.max_active_runs", 4); err == nil {
		t.Fatal("expected no-op setter failure")
	}
	after, err := readText(managed)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("failed setter changed authority:\n before=%s\n after=%s", before, after)
	}
}

func TestSetterReadbackDaemonLimitsWritesUserGlobalAndRejectsNoop(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	output := captureStdout(t, func() {
		if err := daemonLimitsCmd(Args{"max-active-runs": "7", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		OK            bool   `json:"ok"`
		MaxActiveRuns int    `json:"max_active_runs"`
		Source        string `json:"source"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.OK, "daemon limits ok")
	assertEqual(t, 7, payload.MaxActiveRuns, "global max active runs")
	assertEqual(t, configSourceUserGlobal, payload.Source, "daemon setter winning source")
	if err := daemonLimitsCmd(Args{"max-active-runs": "7", "json": "true"}); err == nil {
		t.Fatal("expected repeat daemon setter to fail as a no-op")
	}
}

func TestConfigResolveShowsEffectiveWinnerAndLosingSources(t *testing.T) {
	vault := automationTestVault(t)
	root := filepath.Dir(vault)
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := ensureDir(filepath.Join(xdg, "tusker")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(xdg, "tusker", "config.yaml"), "automation:\n  concurrency:\n    max_active_runs_per_project: 2\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(filepath.Join(root, defaultRepoVaultDir)), "schema: tusker.config/v1\nproject_id: app\nautomation:\n  concurrency:\n    max_active_runs_per_project: 3\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerLocalConfigPath(filepath.Join(root, defaultRepoVaultDir)), "automation:\n  concurrency:\n    max_active_runs_per_project: 4\n"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := configResolveCmd(Args{"vault": vault, "key": "runtime.max_active_runs_per_project"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{"effective: 4", configSourceLocal, configSourceProject, configSourceUserGlobal, configSourceBuiltIn, "lookup: automation.concurrency.max_active_runs_per_project"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("config resolve output missing %q:\n%s", expected, output)
		}
	}
}

func TestConfigResolvePrintsIgnoredGlobalNoteWhenBuiltInWins(t *testing.T) {
	vault := automationTestVault(t)
	global := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", global)
	if err := writeText(filepath.Join(global, "tusker", "config.yaml"), "automation:\n  enabled: true\n"); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := configResolveCmd(Args{"vault": vault, "key": "automation.enabled"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "effective: false") || !strings.Contains(output, "ignored at user-global layer; behavioral settings must be project-local") {
		t.Fatalf("config resolve omitted ignored-layer explanation:\n%s", output)
	}
}
