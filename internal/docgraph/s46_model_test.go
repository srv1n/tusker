package docgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The canonical A1/A2 S46 coverage lives in s46_document_contract_test.go
// and s46_document_routing_test.go (TestS46DocumentContract and
// TestS46DocumentRouting). The tests below cover the remaining model-story
// behaviors from the same task packet without duplicating those cases:
// legacy forwarding stubs, authored-index visibility in scan and browse,
// and generated-output freshness with the legacy-location diagnostic.

// TestS46ForwardingStubResolution covers the legacy-forwarding side of the
// model contract: a subject-less superseded placeholder carries only its
// successor link, owns no subject, and resolves to the current document.
func TestS46ForwardingStubResolution(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/proposals/new.md", "---\nsubject: new-home\nkind: proposal\nstatus: accepted\npart_of: overview\n---\n# New\n")
	writeDoc(t, root, ".tusker/specs/legacy.md", "---\nstatus: superseded\nsuperseded_by: new-home\n---\nSee [the new location](../../docs/system/proposals/new-home.md).\n")

	issues, err := ValidateRepository(root)
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v", err)
	}
	for _, issue := range issues {
		if issue.Code == "DOC_REQUIRED_FIELD_MISSING" || issue.Code == "DOC_DUPLICATE_SUBJECT" {
			t.Fatalf("forwarding stub must not own identity fields: %#v", issue)
		}
	}
	corpus, _, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository() error = %v", err)
	}
	if got := DuplicateSubjects(corpus); len(got) != 0 {
		t.Fatalf("stub created duplicate ownership: %#v", got)
	}
	var stubFound bool
	for _, doc := range corpus.Documents {
		if doc.Path == ".tusker/specs/legacy.md" {
			stubFound = true
			if !IsForwardingStub(doc) {
				t.Fatalf("legacy placeholder not classified as a stub: %#v", doc)
			}
		}
	}
	if !stubFound {
		t.Fatal("legacy stub missing from corpus")
	}
	current, ok := NewResolver(corpus).ResolveCurrentFrom("", ".tusker/specs/legacy.md")
	if !ok || current.Document.Subject != "new-home" {
		t.Fatalf("legacy path did not forward to the current document: %#v ok=%v", current, ok)
	}
	strict, ok := NewResolver(corpus).ResolveStrict("new-home")
	if !ok || strict.CanonicalRef != "docs/system/proposals/new.md" {
		t.Fatalf("successor subject did not resolve strictly: %#v ok=%v", strict, ok)
	}
}

// TestS46AuthoredIndexVisibility covers the narrowed INDEX.md exclusion:
// authored nested indexes stay visible in both the scan and browse while the
// known generated corpus index and symlinked entries stay out.
func TestS46AuthoredIndexVisibility(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/domains/waves/INDEX.md", "---\nsubject: waves-index\npart_of: overview\n---\n# Waves index\n")
	writeDoc(t, root, "docs/system/INDEX.md", "<!-- tusker:docs-map:begin -->\n# Documentation index\n")
	target := filepath.Join(root, filepath.FromSlash("docs/system/real.md"))
	if err := os.Symlink(filepath.Join(root, "outside.md"), target); err != nil {
		t.Fatal(err)
	}

	corpus, _, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository() error = %v", err)
	}
	var sawAuthored, sawGenerated, sawSymlink bool
	for _, doc := range corpus.Documents {
		switch doc.Path {
		case "docs/system/domains/waves/INDEX.md":
			sawAuthored = true
		case "docs/system/INDEX.md":
			sawGenerated = true
		case "docs/system/real.md":
			sawSymlink = true
		}
	}
	if !sawAuthored {
		t.Fatalf("authored nested INDEX.md hidden from corpus: %#v", corpus.Documents)
	}
	if sawGenerated {
		t.Fatal("known generated INDEX.md entered the corpus")
	}
	if sawSymlink {
		t.Fatal("symlinked document entered the corpus")
	}

	browsed, err := Browse(root, "docs/system/domains/waves", DefaultBrowseLimit)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	var sawFile bool
	for _, entry := range browsed.Entries {
		if entry.Path == "docs/system/domains/waves/INDEX.md" {
			sawFile = true
		}
	}
	if !sawFile {
		t.Fatalf("authored INDEX.md hidden from browse: %#v", browsed.Entries)
	}
}

// TestS46GeneratedFreshness covers the portable storage half of the routing
// contract: generated outputs live under .tusker/_generated/docs, a plain
// overview stays useful with them absent, and a legacy-only tree reports a
// migration diagnostic instead of a stale-map failure.
func TestS46GeneratedFreshness(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\ntitle: \"Overview\"\nsubject: overview\n---\n\n# Overview\n")
	writeDoc(t, root, "docs/system/cli.md", "---\ntitle: \"CLI\"\nsubject: cli\npart_of: overview\n---\n\n# CLI\n")
	if _, _, err := LoadRepository(root); err != nil {
		t.Fatalf("plain overview must load with generated outputs absent: %v", err)
	}
	if err := WriteDocsMap(root); err != nil {
		t.Fatalf("WriteDocsMap() error = %v", err)
	}
	for _, rel := range []string{GeneratedIndexRelPath, GeneratedGraphRelPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("generated output missing at %s: %v", rel, err)
		}
	}
	issues, err := CheckDocsMapFresh(root)
	if err != nil {
		t.Fatalf("CheckDocsMapFresh() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("fresh map should validate clean, got %#v", issues)
	}

	for _, rel := range []string{GeneratedIndexRelPath, GeneratedGraphRelPath} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	issues, err = CheckDocsMapFresh(root)
	if err != nil {
		t.Fatalf("CheckDocsMapFresh() error = %v", err)
	}
	var legacy int
	for _, issue := range issues {
		if issue.Code == "DOCS_MAP_LEGACY_LOCATION" && strings.Contains(issue.Message, "tusker docs map") {
			legacy++
		}
		if issue.Code == "DOCS_MAP_STALE" {
			t.Fatalf("legacy-only tree misreported as stale: %#v", issue)
		}
	}
	if legacy != 2 {
		t.Fatalf("expected two legacy-location diagnostics, got %#v", issues)
	}
}
