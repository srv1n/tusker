package docgraph

import (
	"strings"
	"testing"
)

func loadCorpus(t *testing.T, root string) Corpus {
	t.Helper()
	corpus, _, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository() error = %v", err)
	}
	return corpus
}

func TestFindRanksCanonicalFirst(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/orchestration.md", "---\nsubject: orchestration\nkeywords: [worktree, daemon]\npart_of: overview\n---\n# Orchestration\nHow workers run.\n")
	writeDoc(t, root, ".tusker/specs/resume-plan.md", "---\nsubject: resume-plan\nkeywords: [worktree]\npart_of: overview\n---\n# Resume plan\n")
	writeDoc(t, root, ".tusker/specs/decisions/resume-choice.md", "---\nsubject: resume-choice\nkeywords: [worktree]\npart_of: resume-plan\ndecides_for: .tusker/specs/resume-plan.md\n---\n# Resume choice\n")

	result := Find(loadCorpus(t, root), "worktree")
	if len(result.Matches) == 0 {
		t.Fatalf("expected matches for worktree, got none; suggestions=%v", result.Suggestions)
	}
	kinds := []Kind{KindCanonical, KindSpec, KindDecision}
	for i, subject := range []string{"orchestration", "resume-plan", "resume-choice"} {
		if i >= len(result.Matches) {
			t.Fatalf("expected at least %d matches, got %d: %#v", i+1, len(result.Matches), result.Matches)
		}
		if result.Matches[i].Subject != subject {
			t.Fatalf("match %d = %q, want %q (kind %s); matches=%#v", i, result.Matches[i].Subject, subject, kinds[i], result.Matches)
		}
	}
	if result.Matches[0].Description == "" {
		t.Fatalf("first match has no description: %#v", result.Matches[0])
	}
}

func TestFindResolvesTombstoneToSuccessor(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/old-runner.md", "---\nsubject: old-runner\nkeywords: [runner, dispatch]\npart_of: overview\nstatus: superseded\nsuperseded_by: new-runner\n---\n# Old runner\n")
	writeDoc(t, root, "docs/system/new-runner.md", "---\nsubject: new-runner\nkeywords: [runner, dispatch]\npart_of: overview\n---\n# New runner\n")

	result := Find(loadCorpus(t, root), "old-runner")
	if len(result.Matches) == 0 {
		t.Fatalf("expected a match, got none; suggestions=%v", result.Suggestions)
	}
	first := result.Matches[0]
	if first.Subject != "new-runner" {
		t.Fatalf("superseded query did not resolve forward: got subject %q, want new-runner; matches=%#v", first.Subject, result.Matches)
	}
	if first.ResolvedFrom != "old-runner" {
		t.Fatalf("successor not marked as resolved from old-runner: %#v", first)
	}
}

func TestFindNoMatchSuggestsClosest(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/orchestration.md", "---\nsubject: orchestration\nkeywords: [worktree, daemon]\npart_of: overview\n---\n# Orchestration\n")
	writeDoc(t, root, ".tusker/specs/validation.md", "---\nsubject: validation\nkeywords: [lint, headers]\npart_of: overview\n---\n# Validation\n")

	result := Find(loadCorpus(t, root), "orchestraton")
	if len(result.Matches) != 0 {
		t.Fatalf("expected no matches for a misspelled query, got %#v", result.Matches)
	}
	if len(result.Suggestions) == 0 {
		t.Fatalf("no-match query returned no suggestions")
	}
	if result.Suggestions[0] != "orchestration" {
		t.Fatalf("closest suggestion = %q, want orchestration; suggestions=%v", result.Suggestions[0], result.Suggestions)
	}
	for _, suggestion := range result.Suggestions {
		if strings.TrimSpace(suggestion) == "" {
			t.Fatalf("empty suggestion in %v", result.Suggestions)
		}
	}
}
