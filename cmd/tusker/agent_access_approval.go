package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tusker/internal/acp"
	runnercore "tusker/internal/runner"
)

const (
	AgentAccessApprovalPending   = "pending"
	AgentAccessApprovalAllowed   = "allowed"
	AgentAccessApprovalDenied    = "denied"
	AgentAccessApprovalExpired   = "expired"
	AgentAccessApprovalCancelled = "cancelled"
	AgentAccessApprovalAllowOnce = "allow_once"
	AgentAccessApprovalDeny      = "deny"
)

const (
	agentAccessApprovalMaxID          = 512
	agentAccessApprovalMaxArguments   = 128 * 1024
	agentAccessApprovalMaxReason      = 4096
	agentAccessApprovalMaxTargets     = 128
	agentAccessApprovalDefaultTimeout = 2 * time.Minute
)

// AgentAccessApprovalRequest is the normalized identity of one native
// permission callback. Arguments are hashed for binding and never persisted,
// preventing the runtime database from becoming a command replay source.
type AgentAccessApprovalRequest struct {
	RequestID         string
	ProjectID         string
	TaskID            string
	AttemptID         string
	ExecutionID       string
	SessionID         string
	NativeRequestID   string
	Route             string
	PolicyFingerprint string
	Tool              string
	Arguments         string
	RedactedArguments json.RawMessage
	WorkingDirectory  string
	Targets           []string
	Reason            string
	NativeOptionID    string
	NativeOptionKind  string
	ExpiresAt         string
	LiveUntil         string
}

// AgentAccessApproval is the safe Serve/UI projection. It contains an
// immutable argument digest and redacted display payload, never raw arguments.
type AgentAccessApproval struct {
	RequestID         string          `json:"requestId"`
	ProjectID         string          `json:"projectId"`
	TaskID            string          `json:"taskId,omitempty"`
	AttemptID         string          `json:"attemptId"`
	ExecutionID       string          `json:"executionId,omitempty"`
	SessionID         string          `json:"sessionId"`
	NativeRequestID   string          `json:"nativeRequestId"`
	Route             string          `json:"route"`
	PolicyFingerprint string          `json:"policyFingerprint"`
	Tool              string          `json:"tool"`
	ArgsDigest        string          `json:"argsDigest"`
	RedactedArguments json.RawMessage `json:"redactedArguments"`
	WorkingDirectory  string          `json:"workingDirectory,omitempty"`
	Targets           []string        `json:"targets,omitempty"`
	Reason            string          `json:"reason,omitempty"`
	NativeOptionID    string          `json:"nativeOptionId"`
	NativeOptionKind  string          `json:"nativeOptionKind"`
	State             string          `json:"state"`
	StateRevision     int             `json:"stateRevision"`
	ExpiresAt         string          `json:"expiresAt"`
	LiveUntil         string          `json:"liveUntil,omitempty"`
	Decision          string          `json:"decision,omitempty"`
	DecisionActor     string          `json:"decisionActor,omitempty"`
	DecisionAt        string          `json:"decisionAt,omitempty"`
	TerminalReason    string          `json:"terminalReason,omitempty"`
	CreatedAt         string          `json:"createdAt"`
	UpdatedAt         string          `json:"updatedAt"`
	BindingDigest     string          `json:"-"`
}

type AgentAccessApprovalResponse struct {
	RequestID        string
	ExpectedRevision int
	Decision         string
	Actor            string
}

const agentAccessApprovalSelect = "SELECT request_id, project_id, task_id, attempt_id, execution_id, session_id, native_request_id, route, policy_fingerprint, tool, binding_digest, args_digest, redacted_arguments_json, working_directory, targets_json, reason, native_option_id, native_option_kind, state, state_revision, expires_at, live_until, decision, decision_actor, decision_at, terminal_reason, created_at, updated_at FROM agent_access_approvals"

