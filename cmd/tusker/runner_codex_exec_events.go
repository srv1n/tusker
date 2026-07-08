package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type codexExecTurnSnapshot struct {
	index        int
	status       string
	inputTokens  int
	outputTokens int
	totalTokens  int
}

type codexExecCommandSnapshot struct {
	id        string
	startedAt time.Time
}

func (d *Daemon) ingestCodexExecRawLog(run RunStatus) bool {
	if d == nil || d.store == nil || RunnerName(strings.TrimSpace(run.Runner)) != RunnerCodexExec || strings.TrimSpace(run.RawLogPath) == "" {
		return false
	}
	text, err := readText(run.RawLogPath)
	if err != nil || strings.TrimSpace(text) == "" {
		return false
	}
	turns, _ := d.store.ListTurnsForAttempt(run.ActiveAttemptID)
	known := map[string]codexExecTurnSnapshot{}
	nextIndex := 0
	for _, turn := range turns {
		known[turn.TurnID] = codexExecTurnSnapshot{
			index:        turn.TurnIndex,
			status:       turn.Status,
			inputTokens:  turn.InputTokens,
			outputTokens: turn.OutputTokens,
			totalTokens:  turn.TotalTokens,
		}
		if turn.TurnIndex >= nextIndex {
			nextIndex = turn.TurnIndex + 1
		}
	}
	eventLog := NewEventLog(run.EventSinkPath)
	threadStartedRecorded := codexExecEventRecorded(run.EventSinkPath, run.ActiveAttemptID, "thread_started")
	changed := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(line), &value) != nil {
			continue
		}
		payload, _ := value.(map[string]any)
		kind := normalizeCodexExecEventKind(stringValue(payload["type"]))
		if kind == "" {
			kind = normalizeCodexExecEventKind(stringValue(payload["event"]))
		}
		if kind == "" {
			kind = normalizeCodexExecEventKind(stringValue(payload["method"]))
		}
		at := firstNonEmpty(codexExecEventTime(payload), time.Now().UTC().Format(time.RFC3339))
		sessionRef := firstNonEmpty(findSessionRef(value), run.SessionRef)
		turnID := codexExecTurnID(value)
		usage := extractUsageCounters(json.RawMessage(line))
		if turnID == "" {
			turnID = usage.turnID
		}
		switch kind {
		case "threadstarted", "sessionstarted":
			if !threadStartedRecorded {
				_ = eventLog.Append("thread_started", run.ActiveAttemptID, RunnerCodexExec, map[string]any{
					"project_id":  run.ProjectID,
					"record_id":   run.RecordID,
					"item_id":     run.ItemID,
					"attempt_id":  run.ActiveAttemptID,
					"session_ref": sessionRef,
				})
				threadStartedRecorded = true
				changed = true
			}
		case "turnstarted":
			if turnID == "" {
				continue
			}
			snapshot, ok := known[turnID]
			if !ok {
				snapshot.index = nextIndex
				nextIndex++
			}
			if snapshot.status == "" {
				_ = eventLog.Append("turn_started", run.ActiveAttemptID, RunnerCodexExec, codexExecTurnPayload(run, sessionRef, turnID, snapshot.index, map[string]any{"source": "codex_exec"}))
				changed = true
				_ = d.store.SaveTurn(RunTurn{
					AttemptID:       run.ActiveAttemptID,
					ProjectID:       run.ProjectID,
					RecordID:        run.RecordID,
					TurnID:          turnID,
					TurnIndex:       snapshot.index,
					SessionRef:      sessionRef,
					Status:          "running",
					StartedAt:       at,
					LastEventAt:     at,
					LeaseGeneration: run.LeaseGeneration,
				})
			}
			snapshot.status = firstNonEmpty(snapshot.status, "running")
			known[turnID] = snapshot
		case "turncompleted":
			if turnID == "" {
				continue
			}
			snapshot, ok := known[turnID]
			if !ok {
				snapshot.index = nextIndex
				nextIndex++
			}
			status := firstNonEmpty(codexExecTurnStatus(value), "completed")
			if usage.hasAny() {
				snapshot.inputTokens = maxInt(snapshot.inputTokens, usage.inputTokens)
				snapshot.outputTokens = maxInt(snapshot.outputTokens, usage.outputTokens)
				snapshot.totalTokens = maxInt(snapshot.totalTokens, usage.totalTokens)
			}
			if snapshot.status != status {
				_ = eventLog.Append("turn_completed", run.ActiveAttemptID, RunnerCodexExec, codexExecTurnPayload(run, sessionRef, turnID, snapshot.index, map[string]any{
					"status":        status,
					"input_tokens":  snapshot.inputTokens,
					"output_tokens": snapshot.outputTokens,
					"total_tokens":  snapshot.totalTokens,
				}))
				changed = true
			}
			_ = d.store.SaveTurn(RunTurn{
				AttemptID:       run.ActiveAttemptID,
				ProjectID:       run.ProjectID,
				RecordID:        run.RecordID,
				TurnID:          turnID,
				TurnIndex:       snapshot.index,
				SessionRef:      sessionRef,
				Status:          status,
				InputTokens:     snapshot.inputTokens,
				OutputTokens:    snapshot.outputTokens,
				TotalTokens:     snapshot.totalTokens,
				CompletedAt:     at,
				LastEventAt:     at,
				LeaseGeneration: run.LeaseGeneration,
			})
			snapshot.status = status
			known[turnID] = snapshot
		default:
			if turnID == "" || !usage.hasAny() {
				continue
			}
			snapshot, ok := known[turnID]
			if !ok {
				snapshot.index = nextIndex
				nextIndex++
			}
			if usage.inputTokens > snapshot.inputTokens || usage.outputTokens > snapshot.outputTokens || usage.totalTokens > snapshot.totalTokens {
				snapshot.inputTokens = maxInt(snapshot.inputTokens, usage.inputTokens)
				snapshot.outputTokens = maxInt(snapshot.outputTokens, usage.outputTokens)
				snapshot.totalTokens = maxInt(snapshot.totalTokens, usage.totalTokens)
				_ = eventLog.Append("turn_usage_updated", run.ActiveAttemptID, RunnerCodexExec, codexExecTurnPayload(run, sessionRef, turnID, snapshot.index, map[string]any{
					"source":        "codex_exec",
					"input_tokens":  snapshot.inputTokens,
					"output_tokens": snapshot.outputTokens,
					"total_tokens":  snapshot.totalTokens,
				}))
				changed = true
			}
			_ = d.store.SaveTurn(RunTurn{
				AttemptID:       run.ActiveAttemptID,
				ProjectID:       run.ProjectID,
				RecordID:        run.RecordID,
				TurnID:          turnID,
				TurnIndex:       snapshot.index,
				SessionRef:      sessionRef,
				Status:          firstNonEmpty(snapshot.status, "running"),
				InputTokens:     snapshot.inputTokens,
				OutputTokens:    snapshot.outputTokens,
				TotalTokens:     snapshot.totalTokens,
				LastEventAt:     at,
				LeaseGeneration: run.LeaseGeneration,
			})
			known[turnID] = snapshot
		}
	}
	return changed
}

