package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOrphanProjectRunsDoNotHoldCapacity(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project, vault := fairDispatchPollProject(t, store, "healthy", "HLT", 1)
	disabled := RegisteredProject{ProjectID: "disabled", Name: "disabled", RepoRoot: t.TempDir(), VaultRoot: t.TempDir()}
	if err := store.UpsertProject(disabled); err != nil {
		t.Fatal(err)
	}
	for _, run := range []RunStatus{
		{ProjectID: "removed", RecordID: "orphan-running", LeaseState: "running"},
		{ProjectID: "removed", RecordID: "orphan-claimed", LeaseState: "claimed"},
		{ProjectID: "removed", RecordID: "orphan-retry", LeaseState: "retry_queued"},
		{RecordID: "unregistered", LeaseState: "running"},
		{ProjectID: disabled.ProjectID, RecordID: "disabled-running", LeaseState: "running"},
		{ProjectID: project.ProjectID, RecordID: "terminal", LeaseState: "running", Terminal: true},
		{ProjectID: project.ProjectID, RecordID: "queued", LeaseState: "retry_queued"},
	} {
		run.HandRun = true
		run.LeaseExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := registeredProjectRuns(store, runs)
	if err != nil || countDispatchCapacityRuns(filtered) != 1 {
		t.Fatalf("registered capacity = %d, err=%v; want disabled project's one run", countDispatchCapacityRuns(filtered), err)
	}
	if findings := doctorCapacityFindings(vault, store, project.ProjectID, runs, DiagnosticScope{}, "HLT-T-0001", "now"); len(findings) != 0 {
		t.Fatalf("orphan/queued/terminal runs caused false capacity diagnosis: %#v", findings)
	}
	// One disabled project's run still leaves one of the two global slots free.
	daemon := &Daemon{store: store, stateRoot: stateRoot, frontiers: map[string]*projectFrontierIndex{}, frontierHints: map[string][]daemonControlChange{}}
	var order []string
	daemon.fairDispatchRun = fairDispatchRecorder(&order)
	candidate := fairDispatchTestCandidate(project.ProjectID, "HLT-T-0001", "p1", "")
	candidate.Project = project
	candidate.Workflow, err = loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, note := range notes {
		if stringField(note.Data, "id") == "HLT-T-0001" {
			candidate.Note = note
			candidate.Run.RecordID = trackerRecordID(note)
		}
	}
	if err := daemon.dispatchFairCandidates(context.Background(), []daemonDispatchCandidate{candidate}, 2); err != nil {
		t.Fatal(err)
	}
	assertFairDispatchOrder(t, []string{"healthy/HLT-T-0001"}, order)
	// The recorder simulates dispatch without persisting the claimed run.
	claimed := candidate.Run
	claimed.LeaseState = string(LeaseStateClaimed)
	if err := store.UpsertRun(claimed); err != nil {
		t.Fatal(err)
	}
	runs, err = store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	findings := doctorCapacityFindings(vault, store, project.ProjectID, runs, DiagnosticScope{}, "HLT-T-0001", "now")
	if len(findings) != 1 || findings[0].Code != "doctor-global-capacity" || !strings.Contains(findings[0].Evidence.Detail, "(2/2)") {
		t.Fatalf("disabled project's run must still count: %#v", findings)
	}
}

func TestProjectsRemoveRefusesUnknownAndPreviews(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	project := RegisteredProject{ProjectID: "full-project-id", Name: "demo", RepoRoot: t.TempDir(), VaultRoot: t.TempDir()}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{project.ProjectID, "full"} {
		if err := store.UpsertRun(RunStatus{ProjectID: id, RecordID: "run", LeaseState: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	beforeProjects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	beforeRuns, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []string{"false", "true"} {
		if err := projectsRemoveCmd(Args{"id": "full", "dry-run": dryRun}); err == nil || !strings.Contains(err.Error(), "project not found") {
			t.Fatalf("unknown/truncated ID must refuse (dry-run=%s): %v", dryRun, err)
		}
	}
	output := captureStdout(t, func() {
		if err := projectsRemoveCmd(Args{"id": project.ProjectID, "dry-run": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var preview struct {
		DryRun      bool           `json:"dry_run"`
		ProjectID   string         `json:"project_id"`
		WouldDelete map[string]int `json:"would_delete"`
	}
	if err := json.Unmarshal([]byte(output), &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || preview.ProjectID != project.ProjectID || preview.WouldDelete["projects"] != 1 || preview.WouldDelete["runs"] != 1 || len(preview.WouldDelete) != 7 {
		t.Fatalf("incomplete removal preview: %s", output)
	}
	output = captureStdout(t, func() {
		if err := projectsRemoveCmd(Args{"id": project.ProjectID, "dry-run": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "no changes made") || !strings.Contains(output, "runs: 1 rows would be deleted") {
		t.Fatalf("missing text preview: %s", output)
	}
	afterProjects, err := store.ListProjects()
	if err != nil || !reflect.DeepEqual(beforeProjects, afterProjects) {
		t.Fatalf("refusal/preview changed registrations: %#v, err=%v", afterProjects, err)
	}
	afterRuns, err := store.ListRuns()
	if err != nil || !reflect.DeepEqual(beforeRuns, afterRuns) {
		t.Fatalf("refusal/preview changed runs: %#v, err=%v", afterRuns, err)
	}
	if err := projectsRemoveCmd(Args{"id": project.ProjectID}); err != nil {
		t.Fatal(err)
	}
	afterProjects, err = store.ListProjects()
	if err != nil || len(afterProjects) != 0 {
		t.Fatalf("real removal kept registration: %#v, err=%v", afterProjects, err)
	}
	afterRuns, err = store.ListRuns()
	if err != nil || len(afterRuns) != 1 || afterRuns[0].ProjectID != "full" {
		t.Fatalf("real removal purged the wrong runs: %#v, err=%v", afterRuns, err)
	}
}
