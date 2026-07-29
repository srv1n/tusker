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

// CodexExecutionObservation is the adapter-local representation of a Codex
// notification, hook, or authoritative Cloud poll.  It deliberately carries
// no task, lease, proof, or outcome fields: it is translated into the bounded
// provider-observation envelope before it reaches the ledger.
type CodexExecutionObservation struct {
	ProjectID                string
	ParentExecutionID        string // optional only when the parent is already attached
	ThreadID                 string // the resumable top-level Codex thread
	SourceEventID            string
	SourceSequence           int64
	SourceCursor             string
	Kind                     string
	Status                   string
	OccurredAt               string
	ChildID                  string
	AgentType                string
	Label                    string
	Capabilities             []ProviderCapabilityFact
	Metadata                 map[string]any
	VisibilityDegradedReason string
}

// CodexExecutionAdapter translates the three Codex observation surfaces into
// the generic ledger. Notifications are hints; callers can replay either
// notifications or authoritative polls because source identities are stable.
// It never creates a run, binds work, or writes process/outcome authority.
type CodexExecutionAdapter struct{ Store *RuntimeStore }

// ObserveRunPayload is the runner seam for local JSONL and authoritative Cloud
// polls. It observes only already-correlated execution records; a runner row
// without an execution identity remains deliberately unbound rather than
// causing a synthetic delivery owner to appear.
func (a CodexExecutionAdapter) ObserveRunPayload(run RunStatus, payload any, sequence int64, source string) (bool, error) {
	if a.Store == nil {
		return false, providerObservationError("codex execution adapter store is nil")
	}
	threadID := strings.TrimSpace(run.SessionRef)
	if RunnerName(run.Runner) == RunnerCodexCloud {
		threadID = strings.TrimSpace(run.CloudTaskID)
	}
	if threadID == "" {
		threadID = codexPayloadString(payload, "thread_id", "threadId", "session_id", "sessionId", "task_id", "taskId")
	}
	if threadID == "" {
		return false, nil
	}
	parentID, err := a.executionForCodexRun(run, threadID)
	if err != nil || parentID == "" {
		return false, err
	}
	childID := codexPayloadString(payload, "child_id", "childId", "subagent_id", "subagentId", "agent_id", "agentId")
	status := codexPayloadString(payload, "status", "state")
	if status == "" {
		status = "running"
	}
	agentType := codexPayloadString(payload, "agent_type", "agentType", "agent_name", "agentName")
	label := codexPayloadString(payload, "label", "name", "spawn_label", "spawnLabel")
	occurredAt := codexPayloadString(payload, "timestamp", "occurred_at", "occurredAt", "created_at", "createdAt")
	diagnostic := ""
	if occurredAt == "" {
		occurredAt = time.Now().UTC().Format(time.RFC3339Nano)
		diagnostic = "provider_timestamp_missing_requires_authoritative_fetch"
	}
	raw, _ := json.Marshal(payload)
	digest := sha256.Sum256(append(append([]byte(source+":"), raw...), []byte(":"+strconv.FormatInt(sequence, 10))...))
	metadata := map[string]any{"observation_source": source}
	if environmentID := codexPayloadString(payload, "environment_id", "environmentId"); environmentID != "" {
		metadata["environment_id"] = environmentID
	}
	result, err := a.Observe(CodexExecutionObservation{ProjectID: run.ProjectID, ParentExecutionID: parentID, ThreadID: threadID, SourceEventID: source + ":" + hex.EncodeToString(digest[:]), SourceSequence: sequence, Kind: source, Status: status, OccurredAt: occurredAt, ChildID: childID, AgentType: agentType, Label: label, Metadata: metadata, VisibilityDegradedReason: diagnostic})
	if err != nil {
		return false, err
	}
	return !result.Duplicate, nil
}

