package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeExecutionAdapterNamedSessionAndNativeChildPreserveResumeIdentity(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	start := ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "claude-parent", SourceEventID: "start-1", SourceSequence: 1, Kind: "SubagentStart", Status: "running", OccurredAt: "2026-07-29T12:00:00Z", ChildID: "child-1", AgentType: "reviewer", Label: "Check migration", TranscriptRef: "/tmp/child.jsonl"}
	result, err := adapter.Observe(start)
	if err != nil || !result.ChildCreated || result.ChildExecutionID == "" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	view, err := store.ExecutionView(parent.ExecutionID)
	if err != nil || view.SessionRef != "claude-parent" || view.ProviderSessionID != "claude-parent" {
		t.Fatalf("parent=%#v err=%v", view, err)
	}
	child, err := store.Execution(result.ChildExecutionID)
	if err != nil || child == nil || child.ProviderChildHandle != "child-1" || child.AgentType != "reviewer" || child.ParentExecutionID != parent.ExecutionID {
		t.Fatalf("child=%#v err=%v", child, err)
	}
	stop := start
	stop.SourceEventID, stop.SourceSequence, stop.Kind, stop.Status = "stop-1", 2, "SubagentStop", "completed"
	if _, err := adapter.Observe(stop); err != nil {
		t.Fatal(err)
	}
	var observations int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE provider = 'claude'`, nil, &observations); err != nil || observations != 2 {
		t.Fatalf("observations=%d err=%v", observations, err)
	}
}

func TestClaudeExecutionAdapterRunPayloadIsReplaySafeAndWiredToLiveIngress(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "claude", ProviderSessionID: "parent-1", SessionRef: "parent-1", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	payload := map[string]any{"hook_event_name": "SubagentStart", "session_id": "parent-1", "subagent_id": "child-1", "agent_type": "explorer", "name": "Map seams", "transcript_path": "/tmp/subagent.jsonl", "timestamp": "2026-07-29T12:00:00Z"}
	changed, err := adapter.ObserveRunPayload(RunStatus{ProjectID: "project-1", Runner: string(RunnerClaude), SessionRef: "parent-1"}, payload, 1, "claude_stream_json")
	if err != nil || !changed {
		t.Fatalf("changed=%t err=%v", changed, err)
	}
	changed, err = adapter.ObserveRunPayload(RunStatus{ProjectID: "project-1", Runner: string(RunnerClaude), SessionRef: "parent-1"}, payload, 1, "claude_stream_json")
	if err != nil || changed {
		t.Fatalf("replay changed=%t err=%v", changed, err)
	}
	h := &claudeLiveHandle{runtimeStore: store, projectID: "project-1", attemptID: "attempt-1", sessionRef: "parent-1", rawLogPath: t.TempDir() + "/claude.log"}
	h.observeExecutionPayload(map[string]any{"hook_event_name": "SubagentStart", "session_id": "parent-1", "subagent_id": "child-2", "agent_type": "reviewer", "timestamp": "2026-07-29T12:01:00Z"})
	var children int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE provider = 'claude' AND node_kind = 'provider_child'`, nil, &children); err != nil || children != 2 {
		t.Fatalf("children=%d err=%v", children, err)
	}
}

func TestClaudeExecutionAdapterMissingStartMalformedAndRegressionStayTruthful(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	if _, err := adapter.Observe(ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "parent-1", Status: "running", OccurredAt: "not-a-time"}); err == nil {
		t.Fatal("malformed timestamp succeeded")
	}
	missingStart, err := adapter.Observe(ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "parent-1", SourceEventID: "stop-without-start", Kind: "SubagentStop", Status: "completed", OccurredAt: "2026-07-29T12:00:00Z", ChildID: "lost-child", AgentType: "worker"})
	if err != nil || !missingStart.Degraded || !strings.Contains(missingStart.DegradedReason, "subagent_start_missing") {
		t.Fatalf("missing start=%#v err=%v", missingStart, err)
	}
	if _, err := adapter.Observe(ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "parent-1", SourceEventID: "parent-done", Status: "completed", OccurredAt: "2026-07-29T12:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	regression, err := adapter.Observe(ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "parent-1", SourceEventID: "late-running", Status: "running", OccurredAt: "2026-07-29T12:02:00Z"})
	if err != nil || !regression.Degraded || regression.DegradedReason != "provider_status_regression_requires_authoritative_fetch" {
		t.Fatalf("regression=%#v err=%v", regression, err)
	}
	var childObservations int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE child_execution_id != ''`, nil, &childObservations); err != nil || childObservations != 1 {
		t.Fatalf("parent exit forged child state: %d err=%v", childObservations, err)
	}
}

func TestClaudeExecutionAdapterCapabilitiesDoNotInventChildControls(t *testing.T) {
	capabilities := claudeDefaultCapabilities("2026-07-29T12:00:00Z")
	want := map[string]string{"parent_interrupt": "unknown", "independent_child_control": "false", "resume": "unknown", "enumeration": "false", "replay": "false"}
	for _, fact := range capabilities {
		if want[fact.Name] != fact.State || fact.Provenance == "" {
			t.Fatalf("capability=%#v", fact)
		}
	}
}

func TestClaudeExecutionAdapterParentTerminalReconcilesUnstoppedChildOnceAcrossRestart(t *testing.T) {
	state := filepath.Join(t.TempDir(), "runtime")
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	start := ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "parent-1", SourceEventID: "child-start", SourceSequence: 1, Kind: "SubagentStart", Status: "running", OccurredAt: "2026-07-29T12:00:00Z", ChildID: "unsettled-child", AgentType: "explorer"}
	if _, err := adapter.Observe(start); err != nil {
		t.Fatal(err)
	}
	terminal := ClaudeExecutionObservation{ProjectID: "project-1", ParentExecutionID: parent.ExecutionID, SessionID: "parent-1", SourceEventID: "parent-result", SourceSequence: 2, Status: "completed", OccurredAt: "2026-07-29T12:01:00Z"}
	if _, err := adapter.Observe(terminal); err != nil {
		t.Fatal(err)
	}
	assertClaudePartialChildDiagnostic(t, store, 1)
	if _, err := adapter.Observe(terminal); err != nil {
		t.Fatal(err)
	}
	assertClaudePartialChildDiagnostic(t, store, 1)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	adapter = ClaudeExecutionAdapter{Store: store}
	if _, err := adapter.Observe(terminal); err != nil {
		t.Fatal(err)
	}
	assertClaudePartialChildDiagnostic(t, store, 1)
	var leaseRows, outcomeRows int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &leaseRows); err != nil || leaseRows != 0 {
		t.Fatalf("reconciliation fabricated run rows=%d err=%v", leaseRows, err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE child_handle = 'unsettled-child' AND status IN ('terminal','failed','cancelled')`, nil, &outcomeRows); err != nil || outcomeRows != 0 {
		t.Fatalf("reconciliation fabricated child settlement=%d err=%v", outcomeRows, err)
	}
}

