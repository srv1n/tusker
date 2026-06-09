package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type WorkflowFile struct {
	Path string
	Body string
	Data Workflow
}

type Workflow struct {
	WorkflowVersion      int `yaml:"workflow_version"`
	TrackerSchemaVersion int `yaml:"tracker_schema_version"`
	Tracker              struct {
		Kind               string   `yaml:"kind"`
		ActiveStates       []string `yaml:"dispatch_states"`
		LegacyActiveStates []string `yaml:"active_states,omitempty"`
		ReviewStates       []string `yaml:"review_states"`
		TerminalStates     []string `yaml:"terminal_states"`
	} `yaml:"tracker"`
	Agents struct {
		Default                    string         `yaml:"default"`
		Enabled                    []string       `yaml:"enabled"`
		MaxConcurrentAgents        int            `yaml:"max_concurrent_agents"`
		MaxConcurrentAgentsByState map[string]int `yaml:"max_concurrent_agents_by_state"`
	} `yaml:"agents"`
	Runtime struct {
		PollIntervalMS          int `yaml:"poll_interval_ms"`
		LeaseTTLMS              int `yaml:"lease_ttl_ms"`
		MaxActiveRunsPerProject int `yaml:"max_active_runs_per_project"`
	} `yaml:"runtime"`
	Workspace struct {
		Root     string `yaml:"root"`
		Strategy string `yaml:"strategy"`
	} `yaml:"workspace"`
	Retry struct {
		MaxAttempts int   `yaml:"max_attempts"`
		BackoffMS   []int `yaml:"backoff_ms"`
	} `yaml:"retry"`
	Reviewer     ReviewerPolicy              `yaml:"reviewer"`
	ExternalLoop ExternalLoopCaps            `yaml:"external_loop"`
	Runners      map[string]RunnerDefinition `yaml:"runners"`
	Codex        struct {
		Command           string `yaml:"command"`
		ApprovalPolicy    string `yaml:"approval_policy"`
		ThreadSandbox     string `yaml:"thread_sandbox"`
		TurnSandboxPolicy string `yaml:"turn_sandbox_policy"`
		TurnTimeoutMS     int    `yaml:"turn_timeout_ms"`
		ReadTimeoutMS     int    `yaml:"read_timeout_ms"`
		StallTimeoutMS    int    `yaml:"stall_timeout_ms"`
		MaxTurns          int    `yaml:"max_turns"`
	} `yaml:"codex"`
	CodexCloud CodexCloudConfig `yaml:"codex_cloud"`
	Claude     struct {
		Command string `yaml:"command"`
	} `yaml:"claude"`
	Extensions ExtensionPolicy `yaml:"extensions"`
	Hooks      struct {
		AfterWorkspaceCreate  []string `yaml:"after_workspace_create"`
		BeforeWorkspaceRemove []string `yaml:"before_workspace_remove"`
	} `yaml:"hooks"`
	Fanout FanoutPolicy `yaml:"fanout"`
}

type RunnerDefinition struct {
	Kind              string `yaml:"kind" json:"kind"`
	Command           string `yaml:"command" json:"command"`
	ApprovalPolicy    string `yaml:"approval_policy,omitempty" json:"approval_policy,omitempty"`
	ThreadSandbox     string `yaml:"thread_sandbox,omitempty" json:"thread_sandbox,omitempty"`
	TurnSandboxPolicy string `yaml:"turn_sandbox_policy,omitempty" json:"turn_sandbox_policy,omitempty"`
	TurnTimeoutMS     int    `yaml:"turn_timeout_ms,omitempty" json:"turn_timeout_ms,omitempty"`
	ReadTimeoutMS     int    `yaml:"read_timeout_ms,omitempty" json:"read_timeout_ms,omitempty"`
	StallTimeoutMS    int    `yaml:"stall_timeout_ms,omitempty" json:"stall_timeout_ms,omitempty"`
	MaxTurns          int    `yaml:"max_turns,omitempty" json:"max_turns,omitempty"`
	EnvironmentID     string `yaml:"environment_id,omitempty" json:"environment_id,omitempty"`
	ApplyMode         string `yaml:"apply_mode,omitempty" json:"apply_mode,omitempty"`
	PRMode            string `yaml:"pr_mode,omitempty" json:"pr_mode,omitempty"`
	ExternalCollect   bool   `yaml:"external_collect,omitempty" json:"external_collect,omitempty"`
	StatusCommand     string `yaml:"status_command,omitempty" json:"status_command,omitempty"`
	CollectCommand    string `yaml:"collect_command,omitempty" json:"collect_command,omitempty"`
}

