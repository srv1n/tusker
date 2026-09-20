package main

import (
	"context"
	"database/sql"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWorkerAttentionIdenticalEvaluationIsNoOp(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	if err := store.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "test",
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1", LeaseGeneration: 1, WorkRevision: 1,
		StartedAt: "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	first, err := store.EvaluateWorkerAttention(identity, time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AttentionRequired {
		t.Fatal("old baseline must require attention")
	}
	second, err := store.EvaluateWorkerAttention(identity, time.Now().UTC().Add(time.Hour), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.EvaluatedAt != first.EvaluatedAt {
		t.Fatalf("identical evaluation must not rewrite evaluated_at: %q vs %q", first.EvaluatedAt, second.EvaluatedAt)
	}
	persisted, found, err := store.loadWorkerAttention(identity.ProjectID, identity.AttemptID, identity.AttemptGeneration)
	if err != nil || !found {
		t.Fatal(err)
	}
	if persisted.EvaluatedAt != first.EvaluatedAt {
		t.Fatal("persisted evaluated_at must be unchanged on repeat evaluation")
	}
	if _, _, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity: identity, ProviderEventID: "evt-live", Source: "devin", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	third, err := store.EvaluateWorkerAttention(identity, time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if third.AttentionRequired {
		t.Fatal("provider activity must clear attention")
	}
	if third.EvaluatedAt == first.EvaluatedAt {
		t.Fatal("a material transition must update evaluated_at")
	}
}

func TestWorkerAttentionConcurrentTransitionWritesOnce(t *testing.T) {
	stateRoot := t.TempDir()
	storeA, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer storeA.Close()
	identity := seedWorkerRun(t, storeA, "attempt-1", 1, 1, "devin", "devin-1")
	if err := storeA.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "test",
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1", LeaseGeneration: 1, WorkRevision: 1,
		StartedAt: "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	initial, err := storeA.EvaluateWorkerAttention(identity, time.Now().UTC(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !initial.AttentionRequired {
		t.Fatal("initial evaluation must persist the silent baseline")
	}
	storeB, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer storeB.Close()
	transition := initial
	transition.AttentionRequired = false
	transition.LastActivityAt = time.Now().UTC().Format(time.RFC3339Nano)
	transition.SilenceSince = transition.LastActivityAt
	transition.RecommendedAction = ""
	stores := []*RuntimeStore{storeA, storeB}
	evaluated := []string{
		time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
		time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339Nano),
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := range stores {
		go func(store *RuntimeStore, evaluatedAt string) {
			<-start
			candidate := transition
			candidate.EvaluatedAt = evaluatedAt
			_, err := store.saveWorkerAttention(candidate)
			errs <- err
		}(stores[i], evaluated[i])
	}
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	persisted, found, err := storeA.loadWorkerAttention(identity.ProjectID, identity.AttemptID, identity.AttemptGeneration)
	if err != nil || !found {
		t.Fatal(err)
	}
	if persisted.AttentionRequired || persisted.LastActivityAt != transition.LastActivityAt {
		t.Fatalf("persisted material must reflect the transition: %#v", persisted)
	}
	if persisted.EvaluatedAt != evaluated[0] && persisted.EvaluatedAt != evaluated[1] {
		t.Fatalf("persisted evaluated_at must be one racer's value, got %q", persisted.EvaluatedAt)
	}
	winner := persisted.EvaluatedAt
	identical := transition
	identical.EvaluatedAt = time.Now().UTC().Add(3 * time.Hour).Format(time.RFC3339Nano)
	repeated, err := storeB.saveWorkerAttention(identical)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.EvaluatedAt != winner {
		t.Fatalf("identical transition must preserve the winning evaluated_at %q, got %q", winner, repeated.EvaluatedAt)
	}
}

func TestWorkerDeliveryLegacyCheckMigratesOnFastPath(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", runtimeStoreSQLiteDSN(runtimeStoreDBPath(stateRoot), runtimeStoreBusyTimeout))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacy := []string{
		`ALTER TABLE worker_deliveries RENAME TO worker_deliveries_new`,
		`CREATE TABLE worker_deliveries (
			delivery_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL,
			project_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			work_revision INTEGER NOT NULL,
			attempt_id TEXT NOT NULL,
			attempt_generation INTEGER NOT NULL,
			provider TEXT NOT NULL,
			native_session_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('question','instruction','answer')),
			body TEXT NOT NULL,
			state TEXT NOT NULL CHECK(state IN ('stored','accepted','uncertain','replied','applied','unsupported','stale')),
			stored_at TEXT NOT NULL,
			provider_accepted_at TEXT NOT NULL DEFAULT '',
			worker_activity_observed_at TEXT NOT NULL DEFAULT '',
			correlated_reply_event_id TEXT NOT NULL DEFAULT '',
			correlated_reply_at TEXT NOT NULL DEFAULT '',
			decision_applied_at TEXT NOT NULL DEFAULT '',
			provider_receipt_json TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, attempt_id, attempt_generation, idempotency_key)
		)`,
		`INSERT INTO worker_deliveries SELECT * FROM worker_deliveries_new`,
		`DROP TABLE worker_deliveries_new`,
		`DROP INDEX IF EXISTS worker_deliveries_reply_event`,
	}
	for _, stmt := range legacy {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("legacy schema step %q: %v", stmt, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	identity := seedWorkerRun(t, reopened, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := reopened.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k-legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := reopened.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatalf("fast-path reopen must rebuild the delivering CHECK and claim: claimed=%v err=%v", claimed, err)
	}
}

func TestDaemonWorkerAttentionOperatorFlow(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Worker watch", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	workflowFile := workflowPath(vault)
	raw, err := readText(workflowFile)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(raw, "\n")
	injected := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "visibility_interval_ms:") {
			lines[i] = line[:len(line)-len(strings.TrimLeft(line, " "))] + "visibility_interval_ms: 60000"
			injected = true
			break
		}
	}
	if !injected {
		t.Fatal("could not locate visibility_interval_ms in WORKFLOW.md")
	}
	if err := writeText(workflowFile, strings.Join(lines, "\n")); err != nil {
		t.Fatal(err)
	}
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	stateRoot := DefaultStateRoot()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sleeper := exec.Command("sleep", "300")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_, _ = sleeper.Process.Wait()
	})
	pid := sleeper.Process.Pid
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexAppServer), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1",
		LeaseGeneration: 1, WorkRevision: 1, AttemptCount: 1,
		LeaseOwner:       "attempt-1",
		ProcessPID:       pid,
		ProcessPGID:      processGroupID(pid),
		ProcessStartedAt: recordedProcessStartTime(pid, now.Format(time.RFC3339)),
		StartedAt:        now.Add(-time.Hour).Format(time.RFC3339Nano),
		UpdatedAt:        now.Format(time.RFC3339),
		LastHeartbeatAt:  now.Format(time.RFC3339),
		LastEventAt:      now.Format(time.RFC3339),
		LeaseExpiresAt:   now.Add(time.Hour).Format(time.RFC3339),
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: project.ProjectID, DisplayName: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedExecution(ManagedExecutionInput{
		ProjectID: project.ProjectID, ParentExecutionID: root.ExecutionID, TaskID: "APP-T-0001",
		AttemptID: "attempt-1", LeaseGeneration: 1, Provider: "devin", ProviderSessionID: "devin-1", Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	daemon, err := NewDaemon(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollProjectOnce(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	current, err := daemon.store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || current == nil {
		t.Fatalf("run must survive the poll: %#v err=%v", current, err)
	}
	if current.LeaseState != string(LeaseStateRunning) || current.ActiveAttemptID != "attempt-1" {
		t.Fatalf("poll must not retire a fresh running lease: %#v", current)
	}
	attention, err := daemon.store.WorkerAttentionForRun(*current)
	if err != nil {
		t.Fatal(err)
	}
	if attention == nil || !attention.AttentionRequired {
		t.Fatalf("daemon poll must persist attention for a silent current worker, got %#v", attention)
	}
	t.Logf("attempt=%s generation=%d provider=%s session=%s attention_required=%t silence_since=%s",
		attention.Identity.AttemptID, attention.Identity.AttemptGeneration, attention.Identity.Provider, attention.Identity.NativeSessionID,
		attention.AttentionRequired, attention.SilenceSince)
	inspection, err := buildRunInspection(daemon.store, current)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Attention == nil || !inspection.Attention.AttentionRequired {
		t.Fatalf("runs inspect projection must surface attention, got %#v", inspection.Attention)
	}
	summary, err := (&serveServer{store: daemon.store, now: time.Now}).runSummaryChecked(serveSnapshot{}, *current)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Attention == nil || !summary.Attention.AttentionRequired {
		t.Fatal("serve run summary must surface attention")
	}

	if _, _, err := daemon.store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity:        attention.Identity,
		ProviderEventID: "evt-live-1", Source: "devin",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.PollProjectOnce(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	cleared, err := daemon.store.WorkerAttentionForRun(*current)
	if err != nil {
		t.Fatal(err)
	}
	if cleared == nil || cleared.AttentionRequired {
		t.Fatalf("correlated provider activity must clear attention, got %#v", cleared)
	}
	t.Logf("activity cleared attention: evaluated_at=%s last_activity_at=%s", cleared.EvaluatedAt, cleared.LastActivityAt)
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewDaemon(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.store.WorkerAttentionForRun(*current)
	if err != nil {
		t.Fatal(err)
	}
	if persisted == nil || persisted.AttentionRequired {
		t.Fatal("cleared attention must survive daemon/store reopen")
	}

	if err := reopened.store.UpsertRun(RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexAppServer), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-2",
		LeaseGeneration: 2, WorkRevision: 1, AttemptCount: 2,
		LeaseOwner:       "attempt-2",
		ProcessPID:       pid,
		ProcessPGID:      processGroupID(pid),
		ProcessStartedAt: recordedProcessStartTime(pid, now.Format(time.RFC3339)),
		StartedAt:        now.Add(-time.Hour).Format(time.RFC3339Nano),
		UpdatedAt:        now.Format(time.RFC3339),
		LastHeartbeatAt:  now.Format(time.RFC3339),
		LastEventAt:      now.Format(time.RFC3339),
		LeaseExpiresAt:   now.Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	root2, err := reopened.store.CreateDirectExecution(DirectExecutionInput{ProjectID: project.ProjectID, DisplayName: "root-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.store.CreateManagedExecution(ManagedExecutionInput{
		ProjectID: project.ProjectID, ParentExecutionID: root2.ExecutionID, TaskID: "APP-T-0001",
		AttemptID: "attempt-2", LeaseGeneration: 2, Provider: "devin", ProviderSessionID: "devin-2", Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.PollProjectOnce(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	gen2Run, err := reopened.store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || gen2Run == nil {
		t.Fatal(err)
	}
	gen2, err := reopened.store.WorkerAttentionForRun(*gen2Run)
	if err != nil {
		t.Fatal(err)
	}
	if gen2 == nil || !gen2.AttentionRequired {
		t.Fatalf("silent generation 2 must require attention, got %#v", gen2)
	}
	t.Logf("generation 2 attempt=%s session=%s attention_required=%t", gen2.Identity.AttemptID, gen2.Identity.NativeSessionID, gen2.AttentionRequired)

	late, _, err := reopened.store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity:        WorkerAttemptIdentity{ProjectID: project.ProjectID, TaskID: "APP-T-0001", AttemptID: "attempt-1", Provider: "devin", NativeSessionID: "devin-1", WorkRevision: 1, AttemptGeneration: 1},
		ProviderEventID: "evt-late-gen1", Source: "devin",
		OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !late.Stale {
		t.Fatal("late generation-1 activity must be stored stale")
	}
	if err := reopened.PollProjectOnce(context.Background(), project.ProjectID); err != nil {
		t.Fatal(err)
	}
	gen2After, err := reopened.store.WorkerAttentionForRun(*gen2Run)
	if err != nil {
		t.Fatal(err)
	}
	if gen2After == nil || !gen2After.AttentionRequired {
		t.Fatal("stale generation-1 activity must not clear generation-2 attention")
	}
	t.Logf("late gen1 activity stayed stale; gen2 attention_required=%t", gen2After.AttentionRequired)
}
