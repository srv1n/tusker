package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"tusker/internal/docgraph"
)

// TestTrustDocumentRouting is the focused contract check named by the
// trust-4 delivery plan. It exercises the current corpus route, supersession,
// broken-link reporting, and the bounded discovery journey at both fixture
// sizes without reading the full document bodies.
func TestTrustDocumentRouting(t *testing.T) {
	t.Run("current roots and supersession", func(t *testing.T) {
		root := t.TempDir()
		writeTrustDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
		writeTrustDoc(t, root, "docs/system/old-guide.md", "---\nsubject: old-guide\npart_of: overview\nstatus: superseded\nsuperseded_by: current-spec\n---\n# Old guide\n")
		writeTrustDoc(t, root, ".tusker/specs/current.md", "---\nsubject: current-spec\nkeywords: [routing]\npart_of: overview\n---\n# Current spec\n")
		// A legacy intake file is preserved as source material, but it is not a
		// governing reference in the current-only resolver.
		writeTrustDoc(t, root, "docs/specs/legacy.md", "# Legacy intake\n")

		corpus, _, err := docgraph.LoadRepository(root)
		if err != nil {
			t.Fatal(err)
		}
		current, ok := docgraph.ResolveCurrentReference(corpus, "old-guide")
		if !ok || current.Document.Subject != "current-spec" || current.ResolvedFrom != "old-guide" {
			t.Fatalf("superseded route = %#v, ok=%v", current, ok)
		}
		if _, ok := docgraph.ResolveReference(corpus, "docs/specs/legacy.md"); ok {
			t.Fatal("obsolete docs/specs route became a governing document")
		}
		if !v7SpecRefExists(filepath.Join(root, ".tusker"), ".tusker/specs/current.md", nil) {
			t.Fatal("current .tusker/specs reference did not resolve")
		}
		if v7SpecRefExists(filepath.Join(root, ".tusker"), "docs/specs/legacy.md", nil) {
			t.Fatal("obsolete docs/specs reference resolved as governing spec")
		}
	})

	t.Run("broken routes fail validation", func(t *testing.T) {
		root := t.TempDir()
		writeTrustDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
		writeTrustDoc(t, root, "docs/system/broken.md", "---\nsubject: broken\npart_of: overview\n---\n# Broken\nSee [[missing-route]].\n")
		issues, err := docgraph.ValidateRepository(root)
		if err != nil {
			t.Fatal(err)
		}
		if !trustIssueCode(issues, "DOC_LINK_DANGLING") {
			t.Fatalf("missing broken-link defect: %#v", issues)
		}
	})

	for _, size := range []int{100, 1000} {
		t.Run("bounded discovery "+strconv.Itoa(size), func(t *testing.T) {
			root := t.TempDir()
			writeTrustDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
			for i := 0; i < size; i++ {
				writeTrustDoc(t, root, filepath.Join(".tusker/specs", "routing-"+strconv.Itoa(i)+".md"),
					"---\nsubject: routing-"+strconv.Itoa(i)+"\nkeywords: [routing]\npart_of: overview\nread_when: choosing a governing route\nskip_when: reading the full contract\n---\n# Routing\n")
			}
			corpus, _, err := docgraph.LoadRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			result := docgraph.Find(corpus, "routing")
			elapsed := time.Since(started)
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Matches) != docgraph.DefaultFindLimit || result.TotalMatches != size || !result.Truncated {
				t.Fatalf("size %d result = matches %d total %d truncated %v", size, len(result.Matches), result.TotalMatches, result.Truncated)
			}
			if len(encoded) > 3200 {
				t.Fatalf("bounded shortlist JSON is %d bytes, expected <= 3200", len(encoded))
			}
			t.Logf("documents=%d matches=%d bytes=%d elapsed=%s", size, len(result.Matches), len(encoded), elapsed)
		})
	}
}

func writeTrustDoc(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func trustIssueCode(issues []docgraph.Issue, code string) bool {
	for _, current := range issues {
		if current.Code == code {
			return true
		}
	}
	return false
}
