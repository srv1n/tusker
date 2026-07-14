package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type runtimeTokenTotals struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type runtimeArtifactPaths struct {
	Workspace string `json:"workspace"`
	Prompt    string `json:"prompt"`
	Events    string `json:"events"`
	RawLog    string `json:"raw_log"`
	Status    string `json:"status"`
}

type runInspection struct {
	OK                       bool                        `json:"ok"`
	Run                      *RunStatus                  `json:"run"`
	Attempts                 []RunAttempt                `json:"attempts"`
	Turns                    []RunTurn                   `json:"turns"`
	ActiveTurn               *RunTurn                    `json:"active_turn"`
	LatestTurn               *RunTurn                    `json:"latest_turn"`
	Sessions                 []RunnerSession             `json:"sessions"`
	LatestSession            *RunnerSession              `json:"latest_session"`
	SupervisorDecisions      []RuntimeSupervisorDecision `json:"supervisor_decisions"`
	LatestSupervisorDecision *RuntimeSupervisorDecision  `json:"latest_supervisor_decision"`
	ExternalLoopEvents       []ExternalLoopEvent         `json:"external_loop_events"`
	ExternalLoopCounters     ExternalLoopCounters        `json:"external_loop_counters"`
	LatestEvent              map[string]any              `json:"latest_event,omitempty"`
	TokenTotals              runtimeTokenTotals          `json:"token_totals"`
	FailureClass             string                      `json:"failure_class,omitempty"`
	Paths                    runtimeArtifactPaths        `json:"paths"`
	Authorization            *RunAuthorization           `json:"authorization,omitempty"`
	Identity                 *RunIdentityMetadata        `json:"identity,omitempty"`
	Resume                   runResumeCapability         `json:"resume"`
}

