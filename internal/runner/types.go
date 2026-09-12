// Package runner is the provider-neutral boundary for installed coding-agent
// processes. It deliberately knows nothing about Tusker tasks, leases, waves,
// vaults, or verification state.
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const ConformanceSchema = "tusker.runner-conformance/v1"

type Transport string

const (
	TransportCLI Transport = "cli"
	TransportACP Transport = "acp_stdio"
)

type PermissionPreset string

const (
	PresetReadOnly         PermissionPreset = "read-only"
	PresetWorkspaceOffline PermissionPreset = "workspace-write-offline"
	PresetWorkspaceNetwork PermissionPreset = "workspace-write-network"
	PresetDangerFullAccess PermissionPreset = "danger-full-access"
)

// AccessControl is the fixed provider-neutral set of controls exposed by
// Tusker. It is intentionally closed rather than a provider-specific policy
// dictionary, so support can be qualified consistently.
type AccessControl string

const (
	AccessWorkspaceWrite      AccessControl = "workspace_write"
	AccessReferenceRead       AccessControl = "reference_read"
	AccessReferenceWrite      AccessControl = "reference_write"
	AccessPrivateReadDeny     AccessControl = "private_read_deny"
	AccessPrivateWriteDeny    AccessControl = "private_write_deny"
	AccessNetwork             AccessControl = "network"
	AccessDestructiveApproval AccessControl = "destructive_approval"
	AccessReviewOnly          AccessControl = "review_only"
)

type AccessMechanism string

const (
	AccessNativeSetting AccessMechanism = "native_setting"
	AccessNativeHook    AccessMechanism = "native_hook"
	AccessAdvisory      AccessMechanism = "advisory"
	AccessUnsupported   AccessMechanism = "unsupported"
)

type AccessFolder struct {
	Path   string `json:"path" yaml:"path"`
	Access string `json:"access" yaml:"access"`
}

// AgentAccessV1 is the normalized authored access object. It is independent
// of any provider's native flags.
type AgentAccessV1 struct {
	Schema             string         `json:"schema" yaml:"schema"`
	Mode               string         `json:"mode" yaml:"mode"`
	Network            bool           `json:"network" yaml:"network"`
	DestructiveActions string         `json:"destructive_actions" yaml:"destructive_actions"`
	Folders            []AccessFolder `json:"folders" yaml:"folders"`
	PrivateFolders     []string       `json:"private_folders" yaml:"private_folders"`
}

type AccessIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Remedy  string `json:"remedy"`
}

type ControlSupport struct {
	Control   AccessControl   `json:"control"`
	Mechanism AccessMechanism `json:"mechanism"`
	Coverage  string          `json:"coverage"`
	Evidence  []string        `json:"evidence"`
}

// CommandBehavior is the deliberately closed command-policy vocabulary shown
// to operators and consumed by provider adapters. It is not a shell rule
// language: adapters classify the bounded native tool request, then this
// evaluator applies the same precedence everywhere.
type CommandBehavior string

const (
	CommandAutomatic CommandBehavior = "automatic"
	CommandAsk       CommandBehavior = "ask"
	CommandBlock     CommandBehavior = "block"
)

// CommandPolicy describes the fixed behavior classes of an access profile.
// Catastrophic, private, and outside-scope requests are always blocked;
// review-only writes are always blocked. Only recognized destructive work is
// configurable between ask and block.
type CommandPolicy struct {
	Routine          CommandBehavior `json:"routine"`
	Destructive      CommandBehavior `json:"destructive"`
	Catastrophic     CommandBehavior `json:"catastrophic"`
	Private          CommandBehavior `json:"private"`
	OutsideWorkspace CommandBehavior `json:"outside_workspace"`
	ReviewWrite      CommandBehavior `json:"review_write"`
	Rules            []CommandRule   `json:"rules,omitempty"`
}

type CommandRule struct {
	ID       string          `json:"id"`
	Behavior CommandBehavior `json:"behavior"`
	Examples []string        `json:"examples"`
	Coverage string          `json:"coverage"`
}

