package main

import (
	"context"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The daemon works on a project only when something changed or the operator
// is using it (docs/system/proposals/event-driven-daemon.md). CLI commands,
// runner reports and UI interaction wake it over the control socket. The
// 5-second heartbeat below never loads a vault and never starts git: it runs
// the process janitor and polls a project only when an event is due.

// startupReconcile rescans only projects with live work. Every other project
// waits for an event; its scheduled departure and batch-gate wake-ups are
// seeded from its workflow without reading its tasks.
func (d *Daemon) startupReconcile(ctx context.Context) error {
	now := time.Now()
	if err := d.store.ExpireRunDirectives(now.UTC()); err != nil {
		return err
	}
	runs, err := d.store.ListReconcileRuns("")
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, run := range runs {
		if runtimeRunNeedsHotReconcileAt(run, now.UTC()) || runWaitsForAdmission(run) {
			live[run.ProjectID] = true
		}
	}
	loaded, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{})
	if err != nil {
		return err
	}
	for _, project := range loaded {
		if !project.Project.Enabled || !project.Loadable() {
			continue
		}
		id := project.Project.ProjectID
		d.setProjectTier(id, live[id])
		if err := d.refreshDepartureSchedule(id, project.Workflow.Data, now); err != nil {
			log.Printf("daemon startup: project=%s departure schedule: %v", id, err)
		}
		d.noteBatchGateWake(id, project.Workflow.Data.Orchestration.BatchGate, now, false)
		if live[id] {
			if err := d.runPoll(ctx, id); err != nil {
				return err
			}
		}
	}
	// Overwrite schedules persisted by an older timed daemon so none reads
	// overdue against a due time that no longer exists.
	d.persistSelfServiceSchedules("")
	return nil
}

// heartbeat runs one beat and polls each due project once.
func (d *Daemon) heartbeat(ctx context.Context, now time.Time) error {
	d.runProcessJanitor(now)
	due, err := d.heartbeatProjectsDue(now)
	if err != nil {
		return err
	}
	for _, projectID := range sortedKeys(due) {
		if due[projectID] {
			// An outside edit needs the full rescan, not the frontier.
			err = d.runPoll(ctx, projectID)
		} else {
			err = d.runScheduledPoll(ctx, projectID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// heartbeatProjectsDue returns the projects with a due event, each mapped to
// whether it needs a full rescan. It reads the runtime store and in-memory
// state and, for active projects only, file metadata.
func (d *Daemon) heartbeatProjectsDue(now time.Time) (map[string]bool, error) {
	now = now.UTC()
	loaded, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return nil, err
	}
	projects := loadedRegisteredProjects(loaded)
	d.pruneFrontiers(projects)
	runs, err := d.store.ListReconcileRuns("")
	if err != nil {
		return nil, err
	}
	byProject := map[string][]RunStatus{}
	for _, run := range runs {
		byProject[run.ProjectID] = append(byProject[run.ProjectID], run)
	}
	capacity := countDispatchCapacityRuns(runs)
	capacityFreed := capacity < d.heartbeatCapacity
	d.heartbeatCapacity = capacity

	due := map[string]bool{}
	for _, id := range d.departureProjectsDue(now) {
		due[id] = false
	}
	seenRuns := map[string]bool{}
	enabled := map[string]bool{}
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		id := project.ProjectID
		enabled[id] = true
		if d.batchGateDue(id, now) {
			due[id] = false
		}
		active := d.projectActive(id, byProject[id], now)
		d.setProjectTier(id, active)
		if !active {
			continue
		}
		for _, run := range byProject[id] {
			seenRuns[run.ProjectID+"\x00"+run.RecordID] = true
			if d.runEventDue(run, now) {
				due[id] = false
			}
			waiting := runWaitsForAdmission(run) || (!run.Terminal && LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateUnclaimed && strings.TrimSpace(run.LastError) == "")
			if waiting && (capacityFreed || now.Sub(d.lastProjectPoll(id)) >= d.nextPollInterval()) {
				due[id] = false
			}
		}
		if d.projectEdited(project) {
			d.recordFrontierHint(id, nil)
			if d.serve != nil {
				d.serve.invalidateProjectSnapshot(id)
			}
			due[id] = true
		}
	}
	for key := range d.heartbeatRunEvents {
		if !seenRuns[key] {
			delete(d.heartbeatRunEvents, key)
		}
	}
	d.reconcileMu.Lock()
	for id := range d.reconcileSchedule {
		if !enabled[id] {
			delete(d.reconcileSchedule, id)
		}
	}
	d.reconcileMu.Unlock()
	return due, nil
}

// projectActive: a live or queued run, an armed wave with unfinished members,
// or an interaction within projectInteractionWindow. Being registered or shown
// in the UI does not make a project active.
func (d *Daemon) projectActive(projectID string, runs []RunStatus, now time.Time) bool {
	for _, run := range runs {
		if runtimeRunNeedsHotReconcileAt(run, now) || runWaitsForAdmission(run) {
			return true
		}
	}
	return d.recentProjectActivity(projectID, now) || d.projectHasArmedWork(projectID)
}

// projectHasArmedWork reads the in-memory frontier from the last scan.
func (d *Daemon) projectHasArmedWork(projectID string) bool {
	d.frontierMu.Lock()
	defer d.frontierMu.Unlock()
	index := d.frontiers[projectID]
	if index == nil {
		return false
	}
	for _, wave := range index.Records {
		if effectiveV7Kind(wave.Data) != "wave" || stringField(wave.Data, "authorization") != "armed" || v7WaveHasDurableTerminal(wave) {
			continue
		}
		for _, member := range normalizeList(wave.Data["members"]) {
			if task, ok := index.Records[member]; ok && !v7TerminalTaskStatus(stringField(task.Data, "status")) {
				return true
			}
		}
	}
	return false
}

// runEventDue reports a timed or liveness event on a run: a retry backoff or
// lease that came due, or a runner whose process is gone or whose heartbeat
// went stale. It fires once per distinct run state, so a poll that leaves the
// run unchanged is not repeated every beat.
func (d *Daemon) runEventDue(run RunStatus, now time.Time) bool {
	if run.Terminal {
		return false
	}
	event := ""
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateRetryQueued:
		if shouldDispatchRun(run, now) {
			event = "retry"
		}
	case LeaseStateClaimed, LeaseStateRunning:
		if expires, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(run.LeaseExpiresAt)); err == nil && !expires.After(now) {
			event = "lease_expired"
		} else if (run.ProcessPGID > 0 && !processGroupExists(run.ProcessPGID)) || (run.ProcessPGID <= 0 && run.ProcessPID > 0 && !processExists(run.ProcessPID)) {
			event = "process_gone"
		} else if beat, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(run.LastHeartbeatAt)); err == nil && now.Sub(beat) > daemonHeartbeatDeadThreshold {
			event = "silent"
		}
	}
	if event == "" {
		return false
	}
	key := run.ProjectID + "\x00" + run.RecordID
	signature := strings.Join([]string{event, run.LeaseState, run.NextRetryAt, run.LeaseExpiresAt, strconv.Itoa(run.LeaseGeneration), strconv.Itoa(run.AttemptCount)}, "\x00")
	if d.heartbeatRunEvents == nil {
		d.heartbeatRunEvents = map[string]string{}
	}
	if d.heartbeatRunEvents[key] == signature {
		return false
	}
	d.heartbeatRunEvents[key] = signature
	return true
}

