package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type codexRPCResponse struct {
	result json.RawMessage
	err    string
}

type codexLiveHandle struct {
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
	activeStates    []string
	notePath        string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan codexRPCResponse
	nextID    atomic.Int64

	threadID     string
	messageRef   string
	turnID       string
	turnIndex    int
	eventLog     *EventLog
	runtimeStore *RuntimeStore
	interrupted  atomic.Bool
	doneOnce     sync.Once
}

func shouldUseLiveCodex(command string) bool {
	command = strings.TrimSpace(command)
	return command == "" || strings.Contains(command, "app-server")
}

func startLiveCodex(ctx context.Context, req StartRequest, resume *ResumeRequest) (*StartResult, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "codex app-server"
	}
	if err := ensureDir(filepath.Dir(req.RawLogPath)); err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Dir(req.StatusPath)); err != nil {
		return nil, err
	}
	workspaceCWD, err := runnerWorkspaceCWD(RunnerCodex, req.WorkspacePath)
	if err != nil {
		return nil, err
	}
	policy := codexPolicyForLane(req.CodexPolicy, req.Lane)
	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	cmd.Dir = workspaceCWD
	if err := assertRunnerCommandDir(RunnerCodex, cmd.Dir, req.WorkspacePath); err != nil {
		return nil, err
	}
	cmd.Env = runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkspacePath: workspaceCWD, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: policy,
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
	handle := &codexLiveHandle{
		projectID:       req.ProjectID,
		recordID:        req.RecordID,
		itemID:          req.ItemID,
		attemptID:       req.AttemptID,
		leaseGeneration: req.LeaseGeneration,
		eventSinkPath:   req.EventSinkPath,
		rawLogPath:      req.RawLogPath,
		statusPath:      req.StatusPath,
		runner:          RunnerCodex,
		policy:          policy,
		activeStates:    append([]string{}, req.ActiveStates...),
		notePath:        req.NotePath,
		turnIndex:       -1,
		eventLog:        NewEventLog(req.EventSinkPath),
		runtimeStore:    runtimeStore,
		cmd:             cmd,
		stdin:           stdin,
		stdout:          stdout,
		stderr:          stderr,
		pending:         map[string]chan codexRPCResponse{},
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

	threadID, err := handle.threadStart(workspaceCWD)
	if err != nil {
		_ = handle.Interrupt(context.Background())
		liveRegistry.Unregister(handle.attemptID)
		return nil, err
	}
	handle.threadID = threadID

	prompt, err := readText(req.PromptPath)
	if err != nil {
		return nil, err
	}
	turnID, err := handle.turnStart(threadID, prompt)
	if err != nil {
		return nil, err
	}
	handle.turnID = turnID

	pid := cmd.Process.Pid
	processStartedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	return &StartResult{
		SessionRef:   threadID,
		MessageRef:   handle.messageRef,
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

func (h *codexLiveHandle) AttemptID() string  { return h.attemptID }
func (h *codexLiveHandle) ProjectID() string  { return h.projectID }
func (h *codexLiveHandle) RecordID() string   { return h.recordID }
func (h *codexLiveHandle) ItemID() string     { return h.itemID }
func (h *codexLiveHandle) Runner() RunnerName { return h.runner }

func (h *codexLiveHandle) Interrupt(ctx context.Context) error {
	h.interrupted.Store(true)
	if h.threadID != "" && h.turnID != "" {
		if err := h.request(ctx, "turn/interrupt", map[string]any{
			"threadId": h.threadID,
			"turnId":   h.turnID,
		}, nil); err == nil {
			return nil
		}
	}
	if h.cmd != nil && h.cmd.Process != nil {
		return syscall.Kill(-h.cmd.Process.Pid, syscall.SIGINT)
	}
	return nil
}

func (h *codexLiveHandle) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := h.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "tusker",
			"version": "dev",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, nil); err != nil {
		return err
	}
	return h.notify("initialized", nil)
}

func (h *codexLiveHandle) threadStart(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.readTimeout())
	defer cancel()
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	err := h.request(ctx, "thread/start", map[string]any{
		"cwd":            cwd,
		"approvalPolicy": h.policy.ApprovalPolicy,
		"sandbox":        h.policy.ThreadSandbox,
	}, &resp)
	return resp.Thread.ID, err
}

func (h *codexLiveHandle) threadResume(sessionRef, cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.readTimeout())
	defer cancel()
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	err := h.request(ctx, "thread/resume", map[string]any{
		"threadId":       sessionRef,
		"cwd":            cwd,
		"approvalPolicy": h.policy.ApprovalPolicy,
		"sandbox":        h.policy.ThreadSandbox,
		"excludeTurns":   true,
	}, &resp)
	return resp.Thread.ID, err
}