func (r AgentAccessApprovalRequest) normalize(now time.Time) (AgentAccessApprovalRequest, string, error) {
	trim := func(value string) string { return strings.TrimSpace(value) }
	r.RequestID, r.ProjectID, r.TaskID, r.AttemptID = trim(r.RequestID), trim(r.ProjectID), trim(r.TaskID), trim(r.AttemptID)
	r.ExecutionID, r.SessionID, r.NativeRequestID = trim(r.ExecutionID), trim(r.SessionID), trim(r.NativeRequestID)
	r.Route, r.PolicyFingerprint, r.Tool = trim(r.Route), trim(r.PolicyFingerprint), trim(r.Tool)
	r.WorkingDirectory, r.Reason, r.NativeOptionID = trim(r.WorkingDirectory), trim(r.Reason), trim(r.NativeOptionID)
	r.NativeOptionKind, r.ExpiresAt, r.LiveUntil = strings.ToLower(trim(r.NativeOptionKind)), trim(r.ExpiresAt), trim(r.LiveUntil)
	if r.NativeOptionKind == "" {
		r.NativeOptionKind = AgentAccessApprovalAllowOnce
	}
	for name, value := range map[string]string{"project_id": r.ProjectID, "attempt_id": r.AttemptID, "session_id": r.SessionID, "native_request_id": r.NativeRequestID, "route": r.Route, "policy_fingerprint": r.PolicyFingerprint, "tool": r.Tool, "native_option_id": r.NativeOptionID} {
		if value == "" || len(value) > agentAccessApprovalMaxID || strings.ContainsAny(value, "\x00\r\n") {
			return r, "", fmt.Errorf("agent access approval %s is missing or invalid", name)
		}
	}
	if len(r.Arguments) > agentAccessApprovalMaxArguments || len(r.Reason) > agentAccessApprovalMaxReason || len(r.Targets) > agentAccessApprovalMaxTargets {
		return r, "", errors.New("agent access approval payload exceeds its bound")
	}
	for _, target := range r.Targets {
		if target == "" || len(target) > agentAccessApprovalMaxID || strings.ContainsAny(target, "\x00\r\n") {
			return r, "", errors.New("agent access approval target is invalid")
		}
	}
	if len(r.RedactedArguments) == 0 {
		r.RedactedArguments = json.RawMessage([]byte("{}"))
	}
	if len(r.RedactedArguments) > agentAccessApprovalMaxArguments || !json.Valid(r.RedactedArguments) {
		return r, "", errors.New("agent access approval redacted arguments are invalid")
	}
	if r.ExpiresAt == "" {
		r.ExpiresAt = now.Add(agentAccessApprovalDefaultTimeout).Format(time.RFC3339Nano)
	}
	expires, err := time.Parse(time.RFC3339Nano, r.ExpiresAt)
	if err != nil || !expires.After(now) {
		return r, "", errors.New("agent access approval expiry is invalid or elapsed")
	}
	if r.LiveUntil == "" {
		r.LiveUntil = r.ExpiresAt
	}
	liveUntil, err := time.Parse(time.RFC3339Nano, r.LiveUntil)
	if err != nil || !liveUntil.After(now) || liveUntil.Before(expires) {
		return r, "", errors.New("agent access approval liveness bound is invalid")
	}
	if r.NativeOptionKind != AgentAccessApprovalAllowOnce {
		return r, "", errors.New("agent access approval must offer an allow-once native option")
	}
	binding, err := json.Marshal(struct {
		ProjectID, TaskID, AttemptID, ExecutionID, SessionID, NativeRequestID string
		Route, PolicyFingerprint, Tool, Arguments, WorkingDirectory, Reason   string
		RedactedArguments                                                     json.RawMessage
		Targets                                                               []string
		NativeOptionID                                                        string
		NativeOptionKind                                                      string
		ExpiresAt                                                             string
		LiveUntil                                                             string
	}{r.ProjectID, r.TaskID, r.AttemptID, r.ExecutionID, r.SessionID, r.NativeRequestID, r.Route, r.PolicyFingerprint, r.Tool, r.Arguments, r.WorkingDirectory, r.Reason, r.RedactedArguments, r.Targets, r.NativeOptionID, r.NativeOptionKind, r.ExpiresAt, r.LiveUntil})
	if err != nil {
		return r, "", err
	}
	sum := sha256.Sum256(binding)
	if r.RequestID == "" {
		r.RequestID = "agent-access:" + hex.EncodeToString(sum[:16])
	}
	if len(r.RequestID) > agentAccessApprovalMaxID || strings.ContainsAny(r.RequestID, "\x00\r\n") {
		return r, "", errors.New("agent access approval request id is invalid")
	}
	return r, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func approvalArgsDigest(arguments string) string {
	sum := sha256.Sum256([]byte(arguments))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func approvalRedactedArguments(raw []byte) json.RawMessage {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return json.RawMessage([]byte("{\"unavailable\":true}"))
	}
	redactApprovalValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage([]byte("{\"unavailable\":true}"))
	}
	return encoded
}

func redactApprovalValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "authorization") || strings.Contains(lower, "api_key") {
				typed[key] = "[REDACTED]"
				continue
			}
			redactApprovalValue(nested)
		}
	case []any:
		for _, nested := range typed {
			redactApprovalValue(nested)
		}
	}
}

func agentAccessApprovalPolicyFingerprint(policy any) string {
	encoded, err := json.Marshal(policy)
	if err != nil {
		return "sha256:unknown"
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func liveApprovalPolicyFingerprint(policy CodexPolicy, privateFolders []string) string {
	return agentAccessApprovalPolicyFingerprint(struct {
		Policy         CodexPolicy
		PrivateFolders []string
	}{policy, append([]string(nil), privateFolders...)})
}

// waitForAgentAccessApproval blocks only while the provider has an outstanding
// native callback. The native caller remains responsible for writing the
// exact provider option; this helper only returns allow-once or reject.
func waitForAgentAccessApproval(ctx context.Context, store *RuntimeStore, request AgentAccessApprovalRequest) (AgentAccessApproval, error) {
	if store == nil {
		return AgentAccessApproval{}, errors.New("live approval persistence is unavailable")
	}
	approval, _, err := store.CreateAgentAccessApproval(request)
	if err != nil {
		return AgentAccessApproval{}, err
	}
	check := func() (AgentAccessApproval, bool, error) {
		current, readErr := store.AgentAccessApproval(approval.RequestID)
		if readErr != nil {
			return AgentAccessApproval{}, false, readErr
		}
		switch current.State {
		case AgentAccessApprovalAllowed:
			return current, true, nil
		case AgentAccessApprovalDenied, AgentAccessApprovalExpired, AgentAccessApprovalCancelled:
			return current, true, fmt.Errorf("native approval settled as %s", current.State)
		default:
			return current, false, nil
		}
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			current, terminal, readErr := check()
			if readErr != nil {
				return approval, readErr
			}
			if terminal {
				return current, nil
			}
			_, _ = store.CancelAgentAccessApproval(approval.RequestID, current.StateRevision, "native callback context ended")
			return current, ctx.Err()
		case <-ticker.C:
			current, terminal, readErr := check()
			if readErr != nil {
				return approval, readErr
			}
			if terminal {
				return current, nil
			}
			if _, expireErr := store.ExpireAgentAccessApprovals(time.Now().UTC()); expireErr != nil {
				return current, expireErr
			}
		}
	}
}

func destructiveAgentCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}
	// Device/filesystem formatters are catastrophic even when they are not
	// part of the ordinary destructive-command list. Classifying them here is
	// what keeps the provider callback from falling through to auto-accept.
	if destructiveCommandUsesProtectedDevice(lower) || commandContainsUnsafeGitMutation(lower) || gitCommandUsesAlternateRepository(command) {
		return true
	}
	for _, marker := range []string{"rm ", "rm -", "rmdir ", "chmod ", "chown ", "git branch -d", "git branch -D", "git push --force", "git push -f"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// destructiveApprovalBoundaryReason is the last, provider-neutral guard
// before a native allow-once request is persisted. It deliberately handles
// only boundaries that can be proved from the callback; it is not a shell
// interpreter. An unparseable or variable target is therefore rejected.
func destructiveApprovalBoundaryReason(command, cwd, workspaceRoot string, targets, privateFolders []string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "destructive command has an ambiguous target; Tusker rejects it without approval"
	}
	if destructiveCommandAmbiguous(command) {
		return "destructive command has an ambiguous target; Tusker rejects it without approval"
	}
	if destructiveCommandUsesProtectedDevice(command) {
		return "destructive command targets a protected raw device or filesystem; approval cannot override catastrophic targets"
	}
	if reason := destructiveGitBoundaryReason(command); reason != "" {
		return reason
	}
	if ok, reason := approvalPathWithinWorkspace(cwd, workspaceRoot); !ok {
		return "destructive action rejected: cwd " + reason
	}

	resolvedRoot, err := approvalCanonicalPath(workspaceRoot)
	if err != nil {
		return "destructive action rejected: workspace is invalid"
	}
	resolvedTargets := append([]string{}, targets...)
	resolvedTargets = append(resolvedTargets, destructiveCommandTargets(command)...)
	if destructiveCommandNeedsExplicitTarget(command) && len(resolvedTargets) == 0 {
		return "destructive command has an ambiguous target; Tusker rejects it without approval"
	}
	for _, target := range uniqueNonEmptyStrings(resolvedTargets) {
		if !filepath.IsAbs(target) {
			target = filepath.Join(cwd, target)
		}
		resolvedTarget, pathErr := approvalCanonicalPath(target)
		if pathErr != nil {
			return "destructive command has an ambiguous target; Tusker rejects it without approval"
		}
		if filepath.Dir(resolvedTarget) == resolvedTarget {
			return "destructive action targets the filesystem root; approval cannot override catastrophic targets"
		}
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			if homeRoot, homePathErr := approvalCanonicalPath(home); homePathErr == nil && resolvedTarget == homeRoot {
				return "destructive action targets the home directory; approval cannot override catastrophic targets"
			}
		}
		for _, private := range privateFolders {
			privateRoot, privateErr := approvalCanonicalPath(private)
			if privateErr != nil {
				return "destructive action cannot verify a private-folder exclusion; Tusker rejects it without approval"
			}
			if pathWithinLexical(privateRoot, resolvedTarget) || pathWithinLexical(resolvedTarget, privateRoot) {
				return "destructive action targets a private folder; approval cannot override private exclusions"
			}
		}
		if resolvedTarget == resolvedRoot {
			return "destructive action targets the project/worktree root; approval cannot override catastrophic targets"
		}
		if !pathWithinLexical(resolvedRoot, resolvedTarget) {
			return "destructive action targets outside the prepared workspace; approval cannot widen writable scope"
		}
	}
	return ""
}

