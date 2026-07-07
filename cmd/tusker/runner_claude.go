package main

import (
	"context"
	"strings"
)

type ClaudeRunner struct{}

func (r *ClaudeRunner) Name() RunnerName { return RunnerClaude }

func (r *ClaudeRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: true, ExplicitApprovals: false, Heartbeats: true, MachineFinalStatus: false}
}

func (r *ClaudeRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if shouldUseLiveClaude(req.Command) {
		return startLiveClaude(ctx, req, nil)
	}
	return executeRunnerCommand(ctx, r.Name(), runnerExecRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		RepoRoot: req.RepoRoot, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath, Command: req.Command, NotePath: req.NotePath, VaultPath: req.VaultPath,
		CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}, r.Capabilities())
}

func (r *ClaudeRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "claude -p --resume {{session_ref}} --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
		if strings.TrimSpace(req.MessageRef) != "" {
			command += " --resume-session-at {{message_ref}}"
		}
	} else if strings.Contains(command, "{{session_ref}}") {
	} else if strings.HasPrefix(command, "claude ") {
		command = strings.Replace(command, "claude ", "claude --resume {{session_ref}} ", 1)
		if strings.TrimSpace(req.MessageRef) != "" && !strings.Contains(command, "--resume-session-at") {
			command += " --resume-session-at {{message_ref}}"
		}
	}
	if shouldUseLiveClaude(command) {
		return startLiveClaude(ctx, StartRequest{
			ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
			Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
			EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
			RepoRoot: req.RepoRoot, Command: command, NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
		}, &req)
	}
	return executeRunnerCommand(ctx, r.Name(), runnerExecRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, SessionRef: req.SessionRef, MessageRef: req.MessageRef, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath,
		RepoRoot: req.RepoRoot, PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		Command: command, NotePath: req.NotePath, VaultPath: req.VaultPath, ResumeMode: true, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}, r.Capabilities())
}

func (r *ClaudeRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing session ref"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateRetryQueued, Outcome: AttemptOutcomeNone, Reason: "session ref is present; claude-code continuity is best-effort"}, nil
}

func (r *ClaudeRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *ClaudeRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}
