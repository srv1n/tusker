package main

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

type ClaudeRunner struct{}

func (r *ClaudeRunner) Name() RunnerName { return RunnerClaude }

func (r *ClaudeRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: true, ExplicitApprovals: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true, SoftSay: true, HardSay: true, PreassignSessionID: true, ResumeAfterDeath: true}
}

func (r *ClaudeRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if err := validateClaudeSessionFlags(req.Command, req.CommandArgv); err != nil {
		return nil, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	req.NativeSessionID = id.String()
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

func (r *ClaudeRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	startReq := StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates, WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath,
		RepoRoot: req.RepoRoot, Command: req.Command, CommandArgv: append([]string(nil), req.CommandArgv...), CommandExecutableFP: req.CommandExecutableFP, CommandSearchPath: req.CommandSearchPath, RunnerPathPrefix: req.RunnerPathPrefix, RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		PrivateFolders: req.PrivateFolders,
		NotePath:       req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, tuskerError(errorConfigInvalid, "Claude resume requires a session ref")
	}
	if err := validateClaudeResumeFlags(req.Command, req.CommandArgv); err != nil {
		return nil, err
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), startReq, &req, r.Capabilities())
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