func approvalPathWithinWorkspace(path, workspaceRoot string) (bool, string) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return false, "has no prepared workspace"
	}
	root, err := approvalCanonicalPath(workspaceRoot)
	if err != nil {
		return false, "has an invalid prepared workspace"
	}
	target, err := approvalCanonicalPath(path)
	if err != nil {
		return false, "is invalid"
	}
	if !pathWithinLexical(root, target) {
		return false, "escapes the prepared workspace"
	}
	return true, ""
}

func approvalCanonicalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	resolved := resolvePathWithMissingTail(filepath.Clean(path))
	if resolved == "" {
		return "", errors.New("path cannot be resolved")
	}
	return filepath.Clean(resolved), nil
}

func destructiveCommandAmbiguous(command string) bool {
	if strings.Contains(command, "$ (") || strings.Contains(command, "$(") || strings.Contains(command, "`") {
		return true
	}
	for _, marker := range []string{"&&", "||", ";", "|", " >", "> ", "< ", "\n", "\r"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	fields, err := shellLikeFields(command)
	if err != nil {
		return true
	}
	for _, field := range fields {
		if strings.HasPrefix(field, "$") || strings.HasPrefix(field, "~") || strings.ContainsAny(field, "*?") {
			return true
		}
	}
	return false
}

func destructiveCommandUsesProtectedDevice(command string) bool {
	lower := strings.ToLower(command)
	for _, marker := range []string{"mkfs", "newfs", "wipefs", "diskutil erase", "diskutil apfs delete", "dd if=", "fdisk", "sfdisk", "parted ", "gpart ", "format ", "/dev/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func destructiveCommandTargets(command string) []string {
	fields, err := shellLikeFields(command)
	if err != nil {
		return nil
	}
	operation := -1
	for i, field := range fields {
		base := strings.ToLower(filepath.Base(strings.TrimSpace(field)))
		switch base {
		case "rm", "rmdir", "chmod", "chown":
			operation = i
		}
		if operation >= 0 {
			break
		}
	}
	if operation < 0 {
		return destructiveGitCommandTargets(command)
	}
	var targets []string
	for _, field := range fields[operation+1:] {
		if field == "--" {
			continue
		}
		if strings.HasPrefix(field, "-") {
			continue
		}
		targets = append(targets, field)
	}
	return targets
}

func destructiveCommandNeedsExplicitTarget(command string) bool {
	fields, err := shellLikeFields(command)
	if err != nil {
		return false
	}
	for _, field := range fields {
		switch strings.ToLower(filepath.Base(strings.TrimSpace(field))) {
		case "rm", "rmdir", "chmod", "chown":
			return true
		}
	}
	if subcommand, args, ok := gitCommandParts(command); ok && (subcommand == "restore" || subcommand == "checkout") {
		return len(gitPathArguments(subcommand, args)) == 0
	}
	return false
}

func (h *codexLiveHandle) awaitCodexAgentAccessApproval(method, nativeRequestID string, params json.RawMessage) (codexApprovalDecision, bool) {
	if h == nil || !strings.EqualFold(strings.TrimSpace(h.policy.ApprovalPolicy), "on-request") {
		return codexApprovalDecision{}, false
	}
	payload := approvalPayload(params)
	command := firstNonEmpty(strings.TrimSpace(stringValue(payload["command"])), strings.TrimSpace(stringValue(payload["cmd"])), strings.Join(stringListFromAny(payload["argv"]), " "))
	if !destructiveAgentCommand(command) {
		return codexApprovalDecision{}, false
	}
	if decision := commandPolicyDecision(h.policy, runnercore.CommandPolicyRequest{Mutating: true, Destructive: true, ReviewOnly: activeCodexPolicyIsReviewOnly(h.policy)}); decision.Behavior == runnercore.CommandBlock {
		return h.rejectApproval("command", true, decision.Reason, command), true
	}
	if preflight := h.evaluateDestructiveCommandBoundary(params); preflight.Decision != "accept" {
		// A mandatory boundary denial is terminal for this callback. Do not
		// create an approval card that could be used to override it.
		return preflight, true
	}
	cwd := approvalCWD(payload, h.workspaceRoot())
	targets := approvalPaths(params)
	if len(targets) == 0 && cwd != "" {
		targets = []string{cwd}
	}
	request := AgentAccessApprovalRequest{
		RequestID:         "codex:" + h.attemptID + ":" + nativeRequestID,
		ProjectID:         h.projectID,
		TaskID:            h.itemID,
		AttemptID:         h.attemptID,
		SessionID:         firstNonEmpty(h.threadID, "pending-session"),
		NativeRequestID:   nativeRequestID,
		Route:             "codex_app_server",
		PolicyFingerprint: liveApprovalPolicyFingerprint(h.policy, h.privateFolders),
		Tool:              "execute",
		Arguments:         string(params),
		RedactedArguments: approvalRedactedArguments(params),
		WorkingDirectory:  cwd,
		Targets:           targets,
		Reason:            "recognized destructive command requires one-time approval",
		NativeOptionID:    "accept",
		NativeOptionKind:  AgentAccessApprovalAllowOnce,
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), h.readTimeout())
	defer cancel()
	approval, err := waitForAgentAccessApproval(waitCtx, h.runtimeStore, request)
	if err != nil || approval.State != AgentAccessApprovalAllowed {
		reason := "native destructive action was not approved"
		if err != nil {
			reason = err.Error()
		}
		return h.rejectApproval("command", true, reason, command), true
	}
	return codexApprovalDecision{RequestType: "command", Decision: "accept", Subject: command, Mutating: true}, true
}

func (h *codexLiveHandle) evaluateDestructiveCommandApproval(params json.RawMessage) codexApprovalDecision {
	payload := approvalPayload(params)
	command := firstNonEmpty(
		strings.TrimSpace(stringValue(payload["command"])),
		strings.TrimSpace(stringValue(payload["cmd"])),
		strings.Join(stringListFromAny(payload["argv"]), " "),
	)
	mutating := commandLooksMutating(command)
	writesWorkspace := commandWritesWorkspace(command)
	if command == "" {
		return h.rejectApproval("command", mutating, "command approval request is missing a command", "")
	}
	if reason := h.policyDenialReason(writesWorkspace); reason != "" {
		return h.rejectApproval("command", mutating, reason, command)
	}
	if reason := commandPolicyRejectReason(h.policy, runnercore.CommandPolicyRequest{Mutating: true, Destructive: true, ReviewOnly: activeCodexPolicyIsReviewOnly(h.policy)}); reason != "" {
		return h.rejectApproval("command", mutating, reason, command)
	}
	return h.evaluateDestructiveCommandBoundary(params)
}

// evaluateDestructiveCommandBoundary applies the checks that must run before
// an allow-once card is persisted. Approval policy itself is intentionally not
// checked here: on-request is the policy that creates the card. Read-only
// sandboxing remains terminal because it cannot grant mutation authority.
func (h *codexLiveHandle) evaluateDestructiveCommandBoundary(params json.RawMessage) codexApprovalDecision {
	payload := approvalPayload(params)
	command := firstNonEmpty(
		strings.TrimSpace(stringValue(payload["command"])),
		strings.TrimSpace(stringValue(payload["cmd"])),
		strings.Join(stringListFromAny(payload["argv"]), " "),
	)
	mutating := commandLooksMutating(command)
	decision := codexApprovalDecision{RequestType: "command", Decision: "accept", Subject: command, Mutating: mutating}
	if command == "" {
		return h.rejectApproval("command", mutating, "command approval request is missing a command", "")
	}
	if reason := commandPolicyRejectReason(h.policy, runnercore.CommandPolicyRequest{Mutating: true, Destructive: true, ReviewOnly: activeCodexPolicyIsReviewOnly(h.policy)}); reason != "" {
		return h.rejectApproval("command", mutating, reason, command)
	}
	activeSandbox := firstNonEmpty(strings.TrimSpace(h.policy.TurnSandboxPolicy), strings.TrimSpace(h.policy.ThreadSandbox))
	if mutating && activeSandbox == "read-only" {
		return h.rejectApproval("command", mutating, "read-only sandbox rejects mutating command approval requests", command)
	}
	cwd := approvalCWD(payload, h.workspaceRoot())
	if ok, reason := h.pathAllowed(cwd, h.workspaceRoot()); !ok {
		return h.rejectApproval("command", mutating, "command approval rejected: cwd "+reason, command)
	}
	if reason := destructiveApprovalBoundaryReason(command, cwd, h.workspaceRoot(), approvalPaths(params), h.privateFolders); reason != "" {
		return h.rejectApproval("command", mutating, reason, command)
	}
	if commandMentionsSecretPath(command) {
		return h.rejectApproval("command", mutating, "command approval rejected: command references a secret path", command)
	}
	return decision
}

func (h *claudeLiveHandle) awaitClaudeAgentAccessApproval(nativeRequestID string, request map[string]any) (codexApprovalDecision, bool) {
	if h == nil || !strings.EqualFold(strings.TrimSpace(h.policy.ApprovalPolicy), "on-request") {
		return codexApprovalDecision{}, false
	}
	input := claudeToolInput(request)
	toolName := firstNonEmpty(strings.TrimSpace(stringValue(request["name"])), strings.TrimSpace(stringValue(request["tool_name"])), strings.TrimSpace(stringValue(request["toolName"])), strings.TrimSpace(stringValue(request["tool"])))
	command := firstNonEmpty(strings.TrimSpace(stringValue(input["command"])), strings.TrimSpace(stringValue(input["cmd"])), strings.Join(stringListFromAny(input["argv"]), " "))
	if !destructiveAgentCommand(command) {
		return codexApprovalDecision{}, false
	}
	if decision := commandPolicyDecision(h.policy, runnercore.CommandPolicyRequest{Mutating: true, Destructive: true, ReviewOnly: activeCodexPolicyIsReviewOnly(h.policy)}); decision.Behavior == runnercore.CommandBlock {
		return claudeRejectApproval(true, decision.Reason, firstNonEmpty(command, toolName)), true
	}
	if preflight := h.evaluateDestructiveToolApproval(request); preflight.Decision != "accept" {
		// Mandatory boundary denials are not approvable exceptions.
		return preflight, true
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return codexApprovalDecision{}, false
	}
	cwd := strings.TrimSpace(stringValue(input["cwd"]))
	if cwd == "" && h.cmd != nil {
		cwd = h.cmd.Dir
	}
	targets := approvalPaths(raw)
	if len(targets) == 0 && cwd != "" {
		targets = []string{cwd}
	}
	approvalRequest := AgentAccessApprovalRequest{
		RequestID:         "claude:" + h.attemptID + ":" + nativeRequestID,
		ProjectID:         h.projectID,
		TaskID:            h.itemID,
		AttemptID:         h.attemptID,
		SessionID:         firstNonEmpty(h.SessionRef(), "pending-session"),
		NativeRequestID:   nativeRequestID,
		Route:             "claude_cli",
		PolicyFingerprint: liveApprovalPolicyFingerprint(h.policy, h.privateFolders),
		Tool:              firstNonEmpty(toolName, "tool"),
		Arguments:         string(raw),
		RedactedArguments: approvalRedactedArguments(raw),
		WorkingDirectory:  cwd,
		Targets:           targets,
		Reason:            "recognized destructive tool action requires one-time approval",
		NativeOptionID:    "allow",
		NativeOptionKind:  AgentAccessApprovalAllowOnce,
	}
	timeout := 30 * time.Second
	if h.policy.ReadTimeoutMS > 0 {
		timeout = time.Duration(h.policy.ReadTimeoutMS) * time.Millisecond
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	approval, waitErr := waitForAgentAccessApproval(waitCtx, h.runtimeStore, approvalRequest)
	if waitErr != nil || approval.State != AgentAccessApprovalAllowed {
		reason := "native destructive action was not approved"
		if waitErr != nil {
			reason = waitErr.Error()
		}
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: reason, Subject: firstNonEmpty(command, toolName), Mutating: true}, true
	}
	return codexApprovalDecision{RequestType: "tool", Decision: "accept", Subject: firstNonEmpty(command, toolName), Mutating: true}, true
}

func (h *claudeLiveHandle) evaluateDestructiveToolApproval(request map[string]any) codexApprovalDecision {
	input := claudeToolInput(request)
	toolName := firstNonEmpty(strings.TrimSpace(stringValue(request["name"])), strings.TrimSpace(stringValue(request["tool_name"])), strings.TrimSpace(stringValue(request["toolName"])), strings.TrimSpace(stringValue(request["tool"])))
	command := firstNonEmpty(strings.TrimSpace(stringValue(input["command"])), strings.TrimSpace(stringValue(input["cmd"])), strings.Join(stringListFromAny(input["argv"]), " "))
	subject := firstNonEmpty(command, toolName)
	mutating := claudeToolLooksMutating(toolName, command)
	if reason := commandPolicyRejectReason(h.policy, runnercore.CommandPolicyRequest{Mutating: claudeToolWritesWorkspace(toolName, command), Destructive: true, ReviewOnly: activeCodexPolicyIsReviewOnly(h.policy)}); reason != "" {
		return claudeRejectApproval(mutating, reason, subject)
	}
	decision := codexApprovalDecision{RequestType: "tool", Decision: "accept", Subject: subject, Mutating: mutating}
	activeSandbox := firstNonEmpty(strings.TrimSpace(h.policy.TurnSandboxPolicy), strings.TrimSpace(h.policy.ThreadSandbox))
	if mutating && activeSandbox == "read-only" {
		return claudeRejectApproval(mutating, "read-only sandbox rejects mutating Claude Code tool approval requests", subject)
	}
	workspace := ""
	if h.cmd != nil {
		workspace = h.cmd.Dir
	}
	cwd := strings.TrimSpace(stringValue(input["cwd"]))
	if cwd == "" {
		cwd = workspace
	}
	if ok, reason := approvalPathWithinWorkspace(cwd, workspace); !ok {
		return claudeRejectApproval(mutating, "tool approval rejected: cwd "+reason, subject)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return claudeRejectApproval(mutating, "tool approval rejected: request is invalid", subject)
	}
	if reason := destructiveApprovalBoundaryReason(command, cwd, workspace, approvalPaths(raw), h.privateFolders); reason != "" {
		return claudeRejectApproval(mutating, reason, subject)
	}
	if commandMentionsSecretPath(command) {
		return claudeRejectApproval(mutating, "tool approval rejected: command references a secret path", subject)
	}
	return decision
}

func claudeRejectApproval(mutating bool, reason, subject string) codexApprovalDecision {
	return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: reason, Subject: subject, Mutating: mutating}
}

