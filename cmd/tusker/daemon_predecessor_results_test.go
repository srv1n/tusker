package main

import (
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
		{ProjectID: "wanted", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Terminal: true, AttemptOutcome: string(AttemptOutcomeSucceeded), FinalSummary: "expected predecessor", LogsSummary: "checks passed", ApplyRef: "abc123", WorkRevision: 2},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}

	got := renderDirectPredecessorResults(store, "wanted", Note{Data: map[string]any{"dependencies": []string{"APP-T-0001:hard"}}}, t.TempDir())
	if !strings.Contains(got, "`APP-T-0001`: result=expected predecessor") || strings.Contains(got, "completed result unavailable") || strings.Contains(got, "wrong project") {
		t.Fatalf("project-scoped predecessor result not rendered: %s", got)
	}
}
