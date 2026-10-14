package main

import (
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

func feedbackSignalIssuesContainCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func validFeedbackSignalForTest(category string) feedbackSignal {
	var parts []string
	for _, part := range []string{"APP", category, "APP-T-0001", "unit-test"} {
		parts = append(parts, normalizedFeedbackSignalDedupeKey(part))
	}
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
		DedupeKey:  strings.Join(parts, ":"),
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