type runResumeCapability struct {
	Supported bool   `json:"supported"`
	Command   string `json:"command,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func resumeCapability(run *RunStatus, session *RunnerSession) runResumeCapability {
	if run == nil || session == nil || strings.TrimSpace(session.SessionRef) == "" {
		return runResumeCapability{Reason: "runner session id is unavailable"}
	}
	if !session.Resumable {
		return runResumeCapability{Reason: firstNonEmpty(session.LastError, "runner session is not resumable")}
	}
	quoted := shellSingleQuote(session.SessionRef)
	switch RunnerName(run.Runner) {
	case RunnerCodex, RunnerCodexExec, RunnerCodexAppServer:
		return runResumeCapability{Supported: true, Command: "codex exec resume " + quoted}
	case RunnerClaude:
		return runResumeCapability{Supported: true, Command: "claude --resume " + quoted}
	default:
		return runResumeCapability{Reason: "runner does not support native resume"}
	}
}

func (s *RuntimeStore) FindRun(identity string) (*RunStatus, error) {
	rows, err := s.ListRuns()
	if err != nil {
		return nil, err
	}
	for _, run := range rows {
		if run.ItemID == identity || run.RecordID == identity {
			copy := run
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *RuntimeStore) ListAttemptsForRun(projectID, recordID string) ([]RunAttempt, error) {
	rows, err := s.query(`SELECT attempt_id, project_id, record_id, item_id, runner, lane, work_revision, workspace_path, session_ref, parent_attempt_id, child_type, branch_name, merge_rule, fanout_group, cloud_task_id, cloud_status, cloud_environment_id, cloud_attempt_number, pull_request_url, apply_ref, logs_summary, final_summary, process_pid, outcome, exit_code, turns_used, prompt_path, event_sink_path, raw_log_path, status_path, last_error, started_at, finished_at
		FROM attempts
		WHERE project_id = ? AND record_id = ?
		ORDER BY started_at DESC, attempt_id DESC`, projectID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunAttempt
	for rows.Next() {
		var attempt RunAttempt
		if err := rows.Scan(&attempt.AttemptID, &attempt.ProjectID, &attempt.RecordID, &attempt.ItemID, &attempt.Runner, &attempt.Lane, &attempt.WorkRevision, &attempt.WorkspacePath, &attempt.SessionRef, &attempt.ParentAttemptID, &attempt.ChildType, &attempt.BranchName, &attempt.MergeRule, &attempt.FanoutGroup, &attempt.CloudTaskID, &attempt.CloudStatus, &attempt.CloudEnvironmentID, &attempt.CloudAttemptNumber, &attempt.PullRequestURL, &attempt.ApplyRef, &attempt.LogsSummary, &attempt.FinalSummary, &attempt.ProcessPID, &attempt.Outcome, &attempt.ExitCode, &attempt.TurnsUsed, &attempt.PromptPath, &attempt.EventSinkPath, &attempt.RawLogPath, &attempt.StatusPath, &attempt.LastError, &attempt.StartedAt, &attempt.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

func buildRunInspection(store *RuntimeStore, run *RunStatus) (runInspection, error) {
	if run == nil {
		return runInspection{}, tuskerError(errorNotFound, "run not found")
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return runInspection{}, err
	}
	turns, err := store.ListTurnsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return runInspection{}, err
	}
	sessions, err := store.ListSessionsForRun(run.ProjectID, run.RecordID, run.Runner)
	if err != nil {
		return runInspection{}, err
	}
	supervisorDecisions, err := store.ListRuntimeSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return runInspection{}, err
	}
	externalLoopEvents, err := store.ListExternalLoopEvents(run.ProjectID, run.RecordID)
	if err != nil {
		return runInspection{}, err
	}
	latestSession := latestRunnerSession(sessions)
	authorization, err := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	if err != nil {
		return runInspection{}, err
	}
	identity, err := store.RunIdentity(run.ProjectID, run.RecordID)
	if err != nil {
		return runInspection{}, err
	}
	eventPath := bestRunEventPath(*run, attempts)
	return runInspection{
		OK:                       true,
		Run:                      run,
		Attempts:                 attempts,
		Turns:                    turns,
		ActiveTurn:               activeRunTurn(turns),
		LatestTurn:               latestRunTurn(turns),
		Sessions:                 sessions,
		LatestSession:            latestSession,
		SupervisorDecisions:      supervisorDecisions,
		LatestSupervisorDecision: latestSupervisorDecision(supervisorDecisions),
		ExternalLoopEvents:       externalLoopEvents,
		ExternalLoopCounters:     externalLoopCountersForEvents(externalLoopEvents),
		LatestEvent:              latestJSONLEvent(eventPath),
		TokenTotals:              tokenTotalsForTurns(turns),
		FailureClass:             runtimeFailureClass(*run, attempts, turns),
		Paths: runtimeArtifactPaths{
			Workspace: run.WorkspacePath,
			Prompt:    run.PromptPath,
			Events:    eventPath,
			RawLog:    bestRunLogPath(*run, attempts),
			Status:    run.StatusPath,
		},
		Authorization: authorization,
		Identity:      identity,
		Resume:        resumeCapability(run, latestSession),
	}, nil
}

func runsInspectCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	inspection, err := buildRunInspection(store, run)
	if err != nil {
		return err
	}
	run = inspection.Run
	attempts := inspection.Attempts
	turns := inspection.Turns
	if args.Bool("json") {
		emitJSON(inspection)
		return nil
	}
	fmt.Printf("%s (%s)\n", firstNonEmpty(run.ItemID, run.RecordID), run.Runner)
	fmt.Printf("lease=%s outcome=%s lane=%s rev=%d attempts=%d pid=%d\n", run.LeaseState, run.AttemptOutcome, firstNonEmpty(run.Lane, runLaneExecute), run.WorkRevision, run.AttemptCount, run.ProcessPID)
	if inspection.FailureClass != "" {
		fmt.Printf("failure_class=%s\n", inspection.FailureClass)
	}
	fmt.Printf("tokens total=%d input=%d output=%d\n", inspection.TokenTotals.TotalTokens, inspection.TokenTotals.InputTokens, inspection.TokenTotals.OutputTokens)
	fmt.Printf("turns=%d", len(turns))
	if latest := inspection.LatestTurn; latest != nil {
		fmt.Printf(" latest=%s status=%s last_event=%s tokens=%d", latest.TurnID, latest.Status, latest.LastEventAt, latest.TotalTokens)
	}
	if active := inspection.ActiveTurn; active != nil {
		fmt.Printf(" active=%s", active.TurnID)
	}
	fmt.Println()
	if run.SessionRef != "" {
		fmt.Printf("session=%s\n", run.SessionRef)
	}
	if run.CloudTaskID != "" {
		fmt.Printf("cloud_task=%s", run.CloudTaskID)
		if run.CloudStatus != "" {
			fmt.Printf(" status=%s", run.CloudStatus)
		}
		if run.CloudEnvironmentID != "" {
			fmt.Printf(" environment=%s", run.CloudEnvironmentID)
		}
		if run.CloudAttemptNumber > 0 {
			fmt.Printf(" attempt=%d", run.CloudAttemptNumber)
		}
		fmt.Println()
	}
	if run.PullRequestURL != "" {
		fmt.Printf("pull_request=%s\n", run.PullRequestURL)
	}
	if run.ApplyRef != "" {
		fmt.Printf("apply_ref=%s\n", run.ApplyRef)
	}
	if run.WorkspacePath != "" {
		fmt.Printf("workspace=%s\n", run.WorkspacePath)
	}
	if run.RawLogPath != "" {
		fmt.Printf("log=%s\n", run.RawLogPath)
	}
	if run.EventSinkPath != "" {
		fmt.Printf("events=%s\n", run.EventSinkPath)
	}
	if run.PromptPath != "" {
		fmt.Printf("prompt=%s\n", run.PromptPath)
	}
	if run.StatusPath != "" {
		fmt.Printf("status=%s\n", run.StatusPath)
	}
	if inspection.LatestEvent != nil {
		fmt.Printf("latest event=%s at=%s\n", stringValue(inspection.LatestEvent["kind"]), stringValue(inspection.LatestEvent["at"]))
	}
	if inspection.LatestSession != nil {
		fmt.Printf("latest session state=%s resumable=%t last_seen=%s\n", inspection.LatestSession.State, inspection.LatestSession.Resumable, inspection.LatestSession.LastSeenAt)
	}
	if inspection.ExternalLoopCounters.Events > 0 {
		fmt.Printf("external loop events=%d cycles=%d repairs=%d threads=%d\n", inspection.ExternalLoopCounters.Events, inspection.ExternalLoopCounters.Cycles, inspection.ExternalLoopCounters.RepairContinuations, inspection.ExternalLoopCounters.ExternalThreads)
	}
	if latest := inspection.LatestSupervisorDecision; latest != nil {
		fmt.Printf("latest supervisor decision=%s", latest.Kind)
		if latest.Reason != "" {
			fmt.Printf(" reason=%q", latest.Reason)
		}
		if latest.AttemptID != "" {
			fmt.Printf(" attempt=%s", latest.AttemptID)
		}
		if latest.ParentAttemptID != "" {
			fmt.Printf(" parent_attempt=%s", latest.ParentAttemptID)
		}
		if latest.BranchName != "" {
			fmt.Printf(" branch=%s", latest.BranchName)
		}
		if latest.WorkspacePath != "" {
			fmt.Printf(" workspace=%s", latest.WorkspacePath)
		}
		if latest.ValidationDelta != "" {
			fmt.Printf(" validation_delta=%q", latest.ValidationDelta)
		}
		if latest.MergeRule != "" {
			fmt.Printf(" merge_rule=%q", latest.MergeRule)
		}
		if latest.ContextSignal != "" {
			fmt.Printf(" signal=%s", latest.ContextSignal)
		}
		if latest.TotalTokens > 0 {
			fmt.Printf(" tokens=%d", latest.TotalTokens)
		}
		if latest.CreatedAt != "" {
			fmt.Printf(" at=%s", latest.CreatedAt)
		}
		fmt.Println()
	}
	fmt.Printf("attempt history=%d\n", len(attempts))
	return nil
}

func latestRunTurn(turns []RunTurn) *RunTurn {
	var latest *RunTurn
	for i := range turns {
		turn := &turns[i]
		if latest == nil {
			latest = turn
			continue
		}
		if turn.LastEventAt > latest.LastEventAt || (turn.LastEventAt == latest.LastEventAt && turn.TurnIndex > latest.TurnIndex) {
			latest = turn
		}
	}
	return latest
}

func activeRunTurn(turns []RunTurn) *RunTurn {
	var active *RunTurn
	for i := range turns {
		turn := &turns[i]
		if strings.TrimSpace(turn.Status) != "running" {
			continue
		}
		if active == nil || turn.LastEventAt > active.LastEventAt || (turn.LastEventAt == active.LastEventAt && turn.TurnIndex > active.TurnIndex) {
			active = turn
		}
	}
	return active
}

func latestSupervisorDecision(decisions []RuntimeSupervisorDecision) *RuntimeSupervisorDecision {
	var latest *RuntimeSupervisorDecision
	for i := range decisions {
		decision := &decisions[i]
		if latest == nil {
			latest = decision
			continue
		}
		if decision.CreatedAt > latest.CreatedAt || (decision.CreatedAt == latest.CreatedAt && decision.DecisionID > latest.DecisionID) {
			latest = decision
		}
	}
	return latest
}

func latestRunnerSession(sessions []RunnerSession) *RunnerSession {
	if len(sessions) == 0 {
		return nil
	}
	return &sessions[0]
}

func tokenTotalsForTurns(turns []RunTurn) runtimeTokenTotals {
	var totals runtimeTokenTotals
	for _, turn := range turns {
		totals.InputTokens += turn.InputTokens
		totals.OutputTokens += turn.OutputTokens
		totals.TotalTokens += turn.TotalTokens
	}
	if totals.TotalTokens == 0 && (totals.InputTokens > 0 || totals.OutputTokens > 0) {
		totals.TotalTokens = totals.InputTokens + totals.OutputTokens
	}
	return totals
}

func bestRunEventPath(run RunStatus, attempts []RunAttempt) string {
	if strings.TrimSpace(run.EventSinkPath) != "" {
		return run.EventSinkPath
	}
	for _, attempt := range attempts {
		if strings.TrimSpace(attempt.EventSinkPath) != "" {
			return attempt.EventSinkPath
		}
	}
	return ""
}

func bestRunLogPath(run RunStatus, attempts []RunAttempt) string {
	if strings.TrimSpace(run.RawLogPath) != "" {
		return run.RawLogPath
	}
	for _, attempt := range attempts {
		if strings.TrimSpace(attempt.RawLogPath) != "" {
			return attempt.RawLogPath
		}
	}
	return ""
}

func latestJSONLEvent(path string) map[string]any {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	content, err := readText(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil {
			return event
		}
	}
	return nil
}

func runtimeFailureClass(run RunStatus, attempts []RunAttempt, turns []RunTurn) string {
	reason := strings.TrimSpace(run.LastError)
	for _, attempt := range attempts {
		if reason != "" {
			break
		}
		reason = strings.TrimSpace(attempt.LastError)
	}
	for i := len(turns) - 1; i >= 0 && reason == ""; i-- {
		reason = strings.TrimSpace(turns[i].LastError)
	}
	outcome := strings.TrimSpace(run.AttemptOutcome)
	for _, attempt := range attempts {
		if outcome != "" && outcome != string(AttemptOutcomeNone) {
			break
		}
		outcome = strings.TrimSpace(attempt.Outcome)
	}
	text := strings.ToLower(reason)
	switch {
	case LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateParkedNoProgress:
		return "no_progress"
	case text == "" && (outcome == "" || outcome == string(AttemptOutcomeNone) || outcome == string(AttemptOutcomeSucceeded)):
		return ""
	case strings.Contains(text, "context window") || strings.Contains(text, "context-window") || strings.Contains(text, "context length") || strings.Contains(text, "maximum context") || strings.Contains(text, "context limit"):
		return "context_window"
	case strings.Contains(text, "auth") || strings.Contains(text, "api key") || strings.Contains(text, "token expired") || strings.Contains(text, "invalid token") || strings.Contains(text, "login required") || strings.Contains(text, "not logged in") || strings.Contains(text, "unauthorized") || strings.Contains(text, "forbidden"):
		return "auth"
	case strings.Contains(text, "approval") || strings.Contains(text, "permission denied") || strings.Contains(text, "sandbox"):
		return "policy"
	case strings.Contains(text, "config") || strings.Contains(text, "unsupported runner") || strings.Contains(text, "command is empty") || strings.Contains(text, "invalid workflow"):
		return "config"
	case strings.Contains(text, "budget") || strings.Contains(text, "quota") || strings.Contains(text, "spend limit") || strings.Contains(text, "rate limit budget"):
		return "budget"
	case strings.Contains(text, "deterministic") || strings.Contains(text, "validation failed") || strings.Contains(text, "invalid request") || strings.Contains(text, "bad request"):
		return "deterministic"
	case strings.Contains(text, "stalled") || strings.Contains(text, "timeout") || strings.Contains(text, "timed out"):
		return "runner_stall"
	case strings.Contains(text, "interrupt") || outcome == string(AttemptOutcomeCancelled):
		return "operator_interrupt"
	case strings.Contains(text, "early exit") || outcome == string(AttemptOutcomeEarlyExit):
		return "runner_early_exit"
	case strings.Contains(text, "dispatch declined") || outcome == string(AttemptOutcomeDispatchDeclined):
		return "dispatch_declined"
	case strings.Contains(text, "awaiting land") || outcome == string(AttemptOutcomeWaitingForReview):
		return "review_complete"
	case strings.Contains(text, "turn cap exhausted") || outcome == string(AttemptOutcomeTurnCapExhausted):
		return "turn_cap_exhausted"
	case strings.Contains(text, "missing session") || strings.Contains(text, "resume"):
		return "session"
	case strings.Contains(text, "exit") || outcome == string(AttemptOutcomeFailed):
		return "runner_failure"
	case outcome == string(AttemptOutcomeBlocked):
		return "blocked"
	case outcome == string(AttemptOutcomeWaitingForHuman):
		return "waiting_for_human"
	case outcome == string(AttemptOutcomeBudgetExceeded):
		return "budget"
	case outcome == string(AttemptOutcomeAbandoned):
		return "abandoned"
	default:
		return "unknown"
	}
}

func runsLogsCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	lines := 40
	if raw := strings.TrimSpace(args.String("lines")); raw != "" {
		lines = maxInt(1, atoiSafe(raw))
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	logPath := strings.TrimSpace(run.RawLogPath)
	if logPath == "" {
		attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			return err
		}
		if len(attempts) > 0 {
			logPath = attempts[0].RawLogPath
		}
	}
	if logPath == "" {
		return tuskerError(errorNotFound, "no log path recorded for "+identity)
	}
	if args.Bool("follow") {
		return followLog(logPath)
	}
	content, err := readText(logPath)
	if err != nil {
		return err
	}
	tail := tailText(content, lines)
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "path": logPath, "tail": tail, "running": runProcessGroupAlive(*run)})
		return nil
	}
	fmt.Print(tail)
	return nil
}

func runsEventsCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	lines := 40
	if raw := strings.TrimSpace(args.String("lines")); raw != "" {
		lines = maxInt(1, atoiSafe(raw))
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	eventPath := strings.TrimSpace(run.EventSinkPath)
	if eventPath == "" {
		attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
		if err != nil {
			return err
		}
		if len(attempts) > 0 {
			eventPath = attempts[0].EventSinkPath
		}
	}
	if eventPath == "" {
		return tuskerError(errorNotFound, "no event path recorded for "+identity)
	}
	if args.Bool("follow") {
		return followLog(eventPath)
	}
	content, err := readText(eventPath)
	if err != nil {
		return err
	}
	tail := tailText(content, lines)
	if args.Bool("json") {
		events := parseEventTail(tail)
		emitJSON(map[string]any{
			"ok":           true,
			"run":          run,
			"path":         eventPath,
			"event_path":   eventPath,
			"latest_event": latestJSONLEvent(eventPath),
			"events":       events,
			"tail":         tail,
			"running":      runProcessGroupAlive(*run),
		})
		return nil
	}
	fmt.Print(tail)
	return nil
}

func runsInterruptCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	run, viaDaemon, err := interruptRuntimeRun(DefaultStateRoot(), nil, identity)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":              true,
			"interrupted":     true,
			"item_id":         run.ItemID,
			"record_id":       run.RecordID,
			"lease_state":     run.LeaseState,
			"process_running": runProcessGroupAlive(*run),
			"via_daemon":      viaDaemon,
		})
		return nil
	}
	if viaDaemon {
		fmt.Printf("Sent interrupt to %s via daemon control\n", firstNonEmpty(run.ItemID, run.RecordID, identity))
		return nil
	}
	fmt.Printf("Sent interrupt to %s\n", firstNonEmpty(run.ItemID, run.RecordID, identity))
	return nil
}

// interruptRuntimeRun is the single operator interrupt path shared by the CLI
// and Serve. A live daemon gets first ownership of its in-memory runner handle;
// otherwise the runtime store path signals a verified process or retires a dead
// process row with the same canonical interrupted outcome.
func interruptRuntimeRun(stateRoot string, store *RuntimeStore, identity string) (*RunStatus, bool, error) {
	return interruptRuntimeRunWithHook(stateRoot, store, identity, nil)
}

func interruptRuntimeRunWithHook(stateRoot string, store *RuntimeStore, identity string, afterRead func()) (*RunStatus, bool, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return nil, false, tuskerError(errorInvalidArg, "run identity is required")
	}

	if readDaemonLiveness(stateRoot, time.Now().UTC()).Alive {
		if resp, err := sendDaemonControl(stateRoot, daemonControlRequest{Command: "interrupt", Identity: identity}); err == nil {
			if !resp.OK {
				return nil, true, tuskerError(errorHookFailed, firstNonEmpty(resp.Message, "daemon interrupt failed"))
			}
			run, err := findInterruptRun(stateRoot, store, identity)
			return run, true, err
		}
	}

	run, ownedStore, err := findInterruptRunWithStore(stateRoot, store, identity)
	if err != nil {
		return nil, false, err
	}
	if ownedStore != nil {
		defer ownedStore.Close()
		store = ownedStore
	}
	if afterRead != nil {
		afterRead()
	}
	handle := matchingLiveRunHandle(*run)
	if handle != nil {
		if err = handle.Interrupt(context.Background()); err == nil {
			err = interruptRunProcess(store, run, true)
		}
	} else {
		err = interruptRunProcess(store, run, false)
	}
	return run, false, err
}

func findInterruptRun(stateRoot string, store *RuntimeStore, identity string) (*RunStatus, error) {
	run, ownedStore, err := findInterruptRunWithStore(stateRoot, store, identity)
	if ownedStore != nil {
		defer ownedStore.Close()
	}
	return run, err
}

func findInterruptRunWithStore(stateRoot string, store *RuntimeStore, identity string) (*RunStatus, *RuntimeStore, error) {
	ownedStore := (*RuntimeStore)(nil)
	if store == nil {
		var err error
		ownedStore, err = OpenRuntimeStore(stateRoot)
		if err != nil {
			return nil, nil, err
		}
		store = ownedStore
	}
	run, err := store.FindRun(identity)
	if err != nil {
		if ownedStore != nil {
			_ = ownedStore.Close()
		}
		return nil, nil, err
	}
	if run == nil {
		if ownedStore != nil {
			_ = ownedStore.Close()
		}
		return nil, nil, tuskerError(errorNotFound, "run not found: "+identity)
	}
	return run, ownedStore, nil
}

func runsReleaseCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	if runProcessGroupAlive(*run) {
		return tuskerError(errorInvalidTransition, "run process is still running; use tusker runs interrupt before release", withContext(map[string]any{"pid": run.ProcessPID}))
	}
	reason := firstNonEmpty(strings.TrimSpace(args.String("reason")), "released dead run by operator")
	if err := finishRuntimeRun(store, run, LeaseStateReleased, AttemptOutcomeAbandoned, 0, reason, false); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "released": true, "item_id": run.ItemID, "record_id": run.RecordID})
		return nil
	}
	fmt.Printf("Released %s\n", firstNonEmpty(run.ItemID, run.RecordID))
	return nil
}

func runsRetireCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	reason := strings.TrimSpace(args.String("reason"))
	if reason == "" {
		return tuskerError(errorMissingArg, "runs retire requires --reason <text>", withHint("retirement is terminal operator action; record why the run is being retired"))
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	now := time.Now().UTC()
	if !args.Bool("force") && runHasFreshLiveHeartbeat(*run, now) {
		return tuskerError(errorInvalidTransition, "run has a fresh heartbeat and verified-live pid; use tusker runs interrupt before retire, or pass --force for an explicit operator retirement", withContext(map[string]any{"pid": run.ProcessPID, "last_heartbeat_at": run.LastHeartbeatAt}))
	}
	actor := firstNonEmpty(strings.TrimSpace(args.String("by")), strings.TrimSpace(args.String("actor")), defaultActorName())
	previousLease := run.LeaseState
	previousOutcome := run.AttemptOutcome
	retired, err := retireRuntimeRun(store, DefaultStateRoot(), *run, actor, reason, now, args.Bool("force"))
	if err != nil {
		return err
	}
	run = &retired
	if args.Bool("json") {
		emitJSON(map[string]any{
			"ok":               true,
			"retired":          true,
			"item_id":          run.ItemID,
			"record_id":        run.RecordID,
			"actor":            actor,
			"reason":           reason,
			"lease_state":      run.LeaseState,
			"attempt_outcome":  run.AttemptOutcome,
			"terminal":         run.Terminal,
			"event_sink_path":  run.EventSinkPath,
			"previous_lease":   previousLease,
			"previous_outcome": previousOutcome,
			"force":            args.Bool("force"),
		})
		return nil
	}
	fmt.Printf("Retired %s by %s: %s\n", firstNonEmpty(run.ItemID, run.RecordID), actor, reason)
	return nil
}

func retireRuntimeRun(store *RuntimeStore, stateRoot string, run RunStatus, actor, reason string, now time.Time, forced bool) (RunStatus, error) {
	if store == nil {
		return run, tuskerError(errorConfigInvalid, "runtime store is required")
	}
	actor = firstNonEmpty(strings.TrimSpace(actor), defaultActorName())
	reason = firstNonEmpty(strings.TrimSpace(reason), "runtime row retired")
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	previousLease := run.LeaseState
	previousOutcome := run.AttemptOutcome
	outcome := retiredRunOutcome(run.AttemptOutcome)
	nowText := now.Format(time.RFC3339)
	attempts, _ := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	eventPath := bestRunEventPath(run, attempts)
	if eventPath == "" {
		eventPath = fallbackRetireEventPath(stateRoot, run, now)
	}
	if eventPath != "" {
		run.EventSinkPath = eventPath
		if err := appendRunRetiredEvent(eventPath, run, actor, reason, previousLease, previousOutcome, forced); err != nil {
			return run, err
		}
	}
	updateRunAttemptFromRun(store, run, outcome, exitCodeForOutcome(outcome), "retired by "+actor+": "+reason, nowText)
	run.LeaseState = string(LeaseStateReleased)
	run.AttemptOutcome = string(outcome)
	run.NextRetryAt = ""
	run.LastError = "retired by " + actor + ": " + reason
	run.UpdatedAt = nowText
	run.Terminal = true
	clearRetiredRunExecution(&run)
	run.EventSinkPath = eventPath
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = store.MarkSessionState(run.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateReleased), nowText, run.LastError, false)
	}
	return run, store.UpsertRun(run)
}

func clearRetiredRunExecution(run *RunStatus) {
	run.ActiveAttemptID = ""
	run.LeaseOwner = ""
	run.LeaseExpiresAt = ""
	run.ProcessPID = 0
	run.ProcessPGID = 0
	run.ProcessStartedAt = ""
}

func retiredRunOutcome(outcome string) AttemptOutcome {
	trimmed := AttemptOutcome(strings.TrimSpace(outcome))
	if trimmed == "" || trimmed == AttemptOutcomeNone {
		return AttemptOutcomeAbandoned
	}
	return trimmed
}

func runHasFreshLiveHeartbeat(run RunStatus, now time.Time) bool {
	heartbeatAt, ok := parseRunTimestamp(run.LastHeartbeatAt)
	return ok && now.Sub(heartbeatAt) <= daemonHeartbeatDeadThreshold && processIdentityMatches(run)
}

func fallbackRetireEventPath(stateRoot string, run RunStatus, now time.Time) string {
	project := sanitizeProjectID(firstNonEmpty(run.ProjectID, "project"))
	record := sanitizeProjectID(firstNonEmpty(run.RecordID, run.ItemID, "run"))
	dir := filepath.Join(stateRoot, "runs", project, record)
	if err := ensureDir(dir); err != nil {
		return ""
	}
	return filepath.Join(dir, "retire-"+now.Format("20060102T150405Z")+".events.jsonl")
}

func appendRunRetiredEvent(path string, run RunStatus, actor, reason, previousLease, previousOutcome string, forced bool) error {
	return NewEventLog(path).Append("run_retired", firstNonEmpty(run.ActiveAttemptID, run.RecordID), RunnerName(run.Runner), map[string]any{
		"project_id":       run.ProjectID,
		"record_id":        run.RecordID,
		"item_id":          run.ItemID,
		"actor":            actor,
		"reason":           reason,
		"previous_lease":   previousLease,
		"previous_outcome": previousOutcome,
		"force":            forced,
	})
}

func redriveCmd(args Args) error {
	identity, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	run, err := store.FindRun(identity)
	if err != nil {
		return err
	}
	if run == nil {
		return tuskerError(errorNotFound, "run not found: "+identity)
	}
	if runProcessGroupAlive(*run) {
		return tuskerError(errorInvalidTransition, "run process is still running; interrupt it before redrive", withContext(map[string]any{"pid": run.ProcessPID}))
	}
	now := time.Now().UTC()
	actor := firstNonEmpty(strings.TrimSpace(args.String("by")), strings.TrimSpace(args.String("actor")), defaultActorName())
	reason := firstNonEmpty(strings.TrimSpace(args.String("reason")), "operator redrive")
	previousAttemptCount := run.AttemptCount
	previousLeaseState := run.LeaseState
	previousAttemptOutcome := run.AttemptOutcome
	previousAttemptID := run.ActiveAttemptID
	previousSessionRef := run.SessionRef
	previousWorkspacePath := run.WorkspacePath
	reset, err := redriveRuntimeRun(store, run, actor, reason, now)
	if err != nil {
		return err
	}
	auditPayload, err := json.Marshal(map[string]any{
		"actor":                    actor,
		"reason":                   reason,
		"reset_at":                 reset.ResetAt,
		"previous_attempt_count":   previousAttemptCount,
		"previous_lease_state":     previousLeaseState,
		"previous_attempt_outcome": previousAttemptOutcome,
	})
	if err != nil {
		return err
	}
	if _, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID:        run.ProjectID,
		RecordID:         run.RecordID,
		ItemID:           run.ItemID,
		Runner:           run.Runner,
		WorkRevision:     run.WorkRevision,
		AttemptID:        previousAttemptID,
		ParentAttemptID:  previousAttemptID,
		SessionRef:       previousSessionRef,
		ParentSessionRef: previousSessionRef,
		Kind:             string(SupervisorDecisionRedrive),
		Reason:           reason,
		WorkspacePath:    previousWorkspacePath,
		ValidationDelta:  string(auditPayload),
		LeaseState:       run.LeaseState,
		ContextSignal:    "operator_redrive",
		CreatedAt:        now.Format(time.RFC3339),
	}); err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "redriven": true, "item_id": run.ItemID, "record_id": run.RecordID, "reset": reset, "lease_state": run.LeaseState})
		return nil
	}
	fmt.Printf("Redriven %s; budget window reset at %s\n", firstNonEmpty(run.ItemID, run.RecordID), reset.ResetAt)
	return nil
}

func redriveRuntimeRun(store *RuntimeStore, run *RunStatus, actor, reason string, now time.Time) (BudgetRedriveRecord, error) {
	return redriveRuntimeRunWithHook(store, run, actor, reason, now, nil)
}

func redriveRuntimeRunWithHook(store *RuntimeStore, run *RunStatus, actor, reason string, now time.Time, afterRead func()) (BudgetRedriveRecord, error) {
	if store == nil || run == nil {
		return BudgetRedriveRecord{}, tuskerError(errorNotFound, "run not found")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	actor = firstNonEmpty(strings.TrimSpace(actor), defaultActorName())
	reason = firstNonEmpty(strings.TrimSpace(reason), "operator redrive")
	expected := *run
	if afterRead != nil {
		afterRead()
	}
	redriven := expected
	redriven.LeaseState = string(LeaseStateRetryQueued)
	redriven.AttemptOutcome = string(AttemptOutcomeNone)
	redriven.AttemptCount = 0
	redriven.NextRetryAt = now.Format(time.RFC3339)
	redriven.LastError = "redriven by " + actor + ": " + reason
	redriven.LastEventAt = now.Format(time.RFC3339)
	redriven.UpdatedAt = now.Format(time.RFC3339)
	redriven.Terminal = false
	clearActiveExecution(&redriven)
	clearRunCloudRefs(&redriven)
	reset := BudgetRedriveRecord{Actor: actor, Reason: reason, ResetAt: now.Format(time.RFC3339)}
	updated, err := store.RedriveRunIfSnapshot(expected, redriven, reset)
	if err != nil {
		return BudgetRedriveRecord{}, err
	}
	if !updated {
		return BudgetRedriveRecord{}, tuskerError("CAS_CONFLICT", "run changed while redrive was being applied: "+firstNonEmpty(expected.ItemID, expected.RecordID), withHint("reload the run and retry; Tusker did not clear the newer lease or reset its budget window"))
	}
	*run = redriven
	return reset, nil
}

func parseEventTail(tail string) []map[string]any {
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(tail), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil {
			events = append(events, event)
		}
	}
	return events
}

func tailText(content string, lines int) string {
	rows := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) > lines {
		rows = rows[len(rows)-lines:]
	}
	if len(rows) == 0 {
		return ""
	}
	return strings.Join(rows, "\n") + "\n"
}

func followLog(path string) error {
	var offset int64
	for {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Size() > offset {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := file.Seek(offset, 0); err != nil {
				_ = file.Close()
				return err
			}
			if _, err := ioCopyStdout(file); err != nil {
				_ = file.Close()
				return err
			}
			offset = info.Size()
			_ = file.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func ioCopyStdout(file *os.File) (int64, error) {
	return io.Copy(os.Stdout, file)
}
