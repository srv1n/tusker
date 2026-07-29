package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecutionLifecycleDimensions(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: root.ExecutionID, Provider: "codex", ProviderSessionID: "thread-lifecycle", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	e := providerEvent(1, "lifecycle-provider")
	e.ParentProviderSessionID = "thread-lifecycle"
	e.Status = "running"
	child, err := store.ApplyProviderExecutionEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := store.ExecutionLifecycle(child.ChildExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if facts.DeliveryState != "unbound" || facts.ProviderState != "unavailable" || facts.OutcomeState != "unknown" || facts.DerivedPhase == "" {
		t.Fatalf("separate lifecycle facts=%#v", facts)
	}
	var persisted int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_lifecycle_evidence WHERE execution_id=?`, []any{child.ChildExecutionID}, &persisted); err != nil || persisted == 0 {
		t.Fatalf("persisted lifecycle=%d err=%v", persisted, err)
	}
	testExecutionProviderTrueCapabilityWithoutRouteStaysUnavailable(t)
}

func TestExecutionCancellationManagedPIDFence(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	pid := os.Getpid()
	run := RunStatus{ProjectID: "project-1", RecordID: "MANAGED-T-1", ItemID: "MANAGED-T-1", ActiveAttemptID: "attempt-fenced", LeaseGeneration: 7, LeaseState: string(LeaseStateRunning), ProcessPID: pid, ProcessPGID: processGroupID(pid), ProcessStartedAt: "1970-01-01T00:00:00Z", UpdatedAt: executionNow()}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	managed, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "MANAGED-T-1", AttemptID: "attempt-fenced", LeaseGeneration: 7, Source: "managed", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	control, err := store.RequestExecutionCancellation(managed.ExecutionID, "pid-reuse")
	if err == nil || !control.Available {
		t.Fatalf("PID reuse must refuse before signal: control=%#v err=%v", control, err)
	}
	var wrapperPID, generation int
	if err := store.queryRowScan(`SELECT process_pid, lease_generation FROM execution_cancellation_evidence WHERE execution_id=? AND request_key=? AND stage='wrapper_signal'`, []any{managed.ExecutionID, "pid-reuse"}, &wrapperPID, &generation); err != nil || wrapperPID != pid || generation != 7 {
		t.Fatalf("wrapper fence=%d/%d err=%v", wrapperPID, generation, err)
	}
	var escalations, settlements int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=? AND request_key=? AND stage='escalation'`, []any{managed.ExecutionID, "pid-reuse"}, &escalations); err != nil || escalations != 1 {
		t.Fatalf("escalation=%d err=%v", escalations, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=? AND request_key=? AND stage='os_settled'`, []any{managed.ExecutionID, "pid-reuse"}, &settlements); err != nil || settlements != 0 {
		t.Fatalf("false settlement=%d err=%v", settlements, err)
	}
}

func TestExecutionCancellationEvidenceIsIdempotentAndProviderSafe(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: root.ExecutionID, Provider: "claude", ProviderSessionID: "thread-cancel", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	control, err := store.RequestExecutionCancellation(root.ExecutionID, "same-request")
	if err != nil || control.Available {
		t.Fatalf("unproved provider cancellation=%#v err=%v", control, err)
	}
	if _, err = store.RequestExecutionCancellation(root.ExecutionID, "same-request"); err != nil {
		t.Fatal(err)
	}
	var requests, stages int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=? AND request_key=? AND stage='requested'`, []any{root.ExecutionID, "same-request"}, &requests); err != nil || requests != 1 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=? AND request_key=?`, []any{root.ExecutionID, "same-request"}, &stages); err != nil || stages < 5 {
		t.Fatalf("stages=%d err=%v", stages, err)
	}
	TestExecutionCancellationManagedPIDFence(t)
}

func testExecutionProviderTrueCapabilityWithoutRouteStaysUnavailable(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: root.ExecutionID, Provider: "codex", ProviderSessionID: "route-check", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = store.ApplyProviderExecutionEvent(ProviderExecutionEventEnvelope{Version: 1, ProjectID: "project-1", Provider: "codex", SourceEventID: "parent-cap", ParentProviderSessionID: "route-check", Status: "running", OccurredAt: now, Capabilities: []ProviderCapabilityFact{{Name: "parent_interrupt", State: "true", Provenance: "adapter-fetch", FreshAt: now}}})
	if err != nil {
		t.Fatal(err)
	}
	control, err := store.ExecutionControlAvailability(root.ExecutionID)
	if err != nil || control.Available || control.Target != "provider" || !strings.Contains(control.Reason, "no target-specific control route") {
		t.Fatalf("control=%#v err=%v", control, err)
	}
}

func TestExecutionLifecycleRecovery(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	if _, err := adapter.Observe(ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "restart-parent", SourceEventID: "child-start", SourceSequence: 1, Kind: "SubagentStart", Status: "running", OccurredAt: "2026-07-29T12:00:00Z", ChildID: "detached", AgentType: "worker"}); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Observe(ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "restart-parent", SourceEventID: "parent-exit", SourceSequence: 2, Status: "completed", OccurredAt: "2026-07-29T12:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var childID string
	if err := store.queryRowScan(`SELECT child_execution_id FROM provider_execution_observations WHERE child_handle='detached' LIMIT 1`, nil, &childID); err != nil {
		t.Fatal(err)
	}
	facts, err := store.ExecutionLifecycle(childID)
	if err != nil {
		t.Fatal(err)
	}
	if facts.OutcomeState != "unknown" {
		t.Fatalf("parent exit forged child outcome: %#v", facts)
	}
}
func TestExecutionCancellationEvidence(t *testing.T) {
	TestExecutionCancellationEvidenceIsIdempotentAndProviderSafe(t)
}
