package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type claudeLiveHandle struct {
	projectID       string
	recordID        string
	itemID          string
	attemptID       string
	leaseGeneration int
	eventSinkPath   string
	rawLogPath      string
	statusPath      string
	runner          RunnerName
	policy          CodexPolicy

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	ioWG   sync.WaitGroup

	writeMu                sync.Mutex
	nextID                 atomic.Int64
	sessionMu              sync.RWMutex
	sessionRef             string
	messageRef             string
	turnID                 string
	turnIndex              int
	eventLog               *EventLog
	runtimeStore           *RuntimeStore
	interrupted            atomic.Bool
	providerResultObserved atomic.Bool
	turnCompleted          atomic.Bool
	criticalOnce           sync.Once
	doneOnce               sync.Once
}

func shouldUseLiveClaude(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}
	return strings.Contains(command, "stream-json") || strings.Contains(command, "--input-format")
}

func startLiveClaude(ctx context.Context, req StartRequest, resume *ResumeRequest) (*StartResult, error) {
	if extensionPolicyRequestsNativeBridge(req.CodexPolicy.Extensions) {
		if err := NewEventLog(req.EventSinkPath).Append("extension_bridge_unsupported", req.AttemptID, RunnerClaude, map[string]any{
			"reason": "claude-code native extension bridge is not implemented",
		}); err != nil {
			return nil, fmt.Errorf("record unsupported Claude extension bridge: %w", err)
		}
		return nil, tuskerError(errorConfigInvalid, "claude-code extension bridge is unsupported; disable workflow extensions or use the Codex runner for extension tools")
	}

	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
		if resume != nil && strings.TrimSpace(resume.SessionRef) != "" {
			command += " --resume {{session_ref}}"
			if strings.TrimSpace(resume.MessageRef) != "" {
				command += " --resume-session-at {{message_ref}}"
			}
		}
	}
	if err := ensureDir(filepath.Dir(req.RawLogPath)); err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Dir(req.StatusPath)); err != nil {
		return nil, err
	}
	workspaceCWD, err := runnerWorkspaceCWD(RunnerClaude, req.WorkspacePath)
	if err != nil {
		return nil, err
	}
	command = replaceTemplateTokens(command, map[string]string{
		"{{workspace_path}}": workspaceCWD,
		"{{prompt_path}}":    req.PromptPath,
		"{{raw_log_path}}":   req.RawLogPath,
		"{{status_path}}":    req.StatusPath,
		"{{note_path}}":      req.NotePath,
		"{{vault_path}}":     req.VaultPath,
		"{{session_ref}}":    resumeSessionRef(resume),
		"{{message_ref}}":    resumeMessageRef(resume),
	})
	command = runnerCommandWithPathPrefix(command, req.RunnerPathPrefix)
	policy := codexPolicyForLane(req.CodexPolicy, req.Lane)
	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	cmd.Dir = workspaceCWD
	if err := assertRunnerCommandDir(RunnerClaude, cmd.Dir, req.WorkspacePath); err != nil {
		return nil, err
	}
	cmd.Env = runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkspacePath: workspaceCWD, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		RunnerPathPrefix: req.RunnerPathPrefix,
		NotePath:         req.NotePath, VaultPath: req.VaultPath, SessionRef: resumeSessionRef(resume), MessageRef: resumeMessageRef(resume),
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		CodexPolicy: policy,
	})
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	runtimeStore, _ := OpenRuntimeStore(DefaultStateRoot())
	handle := &claudeLiveHandle{
		projectID:       req.ProjectID,
		recordID:        req.RecordID,
		itemID:          req.ItemID,
		attemptID:       req.AttemptID,
		leaseGeneration: req.LeaseGeneration,
		eventSinkPath:   req.EventSinkPath,
		rawLogPath:      req.RawLogPath,
		statusPath:      req.StatusPath,
		runner:          RunnerClaude,
		policy:          policy,
		turnIndex:       -1,
		eventLog:        NewEventLog(req.EventSinkPath),
		runtimeStore:    runtimeStore,
		cmd:             cmd,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
	}
	handle.nextID.Store(1)
	liveRegistry.Register(handle)
	handle.ioWG.Add(2)
	go handle.readStdout()
	go handle.readStderr()
	go handle.waitForExit()

	if err := handle.initialize(); err != nil {
		_ = handle.Interrupt(context.Background())
		liveRegistry.Unregister(handle.attemptID)
		return nil, err
	}
	if err := handle.setPermissionMode("bypassPermissions"); err != nil {
		_ = appendRawLogLine(req.RawLogPath, "failed to set claude permission mode: "+err.Error())
	}
	prompt, err := readText(req.PromptPath)
	if err != nil {
		return nil, err
	}
	if err := handle.sendUserMessage(prompt); err != nil {
		return nil, err
	}
	handle.waitForSession(5 * time.Second)

	pid := cmd.Process.Pid
	processStartedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	return &StartResult{
		SessionRef:   firstNonEmpty(handle.SessionRef(), resumeSessionRef(resume)),
		MessageRef:   firstNonEmpty(handle.MessageRef(), resumeMessageRef(resume)),
		StartedAt:    processStartedAt,
		PID:          pid,
		PGID:         processGroupID(pid),
		ProcessStart: processStartedAt,
		StatusPath:   req.StatusPath,
		Capabilities: RunnerCapabilities{StructuredEvents: true, ResumeSession: false, ExplicitApprovals: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true},
		Completed:    false,
		Outcome:      AttemptOutcomeNone,
	}, nil
}

