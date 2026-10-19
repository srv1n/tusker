package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
		// The armed-wave check reads the in-memory frontier, which is empty
		// after a restart; the rescan below rebuilds it.
		if !live[id] && vaultHasArmedWave(project.Project.VaultRoot) {
			live[id] = true
		}
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

// vaultHasArmedWave reads only the wave files: an armed wave that is not yet
// landed or closed. Member status is left to the rescan it triggers.
func vaultHasArmedWave(vaultRoot string) bool {
	paths, _ := filepath.Glob(filepath.Join(vaultRoot, "work", "waves", "*.md"))
	if len(paths) == 0 {
		return false
	}
	waves, err := loadOperationalNotesByPath(vaultRoot, paths)
	if err != nil {
		return false
	}
	for _, wave := range waves {
		if stringField(wave.Data, "authorization") == "armed" && !v7WaveHasDurableTerminal(wave) {
			return true
		}
	}
	return false
}

// heartbeat runs one beat and polls each due project once.
func (d *Daemon) heartbeat(ctx context.Context, now time.Time) error {
	d.runProcessJanitor(now)
	due, err := d.heartbeatProjectsDue(now)
	if err != nil {
		return err
	}
	if _, global := due["*"]; global {
		// Freed capacity with several projects waiting: one registry poll lets
		// the fair scheduler pick, instead of the first project taking it all.
		err := d.runPoll(ctx, "")
		for projectID := range due {
			if projectID != "*" {
				d.recordProjectSignature(projectID)
			}
		}
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
	waitingProjects := 0
	for _, project := range projects {
		if !project.Enabled {
			continue
		}
		id := project.ProjectID
		enabled[id] = true
		if d.batchGateDue(id, now) {
			due[id] = false
		}
		// Run events are checked whether or not the project counts as active: a
		// claimed run whose lease expired with no process left does not make its
		// project active, yet only a poll of that project reclaims it.
		var wf *Workflow
		for _, run := range byProject[id] {
			if run.Terminal {
				continue
			}
			seenRuns[run.ProjectID+"\x00"+run.RecordID] = true
			if wf == nil && run.ProcessPID > 0 {
				loaded := heartbeatWorkflow(project.VaultRoot)
				wf = &loaded
			}
			if d.runEventDue(run, wf, now) {
				due[id] = false
			}
		}
		active := d.projectActive(id, byProject[id], now)
		d.setProjectTier(id, active)
		if !active {
			continue
		}
		waiting := false
		for _, run := range byProject[id] {
			if runWaitsForAdmission(run) || (!run.Terminal && LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateUnclaimed && strings.TrimSpace(run.LastError) == "") {
				waiting = true
			}
		}
		if waiting {
			if capacityFreed {
				waitingProjects++
			}
			if capacityFreed || now.Sub(d.lastProjectPoll(id)) >= d.nextPollInterval() {
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
	if waitingProjects > 1 {
		due["*"] = false
	}
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
// lease that came due, or a runner whose process is gone or that stalled by
// the same rule the poll applies (runStallReason, with the project's stall
// timeout and first-event deadline). wf may be nil for a run with no process.
// It fires once per distinct run state, so a poll that leaves the run
// unchanged is not repeated every beat; a run that is not stalled yet is not
// recorded, so the event fires when it does stall.
func (d *Daemon) runEventDue(run RunStatus, wf *Workflow, now time.Time) bool {
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
		if expires, ok := parseRunTimestamp(run.LeaseExpiresAt); ok && !expires.After(now) {
			event = "lease_expired"
		} else if (run.ProcessPGID > 0 && !processGroupExists(run.ProcessPGID)) || (run.ProcessPGID <= 0 && run.ProcessPID > 0 && !processExists(run.ProcessPID)) {
			event = "process_gone"
		} else if run.ProcessPID > 0 && wf != nil {
			// The poll checks stalls only for a run whose process is alive.
			if stalled, reason := runStallReason(run, *wf, now); stalled {
				event = "stalled: " + reason
			}
		}
	}
	if event == "" {
		return false
	}
	key := run.ProjectID + "\x00" + run.RecordID
	signature := strings.Join([]string{event, run.LeaseState, run.NextRetryAt, run.LeaseExpiresAt, strconv.Itoa(run.LeaseGeneration), strconv.Itoa(run.AttemptCount), run.FirstEventAt, run.LastHeartbeatAt}, "\x00")
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

// projectEditSignature is a fingerprint of a project's task, wave and spec
// files (file count and newest modification time) plus the HEAD and
// integration branch refs. It starts no process.
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
	signature := strconv.Itoa(count) + "@" + strconv.FormatInt(newest.UnixNano(), 10)
	// ponytail: a linked worktree's .git file is not followed; its branch refs
	// change only through the CLI or a run, which already wake the daemon.
	if gitDir := filepath.Join(project.RepoRoot, ".git"); project.RepoRoot != "" && dirExists(gitDir) {
		signature += "|" + gitIntegrationRefs(gitDir, heartbeatWorkflow(project.VaultRoot).Orchestration.DefaultBranch)
	}
	return signature
}

// gitIntegrationRefs returns HEAD and the integration branch's commit, read
// from its loose ref or its packed-refs entry. Agent worktrees share refs/,
// so watching the whole directory read every agent commit as a hand edit.
// Without a configured branch, HEAD's branch, main and master stand in.
func gitIntegrationRefs(gitDir, configured string) string {
	head, _ := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	headBranch, symbolic := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
	if !symbolic {
		headBranch = "" // detached
	}
	branches := []string{strings.TrimSpace(configured), headBranch, "main", "master"}
	packed, _ := os.ReadFile(filepath.Join(gitDir, "packed-refs"))
	out := []string{strings.TrimSpace(string(head))}
	seen := map[string]bool{}
	for _, branch := range branches {
		if branch == "" || seen[branch] {
			continue
		}
		seen[branch] = true
		ref := "refs/heads/" + branch
		value := ""
		if loose, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
			value = strings.TrimSpace(string(loose))
		} else {
			for _, line := range strings.Split(string(packed), "\n") {
				if strings.HasSuffix(line, " "+ref) {
					value = line
					break
				}
			}
		}
		out = append(out, branch+"="+value)
	}
	return strings.Join(out, ",")
}

// heartbeatWorkflows caches each project's workflow by WORKFLOW.md's stat, so
// the heartbeat reads the file only after it changes.
var heartbeatWorkflows = struct {
	sync.Mutex
	entries map[string]heartbeatWorkflowEntry
}{entries: map[string]heartbeatWorkflowEntry{}}

type heartbeatWorkflowEntry struct {
	modTime time.Time
	size    int64
	wf      Workflow
}

func heartbeatWorkflow(vaultRoot string) Workflow {
	path := workflowPath(vaultRoot)
	info, err := os.Stat(path)
	if err != nil {
		return defaultWorkflow()
	}
	heartbeatWorkflows.Lock()
	defer heartbeatWorkflows.Unlock()
	if entry, ok := heartbeatWorkflows.entries[path]; ok && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
		return entry.wf
	}
	wf := defaultWorkflow()
	if file, err := loadWorkflow(vaultRoot); err == nil {
		wf = file.Data
	}
	heartbeatWorkflows.entries[path] = heartbeatWorkflowEntry{modTime: info.ModTime(), size: info.Size(), wf: wf}
	return wf
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
