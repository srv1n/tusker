package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// reconcileLiveCadence is the heartbeat: liveness, budget, silence and the
	// stat-only edit check for active projects. It never loads a vault.
	reconcileLiveCadence = 5 * time.Second
	// projectInteractionWindow keeps a project active after the operator or a
	// CLI command touched it.
	projectInteractionWindow = 10 * time.Minute
	// reconcileFullCadence bounds how long architect wave reports may reuse
	// the in-memory frontier instead of reading the vault.
	reconcileFullCadence = 5 * time.Minute
)

type adaptiveProjectReconcileState struct {
	LastActivityAt     time.Time
	LastActivityReason string
	LastPollAt         time.Time
	Cadence            time.Duration
	Tier               string
}

type adaptiveProjectReconcileStatus struct {
	Tier               string `json:"tier"`
	CadenceMS          int64  `json:"cadenceMs"`
	LastActivityAt     string `json:"lastActivityAt,omitempty"`
	LastActivityReason string `json:"lastActivityReason,omitempty"`
	LastPollAt         string `json:"lastPollAt,omitempty"`
	// NextDueAt is kept for older clients. Reconciliation is event-driven, so
	// there is no scheduled due time and it stays empty.
	NextDueAt string `json:"nextDueAt,omitempty"`
}

// runtimeRunNeedsHotReconcileAt reports a live or queued run: one that makes
// its project active.
func runtimeRunNeedsHotReconcileAt(run RunStatus, now time.Time) bool {
	if run.Terminal {
		return false
	}
	// A dead/stale row must not keep its project active forever. Rows without
	// timestamps are legacy data; keep those active until one poll repairs them.
	lastHeartbeat := strings.TrimSpace(run.LastHeartbeatAt)
	if lastHeartbeat != "" {
		if heartbeatAt, err := time.Parse(time.RFC3339Nano, lastHeartbeat); err == nil &&
			run.ProcessPID <= 0 && run.ProcessPGID <= 0 && now.Sub(heartbeatAt) > daemonHeartbeatDeadThreshold {
			return false
		}
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued, LeaseStateInterrupted:
		return true
	case LeaseStateUnclaimed:
		// A clean unclaimed row is runnable work waiting on shared/project
		// capacity. A review handoff is also deliberately unclaimed while it
		// waits for an upstream DAG edge to land. Ordinary policy-blocked
		// execute rows are not live work.
		if run.Lane == runLaneReview && run.AttemptOutcome == string(AttemptOutcomeWaitingForReview) {
			return true
		}
		return strings.TrimSpace(run.LastError) == ""
	case LeaseStateReleased:
		// A review handoff has no active lease by design, but it is not quiescent
		// work. Before a reviewer can claim it, an upstream DAG edge may still
		// need to land on integration. After the reviewer submits, the
		// authoritative completion reactor must consume that typed receipt.
		if run.Lane != runLaneReview {
			return false
		}
		if run.AttemptOutcome == string(AttemptOutcomeWaitingForReview) {
			return true
		}
		return run.AttemptOutcome == string(AttemptOutcomeSucceeded) &&
			strings.Contains(run.LastError, "typed review result recorded; awaiting review reactor")
	default:
		return false
	}
}

// runWaitsForAdmission reports a queued run held by capacity, load, or the
// build queue. Those clear on their own, so the heartbeat retries its project
// when capacity frees and otherwise at the poll interval.
func runWaitsForAdmission(run RunStatus) bool {
	return !run.Terminal && LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateUnclaimed &&
		(strings.HasPrefix(run.LastError, fairDispatchReasonPrefix) || strings.HasPrefix(run.LastError, "automation plan do_not_dispatch: waiting: ") ||
			// Capacity blockers are joined in any order ahead of scheduling.
			(strings.HasPrefix(run.LastError, "automation plan do_not_dispatch: ") && strings.Contains(run.LastError, "active run limit reached")))
}

// noteProjectActivity records an interaction: a CLI command, a runner report,
// or the operator opening the project in the UI. It keeps the project active
// for projectInteractionWindow.
func (d *Daemon) noteProjectActivity(projectID, reason string, now time.Time) {
	projectID = strings.TrimSpace(projectID)
	if d == nil || projectID == "" {
		return
	}
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	if d.reconcileSchedule == nil {
		d.reconcileSchedule = map[string]adaptiveProjectReconcileState{}
	}
	state := d.reconcileSchedule[projectID]
	state.LastActivityAt = now.UTC()
	state.LastActivityReason = strings.TrimSpace(reason)
	state.Tier, state.Cadence = "active", reconcileLiveCadence
	d.reconcileSchedule[projectID] = state
}

