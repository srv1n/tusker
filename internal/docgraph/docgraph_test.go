package docgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDocHeaders(t *testing.T) {
	content := []byte("---\nsubject: orchestration\nkeywords: [daemon, workers]\npart_of: overview\nupdates: [docs/system/00-overview.md]\nsources: [operator-notes]\n---\n\n# Orchestration\n")
	doc, err := ParseDocHeaders("docs/system/orchestration.md", content)
	if err != nil {
		t.Fatalf("ParseDocHeaders() error = %v", err)
	}
	if doc.Kind != KindCanonical || doc.Subject != "orchestration" {
		t.Fatalf("unexpected document identity: %#v", doc)
	}
	if strings.Join(doc.Keywords, ",") != "daemon,workers" || doc.PartOf != "overview" {
		t.Fatalf("header connections not normalized: %#v", doc)
	}
	if len(doc.Updates) != 1 || doc.Updates[0] != "docs/system/00-overview.md" || len(doc.Sources) != 1 {
		t.Fatalf("edge fields not parsed: %#v", doc)
	}
}

func TestLoadRepositoryBuildsOneCorpusAcrossDocumentKinds(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, ".tusker/specs/example.md", "---\nsubject: example-spec\nkeywords: [example]\npart_of: overview\n---\n# Example\n")
	writeDoc(t, root, ".tusker/specs/decisions/example.md", "---\nsubject: example-decision\nkeywords: [example]\npart_of: example-spec\ndecides_for: .tusker/specs/example.md\n---\n# Decision\n")

	corpus, issues, err := LoadRepository(root)
	if err != nil {
		t.Fatalf("LoadRepository() error = %v", err)
	}
	if len(issues) != 0 || len(corpus.Documents) != 3 {
		t.Fatalf("unexpected corpus: issues=%#v documents=%#v", issues, corpus.Documents)
	}
	kinds := map[Kind]bool{}
	for _, doc := range corpus.Documents {
		kinds[doc.Kind] = true
	}
	if !kinds[KindCanonical] || !kinds[KindSpec] || !kinds[KindDecision] {
		t.Fatalf("corpus did not retain all document kinds: %#v", kinds)
	}
}

func TestHeaderLintNamesDefects(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/duplicate.md", "---\nsubject: overview\nkeywords: [duplicate]\npart_of: overview\n---\n# Duplicate\n")
	writeDoc(t, root, "docs/system/missing.md", "---\ntitle: Missing\n---\n# Missing\n")
	flagged := []string{"guide-v2.md", "notes_new.md", "guide-final.md", "report (1).md"}
	notFlagged := []string{"part-1.md", "step-02.md"}
	for _, filename := range append(append([]string{}, flagged...), notFlagged...) {
		writeDoc(t, root, filepath.Join("docs/system", filename), "---\nsubject: "+filename+"\npart_of: overview\n---\n# Guide\n")
	}

	issues, err := ValidateRepository(root)
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v", err)
	}
	assertIssue(t, issues, "DOC_DUPLICATE_SUBJECT", "docs/system/duplicate.md", "duplicate subject")
	assertIssue(t, issues, "DOC_REQUIRED_FIELD_MISSING", "docs/system/missing.md", `required header field "subject"`)
	assertNoIssue(t, issues, "DOC_REQUIRED_FIELD_MISSING", "docs/system/missing.md", `required header field "keywords"`)
	for _, filename := range flagged {
		assertIssue(t, issues, "DOC_VERSIONED_FILENAME", filepath.ToSlash(filepath.Join("docs/system", filename)), "version-suffixed")
	}
	for _, filename := range notFlagged {
		assertNoIssue(t, issues, "DOC_VERSIONED_FILENAME", filepath.ToSlash(filepath.Join("docs/system", filename)), "version-suffixed")
	}
}

func TestDecisionLogRequiresDecidesFor(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, ".tusker/specs/example.md", "---\nsubject: example-spec\npart_of: overview\n---\n# Example\n")
	writeDoc(t, root, ".tusker/specs/decisions/orphan.md", "---\nsubject: orphan-decision\npart_of: example-spec\n---\n# Decision\n")
	writeDoc(t, root, ".tusker/specs/decisions/linked.md", "---\nsubject: linked-decision\npart_of: example-spec\ndecides_for: .tusker/specs/example.md\n---\n# Decision\n")

	issues, err := ValidateRepository(root)
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v", err)
	}
	assertIssue(t, issues, "DOC_REQUIRED_FIELD_MISSING", ".tusker/specs/decisions/orphan.md", `required header field "decides_for"`)
	assertNoIssue(t, issues, "DOC_REQUIRED_FIELD_MISSING", ".tusker/specs/decisions/linked.md", `required header field "decides_for"`)
}

func TestTombstoneSuccessorRequired(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		code    string
		message string
	}{
		{
			name:    "missing successor",
			body:    "---\nsubject: old-guide\nkeywords: [guide]\npart_of: overview\nstatus: superseded\n---\nUse the new guide.\n",
			code:    "DOC_TOMBSTONE_SUCCESSOR_MISSING",
			message: "must name its successor",
		},
		{
			name:    "unknown successor",
			body:    "---\nsubject: old-guide\nkeywords: [guide]\npart_of: overview\nstatus: superseded\nsuperseded_by: new-guide\n---\nUse the new guide.\n",
			code:    "DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND",
			message: "subject does not exist",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
			writeDoc(t, root, "docs/system/old-guide.md", tc.body)
			issues, err := ValidateRepository(root)
			if err != nil {
				t.Fatalf("ValidateRepository() error = %v", err)
			}
			assertIssue(t, issues, tc.code, "docs/system/old-guide.md", tc.message)
		})
	}
}

func writeDoc(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertIssue(t *testing.T, issues []Issue, code, path, message string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.Path == path && strings.Contains(issue.Message, message) {
			return
		}
	}
	t.Fatalf("missing issue code=%q path=%q message containing %q; issues=%#v", code, path, message, issues)
}

func assertNoIssue(t *testing.T, issues []Issue, code, path, message string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.Path == path && strings.Contains(issue.Message, message) {
			t.Fatalf("unexpected issue code=%q path=%q message containing %q; issues=%#v", code, path, message, issues)
		}
	}
}
