package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultCodexCloudStartCommand   = "codex cloud start --json --environment {{environment_id}} --apply-mode {{apply_mode}} --pr-mode {{pr_mode}}"
	defaultCodexCloudStatusCommand  = "codex cloud status --json {{cloud_task_id}}"
	defaultCodexCloudCollectCommand = "codex cloud status --json {{cloud_task_id}}"
)

type CodexCloudRunner struct {
	Config   CodexCloudConfig
	Executor codexCloudExecutor
}

type codexCloudExecutor interface {
	RunCodexCloud(ctx context.Context, req codexCloudExecRequest) ([]byte, error)
}

type codexCloudExecutorFunc func(context.Context, codexCloudExecRequest) ([]byte, error)

func (f codexCloudExecutorFunc) RunCodexCloud(ctx context.Context, req codexCloudExecRequest) ([]byte, error) {
	return f(ctx, req)
}

type codexCloudExecRequest struct {
	Command     string
	Stdin       string
	Dir         string
	Env         []string
	CloudTaskID string
}

type shellCodexCloudExecutor struct{}

func (shellCodexCloudExecutor) RunCodexCloud(ctx context.Context, req codexCloudExecRequest) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "sh", "-lc", req.Command)
	cmd.Dir = req.Dir
	cmd.Env = req.Env
	cmd.Stdin = strings.NewReader(req.Stdin)
	return cmd.CombinedOutput()
}

type codexCloudSnapshot struct {
	TaskID         string
	Status         string
	EnvironmentID  string
	AttemptNumber  int
	PullRequestURL string
	ApplyRef       string
	LogsSummary    string
	FinalSummary   string
}

func (r *CodexCloudRunner) Name() RunnerName { return RunnerCodexCloud }

func (r *CodexCloudRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, MachineFinalStatus: true, ArtifactEnumeration: true}
}

func (r *CodexCloudRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	config := withDefaultCodexCloudConfig(r.Config)
	if command := strings.TrimSpace(req.Command); command != "" {
		config.Command = command
	}
	if err := validateCodexCloudRuntimeConfig(config); err != nil {
		return nil, err
	}
	prompt, err := readText(req.PromptPath)
	if err != nil {
		return nil, err
	}
	workspaceCWD, err := runnerWorkspaceCWD(r.Name(), req.WorkspacePath)
	if err != nil {
		return nil, err
	}
	eventLog := NewEventLog(req.EventSinkPath)
	if err := eventLog.Validate(); err != nil {
		return nil, fmt.Errorf("preflight codex cloud event sink: %w", err)
	}
	if err := preflightCodexCloudRawLog(req.RawLogPath); err != nil {
		return nil, fmt.Errorf("preflight codex cloud raw log: %w", err)
	}
	command := codexCloudCommand(config.Command, config, req, "")
	output, launchErr := r.executor().RunCodexCloud(ctx, codexCloudExecRequest{
		Command: command,
		Stdin:   prompt,
		Dir:     workspaceCWD,
		Env:     codexCloudEnv(req, config, ""),
	})
	rawLogErr := appendCodexCloudRawLog(req.RawLogPath, output)
	snapshot, parseErr := parseCodexCloudOutput(output)
	if parseErr != nil {
		return nil, codexCloudLaunchError(req.RawLogPath, "parse codex cloud start output", parseErr, launchErr, rawLogErr)
	}
	snapshot.EnvironmentID = firstNonEmpty(snapshot.EnvironmentID, config.EnvironmentID)
	if strings.TrimSpace(snapshot.TaskID) == "" {
		return nil, codexCloudLaunchError(req.RawLogPath, "codex cloud start output did not include a task id", nil, launchErr, rawLogErr)
	}
	result := codexCloudRuntimeResult(snapshot, config)
	if launchErr != nil {
		result.Reason += "; start command reported: " + launchErr.Error()
	}
	startedAt := time.Now().UTC().Format(time.RFC3339)
	payload := map[string]any{
		"cloud_task_id":  snapshot.TaskID,
		"remote_status":  snapshot.Status,
		"environment_id": snapshot.EnvironmentID,
		"attempt_number": snapshot.AttemptNumber,
	}
	if launchErr != nil {
		payload["start_command_error"] = launchErr.Error()
	}
	if rawLogErr != nil {
		payload["raw_log_error"] = rawLogErr.Error()
	}
	if err := eventLog.Append("codex_cloud_task_started", req.AttemptID, r.Name(), payload); err != nil {
		return nil, codexCloudTrackingError(snapshot.TaskID, req.RawLogPath, err, rawLogErr)
	}
	return &StartResult{
		StartedAt:          startedAt,
		StatusPath:         req.StatusPath,
		Capabilities:       r.Capabilities(),
		Completed:          codexCloudTerminal(result.Outcome),
		Outcome:            result.Outcome,
		Reason:             result.Reason,
		ExitCode:           codexCloudExitCode(result.Outcome),
		CloudTaskID:        snapshot.TaskID,
		CloudStatus:        snapshot.Status,
		CloudEnvironmentID: snapshot.EnvironmentID,
		CloudAttemptNumber: snapshot.AttemptNumber,
		PullRequestURL:     snapshot.PullRequestURL,
		ApplyRef:           snapshot.ApplyRef,
		LogsSummary:        snapshot.LogsSummary,
		FinalSummary:       snapshot.FinalSummary,
	}, nil
}

