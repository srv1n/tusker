package main

import (
	"context"
	"regexp"
	"strings"
)

type CodexRunner struct{}

var codexExecCommandPrefix = regexp.MustCompile(`^codex[[:space:]]+exec[[:space:]]+`)

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
		RepoRoot: req.RepoRoot, EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath, Command: req.Command, CommandArgv: append([]string(nil), req.CommandArgv...), CommandExecutableFP: req.CommandExecutableFP, CommandSearchPath: req.CommandSearchPath, RunnerPathPrefix: req.RunnerPathPrefix,
		RunnerProfile: req.RunnerProfile, RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		Actor:    req.Actor,
		NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy,
		ExternalLoop: req.ExternalLoop,
	}, r.Capabilities())
}

func (r *CodexRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	_ = ctx
	_ = req
	return nil, tuskerError(errorInvalidTransition, "codex runner does not support native session resume; request explicit context recovery")
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
	_ = ctx
	_ = req
	return nil, tuskerError(errorInvalidTransition, "codex_app_server runner does not support native session resume; request explicit context recovery")
}

type CodexExecRunner struct{}

func (r *CodexExecRunner) Name() RunnerName { return RunnerCodexExec }

func (r *CodexExecRunner) Capabilities() RunnerCapabilities {
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: true, HardSay: true, ResumeAfterDeath: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true}
}

func (r *CodexExecRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if len(req.CommandArgv) == 0 {
		return nil, tuskerError(errorConfigInvalid, "dispatch must supply a prepared argv; command-string launches are no longer supported")
	}
	if strings.TrimSpace(req.Command) == "" {
		req.Command = defaultCodexExecCommand()
	}
	if shouldUseLiveCodex(req.Command) {
		return nil, tuskerError(errorConfigInvalid, "codex_exec runner requires a detached codex exec command, not app-server")
	}
	projection, err := projectWorkerMCP(req.ProjectID, req.RecordID, req.ItemID, req.AttemptID, req.LeaseGeneration, req.WorkRevision, req.EventSinkPath, req.StatusPath, 900, false)
	if err != nil {
		return nil, err
	}
	req.CommandArgv = appendCodexMCP(req.CommandArgv, projection)
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

func (r *CodexExecRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	if len(req.CommandArgv) == 0 {
		return nil, tuskerError(errorConfigInvalid, "dispatch must supply a prepared argv; command-string launches are no longer supported")
	}
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, tuskerError(errorMissingArg, "codex_exec resume requires session_ref")
	}
	command := codexExecResumeCommand(req.Command)
	if shouldUseLiveCodex(command) {
		return nil, tuskerError(errorConfigInvalid, "codex_exec runner requires a detached codex exec resume command, not app-server")
	}
	resumedArgv := codexExecResumeArgv(req.CommandArgv, req.SessionRef)
	if len(resumedArgv) < 4 || resumedArgv[1] != "exec" || resumedArgv[len(resumedArgv)-3] != "resume" {
		return nil, tuskerError(errorConfigInvalid, "codex_exec resume requires a direct codex exec command")
	}
	projection, err := projectWorkerMCP(req.ProjectID, req.RecordID, req.ItemID, req.AttemptID, req.LeaseGeneration, req.WorkRevision, req.EventSinkPath, req.StatusPath, 900, false)
	if err != nil {
		return nil, err
	}
	req.CommandArgv = appendCodexMCP(resumedArgv, projection)
	req.Command = command
	startReq := req.startRequest(command, req.CommandArgv)
	return startDetachedRunnerWrapper(ctx, r.Name(), startReq, &req, r.Capabilities())
}

// startRequest is the one ResumeRequest -> StartRequest projection for the
// exec-style runners, so a resume launches under the same policy inputs
// (private folders, actor, principal, codex policy) Start would.
func (req ResumeRequest) startRequest(command string, argv []string) StartRequest {
	return StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates,
		WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath,
		RepoRoot: req.RepoRoot, Command: command, CommandArgv: append([]string(nil), argv...), CommandExecutableFP: req.CommandExecutableFP,
		CommandSearchPath: req.CommandSearchPath, RunnerPathPrefix: req.RunnerPathPrefix, RunnerProfile: req.RunnerProfile,
		RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		PrivateFolders: append([]string(nil), req.PrivateFolders...),
		NotePath:       req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
		Principal: req.Principal, Actor: req.Actor,
	}
}

func codexExecResumeArgv(argv []string, sessionRef string) []string {
	if len(argv) < 3 || argv[1] != "exec" {
		return append([]string(nil), argv...)
	}
	args := append([]string(nil), argv[2:]...)
	if len(args) > 0 && args[len(args)-1] == "-" {
		args = args[:len(args)-1]
	}
	out := []string{argv[0], "exec"}
	out = append(out, args...)
	out = append(out, "resume")
	out = append(out, sessionRef, "-")
	return out
}

func (r *CodexExecRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	return nil, nil
}

func (r *CodexExecRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *CodexExecRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}

func defaultCodexExecCommand() string {
	return "codex exec --json --skip-git-repo-check -"
}

func codexExecResumeCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		command = defaultCodexExecCommand()
	}
	if strings.Contains(command, "{{session_ref}}") {
		return command
	}
	fields := strings.Fields(command)
	if len(fields) >= 2 && fields[0] == "codex" && fields[1] == "exec" {
		if len(fields) >= 3 && fields[2] == "resume" {
			return command
		}
		args := append([]string{}, fields[2:]...)
		if len(args) > 0 && args[len(args)-1] == "-" {
			args = args[:len(args)-1]
		}
		out := append([]string{"codex", "exec"}, args...)
		out = append(out, "resume")
		out = append(out, "{{session_ref}}", "-")
		return strings.Join(out, " ")
	}
	return command
}
