package docgraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	docsMapBeginMarker = "<!-- tusker:docs-map:begin -->"
	docsMapEndMarker   = "<!-- tusker:docs-map:end -->"

	overviewRelPath = "docs/system/00-overview.md"
	indexRelPath    = "docs/system/INDEX.md"
	graphRelPath    = "docs/system/graph.json"
)

// MapDefect names one structural reason the doc graph cannot be turned into a
// map. Generation refuses the whole corpus when any defect is present.
type MapDefect struct {
	Code    string
	Path    string
	Message string
}

// MapError is returned when the corpus is malformed. It carries every defect so
// a caller can report all of them at once; nothing is written when it is set.
type MapError struct {
	Defects []MapDefect
}

func (e *MapError) Error() string {
	if e == nil || len(e.Defects) == 0 {
		return "doc graph is malformed"
	}
	parts := make([]string, 0, len(e.Defects))
	for _, defect := range e.Defects {
		parts = append(parts, fmt.Sprintf("%s: %s (%s)", defect.Code, defect.Message, defect.Path))
	}
	return "doc graph is malformed: " + strings.Join(parts, "; ")
}

type mapNode struct {
	Subject string `json:"subject"`
	Kind    Kind   `json:"kind"`
	Path    string `json:"path"`
	Title   string `json:"title"`
	Status  string `json:"status"`
}

type mapEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

type mapGraph struct {
	Nodes []mapNode `json:"nodes"`
	Edges []mapEdge `json:"edges"`
}

type mapArtifacts struct {
	overview string
	index    string
	graph    []byte
}

// WriteDocsMap regenerates the three doc-map artifacts on disk from the current
// front-matter corpus. It refuses malformed graphs and writes nothing in that
// case. Output is deterministic (sorted by subject) so re-running is a no-op.
func WriteDocsMap(repoRoot string) error {
	artifacts, err := buildDocsMap(repoRoot)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(repoRoot, filepath.FromSlash(overviewRelPath)), []byte(artifacts.overview), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(repoRoot, filepath.FromSlash(indexRelPath)), []byte(artifacts.index), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(repoRoot, filepath.FromSlash(graphRelPath)), artifacts.graph, 0o644)
}

// CheckDocsMapFresh regenerates the artifacts in memory and diffs them against
// the committed files. Stale or missing artifacts, and malformed graphs, are
// returned as named issues pointing back at the map command.
func CheckDocsMapFresh(repoRoot string) ([]Issue, error) {
	if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(overviewRelPath))); os.IsNotExist(err) {
		return nil, nil
	}
	artifacts, err := buildDocsMap(repoRoot)
	if err != nil {
		if mapErr, ok := err.(*MapError); ok {
			issues := make([]Issue, 0, len(mapErr.Defects))
			for _, defect := range mapErr.Defects {
				issues = append(issues, Issue{Code: defect.Code, Path: defect.Path, Message: defect.Message})
			}
			return issues, nil
		}
		return nil, err
	}
	var issues []Issue
	checks := []struct {
		rel  string
		want []byte
	}{
		{overviewRelPath, []byte(artifacts.overview)},
		{indexRelPath, []byte(artifacts.index)},
		{graphRelPath, artifacts.graph},
	}
	for _, check := range checks {
		committed, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(check.rel)))
		if err != nil {
			if os.IsNotExist(err) {
				issues = append(issues, Issue{
					Code:    "DOCS_MAP_STALE",
					Path:    check.rel,
					Message: "generated doc map is missing; run `tusker docs map`",
				})
				continue
			}
			return nil, err
		}
		if !bytes.Equal(committed, check.want) {
			issues = append(issues, Issue{
				Code:    "DOCS_MAP_STALE",
				Path:    check.rel,
				Message: "generated doc map is stale; run `tusker docs map`",
			})
		}
	}
	sortIssues(issues)
	return issues, nil
}

