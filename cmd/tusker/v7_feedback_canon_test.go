package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedbackCandidatesSurfaceAtThresholdAndHideBelow(t *testing.T) {
	root := t.TempDir()
	repos := []string{
		filepath.Join(root, "alpha"),
		filepath.Join(root, "beta"),
		filepath.Join(root, "gamma"),
	}
	for i, repo := range repos {
		vault := filepath.Join(repo, ".tusker")
		writeFeedbackCanonFixtureNote(t, vault, fmt.Sprintf("2026-05-0%d", i+1), "codex", fmt.Sprintf("raw-output-%d", i+1), feedbackCanonFixtureFriction(), "Keep task notes to command summaries and proof rows.")
	}

	output := captureStdout(t, func() {
		if err := feedbackCandidatesCmd(Args{"repo": strings.Join(repos, "\n"), "threshold": "3"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		"Threshold: 3 distinct notes",
		"Candidates: 1",
		"alpha:feedback/agents/2026-05-01-codex-raw-output-1.md",
		"beta:feedback/agents/2026-05-02-codex-raw-output-2.md",
		"gamma:feedback/agents/2026-05-03-codex-raw-output-3.md",
	} {
		assertContainsIndexTest(t, output, expected)
	}

	below := captureStdout(t, func() {
		if err := feedbackCandidatesCmd(Args{"repo": strings.Join(repos, "\n"), "threshold": "4"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, below, "Candidates: 0")
	assertNotContainsIndexTest(t, below, "alpha:feedback/agents/2026-05-01-codex-raw-output-1.md")
}

func writeFeedbackCanonFixtureNote(t *testing.T, vault, date, actor, slug, friction, productIdea string) {
	t.Helper()
	content := strings.Join([]string{
		"# Agent Feedback",
		"",
		"- context: A reviewer had to inspect a noisy task handoff.",
		"- friction: " + friction,
		"- product-idea: " + productIdea,
		"- impact: Reviewers see concise evidence instead of noisy transcripts.",
		"- related: tusker edit",
		"- theme: task markdown logging",
		"",
	}, "\n")
	if err := writeText(filepath.Join(vault, "feedback", "agents", date+"-"+actor+"-"+slug+".md"), content); err != nil {
		t.Fatal(err)
	}
}

func feedbackCanonFixtureFriction() string {
	return "Agents paste full terminal output into task notes."
}
