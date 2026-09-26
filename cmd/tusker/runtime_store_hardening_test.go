package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureRuntimeStateRootTightensExistingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not portable on Windows")
	}
	root := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureRuntimeStateRoot(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("runtime root mode = %o, want 700", got)
	}
}

func TestEnsureRuntimeStateRootRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are not portable on Windows")
	}
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "state")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureRuntimeStateRoot(link); err == nil {
		t.Fatal("expected symlink runtime root to be rejected")
	}
}

func TestRuntimeSchemaMarkerCannotMaskMissingTableOrColumn(t *testing.T) {
	root := t.TempDir()
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec("DROP TABLE run_directives"); err != nil {
		t.Fatal(err)
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("schema marker incorrectly accepted missing table")
	}
	if _, err := store.exec("CREATE TABLE run_directives (project_id TEXT, record_id TEXT, expires_at TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec("DROP TABLE runs"); err != nil {
		t.Fatal(err)
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("schema marker incorrectly accepted missing authority table")
	}
}

func TestRuntimeSchemaMarkerCannotMaskMissingAuthorizationAttempt(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.exec("ALTER TABLE run_authorizations DROP COLUMN attempt_id"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec("PRAGMA user_version = " + fmt.Sprint(runtimeSchemaVersion)); err != nil {
		t.Fatal(err)
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("schema marker incorrectly accepted missing authorization attempt")
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	var found int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM pragma_table_info('run_authorizations') WHERE name = 'attempt_id'`, nil, &found); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatal("migration did not restore authorization attempt column")
	}
}

func TestRuntimeSchemaMarkerMigratesRunFailureColumns(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, stmt := range []string{
		"ALTER TABLE runs DROP COLUMN reason_code",
		"ALTER TABLE runs DROP COLUMN infrastructure_json",
		"ALTER TABLE attempts DROP COLUMN reason_code",
	} {
		if _, err := store.exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("schema marker accepted missing run failure columns")
	}
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	for _, item := range [][2]string{{"runs", "reason_code"}, {"runs", "infrastructure_json"}, {"attempts", "reason_code"}} {
		var found int
		if err := store.queryRowScan(`SELECT COUNT(*) FROM pragma_table_info('`+item[0]+`') WHERE name = ?`, []any{item[1]}, &found); err != nil || found != 1 {
			t.Fatalf("migration did not restore %s.%s: found=%d err=%v", item[0], item[1], found, err)
		}
	}
}

func TestTightenRuntimeStateFilesRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are not portable on Windows")
	}
	root := t.TempDir()
	target := filepath.Join(root, "outside.db")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "daemon.db")); err != nil {
		t.Fatal(err)
	}
	if err := tightenRuntimeStateFiles(root); err == nil {
		t.Fatal("expected symlink database file to be rejected")
	}
}

func TestRuntimeStoreReadOnlyRejectsSymlinkedSQLiteSidecar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions are not portable on Windows")
	}
	root := t.TempDir()
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside-wal")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "daemon.db-wal")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRuntimeStoreReadOnly(root); err == nil {
		t.Fatal("expected read-only runtime store to reject symlinked WAL sidecar")
	}
}

func TestListRunsForProjectPageIsScopedAndBounded(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, run := range []RunStatus{
		{ProjectID: "project-a", RecordID: "A-1", ItemID: "A-1", UpdatedAt: "2026-08-09T00:00:01Z"},
		{ProjectID: "project-a", RecordID: "A-2", ItemID: "A-2", UpdatedAt: "2026-08-09T00:00:02Z"},
		{ProjectID: "project-a", RecordID: "A-3", ItemID: "A-3", UpdatedAt: "2026-08-09T00:00:03Z"},
		{ProjectID: "project-b", RecordID: "B-1", ItemID: "B-1", UpdatedAt: "2026-08-09T00:00:04Z"},
		{ProjectID: "", RecordID: "LEGACY-1", ItemID: "LEGACY-1", UpdatedAt: "2026-08-09T00:00:05Z"},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}

	runs, truncated, err := store.ListRunsForProjectPage("project-a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(runs) != 2 {
		t.Fatalf("page len=%d truncated=%t, want len=2 truncated=true", len(runs), truncated)
	}
	for _, run := range runs {
		if run.ProjectID != "project-a" {
			t.Fatalf("cross-project or unattributed row leaked into project page: %#v", run)
		}
	}
}

func TestRuntimeSchemaCompleteTakesFastPathAndDetectsMissingLateSchema(t *testing.T) {
	root := t.TempDir()
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if !store.runtimeSchemaComplete() {
		t.Fatal("freshly migrated store must report complete so reopen takes the fast path")
	}
	for _, stmt := range []string{
		"ALTER TABLE runs DROP COLUMN infrastructure_json",
		"DROP TRIGGER execution_cancel_no_delete",
	} {
		if _, err := store.exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("store missing a late column and trigger reported complete")
	}
	store.Close()
	store, err = OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !store.runtimeSchemaComplete() {
		t.Fatal("reopen did not run the full migration to restore missing schema")
	}
	if _, err := store.exec("DELETE FROM execution_ledger_migrations WHERE component = 'legacy_backfill'"); err != nil {
		t.Fatal(err)
	}
	if store.runtimeSchemaComplete() {
		t.Fatal("store that never ran the legacy backfill reported complete")
	}
}

func TestRuntimeStoreReopenBackfillsPopulatedLegacyAttemptsOnce(t *testing.T) {
	root := t.TempDir()
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []RunAttempt{
		{AttemptID: "legacy-parent", ProjectID: "p", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "codex", SessionRef: "native-session", StartedAt: "2026-07-29T00:00:00Z"},
		{AttemptID: "legacy-child", ProjectID: "p", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "codex", ParentAttemptID: "legacy-parent", ChildType: "worker", StartedAt: "2026-07-29T00:01:00Z"},
	} {
		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		"DELETE FROM execution_ledger_migrations WHERE component = 'legacy_backfill'",
		"ALTER TABLE runs DROP COLUMN infrastructure_json",
		"DROP TRIGGER execution_cancel_no_delete",
	} {
		if _, err := store.exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	for reopen := 1; reopen <= 2; reopen++ {
		store, err = OpenRuntimeStore(root)
		if err != nil {
			t.Fatalf("reopen %d: %v", reopen, err)
		}
		if !store.runtimeSchemaComplete() {
			t.Fatalf("reopen %d left schema incomplete", reopen)
		}
		for _, id := range []string{"legacy-parent", "legacy-child"} {
			var count int
			if err := store.queryRowScan(`SELECT COUNT(*) FROM attempts WHERE attempt_id = ? AND project_id = 'p' AND item_id = 'TASK-1'`, []any{id}, &count); err != nil || count != 1 {
				t.Fatalf("reopen %d retained attempt %s: count=%d err=%v", reopen, id, count, err)
			}
		}
		parent, err := store.Execution(legacyExecutionID("legacy-parent"))
		if err != nil || parent == nil || parent.NodeKind != ExecutionNodeRoot || parent.SessionRef != "native-session" || parent.CreatedAt != "2026-07-29T00:00:00Z" {
			t.Fatalf("reopen %d parent execution=%#v err=%v", reopen, parent, err)
		}
		child, err := store.Execution(legacyExecutionID("legacy-child"))
		if err != nil || child == nil || child.NodeKind != ExecutionNodeManagedAttempt || child.ParentExecutionID != parent.ExecutionID || child.RootExecutionID != parent.ExecutionID || child.CreatedAt != "2026-07-29T00:01:00Z" {
			t.Fatalf("reopen %d child execution=%#v err=%v", reopen, child, err)
		}
		var records, edges int
		if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_records WHERE project_id = 'p' AND attempt_id IN ('legacy-parent', 'legacy-child')`, nil, &records); err != nil {
			t.Fatal(err)
		}
		if err := store.queryRowScan(`SELECT COUNT(*) FROM execution_edges WHERE parent_execution_id = ? AND child_execution_id = ?`, []any{parent.ExecutionID, child.ExecutionID}, &edges); err != nil {
			t.Fatal(err)
		}
		if records != 2 || edges != 1 {
			t.Fatalf("reopen %d duplicated lineage: records=%d edges=%d", reopen, records, edges)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