func (h *codexLiveHandle) threadFork(sessionRef, cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.readTimeout())
	defer cancel()
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	err := h.request(ctx, "thread/fork", map[string]any{
		"threadId":       sessionRef,
		"cwd":            cwd,
		"approvalPolicy": h.policy.ApprovalPolicy,
		"sandbox":        h.policy.ThreadSandbox,
		"ephemeral":      false,
	}, &resp)
	return resp.Thread.ID, err
}

func (h *codexLiveHandle) turnStart(threadID, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), h.turnTimeout())
	defer cancel()
	var resp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	err := h.request(ctx, "turn/start", map[string]any{
		"threadId":       threadID,
		"cwd":            h.cmd.Dir,
		"approvalPolicy": h.policy.ApprovalPolicy,
		"sandboxPolicy":  codexTurnSandboxPolicy(h.policy.TurnSandboxPolicy, h.cmd.Dir),
		"input": []map[string]any{
			{"type": "text", "text": prompt, "text_elements": []any{}},
		},
	}, &resp)
	return resp.Turn.ID, err
}

func codexTurnSandboxPolicy(policy, cwd string) map[string]any {
	switch strings.TrimSpace(policy) {
	case "danger-full-access":
		return map[string]any{"type": "dangerFullAccess"}
	case "read-only":
		return map[string]any{"type": "readOnly", "networkAccess": false}
	default:
		return map[string]any{
			"type":                "workspaceWrite",
			"writableRoots":       []string{cwd},
			"networkAccess":       false,
			"excludeTmpdirEnvVar": false,
			"excludeSlashTmp":     false,
		}
	}
}

func (h *codexLiveHandle) readTimeout() time.Duration {
	if h.policy.ReadTimeoutMS <= 0 {
		return 30 * time.Second
	}
	return time.Duration(h.policy.ReadTimeoutMS) * time.Millisecond
}

func (h *codexLiveHandle) turnTimeout() time.Duration {
	if h.policy.TurnTimeoutMS <= 0 {
		return 30 * time.Second
	}
	return time.Duration(h.policy.TurnTimeoutMS) * time.Millisecond
}

