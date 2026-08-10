package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// ACPPermissionRequest is Tusker's deliberately small, provider-neutral view
// of an ACP session/request_permission callback. The adapter must bind both
// identities to the currently supervised attempt before calling the broker.
// It must never pass provider prompts, credentials, or raw tool arguments here.
type ACPPermissionRequest struct {
	AttemptID      string
	BoundAttemptID string
	SessionID      string
	BoundSessionID string
	Workspace      string
	Target         string
	ToolKind       string
	Options        []ACPPermissionOption
	Cancelled      bool
}

// ACPPermissionOption is an ACP option advertised for one callback. The
// broker only ever selects the exact, one-shot allow_once option.
type ACPPermissionOption struct {
	OptionID string
	Kind     string
}

// ACPPermissionPolicy is the already-resolved Tusker authority ceiling. An
// empty allowlist grants nothing; adapters cannot use a provider default to
// widen it.
type ACPPermissionPolicy struct {
	AllowedToolKinds    map[string]bool
	BudgetAuthorized    bool
	ReadOnly            bool
	AllowNetwork        bool
	AllowWorkspaceWrite bool
}

type ACPPermissionOutcome string

const (
	ACPPermissionAllowOnce ACPPermissionOutcome = "allow_once"
	ACPPermissionReject    ACPPermissionOutcome = "reject"
	ACPPermissionCancelled ACPPermissionOutcome = "cancelled"
)

// Reason codes are stable machine-facing diagnostics. Keep their values free
// of request data so that a decision can be persisted without leaking it.
const (
	ACPPermissionReasonAllowed                  = "allowed"
	ACPPermissionReasonCancelled                = "cancelled"
	ACPPermissionReasonAttemptMismatch          = "attempt_mismatch"
	ACPPermissionReasonSessionMismatch          = "session_mismatch"
	ACPPermissionReasonInvalidRequest           = "invalid_request"
	ACPPermissionReasonWorkspaceInvalid         = "workspace_invalid"
	ACPPermissionReasonTargetOutsideWorkspace   = "target_outside_workspace"
	ACPPermissionReasonUnknownTool              = "unknown_tool"
	ACPPermissionReasonToolNotAllowed           = "tool_not_allowed"
	ACPPermissionReasonBudgetExceeded           = "budget_exceeded"
	ACPPermissionReasonReadOnly                 = "read_only"
	ACPPermissionReasonNetworkNotAllowed        = "network_not_allowed"
	ACPPermissionReasonWorkspaceWriteNotAllowed = "workspace_write_not_allowed"
	ACPPermissionReasonAllowOnceUnavailable     = "allow_once_unavailable"
)

// ACPPermissionAudit is intentionally bounded and redacted. TargetDigest is
// a SHA-256 digest of the normalized target, never the target itself. It is
// suitable for correlating an adapter callback with an attempt receipt without
// retaining a command, prompt, credential, or arbitrary provider arguments.
type ACPPermissionAudit struct {
	OperationClass string `json:"operation_class"`
	TargetDigest   string `json:"target_digest,omitempty"`
	PolicyRule     string `json:"policy_rule"`
	Outcome        string `json:"outcome"`
	ReasonCode     string `json:"reason_code"`
}

type ACPPermissionDecision struct {
	Outcome    ACPPermissionOutcome
	OptionID   string
	ReasonCode string
	Audit      ACPPermissionAudit
}

const (
	acpPermissionMaxIdentityBytes  = 256
	acpPermissionMaxWorkspaceBytes = 4096
	acpPermissionMaxTargetBytes    = 4096
	acpPermissionMaxOptionBytes    = 128
	acpPermissionMaxToolKindBytes  = 64
)

