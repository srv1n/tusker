package main

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	runnercore "tusker/internal/runner"
)

const (
	workerDeliveryBodyLimit = 32 * 1024
	workerJSONFieldLimit    = 16 * 1024
)

type WorkerAttemptIdentity struct {
	ProjectID         string `json:"project_id"`
	TaskID            string `json:"task_id"`
	AttemptID         string `json:"attempt_id"`
	Provider          string `json:"provider"`
	NativeSessionID   string `json:"native_session_id"`
	WorkRevision      int    `json:"work_revision"`
	AttemptGeneration int    `json:"attempt_generation"`
}

type WorkerCoordinationEvent struct {
	EventID         string                `json:"event_id"`
	Identity        WorkerAttemptIdentity `json:"identity"`
	Kind            string                `json:"kind"`
	Milestone       string                `json:"milestone"`
	Status          string                `json:"status"`
	NextMilestone   string                `json:"next_milestone"`
	Evidence        []string              `json:"evidence,omitempty"`
	Details         map[string]any        `json:"details,omitempty"`
	ProviderEventID string                `json:"provider_event_id"`
	OccurredAt      string                `json:"occurred_at"`
	StoredAt        string                `json:"stored_at"`
	Stale           bool                  `json:"stale"`
}

type WorkerProviderActivity struct {
	ActivityID      string                `json:"activity_id"`
	Identity        WorkerAttemptIdentity `json:"identity"`
	ProviderEventID string                `json:"provider_event_id"`
	Cursor          string                `json:"cursor"`
	Status          string                `json:"status"`
	Source          string                `json:"source"`
	OccurredAt      string                `json:"occurred_at"`
	StoredAt        string                `json:"stored_at"`
	Stale           bool                  `json:"stale"`
}

type WorkerDelivery struct {
	DeliveryID               string                `json:"delivery_id"`
	IdempotencyKey           string                `json:"idempotency_key"`
	Identity                 WorkerAttemptIdentity `json:"identity"`
	Kind                     string                `json:"kind"`
	Body                     string                `json:"body"`
	State                    string                `json:"state"`
	StoredAt                 string                `json:"stored_at"`
	ProviderAcceptedAt       string                `json:"provider_accepted_at"`
	WorkerActivityObservedAt string                `json:"worker_activity_observed_at"`
	CorrelatedReplyEventID   string                `json:"correlated_reply_event_id"`
	CorrelatedReplyAt        string                `json:"correlated_reply_at"`
	DecisionAppliedAt        string                `json:"decision_applied_at"`
	LastError                string                `json:"last_error"`
	ProviderReceipt          map[string]any        `json:"provider_receipt,omitempty"`
}

type WorkerAttention struct {
	Identity          WorkerAttemptIdentity `json:"identity"`
	AttentionRequired bool                  `json:"attention_required"`
	LastMilestone     string                `json:"last_milestone"`
	LastActivityAt    string                `json:"last_activity_at"`
	SilenceSince      string                `json:"silence_since"`
	EvaluatedAt       string                `json:"evaluated_at"`
	RecommendedAction string                `json:"recommended_action"`
}

type WorkerProviderQualification struct {
	Provider     string                              `json:"provider"`
	Endpoint     string                              `json:"endpoint"`
	Version      string                              `json:"version"`
	QualifiedAt  string                              `json:"qualified_at"`
	Capabilities runnercore.CoordinationCapabilities `json:"capabilities"`
}

// RunSessionProcessObservation is the read-only process identity result used
// when a run is reopened. A live PID is useful only when its recorded start
// identity and process group still match; a PID that merely exists is not an
// owner proof.
type RunSessionProcessObservation struct {
	State     string `json:"state"`
	Verified  bool   `json:"verified"`
	PID       int    `json:"pid"`
	PGID      int    `json:"pgid"`
	StartedAt string `json:"started_at"`
	Reason    string `json:"reason,omitempty"`
}

