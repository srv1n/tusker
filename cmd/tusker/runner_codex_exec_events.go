package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	started      bool
}

type codexExecCommandSnapshot struct {
	id        string
	startedAt time.Time
}

func codexExecCompletedCommand(value any) (id, command string, observation v7VerificationCommandObservation, ok bool) {
	payload, ok := value.(map[string]any)
	if !ok {
		return "", "", observation, false
	}
	item := codexExecCommandItem(payload)
	if item == nil {
		return "", "", observation, false
	}
	state, id, at, completed := codexExecRawCommandEvent(value)
	if !completed || state != "completed" {
		return "", "", observation, false
	}
	command = strings.TrimSpace(firstNonEmpty(stringValue(item["command"]), stringValue(item["cmd"])))
	if command == "" {
		return "", "", observation, false
	}
	rawExitCode := firstNonEmpty(stringValue(item["exit_code"]), stringValue(item["exitCode"]), stringValue(payload["exit_code"]), stringValue(payload["exitCode"]))
	if rawExitCode == "" {
		return "", "", observation, false
	}
	exitCode := atoiSafe(rawExitCode)
	status := normalizeCodexExecEventKind(firstNonEmpty(stringValue(item["status"]), stringValue(payload["status"])))
	if status != "completed" && exitCode == 0 {
		exitCode = 1
	}
	output := firstNonEmpty(stringValue(item["aggregated_output"]), stringValue(item["aggregatedOutput"]), stringValue(item["output"]))
	digest := sha256.Sum256([]byte(output))
	observation = v7VerificationCommandObservation{FinishedAt: at, ExitCode: exitCode, Digest: "sha256:" + hex.EncodeToString(digest[:])}
	if observation.FinishedAt.IsZero() {
		observation.FinishedAt = time.Now().UTC()
	}
	observation.Message = "pass"
	if exitCode != 0 {
		observation.Message = fmt.Sprintf("exit status %d", exitCode)
	}
	return id, command, observation, true
}

func (d *Daemon) ingestCodexExecRawLog(run RunStatus) (bool, error) {
	if d == nil || d.store == nil || RunnerName(strings.TrimSpace(run.Runner)) != RunnerCodexExec || strings.TrimSpace(run.RawLogPath) == "" {
		return false, nil
	}
	// ponytail: polls already visit attempts serially; split this lock per
	// attempt if concurrent replay becomes a measured bottleneck.
	d.codexExecIngestMu.Lock()
	defer d.codexExecIngestMu.Unlock()
	info, err := os.Stat(run.RawLogPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat codex-exec raw log: %w", err)
	}
	snapshot, err := snapshotEventLogFileInfo(info, run.RawLogPath)
	if err != nil {
		return false, err
	}
	state, err := d.codexExecIngestState(run)
	if err != nil {
		return false, err
	}
	if state.rawInitialized && !state.retry && snapshot == state.rawSnapshot {
		if state.eventLog == nil {
			return false, nil
		}
		eventInfo, err := os.Stat(state.eventPath)
		if err != nil {
			return false, fmt.Errorf("stat codex-exec event log: %w", err)
		}
		eventSnapshot, err := snapshotEventLogFileInfo(eventInfo, state.eventPath)
		if err != nil {
			return false, err
		}
		if eventSnapshot == state.eventSnapshot {
			return false, nil
		}
		if err := state.refreshEvents(run.ActiveAttemptID); err != nil {
			state.retry = true
			return false, fmt.Errorf("validate codex-exec event log before replay: %w", err)
		}
		return false, nil
	}
	file, err := os.Open(run.RawLogPath)
	if err != nil {
		return false, fmt.Errorf("open codex-exec raw log: %w", err)
	}
	defer file.Close()
	opened, err := snapshotEventLogFile(file, run.RawLogPath)
	if err != nil {
		return false, err
	}
	snapshot = opened
	reset := state.rawInitialized && (!snapshot.matchesIdentity(state.rawSnapshot) || snapshot.Size < state.rawSnapshot.Size || (snapshot.Size == state.rawSnapshot.Size && snapshot.ModTimeUnixNano != state.rawSnapshot.ModTimeUnixNano))
	if state.rawInitialized && !reset {
		matches, err := state.rawBoundaryMatches(file)
		if err != nil {
			return false, fmt.Errorf("check codex-exec raw log boundary: %w", err)
		}
		reset = !matches
	}
	if reset {
		state.rawOffset, state.rawLine, state.rawBoundary = 0, 0, nil
	}
	state.rawSnapshot, state.rawInitialized = snapshot, true
	if err := state.refreshEvents(run.ActiveAttemptID); err != nil {
		state.retry = true
		return false, fmt.Errorf("validate codex-exec event log before replay: %w", err)
	}
	if _, err := file.Seek(state.rawOffset, io.SeekStart); err != nil {
		state.retry = true
		return false, err
	}
	reader := bufio.NewReader(io.LimitReader(file, snapshot.Size-state.rawOffset))
	changed := false
	for {
		line, readErr := readCodexExecIngestLine(reader)
		state.stats.rawBytes += int64(len(line))
		if errors.Is(readErr, io.EOF) {
			// Keep the cursor before an incomplete trailing JSONL record. A later
			// append supplies the rest, even if the existing prefix is valid JSON.
			break
		}
		if readErr != nil {
			state.retry = true
			return changed, fmt.Errorf("read codex-exec raw log: %w", readErr)
		}
		var value any
		if json.Unmarshal(bytes.TrimSpace(line), &value) == nil {
			state.stats.rawRecords++
			recorded, err := d.ingestCodexExecRecord(run, state, value, line, state.rawLine+1)
			changed = changed || recorded
			if err != nil {
				state.retry = true
				return changed, err
			}
		}
		state.advanceRaw(line)
	}
	if err := state.refreshEvents(run.ActiveAttemptID); err != nil {
		state.retry = true
		return changed, fmt.Errorf("validate codex-exec event log after replay: %w", err)
	}
	state.retry = false
	return changed, nil
}