func buildDocsMap(repoRoot string) (mapArtifacts, error) {
	corpus, _, err := LoadRepository(repoRoot)
	if err != nil {
		return mapArtifacts{}, err
	}
	docs := make([]Document, len(corpus.Documents))
	copy(docs, corpus.Documents)
	sort.Slice(docs, func(i, j int) bool { return docs[i].Subject < docs[j].Subject })

	if defects := findMapDefects(docs); len(defects) > 0 {
		sort.Slice(defects, func(i, j int) bool {
			if defects[i].Path != defects[j].Path {
				return defects[i].Path < defects[j].Path
			}
			return defects[i].Code < defects[j].Code
		})
		return mapArtifacts{}, &MapError{Defects: defects}
	}

	overviewPath := filepath.Join(repoRoot, filepath.FromSlash(overviewRelPath))
	current, err := os.ReadFile(overviewPath)
	if err != nil {
		return mapArtifacts{}, err
	}
	overview, err := injectRegion(string(current), renderMermaid(docs))
	if err != nil {
		return mapArtifacts{}, err
	}

	graphBytes, err := renderGraphJSON(docs)
	if err != nil {
		return mapArtifacts{}, err
	}

	return mapArtifacts{
		overview: overview,
		index:    renderIndex(docs),
		graph:    graphBytes,
	}, nil
}

func findMapDefects(docs []Document) []MapDefect {
	var defects []MapDefect
	subjects := make(map[string]struct{}, len(docs))
	seen := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		if _, ok := seen[doc.Subject]; ok {
			defects = append(defects, MapDefect{
				Code:    "DOCS_MAP_DUPLICATE_SUBJECT",
				Path:    doc.Path,
				Message: fmt.Sprintf("duplicate subject %q cannot appear on the map", doc.Subject),
			})
			continue
		}
		seen[doc.Subject] = struct{}{}
		subjects[doc.Subject] = struct{}{}
	}

	for _, doc := range docs {
		if isRoot(doc) {
			continue
		}
		parent := strings.TrimSpace(doc.PartOf)
		if parent == "" {
			defects = append(defects, MapDefect{
				Code:    "DOCS_MAP_ORPHAN",
				Path:    doc.Path,
				Message: fmt.Sprintf("document %q has no part_of parent and is not the root overview", doc.Subject),
			})
			continue
		}
		if _, ok := subjects[parent]; !ok {
			defects = append(defects, MapDefect{
				Code:    "DOCS_MAP_DANGLING_EDGE",
				Path:    doc.Path,
				Message: fmt.Sprintf("part_of names %q, but no document declares that subject", parent),
			})
		}
	}

	for _, doc := range docs {
		successor := strings.TrimSpace(doc.SupersededBy)
		if successor == "" {
			continue
		}
		if _, ok := subjects[successor]; !ok {
			defects = append(defects, MapDefect{
				Code:    "DOCS_MAP_DANGLING_EDGE",
				Path:    doc.Path,
				Message: fmt.Sprintf("superseded_by names %q, but no document declares that subject", successor),
			})
		}
	}

	defects = append(defects, findPartOfCycles(docs, subjects)...)
	return defects
}

func findPartOfCycles(docs []Document, subjects map[string]struct{}) []MapDefect {
	parent := make(map[string]string, len(docs))
	pathBySubject := make(map[string]string, len(docs))
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		pathBySubject[doc.Subject] = doc.Path
		if target := strings.TrimSpace(doc.PartOf); target != "" {
			if _, ok := subjects[target]; ok {
				parent[doc.Subject] = target
			}
		}
	}

	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(parent))
	reported := make(map[string]struct{})
	var defects []MapDefect

	var walk func(subject string, stack []string)
	walk = func(subject string, stack []string) {
		switch state[subject] {
		case done:
			return
		case visiting:
			cycle := append(stack, subject)
			for _, member := range cycleMembers(cycle, subject) {
				if _, ok := reported[member]; ok {
					continue
				}
				reported[member] = struct{}{}
				defects = append(defects, MapDefect{
					Code:    "DOCS_MAP_CYCLE",
					Path:    pathBySubject[member],
					Message: fmt.Sprintf("part_of chain forms a cycle through %q", member),
				})
			}
			return
		}
		state[subject] = visiting
		if next, ok := parent[subject]; ok {
			walk(next, append(stack, subject))
		}
		state[subject] = done
	}

	starts := make([]string, 0, len(parent))
	for subject := range parent {
		starts = append(starts, subject)
	}
	sort.Strings(starts)
	for _, subject := range starts {
		walk(subject, nil)
	}
	return defects
}

func cycleMembers(stack []string, start string) []string {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == start {
			return stack[i:]
		}
	}
	return stack
}