func (d *Daemon) recentProjectActivity(projectID string, now time.Time) bool {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	last := d.reconcileSchedule[projectID].LastActivityAt
	return !last.IsZero() && now.Sub(last) < projectInteractionWindow
}

// setProjectTier publishes the active/idle state the heartbeat computed.
func (d *Daemon) setProjectTier(projectID string, active bool) {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	if d.reconcileSchedule == nil {
		d.reconcileSchedule = map[string]adaptiveProjectReconcileState{}
	}
	state := d.reconcileSchedule[projectID]
	state.Tier, state.Cadence = "idle", 0
	if active {
		state.Tier, state.Cadence = "active", reconcileLiveCadence
	}
	d.reconcileSchedule[projectID] = state
}

func (d *Daemon) lastProjectPoll(projectID string) time.Time {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	return d.reconcileSchedule[projectID].LastPollAt
}

// recordPollSchedule stamps the poll time of one project, or of every known
// project for a registry-wide poll.
func (d *Daemon) recordPollSchedule(projectID string, now time.Time) {
	if d == nil {
		return
	}
	projectID = strings.TrimSpace(projectID)
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	if d.reconcileSchedule == nil {
		d.reconcileSchedule = map[string]adaptiveProjectReconcileState{}
	}
	for id, state := range d.reconcileSchedule {
		if projectID == "" || id == projectID {
			state.LastPollAt = now.UTC()
			d.reconcileSchedule[id] = state
		}
	}
	if _, ok := d.reconcileSchedule[projectID]; projectID != "" && !ok {
		d.reconcileSchedule[projectID] = adaptiveProjectReconcileState{LastPollAt: now.UTC(), Tier: "idle"}
	}
}

func (d *Daemon) adaptiveReconcileStatus(projectID string) adaptiveProjectReconcileStatus {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	state := d.reconcileSchedule[strings.TrimSpace(projectID)]
	result := adaptiveProjectReconcileStatus{
		Tier: firstNonEmpty(state.Tier, "idle"), CadenceMS: state.Cadence.Milliseconds(),
		LastActivityReason: state.LastActivityReason,
	}
	if !state.LastActivityAt.IsZero() {
		result.LastActivityAt = state.LastActivityAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.LastPollAt.IsZero() {
		result.LastPollAt = state.LastPollAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

// Bounded automatic safe repair.
//
// A recoverable metadata inconsistency (currently: a dispatch reservation
// proven to have no live or uncertain owner) receives at most one automatic
// safe repair per unchanged diagnostic fingerprint. The attempt ledger lives
// in daemon_settings so the bound survives daemon restart. A failed
// postcondition or a recurrence after a recorded repair escalates exactly
// once with evidence; normal waits (disabled/paused/uncertain work) cause
// neither repairs nor escalation spam. Materially new state (a new lease
// generation, owner, or expiry) is a new diagnosis, not an evasion: the
// fingerprint binds the exact lease identity that was repaired.
const (
	selfServiceRepairKindDeadReservation = "dead_reservation"

	selfServiceRepairAttemptKeyPrefix    = "self_service_repair_attempt:"
	selfServiceRepairEscalationKeyPrefix = "self_service_repair_escalated:"
	selfServiceRepairScheduleKeyPrefix   = "self_service_reconcile_schedule:"
)

// SelfServiceRepairEscalation is one persisted once-only escalation for a
// diagnostic fingerprint whose automatic repair did not hold.
type SelfServiceRepairEscalation struct {
	Fingerprint string `json:"fingerprint"`
	Kind        string `json:"kind"`
	ProjectID   string `json:"project_id"`
	RecordID    string `json:"record_id"`
	Evidence    string `json:"evidence"`
	RecordedAt  string `json:"recorded_at"`
}

// SelfServiceReconcileSchedule is the persisted per-project schedule and
// last-observation snapshot. Overdue reconciliation reads this (not a fixed
// heartbeat age) so deliberate idle polling is never mislabeled and an
// overdue project exposes its next actor and action.
type SelfServiceReconcileSchedule struct {
	ProjectID          string `json:"project_id"`
	Tier               string `json:"tier"`
	CadenceMS          int64  `json:"cadence_ms"`
	LastActivityAt     string `json:"last_activity_at,omitempty"`
	LastActivityReason string `json:"last_activity_reason,omitempty"`
	LastPollAt         string `json:"last_poll_at,omitempty"`
	NextDueAt          string `json:"next_due_at,omitempty"`
	RecordedAt         string `json:"recorded_at"`
}

// selfServiceRepairFingerprint binds an allowlisted repair kind to the exact
// state revision it was diagnosed from. Empty input yields no fingerprint
// and therefore no repair.
func selfServiceRepairFingerprint(projectID, kind, revision string) string {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	revision = strings.TrimSpace(revision)
	if projectID == "" || kind == "" || revision == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("tusker.self-repair/v1\x00" + projectID + "\x00" + kind + "\x00" + revision))
	return hex.EncodeToString(sum[:])
}

// deadReservationRevision returns the exact lease identity of a dispatch
// reservation that is provably dead: claimed/running, past expiry, with a
// recorded owner and generation, and no live PID or process group. Anything
// else (including uncertain ownership) is not a repair candidate.
func deadReservationRevision(run RunStatus) string {
	if run.Terminal {
		return ""
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning:
	default:
		return ""
	}
	owner := strings.TrimSpace(run.LeaseOwner)
	expires := strings.TrimSpace(run.LeaseExpiresAt)
	if owner == "" || run.LeaseGeneration <= 0 || expires == "" {
		return ""
	}
	if run.ProcessPID > 0 && runProcessGroupAlive(run) {
		return ""
	}
	return strings.Join([]string{strings.TrimSpace(run.RecordID), owner, strconv.Itoa(run.LeaseGeneration), expires}, "\x00")
}

func (s *RuntimeStore) selfServiceRepairAttempted(fingerprint string) (bool, error) {
	if s == nil || strings.TrimSpace(fingerprint) == "" {
		return false, nil
	}
	value, err := s.GetSetting(selfServiceRepairAttemptKeyPrefix + fingerprint)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) != "", nil
}

func (s *RuntimeStore) recordSelfServiceRepairAttempt(fingerprint string, now time.Time) error {
	if s == nil || strings.TrimSpace(fingerprint) == "" {
		return nil
	}
	return s.SetSetting(selfServiceRepairAttemptKeyPrefix+fingerprint, now.UTC().Format(time.RFC3339Nano))
}

func (s *RuntimeStore) selfServiceRepairEscalated(fingerprint string) (bool, error) {
	if s == nil || strings.TrimSpace(fingerprint) == "" {
		return false, nil
	}
	value, err := s.GetSetting(selfServiceRepairEscalationKeyPrefix + fingerprint)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) != "", nil
}

