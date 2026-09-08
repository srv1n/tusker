package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderDirectPredecessorResultsUsesProjectScopedRun(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, run := range []RunStatus{
		{ProjectID: "other", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Terminal: true, AttemptOutcome: string(AttemptOutcomeSucceeded), FinalSummary: "wrong project"},
		{ProjectID: "wanted", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, Terminal: true, AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 2},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}
	attempt := RunAttempt{AttemptID: "attempt-1", ProjectID: "wanted", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, Outcome: string(AttemptOutcomeSucceeded), FinalSummary: "expected predecessor", LogsSummary: "checks passed", ApplyRef: "abc123"}
	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: attempt.AttemptID, ProjectID: attempt.ProjectID, RecordID: attempt.RecordID, ItemID: attempt.ItemID, Lane: attempt.Lane, Outcome: attempt.Outcome}); err != nil {
		t.Fatal(err)
	}

	got := renderDirectPredecessorResults(store, "wanted", Note{Data: map[string]any{"dependencies": []string{"APP-T-0001:hard"}}}, t.TempDir())
	if !strings.Contains(got, "`APP-T-0001`: result=expected predecessor") || strings.Contains(got, "completed result unavailable") || strings.Contains(got, "wrong project") {
		t.Fatalf("project-scoped predecessor result not rendered: %s", got)
	}
}

func TestLatestDispatchRunUsesProjectScope(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, project := range []string{"first", "second"} {
		if err := store.UpsertRun(RunStatus{ProjectID: project, RecordID: "APP-T-0001", ItemID: "APP-T-0001", WorkRevision: len(project)}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := (&Daemon{store: store}).latestDispatchRun(RunStatus{ProjectID: "second", RecordID: "APP-T-0001"})
	if err != nil || got.ProjectID != "second" {
		t.Fatalf("project-scoped dispatch lookup = %#v, %v", got, err)
	}
}

func TestDaemonEvidenceDoesNotInventKindOrCoverage(t *testing.T) {
	workspace := t.TempDir()
	if err := writeText(filepath.Join(workspace, "result.txt"), "result\n"); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{RecordID: "APP-T-0001", WorkspacePath: workspace}
	for _, contract := range []map[string]any{
		{"kind": "unknown", "path": "result.txt", "acceptance_ids": []string{"A1"}},
		{"kind": "diff_summary", "path": "result.txt"},
	} {
		note := Note{Data: map[string]any{"artifact_contract": contract}, Body: "## Acceptance\n\n- A1: works\n"}
		if err := recordDaemonImplementationEvidence(t.TempDir(), note, run); err == nil {
			t.Fatalf("unsafe artifact contract was accepted: %#v", contract)
		}
	}
}