func (h *codexLiveHandle) notify(method string, params any) error {
	return h.writeJSON(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (h *codexLiveHandle) request(ctx context.Context, method string, params any, out any) error {
	id := strconv.FormatInt(h.nextID.Add(1), 10)
	ch := make(chan codexRPCResponse, 1)
	h.pendingMu.Lock()
	h.pending[id] = ch
	h.pendingMu.Unlock()
	defer func() {
		h.pendingMu.Lock()
		delete(h.pending, id)
		h.pendingMu.Unlock()
	}()
	if err := h.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-ch:
		if resp.err != "" {
			return fmt.Errorf("%s failed: %s", method, resp.err)
		}
		if out != nil && len(resp.result) > 0 {
			return json.Unmarshal(resp.result, out)
		}
		return nil
	}
}

func (h *codexLiveHandle) writeJSON(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	_, err = h.stdin.Write(append(raw, '\n'))
	return err
}

func (h *codexLiveHandle) readStdout() {
	defer h.closeRuntimeStore()
	scanner := bufio.NewScanner(h.stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_ = appendRawLogLine(h.rawLogPath, line)
		h.handleStdoutLine(line)
	}
}

func (h *codexLiveHandle) readStderr() {
	scanner := bufio.NewScanner(h.stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		_ = appendRawLogLine(h.rawLogPath, scanner.Text())
	}
}

func (h *codexLiveHandle) handleStdoutLine(line string) {
	if sessionRef := extractSessionRefFromJSON(line); sessionRef != "" {
		h.threadID = sessionRef
	}
	if messageRef := extractMessageRefFromJSON(line); messageRef != "" {
		h.messageRef = messageRef
	}
	var msg map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	idRaw, hasID := msg["id"]
	methodRaw, hasMethod := msg["method"]
	if hasMethod && hasID {
		h.handleServerRequest(stringValueFromRaw(methodRaw), normalizeRequestID(idRaw), msg["params"])
		return
	}
	if hasID {
		id := normalizeRequestID(idRaw)
		h.pendingMu.Lock()
		ch := h.pending[id]
		delete(h.pending, id)
		h.pendingMu.Unlock()
		if ch != nil {
			if errRaw, ok := msg["error"]; ok {
				ch <- codexRPCResponse{err: decodeRPCError(errRaw)}
			} else {
				ch <- codexRPCResponse{result: msg["result"]}
			}
		}
		return
	}
	if hasMethod {
		h.handleNotification(stringValueFromRaw(methodRaw), msg["params"])
	}
}

func (h *codexLiveHandle) handleServerRequest(method, id string, params json.RawMessage) {
	if dispatch := dispatchCodexExtensionToolRequest(method, params, h.policy.Extensions, h.notePath); dispatch.Handled {
		h.recordExtensionToolDispatch(method, dispatch)
		if dispatch.Error != "" {
			h.writeRPCError(id, -32000, dispatch.Error)
			return
		}
		h.writeRPCResult(id, dynamicToolCallResponse(dispatch.Result))
		return
	}

	switch method {
	case "item/commandExecution/requestApproval":
		decision := h.evaluateCommandApproval(params)
		h.recordApprovalDecision(method, decision)
		h.writeRPCResult(id, appServerApprovalResult(decision, "accept", "reject"))
	case "item/fileChange/requestApproval":
		decision := h.evaluateFileChangeApproval(params)
		h.recordApprovalDecision(method, decision)
		h.writeRPCResult(id, appServerApprovalResult(decision, "accept", "reject"))
	case "item/tool/requestUserInput":
		var payload struct {
			Questions []struct {
				ID string `json:"id"`
			} `json:"questions"`
		}
		answers := map[string]any{}
		if json.Unmarshal(params, &payload) == nil {
			for _, question := range payload.Questions {
				if strings.TrimSpace(question.ID) == "" {
					continue
				}
				answers[question.ID] = map[string]any{"answers": []string{}}
			}
		}
		h.writeRPCResult(id, map[string]any{"answers": answers})
	case "item/permissions/requestApproval":
		decision := h.rejectApproval("permissions", true, "permission approval requests require elevated or human-scoped access; Tusker rejects them instead of granting permissions", approvalSubjectFromParams(params))
		h.recordApprovalDecision(method, decision)
		h.writeRPCError(id, -32000, decision.Reason)
	case "mcpServer/elicitation/request":
		h.writeRPCResult(id, map[string]any{"action": "cancel", "content": nil, "_meta": nil})
	case "applyPatchApproval":
		decision := h.evaluateFileChangeApproval(params)
		h.recordApprovalDecision(method, decision)
		h.writeRPCResult(id, appServerApprovalResult(decision, "approved", "denied"))
	case "execCommandApproval":
		decision := h.evaluateCommandApproval(params)
		h.recordApprovalDecision(method, decision)
		h.writeRPCResult(id, appServerApprovalResult(decision, "approved", "denied"))
	case "account/chatgptAuthTokens/refresh":
		h.writeRPCError(id, -32000, "chatgpt auth token refresh is not available in the Tusker runner")
	default:
		h.writeRPCError(id, -32601, "unsupported Codex app-server request: "+method)
	}
}

type codexApprovalDecision struct {
	RequestType string
	Decision    string
	Reason      string
	Subject     string
	Mutating    bool
}

func (h *codexLiveHandle) evaluateCommandApproval(params json.RawMessage) codexApprovalDecision {
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
	if reason := h.policyDenialReason(mutating); reason != "" {
		return h.rejectApproval("command", mutating, reason, command)
	}
	cwd := approvalCWD(payload, h.workspaceRoot())
	if ok, reason := h.pathAllowed(cwd, h.workspaceRoot()); !ok {
		return h.rejectApproval("command", mutating, "command approval rejected: cwd "+reason, command)
	}
	if commandContainsUnsafeGitMutation(command) {
		return h.rejectApproval("command", mutating, "command approval rejected: unsafe git state mutation is not allowed", command)
	}
	if commandMentionsSecretPath(command) {
		return h.rejectApproval("command", mutating, "command approval rejected: command references a secret path", command)
	}
	return decision
}

func (h *codexLiveHandle) evaluateFileChangeApproval(params json.RawMessage) codexApprovalDecision {
	paths := approvalPaths(params)
	subject := strings.Join(paths, ",")
	if subject == "" {
		subject = approvalSubjectFromParams(params)
	}
	if reason := h.policyDenialReason(true); reason != "" {
		return h.rejectApproval("file_change", true, reason, subject)
	}
	if len(paths) == 0 {
		return h.rejectApproval("file_change", true, "file change approval request did not include changed paths", subject)
	}
	payload := approvalPayload(params)
	cwd := approvalCWD(payload, h.workspaceRoot())
	for _, path := range paths {
		if approvalPathLooksSecret(path) {
			return h.rejectApproval("file_change", true, "file change approval rejected: secret path is not writable", path)
		}
		checkedPath := path
		if !filepath.IsAbs(checkedPath) {
			checkedPath = filepath.Join(cwd, checkedPath)
		}
		if ok, reason := h.pathAllowed(checkedPath, h.workspaceRoot()); !ok {
			return h.rejectApproval("file_change", true, "file change approval rejected: path "+reason, path)
		}
	}
	return codexApprovalDecision{RequestType: "file_change", Decision: "accept", Subject: subject, Mutating: true}
}

func (h *codexLiveHandle) rejectApproval(requestType string, mutating bool, reason, subject string) codexApprovalDecision {
	return codexApprovalDecision{
		RequestType: requestType,
		Decision:    "reject",
		Reason:      reason,
		Subject:     subject,
		Mutating:    mutating,
	}
}

func (h *codexLiveHandle) policyDenialReason(mutating bool) string {
	approvalPolicy := strings.TrimSpace(h.policy.ApprovalPolicy)
	activeSandbox := firstNonEmpty(strings.TrimSpace(h.policy.TurnSandboxPolicy), strings.TrimSpace(h.policy.ThreadSandbox))
	if mutating && activeSandbox == "read-only" {
		return "read-only sandbox rejects mutating approval requests"
	}
	if approvalPolicy == "untrusted" {
		return "approval_policy=" + approvalPolicy + " requires human approval; Tusker rejects instead of silently approving"
	}
	return ""
}

func (h *codexLiveHandle) workspaceRoot() string {
	if h.cmd == nil {
		return ""
	}
	return h.cmd.Dir
}

func (h *codexLiveHandle) pathAllowed(path, workspaceRoot string) (bool, string) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return false, "has no prepared workspace"
	}
	cleanRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false, "has invalid workspace root"
	}
	cleanPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return false, "is invalid"
	}
	cleanRoot = filepath.Clean(cleanRoot)
	cleanPath = filepath.Clean(cleanPath)
	if resolvedRoot, err := filepath.EvalSymlinks(cleanRoot); err == nil {
		cleanRoot = filepath.Clean(resolvedRoot)
	}
	if resolvedPath, err := resolveExistingPath(cleanPath); err == nil {
		cleanPath = filepath.Clean(resolvedPath)
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, "escapes the prepared workspace"
	}
	return true, ""
}