// RunSessionCapability describes the provider action available after a
// durable session has been reopened. An unsupported or missing capability is
// explicit so callers never turn a missing native resume into a new launch.
type RunSessionCapability struct {
	Supported bool   `json:"supported"`
	Command   string `json:"command,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// RunSessionReconciliation is a read-only projection of one persisted run.
// It intentionally carries the canonical run row and current attempt/session
// identities together with the last durable coordination evidence. Reopening
// this projection never claims a lease, queues a directive, or starts a
// worker.
type RunSessionReconciliation struct {
	Run                     RunStatus                    `json:"run"`
	Attempt                 *RunAttempt                  `json:"attempt,omitempty"`
	Session                 *RunnerSession               `json:"session,omitempty"`
	Identity                *WorkerAttemptIdentity       `json:"identity,omitempty"`
	Attention               *WorkerAttention             `json:"attention,omitempty"`
	LastCompletedCheckpoint *WorkerCoordinationEvent     `json:"last_completed_checkpoint,omitempty"`
	UnresolvedOperation     *WorkerCoordinationEvent     `json:"unresolved_operation,omitempty"`
	Process                 RunSessionProcessObservation `json:"process"`
	Capability              RunSessionCapability         `json:"capability"`
	State                   string                       `json:"state"`
	Terminal                bool                         `json:"terminal"`
	TerminalOutcome         string                       `json:"terminal_outcome,omitempty"`
	Reconnectable           bool                         `json:"reconnectable"`
	StaleSnapshot           bool                         `json:"stale_snapshot,omitempty"`
	RefusalReasons          []string                     `json:"refusal_reasons,omitempty"`
	ObservedAt              string                       `json:"observed_at"`
}

const (
	runSessionStateIdle     = "idle"
	runSessionStateLive     = "live"
	runSessionStateTerminal = "terminal"
	runSessionStateUnknown  = "unknown"
)

func workerNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func validWorkerCoordinationKind(kind string) bool {
	switch kind {
	case "progress", "question", "blocked", "completed":
		return true
	}
	return false
}

func validWorkerDeliveryKind(kind string) bool {
	switch kind {
	case "question", "instruction", "answer":
		return true
	}
	return false
}

func (i WorkerAttemptIdentity) validate() error {
	if strings.TrimSpace(i.ProjectID) == "" || strings.TrimSpace(i.TaskID) == "" || strings.TrimSpace(i.AttemptID) == "" || strings.TrimSpace(i.Provider) == "" || strings.TrimSpace(i.NativeSessionID) == "" {
		return tuskerError(errorInvalidArg, "worker identity requires project, task, attempt, provider, and native session id")
	}
	if i.WorkRevision <= 0 || i.AttemptGeneration <= 0 {
		return tuskerError(errorInvalidArg, "worker identity requires positive work revision and attempt generation")
	}
	return nil
}

func workerJSONField(value any, name string) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", tuskerError(errorInvalidArg, name+" must be JSON serializable")
	}
	if len(raw) > workerJSONFieldLimit {
		return "", tuskerError(errorInvalidArg, name+" exceeds the 16KiB bound")
	}
	return string(raw), nil
}

func (s *RuntimeStore) migrateWorkerCoordination() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS worker_coordination_events (
			event_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			work_revision INTEGER NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL,
			provider TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('progress','question','blocked','completed')),
			milestone TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			next_milestone TEXT NOT NULL DEFAULT '',
			evidence_json TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '',
			provider_event_id TEXT NOT NULL DEFAULT '',
			occurred_at TEXT NOT NULL DEFAULT '',
			stored_at TEXT NOT NULL,
			stale INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS worker_provider_activity (
			activity_id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			work_revision INTEGER NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL,
			provider TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			provider_event_id TEXT NOT NULL DEFAULT '',
			cursor TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			occurred_at TEXT NOT NULL DEFAULT '',
			stored_at TEXT NOT NULL,
			stale INTEGER NOT NULL DEFAULT 0,
			UNIQUE(provider, native_session_id, provider_event_id)
		);`,
		`CREATE TABLE IF NOT EXISTS worker_provider_cursors (
			project_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL,
			provider TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			cursor TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(project_id, attempt_id, attempt_generation, provider, native_session_id)
		);`,
		`CREATE TABLE IF NOT EXISTS worker_deliveries (
			delivery_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL,
			project_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			work_revision INTEGER NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL,
			provider TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('question','instruction','answer')),
			body TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('stored','delivering','accepted','uncertain','replied','applied','unsupported','stale')),
			stored_at TEXT NOT NULL,
			provider_accepted_at TEXT NOT NULL DEFAULT '',
			worker_activity_observed_at TEXT NOT NULL DEFAULT '',
			correlated_reply_event_id TEXT NOT NULL DEFAULT '',
			correlated_reply_at TEXT NOT NULL DEFAULT '',
			decision_applied_at TEXT NOT NULL DEFAULT '',
			provider_receipt_json TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, attempt_id, attempt_generation, idempotency_key)
		);`,
		`CREATE TABLE IF NOT EXISTS worker_attention (
			project_id TEXT NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL,
			task_id TEXT NOT NULL,
			work_revision INTEGER NOT NULL,
			provider TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			attention_required INTEGER NOT NULL DEFAULT 0,
			last_milestone TEXT NOT NULL DEFAULT '',
			last_activity_at TEXT NOT NULL DEFAULT '',
			silence_since TEXT NOT NULL DEFAULT '',
			evaluated_at TEXT NOT NULL,
			recommended_action TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(project_id, attempt_id, attempt_generation)
		);`,
		`CREATE TABLE IF NOT EXISTS worker_provider_qualifications (
			provider TEXT NOT NULL,
			endpoint TEXT NOT NULL,
			version TEXT NOT NULL,
			capabilities_json TEXT NOT NULL,
			evidence TEXT NOT NULL DEFAULT '',
			qualified_at TEXT NOT NULL,
			PRIMARY KEY(provider, endpoint, version)
		);`,
	}
	for _, statement := range statements {
		if _, err := s.exec(statement); err != nil {
			return err
		}
	}
	var deliveriesSchema string
	err := s.queryRowScan(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'worker_deliveries'`, nil, &deliveriesSchema)
	if err != nil {
		return err
	}
	if !strings.Contains(deliveriesSchema, "'delivering'") {
		if err := s.withBusyRetry(func() error {
			tx, err := s.db.Begin()
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.Exec(`ALTER TABLE worker_deliveries RENAME TO worker_deliveries_legacy_state`); err != nil {
				return err
			}
			if _, err := tx.Exec(`CREATE TABLE worker_deliveries (
				delivery_id TEXT PRIMARY KEY,
				idempotency_key TEXT NOT NULL,
				project_id TEXT NOT NULL,
				task_id TEXT NOT NULL,
				work_revision INTEGER NOT NULL,
				attempt_id TEXT NOT NULL,
				attempt_generation INTEGER NOT NULL,
				provider TEXT NOT NULL,
				native_session_id TEXT NOT NULL,
				kind TEXT NOT NULL CHECK(kind IN ('question','instruction','answer')),
				body TEXT NOT NULL,
				state TEXT NOT NULL CHECK(state IN ('stored','delivering','accepted','uncertain','replied','applied','unsupported','stale')),
				stored_at TEXT NOT NULL,
				provider_accepted_at TEXT NOT NULL DEFAULT '',
				worker_activity_observed_at TEXT NOT NULL DEFAULT '',
				correlated_reply_event_id TEXT NOT NULL DEFAULT '',
				correlated_reply_at TEXT NOT NULL DEFAULT '',
				decision_applied_at TEXT NOT NULL DEFAULT '',
				provider_receipt_json TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				UNIQUE(project_id, attempt_id, attempt_generation, idempotency_key)
			)`); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO worker_deliveries SELECT * FROM worker_deliveries_legacy_state`); err != nil {
				return err
			}
			if _, err := tx.Exec(`DROP TABLE worker_deliveries_legacy_state`); err != nil {
				return err
			}
			return tx.Commit()
		}); err != nil {
			return err
		}
	}
	_, err = s.exec(`CREATE UNIQUE INDEX IF NOT EXISTS worker_deliveries_reply_event ON worker_deliveries(correlated_reply_event_id) WHERE correlated_reply_event_id != ''`)
	return err
}

func (s *RuntimeStore) workerIdentityCurrent(identity WorkerAttemptIdentity) (bool, error) {
	run, err := s.LookupRunForAttempt(identity.ProjectID, identity.AttemptID, identity.AttemptGeneration)
	if err != nil {
		return false, err
	}
	if run == nil || run.Terminal || run.WorkRevision != identity.WorkRevision || firstNonEmpty(run.ItemID, run.RecordID) != identity.TaskID {
		return false, nil
	}
	current, err := s.WorkerIdentityForRun(*run)
	if err != nil {
		return false, err
	}
	return current != nil && *current == identity, nil
}

// ReconcileRunSession reconstructs the canonical state of a persisted run
// without performing reconciliation side effects. In particular, it does
// not call the daemon's launch or lease paths and it does not evaluate/write
// WorkerAttention. Callers that have already loaded a run should use this
// method after reopening the store so a stale in-memory generation cannot
// become authority.
func (s *RuntimeStore) ReconcileRunSession(run RunStatus) (*RunSessionReconciliation, error) {
	return s.ReconcileRunSessionWithProbe(run, processIdentityMatches)
}