type FanoutPolicy struct {
	Enabled           bool     `yaml:"enabled" json:"enabled"`
	MaxChildren       int      `yaml:"max_children" json:"max_children"`
	AllowedChildTypes []string `yaml:"allowed_child_types" json:"allowed_child_types"`
	MergeRule         string   `yaml:"merge_rule" json:"merge_rule"`
}

type ReviewerPolicy struct {
	Enabled            bool     `yaml:"enabled" json:"enabled"`
	Runner             string   `yaml:"runner" json:"runner"`
	Actor              string   `yaml:"actor" json:"actor"`
	AutoCloseRisks     []string `yaml:"auto_close_risks" json:"auto_close_risks"`
	HumanRequiredRisks []string `yaml:"human_required_risks" json:"human_required_risks"`
	Prompt             string   `yaml:"prompt" json:"prompt"`
}

type ExtensionPolicy struct {
	Enabled              bool     `yaml:"enabled" json:"enabled"`
	AllowedTools         []string `yaml:"allowed_tools" json:"allowed_tools"`
	AllowedMCPs          []string `yaml:"allowed_mcps" json:"allowed_mcps"`
	AllowTuskerReadTools bool     `yaml:"allow_tusker_read_tools" json:"allow_tusker_read_tools"`
}

func workflowPath(vaultPath string) string {
	return filepath.Join(vaultPath, "WORKFLOW.md")
}

func legacyConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, "_system", "config.yaml")
}

func defaultWorkflow() Workflow {
	var wf Workflow
	wf.WorkflowVersion = 1
	wf.TrackerSchemaVersion = 7
	wf.Tracker.Kind = "tusker_vault"
	wf.Tracker.ActiveStates = []string{"ready", "rework"}
	wf.Tracker.ReviewStates = []string{"review"}
	wf.Tracker.TerminalStates = []string{"done", "cancelled", "superseded"}
	wf.Agents.Default = string(RunnerCodexAppServer)
	wf.Agents.Enabled = []string{string(RunnerCodexAppServer), string(RunnerCodexExec), string(RunnerClaude)}
	wf.Agents.MaxConcurrentAgents = 2
	wf.Agents.MaxConcurrentAgentsByState = map[string]int{"rework": 1}
	wf.Runtime.PollIntervalMS = 5000
	wf.Runtime.LeaseTTLMS = 900000
	wf.Runtime.MaxActiveRunsPerProject = 1
	wf.Workspace.Root = "../.tusker-worktrees"
	wf.Workspace.Strategy = "worktree"
	wf.Retry.MaxAttempts = 3
	wf.Retry.BackoffMS = []int{30000, 120000, 600000}
	wf.Reviewer.Enabled = true
	wf.Reviewer.Runner = string(RunnerCodexAppServer)
	wf.Reviewer.Actor = "reviewer:agent"
	wf.Reviewer.AutoCloseRisks = []string{"low", "medium"}
	wf.Reviewer.HumanRequiredRisks = []string{"high", "critical"}
	wf.Reviewer.Prompt = defaultReviewerPrompt()
	wf.ExternalLoop = ExternalLoopCaps{
		MaxCycles:              externalLoopDefaultMaxCycles,
		MaxRepairContinuations: externalLoopDefaultMaxRepairContinuations,
		MaxExternalThreads:     externalLoopDefaultMaxExternalThreads,
		WallClockTimeoutHours:  externalLoopDefaultWallClockTimeoutHours,
	}
	wf.Runners = map[string]RunnerDefinition{
		string(RunnerCodexAppServer): {Kind: string(RunnerCodexAppServer), Command: "codex app-server"},
		string(RunnerCodexExec):      {Kind: string(RunnerCodexExec), Command: "codex exec --skip-git-repo-check -"},
		string(RunnerClaude):         {Kind: string(RunnerClaude), Command: "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"},
	}
	wf.Codex.Command = "codex app-server"
	wf.Codex.ApprovalPolicy = "on-request"
	wf.Codex.ThreadSandbox = "workspace-write"
	wf.Codex.TurnSandboxPolicy = "workspace-write"
	wf.Codex.TurnTimeoutMS = 600000
	wf.Codex.ReadTimeoutMS = 30000
	wf.Codex.StallTimeoutMS = 120000
	wf.Codex.MaxTurns = 1
	wf.Claude.Command = "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
	wf.Extensions.Enabled = false
	wf.Extensions.AllowedTools = []string{}
	wf.Extensions.AllowedMCPs = []string{}
	wf.Extensions.AllowTuskerReadTools = false
	wf.Hooks.AfterWorkspaceCreate = []string{}
	wf.Hooks.BeforeWorkspaceRemove = []string{}
	wf.Fanout.Enabled = false
	wf.Fanout.MaxChildren = 0
	wf.Fanout.AllowedChildTypes = []string{}
	wf.Fanout.MergeRule = "manual_review"
	normalizeWorkflowDispatchStates(&wf)
	return wf
}

