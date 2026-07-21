package docgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wellFormedCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md",
		"---\ntitle: \"System overview\"\nsubject: overview\nstatus: canonical\n---\n\n# Overview\n\nIntro.\n")
	writeDoc(t, root, "docs/system/cli.md",
		"---\ntitle: \"CLI reference\"\nsubject: cli\npart_of: overview\nstatus: canonical\nlast_verified: \"2026-07-21 @ abc123\"\n---\n\n# CLI\n")
	writeDoc(t, root, ".tusker/specs/knowledge-graph.md",
		"---\ntitle: \"Knowledge graph\"\nsubject: knowledge-graph\npart_of: overview\nstatus: canonical\nupdates:\n  - docs/system/00-overview.md\n---\n\n# KG\n")
	writeDoc(t, root, ".tusker/specs/decisions/kg-grill.md",
		"---\ntitle: \"KG decisions\"\nsubject: kg-grill\npart_of: knowledge-graph\ndecides_for: knowledge-graph\nstatus: canonical\n---\n\n# Grill\n")
	return root
}

func TestMapRendersAllThreeArtifacts(t *testing.T) {
	root := wellFormedCorpus(t)
	if err := WriteDocsMap(root); err != nil {
		t.Fatalf("WriteDocsMap() error = %v", err)
	}

	overview, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(overviewRelPath)))
	if err != nil {
		t.Fatalf("read overview: %v", err)
	}
	overviewText := string(overview)
	if !strings.Contains(overviewText, docsMapBeginMarker) || !strings.Contains(overviewText, docsMapEndMarker) {
		t.Fatalf("overview missing generated region markers:\n%s", overviewText)
	}
	if !strings.Contains(overviewText, "```mermaid") || !strings.Contains(overviewText, "graph TD") {
		t.Fatalf("overview missing mermaid DAG:\n%s", overviewText)
	}
	if !strings.Contains(overviewText, "# Overview") {
		t.Fatalf("generation clobbered prose outside the fence:\n%s", overviewText)
	}
	if !strings.Contains(overviewText, "-->") {
		t.Fatalf("mermaid DAG has no edges:\n%s", overviewText)
	}

	index, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(indexRelPath)))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	indexText := string(index)
	for _, want := range []string{"| Subject | File | Description | Freshness |", "cli", "2026-07-21 @ abc123", "never"} {
		if !strings.Contains(indexText, want) {
			t.Fatalf("index missing %q:\n%s", want, indexText)
		}
	}

	graphBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(graphRelPath)))
	if err != nil {
		t.Fatalf("read graph: %v", err)
	}
	var graph mapGraph
	if err := json.Unmarshal(graphBytes, &graph); err != nil {
		t.Fatalf("graph.json is not valid JSON: %v", err)
	}
	if len(graph.Nodes) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(graph.Nodes))
	}
	var havePartOf, haveUpdates, haveDecidesFor bool
	for _, edge := range graph.Edges {
		switch edge.Kind {
		case "part_of":
			havePartOf = true
		case "updates":
			haveUpdates = true
		case "decides_for":
			haveDecidesFor = true
		}
	}
	if !havePartOf || !haveUpdates || !haveDecidesFor {
		t.Fatalf("graph.json missing expected edge kinds: %#v", graph.Edges)
	}

	// Re-running must be a no-op (deterministic output).
	before := map[string][]byte{overviewRelPath: overview, indexRelPath: index, graphRelPath: graphBytes}
	if err := WriteDocsMap(root); err != nil {
		t.Fatalf("second WriteDocsMap() error = %v", err)
	}
	for rel, want := range before {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("re-read %s: %v", rel, err)
		}
		if string(got) != string(want) {
			t.Fatalf("second run produced a diff in %s", rel)
		}
	}
}

func TestMapRefusesMalformedGraph(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
		code   string
	}{
		{
			name: "orphan",
			mutate: func(t *testing.T, root string) {
				writeDoc(t, root, "docs/system/orphan.md",
					"---\ntitle: \"Orphan\"\nsubject: orphan\nstatus: canonical\n---\n\n# Orphan\n")
			},
			code: "DOCS_MAP_ORPHAN",
		},
		{
			name: "duplicate",
			mutate: func(t *testing.T, root string) {
				writeDoc(t, root, "docs/system/cli-copy.md",
					"---\ntitle: \"CLI again\"\nsubject: cli\npart_of: overview\nstatus: canonical\n---\n\n# Dup\n")
			},
			code: "DOCS_MAP_DUPLICATE_SUBJECT",
		},
		{
			name: "dangling",
			mutate: func(t *testing.T, root string) {
				writeDoc(t, root, "docs/system/child.md",
					"---\ntitle: \"Child\"\nsubject: child\npart_of: nonexistent\nstatus: canonical\n---\n\n# Child\n")
			},
			code: "DOCS_MAP_DANGLING_EDGE",
		},
		{
			name: "cycle",
			mutate: func(t *testing.T, root string) {
				writeDoc(t, root, "docs/system/loop-a.md",
					"---\ntitle: \"A\"\nsubject: loop-a\npart_of: loop-b\nstatus: canonical\n---\n\n# A\n")
				writeDoc(t, root, "docs/system/loop-b.md",
					"---\ntitle: \"B\"\nsubject: loop-b\npart_of: loop-a\nstatus: canonical\n---\n\n# B\n")
			},
			code: "DOCS_MAP_CYCLE",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := wellFormedCorpus(t)
			tc.mutate(t, root)

			err := WriteDocsMap(root)
			if err == nil {
				t.Fatalf("expected refusal for %s", tc.name)
			}
			mapErr, ok := err.(*MapError)
			if !ok {
				t.Fatalf("expected *MapError, got %T: %v", err, err)
			}
			var found bool
			for _, defect := range mapErr.Defects {
				if defect.Code == tc.code {
					found = true
					if defect.Path == "" {
						t.Fatalf("defect %s does not name a file", tc.code)
					}
				}
			}
			if !found {
				t.Fatalf("expected defect %s, got %#v", tc.code, mapErr.Defects)
			}

			// Refusal must write nothing: no INDEX or graph.json created.
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(indexRelPath))); !os.IsNotExist(err) {
				t.Fatalf("INDEX.md written despite refusal")
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(graphRelPath))); !os.IsNotExist(err) {
				t.Fatalf("graph.json written despite refusal")
			}
		})
	}
}