// EvaluateACPPermission evaluates one normalized provider callback against
// the pre-resolved Tusker policy. It is pure: it performs no provider I/O,
// changes no task state, and has no approval memory. Failures are rejects.
func EvaluateACPPermission(req ACPPermissionRequest, policy ACPPermissionPolicy) ACPPermissionDecision {
	toolKind := normalizeACPPermissionToolKind(req.ToolKind)
	auditToolKind := toolKind
	if !acpPermissionKnownToolKind(auditToolKind) {
		auditToolKind = "unknown"
	} else if len(auditToolKind) > acpPermissionMaxToolKindBytes {
		auditToolKind = "invalid"
	}
	finish := func(outcome ACPPermissionOutcome, optionID, reason string) ACPPermissionDecision {
		return ACPPermissionDecision{
			Outcome:    outcome,
			OptionID:   optionID,
			ReasonCode: reason,
			Audit: ACPPermissionAudit{
				OperationClass: auditToolKind,
				TargetDigest:   acpPermissionTargetDigest(req.Target),
				PolicyRule:     acpPermissionPolicyRule(reason),
				Outcome:        string(outcome),
				ReasonCode:     reason,
			},
		}
	}

	if req.Cancelled {
		return finish(ACPPermissionCancelled, "", ACPPermissionReasonCancelled)
	}
	if !acpPermissionIdentityMatches(req.AttemptID, req.BoundAttemptID) {
		return finish(ACPPermissionReject, "", ACPPermissionReasonAttemptMismatch)
	}
	if !acpPermissionIdentityMatches(req.SessionID, req.BoundSessionID) {
		return finish(ACPPermissionReject, "", ACPPermissionReasonSessionMismatch)
	}
	if !acpPermissionRequestBounded(req) {
		return finish(ACPPermissionReject, "", ACPPermissionReasonInvalidRequest)
	}
	if !acpPermissionKnownToolKind(toolKind) {
		return finish(ACPPermissionReject, "", ACPPermissionReasonUnknownTool)
	}
	if !policy.BudgetAuthorized {
		return finish(ACPPermissionReject, "", ACPPermissionReasonBudgetExceeded)
	}
	if !policy.AllowedToolKinds[toolKind] {
		return finish(ACPPermissionReject, "", ACPPermissionReasonToolNotAllowed)
	}

	// Read and write are workspace filesystem operations; verify both their
	// lexical spelling and resolved path to reject traversal and symlink escape.
	if toolKind == "read" || toolKind == "write" {
		valid, reason := acpPermissionWorkspaceTargetValid(req.Workspace, req.Target)
		if !valid {
			return finish(ACPPermissionReject, "", reason)
		}
	}
	if toolKind == "write" {
		if policy.ReadOnly {
			return finish(ACPPermissionReject, "", ACPPermissionReasonReadOnly)
		}
		if !policy.AllowWorkspaceWrite {
			return finish(ACPPermissionReject, "", ACPPermissionReasonWorkspaceWriteNotAllowed)
		}
	}
	if toolKind == "network" && !policy.AllowNetwork {
		return finish(ACPPermissionReject, "", ACPPermissionReasonNetworkNotAllowed)
	}
	if optionID := acpPermissionAllowOnceOption(req.Options); optionID != "" {
		return finish(ACPPermissionAllowOnce, optionID, ACPPermissionReasonAllowed)
	}
	return finish(ACPPermissionReject, "", ACPPermissionReasonAllowOnceUnavailable)
}

func acpPermissionIdentityMatches(requestID, boundID string) bool {
	requestID = strings.TrimSpace(requestID)
	boundID = strings.TrimSpace(boundID)
	return requestID != "" && boundID != "" && requestID == boundID
}

