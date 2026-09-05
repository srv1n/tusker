package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"tusker/internal/docgraph"
)

type serveDocgraphDoc struct {
	Subject      string   `json:"subject"`
	Title        string   `json:"title"`
	Path         string   `json:"path"`
	Kind         string   `json:"kind"`
	Status       string   `json:"status"`
	Keywords     []string `json:"keywords"`
	PartOf       string   `json:"part_of"`
	Updates      []string `json:"updates"`
	DecidesFor   string   `json:"decides_for"`
	SupersededBy string   `json:"superseded_by"`
}

type serveDocgraphNode struct {
	Subject string `json:"subject"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Status  string `json:"status"`
}

type serveDocgraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type serveDocgraphGraph struct {
	Nodes          []serveDocgraphNode `json:"nodes"`
	GraphGenerated bool                `json:"graph_generated"`
	Edges          []serveDocgraphEdge `json:"edges"`
}

type serveDocgraphIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type serveDocgraphList struct {
	Docs   []serveDocgraphDoc   `json:"docs"`
	Graph  serveDocgraphGraph   `json:"graph"`
	Issues []serveDocgraphIssue `json:"issues"`
}

type serveDocgraphLink struct {
	Ref      string `json:"ref"`
	Subject  string `json:"subject"`
	Path     string `json:"path"`
	Resolved bool   `json:"resolved"`
}

// serveDocgraphBacklink uses the same semantic edge kinds as the graph API;
// body links are reported as "link", alongside typed metadata relationships.
type serveDocgraphBacklink struct {
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Via     string `json:"via"`
}

type serveDocgraphSuccessor struct {
	Subject string `json:"subject"`
	Path    string `json:"path"`
}

type serveDocgraphDetail struct {
	Subject   string                  `json:"subject"`
	Title     string                  `json:"title"`
	Path      string                  `json:"path"`
	Kind      string                  `json:"kind"`
	Status    string                  `json:"status"`
	Rev       string                  `json:"rev"`
	Header    map[string]any          `json:"header"`
	Body      string                  `json:"body"`
	Links     []serveDocgraphLink     `json:"links"`
	Backlinks []serveDocgraphBacklink `json:"backlinks"`
	Successor *serveDocgraphSuccessor `json:"successor"`
}

// serveDocgraphSaveRequest is the PUT /api/docgraph/doc body. Body and Header
// are optional; a nil pointer / absent raw message means "leave this region as
// it is on disk". base_rev is the sha256 the client loaded, for optimistic
// concurrency.
type serveDocgraphSaveRequest struct {
	BaseRev string          `json:"base_rev"`
	Body    *string         `json:"body"`
	Header  json.RawMessage `json:"header"`
	Actor   string          `json:"actor,omitempty"`
}

// serveDocgraphSaveResponse is the GET detail shape plus warnings (never null).
type serveDocgraphSaveResponse struct {
	serveDocgraphDetail
	Warnings []string `json:"warnings"`
}

func serveDocgraphKindRank(kind docgraph.Kind) int {
	switch kind {
	case docgraph.KindCanonical:
		return 0
	case docgraph.KindSpec:
		return 1
	case docgraph.KindDecision:
		return 2
	default:
		return 3
	}
}

func serveDocgraphTitle(doc docgraph.Document) string {
	for _, line := range strings.Split(doc.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return doc.Subject
}

func serveDocgraphStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return values
}

func (s *serveServer) handleDocgraph(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	corpus, issues, err := docgraph.LoadRepository(project.RepoRoot)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	docs := make([]docgraph.Document, len(corpus.Documents))
	copy(docs, corpus.Documents)
	sort.SliceStable(docs, func(i, j int) bool {
		if ri, rj := serveDocgraphKindRank(docs[i].Kind), serveDocgraphKindRank(docs[j].Kind); ri != rj {
			return ri < rj
		}
		return docs[i].Subject < docs[j].Subject
	})

	out := serveDocgraphList{
		Docs:   make([]serveDocgraphDoc, 0, len(docs)),
		Graph:  serveDocgraphGraph{Nodes: []serveDocgraphNode{}, GraphGenerated: true, Edges: []serveDocgraphEdge{}},
		Issues: make([]serveDocgraphIssue, 0, len(issues)),
	}
	for _, issue := range issues {
		out.Issues = append(out.Issues, serveDocgraphIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
	for _, doc := range docs {
		title := serveDocgraphTitle(doc)
		out.Docs = append(out.Docs, serveDocgraphDoc{
			Subject:      doc.Subject,
			Title:        title,
			Path:         doc.Path,
			Kind:         string(doc.Kind),
			Status:       doc.Status,
			Keywords:     serveDocgraphStrings(doc.Keywords),
			PartOf:       doc.PartOf,
			Updates:      serveDocgraphStrings(doc.Updates),
			DecidesFor:   doc.DecidesFor,
			SupersededBy: doc.SupersededBy,
		})
		if doc.Subject == "" {
			continue
		}
		out.Graph.Nodes = append(out.Graph.Nodes, serveDocgraphNode{
			Subject: doc.Subject,
			Kind:    string(doc.Kind),
			Path:    doc.Path,
			Title:   title,
			Status:  doc.Status,
		})
	}
	semanticLinks, _ := docgraph.SemanticLinks(corpus)
	for _, link := range semanticLinks {
		out.Graph.Edges = append(out.Graph.Edges, serveDocgraphEdge{From: link.From, To: link.To, Kind: link.Kind})
	}
	sort.SliceStable(out.Graph.Edges, func(i, j int) bool {
		a, b := out.Graph.Edges[i], out.Graph.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.To < b.To
	})
	serveJSON(w, http.StatusOK, out)
}

func (s *serveServer) handleDocgraphDoc(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	if subject == "" {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "subject is required"})
		return
	}
	corpus, _, err := docgraph.LoadRepository(project.RepoRoot)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	detail, ok := serveDocgraphBuildDetail(project.RepoRoot, corpus, subject)
	if !ok {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "doc not found"})
		return
	}
	serveJSON(w, http.StatusOK, detail)
}

// serveDocgraphBuildDetail assembles the read-detail shape (links, backlinks,
// successor, and the on-disk rev) for one subject in an already-loaded corpus.
// Shared by GET and the PUT save response so both stay identical.
func serveDocgraphBuildDetail(repoRoot string, corpus docgraph.Corpus, subject string) (serveDocgraphDetail, bool) {
	var target *docgraph.Document
	for i := range corpus.Documents {
		doc := &corpus.Documents[i]
		if doc.Subject == subject {
			target = doc
		}
	}
	if target == nil {
		return serveDocgraphDetail{}, false
	}
	resolver := docgraph.NewResolver(corpus)
	detail := serveDocgraphDetail{
		Subject:   target.Subject,
		Title:     serveDocgraphTitle(*target),
		Path:      target.Path,
		Kind:      string(target.Kind),
		Status:    target.Status,
		Rev:       serveDocgraphFileRev(repoRoot, target.Path),
		Header:    target.Raw,
		Body:      target.Body,
		Links:     serveDocgraphLinks(*target, resolver),
		Backlinks: serveDocgraphBacklinks(subject, corpus, resolver),
	}
	if successor := strings.TrimSpace(target.SupersededBy); successor != "" && strings.EqualFold(strings.TrimSpace(target.Status), "superseded") {
		if resolved, ok := resolver.ResolveFrom(target.Path, successor); ok {
			detail.Successor = &serveDocgraphSuccessor{Subject: resolved.Document.Subject, Path: resolved.Document.Path}
		}
	}
	return detail, true
}

// handleDocgraphDocSave splices a body and/or header edit into the on-disk
// document addressed by subject (never a client path), refuses on a stale
// base_rev (409) or any newly-introduced defect (422), and writes atomically.
func (s *serveServer) handleDocgraphDocSave(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForSnapshot(strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return
	}
	subject := strings.TrimSpace(r.URL.Query().Get("subject"))
	if subject == "" {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "subject is required"})
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "could not read request body"})
		return
	}
	var req serveDocgraphSaveRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body: " + err.Error()})
		return
	}
	if _, actorErr := s.serveOperatorActor(serveActionBody{"actor": req.Actor}, "serve docgraph save"); actorErr != nil {
		status, result := serveOperatorActorResult("docgraph save", actorErr)
		serveJSON(w, status, result)
		return
	}
	baseRev := strings.TrimSpace(req.BaseRev)
	if baseRev == "" {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "base_rev is required"})
		return
	}
	headerPresent := len(req.Header) > 0 && !bytes.Equal(bytes.TrimSpace(req.Header), []byte("null"))
	bodyPresent := req.Body != nil
	if !headerPresent && !bodyPresent {
		serveJSON(w, http.StatusBadRequest, map[string]any{"error": "body or header is required"})
		return
	}

	corpus, _, err := docgraph.LoadRepository(project.RepoRoot)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	targetIndex := -1
	for i := range corpus.Documents {
		if corpus.Documents[i].Subject == subject {
			targetIndex = i
			break
		}
	}
	if targetIndex < 0 {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "doc not found"})
		return
	}
	relPath := corpus.Documents[targetIndex].Path
	original, err := docgraph.ReadDocumentFile(project.RepoRoot, relPath)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	currentRev := serveDocgraphRev(original)
	if currentRev != baseRev {
		serveJSON(w, http.StatusConflict, map[string]any{
			"error":       "document changed on disk since it was loaded; reload before saving",
			"code":        "DOC_SAVE_CONFLICT",
			"current_rev": currentRev,
		})
		return
	}

	_, bodyStart, ok := serveDocgraphSplitFile(string(original))
	if !ok {
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "document is missing valid front matter",
			"defects": []serveDocgraphIssue{{
				Code:    "DOC_HEADER_MISSING",
				Path:    relPath,
				Message: "missing YAML front matter (expected an opening --- line)",
			}},
		})
		return
	}

	var headerRegion string
	if headerPresent {
		var headerMap map[string]any
		if err := json.Unmarshal(req.Header, &headerMap); err != nil {
			serveJSON(w, http.StatusBadRequest, map[string]any{"error": "header must be a JSON object"})
			return
		}
		yamlBytes, err := serveDocgraphMarshalHeader(headerMap)
		if err != nil {
			serveJSON(w, http.StatusBadRequest, map[string]any{"error": "could not encode header: " + err.Error()})
			return
		}
		headerRegion = "---\n" + yamlBytes + "---\n"
	} else {
		// Header untouched: preserve the exact opening delimiter, YAML, and
		// closing delimiter bytes (acceptance A5).
		headerRegion = string(original[:bodyStart])
	}

	var bodyRegion string
	if bodyPresent {
		bodyRegion = serveDocgraphComposeBody(*req.Body)
	} else {
		// Body untouched: preserve exact bytes after the closing delimiter.
		bodyRegion = string(original[bodyStart:])
	}
	newContent := headerRegion + bodyRegion

	newDoc, parseErr := docgraph.ParseDocHeaders(relPath, []byte(newContent))
	if parseErr != nil {
		code := "DOC_HEADER_PARSE_ERROR"
		msg := parseErr.Error()
		if pe, ok := parseErr.(*docgraph.ParseError); ok {
			code = pe.Code
			msg = pe.Message
		}
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   "edit would produce an invalid document",
			"defects": []serveDocgraphIssue{{Code: code, Path: relPath, Message: msg}},
		})
		return
	}

	baseIssues := docgraph.ValidateCorpus(corpus)
	editedDocs := make([]docgraph.Document, len(corpus.Documents))
	copy(editedDocs, corpus.Documents)
	editedDocs[targetIndex] = newDoc
	editedIssues := docgraph.ValidateCorpus(docgraph.Corpus{Documents: editedDocs})
	if defects := serveDocgraphNewDefects(baseIssues, editedIssues); len(defects) > 0 {
		serveJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   "edit would break the document's rules",
			"defects": defects,
		})
		return
	}

	if err := serveDocgraphAtomicWrite(project.RepoRoot, relPath, []byte(newContent)); err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	reloaded, _, err := docgraph.LoadRepository(project.RepoRoot)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	detail, ok := serveDocgraphBuildDetail(project.RepoRoot, reloaded, newDoc.Subject)
	if !ok {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": "saved document could not be reloaded"})
		return
	}
	warnings := []string{}
	if stale, err := docgraph.CheckDocsMapFresh(project.RepoRoot); err == nil && len(stale) > 0 {
		warnings = append(warnings, "docs map is stale; run tusker docs map")
	}
	serveJSON(w, http.StatusOK, serveDocgraphSaveResponse{serveDocgraphDetail: detail, Warnings: warnings})
}

// serveDocgraphSplitFile locates the front-matter boundary in a document's raw
// bytes without normalizing, so a body-only save preserves the header bytes
// exactly. bodyStart is the offset of the body, just past the closing "---\n".
func serveDocgraphSplitFile(raw string) (headerYAML string, bodyStart int, ok bool) {
	// CRLF files parse fine on the read path (parseFrontmatter normalizes), so
	// the save path must accept them too; the closing-delimiter scan already
	// tolerates \r via TrimSpace.
	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		return "", 0, false
	}
	lines := strings.SplitAfter(raw, "\n")
	openingLength := len(lines[0])
	lineStart := openingLength
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\n")
		if strings.TrimSpace(line) == "---" {
			return raw[openingLength:lineStart], lineStart + len(lines[i]), true
		}
		lineStart += len(lines[i])
	}
	return "", 0, false
}

// serveDocgraphComposeBody normalizes an edited body into the tail that follows
// the closing delimiter: exactly one blank line, then the body with a single
// trailing newline.
func serveDocgraphComposeBody(body string) string {
	b := strings.TrimLeft(body, "\n")
	if !strings.HasSuffix(b, "\n") {
		b += "\n"
	}
	return "\n" + b
}

// serveDocgraphMarshalHeader re-marshals an edited header as YAML with 2-space
// indentation, matching the corpus's on-disk convention.
func serveDocgraphMarshalHeader(header map[string]any) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(header); err != nil {
		_ = enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// serveDocgraphNewDefects returns edited-corpus issues that were not already
// present before the edit, so a save is only refused for defects it introduces.
func serveDocgraphNewDefects(base, edited []docgraph.Issue) []serveDocgraphIssue {
	seen := map[string]struct{}{}
	for _, issue := range base {
		seen[issue.Code+"\x00"+issue.Path+"\x00"+issue.Message] = struct{}{}
	}
	defects := []serveDocgraphIssue{}
	for _, issue := range edited {
		if _, ok := seen[issue.Code+"\x00"+issue.Path+"\x00"+issue.Message]; ok {
			continue
		}
		defects = append(defects, serveDocgraphIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
	return defects
}

// serveDocgraphAtomicWrite writes content to a temp file in the same directory
// and renames it into place, so a failed or partial write never corrupts the
// document.
func serveDocgraphAtomicWrite(repoRoot, relPath string, content []byte) error {
	return docgraph.WriteDocumentFile(repoRoot, relPath, content)
}

func serveDocgraphFileRev(repoRoot, relPath string) string {
	raw, err := docgraph.ReadDocumentFile(repoRoot, relPath)
	if err != nil {
		return ""
	}
	return serveDocgraphRev(raw)
}

func serveDocgraphRev(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func serveDocgraphLinks(doc docgraph.Document, resolver *docgraph.Resolver) []serveDocgraphLink {
	links := []serveDocgraphLink{}
	for _, ref := range docgraph.ExtractReferences(doc.Body) {
		link := serveDocgraphLink{Ref: ref}
		if resolved, ok := resolver.ResolveFrom(doc.Path, ref); ok {
			link.Subject = resolved.Document.Subject
			link.Path = resolved.Document.Path
			link.Resolved = true
		}
		links = append(links, link)
	}
	return links
}

func serveDocgraphBacklinks(subject string, corpus docgraph.Corpus, resolver *docgraph.Resolver) []serveDocgraphBacklink {
	backlinks := []serveDocgraphBacklink{}
	for _, link := range resolver.Backlinks(subject, corpus) {
		source, ok := resolver.Resolve(link.From)
		if !ok || source.Document.Subject == subject {
			continue
		}
		backlinks = append(backlinks, serveDocgraphBacklink{
			Subject: source.Document.Subject,
			Title:   serveDocgraphTitle(source.Document),
			Path:    source.Document.Path,
			Kind:    string(source.Document.Kind),
			Via:     link.Kind,
		})
	}
	sort.SliceStable(backlinks, func(i, j int) bool {
		a, b := backlinks[i], backlinks[j]
		if ra, rb := serveDocgraphKindRank(docgraph.Kind(a.Kind)), serveDocgraphKindRank(docgraph.Kind(b.Kind)); ra != rb {
			return ra < rb
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Via < b.Via
	})
	return backlinks
}
