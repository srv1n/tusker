package main

import (
	"context"
	"strings"
)

type CodexRunner struct{}

func (r *CodexRunner) Name() RunnerName { return RunnerCodex }

func (r *CodexRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: false, ExplicitApprovals: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true}
}

func (r *CodexRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if shouldUseLiveCodex(req.Command) {
		return startDetachedRunnerWrapper(ctx, RunnerCodexAppServer, req, nil, r.Capabilities())
	}
	return executeRunnerCommand(ctx, r.Name(), runnerExecRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		RepoRoot: req.RepoRoot, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath, Command: req.Command, NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy,
		ExternalLoop: req.ExternalLoop,
	}, r.Capabilities())
}

func (r *CodexRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "codex app-server"
	}
	startReq := StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		RepoRoot: req.RepoRoot, Command: command, NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}
	return r.Start(ctx, startReq)
}

func (r *CodexRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing session ref"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateRetryQueued, Outcome: AttemptOutcomeNone, Reason: "previous session exists; queued fresh continuation attempt"}, nil
}

func (r *CodexRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *CodexRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}

type CodexAppServerRunner struct{ CodexRunner }

func (r *CodexAppServerRunner) Name() RunnerName { return RunnerCodexAppServer }

func (r *CodexAppServerRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		req.Command = "codex app-server"
	}
	if !shouldUseLiveCodex(req.Command) {
		return nil, tuskerError(errorConfigInvalid, "codex_app_server runner requires an app-server command")
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

func (r *CodexAppServerRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		req.Command = "codex app-server"
	}
	if !shouldUseLiveCodex(req.Command) {
		return nil, tuskerError(errorConfigInvalid, "codex_app_server runner requires an app-server command")
	}
	startReq := StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, StatusPath: req.StatusPath,
		RepoRoot: req.RepoRoot, Command: req.Command, NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}
	return r.Start(ctx, startReq)
}

type CodexExecRunner struct{}

func (r *CodexExecRunner) Name() RunnerName { return RunnerCodexExec }

func (r *CodexExecRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: false, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true}
}

func (r *CodexExecRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		req.Command = "codex exec --skip-git-repo-check -"
	}
	if shouldUseLiveCodex(req.Command) {
		return nil, tuskerError(errorConfigInvalid, "codex_exec runner requires a detached codex exec command, not app-server")
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

func (r *CodexExecRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	return nil, tuskerError(errorConfigInvalid, "codex_exec runner does not support local app-server session resume")
}

func (r *CodexExecRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	return nil, nil
}

func (r *CodexExecRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *CodexExecRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}
