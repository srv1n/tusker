package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedbackReviewRanksAndRendersPacket(t *testing.T) {
	signals := []feedbackReviewSignal{
		{
			ID:             "SIG-CLI",
			Date:           "2026-05-23",
			Task:           "APP-T-0005",
			Category:       "cli_friction",
			Severity:       "P2",
			Confidence:     "high",
			Frequency:      10,
			Summary:        "CLI help buries the validation retry hint.",
			ObservedFacts:  []string{"10 agents searched docs before finding the retry command."},
			Recommendation: "Show the retry command next to validation failures.",
		},
		{
			ID:             "SIG-ACCEPT",
			Date:           "2026-05-22",
			Task:           "APP-T-0002",
			Category:       "acceptance_quality",
			Severity:       "P1",
			Confidence:     "high",
			Frequency:      2,
			Summary:        "Thin acceptance criteria caused proof churn.",
			ObservedFacts:  []string{"A1 used vague success wording.", "Review sent the task back for missing proof mapping."},
			Recommendation: "Require concrete acceptance and proof rows before dispatch.",
		},
		{
			ID:             "SIG-REVIEW",
			Date:           "2026-05-23",
			Task:           "APP-T-0003",
			Category:       "review_loop",
			Severity:       "P1",
			Confidence:     "high",
			Frequency:      2,
			Summary:        "Review loops repeated after handoff.",
			ObservedFacts:  []string{"Task had two review requests and one rework transition."},
			Recommendation: "Create a product ticket for early review expectations.",
		},
		{
			ID:             "SIG-TOKEN",
			Date:           "2026-05-21",
			Task:           "APP-T-0001",
			Category:       "token_burn",
			Severity:       "P1",
			Confidence:     "high",
			Frequency:      4,
			Summary:        "Token burn repeated around proof churn.",
			ObservedFacts:  []string{"Four turns exceeded the token budget without a state transition."},
			Recommendation: "Create a product ticket for proof-churn token burn.",
		},
		{
			ID:             "SIG-LOWCONF",
			Date:           "2026-05-23",
			Task:           "APP-T-0004",
			Category:       "token_burn",
			Severity:       "P1",
			Confidence:     "low",
			Frequency:      20,
			Summary:        "Possible token burn without enough supporting evidence.",
			ObservedFacts:  []string{"Token total was high, but transition data is incomplete."},
			Recommendation: "Wait for stronger token and state transition evidence.",
		},
	}

	packet := buildFeedbackReviewPacket("2026-05-23", "2026-05-20", signals)
	got := renderFeedbackReviewPacketMarkdown(packet)

	for _, expected := range []string{
		"## Facts",
		"## Likely Causes",
		"## Proposed Actions",
		"## Needs Human Decision",
		"**product ticket** - Create a product ticket for early review expectations.",
		"**acceptance-criteria fix** - Require concrete acceptance and proof rows before dispatch.",
		"**CLI hint** - Show the retry command next to validation failures.",
		"bad task contract, not implementation alone",
		"Citation: signals SIG-ACCEPT; tasks APP-T-0002; dates 2026-05-22.",
		"Prevents recurrence by requiring observable acceptance criteria and a proof map before the task is runnable.",
	} {
		assertContainsIndexTest(t, got, expected)
	}

	first := strings.Index(got, "token burn: Token burn repeated")
	second := strings.Index(got, "review loop: Review loops repeated")
	third := strings.Index(got, "acceptance quality: Thin acceptance criteria")
	fourth := strings.Index(got, "token burn: Possible token burn without enough")
	fifth := strings.Index(got, "cli friction: CLI help buries")
	if first < 0 || second < 0 || third < 0 || fourth < 0 || fifth < 0 {
		t.Fatalf("expected ranked facts in review:\n%s", got)
	}
	if !(first < second && second < third && third < fourth && fourth < fifth) {
		t.Fatalf("findings were not sorted by severity, confidence, frequency, then recency:\n%s", got)
	}
}

