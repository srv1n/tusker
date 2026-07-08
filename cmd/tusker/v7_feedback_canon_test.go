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

func TestFeedbackPromoteWritesCanonEntryWithClassDomainAndProvenance(t *testing.T) {
	vault := feedbackCanonTestVault(t)
	candidateID := seedFeedbackCanonCandidate(t, vault)

	if err := feedbackPromoteCmd(Args{"vault": vault, "candidate": candidateID, "class": "prohibition"}); err == nil || !strings.Contains(err.Error(), "--domain") {
		t.Fatalf("expected missing domain rejection, got %v", err)
	}
	if err := feedbackPromoteCmd(Args{"vault": vault, "candidate": candidateID, "domain": "project"}); err == nil || !strings.Contains(err.Error(), "--class") {
		t.Fatalf("expected missing class rejection, got %v", err)
	}

	if err := feedbackPromoteCmd(Args{
		"vault":     vault,
		"candidate": candidateID,
		"domain":    "project",
		"class":     "prohibition",
		"topic":     "task markdown logging",
		"lesson":    "Do not paste raw terminal output into task markdown.",
		"date":      "2026-05-04",
		"quiet":     "true",
	}); err != nil {
		t.Fatal(err)
	}

	canon := mustReadIndexTest(t, filepath.Join(vault, "knowledge", "domains", "project", "CANON.md"))
	for _, expected := range []string{
		"## Canon Entries",
		"- class: prohibition",
		"- topic: task markdown logging",
		"- lesson: Do not paste raw terminal output into task markdown.",
		"- recurrence_count: 3",
		"- date_span: 2026-05-01..2026-05-03",
		"feedback/agents/2026-05-01-codex-raw-output-1.md",
		"- source_repos: repo",
	} {
		assertContainsIndexTest(t, canon, expected)
	}
}

func TestPacketProhibitionCanonClassRendersInDomainContext(t *testing.T) {
	vault := feedbackCanonTestVault(t)
	candidateID := seedFeedbackCanonCandidate(t, vault)
	lesson := "Do not paste raw terminal output into task markdown."
	if err := feedbackPromoteCmd(Args{
		"vault":     vault,
		"candidate": candidateID,
		"domain":    "project",
		"class":     "prohibition",
		"topic":     "task markdown logging",
		"lesson":    lesson,
		"date":      "2026-05-04",
		"quiet":     "true",
	}); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Packet fixture.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Packet canon", "domains": "project", "v7": "true"}, newV7Task)

	packet := v7Packet(vault, mustV7Task(t, vault, "APP-T-0001"), mustIndex(t, vault), "agent")
	assertContainsIndexTest(t, packet, "Prohibitions:")
	assertContainsIndexTest(t, packet, lesson)
}

func TestCanonSupersedeRetiresExplicitEntryAndCanonPoolCoexists(t *testing.T) {
	vault := feedbackCanonTestVault(t)
	candidateID := seedFeedbackCanonCandidate(t, vault)
	args := Args{
		"vault":     vault,
		"candidate": candidateID,
		"domain":    "project",
		"class":     "pattern",
		"topic":     "task markdown logging",
		"lesson":    "Keep task markdown limited to concise proof summaries.",
		"date":      "2026-05-04",
		"quiet":     "true",
	}
	if err := feedbackPromoteCmd(args); err != nil {
		t.Fatal(err)
	}
	first := feedbackCanonEntriesForTest(t, vault)[0]

	args["lesson"] = "Keep source feedback paths attached to promoted lessons."
	if err := feedbackPromoteCmd(args); err != nil {
		t.Fatal(err)
	}
	entries := feedbackCanonEntriesForTest(t, vault)
	currentForTopic := 0
	for _, entry := range entries {
		if entry.Topic == "task markdown logging" && entry.Status == "current" {
			currentForTopic++
		}
	}
	if currentForTopic != 2 {
		t.Fatalf("expected two coexisting current entries for the same topic, got %#v", entries)
	}

	args["lesson"] = "Prefer reviewed proof summaries over raw transcript excerpts."
	args["supersedes"] = first.ID
	if err := feedbackPromoteCmd(args); err != nil {
		t.Fatal(err)
	}
	entries = feedbackCanonEntriesForTest(t, vault)
	var superseded, replacement feedbackCanonEntry
	for _, entry := range entries {
		if entry.ID == first.ID {
			superseded = entry
		}
		if containsString(entry.Supersedes, first.ID) {
			replacement = entry
		}
	}
	if superseded.Status != "superseded" || superseded.SupersededBy == "" {
		t.Fatalf("expected first entry to record supersession, got %#v", superseded)
	}
	if replacement.ID == "" || !containsString(replacement.Supersedes, first.ID) {
		t.Fatalf("expected replacement to record supersedes %s, got %#v", first.ID, entries)
	}
}

func feedbackCanonTestVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "repo", ".tusker")
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	if err := ensureV7Domain(vault, "project", "Project", "Project canon."); err != nil {
		t.Fatal(err)
	}
	return vault
}

func seedFeedbackCanonCandidate(t *testing.T, vault string) string {
	t.Helper()
	for i := 1; i <= 3; i++ {
		writeFeedbackCanonFixtureNote(t, vault, fmt.Sprintf("2026-05-0%d", i), "codex", fmt.Sprintf("raw-output-%d", i), feedbackCanonFixtureFriction(), "Keep task notes to command summaries and proof rows.")
	}
	candidates, err := buildFeedbackCandidates(Args{"vault": vault})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one fixture candidate, got %#v", candidates)
	}
	return candidates[0].ID
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
		"- related: tusker verify add",
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

func feedbackCanonEntriesForTest(t *testing.T, vault string) []feedbackCanonEntry {
	t.Helper()
	_, body, err := parseFrontmatterMustRead(filepath.Join(vault, "knowledge", "domains", "project", "CANON.md"))
	if err != nil {
		t.Fatal(err)
	}
	return feedbackCanonEntriesFromBody(body)
}
