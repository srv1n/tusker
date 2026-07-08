package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type RunnerName string

const (
	RunnerCodex          RunnerName = "codex"
	RunnerCodexAppServer RunnerName = "codex_app_server"
	RunnerCodexExec      RunnerName = "codex_exec"
	RunnerCodexCloud     RunnerName = "codex_cloud"
	RunnerClaude         RunnerName = "claude-code"
)

type LeaseState string

const (
	LeaseStateUnclaimed        LeaseState = "unclaimed"
	LeaseStateClaimed          LeaseState = "claimed"
	LeaseStateRunning          LeaseState = "running"
	LeaseStateRetryQueued      LeaseState = "retry_queued"
	LeaseStateParkedNoProgress LeaseState = "parked_no_progress"
	LeaseStateParkedBudget     LeaseState = "parked_budget"
	LeaseStateInterrupted      LeaseState = "interrupted"
	LeaseStateReleased         LeaseState = "released"
)

type AttemptOutcome string

const (
	AttemptOutcomeNone             AttemptOutcome = "none"
	AttemptOutcomeSucceeded        AttemptOutcome = "succeeded"
	AttemptOutcomeBlocked          AttemptOutcome = "blocked"
	AttemptOutcomeFailed           AttemptOutcome = "failed"
	AttemptOutcomeCancelled        AttemptOutcome = "cancelled"
	AttemptOutcomeAbandoned        AttemptOutcome = "abandoned"
	AttemptOutcomeEarlyExit        AttemptOutcome = "early_exit"
	AttemptOutcomeDispatchDeclined AttemptOutcome = "dispatch_declined"
	AttemptOutcomeTurnCapExhausted AttemptOutcome = "turn_cap_exhausted"
	AttemptOutcomeWaitingForHuman  AttemptOutcome = "waiting_for_human"
	AttemptOutcomeWaitingForReview AttemptOutcome = "waiting_for_review"
	AttemptOutcomeBudgetExceeded   AttemptOutcome = "budget_exceeded"
)

type RunnerCapabilities struct {
	StructuredEvents    bool
	ResumeSession       bool
	ExplicitApprovals   bool
	Heartbeats          bool
	MachineFinalStatus  bool
	UsageMetrics        bool
	ArtifactEnumeration bool
}

type StartRequest struct {
	ProjectID       string
	RecordID        string
	ItemID          string
	AttemptID       string
	Lane            string
	WorkRevision    int
	LeaseGeneration int
	ActiveStates    []string
	WorkingDir      string
	WorkspacePath   string
	RepoRoot        string
	PromptPath      string
	EventSinkPath   string
	RawLogPath      string
	StatusPath      string
	Command         string
	RunnerProfile   string
	RunnerHarness   string
	RunnerModel     string
	RunnerEffort    string
	NotePath        string
	VaultPath       string
	Budget          map[string]any
	CodexPolicy     CodexPolicy
	ExternalLoop    ExternalLoopLaunchContext
}

type ResumeRequest struct {
	ProjectID       string
	RecordID        string
	ItemID          string
	AttemptID       string
	Lane            string
	WorkRevision    int
	LeaseGeneration int
	ActiveStates    []string
	SessionRef      string
	MessageRef      string
	WorkingDir      string
	WorkspacePath   string
	RepoRoot        string
	PromptPath      string
	EventSinkPath   string
	RawLogPath      string
	StatusPath      string
	Command         string
	RunnerProfile   string
	RunnerHarness   string
	RunnerModel     string
	RunnerEffort    string
	NotePath        string
	VaultPath       string
	CodexPolicy     CodexPolicy
	ExternalLoop    ExternalLoopLaunchContext
}

type ExternalLoopLaunchContext struct {
	Stage       string
	Action      string
	OriginJobID string
	EventID     string
	Reason      string
}