func extensionPolicyRequestsNativeBridge(policy ExtensionPolicy) bool {
	policy = withDefaultExtensionPolicy(policy)
	return policy.Enabled && (len(policy.AllowedTools) > 0 || len(policy.AllowedMCPs) > 0 || policy.AllowTuskerReadTools)
}

func resumeSessionRef(resume *ResumeRequest) string {
	if resume == nil {
		return ""
	}
	return resume.SessionRef
}

func resumeMessageRef(resume *ResumeRequest) string {
	if resume == nil {
		return ""
	}
	return resume.MessageRef
}

func (h *claudeLiveHandle) AttemptID() string  { return h.attemptID }
func (h *claudeLiveHandle) ProjectID() string  { return h.projectID }
func (h *claudeLiveHandle) RecordID() string   { return h.recordID }
func (h *claudeLiveHandle) ItemID() string     { return h.itemID }
func (h *claudeLiveHandle) Runner() RunnerName { return h.runner }

func (h *claudeLiveHandle) SessionRef() string {
	h.sessionMu.RLock()
	defer h.sessionMu.RUnlock()
	return h.sessionRef
}

func (h *claudeLiveHandle) MessageRef() string {
	h.sessionMu.RLock()
	defer h.sessionMu.RUnlock()
	return h.messageRef
}

func (h *claudeLiveHandle) setRefs(sessionRef, messageRef string) {
	h.sessionMu.Lock()
	defer h.sessionMu.Unlock()
	if strings.TrimSpace(sessionRef) != "" {
		h.sessionRef = sessionRef
	}
	if strings.TrimSpace(messageRef) != "" {
		h.messageRef = messageRef
	}
}

func (h *claudeLiveHandle) waitForSession(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.TrimSpace(h.SessionRef()) != "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (h *claudeLiveHandle) Interrupt(ctx context.Context) error {
	h.interrupted.Store(true)
	if err := h.controlRequest("interrupt", map[string]any{}); err == nil {
		return nil
	}
	if h.cmd != nil && h.cmd.Process != nil {
		return syscall.Kill(-h.cmd.Process.Pid, syscall.SIGINT)
	}
	return nil
}

func (h *claudeLiveHandle) initialize() error {
	return h.controlRequest("initialize", map[string]any{"hooks": nil})
}

func (h *claudeLiveHandle) setPermissionMode(mode string) error {
	return h.controlRequest("set_permission_mode", map[string]any{"mode": mode})
}

func (h *claudeLiveHandle) sendUserMessage(prompt string) error {
	return h.writeJSON(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	})
}

func (h *claudeLiveHandle) controlRequest(subtype string, request any) error {
	return h.writeJSON(map[string]any{
		"type":       "control_request",
		"request_id": strconv.FormatInt(h.nextID.Add(1), 10),
		"request":    withSubtype(subtype, request),
	})
}

func withSubtype(subtype string, request any) any {
	if request == nil {
		return map[string]any{"subtype": subtype}
	}
	payload, ok := request.(map[string]any)
	if !ok {
		return map[string]any{"subtype": subtype}
	}
	payload["subtype"] = subtype
	return payload
}

func (h *claudeLiveHandle) writeJSON(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_, err = h.stdin.Write(append(raw, '\n'))
	return err
}

