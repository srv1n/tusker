package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultReviewerActor = "agent:reviewer/codex"

type WorkflowFile struct {
	Path string
	Body string
	Data Workflow
}

type RuntimeServeConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Addr     string   `yaml:"addr"`
	DocsDirs []string `yaml:"docs_dirs,omitempty"`
}

type Workflow struct {
	WorkflowVersion      int                               `yaml:"workflow_version"`
	TrackerSchemaVersion int                               `yaml:"tracker_schema_version"`
	AutomationEnabled    bool                              `yaml:"automation_enabled"`
	DispatchScope        automationDispatchScopeProjection `yaml:"-" json:"dispatch_scope"`
	CompletionReactor    completionReactorModeProjection   `yaml:"-" json:"completion_reactor"`
	// ScheduledPromotion is intentionally a workflow contract, rather than a
	// daemon switch.  Resolving it is side-effect free; the departure runner is
	// responsible for acting only on the permissions projected here.
	ScheduledPromotion ScheduledPromotionPolicy `yaml:"scheduled_promotion,omitempty" json:"scheduled_promotion"`
	Tracker            struct {
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
		PollIntervalMS          int                   `yaml:"poll_interval_ms"`
		LeaseTTLMS              int                   `yaml:"lease_ttl_ms"`
		MaxActiveRunsPerProject int                   `yaml:"max_active_runs_per_project"`
		MaxContinuationRetries  int                   `yaml:"max_continuation_retries"`
		Budget                  RuntimeBudgetConfig   `yaml:"budget"`
		Serve                   RuntimeServeConfig    `yaml:"serve"`
		Sentinel                RuntimeSentinelConfig `yaml:"sentinel"`
	} `yaml:"runtime"`
	Workspace struct {
		Root     string `yaml:"root"`
		Strategy string `yaml:"strategy"`
		// MaxLiveWorktrees is the hard cap on how many live work copies may exist
		// at once PER PROJECT (the cap is enforced against this project's own
		// workspace root, not machine-wide). The machine-wide ceiling is therefore
		// projects × max_live_worktrees, so set this with the number of active
		// projects in mind rather than as a single machine disk budget. It is a
		// MEASURED number from the real machine (peak worktrees before the disk
		// fills), never a guess — see .tusker/specs/build-and-test-economics.md,
		// "Measured floors". Zero leaves the cap off. Opening a work copy past this
		// limit is refused up front so we can never again spin up enough cold
		// builds to wedge the disk (the 2026-07-20 incident).
		MaxLiveWorktrees int `yaml:"max_live_worktrees,omitempty"`
	} `yaml:"workspace"`
	Retry struct {
		MaxAttempts int   `yaml:"max_attempts"`
		BackoffMS   []int `yaml:"backoff_ms"`
	} `yaml:"retry"`
	Reviewer             ReviewerPolicy                     `yaml:"reviewer"`
	ExternalLoop         ExternalLoopCaps                   `yaml:"external_loop"`
	Runners              map[string]RunnerDefinition        `yaml:"runners"`
	RunnerProfiles       map[string]RunnerProfileDefinition `yaml:"runner_profiles,omitempty"`
	RunnerProfileSources map[string]string                  `yaml:"-" json:"runner_profile_sources,omitempty"`
	RunnerDefaultProfile string                             `yaml:"runner_default_profile,omitempty"`
	RunnerLaneProfiles   map[string]string                  `yaml:"runner_lane_profiles,omitempty"`
	RunnerRouting        []RunnerRoutingRule                `yaml:"runner_routing,omitempty"`
	RunnerDenylist       []RunnerDenyRule                   `yaml:"runner_denylist,omitempty"`
	Codex                struct {
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
	Fanout        FanoutPolicy        `yaml:"fanout"`
	Orchestration OrchestrationPolicy `yaml:"orchestration,omitempty"`
}

// ScheduledPromotionPolicy is versioned so a future expansion cannot turn an
// old configuration into new authority. Release and paid triage are separate
// authorities; neither is implied by promote mode.
type ScheduledPromotionPolicy struct {
	Version     int                           `yaml:"version" json:"version"`
	Mode        string                        `yaml:"mode" json:"mode"`
	Release     ScheduledPromotionRelease     `yaml:"release,omitempty" json:"release"`
	ModelTriage ScheduledPromotionModelTriage `yaml:"model_triage,omitempty" json:"model_triage"`
	Effective   ScheduledPromotionProjection  `yaml:"-" json:"effective"`
}

type ScheduledPromotionRelease struct {
	Profile    string `yaml:"profile,omitempty" json:"profile,omitempty"`
	Authorized bool   `yaml:"authorized,omitempty" json:"authorized"`
}

type ScheduledPromotionModelTriage struct {
	Authorized bool `yaml:"authorized,omitempty" json:"authorized"`
}

// ScheduledPromotionProjection is the configuration-inspection surface. It
// makes the effective capabilities and the origin of the disabled default
// observable without making configuration inspection perform any work.
type ScheduledPromotionProjection struct {
	Configured  bool   `json:"configured"`
	Mode        string `json:"mode"`
	Provenance  string `json:"provenance"`
	Observe     bool   `json:"observe"`
	Stage       bool   `json:"stage"`
	Promote     bool   `json:"promote"`
	Release     bool   `json:"release"`
	ModelTriage bool   `json:"model_triage"`
}

const (
	scheduledPromotionPolicyVersion = 1
	scheduledPromotionDisabled      = "disabled"
	scheduledPromotionShadow        = "shadow"
	scheduledPromotionStage         = "stage"
	scheduledPromotionPromote       = "promote"
)

func defaultScheduledPromotionPolicy() ScheduledPromotionPolicy {
	policy := ScheduledPromotionPolicy{Version: scheduledPromotionPolicyVersion, Mode: scheduledPromotionDisabled}
	policy.Effective = scheduledPromotionProjection(policy, true, "fresh default")
	return policy
}

func scheduledPromotionProjection(policy ScheduledPromotionPolicy, configured bool, provenance string) ScheduledPromotionProjection {
	projection := ScheduledPromotionProjection{Configured: configured, Mode: policy.Mode, Provenance: provenance}
	switch policy.Mode {
	case scheduledPromotionShadow:
		projection.Observe = true
	case scheduledPromotionStage:
		projection.Observe = true
		projection.Stage = true
	case scheduledPromotionPromote:
		projection.Observe = true
		projection.Stage = true
		projection.Promote = true
		projection.Release = policy.Release.Authorized && strings.TrimSpace(policy.Release.Profile) != ""
		projection.ModelTriage = policy.ModelTriage.Authorized
	}
	return projection
}

type OrchestrationPolicy struct {
	DefaultBranch         string                 `yaml:"default_branch,omitempty" json:"default_branch,omitempty"`
	BranchAgeWarningHours int                    `yaml:"branch_age_warning_hours,omitempty" json:"branch_age_warning_hours"`
	SharedNamespaces      []string               `yaml:"shared_namespaces,omitempty" json:"shared_namespaces,omitempty"`
	NamespaceLints        []NamespaceLintPattern `yaml:"namespace_lints,omitempty" json:"namespace_lints,omitempty"`
	BatchGate             BatchGatePolicy        `yaml:"batch_gate,omitempty" json:"batch_gate"`
	Gate                  GateTierPolicy         `yaml:"gate,omitempty" json:"gate"`
}

// GateTierPolicy is the project's declared gate contract. Tusker owns the proof
// economics (harvest, preflight, defect harvesting); the project owns every
// toolchain-specific detail here, so no per-language runner logic lives in the
// binary.
type GateTierPolicy struct {
	// Profile is the canonical gate profile. A run requesting a different one
	// is refused rather than silently discarding the warm build.
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
	// HarvestCommands must be the runner's no-fail-fast form, e.g.
	// "cargo nextest run --no-fail-fast" or "go test ./...". Defaults to the
	// batch gate's commands.
	HarvestCommands []string `yaml:"harvest_commands,omitempty" json:"harvest_commands,omitempty"`
	// MinFreeDiskGB is the project's measured peak build footprint.
	MinFreeDiskGB float64 `yaml:"min_free_disk_gb,omitempty" json:"min_free_disk_gb,omitempty"`
	// BuildSlotLocks are paths whose presence means another stream owns the
	// host's build slot.
	BuildSlotLocks []string `yaml:"build_slot_locks,omitempty" json:"build_slot_locks,omitempty"`
	// AllowDirtyTree opts out of the frozen-tree precondition.
	AllowDirtyTree bool `yaml:"allow_dirty_tree,omitempty" json:"allow_dirty_tree,omitempty"`
	// DefectTargetRegex has one capture group naming the failing target, e.g.
	// `^--- FAIL: (\S+)` for Go or `^test (\S+) \.\.\. FAILED` for Rust.
	DefectTargetRegex string `yaml:"defect_target_regex,omitempty" json:"defect_target_regex,omitempty"`
	// DefectLineLimit caps each defect excerpt. Defaults to 12 lines.
	DefectLineLimit int `yaml:"defect_line_limit,omitempty" json:"defect_line_limit,omitempty"`
	// PromotionFailurePatterns are explicit project-owned classifications. They
	// are never inferred from arbitrary test assertion text.
	InfrastructureFailurePatterns []string `yaml:"infrastructure_failure_patterns,omitempty" json:"infrastructure_failure_patterns,omitempty"`
	FlakeFailurePatterns          []string `yaml:"flake_failure_patterns,omitempty" json:"flake_failure_patterns,omitempty"`
	FlakeFailureAction            string   `yaml:"flake_failure_action,omitempty" json:"flake_failure_action,omitempty"`
	// Scopes map areas of the project to the harvest commands that cover them,
	// enabling the Stage 1 per-change (selective) gate: `tusker gate --changed`
	// runs only the scopes a change touched. When empty, only the whole-harvest
	// gate is available. A change touching a path no scope owns fails closed to
	// the full harvest set rather than being skipped.
	Scopes []GateScope `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	// IsolationProvider is the NAME of a user/daemon-owned container/VM profile
	// required for scheduled full-promotion gates. Repository workflow never
	// selects an executable: sandbox-exec, process groups, audit sessions, and
	// ancestry walks are not sufficient to contain a daemonizing gate.
	IsolationProvider string `yaml:"isolation_provider,omitempty" json:"isolation_provider,omitempty"`
}

type NamespaceLintPattern struct {
	Name                 string `yaml:"name" json:"name"`
	Glob                 string `yaml:"glob" json:"glob"`
	CaptureRegex         string `yaml:"capture_regex" json:"capture_regex"`
	NamingRecommendation string `yaml:"naming_recommendation,omitempty" json:"naming_recommendation,omitempty"`
}

type BatchGatePolicy struct {
	Enabled     bool `yaml:"enabled" json:"enabled"`
	PeriodHours int  `yaml:"period_hours,omitempty" json:"period_hours"`
	// Windows are daily wall-clock departure times ("HH:MM") in the daemon
	// host's LOCAL time. When set, the cycle fires AT these times each day
	// rather than every period_hours; period_hours is the fallback when empty.
	// No day-of-week/calendar scheduling and no timezone selector in v1.
	Windows        []string `yaml:"windows,omitempty" json:"windows,omitempty"`
	Commands       []string `yaml:"commands,omitempty" json:"commands,omitempty"`
	FeatureProfile string   `yaml:"feature_profile,omitempty" json:"feature_profile,omitempty"`
	MaxRepairs     int      `yaml:"max_repairs,omitempty" json:"max_repairs"`
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
	MaxCycles          int      `yaml:"max_cycles" json:"max_cycles"`
	AutoCloseRisks     []string `yaml:"auto_close_risks" json:"auto_close_risks"`
	HumanRequiredRisks []string `yaml:"human_required_risks,omitempty" json:"human_required_risks,omitempty"`
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
	wf.AutomationEnabled = false
	wf.DispatchScope = defaultAutomationDispatchScope()
	wf.CompletionReactor = defaultCompletionReactorMode()
	wf.ScheduledPromotion = defaultScheduledPromotionPolicy()
	wf.Tracker.Kind = "tusker_vault"
	wf.Tracker.ActiveStates = []string{"ready", "rework"}
	wf.Tracker.ReviewStates = []string{"review"}
	wf.Tracker.TerminalStates = []string{"done", "cancelled", "superseded"}
	wf.Agents.Default = string(RunnerCodexExec)
	wf.Agents.Enabled = []string{string(RunnerCodexExec), string(RunnerClaude)}
	wf.Agents.MaxConcurrentAgents = 2
	wf.Agents.MaxConcurrentAgentsByState = map[string]int{"rework": 1}
	wf.Runtime.PollIntervalMS = int(defaultReconcileTick / time.Millisecond)
	wf.Runtime.LeaseTTLMS = 900000
	wf.Runtime.MaxActiveRunsPerProject = 1
	wf.Runtime.MaxContinuationRetries = 3
	wf.Runtime.Budget = defaultRuntimeBudgetConfig()
	wf.Runtime.Serve = RuntimeServeConfig{Enabled: true, Addr: defaultServeAddr}
	wf.Runtime.Sentinel = defaultRuntimeSentinelConfig()
	wf.Workspace.Root = "."
	wf.Workspace.Strategy = string(WorkspaceStrategyShared)
	wf.Retry.MaxAttempts = 3
	wf.Retry.BackoffMS = []int{30000, 120000, 600000}
	wf.Reviewer.Enabled = true
	wf.Reviewer.Runner = string(RunnerCodexExec)
	wf.Reviewer.Actor = defaultReviewerActor
	wf.Reviewer.MaxCycles = 3
	wf.Reviewer.AutoCloseRisks = []string{"low", "medium", "high", "critical"}
	wf.Reviewer.Prompt = defaultReviewerPrompt()
	wf.ExternalLoop = ExternalLoopCaps{
		MaxCycles:              externalLoopDefaultMaxCycles,
		MaxRepairContinuations: externalLoopDefaultMaxRepairContinuations,
		MaxExternalThreads:     externalLoopDefaultMaxExternalThreads,
		WallClockTimeoutHours:  externalLoopDefaultWallClockTimeoutHours,
	}
	wf.Runners = map[string]RunnerDefinition{
		string(RunnerCodexExec): {Kind: string(RunnerCodexExec), Command: defaultCodexExecCommand()},
		string(RunnerClaude):    {Kind: string(RunnerClaude), Command: "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"},
	}
	wf.Codex.Command = defaultCodexExecCommand()
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
	wf.Orchestration.BranchAgeWarningHours = 48
	wf.Orchestration.BatchGate.PeriodHours = 24
	wf.Orchestration.BatchGate.MaxRepairs = 3
	normalizeWorkflowDispatchStates(&wf)
	return wf
}

func defaultReviewerPrompt() string {
	return strings.TrimSpace(`You are the independent Tusker reviewer for {{ note.id }}.

Review only. Do not edit implementation files, merge, land, close, move refs, or change task state. Your only lifecycle output is one typed result submitted with ` + "`tusker review submit`" + `.

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

Checklist:
1. Read the task acceptance contract, proof mode, verification rows, evidence cards, and gates.
2. Inspect the current diff against the task scope. Call out surprise files or drive-by refactors.
3. Run the smallest verification commands needed to prove the acceptance contract.
4. Confirm project skill/domain canon changes only when the task changed durable project knowledge.
5. Risk alone does not justify a human gate. Treat risk as proof depth and landing safeguards, never as implicit human authority. Create or honor a human gate only for a named capability, external authority, unresolved product fact, or contractually subjective acceptance; do not re-approve choices already settled by the task/spec.
6. If a caveat changes scope, record it as an actionable typed finding.

Submit exactly one result for the injected review attempt: ` + "`tusker review submit {{ note.id }} --attempt {{ attempt.id }} --task-rev {{ review.task_rev }} --source-sha {{ review.source_sha }} --work-rev {{ review.work_rev }} --proof-fingerprint {{ review.proof_fingerprint }} --gate-fingerprint {{ review.gate_fingerprint }} --verdict pass|changes_requested|blocked --covers <acceptance-ids> --summary \"<bounded summary>\"`" + `. A pass requires complete objective proof and satisfied gates; changes_requested needs an actionable finding; blocked needs a machine, infrastructure, or genuine-human blocker.

Explicit blocking gates must be reported in the typed result; do not change gate or task state.`)
}

