package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func servePut(t *testing.T, server *serveServer, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:7420"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func docRev(t *testing.T, server *serveServer, subject string) string {
	t.Helper()
	var out serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject="+subject, &out)
	if out.Rev == "" {
		t.Fatalf("GET %s returned empty rev", subject)
	}
	return out.Rev
}

func docFileRev(t *testing.T, repoRoot, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func hasDefect(defects []serveDocgraphIssue, code string) bool {
	for _, d := range defects {
		if d.Code == code {
			return true
		}
	}
	return false
}

// headerRegion returns the bytes up to and including the closing --- delimiter.
func headerRegion(raw string) string {
	for _, marker := range []string{"\n---\n", "\r\n---\r\n"} {
		if idx := strings.Index(raw, marker); idx >= 0 {
			return raw[:idx+len(marker)]
		}
	}
	return raw
}

func TestDocSaveWritesFile(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	rev := docRev(t, server, "alpha")
	newBody := "# Alpha Spec\n\nRewritten body with [[beta]].\n"
	payload := mustJSON(t, map[string]any{"base_rev": rev, "body": newBody})
	code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=alpha", payload)
	if code != http.StatusOK {
		t.Fatalf("save status = %d: %s", code, raw)
	}

	var resp serveDocgraphSaveResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if !strings.Contains(resp.Body, "Rewritten body with") {
		t.Fatalf("response body did not reflect edit: %q", resp.Body)
	}
	if resp.Warnings == nil {
		t.Fatalf("warnings must never be null: %s", raw)
	}
	if resp.Rev == rev {
		t.Fatalf("rev should change after an edit")
	}
	if want := docFileRev(t, server.repoRoot, ".tusker/specs/alpha.md"); resp.Rev != want {
		t.Fatalf("response rev %q != sha256 of new file bytes %q", resp.Rev, want)
	}
	content, err := os.ReadFile(filepath.Join(server.repoRoot, ".tusker/specs/alpha.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Rewritten body with [[beta]].") {
		t.Fatalf("edit not persisted to disk: %q", content)
	}
}

func TestDocSaveRejectsSymlinkedDocumentPath(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs", "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(external, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(repoRoot, "docs", "system", "doc.md")
	if err := os.Symlink(external, leaf); err != nil {
		t.Fatal(err)
	}
	if err := serveDocgraphAtomicWrite(repoRoot, "docs/system/doc.md", []byte("changed\n")); err == nil {
		t.Fatal("atomic document save followed a symlinked leaf")
	}
	if got, err := os.ReadFile(external); err != nil || string(got) != "outside\n" {
		t.Fatalf("external document changed: %q (%v)", got, err)
	}
}

func TestDocSaveRefusesHeaderDefects(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	path := filepath.Join(server.repoRoot, ".tusker/specs/alpha.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rev := docRev(t, server, "alpha")

	// Rename alpha's subject onto beta's -> duplicate subject.
	header := map[string]any{
		"title":    "Alpha Spec",
		"subject":  "beta",
		"part_of":  "overview",
		"keywords": []string{"alpha"},
		"status":   "active",
	}
	payload := mustJSON(t, map[string]any{"base_rev": rev, "header": header})
	code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=alpha", payload)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", code, raw)
	}
	var resp struct {
		Defects []serveDocgraphIssue `json:"defects"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if !hasDefect(resp.Defects, "DOC_DUPLICATE_SUBJECT") {
		t.Fatalf("expected DOC_DUPLICATE_SUBJECT defect: %#v", resp.Defects)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("file must be byte-unchanged on refusal")
	}
}

func TestDocSaveRefreshesCorpus(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	rev := docRev(t, server, "beta")
	newBody := "# Beta Renamed\n\nRefers to [[alpha]] and now [[overview]].\n"
	payload := mustJSON(t, map[string]any{"base_rev": rev, "body": newBody})
	code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=beta", payload)
	if code != http.StatusOK {
		t.Fatalf("save status = %d: %s", code, raw)
	}

	// Backlinks refresh without a restart: overview now backlinked from beta.
	var overview serveDocgraphDetail
	serveDecode(t, server, "/api/docgraph/doc?project=app&subject=overview", &overview)
	if !hasBacklink(overview.Backlinks, "beta", "wiki") {
		t.Fatalf("overview backlinks missing edited beta via wiki: %#v", overview.Backlinks)
	}

	// The list reflects the H1 title change.
	var list serveDocgraphList
	serveDecode(t, server, "/api/docgraph?project=app", &list)
	found := false
	for _, d := range list.Docs {
		if d.Subject == "beta" {
			found = true
			if d.Title != "Beta Renamed" {
				t.Fatalf("beta title = %q, want %q", d.Title, "Beta Renamed")
			}
		}
	}
	if !found {
		t.Fatalf("beta missing from refreshed list")
	}
}

func TestDocSaveConflictRefused(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	path := filepath.Join(server.repoRoot, ".tusker/specs/alpha.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payload := mustJSON(t, map[string]any{"base_rev": "deadbeef", "body": "# Alpha Spec\n\nStale write.\n"})
	code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=alpha", payload)
	if code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", code, raw)
	}
	var resp struct {
		Code       string `json:"code"`
		CurrentRev string `json:"current_rev"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	if resp.Code != "DOC_SAVE_CONFLICT" {
		t.Fatalf("code = %q, want DOC_SAVE_CONFLICT", resp.Code)
	}
	if want := docFileRev(t, server.repoRoot, ".tusker/specs/alpha.md"); resp.CurrentRev != want {
		t.Fatalf("current_rev = %q, want %q", resp.CurrentRev, want)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("file must be unchanged on conflict")
	}
}

func TestDocSaveBodyOnlyPreservesHeaderBytes(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)

	path := filepath.Join(server.repoRoot, ".tusker/specs/alpha.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	headerBefore := headerRegion(string(before))

	rev := docRev(t, server, "alpha")
	payload := mustJSON(t, map[string]any{"base_rev": rev, "body": "# Alpha Spec\n\nBrand new body only.\n"})
	code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=alpha", payload)
	if code != http.StatusOK {
		t.Fatalf("status = %d: %s", code, raw)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if headerBefore != headerRegion(string(after)) {
		t.Fatalf("header region changed on body-only save:\nbefore=%q\nafter=%q", headerBefore, headerRegion(string(after)))
	}
	if !strings.Contains(string(after), "Brand new body only.") {
		t.Fatalf("body edit not persisted: %q", after)
	}
}

func TestDocSaveHandlesCRLFDocuments(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)
	path := filepath.Join(server.repoRoot, ".tusker/specs/crlf.md")
	content := "---\r\ntitle: \"CRLF Spec\"\r\nsubject: crlf\r\npart_of: overview\r\nkeywords: [crlf]\r\nstatus: active\r\n---\r\n\r\n# CRLF Spec\r\n\r\nWindows line endings.\r\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	headerBefore := headerRegion(content)

	rev := docRev(t, server, "crlf")
	payload := mustJSON(t, map[string]any{"base_rev": rev, "body": "# CRLF Spec\n\nEdited body.\n"})
	code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=crlf", payload)
	if code != http.StatusOK {
		t.Fatalf("CRLF doc save status = %d, want 200: %s", code, raw)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if headerBefore != headerRegion(string(after)) {
		t.Fatalf("CRLF header region changed on body-only save:\nbefore=%q\nafter=%q", headerBefore, headerRegion(string(after)))
	}
	if !strings.Contains(string(after), "Edited body.") {
		t.Fatalf("body edit not persisted: %q", after)
	}
}

func TestDocSaveRejectsBadRequests(t *testing.T) {
	server := newServeFixture(t)
	seedDocgraphCorpus(t, server.repoRoot)
	rev := docRev(t, server, "alpha")

	if code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=alpha",
		mustJSON(t, map[string]any{"body": "# X\n\nx\n"})); code != http.StatusBadRequest {
		t.Fatalf("missing base_rev status = %d, want 400: %s", code, raw)
	}
	if code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=alpha",
		mustJSON(t, map[string]any{"base_rev": rev})); code != http.StatusBadRequest {
		t.Fatalf("no body/header status = %d, want 400: %s", code, raw)
	}
	if code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=does-not-exist",
		mustJSON(t, map[string]any{"base_rev": rev, "body": "# X\n\nx\n"})); code != http.StatusNotFound {
		t.Fatalf("unknown subject status = %d, want 404: %s", code, raw)
	}
}