func (h *claudeLiveHandle) readStdout() {
	defer h.ioWG.Done()
	defer h.stdout.Close()
	scanner := bufio.NewScanner(h.stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_ = appendRawLogLine(h.rawLogPath, line)
		h.handleStdoutLine(line)
	}
	if err := scanner.Err(); err != nil {
		h.failCriticalRunnerIO("stdout scan failed", err)
	}
}

func (h *claudeLiveHandle) readStderr() {
	defer h.ioWG.Done()
	defer h.stderr.Close()
	scanner := bufio.NewScanner(h.stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		_ = appendRawLogLine(h.rawLogPath, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		h.failCriticalRunnerIO("stderr scan failed", err)
	}
}

func (h *claudeLiveHandle) handleStdoutLine(line string) {
	h.setRefs(extractSessionRefFromJSON(line), extractMessageRefFromJSON(line))
	var payload map[string]any
	if json.Unmarshal([]byte(line), &payload) != nil {
		return
	}
	// Claude stream JSON carries both top-level session metadata and native
	// subagent hook facts. Persist those through the same untrusted envelope as
	// replay, never by mutating run ownership or substituting a child id for the
	// resumable parent session.
	h.observeExecutionPayload(payload)
	h.observeStreamPayload(payload)
	switch strings.TrimSpace(stringValue(payload["type"])) {
	case "control_request":
		h.handleControlRequest(payload)
	case "result":
		isError, _ := payload["is_error"].(bool)
		subtype := strings.TrimSpace(stringValue(payload["subtype"]))
		status := "completed"
		reason := ""
		switch {
		case subtype == "interrupted" || h.interrupted.Load():
			status = "interrupted"
			reason = "interrupted"
			h.recordTurnCompleted(h.ensureTurnID(payload), status, reason, time.Now().UTC().Format(time.RFC3339))
			h.finalize(130)
		case isError || strings.Contains(subtype, "error"):
			status = "failed"
			reason = firstNonEmpty(strings.TrimSpace(stringValue(payload["error"])), subtype)
			h.recordTurnCompleted(h.ensureTurnID(payload), status, reason, time.Now().UTC().Format(time.RFC3339))
			h.finalize(1)
		default:
			h.recordTurnCompleted(h.ensureTurnID(payload), status, reason, time.Now().UTC().Format(time.RFC3339))
			h.finalize(0)
		}
	}
}

func (h *claudeLiveHandle) observeExecutionPayload(payload map[string]any) {
	if h.runtimeStore == nil {
		return
	}
	if _, err := (ClaudeExecutionAdapter{Store: h.runtimeStore}).ObserveRunPayload(RunStatus{
		ProjectID: h.projectID, RecordID: h.recordID, ItemID: h.itemID, ActiveAttemptID: h.attemptID,
		Runner: string(RunnerClaude), SessionRef: h.SessionRef(),
	}, payload, 0, "claude_stream_json"); err != nil {
		_ = appendRawLogLine(h.rawLogPath, "claude execution observation rejected: "+err.Error())
	} else if strings.EqualFold(strings.TrimSpace(stringValue(payload["type"])), "result") {
		// A parsed provider result is stronger than process EOF. Do not append a
		// second synthetic terminal observation when waitForExit runs afterwards.
		h.providerResultObserved.Store(true)
	}
}

func (h *claudeLiveHandle) handleControlRequest(payload map[string]any) {
	requestID := strings.TrimSpace(stringValue(payload["request_id"]))
	request, _ := payload["request"].(map[string]any)
	subtype := strings.TrimSpace(stringValue(request["subtype"]))
	var response any
	switch subtype {
	case "can_use_tool":
		decision := h.evaluateToolApproval(request)
		h.recordApprovalDecision("can_use_tool", decision)
		if decision.Decision == "accept" {
			response = map[string]any{
				"behavior":     "allow",
				"updatedInput": request["input"],
			}
		} else {
			response = map[string]any{
				"behavior": "deny",
				"message":  decision.Reason,
			}
		}
	case "hook_callback":
		decision := h.evaluateToolApproval(request)
		h.recordApprovalDecision("hook_callback", decision)
		permissionDecision := "allow"
		if decision.Decision != "accept" {
			permissionDecision = "deny"
		}
		response = map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       permissionDecision,
				"permissionDecisionReason": firstNonEmpty(decision.Reason, "Evaluated by Tusker"),
			},
		}
	default:
		response = nil
	}
	_ = h.writeJSON(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   response,
		},
	})
}

