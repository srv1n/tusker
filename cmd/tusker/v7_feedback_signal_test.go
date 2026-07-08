package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFeedbackSignalValidationAcceptsModelAndInitialCategories(t *testing.T) {
	for _, category := range feedbackSignalCategoryNames() {
		signal := validFeedbackSignalForTest(category)
		if issues := validateFeedbackSignal(signal); len(issues) != 0 {
			t.Fatalf("expected %s signal to validate, got %#v", category, issues)
		}
		completed := completeFeedbackSignal(signal)
		if completed.Schema != feedbackSignalSchema {
			t.Fatalf("schema mismatch: %s", completed.Schema)
		}
		if completed.ID == "" || completed.Date == "" || completed.Project == "" || completed.Source == "" || completed.DedupeKey == "" {
			t.Fatalf("completed signal missed required identity fields: %#v", completed)
		}
	}
}

func TestFeedbackSignalValidationRejectsInvalidLabelsAndMissingDedupe(t *testing.T) {
	signal := validFeedbackSignalForTest("review_loop")
	signal.Category = "unknown"
	signal.Severity = "urgent"
	signal.Confidence = "certain"
	signal.DedupeKey = ""

	issues := validateFeedbackSignal(signal)
	for _, code := range []string{
		"FEEDBACK_SIGNAL_CATEGORY_INVALID",
		"FEEDBACK_SIGNAL_SEVERITY_INVALID",
		"FEEDBACK_SIGNAL_CONFIDENCE_INVALID",
		"FEEDBACK_SIGNAL_DEDUPE_KEY_MISSING",
	} {
		if !feedbackSignalIssuesContainCode(issues, code) {
			t.Fatalf("expected %s in %#v", code, issues)
		}
	}
}

func TestFeedbackSignalValidationRejectsRawPayloadAndUnsafeFacts(t *testing.T) {
	signal := validFeedbackSignalForTest("token_burn")
	signal.RawPayload = map[string]any{"transcript": "full raw event payload"}
	signal.ObservedFacts["raw_transcript"] = "function_call_output\nOriginal token count: 14000"
	signal.ObservedFacts["reason_excerpt"] = strings.Repeat("x", feedbackSignalMaxFactStringChars+1)

	issues := validateFeedbackSignal(signal)
	for _, code := range []string{
		"FEEDBACK_SIGNAL_RAW_PAYLOAD_FORBIDDEN",
		"FEEDBACK_SIGNAL_FACT_KEY_INVALID",
		"FEEDBACK_SIGNAL_FACT_VALUE_INVALID",
	} {
		if !feedbackSignalIssuesContainCode(issues, code) {
			t.Fatalf("expected %s in %#v", code, issues)
		}
	}
}

func TestDeriveFeedbackSignalsFromTaskAndEventInputs(t *testing.T) {
	input := feedbackSignalReducerInput{
		Date:    "2026-05-31",
		Project: "APP",
		Source:  "unit-test",
		Tasks: []feedbackSignalTaskInput{{
			ID:                  "APP-T-0001",
			Status:              "review",
			AcceptanceTotal:     4,
			AcceptanceSatisfied: 2,
			MissingAcceptance:   []string{"A3", "A4"},
			MissingProof:        []string{"focused_test"},
			ReviewTransitions:   2,
			ReworkTransitions:   1,
		}},
		Events: []feedbackSignalEventInput{{
			Kind:      "token_count",
			TaskID:    "APP-T-0001",
			AttemptID: "APP-T-0001-A-0001",
			Payload:   map[string]any{"total_tokens": 125000},
		}},
	}

	signals := deriveFeedbackSignals(input)
	if len(signals) != 3 {
		t.Fatalf("expected three derived signals, got %d: %#v", len(signals), signals)
	}
	byCategory := map[string]feedbackSignal{}
	for _, signal := range signals {
		byCategory[signal.Category] = signal
		if issues := validateFeedbackSignal(signal); len(issues) != 0 {
			t.Fatalf("derived signal should validate cleanly: %#v", issues)
		}
	}
	for _, category := range []string{"acceptance_quality", "review_loop", "token_burn"} {
		if byCategory[category].Category == "" {
			t.Fatalf("missing derived %s signal in %#v", category, signals)
		}
	}

	again := deriveFeedbackSignals(input)
	for i := range signals {
		if signals[i].DedupeKey != again[i].DedupeKey || signals[i].ID != again[i].ID {
			t.Fatalf("signal identity must be deterministic: %#v vs %#v", signals[i], again[i])
		}
	}
}

