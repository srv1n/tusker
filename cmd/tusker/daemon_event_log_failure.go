package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	invariantCheckEventLogPersistence       = "event_log_persistence"
	eventLogPersistenceFailureSettingKey    = "event_log_persistence_failures"
	eventLogPersistenceFailureSchemaVersion = 1
	eventLogPersistenceFailureCASAttempts   = 32
)

type eventLogPersistenceFailure struct {
	EventSinkPath string `json:"event_sink_path,omitempty"`
	EventKind     string `json:"event_kind"`
	RecordID      string `json:"record_id,omitempty"`
	Reason        string `json:"reason"`
	OpenedAt      string `json:"opened_at"`
	LastFailedAt  string `json:"last_failed_at"`
}

type eventLogPersistenceFailureRegistry struct {
	Version  int                          `json:"version"`
	Failures []eventLogPersistenceFailure `json:"failures"`
}

func (d *Daemon) tripEventLogPersistenceCircuit(eventKind, recordID string, appendErr error) {
	if appendErr == nil {
		return
	}
	eventKind = firstNonEmpty(strings.TrimSpace(eventKind), "runtime_event")
	recordID = strings.TrimSpace(recordID)
	detail := fmt.Sprintf("cannot persist %s event", eventKind)
	if recordID != "" {
		detail += " for " + recordID
	}
	detail += ": " + appendErr.Error()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	failure := eventLogPersistenceFailure{
		EventKind:    eventKind,
		RecordID:     recordID,
		Reason:       appendErr.Error(),
		OpenedAt:     now,
		LastFailedAt: now,
	}
	if d != nil && d.store != nil {
		if path, err := d.eventSinkPathForRecord(recordID); err == nil {
			failure.EventSinkPath = path
		} else {
			failure.Reason += "; resolve event sink: " + err.Error()
		}
		failures, err := d.store.addEventLogPersistenceFailure(failure)
		if err != nil {
			fmt.Fprintf(os.Stderr, "event_log_persistence_failure: %s; cannot persist failed sink registry: %v\n", detail, err)
			status := mergeEventLogPersistenceFailures(invariantCircuitStatus{
				Open:          true,
				OpenedAt:      now,
				LastCheckedAt: now,
			}, []eventLogPersistenceFailure{failure})
			if statusErr := d.store.SetInvariantCircuitStatus(status); statusErr != nil {
				fmt.Fprintf(os.Stderr, "event_log_persistence_failure: %s; cannot open fallback invariant circuit: %v\n", detail, statusErr)
			}
			return
		}
		status := mergeEventLogPersistenceFailures(invariantCircuitStatus{
			Open:          true,
			OpenedAt:      now,
			LastCheckedAt: now,
		}, failures)
		if err := d.store.SetInvariantCircuitStatus(status); err != nil {
			fmt.Fprintf(os.Stderr, "event_log_persistence_failure: %s; cannot open invariant circuit: %v\n", detail, err)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "event_log_persistence_failure: %s\n", detail)
}

func (d *Daemon) eventSinkPathForRecord(recordID string) (string, error) {
	if d == nil || d.store == nil || strings.TrimSpace(recordID) == "" {
		return "", nil
	}
	run, err := d.store.FindRun(recordID)
	if err != nil {
		return "", err
	}
	if run == nil {
		return "", nil
	}
	return strings.TrimSpace(run.EventSinkPath), nil
}

func (s *RuntimeStore) ReadEventLogPersistenceFailures() ([]eventLogPersistenceFailure, error) {
	registry, _, err := s.readEventLogPersistenceFailureRegistry()
	return registry.Failures, err
}

func (s *RuntimeStore) readEventLogPersistenceFailureRegistry() (eventLogPersistenceFailureRegistry, string, error) {
	registry := eventLogPersistenceFailureRegistry{Version: eventLogPersistenceFailureSchemaVersion}
	if s == nil {
		return registry, "", nil
	}
	raw, err := s.GetSetting(eventLogPersistenceFailureSettingKey)
	if err != nil {
		return registry, raw, fmt.Errorf("read event-log persistence failure registry: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return registry, raw, nil
	}
	if err := json.Unmarshal([]byte(raw), &registry); err != nil {
		return eventLogPersistenceFailureRegistry{}, raw, fmt.Errorf("read event-log persistence failure registry: malformed JSON: %w", err)
	}
	if registry.Version != eventLogPersistenceFailureSchemaVersion {
		return eventLogPersistenceFailureRegistry{}, raw, fmt.Errorf("read event-log persistence failure registry: unsupported version %d", registry.Version)
	}
	registry.Failures = normalizeEventLogPersistenceFailures(registry.Failures)
	for _, failure := range registry.Failures {
		if strings.TrimSpace(failure.EventKind) == "" || strings.TrimSpace(failure.Reason) == "" || strings.TrimSpace(failure.OpenedAt) == "" {
			return eventLogPersistenceFailureRegistry{}, raw, fmt.Errorf("read event-log persistence failure registry: invalid failure entry for %q", failure.RecordID)
		}
	}
	return registry, raw, nil
}

func (s *RuntimeStore) addEventLogPersistenceFailure(failure eventLogPersistenceFailure) ([]eventLogPersistenceFailure, error) {
	for attempt := 0; attempt < eventLogPersistenceFailureCASAttempts; attempt++ {
		registry, raw, err := s.readEventLogPersistenceFailureRegistry()
		if err != nil {
			return nil, err
		}
		key := eventLogPersistenceFailureKey(failure)
		replaced := false
		for index := range registry.Failures {
			if eventLogPersistenceFailureKey(registry.Failures[index]) != key {
				continue
			}
			failure.OpenedAt = firstNonEmpty(registry.Failures[index].OpenedAt, failure.OpenedAt)
			registry.Failures[index] = failure
			replaced = true
			break
		}
		if !replaced {
			registry.Failures = append(registry.Failures, failure)
		}
		registry.Failures = normalizeEventLogPersistenceFailures(registry.Failures)
		updated, err := s.compareAndSwapEventLogPersistenceFailureRegistry(raw, registry.Failures)
		if err != nil {
			return nil, err
		}
		if updated {
			return registry.Failures, nil
		}
	}
	return nil, fmt.Errorf("persist event-log failure registry: concurrent updates did not settle after %d attempts", eventLogPersistenceFailureCASAttempts)
}

func (s *RuntimeStore) compareAndSwapEventLogPersistenceFailureRegistry(raw string, failures []eventLogPersistenceFailure) (bool, error) {
	registry := eventLogPersistenceFailureRegistry{
		Version:  eventLogPersistenceFailureSchemaVersion,
		Failures: normalizeEventLogPersistenceFailures(failures),
	}
	encoded, err := json.Marshal(registry)
	if err != nil {
		return false, fmt.Errorf("marshal event-log persistence failure registry: %w", err)
	}
	var result interface {
		RowsAffected() (int64, error)
	}
	if strings.TrimSpace(raw) == "" {
		if len(registry.Failures) == 0 {
			return true, nil
		}
		result, err = s.exec(`INSERT INTO daemon_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`, eventLogPersistenceFailureSettingKey, string(encoded))
	} else {
		result, err = s.exec(`UPDATE daemon_settings SET value = ? WHERE key = ? AND value = ?`, string(encoded), eventLogPersistenceFailureSettingKey, raw)
	}
	if err != nil {
		return false, fmt.Errorf("persist event-log failure registry: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("persist event-log failure registry: %w", err)
	}
	return affected == 1, nil
}

func normalizeEventLogPersistenceFailures(failures []eventLogPersistenceFailure) []eventLogPersistenceFailure {
	out := append([]eventLogPersistenceFailure(nil), failures...)
	for index := range out {
		out[index].EventSinkPath = strings.TrimSpace(out[index].EventSinkPath)
		out[index].EventKind = firstNonEmpty(strings.TrimSpace(out[index].EventKind), "runtime_event")
		out[index].RecordID = strings.TrimSpace(out[index].RecordID)
		out[index].Reason = strings.TrimSpace(out[index].Reason)
		out[index].OpenedAt = strings.TrimSpace(out[index].OpenedAt)
		out[index].LastFailedAt = strings.TrimSpace(out[index].LastFailedAt)
	}
	sort.Slice(out, func(i, j int) bool {
		return eventLogPersistenceFailureKey(out[i]) < eventLogPersistenceFailureKey(out[j])
	})
	return out
}

func eventLogPersistenceFailureKey(failure eventLogPersistenceFailure) string {
	eventIdentity := strings.TrimSpace(failure.EventKind) + "\x00" + strings.TrimSpace(failure.RecordID)
	if path := strings.TrimSpace(failure.EventSinkPath); path != "" {
		return "path\x00" + path + "\x00" + eventIdentity
	}
	return "record\x00" + eventIdentity
}

func mergeEventLogPersistenceFailures(status invariantCircuitStatus, failures []eventLogPersistenceFailure) invariantCircuitStatus {
	if len(failures) == 0 {
		return status
	}
	failures = normalizeEventLogPersistenceFailures(failures)
	status.Open = true
	status.Reason = "event_log_persistence_failure"
	status.Checks = uniqueStrings(append(status.Checks, invariantCheckEventLogPersistence))
	for _, failure := range failures {
		detail := fmt.Sprintf("cannot persist %s event", failure.EventKind)
		if failure.RecordID != "" {
			detail += " for " + failure.RecordID
		}
		if failure.EventSinkPath != "" {
			detail += " to " + failure.EventSinkPath
		}
		detail += ": " + failure.Reason
		status.Violations = append(status.Violations, runtimeInvariantViolation{
			Check:    invariantCheckEventLogPersistence,
			RecordID: failure.RecordID,
			Detail:   detail,
			Fields: map[string]any{
				"event_kind":      failure.EventKind,
				"event_sink_path": failure.EventSinkPath,
				"opened_at":       failure.OpenedAt,
				"last_failed_at":  failure.LastFailedAt,
			},
		})
	}
	status.Summary = status.Violations[len(status.Violations)-len(failures)].Detail
	return status
}

func (d *Daemon) probeEventLogPersistenceFailures(failures []eventLogPersistenceFailure) []error {
	probeErrors := make([]error, len(failures))
	indexes := make([]int, len(failures))
	for index := range failures {
		indexes[index] = index
	}
	sort.Slice(indexes, func(i, j int) bool {
		left, right := failures[indexes[i]], failures[indexes[j]]
		leftPriority := eventLogPersistenceRecoveryPriority(left)
		rightPriority := eventLogPersistenceRecoveryPriority(right)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return eventLogPersistenceFailureKey(left) < eventLogPersistenceFailureKey(right)
	})
	for _, index := range indexes {
		path := strings.TrimSpace(failures[index].EventSinkPath)
		if path == "" {
			resolved, err := d.eventSinkPathForRecord(failures[index].RecordID)
			if err != nil {
				probeErrors[index] = fmt.Errorf("resolve event sink for %s: %w", failures[index].RecordID, err)
				continue
			}
			path = resolved
			failures[index].EventSinkPath = path
		}
		if path == "" {
			probeErrors[index] = fmt.Errorf("no event sink path is recorded for %s", firstNonEmpty(failures[index].RecordID, failures[index].EventKind))
			continue
		}
		var err error
		switch strings.TrimSpace(failures[index].EventKind) {
		case "supervisor_decision", "supervisor_decision_store", "supervisor_decision_lookup":
			err = d.replayEventLogPersistenceFailure(path, failures[index])
		}
		if err == nil {
			err = NewEventLog(path).Append("event_log_persistence_probe", "", RunnerName("daemon"), map[string]any{
				"record_id": failures[index].RecordID,
				"repair_of": failures[index].EventKind,
			})
		}
		if err != nil {
			err = fmt.Errorf("recover event sink %q: %w", path, err)
		}
		probeErrors[index] = err
	}
	return probeErrors
}

func eventLogPersistenceRecoveryPriority(failure eventLogPersistenceFailure) int {
	if strings.TrimSpace(failure.EventKind) == "supervisor_decision" {
		return 0
	}
	return 1
}

func (d *Daemon) replayEventLogPersistenceFailure(path string, failure eventLogPersistenceFailure) error {
	if strings.TrimSpace(failure.EventKind) != "supervisor_decision" {
		return fmt.Errorf("quarantined failed %s event for %s: automatic replay is unavailable", firstNonEmpty(failure.EventKind, "runtime_event"), firstNonEmpty(failure.RecordID, path))
	}
	if d == nil || d.store == nil {
		return fmt.Errorf("quarantined failed supervisor_decision event for %s: runtime store is unavailable", firstNonEmpty(failure.RecordID, path))
	}
	recordID := strings.TrimSpace(failure.RecordID)
	if recordID == "" {
		return fmt.Errorf("quarantined failed supervisor_decision event for %s: record ID is unavailable", path)
	}
	run, err := d.store.FindRun(recordID)
	if err != nil {
		return fmt.Errorf("load canonical supervisor decision for %s: %w", recordID, err)
	}
	if run == nil || strings.TrimSpace(run.ProjectID) == "" {
		return fmt.Errorf("quarantined failed supervisor_decision event for %s: canonical run is unavailable", recordID)
	}
	decisions, err := d.store.ListSupervisorDecisionsForRun(run.ProjectID, recordID)
	if err != nil {
		return fmt.Errorf("load canonical supervisor decision for %s: %w", recordID, err)
	}
	if len(decisions) == 0 {
		return fmt.Errorf("quarantined failed supervisor_decision event for %s: no canonical decision is available for replay", recordID)
	}
	log := NewEventLog(path)
	for _, decision := range decisions {
		if err := log.Append("supervisor_decision", decision.AttemptID, RunnerName(decision.Runner), supervisorDecisionEventPayload(decision)); err != nil {
			return fmt.Errorf("replay supervisor decision %s for %s: %w", firstNonEmpty(decision.DecisionID, "<unknown>"), recordID, err)
		}
	}
	return nil
}

func supervisorDecisionEventPayload(decision SupervisorDecision) map[string]any {
	return map[string]any{
		"decision_id":           decision.DecisionID,
		"project_id":            decision.ProjectID,
		"record_id":             decision.RecordID,
		"item_id":               decision.ItemID,
		"runner":                decision.Runner,
		"work_revision":         decision.WorkRevision,
		"attempt_id":            decision.AttemptID,
		"parent_attempt_id":     decision.ParentAttemptID,
		"session_ref":           decision.SessionRef,
		"parent_session_ref":    decision.ParentSessionRef,
		"target_attempt_id":     decision.TargetAttemptID,
		"target_session_ref":    decision.TargetSessionRef,
		"kind":                  decision.Kind,
		"reason":                decision.Reason,
		"branch_name":           decision.BranchName,
		"workspace_path":        decision.WorkspacePath,
		"validation_delta":      decision.ValidationDelta,
		"merge_rule":            decision.MergeRule,
		"lease_state":           decision.LeaseState,
		"context_signal":        decision.ContextSignal,
		"input_tokens":          decision.InputTokens,
		"output_tokens":         decision.OutputTokens,
		"total_tokens":          decision.TotalTokens,
		"context_window_tokens": decision.ContextWindowTokens,
		"created_at":            decision.CreatedAt,
	}
}