func (h *claudeLiveHandle) observeStreamPayload(payload map[string]any) {
	now := time.Now().UTC().Format(time.RFC3339)
	if turnID := h.turnRefFromPayload(payload); turnID != "" {
		h.recordTurnStarted(turnID, now, map[string]any{
			"source":      "claude_stream_json",
			"stream_type": strings.TrimSpace(stringValue(payload["type"])),
		})
	}
	var usage turnUsageCounters
	collectUsageCounters(payload, &usage)
	if usage.totalTokens == 0 && (usage.inputTokens > 0 || usage.outputTokens > 0) {
		usage.totalTokens = usage.inputTokens + usage.outputTokens
	}
	if usage.hasAny() {
		h.recordTurnUsage("claude_stream_json", now, usage)
	}
}

func (h *claudeLiveHandle) evaluateToolApproval(request map[string]any) codexApprovalDecision {
	input, _ := request["input"].(map[string]any)
	toolName := firstNonEmpty(
		strings.TrimSpace(stringValue(request["name"])),
		strings.TrimSpace(stringValue(request["tool_name"])),
		strings.TrimSpace(stringValue(request["toolName"])),
		strings.TrimSpace(stringValue(request["tool"])),
	)
	command := firstNonEmpty(
		strings.TrimSpace(stringValue(input["command"])),
		strings.TrimSpace(stringValue(input["cmd"])),
		strings.Join(stringListFromAny(input["argv"]), " "),
	)
	subject := firstNonEmpty(command, toolName)
	mutating := claudeToolLooksMutating(toolName, command)
	decision := codexApprovalDecision{RequestType: "tool", Decision: "accept", Subject: subject, Mutating: mutating}
	if reason := h.policyDenialReason(mutating); reason != "" {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: reason, Subject: subject, Mutating: mutating}
	}
	if commandContainsUnsafeGitMutation(command) {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: "tool approval rejected: unsafe git state mutation is not allowed", Subject: subject, Mutating: true}
	}
	if commandMentionsSecretPath(command) {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: "tool approval rejected: command references a secret path", Subject: subject, Mutating: true}
	}
	return decision
}

func claudeToolLooksMutating(toolName, command string) bool {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	switch normalized {
	case "read", "grep", "glob", "ls", "webfetch", "websearch":
		return false
	case "bash":
		return commandLooksMutating(command)
	case "write", "edit", "multiedit", "notebookedit", "todowrite":
		return true
	default:
		return true
	}
}

func (h *claudeLiveHandle) policyDenialReason(mutating bool) string {
	approvalPolicy := strings.TrimSpace(h.policy.ApprovalPolicy)
	if approvalPolicy == "never" {
		return "approval_policy=never rejects Claude Code tool approval requests"
	}
	activeSandbox := firstNonEmpty(strings.TrimSpace(h.policy.TurnSandboxPolicy), strings.TrimSpace(h.policy.ThreadSandbox))
	if mutating && activeSandbox == "read-only" {
		return "read-only sandbox rejects mutating Claude Code tool approval requests"
	}
	if approvalPolicy == "on-request" || approvalPolicy == "untrusted" {
		return "approval_policy=" + approvalPolicy + " requires human approval; Tusker rejects instead of silently approving"
	}
	return ""
}

func (h *claudeLiveHandle) recordApprovalDecision(method string, decision codexApprovalDecision) {
	reason := strings.TrimSpace(decision.Reason)
	message := "claude approval " + decision.Decision + ": method=" + method + " type=" + decision.RequestType
	if reason != "" {
		message += " reason=" + reason
	}
	_ = appendRawLogLine(h.rawLogPath, message)
	if h.eventLog == nil || strings.TrimSpace(h.eventSinkPath) == "" {
		return
	}
	_ = h.eventLog.Append("claude_approval_decision", h.attemptID, h.runner, map[string]any{
		"project_id":          h.projectID,
		"record_id":           h.recordID,
		"item_id":             h.itemID,
		"attempt_id":          h.attemptID,
		"session_ref":         h.SessionRef(),
		"turn_id":             h.turnID,
		"method":              method,
		"request_type":        decision.RequestType,
		"decision":            decision.Decision,
		"reason":              reason,
		"subject":             decision.Subject,
		"mutating":            decision.Mutating,
		"approval_policy":     h.policy.ApprovalPolicy,
		"thread_sandbox":      h.policy.ThreadSandbox,
		"turn_sandbox_policy": h.policy.TurnSandboxPolicy,
	})
}

