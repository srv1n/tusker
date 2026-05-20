package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
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
	LatestEvent              map[string]any              `json:"latest_event,omitempty"`
	TokenTotals              runtimeTokenTotals          `json:"token_totals"`
	FailureClass             string                      `json:"failure_class,omitempty"`
	Paths                    runtimeArtifactPaths        `json:"paths"`
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
	rows, err := s.db.Query(`SELECT attempt_id, project_id, record_id, item_id, runner, lane, work_revision, workspace_path, session_ref, process_pid, outcome, exit_code, prompt_path, event_sink_path, raw_log_path, status_path, last_error, started_at, finished_at
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
		if err := rows.Scan(&attempt.AttemptID, &attempt.ProjectID, &attempt.RecordID, &attempt.ItemID, &attempt.Runner, &attempt.Lane, &attempt.WorkRevision, &attempt.WorkspacePath, &attempt.SessionRef, &attempt.ProcessPID, &attempt.Outcome, &attempt.ExitCode, &attempt.PromptPath, &attempt.EventSinkPath, &attempt.RawLogPath, &attempt.StatusPath, &attempt.LastError, &attempt.StartedAt, &attempt.FinishedAt); err != nil {
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
	latestSession := latestRunnerSession(sessions)
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
	case strings.Contains(text, "missing session") || strings.Contains(text, "resume"):
		return "session"
	case strings.Contains(text, "exit") || outcome == string(AttemptOutcomeFailed):
		return "runner_failure"
	case outcome == string(AttemptOutcomeBlocked):
		return "blocked"
	case outcome == string(AttemptOutcomeWaitingForHuman):
		return "waiting_for_human"
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
		emitJSON(map[string]any{"ok": true, "path": logPath, "tail": tail, "running": run.ProcessPID > 0 && processExists(run.ProcessPID)})
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
			"running":      run.ProcessPID > 0 && processExists(run.ProcessPID),
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
	if resp, err := sendDaemonControl(DefaultStateRoot(), daemonControlRequest{Command: "interrupt", Identity: identity}); err == nil {
		if !resp.OK {
			return tuskerError(errorHookFailed, firstNonEmpty(resp.Message, "daemon interrupt failed"))
		}
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "interrupted": true, "item_id": identity})
			return nil
		}
		fmt.Printf("Sent interrupt to %s via daemon control\n", identity)
		return nil
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
	if run.ProcessPID <= 0 || !processExists(run.ProcessPID) {
		if args.Bool("json") {
			emitJSON(map[string]any{"ok": true, "interrupted": false, "reason": "process not running"})
			return nil
		}
		fmt.Println("Process is not running")
		return nil
	}
	if err := syscall.Kill(-run.ProcessPID, syscall.SIGINT); err != nil && !strings.Contains(err.Error(), "no such process") {
		return err
	}
	for i := 0; i < 6 && processExists(run.ProcessPID); i++ {
		time.Sleep(150 * time.Millisecond)
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-run.ProcessPID, syscall.SIGTERM)
		for i := 0; i < 4 && processExists(run.ProcessPID); i++ {
			time.Sleep(150 * time.Millisecond)
		}
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-run.ProcessPID, syscall.SIGKILL)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	updateRunAttemptFromRun(store, *run, AttemptOutcomeCancelled, 130, "interrupt requested by operator", now)
	run.LeaseState = string(LeaseStateInterrupted)
	run.AttemptOutcome = string(AttemptOutcomeCancelled)
	run.NextRetryAt = ""
	run.LastError = "interrupt requested by operator"
	run.UpdatedAt = now
	clearActiveExecution(run)
	if err := store.UpsertRun(*run); err != nil {
		return err
	}
	if strings.TrimSpace(run.SessionRef) != "" {
		_ = store.MarkSessionState(run.ProjectID, run.SessionRef, sessionStateForLeaseState(LeaseStateInterrupted), "", run.LastError, true)
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "interrupted": true, "item_id": run.ItemID, "record_id": run.RecordID, "pid": run.ProcessPID})
		return nil
	}
	fmt.Printf("Sent interrupt to %s (pid %d)\n", firstNonEmpty(run.ItemID, run.RecordID), run.ProcessPID)
	return nil
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
