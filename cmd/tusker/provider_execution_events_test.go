package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func providerObservationFixture(t *testing.T, state string) (*RuntimeStore, ExecutionRecord) {
	t.Helper()
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", SessionRef: "resume-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	return store, parent
}

func providerEvent(sequence int64, id string) ProviderExecutionEventEnvelope {
	return ProviderExecutionEventEnvelope{
		Version: providerObservationVersion, ProjectID: "project-1", Provider: "codex", SourceEventID: id, SourceSequence: sequence, SourceCursor: "cursor-" + id,
		ParentProviderSessionID: "thread-1", ChildHandle: "child-1", AgentType: "reviewer", Label: "Lease audit", Status: "running", OccurredAt: "2026-07-29T12:00:00Z",
		Capabilities: []ProviderCapabilityFact{{Name: "independent_stop", State: "unknown", Provenance: "codex hook", FreshAt: "2026-07-29T12:00:00Z"}},
		Metadata:     map[string]any{"authorization": "do-not-store", "safe": "value"},
	}
}

func TestProviderExecutionEventEnvelope(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", SessionRef: "resume-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	result, err := store.ApplyProviderExecutionEvent(providerEvent(1, "event-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.ChildCreated || !result.CheckpointAdvanced || !result.Degraded || result.ChildExecutionID == "" {
		t.Fatalf("unexpected ingestion result: %#v", result)
	}
	child, err := store.Execution(result.ChildExecutionID)
	if err != nil || child == nil || child.ParentExecutionID != parent.ExecutionID || child.ProviderChildHandle != "child-1" {
		t.Fatalf("child correlation=%#v err=%v", child, err)
	}
	var metadata, sessionRef string
	if err := store.queryRowScan(`SELECT metadata_json FROM provider_execution_observations WHERE observation_id = ?`, []any{result.ObservationID}, &metadata); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(metadata, "do-not-store") || !strings.Contains(metadata, "[REDACTED]") {
		t.Fatalf("metadata was not redacted: %s", metadata)
	}
	if err := store.queryRowScan(`SELECT session_ref FROM execution_attachment_events WHERE execution_id = ?`, []any{parent.ExecutionID}, &sessionRef); err != nil {
		t.Fatal(err)
	}
	if sessionRef != "resume-parent" {
		t.Fatalf("provider child overwrote parent resume identity: %q", sessionRef)
	}
}

func TestProviderExecutionEventReplay(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, _ := providerObservationFixture(t, state)
	first, err := store.ApplyProviderExecutionEvent(providerEvent(2, "event-2"))
	if err != nil || !first.CheckpointAdvanced {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	replay, err := store.ApplyProviderExecutionEvent(providerEvent(2, "event-2"))
	if err != nil || !replay.Duplicate || replay.ObservationID != first.ObservationID || replay.ChildCreated {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	late := providerEvent(1, "event-1")
	late.SourceCursor = "old-cursor"
	result, err := store.ApplyProviderExecutionEvent(late)
	if err != nil || result.CheckpointAdvanced {
		t.Fatalf("late=%#v err=%v", result, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var sequence int64
	var cursor string
	if err := store.queryRowScan(`SELECT source_sequence, source_cursor FROM provider_source_checkpoints WHERE project_id = 'project-1' AND provider = 'codex' AND parent_provider_session_id = 'thread-1'`, nil, &sequence, &cursor); err != nil {
		t.Fatal(err)
	}
	if sequence != 2 || cursor != "cursor-event-2" {
		t.Fatalf("restart checkpoint=%d,%q", sequence, cursor)
	}
	var observations int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM provider_execution_observations`, nil, &observations); err != nil || observations != 2 {
		t.Fatalf("observations=%d err=%v", observations, err)
	}
}

func TestProviderObservationAuthorityRefusals(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", SessionRef: "resume-parent", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "ORC-T-9999", ItemID: "ORC-T-9999", LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	bad := providerEvent(1, "bad")
	bad.Status = "grant_proof_and_arm_wave"
	if _, err := store.ApplyProviderExecutionEvent(bad); errorToIssue(err).Code != providerObservationRefused {
		t.Fatalf("bad status error=%v issue=%#v", err, errorToIssue(err))
	}
	oversized := providerEvent(2, "large")
	oversized.Metadata = map[string]any{"payload": strings.Repeat("x", providerObservationMaxMetadata+1)}
	if _, err := store.ApplyProviderExecutionEvent(oversized); errorToIssue(err).Code != providerObservationRefused {
		t.Fatalf("oversized error=%v", err)
	}
	if _, err := store.ApplyProviderExecutionEvent(providerEvent(3, "good")); err != nil {
		t.Fatal(err)
	}
	var runs, bindings, proofs int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &runs); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_binding_events`, nil, &bindings); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT COUNT(*) FROM gate_ledger`, nil, &proofs); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || bindings != 0 || proofs != 0 {
		t.Fatalf("provider input acquired authority: runs=%d bindings=%d proofs=%d", runs, bindings, proofs)
	}
}

func TestProviderExecutionEventAtomicity(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "codex", ProviderSessionID: "thread-1", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	first, err := store.ApplyProviderExecutionEvent(providerEvent(1, "same-event"))
	if err != nil {
		t.Fatal(err)
	}
	replay := providerEvent(1, "same-event")
	replay.ChildHandle = "forged-second-child"
	duplicate, err := store.ApplyProviderExecutionEvent(replay)
	if err != nil || !duplicate.Duplicate || duplicate.ChildExecutionID != first.ChildExecutionID {
		t.Fatalf("duplicate=%#v first=%#v err=%v", duplicate, first, err)
	}
	for table, want := range map[string]int{"execution_records": 2, "execution_edges": 1, "provider_execution_observations": 1, "provider_event_receipts": 1} {
		var got int
		if err := store.queryRowScan(`SELECT COUNT(*) FROM `+table, nil, &got); err != nil || got != want {
			t.Fatalf("%s=%d want=%d err=%v", table, got, want, err)
		}
	}
	store.providerObservationBeforePersist = func(*sql.Tx) error { return errors.New("injected persistence failure") }
	failing := providerEvent(2, "rollback-event")
	failing.ChildHandle = "must-not-survive"
	if _, err := store.ApplyProviderExecutionEvent(failing); err == nil {
		t.Fatal("injected provider observation failure succeeded")
	}
	store.providerObservationBeforePersist = nil
	for table, want := range map[string]int{"execution_records": 2, "execution_edges": 1, "provider_execution_observations": 1, "provider_event_receipts": 1} {
		var got int
		if err := store.queryRowScan(`SELECT COUNT(*) FROM `+table, nil, &got); err != nil || got != want {
			t.Fatalf("rollback %s=%d want=%d err=%v", table, got, want, err)
		}
	}
}
