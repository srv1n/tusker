package main

import (
	"path/filepath"
	"testing"
)

func TestExecutionLedgerMigratesFirstRevisionSchema(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP TABLE execution_edges`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DROP TABLE execution_records`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`CREATE TABLE execution_records (execution_id TEXT PRIMARY KEY, root_execution_id TEXT NOT NULL, project_id TEXT NOT NULL, node_kind TEXT NOT NULL, display_name TEXT NOT NULL DEFAULT '', search_label TEXT NOT NULL DEFAULT '', task_id TEXT NOT NULL DEFAULT '', wave_id TEXT NOT NULL DEFAULT '', attempt_id TEXT NOT NULL DEFAULT '', session_ref TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '', provider_session_id TEXT NOT NULL DEFAULT '', agent_type TEXT NOT NULL DEFAULT '', provider_child_handle TEXT NOT NULL DEFAULT '', creator TEXT NOT NULL DEFAULT '', lease_generation INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`CREATE TABLE execution_edges (parent_execution_id TEXT NOT NULL, child_execution_id TEXT NOT NULL, kind TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(parent_execution_id, child_execution_id))`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatalf("first-revision ledger migration failed: %v", err)
	}
	defer store.Close()
	for _, column := range []string{"parent_execution_id", "wave_authorization_generation"} {
		var found int
		if err := store.queryRowScan(`SELECT COUNT(*) FROM pragma_table_info('execution_records') WHERE name = ?`, []any{column}, &found); err != nil || found != 1 {
			t.Fatalf("missing upgraded column %s: found=%d err=%v", column, found, err)
		}
	}
}

func TestExecutionEdgesRejectAdversarialSQL(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1"})
	if err != nil {
		t.Fatal(err)
	}
	child, created, err := store.UpsertProviderChildExecution(ProviderChildExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, Provider: "codex", ProviderChildHandle: "child"})
	if err != nil || !created {
		t.Fatalf("child: %#v %v", child, err)
	}
	other, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-2"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, parent, child, kind string }{
		{"invalid kind", root.ExecutionID, child.ExecutionID, "made_up"},
		{"missing endpoint", "missing", child.ExecutionID, string(ExecutionProviderChildOf)},
		{"cross project", other.ExecutionID, child.ExecutionID, string(ExecutionProviderChildOf)},
		{"self", root.ExecutionID, root.ExecutionID, string(ExecutionProviderChildOf)},
		{"cycle", child.ExecutionID, root.ExecutionID, string(ExecutionProviderChildOf)},
	}
	for _, tc := range cases {
		if _, err := store.exec(`INSERT INTO execution_edges (parent_execution_id, child_execution_id, kind, created_at) VALUES (?, ?, ?, ?)`, tc.parent, tc.child, tc.kind, executionNow()); err == nil {
			t.Fatalf("%s direct edge bypassed SQLite guard", tc.name)
		}
	}
	for _, statement := range []string{
		`UPDATE execution_edges SET kind = 'invalid' WHERE child_execution_id = '` + child.ExecutionID + `'`,
		`UPDATE execution_edges SET parent_execution_id = '` + child.ExecutionID + `' WHERE child_execution_id = '` + child.ExecutionID + `'`,
		`DELETE FROM execution_edges WHERE child_execution_id = '` + child.ExecutionID + `'`,
		`UPDATE execution_records SET parent_execution_id = 'forged-parent' WHERE execution_id = '` + child.ExecutionID + `'`,
		`UPDATE execution_records SET root_execution_id = 'forged-root' WHERE execution_id = '` + child.ExecutionID + `'`,
		`DELETE FROM execution_records WHERE execution_id = '` + child.ExecutionID + `'`,
	} {
		if _, err := store.exec(statement); err == nil {
			t.Fatalf("adversarial mutation bypassed SQLite guard: %s", statement)
		}
	}
	edgeLess, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "edge-less", TaskID: "TASK-1", WaveID: "W-1", SessionRef: "session-1", Source: "direct_codex", Provider: "codex", ProviderSessionID: "provider-session", AgentType: "reviewer", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`DELETE FROM execution_records WHERE execution_id = ?`, edgeLess.ExecutionID); err == nil {
		t.Fatal("edge-less execution root deletion bypassed SQLite guard")
	}
	// The identity ledger deliberately has no mutable columns. Display/binding
	// changes must be represented by later audited history, not UPDATE.
	for _, mutation := range []struct {
		column string
		value  any
	}{
		{"display_name", "renamed"}, {"search_label", "renamed"}, {"task_id", "TASK-2"}, {"wave_id", "W-2"}, {"wave_authorization_generation", 9},
		{"attempt_id", "attempt-2"}, {"session_ref", "session-2"}, {"source", "direct_claude"}, {"provider", "claude"}, {"provider_session_id", "provider-session-2"},
		{"agent_type", "explorer"}, {"provider_child_handle", "child-2"}, {"creator", "other"}, {"lease_generation", 12}, {"created_at", "2026-02-02T00:00:00Z"},
	} {
		if _, err := store.exec(`UPDATE execution_records SET `+mutation.column+` = ? WHERE execution_id = ?`, mutation.value, edgeLess.ExecutionID); err == nil {
			t.Fatalf("immutable correlation field %s was updated", mutation.column)
		}
	}
}

func TestExecutionBackfillRejectsAnyImmutableMetadataConflictAtomically(t *testing.T) {
	variants := []string{"root", "parent", "kind", "task", "session", "provider"}
	for _, variant := range variants {
		t.Run(variant, func(t *testing.T) {
			store := executionLedgerStore(t)
			parentID, childID, laterID := "legacy-parent", "legacy-child", "legacy-later"
			if err := store.SaveAttempt(RunAttempt{AttemptID: parentID, ProjectID: "p", RecordID: parentID, ItemID: "TASK-P", Runner: "codex", StartedAt: "2026-01-01T00:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveAttempt(RunAttempt{AttemptID: childID, ProjectID: "p", RecordID: childID, ItemID: "TASK-C", Runner: "codex", SessionRef: "session-c", ParentAttemptID: parentID, ChildType: "worker", StartedAt: "2026-01-01T00:01:00Z"}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveAttempt(RunAttempt{AttemptID: laterID, ProjectID: "p", RecordID: laterID, ItemID: "TASK-L", Runner: "codex", StartedAt: "2026-01-01T00:02:00Z"}); err != nil {
				t.Fatal(err)
			}
			record := ExecutionRecord{ExecutionID: legacyExecutionID(childID), RootExecutionID: legacyExecutionID(parentID), ParentExecutionID: legacyExecutionID(parentID), ProjectID: "p", NodeKind: ExecutionNodeManagedAttempt, SearchLabel: "task-c", TaskID: "TASK-C", AttemptID: childID, SessionRef: "session-c", Source: "legacy_attempt", Provider: "codex", AgentType: "worker", Creator: "migration:execution-ledger", CreatedAt: "2026-01-01T00:01:00Z"}
			switch variant {
			case "root":
				record.RootExecutionID = "forged-root"
			case "parent":
				record.ParentExecutionID = "forged-parent"
			case "kind":
				record.NodeKind = ExecutionNodeProviderChild
			case "task":
				record.TaskID = "FORGED"
			case "session":
				record.SessionRef = "forged-session"
			case "provider":
				record.Provider = "claude"
			}
			if _, err := store.exec(`INSERT INTO execution_records (execution_id, root_execution_id, parent_execution_id, project_id, node_kind, display_name, search_label, task_id, wave_id, wave_authorization_generation, attempt_id, session_ref, source, provider, provider_session_id, agent_type, provider_child_handle, creator, lease_generation, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.ExecutionID, record.RootExecutionID, record.ParentExecutionID, record.ProjectID, record.NodeKind, record.DisplayName, record.SearchLabel, record.TaskID, record.WaveID, record.WaveGeneration, record.AttemptID, record.SessionRef, record.Source, record.Provider, record.ProviderSessionID, record.AgentType, record.ProviderChildHandle, record.Creator, record.LeaseGeneration, record.CreatedAt); err != nil {
				t.Fatal(err)
			}
			if err := store.backfillExecutionLedger(); err == nil {
				t.Fatal("conflicting immutable legacy projection was accepted")
			}
			for _, id := range []string{legacyExecutionID(parentID), legacyExecutionID(laterID)} {
				if record, err := store.Execution(id); err != nil || record != nil {
					t.Fatalf("backfill leaked partial row %s: %#v %v", id, record, err)
				}
			}
		})
	}
}

