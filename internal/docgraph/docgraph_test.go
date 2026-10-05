package docgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDocHeaders(t *testing.T) {
	content := []byte("---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: orchestration\nkeywords: [daemon, workers]\npart_of: overview\ndescribes: [internal/docgraph, cmd/tusker/docs_cmd.go]\nupdates: [docs/system/00-overview.md]\nsources: [operator-notes]\nlast_verified: '2026-08-01'\n---\n\n# Orchestration\n")
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
	if strings.Join(doc.Describes, ",") != "internal/docgraph,cmd/tusker/docs_cmd.go" || doc.LastVerified != "2026-08-01" {
		t.Fatalf("describes/freshness fields not parsed: %#v", doc)
	}
	if len(doc.Updates) != 1 || doc.Updates[0] != "docs/system/00-overview.md" || len(doc.Sources) != 1 {
		t.Fatalf("edge fields not parsed: %#v", doc)
	}
}

func TestLoadRepositoryBuildsOneCorpusAcrossDocumentKinds(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, ".tusker/specs/example.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: example-spec\nkeywords: [example]\npart_of: overview\n---\n# Example\n")
	writeDoc(t, root, ".tusker/specs/decisions/example.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: example-decision\nkeywords: [example]\npart_of: example-spec\ndecides_for: .tusker/specs/example.md\n---\n# Decision\n")

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
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/duplicate.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\nkeywords: [duplicate]\npart_of: overview\n---\n# Duplicate\n")
	writeDoc(t, root, "docs/system/missing.md", "---\ntitle: Missing\n---\n# Missing\n")
	flagged := []string{"guide-v2.md", "notes_new.md", "guide-final.md", "report (1).md"}
	notFlagged := []string{"part-1.md", "step-02.md"}
	for _, filename := range append(append([]string{}, flagged...), notFlagged...) {
		writeDoc(t, root, filepath.Join("docs/system", filename), "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: "+filename+"\npart_of: overview\n---\n# Guide\n")
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
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDoc(t, root, ".tusker/specs/example.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: example-spec\npart_of: overview\n---\n# Example\n")
	writeDoc(t, root, ".tusker/specs/decisions/orphan.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: orphan-decision\npart_of: example-spec\n---\n# Decision\n")
	writeDoc(t, root, ".tusker/specs/decisions/linked.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: linked-decision\npart_of: example-spec\ndecides_for: .tusker/specs/example.md\n---\n# Decision\n")

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
			body:    "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: old-guide\nkeywords: [guide]\npart_of: overview\nstatus: superseded\n---\nUse the new guide.\n",
			code:    "DOC_TOMBSTONE_SUCCESSOR_MISSING",
			message: "must name its successor",
		},
		{
			name:    "unknown successor",
			body:    "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: old-guide\nkeywords: [guide]\npart_of: overview\nstatus: superseded\nsuperseded_by: new-guide\n---\nUse the new guide.\n",
			code:    "DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND",
			message: "subject does not exist",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
			writeDoc(t, root, "docs/system/old-guide.md", tc.body)
			issues, err := ValidateRepository(root)
			if err != nil {
				t.Fatalf("ValidateRepository() error = %v", err)
			}
			assertIssue(t, issues, tc.code, "docs/system/old-guide.md", tc.message)
		})
	}
}

func TestDescribesRequired(t *testing.T) {
	const head = "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\npart_of: overview\n"
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\ncode_conformance: not_applicable\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/target.md", head+"subject: target\nstatus: current\ncode_conformance: not_applicable\n---\n")
	docs := map[string]string{
		"docs/system/current.md":           head + "subject: current-chapter\nkind: doc\nstatus: current\n---\n",
		"docs/system/legacy.md":            head + "subject: legacy-chapter\nstatus: canonical\n---\n",
		"docs/system/decision.md":          head + "subject: accepted-decision\nkind: decision\ndecides_for: docs/system/target.md\nstatus: accepted\n---\n",
		"docs/system/accepted-proposal.md": head + "subject: accepted-proposal\nkind: proposal\nstatus: accepted\n---\n",
		"docs/system/matches.md":           head + "subject: matches-chapter\nkind: doc\nstatus: current\ncode_conformance: matches\nlast_verified: \"2026-10-05 @ abc1234\"\n---\n",
		"docs/system/proposed.md":          head + "subject: proposed-proposal\nkind: proposal\nstatus: proposed\n---\n",
		"docs/system/superseded.md":        head + "subject: superseded-chapter\nkind: doc\nstatus: superseded\nsuperseded_by: target\n---\n",
		"docs/system/exempt.md":            head + "subject: exempt-chapter\nkind: doc\nstatus: current\ncode_conformance: not_applicable\n---\n",
		"docs/system/described.md":         head + "subject: described-chapter\nkind: doc\nstatus: current\ndescribes: [internal/docgraph]\n---\n",
	}
	for path, body := range docs {
		writeDoc(t, root, path, body)
	}
	issues, err := ValidateRepository(root)
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v", err)
	}
	for _, path := range []string{"docs/system/current.md", "docs/system/legacy.md", "docs/system/decision.md", "docs/system/accepted-proposal.md"} {
		assertIssue(t, issues, "DOC_DESCRIBES_REQUIRED", path, "describes")
	}
	assertIssue(t, issues, "DOC_CONFORMANCE_INVALID", "docs/system/matches.md", `code_conformance "matches" requires a stated verification scope in describes`)
	for _, path := range []string{"docs/system/proposed.md", "docs/system/superseded.md", "docs/system/exempt.md", "docs/system/described.md", "docs/system/00-overview.md"} {
		assertNoIssue(t, issues, "DOC_DESCRIBES_REQUIRED", path, "")
	}
}

func TestDocTouchIntersectsCoarsePathsAndHonorsWaivers(t *testing.T) {
	corpus := Corpus{Documents: []Document{
		{Path: "docs/system/graph.md", Kind: KindCanonical, Subject: "graph", Describes: []string{"internal/docgraph"}},
		{Path: "docs/system/cli.md", Kind: KindCanonical, Subject: "cli", Describes: []string{"cmd/tusker/docs_cmd.go"}},
	}}
	report := CheckDocTouch(corpus, []string{"internal/docgraph/docmap.go", "cmd/tusker/docs_cmd.go"}, map[string]bool{"cli": true})
	if len(report.Implicated) != 2 || len(report.Missing) != 1 || report.Missing[0].Subject != "graph" {
		t.Fatalf("unexpected doc-touch report: %#v", report)
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
