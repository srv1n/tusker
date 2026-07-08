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
	ProjectID       string
	RecordID        string
	ItemID          string
	AttemptID       string
	Lane            string
	WorkRevision    int
	LeaseGeneration int
	SessionRef      string
	MessageRef      string
	WorkingDir      string
	WorkspacePath   string
	RepoRoot        string
	PromptPath      string
	EventSinkPath   string
	RawLogPath      string
	StatusPath      string
	Command         string
	RunnerProfile   string
	RunnerHarness   string
	RunnerModel     string
	RunnerEffort    string
	NotePath        string
	VaultPath       string
	ResumeMode      bool
	CodexPolicy     CodexPolicy
	ExternalLoop    ExternalLoopLaunchContext
}

type runnerProcessStatus struct {
	ExitCode    int    `json:"exit_code"`
	CompletedAt string `json:"completed_at"`
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
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, tuskerError(errorConfigInvalid, fmt.Sprintf("%s runner command is empty", runner))
	}
	if strings.Contains(command, "app-server") {
		return nil, tuskerError(errorConfigInvalid, fmt.Sprintf("%s command %q is app-server mode; the current daemon runner needs a detached CLI command", runner, command))
	}
	if err := ensureDir(filepath.Dir(req.RawLogPath)); err != nil {
		return nil, err
	}
	if err := ensureDir(filepath.Dir(req.StatusPath)); err != nil {
		return nil, err
	}
	workspaceCWD, err := runnerWorkspaceCWD(runner, req.WorkspacePath)
	if err != nil {
		return nil, err
	}

	expanded := replaceTemplateTokens(command, map[string]string{
		"{{workspace_path}}":  workspaceCWD,
		"{{prompt_path}}":     req.PromptPath,
		"{{event_sink_path}}": req.EventSinkPath,
		"{{raw_log_path}}":    req.RawLogPath,
		"{{status_path}}":     req.StatusPath,
		"{{note_path}}":       req.NotePath,
		"{{vault_path}}":      req.VaultPath,
		"{{session_ref}}":     req.SessionRef,
		"{{message_ref}}":     req.MessageRef,
	})
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
`, expanded)

	eventLog := NewEventLog(req.EventSinkPath)
	_ = eventLog.Append("attempt_started", req.AttemptID, runner, map[string]any{
		"command":     command,
		"item_id":     req.ItemID,
		"resume_mode": req.ResumeMode,
		"session_ref": req.SessionRef,
	})

	cmd := exec.CommandContext(ctx, "sh", "-lc", script)
	cmd.Dir = workspaceCWD
	if err := assertRunnerCommandDir(runner, cmd.Dir, req.WorkspacePath); err != nil {
		return nil, err
	}
	cmd.Env = runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkspacePath: workspaceCWD, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		NotePath: req.NotePath, VaultPath: req.VaultPath, SessionRef: req.SessionRef, MessageRef: req.MessageRef,
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		CodexPolicy:  withDefaultCodexPolicy(req.CodexPolicy),
		ExternalLoop: req.ExternalLoop,
	})
	closeStdin := attachDevNullStdin(cmd)
	defer closeStdin()
	if file, err := os.OpenFile(req.RawLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		defer file.Close()
		cmd.Stdout = file
		cmd.Stderr = file
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	pgid := processGroupID(pid)
	processStartedAt := recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))
	_ = cmd.Process.Release()

	result := &StartResult{
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
	}
	_ = eventLog.Append("attempt_spawned", req.AttemptID, runner, map[string]any{
		"pid": pid, "status_path": req.StatusPath,
	})
	return result, nil
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
		for _, nested := range current {
			if candidate := findSessionRef(nested); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, nested := range current {
			if candidate := findSessionRef(nested); candidate != "" {
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
