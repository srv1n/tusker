package docgraph

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/v7schema"
)

func TestResolverUsesOneSubjectAndPathRoute(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/target.md", "---\nsubject: target-document\npart_of: overview\n---\n# Target\n")
	writeDoc(t, root, ".tusker/specs/current.md", "---\nsubject: current-spec\npart_of: overview\n---\n# Current\n")
	writeDoc(t, root, ".tusker/specs/target.md", "---\nsubject: target.md\npart_of: overview\n---\n# Other target\n")
	writeDoc(t, root, ".tusker/specs/decisions/current.md", "---\nsubject: current-decision\npart_of: current-spec\ndecides_for: current-spec\n---\n# Decision\n")
	writeDoc(t, root, "docs/system/old.md", "---\nsubject: old-guide\npart_of: overview\nstatus: superseded\nsuperseded_by: current-spec\n---\n# Old\n")

	resolver := NewResolver(loadCorpus(t, root))
	bySubject, ok := resolver.Resolve("current-spec")
	if !ok || bySubject.CanonicalRef != ".tusker/specs/current.md" {
		t.Fatalf("subject resolution = %#v, ok=%v", bySubject, ok)
	}
	byPath, ok := resolver.Resolve("./.tusker/specs/current.md#constraints")
	if !ok || byPath.Document.Subject != "current-spec" {
		t.Fatalf("path resolution = %#v, ok=%v", byPath, ok)
	}
	relative, ok := resolver.ResolveFrom("docs/system/guide.md", "./target.md")
	if !ok || relative.Document.Subject != "target-document" {
		t.Fatalf("relative path resolution = %#v, ok=%v", relative, ok)
	}
	bareRelative, ok := resolver.ResolveFrom("docs/system/guide.md", "target.md")
	if !ok || bareRelative.Document.Subject != "target-document" {
		t.Fatalf("same-directory Markdown path resolution = %#v", bareRelative)
	}
	if _, ok := resolver.Resolve("docs/specs/target.md"); ok {
		t.Fatal("qualified path resolved through an unrelated basename")
	}
	current, ok := resolver.ResolveCurrent("old-guide")
	if !ok || current.Document.Subject != "current-spec" || current.ResolvedFrom != "old-guide" {
		t.Fatalf("supersession resolution = %#v, ok=%v", current, ok)
	}
	if _, ok := resolver.Resolve("../outside.md"); ok {
		t.Fatal("resolver accepted a path that escapes the repository")
	}
}

func TestSemanticLinksBacklinksAndBrokenRoutesShareResolver(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/guide.md", "---\nsubject: guide\npart_of: overview\n---\n# Guide\nSee [[current-spec]] and [the overview](00-overview.md).\n")
	writeDoc(t, root, ".tusker/specs/current.md", "---\nsubject: current-spec\npart_of: overview\nsources: [docs/system/guide.md, external-record]\n---\n# Current\n")
	writeDoc(t, root, "docs/system/broken.md", "---\nsubject: broken\npart_of: overview\n---\n# Broken\nSee [[missing-route]].\n")

	corpus := loadCorpus(t, root)
	links, broken := SemanticLinks(corpus)
	var guideToSpec, guideToOverview, specToGuide bool
	for _, link := range links {
		if link.From == "guide" {
			if link.To == "current-spec" && link.Kind == "link" {
				guideToSpec = true
			}
			if link.To == "overview" && link.Kind == "link" {
				guideToOverview = true
			}
		}
		if link.From == "current-spec" && link.To == "guide" && link.Kind == "source" {
			specToGuide = true
		}
	}
	if !guideToSpec || !guideToOverview || !specToGuide {
		t.Fatalf("semantic links omitted body references: %#v", links)
	}
	var foundBroken bool
	for _, link := range broken {
		if link.Path == "docs/system/broken.md" && link.Ref == "missing-route" {
			foundBroken = true
		}
	}
	if !foundBroken {
		t.Fatalf("broken managed route was not reported: %#v", broken)
	}
	backlinks := Backlinks(corpus, "current-spec")
	if len(backlinks) != 1 || backlinks[0].From != "guide" || backlinks[0].Kind != "link" {
		t.Fatalf("backlinks = %#v", backlinks)
	}
}

