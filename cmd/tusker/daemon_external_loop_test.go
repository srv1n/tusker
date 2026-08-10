package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDaemonAutoAdvanceExternalCollectsAndDispatchesApplyInput(t *testing.T) {
	vault := automationTestVault(t)
	installCodexSleepShimForTest(t)
	writeDaemonExternalLoopConfig(t, vault, defaultCodexExecCommand())
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Daemon auto advance", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)

	sourceDir := writeExternalFetchFiles(t, map[string]string{
		"fix.patch": "diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n",
		"notes.md":  "# Review\n\nPatch is scoped.\n",
	})
	installFakeExternalCollectFetcher(t, sourceDir, []string{"fix.patch", "notes.md"})
	seedReleasedExternalRun(t, project, "APP-T-0001", "chatgpt-browser", "cgpt-auto-1")

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	inputs, err := daemon.store.ListApplyInputsForRun(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].RelPath != "architect/APP-T-0001/fix.patch" {
		t.Fatalf("expected one collected apply input, got %#v", inputs)
	}
	events, err := daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Stage != externalLoopStageCollected || events[0].Action != externalLoopActionApplyPatch {
		t.Fatalf("expected one collected/apply_patch event, got %#v", events)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	defer killRunProcess(run)
	assertEqual(t, "codex_exec", run.Runner, "apply runner")
	if !isDispatchingLeaseState(run.LeaseState) {
		t.Fatalf("expected daemon to dispatch apply runner, got run %#v", run)
	}
	assertExists(t, filepath.Join(project.RepoRoot, "architect", "APP-T-0001", "fix.patch"))
	if strings.TrimSpace(run.CloudTaskID) != "" {
		t.Fatalf("apply run should not retain external cloud task id, got %#v", run)
	}
}

func TestDaemonAutoAdvanceExternalCollectFailureRecordsBlockedEvent(t *testing.T) {
	vault := automationTestVault(t)
	writeDaemonExternalLoopConfig(t, vault, `true`)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Daemon collect blocked", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	previous := runExternalCollectFetch
	runExternalCollectFetch = func(ctx context.Context, req externalFetchRequest) (externalFetchResult, error) {
		return externalFetchResult{}, errors.New("browser auth required")
	}
	t.Cleanup(func() { runExternalCollectFetch = previous })
	seedReleasedExternalRun(t, project, "APP-T-0001", "chatgpt-browser", "cgpt-auto-blocked")

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Stage != externalLoopStageBlocked || events[0].Action != externalLoopActionEscalateHuman {
		t.Fatalf("expected blocked escalation event, got %#v", events)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if !strings.Contains(run.LastError, "browser auth required") {
		t.Fatalf("expected collect failure in run last_error, got %#v", run)
	}

	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err = daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected blocked event to be idempotent, got %#v", events)
	}
}

func TestDaemonAutoAdvanceExternalApplyFailureDispatchesRepairContinuation(t *testing.T) {
	vault := automationTestVault(t)
	writeDaemonExternalLoopConfig(t, vault, `true`)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Daemon repair continuation", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	seedCollectedExternalApplyState(t, project, "APP-T-0001", "cgpt-original-failure")
	seedApplyResultRun(t, project, "APP-T-0001", string(LeaseStateRetryQueued), string(AttemptOutcomeFailed), "git apply conflict in vault picker")

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	events, err := daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasExternalLoopEvent(events, externalLoopStageApplyFailed, externalLoopActionContinueThreadOnFailure) {
		t.Fatalf("expected apply_failed/continue_thread event, got %#v", events)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, "chatgpt-browser", run.Runner, "repair runner")
	assertEqual(t, runLaneExecute, run.Lane, "repair lane")
	if !isDispatchingLeaseState(run.LeaseState) {
		t.Fatalf("expected external repair continuation to be dispatching, got %#v", run)
	}
	if strings.TrimSpace(run.CloudTaskID) == "" {
		t.Fatalf("expected external repair continuation cloud task id, got %#v", run)
	}
}

func TestDaemonAutoAdvanceExternalReviewAcceptedClosesLowRiskTask(t *testing.T) {
	vault := automationTestVault(t)
	writeDaemonExternalLoopConfig(t, vault, `true`)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Daemon accepted external review", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "ready", "next_owner": "reviewer:agent"})
	project := registerAutomationTestProject(t, vault)
	sourceDir := writeExternalFetchFiles(t, map[string]string{
		"review.md": "# RESULT\n\n```json\n{\"kind\":\"review\",\"verdict\":\"approve\",\"risk\":\"low\",\"summary\":\"All acceptance checks pass.\",\"findings\":[]}\n```\n",
	})
	installFakeExternalCollectFetcher(t, sourceDir, []string{"review.md"})
	seedReleasedExternalReviewRun(t, project, "APP-T-0001", "chatgpt-browser", "cgpt-review-accepted")

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	events, err := daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasExternalLoopEvent(events, externalLoopStageCollected, externalLoopActionCloseTask) {
		t.Fatalf("expected collected/close_task event, got %#v", events)
	}
	note, err := resolveNote(project.VaultRoot, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "done", stringField(note.Data, "status"), "task status")
	if strings.TrimSpace(stringField(note.Data, "closed_at")) == "" {
		t.Fatalf("expected task to be closed, got %#v", note.Data)
	}
}

func TestDaemonAutoAdvanceExternalApplySuccessDispatchesExternalReview(t *testing.T) {
	vault := automationTestVault(t)
	writeDaemonExternalLoopConfig(t, vault, `true`)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Daemon external review", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "ready", "next_owner": "reviewer:agent"})
	project := registerAutomationTestProject(t, vault)
	seedCollectedExternalApplyState(t, project, "APP-T-0001", "cgpt-original-review")
	seedApplyResultRun(t, project, "APP-T-0001", string(LeaseStateReleased), string(AttemptOutcomeSucceeded), "")

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	events, err := daemon.store.ListExternalLoopEvents(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if !hasExternalLoopEvent(events, externalLoopStageApplySucceeded, externalLoopActionRequestReviewNext) {
		t.Fatalf("expected apply_succeeded/request_review_next event, got %#v", events)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	assertEqual(t, "chatgpt-browser", run.Runner, "review runner")
	assertEqual(t, runLaneReview, run.Lane, "review lane")
	if !isDispatchingLeaseState(run.LeaseState) {
		t.Fatalf("expected external review to be dispatching, got %#v", run)
	}
	if strings.TrimSpace(run.CloudTaskID) == "" {
		t.Fatalf("expected external review cloud task id, got %#v", run)
	}
}

func writeDaemonExternalLoopConfig(t *testing.T, vault, applyCommand string) {
	t.Helper()
	body := strings.TrimSpace(`
schema: tusker.config/v1
project_id: app
automation:
  trigger_states: [ready, rework]
  default_runner: chatgpt-browser
  enabled_runners: [chatgpt-browser, codex_exec]
  concurrency:
    max_active_runs: 10
    max_active_runs_per_project: 10
  runners:
    chatgpt-browser:
      kind: codex_cloud
      environment_id: chatgpt-browser
      apply_mode: manual
      pr_mode: none
      external_collect: true
      command: "printf '{\"task_id\":\"cgpt-daemon-next\",\"status\":\"queued\"}'"
      status_command: "printf '{\"task_id\":\"{{cloud_task_id}}\",\"status\":\"queued\"}'"
      collect_command: "chatgpt-handoff fetch {{cloud_task_id}} --json --out-dir {{out_dir}}"
    codex_exec:
      kind: codex_exec
      command: "`+applyCommand+`"
`) + "\n"
	if err := writeText(managedTuskerConfigPath(vault), body); err != nil {
		t.Fatal(err)
	}
}

func seedReleasedExternalRun(t *testing.T, project RegisteredProject, recordID, runner, jobID string) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        recordID,
		ItemID:          recordID,
		Runner:          runner,
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeSucceeded),
		ActiveAttemptID: "attempt-external-1",
		CloudTaskID:     jobID,
		WorkRevision:    1,
		AttemptCount:    1,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
}

func seedReleasedExternalReviewRun(t *testing.T, project RegisteredProject, recordID, runner, jobID string) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        recordID,
		ItemID:          recordID,
		Runner:          runner,
		Lane:            runLaneReview,
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeSucceeded),
		ActiveAttemptID: "attempt-external-review-1",
		CloudTaskID:     jobID,
		WorkRevision:    1,
		AttemptCount:    1,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}
}

