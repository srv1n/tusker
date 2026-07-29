package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProviderExecutionEventEnvelope is the only generic ingress contract for
// untrusted provider observations. It deliberately contains no task, lease,
// proof, wave, ref, or outcome authority fields.
type ProviderExecutionEventEnvelope struct {
	Version                 int                      `json:"version"`
	ProjectID               string                   `json:"project_id"`
	Provider                string                   `json:"provider"`
	SourceEventID           string                   `json:"source_event_id,omitempty"`
	SourceSequence          int64                    `json:"source_sequence,omitempty"`
	SourceCursor            string                   `json:"source_cursor,omitempty"`
	ParentProviderSessionID string                   `json:"parent_provider_session_id"`
	ChildHandle             string                   `json:"child_handle,omitempty"`
	AgentType               string                   `json:"agent_type,omitempty"`
	Label                   string                   `json:"label,omitempty"`
	Status                  string                   `json:"status"`
	OccurredAt              string                   `json:"occurred_at"`
	Capabilities            []ProviderCapabilityFact `json:"capabilities,omitempty"`
	Metadata                map[string]any           `json:"metadata,omitempty"`
	// VisibilityDegradedReason is adapter-owned diagnostic state only. It is
	// deliberately not a lifecycle/outcome transition and is persisted beside
	// the immutable observation so operators can repair provider regressions.
	VisibilityDegradedReason string `json:"visibility_degraded_reason,omitempty"`
}

// ProviderCapabilityFact has explicit provenance and freshness. A provider
// cannot imply an independent control merely by omitting this fact.
type ProviderCapabilityFact struct {
	Name       string `json:"name"`
	State      string `json:"state"` // true, false, or unknown
	Provenance string `json:"provenance"`
	FreshAt    string `json:"fresh_at"`
}

type ProviderObservationResult struct {
	ObservationID      string `json:"observation_id"`
	ParentExecutionID  string `json:"parent_execution_id"`
	ChildExecutionID   string `json:"child_execution_id,omitempty"`
	Duplicate          bool   `json:"duplicate"`
	ChildCreated       bool   `json:"child_created"`
	CheckpointAdvanced bool   `json:"checkpoint_advanced"`
	Degraded           bool   `json:"degraded"`
	DegradedReason     string `json:"degraded_reason,omitempty"`
}

const (
	providerObservationVersion      = 1
	providerObservationMaxEnvelope  = 64 << 10
	providerObservationMaxMetadata  = 16 << 10
	providerObservationMaxMetaKeys  = 32
	providerObservationMaxMetaDepth = 3
	providerObservationMaxMetaValue = 1024
	providerObservationRefused      = "PROVIDER_OBSERVATION_REFUSED"
)

