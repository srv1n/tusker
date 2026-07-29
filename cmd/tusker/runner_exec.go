package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type runnerExecRequest struct {
	ProjectID           string
	RecordID            string
	ItemID              string
	AttemptID           string
	Lane                string
	WorkRevision        int
	LeaseGeneration     int
	SessionRef          string
	MessageRef          string
	WorkingDir          string
	WorkspacePath       string
	RepoRoot            string
	PromptPath          string
	EventSinkPath       string
	RawLogPath          string
	RawLogMaxBytes      int64
	StatusPath          string
	RunnerPathPrefix    string
	Command             string
	CommandArgv         []string
	CommandExecutableFP string
	CommandSearchPath   string
	RunnerProfile       string
	RunnerHarness       string
	RunnerModel         string
	RunnerEffort        string
	NotePath            string
	VaultPath           string
	ResumeMode          bool
	CodexPolicy         CodexPolicy
	ExternalLoop        ExternalLoopLaunchContext
	ContainmentPGID     int
}

type runnerProcessStatus struct {
	ExitCode    int    `json:"exit_code"`
	CompletedAt string `json:"completed_at"`
	Outcome     string `json:"outcome,omitempty"`
	Reason      string `json:"reason,omitempty"`
	TurnsUsed   int    `json:"turns_used,omitempty"`
}

type runnerEventLog interface {
	Append(kind string, attemptID string, runner RunnerName, payload map[string]any) error
}

func attachDevNullStdin(cmd *exec.Cmd) func() {
	if cmd == nil {
		return func() {}
	}
	file, err := os.Open(os.DevNull)
	if err != nil {
		cmd.Stdin = nil
		return func() {}
	}
	cmd.Stdin = file
	return func() { _ = file.Close() }
}

func executeRunnerCommand(ctx context.Context, runner RunnerName, req runnerExecRequest, capabilities RunnerCapabilities) (*StartResult, error) {
	return executeRunnerCommandWithEventLog(ctx, runner, req, capabilities, NewEventLog(req.EventSinkPath))
}