func defaultReviewerPrompt() string {
	return strings.TrimSpace(`You are the independent Tusker reviewer for {{ note.id }}.

Review only. Do not edit implementation files. If the work needs changes, mark the task ` + "`rework`" + ` with a specific acceptance/proof reason instead of fixing it yourself.

Task:
- ID: {{ note.id }}
- Title: {{ note.title }}
- Risk: {{ note.risk }}
- Status: {{ note.status }}
- Attempt: {{ attempt.id }}
- Workspace: {{ workspace.path }}
- Vault: {{ vault.path }}

Policy:
- Reviewer actor: {{ reviewer.actor }}
- Auto-close allowed: {{ reviewer.auto_close_allowed }}
- Human close required: {{ reviewer.human_required }}

Checklist:
1. Read the task acceptance contract, proof mode, verification rows, evidence cards, and gates.
2. Inspect the current diff against the task scope. Call out surprise files or drive-by refactors.
3. Run the smallest verification commands needed to prove the acceptance contract.
4. Confirm project skill/domain canon changes only when the task changed durable project knowledge.
5. For high or critical risk, leave the task in review with a human-actionable recommendation.
6. If a caveat changes scope, decide whether it is acceptable or requires rework.

If the task fails review, run:
tusker status {{ note.id }} rework --by {{ reviewer.actor }} --reason "<specific unmet acceptance item>"

If auto-close is allowed and every check passes, run:
{{ reviewer.verify_command }}
{{ reviewer.close_command }}

If human close is required and every check passes, do not run ` + "`verify`" + ` or ` + "`close`" + `. Leave the task in ` + "`review`" + ` and state the human-review recommendation in your final response.`)
}

func reviewerPolicyCoversRisk(policy ReviewerPolicy, risk string) bool {
	return reviewerMayAutoCloseRisk(policy, risk) || reviewerRequiresHumanRisk(policy, risk)
}

func reviewerMayAutoCloseRisk(policy ReviewerPolicy, risk string) bool {
	return stringListContainsFold(policy.AutoCloseRisks, risk)
}

func reviewerRequiresHumanRisk(policy ReviewerPolicy, risk string) bool {
	return stringListContainsFold(policy.HumanRequiredRisks, risk)
}

func stringListContainsFold(values []string, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == target {
			return true
		}
	}
	return false
}