func awaitCodexACPAgentAccessApproval(ctx context.Context, provenance acpAttemptProvenance, request acp.PermissionRequest, workspace string, policy CodexPolicy) (acp.PermissionDecision, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(policy.ApprovalPolicy), "on-request") || ctx.Err() != nil {
		return acp.Reject, false, nil
	}
	decoded := DecodeCodexACPPermission(request)
	if decoded.Operation != CodexACPPermissionExecute {
		return acp.Reject, false, nil
	}
	payload := approvalPayload(request.RawInput)
	command := firstNonEmpty(strings.TrimSpace(stringValue(payload["command"])), strings.TrimSpace(stringValue(payload["cmd"])), strings.Join(stringListFromAny(payload["argv"]), " "))
	if !destructiveAgentCommand(command) {
		return acp.Reject, false, nil
	}
	if decision := commandPolicyDecision(policy, runnercore.CommandPolicyRequest{Mutating: true, Destructive: true, ReviewOnly: activeCodexPolicyIsReviewOnly(policy)}); decision.Behavior == runnercore.CommandBlock {
		return acp.Reject, true, errors.New(decision.Reason)
	}
	if reason := destructiveApprovalBoundaryReason(command, firstNonEmpty(strings.TrimSpace(stringValue(payload["cwd"])), workspace), workspace, approvalPaths(request.RawInput), provenance.PrivateFolders); reason != "" {
		return acp.Reject, true, errors.New(reason)
	}
	optionID := acpPermissionAllowOnceOption(requestOptions(request.Options))
	if optionID == "" {
		return acp.Reject, true, errors.New("ACP destructive request has no allow-once option")
	}
	store := provenance.RuntimeStore
	ownedStore := false
	if store == nil {
		var err error
		store, err = OpenRuntimeStore(DefaultStateRoot())
		if err != nil {
			return acp.Reject, true, err
		}
		ownedStore = true
	}
	if ownedStore {
		defer store.Close()
	}
	now := time.Now().UTC()
	cwd := strings.TrimSpace(stringValue(payload["cwd"]))
	if cwd == "" {
		cwd = workspace
	}
	requestID := "acp:" + provenance.AttemptID + ":" + request.ToolCallID
	approvalRequest := AgentAccessApprovalRequest{
		RequestID:         requestID,
		ProjectID:         provenance.ProjectID,
		TaskID:            provenance.TaskID,
		AttemptID:         provenance.AttemptID,
		SessionID:         request.SessionID,
		NativeRequestID:   request.ToolCallID,
		Route:             "codex_acp",
		PolicyFingerprint: liveApprovalPolicyFingerprint(policy, provenance.PrivateFolders),
		Tool:              "execute",
		Arguments:         string(request.Raw),
		RedactedArguments: approvalRedactedArguments(request.Raw),
		WorkingDirectory:  cwd,
		Targets:           []string{cwd},
		Reason:            "recognized destructive command requires one-time approval",
		NativeOptionID:    optionID,
		NativeOptionKind:  AgentAccessApprovalAllowOnce,
		ExpiresAt:         now.Add(2 * time.Minute).Format(time.RFC3339Nano),
		LiveUntil:         now.Add(2 * time.Minute).Format(time.RFC3339Nano),
	}
	approval, waitErr := waitForAgentAccessApproval(ctx, store, approvalRequest)
	if waitErr != nil {
		if ctx.Err() != nil {
			return acp.Cancelled, true, ctx.Err()
		}
		return acp.Reject, true, waitErr
	}
	if approval.State != AgentAccessApprovalAllowed {
		return acp.Reject, true, fmt.Errorf("native ACP approval settled as %s", approval.State)
	}
	return acp.AllowOnce, true, nil
}