func (d *Daemon) ingestCodexExecRecord(run RunStatus, state *codexExecIngestState, value any, line []byte, sequence int64) (bool, error) {
	state.sessionRef = firstNonEmpty(run.SessionRef, state.sessionRef, findSessionRef(value))
	state.messageRef = firstNonEmpty(state.messageRef, findMessageRef(value))
	changed, err := (CodexExecutionAdapter{Store: d.store}).ObserveRunPayload(run, value, sequence, "codex_exec_jsonl")
	if err != nil {
		return false, fmt.Errorf("observe codex-exec execution event: %w", err)
	}
	payload, _ := value.(map[string]any)
	kind := normalizeCodexExecEventKind(firstNonEmpty(stringValue(payload["type"]), stringValue(payload["event"]), stringValue(payload["method"])))
	at := firstNonEmpty(codexExecEventTime(payload), time.Now().UTC().Format(time.RFC3339))
	// Nested child identifiers must never replace the resumable parent thread.
	sessionRef := firstNonEmpty(run.SessionRef, findSessionRef(value))
	turnID := codexExecTurnID(value)
	usage := extractUsageCounters(json.RawMessage(line))
	if commandID, command, observation, completed := codexExecCompletedCommand(value); completed {
		fields := map[string]any{"command_id": commandID}
		if commandID == "" {
			digest := sha256.Sum256(append(append([]byte(nil), bytes.TrimSpace(line)...), []byte(":"+strconv.FormatInt(sequence, 10))...))
			fields["source_event_id"] = hex.EncodeToString(digest[:])
		}
		found, err := state.hasEvent(run, codexExecIngestEventKey("command_completed", fields))
		if err != nil {
			return changed, err
		}
		if !found {
			fields["command"], fields["result"] = command, observation.Message
			fields["exit_code"], fields["output_sha256"] = observation.ExitCode, observation.Digest
			fields["finished_at"] = observation.FinishedAt.Format(time.RFC3339Nano)
			fields["material"] = codexExecObservedMaterialFingerprint(d.store, run)
			recorded, err := state.appendEvent(run, "command_completed", fields)
			changed = changed || recorded
			if err != nil {
				return changed, fmt.Errorf("record codex-exec command event: %w", err)
			}
		}
	}
	if turnID == "" {
		turnID = usage.turnID
	}
	if kind == "threadstarted" || kind == "sessionstarted" {
		recorded, err := state.appendEvent(run, "thread_started", map[string]any{
			"project_id": run.ProjectID, "record_id": run.RecordID, "item_id": run.ItemID,
			"attempt_id": run.ActiveAttemptID, "session_ref": sessionRef,
		})
		return changed || recorded, wrapCodexExecEventError("thread_started", err)
	}
	if turnID == "" || (kind != "turnstarted" && kind != "turncompleted" && !usage.hasAny()) {
		return changed, nil
	}
	prior, err := state.turn(d.store, run, turnID)
	if err != nil {
		return changed, fmt.Errorf("load codex-exec turn: %w", err)
	}
	snapshot := prior
	turn := RunTurn{
		AttemptID: run.ActiveAttemptID, ProjectID: run.ProjectID, RecordID: run.RecordID,
		TurnID: turnID, TurnIndex: prior.index, SessionRef: sessionRef,
		LastEventAt: at, LeaseGeneration: run.LeaseGeneration,
	}
	var eventKind string
	var fields map[string]any
	switch kind {
	case "turnstarted":
		eventKind, fields = "turn_started", map[string]any{"source": "codex_exec"}
		snapshot.status = firstNonEmpty(snapshot.status, "running")
		snapshot.started = true
		turn.StartedAt = at
	case "turncompleted":
		eventKind = "turn_completed"
		snapshot.status = firstNonEmpty(codexExecTurnStatus(value), "completed")
		turn.CompletedAt = at
	default:
		eventKind = "turn_usage_updated"
		snapshot.status = firstNonEmpty(snapshot.status, "running")
	}
	if kind != "turnstarted" {
		snapshot.inputTokens = maxInt(snapshot.inputTokens, usage.inputTokens)
		snapshot.outputTokens = maxInt(snapshot.outputTokens, usage.outputTokens)
		snapshot.totalTokens = maxInt(snapshot.totalTokens, usage.totalTokens)
		fields = map[string]any{
			"input_tokens": snapshot.inputTokens, "output_tokens": snapshot.outputTokens,
			"total_tokens": snapshot.totalTokens, "status": snapshot.status,
		}
		if kind != "turncompleted" {
			fields["source"] = "codex_exec"
		}
	}
	if kind == "turnstarted" || kind == "turncompleted" || snapshot != prior {
		recorded, err := state.appendEvent(run, eventKind, codexExecTurnPayload(run, sessionRef, turnID, snapshot.index, fields))
		changed = changed || recorded
		if err != nil {
			return changed, wrapCodexExecEventError(eventKind, err)
		}
	}
	// Replayed starts and unchanged terminal snapshots leave durable rows alone,
	// including their original timestamps when provider timestamps are absent.
	if snapshot != prior {
		turn.Status, turn.InputTokens, turn.OutputTokens, turn.TotalTokens = snapshot.status, snapshot.inputTokens, snapshot.outputTokens, snapshot.totalTokens
		if kind == "turnstarted" {
			// SaveTurn preserves a terminal turn's last-event timestamp when
			// filling a missing start observed earlier in the raw history.
			turn.Status = "running"
		}
		if err := state.saveTurn(d.store, turn, snapshot); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}

func wrapCodexExecEventError(kind string, err error) error {
	if err != nil {
		return fmt.Errorf("record codex-exec %s event: %w", kind, err)
	}
	return nil
}

func codexExecObservedMaterialFingerprint(store *RuntimeStore, run RunStatus) string {
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{LoadDisabled: true, ProjectID: run.ProjectID})
	if err != nil || len(projects) != 1 || projects[0].LoadError != nil {
		return ""
	}
	task, err := resolveV7Note(projects[0].Project.VaultRoot, run.ItemID, "task")
	if err != nil {
		return ""
	}
	identity, _, err := v7VerificationReceiptIdentityFor(projects[0].Project.VaultRoot, task, run.WorkspacePath, nil)
	if err != nil {
		return ""
	}
	return identity.MaterialFingerprint
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