func defaultWorkflowMarkdown() string {
	wf := defaultWorkflow()
	raw, _ := yaml.Marshal(wf)
	return "---\n" + strings.TrimSpace(string(raw)) + "\n---\n\n## Routing\n\nYou are working on {{ note.id }} for {{ project.name }}. Dispatch only makes sense when this task is in a dispatch state (`ready` or `rework`) and the workspace is ready at {{ workspace.path }}.\n\n## Hard stop check\n\nBefore doing work, run `tusker closeout status {{ note.id }} --json` when the V7 closeout command is available. If it reports `agent_action=stop_until_human_response`, do not validate, inspect files, spawn subagents, or modify Tusker records. Reply with the pending human gates/proof and whether the closeout checkpoint or review packet is still needed.\n\nRevalidate only after you edited files, a task/gate/evidence state changed, the closeout fingerprint no longer matches, or the user explicitly asked for fresh validation.\n\n## Prompt\n\nUse the installed Tusker skill bundle for durable task semantics and proof discipline. Work inside {{ workspace.path }}. Treat {{ repo.root }} as the source repository root for context only unless the task explicitly requires comparing against it.\n\nItem: {{ note.title }}\nRecord: {{ note.record_id }}\nType: {{ note.type }}\nAttempt: {{ attempt.number }}\nWorkflow: {{ workflow.path }}\nVault: {{ vault.path }}\n\n## Command budget\n\nUse the smallest command that proves or locates the next fact. Prefer packets/capsules, path-scoped status/search, repo-configured wrappers and build-lock/status commands, and redirected logs with small tails. Report validation as command + PASS/FAIL plus the first actionable failure; do not paste raw transcripts or repeat unchanged-state updates.\n\n## External Apply Inputs\n\nSome tasks may have external apply inputs collected by Tusker under `architect/{{ note.id }}/` or a workspace-local mirror of that directory.\n\nWhen that directory contains exactly one `*.patch` or `*.diff` file:\n\n1. inspect the task acceptance and verification contract first;\n2. run `git apply --check --3way <patch>`;\n3. apply with `git apply --3way <patch>` only after the check passes;\n4. resolve conflicts only when the resolution is mechanical and clearly within the task contract;\n5. run the task verification commands;\n6. record compact verification evidence;\n7. use `tusker finish {{ note.id }} --request-review` when machine proof is complete;\n8. create a concrete gate or move to rework/blocked when proof cannot be completed.\n\nIf there are zero patches, multiple patches, a patch outside scope, or an ambiguous conflict, stop and report the blocker through Tusker. Do not invent or silently repair patches.\n\n## Completion contract\n\nSatisfy the task proof mode. For proof_mode=inline, record concise verification rows with `tusker verify add`; do not create evidence files. For card/artifact/audit, create only the evidence the proof mode requires. When machine work is complete and only human-owned proof or gates remain, run `tusker closeout <task-id> --emit-packet --validate \"<command>\"`, then stop. When the work is demonstrably ready for verification, use `tusker finish <task-id> --request-review` so the task reaches `review` or a branch-safe `propose status ... --status review` proposal is created. Attempt handoff alone is not a review request. If proof is blocked, create/propose a gate with a concrete owner, action, and verification instead of appending negative evidence.\n\n## Reviewer contract\n\nIf `reviewer.enabled` is true, tasks in `review` may be dispatched to `reviewer.runner` for independent review. The reviewer must not edit implementation files. Low/medium risks can be verified and closed by `reviewer.actor` after all gates pass; high/critical risks stay in `review` for human verification and close.\n\n## Retry policy\n\nRetry only transient infrastructure failures. Human-directed rework creates a new task revision; runtime activity remains in the run/lease store.\n\n## Human override policy\n\nHumans may edit tasks directly, but runtime state belongs to the daemon store.\n"
}

func normalizeWorkflowDispatchStates(wf *Workflow) {
	if len(wf.Tracker.ActiveStates) == 0 && len(wf.Tracker.LegacyActiveStates) > 0 {
		wf.Tracker.ActiveStates = append([]string{}, wf.Tracker.LegacyActiveStates...)
	}
	wf.Tracker.LegacyActiveStates = nil
}

func loadWorkflow(vaultPath string) (WorkflowFile, error) {
	filePath := workflowPath(vaultPath)
	if !fileExists(filePath) {
		return WorkflowFile{}, tuskerError(errorNotFound, "WORKFLOW.md not found", withPath(filePath))
	}
	text, err := readText(filePath)
	if err != nil {
		return WorkflowFile{}, err
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		return WorkflowFile{}, tuskerError(errorConfigInvalid, fmt.Sprintf("failed to parse WORKFLOW.md: %s", err.Error()), withPath(filePath))
	}
	raw, err := yaml.Marshal(data)
	if err != nil {
		return WorkflowFile{}, err
	}
	wf := defaultWorkflow()
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		return WorkflowFile{}, tuskerError(errorConfigInvalid, fmt.Sprintf("failed to decode WORKFLOW.md: %s", err.Error()), withPath(filePath))
	}
	normalizeWorkflowDispatchStates(&wf)
	wfFile := WorkflowFile{Path: filePath, Body: body, Data: wf}
	if err := validateWorkflowFile(wfFile); err != nil {
		return WorkflowFile{}, err
	}
	if overlaid, err := applyTuskerAutomationConfig(vaultPath, wfFile); err != nil {
		return WorkflowFile{}, err
	} else {
		wfFile = overlaid
	}
	return wfFile, nil
}