func resolveExistingPath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(path)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolvedParent, filepath.Base(path)), nil
	}
	return "", fmt.Errorf("path does not exist")
}

func appServerApprovalResult(decision codexApprovalDecision, acceptValue, rejectValue string) map[string]any {
	value := acceptValue
	if decision.Decision != "accept" {
		value = rejectValue
	}
	result := map[string]any{"decision": value}
	if decision.Reason != "" {
		result["reason"] = decision.Reason
	}
	return result
}

func approvalPayload(raw json.RawMessage) map[string]any {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return map[string]any{}
	}
	return payload
}

func approvalCWD(payload map[string]any, fallbackCWD string) string {
	if cwd := strings.TrimSpace(stringValue(payload["cwd"])); cwd != "" {
		return cwd
	}
	return fallbackCWD
}

func approvalSubjectFromParams(raw json.RawMessage) string {
	payload := approvalPayload(raw)
	for _, key := range []string{"command", "cmd", "reason", "path", "filePath", "file_path", "cwd"} {
		if value := strings.TrimSpace(stringValue(payload[key])); value != "" {
			return value
		}
	}
	return ""
}

func approvalPaths(raw json.RawMessage) []string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var paths []string
	collectApprovalPaths(value, &paths)
	return uniqueNonEmptyStrings(paths)
}

func collectApprovalPaths(value any, paths *[]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectApprovalPaths(item, paths)
		}
	case map[string]any:
		for key, nested := range typed {
			if approvalPathKey(key) {
				pathValues := stringsFromApprovalValue(nested)
				for _, path := range pathValues {
					*paths = append(*paths, path)
				}
				if len(pathValues) > 0 {
					continue
				}
			}
			collectApprovalPaths(nested, paths)
		}
	}
}

func approvalPathKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "path", "paths", "file", "files", "filepath", "file_path", "absolute_path", "relative_path":
		return true
	default:
		return false
	}
}