func codexExecEventRecorded(path, attemptID, kind string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	text, err := readText(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event Event
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.AttemptID == attemptID && event.Kind == kind {
			return true
		}
	}
	return false
}

func normalizeCodexExecEventKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	replacer := strings.NewReplacer(".", "", "-", "", "_", "", "/", "")
	return replacer.Replace(kind)
}

func codexExecTurnID(value any) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range []string{"turn_id", "turnId"} {
			if candidate := strings.TrimSpace(stringValue(current[key])); candidate != "" {
				return candidate
			}
		}
		if turn, ok := current["turn"].(map[string]any); ok {
			if candidate := strings.TrimSpace(stringValue(turn["id"])); candidate != "" {
				return candidate
			}
		}
		for _, nested := range current {
			if candidate := codexExecTurnID(nested); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, nested := range current {
			if candidate := codexExecTurnID(nested); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func codexExecTurnStatus(value any) string {
	switch current := value.(type) {
	case map[string]any:
		if status := strings.TrimSpace(stringValue(current["status"])); status != "" {
			return normalizedTurnStatus(status)
		}
		if turn, ok := current["turn"].(map[string]any); ok {
			if status := strings.TrimSpace(stringValue(turn["status"])); status != "" {
				return normalizedTurnStatus(status)
			}
		}
	}
	return "completed"
}

func codexExecInFlightCommandStartedAt(run RunStatus, fallback time.Time) (time.Time, bool) {
	if RunnerName(strings.TrimSpace(run.Runner)) != RunnerCodexExec || strings.TrimSpace(run.RawLogPath) == "" {
		return time.Time{}, false
	}
	text, err := readText(run.RawLogPath)
	if err != nil || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	byID := map[string]codexExecCommandSnapshot{}
	var anonymous []codexExecCommandSnapshot
	sequence := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(line), &value) != nil {
			continue
		}
		state, id, at, ok := codexExecRawCommandEvent(value)
		if !ok {
			continue
		}
		if at.IsZero() {
			at = fallback
		}
		switch state {
		case "started":
			if id == "" {
				sequence++
				anonymous = append(anonymous, codexExecCommandSnapshot{id: "anonymous-" + strconv.Itoa(sequence), startedAt: at})
				continue
			}
			byID[id] = codexExecCommandSnapshot{id: id, startedAt: at}
		case "completed":
			if id != "" {
				delete(byID, id)
				continue
			}
			if len(anonymous) > 0 {
				anonymous = anonymous[:len(anonymous)-1]
			}
		}
	}
	var oldest time.Time
	found := false
	for _, command := range byID {
		if command.startedAt.IsZero() {
			continue
		}
		if !found || command.startedAt.Before(oldest) {
			oldest = command.startedAt
			found = true
		}
	}
	for _, command := range anonymous {
		if command.startedAt.IsZero() {
			continue
		}
		if !found || command.startedAt.Before(oldest) {
			oldest = command.startedAt
			found = true
		}
	}
	return oldest, found
}

