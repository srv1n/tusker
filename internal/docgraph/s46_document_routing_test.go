package docgraph

import (
	"testing"
)

// TestS46DocumentRouting covers TSK-T-0055 A2: portable roots, authored
// indexes, aliases and generated output locations resolve consistently with
// escape and duplicate rejection.
func TestS46DocumentRouting(t *testing.T) {
	t.Run("portable roots share one corpus", func(t *testing.T) {
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
		writeDoc(t, root, "docs/system/domains/billing/00-index.md", "---\nkind: doc\nsubject: billing-index\npart_of: overview\nstatus: current\n---\n# Billing\n")
		writeDoc(t, root, ".tusker/specs/change.md", "---\nkind: proposal\nsubject: change\npart_of: overview\nstatus: proposed\n---\n# Change\n")
		corpus, issues, err := LoadRepository(root)
		if err != nil {
			t.Fatalf("LoadRepository() error = %v", err)
		}
		if len(issues) != 0 {
			t.Fatalf("unexpected issues: %#v", issues)
		}
		if len(corpus.Documents) != 3 {
			t.Fatalf("expected one corpus with 3 documents, got %#v", corpus.Documents)
		}
	})

	t.Run("authored indexes stay visible while generated output is excluded", func(t *testing.T) {
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
		writeDoc(t, root, "docs/system/domains/billing/00-index.md", "---\nkind: doc\nsubject: billing-index\npart_of: overview\nstatus: current\n---\n# Billing\n")
		writeDoc(t, root, "docs/system/domains/billing/INDEX.md", "---\nkind: doc\nsubject: billing-nested-index\npart_of: billing-index\nstatus: current\n---\n# Nested\n")
		writeDoc(t, root, "docs/system/INDEX.md", "---\nkind: doc\nsubject: legacy-generated\npart_of: overview\nstatus: current\n---\n# Generated\n")
		corpus, _, err := LoadRepository(root)
		if err != nil {
			t.Fatalf("LoadRepository() error = %v", err)
		}
		paths := map[string]bool{}
		for _, doc := range corpus.Documents {
			paths[doc.Path] = true
		}
		for _, want := range []string{
			"docs/system/domains/billing/00-index.md",
			"docs/system/domains/billing/INDEX.md",
		} {
			if !paths[want] {
				t.Fatalf("authored index %q must remain visible; corpus=%v", want, paths)
			}
		}
		if paths["docs/system/INDEX.md"] {
			t.Fatalf("known generated index must stay excluded; corpus=%v", paths)
		}
	})

	t.Run("generated outputs live under tusker generated docs", func(t *testing.T) {
		if GeneratedDocsRoot != ".tusker/_generated/docs" {
			t.Fatalf("GeneratedDocsRoot = %q, want .tusker/_generated/docs", GeneratedDocsRoot)
		}
		if GeneratedIndexRelPath != ".tusker/_generated/docs/INDEX.md" || GeneratedGraphRelPath != ".tusker/_generated/docs/graph.json" {
			t.Fatalf("generated index/graph paths point outside .tusker/_generated/docs: %q %q", GeneratedIndexRelPath, GeneratedGraphRelPath)
		}
	})

	t.Run("aliases resolve without duplicate ownership", func(t *testing.T) {
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
		writeDoc(t, root, "docs/system/guide.md", "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: current\naliases: [how-to]\n---\nSee [the change](change) and [[how-to]].\n")
		writeDoc(t, root, ".tusker/specs/change.md", "---\nkind: proposal\nsubject: change\npart_of: overview\nstatus: proposed\n---\n# Change\n")
		corpus, _, err := LoadRepository(root)
		if err != nil {
			t.Fatalf("LoadRepository() error = %v", err)
		}
		if _, ok := ResolveReference(corpus, "how-to"); !ok {
			t.Fatal("alias how-to must resolve to the guide document")
		}
		if _, ok := ResolveReference(corpus, ".tusker/specs/change.md"); !ok {
			t.Fatal("explicit portable path must resolve")
		}
	})

	t.Run("escapes and duplicates are rejected", func(t *testing.T) {
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
		writeDoc(t, root, "docs/system/guide.md", "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: current\n---\n# Guide\n")
		escapeCorpus, _, err := LoadRepository(root)
		if err != nil {
			t.Fatalf("LoadRepository() error = %v", err)
		}
		for _, escaped := range []string{"../escape.md", "/abs/path.md", "https://example.com/guide.md"} {
			if _, ok := ResolveStrictReference(escapeCorpus, escaped); ok {
				t.Fatalf("strict resolution must refuse escape/external reference %q", escaped)
			}
			if _, ok := ResolveReference(escapeCorpus, escaped); ok {
				t.Fatalf("lenient resolution must refuse escape/external reference %q", escaped)
			}
		}
		writeDoc(t, root, "docs/system/a.md", "---\nkind: doc\nsubject: twin\npart_of: overview\nstatus: current\n---\n# A\n")
		writeDoc(t, root, "docs/system/b.md", "---\nkind: doc\nsubject: twin\npart_of: overview\nstatus: current\n---\n# B\n")
		corpus, _, err := LoadRepository(root)
		if err != nil {
			t.Fatalf("LoadRepository() error = %v", err)
		}
		if _, ok := ResolveStrictReference(corpus, "twin"); ok {
			t.Fatal("strict resolution must refuse ambiguous duplicate subjects")
		}
		issues, err := ValidateRepository(root)
		if err != nil {
			t.Fatalf("ValidateRepository() error = %v", err)
		}
		assertIssue(t, issues, "DOC_DUPLICATE_SUBJECT", "docs/system/a.md", "duplicate subject")
		assertIssue(t, issues, "DOC_DUPLICATE_SUBJECT", "docs/system/b.md", "duplicate subject")
	})
}
