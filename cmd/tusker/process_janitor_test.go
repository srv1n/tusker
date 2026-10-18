package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A starter that cannot be alive: PIDs never reach this value on darwin or
// default Linux.
const deadTestStarter = "pid:4999999@2000-01-01T00:00:00Z"

type janitorTestProc struct {
	pid  int
	done chan struct{}
}

func (p janitorTestProc) stopped(within time.Duration) bool {
	select {
	case <-p.done:
		return true
	case <-time.After(within):
		return false
	}
}

// startJanitorTestGroup starts a sleeping group leader with a child, reaped by
// a goroutine so a stopped group does not linger as a zombie.
func startJanitorTestGroup(t *testing.T) janitorTestProc {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := janitorTestProc{pid: cmd.Process.Pid, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	t.Cleanup(func() { _ = syscall.Kill(-p.pid, syscall.SIGKILL); <-p.done })
	return p
}

func recordJanitorTestRow(t *testing.T, store *RuntimeStore, owner, kind string, p janitorTestProc, start, starter string) ownedProcess {
	t.Helper()
	if start == "" {
		start, _ = processStartTime(p.pid)
	}
	if _, err := store.recordOwnedProcess(owner, kind, p.pid, p.pid, start, starter); err != nil {
		t.Fatal(err)
	}
	return inventoryRow(t, store, owner)
}

func processEventsFor(t *testing.T, store *RuntimeStore, owner string) []processEvent {
	t.Helper()
	all, err := store.processEvents()
	if err != nil {
		t.Fatal(err)
	}
	var out []processEvent
	for _, e := range all {
		if e.Owner == owner {
			out = append(out, e)
		}
	}
	return out
}

func liveTestRun(t *testing.T, store *RuntimeStore, recordID, attemptID, lastEvent string) {
	t.Helper()
	if err := store.UpsertRun(RunStatus{ProjectID: "proj", RecordID: recordID, ActiveAttemptID: attemptID, LeaseState: string(LeaseStateRunning), LastEventAt: lastEvent}); err != nil {
		t.Fatal(err)
	}
}

// A1: a gate left running by a crashed daemon is stopped with its group.
func TestJanitorStopsOrphanedGateAfterCrash(t *testing.T) {
	store := openInventoryTestStore(t)
	gate := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "landing_gate:lane-a", processKindCheck, gate, "", deadTestStarter)
	if err := runProcessJanitor(store, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !gate.stopped(2 * time.Second) {
		t.Fatal("orphaned gate still running")
	}
	if row := inventoryRow(t, store, "landing_gate:lane-a"); row.EndReason != "starter_stopped" {
		t.Fatalf("end reason %q", row.EndReason)
	}
	events := processEventsFor(t, store, "landing_gate:lane-a")
	if len(events) != 1 || events[0].Kind != "stopped" || events[0].Rule != ruleCheckEndsWithStarter || events[0].PID != gate.pid || events[0].Reason == "" {
		t.Fatalf("events %+v", events)
	}
}

// A2: a live agent session from a previous daemon is adopted, not stopped,
// however long it has run; one whose run no longer owns it is stopped.
func TestJanitorAdoptsLiveAgentSession(t *testing.T) {
	store := openInventoryTestStore(t)
	now := time.Now().UTC()
	liveTestRun(t, store, "T-1", "ATT-LIVE", now.Format(time.RFC3339))
	live := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "ATT-LIVE", processKindAgentSession, live, "", deadTestStarter)
	if _, err := store.exec(`UPDATE process_inventory SET started_at = ? WHERE owner = 'ATT-LIVE'`, now.Add(-3*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	orphan := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "ATT-GONE", processKindAgentSession, orphan, "", deadTestStarter)
	if err := runProcessJanitor(store, 0, now); err != nil {
		t.Fatal(err)
	}
	if live.stopped(200 * time.Millisecond) {
		t.Fatal("live agent session was stopped")
	}
	row := inventoryRow(t, store, "ATT-LIVE")
	if row.EndedAt != "" || row.Starter != currentProcessStarter() {
		t.Fatalf("live session not adopted: %+v", row)
	}
	if events := processEventsFor(t, store, "ATT-LIVE"); len(events) != 1 || events[0].Kind != "adopted" {
		t.Fatalf("live session events %+v", events)
	}
	if !orphan.stopped(2 * time.Second) {
		t.Fatal("agent session without a live run kept running")
	}
	if events := processEventsFor(t, store, "ATT-GONE"); len(events) != 1 || events[0].Rule != ruleAgentOwnerGone {
		t.Fatalf("orphan events %+v", events)
	}
}

// A3: a process whose start time does not match its row is reported only.
func TestJanitorRefusesMismatchedIdentity(t *testing.T) {
	store := openInventoryTestStore(t)
	proc := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "landing_gate:reused", processKindCheck, proc, "1999-01-01T00:00:00Z", deadTestStarter)
	if err := runProcessJanitor(store, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if proc.stopped(200 * time.Millisecond) {
		t.Fatal("process with mismatched identity was stopped")
	}
	events := processEventsFor(t, store, "landing_gate:reused")
	if len(events) != 1 || events[0].Kind != "reported" || events[0].Rule != ruleIdentityMustMatch {
		t.Fatalf("events %+v", events)
	}
	if row := inventoryRow(t, store, "landing_gate:reused"); row.EndReason != "identity_unconfirmed" {
		t.Fatalf("row %+v", row)
	}
}

// A4: three interrupted attempts of one gate never overlap: each new attempt
// stops the dead starter's copy first. Two gates never share the slot.
func TestJanitorGateSingleCopy(t *testing.T) {
	store := openInventoryTestStore(t)
	dir := t.TempDir()
	const owner = "landing_gate:lane-single"
	for i := range 3 {
		orphan := startJanitorTestGroup(t)
		if _, err := store.recordOwnedProcess(owner, processKindCheck, orphan.pid, orphan.pid, mustStart(t, orphan.pid), deadTestStarter); err != nil {
			t.Fatal(err)
		}
		out, err := runV7OwnedLandingGateCommand(context.Background(), owner, dir, "if kill -0 "+strconv.Itoa(orphan.pid)+" 2>/dev/null; then echo overlap; else echo alone; fi", false)
		if err != nil || strings.TrimSpace(string(out)) != "alone" {
			t.Fatalf("attempt %d: %q %v", i, out, err)
		}
	}
	lock := filepath.Join(dir, "slot")
	var wg sync.WaitGroup
	outs := make([]string, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, _ := runV7OwnedLandingGateCommand(context.Background(), "landing_gate:slot-"+strconv.Itoa(i), dir, "mkdir "+lock+" || echo overlap; sleep 0.3; rmdir "+lock, false)
			outs[i] = string(out)
		}()
	}
	wg.Wait()
	if strings.Contains(strings.Join(outs, ""), "overlap") {
		t.Fatalf("two gates ran at once: %q", outs)
	}
}

