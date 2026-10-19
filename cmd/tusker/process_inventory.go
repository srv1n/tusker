package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process inventory: every process Tusker starts gets one row naming its
// owner and identity (PID, process group, kernel start time) so it can be
// stopped as a group, and only when that identity still matches.
const (
	processKindAgentSession = "agent_session"
	processKindCheck        = "check"

	defaultCheckSilenceLimit = 10 * time.Minute
)

// errCheckStuckStopped marks a check stopped for silence. It must never read
// as an ordinary failed check: a hung test is not a red test.
var errCheckStuckStopped = errors.New("check stuck, stopped")

type ownedProcess struct {
	ID             int64
	Owner          string
	Kind           string
	PID            int
	PGID           int
	ProcessStart   string
	Starter        string
	StartedAt      string
	LastSignOfLife string
	EndedAt        string
	EndReason      string
}

var currentStarterOnce = sync.OnceValue(func() string { return processStarterFor(os.Getpid()) })

// currentProcessStarter names this daemon or CLI process as a starter.
func currentProcessStarter() string { return currentStarterOnce() }

// processStarterFor identifies a starter by PID and kernel start time so a
// reused PID never inherits another starter's checks.
func processStarterFor(pid int) string {
	start, _ := processStartTime(pid)
	return fmt.Sprintf("pid:%d@%s", pid, start)
}

