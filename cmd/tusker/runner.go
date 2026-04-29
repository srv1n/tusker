package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RunnerName string

const (
	RunnerCodex  RunnerName = "codex"
	RunnerClaude RunnerName = "claude-code"
)

type LeaseState string

const (
	LeaseStateUnclaimed   LeaseState = "unclaimed"
	LeaseStateClaimed     LeaseState = "claimed"
	LeaseStateRunning     LeaseState = "running"
	LeaseStateRetryQueued LeaseState = "retry_queued"
	LeaseStateInterrupted LeaseState = "interrupted"
	LeaseStateReleased    LeaseState = "released"
)

type AttemptOutcome string

const (
	AttemptOutcomeNone      AttemptOutcome = "none"
	AttemptOutcomeSucceeded AttemptOutcome = "succeeded"
	AttemptOutcomeBlocked   AttemptOutcome = "blocked"
	AttemptOutcomeFailed    AttemptOutcome = "failed"
	AttemptOutcomeCancelled AttemptOutcome = "cancelled"
	AttemptOutcomeAbandoned AttemptOutcome = "abandoned"
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
	ProjectID     string
	RecordID      string
	ItemID        string
	AttemptID     string
	WorkRevision  int
	ActiveStates  []string
	WorkingDir    string
	WorkspacePath string
	RepoRoot      string
	PromptPath    string
	EventSinkPath string
	RawLogPath    string
	StatusPath    string
	Command       string
	NotePath      string
	VaultPath     string
	Budget        map[string]any
	CodexPolicy   CodexPolicy
}

type ResumeRequest struct {
	ProjectID     string
	RecordID      string
	ItemID        string
	AttemptID     string
	WorkRevision  int
	ActiveStates  []string
	SessionRef    string
	MessageRef    string
	WorkingDir    string
	WorkspacePath string
	RepoRoot      string
	PromptPath    string
	EventSinkPath string
	RawLogPath    string
	StatusPath    string
	Command       string
	NotePath      string
	VaultPath     string
	CodexPolicy   CodexPolicy
}

type CodexPolicy struct {
	ApprovalPolicy    string
	ThreadSandbox     string
	TurnSandboxPolicy string
	TurnTimeoutMS     int
	ReadTimeoutMS     int
	StallTimeoutMS    int
	MaxTurns          int
	Extensions        ExtensionPolicy
}

type StartResult struct {
	SessionRef   string
	MessageRef   string
	StartedAt    string
	FinishedAt   string
	PID          int
	StatusPath   string
	Capabilities RunnerCapabilities
	Completed    bool
	Outcome      AttemptOutcome
	Reason       string
	ExitCode     int
}

type ResumeResult = StartResult

type ReconcileRequest struct {
	Runner     string
	ProjectID  string
	RecordID   string
	AttemptID  string
	SessionRef string
}

type ReconcileResult struct {
	LeaseState LeaseState
	Outcome    AttemptOutcome
	Reason     string
}

type InterruptRequest struct {
	AttemptID  string
	SessionRef string
}

type CollectRequest struct {
	AttemptID string
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
		ApprovalPolicy:    wf.Codex.ApprovalPolicy,
		ThreadSandbox:     wf.Codex.ThreadSandbox,
		TurnSandboxPolicy: wf.Codex.TurnSandboxPolicy,
		TurnTimeoutMS:     wf.Codex.TurnTimeoutMS,
		ReadTimeoutMS:     wf.Codex.ReadTimeoutMS,
		StallTimeoutMS:    wf.Codex.StallTimeoutMS,
		MaxTurns:          wf.Codex.MaxTurns,
		Extensions:        wf.Extensions,
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
	return append(os.Environ(),
		"TUSKER_PROJECT_ID="+req.ProjectID,
		"TUSKER_RECORD_ID="+req.RecordID,
		"TUSKER_ITEM_ID="+req.ItemID,
		"TUSKER_ATTEMPT_ID="+req.AttemptID,
		"TUSKER_WORK_REVISION="+fmt.Sprintf("%d", req.WorkRevision),
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
		"TUSKER_CODEX_TURN_TIMEOUT_MS="+fmt.Sprintf("%d", req.CodexPolicy.TurnTimeoutMS),
		"TUSKER_CODEX_READ_TIMEOUT_MS="+fmt.Sprintf("%d", req.CodexPolicy.ReadTimeoutMS),
		"TUSKER_CODEX_STALL_TIMEOUT_MS="+fmt.Sprintf("%d", req.CodexPolicy.StallTimeoutMS),
		"TUSKER_CODEX_MAX_TURNS="+fmt.Sprintf("%d", req.CodexPolicy.MaxTurns),
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
	ProjectID     string
	RecordID      string
	ItemID        string
	AttemptID     string
	WorkRevision  int
	WorkspacePath string
	RepoRoot      string
	PromptPath    string
	EventSinkPath string
	RawLogPath    string
	StatusPath    string
	NotePath      string
	VaultPath     string
	SessionRef    string
	MessageRef    string
	CodexPolicy   CodexPolicy
}
