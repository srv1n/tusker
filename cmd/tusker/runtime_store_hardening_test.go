package main

import (
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
