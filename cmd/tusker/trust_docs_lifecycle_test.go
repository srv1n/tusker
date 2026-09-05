package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

// TestTrustDocsLifecycle is the focused contract check named by the trust-5
// delivery plan. It keeps the executable check at the shared docgraph seam;
// installed skill provenance remains owned by the contract/install worker.
func TestTrustDocsLifecycle(t *testing.T) {
	root := t.TempDir()
	writeTrustDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeTrustDoc(t, root, "docs/system/guide.md", "---\nsubject: guide\npart_of: overview\nstatus: canonical\nread_when: choosing the current guide\nskip_when: checking a decision only\n---\n# Guide\nRead [[lifecycle-spec]] before changing this route.\n")
	writeTrustDoc(t, root, "docs/system/old-guide.md", "---\nsubject: old-guide\npart_of: overview\nstatus: superseded\nsuperseded_by: guide\n---\n# Old guide\n")
	writeTrustDoc(t, root, ".tusker/specs/lifecycle.md", "---\nsubject: lifecycle-spec\npart_of: overview\nstatus: canonical\nsources: [docs/system/guide.md]\n---\n# Lifecycle contract\n")
	writeTrustDoc(t, root, ".tusker/specs/decisions/lifecycle.md", "---\nsubject: lifecycle-decision\npart_of: lifecycle-spec\ndecides_for: lifecycle-spec\nstatus: canonical\n---\n# Decision\n")
	customPath := filepath.Join(root, "docs", "custom.md")
	customBytes := []byte("# User-owned notes\nKeep this exact.\n")
	if err := os.MkdirAll(filepath.Dir(customPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(customPath, customBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	corpus, _, err := docgraph.LoadRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	links, _ := docgraph.SemanticLinks(corpus)
	if !lifecycleLink(links, "guide", "lifecycle-spec", "link") {
		t.Fatalf("body link missing from semantic graph: %#v", links)
	}
	backlinks := docgraph.Backlinks(corpus, "guide")
	if !lifecycleLink(backlinks, "old-guide", "guide", "superseded_by") {
		t.Fatalf("supersession backlink missing: %#v", backlinks)
	}

	if err := docgraph.WriteDocsMap(root); err != nil {
		t.Fatalf("WriteDocsMap() error = %v", err)
	}
	if issues, err := docgraph.CheckDocsMapFresh(root); err != nil || len(issues) != 0 {
		t.Fatalf("fresh generated map = issues %#v err %v", issues, err)
	}
	if got, err := os.ReadFile(customPath); err != nil || string(got) != string(customBytes) {
		t.Fatalf("map changed user-owned source: %q err=%v", got, err)
	}

	brokenGuide := "---\nsubject: guide\npart_of: overview\nstatus: canonical\nread_when: choosing the current guide\nskip_when: checking a decision only\n---\n# Guide\nRead [[missing-route]].\n"
	writeTrustDoc(t, root, "docs/system/guide.md", brokenGuide)
	issues, err := docgraph.ValidateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if !trustIssueCode(issues, "DOC_LINK_DANGLING") {
		t.Fatalf("broken mandatory route was not rejected: %#v", issues)
	}
	if issues, err := docgraph.CheckDocsMapFresh(root); err != nil || !trustIssueCode(issues, "DOCS_MAP_DANGLING_LINK") {
		t.Fatalf("broken map route = issues %#v err %v", issues, err)
	}

	if !strings.Contains(docsScaffold("new-spec", "spec"), "decisions_locked: false") {
		t.Fatal("spec scaffold omitted decisions_locked lifecycle field")
	}
}

func lifecycleLink(links []docgraph.DocumentLink, from, to, kind string) bool {
	for _, link := range links {
		if link.From == from && link.To == to && link.Kind == kind {
			return true
		}
	}
	return false
}