func (s *RuntimeStore) recordSelfServiceRepairEscalation(escalation SelfServiceRepairEscalation) error {
	if s == nil || strings.TrimSpace(escalation.Fingerprint) == "" {
		return nil
	}
	raw, err := json.Marshal(escalation)
	if err != nil {
		return err
	}
	return s.SetSetting(selfServiceRepairEscalationKeyPrefix+escalation.Fingerprint, string(raw))
}

// ListSelfServiceRepairEscalations returns persisted once-only repair
// escalations, oldest first. The CLI doctor and Serve diagnosis read this;
// the repair path only ever appends.
func (s *RuntimeStore) listSelfServiceSettingKeys(prefix string) ([]string, error) {
	rows, err := s.query(`SELECT key FROM daemon_settings WHERE key LIKE ? ORDER BY key`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *RuntimeStore) ListSelfServiceRepairEscalations() ([]SelfServiceRepairEscalation, error) {
	if s == nil {
		return nil, nil
	}
	keys, err := s.listSelfServiceSettingKeys(selfServiceRepairEscalationKeyPrefix)
	if err != nil {
		return nil, err
	}
	out := make([]SelfServiceRepairEscalation, 0, len(keys))
	for _, key := range keys {
		value, err := s.GetSetting(key)
		if err != nil {
			return nil, err
		}
		var escalation SelfServiceRepairEscalation
		if err := json.Unmarshal([]byte(value), &escalation); err != nil {
			continue
		}
		out = append(out, escalation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt < out[j].RecordedAt })
	return out, nil
}

// autoRepairDeadReservation applies at most one automatic safe repair for an
// unchanged dead-reservation fingerprint using the existing guarded reclaim
// primitive, then verifies the postcondition against a fresh read.
//
// projectEnabled=false preserves the legacy unbounded reclaim without any
// repair accounting: a disabled project is a normal wait, never a repair
// candidate, and it never escalates.
//
// Returns changed (the underlying row moved, by either path), repaired (this
// call consumed the one automatic repair for the fingerprint), and escalated
// (this call recorded the once-only escalation).
func autoRepairDeadReservation(store *RuntimeStore, projectEnabled bool, run RunStatus, now time.Time) (changed, repaired, escalated bool, err error) {
	if store == nil || (run.HandRun && !run.Terminal) {
		return false, false, false, nil
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	revision := deadReservationRevision(run)
	if revision == "" {
		return false, false, false, nil
	}
	if !projectEnabled {
		changed, err := store.reclaimExpiredRunLeaseIfSnapshot(run, now, defaultRunLeaseTTL, "daemon poll reclaimed expired dead lease")
		if changed && err == nil {
			err = store.requeueReclaimedDeadReservation(run, now, "daemon poll reclaimed expired dead lease")
		}
		return changed, false, false, err
	}
	fingerprint := selfServiceRepairFingerprint(run.ProjectID, selfServiceRepairKindDeadReservation, revision)
	if fingerprint == "" {
		return false, false, false, nil
	}
	alreadyEscalated, err := store.selfServiceRepairEscalated(fingerprint)
	if err != nil {
		return false, false, false, err
	}
	if alreadyEscalated {
		return false, false, false, nil
	}
	attempted, err := store.selfServiceRepairAttempted(fingerprint)
	if err != nil {
		return false, false, false, err
	}
	if attempted {
		if !selfServiceDeadReservationRecurred(store, run, revision) {
			return false, false, false, nil
		}
		escalated, err := selfServiceEscalateOnce(store, fingerprint, run, now,
			"dead reservation "+run.RecordID+" still held by "+strings.TrimSpace(run.LeaseOwner)+
				" generation "+strconv.Itoa(run.LeaseGeneration)+" after automatic safe repair")
		return false, false, escalated, err
	}
	changed, err = store.reclaimExpiredRunLeaseIfSnapshot(run, now, defaultRunLeaseTTL, "self-service automatic safe repair: dead reservation")
	if err != nil {
		return false, false, false, err
	}
	if changed {
		if err := store.requeueReclaimedDeadReservation(run, now, "self-service automatic safe repair: dead reservation"); err != nil {
			return true, false, false, err
		}
	}
	if !changed {
		// Contention or concurrent movement: no repair was applied, so no
		// ledger entry is consumed. The next tick retries normally.
		return false, false, false, nil
	}
	if err := store.recordSelfServiceRepairAttempt(fingerprint, now); err != nil {
		return true, false, false, err
	}
	if !selfServiceDeadReservationRecurred(store, run, revision) {
		return true, true, false, nil
	}
	escalated, err = selfServiceEscalateOnce(store, fingerprint, run, now,
		"dead reservation "+run.RecordID+" still held by "+strings.TrimSpace(run.LeaseOwner)+
			" generation "+strconv.Itoa(run.LeaseGeneration)+" immediately after automatic safe repair")
	return true, true, escalated, err
}

// requeueReclaimedDeadReservation turns the daemon's own reclaim of a dead
// dispatched lease into a due retry. Reclaim leaves the row interrupted, the
// operator-stop state that never dispatches, so an armed task would wait
// forever: no retry time is set and the heartbeat sees no due event. The poll
// still parks a retry_queued run at the attempt cap before it dispatches. Only
// the exact row reclaim just wrote moves; a hand run keeps its released state.
func (s *RuntimeStore) requeueReclaimedDeadReservation(run RunStatus, now time.Time, reason string) error {
	if run.HandRun {
		return nil
	}
	stamp := now.UTC().Format(time.RFC3339)
	_, err := s.exec(`UPDATE runs
		SET lease_state = 'retry_queued', next_retry_at = ?, terminal = 0, active_attempt_id = '',
			prompt_path = '', event_sink_path = '', raw_log_path = '', status_path = '', updated_at = ?
		WHERE project_id = ? AND record_id = ? AND lease_state = 'interrupted' AND (hand_run IS NULL OR hand_run = 0)
			AND last_error = ? AND lease_generation = ? AND active_attempt_id = ?`,
		stamp, stamp, run.ProjectID, run.RecordID, reason, run.LeaseGeneration, run.ActiveAttemptID)
	return err
}

// selfServiceDeadReservationRecurred reports whether the exact dead lease
// identity is still present on a fresh read: the postcondition failed or the
// fault returned.
func selfServiceDeadReservationRecurred(store *RuntimeStore, run RunStatus, revision string) bool {
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil || current == nil {
		return false
	}
	return deadReservationRevision(*current) == revision
}

// selfServiceEscalateOnce records the once-only escalation and reports
// whether this call performed the escalation.
func selfServiceEscalateOnce(store *RuntimeStore, fingerprint string, run RunStatus, now time.Time, evidence string) (bool, error) {
	escalation := SelfServiceRepairEscalation{
		Fingerprint: fingerprint,
		Kind:        selfServiceRepairKindDeadReservation,
		ProjectID:   run.ProjectID,
		RecordID:    run.RecordID,
		Evidence:    evidence,
		RecordedAt:  now.UTC().Format(time.RFC3339Nano),
	}
	if err := store.recordSelfServiceRepairEscalation(escalation); err != nil {
		return false, err
	}
	return true, nil
}

// recordSelfServiceReconcileSchedule persists the per-project schedule and
// last-observation snapshot after a successful poll. Overdue diagnosis reads
// this persisted snapshot; a missing snapshot is unavailable, never healthy.
func (d *Daemon) recordSelfServiceReconcileSchedule(projectID string, now time.Time) {
	if d == nil || d.store == nil || strings.TrimSpace(projectID) == "" {
		return
	}
	now = now.UTC()
	status := d.adaptiveReconcileStatus(projectID)
	snapshot := SelfServiceReconcileSchedule{
		ProjectID:          strings.TrimSpace(projectID),
		Tier:               status.Tier,
		CadenceMS:          status.CadenceMS,
		LastActivityAt:     status.LastActivityAt,
		LastActivityReason: status.LastActivityReason,
		LastPollAt:         status.LastPollAt,
		NextDueAt:          status.NextDueAt,
		RecordedAt:         now.Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	_ = d.store.SetSetting(selfServiceRepairScheduleKeyPrefix+snapshot.ProjectID, string(raw))
}

// persistSelfServiceSchedules persists the in-memory schedule snapshots so
// overdue diagnosis survives restart. A project filter persists only that
// project; an empty filter persists every scheduled project.
func (d *Daemon) persistSelfServiceSchedules(projectID string) {
	if d == nil || d.store == nil {
		return
	}
	now := time.Now().UTC()
	d.reconcileMu.Lock()
	ids := make([]string, 0, len(d.reconcileSchedule))
	for id := range d.reconcileSchedule {
		if projectID != "" && id != strings.TrimSpace(projectID) {
			continue
		}
		ids = append(ids, id)
	}
	d.reconcileMu.Unlock()
	for _, id := range ids {
		d.recordSelfServiceReconcileSchedule(id, now)
	}
}

// loadSelfServiceReconcileSchedule reads the persisted per-project schedule
// snapshot. ok=false means unavailable (never inferred healthy).
func loadSelfServiceReconcileSchedule(store *RuntimeStore, projectID string) (snapshot SelfServiceReconcileSchedule, ok bool) {
	if store == nil || strings.TrimSpace(projectID) == "" {
		return SelfServiceReconcileSchedule{}, false
	}
	value, err := store.GetSetting(selfServiceRepairScheduleKeyPrefix + strings.TrimSpace(projectID))
	if err != nil || strings.TrimSpace(value) == "" {
		return SelfServiceReconcileSchedule{}, false
	}
	if err := json.Unmarshal([]byte(value), &snapshot); err != nil {
		return SelfServiceReconcileSchedule{}, false
	}
	return snapshot, true
}

// selfServiceScheduleOverdue classifies a persisted schedule snapshot
// against its own published next-due time. It returns overdue, the next
// actor, and the scoped next action. Deliberate idle polling (a future
// next-due time) is a normal wait, not a fault.
func selfServiceScheduleOverdue(snapshot SelfServiceReconcileSchedule, now time.Time) (overdue bool, nextActor, nextAction string) {
	nextDue, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(snapshot.NextDueAt))
	if err != nil {
		if fallback, fallbackErr := time.Parse(time.RFC3339, strings.TrimSpace(snapshot.NextDueAt)); fallbackErr == nil {
			nextDue, err = fallback, nil
		}
	}
	if err != nil || !now.UTC().After(nextDue) {
		return false, string(DiagnosticAuthorityNone), ""
	}
	return true, string(DiagnosticAuthorityOperator),
		"reconcile project " + snapshot.ProjectID + " (schedule overdue since " + snapshot.NextDueAt + ")"
}