// CommandPolicyRequest is the normalized classification supplied by a native
// adapter. Path and command parsing remain adapter-scoped; behavior precedence
// does not.
type CommandPolicyRequest struct {
	Mutating         bool
	Destructive      bool
	Catastrophic     bool
	Private          bool
	OutsideWorkspace bool
	ReviewOnly       bool
}

type CommandPolicyDecision struct {
	Behavior CommandBehavior `json:"behavior"`
	Rule     string          `json:"rule"`
	Reason   string          `json:"reason,omitempty"`
}

// NewCommandPolicy returns the effective fixed policy for the ordinary access
// modes. The mandatory classes are never configurable.
func NewCommandPolicy(reviewOnly bool, destructiveActions string) CommandPolicy {
	destructive := CommandAsk
	if destructiveActions == "deny" || reviewOnly {
		destructive = CommandBlock
	}
	return CommandPolicy{
		Routine:          CommandAutomatic,
		Destructive:      destructive,
		Catastrophic:     CommandBlock,
		Private:          CommandBlock,
		OutsideWorkspace: CommandBlock,
		ReviewWrite:      CommandBlock,
		Rules: []CommandRule{
			{ID: "routine", Behavior: CommandAutomatic, Examples: []string{"project edits", "builds and tests", "git status", "git add", "git commit"}, Coverage: "supported routine project work"},
			{ID: "destructive", Behavior: destructive, Examples: []string{"bounded recursive delete", "git reset --hard", "destructive git clean", "force push"}, Coverage: "recognized native command requests"},
			{ID: "catastrophic", Behavior: CommandBlock, Examples: []string{"filesystem root", "home root", "raw-device and format operations"}, Coverage: "recognized catastrophic targets"},
			{ID: "private", Behavior: CommandBlock, Examples: []string{"configured private folders"}, Coverage: "resolved private-folder paths"},
			{ID: "outside_workspace", Behavior: CommandBlock, Examples: []string{"outside writable roots"}, Coverage: "resolved command targets"},
			{ID: "review_write", Behavior: CommandBlock, Examples: []string{"all project writes in Review only"}, Coverage: "native tool requests"},
		},
	}
}

func (p CommandPolicy) IsZero() bool {
	return p.Routine == "" && p.Destructive == "" && p.Catastrophic == "" && p.Private == "" && p.OutsideWorkspace == "" && p.ReviewWrite == "" && len(p.Rules) == 0
}

// EvaluateCommandPolicy applies the policy precedence used by all supported
// callback routes. An approval can only be offered for the resulting ask
// behavior; it can never turn a block into an allow.
func EvaluateCommandPolicy(policy CommandPolicy, request CommandPolicyRequest) CommandPolicyDecision {
	if policy.IsZero() {
		policy = NewCommandPolicy(request.ReviewOnly, "ask")
	}
	if request.Catastrophic {
		return CommandPolicyDecision{Behavior: CommandBlock, Rule: "catastrophic", Reason: "catastrophic targets are always blocked"}
	}
	if request.Private {
		return CommandPolicyDecision{Behavior: CommandBlock, Rule: "private", Reason: "private-folder targets are always blocked"}
	}
	if request.OutsideWorkspace {
		return CommandPolicyDecision{Behavior: CommandBlock, Rule: "outside_workspace", Reason: "targets outside writable scope are always blocked"}
	}
	if request.ReviewOnly && request.Mutating {
		return CommandPolicyDecision{Behavior: CommandBlock, Rule: "review_write", Reason: "review-only profiles cannot write project files"}
	}
	if request.Destructive {
		behavior := policy.Destructive
		if behavior == "" {
			behavior = CommandAsk
		}
		return CommandPolicyDecision{Behavior: behavior, Rule: "destructive", Reason: "recognized destructive action"}
	}
	behavior := policy.Routine
	if behavior == "" {
		behavior = CommandAutomatic
	}
	return CommandPolicyDecision{Behavior: behavior, Rule: "routine", Reason: "routine project work"}
}

