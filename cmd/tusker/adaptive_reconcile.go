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
	reconcileHotCadence  = time.Minute
	reconcileWarmCadence = 5 * time.Minute
	reconcileCoolCadence = 10 * time.Minute
	reconcileColdCadence = 30 * time.Minute
	reconcileLiveCadence = 5 * time.Second
)

type adaptiveProjectReconcileState struct {
	LastActivityAt     time.Time
	LastActivityReason string
	LastPollAt         time.Time
	NextDueAt          time.Time
	Cadence            time.Duration
	Tier               string
}

type adaptiveProjectReconcileStatus struct {
	Tier               string `json:"tier"`
	CadenceMS          int64  `json:"cadenceMs"`
	LastActivityAt     string `json:"lastActivityAt,omitempty"`
	LastActivityReason string `json:"lastActivityReason,omitempty"`
	LastPollAt         string `json:"lastPollAt,omitempty"`
	NextDueAt          string `json:"nextDueAt,omitempty"`
}

func adaptiveReconcileCadence(idle time.Duration, runtimeUrgent bool) (string, time.Duration) {
	return adaptiveReconcileCadenceWithHot(idle, runtimeUrgent, reconcileHotCadence)
}

func adaptiveReconcileCadenceWithHot(idle time.Duration, runtimeUrgent bool, hotCadence time.Duration) (string, time.Duration) {
	if runtimeUrgent {
		return "live", reconcileLiveCadence
	}
	if hotCadence <= 0 {
		hotCadence = reconcileHotCadence
	}
	switch {
	case idle < reconcileHotCadence:
		return "hot", hotCadence
	case idle < reconcileWarmCadence:
		return "warm", reconcileWarmCadence
	case idle < reconcileCoolCadence:
		return "cool", reconcileCoolCadence
	default:
		return "cold", reconcileColdCadence
	}
}

func runtimeRunNeedsHotReconcile(run RunStatus) bool {
	return runtimeRunNeedsHotReconcileAt(run, time.Now().UTC())
}

func runtimeRunNeedsHotReconcileAt(run RunStatus, now time.Time) bool {
	if run.Terminal {
		return false
	}
	// A dead/stale row must get a bounded reconciliation, not pin its project
	// to the five-second loop forever. Rows without timestamps are legacy data;
	// keep those urgent until one normal poll can repair them.
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
		// capacity. Keep it live so capacity released by another project is
		// observed promptly. A review handoff is also deliberately unclaimed while
		// it waits for an upstream DAG edge to land; that is live control-plane
		// work even when the last pass recorded a dependency blocker. Ordinary
		// policy-blocked execute rows may still back off.
		if run.Lane == runLaneReview && run.AttemptOutcome == string(AttemptOutcomeWaitingForReview) {
			return true
		}
		return strings.TrimSpace(run.LastError) == ""
	case LeaseStateReleased:
		// A review handoff has no active lease by design, but it is not quiescent
		// work. Before a reviewer can claim it, an upstream DAG edge may still
		// need to land on integration. After the reviewer submits, the
		// authoritative completion reactor must consume that typed receipt. Keep
		// both durable handoffs live without making every released historical run
		// poll forever.
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

func (d *Daemon) noteProjectActivity(projectID, reason string, now time.Time) {
	projectID = strings.TrimSpace(projectID)
	if d == nil || projectID == "" {
		return
	}
	now = now.UTC()
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	if d.reconcileSchedule == nil {
		d.reconcileSchedule = map[string]adaptiveProjectReconcileState{}
	}
	state := d.reconcileSchedule[projectID]
	state.LastActivityAt = now
	state.LastActivityReason = strings.TrimSpace(reason)
	state.Tier = "hot"
	state.Cadence = reconcileHotCadence
	state.NextDueAt = now.Add(reconcileHotCadence)
	d.reconcileSchedule[projectID] = state
}

func (d *Daemon) recordProjectPoll(projectID string, now time.Time, runtimeUrgent bool) {
	projectID = strings.TrimSpace(projectID)
	if d == nil || projectID == "" {
		return
	}
	now = now.UTC()
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	if d.reconcileSchedule == nil {
		d.reconcileSchedule = map[string]adaptiveProjectReconcileState{}
	}
	state := d.reconcileSchedule[projectID]
	if state.LastActivityAt.IsZero() {
		state.LastActivityAt = now
		state.LastActivityReason = "daemon_start"
	}
	tier, cadence := adaptiveReconcileCadenceWithHot(now.Sub(state.LastActivityAt), runtimeUrgent, d.nextPollInterval())
	state.LastPollAt = now
	state.Tier = tier
	state.Cadence = cadence
	state.NextDueAt = now.Add(cadence)
	d.reconcileSchedule[projectID] = state
}

func (d *Daemon) recordPollSchedule(projectID string, now time.Time) error {
	loaded, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return err
	}
	projects := loadedRegisteredProjects(loaded)
	runs, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	urgent := map[string]bool{}
	for _, run := range runs {
		if runtimeRunNeedsHotReconcileAt(run, now) {
			urgent[run.ProjectID] = true
		}
	}
	enabled := map[string]bool{}
	for _, project := range projects {
		if !project.Enabled || (projectID != "" && project.ProjectID != projectID) {
			continue
		}
		enabled[project.ProjectID] = true
		d.recordProjectPoll(project.ProjectID, now, urgent[project.ProjectID])
	}
	if projectID == "" {
		d.reconcileMu.Lock()
		for id := range d.reconcileSchedule {
			if !enabled[id] {
				delete(d.reconcileSchedule, id)
			}
		}
		d.reconcileMu.Unlock()
	}
	return nil
}

