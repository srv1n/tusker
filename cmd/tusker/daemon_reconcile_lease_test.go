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

func TestLeaseClaimCASWorkRevisionFence(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	run := RunStatus{
		ProjectID:      "project-1",
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   2,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimRunLease("project-1", "APP-T-0001", "owner-stale", 1, defaultRunLeaseTTL, now, true, RuntimeLeaseClaimPrecondition{
		ExpectedOwner:           "",
		ExpectedLeaseGeneration: 0,
		ExpectedWorkRevision:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, claimed, "stale work_revision claim")
	current, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateUnclaimed), current.LeaseState, "stale claim leaves row unclaimed")
	assertEqual(t, "", current.LeaseOwner, "stale claim does not set owner")

	claimed, err = store.ClaimRunLease("project-1", "APP-T-0001", "owner-current", 1, defaultRunLeaseTTL, now, true, RuntimeLeaseClaimPrecondition{
		ExpectedOwner:           "",
		ExpectedLeaseGeneration: 0,
		ExpectedWorkRevision:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, claimed, "current work_revision claim")
	current, err = store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "owner-current", current.LeaseOwner, "current claim sets owner")
}

func TestDispatchLostCASAbortsBeforeWorkspacePrepAndPreservesControlMutation(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "CAS dispatch", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(t.TempDir(), "workspaces")
	wfFile.Data.Workspace.Strategy = string(WorkspaceStrategyWorktree)
	wfFile.Data.Workspace.Root = workspaceRoot
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	stale := RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         wfFile.Data.Agents.Default,
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   1,
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertRun(stale); err != nil {
		t.Fatal(err)
	}
	mutated := stale
	mutated.WorkRevision = 2
	mutated.LastError = "cancelled by operator"
	mutated.UpdatedAt = time.Date(2026, 7, 6, 12, 5, 0, 0, time.UTC).Format(time.RFC3339)
	if err := store.UpsertRun(mutated); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	updated, persisted, err := daemon.dispatchRun(context.Background(), project, wfFile, note, stale, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, persisted, "lost CAS is already represented by current store row")
	assertEqual(t, 2, updated.WorkRevision, "lost CAS returns current row")
	assertEqual(t, "cancelled by operator", updated.LastError, "lost CAS preserves control mutation")
	current := latestRunForRecord(t, store, project.ProjectID, "APP-T-0001")
	assertEqual(t, 2, current.WorkRevision, "stale dispatch must not overwrite work_revision")
	assertEqual(t, "cancelled by operator", current.LastError, "stale dispatch must not overwrite control mutation")
	assertEqual(t, string(LeaseStateUnclaimed), current.LeaseState, "stale dispatch must not claim row")
	if fileExists(workspaceRoot) {
		entries, err := os.ReadDir(workspaceRoot)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 0 {
			t.Fatalf("lost CAS must not prepare workspaces, found %d entries in %s", len(entries), workspaceRoot)
		}
	}
	attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, len(attempts), "lost CAS must not save attempts")
	runDir := filepath.Join(DefaultStateRoot(), "runs", project.ProjectKey, "APP-T-0001")
	if fileExists(runDir) {
		t.Fatalf("lost CAS must not create run artifacts at %s", runDir)
	}
}

func TestDispatchCASHappyPathStillDispatches(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "CAS happy", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Agents.Default = "test-dispatch"
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, "test-dispatch")
	wfFile.Data.Runners["test-dispatch"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: "sleep 30"}
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         "test-dispatch",
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateUnclaimed),
		AttemptOutcome: string(AttemptOutcomeNone),
		WorkRevision:   intField(note.Data, "work_revision"),
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	updated, persisted, err := daemon.dispatchRun(context.Background(), project, wfFile, note, run, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, persisted, "successful dispatch writes conditionally inside dispatchRun")
	assertEqual(t, string(LeaseStateRunning), updated.LeaseState, "happy dispatch lease state")
	if strings.TrimSpace(updated.LeaseOwner) == "" || strings.TrimSpace(updated.ActiveAttemptID) == "" {
		t.Fatalf("happy dispatch should record lease owner and attempt: %#v", updated)
	}
	if updated.ProcessPGID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-updated.ProcessPGID, syscall.SIGKILL) })
	} else if updated.ProcessPID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(updated.ProcessPID, syscall.SIGKILL) })
	}
	current := latestRunForRecord(t, store, project.ProjectID, "APP-T-0001")
	assertEqual(t, updated.LeaseOwner, current.LeaseOwner, "stored happy dispatch owner")
	assertEqual(t, updated.LeaseGeneration, current.LeaseGeneration, "stored happy dispatch generation")
	attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, len(attempts), "happy dispatch saves one attempt")
	if strings.TrimSpace(updated.WorkspacePath) == "" || !fileExists(updated.WorkspacePath) {
		t.Fatalf("happy dispatch should prepare a workspace, got %q", updated.WorkspacePath)
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

// F3: killSpawnedRunProcess is the production kill path invoked on dispatchRun's
// post-spawn lease-lost fences (a concurrent operator stop revoked the lease
// after the child was spawned but before the row write; the stop's interrupt saw
// ProcessPID=0 and killed nothing). This proves the helper targets the run's real
// process group and reaps it. Full end-to-end orphan-reaping (the claim/write
// race inside dispatchRun) has no clean unit seam and is covered by the wiring
// on both fences plus manual/integration reasoning; F3's runtime impact is low.
func TestKillSpawnedRunProcessReapsOrphanedGroup(t *testing.T) {
	// Guard: a run with no spawned process is a no-op (must not signal anything).
	killSpawnedRunProcess(RunStatus{})

	// A long-lived child in its own session/group; it will NOT self-exit during
	// the test, so a passing assertion can only come from the helper killing it.
	child := exec.Command("sleep", "600")
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pid := child.Process.Pid
	pgid := processGroupID(pid)
	t.Cleanup(func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = child.Process.Kill()
		_, _ = child.Process.Wait()
	})
	if !processExists(pid) {
		t.Fatalf("precondition: spawned child %d should be alive", pid)
	}

	run := RunStatus{ProcessPID: pid, ProcessPGID: pgid}
	// The kill must target the child's real process group, not the (zero) pgid of
	// the store-fetched row that dispatchRun's fence returns.
	assertEqual(t, pgid, processSignalGroup(run), "kill targets the spawned process group")

	killSpawnedRunProcess(run)
	// processExists treats a reaped/zombie process as gone, so no blocking Wait is
	// needed; poll briefly to absorb signal-delivery latency. Cleanup does the
	// kill-then-Wait reap in both the pass and (no-op regression) fail paths, so
	// we never block here.
	reaped := false
	for i := 0; i < 40; i++ {
		if !processExists(pid) {
			reaped = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !reaped {
		t.Fatalf("killSpawnedRunProcess must reap the spawned group; pid %d still alive", pid)
	}
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
