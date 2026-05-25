package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedbackAddWritesStructuredNote(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "alpha")

	if err := feedbackAddCmd(Args{
		"repo":             repo,
		"date":             "2026-05-21",
		"actor":            "codex",
		"slug":             "feedback-loop",
		"context":          "Finished a meaningful Tusker turn.",
		"friction":         "Agent feedback had no first-class capture path.",
		"product-idea":     "Add a concise feedback command.",
		"impact":           "Product signals stay reviewable without status-report sprawl.",
		"related":          "tusker feedback add, AFS-004",
		"affected-command": "tusker feedback add",
		"quiet":            "true",
	}); err != nil {
		t.Fatal(err)
	}

	notePath := filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-21-codex-feedback-loop.md")
	text, err := readText(notePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"- context: Finished a meaningful Tusker turn.",
		"- friction: Agent feedback had no first-class capture path.",
		"- product-idea: Add a concise feedback command.",
		"- impact: Product signals stay reviewable without status-report sprawl.",
		"- related: tusker feedback add, AFS-004",
		"- affected-command: tusker feedback add",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("feedback note missing %q:\n%s", expected, text)
		}
	}
	if warnings := validateFeedbackNotes(filepath.Join(repo, "tusker")); len(warnings) != 0 {
		t.Fatalf("expected generated note to validate cleanly, got %#v", warnings)
	}
}

func TestFeedbackAddRejectsLongNoteUnlessAllowed(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "budgeted")
	if err := writeText(filepath.Join(repo, "tusker.yaml"), "feedback:\n  note_max_chars: 40\n"); err != nil {
		t.Fatal(err)
	}
	args := Args{
		"repo":         repo,
		"date":         "2026-05-21",
		"actor":        "codex",
		"slug":         "long-note",
		"context":      "A shared workspace turn hit a repeated product problem.",
		"friction":     "The note is intentionally too long for the configured character budget.",
		"product-idea": "Reject long feedback notes unless the agent explicitly opts in.",
		"impact":       "Prevents progress reports from becoming product feedback.",
		"related":      "tusker feedback add",
		"quiet":        "true",
	}
	if err := feedbackAddCmd(args); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("expected long-note rejection, got %v", err)
	}
	args["allow-long"] = "true"
	if err := feedbackAddCmd(args); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-21-codex-long-note.md"))
}

func TestFeedbackAddRejectsProgressReportUnlessAllowed(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "progress-report")
	args := Args{
		"repo":         repo,
		"date":         "2026-05-21",
		"actor":        "codex",
		"slug":         "status-looking",
		"context":      "Changed files in cmd/tusker during a focused implementation turn.",
		"friction":     "Tests run details are easy to paste into feedback by habit.",
		"product-idea": "Reject progress-shaped notes unless explicitly allowed.",
		"impact":       "Keeps product feedback distinct from proof and handoff status.",
		"related":      "tusker feedback add",
		"quiet":        "true",
	}
	if err := feedbackAddCmd(args); err == nil || !strings.Contains(err.Error(), "progress report") {
		t.Fatalf("expected progress-report rejection, got %v", err)
	}
	if fileExists(filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-21-codex-status-looking.md")) {
		t.Fatal("progress-report-looking note was written without explicit allowance")
	}

	args["allow-progress-report"] = "true"
	if err := feedbackAddCmd(args); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-21-codex-status-looking.md"))
}

