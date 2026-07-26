package main

import (
	"context"
	"strings"
)

type ClaudeRunner struct{}

func (r *ClaudeRunner) Name() RunnerName { return RunnerClaude }

func (r *ClaudeRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: false, ExplicitApprovals: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true}
}

func (r *ClaudeRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if shouldUseLiveClaude(req.Command) {
		return startLiveClaude(ctx, req, nil)
	}
	return executeRunnerCommand(ctx, r.Name(), runnerExecRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		RepoRoot: req.RepoRoot, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath, Command: req.Command, RunnerPathPrefix: req.RunnerPathPrefix,
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		NotePath: req.NotePath, VaultPath: req.VaultPath,
		CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}, r.Capabilities())
}

func (r *ClaudeRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
	}
	startReq := StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath,
		RepoRoot: req.RepoRoot, Command: command, RunnerPathPrefix: req.RunnerPathPrefix, RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}
	if shouldUseLiveClaude(command) {
		// Claude attempts are fresh by default. A runner profile must opt into
		// session continuation by placing the session token in its command.
		if strings.Contains(command, "{{session_ref}}") {
			return startLiveClaude(ctx, startReq, &req)
		}
		return startLiveClaude(ctx, startReq, nil)
	}
	return r.Start(ctx, startReq)
}

func (r *ClaudeRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing session ref"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateRetryQueued, Outcome: AttemptOutcomeNone, Reason: "session is resumable"}, nil
}

func (r *ClaudeRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *ClaudeRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}