func (r *CodexCloudRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	return nil, tuskerError(errorConfigInvalid, "codex_cloud runner does not support local session resume")
}

func (r *CodexCloudRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	config := withDefaultCodexCloudConfig(r.Config)
	if err := validateCodexCloudRuntimeConfig(config); err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(req.CloudTaskID)
	if taskID == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing codex cloud task id"}, nil
	}
	command := codexCloudCommand(config.StatusCommand, config, StartRequest{}, taskID)
	output, err := r.executor().RunCodexCloud(ctx, codexCloudExecRequest{
		Command:     command,
		Env:         codexCloudEnv(StartRequest{}, config, taskID),
		CloudTaskID: taskID,
	})
	if err != nil {
		return nil, fmt.Errorf("codex cloud status failed: %w", err)
	}
	snapshot, err := parseCodexCloudOutput(output)
	if err != nil {
		return nil, err
	}
	snapshot.TaskID = firstNonEmpty(snapshot.TaskID, taskID)
	snapshot.EnvironmentID = firstNonEmpty(snapshot.EnvironmentID, config.EnvironmentID)
	result := codexCloudRuntimeResult(snapshot, config)
	return &result, nil
}

func (r *CodexCloudRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *CodexCloudRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	config := withDefaultCodexCloudConfig(r.Config)
	if err := validateCodexCloudRuntimeConfig(config); err != nil {
		return nil, err
	}
	taskID := strings.TrimSpace(req.CloudTaskID)
	if taskID == "" {
		return &CollectResult{Artifacts: map[string]string{}}, nil
	}
	command := codexCloudCommand(config.CollectCommand, config, StartRequest{}, taskID)
	output, err := r.executor().RunCodexCloud(ctx, codexCloudExecRequest{
		Command:     command,
		Env:         codexCloudEnv(StartRequest{}, config, taskID),
		CloudTaskID: taskID,
	})
	if err != nil {
		return nil, fmt.Errorf("codex cloud collect failed: %w", err)
	}
	snapshot, err := parseCodexCloudOutput(output)
	if err != nil {
		return nil, err
	}
	snapshot.TaskID = firstNonEmpty(snapshot.TaskID, taskID)
	snapshot.EnvironmentID = firstNonEmpty(snapshot.EnvironmentID, config.EnvironmentID)
	return &CollectResult{Artifacts: codexCloudArtifacts(snapshot)}, nil
}

func (r *CodexCloudRunner) executor() codexCloudExecutor {
	if r != nil && r.Executor != nil {
		return r.Executor
	}
	return shellCodexCloudExecutor{}
}

func withDefaultCodexCloudConfig(config CodexCloudConfig) CodexCloudConfig {
	if strings.TrimSpace(config.Command) == "" {
		config.Command = defaultCodexCloudStartCommand
	}
	if strings.TrimSpace(config.StatusCommand) == "" {
		config.StatusCommand = defaultCodexCloudStatusCommand
	}
	if strings.TrimSpace(config.CollectCommand) == "" {
		config.CollectCommand = defaultCodexCloudCollectCommand
	}
	return config
}

func validateCodexCloudRuntimeConfig(config CodexCloudConfig) error {
	if strings.TrimSpace(config.EnvironmentID) == "" {
		return tuskerError(errorConfigInvalid, "codex_cloud.environment_id is required")
	}
	if !validCodexCloudApplyMode(config.ApplyMode) {
		return tuskerError(errorConfigInvalid, "codex_cloud.apply_mode must be one of manual, pull_request")
	}
	if !validCodexCloudPRMode(config.PRMode) {
		return tuskerError(errorConfigInvalid, "codex_cloud.pr_mode must be one of none, draft, ready")
	}
	return nil
}

