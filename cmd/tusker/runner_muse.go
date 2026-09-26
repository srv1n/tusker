package main

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// MuseRunner invokes the installed Muse binary directly and is only
// admitted with prepared argv.
type MuseRunner struct{}

func (r *MuseRunner) Name() RunnerName { return RunnerMuse }

func (r *MuseRunner) Capabilities() RunnerCapabilities {
	// Muse exec has structured output and session IDs, but its headless CLI
	// does not expose a Tusker approval callback we can safely keep open.
	return RunnerCapabilities{StructuredEvents: true, ResumeSession: true, Heartbeats: true, MachineFinalStatus: true, UsageMetrics: true, HardSay: true, PreassignSessionID: true, ResumeAfterDeath: true}
}

func (r *MuseRunner) Start(ctx context.Context, req StartRequest) (*StartResult, error) {
	// Muse 1.3.0 `muse exec --help` exposes no per-invocation MCP overlay.
	// Record the supported CLI ask route instead of mutating user/workspace settings.
	if err := museRecordCLIAskRoute(req.EventSinkPath, req.AttemptID, req.PromptPath); err != nil {
		return nil, err
	}
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
	if session := museCLIArgvSession(req.CommandArgv); session != "" {
		req.NativeSessionID = session
	} else {
		req.NativeSessionID = uuid.NewString()
		req.CommandArgv = append(req.CommandArgv, "--session-id", req.NativeSessionID)
	}
	return startDetachedRunnerWrapper(ctx, r.Name(), req, nil, r.Capabilities())
}

func (r *MuseRunner) Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error) {
	if err := museRecordCLIAskRoute(req.EventSinkPath, req.AttemptID, req.PromptPath); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SessionRef) == "" {
		return nil, tuskerError(errorMissingArg, "muse resume requires session_ref")
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
	startReq := req.startRequest(command, argv)
	return startDetachedRunnerWrapper(ctx, r.Name(), startReq, &req, r.Capabilities())
}

func (r *MuseRunner) Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error) {
	if strings.TrimSpace(req.SessionRef) == "" {
		return &ReconcileResult{LeaseState: LeaseStateReleased, Outcome: AttemptOutcomeAbandoned, Reason: "missing session ref"}, nil
	}
	return &ReconcileResult{LeaseState: LeaseStateRetryQueued, Outcome: AttemptOutcomeNone, Reason: "session is resumable"}, nil
}

func (r *MuseRunner) Interrupt(ctx context.Context, req InterruptRequest) error { return nil }

func (r *MuseRunner) Collect(ctx context.Context, req CollectRequest) (*CollectResult, error) {
	return &CollectResult{Artifacts: map[string]string{}}, nil
}

const museAskRouteHeader = "## Asking the operator or architect (Muse)"

// museAskRoutePrompt is the CLI replacement for the MCP ask tool that other
// harnesses receive. The attempt environment supplies every identity value.
const museAskRoutePrompt = museAskRouteHeader + `

This harness has no Tusker MCP tools. When you need a decision you cannot make
from the task contract, ask with the exact Tusker binary for this attempt:

    "$TUSKER_BIN" message ask --project "$TUSKER_PROJECT_ID" --sender "task:$TUSKER_ITEM_ID" --recipient operator --key "$TUSKER_ATTEMPT_ID-<short-slug>" --body "<question>" --yield --json

Use --recipient task:<TASK-ID> or --contact architect --task "$TUSKER_ITEM_ID"
for a listed contact. The command does not wait. If you cannot continue without
the answer, say so and end your turn; the answer is delivered into this same
session when it resumes.
`

