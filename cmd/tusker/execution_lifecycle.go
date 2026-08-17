package main

// Execution lifecycle is an evidence projection, not a second scheduler.  In
// particular, provider observations and an OS process can disagree for a
// while; retaining both facts is the point of this model.

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const executionLifecycleSchema = "tusker.execution-lifecycle/v1"

type ExecutionLifecycleEvidence struct {
	ExecutionID         string `json:"execution_id"`
	DeliveryState       string `json:"delivery_state"`
	AdmissionState      string `json:"admission_state"`
	ProcessState        string `json:"process_state"`
	ProviderState       string `json:"provider_state"`
	OutcomeState        string `json:"outcome_state"`
	SessionState        string `json:"session_state"`
	ChildAttentionState string `json:"child_attention_state"`
	DerivedPhase        string `json:"derived_phase"`
	ObservedAt          string `json:"observed_at"`
}

type ExecutionCancellationEvidence struct {
	CancellationID  string `json:"cancellation_id"`
	ExecutionID     string `json:"execution_id"`
	RequestKey      string `json:"request_key"`
	Target          string `json:"target"`
	Stage           string `json:"stage"`
	Detail          string `json:"detail,omitempty"`
	LeaseGeneration int    `json:"lease_generation"`
	ProcessPID      int    `json:"process_pid"`
	ProcessStart    string `json:"process_start,omitempty"`
	OccurredAt      string `json:"occurred_at"`
}

type ExecutionControlAvailability struct {
	Available     bool   `json:"available"`
	Action        string `json:"action"`
	Target        string `json:"target,omitempty"`
	Reason        string `json:"reason,omitempty"`
	Provider      string `json:"provider,omitempty"`
	ProviderOwned bool   `json:"provider_owned"`
}

