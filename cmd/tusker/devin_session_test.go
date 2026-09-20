package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// devinSessionFixtureDB builds a minimal Devin-shaped sessions database. The
// code under test must never open it: every state below must produce the same
// source=devin provenance with no conversation id.
func devinSessionFixtureDB(t *testing.T, workingDir string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT NOT NULL, last_activity_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		id       string
		activity int64
	}{{"devin-session-one", 2000}, {"devin-session-two", 1000}} {
		if _, err := db.Exec(`INSERT INTO sessions(id, working_directory, last_activity_at) VALUES(?,?,?)`, row.id, workingDir, row.activity); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func devinSessionGarbageDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func devinSessionDBStates(t *testing.T, workingDir string) map[string]string {
	return map[string]string{
		"missing db":      filepath.Join(t.TempDir(), "absent", "sessions.db"),
		"not sqlite":      devinSessionGarbageDB(t),
		"fabricated rows": devinSessionFixtureDB(t, workingDir),
		"unrelated path":  filepath.Join(t.TempDir(), "anything"),
	}
}

func TestDevinSessionKindWorkerPrecedenceAndGuards(t *testing.T) {
	t.Setenv("CHISEL_SESSION_DB", filepath.Join(t.TempDir(), "sessions.db"))
	t.Setenv("TUSKER_ATTEMPT_ID", "daemon-worker-1")
	if kind := agentSessionKind(); kind != "dispatched Tusker worker" {
		t.Fatalf("dispatched worker kind = %q", kind)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	if kind := agentSessionKind(); kind != "interactive Devin session" {
		t.Fatalf("kind = %q", kind)
	}
	if _, err := v7HumanActor(Args{"by": "human:sarav"}, "test operation"); err == nil || !strings.Contains(err.Error(), "interactive Devin session") {
		t.Fatalf("human actor from Devin session error = %v, want refusal", err)
	}
	if err := rejectAgentSpawn("tusker daemon run"); err == nil || !strings.Contains(err.Error(), "interactive Devin session") {
		t.Fatalf("daemon spawn from Devin session error = %v, want refusal", err)
	}
	t.Setenv("CHISEL_SESSION_DB", "")
	if kind := agentSessionKind(); kind != "" {
		t.Fatalf("kind without markers = %q, want empty", kind)
	}
}

// A Devin session is classified but never carries a native conversation id:
// nothing reads CHISEL_SESSION_DB contents, so every database state produces
// identical provenance and nativeConversationKnown can never be satisfied.
func TestDevinSessionProvenanceNeverCarriesConversation(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	for name, dbPath := range devinSessionDBStates(t, cwd) {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CHISEL_SESSION_DB", dbPath)
			context := TaskAuthoringContextFromEnvironment()
			if context.Source != "devin" || context.ConversationID != "" {
				t.Fatalf("authoring context = %#v, want devin with empty conversation id", context)
			}
			if nativeConversationKnown(context) {
				t.Fatal("devin context must never satisfy native conversation")
			}
		})
	}
}

// --current-workspace fails closed for a Devin session for every database
// state: no immutable per-process session id exists to satisfy the gate.
func TestDevinSessionCurrentWorkspaceRefusesForEveryDBState(t *testing.T) {
	vault, project := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	t.Chdir(project.RepoRoot)
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	for name, dbPath := range devinSessionDBStates(t, project.RepoRoot) {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CHISEL_SESSION_DB", dbPath)
			err := workSessionStartCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:implementer", "current-workspace": "true"})
			if err == nil || !strings.Contains(err.Error(), "native conversation id") {
				t.Fatalf("current-workspace error = %v, want native conversation refusal", err)
			}
		})
	}
}

// Two concurrent same-directory Devin sessions can never produce a false
// independent-review pass: a self_implementation claim cannot be reviewed by
// a Devin session because the reviewer context never satisfies the native
// conversation gate, and a fabricated devin conversation inside a stored
// trigger cannot manufacture one either.
func TestDevinSessionCannotReviewSelfImplementation(t *testing.T) {
	vault, project := workSessionFixture(t, 1)
	configureWorkSessionMaterialScope(t, vault)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"work_revision": 0})
	t.Chdir(project.RepoRoot)
	t.Setenv("CODEX_THREAD_ID", "implementation-conversation")
	t.Setenv("TUSKER_ATTEMPT_ID", "")

	captureStdout(t, func() {
		if err := workSessionStartCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:implementer", "current-workspace": "true"}); err != nil {
			t.Fatal(err)
		}
		if err := workSessionLifecycleCmd(Args{"id": "APP-T-0001", "by": "agent:implementer", "deliverable": "implementation", "verification": "A1 pass", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
			t.Fatal(err)
		}
	})

	// The reviewer is now a Devin session in the same directory; fabricated
	// session rows must not be consulted to manufacture a conversation id.
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CHISEL_SESSION_DB", devinSessionFixtureDB(t, project.RepoRoot))
	err := workSessionReviewCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "reviewer:agent", "source": "devin"})
	if err == nil || !strings.Contains(err.Error(), "independent native conversation") {
		t.Fatalf("devin review error = %v, want native conversation independence refusal", err)
	}

	// Even a stored trigger carrying a fabricated devin conversation cannot be
	// paired with a Devin reviewer context: the reviewer side never resolves.
	forged := "self_implementation;source=devin;conversation=fabricated-session;host=local"
	if SameAuthoringConversation(forged, TaskAuthoringContextFromEnvironment()) {
		t.Fatal("forged devin trigger produced a same-conversation match against a Devin reviewer")
	}
}
