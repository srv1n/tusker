package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	runnercore "tusker/internal/runner"
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
	privateFolders  []string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	ioWG   sync.WaitGroup

	writeMu                sync.Mutex
	echoMu                 sync.Mutex
	echoWait               chan string
	echoBody               string
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
	permissionDenied       atomic.Bool
	turnCompleted          atomic.Bool
	criticalOnce           sync.Once
	doneOnce               sync.Once
	terminalCode           RunFailureReasonCode
}

func validateClaudeSessionFlags(command string, argv []string) error {
	if len(argv) == 0 {
		var err error
		argv, err = shellLikeFields(command)
		if err != nil {
			return tuskerError(errorConfigInvalid, "cannot parse Claude command: "+err.Error())
		}
	}
	for _, arg := range argv {
		if arg == "--bare" {
			return tuskerError(errorConfigInvalid, "Claude runner command cannot use --bare")
		}
		if arg == "--session-id" || strings.HasPrefix(arg, "--session-id=") {
			return tuskerError(errorConfigInvalid, "Claude profile must not supply --session-id; Tusker assigns it")
		}
	}
	return nil
}

func validateClaudeResumeFlags(command string, argv []string) error {
	if len(argv) == 0 {
		var err error
		argv, err = shellLikeFields(command)
		if err != nil {
			return tuskerError(errorConfigInvalid, "cannot parse Claude command: "+err.Error())
		}
	}
	for _, arg := range argv {
		if arg == "--bare" {
			return tuskerError(errorConfigInvalid, "Claude runner command cannot use --bare")
		}
	}
	return nil
}

func claudeSessionArgv(argv []string, id string, resume *ResumeRequest) []string {
	out := make([]string, 0, len(argv)+2)
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--session-id" || arg == "--resume" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--session-id=") || strings.HasPrefix(arg, "--resume=") {
			continue
		}
		out = append(out, arg)
	}
	if resume != nil && strings.TrimSpace(resume.SessionRef) != "" {
		return append(out, "--resume", resume.SessionRef)
	}
	if id != "" {
		return append(out, "--session-id", id)
	}
	return out
}

