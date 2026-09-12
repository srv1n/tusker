package main

import (
	"context"
	"encoding/json"
	"strings"
)

// MuseCLIRunner is intentionally separate from RunnerMuse. RunnerMuse keeps
// the historical `codex --profile muse` route; this adapter invokes the
// installed Muse binary directly and is only admitted with prepared argv.
type MuseCLIRunner struct{}

func (r *MuseCLIRunner) Name() RunnerName { return RunnerMuseCLI }

func (r *MuseCLIRunner) Capabilities() RunnerCapabilities {
	// Muse exec has structured output and session IDs, but its headless CLI
	// does not expose a Tusker approval callback we can safely keep open.
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true}
}

func (r *MuseCLIRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	if strings.TrimSpace(req.Command) == "" {
		req.Command = defaultMuseCLICommand()
	}
	if len(req.CommandArgv) == 0 {
		argv, err := museCLIArgv(req.Command, req.CodexPolicy, req.WorkspacePath, req.RunnerModel, req.RunnerEffort)
		if err != nil {
			return nil, err
		}
		req.CommandArgv = argv
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

func (r *MuseCLIRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, tuskerError(errorMissingArg, "muse_cli resume requires session_ref")
	}
	command := firstNonEmpty(strings.TrimSpace(req.Command), defaultMuseCLICommand())
	argv := append([]string(nil), req.CommandArgv...)
	if len(argv) == 0 {
		var err error
		argv, err = museCLIArgv(command, req.CodexPolicy, req.WorkspacePath, req.RunnerModel, req.RunnerEffort)
		if err != nil {
			return nil, err
		}
	}
	argv = museCLIResumeArgv(argv, req.SessionRef)
	startReq := StartRequest{
		ProjectID: req.ProjectID, RecordID: req.RecordID, ItemID: req.ItemID, AttemptID: req.AttemptID,
		Lane: req.Lane, WorkRevision: req.WorkRevision, LeaseGeneration: req.LeaseGeneration, ActiveStates: req.ActiveStates,
		WorkingDir: req.WorkingDir, WorkspacePath: req.WorkspacePath, PromptPath: req.PromptPath,
		EventSinkPath: req.EventSinkPath, RawLogPath: req.RawLogPath, RawLogMaxBytes: req.RawLogMaxBytes, StatusPath: req.StatusPath,
		RepoRoot: req.RepoRoot, Command: command, CommandArgv: argv, CommandExecutableFP: req.CommandExecutableFP,
		CommandSearchPath: req.CommandSearchPath, RunnerPathPrefix: req.RunnerPathPrefix, RunnerProfile: req.RunnerProfile,
		RunnerHarness: req.RunnerHarness, RunnerModel: req.RunnerModel, RunnerEffort: req.RunnerEffort,
		NotePath: req.NotePath, VaultPath: req.VaultPath, CodexPolicy: req.CodexPolicy, ExternalLoop: req.ExternalLoop,
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), startReq, &req, r.Capabilities())
}

func (r *MuseCLIRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing session ref"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateRetryQueued, Outcome: AttemptOutcomeNone, Reason: "session is resumable"}, nil
}

func (r *MuseCLIRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *MuseCLIRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}

func defaultMuseCLICommand() string { return "muse exec --json" }