func executionLedgerStore(t *testing.T) *RuntimeStore {
	t.Helper()
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestExecutionGraphIdentity(t *testing.T) {
	store := executionLedgerStore(t)
	direct, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "Lease audit", Source: "direct_codex", Provider: "codex", ProviderSessionID: "thread-1", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if direct.ExecutionID == "" || direct.ExecutionID != direct.RootExecutionID || direct.NodeKind != ExecutionNodeRoot {
		t.Fatalf("direct root identity: %#v", direct)
	}
	if direct.DisplayName != "Lease audit" || direct.ProviderSessionID != "thread-1" || direct.SearchLabel != "lease audit" {
		t.Fatalf("direct typed metadata: %#v", direct)
	}
	wave, err := store.CreateWaveExecutionRoot(WaveExecutionRootInput{ProjectID: "project-1", WaveID: "W-0007", AuthorizationGeneration: 2, Creator: "daemon:generation-2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "ORC-T-0066", ItemID: "ORC-T-0066", ActiveAttemptID: "attempt-1", LeaseState: string(LeaseStateRunning), LeaseGeneration: 7}); err != nil {
		t.Fatal(err)
	}
	child, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: wave.ExecutionID, TaskID: "ORC-T-0066", WaveID: "W-0007", AttemptID: "attempt-1", LeaseGeneration: 7, Creator: "daemon"})
	if err != nil {
		t.Fatal(err)
	}
	if child.RootExecutionID != wave.ExecutionID || child.NodeKind != ExecutionNodeManagedAttempt || child.LeaseGeneration != 7 {
		t.Fatalf("managed child identity: %#v", child)
	}
	var edge ExecutionEdge
	if err := store.queryRowScan(`SELECT parent_execution_id, child_execution_id, kind, created_at FROM execution_edges WHERE child_execution_id = ?`, []any{child.ExecutionID}, &edge.ParentExecutionID, &edge.ChildExecutionID, &edge.Kind, &edge.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if edge.ParentExecutionID != wave.ExecutionID || edge.Kind != ExecutionManagedChildOf {
		t.Fatalf("managed edge: %#v", edge)
	}
}

func TestWaveExecutionRootGenerationIsIdempotentAcrossRestart(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateWaveExecutionRoot(WaveExecutionRootInput{ProjectID: "project-1", WaveID: "W-7", AuthorizationGeneration: 4})
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.CreateWaveExecutionRoot(WaveExecutionRootInput{ProjectID: "project-1", WaveID: "W-7", AuthorizationGeneration: 4})
	if err != nil || again.ExecutionID != first.ExecutionID {
		t.Fatalf("same authorization minted another root: %#v %v", again, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	replayed, err := store.CreateWaveExecutionRoot(WaveExecutionRootInput{ProjectID: "project-1", WaveID: "W-7", AuthorizationGeneration: 4})
	if err != nil || replayed.ExecutionID != first.ExecutionID {
		t.Fatalf("restart replay minted another root: %#v %v", replayed, err)
	}
	next, err := store.CreateWaveExecutionRoot(WaveExecutionRootInput{ProjectID: "project-1", WaveID: "W-7", AuthorizationGeneration: 5})
	if err != nil || next.ExecutionID == first.ExecutionID {
		t.Fatalf("new generation failed to get new root: %#v %v", next, err)
	}
}

func TestExecutionRelationshipConstraints(t *testing.T) {
	store := executionLedgerStore(t)
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "ORC-T-0066", AttemptID: "attempt-1"}); err == nil {
		t.Fatal("managed child accepted work that was not independently admitted")
	}
	first, created, err := store.UpsertProviderChildExecution(ProviderChildExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, Provider: "codex", ProviderChildHandle: "subagent-1", DisplayName: "explorer", AgentType: "researcher"})
	if err != nil || !created {
		t.Fatalf("first provider child: created=%v err=%v record=%#v", created, err, first)
	}
	second, created, err := store.UpsertProviderChildExecution(ProviderChildExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, Provider: "codex", ProviderChildHandle: "subagent-1", DisplayName: "ignored duplicate"})
	if err != nil || created || second.ExecutionID != first.ExecutionID {
		t.Fatalf("provider child replay: created=%v err=%v first=%#v second=%#v", created, err, first, second)
	}
	if second.AttemptID != "" || second.LeaseGeneration != 0 || second.NodeKind != ExecutionNodeProviderChild {
		t.Fatalf("provider child was given fake ownership: %#v", second)
	}
	otherParent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "direct_codex"})
	if err != nil {
		t.Fatal(err)
	}
	other, created, err := store.UpsertProviderChildExecution(ProviderChildExecutionInput{ProjectID: "project-1", ParentExecutionID: otherParent.ExecutionID, Provider: "codex", ProviderChildHandle: "subagent-1"})
	if err != nil || !created || other.ExecutionID == first.ExecutionID {
		t.Fatalf("same provider handle under sibling parent collided: %#v created=%v err=%v", other, created, err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "ORC-T-0066", ItemID: "ORC-T-0066", ActiveAttemptID: "real-attempt", LeaseState: string(LeaseStateRunning), LeaseGeneration: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedExecution(ManagedExecutionInput{ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "ORC-T-0066", AttemptID: "fabricated", LeaseGeneration: 99}); err == nil {
		t.Fatal("fabricated attempt/generation was accepted")
	}
}

func TestExecutionGraphMigration(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "legacy-parent", ProjectID: "project-1", RecordID: "record-1", ItemID: "ORC-T-0001", Runner: "codex", StartedAt: "2026-07-29T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: "legacy-child", ProjectID: "project-1", RecordID: "record-1", ItemID: "ORC-T-0002", Runner: "codex", ParentAttemptID: "legacy-parent", ChildType: "reviewer", StartedAt: "2026-07-29T00:01:00Z"}); err != nil {
		t.Fatal(err)
	}
	if err := store.backfillExecutionLedger(); err != nil {
		t.Fatal(err)
	}
	child, err := store.Execution(legacyExecutionID("legacy-child"))
	if err != nil || child == nil {
		t.Fatalf("legacy child missing: %#v %v", child, err)
	}
	if child.RootExecutionID != legacyExecutionID("legacy-parent") || child.AttemptID != "legacy-child" || child.NodeKind != ExecutionNodeManagedAttempt {
		t.Fatalf("legacy projection: %#v", child)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Opening again exercises the interrupted/restarted migration path. The
	// legacy attempt remains readable and the deterministic execution ID is not
	// duplicated or replaced.
	store, err = OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE attempt_id IN ('legacy-parent', 'legacy-child')`, nil, &count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("migration duplicated or lost attempts: %d", count)
	}
	attempts, err := store.ListAttemptsForRun("project-1", "record-1")
	if err != nil || len(attempts) != 2 {
		t.Fatalf("legacy attempts no longer readable: %#v %v", attempts, err)
	}
}
