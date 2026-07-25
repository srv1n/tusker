package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSentinelDetectsConfiguredInvariants(t *testing.T) {
	cases := []struct {
		name     string
		check    string
		setup    func(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, now time.Time)
		previous string
		current  string
		liveness func(RunStatus) bool
		wantText string
	}{
		{
			name:  "held lease under review task",
			check: invariantCheckHeldLeaseDispatchEligible,
			setup: func(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, now time.Time) {
				setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
				mustUpsertRun(t, store, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)})
			},
			wantText: "not dispatch-eligible",
		},
		{
			name:  "attempt count past cap",
			check: invariantCheckAttemptCountWithinCaps,
			setup: func(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, now time.Time) {
				mustUpsertRun(t, store, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptCount: 4, UpdatedAt: now.Format(time.RFC3339)})
			},
			wantText: "attempt count exceeds",
		},
		{
			name:  "fresh heartbeat dead process",
			check: invariantCheckFreshHeartbeatPidLive,
			setup: func(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, now time.Time) {
				mustUpsertRun(t, store, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), ProcessPID: 999999, ProcessStartedAt: "1900-01-01T00:00:00Z", LastHeartbeatAt: now.Format(time.RFC3339), AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)})
			},
			liveness: func(RunStatus) bool { return false },
			wantText: "heartbeat outlived",
		},
		{
			name:  "duplicate active task leases",
			check: invariantCheckUniqueActiveLeasePerTask,
			setup: func(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, now time.Time) {
				mustUpsertRun(t, store, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), LeaseGeneration: 2, AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)})
				mustUpsertRun(t, store, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001#worker", ItemID: "APP-T-0001", Lane: "fanout:worker", LeaseState: string(LeaseStateRunning), LeaseGeneration: 1, AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)})
			},
			wantText: "multiple active leases",
		},
		{
			name:     "last poll did not advance",
			check:    invariantCheckLastPollAdvanced,
			previous: "2026-07-06T12:00:00Z",
			current:  "2026-07-06T12:00:00Z",
			setup: func(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, now time.Time) {
			},
			wantText: "did not advance",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vault := automationTestVault(t)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Sentinel invariant", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
			project := registerAutomationTestProject(t, vault)
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Date(2026, 7, 6, 12, 0, 1, 0, time.UTC)
			tc.setup(t, store, project, vault, now)
			daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
			snapshot := sentinelSnapshotForTest(t, store, project, vault, []string{tc.check}, firstNonEmpty(tc.previous, "2026-07-06T12:00:00Z"), firstNonEmpty(tc.current, "2026-07-06T12:00:01Z"), now, tc.liveness)
			status, err := daemon.refreshInvariantCircuitStatus(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, true, status.Open, "circuit open")
			assertEqual(t, invariantViolationReason, status.Reason, "reason")
			if len(status.Violations) == 0 || !strings.Contains(status.Violations[0].Detail, tc.wantText) {
				t.Fatalf("expected violation containing %q, got %#v", tc.wantText, status.Violations)
			}
		})
	}
}

func TestSentinelCircuitOpenBlocksDispatchButServeReadsStatus(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Circuit blocked", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	setAllEligibleDispatchScopeForAutomationTest(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetInvariantCircuitStatus(invariantCircuitStatus{
		Open:     true,
		Reason:   invariantViolationReason,
		OpenedAt: "2026-07-06T12:00:00Z",
		Summary:  invariantViolationReason + ": injected test violation",
		Violations: []runtimeInvariantViolation{{
			Check:     invariantCheckHeldLeaseDispatchEligible,
			ProjectID: project.ProjectID,
			RecordID:  "APP-T-0001",
			Detail:    "injected test violation",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateUnclaimed), run.LeaseState, "lease not dispatched")
	if !strings.Contains(run.LastError, "invariant circuit open") {
		t.Fatalf("expected invariant blocker in run error, got %#v", run)
	}
	daemonStatus, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, boolFromAny(daemonStatus["invariant_circuit_open"]), "daemon status invariant")
	ctx, err := loadAutomationCommandContext(Args{"vault": vault})
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Close()
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	explanation := ctx.explainTask(note)
	assertEqual(t, false, explanation.Dispatchable, "automation dispatchable")
	if !strings.Contains(strings.Join(explanation.Blockers, "; "), "invariant circuit open") {
		t.Fatalf("expected automation blocker, got %#v", explanation.Blockers)
	}
	server := &serveServer{vaultPath: vault, repoRoot: filepath.Dir(vault), addr: "localhost:7420", store: store, now: time.Now}
	response := httptest.NewRecorder()
	server.handleDaemon(response, httptest.NewRequest("GET", "/api/daemon", nil))
	if response.Code != 200 {
		t.Fatalf("daemon API status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		InvariantCircuit invariantCircuitStatus `json:"invariantCircuit"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, payload.InvariantCircuit.Open, "serve invariant circuit")
}

func TestSentinelResumeRefusesUntilViolationCleared(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Resume sentinel", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)}
	mustUpsertRun(t, store, run)
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if _, err := daemon.refreshInvariantCircuitStatus(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckHeldLeaseDispatchEligible}, "", "2026-07-06T12:00:01Z", now, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.ResumeInvariantCircuit(); err == nil || !strings.Contains(err.Error(), "cannot resume daemon") {
		t.Fatalf("expected resume refusal, got %v", err)
	}
	run.LeaseState = string(LeaseStateReleased)
	run.Terminal = true
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatal(err)
	}
	status, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, status.Open, "circuit closed")
}