func seedCollectedExternalApplyState(t *testing.T, project RegisteredProject, taskID, jobID string) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	artifactDir := filepath.Join(project.RepoRoot, "architect", taskID)
	if err := ensureDir(artifactDir); err != nil {
		t.Fatal(err)
	}
	patchBody := []byte("diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n")
	patchPath := filepath.Join(artifactDir, "fix.patch")
	if err := writeText(patchPath, string(patchBody)); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(patchBody)
	hash := hex.EncodeToString(sum[:])
	if _, err := store.UpsertApplyInput(RuntimeApplyInput{
		ProjectID: project.ProjectID,
		RecordID:  taskID,
		ItemID:    taskID,
		Runner:    "chatgpt-browser",
		JobID:     jobID,
		AttemptID: "attempt-external-1",
		Path:      patchPath,
		RelPath:   filepath.ToSlash(filepath.Join("architect", taskID, "fix.patch")),
		Sha256:    hash,
		Kind:      "patch",
	}); err != nil {
		t.Fatal(err)
	}
	event := ExternalLoopEvent{
		ProjectID: project.ProjectID,
		RecordID:  taskID,
		ItemID:    taskID,
		Runner:    "chatgpt-browser",
		JobID:     jobID,
		AttemptID: "attempt-external-1",
		Stage:     externalLoopStageCollected,
		Action:    externalLoopActionApplyPatch,
		Status:    "ok",
		Reason:    "collected one external apply input",
	}
	event.IdempotencyKey = externalLoopIdempotencyKey(event)
	if _, _, err := store.SaveExternalLoopEvent(event); err != nil {
		t.Fatal(err)
	}
}