func stringsFromApprovalValue(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, stringsFromApprovalValue(item)...)
		}
		return out
	default:
		return nil
	}
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func commandLooksMutating(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}
	if commandContainsUnsafeGitMutation(lower) {
		return true
	}
	if strings.Contains(lower, ">>") || strings.Contains(lower, " >") || strings.Contains(lower, "| tee") || strings.Contains(lower, " tee ") {
		return true
	}
	for _, marker := range []string{
		"touch ", "rm ", "rm -", "mv ", "cp ", "mkdir ", "rmdir ", "chmod ", "chown ", "ln -",
		"apply_patch", "sed -i", "perl -pi", "go mod tidy", "npm install", "npm update", "pnpm install",
		"yarn add", "yarn install", "bun add", "cargo update", "pip install",
		".write(", "write_text(", "create(", "open(",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func commandContainsUnsafeGitMutation(command string) bool {
	lower := strings.ToLower(command)
	for _, marker := range []string{
		"git add", "git am", "git apply", "git branch", "git checkout", "git cherry-pick", "git clean",
		"git commit", "git merge", "git mv", "git pull", "git push", "git rebase", "git reset",
		"git restore", "git rm", "git stash", "git switch", "git update-index",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func commandMentionsSecretPath(command string) bool {
	for _, field := range strings.Fields(command) {
		if approvalPathLooksSecret(strings.Trim(field, `"'`)) {
			return true
		}
	}
	return false
}

func approvalPathLooksSecret(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if normalized == "" {
		return false
	}
	base := filepath.Base(normalized)
	if strings.HasPrefix(base, ".env") || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	switch base {
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "credentials", "credentials.json":
		return true
	}
	for _, segment := range []string{"/.ssh/", "/secrets/", "/secret/"} {
		if strings.Contains(normalized, segment) {
			return true
		}
	}
	return false
}

func stringListFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(stringValue(item)); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func (h *codexLiveHandle) recordApprovalDecision(method string, decision codexApprovalDecision) {
	reason := strings.TrimSpace(decision.Reason)
	message := fmt.Sprintf("codex approval %s: method=%s type=%s", decision.Decision, method, decision.RequestType)
	if reason != "" {
		message += " reason=" + reason
	}
	_ = appendRawLogLine(h.rawLogPath, message)
	if h.eventLog == nil {
		return
	}
	payload := map[string]any{
		"project_id":          h.projectID,
		"record_id":           h.recordID,
		"item_id":             h.itemID,
		"attempt_id":          h.attemptID,
		"session_ref":         h.threadID,
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
	}
	_ = h.eventLog.Append("codex_approval_decision", h.attemptID, h.runner, payload)
}

func (h *codexLiveHandle) writeRPCResult(id string, result any) {
	_ = h.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (h *codexLiveHandle) writeRPCError(id string, code int, message string) {
	_ = h.writeJSON(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

type codexExtensionToolDispatch struct {
	Handled  bool
	ToolName string
	Result   any
	Error    string
	Denied   bool
}

func dispatchCodexExtensionToolRequest(method string, params json.RawMessage, policy ExtensionPolicy, notePath string) codexExtensionToolDispatch {
	if !isCodexExtensionToolCallMethod(method) {
		return codexExtensionToolDispatch{}
	}
	toolName := extractCodexExtensionToolName(params)
	if toolName == "" {
		return codexExtensionToolDispatch{Handled: true, Denied: true, Error: "extension tool request denied: missing tool name"}
	}
	dispatch := codexExtensionToolDispatch{Handled: true, ToolName: toolName}
	if !policy.Enabled {
		dispatch.Denied = true
		dispatch.Error = fmt.Sprintf("extension tool %s denied: extensions are disabled", toolName)
		return dispatch
	}
	if !extensionListAllows(policy.AllowedTools, toolName) {
		dispatch.Denied = true
		dispatch.Error = fmt.Sprintf("extension tool %s denied: not allowed by workflow", toolName)
		return dispatch
	}
	switch toolName {
	case "tusker.show_current":
		if !policy.AllowTuskerReadTools {
			dispatch.Denied = true
			dispatch.Error = "extension tool tusker.show_current denied: tusker read tools are not enabled"
			return dispatch
		}
		result, err := tuskerShowCurrentToolResult(notePath)
		if err != nil {
			dispatch.Denied = true
			dispatch.Error = "extension tool tusker.show_current failed: " + err.Error()
			return dispatch
		}
		dispatch.Result = result
		return dispatch
	default:
		dispatch.Denied = true
		dispatch.Error = fmt.Sprintf("extension tool %s denied: unsupported tool", toolName)
		return dispatch
	}
}

func isCodexExtensionToolCallMethod(method string) bool {
	normalized := strings.ToLower(strings.TrimSpace(method))
	if normalized == "" || strings.Contains(normalized, "requestuserinput") {
		return false
	}
	switch normalized {
	case "tool/call", "tools/call", "item/tool/call", "item/tools/call", "item/tool/execute", "item/tools/execute":
		return true
	default:
		return (strings.Contains(normalized, "/tool/") || strings.Contains(normalized, "/tools/")) &&
			(strings.HasSuffix(normalized, "/call") || strings.HasSuffix(normalized, "/execute"))
	}
}

func extractCodexExtensionToolName(raw json.RawMessage) string {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return extractToolNameFromValue(value)
}

func extractToolNameFromValue(value any) string {
	payload, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"name", "toolName", "tool_name", "serverToolName", "server_tool_name"} {
		if candidate := strings.TrimSpace(stringValue(payload[key])); candidate != "" {
			return candidate
		}
	}
	for _, key := range []string{"tool", "call", "toolCall", "tool_call"} {
		switch nested := payload[key].(type) {
		case string:
			if candidate := strings.TrimSpace(nested); candidate != "" {
				return candidate
			}
		case map[string]any:
			if candidate := extractToolNameFromValue(nested); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func tuskerShowCurrentToolResult(notePath string) (map[string]any, error) {
	if strings.TrimSpace(notePath) == "" {
		return nil, fmt.Errorf("TUSKER_NOTE_PATH is empty")
	}
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		return nil, err
	}
	item := map[string]any{
		"id":            stringField(data, "id"),
		"record_id":     stringField(data, "record_id"),
		"title":         stringField(data, "title"),
		"type":          stringField(data, "type"),
		"status":        stringField(data, "status"),
		"work_revision": intField(data, "work_revision"),
		"summary":       firstNonEmpty(stringField(data, "summary"), noteBodyExcerpt(body, 800)),
		"note_path":     notePath,
	}
	result := map[string]any{
		"name":   "tusker.show_current",
		"output": item,
	}
	if encoded, err := json.Marshal(item); err == nil {
		result["content"] = []map[string]any{{"type": "text", "text": string(encoded)}}
	}
	return result, nil
}

func dynamicToolCallResponse(result any) map[string]any {
	if payload, ok := result.(map[string]any); ok {
		if output, ok := payload["output"]; ok {
			result = output
		}
	}
	text := ""
	if result != nil {
		if raw, err := json.Marshal(result); err == nil {
			text = string(raw)
		} else {
			text = fmt.Sprint(result)
		}
	}
	return map[string]any{
		"success":      true,
		"contentItems": []map[string]any{{"type": "inputText", "text": text}},
	}
}

func noteBodyExcerpt(body string, maxLen int) string {
	body = strings.TrimSpace(body)
	if body == "" || maxLen <= 0 {
		return ""
	}
	lines := strings.Split(body, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kept = append(kept, line)
	}
	excerpt := strings.TrimSpace(strings.Join(kept, "\n"))
	if len(excerpt) <= maxLen {
		return excerpt
	}
	return strings.TrimSpace(excerpt[:maxLen])
}

func (h *codexLiveHandle) recordExtensionToolDispatch(method string, dispatch codexExtensionToolDispatch) {
	outcome := "result"
	if dispatch.Denied || dispatch.Error != "" {
		outcome = "denied"
	}
	message := fmt.Sprintf("extension tool %s: method=%s tool=%s", outcome, method, dispatch.ToolName)
	if dispatch.Error != "" {
		message += " reason=" + dispatch.Error
	}
	_ = appendRawLogLine(h.rawLogPath, message)
	if h.eventLog == nil {
		return
	}
	_ = h.eventLog.Append("extension_tool_"+outcome, h.attemptID, h.runner, map[string]any{
		"project_id": h.projectID,
		"record_id":  h.recordID,
		"item_id":    h.itemID,
		"attempt_id": h.attemptID,
		"method":     method,
		"tool":       dispatch.ToolName,
		"reason":     dispatch.Error,
	})
}

func (h *codexLiveHandle) handleNotification(method string, params json.RawMessage) {
	if usage := extractUsageCounters(params); usage.hasAny() {
		h.recordTurnUsage(method, time.Now().UTC().Format(time.RFC3339), usage)
	}
	switch method {
	case "turn/started":
		var payload struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &payload) == nil {
			if payload.ThreadID != "" {
				h.threadID = payload.ThreadID
			}
			if payload.Turn.ID != "" {
				h.turnID = payload.Turn.ID
			}
			h.recordTurnStarted(payload.Turn.ID, time.Now().UTC().Format(time.RFC3339), map[string]any{"source": method})
		}
	case "turn/completed":
		var payload struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &payload) == nil {
			reason := ""
			if payload.Turn.Error != nil {
				reason = payload.Turn.Error.Message
			}
			now := time.Now().UTC().Format(time.RFC3339)
			status := normalizedTurnStatus(payload.Turn.Status)
			h.recordTurnCompleted(payload.Turn.ID, status, reason, now)
			switch payload.Turn.Status {
			case "completed":
				go h.continueOrFinalize()
			case "interrupted":
				h.finalize(130)
			default:
				_ = appendRawLogLine(h.rawLogPath, reason)
				h.finalize(1)
			}
		}
	}
}

func (h *codexLiveHandle) continueOrFinalize() {
	if !h.shouldContinueTurns() {
		if h.policy.MaxTurns > 1 && h.turnIndex+1 >= h.policy.MaxTurns {
			_ = appendRawLogLine(h.rawLogPath, fmt.Sprintf("max turns reached for attempt %s: completed %d/%d turns", h.attemptID, h.turnIndex+1, h.policy.MaxTurns))
		}
		h.finalize(0)
		return
	}
	h.writeMu.Lock()
	h.turnID = ""
	h.turnIndex = -1
	h.writeMu.Unlock()
	turnID, err := h.turnStart(h.threadID, h.continuationPrompt())
	if err != nil {
		_ = appendRawLogLine(h.rawLogPath, "continuation turn failed: "+err.Error())
		h.finalize(1)
		return
	}
	h.turnID = turnID
}

func (h *codexLiveHandle) shouldContinueTurns() bool {
	if h.interrupted.Load() || h.policy.MaxTurns <= 1 {
		return false
	}
	if h.turnIndex+1 >= h.policy.MaxTurns {
		return false
	}
	if strings.TrimSpace(h.notePath) == "" || len(h.activeStates) == 0 {
		return false
	}
	data, _, err := parseFrontmatterMustRead(h.notePath)
	if err != nil {
		_ = appendRawLogLine(h.rawLogPath, "continuation state check failed: "+err.Error())
		return false
	}
	return containsString(h.activeStates, stringField(data, "status"))
}

func (h *codexLiveHandle) continuationPrompt() string {
	return strings.TrimSpace(fmt.Sprintf(`The tracker item is still in an active state.

Continue on the same Codex thread. Do not re-plan from scratch unless the current proof changes the prior plan.

Current item:
- ID: %s
- Record: %s
- Attempt: %s
- Completed turns in this attempt: %d

Re-read the note and workspace, satisfy the task proof mode, and continue until the item is ready for review or blocked. When it is ready, use tusker finish or move/propose the task to review; attempt handoff alone is not the review queue.`, h.itemID, h.recordID, h.attemptID, h.turnIndex+1)) + "\n"
}

func (h *codexLiveHandle) recordTurnStarted(turnID, at string, payload map[string]any) {
	turnID = firstNonEmpty(strings.TrimSpace(turnID), h.turnID)
	if turnID == "" {
		return
	}
	h.turnID = turnID
	h.ensureTurnIndex()
	h.appendNormalizedTurnEvent("turn_started", at, payloadWithTurn(h, turnID, payload))
	h.saveTurn(RunTurn{
		AttemptID:       h.attemptID,
		ProjectID:       h.projectID,
		RecordID:        h.recordID,
		TurnID:          turnID,
		TurnIndex:       h.turnIndex,
		SessionRef:      h.threadID,
		Status:          "running",
		StartedAt:       at,
		LastEventAt:     at,
		LeaseGeneration: h.leaseGeneration,
	})
}

func (h *codexLiveHandle) recordTurnUsage(source, at string, usage turnUsageCounters) {
	turnID := firstNonEmpty(usage.turnID, h.turnID)
	if turnID == "" {
		return
	}
	h.turnID = turnID
	h.ensureTurnIndex()
	h.appendNormalizedTurnEvent("turn_usage_updated", at, payloadWithTurn(h, turnID, map[string]any{
		"source":        source,
		"input_tokens":  usage.inputTokens,
		"output_tokens": usage.outputTokens,
		"total_tokens":  usage.totalTokens,
	}))
	h.saveTurn(RunTurn{
		AttemptID:       h.attemptID,
		ProjectID:       h.projectID,
		RecordID:        h.recordID,
		TurnID:          turnID,
		TurnIndex:       h.turnIndex,
		SessionRef:      h.threadID,
		Status:          "running",
		InputTokens:     usage.inputTokens,
		OutputTokens:    usage.outputTokens,
		TotalTokens:     usage.totalTokens,
		LastEventAt:     at,
		LeaseGeneration: h.leaseGeneration,
	})
}

func (h *codexLiveHandle) recordTurnCompleted(turnID, status, reason, at string) {
	turnID = firstNonEmpty(strings.TrimSpace(turnID), h.turnID)
	if turnID == "" {
		return
	}
	h.turnID = turnID
	h.ensureTurnIndex()
	h.appendNormalizedTurnEvent("turn_completed", at, payloadWithTurn(h, turnID, map[string]any{
		"status":     status,
		"last_error": reason,
	}))
	h.saveTurn(RunTurn{
		AttemptID:       h.attemptID,
		ProjectID:       h.projectID,
		RecordID:        h.recordID,
		TurnID:          turnID,
		TurnIndex:       h.turnIndex,
		SessionRef:      h.threadID,
		Status:          status,
		CompletedAt:     at,
		LastEventAt:     at,
		LastError:       reason,
		LeaseGeneration: h.leaseGeneration,
	})
}

func (h *codexLiveHandle) appendNormalizedTurnEvent(kind, at string, payload map[string]any) {
	if h.eventLog == nil || strings.TrimSpace(h.eventSinkPath) == "" {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["normalized_at"] = at
	_ = h.eventLog.Append(kind, h.attemptID, h.runner, payload)
}

func (h *codexLiveHandle) saveTurn(turn RunTurn) {
	if h.runtimeStore == nil {
		return
	}
	_ = h.runtimeStore.SaveTurn(turn)
}

func (h *codexLiveHandle) ensureTurnIndex() {
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

func payloadWithTurn(h *codexLiveHandle, turnID string, payload map[string]any) map[string]any {
	out := map[string]any{
		"project_id":  h.projectID,
		"record_id":   h.recordID,
		"item_id":     h.itemID,
		"attempt_id":  h.attemptID,
		"session_ref": h.threadID,
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

func normalizedTurnStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "completed":
		return "completed"
	case "interrupted":
		return "interrupted"
	case "":
		return "unknown"
	default:
		return "failed"
	}
}

func (h *codexLiveHandle) finalize(exitCode int) {
	h.doneOnce.Do(func() {
		_ = writeRunnerStatusFile(h.statusPath, exitCode)
		liveRegistry.Unregister(h.attemptID)
	})
}

func (h *codexLiveHandle) closeRuntimeStore() {
	if h.runtimeStore != nil {
		_ = h.runtimeStore.Close()
		h.runtimeStore = nil
	}
}

func (h *codexLiveHandle) waitForExit() {
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

func normalizeRequestID(raw json.RawMessage) string {
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return asString
	}
	var asInt int64
	if json.Unmarshal(raw, &asInt) == nil {
		return strconv.FormatInt(asInt, 10)
	}
	return strings.TrimSpace(string(raw))
}

func decodeRPCError(raw json.RawMessage) string {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) == nil {
		if msg := strings.TrimSpace(stringValue(payload["message"])); msg != "" {
			return msg
		}
	}
	return strings.TrimSpace(string(raw))
}

func stringValueFromRaw(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return ""
}

type turnUsageCounters struct {
	inputTokens  int
	outputTokens int
	totalTokens  int
	turnID       string
}

func (u turnUsageCounters) hasAny() bool {
	return u.inputTokens > 0 || u.outputTokens > 0 || u.totalTokens > 0
}

func extractUsageCounters(raw json.RawMessage) turnUsageCounters {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return turnUsageCounters{}
	}
	var usage turnUsageCounters
	collectUsageCounters(value, &usage)
	if usage.totalTokens == 0 && (usage.inputTokens > 0 || usage.outputTokens > 0) {
		usage.totalTokens = usage.inputTokens + usage.outputTokens
	}
	return usage
}

func collectUsageCounters(value any, usage *turnUsageCounters) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			switch normalizeUsageKey(key) {
			case "turnid":
				if usage.turnID == "" {
					usage.turnID = strings.TrimSpace(stringValue(nested))
				}
			case "inputtokens", "inputtoken", "prompttokens", "prompttoken":
				usage.inputTokens = maxInt(usage.inputTokens, intValue(nested))
			case "outputtokens", "outputtoken", "completiontokens", "completiontoken":
				usage.outputTokens = maxInt(usage.outputTokens, intValue(nested))
			case "totaltokens", "totaltoken":
				usage.totalTokens = maxInt(usage.totalTokens, intValue(nested))
			}
			collectUsageCounters(nested, usage)
		}
	case []any:
		for _, nested := range typed {
			collectUsageCounters(nested, usage)
		}
	}
}

func normalizeUsageKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer("_", "", "-", "", ".", "")
	return replacer.Replace(key)
}

func appendRawLogLine(path, line string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(line + "\n")
	return err
}

func writeRunnerStatusFile(path string, exitCode int) error {
	payload := runnerProcessStatus{
		ExitCode:    exitCode,
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeText(path, string(raw)+"\n")
}
