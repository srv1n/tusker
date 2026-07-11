package main

import (
	"encoding/json"
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

func TestFeedbackSignalsMultiVaultCollapsesRZNLanes(t *testing.T) {
	repos := writeFeedbackRZNLaneFixtures(t)

	output := captureStdout(t, func() {
		if err := feedbackSignalsCmd(Args{
			"repo":  strings.Join(repos, ","),
			"since": "2026-05-30",
			"date":  "2026-05-31",
			"json":  "true",
		}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Counts struct {
			RawEmitted     int      `json:"raw_emitted"`
			Collapsed      int      `json:"collapsed"`
			Skipped        int      `json:"skipped"`
			NoExplicitNote []string `json:"no_explicit_note"`
		} `json:"counts"`
		Signals []feedbackSignal `json:"signals"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 3, payload.Counts.RawEmitted, "raw emitted count")
	assertEqual(t, 1, payload.Counts.Collapsed, "collapsed count")
	assertEqual(t, 0, payload.Counts.Skipped, "skipped count")
	assertEqual(t, 3, len(payload.Counts.NoExplicitNote), "no explicit note diagnostics")
	if len(payload.Signals) != 1 {
		t.Fatalf("expected one collapsed signal, got %#v", payload.Signals)
	}
	assertEqual(t, "RZN", payload.Signals[0].Project, "collapsed project")
	assertEqual(t, 3, len(payload.Signals[0].Occurrences), "occurrence count")
	for _, occurrence := range payload.Signals[0].Occurrences {
		if occurrence.Vault == "" || occurrence.Project != "RZN" || occurrence.Source == "" {
			t.Fatalf("occurrence should record vault, project, and source: %#v", occurrence)
		}
	}
}

func TestFeedbackReviewReportsCollapseDiagnostics(t *testing.T) {
	repos := writeFeedbackRZNLaneFixtures(t)
	args := Args{
		"repo":  strings.Join(repos, ","),
		"since": "2026-05-30",
		"date":  "2026-05-31",
	}

	jsonOutput := captureStdout(t, func() {
		jsonArgs := Args{}
		for key, value := range args {
			jsonArgs[key] = value
		}
		jsonArgs["json"] = "true"
		if err := feedbackReviewCmd(jsonArgs); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Counts struct {
			RawEmitted     int      `json:"raw_emitted"`
			Collapsed      int      `json:"collapsed"`
			Skipped        int      `json:"skipped"`
			NoExplicitNote []string `json:"no_explicit_note"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 3, payload.Counts.RawEmitted, "review raw emitted count")
	assertEqual(t, 1, payload.Counts.Collapsed, "review collapsed count")
	assertEqual(t, 0, payload.Counts.Skipped, "review skipped count")
	assertEqual(t, 3, len(payload.Counts.NoExplicitNote), "review no explicit note diagnostics")

	markdown := captureStdout(t, func() {
		if err := feedbackReviewCmd(args); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		"Raw signals emitted: 3",
		"Collapsed findings: 1",
		"Signals skipped: 0",
		"No explicit feedback notes:",
		"frequency 3",
	} {
		assertContainsIndexTest(t, markdown, expected)
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

func TestFeedbackPromoteCommandSelectsFindingPreservesSourceRefsAndDoesNotDispatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(root, "state"))
	vault := filepath.Join(root, "tusker")
	if err := writeText(filepath.Join(vault, "feedback", "agents", "2026-05-21-codex-promote-finding.md"), strings.Join([]string{
		"# Agent Feedback",
		"",
		"- context: Review produced a selectable finding from a feedback note.",
		"- friction: Promotion could only target generic review files.",
		"- product-idea: Promote one structured finding while preserving source refs.",
		"- impact: Feedback creates visible Tusker work without hidden fanout.",
		"- related: tusker feedback promote",
		"- affected-command: tusker feedback promote",
		"- priority-hint: P1",
		"- dedupe-key: feedback-promote-finding",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}

	reviewJSON := captureStdout(t, func() {
		if err := feedbackReviewCmd(Args{"vault": vault, "since": "2026-05-20", "date": "2026-05-22", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var reviewPayload struct {
		Findings []map[string]any `json:"findings"`
	}
	if err := json.Unmarshal([]byte(reviewJSON), &reviewPayload); err != nil {
		t.Fatal(err)
	}
	if len(reviewPayload.Findings) == 0 {
		t.Fatalf("expected a selectable finding, got %s", reviewJSON)
	}
	findingID := toString(reviewPayload.Findings[0]["id"])

	promoteJSON := captureStdout(t, func() {
		if err := feedbackPromoteCmd(Args{"vault": vault, "finding": findingID, "since": "2026-05-20", "date": "2026-05-22", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var promotePayload struct {
		Mode     string `json:"mode"`
		Outcomes []struct {
			Operation  string   `json:"Operation"`
			SourceRefs []string `json:"SourceRefs"`
			DedupeKey  string   `json:"DedupeKey"`
		} `json:"outcomes"`
	}
	if err := json.Unmarshal([]byte(promoteJSON), &promotePayload); err != nil {
		t.Fatal(err)
	}
	if promotePayload.Mode != feedbackPromoteModeDryRun || len(promotePayload.Outcomes) != 1 || promotePayload.Outcomes[0].Operation != "create" {
		t.Fatalf("expected dry-run create outcome, got %s", promoteJSON)
	}
	if fileExists(filepath.Join(vault, "work", "tasks", "FBK-T-0001.md")) {
		t.Fatal("dry-run selected finding promotion created a task")
	}
	if !containsString(promotePayload.Outcomes[0].SourceRefs, "feedback-note:"+filepath.Base(root)+":feedback/agents/2026-05-21-codex-promote-finding.md") &&
		!strings.Contains(strings.Join(promotePayload.Outcomes[0].SourceRefs, "\n"), "feedback/agents/2026-05-21-codex-promote-finding.md") {
		t.Fatalf("promotion did not preserve source note refs: %#v", promotePayload.Outcomes[0].SourceRefs)
	}

	if err := feedbackPromoteCmd(Args{"vault": vault, "finding": findingID, "since": "2026-05-20", "date": "2026-05-22", "write": "true", "epic": "FBK", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "FBK-T-0001.md")
	task, err := readText(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`dedupe_key: "feedback-promote-finding"`,
		"source_refs:",
		"feedback/agents/2026-05-21-codex-promote-finding.md",
		"Source feedback:",
		"Prevention statement:",
	} {
		assertContainsIndexTest(t, task, expected)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("promotion should not auto-dispatch fanout runs, got %#v", runs)
	}
}

func writeFeedbackReducerTaskFixture(t *testing.T, vault, id string) {
	t.Helper()
	writeFeedbackReducerTaskFixtureWithStatus(t, vault, id, "review")
}

func writeFeedbackReducerTaskFixtureWithStatus(t *testing.T, vault, id, status string) {
	t.Helper()
	readiness := "waiting_on_review"
	if status == "done" {
		readiness = "done"
	}
	content := strings.Join([]string{
		"---",
		`schema: "tusker.task/v7"`,
		`kind: "task"`,
		`id: "` + id + `"`,
		`project: "tusker"`,
		`title: "TBD"`,
		`status: "` + status + `"`,
		`readiness: "` + readiness + `"`,
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

func writeFeedbackRZNLaneFixtures(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	var repos []string
	for _, lane := range []string{"rzn-web", "rzn-api", "rzn-worker"} {
		repo := filepath.Join(root, lane)
		vault := filepath.Join(repo, ".tusker")
		writeFeedbackReducerTaskFixture(t, vault, "RZN-T-0003")
		repos = append(repos, repo)
	}
	return repos
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