func acpPermissionRequestBounded(req ACPPermissionRequest) bool {
	if strings.TrimSpace(req.Workspace) == "" || strings.TrimSpace(req.Target) == "" ||
		len(req.AttemptID) > acpPermissionMaxIdentityBytes || len(req.BoundAttemptID) > acpPermissionMaxIdentityBytes ||
		len(req.SessionID) > acpPermissionMaxIdentityBytes || len(req.BoundSessionID) > acpPermissionMaxIdentityBytes ||
		len(req.Workspace) > acpPermissionMaxWorkspaceBytes || len(req.Target) > acpPermissionMaxTargetBytes ||
		len(req.ToolKind) > acpPermissionMaxToolKindBytes {
		return false
	}
	if len(req.Options) > 32 {
		return false
	}
	for _, option := range req.Options {
		if len(option.OptionID) > acpPermissionMaxOptionBytes || len(option.Kind) > acpPermissionMaxOptionBytes {
			return false
		}
	}
	return true
}

func normalizeACPPermissionToolKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	kind = strings.ReplaceAll(kind, "-", "_")
	switch kind {
	case "workspace_read":
		return "read"
	case "workspace_write", "file_write":
		return "write"
	case "network_request":
		return "network"
	default:
		return kind
	}
}

func acpPermissionKnownToolKind(kind string) bool {
	return kind == "read" || kind == "write" || kind == "network"
}

func acpPermissionWorkspaceTargetValid(workspace, target string) (bool, string) {
	if !filepath.IsAbs(strings.TrimSpace(workspace)) {
		return false, ACPPermissionReasonWorkspaceInvalid
	}
	workspaceAbs, err := filepath.Abs(workspace)
	if err != nil {
		return false, ACPPermissionReasonWorkspaceInvalid
	}
	workspaceAbs = filepath.Clean(workspaceAbs)
	workspaceResolved := resolvePathWithMissingTail(workspaceAbs)
	if workspaceResolved == "" {
		return false, ACPPermissionReasonWorkspaceInvalid
	}

	targetPath := strings.TrimSpace(target)
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(workspaceAbs, targetPath)
	}
	targetAbs, err := filepath.Abs(targetPath)
	if err != nil || !pathWithinLexical(workspaceAbs, targetAbs) {
		return false, ACPPermissionReasonTargetOutsideWorkspace
	}
	if targetResolved := resolvePathWithMissingTail(targetAbs); targetResolved == "" || !pathWithinLexical(workspaceResolved, targetResolved) {
		return false, ACPPermissionReasonTargetOutsideWorkspace
	}
	return true, ""
}

func acpPermissionAllowOnceOption(options []ACPPermissionOption) string {
	for _, option := range options {
		if option.OptionID != "" && strings.EqualFold(strings.TrimSpace(option.Kind), "allow_once") {
			return option.OptionID
		}
	}
	return ""
}

func acpPermissionTargetDigest(target string) string {
	if target == "" || len(target) > acpPermissionMaxTargetBytes {
		return ""
	}
	sum := sha256.Sum256([]byte(filepath.Clean(strings.TrimSpace(target))))
	return hex.EncodeToString(sum[:])
}

func acpPermissionPolicyRule(reason string) string {
	switch reason {
	case ACPPermissionReasonAllowed:
		return "allowlist_and_envelope"
	case ACPPermissionReasonCancelled:
		return "interrupt"
	case ACPPermissionReasonAttemptMismatch, ACPPermissionReasonSessionMismatch:
		return "attempt_session_binding"
	case ACPPermissionReasonInvalidRequest:
		return "request_shape"
	case ACPPermissionReasonWorkspaceInvalid, ACPPermissionReasonTargetOutsideWorkspace:
		return "workspace_boundary"
	case ACPPermissionReasonUnknownTool, ACPPermissionReasonToolNotAllowed:
		return "tool_allowlist"
	case ACPPermissionReasonBudgetExceeded:
		return "attempt_budget"
	case ACPPermissionReasonReadOnly, ACPPermissionReasonWorkspaceWriteNotAllowed:
		return "workspace_write_policy"
	case ACPPermissionReasonNetworkNotAllowed:
		return "network_policy"
	case ACPPermissionReasonAllowOnceUnavailable:
		return "one_shot_option"
	default:
		return "reject"
	}
}