func startLiveClaude(ctx context.Context, req StartRequest, resume *ResumeRequest) (*StartResult, error) {
	if len(req.CommandArgv) == 0 {
		return nil, tuskerError(errorConfigInvalid, "dispatch must supply a prepared argv; command-string launches are no longer supported")
	}
	if extensionPolicyRequestsNativeBridge(req.CodexPolicy.Extensions) {
		if err := NewEventLog(req.EventSinkPath).Append("extension_bridge_unsupported", req.AttemptID, RunnerClaude, map[string]any{
			"reason": "claude-code native extension bridge is not implemented",
		}); err != nil {
			return nil, fmt.Errorf("record unsupported Claude extension bridge: %w", err)
		}
		return nil, tuskerError(errorConfigInvalid, "claude-code extension bridge is unsupported; disable workflow extensions or use the Codex runner for extension tools")
	}

	policy := codexPolicyForLane(req.CodexPolicy, req.Lane)
	permissionMode, err := claudePermissionModeForPolicy(policy)
	if err != nil {
		return nil, err
	}
	if permissionMode != "bypassPermissions" && strings.Contains(strings.Join(req.CommandArgv, " "), "bypassPermissions") {
		return nil, tuskerError(errorConfigInvalid, "bounded Claude runner command cannot use bypassPermissions")
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
	argv := replaceTemplateArgv(req.CommandArgv, map[string]string{
		"{{workspace_path}}": workspaceCWD, "{{prompt_path}}": req.PromptPath,
		"{{raw_log_path}}": req.RawLogPath, "{{status_path}}": req.StatusPath,
		"{{note_path}}": req.NotePath, "{{vault_path}}": runnerWorkspaceVaultPath(workspaceCWD, req.VaultPath),
		"{{session_ref}}": resumeSessionRef(resume), "{{message_ref}}": resumeMessageRef(resume),
	})
	if req.NativeSessionID != "" || resume != nil {
		argv = claudeSessionArgv(argv, req.NativeSessionID, resume)
	}
	projection, err := projectWorkerMCP(req.ProjectID, req.RecordID, req.ItemID, req.AttemptID, req.LeaseGeneration, req.WorkRevision, req.EventSinkPath, req.StatusPath, 900, true)
	if err != nil {
		return nil, err
	}
	argv = appendClaudeMCP(argv, projection)
	if !slices.Contains(argv, "--replay-user-messages") {
		argv = append(argv, "--replay-user-messages")
	}
	if !filepath.IsAbs(argv[0]) {
		return nil, tuskerError(errorConfigInvalid, "prepared Claude executable must be an absolute path")
	}
	if err := completionVerifyExecutableIdentity(argv[0], req.CommandExecutableFP, req.CommandSearchPath); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workspaceCWD
	if err := assertRunnerCommandDir(RunnerClaude, cmd.Dir, req.WorkspacePath); err != nil {
		return nil, err
	}
	cmd.Env = runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkspacePath: workspaceCWD, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		RunnerPathPrefix: req.RunnerPathPrefix,
		NotePath:         req.NotePath, VaultPath: req.VaultPath, SessionRef: firstNonEmpty(req.NativeSessionID, resumeSessionRef(resume)), MessageRef: resumeMessageRef(resume),
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		CodexPolicy: policy,
	})
	if req.ContainmentPGID <= 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
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
	if req.ContainmentPGID > 0 && processGroupID(cmd.Process.Pid) != req.ContainmentPGID {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, tuskerError(errorInvalidTransition, "Claude child escaped wrapper containment")
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
		privateFolders:  append([]string(nil), req.PrivateFolders...),
		turnIndex:       -1,
		eventLog:        NewEventLog(req.EventSinkPath),
		runtimeStore:    runtimeStore,
		cmd:             cmd,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
	}
	// Keep stdin open in the wrapper: later soft Say delivery uses this stream-json channel.
	handle.setRefs(firstNonEmpty(req.NativeSessionID, resumeSessionRef(resume)), "")
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
	if err := handle.setPermissionMode(permissionMode); err != nil {
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
		SessionRef:   firstNonEmpty(handle.SessionRef(), req.NativeSessionID, resumeSessionRef(resume)),
		MessageRef:   firstNonEmpty(handle.MessageRef(), resumeMessageRef(resume)),
		StartedAt:    processStartedAt,
		PID:          pid,
		PGID:         processGroupID(pid),
		ProcessStart: processStartedAt,
		StatusPath:   req.StatusPath,
		Capabilities: (&ClaudeRunner{}).Capabilities(),
		Completed:    false,
		Outcome:      AttemptOutcomeNone,
	}, nil
}