func codexCloudCommand(command string, config CodexCloudConfig, req StartRequest, taskID string) string {
	return replaceTemplateTokens(command, map[string]string{
		"{{workspace_path}}":         req.WorkspacePath,
		"{{prompt_path}}":            req.PromptPath,
		"{{event_sink_path}}":        req.EventSinkPath,
		"{{raw_log_path}}":           req.RawLogPath,
		"{{status_path}}":            req.StatusPath,
		"{{note_path}}":              req.NotePath,
		"{{vault_path}}":             req.VaultPath,
		"{{environment_id}}":         config.EnvironmentID,
		"{{apply_mode}}":             config.ApplyMode,
		"{{pr_mode}}":                config.PRMode,
		"{{cloud_task_id}}":          taskID,
		"{{external_loop_stage}}":    req.ExternalLoop.Stage,
		"{{external_loop_action}}":   req.ExternalLoop.Action,
		"{{external_origin_job_id}}": req.ExternalLoop.OriginJobID,
		"{{external_loop_event_id}}": req.ExternalLoop.EventID,
	})
}

func codexCloudEnv(req StartRequest, config CodexCloudConfig, taskID string) []string {
	env := runnerEnv(runnerLaunchEnv{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkspacePath: req.WorkspacePath, RepoRoot: req.RepoRoot,
		PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		NotePath: req.NotePath, VaultPath: req.VaultPath,
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		CodexPolicy: withDefaultCodexPolicy(req.CodexPolicy),
	})
	return append(env,
		"TUSKER_CODEX_CLOUD_ENVIRONMENT_ID="+config.EnvironmentID,
		"TUSKER_CODEX_CLOUD_APPLY_MODE="+config.ApplyMode,
		"TUSKER_CODEX_CLOUD_PR_MODE="+config.PRMode,
		"TUSKER_CODEX_CLOUD_EXTERNAL_COLLECT="+fmt.Sprintf("%t", config.ExternalCollect),
		"TUSKER_CODEX_CLOUD_TASK_ID="+taskID,
		"TUSKER_EXTERNAL_LOOP_STAGE="+req.ExternalLoop.Stage,
		"TUSKER_EXTERNAL_LOOP_ACTION="+req.ExternalLoop.Action,
		"TUSKER_EXTERNAL_ORIGIN_JOB_ID="+req.ExternalLoop.OriginJobID,
		"TUSKER_EXTERNAL_LOOP_EVENT_ID="+req.ExternalLoop.EventID,
		"TUSKER_EXTERNAL_LOOP_REASON="+req.ExternalLoop.Reason,
	)
}

