package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// runSessionControlIntent is the durable operator intent for a single run.
// It lives in daemon_settings so a control survives a Serve/daemon restart
// without adding a second runtime projection or transcript store.
type runSessionControlIntent struct {
	Schema          string `json:"schema"`
	Action          string `json:"action"`
	State           string `json:"state"`
	ProjectID       string `json:"project_id"`
	RecordID        string `json:"record_id"`
	ItemID          string `json:"item_id"`
	Actor           string `json:"actor"`
	Reason          string `json:"reason,omitempty"`
	LeaseGeneration int    `json:"lease_generation"`
	LeaseOwner      string `json:"lease_owner,omitempty"`
	AttemptID       string `json:"attempt_id,omitempty"`
	ProcessPID      int    `json:"process_pid,omitempty"`
	ProcessPGID     int    `json:"process_pgid,omitempty"`
	ProcessStarted  string `json:"process_started_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

const (
	runSessionControlSchema            = "tusker.run-session-control/v1"
	runSessionControlKey               = "run_session_control:"
	runSessionControlPause             = "pause"
	runSessionControlStop              = "stop"
	runSessionControlFresh             = "start_fresh"
	runSessionControlContextRecovery   = "recover_context"
	runSessionControlPending           = "pending"
	runSessionControlQueued            = "queued"
	runSessionControlSettledState      = "settled"
	runSessionControlUnknown           = "unknown"
	runSessionControlFreshReasonPrefix = "start fresh requested by "
)

func runSessionControlSettingKey(projectID, recordID string) string {
	return runSessionControlKey + strings.TrimSpace(projectID) + ":" + strings.TrimSpace(recordID)
}

func loadRunSessionControlIntent(store *RuntimeStore, projectID, recordID string) (*runSessionControlIntent, error) {
	if store == nil {
		return nil, fmt.Errorf("runtime store is required")
	}
	raw, err := store.GetSetting(runSessionControlSettingKey(projectID, recordID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, err
	}
	var intent runSessionControlIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return nil, fmt.Errorf("decode run control intent: %w", err)
	}
	return &intent, nil
}

func saveRunSessionControlIntent(store *RuntimeStore, intent runSessionControlIntent) error {
	if store == nil {
		return fmt.Errorf("runtime store is required")
	}
	intent.Schema = runSessionControlSchema
	intent.ProjectID = strings.TrimSpace(intent.ProjectID)
	intent.RecordID = strings.TrimSpace(intent.RecordID)
	intent.Action = strings.TrimSpace(intent.Action)
	intent.State = strings.TrimSpace(intent.State)
	if intent.ProjectID == "" || intent.RecordID == "" || intent.Action == "" || intent.State == "" {
		return fmt.Errorf("run control intent identity is incomplete")
	}
	if intent.UpdatedAt == "" {
		intent.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	return store.SetSetting(runSessionControlSettingKey(intent.ProjectID, intent.RecordID), string(raw))
}

func saveRunSessionControlIntentIfPrior(store *RuntimeStore, intent runSessionControlIntent, prior *runSessionControlIntent) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("runtime store is required")
	}
	intent.Schema = runSessionControlSchema
	if intent.ProjectID == "" || intent.RecordID == "" || intent.Action == "" || intent.State == "" {
		return false, fmt.Errorf("run control intent identity is incomplete")
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return false, err
	}
	expected := ""
	if prior != nil {
		previous, err := json.Marshal(prior)
		if err != nil {
			return false, err
		}
		expected = string(previous)
	}
	return store.SetSettingIfValue(runSessionControlSettingKey(intent.ProjectID, intent.RecordID), expected, string(raw))
}

func runSessionControlPauseReason(run RunStatus) string {
	// RunnerCapabilities has no negotiated pause capability today. Returning a
	// refusal is deliberate: SIGSTOP would look durable while bypassing the
	// provider's acknowledgement and recovery protocol.
	return fmt.Sprintf("runner %s has no negotiated pause capability; use Stop, which remains durable and resumable when settlement is confirmed", firstNonEmpty(run.Runner, "unknown"))
}

func runSessionControlSettled(run RunStatus) bool {
	if runProcessGroupAlive(run) {
		return false
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued:
		return false
	default:
		return true
	}
}

func runSessionControlFreshRun(expected RunStatus, actor string, now time.Time) RunStatus {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fresh := expected
	fresh.LeaseState = string(LeaseStateRetryQueued)
	fresh.AttemptOutcome = string(AttemptOutcomeNone)
	fresh.NextRetryAt = now.UTC().Format(time.RFC3339Nano)
	fresh.LastError = runSessionControlFreshReasonPrefix + firstNonEmpty(strings.TrimSpace(actor), defaultActorName())
	fresh.LastEventAt = now.UTC().Format(time.RFC3339Nano)
	fresh.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	fresh.Terminal = false
	fresh.SessionRef = ""
	fresh.CloudTaskID = ""
	fresh.CloudStatus = ""
	fresh.CloudEnvironmentID = ""
	fresh.CloudAttemptNumber = 0
	clearActiveExecution(&fresh)
	return fresh
}