func TestFeedbackSignalsResolveReviewEventOwnershipFromTaskFile(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	writeFeedbackReviewEventFixture(t, vault, "RZN-T-0002", "BACKEND")
	writeFeedbackReviewEventFixture(t, vault, "RZN-T-0002", "BACKEND")
	writeFeedbackReviewEventFixture(t, vault, "RZN-T-0002", "BACKEND")

	eventOnly, _, err := deriveFeedbackSignalsForVault(vault, "2026-05-31", mustParseFeedbackSignalTestDate(t, "2026-05-30"))
	if err != nil {
		t.Fatal(err)
	}
	if len(eventOnly) != 1 {
		t.Fatalf("event-only review loop should collapse to one signal, got %#v", eventOnly)
	}
	assertEqual(t, "RZN", eventOnly[0].Project, "event-only project")
	assertEqual(t, "unknown", toString(eventOnly[0].ObservedFacts["status"]), "event-only status")

	writeFeedbackReducerTaskFixtureWithStatus(t, vault, "RZN-T-0002", "done")
	withTask, _, err := deriveFeedbackSignalsForVault(vault, "2026-05-31", mustParseFeedbackSignalTestDate(t, "2026-05-30"))
	if err != nil {
		t.Fatal(err)
	}
	var reviewSignals []feedbackSignal
	for _, signal := range withTask {
		if signal.Category == "review_loop" {
			reviewSignals = append(reviewSignals, signal)
		}
	}
	if len(reviewSignals) != 1 {
		t.Fatalf("event-plus-task-file review loop should be one canonical signal, got %#v", withTask)
	}
	assertEqual(t, "RZN", reviewSignals[0].Project, "task-file project")
	assertEqual(t, "done", toString(reviewSignals[0].ObservedFacts["status"]), "task-file status")
	if strings.Contains(strings.Join(feedbackSignalProjectsForTest(withTask), ","), "BACKEND") {
		t.Fatalf("event project leaked into canonical task findings: %#v", withTask)
	}
}

