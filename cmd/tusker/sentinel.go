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
	invariantCircuitSettingKey       = "invariant_circuit_status"
	invariantSpendSnapshotSettingKey = "invariant_spend_snapshot"
	invariantViolationReason         = "invariant_violation"

	invariantCheckHeldLeaseDispatchEligible = "held_lease_dispatch_eligible"
	invariantCheckAttemptCountWithinCaps    = "attempt_count_within_caps"
	invariantCheckFreshHeartbeatPidLive     = "fresh_heartbeat_pid_live"
	invariantCheckUniqueActiveLeasePerTask  = "unique_active_lease_per_task"
	invariantCheckActiveSpendMonotonic      = "active_spend_monotonic"
	invariantCheckLastPollAdvanced          = "last_poll_advanced"

	defaultSentinelFreshHeartbeatMS = int(daemonHeartbeatDeadThreshold / time.Millisecond)
)

type RuntimeSentinelConfig struct {
	Checks           []string `yaml:"checks" json:"checks"`
	FreshHeartbeatMS int      `yaml:"fresh_heartbeat_ms" json:"fresh_heartbeat_ms"`
}

type runtimeInvariantViolation struct {
	Check      string         `json:"check"`
	ProjectID  string         `json:"project_id,omitempty"`
	RecordID   string         `json:"record_id,omitempty"`
	ItemID     string         `json:"item_id,omitempty"`
	Lane       string         `json:"lane,omitempty"`
	LeaseState string         `json:"lease_state,omitempty"`
	Detail     string         `json:"detail"`
	Fields     map[string]any `json:"fields,omitempty"`
}

type invariantCircuitStatus struct {
	Open          bool                        `json:"open"`
	Reason        string                      `json:"reason,omitempty"`
	OpenedAt      string                      `json:"opened_at,omitempty"`
	LastCheckedAt string                      `json:"last_checked_at,omitempty"`
	Summary       string                      `json:"summary,omitempty"`
	Checks        []string                    `json:"checks,omitempty"`
	Violations    []runtimeInvariantViolation `json:"violations,omitempty"`
}

type runtimeSentinelProjectSnapshot struct {
	Project         RegisteredProject
	Workflow        Workflow
	NotesByID       map[string]Note
	NotesByRecordID map[string]Note
}

type runtimeSentinelSnapshot struct {
	Projects       []runtimeSentinelProjectSnapshot
	Runs           []RunStatus
	TokenTotals    map[string]runtimeBudgetTotals
	PreviousPollAt string
	CurrentPollAt  string
	Now            time.Time
	Resume         bool
	Liveness       func(RunStatus) bool
}

type runtimeSpendSnapshot map[string]runtimeBudgetTotals

func defaultRuntimeSentinelConfig() RuntimeSentinelConfig {
	return RuntimeSentinelConfig{
		Checks: []string{
			invariantCheckHeldLeaseDispatchEligible,
			invariantCheckAttemptCountWithinCaps,
			invariantCheckFreshHeartbeatPidLive,
			invariantCheckUniqueActiveLeasePerTask,
			invariantCheckLastPollAdvanced,
		},
		FreshHeartbeatMS: defaultSentinelFreshHeartbeatMS,
	}
}

func withDefaultRuntimeSentinelConfig(config RuntimeSentinelConfig) RuntimeSentinelConfig {
	defaults := defaultRuntimeSentinelConfig()
	if len(config.Checks) == 0 {
		config.Checks = defaults.Checks
	}
	if config.FreshHeartbeatMS <= 0 {
		config.FreshHeartbeatMS = defaults.FreshHeartbeatMS
	}
	return config
}

func (s *RuntimeStore) ReadInvariantCircuitStatus() (invariantCircuitStatus, error) {
	raw, err := s.GetSetting(invariantCircuitSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return invariantCircuitStatus{}, err
	}
	var status invariantCircuitStatus
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return invariantCircuitStatus{}, err
	}
	return status, nil
}

func (s *RuntimeStore) SetInvariantCircuitStatus(status invariantCircuitStatus) error {
	raw, err := json.Marshal(status)
	if err != nil {
		return err
	}
	return s.SetSetting(invariantCircuitSettingKey, string(raw))
}

