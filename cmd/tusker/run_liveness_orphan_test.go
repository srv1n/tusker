package main

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestRunLivenessClassifier(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	defer func() { _ = syscall.Kill(-pgid, syscall.SIGKILL); _ = cmd.Wait() }()
	started, ok := processStartTime(pgid)
	if !ok {
		t.Fatal("cannot read wrapper start identity")
	}
	run := RunStatus{ProcessPID: pgid, ProcessPGID: pgid, ProcessStartedAt: started}
	if got := classifyRunLiveness(run); got != runLivenessAlive {
		t.Fatalf("live wrapper classified %s", got)
	}
	foreign := run
	foreign.ProcessStartedAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if got := classifyRunLiveness(foreign); got != runLivenessForeign {
		t.Fatalf("mismatched group leader classified %s", got)
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run.ProjectID, run.RecordID, run.ActiveAttemptID, run.LeaseGeneration = "p", "r", "attempt", 1
	if err := saveRunSessionControlIntent(store, runSessionControlIntent{
		Action: runSessionControlStop, State: runSessionControlPending,
		ProjectID: run.ProjectID, RecordID: run.RecordID,
		AttemptID: run.ActiveAttemptID, LeaseGeneration: run.LeaseGeneration,
		ProcessPGID: run.ProcessPGID, ProcessStarted: run.ProcessStartedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if runStopSignalAuthorized(store, run) {
		t.Fatal("Stop authorized a group while its leader still exists")
	}
	if err := syscall.Kill(pgid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	deadline := time.Now().Add(time.Second)
	for !processGroupExists(pgid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !processGroupExists(pgid) {
		t.Skip("shell child did not survive wrapper exit on this host")
	}
	if got := classifyRunLiveness(run); got != runLivenessOrphaned {
		t.Fatalf("live child group classified %s", got)
	}
	if boot, ok := hostBootTime(); ok {
		preBoot := run
		preBoot.ProcessStartedAt = boot.Add(-time.Minute).Format(time.RFC3339)
		if got := classifyRunLiveness(preBoot); got != runLivenessGone {
			t.Fatalf("preboot group classified %s", got)
		}
	}
}

func TestClaimOrphanedGroupBlocksOwnedPath(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := cmd.Process.Pid
	defer func() { _ = syscall.Kill(-pgid, syscall.SIGKILL); _ = cmd.Wait() }()
	started, ok := processStartTime(pgid)
	if !ok {
		t.Fatal("cannot read wrapper start identity")
	}
	time.Sleep(40 * time.Millisecond)
	if err := syscall.Kill(pgid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if !processGroupExists(pgid) {
		t.Skip("shell child did not survive wrapper exit on this host")
	}
	store, candidateRun, service := ownedPathClaimFixture(t, "APP-T-ORPHAN-2", "APP-T-ORPHAN-1", []string{"migrations/0014.sql"}, []string{"migrations"}, func(holder *RunStatus, now time.Time) {
		holder.LeaseExpiresAt = now.Add(-time.Minute).Format(time.RFC3339)
		holder.ProcessPID, holder.ProcessPGID, holder.ProcessStartedAt = pgid, pgid, started
	})
	result, err := service.claim(candidateRun, "lane-b")
	var typed *TuskerError
	if err == nil || result.Claimed || !errors.As(err, &typed) || typed.Code != "OWNED_PATH_CONFLICT" || !strings.Contains(typed.Message, "lease_expired_orphaned_group") {
		t.Fatalf("orphaned holder did not fence claim: result=%#v err=%v", result, err)
	}
	holder, err := store.FindRun("APP-T-ORPHAN-1")
	if err != nil || holder == nil || holder.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("orphaned holder was reclaimed: %#v err=%v", holder, err)
	}
}

func TestRunStopIntentCAS(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	intent := runSessionControlIntent{Action: runSessionControlStop, State: runSessionControlPending, ProjectID: "p", RecordID: "r", LeaseGeneration: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := saveRunSessionControlIntentIfPrior(store, intent, nil)
			if err != nil {
				t.Errorf("save stop intent: %v", err)
			}
			wins <- won
		}()
	}
	wg.Wait()
	close(wins)
	count := 0
	for won := range wins {
		if won {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("CAS winners=%d, want one", count)
	}
}

func TestStartFreshIgnoresRejectedSession(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{ProjectID: "p", RecordID: "r", Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateRetryQueued), AttemptCount: 1, LastError: "overwritten diagnostic"}
	if err := store.SaveSession(RunnerSession{ProjectID: "p", RecordID: "r", Runner: run.Runner, SessionRef: "rejected", Resumable: true, StartedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := saveRunSessionControlIntent(store, runSessionControlIntent{Action: runSessionControlFresh, State: runSessionControlQueued, ProjectID: "p", RecordID: "r"}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	resolved, err := d.resolveResumeSession(RegisteredProject{ProjectID: "p"}, Note{}, run)
	if err != nil || resolved.SessionRef != "" {
		t.Fatalf("rejected session revived: %#v err=%v", resolved, err)
	}
}