func codexExecRawCommandEvent(value any) (state, id string, at time.Time, ok bool) {
	payload, ok := value.(map[string]any)
	if !ok {
		return "", "", time.Time{}, false
	}
	if when, found := codexExecRawEventTimestamp(payload); found {
		at = when
	}
	if response := codexExecResponseItemPayload(payload); response != nil {
		responseType := normalizeCodexExecEventKind(stringValue(response["type"]))
		callID := firstNonEmpty(strings.TrimSpace(stringValue(response["call_id"])), strings.TrimSpace(stringValue(response["callId"])))
		switch responseType {
		case "functioncall":
			if codexExecFunctionCallIsCommand(response) {
				return "started", callID, at, true
			}
		case "functioncalloutput":
			return "completed", callID, at, true
		}
	}
	item := codexExecCommandItem(payload)
	if item == nil {
		return "", "", time.Time{}, false
	}
	itemID := codexExecCommandItemID(payload, item)
	eventKind := normalizeCodexExecEventKind(firstNonEmpty(stringValue(payload["type"]), stringValue(payload["event"]), stringValue(payload["method"])))
	status := normalizeCodexExecEventKind(firstNonEmpty(stringValue(item["status"]), stringValue(payload["status"])))
	if codexExecCommandEventStarted(eventKind, status) {
		return "started", itemID, at, true
	}
	if codexExecCommandEventCompleted(eventKind, status) {
		return "completed", itemID, at, true
	}
	return "", "", time.Time{}, false
}

func codexExecRawEventTimestamp(payload map[string]any) (time.Time, bool) {
	if parsed, ok := parseEventTimestamp(codexExecEventTime(payload)); ok {
		return parsed, true
	}
	for _, key := range []string{"payload", "params"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if parsed, ok := parseEventTimestamp(codexExecEventTime(nested)); ok {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

func codexExecResponseItemPayload(payload map[string]any) map[string]any {
	if normalizeCodexExecEventKind(stringValue(payload["type"])) == "responseitem" {
		if nested, ok := payload["payload"].(map[string]any); ok {
			return nested
		}
		return payload
	}
	responseType := normalizeCodexExecEventKind(stringValue(payload["type"]))
	if responseType == "functioncall" || responseType == "functioncalloutput" {
		return payload
	}
	return nil
}

func codexExecFunctionCallIsCommand(response map[string]any) bool {
	name := normalizeCodexExecEventKind(stringValue(response["name"]))
	return name == "execcommand" || name == "functionsexeccommand" || strings.Contains(name, "execcommand")
}

func codexExecCommandItem(payload map[string]any) map[string]any {
	for _, path := range [][]string{
		{"params", "item"},
		{"payload", "item"},
		{"item"},
	} {
		if item := codexExecNestedMap(payload, path...); item != nil && codexExecItemIsCommandExecution(item) {
			return item
		}
	}
	if codexExecItemIsCommandExecution(payload) {
		return payload
	}
	return nil
}

func codexExecNestedMap(payload map[string]any, path ...string) map[string]any {
	current := payload
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func codexExecItemIsCommandExecution(item map[string]any) bool {
	itemType := normalizeCodexExecEventKind(stringValue(item["type"]))
	if itemType == "commandexecution" {
		return true
	}
	return strings.TrimSpace(stringValue(item["command"])) != "" || strings.TrimSpace(stringValue(item["cmd"])) != ""
}

func codexExecCommandItemID(payload, item map[string]any) string {
	for _, current := range []map[string]any{item, payload} {
		for _, key := range []string{"id", "item_id", "itemId", "call_id", "callId"} {
			if value := strings.TrimSpace(stringValue(current[key])); value != "" {
				return value
			}
		}
	}
	return ""
}

func codexExecCommandEventStarted(eventKind, status string) bool {
	return strings.Contains(eventKind, "started") || status == "inprogress" || status == "running" || status == "started"
}

func codexExecCommandEventCompleted(eventKind, status string) bool {
	return strings.Contains(eventKind, "completed") || status == "completed" || status == "failed" || status == "cancelled" || status == "canceled"
}

func codexExecEventTime(payload map[string]any) string {
	for _, key := range []string{"timestamp", "time", "created_at", "createdAt", "at"} {
		if value := strings.TrimSpace(stringValue(payload[key])); value != "" {
			return value
		}
	}
	return ""
}

func codexExecTurnPayload(run RunStatus, sessionRef, turnID string, turnIndex int, payload map[string]any) map[string]any {
	out := map[string]any{
		"project_id":  run.ProjectID,
		"record_id":   run.RecordID,
		"item_id":     run.ItemID,
		"attempt_id":  run.ActiveAttemptID,
		"session_ref": sessionRef,
		"turn_id":     turnID,
		"turn_index":  turnIndex,
	}
	for key, value := range payload {
		out[key] = value
	}
	return out
}
