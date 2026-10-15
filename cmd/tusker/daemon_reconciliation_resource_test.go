package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestProjectReconcileNotificationIsImmediateAndStaysCoalescedUntilConsumed(t *testing.T) {
	d := &Daemon{notifyWake: make(chan string, 1)}
	defer d.stopNotifyTimers()
	d.scheduleProjectReconcile("alpha")
	select {
	case id := <-d.notifyWake:
		if id != "alpha" {
			t.Fatalf("wake = %q", id)
		}
	default:
		t.Fatal("CLI notification waited for a timer")
	}
	for range 100 {
		d.scheduleProjectReconcile("alpha")
	}
	d.scheduleProjectReconcile("beta")
	d.scheduleProjectReconcile("gamma")
	d.takeProjectReconcile("alpha")
	seen := map[string]bool{}
	for range 2 {
		select {
		case id := <-d.notifyWake:
			if id == "alpha" || seen[id] {
				t.Fatalf("duplicate queued wake: %q", id)
			}
			seen[id] = true
			d.takeProjectReconcile(id)
		default:
			t.Fatal("a project wake was lost when the bounded channel filled")
		}
	}
	if !seen["beta"] || !seen["gamma"] {
		t.Fatalf("pending projects = %#v", seen)
	}
	d.scheduleProjectReconcile("alpha")
	select {
	case id := <-d.notifyWake:
		if id != "alpha" {
			t.Fatalf("trailing mutation wake = %q", id)
		}
	default:
		t.Fatal("a mutation after consumption did not wake the next poll")
	}
}

func TestFrontierMaintenanceAvoidsVaultWalkAndFallsBackForExternalEdits(t *testing.T) {
	vault := t.TempDir()
	for i := range 40 {
		status := "backlog"
		if i == 0 {
			status = "in-progress"
		}
		writeReconcileResourceTask(t, vault, fmt.Sprintf("APP-T-%04d", i+1), status)
	}
	notes, err := listOperationalNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	d := &Daemon{}
	d.rebuildFrontier("app", notes)
	d.frontierScannedAt["app"] = now
	project := RegisteredProject{ProjectID: "app", VaultRoot: vault}
	wf := defaultWorkflow()
	runs := []RunStatus{{ProjectID: "app", RecordID: "APP-T-0001", LeaseState: string(LeaseStateRunning)}}
	var walks, reads atomic.Int64
	noteCacheListObserver = func() { walks.Add(1) }
	noteCacheReadObserver = func() { reads.Add(1) }
	t.Cleanup(func() { noteCacheListObserver = nil; noteCacheReadObserver = nil })
	if got, ok := d.frontierMaintenanceNotes(project, wf, runs, now.Add(reconcileLiveCadence)); !ok || len(got) != len(notes) {
		t.Fatalf("active maintenance: ok=%v notes=%d", ok, len(got))
	}
	if walks.Load() != 0 || reads.Load() > 1 {
		t.Fatalf("one active run scanned historical tasks: walks=%d reads=%d", walks.Load(), reads.Load())
	}
	if _, ok := d.frontierMaintenanceNotes(project, wf, nil, now.Add(2*reconcileLiveCadence)); !ok {
		t.Fatal("idle frontier could not be reused")
	}
	if walks.Load() != 0 {
		t.Fatal("idle maintenance walked the vault")
	}

	// External writers do not send CLI events. New records appear at the
	// bounded full-read fallback, while the active lease cadence remains 5s.
	writeReconcileResourceTask(t, vault, "APP-T-0099", "backlog")
	if got, ok := d.frontierMaintenanceNotes(project, wf, runs, now.Add(time.Minute)); !ok || len(got) != len(notes) {
		t.Fatalf("maintenance unexpectedly required a full scan: ok=%v count=%d", ok, len(got))
	}
	if _, ok := d.frontierMaintenanceNotes(project, wf, runs, now.Add(reconcileFullCadence)); ok {
		t.Fatal("external-edit fallback did not force a full scan at five minutes")
	}
	fresh, err := listOperationalNotes(vault)
	if err != nil || len(fresh) != len(notes)+1 {
		t.Fatalf("full fallback missed external record: count=%d err=%v", len(fresh), err)
	}
}

