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
}

type EffectivePolicy struct {
	Preset     PermissionPreset `json:"preset"`
	Filesystem string           `json:"filesystem"`
	Network    bool             `json:"network"`
	Approvals  string           `json:"approvals"`
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
	Provider           string            `json:"provider"`
	Dialect            string            `json:"dialect,omitempty"`
	Transport          Transport         `json:"transport"`
	HostOS             string            `json:"host_os"`
	HostArchitecture   string            `json:"host_architecture"`
	Executable         string            `json:"executable,omitempty"`
	ExecutableIdentity string            `json:"executable_identity,omitempty"`
	Version            string            `json:"version,omitempty"`
	Preset             PermissionPreset  `json:"preset"`
	SuiteVersion       string            `json:"suite_version"`
	StartedAt          time.Time         `json:"started_at"`
	FinishedAt         time.Time         `json:"finished_at"`
	Live               bool              `json:"live"`
	Ready              bool              `json:"ready"`
	ValidUntil         *time.Time        `json:"valid_until,omitempty"`
	ConfigurationHash  string            `json:"configuration_hash,omitempty"`
	PolicyHash         string            `json:"policy_hash,omitempty"`
	Cases              []ConformanceCase `json:"cases"`
}
