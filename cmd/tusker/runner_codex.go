package main

import (
	"context"
	"strings"
)

type CodexRunner struct{}

func (r *CodexRunner) Name() RunnerName { return RunnerCodex }

func (r *CodexRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: true, ExplicitApprovals: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true}
}

func (r *CodexRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if shouldUseLiveCodex(req.Command) {
		return startLiveCodex(ctx, req, nil)
	}
	return executeRunnerCommand(ctx, r.Name(), runnerExecRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		WorkRevision: req.WorkRevision, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		RepoRoot: req.RepoRoot, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath, Command: req.Command, NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy,
	}, r.Capabilities())
}

func (r *CodexRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "codex exec resume --skip-git-repo-check --json {{session_ref}} -"
	} else if strings.Contains(command, "{{session_ref}}") {
		// user-provided resume-aware command
	} else if strings.HasPrefix(command, "codex exec ") {
		command = "codex exec resume --skip-git-repo-check --json {{session_ref}} -"
	}
	if shouldUseLiveCodex(command) {
		return startLiveCodex(ctx, StartRequest{
			ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
			WorkRevision: req.WorkRevision, ActiveStates: req.ActiveStates, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
			EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
			RepoRoot: req.RepoRoot, Command: command, NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy,
		}, &req)
	}
	return executeRunnerCommand(ctx, r.Name(), runnerExecRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		WorkRevision: req.WorkRevision, SessionRef: req.SessionRef, MessageRef: req.MessageRef, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath,
		RepoRoot: req.RepoRoot, PromptPath: req.PromptPath, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		Command: command, NotePath: req.NotePath, VaultPath: req.VaultPath, ResumeMode: true, CodexPolicy: req.CodexPolicy,
	}, r.Capabilities())
}

func (r *CodexRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing session ref"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateRetryQueued, Outcome: AttemptOutcomeNone, Reason: "session is resumable"}, nil
}

func (r *CodexRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *CodexRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}
