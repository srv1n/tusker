package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// These tests exercise the restart boundary rather than calling the migration
// directly: the migration marker is only meaningful when OpenRuntimeStore
// decides whether a persisted database needs repair.
func TestExecutionLedgerConstraintMigrationRepairsDriftDespiteVersionMarker(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := store.queryRowScan(`SELECT version FROM execution_ledger_migrations WHERE component = 'constraints'`, nil, &version); err != nil || version != 1 {
		t.Fatalf("initial constraint marker: version=%d err=%v", version, err)
	}
	for _, statement := range []string{
		`DROP INDEX execution_records_root`,
		`CREATE INDEX execution_records_root ON execution_records(project_id)`,
		`DROP TRIGGER execution_records_immutable`,
		`CREATE TRIGGER execution_records_immutable BEFORE UPDATE ON execution_records BEGIN SELECT 1; END`,
	} {
		if _, err := store.exec(statement); err != nil {
			t.Fatalf("seed drift %q: %v", statement, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatalf("reopen must repair same-named drift: %v", err)
	}
	defer reopened.Close()
	if err := reopened.queryRowScan(`SELECT version FROM execution_ledger_migrations WHERE component = 'constraints'`, nil, &version); err != nil || version != 1 {
		t.Fatalf("repaired constraint marker: version=%d err=%v", version, err)
	}
	assertExecutionLedgerSchemaObject(t, reopened, "index", "execution_records_root", `CREATE INDEX IF NOT EXISTS execution_records_root ON execution_records(root_execution_id);`)
	assertExecutionLedgerSchemaObject(t, reopened, "trigger", "execution_records_immutable", `CREATE TRIGGER IF NOT EXISTS execution_records_immutable BEFORE UPDATE ON execution_records BEGIN SELECT RAISE(ABORT, 'execution records are immutable'); END;`)
}

func TestExecutionLedgerConstraintMigrationFailureRollsBackWithoutStampingVersion(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-constraint database containing duplicate legacy attempts.
	// The missing index makes the version marker insufficient; reopening must
	// attempt the transactional repair and leave every preexisting object alone
	// when the unique-index creation fails.
	for _, statement := range []string{
		`DELETE FROM execution_ledger_migrations WHERE component = 'constraints'`,
		`DROP INDEX execution_records_attempt_id`,
	} {
		if _, err := store.exec(statement); err != nil {
			t.Fatalf("seed incompatible legacy state %q: %v", statement, err)
		}
	}
	for _, executionID := range []string{"legacy-duplicate-a", "legacy-duplicate-b"} {
		if _, err := store.exec(`INSERT INTO execution_records (execution_id, root_execution_id, project_id, node_kind, attempt_id, created_at) VALUES (?, ?, 'project-1', 'root', 'duplicate-attempt', '2026-07-30T00:00:00Z')`, executionID, executionID); err != nil {
			t.Fatalf("seed duplicate legacy attempt %s: %v", executionID, err)
		}
	}
	before := executionLedgerMigrationCatalog(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := OpenRuntimeStore(stateRoot); err == nil {
		t.Fatal("reopen accepted incompatible duplicate legacy attempt rows")
	}
	readonly, err := OpenRuntimeStoreReadOnly(stateRoot)
	if err != nil {
		t.Fatalf("inspect failed migration state: %v", err)
	}
	defer readonly.Close()
	after := executionLedgerMigrationCatalog(t, readonly)
	if after != before {
		t.Fatalf("failed migration changed schema objects\nbefore:\n%s\nafter:\n%s", before, after)
	}
	var markers int
	if err := readonly.queryRowScan(`SELECT COUNT(*) FROM execution_ledger_migrations WHERE component = 'constraints'`, nil, &markers); err != nil {
		t.Fatal(err)
	}
	if markers != 0 {
		t.Fatalf("failed migration stamped constraints version: %d", markers)
	}
}

func TestExecutionLedgerConstraintMigrationRepeatedReopenDoesNotChurnCatalog(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	before := executionLedgerMigrationCatalog(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	after := executionLedgerMigrationCatalog(t, reopened)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("clean reopen churned execution-ledger catalog\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func assertExecutionLedgerSchemaObject(t *testing.T, store *RuntimeStore, kind, name, want string) {
	t.Helper()
	var got string
	if err := store.queryRowScan(`SELECT sql FROM sqlite_master WHERE type = ? AND name = ?`, []any{kind, name}, &got); err != nil {
		t.Fatalf("read %s %s: %v", kind, name, err)
	}
	if normalizeSQLiteSchemaSQL(got) != normalizeSQLiteSchemaSQL(want) {
		t.Fatalf("%s %s was not repaired\nwant: %s\n got: %s", kind, name, want, got)
	}
}

func executionLedgerMigrationCatalog(t *testing.T, store *RuntimeStore) string {
	t.Helper()
	rows, err := store.query(`SELECT type, name, tbl_name, sql FROM sqlite_master
		WHERE name = 'execution_ledger_migrations' OR name LIKE 'execution_%'
		ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var catalog strings.Builder
	for rows.Next() {
		var kind, name, table string
		var statement sql.NullString
		if err := rows.Scan(&kind, &name, &table, &statement); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&catalog, "%s\x00%s\x00%s\x00%s\n", kind, name, table, statement.String)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return catalog.String()
}