func TestSemanticLinksKeepTrackerReferencesOutsideDocumentCorpus(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/guide.md", "---\nsubject: guide\npart_of: overview\n---\n# Guide\nSee [[FLW]], [[FLW-T-0001]], [[W-0001]], and [[ABC]].\n")

	links, broken := SemanticLinks(loadCorpus(t, root))
	for _, link := range links {
		if link.From == "guide" && link.Kind == "link" {
			t.Fatalf("tracker reference became a document edge: %#v", link)
		}
	}
	var foundABC bool
	for _, link := range broken {
		if link.Path == "docs/system/guide.md" && link.Ref == "ABC" {
			foundABC = true
		}
		if link.Ref == "FLW" || link.Ref == "FLW-T-0001" || link.Ref == "W-0001" {
			t.Fatalf("known tracker reference became a dangling document link: %#v", link)
		}
	}
	if !foundABC {
		t.Fatalf("unrecognized bare reference was suppressed: %#v", broken)
	}
}

func TestRepositorySpecsAreDiscoverableFromCanonicalRoot(t *testing.T) {
	// Go runs package tests in their source directory. runtime.Caller paths
	// are module-relative under -trimpath and are not filesystem locations.
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	corpus, _, err := LoadRepository(repoRoot)
	if err != nil {
		t.Fatalf("LoadRepository(%q) error = %v", repoRoot, err)
	}
	for _, test := range []struct {
		query string
		path  string
	}{
		{query: "spec-to-proof", path: ".tusker/specs/spec-to-proof.md"},
		{query: "tusker-trust-and-efficiency", path: ".tusker/specs/tusker-trust-and-efficiency.md"},
	} {
		result := Find(corpus, test.query)
		if len(result.Matches) != 1 || result.Matches[0].Path != test.path || result.Matches[0].Subject != test.query {
			t.Fatalf("Find(%q) = %#v, want one canonical match at %s", test.query, result, test.path)
		}
		if result.Matches[0].ReadWhen == "" || result.Matches[0].SkipWhen == "" {
			t.Fatalf("Find(%q) omitted routing metadata: %#v", test.query, result.Matches[0])
		}
	}
	links, broken := SemanticLinks(corpus)
	var contractLink bool
	for _, link := range links {
		if link.From == "tusker-trust-and-efficiency" && link.To == "spec-to-proof" && link.Kind == "link" {
			contractLink = true
		}
	}
	if !contractLink {
		t.Fatalf("canonical trust spec lost its relative link to spec-to-proof: %#v", links)
	}
	for _, link := range broken {
		if link.Path != ".tusker/specs/spec-to-proof.md" && link.Path != ".tusker/specs/tusker-trust-and-efficiency.md" {
			continue
		}
		if trackerReferenceProject(link.Ref) != "" || v7schema.WaveIDPattern.MatchString(link.Ref) || v7schema.EscalationIDPattern.MatchString(link.Ref) {
			t.Fatalf("canonical spec tracker reference became a dangling doc link: %#v", link)
		}
	}
}

func TestExtractReferencesSkipsFencedExamplesAndImages(t *testing.T) {
	body := "See [[guide|the guide]] and [spec](../specs/current.md#why).\n\n```md\n[[example]]\n```\n\n![preview](preview.png)\n"
	refs := ExtractReferences(body)
	joined := strings.Join(refs, ",")
	if joined != "guide,../specs/current.md" {
		t.Fatalf("references = %#v, want guide,../specs/current.md", refs)
	}
}

func TestFindIsBoundedAndReportsTotal(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	for i := 0; i < 100; i++ {
		writeDoc(t, root, fmt.Sprintf(".tusker/specs/spec-%03d.md", i), fmt.Sprintf("---\nsubject: spec-%03d\nkeywords: [routing]\npart_of: overview\n---\n# Spec %03d\n", i, i))
	}
	result := Find(loadCorpus(t, root), "routing")
	if len(result.Matches) != DefaultFindLimit || result.TotalMatches != 100 || !result.Truncated {
		t.Fatalf("bounded find = matches %d total %d truncated %v limit %d", len(result.Matches), result.TotalMatches, result.Truncated, result.Limit)
	}
}
