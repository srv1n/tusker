package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedbackPromotePlannerDefaultsDryRunAndCreatesCLIProposalWithPrevention(t *testing.T) {
	record := feedbackRecord{
		RelativePath:    "feedback/agents/2026-05-31-codex-proof-noise.md",
		Date:            "2026-05-31",
		PriorityHint:    "P1",
		AffectedCommand: "tusker validate",
		Fields: map[string]string{
			"context":      "Agents repeatedly hit validation output that mixes owned and unrelated changes.",
			"friction":     "Agents waste turns sorting unrelated workspace churn.",
			"product-idea": "Scope validation output to owned changes by default.",
			"impact":       "Promoted feedback becomes actionable without drowning the backlog.",
			"related":      "tusker validate",
			"dedupe-key":   "validation-owned-scope",
		},
	}

	plan, err := planFeedbackSignalPromotion(record, feedbackPromoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != feedbackPromoteModeDryRun || !plan.DryRun {
		t.Fatalf("promotion planner must default to dry-run, got mode=%q dry=%v", plan.Mode, plan.DryRun)
	}
	if len(plan.Outcomes) != 1 {
		t.Fatalf("expected exactly one outcome, got %#v", plan.Outcomes)
	}
	outcome := plan.Outcomes[0]
	assertEqual(t, "create", outcome.Operation, "operation")
	assertEqual(t, "cli_proposal", outcome.Kind, "kind")
	if outcome.Prevention == "" || !strings.Contains(outcome.Prevention, "Scope validation output") {
		t.Fatalf("expected generated prevention statement, got %q", outcome.Prevention)
	}
	if !containsString(outcome.SourceRefs, "feedback/agents/2026-05-31-codex-proof-noise.md") {
		t.Fatalf("expected source ref to include feedback path, got %#v", outcome.SourceRefs)
	}
	if plan.Summary.Created != 1 || plan.Summary.Skipped != 0 {
		t.Fatalf("unexpected summary: %#v", plan.Summary)
	}
}

func TestFeedbackPromotePlannerSkipsLowPrioritySingleEvidence(t *testing.T) {
	plan, err := planFeedbackPromotion(feedbackPromoteSource{
		Kind:        "daily_review_action",
		ID:          "review-2026-05-31#slow-help",
		Title:       "Polish help text for a rare command",
		ProductIdea: "Make rarely used help output friendlier.",
		Friction:    "A single P3 report found wording awkward.",
		Severity:    "P3",
		Evidence: []feedbackPromoteEvidence{{
			Source: "review",
			Ref:    "feedback/reviews/2026-05-31.md#slow-help",
		}},
	}, feedbackPromoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := plan.Outcomes[0]
	assertEqual(t, "skip", outcome.Operation, "operation")
	assertEqual(t, "skip", outcome.Kind, "kind")
	if !strings.Contains(strings.Join(outcome.Reasons, "\n"), "repeated evidence or P0/P1") {
		t.Fatalf("skip should explain promotion threshold, got %#v", outcome.Reasons)
	}
}

func TestFeedbackPromotePlannerAllowsRepeatedEvidenceWithoutP1(t *testing.T) {
	plan, err := planFeedbackPromotion(feedbackPromoteSource{
		Kind:        "daily_review_action",
		ID:          "review-2026-05-31#runbook",
		Title:       "Provider setup workflow keeps recurring",
		ProductIdea: "Draft a provider setup runbook.",
		Friction:    "Agents rediscover provider setup steps.",
		Severity:    "P2",
		Evidence: []feedbackPromoteEvidence{
			{Source: "feedback", Ref: "feedback/agents/2026-05-30-a.md"},
			{Source: "feedback", Ref: "feedback/agents/2026-05-31-b.md"},
		},
	}, feedbackPromoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := plan.Outcomes[0]
	assertEqual(t, "create", outcome.Operation, "operation")
	assertEqual(t, "runbook", outcome.Kind, "kind")
	if !strings.Contains(strings.Join(outcome.Reasons, "\n"), "repeated evidence count is 2") {
		t.Fatalf("expected repeated-evidence reason, got %#v", outcome.Reasons)
	}
}

func TestFeedbackPromotePlannerRoutesSensitivePolicyToDecision(t *testing.T) {
	plan, err := planFeedbackPromotion(feedbackPromoteSource{
		ID:          "review-2026-05-31#privacy",
		Title:       "Decide retention policy for feedback digests",
		ProductIdea: "Choose whether privacy-sensitive feedback is retained in repo-local digests.",
		Friction:    "The product policy is unclear for customer data references.",
		Severity:    "P1",
		Tags:        []string{"privacy", "policy"},
	}, feedbackPromoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := plan.Outcomes[0]
	assertEqual(t, "create", outcome.Operation, "operation")
	assertEqual(t, "decision", outcome.Kind, "kind")
	if !outcome.NeedsHumanDecision {
		t.Fatal("decision outcome should be marked as needing human decision")
	}
	if toString(outcome.ProposedFields["suggested_resolution"]) == "" {
		t.Fatalf("decision should include suggested resolution, got %#v", outcome.ProposedFields)
	}
}

func TestFeedbackPromotePlannerRoutesCredentialAccessToGate(t *testing.T) {
	plan, err := planFeedbackPromotion(feedbackPromoteSource{
		ID:          "review-2026-05-31#provider-access",
		Title:       "Provider credential setup blocks smoke tests",
		ProductIdea: "Have a human provision provider credentials before release smoke.",
		Friction:    "Agents cannot complete live provider checks without access.",
		Severity:    "P1",
	}, feedbackPromoteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := plan.Outcomes[0]
	assertEqual(t, "create", outcome.Operation, "operation")
	assertEqual(t, "gate", outcome.Kind, "kind")
	assertEqual(t, "auth", toString(outcome.ProposedFields["gate_kind"]), "gate kind")
	if !outcome.NeedsHumanDecision {
		t.Fatal("gate outcome should be counted as human/external decision work")
	}
}

func TestFeedbackPromoteDuplicateMatchingUsesDedupeSourceTitleAndTask(t *testing.T) {
	existing := []feedbackPromoteExistingWork{
		{
			ID:         "APP-T-0001",
			Kind:       "task",
			Title:      "Scope validation output to owned changes by default",
			Path:       "work/tasks/APP-T-0001.md",
			DedupeKeys: []string{"validation-owned-scope"},
		},
		{
			ID:         "APP-T-0002",
			Kind:       "task",
			Title:      "Keep feedback linked to source signals",
			Path:       "work/tasks/APP-T-0002.md",
			SourceRefs: []string{"feedback/agents/2026-05-30-codex-source.md"},
		},
		{
			ID:    "APP-T-0003",
			Kind:  "task",
			Title: "Create daily review packet",
			Path:  "work/tasks/APP-T-0003.md",
		},
		{
			ID:           "APP-T-0004",
			Kind:         "task",
			Title:        "Link review findings to existing task",
			Path:         "work/tasks/APP-T-0004.md",
			RelatedTasks: []string{"APP-T-0100"},
		},
	}

	tests := []struct {
		name  string
		src   feedbackPromoteSource
		field string
		id    string
	}{
		{
			name:  "dedupe key",
			src:   feedbackPromoteSource{Title: "New wording", DedupeKey: "Validation-Owned-Scope", Severity: "P1"},
			field: "dedupe_key",
			id:    "APP-T-0001",
		},
		{
			name:  "source reference",
			src:   feedbackPromoteSource{Title: "Source repeat", Path: "feedback/agents/2026-05-30-codex-source.md", Severity: "P1"},
			field: "source",
			id:    "APP-T-0002",
		},
		{
			name:  "title",
			src:   feedbackPromoteSource{Title: "Create daily review packet", Severity: "P1"},
			field: "title",
			id:    "APP-T-0003",
		},
		{
			name:  "related task",
			src:   feedbackPromoteSource{Title: "APP-T-0100 needs another linked note", RelatedTask: "APP-T-0100", Severity: "P1"},
			field: "task",
			id:    "APP-T-0004",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := planFeedbackPromotion(tt.src, feedbackPromoteOptions{ExistingWork: existing})
			if err != nil {
				t.Fatal(err)
			}
			outcome := plan.Outcomes[0]
			assertEqual(t, "update", outcome.Operation, "operation")
			assertEqual(t, "task", outcome.Kind, "kind")
			assertEqual(t, tt.id, outcome.TargetID, "target")
			if outcome.Duplicate == nil {
				t.Fatal("expected duplicate match")
			}
			assertEqual(t, tt.field, outcome.Duplicate.Field, "duplicate field")
		})
	}
}

func TestFeedbackPromoteCollectsExistingWorkFromVault(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := writeText(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"), strings.Join([]string{
		"---",
		`schema: "tusker.task/v7"`,
		`kind: "task"`,
		`id: "APP-T-0001"`,
		`title: "Scope validation output to owned changes by default"`,
		`dedupe_key: "validation-owned-scope"`,
		`source_refs: ["feedback/agents/2026-05-31-codex-proof-noise.md"]`,
		"---",
		"",
		"# Scope validation output",
		"",
		"- related-task: APP-T-0099",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}

	plan, err := planFeedbackPromotion(feedbackPromoteSource{
		Title:     "Different title",
		DedupeKey: "validation-owned-scope",
		Severity:  "P1",
	}, feedbackPromoteOptions{VaultPath: vault})
	if err != nil {
		t.Fatal(err)
	}
	outcome := plan.Outcomes[0]
	assertEqual(t, "update", outcome.Operation, "operation")
	assertEqual(t, "APP-T-0001", outcome.TargetID, "target")
	if outcome.Duplicate == nil || outcome.Duplicate.Field != "dedupe_key" {
		t.Fatalf("expected dedupe duplicate from vault, got %#v", outcome.Duplicate)
	}
}

func TestFeedbackPromoteRenderSummaryIsBounded(t *testing.T) {
	plan := feedbackPromotePlan{
		Mode: feedbackPromoteModeDryRun,
		Outcomes: []feedbackPromoteOutcome{
			{Operation: "create", Kind: "task", Title: "One", Prevention: "Prevent one.", Reasons: []string{"first"}},
			{Operation: "skip", Kind: "skip", Title: "Two", Prevention: "Prevent two.", Reasons: []string{"second"}},
		},
	}
	output := renderFeedbackPromotePlanMarkdown(plan, 1)
	if !strings.Contains(output, "created=1 updated=0 linked=0 skipped=1 needs-human-decision=0") {
		t.Fatalf("summary counts missing:\n%s", output)
	}
	if !strings.Contains(output, "1 more outcomes omitted") || strings.Contains(output, "Two") {
		t.Fatalf("bounded summary not enforced:\n%s", output)
	}
}
