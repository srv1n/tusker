package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

func (s *RuntimeStore) ownedProcesses() ([]ownedProcess, error) {
	rows, err := s.query(`SELECT id, owner, kind, pid, pgid, process_start, starter, started_at, last_sign_of_life, ended_at, end_reason
		FROM process_inventory ORDER BY id`)
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
	all, err := store.ownedProcesses()
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
		stopped++
	}
	return stopped, nil
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

// runOwnedCheck is the one way Tusker runs a check: own process group,
// inventory row before it can do work unrecorded, whole group stopped on
// finish, ctx cancel, or no new output for the silence limit. A silence stop
// returns an error wrapping errCheckStuckStopped.
func runOwnedCheck(ctx context.Context, owner string, cmd *exec.Cmd, silence time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
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
			if time.Since(last) > silence {
				stopProcessGroup(pgid)
				<-done
				reason, runErr = "stuck_stopped", fmt.Errorf("%w: no output for %s", errCheckStuckStopped, silence)
			} else if last.After(recorded) {
				_ = store.touchOwnedProcess(id, last)
				recorded = last
			}
		}
	}
	// Stragglers that detached from the leader's pipes still share the group.
	// ponytail: tiny PID-reuse window between reap and this kill; close it with
	// a pidfd/kqueue-held group if it ever matters.
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	_ = store.endOwnedProcess(id, reason)
	output, _ := out.snapshot()
	return output, runErr
}
