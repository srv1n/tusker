package main

import "testing"

func TestReviewResultProtocolLegacyFindingMigration(t *testing.T) {
	note := Note{Data: map[string]any{"id": "APP-T-0001", "state_rev": "sha256:state", "work_revision": 2}, Body: "## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | test | fail | " + reviewerFindingRowMarker("review-1") + " actionable regression |\n"}
	result, ok := legacyReviewerFindingResult(note, "review-1")
	if !ok || result.TaskID != "APP-T-0001" || result.AttemptID != "review-1" || result.Verdict != "changes_requested" || len(result.Findings) != 1 {
		t.Fatalf("legacy result=%#v ok=%v", result, ok)
	}
}

func TestReviewResultProtocolStoreReplayAndConflict(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := ReviewResult{Schema: reviewResultSchema, ProjectID: "app", TaskID: "APP-T-0001", WorkRevision: 1, AttemptID: "review-1", Verdict: "blocked", Summary: "runner unavailable"}
	replay, err := store.SaveReviewResult(result)
	if err != nil || replay {
		t.Fatalf("first save replay=%v err=%v", replay, err)
	}
	replay, err = store.SaveReviewResult(result)
	if err != nil || !replay {
		t.Fatalf("exact replay replay=%v err=%v", replay, err)
	}
	result.Verdict = "pass"
	if _, err := store.SaveReviewResult(result); err == nil {
		t.Fatal("conflicting second verdict accepted")
	}
}