func requestOptions(options []acp.PermissionOption) []ACPPermissionOption {
	out := make([]ACPPermissionOption, 0, len(options))
	for _, option := range options {
		out = append(out, ACPPermissionOption{OptionID: option.ID, Kind: option.Kind})
	}
	return out
}

type agentAccessApprovalScanner interface{ Scan(dest ...any) error }

func scanAgentAccessApproval(row agentAccessApprovalScanner) (AgentAccessApproval, error) {
	var a AgentAccessApproval
	var redacted, targets string
	err := row.Scan(&a.RequestID, &a.ProjectID, &a.TaskID, &a.AttemptID, &a.ExecutionID, &a.SessionID, &a.NativeRequestID, &a.Route, &a.PolicyFingerprint, &a.Tool, &a.BindingDigest, &a.ArgsDigest, &redacted, &a.WorkingDirectory, &targets, &a.Reason, &a.NativeOptionID, &a.NativeOptionKind, &a.State, &a.StateRevision, &a.ExpiresAt, &a.LiveUntil, &a.Decision, &a.DecisionActor, &a.DecisionAt, &a.TerminalReason, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return a, err
	}
	if !json.Valid([]byte(redacted)) || !json.Valid([]byte(targets)) {
		return a, errors.New("agent access approval contains invalid JSON projection")
	}
	a.RedactedArguments = json.RawMessage(redacted)
	if err := json.Unmarshal([]byte(targets), &a.Targets); err != nil {
		return a, err
	}
	return a, nil
}