func TestFeedbackAddRejectsRecentDuplicateDedupeKeyUnlessAllowed(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "dedupe")
	existingPath := filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-10-codex-validation-noise.md")
	if err := writeText(existingPath, strings.Join([]string{
		"# Agent Feedback",
		"",
		"- context: Validation produced repeated unrelated shared workspace noise.",
		"- friction: Agents spend time sorting output they cannot act on.",
		"- product-idea: Scope validation feedback to owned changes.",
		"- impact: Reduces wasted handoff time.",
		"- related: tusker validate",
		"- dedupe-key: validation-owned-scope",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	args := Args{
		"repo":          repo,
		"date":          "2026-05-21",
		"actor":         "codex",
		"slug":          "validation-repeat",
		"context":       "A new validation turn hit the same owned-scope issue.",
		"friction":      "The CLI still surfaces unrelated workspace churn as actionable.",
		"product-idea":  "Keep validation feedback grouped by stable dedupe key.",
		"impact":        "Repeated reports collapse into one useful product signal.",
		"related":       "tusker validate",
		"dedupe-key":    "Validation-Owned-Scope",
		"priority-hint": "P2",
		"quiet":         "true",
	}
	err := feedbackAddCmd(args)
	if err == nil {
		t.Fatal("expected duplicate dedupe-key rejection")
	}
	if !strings.Contains(err.Error(), "Validation-Owned-Scope") || !strings.Contains(err.Error(), "2026-05-10-codex-validation-noise.md") {
		t.Fatalf("duplicate error should name key and existing path, got %v", err)
	}

	args["allow-duplicate"] = "true"
	if err := feedbackAddCmd(args); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-21-codex-validation-repeat.md")
	text, err := readText(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "- dedupe-key: Validation-Owned-Scope") {
		t.Fatalf("feedback note did not render dedupe-key:\n%s", text)
	}
}

func TestFeedbackDigestGroupsAndFlagsNotes(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	if err := writeText(filepath.Join(alpha, "tusker", "feedback", "agents", "README.md"), "template\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(alpha, "tusker", "feedback", "agents", "examples", "2026-05-21-codex-example.md"), "- context: example\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(alpha, "tusker", "feedback", "agents", "2026-05-21-codex-validation-noise.md"), strings.Join([]string{
		"# Validation Feedback",
		"",
		"- Context: `tusker validate` was run after a focused change.",
		"- Friction: unrelated shared-workspace edits made validation noisy and blocked handoff.",
		"- Product idea: digest scoped feedback by command so repeated validation pain is visible.",
		"- Impact: blocked agents waste turns debugging work they do not own.",
		"- Related task or command: tusker validate",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(beta, "tusker", "feedback", "agents", "2026-05-21-codex-status-report.md"), strings.Join([]string{
		"# Status Report",
		"",
		"- context: changed files and completed implementation.",
		"- friction: validation and tests run details were copied into feedback.",
		"- impact: routine progress obscures product signal.",
		"- related: APP-T-0001",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := feedbackDigestCmd(Args{
			"repo":  alpha + "\n" + beta,
			"since": "2026-05-20",
			"date":  "2026-05-21",
			"write": "true",
		}); err != nil {
			t.Fatal(err)
		}
	})
	digestPath := filepath.Join(alpha, "tusker", "feedback", "digests", "2026-05-21.md")
	assertExists(t, digestPath)
	digestText, err := readText(digestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, haystack := range []string{output, digestText} {
		for _, expected := range []string{
			"## By Theme",
			"validation and proof",
			"## By Source Repo",
			"alpha",
			"## By Priority Hint",
			"P1",
			"## By Affected Command",
			"`tusker validate`",
			"## Malformed Or Over Budget",
			"FEEDBACK_MISSING_FIELD",
			"FEEDBACK_PROGRESS_REPORT",
		} {
			if !strings.Contains(haystack, expected) {
				t.Fatalf("digest missing %q:\n%s", expected, haystack)
			}
		}
		if strings.Contains(haystack, "codex-example") || strings.Contains(haystack, "README.md") {
			t.Fatalf("digest included template/example note:\n%s", haystack)
		}
	}
}

func TestFeedbackValidationHelperWarnsOnMalformedProgressReports(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := writeText(filepath.Join(vault, "feedback", "agents", "2026-05-21-codex-progress.md"), strings.Join([]string{
		"# Progress",
		"",
		"- context: changed files in cmd/tusker and completed the implementation.",
		"- friction: validation output and tests run are being copied here.",
		"- impact: routine progress reports hide useful product feedback.",
		"- related: APP-T-0001",
		"",
	}, "\n")); err != nil {
		t.Fatal(err)
	}
	warnings := validateFeedbackNotes(vault)
	if !feedbackIssuesContainCode(warnings, "FEEDBACK_MISSING_FIELD") {
		t.Fatalf("expected missing field warning, got %#v", warnings)
	}
	if !feedbackIssuesContainCode(warnings, "FEEDBACK_PROGRESS_REPORT") {
		t.Fatalf("expected progress-report warning, got %#v", warnings)
	}
}

func TestFeedbackValidationIgnoresAgentGuidanceMigrationDrafts(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	if err := writeText(filepath.Join(vault, "feedback", "agents", "2026-05-21-agent-guidance-migration-draft.md"), "# Agent Guidance Migration Draft\n\nNot structured feedback.\n"); err != nil {
		t.Fatal(err)
	}
	if warnings := validateFeedbackNotes(vault); len(warnings) != 0 {
		t.Fatalf("migration drafts should not be validated as feedback notes, got %#v", warnings)
	}
}

func TestSyncRepoContractCreatesFeedbackReadmeAtExplicitVault(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	customVault := filepath.Join(t.TempDir(), "custom-vault")
	if err := syncRepoContract(Args{"repo": repo, "vault": customVault, "force": "true"}); err != nil {
		t.Fatal(err)
	}
	assertExists(t, filepath.Join(customVault, "feedback", "agents", "README.md"))
	if fileExists(filepath.Join(repo, "tusker", "feedback", "agents", "README.md")) {
		t.Fatal("sync-repo-contract ignored --vault and created feedback README in repo default vault")
	}
}

func TestEnsureFeedbackReadmeForRepoIsIdempotent(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name     string
		hasSkill bool
	}{
		{name: "without-skill"},
		{name: "with-skill", hasSkill: true},
	} {
		repo := filepath.Join(root, tc.name)
		if tc.hasSkill {
			if err := writeText(filepath.Join(repo, "tusker", "SKILL.md"), "# Project Skill\n"); err != nil {
				t.Fatal(err)
			}
		}
		notePath := filepath.Join(repo, "tusker", "feedback", "agents", "2026-05-21-codex-existing.md")
		if err := writeText(notePath, "- context: existing\n"); err != nil {
			t.Fatal(err)
		}
		readmePath, created, err := ensureFeedbackReadmeForRepo(repo)
		if err != nil {
			t.Fatal(err)
		}
		if !created {
			t.Fatalf("expected README to be created for %s", tc.name)
		}
		readme, err := readText(readmePath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(readme, "- product-idea:") {
			t.Fatalf("README missing feedback template:\n%s", readme)
		}
		note, err := readText(notePath)
		if err != nil {
			t.Fatal(err)
		}
		if note != "- context: existing\n" {
			t.Fatalf("existing feedback note was overwritten:\n%s", note)
		}
		if err := writeText(readmePath, "custom feedback readme\n"); err != nil {
			t.Fatal(err)
		}
		_, created, err = ensureFeedbackReadmeForRepo(repo)
		if err != nil {
			t.Fatal(err)
		}
		if created {
			t.Fatalf("expected existing README to be preserved for %s", tc.name)
		}
		readme, err = readText(readmePath)
		if err != nil {
			t.Fatal(err)
		}
		if readme != "custom feedback readme\n" {
			t.Fatalf("existing README was overwritten:\n%s", readme)
		}
	}
}

func feedbackIssuesContainCode(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
