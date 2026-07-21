package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func writeMapDoc(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFlagsStaleDocsMap(t *testing.T) {
	repo := t.TempDir()
	writeMapDoc(t, repo, "docs/system/00-overview.md",
		"---\ntitle: \"Overview\"\nsubject: overview\nstatus: canonical\n---\n\n# Overview\n")
	writeMapDoc(t, repo, "docs/system/cli.md",
		"---\ntitle: \"CLI\"\nsubject: cli\npart_of: overview\nstatus: canonical\n---\n\n# CLI\n")

	// Fresh generation must validate clean.
	if err := docgraph.WriteDocsMap(repo); err != nil {
		t.Fatalf("WriteDocsMap() error = %v", err)
	}
	issues, err := docgraph.CheckDocsMapFresh(repo)
	if err != nil {
		t.Fatalf("CheckDocsMapFresh() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("freshly generated map should validate clean, got %#v", issues)
	}

	// Adding a document without regenerating must go red with a run hint.
	writeMapDoc(t, repo, "docs/system/gates.md",
		"---\ntitle: \"Gates\"\nsubject: gates\npart_of: overview\nstatus: canonical\n---\n\n# Gates\n")
	issues, err = docgraph.CheckDocsMapFresh(repo)
	if err != nil {
		t.Fatalf("CheckDocsMapFresh() error = %v", err)
	}
	var flagged bool
	for _, issue := range issues {
		if issue.Code == "DOCS_MAP_STALE" && strings.Contains(issue.Message, "tusker docs map") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("stale doc map should be flagged with a run hint, got %#v", issues)
	}

	// Regenerating clears the defect.
	if err := docgraph.WriteDocsMap(repo); err != nil {
		t.Fatalf("regenerate WriteDocsMap() error = %v", err)
	}
	issues, err = docgraph.CheckDocsMapFresh(repo)
	if err != nil {
		t.Fatalf("CheckDocsMapFresh() error = %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("regenerated map should validate clean, got %#v", issues)
	}
}
