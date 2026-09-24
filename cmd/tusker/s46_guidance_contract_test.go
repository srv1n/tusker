package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func s46GuidanceFile(t *testing.T, repoRoot, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertS46Contains(t *testing.T, rel, body string, wants ...string) {
	t.Helper()
	flat := strings.Join(strings.Fields(body), " ")
	for _, want := range wants {
		if !strings.Contains(flat, want) {
			t.Fatalf("%s lacks %q", rel, want)
		}
	}
}

func assertS46Absent(t *testing.T, rel, body string, rejects ...string) {
	t.Helper()
	for _, reject := range rejects {
		if strings.Contains(body, reject) {
			t.Fatalf("%s carries stale wording %q", rel, reject)
		}
	}
}

// TestS46GuidanceContract covers TSK-T-0060 acceptance A1 and A2: shipped
// guidance and templates describe the implemented portable placement,
// metadata and lifecycle boundaries, and a fresh scaffold answers the four
// cold-reader questions with exact actionable destinations.
func TestS46GuidanceContract(t *testing.T) {
	repoRoot := repoRootForFreshCloneTest(t)

	placement := []string{
		"docs/system/proposals/",
		"docs/system/decisions/",
		"thin pointers only",
	}
	guidancePages := []string{
		"skills/tusker/references/KNOWLEDGE.md",
		"skills/tusker/references/SPECS.md",
		"skills/tusker/references/REPO_ONBOARDING.md",
		"docs/system/00-overview.md",
		"docs/system/knowledge-and-feedback.md",
		"docs/system/cli.md",
		"docs/system/skills.md",
		"README.md",
	}
	for _, rel := range guidancePages {
		body := s46GuidanceFile(t, repoRoot, rel)
		assertS46Contains(t, rel, body, placement...)
	}
	for _, rel := range []string{
		"skills/tusker/references/KNOWLEDGE.md",
		"skills/tusker/references/SPECS.md",
		"skills/tusker/references/REPO_ONBOARDING.md",
	} {
		body := s46GuidanceFile(t, repoRoot, rel)
		assertS46Absent(t, rel, body,
			"proposals in `.tusker/specs/`",
			"Governing contracts belong in `.tusker/specs/`",
			"--kind doc|spec`",
			"--kind spec --vault",
		)
	}

	templates := []string{
		"skills/tusker/assets/templates/project-skill.md",
		"skills/tusker/assets/templates/domain-index.md",
		"skills/tusker/assets/templates/domain-canon.md",
		"skills/tusker/assets/templates/doc.md",
		"skills/tusker/assets/templates/agent-doc.md",
		"skills/tusker/assets/templates/onboard-prompt.md",
	}
	for _, rel := range templates {
		body := s46GuidanceFile(t, repoRoot, rel)
		assertS46Contains(t, rel, body, "docs/system/proposals/", "docs/system/decisions/")
	}

	// Implemented scaffold boundaries: kind-specific lifecycle separate from
	// code conformance, proposals carry updates, decisions carry decides_for.
	docScaffold := docsScaffoldPortable("scaffold-doc", docgraph.KindDoc, "overview", "")
	assertS46Contains(t, "doc scaffold", docScaffold, "kind: doc", "status: current")
	proposalScaffold := docsScaffoldPortable("scaffold-cap", docgraph.KindProposal, "overview", "")
	assertS46Contains(t, "proposal scaffold", proposalScaffold, "kind: proposal", "status: proposed", "updates: []")
	decisionScaffold := docsScaffoldPortable("scaffold-model", docgraph.KindDecision, "overview", "scaffold-cap")
	assertS46Contains(t, "decision scaffold", decisionScaffold, "kind: decision", "decides_for: scaffold-cap")
	for _, scaffold := range []string{docScaffold, proposalScaffold, decisionScaffold} {
		assertS46Contains(t, "scaffold", scaffold, "code_conformance: unverified")
	}

	// A fresh scaffold answers the four cold-reader questions with exact
	// actionable destinations and passes repository validation.
	fresh := t.TempDir()
	writeDocsFixture(t, fresh, "docs/system/00-overview.md",
		"---\nsubject: overview\nkind: doc\nstatus: current\n---\n\n# Overview\n")
	writeDocsFixture(t, fresh, "docs/system/domains/billing/00-index.md",
		"---\nsubject: billing\nkind: doc\nstatus: current\npart_of: overview\n---\n\n# Billing\n")
	writeDocsFixture(t, fresh, "docs/system/domains/billing/billing-overview.md",
		"---\nsubject: billing-overview\nkind: doc\nstatus: current\npart_of: billing\n---\n\n# Billing overview\n")
	writeDocsFixture(t, fresh, "docs/system/proposals/billing-cap.md",
		"---\nsubject: billing-cap\nkind: proposal\nstatus: proposed\npart_of: overview\nupdates: [billing-overview]\n---\n\n# Billing cap\n")
	writeDocsFixture(t, fresh, "docs/system/decisions/billing-model.md",
		"---\nsubject: billing-model\nkind: decision\nstatus: proposed\npart_of: overview\ndecides_for: billing-cap\n---\n\n# Billing model\n")
	issues, err := docgraph.ValidateRepository(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("fresh portable scaffold should validate: %#v", issues)
	}
	corpus, _, err := docgraph.LoadRepository(fresh)
	if err != nil {
		t.Fatal(err)
	}
	bySubject := map[string]string{}
	for _, doc := range corpus.Documents {
		bySubject[doc.Subject] = filepath.ToSlash(doc.Path)
	}
	for subject, want := range map[string]string{
		"billing-overview": "docs/system/domains/billing/billing-overview.md",
		"billing-cap":      "docs/system/proposals/billing-cap.md",
		"billing-model":    "docs/system/decisions/billing-model.md",
	} {
		if got := bySubject[subject]; got != want {
			t.Fatalf("subject %s resolves to %q, want %q", subject, got, want)
		}
	}
}
