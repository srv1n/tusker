package main

import (
	"strings"
	"testing"
)

func TestCodexExecutionAdapterLocalThreadAndChildPreserveResumeIdentity(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := CodexExecutionAdapter{Store: store}
	result, err := adapter.Observe(CodexExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, ThreadID: "thread-parent", SourceEventID: "native-child-start", SourceSequence: 4, Status: "running", OccurredAt: "2026-07-29T12:00:00Z", ChildID: "child-provider-id", AgentType: "reviewer", Label: "Check migration"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChildCreated || result.ChildExecutionID == "" || !result.CheckpointAdvanced {
		t.Fatalf("result=%#v", result)
	}
	view, err := store.ExecutionView(parent.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ProviderSessionID != "thread-parent" || view.SessionRef != "thread-parent" {
		t.Fatalf("parent identity=%#v", view)
	}
	child, err := store.Execution(result.ChildExecutionID)
	if err != nil || child == nil || child.ProviderChildHandle != "child-provider-id" || child.AgentType != "reviewer" || child.ParentExecutionID != parent.ExecutionID {
		t.Fatalf("child=%#v err=%v", child, err)
	}
	if _, err := adapter.Observe(CodexExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, ThreadID: "thread-parent", SourceEventID: "native-child-start", SourceSequence: 4, Status: "running", OccurredAt: "2026-07-29T12:00:00Z", ChildID: "forged-child"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE node_kind = 'provider_child'`, nil, &count); err != nil || count != 1 {
		t.Fatalf("child count=%d err=%v", count, err)
	}
}

func TestCodexExecutionAdapterCloudDoesNotInventLocalProcessFacts(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "codex_cloud", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := CodexExecutionAdapter{Store: store}
	results, err := adapter.ObserveCloud("project-1", root.ExecutionID, "cloud-task-1", "env-prod", "running", "2026-07-29T12:00:00Z", 7, "cloud-cursor-7", []CodexExecutionObservation{{SourceEventID: "cloud-child-1", ChildID: "child-1", AgentType: "explorer", Label: "Map seams", Status: "running"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[1].ChildExecutionID == "" {
		t.Fatalf("results=%#v", results)
	}
	var metadata string
	if err := store.queryRowScan(`SELECT metadata_json FROM provider_execution_observations WHERE observation_id = ?`, []any{results[0].ObservationID}, &metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata, "env-prod") || strings.Contains(metadata, "pid") || strings.Contains(metadata, "heartbeat") {
		t.Fatalf("cloud metadata=%s", metadata)
	}
	var runs int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &runs); err != nil || runs != 0 {
		t.Fatalf("cloud observation wrote runs=%d err=%v", runs, err)
	}
}

func TestCodexExecutionAdapterMalformedAndOpaqueDeliveryAreSafe(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := CodexExecutionAdapter{Store: store}
	if _, err := adapter.Observe(CodexExecutionObservation{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, ThreadID: "thread-1", Status: "running", OccurredAt: "not-a-time"}); err == nil {
		t.Fatal("malformed timestamp succeeded")
	}
	var attachments int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_attachment_events WHERE execution_id = ?`, []any{root.ExecutionID}, &attachments); err != nil || attachments != 0 {
		t.Fatalf("malformed hook changed attachment state=%d err=%v", attachments, err)
	}
	result, err := adapter.Observe(CodexExecutionObservation{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, ThreadID: "thread-1", Status: "provider-regression", OccurredAt: "2026-07-29T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Degraded || result.DegradedReason == "" {
		t.Fatalf("opaque event should be degraded: %#v", result)
	}
}

func TestCodexExecutionAdapterFlagsStatusRegressionAndRejectsCloudProcessMetadata(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "codex_cloud", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := CodexExecutionAdapter{Store: store}
	if _, err := adapter.Observe(CodexExecutionObservation{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, ThreadID: "cloud-1", SourceEventID: "done", Status: "completed", OccurredAt: "2026-07-29T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	regression, err := adapter.Observe(CodexExecutionObservation{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, ThreadID: "cloud-1", SourceEventID: "late-running", Status: "running", OccurredAt: "2026-07-29T12:01:00Z"})
	if err != nil || !regression.Degraded || regression.DegradedReason != "provider_status_regression_requires_authoritative_fetch" {
		t.Fatalf("regression=%#v err=%v", regression, err)
	}
	_, err = adapter.ObserveCloud("project-1", root.ExecutionID, "cloud-2", "env", "running", "2026-07-29T12:00:00Z", 1, "c", []CodexExecutionObservation{{ChildID: "bad", Metadata: map[string]any{"nested": map[string]any{"process_pid": 9}}}})
	if errorToIssue(err).Code != providerObservationRefused {
		t.Fatalf("cloud process metadata error=%v", err)
	}
}

func TestCodexExecutionAdapterRunPayloadWiresExecAndCloudPolls(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	local, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: local.ExecutionID, Provider: "codex", ProviderSessionID: "thread-parent", SessionRef: "thread-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	adapter := CodexExecutionAdapter{Store: store}
	run := RunStatus{ProjectID: "project-1", Runner: string(RunnerCodexExec), SessionRef: "thread-parent"}
	changed, err := adapter.ObserveRunPayload(run, map[string]any{"type": "subagent.started", "child_id": "child-9", "agent_type": "reviewer", "label": "Review", "status": "running", "timestamp": "2026-07-29T12:00:00Z"}, 1, "codex_exec_jsonl")
	if err != nil || !changed {
		t.Fatalf("exec wiring changed=%t err=%v", changed, err)
	}
	changed, err = adapter.ObserveRunPayload(run, map[string]any{"type": "subagent.started", "child_id": "child-9", "agent_type": "reviewer", "label": "Review", "status": "running", "timestamp": "2026-07-29T12:00:00Z"}, 1, "codex_exec_jsonl")
	if err != nil || changed {
		t.Fatalf("exec replay changed=%t err=%v", changed, err)
	}
	cloud, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "codex_cloud", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: cloud.ExecutionID, Provider: "codex", ProviderSessionID: "cloud-task-1", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	changed, err = adapter.ObserveRunPayload(RunStatus{ProjectID: "project-1", Runner: string(RunnerCodexCloud), CloudTaskID: "cloud-task-1"}, map[string]any{"task_id": "cloud-task-1", "status": "running", "environment_id": "env-prod"}, 2, "authoritative_codex_cloud_poll")
	if err != nil || !changed {
		t.Fatalf("cloud wiring changed=%t err=%v", changed, err)
	}
	var metadata, reason string
	if err := store.queryRowScan(`SELECT metadata_json, degraded_reason FROM provider_execution_observations WHERE parent_provider_session_id = 'cloud-task-1'`, nil, &metadata, &reason); err != nil || !strings.Contains(metadata, "env-prod") || reason != "provider_timestamp_missing_requires_authoritative_fetch" {
		t.Fatalf("cloud poll metadata=%q reason=%q err=%v", metadata, reason, err)
	}
	app, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: app.ExecutionID, Provider: "codex", ProviderSessionID: "app-thread", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	handle := &codexLiveHandle{runtimeStore: store, projectID: "project-1", attemptID: "app-attempt", threadID: "app-thread", rawLogPath: t.TempDir() + "/app.log"}
	handle.observeExecutionNotification("subagent/started", []byte(`{"child_id":"app-child","agent_type":"reviewer","status":"running","timestamp":"2026-07-29T12:00:00Z"}`))
	var appChildren int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE provider_child_handle = 'app-child'`, nil, &appChildren); err != nil || appChildren != 1 {
		t.Fatalf("app notification children=%d err=%v", appChildren, err)
	}
}