func (a CodexExecutionAdapter) executionForCodexRun(run RunStatus, providerHandle string) (string, error) {
	var id string
	err := a.Store.queryRowScan(`SELECT execution_id FROM execution_records WHERE project_id = ? AND attempt_id = ? LIMIT 1`, []any{run.ProjectID, run.ActiveAttemptID}, &id)
	if err == nil {
		return id, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	err = a.Store.queryRowScan(`SELECT execution_id FROM execution_attachment_events WHERE project_id = ? AND provider = 'codex' AND provider_session_id = ? LIMIT 1`, []any{run.ProjectID, providerHandle}, &id)
	if err != nil {
		return "", nil
	}
	return id, nil
}

func codexPayloadString(value any, keys ...string) string {
	switch current := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if candidate := strings.TrimSpace(stringValue(current[key])); candidate != "" {
				return candidate
			}
		}
		for _, nested := range current {
			if candidate := codexPayloadString(nested, keys...); candidate != "" {
				return candidate
			}
		}
	case []any:
		for _, nested := range current {
			if candidate := codexPayloadString(nested, keys...); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func (a CodexExecutionAdapter) Observe(observation CodexExecutionObservation) (ProviderObservationResult, error) {
	if a.Store == nil {
		return ProviderObservationResult{}, providerObservationError("codex execution adapter store is nil")
	}
	observation.ProjectID = strings.TrimSpace(observation.ProjectID)
	observation.ThreadID = strings.TrimSpace(observation.ThreadID)
	if observation.ProjectID == "" || observation.ThreadID == "" {
		return ProviderObservationResult{}, providerObservationError("codex observation requires project and top-level thread id")
	}
	status := codexObservationStatus(observation.Status, observation.Kind)
	occurredAt := strings.TrimSpace(observation.OccurredAt)
	if occurredAt == "" {
		occurredAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	eventID := strings.TrimSpace(observation.SourceEventID)
	if eventID == "" && observation.SourceSequence <= 0 {
		eventID = codexObservationIdentity(observation)
	}
	capabilities := observation.Capabilities
	if len(capabilities) == 0 {
		capabilities = codexDefaultCapabilities(occurredAt)
	}
	diagnostic := strings.TrimSpace(observation.VisibilityDegradedReason)
	if !knownCodexObservationStatus(observation.Status, observation.Kind) {
		diagnostic = "unrecognized_provider_status_requires_authoritative_fetch"
	}
	envelope := ProviderExecutionEventEnvelope{
		Version: providerObservationVersion, ProjectID: observation.ProjectID, Provider: "codex",
		SourceEventID: eventID, SourceSequence: observation.SourceSequence, SourceCursor: strings.TrimSpace(observation.SourceCursor),
		ParentProviderSessionID: observation.ThreadID, ChildHandle: strings.TrimSpace(observation.ChildID),
		AgentType: strings.TrimSpace(observation.AgentType), Label: strings.TrimSpace(observation.Label),
		Status: status, OccurredAt: occurredAt, Capabilities: capabilities, Metadata: observation.Metadata, VisibilityDegradedReason: diagnostic,
	}
	// Validate every bounded/untrusted field before adding the durable parent
	// correlation. A malformed hook must be a no-op, including attachments.
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
			return ProviderObservationResult{}, providerObservationError("codex observation parent execution is not available in this project")
		}
		// Attachment is an audited correlation event. It is the only identity
		// bridge here; child IDs never become the parent resume identity.
		if _, _, err := a.Store.AttachExecution(ExecutionAttachmentInput{
			ProjectID: observation.ProjectID, ExecutionID: parentID, Provider: "codex",
			ProviderSessionID: observation.ThreadID, SessionRef: observation.ThreadID,
			Source: "codex_adapter", Actor: "provider_observation",
		}); err != nil {
			return ProviderObservationResult{}, err
		}
	}
	if codexProviderStatusRegression(a.Store, envelope) {
		envelope.VisibilityDegradedReason = "provider_status_regression_requires_authoritative_fetch"
	}
	return a.Store.ApplyProviderExecutionEvent(envelope)
}

func knownCodexObservationStatus(status, kind string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(kind))
	}
	switch status {
	case "queued", "created", "started", "starting", "active", "in_progress", "running", "cancel_requested", "interrupt_requested", "cancelled", "canceled", "complete", "completed", "terminal", "succeeded", "error", "failed", "acknowledged":
		return true
	default:
		return false
	}
}