// ReconcileRunSessionWithProbe is the same read-only projection with an
// injectable process identity probe for deterministic component checks. The
// production wrapper above always uses the PID plus start-identity guard.
func (s *RuntimeStore) ReconcileRunSessionWithProbe(run RunStatus, probe func(RunStatus) bool) (*RunSessionReconciliation, error) {
	if s == nil || s.db == nil {
		return nil, tuskerError(errorNotFound, "runtime store is unavailable")
	}
	projectID, recordID := strings.TrimSpace(run.ProjectID), strings.TrimSpace(run.RecordID)
	if projectID == "" || recordID == "" {
		return nil, tuskerError(errorInvalidArg, "run session reconciliation requires project_id and record_id")
	}
	if probe == nil {
		probe = processIdentityMatches
	}

	// Always reload the exact record ID. FindRunScoped also accepts item IDs,
	// which are intentionally non-unique when a task has managed child runs.
	rows, err := s.query(`SELECT `+runtimeRunColumns+` FROM runs WHERE project_id = ? AND record_id = ? LIMIT 1`, projectID, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs, err := scanRunRows(rows, 0)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, tuskerError(errorNotFound, "run not found: "+recordID)
	}
	canonical := runs[0]
	result := &RunSessionReconciliation{
		Run:        canonical,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Process: RunSessionProcessObservation{
			PID:       canonical.ProcessPID,
			PGID:      canonical.ProcessPGID,
			StartedAt: canonical.ProcessStartedAt,
		},
	}
	if run.LeaseGeneration > 0 && (run.LeaseGeneration != canonical.LeaseGeneration ||
		run.ActiveAttemptID != canonical.ActiveAttemptID || run.WorkRevision != canonical.WorkRevision) {
		result.StaleSnapshot = true
		result.RefusalReasons = append(result.RefusalReasons, "input run snapshot is stale; canonical project, attempt, and generation were reloaded")
	}

	attempts, err := s.ListAttemptsForRun(canonical.ProjectID, canonical.RecordID)
	if err != nil {
		return nil, err
	}
	for i := range attempts {
		if strings.TrimSpace(canonical.ActiveAttemptID) != "" && attempts[i].AttemptID == canonical.ActiveAttemptID {
			attempt := attempts[i]
			result.Attempt = &attempt
			break
		}
	}
	if result.Attempt == nil && strings.TrimSpace(canonical.ActiveAttemptID) == "" && len(attempts) > 0 {
		// A released/terminal row may have cleared the active pointer. Preserve
		// the newest durable receipt for inspection, but never treat it as live.
		attempt := attempts[0]
		result.Attempt = &attempt
	}
	if canonical.ActiveAttemptID != "" && result.Attempt == nil {
		result.RefusalReasons = append(result.RefusalReasons, "active attempt receipt is missing")
	}

	// The run's session reference is authoritative. Falling back to the
	// attempt's reference is safe because it is the same immutable attempt; a
	// historical latest session is used only when the run is already inactive.
	sessionRef := strings.TrimSpace(canonical.SessionRef)
	if sessionRef == "" && result.Attempt != nil {
		sessionRef = strings.TrimSpace(result.Attempt.SessionRef)
	}
	if sessionRef != "" {
		session, sessionErr := s.FindSessionByRef(canonical.ProjectID, sessionRef)
		if sessionErr != nil {
			return nil, sessionErr
		}
		if session != nil && sessionMatchesRun(canonical, result.Attempt, *session) {
			result.Session = session
		} else if session != nil {
			result.RefusalReasons = append(result.RefusalReasons, "stored session identity does not match the current project, attempt, or work revision")
		}
	}
	freshSessionRequested := false
	if intent, intentErr := loadRunSessionControlIntent(s, canonical.ProjectID, canonical.RecordID); intentErr != nil {
		return nil, intentErr
	} else if intent != nil && (intent.Action == runSessionControlFresh || intent.Action == runSessionControlContextRecovery) && intent.State == runSessionControlQueued {
		freshSessionRequested = true
	}
	if !freshSessionRequested {
		decisions, decisionErr := s.ListRuntimeSupervisorDecisionsForRun(canonical.ProjectID, canonical.RecordID)
		if decisionErr != nil {
			return nil, decisionErr
		}
		for i := len(decisions) - 1; i >= 0; i-- {
			if decisions[i].ContextSignal == "explicit_context_recovery" || decisions[i].ContextSignal == "operator_start_fresh" {
				freshSessionRequested = true
				break
			}
		}
	}
	if freshSessionRequested {
		result.Session = nil
	}
	if result.Session == nil && !freshSessionRequested && !isDispatchingLeaseState(canonical.LeaseState) && canonical.ActiveAttemptID == "" {
		latest, sessionErr := s.LatestSession(canonical.ProjectID, canonical.RecordID, canonical.Runner)
		if sessionErr != nil {
			return nil, sessionErr
		}
		if latest != nil && (latest.WorkRevision == 0 || latest.WorkRevision == canonical.WorkRevision) {
			result.Session = latest
		}
	}

	identity, err := s.WorkerIdentityForRun(canonical)
	if err != nil {
		return nil, err
	}
	result.Identity = identity
	if identity != nil {
		attention, found, attentionErr := s.loadWorkerAttention(identity.ProjectID, identity.AttemptID, identity.AttemptGeneration)
		if attentionErr != nil {
			return nil, attentionErr
		}
		if found && attention.Identity == *identity {
			result.Attention = &attention
		}
	}

	events, err := s.currentCoordinationEvents(canonical, identity)
	if err != nil {
		return nil, err
	}
	for i := range events {
		if workerEventIsCompleted(events[i]) {
			result.LastCompletedCheckpoint = &events[i]
		}
		if !workerEventIsCompleted(events[i]) {
			result.UnresolvedOperation = &events[i]
		}
	}

	result.Capability = runSessionCapability(&canonical, result.Session)
	if !result.Capability.Supported && result.Capability.Reason != "" {
		result.RefusalReasons = append(result.RefusalReasons, result.Capability.Reason)
	}
	result.reconcileProcess(probe)
	return result, nil
}

func sessionMatchesRun(run RunStatus, attempt *RunAttempt, session RunnerSession) bool {
	if session.ProjectID != run.ProjectID || session.RecordID != run.RecordID || session.SessionRef == "" {
		return false
	}
	if run.ActiveAttemptID != "" && session.LastAttemptID != run.ActiveAttemptID {
		return false
	}
	if attempt != nil && attempt.SessionRef != "" && session.SessionRef != attempt.SessionRef {
		return false
	}
	if run.WorkRevision > 0 && session.WorkRevision > 0 && session.WorkRevision != run.WorkRevision {
		return false
	}
	return true
}

func runSessionCapability(run *RunStatus, session *RunnerSession) RunSessionCapability {
	capability := RunSessionCapability{}
	if run == nil || session == nil || strings.TrimSpace(session.SessionRef) == "" {
		capability.Reason = "native session reference is unavailable"
		return capability
	}
	resume := resumeCapability(run, session)
	capability.Supported, capability.Command, capability.Reason = resume.Supported, resume.Command, resume.Reason
	return capability
}

func workerEventIsCompleted(event WorkerCoordinationEvent) bool {
	if event.Kind == "completed" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(event.Status)) {
	case "completed", "done", "succeeded", "terminal":
		return true
	default:
		return false
	}
}

func (r *RunSessionReconciliation) reconcileProcess(probe func(RunStatus) bool) {
	if r == nil {
		return
	}
	dispatching := isDispatchingLeaseState(r.Run.LeaseState)
	hasProcessFacts := r.Process.PID > 0 || r.Process.PGID > 0 || strings.TrimSpace(r.Process.StartedAt) != ""
	hasReceipt, outcome := runSessionTerminalReceipt(r.Run, r.Attempt)
	r.TerminalOutcome = outcome
	if !dispatching && r.Run.ActiveAttemptID == "" && !r.Run.Terminal {
		r.State = runSessionStateIdle
		return
	}
	if r.Run.Terminal && !hasReceipt {
		r.Process.State = runSessionStateUnknown
		r.Process.Reason = "terminal flag has no terminal attempt receipt"
		r.RefusalReasons = append(r.RefusalReasons, r.Process.Reason)
		r.State = runSessionStateUnknown
		return
	}
	live := hasProcessFacts && probe(r.Run)
	if live {
		r.Process.State, r.Process.Verified = "live", true
		if hasReceipt || r.Run.Terminal {
			r.Process.Reason = "live owner contradicts the recorded terminal receipt"
			r.RefusalReasons = append(r.RefusalReasons, r.Process.Reason)
			r.State = runSessionStateUnknown
			return
		}
		if strings.TrimSpace(r.Run.LeaseOwner) == "" || strings.TrimSpace(r.Run.ActiveAttemptID) == "" {
			r.Process.Reason = "live process has no matching durable lease owner or attempt"
			r.RefusalReasons = append(r.RefusalReasons, r.Process.Reason)
			r.State = runSessionStateUnknown
			return
		}
		r.State = runSessionStateLive
		r.Reconnectable = r.Identity != nil && r.Session != nil
		if !r.Reconnectable {
			r.RefusalReasons = append(r.RefusalReasons, "live owner is missing a matching native attempt/session identity")
		}
		return
	}
	if hasReceipt {
		r.Process.State = "exited"
		r.Terminal = true
		r.State = runSessionStateTerminal
		return
	}
	if !hasProcessFacts {
		r.Process.Reason = "recorded process identity is unavailable"
	} else {
		r.Process.Reason = "recorded process identity does not match; terminal receipt is missing"
	}
	r.RefusalReasons = append(r.RefusalReasons, r.Process.Reason)
	r.Process.State = runSessionStateUnknown
	r.State = runSessionStateUnknown
}