func TestScheduledActivePollAvoidsVaultWalkAndFindsExternalRecords(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Warm interactive lease", "risk": "low", "priority": "p0", "owned-paths": "src", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	for i := 2; i <= 40; i++ {
		writeReconcileResourceTask(t, vault, fmt.Sprintf("APP-T-%04d", i), "backlog")
	}
	project := registerAutomationTestProject(t, vault)
	d, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.serve = newServeServer(vault, project.RepoRoot, defaultServeAddr, d.store, nil)
	d.stream = d.serve.stream
	defer d.stream.Close()
	now := time.Now().UTC()
	workRevision := intField(mustTaskData(t, vault, "APP-T-0001"), "work_revision")
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), WorkRevision: workRevision, UpdatedAt: now.Format(time.RFC3339),
	}
	if err := d.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	claimed, err := d.store.ClaimRunLease(project.ProjectID, run.RecordID, "agent:editor", 1, defaultRunLeaseTTL, now, true, true, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseStateUnclaimed, ExpectedWorkRevision: workRevision,
	})
	if err != nil || !claimed {
		t.Fatalf("interactive lease claim: claimed=%v err=%v", claimed, err)
	}
	assertLeaseHeld := func() {
		t.Helper()
		current, err := d.store.FindRunScoped(project.ProjectID, run.RecordID)
		if err != nil || current == nil || !current.HandRun || current.LeaseState != string(LeaseStateClaimed) || current.LeaseOwner != "agent:editor" || current.LeaseGeneration != 1 {
			t.Fatalf("scheduled maintenance changed live ownership: run=%#v err=%v", current, err)
		}
	}
	if err := d.runScheduledPoll(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	assertLeaseHeld()
	if _, err := d.serve.loadSnapshotForProject(project.ProjectID); err != nil {
		t.Fatal(err)
	}
	// Force an observable timestamp change without sleeping for a wall-clock
	// second. The scheduled task loop updates this runtime row on the next tick.
	current, err := d.store.FindRunScoped(project.ProjectID, run.RecordID)
	if err != nil || current == nil {
		t.Fatalf("load live lease: run=%#v err=%v", current, err)
	}
	current.UpdatedAt = now.Add(-time.Minute).Format(time.RFC3339)
	if err := d.store.UpsertRun(*current); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe, ok := d.stream.Subscribe()
	if !ok {
		t.Fatal("subscribe to live poll events")
	}
	defer unsubscribe()
	var walks, reads atomic.Int64
	previousList, previousRead := noteCacheListObserver, noteCacheReadObserver
	noteCacheListObserver = func() { walks.Add(1) }
	noteCacheReadObserver = func() { reads.Add(1) }
	t.Cleanup(func() { noteCacheListObserver, noteCacheReadObserver = previousList, previousRead })
	if err := d.runScheduledPoll(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	assertLeaseHeld()
	if _, err := d.serve.loadSnapshotForProject(project.ProjectID); err != nil {
		t.Fatal(err)
	}
	if walks.Load() != 0 || reads.Load() > 1 {
		t.Fatalf("unchanged scheduled live tick scanned the vault: walks=%d reads=%d", walks.Load(), reads.Load())
	}
	for len(events) > 0 {
		event := <-events
		if containsString(event.Keys, "projects") || event.Kind == serveStreamKindLeaseTransition {
			t.Fatalf("timestamp update emitted broad metadata/lease event: %#v", event)
		}
	}

	// A direct filesystem write has no CLI event or write-generation change.
	writeReconcileResourceTask(t, vault, "APP-T-0099", "backlog")
	d.frontierScannedAt[project.ProjectID] = time.Now().UTC().Add(-reconcileFullCadence)
	if err := d.runScheduledPoll(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	assertLeaseHeld()
	if walks.Load() == 0 {
		t.Fatal("aged scheduled tick skipped the external-edit fallback")
	}
	if got := d.pollTaskMeta[project.ProjectID+"\x00APP-T-0099"]; got.TaskID != "APP-T-0099" || got.Status != "backlog" {
		t.Fatalf("scheduled full fallback missed external record: %#v", got)
	}
	snap, err := d.serve.loadSnapshotForProject(project.ProjectID)
	if err != nil || stringField(snap.notesByID["APP-T-0099"].Data, "id") != "APP-T-0099" {
		t.Fatalf("full fallback left Serve metadata stale: err=%v", err)
	}
}

func TestDaemonRuntimeStreamPreservesMetadataAndTaskStatusInvalidates(t *testing.T) {
	broker := newServeStreamBroker()
	defer broker.Close()
	entry := &serveSnapshotEntry{project: RegisteredProject{ProjectID: "app"}, ready: true}
	server := &serveServer{snapshots: map[string]*serveSnapshotEntry{"app": entry}, stream: broker}
	d := &Daemon{serve: server, stream: broker}
	before := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", LeaseState: string(LeaseStateRunning), UpdatedAt: "2026-07-15T06:00:00Z"}
	after := before
	after.UpdatedAt, after.LastHeartbeatAt = "2026-07-15T06:00:05Z", "2026-07-15T06:00:05Z"
	d.emitLeaseTransitionStreamEvent(before, after)
	if entry.invalid || !entry.runtimeInvalid {
		t.Fatalf("heartbeat invalidation: metadata=%v runtime=%v", entry.invalid, entry.runtimeInvalid)
	}
	entry.runtimeInvalid = false
	after.LeaseState = string(LeaseStateReleased)
	after.AttemptOutcome = string(AttemptOutcomeSucceeded)
	d.emitLeaseTransitionStreamEvent(before, after)
	if entry.invalid || !entry.runtimeInvalid {
		t.Fatalf("lease invalidation: metadata=%v runtime=%v", entry.invalid, entry.runtimeInvalid)
	}
	d.observeTaskStatusForStream("app", "APP-T-0001", "ready")
	d.observeTaskStatusForStream("app", "APP-T-0001", "review")
	if !entry.invalid {
		t.Fatal("canonical task status change did not invalidate metadata")
	}
}

func TestFrontierMaintenanceReloadsChangedActiveTaskAndRejectsMissingTask(t *testing.T) {
	vault := t.TempDir()
	path := writeReconcileResourceTask(t, vault, "APP-T-0001", "in-progress")
	notes, err := listOperationalNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{}
	d.rebuildFrontier("app", notes)
	project := RegisteredProject{ProjectID: "app", VaultRoot: vault}
	runs := []RunStatus{{ProjectID: "app", RecordID: "APP-T-0001", LeaseState: string(LeaseStateRunning)}}
	writeReconcileResourceTask(t, vault, "APP-T-0001", "review")
	got, ok := d.frontierMaintenanceNotes(project, defaultWorkflow(), runs, time.Now().UTC())
	if !ok || len(got) != 1 || stringField(got[0].Data, "status") != "review" || !d.pollFrontierChanged {
		t.Fatalf("active task edit missed: ok=%v notes=%#v changed=%v", ok, got, d.pollFrontierChanged)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.frontierMaintenanceNotes(project, defaultWorkflow(), runs, time.Now().UTC()); ok {
		t.Fatal("missing active task did not force canonical full reconciliation")
	}
}

func TestHintedMaintenanceHonorsExternalFallbackAndShallowWake(t *testing.T) {
	vault := t.TempDir()
	writeReconcileResourceTask(t, vault, "APP-T-0001", "in-progress")
	notes, err := listOperationalNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{}
	d.rebuildFrontier("app", notes)
	project := RegisteredProject{ProjectID: "app", VaultRoot: vault}
	hint := daemonControlChange{ID: "APP-T-0001", Kind: "run"}
	if _, ok := d.applyFrontierHint(project, []daemonControlChange{hint}); !ok {
		t.Fatal("fresh runtime hint did not reuse the frontier")
	}
	d.frontierScannedAt["app"] = time.Now().UTC().Add(-reconcileFullCadence)
	if _, ok := d.applyFrontierHint(project, []daemonControlChange{hint}); ok {
		t.Fatal("continuous notifications bypassed the external-edit fallback")
	}
	d.frontierScannedAt["app"] = time.Now().UTC()
	d.recordFrontierHint("app", []daemonControlChange{hint})
	d.recordFrontierHint("app", nil)
	d.recordFrontierHint("app", []daemonControlChange{hint})
	if _, ok := d.applyFrontierHint(project, d.takeFrontierHint("app")); ok {
		t.Fatal("precise hints masked a coalesced project-only wake")
	}
}

func TestTerminalHumanWaitSkipsOrdinaryReviewsAndSharesIndex(t *testing.T) {
	vault := t.TempDir()
	var loads int
	previous := loadV7IndexObserver
	loadV7IndexObserver = func() { loads++ }
	t.Cleanup(func() { loadV7IndexObserver = previous })
	check := newV7TerminalHumanWaitChecker(vault)
	for range 20 {
		if check(Note{Data: map[string]any{"schema": "tusker.task/v7", "kind": "task", "id": "APP-T-0001", "status": "review"}}) {
			t.Fatal("ordinary review became a human wait")
		}
	}
	if loads != 0 {
		t.Fatalf("ordinary reviews loaded the full note index %d times", loads)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		if check(Note{Data: map[string]any{"schema": "tusker.task/v7", "kind": "task", "id": id, "closeout_status": "machine_complete_waiting_for_human"}}) {
			t.Fatal("missing closeout certified a human wait")
		}
	}
	if loads != 1 {
		t.Fatalf("matching closeouts loaded the project index %d times, want 1", loads)
	}
}

func TestRunnerStatusNotificationTargetsOnlyItsProject(t *testing.T) {
	previous := daemonControlOneWaySender
	defer func() { daemonControlOneWaySender = previous }()
	var requests []daemonControlRequest
	daemonControlOneWaySender = func(_ string, req daemonControlRequest, timeout time.Duration) error {
		if timeout != 250*time.Millisecond {
			t.Fatalf("notification timeout = %s", timeout)
		}
		requests = append(requests, req)
		return nil
	}
	notifyDaemonRunStatus(StartRequest{})
	notifyDaemonRunStatus(StartRequest{ProjectID: "app", RecordID: "APP-T-0001"})
	if len(requests) != 1 || requests[0].Cause != "runner_status" || requests[0].ProjectID != "app" || !daemonControlChangesRuntimeOnly(requests[0].Changes) {
		t.Fatalf("runner notifications = %#v", requests)
	}
}

func writeReconcileResourceTask(t *testing.T, vault, id, status string) string {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := serializeDocument(map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": id, "status": status,
		"state_rev": status, "work_revision": 1,
	}, "\n# Task contract\n", v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExternalFallbackDoesNotRelaxDaemonWatchdog(t *testing.T) {
	now := time.Now().UTC()
	d := &Daemon{}
	d.recordProjectPoll("idle", now, false)
	if cadence := d.adaptiveWatchdogCadence(); cadence != reconcileHotCadence {
		t.Fatalf("idle fallback relaxed watchdog cadence: %s", cadence)
	}
	d.recordProjectPoll("live", now, true)
	if cadence := d.adaptiveWatchdogCadence(); cadence != reconcileLiveCadence {
		t.Fatalf("active watchdog cadence = %s", cadence)
	}
}