// ObserveCloud records Cloud state as a provider observation, not a local
// process result. In particular it accepts no PID/heartbeat/OS-settlement
// metadata and always uses the durable Cloud task as the parent handle.
func (a CodexExecutionAdapter) ObserveCloud(projectID, executionID, cloudTaskID, environmentID, status, occurredAt string, sequence int64, cursor string, children []CodexExecutionObservation) ([]ProviderObservationResult, error) {
	if err := rejectCodexCloudLocalProcessMetadata(map[string]any{"environment_id": environmentID}); err != nil {
		return nil, err
	}
	if strings.TrimSpace(occurredAt) == "" {
		occurredAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	metadata := map[string]any{"environment_id": strings.TrimSpace(environmentID), "observation_source": "authoritative_cloud_fetch"}
	root := CodexExecutionObservation{ProjectID: projectID, ParentExecutionID: executionID, ThreadID: cloudTaskID, SourceSequence: sequence, SourceCursor: cursor, Kind: "cloud_status", Status: status, OccurredAt: occurredAt, Metadata: metadata,
		Capabilities: []ProviderCapabilityFact{{Name: "parent_interrupt", State: "unknown", Provenance: "codex_cloud_status", FreshAt: occurredAt}, {Name: "independent_child_control", State: "unknown", Provenance: "codex_cloud_status", FreshAt: occurredAt}, {Name: "resume", State: "false", Provenance: "codex_cloud_status", FreshAt: occurredAt}, {Name: "enumeration", State: "true", Provenance: "codex_cloud_status", FreshAt: occurredAt}, {Name: "replay", State: "true", Provenance: "codex_cloud_status", FreshAt: occurredAt}}}
	results := make([]ProviderObservationResult, 0, 1+len(children))
	result, err := a.Observe(root)
	if err != nil {
		return nil, err
	}
	results = append(results, result)
	for i := range children {
		child := children[i]
		child.ProjectID, child.ParentExecutionID, child.ThreadID = projectID, executionID, cloudTaskID
		if child.SourceSequence == 0 {
			child.SourceSequence = sequence + int64(i) + 1
		}
		if child.SourceCursor == "" {
			child.SourceCursor = cursor
		}
		if child.OccurredAt == "" {
			child.OccurredAt = occurredAt
		}
		if child.Metadata == nil {
			child.Metadata = map[string]any{}
		}
		if err := rejectCodexCloudLocalProcessMetadata(child.Metadata); err != nil {
			return nil, err
		}
		child.Metadata["environment_id"] = strings.TrimSpace(environmentID)
		child.Metadata["observation_source"] = "authoritative_cloud_fetch"
		result, err = a.Observe(child)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func codexProviderStatusRegression(store *RuntimeStore, event ProviderExecutionEventEnvelope) bool {
	if store == nil || !validProviderObservationStatus(event.Status) {
		return false
	}
	var prior string
	err := store.queryRowScan(`SELECT status FROM provider_execution_observations WHERE project_id = ? AND provider = 'codex' AND parent_provider_session_id = ? ORDER BY received_at DESC, observation_id DESC LIMIT 1`, []any{event.ProjectID, event.ParentProviderSessionID}, &prior)
	if err != nil {
		return false
	}
	terminal := prior == "terminal" || prior == "failed" || prior == "cancelled"
	return terminal && (event.Status == "starting" || event.Status == "running" || event.Status == "interrupt_requested")
}

func rejectCodexCloudLocalProcessMetadata(metadata map[string]any) error {
	for key, value := range metadata {
		normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.TrimSpace(key)))
		if strings.Contains(normalized, "pid") || strings.Contains(normalized, "pgid") || strings.Contains(normalized, "heartbeat") || strings.Contains(normalized, "ossettlement") || strings.Contains(normalized, "process") {
			return providerObservationError("codex cloud observation cannot contain local process facts")
		}
		switch nested := value.(type) {
		case map[string]any:
			if err := rejectCodexCloudLocalProcessMetadata(nested); err != nil {
				return err
			}
		case []any:
			for _, item := range nested {
				if object, ok := item.(map[string]any); ok {
					if err := rejectCodexCloudLocalProcessMetadata(object); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func codexObservationStatus(status, kind string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(kind))
	}
	switch status {
	case "queued", "created", "started", "starting":
		return "starting"
	case "active", "in_progress", "running":
		return "running"
	case "cancel_requested", "interrupt_requested":
		return "interrupt_requested"
	case "cancelled", "canceled":
		return "cancelled"
	case "complete", "completed", "terminal", "succeeded":
		return "terminal"
	case "error", "failed":
		return "failed"
	case "acknowledged":
		return "acknowledged"
	default:
		return "unknown"
	}
}

func codexObservationIdentity(observation CodexExecutionObservation) string {
	// This is deliberately a content identity only for adapter calls lacking a
	// native event ID/sequence. It makes restart replay harmless while marking
	// opaque delivery as degraded in the generic ingestion layer.
	payload, _ := json.Marshal([]string{observation.ThreadID, observation.ChildID, observation.Kind, observation.Status, observation.OccurredAt, observation.Label, observation.AgentType})
	digest := sha256.Sum256(payload)
	return "codex:" + hex.EncodeToString(digest[:])
}

func codexDefaultCapabilities(at string) []ProviderCapabilityFact {
	return []ProviderCapabilityFact{
		{Name: "parent_interrupt", State: "unknown", Provenance: "codex_adapter_default", FreshAt: at},
		{Name: "independent_child_control", State: "unknown", Provenance: "codex_adapter_default", FreshAt: at},
		{Name: "resume", State: "unknown", Provenance: "codex_adapter_default", FreshAt: at},
		{Name: "enumeration", State: "unknown", Provenance: "codex_adapter_default", FreshAt: at},
		{Name: "replay", State: "unknown", Provenance: "codex_adapter_default", FreshAt: at},
	}
}