type CodexPolicy struct {
	ApprovalPolicy     string
	ThreadSandbox      string
	TurnSandboxPolicy  string
	TurnSandboxNetwork *bool
	TurnTimeoutMS      int
	ReadTimeoutMS      int
	StallTimeoutMS     int
	MaxTurns           int
	Extensions         ExtensionPolicy
}

type CodexCloudConfig struct {
	Command         string `yaml:"command" json:"command"`
	StatusCommand   string `yaml:"status_command" json:"status_command"`
	CollectCommand  string `yaml:"collect_command" json:"collect_command"`
	EnvironmentID   string `yaml:"environment_id" json:"environment_id"`
	ApplyMode       string `yaml:"apply_mode" json:"apply_mode"`
	PRMode          string `yaml:"pr_mode" json:"pr_mode"`
	ExternalCollect bool   `yaml:"external_collect,omitempty" json:"external_collect,omitempty"`

	ApprovalPolicy    string `yaml:"approval_policy,omitempty" json:"approval_policy,omitempty"`
	ThreadSandbox     string `yaml:"thread_sandbox,omitempty" json:"thread_sandbox,omitempty"`
	TurnSandboxPolicy string `yaml:"turn_sandbox_policy,omitempty" json:"turn_sandbox_policy,omitempty"`
	TurnTimeoutMS     int    `yaml:"turn_timeout_ms,omitempty" json:"turn_timeout_ms,omitempty"`
	ReadTimeoutMS     int    `yaml:"read_timeout_ms,omitempty" json:"read_timeout_ms,omitempty"`
	StallTimeoutMS    int    `yaml:"stall_timeout_ms,omitempty" json:"stall_timeout_ms,omitempty"`
	MaxTurns          int    `yaml:"max_turns,omitempty" json:"max_turns,omitempty"`
}

type StartResult struct {
	SessionRef   string
	MessageRef   string
	StartedAt    string
	FinishedAt   string
	PID          int
	PGID         int
	ProcessStart string
	StatusPath   string
	Capabilities RunnerCapabilities
	Completed    bool
	Outcome      AttemptOutcome
	Reason       string
	ExitCode     int

	CloudTaskID        string
	CloudStatus        string
	CloudEnvironmentID string
	CloudAttemptNumber int
	PullRequestURL     string
	ApplyRef           string
	LogsSummary        string
	FinalSummary       string
}

type ResumeResult = StartResult

type ReconcileRequest struct {
	Runner      string
	ProjectID   string
	RecordID    string
	AttemptID   string
	SessionRef  string
	CloudTaskID string
}

type ReconcileResult struct {
	LeaseState LeaseState
	Outcome    AttemptOutcome
	Reason     string

	CloudTaskID        string
	CloudStatus        string
	CloudEnvironmentID string
	CloudAttemptNumber int
	PullRequestURL     string
	ApplyRef           string
	LogsSummary        string
	FinalSummary       string
}

type InterruptRequest struct {
	AttemptID  string
	SessionRef string
}

type CollectRequest struct {
	AttemptID   string
	CloudTaskID string
}

type CollectResult struct {
	Artifacts map[string]string
}

type Runner interface {
	Name() RunnerName
	Capabilities() RunnerCapabilities
	Start(ctx context.Context, req StartRequest) (*StartResult, error)
	Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error)
	Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error)
	Interrupt(ctx context.Context, req InterruptRequest) error
	Collect(ctx context.Context, req CollectRequest) (*CollectResult, error)
}

func codexPolicyFromWorkflow(wf Workflow) CodexPolicy {
	return CodexPolicy{
		ApprovalPolicy:     wf.Codex.ApprovalPolicy,
		ThreadSandbox:      wf.Codex.ThreadSandbox,
		TurnSandboxPolicy:  wf.Codex.TurnSandboxPolicy,
		TurnSandboxNetwork: nil,
		TurnTimeoutMS:      wf.Codex.TurnTimeoutMS,
		ReadTimeoutMS:      wf.Codex.ReadTimeoutMS,
		StallTimeoutMS:     wf.Codex.StallTimeoutMS,
		MaxTurns:           wf.Codex.MaxTurns,
		Extensions:         wf.Extensions,
	}
}