func (s *RuntimeStore) recordOwnedProcess(owner, kind string, pid, pgid int, processStart, starter string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.exec(`INSERT INTO process_inventory (owner, kind, pid, pgid, process_start, starter, started_at, last_sign_of_life)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, owner, kind, pid, pgid, processStart, starter, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *RuntimeStore) touchOwnedProcess(id int64, at time.Time) error {
	_, err := s.exec(`UPDATE process_inventory SET last_sign_of_life = ? WHERE id = ? AND ended_at = ''`, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

// endOwnedProcess records the first end reason only; a starter stop that
// already ended the row is not overwritten by the check's own exit.
func (s *RuntimeStore) endOwnedProcess(id int64, reason string) error {
	_, err := s.exec(`UPDATE process_inventory SET ended_at = ?, end_reason = ? WHERE id = ? AND ended_at = ''`,
		time.Now().UTC().Format(time.RFC3339Nano), reason, id)
	return err
}

// ownedProcesses returns every row ever recorded, for history views.
func (s *RuntimeStore) ownedProcesses() ([]ownedProcess, error) {
	return s.queryOwnedProcesses(``)
}

// openOwnedProcesses returns only rows whose process has not ended, so the
// janitor and starter cleanup never rescan history.
func (s *RuntimeStore) openOwnedProcesses() ([]ownedProcess, error) {
	return s.queryOwnedProcesses(`WHERE ended_at = ''`)
}

func (s *RuntimeStore) queryOwnedProcesses(where string) ([]ownedProcess, error) {
	rows, err := s.query(`SELECT id, owner, kind, pid, pgid, process_start, starter, started_at, last_sign_of_life, ended_at, end_reason
		FROM process_inventory ` + where + ` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ownedProcess
	for rows.Next() {
		var p ownedProcess
		if err := rows.Scan(&p.ID, &p.Owner, &p.Kind, &p.PID, &p.PGID, &p.ProcessStart, &p.Starter, &p.StartedAt, &p.LastSignOfLife, &p.EndedAt, &p.EndReason); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// stopOwnedChecksByStarter stops every still-running check started by the
// given starter (watchdog and shutdown path). A row whose PID, process group
// and start time no longer all match is left alone and not counted.
func stopOwnedChecksByStarter(store *RuntimeStore, starter string) (int, error) {
	all, err := store.openOwnedProcesses()
	if err != nil {
		return 0, err
	}
	stopped := 0
	for _, p := range all {
		if p.Kind != processKindCheck || p.Starter != starter || p.EndedAt != "" {
			continue
		}
		if !processIdentityMatches(RunStatus{ProcessPID: p.PID, ProcessPGID: p.PGID, ProcessStartedAt: p.ProcessStart}) {
			continue
		}
		stopProcessGroup(p.PGID)
		if err := store.endOwnedProcess(p.ID, "starter_stopped"); err != nil {
			return stopped, err
		}
		_ = store.recordProcessEvent(p, "stopped", "starter is exiting", ruleCheckEndsWithStarter)
		stopped++
	}
	return stopped, nil
}

// Rules named on durable process events.
const (
	ruleCheckEndsWithStarter = "check_ends_with_starter"
	ruleAgentOwnerGone       = "agent_session_owner_gone"
	ruleAgentAdopted         = "agent_session_survives_restart"
	ruleAgentSilence         = "agent_silence_limit"
	ruleIdentityMustMatch    = "identity_must_match"
	ruleOperator             = "operator"
)

type processEvent struct {
	ProcessID int64
	Owner     string
	PID       int
	PGID      int
	Kind      string // stopped, reported, adopted, silent
	Reason    string
	Rule      string
	At        string
}

// recordProcessEvent leaves the durable trace every automatic action owes the
// operator: which process, its owner, what happened, why and which rule fired.
func (s *RuntimeStore) recordProcessEvent(p ownedProcess, kind, reason, rule string) error {
	_, err := s.exec(`INSERT INTO process_events (process_id, owner, pid, pgid, kind, reason, rule, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Owner, p.PID, p.PGID, kind, reason, rule, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *RuntimeStore) processEvents() ([]processEvent, error) {
	rows, err := s.query(`SELECT process_id, owner, pid, pgid, kind, reason, rule, at FROM process_events ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []processEvent
	for rows.Next() {
		var e processEvent
		if err := rows.Scan(&e.ProcessID, &e.Owner, &e.PID, &e.PGID, &e.Kind, &e.Reason, &e.Rule, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// starterAlive reports whether a "pid:<pid>@<start>" starter still runs. A
// starter recorded without a start time counts as alive while its PID exists.
func starterAlive(starter string) bool {
	pid, start, ok := parseProcessStarter(starter)
	if !ok || !processExists(pid) {
		return false
	}
	if start == "" {
		return true
	}
	actual, ok := processStartTime(pid)
	return !ok || actual == start
}

func parseProcessStarter(starter string) (int, string, bool) {
	rest, ok := strings.CutPrefix(starter, "pid:")
	if !ok {
		return 0, "", false
	}
	pidText, start, _ := strings.Cut(rest, "@")
	pid, err := strconv.Atoi(pidText)
	return pid, start, err == nil && pid > 0
}

// prepareLandingGate takes the one global landing-gate slot and stops any
// copy of the same gate a dead starter left running, so at most one copy of a
// gate runs and two projects never run full suites at once. The slot is a
// flock, so a crashed holder releases it with its process.
// ponytail: the slot is per gate command, not per whole gate, and has a fixed
// size of one; make it a counted slot when the app setting exists.
func prepareLandingGate(ctx context.Context, owner string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root := DefaultStateRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	slot, err := os.OpenFile(filepath.Join(root, "landing-gate.slot"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for syscall.Flock(int(slot.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		select {
		case <-ctx.Done():
			_ = slot.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	release := func() { _ = syscall.Flock(int(slot.Fd()), syscall.LOCK_UN); _ = slot.Close() }
	store, err := OpenRuntimeStore(root)
	if err != nil {
		release()
		return nil, fmt.Errorf("gate refused: process inventory unavailable: %w", err)
	}
	defer store.Close()
	rows, err := store.openOwnedProcesses()
	if err != nil {
		release()
		return nil, err
	}
	for _, p := range rows {
		if p.Owner != owner || p.Kind != processKindCheck || p.EndedAt != "" || starterAlive(p.Starter) {
			continue
		}
		if !processIdentityMatches(RunStatus{ProcessPID: p.PID, ProcessPGID: p.PGID, ProcessStartedAt: p.ProcessStart}) {
			continue // the janitor reports what it cannot confirm
		}
		stopProcessGroup(p.PGID)
		_ = store.endOwnedProcess(p.ID, "starter_stopped")
		_ = store.recordProcessEvent(p, "stopped", "a dead starter's copy of this gate was still running", ruleCheckEndsWithStarter)
	}
	return release, nil
}

// stopProcessGroup mirrors the verification runner: TERM the group, give it
// 500ms, then KILL whatever is left.
func stopProcessGroup(pgid int) {
	if pgid <= 0 {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(-pgid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

type silenceWatchedOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	last time.Time
}

// processGroupCPUSeconds sums the CPU time of every live member of a group
// from one process listing. Members that exit take their time with them, so
// callers compare samples for an increase, not a total.
func processGroupCPUSeconds(pgid int) (float64, bool) {
	out, err := exec.Command("ps", "-axo", "pgid=,time=").Output()
	if err != nil {
		return 0, false
	}
	total, found := 0.0, false
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != strconv.Itoa(pgid) {
			continue
		}
		if seconds, ok := parsePSCPUTime(fields[1]); ok {
			total, found = total+seconds, true
		}
	}
	return total, found
}

// parsePSCPUTime reads ps time: "[DD-][HH:]MM:SS[.ss]" (darwin keeps
// hundredths, Linux whole seconds).
func parsePSCPUTime(text string) (float64, bool) {
	days := 0.0
	if d, rest, ok := strings.Cut(text, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, text = float64(n), rest
	}
	total := 0.0
	for _, part := range strings.Split(text, ":") {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, false
		}
		total = total*60 + n
	}
	return days*86400 + total, true
}

func (o *silenceWatchedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(p) > 0 {
		o.last = time.Now()
	}
	return o.buf.Write(p)
}

func (o *silenceWatchedOutput) snapshot() ([]byte, time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]byte(nil), o.buf.Bytes()...), o.last
}

type checkSilenceLimitKey struct{}

// withCheckSilenceLimit carries a project's check silence limit to the
// runOwnedCheck calls below a caller that passes no explicit limit.
func withCheckSilenceLimit(ctx context.Context, limit time.Duration) context.Context {
	return context.WithValue(ctx, checkSilenceLimitKey{}, limit)
}

// runOwnedCheck is the one way Tusker runs a check: own process group,
// inventory row before it can do work unrecorded, whole group stopped on
// finish, ctx cancel, or no new output for the silence limit. A silence stop
// returns an error wrapping errCheckStuckStopped. A zero silence takes the
// limit from ctx (withCheckSilenceLimit), else the default.
func runOwnedCheck(ctx context.Context, owner string, cmd *exec.Cmd, silence time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if silence <= 0 {
		silence, _ = ctx.Value(checkSilenceLimitKey{}).(time.Duration)
	}
	if silence <= 0 {
		silence = defaultCheckSilenceLimit
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return nil, fmt.Errorf("check refused: process inventory unavailable: %w", err)
	}
	defer store.Close()
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "TUSKER_PROCESS_OWNER="+owner)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	out := &silenceWatchedOutput{last: time.Now()}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pid := cmd.Process.Pid
	pgid := pid // Setpgid makes the leader's PID the group ID.
	processStart, _ := processStartTime(pid)
	id, err := store.recordOwnedProcess(owner, processKindCheck, pid, pgid, processStart, currentProcessStarter())
	if err != nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = cmd.Wait()
		return nil, fmt.Errorf("check refused: record process inventory: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	interval := min(max(silence/4, 10*time.Millisecond), 5*time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var runErr error
	reason := ""
	recorded := time.Time{}
	lastOutput, busy, lastCPU := time.Time{}, time.Time{}, -1.0
	for reason == "" {
		select {
		case runErr = <-done:
			reason = "passed"
			if runErr != nil {
				reason = "failed"
			}
		case <-ctx.Done():
			stopProcessGroup(pgid)
			<-done
			reason, runErr = "cancelled", ctx.Err()
		case <-ticker.C:
			_, last := out.snapshot()
			if !last.After(lastOutput) {
				// No new output: a quiet but busy check (go test prints nothing
				// until a package ends) still counts as alive while its group
				// keeps burning CPU.
				if cpu, ok := processGroupCPUSeconds(pgid); ok {
					if cpu > lastCPU && lastCPU >= 0 {
						busy = time.Now()
					}
					lastCPU = cpu
				}
			}
			lastOutput = last
			if busy.After(last) {
				last = busy
			}
			if time.Since(last) > silence {
				stopProcessGroup(pgid)
				<-done
				reason, runErr = "stuck_stopped", fmt.Errorf("%w: no output or CPU progress for %s", errCheckStuckStopped, silence)
			} else if last.After(recorded) {
				_ = store.touchOwnedProcess(id, last)
				recorded = last
			}
		}
	}
	// Stragglers that detached from the leader's pipes still share the group.
	// The leader is reaped, so a live process at PID==PGID would be a new
	// group that reused the ID: only kill a leaderless group that still has
	// members. ponytail: a reuse between this probe and the kill is still
	// possible in theory; a pidfd/kqueue-held group would close it.
	if errors.Is(syscall.Kill(pgid, 0), syscall.ESRCH) && syscall.Kill(-pgid, 0) == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	_ = store.endOwnedProcess(id, reason)
	output, _ := out.snapshot()
	return output, runErr
}