// museRecordCLIAskRoute records the route and appends the ask instructions to
// this attempt's prompt once. The daemon rewrites the prompt per attempt, and
// the section lands after any resume fingerprint marker it already carries.
func museRecordCLIAskRoute(eventSink, attemptID, promptPath string) error {
	if err := NewEventLog(eventSink).Append("worker_ask_route", attemptID, RunnerMuse, map[string]any{"route": "cli"}); err != nil {
		return err
	}
	if strings.TrimSpace(promptPath) == "" {
		return nil
	}
	prompt, err := readText(promptPath)
	if err != nil || strings.Contains(prompt, museAskRouteHeader) {
		return err
	}
	return writeText(promptPath, strings.TrimRight(prompt, "\n")+"\n\n"+museAskRoutePrompt)
}

func defaultMuseCLICommand() string { return "muse exec --json" }

func museCLIArgv(command string, policy CodexPolicy, workspace, model, effort string) ([]string, error) {
	fields, err := shellLikeFields(strings.TrimSpace(command))
	if err != nil || len(fields) == 0 {
		return nil, tuskerError(errorConfigInvalid, "muse command must be structured and parseable")
	}
	if fields[0] != "muse" && !strings.HasSuffix(fields[0], "/muse") {
		return nil, tuskerError(errorConfigInvalid, "muse policy can only be enforced for the direct Muse executable")
	}
	if len(fields) == 1 || fields[1] != "exec" {
		return nil, tuskerError(errorConfigInvalid, "muse requires a direct muse exec command")
	}
	if museCLIHasPolicyOverride(fields) {
		return nil, tuskerError(errorConfigInvalid, "muse command cannot override workspace, approval mode, or full-access policy")
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

func museCLIArgvSession(argv []string) string {
	for i, arg := range argv {
		if arg == "--session-id" && i+1 < len(argv) {
			return strings.TrimSpace(argv[i+1])
		}
		if strings.HasPrefix(arg, "--session-id=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--session-id="))
		}
	}
	return ""
}

func museFailureReasonCode(outcome AttemptOutcome, reason string) RunFailureReasonCode {
	switch outcome {
	case AttemptOutcomeCancelled:
		return RunFailureCancelled
	case AttemptOutcomeUnknown:
		return RunFailureOutcomeUnknown
	case AttemptOutcomeFailed:
		lower := strings.ToLower(reason)
		switch {
		case strings.Contains(lower, "rate limit"), strings.Contains(lower, "usage limit"), strings.Contains(lower, "quota"), strings.Contains(lower, "usage_limit"):
			return RunFailureUsageLimit
		case strings.Contains(lower, "auth"), strings.Contains(lower, "login"):
			return RunFailureAuthExpired
		case strings.Contains(lower, "permission"), strings.Contains(lower, "approval"):
			return RunFailurePermissionDenied
		default:
			return RunFailureProviderError
		}
	}
	return ""
}

// classifyMuseCLIOutput consumes only the direct Muse exec record envelope.
// Unknown records are ignored so protocol additions do not turn a successful
// run into a false failure; a missing terminal record remains unknown.
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
		body, _ := payload["payload"].(map[string]any)
		reason := firstNonEmpty(stringValue(payload["reason"]), stringValue(body["reason"]), stringValue(payload["error"]), stringValue(body["error"]), stringValue(payload["reason_code"]), stringValue(body["reason_code"]))
		kind := strings.ToLower(strings.TrimSpace(stringValue(payload["payload_type"])))
		if kind == "" {
			kind = strings.ToLower(strings.TrimSpace(stringValue(payload["record_type"])))
		}
		if kind == "" {
			kind = strings.ToLower(strings.TrimSpace(stringValue(payload["type"])))
		}
		switch kind {
		case "run.terminal.completed", "terminal.completed":
			seenTerminal = true
		case "run.terminal.failed", "terminal.failed", "run.terminal.error", "terminal.error":
			return AttemptOutcomeFailed, firstNonEmpty(reason, "Muse reported a failed terminal result"), session
		case "run.terminal.cancelled", "terminal.cancelled":
			return AttemptOutcomeCancelled, firstNonEmpty(reason, "Muse run cancelled"), session
		}
	}
	if !seenTerminal {
		return AttemptOutcomeUnknown, "Muse terminal result missing", session
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

var _ Runner = (*MuseRunner)(nil)