func reviewerPolicyCoversRisk(policy ReviewerPolicy, risk string) bool {
	return reviewerMayAutoCloseRisk(policy, risk)
}

func reviewerMayAutoCloseRisk(policy ReviewerPolicy, risk string) bool {
	return stringListContainsFold(policy.AutoCloseRisks, risk)
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
	// Orchestration is emitted as a hand-written, commented block below so that
	// the seeded gate stanza and proof policy carry per-field explanations that
	// yaml.Marshal cannot produce. Zero it here so the struct marshal omits the
	// key (omitempty) and the raw block is the single source of `orchestration:`.
	orch := wf.Orchestration
	wf.Orchestration = OrchestrationPolicy{}
	raw, _ := yaml.Marshal(wf)
	raw = append(raw, []byte(defaultProofAndGateBlock(orch))...)
	raw = append(raw, []byte("# runner escalation reasons: system_error|security_concern|unresolvable_conflict|stuck_loop\n")...)
	return "---\n" + strings.TrimSpace(string(raw)) + "\n---\n\n## Routing\n\nYou are working on {{ note.id }} for {{ project.name }}. Dispatch only makes sense when this task is in a dispatch state (`ready` or `rework`) and the workspace is ready at {{ workspace.path }}.\n\n## Hard stop check\n\nBefore doing work, run `tusker closeout status {{ note.id }} --json` when the V7 closeout command is available. If it reports `agent_action=stop_until_human_response`, do not validate, inspect files, spawn subagents, or modify Tusker records. Reply with the pending human gates/proof and whether the closeout checkpoint or review packet is still needed.\n\nRevalidate only after you edited files, a task/gate/evidence state changed, the closeout fingerprint no longer matches, or the user explicitly asked for fresh validation.\n\n## Prompt\n\nUse the installed Tusker skill bundle for durable task semantics and proof discipline. Work inside {{ workspace.path }}. Treat {{ repo.root }} as the source repository root for context only unless the task explicitly requires comparing against it.\n\nItem: {{ note.title }}\nRecord: {{ note.record_id }}\nType: {{ note.type }}\nAttempt: {{ attempt.number }}\nWorkflow: {{ workflow.path }}\nVault: {{ vault.path }}\n\n## Command budget\n\nUse the smallest command that proves or locates the next fact. Prefer packets/capsules, path-scoped status/search, repo-configured wrappers and build-lock/status commands, and redirected validation logs with small tails. Report validation as command + PASS/FAIL plus the first actionable failure; do not paste raw transcripts or repeat unchanged-state updates. Gates over records: write only artifacts a gate consumes (verify rows covering acceptance) or a human decision. Proof is the smallest row set that covers the contract — one command row is a complete proof for a small task. When a guard refuses with a no-decision remedy (open an attempt, use a proposal), apply the remedy, continue, and report it in one line.\n\n## Worker protocol\n\nEach dispatched attempt starts with fresh runner context. Use the injected task packet, `.tusker/scratch/<TASK-ID>/PLAN.md`, and previous structured outcome as the handoff; do not query or replay predecessor transcripts. Work one task only. Search before implementing, do not add placeholders or stubs, and run the configured backpressure commands serially.\n\n## Merge lane guard\n\nDo not push or merge directly to the default branch/main. Finish the task proof, then use `tusker land {{ note.id }}`; the serialized landing lane is the only authorized path from task branches into integration branches and main.\n\n## External Apply Inputs\n\nSome tasks may have external apply inputs collected by Tusker under `architect/{{ note.id }}/` or a workspace-local mirror of that directory.\n\nWhen that directory contains exactly one `*.patch` or `*.diff` file:\n\n1. inspect the task acceptance and verification contract first;\n2. run `git apply --check --3way <patch>`;\n3. apply with `git apply --3way <patch>` only after the check passes;\n4. resolve conflicts only when the resolution is mechanical and clearly within the task contract;\n5. run the task verification commands;\n6. record compact verification evidence;\n7. use `tusker finish {{ note.id }} --request-review` when machine proof is complete;\n8. create a concrete gate or move to rework/blocked when proof cannot be completed.\n\nIf there are zero patches, multiple patches, a patch outside scope, or an ambiguous conflict, stop and report the blocker through Tusker. Do not invent or silently repair patches.\n\n## Completion contract\n\nSatisfy the task proof mode. For proof_mode=inline, record concise verification rows with `tusker verify add`; do not create evidence files. For card/artifact/audit, create only the evidence the proof mode requires. When machine work is complete and only human-owned proof or gates remain, run `tusker closeout <task-id> --emit-packet --validate \"<command>\"`, then stop. When the work is demonstrably ready for verification, use `tusker finish <task-id> --request-review` so the task reaches `review` or a branch-safe `propose status ... --status review` proposal is created. Attempt handoff alone is not a review request. If proof is blocked, create/propose a gate with a concrete owner, action, and verification instead of appending negative evidence.\n\n## Reviewer contract\n\nIf `reviewer.enabled` is true, tasks in `review` may be dispatched to `reviewer.runner` for independent review. The reviewer must not edit implementation files. Independent reviewers may verify and close every risk tier after required objective proof and explicit gates pass. High and critical risk increase proof depth and landing safeguards; they do not imply human authority.\n\n## Retry policy\n\nRetry only transient infrastructure failures. Human-directed rework creates a new task revision; runtime activity remains in the run/lease store.\n\n## Human override policy\n\nHumans may edit tasks directly, but runtime state belongs to the daemon store.\n"
}