func seedApplyResultRun(t *testing.T, project RegisteredProject, taskID, leaseState, outcome, lastError string) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       taskID,
		ItemID:         taskID,
		Runner:         "codex_exec",
		Lane:           runLaneExecute,
		LeaseState:     leaseState,
		AttemptOutcome: outcome,
		WorkRevision:   1,
		AttemptCount:   1,
		LastError:      lastError,
		UpdatedAt:      now,
		StartedAt:      now,
		LastEventAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
}

func hasExternalLoopEvent(events []ExternalLoopEvent, stage, action string) bool {
	for _, event := range events {
		if normalizeExternalLoopStage(event.Stage) == stage && normalizeExternalLoopAction(event.Action) == action {
			return true
		}
	}
	return false
}

func latestRunForRecord(t *testing.T, store *RuntimeStore, projectID, recordID string) RunStatus {
	t.Helper()
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if run.ProjectID == projectID && run.RecordID == recordID {
			return run
		}
	}
	t.Fatalf("run not found for %s/%s", projectID, recordID)
	return RunStatus{}
}

func killRunProcess(run RunStatus) {
	if run.ProcessPID <= 0 {
		return
	}
	pgid := processSignalGroup(run)
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	_ = syscall.Kill(run.ProcessPID, syscall.SIGTERM)
	for i := 0; i < 20 && processExists(run.ProcessPID); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if processExists(run.ProcessPID) {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		_ = syscall.Kill(run.ProcessPID, syscall.SIGKILL)
	}
}

// F2 (end-to-end): dispatchExternalApplyInput publishes the prepared dispatch
// intent before claiming, and MUST do so without clobbering the live stored
// lease. Here a concurrent advance has bumped the stored lease_generation past
// the caller's hydrated ProjectRuns snapshot; the pre-dispatch upsert must leave
// the stored generation intact so dispatchRun's ClaimRunLease CAS misses and the
// concurrent state survives (the row is not re-claimed / re-dispatched over).
// On the old blind-UpsertRun code the pre-upsert forced the row back to the
// stale gen=1/owner-A snapshot, the claim then succeeded, and this assertion set
// fails (owner/lease_state change to a fresh claim).
func TestDispatchExternalApplyInputPreservesConcurrentLeaseAdvance(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Preserve concurrent advance", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	// Benign apply runner: if a (buggy) claim were to succeed, the spawned child
	// is a harmless sleep that the deferred killRunProcess reaps.
	wfFile.Data.Agents.Default = "test-dispatch"
	wfFile.Data.Agents.Enabled = append(wfFile.Data.Agents.Enabled, "test-dispatch")
	wfFile.Data.Runners["test-dispatch"] = RunnerDefinition{Kind: string(RunnerCodexExec), Command: defaultCodexExecCommand()}
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workRevision := intField(note.Data, "work_revision")
	// Caller's hydrated view: gen=1, owner "A", released (claimable). Keeping the
	// snapshot work_revision equal to the note's avoids effectiveRunForTask's
	// revision-change reset, so run.LeaseGeneration stays 1 into the CAS.
	snapshot := RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          "test-dispatch",
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateReleased),
		LeaseOwner:      "A",
		LeaseGeneration: 1,
		AttemptOutcome:  string(AttemptOutcomeNone),
		WorkRevision:    workRevision,
		AttemptCount:    1,
	}
	if err := store.UpsertRun(snapshot); err != nil {
		t.Fatal(err)
	}
	// Concurrent advance bumps the STORED generation to 2 (still claimable); the
	// caller's ProjectRuns snapshot below is deliberately left at gen=1.
	concurrent := snapshot
	concurrent.LeaseGeneration = 2
	if err := store.UpsertRun(concurrent); err != nil {
		t.Fatal(err)
	}

	ctx := &automationCommandContext{
		StateRoot:         DefaultStateRoot(),
		Store:             store,
		Project:           project,
		ProjectRegistered: true,
		Workflow:          wfFile,
		ProjectRuns:       map[string]RunStatus{"APP-T-0001": snapshot},
	}
	explanation := automationTaskExplanation{Dispatchable: true, ID: "APP-T-0001", RecordID: "APP-T-0001", Runner: "test-dispatch"}

	result, dispatchErr := dispatchExternalApplyInput(ctx, note, explanation, "test-dispatch")
	if result != nil {
		defer killRunProcess(*result)
	}
	if dispatchErr != nil {
		t.Fatalf("dispatch should not error when the CAS misses on a concurrent advance: %v", dispatchErr)
	}

	current, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if current == nil {
		t.Fatal("run row missing after dispatch")
	}
	assertEqual(t, 2, current.LeaseGeneration, "concurrent lease generation preserved")
	assertEqual(t, "A", current.LeaseOwner, "row not reclaimed by this dispatch")
	assertEqual(t, string(LeaseStateReleased), current.LeaseState, "row not claimed by this dispatch")
}