func applyTuskerAutomationConfig(vaultPath string, wfFile WorkflowFile) (WorkflowFile, error) {
	cfg, configPath, err := readV7TuskerConfig(vaultPath)
	if err != nil {
		return wfFile, tuskerError(errorConfigInvalid, "failed to parse tusker.yaml automation config: "+err.Error(), withPath(configPath))
	}
	if !v7AutomationConfigPresent(cfg) {
		return wfFile, nil
	}
	wf := wfFile.Data
	triggerStates := normalizeList(cfg.Automation.TriggerStates)
	if len(triggerStates) == 0 {
		triggerStates = []string{"ready", "rework"}
	}
	if containsString(triggerStates, "active") && strings.TrimSpace(cfg.Automation.LegacyProfile) == "" {
		return wfFile, tuskerError(errorConfigInvalid, "automation.trigger_states must not include legacy active without automation.legacy_profile", withPath(configPath), withHint("use ready,rework or set automation.legacy_profile: legacy_active"))
	}
	wf.Tracker.ActiveStates = triggerStates
	if strings.TrimSpace(cfg.Automation.DefaultRunner) != "" {
		wf.Agents.Default = strings.TrimSpace(cfg.Automation.DefaultRunner)
	}
	if len(cfg.Automation.EnabledRunners) > 0 {
		wf.Agents.Enabled = normalizeList(cfg.Automation.EnabledRunners)
	}
	if wf.Reviewer.Enabled && !stringListContainsFold(wf.Agents.Enabled, wf.Reviewer.Runner) {
		wf.Reviewer.Runner = wf.Agents.Default
	}
	if cfg.Automation.Concurrency.MaxActiveRuns > 0 {
		wf.Agents.MaxConcurrentAgents = cfg.Automation.Concurrency.MaxActiveRuns
	}
	if cfg.Automation.Concurrency.MaxActiveRunsPerProject > 0 {
		wf.Runtime.MaxActiveRunsPerProject = cfg.Automation.Concurrency.MaxActiveRunsPerProject
	}
	if cfg.Automation.Concurrency.MaxConcurrentByState != nil {
		wf.Agents.MaxConcurrentAgentsByState = cfg.Automation.Concurrency.MaxConcurrentByState
	}
	if cfg.Automation.ExternalLoop.MaxCycles > 0 {
		wf.ExternalLoop.MaxCycles = cfg.Automation.ExternalLoop.MaxCycles
	}
	if cfg.Automation.ExternalLoop.MaxRepairContinuations > 0 {
		wf.ExternalLoop.MaxRepairContinuations = cfg.Automation.ExternalLoop.MaxRepairContinuations
	}
	if cfg.Automation.ExternalLoop.MaxExternalThreads > 0 {
		wf.ExternalLoop.MaxExternalThreads = cfg.Automation.ExternalLoop.MaxExternalThreads
	}
	if cfg.Automation.ExternalLoop.WallClockTimeoutHours > 0 {
		wf.ExternalLoop.WallClockTimeoutHours = cfg.Automation.ExternalLoop.WallClockTimeoutHours
	}
	if strings.TrimSpace(cfg.Automation.Workspace.Root) != "" {
		wf.Workspace.Root = strings.TrimSpace(cfg.Automation.Workspace.Root)
	}
	if strings.TrimSpace(cfg.Automation.Workspace.Strategy) != "" {
		wf.Workspace.Strategy = strings.TrimSpace(cfg.Automation.Workspace.Strategy)
	}
	if wf.Runners == nil {
		wf.Runners = map[string]RunnerDefinition{}
	}
	for name, runner := range cfg.Automation.Runners {
		definition := RunnerDefinition{
			Kind:              runner.Kind,
			Command:           runner.Command,
			ApprovalPolicy:    runner.ApprovalPolicy,
			ThreadSandbox:     runner.ThreadSandbox,
			TurnSandboxPolicy: runner.TurnSandboxPolicy,
			TurnTimeoutMS:     runner.TurnTimeoutMS,
			ReadTimeoutMS:     runner.ReadTimeoutMS,
			StallTimeoutMS:    runner.StallTimeoutMS,
			MaxTurns:          runner.MaxTurns,
			EnvironmentID:     runner.EnvironmentID,
			ApplyMode:         runner.ApplyMode,
			PRMode:            runner.PRMode,
			ExternalCollect:   runner.ExternalCollect,
			StatusCommand:     runner.StatusCommand,
			CollectCommand:    runner.CollectCommand,
		}
		if strings.TrimSpace(definition.Kind) == "" {
			definition.Kind = strings.TrimSpace(name)
		}
		wf.Runners[strings.TrimSpace(name)] = definition
		applyRunnerDefinitionToLegacyBlocks(&wf, definition)
	}
	if cfg.Automation.Fanout.Enabled {
		wf.Fanout.Enabled = true
		wf.Fanout.MaxChildren = cfg.Automation.Fanout.MaxChildren
		wf.Fanout.AllowedChildTypes = normalizeList(cfg.Automation.Fanout.AllowedChildTypes)
		wf.Fanout.MergeRule = firstNonEmpty(strings.TrimSpace(cfg.Automation.Fanout.MergeRule), wf.Fanout.MergeRule)
	}
	normalizeWorkflowDispatchStates(&wf)
	wfFile.Data = wf
	if err := validateWorkflowFile(wfFile); err != nil {
		return wfFile, err
	}
	return wfFile, nil
}

