package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestEligibilityFunctionReconcileDesiredSet(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Desired set", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if reason := daemonDispatchBlockedReason(vault, note, map[string]Note{"APP-T-0001": note}, map[string]Note{"APP-T-0001": note}); reason != "" {
		t.Fatalf("dispatchable task should derive one desired run, got blocker %q", reason)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{
		"status":     "review",
		"readiness":  "waiting_on_review",
		"next_owner": "reviewer",
	})
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexAppServer),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRetryQueued),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-desired",
		SessionRef:      "session-desired",
		AttemptCount:    1,
		UpdatedAt:       "2026-07-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "zero desired release")
	assertEqual(t, "", run.LeaseOwner, "released lease owner")
}

func TestLeaseClaimCASLeaseRenewReclaimLeaseGenerationFence(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	run := RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateUnclaimed), AttemptOutcome: string(AttemptOutcomeNone)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimRunLease("project-1", "APP-T-0001", "owner-1", 1, defaultRunLeaseTTL, now, true)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, claimed, "first claim")
	claimed, err = store.ClaimRunLease("project-1", "APP-T-0001", "owner-2", 2, defaultRunLeaseTTL, now, true)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, claimed, "second claim")
	renewed, err := store.RenewRunLease(RuntimeLeaseRenewal{ProjectID: "project-1", RecordID: "APP-T-0001", Owner: "owner-1", Generation: 1, TTL: defaultRunLeaseTTL, Now: now.Add(15 * time.Second), Dispatchable: true})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, renewed, "renew")
	current, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.LeaseExpiresAt != now.Add(75*time.Second).Format(time.RFC3339) {
		t.Fatalf("renew did not extend expiry: %#v", current)
	}
	current.LeaseExpiresAt = now.Add(-3 * defaultRunLeaseTTL).Format(time.RFC3339)
	if err := store.UpsertRun(*current); err != nil {
		t.Fatal(err)
	}
	renewed, err = store.RenewRunLease(RuntimeLeaseRenewal{ProjectID: "project-1", RecordID: "APP-T-0001", Owner: "owner-1", Generation: 1, TTL: defaultRunLeaseTTL, Now: now.Add(30 * time.Second), Dispatchable: true})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, renewed, "racing heartbeat")
	reclaimed, err := store.ReclaimExpiredRunLease("project-1", "APP-T-0001", now.Add(30*time.Second), defaultRunLeaseTTL, "expired")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, reclaimed, "heartbeat beats reclaim")
	current, err = store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	current.LeaseExpiresAt = now.Add(-3 * defaultRunLeaseTTL).Format(time.RFC3339)
	if err := store.UpsertRun(*current); err != nil {
		t.Fatal(err)
	}
	reclaimed, err = store.ReclaimExpiredRunLease("project-1", "APP-T-0001", now, defaultRunLeaseTTL, "expired")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, reclaimed, "expired reclaim")
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "APP-T-0002", ItemID: "APP-T-0002", LeaseState: string(LeaseStateRunning), LeaseOwner: "owner-2", LeaseGeneration: 2}); err != nil {
		t.Fatal(err)
	}
	err = store.SaveTurn(RunTurn{ProjectID: "project-1", RecordID: "APP-T-0002", AttemptID: "attempt-2", TurnID: "turn-1", LeaseGeneration: 1})
	if err == nil || !strings.Contains(err.Error(), "stale lease generation") {
		t.Fatalf("expected stale generation fence, got %v", err)
	}
}

func TestHeartbeatStopSignal(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateUnclaimed)}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := store.ClaimRunLease("project-1", "APP-T-0001", "owner-1", 1, defaultRunLeaseTTL, now, true); err != nil || !claimed {
		t.Fatalf("claim failed claimed=%v err=%v", claimed, err)
	}
	renewed, err := store.RenewRunLease(RuntimeLeaseRenewal{ProjectID: "project-1", RecordID: "APP-T-0001", Owner: "owner-1", Generation: 1, TTL: defaultRunLeaseTTL, Now: now.Add(defaultRunHeartbeatInterval), Dispatchable: false})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, renewed, "non-dispatchable heartbeat")
}