func TestClaudeExecutionAdapterStreamResultReconcilesUnstoppedChild(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "claude", ProviderSessionID: "stream-parent", SessionRef: "stream-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	run := RunStatus{ProjectID: "project-1", Runner: string(RunnerClaude), SessionRef: "stream-parent"}
	if _, err := adapter.ObserveRunPayload(run, map[string]any{"hook_event_name": "SubagentStart", "session_id": "stream-parent", "subagent_id": "unsettled-child", "timestamp": "2026-07-29T12:00:00Z"}, 1, "claude_stream_json"); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ObserveRunPayload(run, map[string]any{"type": "result", "session_id": "stream-parent", "subtype": "success", "timestamp": "2026-07-29T12:01:00Z"}, 2, "claude_stream_json"); err != nil {
		t.Fatal(err)
	}
	assertClaudePartialChildDiagnostic(t, store, 1)
}

func TestClaudeLiveFinalizeProcessExitWithoutResultReconcilesChild(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "claude", ProviderSessionID: "exit-parent", SessionRef: "exit-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	adapter := ClaudeExecutionAdapter{Store: store}
	if _, err := adapter.ObserveRunPayload(RunStatus{ProjectID: "project-1", Runner: string(RunnerClaude), SessionRef: "exit-parent"}, map[string]any{"hook_event_name": "SubagentStart", "session_id": "exit-parent", "subagent_id": "unsettled-child", "timestamp": "2026-07-29T12:00:00Z"}, 1, "claude_stream_json"); err != nil {
		t.Fatal(err)
	}
	h := &claudeLiveHandle{runtimeStore: store, projectID: "project-1", attemptID: "attempt-exit", sessionRef: "exit-parent", rawLogPath: t.TempDir() + "/claude.log"}
	h.finalize(1) // production process/EOF path: no type=result reached stdout.
	assertClaudePartialChildDiagnostic(t, store, 1)
	var synthetic int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE parent_provider_session_id = 'exit-parent' AND child_handle = '' AND degraded_reason = 'process_exit_without_result_requires_authoritative_fetch'`, nil, &synthetic); err != nil || synthetic != 1 {
		t.Fatalf("process-exit synthetic=%d err=%v", synthetic, err)
	}
	// The same process-exit receipt is idempotent if a watchdog/recovery path
	// invokes it again before the process record is discarded.
	h.observeProcessExitWithoutResult(1, "2026-07-29T12:02:00Z")
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE parent_provider_session_id = 'exit-parent' AND child_handle = '' AND degraded_reason = 'process_exit_without_result_requires_authoritative_fetch'`, nil, &synthetic); err != nil || synthetic != 1 {
		t.Fatalf("duplicate process-exit synthetic=%d err=%v", synthetic, err)
	}
}

func TestClaudeLiveFinalizeDoesNotAddProcessExitAfterSettledResult(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "claude", ProviderSessionID: "result-parent", SessionRef: "result-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	h := &claudeLiveHandle{runtimeStore: store, projectID: "project-1", attemptID: "attempt-result", sessionRef: "result-parent", rawLogPath: t.TempDir() + "/claude.log"}
	h.observeExecutionPayload(map[string]any{"type": "result", "session_id": "result-parent", "subtype": "success", "timestamp": "2026-07-29T12:00:00Z"})
	h.finalize(0)
	var synthetic int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE parent_provider_session_id = 'result-parent' AND degraded_reason = 'process_exit_without_result_requires_authoritative_fetch'`, nil, &synthetic); err != nil || synthetic != 0 {
		t.Fatalf("result path appended process-exit observation=%d err=%v", synthetic, err)
	}
}

func assertClaudePartialChildDiagnostic(t *testing.T, store *RuntimeStore, want int) {
	t.Helper()
	var got int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations WHERE child_handle = 'unsettled-child' AND status = 'unknown' AND degraded_reason = 'parent_terminal_before_child_stop_partial_or_lost'`, nil, &got); err != nil || got != want {
		t.Fatalf("partial diagnostic=%d want=%d err=%v", got, want, err)
	}
}
