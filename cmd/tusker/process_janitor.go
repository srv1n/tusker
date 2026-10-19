package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultAgentSilenceLimit is how long an agent session may show no sign of
// life before it is flagged silent. It is never stopped for it. Projects
// override it with the "Agent silence flag" setting.
const defaultAgentSilenceLimit = 20 * time.Minute

type psEntry struct {
	PGID   int
	Start  string
	Zombie bool
}

// processListing is one `ps` snapshot of every process: the janitor's only
// process probe per pass, however many rows it checks.
func processListing() (map[int]psEntry, error) {
	out, err := exec.Command("ps", "-axo", "pid=,pgid=,stat=,lstart=").Output()
	if err != nil {
		return nil, err
	}
	list := map[int]psEntry{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 8 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		pgid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		list[pid] = psEntry{PGID: pgid, Start: psStartTime(strings.Join(fields[3:], " ")), Zombie: strings.Contains(fields[2], "Z")}
	}
	return list, nil
}

type janitorSnapshot map[int]psEntry

func (l janitorSnapshot) alive(pid int) (psEntry, bool) {
	e, ok := l[pid]
	return e, ok && !e.Zombie
}

// identity reports whether the row's PID, process group and start time all
// still match a live process.
func (l janitorSnapshot) identity(p ownedProcess) bool {
	e, ok := l.alive(p.PID)
	return ok && p.ProcessStart != "" && e.PGID == p.PGID && e.Start == p.ProcessStart
}

func (l janitorSnapshot) groupHasMembers(pgid int) bool {
	for _, e := range l {
		if e.PGID == pgid && !e.Zombie {
			return true
		}
	}
	return false
}

func (l janitorSnapshot) starterAlive(starter string) bool {
	pid, start, ok := parseProcessStarter(starter)
	if !ok {
		return false
	}
	e, ok := l.alive(pid)
	return ok && (start == "" || e.Start == start)
}

// runProcessJanitor is the daemon's one reconciliation pass over the process
// inventory, at startup and on every poll. It compares open rows with live
// starters and owners:
//
//   - a row whose process is gone is closed;
//   - a row whose identity (PID, group, start time) does not match is reported
//     and closed, never stopped;
//   - a check whose starter is dead is stopped with its group;
//   - an agent session whose run no longer owns it is stopped; one whose run
//     still owns it is adopted, however long it has run, and flagged silent
//     past agentSilence without a sign of life.
//
// Every stop, report, adoption and silent flag leaves a process event.
func runProcessJanitor(store *RuntimeStore, agentSilence time.Duration, now time.Time) error {
	return runProcessJanitorFor(store, func(string) time.Duration { return agentSilence }, now)
}

// runProcessJanitorFor takes the agent silence limit per project, so each
// session is flagged against its own project's setting.
func runProcessJanitorFor(store *RuntimeStore, agentSilence func(projectID string) time.Duration, now time.Time) error {
	open, err := store.openOwnedProcesses()
	if err != nil {
		return err
	}
	if len(open) == 0 {
		return nil
	}
	raw, err := processListing()
	if err != nil {
		return err
	}
	list := janitorSnapshot(raw)
	self := currentProcessStarter()
	var errs []error
	for _, p := range open {
		if err := janitorRow(store, list, self, p, agentSilence, now); err != nil {
			errs = append(errs, fmt.Errorf("process %d (%s): %w", p.ID, p.Owner, err))
		}
	}
	return errors.Join(errs...)
}

func janitorRow(store *RuntimeStore, list janitorSnapshot, self string, p ownedProcess, agentSilence func(string) time.Duration, now time.Time) error {
	if !list.identity(p) {
		if _, leader := list.alive(p.PID); !leader && !list.groupHasMembers(p.PGID) {
			return store.endOwnedProcess(p.ID, "exited")
		}
		// Something runs at that PID or in that group, but it cannot be proven
		// to be ours. Report it once and leave it alone.
		if err := store.recordProcessEvent(p, "reported", "PID, process group and start time do not all match the inventory; not stopped", ruleIdentityMustMatch); err != nil {
			return err
		}
		return store.endOwnedProcess(p.ID, "identity_unconfirmed")
	}
	starterAlive := list.starterAlive(p.Starter)
	switch p.Kind {
	case processKindCheck:
		if starterAlive {
			return nil
		}
		stopProcessGroup(p.PGID)
		if err := store.endOwnedProcess(p.ID, "starter_stopped"); err != nil {
			return err
		}
		return store.recordProcessEvent(p, "stopped", "the process that started this check is gone", ruleCheckEndsWithStarter)
	case processKindAgentSession:
		projectID, lastEvent, live, err := agentSessionOwner(store, p.Owner)
		if err != nil {
			return err
		}
		if !live {
			if attemptFinishedRecently(store, p.Owner, now) {
				return nil
			}
			stopProcessGroup(p.PGID)
			if err := store.endOwnedProcess(p.ID, "owner_gone"); err != nil {
				return err
			}
			return store.recordProcessEvent(p, "stopped", "no live run owns this agent session any more", ruleAgentOwnerGone)
		}
		if !starterAlive && p.Starter != self {
			if _, err := store.exec(`UPDATE process_inventory SET starter = ? WHERE id = ?`, self, p.ID); err != nil {
				return err
			}
			if err := store.recordProcessEvent(p, "adopted", "its starter is gone but its run is live; adopted by "+self, ruleAgentAdopted); err != nil {
				return err
			}
		}
		limit := agentSilence(projectID)
		if limit <= 0 {
			limit = defaultAgentSilenceLimit
		}
		return flagSilentAgentSession(store, p, lastEvent, limit, now)
	}
	return nil
}

