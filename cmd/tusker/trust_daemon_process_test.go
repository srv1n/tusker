package main

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestTrustDaemonProcess(t *testing.T) {
	t.Run("owned process group is reaped without touching another group", func(t *testing.T) {
		owned := exec.Command("sh", "-c", "sleep 30 & wait")
		owned.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := owned.Start(); err != nil {
			t.Fatal(err)
		}
		ownedPGID := processGroupID(owned.Process.Pid)
		unrelated := exec.Command("sh", "-c", "sleep 30")
		unrelated.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := unrelated.Start(); err != nil {
			terminateAndReapRunnerCommand(owned, ownedPGID)
			t.Fatal(err)
		}
		unrelatedPGID := processGroupID(unrelated.Process.Pid)
		t.Cleanup(func() { terminateAndReapRunnerCommand(unrelated, unrelatedPGID) })
		terminateAndReapRunnerCommand(owned, ownedPGID)
		if processGroupExists(ownedPGID) {
			t.Fatalf("owned child group %d survived cancellation", ownedPGID)
		}
		if !processGroupExists(unrelatedPGID) {
			t.Fatalf("unrelated group %d was killed", unrelatedPGID)
		}
	})

	t.Run("retry cap parks work with a terminal action", func(t *testing.T) {
		wf := defaultWorkflow()
		wf.Retry.MaxAttempts = 1
		run := (&Daemon{}).scheduleRetry(RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", LeaseState: string(LeaseStateRunning), AttemptCount: 1}, wf, "fake provider disconnect")
		if run.LeaseState != string(LeaseStateParkedNoProgress) || !run.Terminal || run.NextRetryAt != "" {
			t.Fatalf("capped retry entered an idle relaunch loop: %#v", run)
		}
		if run.LastError == "" {
			t.Fatal("capped retry omitted the actionable failure")
		}
	})

	t.Run("stale identity refuses an unrelated live group", func(t *testing.T) {
		other := exec.Command("sleep", "30")
		other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := other.Start(); err != nil {
			t.Fatal(err)
		}
		pgid := processGroupID(other.Process.Pid)
		t.Cleanup(func() { terminateAndReapRunnerCommand(other, pgid) })
		err := interruptRunProcess(nil, &RunStatus{Runner: string(RunnerCodexExec), ProcessPID: other.Process.Pid, ProcessPGID: pgid, ProcessStartedAt: "1900-01-01T00:00:00Z"}, false)
		if err == nil || !processGroupExists(pgid) {
			t.Fatalf("stale process identity signalled an unrelated group: err=%v alive=%t", err, processGroupExists(pgid))
		}
	})

	t.Run("restart skips an already live durable attempt", func(t *testing.T) {
		store, err := OpenRuntimeStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		run := RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-1", LeaseGeneration: 1}
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		launches := 0
		d := &Daemon{store: store, fairDispatchRun: func(_ context.Context, _ RegisteredProject, _ WorkflowFile, _ Note, run RunStatus, _ string, _ string) (RunStatus, bool, bool, error) {
			launches++
			return run, false, false, nil
		}}
		candidate := daemonDispatchCandidate{Project: RegisteredProject{ProjectID: run.ProjectID}, Run: run, Note: Note{Data: map[string]any{"id": run.ItemID}}, Lane: runLaneExecute}
		if err := d.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 0); err != nil {
			t.Fatal(err)
		}
		if launches != 0 {
			t.Fatalf("restart selection relaunched durable live attempt %d times", launches)
		}
	})

	t.Run("idle interval is bounded", func(t *testing.T) {
		if interval := configuredReconcileInterval("1"); interval < minimumReconcileTick {
			t.Fatalf("idle poll interval %s bypassed lower bound", interval)
		}
		if interval := configuredReconcileInterval("bad"); interval != defaultReconcileTick {
			t.Fatalf("invalid interval=%s want default=%s", interval, defaultReconcileTick)
		}
		if interval := configuredReconcileInterval("5000"); interval != 5*time.Second {
			t.Fatal("valid bounded interval was not retained")
		}
	})
}