func claudePermissionModeForPolicy(policy CodexPolicy) (string, error) {
	mode := strings.TrimSpace(firstNonEmpty(policy.TurnSandboxPolicy, policy.ThreadSandbox))
	switch mode {
	case "read-only":
		return "plan", nil
	case "danger-full-access":
		if strings.TrimSpace(policy.ApprovalPolicy) != "never" {
			return "", tuskerError(errorConfigInvalid, "Claude full access requires approval_policy=never")
		}
		return "bypassPermissions", nil
	case "workspace-write", "":
		return "", tuskerError(errorConfigInvalid, "Claude Code cannot enforce Tusker's bounded workspace-write preset")
	default:
		return "", tuskerError(errorConfigInvalid, "Claude Code cannot enforce sandbox mode "+mode)
	}
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
	if strings.TrimSpace(sessionRef) != "" && (h.sessionRef == "" || h.sessionRef == sessionRef) {
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
		return h.cmd.Process.Signal(syscall.SIGINT)
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

func (h *claudeLiveHandle) sendUserMessageAwaitEcho(body string, timeout time.Duration) (string, error) {
	// The control server handles one connection at a time; this is the sole
	// pending echo and its content must match the message just written.
	wait := make(chan string, 1)
	h.echoMu.Lock()
	h.echoBody, h.echoWait = body, wait
	h.echoMu.Unlock()
	defer func() {
		h.echoMu.Lock()
		h.echoBody, h.echoWait = "", nil
		h.echoMu.Unlock()
	}()
	if err := h.sendUserMessage(body); err != nil {
		return "", err
	}
	select {
	case uuid := <-wait:
		return uuid, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("Claude echo timeout")
	}
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
	if err := h.readClaudeLines(h.stdout, "stdout", h.handleStdoutLine); err != nil {
		h.failCriticalRunnerIO("stdout scan failed", err)
	}
}

func (h *claudeLiveHandle) readStderr() {
	defer h.ioWG.Done()
	defer h.stderr.Close()
	if err := h.readClaudeLines(h.stderr, "stderr", nil); err != nil {
		h.failCriticalRunnerIO("stderr scan failed", err)
	}
}

func (h *claudeLiveHandle) readClaudeLines(input io.Reader, source string, parse func(string)) error {
	const maxLine = 4 * 1024 * 1024
	const prefix = 64 * 1024
	reader := bufio.NewReaderSize(input, 64*1024)
	var line []byte
	total := 0
	for {
		part, err := reader.ReadSlice('\n')
		ended := len(part) > 0 && part[len(part)-1] == '\n'
		if ended {
			part = part[:len(part)-1]
		}
		total += len(part)
		if total <= maxLine {
			line = append(line, part...)
		} else if len(line) < prefix {
			line = append(line, part[:min(len(part), prefix-len(line))]...)
		} else if len(line) > prefix {
			line = line[:prefix]
		}
		if ended || errors.Is(err, io.EOF) {
			if total > maxLine {
				_ = appendRawLogLine(h.rawLogPath, string(line)+fmt.Sprintf(" [truncated %d bytes]", total-len(line)))
				if h.eventLog != nil {
					_ = h.eventLog.Append(source+"_line_oversized", h.attemptID, h.runner, map[string]any{"bytes": total, "truncated_bytes": total - len(line)})
				}
			} else if total > 0 {
				text := string(bytes.TrimSuffix(line, []byte{'\r'}))
				_ = appendRawLogLine(h.rawLogPath, text)
				if parse != nil {
					parse(text)
				}
			}
			line = nil
			total = 0
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return err
		}
	}
}

func (h *claudeLiveHandle) handleStdoutLine(line string) {
	h.setRefs(extractSessionRefFromJSON(line), extractMessageRefFromJSON(line))
	var payload map[string]any
	if json.Unmarshal([]byte(line), &payload) != nil {
		return
	}
	if stringValue(payload["type"]) == "user" {
		if message, ok := payload["message"].(map[string]any); ok && stringValue(message["content"]) != "" {
			h.echoMu.Lock()
			if h.echoWait != nil && stringValue(message["content"]) == h.echoBody {
				if uuid := strings.TrimSpace(stringValue(payload["uuid"])); uuid != "" {
					select {
					case h.echoWait <- uuid:
					default:
					}
				}
			}
			h.echoMu.Unlock()
		}
	}
	if stringValue(payload["type"]) == "system" && stringValue(payload["subtype"]) == "permission_denied" {
		h.permissionDenied.Store(true)
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
		if denials, ok := payload["permission_denials"].([]any); ok && len(denials) > 0 && subtype == "success" && !isError {
			tools := make([]string, 0, len(denials))
			for _, denial := range denials {
				if entry, ok := denial.(map[string]any); ok {
					if name := strings.TrimSpace(stringValue(entry["tool_name"])); name != "" {
						tools = append(tools, name)
					}
				}
			}
			if h.eventLog != nil {
				_ = h.eventLog.Append("claude_permission_denials", h.attemptID, h.runner, map[string]any{"count": len(denials), "tool_names": tools})
			}
		}
		h.terminalCode = claudeFailureCode(payload)
		if h.terminalCode == "" && h.permissionDenied.Load() && (isError || strings.Contains(subtype, "error")) {
			h.terminalCode = RunFailurePermissionDenied
		}
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

func claudeFailureCode(payload map[string]any) RunFailureReasonCode {
	if strings.TrimSpace(stringValue(payload["type"])) != "result" {
		return ""
	}
	denials, _ := payload["permission_denials"].([]any)
	if isError, _ := payload["is_error"].(bool); len(denials) > 0 && (strings.TrimSpace(stringValue(payload["subtype"])) != "success" || isError) {
		return RunFailurePermissionDenied
	}
	subtype := strings.TrimSpace(stringValue(payload["subtype"]))
	switch subtype {
	case "error_max_turns":
		return RunFailureMaxTurns
	case "error_max_budget_usd":
		return RunFailureMaxBudget
	case "error_during_execution":
		message := strings.ToLower(firstNonEmpty(stringValue(payload["error"]), stringValue(payload["result"])))
		switch {
		case strings.Contains(message, "usage limit"), strings.Contains(message, "rate limit"), strings.Contains(message, "quota exceeded"):
			return RunFailureUsageLimit
		case strings.Contains(message, "authentication"), strings.Contains(message, "unauthorized"), strings.Contains(message, "login required"):
			return RunFailureAuthExpired
		default:
			return RunFailureProviderError
		}
	}
	if isError, _ := payload["is_error"].(bool); isError {
		return RunFailureProviderError
	}
	return ""
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
		decision, awaited := h.awaitClaudeAgentAccessApproval(requestID, request)
		if !awaited {
			decision = h.evaluateToolApproval(request)
		}
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
		decision, awaited := h.awaitClaudeAgentAccessApproval(requestID, request)
		if !awaited {
			decision = h.evaluateToolApproval(request)
		}
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
	input := claudeToolInput(request)
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
	writesWorkspace := claudeToolWritesWorkspace(toolName, command)
	if reason := commandPolicyRejectReason(h.policy, runnercore.CommandPolicyRequest{Mutating: writesWorkspace, Destructive: destructiveAgentCommand(command), ReviewOnly: activeCodexPolicyIsReviewOnly(h.policy)}); reason != "" {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: reason, Subject: subject, Mutating: mutating}
	}
	decision := codexApprovalDecision{RequestType: "tool", Decision: "accept", Subject: subject, Mutating: mutating}
	// Run the provider-neutral destructive boundary before policy handling. The
	// native callback path already does this before creating a durable approval;
	// keeping the direct evaluator aligned prevents a caller from accidentally
	// bypassing those catastrophic/private/out-of-workspace checks.
	if destructiveAgentCommand(command) {
		if preflight := h.evaluateDestructiveToolApproval(request); preflight.Decision != "accept" {
			return preflight
		}
	}
	if reason := h.policyDenialReason(writesWorkspace); reason != "" {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: reason, Subject: subject, Mutating: mutating}
	}
	if path := h.privateFolderPath(request); path != "" {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: "tool approval rejected: private folder is excluded by the access policy", Subject: firstNonEmpty(path, subject), Mutating: mutating}
	}
	if commandMentionsSecretPath(command) {
		return codexApprovalDecision{RequestType: "tool", Decision: "reject", Reason: "tool approval rejected: command references a secret path", Subject: subject, Mutating: true}
	}
	return decision
}

func (h *claudeLiveHandle) privateFolderPath(request map[string]any) string {
	if h == nil || len(h.privateFolders) == 0 {
		return ""
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return ""
	}
	input := claudeToolInput(request)
	cwd := strings.TrimSpace(stringValue(input["cwd"]))
	if cwd == "" && h.cmd != nil {
		cwd = h.cmd.Dir
	}
	for _, candidate := range approvalPaths(raw) {
		if !filepath.IsAbs(candidate) && cwd != "" {
			candidate = filepath.Join(cwd, candidate)
		}
		if h.pathInPrivateFolder(candidate) {
			return candidate
		}
	}
	// Shell commands cannot be parsed as a general-purpose interpreter, but an
	// explicitly configured private path must not pass when it is present as a
	// command argument. This covers exact/quoted path references without
	// pretending to inspect arbitrary scripts.
	command := firstNonEmpty(strings.TrimSpace(stringValue(input["command"])), strings.TrimSpace(stringValue(input["cmd"])), strings.Join(stringListFromAny(input["argv"]), " "))
	for _, private := range h.privateFolders {
		if private != "" && strings.Contains(command, private) {
			return private
		}
	}
	return ""
}

func claudeToolInput(request map[string]any) map[string]any {
	for _, key := range []string{"input", "tool_input", "toolInput"} {
		if input, ok := request[key].(map[string]any); ok {
			return input
		}
	}
	return nil
}

func (h *claudeLiveHandle) pathInPrivateFolder(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	canonicalPath, err := canonicalAccessPath(path, false)
	if err != nil {
		canonicalPath = filepath.Clean(path)
	}
	for _, private := range h.privateFolders {
		canonicalPrivate, privateErr := canonicalAccessPath(private, false)
		if privateErr != nil {
			canonicalPrivate = filepath.Clean(private)
		}
		if accessPathContains(canonicalPrivate, canonicalPath) {
			return true
		}
	}
	return false
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

func claudeToolWritesWorkspace(toolName, command string) bool {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	switch normalized {
	case "read", "grep", "glob", "ls", "webfetch", "websearch":
		return false
	case "bash":
		return commandWritesWorkspace(command)
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
		_, _ = writeRunnerStatusFileIfAbsentWithOutcome(h.statusPath, exitCode, AttemptOutcomeNone, reason, 0, h.terminalCode)
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
			_ = h.cmd.Process.Kill()
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