// defaultProofAndGateBlock renders the seeded proof policy and the
// orchestration/gate stanza as commented YAML. yaml.Marshal cannot emit
// comments, and the gate floors are project-specific placeholders that a fresh
// init must land already explained, so this block is authored by hand. The
// orchestration defaults (branch_age_warning_hours, batch_gate) are taken from
// the workflow struct so the raw block stays in agreement with defaultWorkflow().
func defaultProofAndGateBlock(orch OrchestrationPolicy) string {
	return fmt.Sprintf(`# Declared proof policy for this repo. These are the defaults Tusker already
# applies at task-create time (defaultV7ProofMode / defaultV7EvidenceBudget);
# this stanza records the repo's policy for humans and agents to read and is not
# itself consulted at runtime, so editing it does not change resolution.
# Evidence-by-risk-class: inline is the floor; only evidence-bearing modes
# attach files.
proof:
  # proof_mode is the default proof depth Tusker assigns a new task that does
  # not declare its own: inline for every risk class EXCEPT critical, which
  # defaults to audit. inline records verification rows (command + PASS/FAIL)
  # directly on the task and writes no evidence files.
  proof_mode: inline
  # proof_mode for risk=critical tasks; audit adds independent_review and
  # evidence files on top of the inline test proof.
  proof_mode_critical: audit
  # evidence_budget caps the evidence files a task may attach. 0 keeps inline
  # tasks file-free; only the evidence-bearing modes below raise this.
  evidence_budget: 0
  # Evidence files are required ONLY for these evidence-bearing proof modes.
  # Every other mode (inline, focused_test, broad_test, ...) proves inline with
  # no files.
  evidence_bearing_modes:
    - card
    - artifact
    - audit
orchestration:
  # branch_age_warning_hours warns when a task branch outlives this many hours.
  branch_age_warning_hours: %d
  batch_gate:
    # enabled turns on the periodic wave-boundary batch gate.
    enabled: %t
    # period_hours is the batch-gate cycle length when no windows are set.
    period_hours: %d
    # max_repairs caps repair continuations Tusker attempts per batch cycle.
    max_repairs: %d
  # orchestration.gate is this project's gate contract. The floor values below
  # ship COMMENTED OUT as placeholders: uncomment and set them to the project's
  # real, measured toolchain values before relying on the gate. Nothing here
  # inherits an unmeasured floor.
  gate:
    # profile is the canonical gate profile name. Replace this placeholder; a
    # run requesting a different profile is refused rather than discarding the
    # warm build.
    profile: default
    # harvest_commands is the runner's no-fail-fast test/build form, e.g.
    # "go test ./..." or "cargo nextest run --no-fail-fast". Defaults to the
    # batch gate's commands when empty.
    harvest_commands:
      - make test
    # min_free_disk_gb MUST be MEASURED against this project's real peak build
    # footprint, never guessed, before you uncomment it. On 2026-07-20 an
    # unmeasured guess of 15 GB authorized a doomed run: it died on a full disk
    # mid-gate, and its recovery deleted the build cache the next run needed.
    # Measure the peak footprint and set the floor above it.
    # min_free_disk_gb: <measured-peak-build-gb>
    # defect_target_regex has exactly one capture group naming the failing
    # target, e.g. "^--- FAIL: (\S+)" for Go.
    defect_target_regex: '^--- FAIL: (\S+)'
    # defect_line_limit caps each harvested defect excerpt.
    defect_line_limit: 12
    # scopes enable the Stage 1 per-change gate ("tusker gate --changed"): map an
    # area of the repo to the harvest commands that cover it, and a change is
    # gated on only the scopes it touched. A touched path that no scope owns fails
    # closed to the full harvest_commands set above rather than being skipped, so
    # scopes narrow proof cost without ever narrowing coverage. Uncomment and set
    # to this project's real areas; leave empty to only ever run the whole gate.
    # scopes:
    #   - name: api
    #     paths:
    #       - internal/api
    #     commands:
    #       - go test ./internal/api/...
    #   - name: store
    #     paths:
    #       - internal/store
    #     commands:
    #       - go test ./internal/store/...
`, orch.BranchAgeWarningHours, orch.BatchGate.Enabled, orch.BatchGate.PeriodHours, orch.BatchGate.MaxRepairs)
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
	_, configured := data["scheduled_promotion"]
	provenance := "workflow"
	if !configured {
		provenance = "migration default (scheduled_promotion absent)"
	}
	wf.ScheduledPromotion.Effective = scheduledPromotionProjection(wf.ScheduledPromotion, configured, provenance)
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
	resolved, err := resolveTuskerConfig(vaultPath)
	if err != nil {
		return wfFile, err
	}
	cfg := resolved.Config
	if !v7AutomationConfigPresent(cfg) {
		scope, err := resolveAutomationDispatchScope(resolved, wfFile.Data.AutomationEnabled)
		if err != nil {
			return wfFile, err
		}
		wfFile.Data.DispatchScope = scope
		mode, err := resolveCompletionReactorMode(resolved, wfFile.Data.AutomationEnabled)
		if err != nil {
			return wfFile, err
		}
		wfFile.Data.CompletionReactor = mode
		return wfFile, nil
	}
	wf := wfFile.Data
	if cfg.Automation.Enabled != nil {
		wf.AutomationEnabled = *cfg.Automation.Enabled
	}
	scope, err := resolveAutomationDispatchScope(resolved, wf.AutomationEnabled)
	if err != nil {
		return wfFile, err
	}
	wf.DispatchScope = scope
	mode, err := resolveCompletionReactorMode(resolved, wf.AutomationEnabled)
	if err != nil {
		return wfFile, err
	}
	wf.CompletionReactor = mode
	triggerStates := normalizeList(cfg.Automation.TriggerStates)
	if !resolvedConfigKeyPresent(resolved, "automation.trigger_states") {
		triggerStates = []string{"ready", "rework"}
	}
	if containsString(triggerStates, "active") && strings.TrimSpace(cfg.Automation.LegacyProfile) == "" {
		configPath := sourcePathForConfigKey(resolved.Layers, "automation.trigger_states")
		return wfFile, tuskerError(errorConfigInvalid, "automation.trigger_states must not include legacy active without automation.legacy_profile", withPath(configPath), withHint("use ready,rework or set automation.legacy_profile: legacy_active"))
	}
	wf.Tracker.ActiveStates = triggerStates
	if resolvedConfigKeyPresent(resolved, "automation.default_runner") {
		wf.Agents.Default = strings.TrimSpace(cfg.Automation.DefaultRunner)
	}
	if resolvedConfigKeyPresent(resolved, "automation.enabled_runners") {
		wf.Agents.Enabled = normalizeList(cfg.Automation.EnabledRunners)
	}
	wf.RunnerProfiles = runnerProfilesFromSchema(cfg.Automation.Profiles)
	wf.RunnerProfileSources = runnerProfileSourcesFromLayers(wf.RunnerProfiles, resolved.Layers)
	wf.RunnerDefaultProfile = strings.TrimSpace(cfg.Automation.DefaultProfile)
	if resolvedConfigKeyPresent(resolved, "automation.lane_profiles") {
		wf.RunnerLaneProfiles = map[string]string{}
		for lane, profile := range cfg.Automation.LaneProfiles {
			wf.RunnerLaneProfiles[strings.TrimSpace(lane)] = strings.TrimSpace(profile)
		}
		if len(wf.RunnerLaneProfiles) == 0 {
			wf.RunnerLaneProfiles = nil
		}
	}
	if resolvedConfigKeyPresent(resolved, "automation.routing") {
		wf.RunnerRouting = runnerRoutingFromSchema(cfg.Automation.Routing)
	}
	if resolvedConfigKeyPresent(resolved, "automation.denylist") {
		wf.RunnerDenylist = runnerDenylistFromSchema(cfg.Automation.Denylist)
	}
	if wf.Reviewer.Enabled && !stringListContainsFold(wf.Agents.Enabled, wf.Reviewer.Runner) {
		wf.Reviewer.Runner = wf.Agents.Default
	}
	if resolvedConfigKeyPresent(resolved, "automation.concurrency.max_active_runs") {
		wf.Agents.MaxConcurrentAgents = cfg.Automation.Concurrency.MaxActiveRuns
	}
	if resolvedConfigKeyPresent(resolved, "automation.concurrency.max_active_runs_per_project") {
		wf.Runtime.MaxActiveRunsPerProject = cfg.Automation.Concurrency.MaxActiveRunsPerProject
	}
	if resolvedConfigKeyPresent(resolved, "automation.concurrency.max_continuation_retries") {
		wf.Runtime.MaxContinuationRetries = cfg.Automation.Concurrency.MaxContinuationRetries
	}
	if resolvedConfigKeyPresent(resolved, "automation.concurrency.max_concurrent_by_state") {
		wf.Agents.MaxConcurrentAgentsByState = cfg.Automation.Concurrency.MaxConcurrentByState
	}
	if cfg.Automation.Budget.Enabled != nil {
		wf.Runtime.Budget.Enabled = *cfg.Automation.Budget.Enabled
	}
	if resolvedConfigKeyPresent(resolved, "automation.budget.per_attempt_input_tokens") {
		wf.Runtime.Budget.PerAttemptInputTokens = cfg.Automation.Budget.PerAttemptInputTokens
	}
	if resolvedConfigKeyPresent(resolved, "automation.budget.per_attempt_output_tokens") {
		wf.Runtime.Budget.PerAttemptOutputTokens = cfg.Automation.Budget.PerAttemptOutputTokens
	}
	if resolvedConfigKeyPresent(resolved, "automation.budget.per_task_input_tokens") {
		wf.Runtime.Budget.PerTaskInputTokens = cfg.Automation.Budget.PerTaskInputTokens
	}
	if resolvedConfigKeyPresent(resolved, "automation.budget.per_task_output_tokens") {
		wf.Runtime.Budget.PerTaskOutputTokens = cfg.Automation.Budget.PerTaskOutputTokens
	}
	if resolvedConfigKeyPresent(resolved, "automation.budget.daily_input_tokens") {
		wf.Runtime.Budget.DailyInputTokens = cfg.Automation.Budget.DailyInputTokens
	}
	if resolvedConfigKeyPresent(resolved, "automation.budget.daily_output_tokens") {
		wf.Runtime.Budget.DailyOutputTokens = cfg.Automation.Budget.DailyOutputTokens
	}
	wf.Runtime.Budget = withDefaultRuntimeBudgetConfig(wf.Runtime.Budget)
	if resolvedConfigKeyPresent(resolved, "automation.external_loop.max_cycles") {
		wf.ExternalLoop.MaxCycles = cfg.Automation.ExternalLoop.MaxCycles
	}
	if resolvedConfigKeyPresent(resolved, "automation.external_loop.max_repair_continuations") {
		wf.ExternalLoop.MaxRepairContinuations = cfg.Automation.ExternalLoop.MaxRepairContinuations
	}
	if resolvedConfigKeyPresent(resolved, "automation.external_loop.max_external_threads") {
		wf.ExternalLoop.MaxExternalThreads = cfg.Automation.ExternalLoop.MaxExternalThreads
	}
	if resolvedConfigKeyPresent(resolved, "automation.external_loop.wall_clock_timeout_hours") {
		wf.ExternalLoop.WallClockTimeoutHours = cfg.Automation.ExternalLoop.WallClockTimeoutHours
	}
	workspaceRootOverride := strings.TrimSpace(cfg.Automation.Workspace.Root)
	if resolvedConfigKeyPresent(resolved, "automation.workspace.root") {
		wf.Workspace.Root = workspaceRootOverride
	}
	if resolvedConfigKeyPresent(resolved, "automation.workspace.strategy") {
		wf.Workspace.Strategy = strings.TrimSpace(cfg.Automation.Workspace.Strategy)
		if workspaceStrategyFromWorkflow(wf.Workspace.Strategy) != WorkspaceStrategyShared && workspaceRootOverride == "" && strings.TrimSpace(wf.Workspace.Root) == "." {
			wf.Workspace.Root = "workspaces"
		}
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
	if resolvedConfigKeyPresent(resolved, "automation.fanout.enabled") {
		wf.Fanout.Enabled = cfg.Automation.Fanout.Enabled
	}
	if resolvedConfigKeyPresent(resolved, "automation.fanout.max_children") {
		wf.Fanout.MaxChildren = cfg.Automation.Fanout.MaxChildren
	}
	if resolvedConfigKeyPresent(resolved, "automation.fanout.allowed_child_types") {
		wf.Fanout.AllowedChildTypes = normalizeList(cfg.Automation.Fanout.AllowedChildTypes)
	}
	if resolvedConfigKeyPresent(resolved, "automation.fanout.merge_rule") {
		wf.Fanout.MergeRule = strings.TrimSpace(cfg.Automation.Fanout.MergeRule)
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
		strings.TrimSpace(cfg.Automation.CompletionReactor.Mode) != "" ||
		len(cfg.Automation.TriggerStates) > 0 ||
		strings.TrimSpace(cfg.Automation.DefaultRunner) != "" ||
		len(cfg.Automation.EnabledRunners) > 0 ||
		strings.TrimSpace(cfg.Automation.DefaultProfile) != "" ||
		len(cfg.Automation.LaneProfiles) > 0 ||
		len(cfg.Automation.Profiles) > 0 ||
		len(cfg.Automation.Routing) > 0 ||
		len(cfg.Automation.Denylist) > 0 ||
		strings.TrimSpace(cfg.Automation.Workspace.Root) != "" ||
		strings.TrimSpace(cfg.Automation.Workspace.Strategy) != "" ||
		cfg.Automation.Concurrency.MaxActiveRuns > 0 ||
		cfg.Automation.Concurrency.MaxActiveRunsPerProject > 0 ||
		cfg.Automation.Concurrency.MaxContinuationRetries > 0 ||
		len(cfg.Automation.Concurrency.MaxConcurrentByState) > 0 ||
		cfg.Automation.Budget.Enabled != nil ||
		cfg.Automation.Budget.PerAttemptInputTokens > 0 ||
		cfg.Automation.Budget.PerAttemptOutputTokens > 0 ||
		cfg.Automation.Budget.PerTaskInputTokens > 0 ||
		cfg.Automation.Budget.PerTaskOutputTokens > 0 ||
		cfg.Automation.Budget.DailyInputTokens > 0 ||
		cfg.Automation.Budget.DailyOutputTokens > 0 ||
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