func (s *RuntimeStore) migrateProviderExecutionObservations() error {
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS provider_event_receipts (
			project_id TEXT NOT NULL, provider TEXT NOT NULL, parent_provider_session_id TEXT NOT NULL,
			event_key TEXT NOT NULL, observation_id TEXT NOT NULL, received_at TEXT NOT NULL,
			PRIMARY KEY(project_id, provider, parent_provider_session_id, event_key)
		);`,
		`CREATE TABLE IF NOT EXISTS provider_source_checkpoints (
			project_id TEXT NOT NULL, provider TEXT NOT NULL, parent_provider_session_id TEXT NOT NULL,
			source_sequence INTEGER NOT NULL DEFAULT 0, source_cursor TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL,
			PRIMARY KEY(project_id, provider, parent_provider_session_id)
		);`,
		`CREATE TABLE IF NOT EXISTS provider_execution_observations (
			observation_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, provider TEXT NOT NULL,
			parent_provider_session_id TEXT NOT NULL, parent_execution_id TEXT NOT NULL,
			child_execution_id TEXT NOT NULL DEFAULT '', child_handle TEXT NOT NULL DEFAULT '', event_key TEXT NOT NULL,
			source_sequence INTEGER NOT NULL DEFAULT 0, source_cursor TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
			occurred_at TEXT NOT NULL, capabilities_json TEXT NOT NULL DEFAULT '[]', metadata_json TEXT NOT NULL DEFAULT '{}',
			degraded INTEGER NOT NULL DEFAULT 0, degraded_reason TEXT NOT NULL DEFAULT '', received_at TEXT NOT NULL,
			UNIQUE(project_id, provider, parent_provider_session_id, event_key)
		);`,
		`CREATE INDEX IF NOT EXISTS provider_observations_child ON provider_execution_observations(child_execution_id, source_sequence DESC);`,
		`CREATE TRIGGER IF NOT EXISTS provider_event_receipts_immutable BEFORE UPDATE ON provider_event_receipts BEGIN SELECT RAISE(ABORT, 'provider event receipts are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS provider_event_receipts_no_delete BEFORE DELETE ON provider_event_receipts BEGIN SELECT RAISE(ABORT, 'provider event receipts are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS provider_observations_immutable BEFORE UPDATE ON provider_execution_observations BEGIN SELECT RAISE(ABORT, 'provider observations are immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS provider_observations_no_delete BEFORE DELETE ON provider_execution_observations BEGIN SELECT RAISE(ABORT, 'provider observations are immutable'); END;`,
	} {
		if _, err := s.exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func providerObservationError(message string) error {
	return tuskerError(providerObservationRefused, message)
}

func providerEventKey(event ProviderExecutionEventEnvelope) string {
	if id := strings.TrimSpace(event.SourceEventID); id != "" {
		return "id:" + id
	}
	return fmt.Sprintf("seq:%d", event.SourceSequence)
}

func validProviderObservationProvider(provider string) bool {
	return provider == "codex" || provider == "claude"
}

func validProviderObservationStatus(status string) bool {
	switch status {
	case "unknown", "starting", "running", "interrupt_requested", "acknowledged", "terminal", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// ApplyProviderExecutionEvent records observable provider facts only. It must
// never be used as a run, binding, proof, or delivery state transition path.
func (s *RuntimeStore) ApplyProviderExecutionEvent(event ProviderExecutionEventEnvelope) (ProviderObservationResult, error) {
	if s == nil {
		return ProviderObservationResult{}, providerObservationError("provider observation store is nil")
	}
	if err := validateProviderExecutionEvent(&event); err != nil {
		return ProviderObservationResult{}, err
	}
	raw, _ := json.Marshal(event)
	if len(raw) > providerObservationMaxEnvelope {
		return ProviderObservationResult{}, providerObservationError("provider observation envelope exceeds 64KiB")
	}
	metadata, truncated, err := sanitizeProviderMetadata(event.Metadata)
	if err != nil {
		return ProviderObservationResult{}, err
	}
	capabilities, degraded, reason, err := normalizeProviderCapabilities(event.Capabilities)
	if err != nil {
		return ProviderObservationResult{}, err
	}
	if truncated {
		degraded, reason = true, firstNonEmpty(reason, "metadata_redacted_or_truncated")
	}
	if diagnostic := strings.TrimSpace(event.VisibilityDegradedReason); diagnostic != "" {
		degraded, reason = true, diagnostic
	}
	metadataJSON, _ := json.Marshal(metadata)
	capabilitiesJSON, _ := json.Marshal(capabilities)

	parent, err := s.providerObservationParent(event.ProjectID, event.Provider, event.ParentProviderSessionID)
	if err != nil {
		return ProviderObservationResult{}, err
	}
	if parent == nil {
		return ProviderObservationResult{}, providerObservationError("provider observation parent session is not attached to an execution")
	}
	result := ProviderObservationResult{ParentExecutionID: parent.ExecutionID, Degraded: degraded, DegradedReason: reason}
	if event.SourceSequence == 0 && event.SourceCursor != "" {
		result.Degraded, result.DegradedReason = true, firstNonEmpty(result.DegradedReason, "opaque_cursor_not_advanced_without_sequence")
	}
	eventKey := providerEventKey(event)
	now := executionNow()
	err = s.withBusyRetry(func() error {
		tx, txErr := s.db.Begin()
		if txErr != nil {
			return txErr
		}
		defer tx.Rollback()
		var existingID string
		txErr = tx.QueryRow(`SELECT observation_id FROM provider_event_receipts WHERE project_id = ? AND provider = ? AND parent_provider_session_id = ? AND event_key = ?`, event.ProjectID, event.Provider, event.ParentProviderSessionID, eventKey).Scan(&existingID)
		if txErr == nil {
			var degradedInt int
			if txErr = tx.QueryRow(`SELECT parent_execution_id, child_execution_id, degraded, degraded_reason FROM provider_execution_observations WHERE observation_id = ?`, existingID).Scan(&result.ParentExecutionID, &result.ChildExecutionID, &degradedInt, &result.DegradedReason); txErr != nil {
				return txErr
			}
			result.ObservationID, result.Duplicate, result.ChildCreated, result.Degraded = existingID, true, false, degradedInt != 0
			return tx.Commit()
		}
		if txErr != sql.ErrNoRows {
			return txErr
		}
		if event.ChildHandle != "" {
			child, created, childErr := upsertProviderChildExecutionTx(tx, parent, event)
			if childErr != nil {
				return childErr
			}
			result.ChildExecutionID, result.ChildCreated = child.ExecutionID, created
		}
		if s.providerObservationBeforePersist != nil {
			if txErr = s.providerObservationBeforePersist(tx); txErr != nil {
				return txErr
			}
		}
		result.ObservationID = "provider-observation-" + strings.ToLower(newRecordID())
		if _, txErr = tx.Exec(`INSERT INTO provider_execution_observations(observation_id, project_id, provider, parent_provider_session_id, parent_execution_id, child_execution_id, child_handle, event_key, source_sequence, source_cursor, status, occurred_at, capabilities_json, metadata_json, degraded, degraded_reason, received_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, result.ObservationID, event.ProjectID, event.Provider, event.ParentProviderSessionID, parent.ExecutionID, result.ChildExecutionID, event.ChildHandle, eventKey, event.SourceSequence, event.SourceCursor, event.Status, event.OccurredAt, string(capabilitiesJSON), string(metadataJSON), boolToInt(result.Degraded), result.DegradedReason, now); txErr != nil {
			return txErr
		}
		if _, txErr = tx.Exec(`INSERT INTO provider_event_receipts(project_id, provider, parent_provider_session_id, event_key, observation_id, received_at) VALUES(?,?,?,?,?,?)`, event.ProjectID, event.Provider, event.ParentProviderSessionID, eventKey, result.ObservationID, now); txErr != nil {
			return txErr
		}
		if event.SourceSequence > 0 {
			var current int64
			txErr = tx.QueryRow(`SELECT source_sequence FROM provider_source_checkpoints WHERE project_id = ? AND provider = ? AND parent_provider_session_id = ?`, event.ProjectID, event.Provider, event.ParentProviderSessionID).Scan(&current)
			if txErr != nil && txErr != sql.ErrNoRows {
				return txErr
			}
			if txErr == sql.ErrNoRows || event.SourceSequence > current {
				_, txErr = tx.Exec(`INSERT INTO provider_source_checkpoints(project_id, provider, parent_provider_session_id, source_sequence, source_cursor, updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(project_id, provider, parent_provider_session_id) DO UPDATE SET source_sequence=excluded.source_sequence, source_cursor=excluded.source_cursor, updated_at=excluded.updated_at`, event.ProjectID, event.Provider, event.ParentProviderSessionID, event.SourceSequence, event.SourceCursor, now)
				if txErr != nil {
					return txErr
				}
				result.CheckpointAdvanced = true
			}
		}
		// The source's own sequence is preserved in the observation, but is not
		// necessarily present for ID-addressed provider events.  The timeline has
		// its own durable, local sequence so every accepted fact is fetchable.
		timelineExecutionID := firstNonEmpty(result.ChildExecutionID, parent.ExecutionID)
		if err := s.appendExecutionTimelineEventTx(tx, executionTimelineAppend{
			ProjectID: projectIDOr(event.ProjectID, parent.ProjectID), ExecutionID: timelineExecutionID,
			Provider: event.Provider, ProviderEventID: event.SourceEventID, ObservationID: result.ObservationID,
			SourceSequence: event.SourceSequence, OccurredAt: event.OccurredAt, ReceivedAt: now,
			Status: event.Status, Authoritative: true,
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return ProviderObservationResult{}, err
	}
	// Commit a restart-safe lifecycle projection only after the raw immutable
	// provider observation has committed. It remains separate from outcome.
	_ = s.refreshExecutionLifecycle(firstNonEmpty(result.ChildExecutionID, result.ParentExecutionID))
	if result.ChildExecutionID != "" {
		_ = s.refreshExecutionLifecycle(result.ParentExecutionID)
	}
	return result, nil
}

// upsertProviderChildExecutionTx keeps provider identity, typed edge, receipt,
// observation, and checkpoint inside one transaction. In particular, a replay
// cannot create a new child before its existing receipt is consulted.
func upsertProviderChildExecutionTx(tx *sql.Tx, parent *ExecutionRecord, event ProviderExecutionEventEnvelope) (ExecutionRecord, bool, error) {
	var existing ExecutionRecord
	err := tx.QueryRow(`SELECT execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at FROM execution_records WHERE project_id = ? AND parent_execution_id = ? AND provider = ? AND provider_child_handle = ? AND node_kind = 'provider_child'`, event.ProjectID, parent.ExecutionID, event.Provider, event.ChildHandle).Scan(executionScanDest(&existing)...)
	if err == nil {
		return existing, false, nil
	}
	if err != sql.ErrNoRows {
		return ExecutionRecord{}, false, err
	}
	now := executionNow()
	record := ExecutionRecord{ExecutionID: newExecutionID(), RootExecutionID: parent.RootExecutionID, ParentExecutionID: parent.ExecutionID, ProjectID: event.ProjectID, NodeKind: ExecutionNodeProviderChild, DisplayName: event.Label, TaskID: parent.TaskID, WaveID: parent.WaveID, Source: "provider", Provider: event.Provider, ProviderSessionID: event.ParentProviderSessionID, AgentType: event.AgentType, ProviderChildHandle: event.ChildHandle, Creator: "provider_observation", CreatedAt: now}
	record.SearchLabel = normalizeExecutionLabel(record.DisplayName, record.AgentType, record.ProviderChildHandle, record.ExecutionID)
	if err := insertExecutionWithEdgeTx(tx, record, ExecutionEdge{ParentExecutionID: parent.ExecutionID, ChildExecutionID: record.ExecutionID, Kind: ExecutionProviderChildOf, CreatedAt: now}); err != nil {
		return ExecutionRecord{}, false, err
	}
	return record, true, nil
}

func (s *RuntimeStore) providerObservationParent(projectID, provider, sessionID string) (*ExecutionRecord, error) {
	var record ExecutionRecord
	err := s.queryRowScan(`SELECT r.execution_id, r.root_execution_id, r.parent_execution_id, r.project_id, r.node_kind, r.display_name, r.search_label, r.task_id, r.wave_id, r.wave_authorization_generation, r.attempt_id, r.session_ref, r.source, r.provider, r.provider_session_id, r.agent_type, r.provider_child_handle, r.creator, r.lease_generation, r.created_at FROM execution_attachment_events a JOIN execution_records r ON r.execution_id = a.execution_id WHERE a.project_id = ? AND a.provider = ? AND a.provider_session_id = ?`, []any{projectID, provider, sessionID}, executionScanDest(&record)...)
	if err == sql.ErrNoRows {
		return s.providerObservationLegacyParent(projectID, provider, sessionID)
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// Older/direct callers can register an already-known provider session in the
// immutable record. It remains readable during the registration migration, but
// an ambiguous legacy mapping is refused rather than guessed.
func (s *RuntimeStore) providerObservationLegacyParent(projectID, provider, sessionID string) (*ExecutionRecord, error) {
	rows, err := s.query(`SELECT execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at FROM execution_records WHERE project_id = ? AND provider = ? AND provider_session_id = ? AND node_kind != 'provider_child' LIMIT 2`, projectID, provider, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var matches []ExecutionRecord
	for rows.Next() {
		var record ExecutionRecord
		if err := rows.Scan(executionScanDest(&record)...); err != nil {
			return nil, err
		}
		matches = append(matches, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, providerObservationError("provider observation parent session is ambiguous")
	}
	return &matches[0], nil
}

func validateProviderExecutionEvent(event *ProviderExecutionEventEnvelope) error {
	event.ProjectID, event.Provider, event.SourceEventID, event.SourceCursor, event.ParentProviderSessionID, event.ChildHandle, event.AgentType, event.Label, event.Status, event.OccurredAt, event.VisibilityDegradedReason = strings.TrimSpace(event.ProjectID), strings.ToLower(strings.TrimSpace(event.Provider)), strings.TrimSpace(event.SourceEventID), strings.TrimSpace(event.SourceCursor), strings.TrimSpace(event.ParentProviderSessionID), strings.TrimSpace(event.ChildHandle), strings.TrimSpace(event.AgentType), strings.TrimSpace(event.Label), strings.ToLower(strings.TrimSpace(event.Status)), strings.TrimSpace(event.OccurredAt), strings.TrimSpace(event.VisibilityDegradedReason)
	if event.Version != providerObservationVersion {
		return providerObservationError("unsupported provider observation envelope version")
	}
	if event.ProjectID == "" || !validProviderObservationProvider(event.Provider) {
		return providerObservationError("provider observation requires a supported provider and project")
	}
	if event.SourceEventID == "" && event.SourceSequence <= 0 {
		return providerObservationError("provider observation requires source event id or positive sequence")
	}
	if event.ParentProviderSessionID == "" {
		return providerObservationError("provider observation requires parent provider session id")
	}
	if !validProviderObservationStatus(event.Status) {
		return providerObservationError("unsupported provider observation status")
	}
	if _, err := time.Parse(time.RFC3339Nano, event.OccurredAt); err != nil {
		return providerObservationError("provider observation timestamp must be RFC3339")
	}
	for _, value := range []string{event.ProjectID, event.Provider, event.SourceEventID, event.SourceCursor, event.ParentProviderSessionID, event.ChildHandle, event.AgentType, event.Label} {
		if len(value) > providerObservationMaxMetaValue {
			return providerObservationError("provider observation field exceeds bounded length")
		}
	}
	if len(event.VisibilityDegradedReason) > providerObservationMaxMetaValue {
		return providerObservationError("provider observation degradation reason exceeds bounded length")
	}
	return nil
}

func normalizeProviderCapabilities(input []ProviderCapabilityFact) ([]ProviderCapabilityFact, bool, string, error) {
	if len(input) > providerObservationMaxMetaKeys {
		return nil, false, "", providerObservationError("provider observation has too many capability facts")
	}
	out := make([]ProviderCapabilityFact, 0, len(input))
	degraded := false
	for _, fact := range input {
		fact.Name, fact.State, fact.Provenance, fact.FreshAt = strings.TrimSpace(fact.Name), strings.ToLower(strings.TrimSpace(fact.State)), strings.TrimSpace(fact.Provenance), strings.TrimSpace(fact.FreshAt)
		if fact.Name == "" || fact.Provenance == "" || fact.FreshAt == "" {
			return nil, false, "", providerObservationError("provider capability requires name, provenance, and freshness")
		}
		if _, err := time.Parse(time.RFC3339Nano, fact.FreshAt); err != nil {
			return nil, false, "", providerObservationError("provider capability freshness must be RFC3339")
		}
		if fact.State != "true" && fact.State != "false" && fact.State != "unknown" {
			fact.State, degraded = "unknown", true
		}
		out = append(out, fact)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if degraded {
		return out, true, "unsupported_capability_state", nil
	}
	return out, false, "", nil
}

func sanitizeProviderMetadata(input map[string]any) (map[string]any, bool, error) {
	if len(input) > providerObservationMaxMetaKeys {
		return nil, false, providerObservationError("provider observation metadata has too many keys")
	}
	out := map[string]any{}
	truncated := false
	for _, key := range providerMetadataSortedKeys(input) {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" || len(cleanKey) > 128 {
			return nil, false, providerObservationError("provider observation metadata key is invalid")
		}
		if providerMetadataSecretKey(cleanKey) {
			out[cleanKey] = "[REDACTED]"
			truncated = true
			continue
		}
		value, valueTruncated, err := sanitizeProviderMetadataValue(input[key], 1)
		if err != nil {
			return nil, false, err
		}
		out[cleanKey], truncated = value, truncated || valueTruncated
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, false, providerObservationError("provider observation metadata is not JSON encodable")
	}
	if len(raw) > providerObservationMaxMetadata {
		return nil, false, providerObservationError("provider observation metadata exceeds 16KiB")
	}
	return out, truncated, nil
}

func sanitizeProviderMetadataValue(value any, depth int) (any, bool, error) {
	if depth > providerObservationMaxMetaDepth {
		return nil, false, providerObservationError("provider observation metadata nesting exceeds limit")
	}
	switch current := value.(type) {
	case nil, bool, float64, float32, int, int64, int32, json.Number:
		return current, false, nil
	case string:
		if len(current) > providerObservationMaxMetaValue {
			return current[:providerObservationMaxMetaValue], true, nil
		}
		return current, false, nil
	case []any:
		if len(current) > providerObservationMaxMetaKeys {
			return nil, false, providerObservationError("provider observation metadata array exceeds limit")
		}
		out := make([]any, 0, len(current))
		truncated := false
		for _, item := range current {
			clean, cut, err := sanitizeProviderMetadataValue(item, depth+1)
			if err != nil {
				return nil, false, err
			}
			out, truncated = append(out, clean), truncated || cut
		}
		return out, truncated, nil
	case map[string]any:
		if len(current) > providerObservationMaxMetaKeys {
			return nil, false, providerObservationError("provider observation metadata object exceeds limit")
		}
		out := map[string]any{}
		truncated := false
		for _, key := range providerMetadataSortedKeys(current) {
			if providerMetadataSecretKey(key) {
				out[key] = "[REDACTED]"
				truncated = true
				continue
			}
			clean, cut, err := sanitizeProviderMetadataValue(current[key], depth+1)
			if err != nil {
				return nil, false, err
			}
			out[key], truncated = clean, truncated || cut
		}
		return out, truncated, nil
	default:
		return nil, false, providerObservationError("provider observation metadata contains unsupported value")
	}
}

func providerMetadataSortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func providerMetadataSecretKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "authorization") || strings.Contains(key, "credential") || strings.Contains(key, "api_key")
}