func (s *RuntimeStore) CreateAgentAccessApproval(request AgentAccessApprovalRequest) (AgentAccessApproval, bool, error) {
	now := time.Now().UTC()
	r, binding, err := request.normalize(now)
	if err != nil {
		return AgentAccessApproval{}, false, err
	}
	targets, err := json.Marshal(r.Targets)
	if err != nil {
		return AgentAccessApproval{}, false, err
	}
	nowText := now.Format(time.RFC3339Nano)
	var approval AgentAccessApproval
	created := false
	err = s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.Exec("INSERT INTO agent_access_approvals (request_id, project_id, task_id, attempt_id, execution_id, session_id, native_request_id, route, policy_fingerprint, tool, binding_digest, args_digest, redacted_arguments_json, working_directory, targets_json, reason, native_option_id, native_option_kind, state, state_revision, expires_at, live_until, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?) ON CONFLICT(request_id) DO NOTHING", r.RequestID, r.ProjectID, r.TaskID, r.AttemptID, r.ExecutionID, r.SessionID, r.NativeRequestID, r.Route, r.PolicyFingerprint, r.Tool, binding, approvalArgsDigest(r.Arguments), string(r.RedactedArguments), r.WorkingDirectory, string(targets), r.Reason, r.NativeOptionID, r.NativeOptionKind, AgentAccessApprovalPending, r.ExpiresAt, r.LiveUntil, nowText, nowText)
		if err != nil {
			return fmt.Errorf("create agent access approval: %w", err)
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr == nil {
			created = affected == 1
		}
		approval, err = scanAgentAccessApproval(tx.QueryRow(agentAccessApprovalSelect+" WHERE request_id = ?", r.RequestID))
		if err != nil {
			return err
		}
		if approval.BindingDigest != binding {
			return errors.New("agent access approval request identity conflicts with an existing request")
		}
		return tx.Commit()
	})
	return approval, created, err
}

