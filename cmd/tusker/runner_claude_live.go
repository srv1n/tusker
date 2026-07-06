package main

import (
	"bufio"
	"context"
	"encoding/json"
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
	projectID  string
	recordID   string
	itemID     string
	attemptID  string
	rawLogPath string
	statusPath string
	runner     RunnerName

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu     sync.Mutex
	nextID      atomic.Int64
	sessionMu   sync.RWMutex
	sessionRef  string
	messageRef  string
	interrupted atomic.Bool
	doneOnce    sync.Once
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
		_ = NewEventLog(req.EventSinkPath).Append("extension_bridge_unsupported", req.AttemptID, RunnerClaude, map[string]any{
			"reason": "claude-code native extension bridge is not implemented",
		})
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
		"{{session_ref}}":    firstNonEmpty(resumeSessionRef(resume), ""),
		"{{message_ref}}":    firstNonEmpty(resumeMessageRef(resume), ""),
	})
	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	cmd.Dir = workspaceCWD
	if err := assertRunnerCommandDir(RunnerClaude, cmd.Dir, req.WorkspacePath); err != nil {
		return nil, err
	}
	cmd.Env = runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, WorkspacePath: workspaceCWD, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		NotePath: req.NotePath, VaultPath: req.VaultPath, SessionRef: resumeSessionRef(resume), MessageRef: resumeMessageRef(resume),
		CodexPolicy: withDefaultCodexPolicy(req.CodexPolicy),
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
	handle := &claudeLiveHandle{
		projectID:  req.ProjectID,
		recordID:   req.RecordID,
		itemID:     req.ItemID,
		attemptID:  req.AttemptID,
		rawLogPath: req.RawLogPath,
		statusPath: req.StatusPath,
		runner:     RunnerClaude,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
	}
	handle.nextID.Store(1)
	liveRegistry.Register(handle)
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
	handle.waitForSession(2 * time.Second)

	pid := cmd.Process.Pid
	processStartedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	return &StartResult{
		SessionRef:   firstNonEmpty(handle.SessionRef(), resumeSessionRef(resume)),
		MessageRef:   handle.MessageRef(),
		StartedAt:    processStartedAt,
		PID:          pid,
		PGID:         processGroupID(pid),
		ProcessStart: processStartedAt,
		StatusPath:   req.StatusPath,
		Capabilities: RunnerCapabilities{StructuredEvents: true, ResumeSession: true, ExplicitApprovals: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true},
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
	scanner := bufio.NewScanner(h.stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_ = appendRawLogLine(h.rawLogPath, line)
		h.handleStdoutLine(line)
	}
}

func (h *claudeLiveHandle) readStderr() {
	scanner := bufio.NewScanner(h.stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		_ = appendRawLogLine(h.rawLogPath, scanner.Text())
	}
}

func (h *claudeLiveHandle) handleStdoutLine(line string) {
	h.setRefs(extractSessionRefFromJSON(line), extractMessageRefFromJSON(line))
	var payload map[string]any
	if json.Unmarshal([]byte(line), &payload) != nil {
		return
	}
	switch strings.TrimSpace(stringValue(payload["type"])) {
	case "control_request":
		h.handleControlRequest(payload)
	case "result":
		isError, _ := payload["is_error"].(bool)
		subtype := strings.TrimSpace(stringValue(payload["subtype"]))
		switch {
		case subtype == "interrupted" || h.interrupted.Load():
			h.finalize(130)
		case isError || strings.Contains(subtype, "error"):
			h.finalize(1)
		default:
			h.finalize(0)
		}
	}
}

func (h *claudeLiveHandle) handleControlRequest(payload map[string]any) {
	requestID := strings.TrimSpace(stringValue(payload["request_id"]))
	request, _ := payload["request"].(map[string]any)
	subtype := strings.TrimSpace(stringValue(request["subtype"]))
	var response any
	switch subtype {
	case "can_use_tool":
		response = map[string]any{
			"behavior":     "allow",
			"updatedInput": request["input"],
		}
	case "hook_callback":
		response = map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "allow",
				"permissionDecisionReason": "Auto-approved by Tusker",
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

func (h *claudeLiveHandle) finalize(exitCode int) {
	h.doneOnce.Do(func() {
		_ = writeRunnerStatusFile(h.statusPath, exitCode)
		liveRegistry.Unregister(h.attemptID)
	})
}

func (h *claudeLiveHandle) waitForExit() {
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
