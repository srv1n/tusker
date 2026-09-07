package docgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowse(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryDoc(t, root, "docs/system/00-overview.md", "---\ntitle: System Overview\nsubject: overview\nstatus: canonical\n---\n# System Overview\n")
	writeDiscoveryDoc(t, root, "docs/system/architecture/00-index.md", "---\ntitle: Architecture\nsubject: architecture\npart_of: overview\nstatus: canonical\n---\n# Architecture\n")
	writeDiscoveryDoc(t, root, "docs/system/zeta.md", "---\nsubject: zeta\npart_of: overview\nread_when: reading zeta\nstatus: canonical\n---\n# Zeta\n")
	writeDiscoveryDoc(t, root, "docs/system/alpha.md", "---\nsubject: alpha\npart_of: overview\nstatus: canonical\n---\n# Alpha\n")

	result, err := Browse(root, "docs/system", 2)
	if err != nil {
		t.Fatalf("Browse() error = %v", err)
	}
	if result.Path != "docs/system/" || len(result.Entries) != 2 || !result.Truncated || result.Omitted != 2 {
		t.Fatalf("Browse() = %#v", result)
	}
	if result.Entries[0].Kind != "folder" || result.Entries[0].Path != "docs/system/architecture/" || result.Entries[0].Summary != "Architecture" {
		t.Fatalf("folder entry = %#v", result.Entries[0])
	}
	if result.Entries[1].Name != "00-overview.md" || result.Entries[1].Subject != "overview" {
		t.Fatalf("file entry = %#v", result.Entries[1])
	}
}

func TestBrowseRejectsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/system"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../system", filepath.Join(root, "docs/system/alias")); err != nil {
		t.Fatal(err)
	}
	_, err := Browse(root, "docs/system/alias", 1)
	if err == nil || !strings.Contains(err.Error(), "symlinked") {
		t.Fatalf("Browse() symlink error = %v", err)
	}
	pathErr, ok := err.(*PathError)
	if !ok || pathErr.Code != "DOC_PATH_SYMLINK" {
		t.Fatalf("Browse() symlink typed error = %#v", err)
	}
}

func TestBrowseReportsMalformedFolderSummary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs/system/architecture/00-index.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not front matter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Browse(root, "docs/system", 1)
	if err == nil || !strings.Contains(err.Error(), "front matter") {
		t.Fatalf("Browse() malformed summary error = %v", err)
	}
	pathErr, ok := err.(*PathError)
	if !ok || pathErr.Code != "DOC_HEADER_MISSING" || pathErr.Path != "docs/system/architecture/00-index.md" {
		t.Fatalf("Browse() malformed summary typed error = %#v", err)
	}
}

func TestBrowseRejectsSymlinkedFolderSummary(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	if err := os.MkdirAll(filepath.Join(root, "docs/system/architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../00-overview.md", filepath.Join(root, "docs/system/architecture/00-index.md")); err != nil {
		t.Fatal(err)
	}
	_, err := Browse(root, "docs/system", 1)
	if err == nil {
		t.Fatal("Browse() accepted a symlinked folder summary")
	}
	pathErr, ok := err.(*PathError)
	if !ok || pathErr.Code != "DOC_PATH_SYMLINK" || pathErr.Path != "docs/system/architecture/00-index.md" {
		t.Fatalf("Browse() symlinked summary error = %#v", err)
	}
}

func TestBrowseCapsLargeDirectory(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxBrowseLimit+25; i++ {
		writeDiscoveryDoc(t, root, filepath.Join("docs/system", "entry-"+strings.Repeat("x", 8)+"-"+fmt.Sprint(i)+".md"), "---\nsubject: entry-"+fmt.Sprint(i)+"\npart_of: overview\nstatus: canonical\n---\n# Entry\n")
	}
	result, err := Browse(root, "docs/system", MaxBrowseLimit+25)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != MaxBrowseLimit || !result.Truncated || result.Limit != MaxBrowseLimit || result.Omitted != 25 {
		t.Fatalf("large Browse() = entries:%d truncated:%t limit:%d omitted:%d", len(result.Entries), result.Truncated, result.Limit, result.Omitted)
	}
}

func TestReadSection(t *testing.T) {
	body := "# Guide\n\n```md\n## Fake\nnot a section\n```\n\n## Setup\n\nStart here.\n\n### Detail\n\nKeep this detail.\n\n## Next\n\nLater.\n"
	section, err := ReadSection(body, "Setup")
	if err != nil {
		t.Fatalf("ReadSection() error = %v", err)
	}
	if section.Heading != "## Setup" || !strings.Contains(section.Body, "Keep this detail.") || strings.Contains(section.Body, "Later.") {
		t.Fatalf("section = %#v", section)
	}
	if _, err := ReadSection(body, "Fake"); err == nil {
		t.Fatal("ReadSection() treated a fenced heading as a section")
	}
	if _, err := ReadSection(body, "## Missing"); err == nil || !strings.Contains(err.Error(), "## Setup") {
		t.Fatalf("missing section error = %v", err)
	}
}

func TestBacklinks(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeDiscoveryDoc(t, root, "docs/system/guide.md", "---\nsubject: guide\npart_of: overview\nstatus: canonical\n---\n# Guide\nSee [[target]].\n")
	writeDiscoveryDoc(t, root, ".tusker/specs/target.md", "---\nsubject: target\npart_of: overview\nstatus: canonical\n---\n# Target\n")
	corpus, _, err := LoadRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	links := Backlinks(corpus, "target")
	if len(links) != 1 || links[0].From != "guide" || links[0].Kind != "link" {
		t.Fatalf("Backlinks() = %#v", links)
	}
}

func TestDocsMetadata(t *testing.T) {
	doc, err := ParseDocHeaders("docs/system/metadata.md", []byte("---\ntitle: Metadata\nsubject: metadata\npart_of: overview\nread_when: changing metadata\nskip_when: changing runtime\nstatus: canonical\n---\n# Metadata\n"))
	if err != nil {
		t.Fatal(err)
	}
	if DocumentTitle(doc) != "Metadata" || doc.Raw["read_when"] != "changing metadata" || doc.PartOf != "overview" {
		t.Fatalf("metadata = %#v", doc)
	}
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "subject list", body: "subject: [metadata]"},
		{name: "title number", body: "title: 7"},
		{name: "keywords mixed list", body: "keywords: [metadata, 7]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseDocHeaders("docs/system/metadata.md", []byte("---\n"+test.body+"\npart_of: overview\nstatus: canonical\n---\n# Metadata\n"))
			parsed, ok := err.(*ParseError)
			if !ok || parsed.Code != "DOC_HEADER_TYPE_INVALID" {
				t.Fatalf("typed header error = %#v", err)
			}
		})
	}
}

func writeDiscoveryDoc(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