// projectEdited compares the project's stat signature with the one taken
// after its last poll. A project with no baseline gets one and is not due.
func (d *Daemon) projectEdited(project RegisteredProject) bool {
	signature := projectEditSignature(project)
	d.frontierMu.Lock()
	defer d.frontierMu.Unlock()
	if d.projectSignatures == nil {
		d.projectSignatures = map[string]string{}
	}
	previous, ok := d.projectSignatures[project.ProjectID]
	d.projectSignatures[project.ProjectID] = signature
	return ok && previous != signature
}

// recordProjectSignature takes the baseline after a poll, so the daemon's own
// writes during that poll do not read as outside edits.
func (d *Daemon) recordProjectSignature(projectID string) {
	project, err := projectByID(d.store, projectID)
	if err != nil {
		return
	}
	signature := projectEditSignature(project)
	d.frontierMu.Lock()
	defer d.frontierMu.Unlock()
	if d.projectSignatures == nil {
		d.projectSignatures = map[string]string{}
	}
	d.projectSignatures[projectID] = signature
}

// projectEditSignature is a stat-only fingerprint of a project's task, wave
// and spec files and its git refs: file count and newest modification time.
// It reads no file contents and starts no process.
func projectEditSignature(project RegisteredProject) string {
	count := 0
	var newest time.Time
	visit := func(root string) {
		_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			if !entry.IsDir() {
				count++
			}
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			return nil
		})
	}
	if project.VaultRoot != "" {
		for _, rel := range []string{"work/tasks", "work/waves", "specs"} {
			visit(filepath.Join(project.VaultRoot, rel))
		}
	}
	// ponytail: a linked worktree's .git file is not followed; its branch refs
	// change only through the CLI or a run, which already wake the daemon.
	if gitDir := filepath.Join(project.RepoRoot, ".git"); project.RepoRoot != "" && dirExists(gitDir) {
		for _, rel := range []string{"HEAD", "packed-refs", "refs"} {
			visit(filepath.Join(gitDir, rel))
		}
	}
	return strconv.Itoa(count) + "@" + strconv.FormatInt(newest.UnixNano(), 10)
}

// noteBatchGateWake records when the project's batch gate is next due. After
// a poll, a gate that is still due was held (its last run is still going), so
// it is checked again in an hour instead of every beat.
func (d *Daemon) noteBatchGateWake(projectID string, policy BatchGatePolicy, now time.Time, afterPoll bool) {
	var next time.Time
	if policy.Enabled && len(policy.Commands) > 0 {
		if len(policy.Windows) > 0 {
			if windows, err := parseMergeWindows(policy.Windows); err == nil {
				next = mergeWindowNext(windows, now)
			}
		} else {
			period := time.Duration(policy.PeriodHours) * time.Hour
			if period <= 0 {
				period = 24 * time.Hour
			}
			next = now
			if latest, err := d.store.latestBatchGateRun(projectID); err == nil && latest != nil {
				next = scheduledBatchGateRunTime(*latest, now, 2*period).Add(period)
			}
			if afterPoll && !next.After(now) {
				next = now.Add(time.Hour)
			}
		}
	}
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	if d.batchGateNext == nil {
		d.batchGateNext = map[string]time.Time{}
	}
	if next.IsZero() {
		delete(d.batchGateNext, projectID)
		return
	}
	d.batchGateNext[projectID] = next
}

func (d *Daemon) batchGateDue(projectID string, now time.Time) bool {
	d.departureMu.Lock()
	defer d.departureMu.Unlock()
	next, ok := d.batchGateNext[projectID]
	return ok && !next.After(now)
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