func (d *Daemon) adaptiveProjectsDue(now time.Time) ([]string, time.Duration, error) {
	now = now.UTC()
	loaded, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return nil, reconcileHotCadence, err
	}
	projects := loadedRegisteredProjects(loaded)
	runs, err := d.store.ListRuns()
	if err != nil {
		return nil, reconcileHotCadence, err
	}
	urgent := map[string]bool{}
	for _, run := range runs {
		if runtimeRunNeedsHotReconcileAt(run, now) {
			urgent[run.ProjectID] = true
		}
	}
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	if d.reconcileSchedule == nil {
		d.reconcileSchedule = map[string]adaptiveProjectReconcileState{}
	}
	enabled := map[string]bool{}
	due := []string{}
	nextWait := reconcileColdCadence
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		enabled[project.ProjectID] = true
		state, ok := d.reconcileSchedule[project.ProjectID]
		if !ok {
			state = adaptiveProjectReconcileState{LastActivityAt: now, LastActivityReason: "project_discovered"}
		}
		tier, cadence := adaptiveReconcileCadenceWithHot(now.Sub(state.LastActivityAt), urgent[project.ProjectID], d.nextPollInterval())
		if state.NextDueAt.IsZero() || (urgent[project.ProjectID] && state.NextDueAt.After(now.Add(cadence))) {
			state.NextDueAt = now.Add(cadence)
		}
		state.Tier = tier
		state.Cadence = cadence
		d.reconcileSchedule[project.ProjectID] = state
		wait := state.NextDueAt.Sub(now)
		if wait <= 0 {
			due = append(due, project.ProjectID)
			continue
		}
		if wait < nextWait {
			nextWait = wait
		}
	}
	for id := range d.reconcileSchedule {
		if !enabled[id] {
			delete(d.reconcileSchedule, id)
		}
	}
	if len(due) > 0 {
		nextWait = 0
	}
	if len(projects) == 0 {
		nextWait = d.nextPollInterval()
	}
	sort.Strings(due)
	return due, nextWait, nil
}

func (d *Daemon) adaptiveReconcileStatus(projectID string) adaptiveProjectReconcileStatus {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	state := d.reconcileSchedule[strings.TrimSpace(projectID)]
	result := adaptiveProjectReconcileStatus{
		Tier: state.Tier, CadenceMS: state.Cadence.Milliseconds(),
		LastActivityReason: state.LastActivityReason,
	}
	if !state.LastActivityAt.IsZero() {
		result.LastActivityAt = state.LastActivityAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.LastPollAt.IsZero() {
		result.LastPollAt = state.LastPollAt.UTC().Format(time.RFC3339Nano)
	}
	if !state.NextDueAt.IsZero() {
		result.NextDueAt = state.NextDueAt.UTC().Format(time.RFC3339Nano)
	}
	return result
}

func (d *Daemon) adaptiveWatchdogCadence() time.Duration {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()
	result := reconcileColdCadence
	found := false
	for _, state := range d.reconcileSchedule {
		if state.Cadence <= 0 {
			continue
		}
		if !found || state.Cadence < result {
			result = state.Cadence
			found = true
		}
	}
	if !found {
		return reconcileHotCadence
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

func resetTimer(timer *time.Timer, wait time.Duration) {
	if wait < 100*time.Millisecond {
		wait = 100 * time.Millisecond
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(wait)
}