type ResolvedAccess struct {
	Requested      any              `json:"requested"`
	Effective      EffectivePolicy  `json:"effective"`
	Folders        []AccessFolder   `json:"folders,omitempty"`
	References     []string         `json:"references,omitempty"`
	PrivateFolders []string         `json:"private_folders,omitempty"`
	Controls       []ControlSupport `json:"controls"`
	CommandPolicy  *CommandPolicy   `json:"command_policy,omitempty"`
	State          string           `json:"state"`
	Issues         []AccessIssue    `json:"issues"`
	Fingerprint    string           `json:"fingerprint"`
}

type HarnessDefinition struct {
	ID                string            `json:"id" yaml:"id"`
	Provider          string            `json:"provider" yaml:"provider"`
	Transport         Transport         `json:"transport" yaml:"transport"`
	Dialect           string            `json:"dialect,omitempty" yaml:"dialect,omitempty"`
	Executable        string            `json:"executable" yaml:"executable"`
	Args              []string          `json:"args" yaml:"args"`
	Environment       map[string]string `json:"environment,omitempty" yaml:"environment,omitempty"`
	Profile           string            `json:"profile,omitempty" yaml:"profile,omitempty"`
	NativeContainment bool              `json:"native_containment,omitempty" yaml:"native_containment,omitempty"`
	SchemaVersion     int               `json:"schema_version" yaml:"schema_version"`
}

type RunInput struct {
	Prompt         string           `json:"-"`
	Workspace      string           `json:"workspace"`
	Preset         PermissionPreset `json:"preset"`
	Model          string           `json:"model,omitempty"`
	Effort         string           `json:"effort,omitempty"`
	AttemptID      string           `json:"attempt_id,omitempty"`
	ResumeSession  string           `json:"resume_session,omitempty"`
	Deadline       time.Duration    `json:"-"`
	OutputLimit    int              `json:"output_limit,omitempty"`
	SearchPath     string           `json:"-"`
	LiveCanary     bool             `json:"-"`
	VerifiedAuth   bool             `json:"-"`
	PolicyCanary   bool             `json:"-"`
	ProtectedPath  string           `json:"-"`
	Exercise       string           `json:"-"`
	ExerciseScript string           `json:"-"`
	Access         *AgentAccessV1   `json:"access,omitempty"`
	// ResolvedAccess is supplied by the owning command package after it has
	// applied profile/shared-folder precedence and route qualification. Keeping
	// the report on the runner input makes the prepared launch immutable and
	// prevents adapters from silently re-resolving policy.
	ResolvedAccess *ResolvedAccess `json:"-"`
}

type EffectivePolicy struct {
	Preset             PermissionPreset `json:"preset"`
	Filesystem         string           `json:"filesystem"`
	Network            bool             `json:"network"`
	Approvals          string           `json:"approvals"`
	Workspace          string           `json:"workspace,omitempty"`
	TemporaryDirectory string           `json:"temporary_directory,omitempty"`
	ReviewOnly         bool             `json:"review_only,omitempty"`
	AccessFingerprint  string           `json:"access_fingerprint,omitempty"`
}

type PreparedLaunch struct {
	HarnessID          string           `json:"harness_id"`
	Provider           string           `json:"provider"`
	Transport          Transport        `json:"transport"`
	Dialect            string           `json:"dialect,omitempty"`
	Executable         string           `json:"executable"`
	ExecutableIdentity string           `json:"executable_identity"`
	Version            string           `json:"version"`
	Argv               []string         `json:"argv"`
	CWD                string           `json:"cwd"`
	Environment        []string         `json:"-"`
	EnvironmentNames   []string         `json:"environment_names"`
	RequestedPreset    PermissionPreset `json:"requested_preset"`
	EffectivePolicy    EffectivePolicy  `json:"effective_policy"`
	Capabilities       map[string]bool  `json:"capabilities"`
	AuthState          string           `json:"auth_state"`
	ConfigurationHash  string           `json:"configuration_hash"`
	LaunchHash         string           `json:"launch_hash"`
	PreparedAt         time.Time        `json:"prepared_at"`
	Deadline           time.Duration    `json:"-"`
	OutputLimit        int              `json:"output_limit"`
	Access             *ResolvedAccess  `json:"access,omitempty"`
	prompt             string
}

