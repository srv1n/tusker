package main

import (
	"os"
	"testing"
	"time"
)

func TestExecutionControlHistoricalAttemptCannotTargetRetry(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex"})
	if err != nil {
		t.Fatal(err)
	}
	first := RunStatus{ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:a1", ActiveAttemptID: "attempt-a1", LeaseGeneration: 1, WorkRevision: 4}
	if err := store.UpsertRun(first); err != nil {
		t.Fatal(err)
	}
	historical, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-1", AttemptID: "attempt-a1", LeaseGeneration: 1, Source: "managed"})
	if err != nil {
		t.Fatal(err)
	}
	retry := first
	retry.LeaseOwner, retry.ActiveAttemptID, retry.LeaseGeneration = "agent:a2", "attempt-a2", 2
	if err := store.UpsertRun(retry); err != nil {
		t.Fatal(err)
	}
	view, err := store.ExecutionView(historical.ExecutionID)
	if err != nil || view == nil {
		t.Fatalf("historical view=%#v err=%v", view, err)
	}
	if run, err := store.executionGraphRun(*view); err != nil || run != nil {
		t.Fatalf("historical attempt resolved a mutable retry: %#v err=%v", run, err)
	}
	control, err := store.RequestExecutionCancellation(historical.ExecutionID, "historical-a1")
	if err != nil || control.Available {
		t.Fatalf("historical cancellation must be unavailable: %#v err=%v", control, err)
	}
	current, err := store.FindRun("TASK-1")
	if err != nil || current == nil || current.ActiveAttemptID != "attempt-a2" || current.LeaseGeneration != 2 || current.LeaseOwner != "agent:a2" {
		t.Fatalf("historical cancellation changed retry: %#v err=%v", current, err)
	}
}

func TestManualReclaimRequiresExactDeadSnapshot(t *testing.T) {
	store := executionLedgerStore(t)
	now := time.Now().UTC()
	dead := RunStatus{ProjectID: "project-1", RecordID: "TASK-RECLAIM", ItemID: "TASK-RECLAIM", LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:a1", ActiveAttemptID: "attempt-a1", LeaseGeneration: 7, WorkRevision: 3, ProcessPID: 99999999, ProcessPGID: 99999999, ProcessStartedAt: "dead-process", LeaseExpiresAt: now.Add(-3 * defaultRunLeaseTTL).Format(time.RFC3339)}
	if err := store.UpsertRun(dead); err != nil {
		t.Fatal(err)
	}
	stale := dead
	retry := dead
	retry.LeaseOwner, retry.ActiveAttemptID, retry.LeaseGeneration = "agent:a2", "attempt-a2", 8
	if err := store.UpsertRun(retry); err != nil {
		t.Fatal(err)
	}
	if reclaimed, err := store.ReclaimExpiredRunLeaseIfSnapshot(stale, now, defaultRunLeaseTTL, "operator reclaim"); err != nil || reclaimed {
		t.Fatalf("stale reclaim must not clear replacement: reclaimed=%v err=%v", reclaimed, err)
	}
	current, err := store.FindRun(dead.RecordID)
	if err != nil || current == nil || current.ActiveAttemptID != "attempt-a2" || current.LeaseGeneration != 8 {
		t.Fatalf("stale reclaim mutated replacement: %#v err=%v", current, err)
	}

	pid := os.Getpid()
	live := dead
	live.RecordID, live.ItemID, live.LeaseOwner, live.ActiveAttemptID, live.LeaseGeneration = "TASK-LIVE", "TASK-LIVE", "agent:live", "attempt-live", 9
	live.ProcessPID, live.ProcessPGID = pid, processGroupID(pid)
	live.ProcessStartedAt = recordedProcessStartTime(pid, "")
	if live.ProcessStartedAt == "" {
		t.Skip("process start identity unavailable on this host")
	}
	if err := store.UpsertRun(live); err != nil {
		t.Fatal(err)
	}
	if reclaimed, err := store.ReclaimExpiredRunLeaseIfSnapshot(live, now, defaultRunLeaseTTL, "operator reclaim"); err != nil || reclaimed {
		t.Fatalf("live holder reclaim must refuse: reclaimed=%v err=%v", reclaimed, err)
	}
}

func TestExecutionLifecycleChildAttentionClearsOnRecovery(t *testing.T) {
	store := executionLedgerStore(t)
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	failed := providerEvent(1, "attention-failed")
	failed.Status = "failed"
	if _, err := store.ApplyProviderExecutionEvent(failed); err != nil {
		t.Fatal(err)
	}
	if lifecycle, err := store.ExecutionLifecycle(parent.ExecutionID); err != nil || lifecycle.ChildAttentionState != "needs_attention" {
		t.Fatalf("failed child attention=%#v err=%v", lifecycle, err)
	}
	recovered := providerEvent(2, "attention-recovered")
	recovered.Status = "running"
	if _, err := store.ApplyProviderExecutionEvent(recovered); err != nil {
		t.Fatal(err)
	}
	if lifecycle, err := store.ExecutionLifecycle(parent.ExecutionID); err != nil || lifecycle.ChildAttentionState != "none" {
		t.Fatalf("recovered child remained attention-worthy: %#v err=%v", lifecycle, err)
	}
	var historicalFailures int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE child_execution_id != '' AND status='failed'`, nil, &historicalFailures); err != nil || historicalFailures != 1 {
		t.Fatalf("recovery deleted historical evidence: failures=%d err=%v", historicalFailures, err)
	}
}