func runSessionTerminalReceipt(run RunStatus, attempt *RunAttempt) (bool, string) {
	outcome := projectedAttemptOutcome(run.AttemptOutcome, run.LastError)
	if attempt != nil {
		attemptOutcome := projectedAttemptOutcome(attempt.Outcome, attempt.LastError)
		if outcome == AttemptOutcomeNone && attemptOutcome != AttemptOutcomeNone {
			outcome = attemptOutcome
		}
		if attempt.FinishedAt != "" && outcome == AttemptOutcomeNone {
			outcome = AttemptOutcomeUnknown
		}
	}
	if outcome == AttemptOutcomeNone && run.Terminal {
		return false, ""
	}
	if outcome == AttemptOutcomeUnknown {
		// An uncertainty receipt is durable evidence that the provider result
		// cannot be classified. It must stay unknown until an operator resolves
		// the external effect; it is never a terminal success/failure receipt.
		return false, string(outcome)
	}
	return outcome != AttemptOutcomeNone, string(outcome)
}

func (s *RuntimeStore) currentCoordinationEvents(run RunStatus, identity *WorkerAttemptIdentity) ([]WorkerCoordinationEvent, error) {
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	if taskID == "" || run.ActiveAttemptID == "" || run.LeaseGeneration <= 0 || run.WorkRevision <= 0 {
		return nil, nil
	}
	query := `SELECT event_id, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, milestone, status, next_milestone, evidence_json, details_json, provider_event_id, occurred_at, stored_at, stale
		FROM worker_coordination_events WHERE project_id = ? AND task_id = ? AND work_revision = ? AND attempt_id = ? AND attempt_generation = ? AND stale = 0`
	args := []any{run.ProjectID, taskID, run.WorkRevision, run.ActiveAttemptID, run.LeaseGeneration}
	if identity != nil {
		query += ` AND provider = ? AND native_session_id = ?`
		args = append(args, identity.Provider, identity.NativeSessionID)
	}
	query += ` ORDER BY stored_at, event_id`
	rows, err := s.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []WorkerCoordinationEvent
	for rows.Next() {
		var event WorkerCoordinationEvent
		var evidenceJSON, detailsJSON string
		var stale int
		if err := rows.Scan(&event.EventID, &event.Identity.ProjectID, &event.Identity.TaskID, &event.Identity.WorkRevision,
			&event.Identity.AttemptID, &event.Identity.AttemptGeneration, &event.Identity.Provider, &event.Identity.NativeSessionID,
			&event.Kind, &event.Milestone, &event.Status, &event.NextMilestone, &evidenceJSON, &detailsJSON,
			&event.ProviderEventID, &event.OccurredAt, &event.StoredAt, &stale); err != nil {
			return nil, err
		}
		event.Stale = stale != 0
		if evidenceJSON != "" {
			_ = json.Unmarshal([]byte(evidenceJSON), &event.Evidence)
		}
		if detailsJSON != "" {
			_ = json.Unmarshal([]byte(detailsJSON), &event.Details)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *RuntimeStore) RecordWorkerCoordinationEvent(event WorkerCoordinationEvent) (WorkerCoordinationEvent, error) {
	if err := event.Identity.validate(); err != nil {
		return WorkerCoordinationEvent{}, err
	}
	if !validWorkerCoordinationKind(event.Kind) {
		return WorkerCoordinationEvent{}, tuskerError(errorInvalidArg, "worker coordination kind must be progress, question, blocked, or completed")
	}
	evidenceJSON, err := workerJSONField(event.Evidence, "evidence")
	if err != nil {
		return WorkerCoordinationEvent{}, err
	}
	detailsJSON, err := workerJSONField(event.Details, "details")
	if err != nil {
		return WorkerCoordinationEvent{}, err
	}
	if strings.TrimSpace(event.EventID) == "" {
		event.EventID = "wce_" + newRecordID()
	}
	if strings.TrimSpace(event.StoredAt) == "" {
		event.StoredAt = workerNow()
	}
	current, err := s.workerIdentityCurrent(event.Identity)
	if err != nil {
		return WorkerCoordinationEvent{}, err
	}
	event.Stale = !current
	_, err = s.exec(`INSERT INTO worker_coordination_events (
		event_id, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, milestone, status, next_milestone, evidence_json, details_json, provider_event_id, occurred_at, stored_at, stale
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.EventID, event.Identity.ProjectID, event.Identity.TaskID, event.Identity.WorkRevision, event.Identity.AttemptID, event.Identity.AttemptGeneration,
		event.Identity.Provider, event.Identity.NativeSessionID, event.Kind, event.Milestone, event.Status, event.NextMilestone,
		evidenceJSON, detailsJSON, event.ProviderEventID, event.OccurredAt, event.StoredAt, boolToInt(event.Stale))
	if err != nil {
		return WorkerCoordinationEvent{}, err
	}
	return event, nil
}

func (s *RuntimeStore) RecordWorkerProviderActivity(activity WorkerProviderActivity) (WorkerProviderActivity, bool, error) {
	if err := activity.Identity.validate(); err != nil {
		return WorkerProviderActivity{}, false, err
	}
	if strings.TrimSpace(activity.ActivityID) == "" {
		activity.ActivityID = "wpa_" + newRecordID()
	}
	if strings.TrimSpace(activity.StoredAt) == "" {
		activity.StoredAt = workerNow()
	}
	if activity.ProviderEventID != "" {
		if existing, found, err := s.workerProviderActivityByEvent(activity.Identity.Provider, activity.Identity.NativeSessionID, activity.ProviderEventID); err != nil {
			return WorkerProviderActivity{}, false, err
		} else if found {
			return existing, true, nil
		}
	}
	current, err := s.workerIdentityCurrent(activity.Identity)
	if err != nil {
		return WorkerProviderActivity{}, false, err
	}
	activity.Stale = !current
	_, err = s.exec(`INSERT INTO worker_provider_activity (
		activity_id, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		provider_event_id, cursor, status, source, occurred_at, stored_at, stale
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		activity.ActivityID, activity.Identity.ProjectID, activity.Identity.TaskID, activity.Identity.WorkRevision, activity.Identity.AttemptID,
		activity.Identity.AttemptGeneration, activity.Identity.Provider, activity.Identity.NativeSessionID, activity.ProviderEventID,
		activity.Cursor, activity.Status, activity.Source, activity.OccurredAt, activity.StoredAt, boolToInt(activity.Stale))
	if err != nil {
		if activity.ProviderEventID != "" {
			if existing, found, lookupErr := s.workerProviderActivityByEvent(activity.Identity.Provider, activity.Identity.NativeSessionID, activity.ProviderEventID); lookupErr == nil && found {
				return existing, true, nil
			}
		}
		return WorkerProviderActivity{}, false, err
	}
	if !activity.Stale && activity.Cursor != "" {
		if err := s.saveWorkerProviderCursor(activity.Identity, activity.Cursor); err != nil {
			return WorkerProviderActivity{}, false, err
		}
	}
	return activity, false, nil
}

func (s *RuntimeStore) workerProviderActivityByEvent(provider, sessionID, providerEventID string) (WorkerProviderActivity, bool, error) {
	rows, err := s.query(`SELECT activity_id, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		provider_event_id, cursor, status, source, occurred_at, stored_at, stale FROM worker_provider_activity
		WHERE provider = ? AND native_session_id = ? AND provider_event_id = ?`, provider, sessionID, providerEventID)
	if err != nil {
		return WorkerProviderActivity{}, false, err
	}
	defer rows.Close()
	if rows.Next() {
		activity, err := scanWorkerProviderActivity(rows)
		return activity, err == nil, err
	}
	return WorkerProviderActivity{}, false, rows.Err()
}

func scanWorkerProviderActivity(rows *sql.Rows) (WorkerProviderActivity, error) {
	var activity WorkerProviderActivity
	var stale int
	err := rows.Scan(&activity.ActivityID, &activity.Identity.ProjectID, &activity.Identity.TaskID, &activity.Identity.WorkRevision,
		&activity.Identity.AttemptID, &activity.Identity.AttemptGeneration, &activity.Identity.Provider, &activity.Identity.NativeSessionID,
		&activity.ProviderEventID, &activity.Cursor, &activity.Status, &activity.Source, &activity.OccurredAt, &activity.StoredAt, &stale)
	activity.Stale = stale != 0
	return activity, err
}

func (s *RuntimeStore) saveWorkerProviderCursor(identity WorkerAttemptIdentity, cursor string) error {
	_, err := s.exec(`INSERT INTO worker_provider_cursors (project_id, attempt_id, attempt_generation, provider, native_session_id, cursor, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(project_id, attempt_id, attempt_generation, provider, native_session_id) DO UPDATE SET cursor = excluded.cursor, updated_at = excluded.updated_at`,
		identity.ProjectID, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID, cursor, workerNow())
	return err
}

func (s *RuntimeStore) WorkerProviderCursor(identity WorkerAttemptIdentity) (string, error) {
	var cursor string
	err := s.queryRowScan(`SELECT cursor FROM worker_provider_cursors WHERE project_id = ? AND attempt_id = ? AND attempt_generation = ? AND provider = ? AND native_session_id = ?`,
		[]any{identity.ProjectID, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID}, &cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return cursor, err
}

func parseWorkerTimestamp(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (s *RuntimeStore) workerLastActivity(identity WorkerAttemptIdentity) (time.Time, string, error) {
	var latest time.Time
	latestRaw := ""
	consider := func(raw string) {
		if parsed, ok := parseWorkerTimestamp(raw); ok && parsed.After(latest) {
			latest = parsed
			latestRaw = raw
		}
	}
	for _, table := range []string{"worker_provider_activity", "worker_coordination_events"} {
		rows, err := s.query(`SELECT occurred_at, stored_at FROM `+table+` WHERE project_id = ? AND task_id = ? AND work_revision = ? AND attempt_id = ? AND attempt_generation = ? AND provider = ? AND native_session_id = ? AND stale = 0`,
			identity.ProjectID, identity.TaskID, identity.WorkRevision, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID)
		if err != nil {
			return time.Time{}, "", err
		}
		for rows.Next() {
			var occurredAt, storedAt string
			if err := rows.Scan(&occurredAt, &storedAt); err != nil {
				_ = rows.Close()
				return time.Time{}, "", err
			}
			consider(occurredAt)
			consider(storedAt)
		}
		if err := rows.Close(); err != nil {
			return time.Time{}, "", err
		}
	}
	return latest, latestRaw, nil
}

func (s *RuntimeStore) workerActivityBaseline(identity WorkerAttemptIdentity) (time.Time, string, error) {
	for _, candidate := range []struct {
		query string
		args  []any
	}{
		{`SELECT started_at FROM attempts WHERE attempt_id = ?`, []any{identity.AttemptID}},
		{`SELECT started_at FROM runs WHERE project_id = ? AND (item_id = ? OR record_id = ?)`, []any{identity.ProjectID, identity.TaskID, identity.TaskID}},
		{`SELECT updated_at FROM runs WHERE project_id = ? AND (item_id = ? OR record_id = ?)`, []any{identity.ProjectID, identity.TaskID, identity.TaskID}},
		{`SELECT created_at FROM execution_records WHERE attempt_id = ?`, []any{identity.AttemptID}},
	} {
		var raw string
		err := s.queryRowScan(candidate.query, candidate.args, &raw)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return time.Time{}, "", err
		}
		if parsed, ok := parseWorkerTimestamp(raw); ok {
			return parsed, raw, nil
		}
	}
	return time.Time{}, "", nil
}

func (s *RuntimeStore) workerLastMilestone(identity WorkerAttemptIdentity) (string, error) {
	var milestone string
	err := s.queryRowScan(`SELECT milestone FROM worker_coordination_events WHERE project_id = ? AND task_id = ? AND work_revision = ? AND attempt_id = ? AND attempt_generation = ? AND provider = ? AND native_session_id = ? AND stale = 0 ORDER BY occurred_at DESC, stored_at DESC, event_id DESC LIMIT 1`,
		[]any{identity.ProjectID, identity.TaskID, identity.WorkRevision, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID}, &milestone)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return milestone, err
}

func (s *RuntimeStore) EvaluateWorkerAttention(identity WorkerAttemptIdentity, now time.Time, interval time.Duration) (WorkerAttention, error) {
	if err := identity.validate(); err != nil {
		return WorkerAttention{}, err
	}
	if interval <= 0 {
		return WorkerAttention{}, tuskerError(errorInvalidArg, "worker attention requires a positive visibility interval")
	}
	attention := WorkerAttention{Identity: identity, EvaluatedAt: now.UTC().Format(time.RFC3339Nano)}
	current, err := s.workerIdentityCurrent(identity)
	if err != nil {
		return WorkerAttention{}, err
	}
	lastActivity, lastActivityRaw, err := s.workerLastActivity(identity)
	if err != nil {
		return WorkerAttention{}, err
	}
	if lastActivity.IsZero() {
		lastActivity, lastActivityRaw, err = s.workerActivityBaseline(identity)
		if err != nil {
			return WorkerAttention{}, err
		}
	}
	attention.LastActivityAt = lastActivityRaw
	milestone, err := s.workerLastMilestone(identity)
	if err != nil {
		return WorkerAttention{}, err
	}
	attention.LastMilestone = milestone
	if current && !lastActivity.IsZero() && !now.Before(lastActivity.Add(interval)) {
		attention.AttentionRequired = true
		attention.SilenceSince = lastActivity.UTC().Format(time.RFC3339Nano)
		attention.RecommendedAction = "inspect provider session " + identity.Provider + ":" + identity.NativeSessionID
	}
	return s.saveWorkerAttention(attention)
}

func (s *RuntimeStore) loadWorkerAttention(projectID, attemptID string, generation int) (WorkerAttention, bool, error) {
	var attention WorkerAttention
	var required int
	err := s.queryRowScan(`SELECT project_id, attempt_id, attempt_generation, task_id, work_revision, provider, native_session_id,
		attention_required, last_milestone, last_activity_at, silence_since, evaluated_at, recommended_action
		FROM worker_attention WHERE project_id = ? AND attempt_id = ? AND attempt_generation = ?`,
		[]any{projectID, attemptID, generation},
		&attention.Identity.ProjectID, &attention.Identity.AttemptID, &attention.Identity.AttemptGeneration,
		&attention.Identity.TaskID, &attention.Identity.WorkRevision, &attention.Identity.Provider, &attention.Identity.NativeSessionID,
		&required, &attention.LastMilestone, &attention.LastActivityAt, &attention.SilenceSince, &attention.EvaluatedAt, &attention.RecommendedAction)
	if err == sql.ErrNoRows {
		return WorkerAttention{}, false, nil
	}
	if err != nil {
		return WorkerAttention{}, false, err
	}
	attention.AttentionRequired = required != 0
	return attention, true, nil
}

func (s *RuntimeStore) saveWorkerAttention(attention WorkerAttention) (WorkerAttention, error) {
	_, err := s.exec(`INSERT INTO worker_attention (project_id, attempt_id, attempt_generation, task_id, work_revision, provider, native_session_id,
		attention_required, last_milestone, last_activity_at, silence_since, evaluated_at, recommended_action)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id, attempt_id, attempt_generation) DO UPDATE SET
			task_id=excluded.task_id, work_revision=excluded.work_revision, provider=excluded.provider, native_session_id=excluded.native_session_id,
			attention_required=excluded.attention_required, last_milestone=excluded.last_milestone, last_activity_at=excluded.last_activity_at,
			silence_since=excluded.silence_since, evaluated_at=excluded.evaluated_at, recommended_action=excluded.recommended_action
		WHERE worker_attention.task_id != excluded.task_id
			OR worker_attention.work_revision != excluded.work_revision
			OR worker_attention.provider != excluded.provider
			OR worker_attention.native_session_id != excluded.native_session_id
			OR worker_attention.attention_required != excluded.attention_required
			OR worker_attention.last_milestone != excluded.last_milestone
			OR worker_attention.last_activity_at != excluded.last_activity_at
			OR worker_attention.silence_since != excluded.silence_since
			OR worker_attention.recommended_action != excluded.recommended_action`,
		attention.Identity.ProjectID, attention.Identity.AttemptID, attention.Identity.AttemptGeneration, attention.Identity.TaskID,
		attention.Identity.WorkRevision, attention.Identity.Provider, attention.Identity.NativeSessionID, boolToInt(attention.AttentionRequired),
		attention.LastMilestone, attention.LastActivityAt, attention.SilenceSince, attention.EvaluatedAt, attention.RecommendedAction)
	if err != nil {
		return WorkerAttention{}, err
	}
	persisted, found, err := s.loadWorkerAttention(attention.Identity.ProjectID, attention.Identity.AttemptID, attention.Identity.AttemptGeneration)
	if err != nil {
		return WorkerAttention{}, err
	}
	if !found {
		return WorkerAttention{}, tuskerError(errorNotFound, "worker attention row missing after save")
	}
	return persisted, nil
}

func (s *RuntimeStore) WorkerAttentionForRun(run RunStatus) (*WorkerAttention, error) {
	if strings.TrimSpace(run.ActiveAttemptID) == "" || run.LeaseGeneration <= 0 {
		return nil, nil
	}
	attention, found, err := s.loadWorkerAttention(run.ProjectID, run.ActiveAttemptID, run.LeaseGeneration)
	if err != nil || !found {
		return nil, err
	}
	if attention.Identity.TaskID != firstNonEmpty(run.ItemID, run.RecordID) || attention.Identity.WorkRevision != run.WorkRevision {
		return nil, nil
	}
	return &attention, nil
}

func (s *RuntimeStore) WorkerIdentityForRun(run RunStatus) (*WorkerAttemptIdentity, error) {
	if run.Terminal || strings.TrimSpace(run.ActiveAttemptID) == "" || run.LeaseGeneration <= 0 || run.WorkRevision <= 0 {
		return nil, nil
	}
	if run.LeaseState != string(LeaseStateClaimed) && run.LeaseState != string(LeaseStateRunning) {
		return nil, nil
	}
	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	if strings.TrimSpace(taskID) == "" {
		return nil, nil
	}
	rows, err := s.query(`SELECT execution_id, task_id, provider FROM execution_records WHERE project_id = ? AND attempt_id = ? AND lease_generation = ?`,
		run.ProjectID, run.ActiveAttemptID, run.LeaseGeneration)
	if err != nil {
		return nil, err
	}
	type executionMatch struct{ executionID, taskID, provider string }
	var matches []executionMatch
	for rows.Next() {
		var match executionMatch
		if err := rows.Scan(&match.executionID, &match.taskID, &match.provider); err != nil {
			_ = rows.Close()
			return nil, err
		}
		matches = append(matches, match)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(matches) != 1 || matches[0].taskID != taskID {
		return nil, nil
	}
	view, err := s.ExecutionView(matches[0].executionID)
	if err != nil || view == nil {
		return nil, err
	}
	provider, err := s.ExecutionProvider(matches[0].executionID)
	if err != nil {
		return nil, err
	}
	provider = strings.ToLower(strings.TrimSpace(firstNonEmpty(provider, matches[0].provider)))
	sessionID := strings.TrimSpace(view.ProviderSessionID)
	if sessionID == "" && run.SessionRef != "" && provider != "" {
		var parentAttempt string
		err := s.queryRowScan(`SELECT records.attempt_id FROM execution_attachment_events attachments JOIN execution_records records ON records.execution_id = attachments.execution_id WHERE attachments.project_id = ? AND attachments.provider = ? AND attachments.provider_session_id = ?`,
			[]any{run.ProjectID, provider, run.SessionRef}, &parentAttempt)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if err == nil {
			related, err := s.attemptDescendsFrom(run.ProjectID, run.ActiveAttemptID, parentAttempt)
			if err != nil {
				return nil, err
			}
			if related {
				sessionID = run.SessionRef
			}
		}
	}
	if provider == "" || sessionID == "" {
		return nil, nil
	}
	return &WorkerAttemptIdentity{
		ProjectID:         run.ProjectID,
		TaskID:            taskID,
		AttemptID:         run.ActiveAttemptID,
		Provider:          provider,
		NativeSessionID:   sessionID,
		WorkRevision:      run.WorkRevision,
		AttemptGeneration: run.LeaseGeneration,
	}, nil
}

func (s *RuntimeStore) WorkerAttemptStatus(identity WorkerAttemptIdentity, now time.Time, interval time.Duration) (WorkerAttention, []WorkerCoordinationEvent, []WorkerDelivery, error) {
	attention, err := s.EvaluateWorkerAttention(identity, now, interval)
	if err != nil {
		return WorkerAttention{}, nil, nil, err
	}
	events, err := s.listWorkerCoordinationEvents(identity)
	if err != nil {
		return WorkerAttention{}, nil, nil, err
	}
	deliveries, err := s.listWorkerDeliveries(identity)
	if err != nil {
		return WorkerAttention{}, nil, nil, err
	}
	return attention, events, deliveries, nil
}

func (s *RuntimeStore) listWorkerCoordinationEvents(identity WorkerAttemptIdentity) ([]WorkerCoordinationEvent, error) {
	rows, err := s.query(`SELECT event_id, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, milestone, status, next_milestone, evidence_json, details_json, provider_event_id, occurred_at, stored_at, stale
		FROM worker_coordination_events WHERE project_id = ? AND task_id = ? AND work_revision = ? AND attempt_id = ? AND attempt_generation = ? AND provider = ? AND native_session_id = ? ORDER BY stored_at, event_id`,
		identity.ProjectID, identity.TaskID, identity.WorkRevision, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []WorkerCoordinationEvent
	for rows.Next() {
		var event WorkerCoordinationEvent
		var evidenceJSON, detailsJSON string
		var stale int
		if err := rows.Scan(&event.EventID, &event.Identity.ProjectID, &event.Identity.TaskID, &event.Identity.WorkRevision, &event.Identity.AttemptID,
			&event.Identity.AttemptGeneration, &event.Identity.Provider, &event.Identity.NativeSessionID, &event.Kind, &event.Milestone, &event.Status,
			&event.NextMilestone, &evidenceJSON, &detailsJSON, &event.ProviderEventID, &event.OccurredAt, &event.StoredAt, &stale); err != nil {
			return nil, err
		}
		event.Stale = stale != 0
		if evidenceJSON != "" {
			_ = json.Unmarshal([]byte(evidenceJSON), &event.Evidence)
		}
		if detailsJSON != "" {
			_ = json.Unmarshal([]byte(detailsJSON), &event.Details)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *RuntimeStore) listWorkerDeliveries(identity WorkerAttemptIdentity) ([]WorkerDelivery, error) {
	rows, err := s.query(`SELECT delivery_id, idempotency_key, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, body, state, stored_at, provider_accepted_at, worker_activity_observed_at, correlated_reply_event_id, correlated_reply_at, decision_applied_at, provider_receipt_json, last_error
		FROM worker_deliveries WHERE project_id = ? AND task_id = ? AND work_revision = ? AND attempt_id = ? AND attempt_generation = ? AND provider = ? AND native_session_id = ? ORDER BY stored_at, delivery_id`,
		identity.ProjectID, identity.TaskID, identity.WorkRevision, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkerDeliveries(rows)
}

func (s *RuntimeStore) workerPendingDeliveries(identity WorkerAttemptIdentity, states ...string) ([]WorkerDelivery, error) {
	if len(states) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(states))
	args := []any{identity.ProjectID, identity.TaskID, identity.WorkRevision, identity.AttemptID, identity.AttemptGeneration, identity.Provider, identity.NativeSessionID}
	for i, state := range states {
		placeholders[i] = "?"
		args = append(args, state)
	}
	rows, err := s.query(`SELECT delivery_id, idempotency_key, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, body, state, stored_at, provider_accepted_at, worker_activity_observed_at, correlated_reply_event_id, correlated_reply_at, decision_applied_at, provider_receipt_json, last_error
		FROM worker_deliveries WHERE project_id = ? AND task_id = ? AND work_revision = ? AND attempt_id = ? AND attempt_generation = ? AND provider = ? AND native_session_id = ? AND state IN (`+strings.Join(placeholders, ",")+`) ORDER BY stored_at, delivery_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkerDeliveries(rows)
}

func scanWorkerDeliveries(rows *sql.Rows) ([]WorkerDelivery, error) {
	var deliveries []WorkerDelivery
	for rows.Next() {
		var delivery WorkerDelivery
		var receiptJSON string
		if err := rows.Scan(&delivery.DeliveryID, &delivery.IdempotencyKey, &delivery.Identity.ProjectID, &delivery.Identity.TaskID, &delivery.Identity.WorkRevision,
			&delivery.Identity.AttemptID, &delivery.Identity.AttemptGeneration, &delivery.Identity.Provider, &delivery.Identity.NativeSessionID,
			&delivery.Kind, &delivery.Body, &delivery.State, &delivery.StoredAt, &delivery.ProviderAcceptedAt, &delivery.WorkerActivityObservedAt,
			&delivery.CorrelatedReplyEventID, &delivery.CorrelatedReplyAt, &delivery.DecisionAppliedAt, &receiptJSON, &delivery.LastError); err != nil {
			return nil, err
		}
		if receiptJSON != "" {
			_ = json.Unmarshal([]byte(receiptJSON), &delivery.ProviderReceipt)
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, rows.Err()
}

func (s *RuntimeStore) WorkerDelivery(deliveryID string) (WorkerDelivery, bool, error) {
	rows, err := s.query(`SELECT delivery_id, idempotency_key, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, body, state, stored_at, provider_accepted_at, worker_activity_observed_at, correlated_reply_event_id, correlated_reply_at, decision_applied_at, provider_receipt_json, last_error
		FROM worker_deliveries WHERE delivery_id = ?`, strings.TrimSpace(deliveryID))
	if err != nil {
		return WorkerDelivery{}, false, err
	}
	defer rows.Close()
	deliveries, err := scanWorkerDeliveries(rows)
	if err != nil || len(deliveries) == 0 {
		return WorkerDelivery{}, false, err
	}
	return deliveries[0], true, nil
}

func (s *RuntimeStore) PutWorkerDelivery(delivery WorkerDelivery) (WorkerDelivery, bool, error) {
	if err := delivery.Identity.validate(); err != nil {
		return WorkerDelivery{}, false, err
	}
	if !validWorkerDeliveryKind(delivery.Kind) {
		return WorkerDelivery{}, false, tuskerError(errorInvalidArg, "worker delivery kind must be question, instruction, or answer")
	}
	if strings.TrimSpace(delivery.Body) == "" {
		return WorkerDelivery{}, false, tuskerError(errorInvalidArg, "worker delivery requires a body")
	}
	if len(delivery.Body) > workerDeliveryBodyLimit {
		return WorkerDelivery{}, false, tuskerError(errorInvalidArg, "worker delivery body exceeds the 32KiB bound")
	}
	if strings.TrimSpace(delivery.IdempotencyKey) == "" {
		return WorkerDelivery{}, false, tuskerError(errorInvalidArg, "worker delivery requires an idempotency key")
	}
	if strings.TrimSpace(delivery.DeliveryID) == "" {
		delivery.DeliveryID = "wd_" + newRecordID()
	}
	if strings.TrimSpace(delivery.State) == "" {
		delivery.State = "stored"
	}
	if strings.TrimSpace(delivery.StoredAt) == "" {
		delivery.StoredAt = workerNow()
	}
	existing, found, err := s.workerDeliveryByKey(delivery.Identity, delivery.IdempotencyKey)
	if err != nil {
		return WorkerDelivery{}, false, err
	}
	if found {
		if existing.Kind == delivery.Kind && existing.Body == delivery.Body && existing.Identity == delivery.Identity {
			return existing, true, nil
		}
		return WorkerDelivery{}, false, tuskerError(errorInvalidArg, "worker delivery idempotency key already used with a different request")
	}
	_, err = s.exec(`INSERT INTO worker_deliveries (delivery_id, idempotency_key, project_id, task_id, work_revision, attempt_id, attempt_generation,
		provider, native_session_id, kind, body, state, stored_at, provider_receipt_json) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		delivery.DeliveryID, delivery.IdempotencyKey, delivery.Identity.ProjectID, delivery.Identity.TaskID, delivery.Identity.WorkRevision,
		delivery.Identity.AttemptID, delivery.Identity.AttemptGeneration, delivery.Identity.Provider, delivery.Identity.NativeSessionID,
		delivery.Kind, delivery.Body, delivery.State, delivery.StoredAt, "")
	if err != nil {
		if again, foundAgain, lookupErr := s.workerDeliveryByKey(delivery.Identity, delivery.IdempotencyKey); lookupErr == nil && foundAgain {
			if again.Kind == delivery.Kind && again.Body == delivery.Body && again.Identity == delivery.Identity {
				return again, true, nil
			}
			return WorkerDelivery{}, false, tuskerError(errorInvalidArg, "worker delivery idempotency key already used with a different request")
		}
		return WorkerDelivery{}, false, err
	}
	return delivery, false, nil
}

func (s *RuntimeStore) workerDeliveryByKey(identity WorkerAttemptIdentity, key string) (WorkerDelivery, bool, error) {
	rows, err := s.query(`SELECT delivery_id, idempotency_key, project_id, task_id, work_revision, attempt_id, attempt_generation, provider, native_session_id,
		kind, body, state, stored_at, provider_accepted_at, worker_activity_observed_at, correlated_reply_event_id, correlated_reply_at, decision_applied_at, provider_receipt_json, last_error
		FROM worker_deliveries WHERE project_id = ? AND attempt_id = ? AND attempt_generation = ? AND idempotency_key = ?`,
		identity.ProjectID, identity.AttemptID, identity.AttemptGeneration, key)
	if err != nil {
		return WorkerDelivery{}, false, err
	}
	defer rows.Close()
	deliveries, err := scanWorkerDeliveries(rows)
	if err != nil || len(deliveries) == 0 {
		return WorkerDelivery{}, false, err
	}
	return deliveries[0], true, nil
}

func (s *RuntimeStore) transitionWorkerDelivery(deliveryID string, allowed map[string]bool, mutate func(*WorkerDelivery) error) error {
	delivery, found, err := s.WorkerDelivery(deliveryID)
	if err != nil {
		return err
	}
	if !found {
		return tuskerError(errorNotFound, "worker delivery not found: "+deliveryID)
	}
	if !allowed[delivery.State] {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" cannot transition from state "+delivery.State)
	}
	fromState := delivery.State
	if err := mutate(&delivery); err != nil {
		return err
	}
	receiptJSON, err := workerJSONField(delivery.ProviderReceipt, "provider_receipt")
	if err != nil {
		return err
	}
	res, err := s.exec(`UPDATE worker_deliveries SET state = ?, provider_accepted_at = ?, worker_activity_observed_at = ?, correlated_reply_event_id = ?,
		correlated_reply_at = ?, decision_applied_at = ?, provider_receipt_json = ?, last_error = ? WHERE delivery_id = ? AND state = ?`,
		delivery.State, delivery.ProviderAcceptedAt, delivery.WorkerActivityObservedAt, delivery.CorrelatedReplyEventID, delivery.CorrelatedReplyAt,
		delivery.DecisionAppliedAt, receiptJSON, delivery.LastError, delivery.DeliveryID, fromState)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" lost the state transition race")
	}
	return nil
}

func (s *RuntimeStore) ClaimWorkerDelivery(deliveryID string) (WorkerDelivery, bool, error) {
	res, err := s.exec(`UPDATE worker_deliveries SET state = 'delivering' WHERE delivery_id = ? AND state = 'stored'`, strings.TrimSpace(deliveryID))
	if err != nil {
		return WorkerDelivery{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return WorkerDelivery{}, false, err
	}
	delivery, found, err := s.WorkerDelivery(deliveryID)
	if err != nil {
		return WorkerDelivery{}, false, err
	}
	if !found {
		return WorkerDelivery{}, false, tuskerError(errorNotFound, "worker delivery not found: "+deliveryID)
	}
	return delivery, affected == 1, nil
}

func (s *RuntimeStore) MarkWorkerDeliveryAccepted(deliveryID string, receipt map[string]any, at string) error {
	if _, err := workerJSONField(receipt, "provider_receipt"); err != nil {
		return err
	}
	return s.transitionWorkerDelivery(deliveryID, map[string]bool{"delivering": true, "accepted": true}, func(d *WorkerDelivery) error {
		d.State = "accepted"
		d.ProviderAcceptedAt = at
		d.ProviderReceipt = receipt
		d.LastError = ""
		return nil
	})
}

func (s *RuntimeStore) MarkWorkerDeliveryUncertain(deliveryID, reason string) error {
	return s.transitionWorkerDelivery(deliveryID, map[string]bool{"delivering": true, "uncertain": true}, func(d *WorkerDelivery) error {
		d.State = "uncertain"
		d.LastError = reason
		return nil
	})
}

func (s *RuntimeStore) MarkWorkerDeliveryReply(deliveryID, eventID, at string) error {
	delivery, found, err := s.WorkerDelivery(deliveryID)
	if err != nil {
		return err
	}
	if !found {
		return tuskerError(errorNotFound, "worker delivery not found: "+deliveryID)
	}
	current, err := s.workerIdentityCurrent(delivery.Identity)
	if err != nil {
		return err
	}
	if !current {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" identity is stale or superseded")
	}
	err = s.transitionWorkerDelivery(deliveryID, map[string]bool{"delivering": true, "accepted": true, "uncertain": true, "replied": true}, func(d *WorkerDelivery) error {
		d.State = "replied"
		d.CorrelatedReplyEventID = eventID
		d.CorrelatedReplyAt = at
		return nil
	})
	if err != nil && strings.Contains(err.Error(), "correlated_reply_event_id") {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" reply event already correlates another delivery")
	}
	return err
}

func (s *RuntimeStore) ApplyWorkerDeliveryDecision(deliveryID string, now time.Time) error {
	delivery, found, err := s.WorkerDelivery(deliveryID)
	if err != nil {
		return err
	}
	if !found {
		return tuskerError(errorNotFound, "worker delivery not found: "+deliveryID)
	}
	if delivery.State != "replied" {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" cannot apply a decision from state "+delivery.State)
	}
	current, err := s.workerIdentityCurrent(delivery.Identity)
	if err != nil {
		return err
	}
	if !current {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" identity is stale or superseded")
	}
	res, err := s.exec(`UPDATE worker_deliveries SET state = 'applied', decision_applied_at = ? WHERE delivery_id = ? AND state = 'replied'`,
		now.UTC().Format(time.RFC3339Nano), deliveryID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" lost the apply race")
	}
	return nil
}

func (s *RuntimeStore) markWorkerDeliveryState(deliveryID string, allowed map[string]bool, state, lastError string) error {
	return s.transitionWorkerDelivery(deliveryID, allowed, func(d *WorkerDelivery) error {
		d.State = state
		if lastError != "" {
			d.LastError = lastError
		}
		return nil
	})
}

func (s *RuntimeStore) SaveWorkerProviderQualification(q WorkerProviderQualification) error {
	if strings.TrimSpace(q.Provider) == "" || strings.TrimSpace(q.Endpoint) == "" {
		return tuskerError(errorInvalidArg, "provider qualification requires provider and endpoint")
	}
	capabilitiesJSON, err := json.Marshal(q.Capabilities)
	if err != nil {
		return err
	}
	if len(capabilitiesJSON) > workerJSONFieldLimit {
		return tuskerError(errorInvalidArg, "provider qualification capabilities exceed the 16KiB bound")
	}
	if strings.TrimSpace(q.QualifiedAt) == "" {
		q.QualifiedAt = workerNow()
	}
	_, err = s.exec(`INSERT INTO worker_provider_qualifications (provider, endpoint, version, capabilities_json, evidence, qualified_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(provider, endpoint, version) DO UPDATE SET capabilities_json = excluded.capabilities_json, evidence = excluded.evidence, qualified_at = excluded.qualified_at`,
		q.Provider, q.Endpoint, q.Version, string(capabilitiesJSON), q.Capabilities.Evidence, q.QualifiedAt)
	return err
}

func (s *RuntimeStore) WorkerProviderQualifications() ([]WorkerProviderQualification, error) {
	rows, err := s.query(`SELECT provider, endpoint, version, capabilities_json, evidence, qualified_at FROM worker_provider_qualifications ORDER BY provider, endpoint, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var qualifications []WorkerProviderQualification
	for rows.Next() {
		var q WorkerProviderQualification
		var capabilitiesJSON string
		if err := rows.Scan(&q.Provider, &q.Endpoint, &q.Version, &capabilitiesJSON, &q.Capabilities.Evidence, &q.QualifiedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(capabilitiesJSON), &q.Capabilities)
		qualifications = append(qualifications, q)
	}
	return qualifications, rows.Err()
}

func (s *RuntimeStore) workerProviderQualification(provider string) (WorkerProviderQualification, bool, error) {
	qualifications, err := s.WorkerProviderQualifications()
	if err != nil {
		return WorkerProviderQualification{}, false, err
	}
	for _, q := range qualifications {
		if q.Provider == provider {
			return q, true, nil
		}
	}
	return WorkerProviderQualification{}, false, nil
}