func withDefaultCodexPolicy(policy CodexPolicy) CodexPolicy {
	defaults := codexPolicyFromWorkflow(defaultWorkflow())
	if strings.TrimSpace(policy.ApprovalPolicy) == "" {
		policy.ApprovalPolicy = defaults.ApprovalPolicy
	}
	if strings.TrimSpace(policy.ThreadSandbox) == "" {
		policy.ThreadSandbox = defaults.ThreadSandbox
	}
	if strings.TrimSpace(policy.TurnSandboxPolicy) == "" {
		policy.TurnSandboxPolicy = defaults.TurnSandboxPolicy
	}
	if policy.TurnTimeoutMS <= 0 {
		policy.TurnTimeoutMS = defaults.TurnTimeoutMS
	}
	if policy.ReadTimeoutMS <= 0 {
		policy.ReadTimeoutMS = defaults.ReadTimeoutMS
	}
	if policy.StallTimeoutMS <= 0 {
		policy.StallTimeoutMS = defaults.StallTimeoutMS
	}
	if policy.MaxTurns <= 0 {
		policy.MaxTurns = defaults.MaxTurns
	}
	policy.Extensions = withDefaultExtensionPolicy(policy.Extensions)
	return policy
}

func codexPolicyForLane(policy CodexPolicy, lane string) CodexPolicy {
	policy = withDefaultCodexPolicy(policy)
	if strings.TrimSpace(lane) == runLaneReview {
		policy.ApprovalPolicy = "never"
		policy.ThreadSandbox = "read-only"
		policy.TurnSandboxPolicy = "read-only"
	}
	return policy
}

func runnerWorkspaceCWD(runner RunnerName, workspacePath string) (string, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return "", tuskerError(errorConfigInvalid, fmt.Sprintf("%s runner requires workspace_path", runner))
	}
	abs, err := filepath.Abs(workspacePath)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func assertRunnerCommandDir(runner RunnerName, cmdDir, workspacePath string) error {
	expected, err := runnerWorkspaceCWD(runner, workspacePath)
	if err != nil {
		return err
	}
	actual, err := runnerWorkspaceCWD(runner, cmdDir)
	if err != nil {
		return err
	}
	if actual != expected {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("%s runner cwd invariant violated: cmd.Dir must equal workspace_path", runner))
	}
	return nil
}

