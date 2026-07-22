package main

import (
	"net/http"
	"regexp"
	"sort"
	"strings"

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
	Header    map[string]any          `json:"header"`
	Body      string                  `json:"body"`
	Links     []serveDocgraphLink     `json:"links"`
	Backlinks []serveDocgraphBacklink `json:"backlinks"`
	Successor *serveDocgraphSuccessor `json:"successor"`
}

// Ref is everything before an optional |label, mirroring the UI's wiki-link
// syntax exactly; drift here makes the reader render links the API never
// resolved.
var serveDocgraphWikiLink = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)

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

	subjects := map[string]struct{}{}
	for _, doc := range docs {
		if doc.Subject != "" {
			subjects[doc.Subject] = struct{}{}
		}
	}

	out := serveDocgraphList{
		Docs:   make([]serveDocgraphDoc, 0, len(docs)),
		Graph:  serveDocgraphGraph{Nodes: []serveDocgraphNode{}, GraphGenerated: true, Edges: []serveDocgraphEdge{}},
		Issues: make([]serveDocgraphIssue, 0, len(issues)),
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
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		if target := strings.TrimSpace(doc.PartOf); target != "" {
			if _, ok := subjects[target]; ok {
				out.Graph.Edges = append(out.Graph.Edges, serveDocgraphEdge{From: doc.Subject, To: target, Kind: "part_of"})
			}
		}
		for _, raw := range doc.Updates {
			target := strings.TrimSpace(raw)
			if _, ok := subjects[target]; ok {
				out.Graph.Edges = append(out.Graph.Edges, serveDocgraphEdge{From: doc.Subject, To: target, Kind: "updates"})
			}
		}
		if target := strings.TrimSpace(doc.DecidesFor); target != "" {
			if _, ok := subjects[target]; ok {
				out.Graph.Edges = append(out.Graph.Edges, serveDocgraphEdge{From: doc.Subject, To: target, Kind: "decides_for"})
			}
		}
		if target := strings.TrimSpace(doc.SupersededBy); target != "" {
			if _, ok := subjects[target]; ok {
				out.Graph.Edges = append(out.Graph.Edges, serveDocgraphEdge{From: doc.Subject, To: target, Kind: "superseded_by"})
			}
		}
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
	for _, issue := range issues {
		out.Issues = append(out.Issues, serveDocgraphIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
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
	subjectPath := map[string]string{}
	var target *docgraph.Document
	for i := range corpus.Documents {
		doc := &corpus.Documents[i]
		if doc.Subject != "" {
			subjectPath[doc.Subject] = doc.Path
		}
		if doc.Subject == subject {
			target = doc
		}
	}
	if target == nil {
		serveJSON(w, http.StatusNotFound, map[string]any{"error": "doc not found"})
		return
	}

	detail := serveDocgraphDetail{
		Subject:   target.Subject,
		Title:     serveDocgraphTitle(*target),
		Path:      target.Path,
		Kind:      string(target.Kind),
		Status:    target.Status,
		Header:    target.Raw,
		Body:      target.Body,
		Links:     serveDocgraphLinks(target.Body, subjectPath),
		Backlinks: serveDocgraphBacklinks(subject, corpus.Documents),
	}
	if successor := strings.TrimSpace(target.SupersededBy); successor != "" && strings.EqualFold(strings.TrimSpace(target.Status), "superseded") {
		if path, ok := subjectPath[successor]; ok {
			detail.Successor = &serveDocgraphSuccessor{Subject: successor, Path: path}
		}
	}
	serveJSON(w, http.StatusOK, detail)
}

func serveDocgraphLinks(body string, subjectPath map[string]string) []serveDocgraphLink {
	links := []serveDocgraphLink{}
	seen := map[string]struct{}{}
	for _, match := range serveDocgraphWikiLink.FindAllStringSubmatch(body, -1) {
		ref := strings.TrimSpace(match[1])
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		link := serveDocgraphLink{Ref: ref}
		if path, ok := subjectPath[ref]; ok {
			link.Subject = ref
			link.Path = path
			link.Resolved = true
		}
		links = append(links, link)
	}
	return links
}

func serveDocgraphBacklinks(subject string, docs []docgraph.Document) []serveDocgraphBacklink {
	backlinks := []serveDocgraphBacklink{}
	for i := range docs {
		doc := docs[i]
		if doc.Subject == subject || strings.TrimSpace(doc.Subject) == "" {
			continue
		}
		add := func(via string) {
			backlinks = append(backlinks, serveDocgraphBacklink{
				Subject: doc.Subject,
				Title:   serveDocgraphTitle(doc),
				Path:    doc.Path,
				Kind:    string(doc.Kind),
				Via:     via,
			})
		}
		if serveDocgraphBodyReferences(doc.Body, subject) {
			add("wiki")
		}
		if strings.TrimSpace(doc.PartOf) == subject {
			add("part_of")
		}
		if serveDocgraphListContains(doc.Updates, subject) {
			add("updates")
		}
		if strings.TrimSpace(doc.DecidesFor) == subject {
			add("decides_for")
		}
		if strings.TrimSpace(doc.SupersededBy) == subject {
			add("superseded_by")
		}
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

func serveDocgraphBodyReferences(body, subject string) bool {
	for _, match := range serveDocgraphWikiLink.FindAllStringSubmatch(body, -1) {
		if strings.TrimSpace(match[1]) == subject {
			return true
		}
	}
	return false
}

func serveDocgraphListContains(values []string, subject string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == subject {
			return true
		}
	}
	return false
}
