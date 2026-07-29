package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ClaudeExecutionObservation is the deliberately authority-less translation
// of Claude Code session metadata and SubagentStart/SubagentStop hooks. The
// parent SessionID is always the resumable top-level session; a subagent's
// session/id is only ever a provider-child handle.
type ClaudeExecutionObservation struct {
	ProjectID, ParentExecutionID, SessionID  string
	SourceEventID                            string
	SourceSequence                           int64
	SourceCursor, Kind, Status, OccurredAt   string
	ChildID, AgentType, Label, TranscriptRef string
	Capabilities                             []ProviderCapabilityFact
	Metadata                                 map[string]any
	VisibilityDegradedReason                 string
}

// ClaudeExecutionAdapter is the single Claude ingress into the generic,
// replay-safe observation envelope. It observes existing correlated work; it
// cannot create a lease, bind a task, or turn provider facts into delivery
// authority.
type ClaudeExecutionAdapter struct{ Store *RuntimeStore }

func (a ClaudeExecutionAdapter) ObserveRunPayload(run RunStatus, payload any, sequence int64, source string) (bool, error) {
	if a.Store == nil {
		return false, providerObservationError("claude execution adapter store is nil")
	}
	sessionID := strings.TrimSpace(run.SessionRef)
	if sessionID == "" {
		sessionID = claudeTopLevelString(payload, "session_id", "sessionId")
	}
	if sessionID == "" {
		return false, nil // never promote a nested subagent id into a parent session.
	}
	parentID, err := a.executionForClaudeRun(run, sessionID)
	if err != nil || parentID == "" {
		return false, err
	}
	kind := claudeTopLevelString(payload, "hook_event_name", "hookEventName", "event", "type")
	childID := claudePayloadString(payload, "subagent_id", "subagentId", "child_id", "childId", "agent_id", "agentId")
	status := claudePayloadString(payload, "status", "state", "subagent_status", "subagentStatus", "subtype")
	if status == "" {
		status = kind
	}
	occurredAt := claudePayloadString(payload, "timestamp", "occurred_at", "occurredAt", "created_at", "createdAt")
	diagnostic := ""
	if occurredAt == "" {
		occurredAt = time.Now().UTC().Format(time.RFC3339Nano)
		diagnostic = "provider_timestamp_missing_requires_authoritative_fetch"
	}
	transcript := claudePayloadString(payload, "transcript_path", "transcriptPath", "transcript", "transcript_ref", "transcriptRef")
	if childID != "" && transcript == "" && strings.Contains(strings.ToLower(kind), "stop") {
		diagnostic = firstNonEmpty(diagnostic, "child_transcript_missing")
	}
	if childID != "" && (strings.Contains(strings.ToLower(kind), "stop") || claudeObservationStatus(status, kind) == "terminal") && !claudeChildKnown(a.Store, run.ProjectID, parentID, childID) {
		diagnostic = firstNonEmpty(diagnostic, "subagent_start_missing_or_requires_authoritative_reconciliation")
	}
	raw, _ := json.Marshal(payload)
	digest := sha256.Sum256(append(append([]byte(source+":"), raw...), []byte(":"+strconv.FormatInt(sequence, 10))...))
	metadata := map[string]any{"observation_source": source}
	if transcript != "" {
		metadata["transcript_ref"] = transcript
	}
	if name := claudeTopLevelString(payload, "name", "session_name", "sessionName"); name != "" {
		metadata["top_level_name"] = name
	}
	if finalResult := claudePayloadString(payload, "result", "final_result", "finalResult", "error"); finalResult != "" {
		metadata["final_result"] = finalResult
	}
	result, err := a.Observe(ClaudeExecutionObservation{ProjectID: run.ProjectID, ParentExecutionID: parentID, SessionID: sessionID, SourceEventID: source + ":" + hex.EncodeToString(digest[:]), SourceSequence: sequence, Kind: kind, Status: status, OccurredAt: occurredAt, ChildID: childID, AgentType: claudePayloadString(payload, "agent_type", "agentType", "subagent_type", "subagentType"), Label: claudePayloadString(payload, "label", "name", "agent_name", "agentName"), TranscriptRef: transcript, Metadata: metadata, VisibilityDegradedReason: diagnostic})
	if err != nil {
		return false, err
	}
	return !result.Duplicate, nil
}