func (h *claudeLiveHandle) recordTurnStarted(turnID, at string, payload map[string]any) {
	turnID = firstNonEmpty(strings.TrimSpace(turnID), h.turnID)
	if turnID == "" {
		return
	}
	h.turnID = turnID
	h.ensureTurnIndex()
	h.appendNormalizedTurnEvent("turn_started", at, h.payloadWithTurn(turnID, payload))
	h.saveTurn(RunTurn{
		AttemptID:   h.attemptID,
		ProjectID:   h.projectID,
		RecordID:    h.recordID,
		TurnID:      turnID,
		TurnIndex:   h.turnIndex,
		SessionRef:  h.SessionRef(),
		Status:      "running",
		StartedAt:   at,
		LastEventAt: at,
	})
}

func (h *claudeLiveHandle) recordTurnUsage(source, at string, usage turnUsageCounters) {
	turnID := firstNonEmpty(usage.turnID, h.turnID, h.MessageRef(), h.ensureTurnID(nil))
	if turnID == "" {
		return
	}
	h.turnID = turnID
	h.ensureTurnIndex()
	h.appendNormalizedTurnEvent("turn_usage_updated", at, h.payloadWithTurn(turnID, map[string]any{
		"source":        source,
		"input_tokens":  usage.inputTokens,
		"output_tokens": usage.outputTokens,
		"total_tokens":  usage.totalTokens,
	}))
	h.saveTurn(RunTurn{
		AttemptID:    h.attemptID,
		ProjectID:    h.projectID,
		RecordID:     h.recordID,
		TurnID:       turnID,
		TurnIndex:    h.turnIndex,
		SessionRef:   h.SessionRef(),
		Status:       "running",
		InputTokens:  usage.inputTokens,
		OutputTokens: usage.outputTokens,
		TotalTokens:  usage.totalTokens,
		LastEventAt:  at,
	})
}

func (h *claudeLiveHandle) recordTurnCompleted(turnID, status, reason, at string) {
	if !h.turnCompleted.CompareAndSwap(false, true) {
		return
	}
	turnID = firstNonEmpty(strings.TrimSpace(turnID), h.ensureTurnID(nil))
	if turnID == "" {
		return
	}
	h.turnID = turnID
	h.ensureTurnIndex()
	h.appendNormalizedTurnEvent("turn_completed", at, h.payloadWithTurn(turnID, map[string]any{
		"status":     status,
		"last_error": reason,
	}))
	h.saveTurn(RunTurn{
		AttemptID:   h.attemptID,
		ProjectID:   h.projectID,
		RecordID:    h.recordID,
		TurnID:      turnID,
		TurnIndex:   h.turnIndex,
		SessionRef:  h.SessionRef(),
		Status:      status,
		CompletedAt: at,
		LastEventAt: at,
		LastError:   reason,
	})
}

func (h *claudeLiveHandle) appendNormalizedTurnEvent(kind, at string, payload map[string]any) {
	if h.eventLog == nil || strings.TrimSpace(h.eventSinkPath) == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["normalized_at"] = at
	_ = h.eventLog.Append(kind, h.attemptID, h.runner, payload)
}

func (h *claudeLiveHandle) saveTurn(turn RunTurn) {
	if h.runtimeStore == nil {
		return
	}
	_ = h.runtimeStore.SaveTurn(turn)
}

func (h *claudeLiveHandle) ensureTurnIndex() {
	if h.turnIndex >= 0 {
		return
	}
	if h.runtimeStore == nil {
		h.turnIndex = 0
		return
	}
	index, err := h.runtimeStore.NextTurnIndex(h.projectID, h.recordID, h.attemptID)
	if err != nil {
		h.turnIndex = 0
		return
	}
	h.turnIndex = index
}

