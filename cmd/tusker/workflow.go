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
		Kind           string   `yaml:"kind"`
		ActiveStates   []string `yaml:"active_states"`
		ReviewStates   []string `yaml:"review_states"`
		TerminalStates []string `yaml:"terminal_states"`
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
	Reviewer ReviewerPolicy `yaml:"reviewer"`
	Codex    struct {
		Command           string `yaml:"command"`
		ApprovalPolicy    string `yaml:"approval_policy"`
		ThreadSandbox     string `yaml:"thread_sandbox"`
		TurnSandboxPolicy string `yaml:"turn_sandbox_policy"`
		TurnTimeoutMS     int    `yaml:"turn_timeout_ms"`
		ReadTimeoutMS     int    `yaml:"read_timeout_ms"`
		StallTimeoutMS    int    `yaml:"stall_timeout_ms"`
		MaxTurns          int    `yaml:"max_turns"`
	} `yaml:"codex"`
	Claude struct {
		Command string `yaml:"command"`
	} `yaml:"claude"`
	Extensions ExtensionPolicy `yaml:"extensions"`
	Hooks      struct {
		AfterWorkspaceCreate  []string `yaml:"after_workspace_create"`
		BeforeWorkspaceRemove []string `yaml:"before_workspace_remove"`
	} `yaml:"hooks"`
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
	wf.TrackerSchemaVersion = 5
	wf.Tracker.Kind = "tusker_vault"
	wf.Tracker.ActiveStates = []string{"active", "rework"}
	wf.Tracker.ReviewStates = []string{"review"}
	wf.Tracker.TerminalStates = []string{"done", "cancelled"}
	wf.Agents.Default = "codex"
	wf.Agents.Enabled = []string{"sarav", "codex", "claude-code", "gemini"}
	wf.Agents.MaxConcurrentAgents = 3
	wf.Agents.MaxConcurrentAgentsByState = map[string]int{"rework": 1}
	wf.Runtime.PollIntervalMS = 5000
	wf.Runtime.LeaseTTLMS = 900000
	wf.Runtime.MaxActiveRunsPerProject = 1
	wf.Workspace.Root = "workspaces"
	wf.Workspace.Strategy = "worktree"
	wf.Retry.MaxAttempts = 3
	wf.Retry.BackoffMS = []int{30000, 120000, 600000}
	wf.Reviewer.Enabled = true
	wf.Reviewer.Runner = "codex"
	wf.Reviewer.Actor = "agent-reviewer"
	wf.Reviewer.AutoCloseRisks = []string{"low", "medium"}
	wf.Reviewer.HumanRequiredRisks = []string{"high", "critical"}
	wf.Reviewer.Prompt = defaultReviewerPrompt()
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
	return wf
}

func defaultReviewerPrompt() string {
	return strings.TrimSpace(`You are the independent Tusker reviewer for {{ note.id }}.

Review only. Do not edit implementation files. If the work needs changes, mark the task ` + "`rework`" + ` with a specific reason instead of fixing it yourself.

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
1. Read the task acceptance contract, scope, evidence, verification log, and docs resolution.
2. Inspect the current diff against the task scope. Call out surprise files or drive-by refactors.
3. Run the verification commands needed to prove the acceptance contract.
4. Confirm docs impact is applied, nooped, or waived for every ` + "`doc_nodes`" + ` entry.
5. For risk high or critical, confirm the Knowledge delta is real and reviewer-actionable.
6. If a caveat changes scope, decide whether it is acceptable or requires rework.

If the task fails review, run:
tusker status {{ note.id }} rework --by {{ reviewer.actor }} --reason "<specific unmet acceptance item>"

If auto-close is allowed and every check passes, run:
tusker docs check {{ note.id }}
tusker verify {{ note.id }} --by {{ reviewer.actor }} --summary "<what you verified>"
tusker close {{ note.id }} --by {{ reviewer.actor }} --reason "agent review accepted"

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
	return "---\n" + strings.TrimSpace(string(raw)) + "\n---\n\n## Routing\n\nYou are working on {{ note.id }} for {{ project.name }}. Dispatch only makes sense because this task is currently {{ note.status }} and the workspace is ready at {{ workspace.path }}.\n\n## Prompt\n\nUse the installed Tusker skill bundle for durable task semantics, evidence, and verification discipline. Work inside {{ workspace.path }}. Treat {{ repo.root }} as the source repository root for context only unless the task explicitly requires comparing against it.\n\nItem: {{ note.title }}\nRecord: {{ note.record_id }}\nType: {{ note.type }}\nAttempt: {{ attempt.number }}\nWorkflow: {{ workflow.path }}\nVault: {{ vault.path }}\n\n## Completion contract\n\nWhen the work is demonstrably ready for verification, move the task to `review`. If the work is blocked, set status to `blocked` with a concrete blocker instead of exiting cleanly. If the task remains active after a turn, the daemon will continue or retry the same session.\n\n## Reviewer contract\n\nIf `reviewer.enabled` is true, tasks in `review` may be dispatched to `reviewer.runner` for independent review. The reviewer must not edit implementation files. Low/medium risks can be verified and closed by `reviewer.actor` after all gates pass; high/critical risks stay in `review` for human verification and close.\n\n## Retry policy\n\nRetry only transient infrastructure failures. Human-directed rework creates a new active task revision.\n\n## Human override policy\n\nHumans may edit tasks directly, but runtime state belongs to the daemon store.\n"
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
	wfFile := WorkflowFile{Path: filePath, Body: body, Data: wf}
	if err := validateWorkflowFile(wfFile); err != nil {
		return WorkflowFile{}, err
	}
	return wfFile, nil
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
	cfg.Workspace.Root = filepath.ToSlash(filepath.Join("_system", wf.Workspace.Root))
	cfg.Workspace.Isolation = wf.Workspace.Strategy
	return cfg
}

func writeDefaultWorkflow(vaultPath string) error {
	filePath := workflowPath(vaultPath)
	if fileExists(filePath) {
		text, err := readText(filePath)
		if err == nil {
			data, _, parseErr := parseFrontmatter(text)
			if parseErr == nil && (intField(data, "tracker_schema_version") == 5 || intField(data, "tracker_schema_version") == 6) {
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