func (s *RuntimeStore) AgentAccessApproval(requestID string) (AgentAccessApproval, error) {
	var approval AgentAccessApproval
	err := s.withBusyRetry(func() error {
		var err error
		approval, err = scanAgentAccessApproval(s.db.QueryRow(agentAccessApprovalSelect+" WHERE request_id = ?", strings.TrimSpace(requestID)))
		return err
	})
	return approval, err
}

func (s *RuntimeStore) ListAgentAccessApprovals(projectID string, taskID ...string) ([]AgentAccessApproval, error) {
	query, args := agentAccessApprovalSelect+" WHERE project_id = ?", []any{strings.TrimSpace(projectID)}
	if len(taskID) > 0 && strings.TrimSpace(taskID[0]) != "" {
		query += " AND task_id = ?"
		args = append(args, strings.TrimSpace(taskID[0]))
	}
	query += " ORDER BY created_at ASC, request_id ASC"
	rows, err := s.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentAccessApproval
	for rows.Next() {
		a, scanErr := scanAgentAccessApproval(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func normalizeAgentAccessDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case AgentAccessApprovalAllowOnce, "allow", "allow-once":
		return AgentAccessApprovalAllowOnce
	case AgentAccessApprovalDeny, "block", "reject", "denied":
		return AgentAccessApprovalDeny
	default:
		return ""
	}
}

func (s *RuntimeStore) SettleAgentAccessApproval(response AgentAccessApprovalResponse) (AgentAccessApproval, error) {
	requestID, actor := strings.TrimSpace(response.RequestID), strings.TrimSpace(response.Actor)
	decision := normalizeAgentAccessDecision(response.Decision)
	if requestID == "" || response.ExpectedRevision <= 0 || decision == "" {
		return AgentAccessApproval{}, errors.New("agent access approval response requires request id, revision, and allow_once or deny")
	}
	if !strings.HasPrefix(actor, "human:") {
		return AgentAccessApproval{}, errors.New("agent access approval may only be settled by a human operator")
	}
	var approval AgentAccessApproval
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		approval, err = scanAgentAccessApproval(tx.QueryRow(agentAccessApprovalSelect+" WHERE request_id = ?", requestID))
		if err != nil {
			return err
		}
		if approval.State != AgentAccessApprovalPending {
			if approval.Decision == decision {
				return tx.Commit()
			}
			return fmt.Errorf("agent access approval is already %s", approval.State)
		}
		if approval.StateRevision != response.ExpectedRevision {
			return fmt.Errorf("agent access approval revision is stale: got %d, current %d", response.ExpectedRevision, approval.StateRevision)
		}
		expires, parseErr := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
		if parseErr != nil || !expires.After(now) {
			if _, err := tx.Exec("UPDATE agent_access_approvals SET state = ?, state_revision = state_revision + 1, terminal_reason = ?, updated_at = ? WHERE request_id = ? AND state = ? AND state_revision = ?", AgentAccessApprovalExpired, "native request expired before operator response", nowText, requestID, AgentAccessApprovalPending, response.ExpectedRevision); err != nil {
				return err
			}
			_ = tx.Commit()
			approval.State, approval.StateRevision, approval.TerminalReason, approval.UpdatedAt = AgentAccessApprovalExpired, approval.StateRevision+1, "native request expired before operator response", nowText
			return errors.New("agent access approval expired")
		}
		state := AgentAccessApprovalDenied
		if decision == AgentAccessApprovalAllowOnce {
			state = AgentAccessApprovalAllowed
		}
		result, err := tx.Exec("UPDATE agent_access_approvals SET state = ?, state_revision = state_revision + 1, decision = ?, decision_actor = ?, decision_at = ?, terminal_reason = ?, updated_at = ? WHERE request_id = ? AND state = ? AND state_revision = ? AND expires_at > ?", state, decision, actor, nowText, "operator response", nowText, requestID, AgentAccessApprovalPending, response.ExpectedRevision, nowText)
		if err != nil {
			return err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
			return errors.New("agent access approval became stale before settlement")
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		approval.State, approval.StateRevision, approval.Decision, approval.DecisionActor, approval.DecisionAt, approval.TerminalReason, approval.UpdatedAt = state, approval.StateRevision+1, decision, actor, nowText, "operator response", nowText
		return nil
	})
	return approval, err
}

func (s *RuntimeStore) CancelAgentAccessApproval(requestID string, expectedRevision int, reason string) (AgentAccessApproval, error) {
	if strings.TrimSpace(requestID) == "" || expectedRevision <= 0 {
		return AgentAccessApproval{}, errors.New("agent access approval cancellation requires request id and revision")
	}
	var approval AgentAccessApproval
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		approval, err = scanAgentAccessApproval(tx.QueryRow(agentAccessApprovalSelect+" WHERE request_id = ?", strings.TrimSpace(requestID)))
		if err != nil {
			return err
		}
		if approval.State != AgentAccessApprovalPending {
			return tx.Commit()
		}
		if approval.StateRevision != expectedRevision {
			return errors.New("agent access approval cancellation is stale")
		}
		if strings.TrimSpace(reason) == "" {
			reason = "native request cancelled"
		}
		result, err := tx.Exec("UPDATE agent_access_approvals SET state = ?, state_revision = state_revision + 1, terminal_reason = ?, updated_at = ? WHERE request_id = ? AND state = ? AND state_revision = ?", AgentAccessApprovalCancelled, reason, now, requestID, AgentAccessApprovalPending, expectedRevision)
		if err != nil {
			return err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil || affected != 1 {
			return errors.New("agent access approval cancellation became stale")
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		approval.State, approval.StateRevision, approval.TerminalReason, approval.UpdatedAt = AgentAccessApprovalCancelled, approval.StateRevision+1, reason, now
		return nil
	})
	return approval, err
}