func (h *claudeLiveHandle) payloadWithTurn(turnID string, payload map[string]any) map[string]any {
	out := map[string]any{
		"project_id":  h.projectID,
		"record_id":   h.recordID,
		"item_id":     h.itemID,
		"attempt_id":  h.attemptID,
		"session_ref": h.SessionRef(),
		"turn_id":     turnID,
		"turn_index":  h.turnIndex,
	}
	for key, value := range payload {
		if key == "last_error" && strings.TrimSpace(stringValue(value)) == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func (h *claudeLiveHandle) turnRefFromPayload(payload map[string]any) string {
	return firstNonEmpty(
		findMessageRef(payload),
		strings.TrimSpace(stringValue(payload["message_id"])),
		strings.TrimSpace(stringValue(payload["messageId"])),
		strings.TrimSpace(stringValue(payload["uuid"])),
	)
}

func (h *claudeLiveHandle) ensureTurnID(payload map[string]any) string {
	if strings.TrimSpace(h.turnID) != "" {
		return h.turnID
	}
	if payload != nil {
		if turnID := h.turnRefFromPayload(payload); turnID != "" {
			h.turnID = turnID
			return h.turnID
		}
	}
	if messageRef := h.MessageRef(); messageRef != "" {
		h.turnID = messageRef
		return h.turnID
	}
	if sessionRef := h.SessionRef(); sessionRef != "" {
		h.turnID = sessionRef + "-turn"
		return h.turnID
	}
	h.turnID = h.attemptID + "-turn"
	return h.turnID
}

func (h *claudeLiveHandle) finalize(exitCode int) {
	h.doneOnce.Do(func() {
		now := time.Now().UTC().Format(time.RFC3339)
		if !h.providerResultObserved.Load() {
			h.observeProcessExitWithoutResult(exitCode, now)
		}
		status := "completed"
		reason := ""
		switch {
		case exitCode == 130:
			status = "interrupted"
			reason = "interrupted"
		case exitCode != 0:
			status = "failed"
			reason = "runner exited with code " + strconv.Itoa(exitCode)
		}
		h.recordTurnCompleted(h.ensureTurnID(nil), status, reason, now)
		_ = writeRunnerStatusFile(h.statusPath, exitCode)
		liveRegistry.Unregister(h.attemptID)
	})
}

// observeProcessExitWithoutResult records that the local stream ended before
// Claude supplied a typed result. It is a degraded terminal boundary for
// recovery only, not a provider success/failure claim and never an ownership
// or process-state mutation for a native child.
func (h *claudeLiveHandle) observeProcessExitWithoutResult(exitCode int, at string) {
	if h.runtimeStore == nil || strings.TrimSpace(h.SessionRef()) == "" {
		return
	}
	sessionID := h.SessionRef()
	parentID, err := (ClaudeExecutionAdapter{Store: h.runtimeStore}).executionForClaudeRun(RunStatus{ProjectID: h.projectID, ActiveAttemptID: h.attemptID}, sessionID)
	if err != nil || parentID == "" {
		if err != nil {
			_ = appendRawLogLine(h.rawLogPath, "claude process-exit observation lookup failed: "+err.Error())
		}
		return
	}
	_, err = (ClaudeExecutionAdapter{Store: h.runtimeStore}).Observe(ClaudeExecutionObservation{
		ProjectID: h.projectID, ParentExecutionID: parentID, SessionID: sessionID,
		SourceEventID: "claude-process-exit:" + h.attemptID + ":" + sessionID,
		Kind:          "process_exit_without_result", Status: "completed", OccurredAt: at,
		Metadata:                 map[string]any{"observation_source": "process_exit_without_result", "provider_outcome_claimed": false, "exit_code": exitCode},
		VisibilityDegradedReason: "process_exit_without_result_requires_authoritative_fetch",
	})
	if err != nil {
		_ = appendRawLogLine(h.rawLogPath, "claude process-exit observation rejected: "+err.Error())
	}
}

func (h *claudeLiveHandle) failCriticalRunnerIO(message string, err error) {
	if err == nil {
		return
	}
	h.criticalOnce.Do(func() {
		_ = appendRawLogLine(h.rawLogPath, message+": "+err.Error())
		if h.cmd != nil && h.cmd.Process != nil {
			_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGKILL)
		}
		h.doneOnce.Do(func() {})
		_ = writeRunnerStatusFile(h.statusPath, 1)
		liveRegistry.Unregister(h.attemptID)
	})
}

func (h *claudeLiveHandle) waitForExit() {
	h.ioWG.Wait()
	err := h.cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	if h.interrupted.Load() && exitCode == 0 {
		exitCode = 130
	}
	h.finalize(exitCode)
}
