package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// mergeWindowRunningGrace bounds how long a still-running batch gate suppresses
// a new clock-window spawn. It mirrors the period path's stuck-run guard: a run
// in flight blocks a concurrent gate, but only up to this cap so a permanently
// stuck run cannot wedge the window schedule forever.
const mergeWindowRunningGrace = 24 * time.Hour

// mergeWindow is a daily wall-clock departure time in the daemon host's local
// time zone.
type mergeWindow struct {
	hour   int
	minute int
}

// parseMergeWindows converts configured "HH:MM" entries into an ordered
// (ascending, de-duplicated) set of daily local wall-clock times. Malformed
// entries such as "25:00" or "1300" are rejected with a named config error
// rather than silently dropped.
func parseMergeWindows(raw []string) ([]mergeWindow, error) {
	windows := make([]mergeWindow, 0, len(raw))
	seen := map[int]struct{}{}
	for _, entry := range raw {
		trimmed := strings.TrimSpace(entry)
		parts := strings.Split(trimmed, ":")
		if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
			return nil, tuskerError(errorConfigInvalid, "orchestration.batch_gate.windows entry "+strconv.Quote(entry)+" must be a HH:MM local wall-clock time")
		}
		hour, hourErr := strconv.Atoi(parts[0])
		minute, minErr := strconv.Atoi(parts[1])
		if hourErr != nil || minErr != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return nil, tuskerError(errorConfigInvalid, "orchestration.batch_gate.windows entry "+strconv.Quote(entry)+" must be a HH:MM local wall-clock time")
		}
		key := hour*60 + minute
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		windows = append(windows, mergeWindow{hour: hour, minute: minute})
	}
	sort.Slice(windows, func(i, j int) bool {
		return windows[i].hour*60+windows[i].minute < windows[j].hour*60+windows[j].minute
	})
	return windows, nil
}

// mergeWindowMostRecent returns the latest window occurrence at or before now
// (in now's location). Windows must be non-empty, so a result always exists.
func mergeWindowMostRecent(windows []mergeWindow, now time.Time) time.Time {
	loc := now.Location()
	y, m, d := now.Date()
	var best time.Time
	found := false
	for _, off := range []int{0, -1} {
		for _, w := range windows {
			t := time.Date(y, m, d+off, w.hour, w.minute, 0, 0, loc)
			if t.After(now) {
				continue
			}
			if !found || t.After(best) {
				best, found = t, true
			}
		}
	}
	return best
}

// mergeWindowNext returns the earliest window occurrence strictly after now (in
// now's location). An exact boundary hit resolves to the following window, so
// the result is deterministic.
func mergeWindowNext(windows []mergeWindow, now time.Time) time.Time {
	loc := now.Location()
	y, m, d := now.Date()
	var best time.Time
	found := false
	for _, off := range []int{0, 1} {
		for _, w := range windows {
			t := time.Date(y, m, d+off, w.hour, w.minute, 0, 0, loc)
			if !t.After(now) {
				continue
			}
			if !found || t.Before(best) {
				best, found = t, true
			}
		}
	}
	return best
}