func (s *RuntimeStore) ClearInvariantCircuitStatus(checkedAt string) error {
	return s.SetInvariantCircuitStatus(invariantCircuitStatus{
		Open:          false,
		LastCheckedAt: checkedAt,
		Summary:       "invariant circuit closed",
	})
}

func (s *RuntimeStore) ReadInvariantSpendSnapshot() (runtimeSpendSnapshot, error) {
	raw, err := s.GetSetting(invariantSpendSnapshotSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return runtimeSpendSnapshot{}, err
	}
	var snapshot runtimeSpendSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return runtimeSpendSnapshot{}, err
	}
	return snapshot, nil
}

func (s *RuntimeStore) SetInvariantSpendSnapshot(snapshot runtimeSpendSnapshot) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	return s.SetSetting(invariantSpendSnapshotSettingKey, string(raw))
}

func (s *RuntimeStore) RunTokenTotalsByRun() (map[string]runtimeBudgetTotals, error) {
	rows, err := s.query(`SELECT project_id, record_id, COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0)
		FROM turns
		GROUP BY project_id, record_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]runtimeBudgetTotals{}
	for rows.Next() {
		var projectID, recordID string
		var totals runtimeBudgetTotals
		if err := rows.Scan(&projectID, &recordID, &totals.InputTokens, &totals.OutputTokens, &totals.TotalTokens); err != nil {
			return nil, err
		}
		out[runtimeRunKey(projectID, recordID)] = totals
	}
	return out, rows.Err()
}

func (d *Daemon) invariantDispatchBlocker() (string, error) {
	if d == nil || d.store == nil {
		return "", nil
	}
	status, err := d.store.ReadInvariantCircuitStatus()
	if err != nil || !status.Open {
		return "", err
	}
	return "invariant circuit open: " + invariantCircuitSummary(status), nil
}

func (d *Daemon) refreshInvariantCircuitStatus(snapshot runtimeSentinelSnapshot) (invariantCircuitStatus, error) {
	status, err := d.evaluateInvariantSentinel(snapshot)
	if err != nil {
		return status, err
	}
	failures, err := d.store.ReadEventLogPersistenceFailures()
	if err != nil {
		return status, err
	}
	status = mergeEventLogPersistenceFailures(status, failures)
	current, err := d.store.ReadInvariantCircuitStatus()
	if err != nil {
		return status, err
	}
	if len(status.Violations) == 0 {
		if current.Open {
			current.LastCheckedAt = status.LastCheckedAt
			if err := d.store.SetInvariantCircuitStatus(current); err != nil {
				return current, err
			}
			return current, nil
		}
		status.Open = false
		status.Summary = "no invariant violations"
		return status, d.store.SetInvariantCircuitStatus(status)
	}
	if current.Open && strings.TrimSpace(current.OpenedAt) != "" {
		status.OpenedAt = current.OpenedAt
	}
	if err := d.store.SetInvariantCircuitStatus(status); err != nil {
		return status, err
	}
	d.recordInvariantEscalations(snapshot, status)
	fmt.Fprintf(os.Stderr, "%s: %s\n", invariantViolationReason, invariantCircuitSummary(status))
	return status, nil
}

func (d *Daemon) ResumeInvariantCircuit() (invariantCircuitStatus, error) {
	if d == nil || d.store == nil {
		return invariantCircuitStatus{}, nil
	}
	previousPollAt, err := d.store.GetSetting("daemon_last_poll_at")
	if err != nil {
		return invariantCircuitStatus{}, err
	}
	projects, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{Notes: true})
	if err != nil {
		return invariantCircuitStatus{}, err
	}
	runs, err := d.store.ListRuns()
	if err != nil {
		return invariantCircuitStatus{}, err
	}
	snapshot := runtimeSentinelSnapshot{
		Runs:           runs,
		PreviousPollAt: previousPollAt,
		CurrentPollAt:  previousPollAt,
		Now:            time.Now().UTC(),
		Resume:         true,
		Liveness:       processIdentityMatches,
	}
	for _, loaded := range projects {
		if !loaded.Loadable() {
			continue
		}
		notesByID, notesByRecordID := daemonNoteMaps(loaded.Notes)
		snapshot.Projects = append(snapshot.Projects, runtimeSentinelProjectSnapshot{
			Project:         loaded.Project,
			Workflow:        loaded.Workflow.Data,
			NotesByID:       notesByID,
			NotesByRecordID: notesByRecordID,
		})
	}
	status, err := d.evaluateInvariantSentinel(snapshot)
	if err != nil {
		return status, err
	}
	failureRegistry, failureRegistryRaw, err := d.store.readEventLogPersistenceFailureRegistry()
	if err != nil {
		return status, err
	}
	if len(failureRegistry.Failures) > 0 {
		probeErrors := d.probeEventLogPersistenceFailures(failureRegistry.Failures)
		probeFailed := false
		failedAt := time.Now().UTC().Format(time.RFC3339Nano)
		for index, probeErr := range probeErrors {
			if probeErr == nil {
				continue
			}
			probeFailed = true
			failureRegistry.Failures[index].Reason = probeErr.Error()
			failureRegistry.Failures[index].LastFailedAt = failedAt
		}
		remaining := failureRegistry.Failures
		if !probeFailed {
			remaining = nil
		}
		updated, updateErr := d.store.compareAndSwapEventLogPersistenceFailureRegistry(failureRegistryRaw, remaining)
		if updateErr != nil {
			return status, updateErr
		}
		if !updated {
			latest, readErr := d.store.ReadEventLogPersistenceFailures()
			if readErr != nil {
				return status, readErr
			}
			status = mergeEventLogPersistenceFailures(status, latest)
			current, _ := d.store.ReadInvariantCircuitStatus()
			if strings.TrimSpace(current.OpenedAt) != "" {
				status.OpenedAt = current.OpenedAt
			}
			if err := d.store.SetInvariantCircuitStatus(status); err != nil {
				return status, err
			}
			return status, tuskerError(errorInvalidTransition, "cannot resume daemon: event-log failure registry changed during recovery; retry after dispatch is quiescent", withContext(map[string]any{"reason": "event_log_persistence_failure"}))
		}
		if probeFailed {
			status = mergeEventLogPersistenceFailures(status, failureRegistry.Failures)
			current, _ := d.store.ReadInvariantCircuitStatus()
			if strings.TrimSpace(current.OpenedAt) != "" {
				status.OpenedAt = current.OpenedAt
			}
			if err := d.store.SetInvariantCircuitStatus(status); err != nil {
				return status, err
			}
			return status, tuskerError(errorInvalidTransition, "cannot resume daemon: "+invariantCircuitSummary(status), withContext(map[string]any{"reason": "event_log_persistence_failure", "violations": status.Violations}))
		}
	}
	if len(status.Violations) > 0 {
		current, _ := d.store.ReadInvariantCircuitStatus()
		if strings.TrimSpace(current.OpenedAt) != "" {
			status.OpenedAt = current.OpenedAt
		}
		if err := d.store.SetInvariantCircuitStatus(status); err != nil {
			return status, err
		}
		return status, tuskerError(errorInvalidTransition, "cannot resume daemon: "+invariantCircuitSummary(status), withContext(map[string]any{"reason": invariantViolationReason, "violations": status.Violations}))
	}
	if err := d.store.ClearInvariantCircuitStatus(status.LastCheckedAt); err != nil {
		return status, err
	}
	return status, nil
}

func (d *Daemon) evaluateInvariantSentinel(snapshot runtimeSentinelSnapshot) (invariantCircuitStatus, error) {
	if d == nil || d.store == nil {
		return invariantCircuitStatus{}, nil
	}
	if snapshot.Now.IsZero() {
		snapshot.Now = time.Now().UTC()
	}
	if snapshot.Liveness == nil {
		snapshot.Liveness = processIdentityMatches
	}
	if snapshot.TokenTotals == nil {
		snapshot.TokenTotals = map[string]runtimeBudgetTotals{}
	}
	status := invariantCircuitStatus{
		Open:          true,
		Reason:        invariantViolationReason,
		OpenedAt:      snapshot.Now.Format(time.RFC3339Nano),
		LastCheckedAt: snapshot.Now.Format(time.RFC3339Nano),
	}
	runsByProject := sentinelRunsByProject(snapshot.Runs)
	for _, project := range snapshot.Projects {
		config := withDefaultRuntimeSentinelConfig(project.Workflow.Runtime.Sentinel)
		for _, check := range config.Checks {
			if check == invariantCheckActiveSpendMonotonic {
				continue
			}
			status.Checks = append(status.Checks, check)
			if snapshot.Resume && check == invariantCheckLastPollAdvanced {
				continue
			}
			var violations []runtimeInvariantViolation
			switch check {
			case invariantCheckHeldLeaseDispatchEligible:
				violations = sentinelHeldLeaseDispatchEligible(project, runsByProject[project.Project.ProjectID])
			case invariantCheckAttemptCountWithinCaps:
				violations = sentinelAttemptCountWithinCaps(project, runsByProject[project.Project.ProjectID])
			case invariantCheckFreshHeartbeatPidLive:
				violations = sentinelFreshHeartbeatPidLive(project, runsByProject[project.Project.ProjectID], config, snapshot.Now, snapshot.Liveness)
			case invariantCheckUniqueActiveLeasePerTask:
				violations = sentinelUniqueActiveLeasePerTask(project, runsByProject[project.Project.ProjectID])
			case invariantCheckLastPollAdvanced:
				violations = sentinelLastPollAdvanced(snapshot.PreviousPollAt, snapshot.CurrentPollAt)
			default:
				violations = []runtimeInvariantViolation{{
					Check:     check,
					ProjectID: project.Project.ProjectID,
					Detail:    "runtime sentinel check is not registered",
					Fields:    map[string]any{"check": check},
				}}
			}
			status.Violations = append(status.Violations, violations...)
		}
	}
	status.Checks = uniqueStrings(status.Checks)
	sort.Slice(status.Violations, func(i, j int) bool {
		left, right := status.Violations[i], status.Violations[j]
		for _, cmp := range []int{
			strings.Compare(left.ProjectID, right.ProjectID),
			strings.Compare(left.RecordID, right.RecordID),
			strings.Compare(left.Check, right.Check),
			strings.Compare(left.Detail, right.Detail),
		} {
			if cmp != 0 {
				return cmp < 0
			}
		}
		return false
	})
	if len(status.Violations) == 0 {
		status.Open = false
		status.Reason = ""
		status.OpenedAt = ""
		status.Summary = "no invariant violations"
	} else {
		status.Summary = invariantViolationReason + ": " + status.Violations[0].Detail
	}
	return status, nil
}

func sentinelHeldLeaseDispatchEligible(project runtimeSentinelProjectSnapshot, runs []RunStatus) []runtimeInvariantViolation {
	var violations []runtimeInvariantViolation
	for _, run := range runs {
		if !isDispatchCapacityLeaseState(run.LeaseState) {
			continue
		}
		if runnerStatusReadyForReconcile(run) {
			continue
		}
		note := project.NotesByRecordID[run.RecordID]
		if stringField(note.Data, "id") == "" && strings.TrimSpace(run.ItemID) != "" {
			note = project.NotesByID[run.ItemID]
		}
		if stringField(note.Data, "id") == "" {
			violations = append(violations, runViolation(run, invariantCheckHeldLeaseDispatchEligible, "held lease has no matching task", nil))
			continue
		}
		status := strings.TrimSpace(stringField(note.Data, "status"))
		allowed := containsString(project.Workflow.Tracker.ActiveStates, status)
		if run.Lane == runLaneReview {
			allowed = containsString(project.Workflow.Tracker.ReviewStates, status)
		}
		if !allowed {
			violations = append(violations, runViolation(run, invariantCheckHeldLeaseDispatchEligible, "held lease task is not dispatch-eligible", map[string]any{
				"task_status":   status,
				"active_states": project.Workflow.Tracker.ActiveStates,
				"review_states": project.Workflow.Tracker.ReviewStates,
			}))
		}
	}
	return violations
}

func runnerStatusReadyForReconcile(run RunStatus) bool {
	return strings.TrimSpace(run.StatusPath) != "" && fileExists(run.StatusPath)
}

func sentinelAttemptCountWithinCaps(project runtimeSentinelProjectSnapshot, runs []RunStatus) []runtimeInvariantViolation {
	capValue := project.Workflow.Retry.MaxAttempts
	if continuation := maxContinuationRetries(project.Workflow); continuation > capValue {
		capValue = continuation
	}
	if capValue <= 0 {
		return nil
	}
	var violations []runtimeInvariantViolation
	for _, run := range runs {
		if run.AttemptCount <= capValue || run.Terminal || sentinelRunParked(run) {
			continue
		}
		violations = append(violations, runViolation(run, invariantCheckAttemptCountWithinCaps, "run attempt count exceeds configured caps without parked or terminal state", map[string]any{
			"attempt_count": run.AttemptCount,
			"cap":           capValue,
		}))
	}
	return violations
}

func sentinelFreshHeartbeatPidLive(project runtimeSentinelProjectSnapshot, runs []RunStatus, config RuntimeSentinelConfig, now time.Time, liveness func(RunStatus) bool) []runtimeInvariantViolation {
	_ = project
	freshFor := time.Duration(config.FreshHeartbeatMS) * time.Millisecond
	var violations []runtimeInvariantViolation
	for _, run := range runs {
		if !isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		if runnerStatusReadyForReconcile(run) {
			continue
		}
		heartbeatAt, ok := parseSentinelTimestamp(run.LastHeartbeatAt)
		if !ok || now.Sub(heartbeatAt) > freshFor {
			continue
		}
		if liveness(run) {
			continue
		}
		violations = append(violations, runViolation(run, invariantCheckFreshHeartbeatPidLive, "fresh heartbeat outlived its recorded process identity", map[string]any{
			"last_heartbeat_at":  run.LastHeartbeatAt,
			"process_pid":        run.ProcessPID,
			"process_started_at": run.ProcessStartedAt,
		}))
	}
	return violations
}

func sentinelUniqueActiveLeasePerTask(project runtimeSentinelProjectSnapshot, runs []RunStatus) []runtimeInvariantViolation {
	_ = project
	activeByTask := map[string][]RunStatus{}
	maxGenerationByTask := map[string]int{}
	for _, run := range runs {
		if !isDispatchingLeaseState(run.LeaseState) {
			continue
		}
		key := firstNonEmpty(strings.TrimSpace(run.ItemID), strings.TrimSpace(run.RecordID))
		if key == "" {
			continue
		}
		activeByTask[key] = append(activeByTask[key], run)
		if run.LeaseGeneration > maxGenerationByTask[key] {
			maxGenerationByTask[key] = run.LeaseGeneration
		}
	}
	var violations []runtimeInvariantViolation
	for taskID, active := range activeByTask {
		if len(active) > 1 {
			for _, run := range active {
				violations = append(violations, runViolation(run, invariantCheckUniqueActiveLeasePerTask, "multiple active leases for one task", map[string]any{
					"task":               taskID,
					"active_lease_count": len(active),
				}))
			}
		}
		for _, run := range active {
			if maxGenerationByTask[taskID] > 0 && run.LeaseGeneration > 0 && run.LeaseGeneration < maxGenerationByTask[taskID] {
				violations = append(violations, runViolation(run, invariantCheckUniqueActiveLeasePerTask, "active lease is owned by a non-incumbent generation", map[string]any{
					"task":                 taskID,
					"lease_generation":     run.LeaseGeneration,
					"incumbent_generation": maxGenerationByTask[taskID],
				}))
			}
		}
	}
	return violations
}

func sentinelActiveSpendMonotonic(project runtimeSentinelProjectSnapshot, runs []RunStatus, totals map[string]runtimeBudgetTotals, previous runtimeSpendSnapshot) []runtimeInvariantViolation {
	_ = project
	var violations []runtimeInvariantViolation
	for _, run := range runs {
		if !isDispatchCapacityLeaseState(run.LeaseState) {
			continue
		}
		key := runtimeRunKey(run.ProjectID, run.RecordID)
		before, ok := previous[key]
		if !ok {
			continue
		}
		after := totals[key]
		if after.InputTokens < before.InputTokens || after.OutputTokens < before.OutputTokens || after.TotalTokens < before.TotalTokens {
			violations = append(violations, runViolation(run, invariantCheckActiveSpendMonotonic, "active run spend decreased since previous tick", map[string]any{
				"previous": before,
				"current":  after,
			}))
		}
	}
	return violations
}

func sentinelLastPollAdvanced(previousPollAt, currentPollAt string) []runtimeInvariantViolation {
	previousPollAt = strings.TrimSpace(previousPollAt)
	currentPollAt = strings.TrimSpace(currentPollAt)
	if previousPollAt == "" {
		return nil
	}
	if currentPollAt == "" {
		return []runtimeInvariantViolation{{Check: invariantCheckLastPollAdvanced, Detail: "daemon_last_poll_at did not advance"}}
	}
	previous, previousOK := parseSentinelTimestamp(previousPollAt)
	current, currentOK := parseSentinelTimestamp(currentPollAt)
	if previousOK && currentOK {
		if !current.After(previous) {
			return []runtimeInvariantViolation{{Check: invariantCheckLastPollAdvanced, Detail: "daemon_last_poll_at did not advance", Fields: map[string]any{"previous": previousPollAt, "current": currentPollAt}}}
		}
		return nil
	}
	if currentPollAt == previousPollAt {
		return []runtimeInvariantViolation{{Check: invariantCheckLastPollAdvanced, Detail: "daemon_last_poll_at did not advance", Fields: map[string]any{"previous": previousPollAt, "current": currentPollAt}}}
	}
	return nil
}

func daemonNoteMaps(notes []Note) (map[string]Note, map[string]Note) {
	notesByID := map[string]Note{}
	notesByRecordID := map[string]Note{}
	for _, note := range notes {
		if daemonNoteKind(note) != "task" {
			continue
		}
		if id := stringField(note.Data, "id"); id != "" {
			notesByID[id] = note
		}
		if recordID := trackerRecordID(note); recordID != "" {
			notesByRecordID[recordID] = note
		}
	}
	return notesByID, notesByRecordID
}

func sentinelRunsByProject(runs []RunStatus) map[string][]RunStatus {
	out := map[string][]RunStatus{}
	for _, run := range runs {
		out[run.ProjectID] = append(out[run.ProjectID], run)
	}
	return out
}

func currentActiveSpendSnapshot(runs []RunStatus, totals map[string]runtimeBudgetTotals) runtimeSpendSnapshot {
	out := runtimeSpendSnapshot{}
	for _, run := range runs {
		if !isDispatchCapacityLeaseState(run.LeaseState) {
			continue
		}
		key := runtimeRunKey(run.ProjectID, run.RecordID)
		out[key] = totals[key]
	}
	return out
}

func runViolation(run RunStatus, check, detail string, fields map[string]any) runtimeInvariantViolation {
	return runtimeInvariantViolation{
		Check:      check,
		ProjectID:  run.ProjectID,
		RecordID:   run.RecordID,
		ItemID:     run.ItemID,
		Lane:       run.Lane,
		LeaseState: run.LeaseState,
		Detail:     detail,
		Fields:     fields,
	}
}

func sentinelRunParked(run RunStatus) bool {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateParkedNoProgress, LeaseStateParkedBudget:
		return true
	default:
		return false
	}
}

func invariantCircuitSummary(status invariantCircuitStatus) string {
	if status.Reason == "event_log_persistence_failure" {
		summary := strings.TrimSpace(status.Summary)
		if summary == "" {
			summary = status.Reason
		}
		return summary + "; repair event-log storage, then run `tusker daemon resume`"
	}
	if strings.TrimSpace(status.Summary) != "" {
		if !status.Open && len(status.Violations) == 0 {
			return status.Summary
		}
		return invariantCircuitSummaryWithRepair(status.Summary)
	}
	if len(status.Violations) > 0 {
		return invariantCircuitSummaryWithRepair(invariantViolationReason + ": " + status.Violations[0].Detail)
	}
	if strings.TrimSpace(status.Reason) != "" {
		return invariantCircuitSummaryWithRepair(status.Reason)
	}
	return invariantCircuitSummaryWithRepair(invariantViolationReason)
}

func invariantCircuitSummaryWithRepair(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" || strings.Contains(summary, "tusker runs retire") {
		return summary
	}
	return summary + "; repair stale terminal records with `tusker runs retire <task-id-or-record-id> --reason <why>`"
}

func parseSentinelTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func runtimeRunKey(projectID, recordID string) string {
	return strings.TrimSpace(projectID) + "\x00" + strings.TrimSpace(recordID)
}
