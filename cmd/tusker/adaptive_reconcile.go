package main

import (
	"sort"
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