func executeRunnerCommandWithEventLog(ctx context.Context, runner RunnerName, req runnerExecRequest, capabilities RunnerCapabilities, eventLog runnerEventLog) (*StartResult, error) {
	command := strings.TrimSpace(req.Command)
	if err := validateRunnerCommandShape(command, req.CommandArgv); err != nil {
		return nil, tuskerError(errorConfigInvalid, fmt.Sprintf("%s %s", runner, err))
	}
	if err := ensureDir(filepath.Dir(req.RawLogPath)); err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Dir(req.StatusPath)); err != nil {
		return nil, err
	}
	if req.RawLogMaxBytes < 0 {
		return nil, tuskerError(errorConfigInvalid, "runner raw-log byte limit cannot be negative")
	}
	if strings.TrimSpace(req.CommandExecutableFP) != "" && req.RawLogMaxBytes != completionAuthoritativeRawLogMaxBytes {
		return nil, tuskerError(errorConfigInvalid, fmt.Sprintf(
			"completion authority requires the exact %d-byte raw-log policy",
			completionAuthoritativeRawLogMaxBytes,
		))
	}
	boundedRawLog := req.RawLogMaxBytes > 0
	if boundedRawLog {
		if err := os.Remove(req.StatusPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove stale runner status before bounded launch: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	workspaceCWD, err := runnerWorkspaceCWD(runner, req.WorkspacePath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.CommandExecutableFP) != "" {
		if len(req.CommandArgv) == 0 || !filepath.IsAbs(req.CommandArgv[0]) {
			return nil, tuskerError(errorConfigInvalid, "completion authority requires an absolute command argv executable")
		}
		if pathWithin(workspaceCWD, req.CommandArgv[0]) || (strings.TrimSpace(req.RepoRoot) != "" && pathWithin(req.RepoRoot, req.CommandArgv[0])) {
			return nil, tuskerError(errorConfigInvalid, "completion authority refuses an executable from the worker workspace or repository")
		}
		if err := completionVerifyExecutableIdentity(req.CommandArgv[0], req.CommandExecutableFP, req.CommandSearchPath); err != nil {
			return nil, tuskerError(errorConfigInvalid, err.Error())
		}
	}

	tokens := map[string]string{
		"{{workspace_path}}":  workspaceCWD,
		"{{prompt_path}}":     req.PromptPath,
		"{{event_sink_path}}": req.EventSinkPath,
		"{{raw_log_path}}":    req.RawLogPath,
		"{{status_path}}":     req.StatusPath,
		"{{note_path}}":       req.NotePath,
		"{{vault_path}}":      runnerWorkspaceVaultPath(workspaceCWD, req.VaultPath),
		"{{session_ref}}":     req.SessionRef,
		"{{message_ref}}":     req.MessageRef,
	}
	expanded := replaceTemplateTokens(command, tokens)
	expanded = runnerCommandWithPathPrefix(expanded, req.RunnerPathPrefix)
	scriptCommand := expanded
	commandArgs := []string{"tusker-runner"}
	shellExecutable := "sh"
	shellFlag := "-lc"
	if len(req.CommandArgv) > 0 {
		scriptCommand = `"$@"`
		commandArgs = append(commandArgs, replaceTemplateArgv(req.CommandArgv, tokens)...)
		// Structured argv is already fully resolved by trusted Go code. A fixed
		// non-login shell only supplies status-file plumbing; it cannot source
		// repository or operator shell startup files and cannot reparse argv.
		shellExecutable = "/bin/sh"
		shellFlag = "-c"
	}
	script := fmt.Sprintf(`rm -f "$TUSKER_STATUS_PATH"
( %s ) < "$TUSKER_PROMPT_PATH" >> "$TUSKER_RAW_LOG" 2>&1
code=$?
python3 - "$TUSKER_STATUS_PATH" "$code" <<'PY'
import json,sys,datetime
path=sys.argv[1]
code=int(sys.argv[2])
payload={"exit_code":code,"completed_at":datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00","Z")}
open(path,"w",encoding="utf-8").write(json.dumps(payload)+"\n")
PY
`, scriptCommand)
	if boundedRawLog {
		// The trusted parent owns both output streams and the only writable raw
		// log descriptor. In particular, the fixed shell does not redirect an
		// authoritative worker around the byte-budget writer.
		script = fmt.Sprintf(`( %s ) < "$TUSKER_PROMPT_PATH"
`, scriptCommand)
	}

	startedPayload := map[string]any{
		"command": command, "item_id": req.ItemID,
		"resume_mode": req.ResumeMode, "session_ref": req.SessionRef,
	}
	if boundedRawLog {
		startedPayload["raw_log_max_bytes"] = req.RawLogMaxBytes
		startedPayload["raw_log_overflow"] = "kill_process_group"
	}
	if err := eventLog.Append("attempt_started", req.AttemptID, runner, startedPayload); err != nil {
		return nil, fmt.Errorf("record %s attempt_started event: %w", runner, err)
	}

	cmdArgs := append([]string{shellFlag, script}, commandArgs...)
	var cmd *exec.Cmd
	if boundedRawLog {
		cmd = exec.Command(shellExecutable, cmdArgs...)
		cmd.WaitDelay = boundedRunnerWaitDelay
	} else {
		cmd = exec.CommandContext(ctx, shellExecutable, cmdArgs...)
	}
	cmd.Dir = workspaceCWD
	if err := assertRunnerCommandDir(runner, cmd.Dir, req.WorkspacePath); err != nil {
		return nil, err
	}
	cmd.Env = runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkspacePath: workspaceCWD, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		RunnerPathPrefix:  req.RunnerPathPrefix,
		CommandSearchPath: req.CommandSearchPath,
		NotePath:          req.NotePath, VaultPath: req.VaultPath, SessionRef: req.SessionRef, MessageRef: req.MessageRef,
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		CodexPolicy:  withDefaultCodexPolicy(req.CodexPolicy),
		ExternalLoop: req.ExternalLoop,
	})
	closeStdin := attachDevNullStdin(cmd)
	defer closeStdin()
	var authoritativeLog *boundedRawLogWriter
	if boundedRawLog {
		authoritativeLog, err = openBoundedRawLog(req.RawLogPath, req.RawLogMaxBytes, req.ResumeMode)
		if err != nil {
			return nil, fmt.Errorf("open completion-authoritative raw log: %w", err)
		}
		cmd.Stdout = authoritativeLog
		cmd.Stderr = authoritativeLog
	} else if file, openErr := os.OpenFile(req.RawLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); openErr == nil {
		defer file.Close()
		cmd.Stdout = file
		cmd.Stderr = file
	}
	if req.ContainmentPGID <= 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		if authoritativeLog != nil {
			_ = authoritativeLog.close()
		}
		return nil, err
	}
	pid := cmd.Process.Pid
	pgid := processGroupID(pid)
	if req.ContainmentPGID > 0 && pgid != req.ContainmentPGID {
		_ = cmd.Process.Kill()
		return nil, tuskerError(errorInvalidTransition, fmt.Sprintf("runner child escaped wrapper containment: pid=%d pgid=%d expected=%d", pid, pgid, req.ContainmentPGID))
	}
	processStartedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	spawnedPayload := map[string]any{"pid": pid, "status_path": req.StatusPath}
	if boundedRawLog {
		spawnedPayload["raw_log_max_bytes"] = req.RawLogMaxBytes
	}
	if err := eventLog.Append("attempt_spawned", req.AttemptID, runner, spawnedPayload); err != nil {
		terminateAndReapRunnerCommand(cmd, pgid)
		if authoritativeLog != nil {
			_ = authoritativeLog.close()
		}
		return nil, fmt.Errorf("record %s attempt_spawned event for pid %d; launched process was terminated: %w", runner, pid, err)
	}
	if authoritativeLog != nil {
		authoritativeLog.bindTerminator(func() { killContainedRunnerCommand(cmd, pgid, req.ContainmentPGID > 0) })
		go monitorBoundedRunnerCommand(ctx, cmd, pgid, req.ContainmentPGID > 0, authoritativeLog, req.StatusPath)
	} else {
		_ = cmd.Process.Release()
	}

	return &StartResult{
		SessionRef:   firstNonEmpty(req.SessionRef, extractSessionRef(req.RawLogPath)),
		MessageRef:   extractMessageRef(req.RawLogPath),
		StartedAt:    processStartedAt,
		PID:          pid,
		PGID:         pgid,
		ProcessStart: processStartedAt,
		StatusPath:   req.StatusPath,
		Capabilities: capabilities,
		Completed:    false,
		Outcome:      AttemptOutcomeNone,
	}, nil
}

