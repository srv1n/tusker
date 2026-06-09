package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedbackSignalsCommandWritesDerivedSignals(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	writeFeedbackReducerTaskFixture(t, vault, "APP-T-0001")

	if err := feedbackSignalsCmd(Args{
		"vault": vault,
		"since": "2026-05-30",
		"date":  "2026-05-31",
		"write": "true",
	}); err != nil {
		t.Fatal(err)
	}

	files := feedbackReducerJSONFiles(t, filepath.Join(vault, "feedback", "signals", "2026-05-31"))
	if len(files) == 0 {
		t.Fatal("expected feedback signals to be written")
	}
	text, err := readText(files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"schema": "tusker.feedback_signal/v1"`,
		`"category": "acceptance_quality"`,
		`"dedupe_key":`,
		`"observed_facts":`,
	} {
		assertContainsIndexTest(t, text, expected)
	}
}

func TestFeedbackReviewCommandWritesDailyPacket(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	writeFeedbackReducerTaskFixture(t, vault, "APP-T-0001")

	if err := feedbackReviewCmd(Args{
		"vault": vault,
		"since": "2026-05-30",
		"date":  "2026-05-31",
		"write": "true",
	}); err != nil {
		t.Fatal(err)
	}

	reviewPath := filepath.Join(vault, "feedback", "reviews", "2026-05-31.md")
	text, err := readText(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"## Facts",
		"## Likely Causes",
		"## Proposed Actions",
		"bad task contract, not implementation alone",
	} {
		assertContainsIndexTest(t, text, expected)
	}
}

func TestFeedbackPromoteCommandDefaultsDryRunAndWritesOneTask(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	reviewPath := filepath.Join(vault, "feedback", "reviews", "2026-05-31.md")
	if err := writeText(reviewPath, "# Tusker Daily Review - 2026-05-31\n"); err != nil {
		t.Fatal(err)
	}

	if err := feedbackPromoteCmd(Args{
		"vault":  vault,
		"review": reviewPath,
		"date":   "2026-05-31",
	}); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(vault, "work", "tasks", "VSD-T-0001.md")) {
		t.Fatal("dry-run promotion created a task")
	}

	if err := feedbackPromoteCmd(Args{
		"vault":  vault,
		"review": reviewPath,
		"date":   "2026-05-31",
		"write":  "true",
	}); err != nil {
		t.Fatal(err)
	}
	text, err := readText(filepath.Join(vault, "work", "tasks", "VSD-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"Source feedback:",
		"Prevention statement:",
		"Promoted from feedback:",
	} {
		assertContainsIndexTest(t, text, expected)
	}
}

func writeFeedbackReducerTaskFixture(t *testing.T, vault, id string) {
	t.Helper()
	content := strings.Join([]string{
		"---",
		`schema: "tusker.task/v7"`,
		`kind: "task"`,
		`id: "` + id + `"`,
		`project: "tusker"`,
		`title: "TBD"`,
		`status: "review"`,
		`readiness: "waiting_on_review"`,
		`updated_at: "2026-05-31T00:00:00Z"`,
		"---",
		"",
		"# " + id + " · Thin task",
		"",
		"## Acceptance",
		"",
		"| ID | Outcome | Proof |",
		"|---|---|---|",
		"| A1 | Complete the task contract. | Inline verification, evidence, gate, or waiver |",
		"",
		"## Verification",
		"",
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | TBD | pending | Define proof. |",
		"",
	}, "\n")
	if err := writeText(filepath.Join(vault, "work", "tasks", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func feedbackReducerJSONFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, entries...)
	return files
}