func TestFeedbackReviewClassifiesActionTypes(t *testing.T) {
	tests := []struct {
		name     string
		signal   feedbackReviewSignal
		expected string
	}{
		{
			name:     "acceptance",
			expected: "acceptance-criteria fix",
			signal: feedbackReviewSignal{
				ID:         "SIG-A",
				Date:       "2026-05-23",
				Task:       "APP-T-0001",
				Category:   "acceptance_quality",
				Severity:   "P1",
				Confidence: "high",
				Summary:    "Acceptance was vague.",
			},
		},
		{
			name:     "runbook",
			expected: "skill/runbook update",
			signal: feedbackReviewSignal{
				ID:         "SIG-R",
				Date:       "2026-05-23",
				Task:       "APP-T-0002",
				Category:   "workflow_repeat",
				Severity:   "P2",
				Confidence: "medium",
				Summary:    "Setup steps repeated across tasks.",
			},
		},
		{
			name:     "policy",
			expected: "guardrail/policy update",
			signal: feedbackReviewSignal{
				ID:         "SIG-P",
				Date:       "2026-05-23",
				Task:       "APP-T-0003",
				Category:   "closeout_churn",
				Severity:   "P2",
				Confidence: "medium",
				Summary:    "Closeout stop conditions were ambiguous.",
			},
		},
		{
			name:     "decision",
			expected: "decision",
			signal: feedbackReviewSignal{
				ID:             "SIG-D",
				Date:           "2026-05-23",
				Task:           "APP-T-0004",
				Category:       "review_loop",
				Severity:       "P2",
				Confidence:     "medium",
				Summary:        "Human product choice blocks the workflow.",
				Recommendation: "Choose whether manual UI smoke remains mandatory.",
			},
		},
		{
			name:     "noise",
			expected: "ignore-as-noise",
			signal: feedbackReviewSignal{
				ID:         "SIG-N",
				Date:       "2026-05-23",
				Task:       "APP-T-0005",
				Category:   "noise",
				Severity:   "info",
				Confidence: "low",
				Summary:    "One weak signal without recurrence.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet := buildFeedbackReviewPacket("2026-05-23", "2026-05-20", []feedbackReviewSignal{tt.signal})
			if len(packet.Findings) != 1 {
				t.Fatalf("expected one finding, got %#v", packet.Findings)
			}
			if packet.Findings[0].ActionType != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, packet.Findings[0].ActionType)
			}
		})
	}
}

func TestFeedbackReviewReadsSignalFilesAndOmitsRawLogs(t *testing.T) {
	root := t.TempDir()
	if err := writeText(filepath.Join(root, "2026-05-23", "signals.json"), `[
  {
    "id": "SIG-FILE-1",
    "date": "2026-05-23T09:20:00Z",
    "task_id": "APP-T-0100",
    "category": "token_burn",
    "severity": "P1",
    "confidence": "high",
    "frequency": 3,
    "summary": "Token burn repeated around proof churn.",
    "observed_facts": [
      "goroutine 1 [running]: raw stack frame",
      "Task APP-T-0100 requested review twice without accepted proof."
    ],
    "recommendation": "Create a product ticket for proof-churn token burn."
  }
]`); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(root, "more.json"), `{
  "signals": [
    {
      "signal_id": "SIG-FILE-2",
      "date": "2026-05-23",
      "tasks": ["APP-T-0101"],
      "category": "cli_friction",
      "priority": "P2",
      "confidence": "medium",
      "facts": ["FAIL: full command output should stay out of review packets."],
      "summary": "CLI retry hint was hard to find.",
      "recommended_action": "Add a focused CLI hint."
    }
  ]
}`); err != nil {
		t.Fatal(err)
	}

	signals, err := feedbackReviewSignalsFromFiles([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected two parsed signals, got %#v", signals)
	}

	got := renderFeedbackReviewPacketMarkdown(buildFeedbackReviewPacket("2026-05-23", "2026-05-23", signals))
	for _, expected := range []string{
		"SIG-FILE-1",
		"SIG-FILE-2",
		"APP-T-0100",
		"APP-T-0101",
		"Task APP-T-0100 requested review twice without accepted proof.",
	} {
		assertContainsIndexTest(t, got, expected)
	}
	for _, forbidden := range []string{"goroutine 1", "raw stack frame", "FAIL: full command output"} {
		assertNotContainsIndexTest(t, got, forbidden)
	}
}

func TestFeedbackReviewEmptyAndNoiseDaysRecommendNoProductAction(t *testing.T) {
	empty := renderFeedbackReviewPacketMarkdown(buildFeedbackReviewPacket("2026-05-23", "2026-05-23", nil))
	for _, expected := range []string{
		"Recommendation: no product action recommended",
		"## Facts",
		"## Likely Causes",
		"## Proposed Actions",
		"## Needs Human Decision",
		"No actionable feedback signals were present",
	} {
		assertContainsIndexTest(t, empty, expected)
	}

	noiseSignal := feedbackReviewSignal{
		ID:         "SIG-NOISE",
		Date:       "2026-05-23",
		Task:       "APP-T-0999",
		Category:   "noise",
		Severity:   "info",
		Confidence: "low",
		Summary:    "One-off local flake without recurrence.",
	}
	noise := renderFeedbackReviewPacketMarkdown(buildFeedbackReviewPacket("2026-05-23", "2026-05-23", []feedbackReviewSignal{noiseSignal}))
	for _, expected := range []string{
		"Recommendation: no product action recommended",
		"Only low-confidence or one-off noise was present",
		"**ignore-as-noise**",
		"Citation: signals SIG-NOISE; tasks APP-T-0999; dates 2026-05-23.",
	} {
		assertContainsIndexTest(t, noise, expected)
	}
}
