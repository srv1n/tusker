package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDemoKillSessionWorkerExactOwner(t *testing.T) {
	repo := t.TempDir()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	run := RunStatus{
		ProjectID: "demo-project", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1",
		WorkspacePath: repo, ProcessPID: pid, ProcessPGID: pid,
		ProcessStartedAt: recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339)),
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	for _, mismatch := range []struct{ repo, project, task, attempt string }{
		{t.TempDir(), run.ProjectID, run.RecordID, run.ActiveAttemptID},
		{repo, "other-project", run.RecordID, run.ActiveAttemptID},
		{repo, run.ProjectID, run.RecordID, "other-attempt"},
	} {
		if err := demoKillSessionWorker(mismatch.repo, mismatch.project, mismatch.task, mismatch.attempt); err == nil {
			t.Fatal("accepted a worker outside the exact demo owner")
		}
		if !processExists(pid) {
			t.Fatal("mismatched worker was killed")
		}
	}
	if err := demoKillSessionWorker(repo, run.ProjectID, run.RecordID, run.ActiveAttemptID); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("worker exited without a kill signal")
	}
}