func TestAcceptanceQualitySignalGroupsReasonsAndFacts(t *testing.T) {
	signals := deriveFeedbackSignals(feedbackSignalReducerInput{
		Date:    "2026-05-31",
		Project: "APP",
		Source:  "unit-test",
		Tasks: []feedbackSignalTaskInput{{
			ID:                  "APP-T-0042",
			Status:              "done",
			AcceptanceIDs:       []string{"A1", "A2"},
			AcceptanceTotal:     2,
			AcceptanceSatisfied: 1,
			MissingAcceptance:   []string{"A2"},
			ProofMapGaps:        []string{"A1"},
			ProofEvidenceGaps:   []string{"focused_test"},
		}},
	})
	if len(signals) != 1 {
		t.Fatalf("acceptance quality should emit one task-level signal, got %#v", signals)
	}
	signal := signals[0]
	assertEqual(t, "acceptance_quality", signal.Category, "category")
	assertEqual(t, "P1", signal.Severity, "done-with-gaps severity")
	for _, label := range []string{"missing-acceptance", "missing-proof-map", "missing-proof-evidence", "done-with-gaps"} {
		if !containsString(normalizeList(signal.ObservedFacts["reason_labels"]), label) {
			t.Fatalf("missing reason label %q in %#v", label, signal.ObservedFacts["reason_labels"])
		}
	}

	raw, err := json.Marshal(signal)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		ObservedFacts map[string]any `json:"observed_facts"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"A1", "A2"}, normalizeList(stored.ObservedFacts["acceptance_ids"]), "acceptance ids")
	assertEqual(t, []string{"A2"}, normalizeList(stored.ObservedFacts["acceptance_gaps"]), "acceptance gaps")
	assertEqual(t, []string{"A1"}, normalizeList(stored.ObservedFacts["proof_map_gaps"]), "proof map gaps")
	assertEqual(t, []string{"focused_test"}, normalizeList(stored.ObservedFacts["proof_evidence_gaps"]), "proof evidence gaps")
	if _, ok := stored.ObservedFacts["missing_proof"]; ok {
		t.Fatalf("observed facts should not collapse proof-map and evidence gaps into missing_proof: %#v", stored.ObservedFacts)
	}
}

func TestWriteFeedbackSignalUsesDurableStorageContract(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	signal := validFeedbackSignalForTest("review_loop")

	path, err := writeFeedbackSignal(vault, signal)
	if err != nil {
		t.Fatal(err)
	}
	expectedDir := filepath.Join(vault, "feedback", "signals", "2026-05-31")
	if !strings.HasPrefix(path, expectedDir+string(filepath.Separator)) {
		t.Fatalf("signal path should use durable storage contract under %s, got %s", expectedDir, path)
	}
	assertExists(t, path)

	repeatPath, err := writeFeedbackSignal(vault, signal)
	if err != nil {
		t.Fatal(err)
	}
	if path != repeatPath {
		t.Fatalf("signal write path should be deterministic, got %s then %s", path, repeatPath)
	}

	raw, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored feedbackSignal
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Schema != feedbackSignalSchema || stored.Category != "review_loop" || stored.RawPayload != nil {
		t.Fatalf("stored signal mismatch: %#v", stored)
	}
}

func TestFeedbackSignalHelpDistinguishesEventsNotesAndSignals(t *testing.T) {
	help := feedbackSignalHelpText()
	for _, expected := range []string{
		"Events are history",
		"Feedback notes are subjective input",
		"Signals are derived product facts",
		"tusker/feedback/signals/YYYY-MM-DD/*.json",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("signal help missing %q:\n%s", expected, help)
		}
	}
}

func writeFeedbackReviewEventFixture(t *testing.T, vault, taskID, project string) {
	t.Helper()
	eventsDir := filepath.Join(vault, "events")
	entries, _ := filepath.Glob(filepath.Join(eventsDir, "*.json"))
	path := filepath.Join(eventsDir, fmt.Sprintf("%03d.json", len(entries)+1))
	body := fmt.Sprintf(`{"at":"2026-05-31T00:00:00Z","project":%q,"object_kind":"task","object":%q,"event_kind":"review_requested","payload":{"to_status":"review","task":%q}}`, project, taskID, taskID)
	if err := writeText(path, body+"\n"); err != nil {
		t.Fatal(err)
	}
}

func mustParseFeedbackSignalTestDate(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func feedbackSignalProjectsForTest(signals []feedbackSignal) []string {
	var projects []string
	for _, signal := range signals {
		projects = append(projects, signal.Project)
	}
	return projects
}

func validFeedbackSignalForTest(category string) feedbackSignal {
	dedupeKey := feedbackSignalDedupeKey("APP", category, "APP-T-0001", "unit-test")
	return feedbackSignal{
		Schema:     feedbackSignalSchema,
		Date:       "2026-05-31",
		Project:    "APP",
		TaskID:     "APP-T-0001",
		AttemptID:  "APP-T-0001-A-0001",
		Source:     "unit-test",
		Category:   category,
		Severity:   "medium",
		Confidence: "high",
		DedupeKey:  dedupeKey,
		Summary:    "APP-T-0001 repeated a review loop with unresolved proof gaps.",
		ObservedFacts: map[string]any{
			"task":               "APP-T-0001",
			"review_transitions": 2,
			"rework_transitions": 1,
			"reason_excerpt":     "focused proof was still pending",
		},
		Recommendation: "Review proof gaps before another handoff.",
	}
}

func feedbackSignalIssuesContainCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
