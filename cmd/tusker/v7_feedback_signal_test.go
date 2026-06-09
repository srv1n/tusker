package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
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