func (s *RuntimeStore) migrateExecutionLifecycle() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS execution_lifecycle_evidence (event_id TEXT PRIMARY KEY, execution_id TEXT NOT NULL, delivery_state TEXT NOT NULL DEFAULT '', admission_state TEXT NOT NULL DEFAULT '', process_state TEXT NOT NULL DEFAULT '', provider_state TEXT NOT NULL DEFAULT '', outcome_state TEXT NOT NULL DEFAULT '', session_state TEXT NOT NULL DEFAULT '', child_attention_state TEXT NOT NULL DEFAULT '', observed_at TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS execution_lifecycle_latest ON execution_lifecycle_evidence(execution_id, observed_at DESC, event_id DESC);`,
		`CREATE TABLE IF NOT EXISTS execution_cancellation_evidence (cancellation_id TEXT NOT NULL, execution_id TEXT NOT NULL, request_key TEXT NOT NULL, target TEXT NOT NULL, stage TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '', lease_generation INTEGER NOT NULL DEFAULT 0, process_pid INTEGER NOT NULL DEFAULT 0, process_start TEXT NOT NULL DEFAULT '', occurred_at TEXT NOT NULL, PRIMARY KEY(cancellation_id, stage), UNIQUE(execution_id, request_key, stage));`,
		`CREATE INDEX IF NOT EXISTS execution_cancel_lookup ON execution_cancellation_evidence(execution_id, request_key, occurred_at);`,
		`CREATE TRIGGER IF NOT EXISTS execution_lifecycle_immutable BEFORE UPDATE ON execution_lifecycle_evidence BEGIN SELECT RAISE(ABORT, 'execution lifecycle evidence is immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_lifecycle_no_delete BEFORE DELETE ON execution_lifecycle_evidence BEGIN SELECT RAISE(ABORT, 'execution lifecycle evidence is immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_cancel_immutable BEFORE UPDATE ON execution_cancellation_evidence BEGIN SELECT RAISE(ABORT, 'execution cancellation evidence is immutable'); END;`,
		`CREATE TRIGGER IF NOT EXISTS execution_cancel_no_delete BEFORE DELETE ON execution_cancellation_evidence BEGIN SELECT RAISE(ABORT, 'execution cancellation evidence is immutable'); END;`,
	} {
		if _, err := s.exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func executionDerivedPhase(f ExecutionLifecycleEvidence) string {
	// This is intentionally only a display shorthand. It never replaces any
	// dimension and terminal provider/process facts do not imply an outcome.
	if f.ChildAttentionState == "needs_attention" {
		return "needs_attention"
	}
	if f.ProcessState == "running" || f.ProviderState == "running" || f.AdmissionState == "admitted" {
		return "active"
	}
	if f.OutcomeState != "" && f.OutcomeState != "unknown" {
		return "settled"
	}
	if f.ProcessState == "lost" || f.ProviderState == "unknown" {
		return "unsettled"
	}
	return "pending"
}

func (s *RuntimeStore) appendExecutionLifecycleEvidence(e ExecutionLifecycleEvidence) error {
	if strings.TrimSpace(e.ExecutionID) == "" {
		return nil
	}
	e.ObservedAt = firstNonEmpty(e.ObservedAt, executionNow())
	_, err := s.exec(`INSERT INTO execution_lifecycle_evidence(event_id, execution_id, delivery_state, admission_state, process_state, provider_state, outcome_state, session_state, child_attention_state, observed_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, "execution-lifecycle-"+newRecordID(), e.ExecutionID, e.DeliveryState, e.AdmissionState, e.ProcessState, e.ProviderState, e.OutcomeState, e.SessionState, e.ChildAttentionState, e.ObservedAt)
	return err
}

func (s *RuntimeStore) ExecutionLifecycle(executionID string) (ExecutionLifecycleEvidence, error) {
	f := ExecutionLifecycleEvidence{ExecutionID: executionID, OutcomeState: "unknown", ProviderState: "unknown", ProcessState: "unknown", SessionState: "unknown", ChildAttentionState: "none"}
	// The latest immutable projection is the restart baseline. Current source
	// facts may add precision, but a read never deletes contradictory evidence.
	var prior ExecutionLifecycleEvidence
	err := s.queryRowScan(`SELECT execution_id, delivery_state, admission_state, process_state, provider_state, outcome_state, session_state, child_attention_state, observed_at FROM execution_lifecycle_evidence WHERE execution_id=? ORDER BY observed_at DESC, event_id DESC LIMIT 1`, []any{executionID}, &prior.ExecutionID, &prior.DeliveryState, &prior.AdmissionState, &prior.ProcessState, &prior.ProviderState, &prior.OutcomeState, &prior.SessionState, &prior.ChildAttentionState, &prior.ObservedAt)
	if err != nil && err != sql.ErrNoRows {
		return f, err
	}
	v, err := s.ExecutionView(executionID)
	if err != nil || v == nil {
		return f, err
	}
	if v.ProofEligible {
		f.DeliveryState = "bound"
	} else {
		f.DeliveryState = "unbound"
	}
	if v.AttemptID != "" {
		f.AdmissionState = "admitted"
	} else {
		f.AdmissionState = "not_admitted"
	}
	if run, err := s.executionGraphRun(*v); err != nil {
		return f, err
	} else if run != nil {
		f.AdmissionState = "admitted"
		f.ProcessState = "not_running"
		if runProcessGroupAlive(*run) {
			f.ProcessState = "running"
		}
		if run.AttemptOutcome != "" && run.AttemptOutcome != string(AttemptOutcomeNone) {
			f.OutcomeState = run.AttemptOutcome
		}
		if firstNonEmpty(run.SessionRef, v.SessionRef) != "" {
			f.SessionState = "known"
		} else {
			f.SessionState = "unknown"
		}
	}
	if obs, err := s.executionLatestObservation(executionID); err != nil {
		return f, err
	} else if obs != nil {
		if strings.TrimSpace(obs.status) != "" {
			f.ProviderState = obs.status
		}
		if obs.degraded {
			f.ProviderState = "unavailable"
		}
	}
	var childAttention int
	// Current attention is derived from the newest fact for each child. Older
	// failures remain immutable timeline evidence but cannot permanently taint a
	// child that has since recovered.
	if err := s.queryRowScan(`SELECT COUNT(*) FROM (
		SELECT o.child_execution_id, o.status,
			ROW_NUMBER() OVER (PARTITION BY o.child_execution_id ORDER BY o.occurred_at DESC, o.source_sequence DESC, o.observation_id DESC) AS ordinal
		FROM provider_execution_observations o
		JOIN execution_edges e ON e.child_execution_id=o.child_execution_id
		WHERE e.parent_execution_id=?
	) WHERE ordinal=1 AND status IN ('unknown','interrupt_requested','failed')`, []any{executionID}, &childAttention); err != nil && err != sql.ErrNoRows {
		return f, err
	}
	if childAttention > 0 {
		f.ChildAttentionState = "needs_attention"
	}
	if prior.ExecutionID != "" {
		if f.OutcomeState == "unknown" && strings.TrimSpace(prior.OutcomeState) != "" {
			f.OutcomeState = prior.OutcomeState
		}
		if f.ProcessState == "unknown" && strings.TrimSpace(prior.ProcessState) != "" {
			f.ProcessState = prior.ProcessState
		}
		if f.ProviderState == "unknown" && strings.TrimSpace(prior.ProviderState) != "" {
			f.ProviderState = prior.ProviderState
		}
		if f.SessionState == "unknown" && strings.TrimSpace(prior.SessionState) != "" {
			f.SessionState = prior.SessionState
		}
	}
	f.DerivedPhase = executionDerivedPhase(f)
	f.ObservedAt = executionNow()
	return f, nil
}

func (s *RuntimeStore) refreshExecutionLifecycle(executionID string) error {
	f, err := s.ExecutionLifecycle(executionID)
	if err != nil {
		return err
	}
	return s.appendExecutionLifecycleEvidence(f)
}

func (s *RuntimeStore) executionControl(executionID string) (ExecutionControlAvailability, error) {
	v, err := s.ExecutionView(executionID)
	if err != nil || v == nil {
		return ExecutionControlAvailability{}, err
	}
	c := ExecutionControlAvailability{Action: "cancel", Provider: v.Provider, ProviderOwned: v.NodeKind == ExecutionNodeProviderChild}
	if run, err := s.executionGraphRun(*v); err != nil {
		return c, err
	} else if run != nil && v.NodeKind != ExecutionNodeProviderChild {
		c.Target = "managed_run"
		if !processIdentityMatches(*run) {
			c.Reason = "managed run process identity is not currently verifiable"
			return c, nil
		}
		c.Available, c.Reason = true, "Tusker owns this managed run under its immutable lease/process fence"
		return c, nil
	}
	obs, err := s.executionLatestObservation(executionID)
	if err != nil {
		return c, err
	}
	if obs == nil || obs.degraded {
		c.Reason = "provider control unavailable: no fresh non-degraded capability fact"
		return c, nil
	}
	want := "parent_interrupt"
	if c.ProviderOwned {
		want = "independent_child_control"
	}
	for _, fact := range obs.caps {
		if fact.Name == want && fact.State == "true" {
			if t, e := time.Parse(time.RFC3339Nano, fact.FreshAt); e == nil && time.Since(t) <= 5*time.Minute {
				// Capability proof is necessary but not sufficient: this build has
				// no target-specific provider control transport/acknowledgement yet.
				// Never advertise a button whose only implementation is a refusal.
				c.Target, c.Reason = "provider", "provider capability is fresh, but no target-specific control route is installed"
				return c, nil
			}
		}
	}
	c.Reason = "provider has not proved this control is currently available"
	return c, nil
}

func (s *RuntimeStore) ExecutionControlAvailability(executionID string) (ExecutionControlAvailability, error) {
	return s.executionControl(executionID)
}

func (s *RuntimeStore) recordExecutionCancellation(e ExecutionCancellationEvidence) error {
	if e.CancellationID == "" {
		e.CancellationID = "cancel-" + newRecordID()
	}
	e.OccurredAt = firstNonEmpty(e.OccurredAt, executionNow())
	_, err := s.exec(`INSERT OR IGNORE INTO execution_cancellation_evidence(cancellation_id, execution_id, request_key, target, stage, detail, lease_generation, process_pid, process_start, occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, e.CancellationID, e.ExecutionID, e.RequestKey, e.Target, e.Stage, e.Detail, e.LeaseGeneration, e.ProcessPID, e.ProcessStart, e.OccurredAt)
	return err
}

// RequestExecutionCancellation only performs a proven managed-run interrupt.
// Provider controls are recorded as unavailable until an adapter implements a
// target-specific acknowledgement protocol; a successful HTTP call is never
// fabricated into provider or OS settlement.
func (s *RuntimeStore) RequestExecutionCancellation(executionID, requestKey string) (ExecutionControlAvailability, error) {
	requestKey = strings.TrimSpace(requestKey)
	if requestKey == "" {
		requestKey = "operator"
	}
	c, err := s.executionControl(executionID)
	if err != nil {
		return c, err
	}
	if prior, found, err := s.executionCancellationOutcome(executionID, requestKey); err != nil {
		return c, err
	} else if found {
		return prior, nil
	}
	if err := s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "requested", Detail: c.Reason}); err != nil {
		return c, err
	}
	_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "provider_acknowledgement", Detail: "not observed"})
	_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "descendant_settlement", Detail: "not observed; parent state cannot forge child completion"})
	_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "timeout", Detail: "not reached"})
	if !c.Available {
		_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "unavailable", Detail: c.Reason})
		_ = s.refreshExecutionLifecycle(executionID)
		return c, nil
	}
	if c.Target != "managed_run" {
		_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "unavailable", Detail: "adapter control acknowledgement is not implemented"})
		c.Available = false
		c.Reason = "adapter control acknowledgement is not implemented"
		_ = s.refreshExecutionLifecycle(executionID)
		return c, nil
	}
	v, err := s.ExecutionView(executionID)
	if err != nil || v == nil {
		return c, firstNonNil(err, tuskerError(errorNotFound, "execution not found"))
	}
	run, err := s.executionGraphRun(*v)
	if err != nil || run == nil {
		return c, err
	}
	_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "wrapper_signal", LeaseGeneration: run.LeaseGeneration, ProcessPID: run.ProcessPID, ProcessStart: run.ProcessStartedAt})
	if err = interruptRunProcess(s, run, false); err != nil {
		_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "escalation", Detail: err.Error(), LeaseGeneration: run.LeaseGeneration, ProcessPID: run.ProcessPID, ProcessStart: run.ProcessStartedAt})
		_ = s.refreshExecutionLifecycle(executionID)
		return c, err
	}
	_ = s.recordExecutionCancellation(ExecutionCancellationEvidence{ExecutionID: executionID, RequestKey: requestKey, Target: c.Target, Stage: "os_settled", LeaseGeneration: run.LeaseGeneration, Detail: "process group no longer verified live"})
	_ = s.refreshExecutionLifecycle(executionID)
	return c, nil
}

// executionCancellationOutcome replays the durable terminal result for a
// request key. A duplicate must never be inferred from the run's current
// controllability: the original request may have failed while the run remains
// live.
func (s *RuntimeStore) executionCancellationOutcome(executionID, requestKey string) (ExecutionControlAvailability, bool, error) {
	var target, stage, detail string
	err := s.queryRowScan(`SELECT target, stage, detail FROM execution_cancellation_evidence
		WHERE execution_id=? AND request_key=? AND stage IN ('os_settled','unavailable','escalation')
		ORDER BY CASE stage WHEN 'os_settled' THEN 3 WHEN 'unavailable' THEN 2 ELSE 1 END DESC, occurred_at DESC LIMIT 1`, []any{executionID, requestKey}, &target, &stage, &detail)
	if err == sql.ErrNoRows {
		var requested int
		if err := s.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=? AND request_key=? AND stage='requested'`, []any{executionID, requestKey}, &requested); err != nil {
			return ExecutionControlAvailability{}, false, err
		}
		if requested == 0 {
			return ExecutionControlAvailability{}, false, nil
		}
		return ExecutionControlAvailability{Action: "cancel", Target: "managed_run", Reason: "prior cancellation request has no durable terminal outcome; no second signal sent"}, true, nil
	}
	if err != nil {
		return ExecutionControlAvailability{}, false, err
	}
	outcome := ExecutionControlAvailability{Action: "cancel", Target: target, Reason: detail}
	if stage == "os_settled" {
		outcome.Available = true
		if outcome.Reason == "" {
			outcome.Reason = "previous cancellation request settled"
		}
		return outcome, true, nil
	}
	if outcome.Reason == "" {
		outcome.Reason = "previous cancellation request did not settle"
	}
	return outcome, true, nil
}

func executionLifecycleJSON(f ExecutionLifecycleEvidence) string {
	b, _ := json.Marshal(f)
	return string(b)
}