// agentSessionOwner reports whether a run still holds the attempt live, its
// project, and the run's last event time (its sign of life).
func agentSessionOwner(store *RuntimeStore, attemptID string) (string, string, bool, error) {
	var projectID, lastEvent string
	err := store.queryRowScan(`SELECT project_id, last_event_at FROM runs WHERE active_attempt_id = ? AND lease_state IN (?, ?) LIMIT 1`,
		[]any{attemptID, string(LeaseStateClaimed), string(LeaseStateRunning)}, &projectID, &lastEvent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return projectID, lastEvent, err == nil, err
}

// agentExitGrace lets a wrapper whose attempt just finished exit on its own:
// the run releases the attempt a moment before the wrapper writes its last
// status and exits, and stopping it then could cut off those writes.
const agentExitGrace = 2 * time.Minute

func attemptFinishedRecently(store *RuntimeStore, attemptID string, now time.Time) bool {
	var finished string
	if err := store.queryRowScan(`SELECT finished_at FROM attempts WHERE attempt_id = ?`, []any{attemptID}, &finished); err != nil {
		return false
	}
	at := latestTime(finished)
	return !at.IsZero() && now.Sub(at) < agentExitGrace
}

// flagSilentAgentSession emits one silent signal per silence: when the last
// sign of life is older than the limit and no signal was emitted since it.
func flagSilentAgentSession(store *RuntimeStore, p ownedProcess, lastEvent string, limit time.Duration, now time.Time) error {
	last := latestTime(p.StartedAt, p.LastSignOfLife, lastEvent)
	if last.IsZero() {
		return nil
	}
	if lastEvent != "" && last.After(latestTime(p.LastSignOfLife)) {
		if err := store.touchOwnedProcess(p.ID, last); err != nil {
			return err
		}
	}
	if now.Sub(last) <= limit {
		return nil
	}
	var flagged int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM process_events WHERE process_id = ? AND kind = 'silent' AND at >= ?`,
		[]any{p.ID, last.UTC().Format(time.RFC3339Nano)}, &flagged); err != nil {
		return err
	}
	if flagged > 0 {
		return nil
	}
	log.Printf("process janitor: agent session %s (pid %d) silent since %s", p.Owner, p.PID, last.UTC().Format(time.RFC3339))
	return store.recordProcessEvent(p, "silent", fmt.Sprintf("no sign of life for %s; left running", now.Sub(last).Round(time.Second)), ruleAgentSilence)
}

func latestTime(values ...string) time.Time {
	var latest time.Time
	for _, v := range values {
		if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(v)); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

// runProcessJanitor on the daemon logs instead of failing the poll: a broken
// janitor pass must never stop the daemon from working. It runs at most once
// per heartbeat tick: the heartbeat and the polls it starts share one pass.
// The first pass (startup, adoption of a predecessor's sessions) always runs.
func (d *Daemon) runProcessJanitor(now time.Time) {
	if d == nil || d.store == nil {
		return
	}
	last := d.janitorLastAt.Load()
	if since := now.Sub(time.Unix(0, last)); last != 0 && since >= 0 && since < reconcileLiveCadence/2 {
		return
	}
	d.janitorLastAt.Store(now.UnixNano())
	if err := runProcessJanitorFor(d.store, projectAgentSilenceLimits(d.store), now); err != nil {
		log.Printf("process janitor: %v", err)
	}
}

// projectAgentSilenceLimits reads each project's agent silence flag once per
// janitor pass.
func projectAgentSilenceLimits(store *RuntimeStore) func(string) time.Duration {
	cache := map[string]time.Duration{}
	return func(projectID string) time.Duration {
		if limit, ok := cache[projectID]; ok {
			return limit
		}
		limit := cachedAgentSilenceLimit(registeredProjectVault(store, projectID))
		cache[projectID] = limit
		return limit
	}
}

type agentSilenceEntry struct {
	stamp string
	limit time.Duration
}

// agentSilenceByVault keeps each project's agent silence flag across janitor
// passes; it is reread only when a config layer's mtime or size changes.
var agentSilenceByVault sync.Map

func cachedAgentSilenceLimit(vault string) time.Duration {
	if strings.TrimSpace(vault) == "" {
		_, limit := processSilenceLimits(vault)
		return limit
	}
	stamp := tuskerConfigStamp(vault)
	if cached, ok := agentSilenceByVault.Load(vault); ok && cached.(agentSilenceEntry).stamp == stamp {
		return cached.(agentSilenceEntry).limit
	}
	_, limit := processSilenceLimits(vault)
	agentSilenceByVault.Store(vault, agentSilenceEntry{stamp: stamp, limit: limit})
	return limit
}

// tuskerConfigStamp fingerprints the config layers processSilenceLimits
// reads by path, mtime and size, without reading them.
func tuskerConfigStamp(vault string) string {
	var b strings.Builder
	for _, path := range []string{userGlobalTuskerConfigPath(), managedTuskerConfigPath(vault), managedTuskerLocalConfigPath(vault)} {
		b.WriteString(path)
		if info, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "@%d:%d", info.ModTime().UnixNano(), info.Size())
		}
		b.WriteString(";")
	}
	return b.String()
}

// registeredProjectVault returns a registered project's vault root, or "" when
// the project is unknown.
func registeredProjectVault(store *RuntimeStore, projectID string) string {
	if store == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, ProjectID: projectID})
	if err != nil || len(projects) == 0 {
		return ""
	}
	return projects[0].Project.VaultRoot
}