func (s *RuntimeStore) latestBatchGateRun(projectID string) (*BatchGateRun, error) {
	var run BatchGateRun
	err := s.queryRowScan(`SELECT id, project_id, tree_hash, profile, commands_json, status, started_at, finished_at, first_failure
		FROM batch_gate_runs WHERE project_id = ? ORDER BY started_at DESC LIMIT 1`, []any{projectID},
		&run.ID, &run.ProjectID, &run.TreeHash, &run.Profile, &run.CommandsJSON, &run.Status, &run.StartedAt, &run.FinishedAt, &run.FirstFailure)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func (s *RuntimeStore) saveBatchGateRun(run BatchGateRun) error {
	_, err := s.exec(`INSERT INTO batch_gate_runs (id, project_id, tree_hash, profile, commands_json, status, started_at, finished_at, first_failure)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET tree_hash=excluded.tree_hash, status=excluded.status, finished_at=excluded.finished_at, first_failure=excluded.first_failure`,
		run.ID, run.ProjectID, run.TreeHash, run.Profile, run.CommandsJSON, run.Status, run.StartedAt, run.FinishedAt, run.FirstFailure)
	return err
}

func (d *Daemon) scheduleBatchGateIfDue(project RegisteredProject, wf Workflow, now time.Time) error {
	policy := wf.Orchestration.BatchGate
	if !policy.Enabled || len(policy.Commands) == 0 {
		return nil
	}
	latest, err := d.store.latestBatchGateRun(project.ProjectID)
	if err != nil {
		return err
	}
	if len(policy.Windows) > 0 {
		windows, parseErr := parseMergeWindows(policy.Windows)
		if parseErr != nil {
			return parseErr
		}
		windowStart := mergeWindowMostRecent(windows, now)
		if latest != nil {
			if started, tErr := time.Parse(time.RFC3339, latest.StartedAt); tErr == nil {
				// A run started at or after the current window already consumed
				// it, so each window fires at most once per day and is a no-op
				// between windows.
				if !started.Before(windowStart) {
					return nil
				}
				// A run still in flight from before this window must not be
				// joined by a second concurrent gate. Mirror the period path's
				// stuck-run guard: suppress a new spawn while a recent run is
				// running, but bound it so a permanently stuck run cannot wedge
				// the schedule forever.
				if latest.Status == "running" && now.Sub(started) < mergeWindowRunningGrace {
					return nil
				}
			}
		}
	} else {
		period := time.Duration(policy.PeriodHours) * time.Hour
		if period <= 0 {
			period = 24 * time.Hour
		}
		if latest != nil {
			started, parseErr := time.Parse(time.RFC3339, latest.StartedAt)
			if parseErr == nil && now.Sub(started) < period {
				return nil
			}
			if latest.Status == "running" && parseErr == nil && now.Sub(started) < 2*period {
				return nil
			}
		}
	}
	treeHash, err := workspaceTreeStateHash(project.RepoRoot)
	if err != nil {
		return err
	}
	commandsJSON, _ := json.Marshal(policy.Commands)
	run := BatchGateRun{ID: "batch-" + strings.ToLower(newRecordID()), ProjectID: project.ProjectID, TreeHash: treeHash, Profile: policy.FeatureProfile, CommandsJSON: string(commandsJSON), Status: "running", StartedAt: now.UTC().Format(time.RFC3339)}
	if err := d.store.saveBatchGateRun(run); err != nil {
		return err
	}
	go d.executeBatchGate(project, policy, run)
	return nil
}

func (d *Daemon) executeBatchGate(project RegisteredProject, policy BatchGatePolicy, run BatchGateRun) {
	maxRepairs := policy.MaxRepairs
	if maxRepairs <= 0 {
		maxRepairs = 3
	}
	failures := 0
	firstFailCommand := ""
	for _, command := range policy.Commands {
		started := time.Now()
		output, err := runGateCommand(project.RepoRoot, command)
		if err == nil {
			treeHash, hashErr := workspaceTreeStateHash(project.RepoRoot)
			if hashErr == nil {
				_ = d.store.RecordGateLedger(GateLedgerEntry{ID: "gate-" + strings.ToLower(newRecordID()), ProjectID: project.ProjectID, TreeHash: treeHash, Command: command, Profile: policy.FeatureProfile, Host: runtimeLeaseHost(), DurationMS: time.Since(started).Milliseconds(), PassedAt: time.Now().UTC().Format(time.RFC3339)})
			}
			continue
		}
		failures++
		excerpt := actionableGateFailure(output, err)
		if run.FirstFailure == "" {
			run.FirstFailure = excerpt
		}
		if firstFailCommand == "" {
			firstFailCommand = command
		}
		if failures <= maxRepairs {
			_ = createBatchGateRepairTask(project.VaultRoot, run.ID, command, excerpt, policy.FeatureProfile)
		}
	}
	run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if failures == 0 {
		run.Status = "passed"
		// A green shared build-and-test means the commands it re-ran are healthy
		// again: release the dependency-scoped holds whose recorded command this
		// run actually re-ran green, leaving unrelated red markers in place.
		_ = clearBuildFailedMarkers(project.VaultRoot, policy.Commands, policy.FeatureProfile)
	} else {
		run.Status = "failed"
		// The failing piece (repair task or command-tagged task) may have no
		// dependents of its own, so stamp the red command onto every wave with
		// in-flight members. Their not-yet-landed members then hold via
		// v7HeldByFailedUpstream's own-wave reach, so a genuine shared failure
		// actually quarantines the concurrent work it endangers.
		_ = stampFailedCommandOnActiveWaves(project.VaultRoot, firstFailCommand, policy.FeatureProfile)
	}
	_ = d.store.saveBatchGateRun(run)
	_ = refreshStreamBoardForVault(project.VaultRoot)
}

// clearBuildFailedMarkers drops red shared-build markers whose recorded command
// (and profile) the just-passed run actually re-ran green. It is called when a
// shared build-and-test goes green, releasing only the dependents whose failing
// command is now healthy again — an unrelated red marker (a different command or
// feature profile) is left in place. A marker with no recorded command is a
// legacy marker and is cleared unconditionally for backward compatibility.
//
// The clear loop continues through every task and wave even when an individual
// mutate fails, collecting errors and returning them joined, so one bad
// document cannot leave the release half-applied.
func clearBuildFailedMarkers(vaultPath string, greenCommands []string, profile string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	green := make(map[string]struct{}, len(greenCommands))
	for _, c := range greenCommands {
		green[strings.TrimSpace(c)] = struct{}{}
	}
	reRanGreen := func(data map[string]any) bool {
		cmd := strings.TrimSpace(stringField(data, buildFailedCommandField))
		if cmd == "" {
			return true // legacy marker: no command recorded, clear it
		}
		if _, ok := green[cmd]; !ok {
			return false
		}
		markerProfile := strings.TrimSpace(stringField(data, buildFailedProfileField))
		if markerProfile != "" && profile != "" && markerProfile != profile {
			return false
		}
		return true
	}
	clearFields := func(data map[string]any) {
		delete(data, buildFailedField)
		delete(data, buildFailedCommandField)
		delete(data, buildFailedProfileField)
		data["updated_by"] = "tusker:batch-gate"
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	var errs []error
	for _, task := range sortedV7Tasks(idx) {
		if !v7BuildFailed(task) || task.AbsolutePath == "" || !reRanGreen(task.Data) {
			continue
		}
		_, _, mutErr := mutateV7DocumentLocked(task.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			if !boolField(data, buildFailedField) || !reRanGreen(data) {
				return data, body, false, nil
			}
			clearFields(data)
			return data, body, true, nil
		})
		if mutErr != nil {
			errs = append(errs, mutErr)
		}
	}
	for _, wave := range idx.Waves {
		if wave.AbsolutePath == "" || strings.TrimSpace(stringField(wave.Data, buildFailedCommandField)) == "" || !reRanGreen(wave.Data) {
			continue
		}
		_, _, mutErr := mutateV7DocumentLocked(wave.AbsolutePath, v7FrontmatterOrder["wave"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			if strings.TrimSpace(stringField(data, buildFailedCommandField)) == "" || !reRanGreen(data) {
				return data, body, false, nil
			}
			clearFields(data)
			return data, body, true, nil
		})
		if mutErr != nil {
			errs = append(errs, mutErr)
		}
	}
	return errors.Join(errs...)
}

// stampFailedCommandOnActiveWaves records a red gate command onto every wave
// that still has an in-flight (non-terminal) member. Those members' not-yet-
// landed dependents then hold through v7HeldByFailedUpstream's own-wave reach.
// Waves whose members have all landed carry nothing new, so the hold stays
// scoped to work that is actually still in flight rather than freezing the
// whole project.
func stampFailedCommandOnActiveWaves(vaultPath, command, profile string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	var errs []error
	for _, wave := range idx.Waves {
		if wave.AbsolutePath == "" || !v7WaveHasInFlightMember(wave, idx) {
			continue
		}
		_, _, mutErr := mutateV7DocumentLocked(wave.AbsolutePath, v7FrontmatterOrder["wave"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
			data[buildFailedCommandField] = command
			if profile != "" {
				data[buildFailedProfileField] = profile
			}
			data["updated_by"] = "tusker:batch-gate"
			data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
			return data, body, true, nil
		})
		if mutErr != nil {
			errs = append(errs, mutErr)
		}
	}
	return errors.Join(errs...)
}

// v7WaveHasInFlightMember reports whether a wave still has at least one member
// that has not reached a terminal status, i.e. work the red command can still
// endanger.
func v7WaveHasInFlightMember(wave Note, idx v7Index) bool {
	for _, id := range normalizeList(wave.Data["members"]) {
		member, ok := idx.Tasks[id]
		if !ok {
			continue
		}
		if !v7TerminalTaskStatus(stringField(member.Data, "status")) {
			return true
		}
	}
	return false
}

func actionableGateFailure(output string, runErr error) string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	kept := make([]string, 0, 12)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") || strings.Contains(lower, "panic") || strings.Contains(lower, "undefined") {
			kept = append(kept, safePacketText(line, 320))
			if len(kept) == 12 {
				break
			}
		}
	}
	if len(kept) == 0 && runErr != nil {
		kept = append(kept, runErr.Error())
	}
	return strings.Join(kept, "\n")
}