type CandidateCheck struct {
	Path   string `json:"path"`
	Check  string `json:"check"`
	Reason string `json:"reason,omitempty"`
}

type AdmissionError struct {
	Code       string           `json:"code"`
	HarnessID  string           `json:"harness_id,omitempty"`
	Check      string           `json:"check"`
	Reason     string           `json:"reason"`
	Remedy     string           `json:"remedy"`
	Candidates []CandidateCheck `json:"candidates,omitempty"`
}

func (e *AdmissionError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Reason)
}

type EventType string

const (
	EventStarted           EventType = "started"
	EventProgress          EventType = "progress"
	EventPermissionRequest EventType = "permission_request"
	EventCompleted         EventType = "completed"
	EventFailed            EventType = "failed"
	EventCancelled         EventType = "cancelled"
)

type Event struct {
	AttemptID  string          `json:"attempt_id,omitempty"`
	Sequence   uint64          `json:"sequence"`
	ObservedAt time.Time       `json:"observed_at"`
	Type       EventType       `json:"type"`
	Reason     string          `json:"reason,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type EventSink func(context.Context, Event) error

type ExecutionReceipt struct {
	Schema          string           `json:"schema"`
	AttemptID       string           `json:"attempt_id,omitempty"`
	LaunchHash      string           `json:"launch_hash"`
	HarnessID       string           `json:"harness_id"`
	Provider        string           `json:"provider"`
	Transport       Transport        `json:"transport"`
	Version         string           `json:"version"`
	StartedAt       time.Time        `json:"started_at"`
	FinishedAt      time.Time        `json:"finished_at"`
	Outcome         EventType        `json:"outcome"`
	Reason          string           `json:"reason,omitempty"`
	ExitCode        *int             `json:"exit_code,omitempty"`
	SessionID       string           `json:"session_id,omitempty"`
	RequestedPreset PermissionPreset `json:"requested_preset"`
	EffectivePolicy EffectivePolicy  `json:"effective_policy"`
	StdoutTruncated bool             `json:"stdout_truncated"`
	StderrTruncated bool             `json:"stderr_truncated"`
	Stdout          string           `json:"stdout,omitempty"`
	Stderr          string           `json:"stderr,omitempty"`
	Events          []Event          `json:"events"`
}

type CaseResult string

const (
	CasePass        CaseResult = "pass"
	CaseFail        CaseResult = "fail"
	CaseUnsupported CaseResult = "unsupported"
	CaseBlocked     CaseResult = "blocked"
	CaseNotRun      CaseResult = "not_run"
)

type ConformanceCase struct {
	ID       string     `json:"id"`
	Result   CaseResult `json:"result"`
	Evidence string     `json:"evidence,omitempty"`
}

type ConformanceReport struct {
	Schema             string            `json:"schema"`
	HarnessID          string            `json:"harness_id"`
	ProfileID          string            `json:"profile_id,omitempty"`
	ProfileRevision    string            `json:"profile_revision,omitempty"`
	Provider           string            `json:"provider"`
	Dialect            string            `json:"dialect,omitempty"`
	Transport          Transport         `json:"transport"`
	HostOS             string            `json:"host_os"`
	HostArchitecture   string            `json:"host_architecture"`
	Executable         string            `json:"executable,omitempty"`
	ExecutableIdentity string            `json:"executable_identity,omitempty"`
	Version            string            `json:"version,omitempty"`
	Preset             PermissionPreset  `json:"preset"`
	Model              string            `json:"model,omitempty"`
	Effort             string            `json:"effort,omitempty"`
	SuiteVersion       string            `json:"suite_version"`
	StartedAt          time.Time         `json:"started_at"`
	FinishedAt         time.Time         `json:"finished_at"`
	Live               bool              `json:"live"`
	Ready              bool              `json:"ready"`
	ValidUntil         *time.Time        `json:"valid_until,omitempty"`
	ConfigurationHash  string            `json:"configuration_hash,omitempty"`
	PolicyHash         string            `json:"policy_hash,omitempty"`
	NextStep           string            `json:"next_step,omitempty"`
	Access             *ResolvedAccess   `json:"access,omitempty"`
	Cases              []ConformanceCase `json:"cases"`
}
