package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func serveRunHistory(s *serveServer, snap serveSnapshot, taskID string) []serveRunSummary {
	out := []serveRunSummary{}
	for _, run := range snap.runs {
		if run.ItemID == taskID || run.RecordID == taskID {
			out = append(out, s.runSummary(snap, run))
		}
	}
	return out
}

func (s *serveServer) runSummary(snap serveSnapshot, run RunStatus) serveRunSummary {
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	taskTitle := taskID
	if task, ok := snap.notesByID[taskID]; ok {
		taskTitle = firstNonEmpty(stringField(task.Data, "title"), taskID)
	}
	turns, _ := s.store.ListTurnsForRun(run.ProjectID, run.RecordID)
	return serveRunSummary{
		TaskID:            taskID,
		TaskTitle:         taskTitle,
		ProjectID:         firstNonEmpty(run.ProjectID, snap.projectID),
		Runner:            serveRunner(run.Runner),
		RunnerName:        run.Runner,
		Model:             nil,
		Lane:              serveLane(run.Lane),
		LeaseState:        serveLeaseState(run.LeaseState),
		LeaseStateRaw:     run.LeaseState,
		Outcome:           serveRunOutcome(run),
		ElapsedSec:        serveDurationSec(run.StartedAt, run.UpdatedAt, s.now()),
		SinceLastEventSec: serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), s.now()),
		Liveness:          serveRunLiveness(run, s.now()),
		Tokens:            serveTokenTotalsForTurns(turns),
		AttemptCount:      maxInt(run.AttemptCount, len(turnsByAttempt(turns))),
		Terminal:          nil,
		Error:             nullIfBlank(run.LastError),
		LastHeartbeatAt:   nil,
		NextWakeAt:        nullIfBlank(run.NextRetryAt),
	}
}

func serveFindRun(runs []RunStatus, id string) (RunStatus, bool) {
	for _, run := range runs {
		if run.ItemID == id || run.RecordID == id {
			return run, true
		}
	}
	return RunStatus{}, false
}

func serveRunner(runner string) string {
	switch {
	case strings.Contains(runner, "claude"):
		return "claude"
	default:
		return "codex"
	}
}

func serveLane(lane string) string {
	if strings.Contains(strings.ToLower(lane), "review") {
		return "review"
	}
	return "execute"
}

func serveLeaseState(state string) string {
	switch LeaseState(strings.TrimSpace(state)) {
	case LeaseStateReleased, LeaseStateInterrupted, LeaseStateParkedNoProgress:
		return "released"
	default:
		return "held"
	}
}

func serveRunOutcome(run RunStatus) string {
	if LeaseState(run.LeaseState) == LeaseStateParkedNoProgress {
		return "parked-no-progress"
	}
	if LeaseState(run.LeaseState) == LeaseStateRetryQueued {
		return "retry-queued"
	}
	if isDispatchingLeaseState(run.LeaseState) {
		return "running"
	}
	return serveRunOutcomeFromAttempt(run.AttemptOutcome, run.LeaseState)
}

func serveRunOutcomeFromAttempt(outcome, lease string) string {
	switch AttemptOutcome(strings.TrimSpace(outcome)) {
	case AttemptOutcomeSucceeded:
		return "succeeded"
	case AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeAbandoned:
		return "failed"
	case AttemptOutcomeCancelled:
		return "interrupted"
	default:
		if LeaseState(lease) == LeaseStateParkedNoProgress {
			return "parked-no-progress"
		}
		if LeaseState(lease) == LeaseStateRetryQueued {
			return "retry-queued"
		}
		return "running"
	}
}

func serveRunLiveness(run RunStatus, now time.Time) string {
	age := serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), now)
	switch {
	case age < 60:
		return "fresh"
	case age < 120:
		return "stale"
	default:
		return "dead"
	}
}

func serveWorstLiveness(current any, next string) any {
	rank := map[string]int{"fresh": 0, "stale": 1, "dead": 2}
	if current == nil {
		return next
	}
	if rank[next] > rank[fmt.Sprint(current)] {
		return next
	}
	return current
}

func serveSinceSec(value string, now time.Time) int {
	if value == "" {
		return 0
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return maxInt(0, int(now.Sub(ts).Seconds()))
	}
	return 0
}

func serveDurationSec(start, end string, now time.Time) int {
	if start == "" {
		return 0
	}
	startAt, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return 0
	}
	endAt := now
	if end != "" {
		if parsed, err := time.Parse(time.RFC3339, end); err == nil {
			endAt = parsed
		}
	}
	return maxInt(0, int(endAt.Sub(startAt).Seconds()))
}

func serveTokenTotalsForTurns(turns []RunTurn) serveTokenTotals {
	var total serveTokenTotals
	for _, turn := range turns {
		total.Input += turn.InputTokens
		total.Output += turn.OutputTokens
	}
	return total
}

func turnsByAttempt(turns []RunTurn) map[string]struct{} {
	out := map[string]struct{}{}
	for _, turn := range turns {
		if turn.AttemptID != "" {
			out[turn.AttemptID] = struct{}{}
		}
	}
	return out
}

func serveRunEvents(run RunStatus, attempts []RunAttempt) []serveRunEvent {
	eventPath := bestRunEventPath(run, attempts)
	if eventPath == "" {
		return []serveRunEvent{}
	}
	file, err := os.Open(eventPath)
	if err != nil {
		return []serveRunEvent{}
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 256*1024))
	if err != nil {
		return []serveRunEvent{}
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
	}
	out := []serveRunEvent{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(line), &payload) != nil {
			continue
		}
		out = append(out, serveRunEvent{
			TS:    firstNonEmpty(toString(payload["ts"]), toString(payload["timestamp"]), toString(payload["time"])),
			Kind:  firstNonEmpty(toString(payload["kind"]), toString(payload["event"]), "event"),
			Text:  firstNonEmpty(toString(payload["text"]), toString(payload["message"]), toString(payload["summary"])),
			Level: firstNonEmpty(toString(payload["level"]), toString(payload["severity"])),
		})
	}
	return out
}