func runnerEnv(req runnerLaunchEnv) []string {
	extensionPolicy := withDefaultExtensionPolicy(req.CodexPolicy.Extensions)
	extensionPolicyJSON, _ := json.Marshal(extensionPolicy)
	return append(runnerBaseEnv(),
		"TUSKER_PROJECT_ID="+req.ProjectID,
		"TUSKER_RECORD_ID="+req.RecordID,
		"TUSKER_ITEM_ID="+req.ItemID,
		"TUSKER_ATTEMPT_ID="+req.AttemptID,
		"TUSKER_RUN_LANE="+req.Lane,
		"TUSKER_WORK_REVISION="+fmt.Sprintf("%d", req.WorkRevision),
		"TUSKER_LEASE_GENERATION="+fmt.Sprintf("%d", req.LeaseGeneration),
		"TUSKER_WORKSPACE="+req.WorkspacePath,
		"TUSKER_WORKING_DIR="+req.WorkspacePath,
		"TUSKER_REPO_ROOT="+req.RepoRoot,
		"TUSKER_PROMPT_PATH="+req.PromptPath,
		"TUSKER_EVENT_SINK="+req.EventSinkPath,
		"TUSKER_RAW_LOG="+req.RawLogPath,
		"TUSKER_STATUS_PATH="+req.StatusPath,
		"TUSKER_NOTE_PATH="+req.NotePath,
		"TUSKER_VAULT="+req.VaultPath,
		"TUSKER_SESSION_REF="+req.SessionRef,
		"TUSKER_MESSAGE_REF="+req.MessageRef,
		"TUSKER_CODEX_APPROVAL_POLICY="+req.CodexPolicy.ApprovalPolicy,
		"TUSKER_CODEX_THREAD_SANDBOX="+req.CodexPolicy.ThreadSandbox,
		"TUSKER_CODEX_TURN_SANDBOX_POLICY="+req.CodexPolicy.TurnSandboxPolicy,
		"TUSKER_CODEX_TURN_NETWORK_ACCESS="+networkAccessEnvValue(req.CodexPolicy.TurnSandboxNetwork),
		"TUSKER_CODEX_TURN_TIMEOUT_MS="+fmt.Sprintf("%d", req.CodexPolicy.TurnTimeoutMS),
		"TUSKER_CODEX_READ_TIMEOUT_MS="+fmt.Sprintf("%d", req.CodexPolicy.ReadTimeoutMS),
		"TUSKER_CODEX_STALL_TIMEOUT_MS="+fmt.Sprintf("%d", req.CodexPolicy.StallTimeoutMS),
		"TUSKER_CODEX_MAX_TURNS="+fmt.Sprintf("%d", req.CodexPolicy.MaxTurns),
		"TUSKER_RUNNER_PROFILE="+req.RunnerProfile,
		"TUSKER_RUNNER_HARNESS="+req.RunnerHarness,
		"TUSKER_RUNNER_MODEL="+req.RunnerModel,
		"TUSKER_RUNNER_EFFORT="+req.RunnerEffort,
		"TUSKER_EXTERNAL_LOOP_STAGE="+req.ExternalLoop.Stage,
		"TUSKER_EXTERNAL_LOOP_ACTION="+req.ExternalLoop.Action,
		"TUSKER_EXTERNAL_ORIGIN_JOB_ID="+req.ExternalLoop.OriginJobID,
		"TUSKER_EXTERNAL_LOOP_EVENT_ID="+req.ExternalLoop.EventID,
		"TUSKER_EXTERNAL_LOOP_REASON="+req.ExternalLoop.Reason,
		"TUSKER_EXTENSIONS_ENABLED="+fmt.Sprintf("%t", extensionPolicy.Enabled),
		"TUSKER_EXTENSION_ALLOWED_TOOLS="+strings.Join(extensionPolicy.AllowedTools, ","),
		"TUSKER_EXTENSION_ALLOWED_MCPS="+strings.Join(extensionPolicy.AllowedMCPs, ","),
		"TUSKER_EXTENSION_ALLOW_TUSKER_READ_TOOLS="+fmt.Sprintf("%t", extensionPolicy.AllowTuskerReadTools),
		"TUSKER_EXTENSION_POLICY_JSON="+string(extensionPolicyJSON),
	)
}

func withDefaultExtensionPolicy(policy ExtensionPolicy) ExtensionPolicy {
	out := policy
	out.AllowedTools = append([]string{}, policy.AllowedTools...)
	out.AllowedMCPs = append([]string{}, policy.AllowedMCPs...)
	return out
}

type runnerLaunchEnv struct {
	ProjectID       string
	RecordID        string
	ItemID          string
	AttemptID       string
	Lane            string
	WorkRevision    int
	LeaseGeneration int
	WorkspacePath   string
	RepoRoot        string
	PromptPath      string
	EventSinkPath   string
	RawLogPath      string
	StatusPath      string
	NotePath        string
	VaultPath       string
	SessionRef      string
	MessageRef      string
	RunnerProfile   string
	RunnerHarness   string
	RunnerModel     string
	RunnerEffort    string
	CodexPolicy     CodexPolicy
	ExternalLoop    ExternalLoopLaunchContext
}

func networkAccessEnvValue(value *bool) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%t", *value)
}