func v7AutomationConfigPresent(cfg v7TuskerConfigFile) bool {
	return cfg.Automation.Enabled != nil ||
		len(cfg.Automation.TriggerStates) > 0 ||
		strings.TrimSpace(cfg.Automation.DefaultRunner) != "" ||
		len(cfg.Automation.EnabledRunners) > 0 ||
		strings.TrimSpace(cfg.Automation.Workspace.Root) != "" ||
		strings.TrimSpace(cfg.Automation.Workspace.Strategy) != "" ||
		cfg.Automation.Concurrency.MaxActiveRuns > 0 ||
		cfg.Automation.Concurrency.MaxActiveRunsPerProject > 0 ||
		len(cfg.Automation.Concurrency.MaxConcurrentByState) > 0 ||
		cfg.Automation.ExternalLoop.MaxCycles > 0 ||
		cfg.Automation.ExternalLoop.MaxRepairContinuations > 0 ||
		cfg.Automation.ExternalLoop.MaxExternalThreads > 0 ||
		cfg.Automation.ExternalLoop.WallClockTimeoutHours > 0 ||
		len(cfg.Automation.Runners) > 0 ||
		cfg.Automation.Fanout.Enabled
}

func applyRunnerDefinitionToLegacyBlocks(wf *Workflow, definition RunnerDefinition) {
	switch RunnerName(strings.TrimSpace(definition.Kind)) {
	case RunnerCodex, RunnerCodexAppServer, RunnerCodexExec:
		if strings.TrimSpace(definition.Command) != "" {
			wf.Codex.Command = definition.Command
		}
		if strings.TrimSpace(definition.ApprovalPolicy) != "" {
			wf.Codex.ApprovalPolicy = definition.ApprovalPolicy
		}
		if strings.TrimSpace(definition.ThreadSandbox) != "" {
			wf.Codex.ThreadSandbox = definition.ThreadSandbox
		}
		if strings.TrimSpace(definition.TurnSandboxPolicy) != "" {
			wf.Codex.TurnSandboxPolicy = definition.TurnSandboxPolicy
		}
		if definition.TurnTimeoutMS > 0 {
			wf.Codex.TurnTimeoutMS = definition.TurnTimeoutMS
		}
		if definition.ReadTimeoutMS > 0 {
			wf.Codex.ReadTimeoutMS = definition.ReadTimeoutMS
		}
		if definition.StallTimeoutMS > 0 {
			wf.Codex.StallTimeoutMS = definition.StallTimeoutMS
		}
		if definition.MaxTurns > 0 {
			wf.Codex.MaxTurns = definition.MaxTurns
		}
	case RunnerCodexCloud:
		if strings.TrimSpace(definition.Command) != "" {
			wf.CodexCloud.Command = definition.Command
		}
		if strings.TrimSpace(definition.StatusCommand) != "" {
			wf.CodexCloud.StatusCommand = definition.StatusCommand
		}
		if strings.TrimSpace(definition.CollectCommand) != "" {
			wf.CodexCloud.CollectCommand = definition.CollectCommand
		}
		if strings.TrimSpace(definition.EnvironmentID) != "" {
			wf.CodexCloud.EnvironmentID = definition.EnvironmentID
		}
		if strings.TrimSpace(definition.ApplyMode) != "" {
			wf.CodexCloud.ApplyMode = definition.ApplyMode
		}
		if strings.TrimSpace(definition.PRMode) != "" {
			wf.CodexCloud.PRMode = definition.PRMode
		}
		if definition.ExternalCollect {
			wf.CodexCloud.ExternalCollect = true
		}
	case RunnerClaude:
		if strings.TrimSpace(definition.Command) != "" {
			wf.Claude.Command = definition.Command
		}
	}
}

