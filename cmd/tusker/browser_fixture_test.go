package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDisposableRunSessionBrowserFixture creates only the runtime rows and
// vault files needed by run-session.browser.mjs. The caller supplies an
// isolated root so this never touches the resident Tusker state.
func TestDisposableRunSessionBrowserFixture(t *testing.T) {
	root := os.Getenv("TUSKER_BROWSER_FIXTURE_ROOT")
	if root == "" {
		t.Fatal("TUSKER_BROWSER_FIXTURE_ROOT is required")
	}
	vault := filepath.Join(root, ".tusker")
	stateRoot := filepath.Join(root, "state")
	for _, dir := range []string{
		filepath.Join(vault, "work", "tasks"),
		filepath.Join(vault, "work", "epics"),
		filepath.Join(vault, "work", "gates"),
	} {
		if err := ensureDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeText(filepath.Join(vault, "config.yaml"), "schema: tusker.config/v1\nproject_id: app\nstorage:\n  root: .tusker\nruntime:\n  mutation_mode: single_user_local\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(vault, "SKILL.md"), "# Browser fixture\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	writeServeEpic(t, vault, "APP", "Browser fixture")
	writeServeTask(t, vault, serveTaskSeed{ID: "TSK-T-0054-browser", Epic: "APP", Title: "Run session browser fixture", Status: "ready", Risk: "medium", Priority: "p1"})

	eventsPath := filepath.Join(stateRoot, "runs", "app", "TSK-T-0054-browser", "attempt-browser.events.jsonl")
	rawLogPath := filepath.Join(stateRoot, "runs", "app", "TSK-T-0054-browser", "attempt-browser.raw.log")
	if err := ensureDir(filepath.Dir(eventsPath)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(eventsPath, `{"seq":1,"at":"2026-09-22T10:00:00Z","attempt_id":"attempt-browser","runner":"codex_app_server","kind":"agent_message","payload":{"activity":true,"text":"Running fixture tests.","level":"info"}}
{"seq":2,"at":"2026-09-22T10:00:01Z","attempt_id":"attempt-browser","runner":"codex_app_server","kind":"tool_result","payload":{"activity":true,"text":"Tests finished: 2 passed (cargo test)","level":"info"}}
`); err != nil {
		t.Fatal(err)
	}
	if err := writeText(rawLogPath, "fixture raw log\n"); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "Browser fixture", RepoRoot: root, VaultRoot: vault, WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	run := RunStatus{
		ProjectID: "app", RecordID: "TSK-T-0054-browser", ItemID: "TSK-T-0054-browser",
		Runner: string(RunnerCodexAppServer), RunnerHarness: string(RunnerCodexAppServer), Lane: runLaneExecute,
		LeaseState: string(LeaseStateRunning), LeaseOwner: "agent:browser-fixture", LeaseGeneration: 1,
		AttemptOutcome: string(AttemptOutcomeNone), ActiveAttemptID: "attempt-browser", AttemptCount: 1,
		WorkspacePath: root, EventSinkPath: eventsPath, RawLogPath: rawLogPath, WorkRevision: 1,
		StartedAt: now, UpdatedAt: now, LastEventAt: now,
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "attempt-browser", ProjectID: "app", RecordID: run.RecordID, ItemID: run.ItemID,
		Runner: run.Runner, Lane: run.Lane, WorkRevision: 1, WorkspacePath: root,
		EventSinkPath: eventsPath, RawLogPath: rawLogPath, StartedAt: now, Outcome: string(AttemptOutcomeNone),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{
		ProjectID: "app", RecordID: run.RecordID, LeaseGeneration: 1, AttemptID: run.ActiveAttemptID,
		Source: "browser-fixture", Actor: "human:browser-fixture", Trigger: "qualification", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}