func (s *RuntimeStore) ExpireAgentAccessApprovals(now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var expired int
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.Exec("UPDATE agent_access_approvals SET state = ?, state_revision = state_revision + 1, terminal_reason = ?, updated_at = ? WHERE state = ? AND expires_at <= ?", AgentAccessApprovalExpired, "native request deadline elapsed", now.Format(time.RFC3339Nano), AgentAccessApprovalPending, now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
		if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
			return rowsErr
		} else {
			expired = int(affected)
		}
		return tx.Commit()
	})
	return expired, err
}

// ReconcileAgentAccessApprovals is conservative at an authoritative restart
// boundary: a nil live callback means the native request cannot be proven live
// and is expired. Opening a RuntimeStore is not a restart boundary; callers
// must invoke this method explicitly when they own that lifecycle decision.
func (s *RuntimeStore) ReconcileAgentAccessApprovals(now time.Time, live func(AgentAccessApproval) bool) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var expired int
	err := s.withBusyRetry(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		rows, err := tx.Query(agentAccessApprovalSelect+" WHERE state = ?", AgentAccessApprovalPending)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, scanErr := scanAgentAccessApproval(rows)
			if scanErr != nil {
				return scanErr
			}
			expires, parseErr := time.Parse(time.RFC3339Nano, a.ExpiresAt)
			if parseErr == nil && expires.After(now) && live != nil && live(a) {
				continue
			}
			result, updateErr := tx.Exec("UPDATE agent_access_approvals SET state = ?, state_revision = state_revision + 1, terminal_reason = ?, updated_at = ? WHERE request_id = ? AND state = ? AND state_revision = ?", AgentAccessApprovalExpired, "native request is no longer live", now.Format(time.RFC3339Nano), a.RequestID, AgentAccessApprovalPending, a.StateRevision)
			if updateErr != nil {
				return updateErr
			}
			if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
				return rowsErr
			} else {
				expired += int(affected)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.Commit()
	})
	return expired, err
}

// reconcileAgentAccessApprovalsAtDaemonStart is called only by the resident
// daemon's process-start boundary. The in-process registry can preserve a
// callback when a daemon is restarted within the same process (for example,
// an operator-controlled handoff); an absent or mismatched handle fails
// closed, as required after a genuine process restart.
func reconcileAgentAccessApprovalsAtDaemonStart(store *RuntimeStore, now time.Time) (int, error) {
	if store == nil {
		return 0, errors.New("agent access approval reconciliation requires a runtime store")
	}
	return store.ReconcileAgentAccessApprovals(now, func(approval AgentAccessApproval) bool {
		handle := liveRegistry.FindAttempt(approval.AttemptID)
		if handle == nil || handle.ProjectID() != approval.ProjectID {
			return false
		}
		if approval.TaskID != "" && handle.ItemID() != approval.TaskID && handle.RecordID() != approval.TaskID {
			return false
		}
		return true
	})
}

func (s *serveServer) handleAgentAccessApprovals(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID == "" {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "project query parameter is required"})
		return
	}
	approvals, err := s.store.ListAgentAccessApprovals(projectID, r.URL.Query().Get("task"))
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"approvals": approvals})
}

func (s *serveServer) handleAgentAccessApproval(w http.ResponseWriter, r *http.Request, requestID string) {
	approval, err := s.store.AgentAccessApproval(requestID)
	if errors.Is(err, sql.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no rows")) {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "agent access approval not found"})
		return
	}
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if projectID := strings.TrimSpace(r.URL.Query().Get("project")); projectID != "" && projectID != approval.ProjectID {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "agent access approval not found"})
		return
	}
	serveJSON(w, http.StatusOK, approval)
}

func (s *serveServer) handleAgentAccessApprovalResponse(w http.ResponseWriter, body serveActionBody, requestID, routeDecision string) {
	requestID = strings.TrimSpace(firstNonEmpty(requestID, body.string("requestId", "request_id")))
	if requestID == "" {
		serveJSON(w, http.StatusBadRequest, serveActionResult{Refused: true, Reason: "approval request id is required"})
		return
	}
	actor, err := s.serveOperatorActor(body, "agent access approval")
	if err != nil {
		serveJSON(w, http.StatusPreconditionFailed, serveActionResult{Refused: true, Reason: err.Error()})
		return
	}
	approval, err := s.store.AgentAccessApproval(requestID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			status = http.StatusNotFound
		}
		serveJSON(w, status, serveActionResult{Refused: true, Reason: err.Error()})
		return
	}
	if projectID := strings.TrimSpace(body.string("projectId", "project_id", "project")); projectID != "" && projectID != approval.ProjectID {
		serveJSON(w, http.StatusNotFound, serveActionResult{Refused: true, Reason: "approval does not belong to the requested project"})
		return
	}
	revision, parseErr := strconv.Atoi(body.string("expectedRevision", "expected_revision", "revision"))
	if parseErr != nil || revision <= 0 {
		serveJSON(w, http.StatusBadRequest, serveActionResult{Refused: true, Reason: "expected approval revision is required"})
		return
	}
	settled, settleErr := s.store.SettleAgentAccessApproval(AgentAccessApprovalResponse{RequestID: requestID, ExpectedRevision: revision, Decision: firstNonEmpty(routeDecision, body.string("decision", "action")), Actor: actor})
	if settleErr != nil {
		serveJSON(w, http.StatusConflict, map[string]any{"ok": false, "refused": true, "reason": settleErr.Error(), "approval": settled})
		return
	}
	serveJSON(w, http.StatusOK, map[string]any{"ok": true, "approval": settled})
}