func createBatchGateRepairTask(vaultPath, gateRunID, command, excerpt, profile string) error {
	identity := promotionFailureIdentity(PromotionFailurePacket{GateCommand: command, GateProfile: profile, Defects: []GateDefect{{Command: command, Target: command}}})
	return createPromotionFailureRepairTask(vaultPath, gateRunID, command, excerpt, profile, identity, "", "", false)
}

// createPromotionFailureRepairTask coalesces repeated red gates by the stable
// defect identity rather than command alone. A command can contain several
// unrelated tests; merging their repairs would erase ownership and evidence.
func createPromotionFailureRepairTask(vaultPath, gateRunID, command, excerpt, profile, identity, owningTask, artifactRef string, held bool) error {
	if idx, err := loadV7Index(vaultPath); err == nil {
		for _, existing := range idx.Tasks {
			status := stringField(existing.Data, "status")
			if stringField(existing.Data, "promotion_failure_identity") != identity || status == "done" || status == "cancelled" || status == "superseded" {
				continue
			}
			_, _, updateErr := mutateV7DocumentLocked(existing.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, body string) (map[string]any, string, bool, error) {
				data["batch_gate_run"] = gateRunID
				data[buildFailedField] = true
				data[buildFailedCommandField] = command
				data["promotion_failure_identity"] = identity
				if owningTask != "" {
					data["promotion_failure_owner"] = owningTask
				}
				if artifactRef != "" {
					data["promotion_failure_artifact"] = artifactRef
				}
				if profile != "" {
					data[buildFailedProfileField] = profile
				}
				data["updated_by"] = "tusker:batch-gate"
				data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
				intent := "Repair unattended batch gate `" + gateRunID + "`.\n\nFirst actionable failure:\n\n" + excerpt
				return data, replaceSection(body, "## Intent", intent), true, nil
			})
			return updateErr
		}
	}
	if _, err := resolveNote(vaultPath, "BGR"); err != nil {
		if err := newV7Epic(Args{"vault": vaultPath, "quiet": "true", "acronym": "BGR", "title": "Batch gate repairs", "summary": "Automatically generated repair work from unattended batch gates."}); err != nil && !strings.Contains(err.Error(), "already") {
			return err
		}
	}
	id := nextSafeV7TaskID(vaultPath, "BGR")
	if err := newV7Task(Args{"vault": vaultPath, "quiet": "true", "epic": "BGR", "id": id, "title": "Repair red batch gate " + gateRunID, "risk": "medium", "priority": "p1", "status": "backlog", "by": "tusker:batch-gate"}); err != nil {
		return err
	}
	note, err := resolveNote(vaultPath, id)
	if err != nil {
		return err
	}
	body := strings.Join([]string{
		"## Intent", "", "Repair unattended batch gate `" + gateRunID + "`.", "", "First actionable failure:", "", excerpt,
		"", "## Acceptance", "", "| ID | Outcome | Proof |", "|---|---|---|", "| A1 | The failing batch command passes on the repaired tree. | focused_test |",
		"", "## Verification", "", "| Covers | Check | Result | Notes |", "|---|---|---|---|", "| A1 | command: " + strings.ReplaceAll(command, "|", "\\|") + " | pending | spawned from " + gateRunID + " |", "",
	}, "\n")
	_, _, err = mutateV7DocumentLocked(note.AbsolutePath, v7FrontmatterOrder["task"], func(data map[string]any, _ string) (map[string]any, string, bool, error) {
		data["next_action"] = "Fix the first actionable batch failure and rerun the failed command."
		data["batch_gate_command"] = command
		data["batch_gate_run"] = gateRunID
		data["promotion_failure_identity"] = identity
		if owningTask != "" {
			data["promotion_failure_owner"] = owningTask
		}
		if artifactRef != "" {
			data["promotion_failure_artifact"] = artifactRef
		}
		data[buildFailedField] = true
		data[buildFailedCommandField] = command
		if profile != "" {
			data[buildFailedProfileField] = profile
		}
		data["updated_by"] = "tusker:batch-gate"
		data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		return data, body, true, nil
	})
	if err != nil {
		return err
	}
	if held {
		return nil
	}
	return statusCmd(Args{"vault": vaultPath, "id": id, "status": "ready", "actor": "tusker:batch-gate", "reason": "unattended batch gate red"})
}

func promotionFailureRepairTaskID(vaultPath, identity string) string {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return ""
	}
	for id, task := range idx.Tasks {
		if stringField(task.Data, "promotion_failure_identity") == identity && !v7TerminalTaskStatus(stringField(task.Data, "status")) {
			return id
		}
	}
	return ""
}