func mustStart(t *testing.T, pid int) string {
	t.Helper()
	start, ok := processStartTime(pid)
	if !ok {
		t.Fatalf("no start time for %d", pid)
	}
	return start
}

// A4: watchdog exits while a passed review is landing count toward the
// crash-loop breaker; once open, the landing pauses with a visible reason.
func TestCrashLoopBreakerCountsLandingRestarts(t *testing.T) {
	env := newReviewPassEnv(t, map[string]string{"reviewed.txt": "reviewed\n"}, []string{"reviewed.txt"})
	env.submitProposal(t, "pass")
	t.Setenv(daemonLaunchdEnvKey, "1")
	exits := 0
	previous := daemonWatchdogExit
	daemonWatchdogExit = func(string) { exits++ }
	t.Cleanup(func() { daemonWatchdogExit = previous })
	store := env.daemon.store
	now := time.Now().UTC()
	var status daemonCrashLoopStatus
	for i := 0; i <= daemonCrashLoopBurst; i++ {
		var err error
		if status, err = store.beginManagedDaemonStart(false, now); err != nil {
			t.Fatal(err)
		}
		if err := store.SetSetting("daemon_watchdog_beat_at", now.Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		env.daemon.checkWatchdogOrExit(now, time.Second)
	}
	if status, _ = store.beginManagedDaemonStart(false, now); !status.Open || exits != daemonCrashLoopBurst+1 {
		t.Fatalf("breaker not open after %d watchdog exits: %+v", exits, status)
	}
	env.passHandler(t)
	if env.landed() {
		t.Fatal("landing ran with the crash-loop breaker open")
	}
	if run := env.latestRun(t); !strings.Contains(run.LastError, "landing paused: daemon circuit open") || run.ReasonCode != "" {
		t.Fatalf("pause reason not visible: %q (code %q)", run.LastError, run.ReasonCode)
	}
}

// A5: the watchdog stops its daemon's checks before it exits.
func TestWatchdogExitStopsOwnChecks(t *testing.T) {
	store := openInventoryTestStore(t)
	t.Setenv(daemonLaunchdEnvKey, "")
	gate := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "landing_gate:watchdog", processKindCheck, gate, "", currentProcessStarter())
	exited, stoppedFirst := false, false
	previous := daemonWatchdogExit
	daemonWatchdogExit = func(string) { exited, stoppedFirst = true, gate.stopped(time.Second) }
	t.Cleanup(func() { daemonWatchdogExit = previous })
	if err := store.SetSetting("daemon_watchdog_beat_at", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	(&Daemon{store: store}).checkWatchdogOrExit(time.Now().UTC(), time.Second)
	if !exited || !stoppedFirst {
		t.Fatalf("exited=%v stoppedBeforeExit=%v", exited, stoppedFirst)
	}
	if row := inventoryRow(t, store, "landing_gate:watchdog"); row.EndReason != "starter_stopped" {
		t.Fatalf("row %+v", row)
	}
}

// A6: a silent agent session is flagged once and keeps running.
func TestJanitorFlagsSilentAgentSession(t *testing.T) {
	store := openInventoryTestStore(t)
	now := time.Now().UTC()
	liveTestRun(t, store, "T-1", "ATT-QUIET", now.Add(-30*time.Minute).Format(time.RFC3339))
	agent := startJanitorTestGroup(t)
	recordJanitorTestRow(t, store, "ATT-QUIET", processKindAgentSession, agent, "", currentProcessStarter())
	if _, err := store.exec(`UPDATE process_inventory SET started_at = ?, last_sign_of_life = ? WHERE owner = 'ATT-QUIET'`,
		now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := runProcessJanitor(store, 20*time.Minute, now); err != nil {
			t.Fatal(err)
		}
	}
	if agent.stopped(200 * time.Millisecond) {
		t.Fatal("silent agent session was stopped")
	}
	events := processEventsFor(t, store, "ATT-QUIET")
	if len(events) != 1 || events[0].Kind != "silent" || events[0].Rule != ruleAgentSilence {
		t.Fatalf("events %+v", events)
	}
	if row := inventoryRow(t, store, "ATT-QUIET"); row.EndedAt != "" {
		t.Fatalf("row ended: %+v", row)
	}
}
