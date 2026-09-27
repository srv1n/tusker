package main

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReviewCycleCapIgnoresConfigRefusalsAndResetsOnRedrive(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review cap", "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
	project := registerAutomationTestProject(t, vault)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	base := time.Date(2026, 9, 27, 6, 0, 0, 0, time.UTC)
	save := func(id string, at time.Time, lastError, reasonCode string) {
		t.Helper()
		if err := daemon.store.SaveAttempt(RunAttempt{
			AttemptID: id, ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: "devin", Lane: runLaneReview, Outcome: string(AttemptOutcomeFailed),
			LastError: lastError, ReasonCode: reasonCode, StartedAt: at.Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Three instant profile refusals: the reviewer child never started.
	for i, id := range []string{"refused-1", "refused-2", "refused-3"} {
		save(id, base.Add(time.Duration(i)*time.Second), runnerWrapperStartChildFailurePrefix+": Devin ACP requires sandboxed workspace-write with network enabled", "")
	}
	save("refused-code", base.Add(4*time.Second), "runner configuration is invalid", string(RunFailureConfigInvalid))
	save("real-1", base.Add(time.Minute), "runner exited with code 1", "")
	if got, err := daemon.reviewCycleCount(project.ProjectID, "APP-T-0001"); err != nil || got != 1 {
		t.Fatalf("review cycles = %d, %v; want 1 (config refusals excluded)", got, err)
	}
	// An owner redrive resets the window: earlier real cycles stop counting.
	if _, err := daemon.store.RecordBudgetRedrive(project.ProjectID, "APP-T-0001", "human:owner", "requeue", base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := daemon.reviewCycleCount(project.ProjectID, "APP-T-0001"); got != 0 {
		t.Fatalf("review cycles after redrive = %d, want 0", got)
	}
	save("real-2", base.Add(3*time.Minute), "runner exited with code 1", "")
	if got, _ := daemon.reviewCycleCount(project.ProjectID, "APP-T-0001"); got != 1 {
		t.Fatalf("review cycles after redrive and one real attempt = %d, want 1", got)
	}
}

func TestReviewCycleCapNeverParksLiveReviewRun(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Live review at cap", "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer", "source_sha": "abc123", "work_revision": 2})
	project := registerAutomationTestProject(t, vault)
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	for i := 0; i < wfFile.Data.Reviewer.MaxCycles+1; i++ {
		if err := daemon.store.SaveAttempt(RunAttempt{
			AttemptID: "review-" + string(rune('a'+i)), ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
			Runner: "codex", Lane: runLaneReview, StartedAt: now.Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}
	live := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: "codex",
		Lane: runLaneReview, LeaseState: string(LeaseStateRunning), ActiveAttemptID: "review-live",
		LeaseOwner: "review-live", LeaseGeneration: 1, WorkRevision: 2, AttemptCount: 1,
		ProcessPID: pid, ProcessPGID: processGroupID(pid),
		ProcessStartedAt: recordedProcessStartTime(pid, now.Format(time.RFC3339)),
		LastHeartbeatAt:  now.Format(time.RFC3339), LeaseExpiresAt: now.Add(time.Hour).Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
	}
	mustUpsertRun(t, daemon.store, live)
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	after := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if after.LeaseState == string(LeaseStateParkedNoProgress) || strings.Contains(after.LastError, "review cycle cap") {
		t.Fatalf("review cap parked a live review run: %#v", after)
	}
}
