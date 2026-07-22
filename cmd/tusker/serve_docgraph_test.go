package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func seedDocgraphCorpus(t *testing.T, repoRoot string) {
	t.Helper()
	writeDocgraphDoc(t, repoRoot, "docs/system/00-overview.md",
		"title: \"System Overview\"\nsubject: overview\nkeywords: [overview]\nstatus: canonical\n",
		"# System Overview\n\nRoot doc.\n")
	writeDocgraphDoc(t, repoRoot, ".tusker/specs/alpha.md",
		"title: \"Alpha Spec\"\nsubject: alpha\npart_of: overview\nkeywords: [alpha]\nstatus: active\n",
		"# Alpha Spec\n\nSee [[beta]] and [[ghost]].\n")
	writeDocgraphDoc(t, repoRoot, ".tusker/specs/beta.md",
		"title: \"Beta Spec\"\nsubject: beta\npart_of: overview\nkeywords: [beta]\nstatus: active\n",
		"# Beta Spec\n\nRefers to [[alpha]].\n")
	writeDocgraphDoc(t, repoRoot, ".tusker/specs/decisions/alpha-decision.md",
		"title: \"Alpha Decision\"\nsubject: alpha-decision\npart_of: alpha\ndecides_for: alpha\nkeywords: [decision]\nstatus: accepted\n",
		"# Alpha Decision\n\nChose alpha.\n")
}

func writeDocgraphDoc(t *testing.T, repoRoot, rel, frontmatter, body string) {
	t.Helper()
	full := filepath.Join(repoRoot, filepath.FromSlash(rel))
	if err := ensureDir(filepath.Dir(full)); err != nil {
		t.Fatal(err)
	}
	if err := writeText(full, "---\n"+frontmatter+"---\n\n"+body); err != nil {
		t.Fatal(err)
	}
}

func serveGetStatus(t *testing.T, server *serveServer, path string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec.Code
}

func TestDocsListEndpoint(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	var out serveDocgraphList
	serveDecode(t, server, "/api/docgraph?project=app", &out)

	if len(out.Docs) != 4 {
		t.Fatalf("expected 4 docs, got %d: %#v", len(out.Docs), out.Docs)
	}
	wantKinds := []string{"canonical", "spec", "spec", "decision"}
	wantSubjects := []string{"overview", "alpha", "beta", "alpha-decision"}
	for i, doc := range out.Docs {
		if doc.Kind != wantKinds[i] {
			t.Fatalf("doc %d kind = %q, want %q", i, doc.Kind, wantKinds[i])
		}
		if doc.Subject != wantSubjects[i] {
			t.Fatalf("doc %d subject = %q, want %q", i, doc.Subject, wantSubjects[i])
		}
	}
	alpha := out.Docs[1]
	if alpha.Title != "Alpha Spec" || alpha.PartOf != "overview" || alpha.Status != "active" {
		t.Fatalf("alpha grouping fields unexpected: %#v", alpha)
	}
	if len(alpha.Keywords) != 1 || alpha.Keywords[0] != "alpha" {
		t.Fatalf("alpha keywords unexpected: %#v", alpha.Keywords)
	}

	if !out.Graph.GraphGenerated {
		t.Fatalf("graph_generated should be true")
	}
	if len(out.Graph.Nodes) != 4 {
		t.Fatalf("expected 4 graph nodes, got %d", len(out.Graph.Nodes))
	}
	if len(out.Graph.Edges) < 3 {
		t.Fatalf("expected graph edges present, got %#v", out.Graph.Edges)
	}
	if !hasDocgraphEdge(out.Graph.Edges, "alpha", "overview", "part_of") {
		t.Fatalf("missing alpha->overview part_of edge: %#v", out.Graph.Edges)
	}
	if !hasDocgraphEdge(out.Graph.Edges, "alpha-decision", "alpha", "decides_for") {
		t.Fatalf("missing alpha-decision->alpha decides_for edge: %#v", out.Graph.Edges)
	}
}

func hasDocgraphEdge(edges []serveDocgraphEdge, from, to, kind string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to && edge.Kind == kind {
			return true
		}
	}
	return false
}