func TestSentinelConfigCheckListControlsPredicates(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Config sentinel", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	mustUpsertRun(t, store, RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)})
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	status, err := daemon.evaluateInvariantSentinel(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckLastPollAdvanced}, "2026-07-06T12:00:00Z", "2026-07-06T12:00:01Z", now, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, status.Open, "configured checks only")
}

func TestSentinelAllowsCompletedRunnerStatusBeforeReconcile(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Completed status sentinel", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptCount: 1, StatusPath: statusPath, UpdatedAt: now.Format(time.RFC3339)}
	mustUpsertRun(t, store, run)
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	status, err := daemon.evaluateInvariantSentinel(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckHeldLeaseDispatchEligible}, "2026-07-06T11:59:59Z", "2026-07-06T12:00:00Z", now, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, status.Open, "completed runner status should be reconciled, not treated as stale corruption")
}

func TestSentinelBoundedUsesProvidedStoreSnapshot(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Bounded sentinel", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	stale := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning), AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)}
	mustUpsertRun(t, store, stale)
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Runtime.Sentinel.Checks = []string{invariantCheckHeldLeaseDispatchEligible}
	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	notesByID, notesByRecordID := daemonNoteMaps(notes)
	snapshot := runtimeSentinelSnapshot{
		Projects: []runtimeSentinelProjectSnapshot{{
			Project:         project,
			Workflow:        wfFile.Data,
			NotesByID:       notesByID,
			NotesByRecordID: notesByRecordID,
		}},
		PreviousPollAt: "2026-07-06T11:59:59Z",
		CurrentPollAt:  "2026-07-06T12:00:00Z",
		Now:            now,
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	status, err := daemon.evaluateInvariantSentinel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, status.Open, "sentinel should not reread runs outside the provided snapshot")
	snapshot.Runs = []RunStatus{stale}
	status, err = daemon.evaluateInvariantSentinel(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, status.Open, "sentinel detects the violation when it is present in the one-pass snapshot")
}

func TestSentinelDetectsStaleReviewLeaseE2EResumeAfterFix(t *testing.T) {
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review stale sentinel", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Should not dispatch", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	stale := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued), AttemptCount: 1, UpdatedAt: now.Format(time.RFC3339)}
	mustUpsertRun(t, store, stale)
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	status, err := daemon.refreshInvariantCircuitStatus(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckHeldLeaseDispatchEligible}, "2026-07-06T12:00:00Z", "2026-07-06T12:00:01Z", now, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, status.Open, "stale review circuit")
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := latestRunForRecord(t, store, project.ProjectID, "APP-T-0002")
	assertEqual(t, string(LeaseStateUnclaimed), second.LeaseState, "circuit blocks other dispatch")
	stale.LeaseState = string(LeaseStateReleased)
	stale.Terminal = true
	if err := store.UpsertRun(stale); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatal(err)
	}
	resumed, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, resumed.Open, "resume after fix")
}

func sentinelSnapshotForTest(t *testing.T, store *RuntimeStore, project RegisteredProject, vault string, checks []string, previousPollAt, currentPollAt string, now time.Time, liveness func(RunStatus) bool) runtimeSentinelSnapshot {
	t.Helper()
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wfFile.Data.Runtime.Sentinel.Checks = checks
	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	notesByID, notesByRecordID := daemonNoteMaps(notes)
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	totals, err := store.RunTokenTotalsByRun()
	if err != nil {
		t.Fatal(err)
	}
	return runtimeSentinelSnapshot{
		Projects: []runtimeSentinelProjectSnapshot{{
			Project:         project,
			Workflow:        wfFile.Data,
			NotesByID:       notesByID,
			NotesByRecordID: notesByRecordID,
		}},
		Runs:           runs,
		TokenTotals:    totals,
		PreviousPollAt: previousPollAt,
		CurrentPollAt:  currentPollAt,
		Now:            now,
		Liveness:       liveness,
	}
}

func mustUpsertRun(t *testing.T, store *RuntimeStore, run RunStatus) {
	t.Helper()
	if strings.TrimSpace(run.Runner) == "" {
		run.Runner = string(RunnerCodexAppServer)
	}
	if strings.TrimSpace(run.Lane) == "" {
		run.Lane = runLaneExecute
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
}