func museCLIArgv(command string, policy CodexPolicy, workspace, model, effort string) ([]string, error) {
	fields, err := shellLikeFields(strings.TrimSpace(command))
	if err != nil || len(fields) == 0 {
		return nil, tuskerError(errorConfigInvalid, "muse_cli command must be structured and parseable")
	}
	if fields[0] != "muse" && !strings.HasSuffix(fields[0], "/muse") {
		return nil, tuskerError(errorConfigInvalid, "muse_cli policy can only be enforced for the direct Muse executable")
	}
	if len(fields) == 1 || fields[1] != "exec" {
		return nil, tuskerError(errorConfigInvalid, "muse_cli requires a direct muse exec command")
	}
	if museCLIHasPolicyOverride(fields) {
		return nil, tuskerError(errorConfigInvalid, "muse_cli command cannot override workspace, approval mode, or full-access policy")
	}
	add := func(flag string, values ...string) {
		if !commandHasFlag(strings.Join(fields, " "), flag) {
			fields = append(fields, flag)
			fields = append(fields, values...)
		}
	}
	if strings.TrimSpace(workspace) != "" {
		add("--workspace", workspace)
	}
	policy = withDefaultCodexPolicy(policy)
	mode := strings.TrimSpace(firstNonEmpty(policy.TurnSandboxPolicy, policy.ThreadSandbox))
	if mode == "danger-full-access" && policy.ApprovalPolicy == "never" {
		add("--yolo")
	} else {
		approval := firstNonEmpty(strings.TrimSpace(policy.ApprovalPolicy), "never")
		if approval != "on-request" {
			approval = "never"
		}
		add("--approval-mode", approval)
		network := "restricted"
		if policy.TurnSandboxNetwork != nil && *policy.TurnSandboxNetwork {
			network = "enabled"
		}
		add("--sandbox-network", network)
		if mode == "read-only" {
			add("--disable-write")
			add("--disable-shell")
		}
		if network == "restricted" {
			add("--disable-web-tools")
		}
	}
	if strings.TrimSpace(model) != "" {
		add("--model", model)
	}
	if strings.TrimSpace(effort) != "" {
		add("--reasoning-effort", effort)
	}
	add("--json")
	return fields, nil
}

func museCLIHasPolicyOverride(fields []string) bool {
	for _, field := range fields {
		lower := strings.ToLower(strings.TrimSpace(field))
		for _, prefix := range []string{"--yolo", "--workspace", "--approval-mode", "--sandbox-network"} {
			if lower == prefix || strings.HasPrefix(lower, prefix+"=") {
				return true
			}
		}
	}
	return false
}

func museCLIResumeArgv(argv []string, session string) []string {
	out := append([]string(nil), argv...)
	if !commandHasFlag(strings.Join(out, " "), "--session-id") {
		out = append(out, "--session-id", strings.TrimSpace(session))
	}
	return out
}

// classifyMuseCLIOutput consumes only the direct Muse exec record envelope.
// Unknown records are ignored so protocol additions do not turn a successful
// run into a false failure; a missing terminal record remains a failure.
func classifyMuseCLIOutput(output string) (AttemptOutcome, string, string) {
	seenTerminal := false
	session := ""
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(line), &value) != nil {
			continue
		}
		session = firstNonEmpty(session, museSessionID(value))
		payload, _ := value.(map[string]any)
		kind := strings.ToLower(strings.TrimSpace(stringValue(payload["payload_type"])))
		if kind == "" {
			kind = strings.ToLower(strings.TrimSpace(stringValue(payload["record_type"])))
		}
		switch kind {
		case "run.terminal.completed", "terminal.completed":
			seenTerminal = true
		case "run.terminal.failed", "terminal.failed", "run.terminal.error", "terminal.error":
			return AttemptOutcomeFailed, firstNonEmpty(stringValue(payload["reason"]), "Muse reported a failed terminal result"), session
		case "run.terminal.cancelled", "terminal.cancelled":
			return AttemptOutcomeCancelled, firstNonEmpty(stringValue(payload["reason"]), "Muse run cancelled"), session
		}
	}
	if !seenTerminal {
		return AttemptOutcomeFailed, "Muse terminal result missing", session
	}
	return AttemptOutcomeSucceeded, "", session
}

func museSessionID(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"session_id", "sessionId"} {
			if id := strings.TrimSpace(stringValue(current[key])); id != "" {
				return id
			}
		}
		if stream, ok := current["stream"].(map[string]any); ok {
			if id := strings.TrimSpace(stringValue(stream["id"])); id != "" {
				return id
			}
		}
		for _, nested := range current {
			if id := museSessionID(nested); id != "" {
				return id
			}
		}
	case []any:
		for _, nested := range current {
			if id := museSessionID(nested); id != "" {
				return id
			}
		}
	}
	return ""
}

var _ Runner = (*MuseCLIRunner)(nil)