func replaceTemplateArgv(argv []string, replacements map[string]string) []string {
	out := make([]string, len(argv))
	for i, arg := range argv {
		out[i] = replaceTemplateTokens(arg, replacements)
	}
	return out
}

func terminateAndReapRunnerCommand(cmd *exec.Cmd, pgid int) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if pgid > 0 {
		if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
			_ = cmd.Process.Kill()
		}
	} else {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

func readRunnerProcessStatus(statusPath string) (runnerProcessStatus, error) {
	text, err := readText(statusPath)
	if err != nil {
		return runnerProcessStatus{}, err
	}
	var status runnerProcessStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &status); err != nil {
		return runnerProcessStatus{}, err
	}
	return status, nil
}

func extractSessionRef(rawLogPath string) string {
	return extractFirstRef(rawLogPath, extractSessionRefFromJSON)
}

func extractMessageRef(rawLogPath string) string {
	return extractFirstRef(rawLogPath, extractMessageRefFromJSON)
}

func extractFirstRef(rawLogPath string, extractor func(string) string) string {
	text, err := readText(rawLogPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if ref := extractor(line); ref != "" {
			return ref
		}
	}
	return ""
}

func extractSessionRefFromJSON(line string) string {
	var payload any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return ""
	}
	return findSessionRef(payload)
}

func extractMessageRefFromJSON(line string) string {
	var payload any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return ""
	}
	return findMessageRef(payload)
}

func findSessionRef(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"session_id", "sessionId", "thread_id", "threadId"} {
			if candidate := strings.TrimSpace(stringValue(current[key])); candidate != "" {
				return candidate
			}
		}
		if thread, ok := current["thread"].(map[string]any); ok {
			if candidate := strings.TrimSpace(stringValue(thread["id"])); candidate != "" {
				return candidate
			}
		}
		if session, ok := current["session"].(map[string]any); ok {
			if candidate := strings.TrimSpace(stringValue(session["id"])); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func findMessageRef(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"message_id", "messageId", "uuid", "id"} {
			if candidate := strings.TrimSpace(stringValue(current[key])); candidate != "" && looksLikeMessageRef(candidate) {
				return candidate
			}
		}
		if message, ok := current["message"].(map[string]any); ok {
			if candidate := strings.TrimSpace(stringValue(message["id"])); candidate != "" {
				return candidate
			}
			for _, nested := range message {
				if candidate := findMessageRef(nested); candidate != "" {
					return candidate
				}
			}
		}
		for _, nested := range current {
			if candidate := findMessageRef(nested); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, nested := range current {
			if candidate := findMessageRef(nested); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func looksLikeMessageRef(value string) bool {
	if value == "" {
		return false
	}
	if strings.Contains(value, "-") && len(value) >= 8 {
		return true
	}
	return strings.HasPrefix(value, "msg_")
}