func (a ClaudeExecutionAdapter) executionForClaudeRun(run RunStatus, sessionID string) (string, error) {
	var id string
	err := a.Store.queryRowScan(`SELECT execution_id FROM execution_records WHERE project_id = ? AND attempt_id = ? LIMIT 1`, []any{run.ProjectID, run.ActiveAttemptID}, &id)
	if err == nil {
		return id, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	err = a.Store.queryRowScan(`SELECT execution_id FROM execution_attachment_events WHERE project_id = ? AND provider = 'claude' AND provider_session_id = ? LIMIT 1`, []any{run.ProjectID, sessionID}, &id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func claudeChildKnown(store *RuntimeStore, projectID, parentID, childID string) bool {
	if store == nil || strings.TrimSpace(parentID) == "" || strings.TrimSpace(childID) == "" {
		return false
	}
	var count int
	err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE project_id = ? AND parent_execution_id = ? AND provider = 'claude' AND provider_child_handle = ? AND node_kind = 'provider_child'`, []any{projectID, parentID, childID}, &count)
	return err == nil && count > 0
}

func (a ClaudeExecutionAdapter) Observe(observation ClaudeExecutionObservation) (ProviderObservationResult, error) {
	if a.Store == nil {
		return ProviderObservationResult{}, providerObservationError("claude execution adapter store is nil")
	}
	observation.ProjectID, observation.SessionID = strings.TrimSpace(observation.ProjectID), strings.TrimSpace(observation.SessionID)
	if observation.ProjectID == "" || observation.SessionID == "" {
		return ProviderObservationResult{}, providerObservationError("claude observation requires project and top-level session id")
	}
	occurredAt := strings.TrimSpace(observation.OccurredAt)
	if occurredAt == "" {
		occurredAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	eventID := strings.TrimSpace(observation.SourceEventID)
	if eventID == "" && observation.SourceSequence <= 0 {
		eventID = claudeObservationIdentity(observation)
	}
	capabilities := observation.Capabilities
	if len(capabilities) == 0 {
		capabilities = claudeDefaultCapabilities(occurredAt)
	}
	diagnostic := strings.TrimSpace(observation.VisibilityDegradedReason)
	if !knownClaudeObservationStatus(observation.Status, observation.Kind) {
		diagnostic = firstNonEmpty(diagnostic, "unrecognized_provider_status_requires_authoritative_fetch")
	}
	envelope := ProviderExecutionEventEnvelope{Version: providerObservationVersion, ProjectID: observation.ProjectID, Provider: "claude", SourceEventID: eventID, SourceSequence: observation.SourceSequence, SourceCursor: strings.TrimSpace(observation.SourceCursor), ParentProviderSessionID: observation.SessionID, ChildHandle: strings.TrimSpace(observation.ChildID), AgentType: strings.TrimSpace(observation.AgentType), Label: strings.TrimSpace(observation.Label), Status: claudeObservationStatus(observation.Status, observation.Kind), OccurredAt: occurredAt, Capabilities: capabilities, Metadata: observation.Metadata, VisibilityDegradedReason: diagnostic}
	if err := validateProviderExecutionEvent(&envelope); err != nil {
		return ProviderObservationResult{}, err
	}
	if _, _, err := sanitizeProviderMetadata(envelope.Metadata); err != nil {
		return ProviderObservationResult{}, err
	}
	if _, _, _, err := normalizeProviderCapabilities(envelope.Capabilities); err != nil {
		return ProviderObservationResult{}, err
	}
	if parentID := strings.TrimSpace(observation.ParentExecutionID); parentID != "" {
		parent, err := a.Store.Execution(parentID)
		if err != nil {
			return ProviderObservationResult{}, err
		}
		if parent == nil || parent.ProjectID != observation.ProjectID {
			return ProviderObservationResult{}, providerObservationError("claude observation parent execution is not available in this project")
		}
		if _, _, err := a.Store.AttachExecution(ExecutionAttachmentInput{ProjectID: observation.ProjectID, ExecutionID: parentID, Provider: "claude", ProviderSessionID: observation.SessionID, SessionRef: observation.SessionID, Source: "claude_adapter", Actor: "provider_observation"}); err != nil {
			return ProviderObservationResult{}, err
		}
	}
	if claudeProviderStatusRegression(a.Store, envelope) {
		envelope.VisibilityDegradedReason = "provider_status_regression_requires_authoritative_fetch"
	}
	result, err := a.Store.ApplyProviderExecutionEvent(envelope)
	if err != nil {
		return ProviderObservationResult{}, err
	}
	// A parent result says nothing about whether its provider-owned children
	// settled. On terminal parent evidence, record a distinct immutable partial
	// observation for each started child lacking its own terminal stop fact.
	// This is diagnostic-only: it does not alter the child execution, outcome,
	// lease, process, or capability facts.
	if envelope.ChildHandle == "" && claudeTerminalProviderStatus(envelope.Status) {
		if err := a.reconcileUnsettledChildren(envelope); err != nil {
			return ProviderObservationResult{}, err
		}
	}
	return result, nil
}

func claudeTerminalProviderStatus(status string) bool {
	return status == "terminal" || status == "failed" || status == "cancelled"
}

func (a ClaudeExecutionAdapter) reconcileUnsettledChildren(parent ProviderExecutionEventEnvelope) error {
	rows, err := a.Store.query(`SELECT provider_child_handle, agent_type FROM execution_records WHERE project_id = ? AND parent_execution_id = ? AND provider = 'claude' AND node_kind = 'provider_child' ORDER BY execution_id`, parent.ProjectID, parentExecutionIDForProviderSession(a.Store, parent.ProjectID, parent.ParentProviderSessionID))
	if err != nil {
		return err
	}
	type child struct {
		handle    string
		agentType string
	}
	children := make([]child, 0)
	for rows.Next() {
		var childHandle, agentType string
		if err := rows.Scan(&childHandle, &agentType); err != nil {
			_ = rows.Close()
			return err
		}
		children = append(children, child{handle: childHandle, agentType: agentType})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	// RuntimeStore deliberately serializes SQLite access. Release the cursor
	// before asking the store for more facts or appending diagnostics; otherwise
	// a parent-terminal reconciliation waits on its own held connection.
	if err := rows.Close(); err != nil {
		return err
	}
	for _, child := range children {
		settled, err := claudeChildHasTerminalObservation(a.Store, parent.ProjectID, parent.ParentProviderSessionID, child.handle)
		if err != nil {
			return err
		}
		if settled {
			continue
		}
		// The key is solely derived from durable provider parent evidence and the
		// immutable child handle. Replaying the terminal parent event after a
		// crash/restart is therefore a no-op at the generic receipt boundary.
		diagnostic := ProviderExecutionEventEnvelope{
			Version: providerObservationVersion, ProjectID: parent.ProjectID, Provider: "claude",
			SourceEventID:           "claude-parent-terminal:" + parent.SourceEventID + ":" + child.handle,
			ParentProviderSessionID: parent.ParentProviderSessionID, ChildHandle: child.handle, AgentType: child.agentType,
			Status: "unknown", OccurredAt: parent.OccurredAt,
			Metadata:                 map[string]any{"reconciliation": "parent_terminal", "parent_status": parent.Status, "child_state": "partial_or_lost", "provider_provenance": "claude_adapter"},
			VisibilityDegradedReason: "parent_terminal_before_child_stop_partial_or_lost",
		}
		if _, err := a.Store.ApplyProviderExecutionEvent(diagnostic); err != nil {
			return err
		}
	}
	return nil
}

func parentExecutionIDForProviderSession(store *RuntimeStore, projectID, sessionID string) string {
	parent, err := store.providerObservationParent(projectID, "claude", sessionID)
	if err != nil || parent == nil {
		return ""
	}
	return parent.ExecutionID
}

func claudeChildHasTerminalObservation(store *RuntimeStore, projectID, sessionID, childHandle string) (bool, error) {
	var count int
	err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE project_id = ? AND provider = 'claude' AND parent_provider_session_id = ? AND child_handle = ? AND status IN ('terminal', 'failed', 'cancelled')`, []any{projectID, sessionID, childHandle}, &count)
	return count > 0, err
}

func claudeTopLevelString(value any, keys ...string) string {
	m, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range keys {
		if candidate := strings.TrimSpace(stringValue(m[key])); candidate != "" {
			return candidate
		}
	}
	return ""
}
func claudePayloadString(value any, keys ...string) string { return codexPayloadString(value, keys...) }

func knownClaudeObservationStatus(status, kind string) bool {
	status, kind = strings.ToLower(strings.TrimSpace(status)), strings.ToLower(strings.TrimSpace(kind))
	if strings.Contains(kind, "subagentstart") || strings.Contains(kind, "subagent_start") || strings.Contains(kind, "subagentstop") || strings.Contains(kind, "subagent_stop") {
		return true
	}
	if status == "success" || status == "interrupted" {
		return true
	}
	return knownCodexObservationStatus(status, kind)
}
func claudeObservationStatus(status, kind string) string {
	combined := strings.ToLower(strings.TrimSpace(status + " " + kind))
	if strings.Contains(combined, "subagentstart") || strings.Contains(combined, "subagent_start") {
		return "running"
	}
	if strings.Contains(combined, "subagentstop") || strings.Contains(combined, "subagent_stop") {
		return "terminal"
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded":
		return "terminal"
	case "interrupted":
		return "cancelled"
	}
	return codexObservationStatus(status, kind)
}
func claudeObservationIdentity(o ClaudeExecutionObservation) string {
	payload, _ := json.Marshal([]string{o.SessionID, o.ChildID, o.Kind, o.Status, o.OccurredAt, o.Label, o.AgentType, o.TranscriptRef})
	digest := sha256.Sum256(payload)
	return "claude:" + hex.EncodeToString(digest[:])
}
func claudeDefaultCapabilities(at string) []ProviderCapabilityFact {
	return []ProviderCapabilityFact{{Name: "parent_interrupt", State: "unknown", Provenance: "claude_hook_adapter", FreshAt: at}, {Name: "independent_child_control", State: "false", Provenance: "claude_hook_adapter", FreshAt: at}, {Name: "resume", State: "unknown", Provenance: "claude_hook_adapter", FreshAt: at}, {Name: "enumeration", State: "false", Provenance: "claude_hook_adapter", FreshAt: at}, {Name: "replay", State: "false", Provenance: "claude_hook_adapter", FreshAt: at}}
}
func claudeProviderStatusRegression(store *RuntimeStore, event ProviderExecutionEventEnvelope) bool {
	if store == nil {
		return false
	}
	var prior string
	err := store.queryRowScan(`SELECT status FROM provider_execution_observations WHERE project_id = ? AND provider = 'claude' AND parent_provider_session_id = ? AND child_handle = '' ORDER BY received_at DESC, observation_id DESC LIMIT 1`, []any{event.ProjectID, event.ParentProviderSessionID}, &prior)
	if err != nil {
		return false
	}
	return (prior == "terminal" || prior == "failed" || prior == "cancelled") && (event.Status == "starting" || event.Status == "running" || event.Status == "interrupt_requested")
}
