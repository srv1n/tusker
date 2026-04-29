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
	rows, err := s.db.Query(`SELECT attempt_id, project_id, record_id, item_id, runner, work_revision, workspace_path, session_ref, process_pid, outcome, exit_code, prompt_path, event_sink_path, raw_log_path, status_path, last_error, started_at, finished_at
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
		if err := rows.Scan(&attempt.AttemptID, &attempt.ProjectID, &attempt.RecordID, &attempt.ItemID, &attempt.Runner, &attempt.WorkRevision, &attempt.WorkspacePath, &attempt.SessionRef, &attempt.ProcessPID, &attempt.Outcome, &attempt.ExitCode, &attempt.PromptPath, &attempt.EventSinkPath, &attempt.RawLogPath, &attempt.StatusPath, &attempt.LastError, &attempt.StartedAt, &attempt.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
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
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	turns, err := store.ListTurnsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	supervisorDecisions, err := store.ListRuntimeSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	session, err := store.LatestSession(run.ProjectID, run.RecordID, run.Runner)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "run": run, "attempts": attempts, "turns": turns, "supervisor_decisions": supervisorDecisions, "session": session})
		return nil
	}
	fmt.Printf("%s (%s)\n", firstNonEmpty(run.ItemID, run.RecordID), run.Runner)
	fmt.Printf("lease=%s outcome=%s rev=%d attempts=%d pid=%d\n", run.LeaseState, run.AttemptOutcome, run.WorkRevision, run.AttemptCount, run.ProcessPID)
	fmt.Printf("turns=%d", len(turns))
	if latest := latestRunTurn(turns); latest != nil {
		fmt.Printf(" latest=%s status=%s last_event=%s tokens=%d", latest.TurnID, latest.Status, latest.LastEventAt, latest.TotalTokens)
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
	if session != nil {
		fmt.Printf("latest session state=%s resumable=%t last_seen=%s\n", session.State, session.Resumable, session.LastSeenAt)
	}
	if latest := latestSupervisorDecision(supervisorDecisions); latest != nil {
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
		emitJSON(map[string]any{"ok": true, "path": eventPath, "events": events, "tail": tail, "running": run.ProcessPID > 0 && processExists(run.ProcessPID)})
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