func TestDocDetailRendersBodyAndHeader(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	var out serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject=alpha", &out)

	if out.Title != "Alpha Spec" {
		t.Fatalf("title from H1 = %q, want %q", out.Title, "Alpha Spec")
	}
	if strings.Contains(out.Body, "part_of:") || strings.HasPrefix(strings.TrimSpace(out.Body), "---") {
		t.Fatalf("body still carries front-matter: %q", out.Body)
	}
	if !strings.Contains(out.Body, "# Alpha Spec") {
		t.Fatalf("body missing H1 heading: %q", out.Body)
	}
	for _, key := range []string{"subject", "part_of", "keywords", "title", "status"} {
		if _, ok := out.Header[key]; !ok {
			t.Fatalf("header missing key %q: %#v", key, out.Header)
		}
	}
	if out.Header["subject"] != "alpha" {
		t.Fatalf("header subject = %v, want alpha", out.Header["subject"])
	}
}

func TestDocLinksResolveAcrossCorpus(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	var out serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject=alpha", &out)

	if len(out.Links) != 2 {
		t.Fatalf("expected 2 links, got %#v", out.Links)
	}
	first := out.Links[0]
	if first.Ref != "beta" || !first.Resolved || first.Subject != "beta" || first.Path != ".tusker/specs/beta.md" {
		t.Fatalf("first link should resolve to beta: %#v", first)
	}
	second := out.Links[1]
	if second.Ref != "ghost" || second.Resolved || second.Subject != "" || second.Path != "" {
		t.Fatalf("second link should be unresolved: %#v", second)
	}
}

func TestDocBacklinksListed(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	var out serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject=alpha", &out)

	if !hasBacklink(out.Backlinks, "beta", "wiki") {
		t.Fatalf("expected beta backlink via wiki: %#v", out.Backlinks)
	}
	if !hasBacklink(out.Backlinks, "alpha-decision", "part_of") {
		t.Fatalf("expected alpha-decision backlink via part_of: %#v", out.Backlinks)
	}
}

func TestDocLinksPipeLabelSyntaxMatchesFrontend(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)
	writeDocgraphDoc(t, server.repoRoot, ".tusker/specs/gamma.md",
		"title: \"Gamma Spec\"\nsubject: gamma\npart_of: overview\nkeywords: [gamma]\nstatus: active\n",
		"# Gamma Spec\n\nSee [[alpha|the alpha spec]] for details.\n")

	var out serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject=gamma", &out)
	if len(out.Links) != 1 {
		t.Fatalf("expected 1 link, got %#v", out.Links)
	}
	link := out.Links[0]
	if link.Ref != "alpha" || !link.Resolved || link.Subject != "alpha" {
		t.Fatalf("piped wiki link should resolve by ref before the label: %#v", link)
	}

	var alpha serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject=alpha", &alpha)
	if !hasBacklink(alpha.Backlinks, "gamma", "wiki") {
		t.Fatalf("piped wiki link should register a wiki backlink: %#v", alpha.Backlinks)
	}
}

func hasBacklink(backlinks []serveDocgraphBacklink, subject, via string) bool {
	for _, backlink := range backlinks {
		if backlink.Subject == subject && backlink.Via == via {
			return true
		}
	}
	return false
}

func TestDocgraphUnknownSubjectNotFound(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)
	if code := serveGetStatus(t, server, "/api/docgraph/doc?project=app&subject=does-not-exist"); code != http.StatusNotFound {
		t.Fatalf("unknown subject status = %d, want 404", code)
	}
	if code := serveGetStatus(t, server, "/api/docgraph/doc?project=app"); code != http.StatusBadRequest {
		t.Fatalf("missing subject status = %d, want 400", code)
	}
}

func TestDocgraphEmptyCorpus(t *testing.T) {
	server := newServeFixture(t)

	var out serveDocgraphList
	serveDecode(t, server, "/api/docgraph?project=app", &out)

	if len(out.Docs) != 0 || len(out.Graph.Nodes) != 0 || len(out.Graph.Edges) != 0 {
		t.Fatalf("empty corpus should yield empty arrays: %#v", out)
	}
	if !out.Graph.GraphGenerated {
		t.Fatalf("graph_generated should be true even when empty")
	}
}
