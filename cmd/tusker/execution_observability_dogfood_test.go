package main

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
)

// TestExecutionObservabilityDogfoodFixture is intentionally a small
// cross-boundary fixture, rather than a second implementation of the graph.
// It opens a fresh store, persists Codex and Claude roots plus provider-owned
// children, restarts over the populated store, then projects the exact facts
// a factory-level regression needs.  The golden is stable because it records
// authority and topology, never generated IDs or wall-clock values.
func TestExecutionObservabilityDogfoodFixture(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(state) // fresh-install migration
	if err != nil {
		t.Fatal(err)
	}
	codex, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "dogfood", DisplayName: "Codex root", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.AttachExecution(ExecutionAttachmentInput{ProjectID: "dogfood", ExecutionID: codex.ExecutionID, Provider: "codex", ProviderSessionID: "codex-thread", SessionRef: "codex-resume", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	claude, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "dogfood", DisplayName: "Claude root", Source: "direct_claude", Provider: "claude", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.AttachExecution(ExecutionAttachmentInput{ProjectID: "dogfood", ExecutionID: claude.ExecutionID, Provider: "claude", ProviderSessionID: "claude-session", SessionRef: "claude-resume", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	// This root stays unbound. A provider observation remains observation-only,
	// so it must not turn historical direct work into task proof.
	unbound, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "dogfood", DisplayName: "Unbound audit", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.AttachExecution(ExecutionAttachmentInput{ProjectID: "dogfood", ExecutionID: unbound.ExecutionID, Provider: "codex", ProviderSessionID: "unbound-thread", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	wave, err := store.CreateWaveExecutionRoot(WaveExecutionRootInput{ProjectID: "dogfood", WaveID: "W-dogfood", DisplayName: "Dogfood wave", AuthorizationGeneration: 1, Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	for _, managed := range []struct{ task, attempt, provider, session string }{{"ORC-T-CODEX", "attempt-codex", "codex", "managed-codex"}, {"ORC-T-CLAUDE", "attempt-claude", "claude", "managed-claude"}} {
		if err = store.UpsertRun(RunStatus{ProjectID: "dogfood", RecordID: managed.task, ItemID: managed.task, ActiveAttemptID: managed.attempt, LeaseState: string(LeaseStateRunning), LeaseGeneration: 1}); err != nil {
			t.Fatal(err)
		}
		record, createErr := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "dogfood", ParentExecutionID: wave.ExecutionID, TaskID: managed.task, WaveID: "W-dogfood", AttemptID: managed.attempt, LeaseGeneration: 1, Provider: managed.provider, ProviderSessionID: managed.session, Source: "daemon"})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, _, attachErr := store.AttachExecution(ExecutionAttachmentInput{ProjectID: "dogfood", ExecutionID: record.ExecutionID, Provider: managed.provider, ProviderSessionID: managed.session, Actor: "daemon"}); attachErr != nil {
			t.Fatal(attachErr)
		}
	}

	apply := func(provider, parentSession, eventID, child, status string, seq int64) ProviderObservationResult {
		t.Helper()
		result, applyErr := store.ApplyProviderExecutionEvent(ProviderExecutionEventEnvelope{
			Version: providerObservationVersion, ProjectID: "dogfood", Provider: provider, SourceEventID: eventID, SourceSequence: seq, SourceCursor: "cursor-" + eventID,
			ParentProviderSessionID: parentSession, ChildHandle: child, AgentType: "reviewer", Label: "native child", Status: status, OccurredAt: "2026-07-29T12:00:00Z",
		})
		if applyErr != nil {
			t.Fatal(applyErr)
		}
		return result
	}
	codexChild := apply("codex", "codex-thread", "codex-child", "codex-child", "failed", 2)
	claudeChild := apply("claude", "claude-session", "claude-child", "claude-child", "running", 3)
	managedCodexChild := apply("codex", "managed-codex", "managed-codex-child", "managed-codex-child", "running", 1)
	managedClaudeChild := apply("claude", "managed-claude", "managed-claude-child", "managed-claude-child", "running", 1)
	if replay := apply("codex", "codex-thread", "codex-child", "codex-child", "failed", 2); !replay.Duplicate || replay.ChildExecutionID != codexChild.ChildExecutionID {
		t.Fatalf("replay lost idempotency: %#v", replay)
	}
	// A late event is accepted into the local timeline but cannot regress the
	// source checkpoint; this is the subscriber-loss/refetch recovery seam.
	late := apply("codex", "codex-thread", "codex-late", "codex-child", "running", 1)
	if late.CheckpointAdvanced {
		t.Fatalf("late provider event regressed checkpoint: %#v", late)
	}
	beforeBindings := executionDogfoodCount(t, store, "execution_binding_events")
	apply("codex", "unbound-thread", "unbound-event", "unbound-child", "running", 1)
	if got := executionDogfoodCount(t, store, "execution_binding_events"); got != beforeBindings {
		t.Fatalf("unbound provider history acquired binding authority: before=%d after=%d", beforeBindings, got)
	}
	if err = store.UpsertRun(RunStatus{ProjectID: "dogfood", RecordID: "ORC-T-BOUND", ItemID: "ORC-T-BOUND", LeaseState: string(LeaseStateRunning)}); err != nil {
		t.Fatal(err)
	}
	if _, bindErr := store.BindExecution(ExecutionBindingInput{ProjectID: "dogfood", ExecutionID: unbound.ExecutionID, TaskID: "ORC-T-BOUND", WaveID: "W-dogfood", Actor: "operator"}, "bind"); bindErr == nil {
		t.Fatal("unbound execution stole live delivery authority")
	}
	if err = store.SaveAttempt(RunAttempt{AttemptID: "legacy-dogfood", ProjectID: "dogfood", RecordID: "legacy-record", ItemID: "ORC-T-LEGACY", Runner: "codex", StartedAt: "2026-07-29T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	legacyBefore := executionDogfoodCompatibilityRead(t, store)
	if err = store.backfillExecutionLedger(); err != nil {
		t.Fatal(err)
	}

	tail, err := store.ExecutionTimeline("dogfood", codex.ExecutionID, "", "tail", "", 20)
	if err != nil || len(tail.Rows) != 2 || tail.CommittedTail == "" {
		t.Fatalf("authoritative codex tail=%#v err=%v", tail, err)
	}
	if _, err = store.RequestExecutionCancellation(codexChild.ChildExecutionID, "dogfood-cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RequestExecutionCancellation(codexChild.ChildExecutionID, "dogfood-cancel"); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil { // populated-store restart/repeated migration
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if legacyAfter := executionDogfoodCompatibilityRead(t, store); legacyAfter != legacyBefore {
		t.Fatalf("compatible legacy reader changed after migration/restart: before=%#v after=%#v", legacyBefore, legacyAfter)
	}

	graph, err := store.ExecutionGraph("dogfood", ExecutionGraphFilter{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var providerChildren, managedAttempts, attention, unboundRoots, codexRoots, claudeRoots int
	for _, node := range graph.Nodes {
		switch {
		case node.NodeKind == ExecutionNodeProviderChild:
			providerChildren++
		case node.NodeKind == ExecutionNodeManagedAttempt:
			managedAttempts++
		case node.Provider == "codex" && node.NodeKind == ExecutionNodeRoot:
			codexRoots++
		case node.Provider == "claude" && node.NodeKind == ExecutionNodeRoot:
			claudeRoots++
		}
		if node.AttentionChildren > 0 {
			attention++
		}
		if node.NodeKind == ExecutionNodeRoot && !node.ProofEligible {
			unboundRoots++
		}
	}
	afterRestart, err := store.ExecutionTimeline("dogfood", codex.ExecutionID, "", "after", tail.CommittedTail, 20)
	if err != nil || len(afterRestart.Rows) != 0 || afterRestart.CommittedTail != tail.CommittedTail {
		t.Fatalf("restart did not preserve authoritative cursor: %#v err=%v", afterRestart, err)
	}
	lifecycle, err := store.ExecutionLifecycle(codex.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	fixture := struct {
		Schema              string `json:"schema"`
		CodexRoots          int    `json:"codex_roots"`
		ClaudeRoots         int    `json:"claude_roots"`
		ProviderChildren    int    `json:"provider_children"`
		ManagedAttempts     int    `json:"managed_attempts"`
		AttentionRoots      int    `json:"attention_roots"`
		UnboundRoots        int    `json:"unbound_roots"`
		CursorConverged     bool   `json:"cursor_converged"`
		UnboundProofDenied  bool   `json:"unbound_proof_denied"`
		UnboundBindRefused  bool   `json:"unbound_bind_refused"`
		LifecycleDimensions bool   `json:"lifecycle_dimensions"`
		CancellationFacts   int    `json:"cancellation_facts"`
	}{
		Schema: "tusker.execution-observability-dogfood/v1", CodexRoots: codexRoots, ClaudeRoots: claudeRoots,
		ProviderChildren: providerChildren, ManagedAttempts: managedAttempts, AttentionRoots: attention, UnboundRoots: unboundRoots,
		CursorConverged:     afterRestart.CommittedTail == tail.CommittedTail,
		UnboundProofDenied:  unboundRoots > 0,
		UnboundBindRefused:  executionDogfoodCount(t, store, "execution_binding_events") == beforeBindings,
		LifecycleDimensions: lifecycle.DeliveryState != "" && lifecycle.ProviderState != "" && lifecycle.ProcessState != "" && lifecycle.OutcomeState != "",
		CancellationFacts:   executionDogfoodCancellationCount(t, store, codexChild.ChildExecutionID),
	}
	got, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"schema":"tusker.execution-observability-dogfood/v1","codex_roots":3,"claude_roots":1,"provider_children":5,"managed_attempts":2,"attention_roots":1,"unbound_roots":5,"cursor_converged":true,"unbound_proof_denied":true,"unbound_bind_refused":true,"lifecycle_dimensions":true,"cancellation_facts":5}`
	if string(got) != want {
		t.Fatalf("dogfood fixture changed\nwant %s\n got %s", want, got)
	}
	_ = claudeChild
	_ = managedCodexChild
	_ = managedClaudeChild
}

func executionDogfoodCount(t *testing.T, store *RuntimeStore, table string) int {
	t.Helper()
	var count int
	if err := store.queryRowScan("SELECT COUNT(*) FROM "+table, nil, &count); err != nil {
		t.Fatal(err)
	}
	return count
}

func executionDogfoodCancellationCount(t *testing.T, store *RuntimeStore, executionID string) int {
	t.Helper()
	var count int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=?`, []any{executionID}, &count); err != nil {
		t.Fatal(err)
	}
	return count
}

// executionDogfoodCompatibilityRead intentionally uses only the frozen
// pre-ledger attempt/run query surface. It models rolling a migrated state
// back to a compatible binary: identity/lineage and SQLite lease ownership
// must remain readable even though that binary has no execution tables.
func executionDogfoodCompatibilityRead(t *testing.T, store *RuntimeStore) string {
	t.Helper()
	var attempt, project, record, item, parent, runner, leaseState string
	var generation int
	if err := store.queryRowScan(`SELECT a.attempt_id, a.project_id, a.record_id, a.item_id, a.parent_attempt_id, a.runner, COALESCE(r.lease_state,''), COALESCE(r.lease_generation,0) FROM attempts a LEFT JOIN runs r ON r.project_id=a.project_id AND (r.item_id=a.item_id OR r.record_id=a.record_id) WHERE a.attempt_id='legacy-dogfood'`, nil, &attempt, &project, &record, &item, &parent, &runner, &leaseState, &generation); err != nil {
		t.Fatal(err)
	}
	return attempt + "|" + project + "|" + record + "|" + item + "|" + parent + "|" + runner + "|" + leaseState + "|" + strconv.Itoa(generation)
}