func renderMermaid(docs []Document) string {
	ids := make(map[string]string, len(docs))
	for _, doc := range docs {
		if doc.Subject != "" {
			ids[doc.Subject] = mermaidID(doc.Subject)
		}
	}

	var builder strings.Builder
	builder.WriteString("```mermaid\ngraph TD\n")
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("  %s[%q]\n", ids[doc.Subject], mermaidLabel(doc)))
	}
	for _, doc := range docs {
		parent := strings.TrimSpace(doc.PartOf)
		if parent == "" {
			continue
		}
		if _, ok := ids[parent]; !ok {
			continue
		}
		builder.WriteString(fmt.Sprintf("  %s --> %s\n", ids[parent], ids[doc.Subject]))
	}
	builder.WriteString("```")
	return builder.String()
}

func mermaidLabel(doc Document) string {
	title := scalar(doc.Raw["title"])
	if title == "" {
		title = doc.Subject
	}
	return title
}

func mermaidID(subject string) string {
	var builder strings.Builder
	for _, r := range subject {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	id := builder.String()
	if id == "" {
		return "node"
	}
	return "n_" + id
}

func renderIndex(docs []Document) string {
	var builder strings.Builder
	builder.WriteString(docsMapBeginMarker + "\n")
	builder.WriteString("# Documentation index\n\n")
	builder.WriteString("Generated by `tusker docs map`. Do not edit by hand.\n\n")
	builder.WriteString("| Subject | File | Description | Freshness |\n")
	builder.WriteString("|---|---|---|---|\n")
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		description := scalar(doc.Raw["title"])
		if description == "" {
			description = doc.Subject
		}
		builder.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			doc.Subject, doc.Path, description, freshness(doc)))
	}
	builder.WriteString(docsMapEndMarker + "\n")
	return builder.String()
}

func freshness(doc Document) string {
	if value := scalar(doc.Raw["last_verified"]); value != "" {
		return value
	}
	return "never"
}

func renderGraphJSON(docs []Document) ([]byte, error) {
	graph := mapGraph{Nodes: []mapNode{}, Edges: []mapEdge{}}
	subjects := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		subjects[doc.Subject] = struct{}{}
		graph.Nodes = append(graph.Nodes, mapNode{
			Subject: doc.Subject,
			Kind:    doc.Kind,
			Path:    doc.Path,
			Title:   scalar(doc.Raw["title"]),
			Status:  doc.Status,
		})
	}
	for _, doc := range docs {
		if doc.Subject == "" {
			continue
		}
		if parent := strings.TrimSpace(doc.PartOf); parent != "" {
			graph.Edges = append(graph.Edges, mapEdge{From: doc.Subject, To: parent, Kind: "part_of"})
		}
		for _, target := range doc.Updates {
			graph.Edges = append(graph.Edges, mapEdge{From: doc.Subject, To: target, Kind: "updates"})
		}
		if target := strings.TrimSpace(doc.DecidesFor); target != "" {
			graph.Edges = append(graph.Edges, mapEdge{From: doc.Subject, To: target, Kind: "decides_for"})
		}
		if target := strings.TrimSpace(doc.SupersededBy); target != "" {
			graph.Edges = append(graph.Edges, mapEdge{From: doc.Subject, To: target, Kind: "superseded_by"})
		}
	}
	sort.SliceStable(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		if graph.Edges[i].Kind != graph.Edges[j].Kind {
			return graph.Edges[i].Kind < graph.Edges[j].Kind
		}
		return graph.Edges[i].To < graph.Edges[j].To
	})
	encoded, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func injectRegion(content, body string) (string, error) {
	region := docsMapBeginMarker + "\n" + body + "\n" + docsMapEndMarker
	begin := strings.Index(content, docsMapBeginMarker)
	end := strings.Index(content, docsMapEndMarker)
	if begin < 0 && end < 0 {
		trimmed := strings.TrimRight(content, "\n")
		return trimmed + "\n\n" + region + "\n", nil
	}
	if begin < 0 || end < 0 || end < begin {
		return "", fmt.Errorf("%s: malformed generated region markers", overviewRelPath)
	}
	after := end + len(docsMapEndMarker)
	return content[:begin] + region + content[after:], nil
}