func TestPidReuseGuardBootAdoptionVerified(t *testing.T) {
	pid := os.Getpid()
	valid := RunStatus{ProcessPID: pid, ProcessPGID: processGroupID(pid), ProcessStartedAt: recordedProcessStartTime(pid, time.Now().UTC().Format(time.RFC3339))}
	if !processIdentityMatches(valid) {
		t.Fatalf("expected current process identity to match: %#v", valid)
	}
	reused := valid
	reused.ProcessStartedAt = "1900-01-01T00:00:00Z"
	if processIdentityMatches(reused) {
		t.Fatalf("pid reuse guard accepted mismatched start time: %#v", reused)
	}
}

func TestAdoptVerifiedLivenessPrefersWrapperIdentityOverLiveChildPID(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Adopt wrapper", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	wrapper := exec.Command("sleep", "30")
	wrapper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	wrapperPID := wrapper.Process.Pid
	wrapperPGID := processGroupID(wrapperPID)
	wrapperStart := recordedProcessStartTime(wrapperPID, time.Now().UTC().Format(time.RFC3339))
	t.Cleanup(func() {
		_ = syscall.Kill(-wrapperPGID, syscall.SIGKILL)
		_ = wrapper.Process.Kill()
		_, _ = wrapper.Process.Wait()
	})

	child := exec.Command("sleep", "30")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	childPID := child.Process.Pid
	childPGID := processGroupID(childPID)
	childStart := recordedProcessStartTime(childPID, time.Now().UTC().Format(time.RFC3339))
	t.Cleanup(func() {
		_ = syscall.Kill(-childPGID, syscall.SIGKILL)
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	})

	dir := t.TempDir()
	eventPath := filepath.Join(dir, "events.jsonl")
	rawLogPath := filepath.Join(dir, "raw.log")
	statusPath := filepath.Join(dir, "status.json")
	promptPath := filepath.Join(dir, "prompt.md")
	now := time.Now().UTC().Format(time.RFC3339)
	if err := NewEventLog(eventPath).Append("attempt_wrapper_spawned", "attempt-wrapper", RunnerCodexExec, map[string]any{
		"pid":           wrapperPID,
		"pgid":          wrapperPGID,
		"process_start": wrapperStart,
	}); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:        project.ProjectID,
		RecordID:         "APP-T-0001",
		ItemID:           "APP-T-0001",
		Runner:           string(RunnerCodexExec),
		Lane:             runLaneExecute,
		LeaseState:       string(LeaseStateRunning),
		LeaseOwner:       "attempt-wrapper",
		LeaseGeneration:  7,
		AttemptOutcome:   string(AttemptOutcomeNone),
		ActiveAttemptID:  "attempt-wrapper",
		WorkspacePath:    dir,
		PromptPath:       promptPath,
		EventSinkPath:    eventPath,
		RawLogPath:       rawLogPath,
		StatusPath:       statusPath,
		WorkRevision:     0,
		AttemptCount:     1,
		ProcessPID:       childPID,
		ProcessPGID:      childPGID,
		ProcessStartedAt: childStart,
		LastHeartbeatAt:  now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateRunning), run.LeaseState, "lease state")
	assertEqual(t, 7, run.LeaseGeneration, "lease generation")
	assertEqual(t, 1, run.AttemptCount, "attempt count")
	assertEqual(t, wrapperPID, run.ProcessPID, "adopted wrapper pid")
	assertEqual(t, wrapperPGID, run.ProcessPGID, "adopted wrapper pgid")
	assertEqual(t, wrapperStart, run.ProcessStartedAt, "adopted wrapper start")
}

func TestReconcileIdempotentReconcileConverges(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Converge", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "done", "readiness": "done", "next_owner": "none"})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexAppServer), Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "attempt-converge", AttemptCount: 1, UpdatedAt: "2026-07-06T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), first.LeaseState, "first converge")
	assertEqual(t, first.LeaseState, second.LeaseState, "second tick idempotent state")
	assertEqual(t, first.AttemptOutcome, second.AttemptOutcome, "second tick idempotent outcome")
}

func disableReviewerForTest(t *testing.T, vault string) {
	t.Helper()
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Reviewer.Enabled = false
	raw, err := yaml.Marshal(wfFile.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+wfFile.Body); err != nil {
		t.Fatal(err)
	}
}