func appendCodexCloudRawLog(path string, output []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("codex cloud raw log path is empty")
	}
	if len(output) == 0 {
		return nil
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	record := append([]byte{}, output...)
	if record[len(record)-1] != '\n' {
		record = append(record, '\n')
	}
	n, writeErr := file.Write(record)
	if writeErr != nil {
		_ = file.Close()
		return writeErr
	}
	if n != len(record) {
		_ = file.Close()
		return fmt.Errorf("wrote %d of %d bytes: %w", n, len(record), io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func preflightCodexCloudRawLog(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("codex cloud raw log path is empty")
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if info, statErr := file.Stat(); statErr != nil {
		_ = file.Close()
		return statErr
	} else if !info.Mode().IsRegular() {
		_ = file.Close()
		return fmt.Errorf("raw log %q is not a regular file", path)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func codexCloudTrackingError(taskID, rawLogPath string, eventErr, rawLogErr error) error {
	err := fmt.Errorf("record codex cloud task start %s: %w", taskID, eventErr)
	if rawLogErr == nil {
		return fmt.Errorf("%w; remote task id is preserved in raw log %q", err, rawLogPath)
	}
	return fmt.Errorf("%w; raw log also failed: %v; preserve remote task id %s from this error", err, rawLogErr, taskID)
}

func codexCloudLaunchError(rawLogPath, message string, primaryErr, launchErr, rawLogErr error) error {
	parts := []error{}
	if primaryErr != nil {
		parts = append(parts, primaryErr)
	}
	if launchErr != nil {
		parts = append(parts, fmt.Errorf("start command: %w", launchErr))
	}
	if rawLogErr != nil {
		parts = append(parts, fmt.Errorf("raw log: %w", rawLogErr))
	}
	if len(parts) == 0 {
		return tuskerError(errorConfigInvalid, message)
	}
	joined := errors.Join(parts...)
	if rawLogErr == nil {
		return fmt.Errorf("%s (raw output preserved at %q): %w", message, rawLogPath, joined)
	}
	return fmt.Errorf("%s: %w", message, joined)
}

func parseCodexCloudOutput(output []byte) (codexCloudSnapshot, error) {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return codexCloudSnapshot{}, tuskerError(errorConfigInvalid, "codex cloud output was empty")
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err == nil {
		return codexCloudSnapshotFromValue(value), nil
	}
	var out codexCloudSnapshot
	parsed := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		parsed = true
		mergeCodexCloudSnapshot(&out, codexCloudSnapshotFromValue(item))
	}
	if !parsed {
		return codexCloudSnapshot{}, tuskerError(errorConfigInvalid, "failed to parse codex cloud JSON output")
	}
	return out, nil
}

func codexCloudSnapshotFromValue(value any) codexCloudSnapshot {
	switch current := value.(type) {
	case []any:
		var out codexCloudSnapshot
		for _, item := range current {
			mergeCodexCloudSnapshot(&out, codexCloudSnapshotFromValue(item))
		}
		return out
	case map[string]any:
		out := codexCloudSnapshot{
			TaskID:         firstNonEmpty(codexCloudString(current, "cloud_task_id", "task_id", "taskId", "id"), codexCloudNestedString(current, "task", "id"), codexCloudNestedString(current, "task", "task_id"), codexCloudNestedString(current, "run", "id"), codexCloudNestedString(current, "job", "id")),
			Status:         firstNonEmpty(codexCloudString(current, "remote_status", "status", "state"), codexCloudNestedString(current, "task", "status"), codexCloudNestedString(current, "task", "state"), codexCloudNestedString(current, "run", "status"), codexCloudNestedString(current, "job", "status")),
			EnvironmentID:  firstNonEmpty(codexCloudString(current, "environment_id", "environmentId", "env_id"), codexCloudNestedString(current, "environment", "id"), codexCloudNestedString(current, "task", "environment_id")),
			AttemptNumber:  codexCloudFirstInt(current, []string{"attempt_number", "attemptNumber", "attempt"}, []string{"task", "run"}),
			PullRequestURL: firstNonEmpty(codexCloudString(current, "pull_request_url", "pr_url"), codexCloudNestedString(current, "pull_request", "url"), codexCloudNestedString(current, "pull_request", "html_url"), codexCloudNestedString(current, "pr", "url")),
			ApplyRef:       firstNonEmpty(codexCloudString(current, "apply_ref", "apply_id", "patch_id"), codexCloudNestedString(current, "apply", "ref"), codexCloudNestedString(current, "apply", "id"), codexCloudNestedString(current, "apply", "command"), codexCloudNestedString(current, "patch", "id")),
			LogsSummary:    firstNonEmpty(codexCloudString(current, "logs_summary", "log_summary"), codexCloudNestedString(current, "logs", "summary")),
			FinalSummary:   firstNonEmpty(codexCloudString(current, "final_summary", "summary"), codexCloudNestedString(current, "result", "summary")),
		}
		for _, key := range []string{"data", "result"} {
			if nested, ok := current[key]; ok {
				mergeCodexCloudSnapshot(&out, codexCloudSnapshotFromValue(nested))
			}
		}
		return out
	default:
		return codexCloudSnapshot{}
	}
}

func codexCloudRuntimeResult(snapshot codexCloudSnapshot, config CodexCloudConfig) ReconcileResult {
	status := normalizeCodexCloudStatus(snapshot.Status)
	result := ReconcileResult{
		CloudTaskID:        snapshot.TaskID,
		CloudStatus:        snapshot.Status,
		CloudEnvironmentID: snapshot.EnvironmentID,
		CloudAttemptNumber: snapshot.AttemptNumber,
		PullRequestURL:     snapshot.PullRequestURL,
		ApplyRef:           snapshot.ApplyRef,
		LogsSummary:        snapshot.LogsSummary,
		FinalSummary:       snapshot.FinalSummary,
	}
	switch status {
	case "queued", "created", "pending":
		result.LeaseState = LeaseStateClaimed
		result.Outcome = AttemptOutcomeNone
		result.Reason = "codex cloud task queued"
	case "running", "in_progress", "started":
		result.LeaseState = LeaseStateRunning
		result.Outcome = AttemptOutcomeNone
		result.Reason = "codex cloud task running"
	case "completed", "succeeded", "success", "done":
		result.LeaseState = LeaseStateReleased
		if strings.TrimSpace(config.ApplyMode) == "manual" && strings.TrimSpace(snapshot.ApplyRef) != "" {
			if config.ExternalCollect {
				result.Outcome = AttemptOutcomeSucceeded
				result.Reason = "codex cloud task completed; external collect required: " + snapshot.ApplyRef
				break
			}
			result.Outcome = AttemptOutcomeWaitingForHuman
			result.Reason = "codex cloud task completed; manual apply required: " + snapshot.ApplyRef
			break
		}
		result.Outcome = AttemptOutcomeSucceeded
		result.Reason = "codex cloud task completed"
	case "failed", "error":
		result.LeaseState = LeaseStateReleased
		result.Outcome = AttemptOutcomeFailed
		result.Reason = firstNonEmpty(snapshot.FinalSummary, "codex cloud task failed")
	case "cancelled", "canceled":
		result.LeaseState = LeaseStateReleased
		result.Outcome = AttemptOutcomeCancelled
		result.Reason = "codex cloud task cancelled"
	case "needs_input", "needs_input_required", "waiting_for_input", "action_required", "blocked":
		result.LeaseState = LeaseStateReleased
		result.Outcome = AttemptOutcomeWaitingForHuman
		result.Reason = "codex cloud task needs operator input"
	default:
		result.LeaseState = LeaseStateRunning
		result.Outcome = AttemptOutcomeNone
		result.Reason = "codex cloud task status: " + firstNonEmpty(snapshot.Status, "unknown")
	}
	return result
}

func codexCloudTerminal(outcome AttemptOutcome) bool {
	return outcome != AttemptOutcomeNone
}

func codexCloudExitCode(outcome AttemptOutcome) int {
	switch outcome {
	case AttemptOutcomeFailed:
		return 1
	case AttemptOutcomeCancelled:
		return 130
	default:
		return 0
	}
}

func codexCloudArtifacts(snapshot codexCloudSnapshot) map[string]string {
	artifacts := map[string]string{}
	for key, value := range map[string]string{
		"cloud_task_id":    snapshot.TaskID,
		"remote_status":    snapshot.Status,
		"environment_id":   snapshot.EnvironmentID,
		"attempt_number":   fmt.Sprintf("%d", snapshot.AttemptNumber),
		"pull_request_url": snapshot.PullRequestURL,
		"apply_ref":        snapshot.ApplyRef,
		"logs_summary":     snapshot.LogsSummary,
		"final_summary":    snapshot.FinalSummary,
	} {
		if strings.TrimSpace(value) != "" && value != "0" {
			artifacts[key] = value
		}
	}
	return artifacts
}

func normalizeCodexCloudStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	status = strings.ReplaceAll(status, "-", "_")
	status = strings.ReplaceAll(status, " ", "_")
	return status
}

func mergeCodexCloudSnapshot(out *codexCloudSnapshot, next codexCloudSnapshot) {
	if strings.TrimSpace(next.TaskID) != "" {
		out.TaskID = next.TaskID
	}
	if strings.TrimSpace(next.Status) != "" {
		out.Status = next.Status
	}
	if strings.TrimSpace(next.EnvironmentID) != "" {
		out.EnvironmentID = next.EnvironmentID
	}
	if next.AttemptNumber != 0 {
		out.AttemptNumber = next.AttemptNumber
	}
	if strings.TrimSpace(next.PullRequestURL) != "" {
		out.PullRequestURL = next.PullRequestURL
	}
	if strings.TrimSpace(next.ApplyRef) != "" {
		out.ApplyRef = next.ApplyRef
	}
	if strings.TrimSpace(next.LogsSummary) != "" {
		out.LogsSummary = next.LogsSummary
	}
	if strings.TrimSpace(next.FinalSummary) != "" {
		out.FinalSummary = next.FinalSummary
	}
}

func codexCloudString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch values[key].(type) {
		case map[string]any, []any:
			continue
		}
		if value := strings.TrimSpace(stringValue(values[key])); value != "" {
			return value
		}
	}
	return ""
}

func codexCloudNestedString(values map[string]any, parent, key string) string {
	nested, ok := values[parent].(map[string]any)
	if !ok {
		return ""
	}
	return codexCloudString(nested, key)
}

func codexCloudFirstInt(values map[string]any, keys []string, parents []string) int {
	for _, key := range keys {
		if value := codexCloudIntFromAny(values[key]); value != 0 {
			return value
		}
	}
	for _, parent := range parents {
		nested, ok := values[parent].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range keys {
			if value := codexCloudIntFromAny(nested[key]); value != 0 {
				return value
			}
		}
	}
	return 0
}

func codexCloudIntFromAny(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	case json.Number:
		parsed, _ := current.Int64()
		return int(parsed)
	}
	if text := strings.TrimSpace(stringValue(value)); text != "" {
		var parsed int
		if _, err := fmt.Sscanf(text, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