func workflowToConfig(wf Workflow) Config {
	cfg := defaultConfig()
	cfg.Version = wf.WorkflowVersion
	cfg.Agents.Enabled = append([]string{}, wf.Agents.Enabled...)
	cfg.Agents.Concurrency = map[string]int{}
	for _, agent := range wf.Agents.Enabled {
		cfg.Agents.Concurrency[agent] = 1
	}
	for state, capValue := range wf.Agents.MaxConcurrentAgentsByState {
		_ = state
		_ = capValue
	}
	if len(cfg.Agents.Enabled) > 0 && wf.Agents.MaxConcurrentAgents > 0 {
		share := maxInt(1, wf.Agents.MaxConcurrentAgents)
		for _, agent := range cfg.Agents.Enabled {
			if agent == "sarav" {
				cfg.Agents.Concurrency[agent] = 0
				continue
			}
			cfg.Agents.Concurrency[agent] = share
		}
	}
	cfg.Poll.IntervalSeconds = maxInt(1, wf.Runtime.PollIntervalMS/1000)
	cfg.Retry.MaxAttempts = wf.Retry.MaxAttempts
	cfg.Retry.BackoffSeconds = make([]int, 0, len(wf.Retry.BackoffMS))
	for _, value := range wf.Retry.BackoffMS {
		cfg.Retry.BackoffSeconds = append(cfg.Retry.BackoffSeconds, maxInt(1, value/1000))
	}
	cfg.Workspace.Root = filepath.ToSlash(wf.Workspace.Root)
	cfg.Workspace.Isolation = wf.Workspace.Strategy
	return cfg
}

func writeDefaultWorkflow(vaultPath string) error {
	filePath := workflowPath(vaultPath)
	if fileExists(filePath) {
		text, err := readText(filePath)
		if err == nil {
			data, _, parseErr := parseFrontmatter(text)
			if parseErr == nil && intField(data, "tracker_schema_version") == 7 {
				return nil
			}
		}
	}
	return writeText(filePath, defaultWorkflowMarkdown())
}

func workflowInitCmd(args Args) error {
	vaultPath, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	if err := writeDefaultWorkflow(vaultPath); err != nil {
		return err
	}
	if err := writeDefaultConfig(vaultPath); err != nil {
		return err
	}
	if !args.Bool("quiet") {
		fmt.Printf("Initialized workflow contract at %s\n", workflowPath(vaultPath))
	}
	return nil
}

func setWorkflowProjectRunLimit(vaultPath string, limit int) (WorkflowFile, error) {
	if limit <= 0 {
		return WorkflowFile{}, tuskerError(errorInvalidArg, "--max-active-runs must be > 0", withContext(map[string]any{"arg": "--max-active-runs", "value": limit}))
	}
	filePath := workflowPath(vaultPath)
	text, err := readText(filePath)
	if err != nil {
		return WorkflowFile{}, err
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		return WorkflowFile{}, tuskerError(errorConfigInvalid, fmt.Sprintf("failed to parse WORKFLOW.md: %s", err.Error()), withPath(filePath))
	}
	runtimeBlock, ok := data["runtime"].(map[string]any)
	if !ok || runtimeBlock == nil {
		runtimeBlock = map[string]any{}
	}
	runtimeBlock["max_active_runs_per_project"] = limit
	data["runtime"] = runtimeBlock
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		return WorkflowFile{}, err
	}
	if err := writeText(filePath, fm+"\n"+strings.TrimLeft(body, "\n")); err != nil {
		return WorkflowFile{}, err
	}
	return loadWorkflow(vaultPath)
}
