package docgraph

import (
	"testing"
)

// TestS46DocumentContract covers TSK-T-0055 A1: explicit doc/proposal/decision
// kinds with kind-specific lifecycles and independent code conformance, plus
// the legacy compatibility surface that must never imply conformance.
func TestS46DocumentContract(t *testing.T) {
	t.Run("explicit kind beats path", func(t *testing.T) {
		doc, err := ParseDocHeaders("docs/system/guide.md", []byte("---\nkind: decision\nsubject: guide-choice\npart_of: overview\ndecides_for: some-spec\nstatus: accepted\n---\n# Guide\n"))
		if err != nil {
			t.Fatalf("ParseDocHeaders() error = %v", err)
		}
		if doc.Kind != KindDecision || doc.KindSource != KindSourceExplicit {
			t.Fatalf("explicit kind lost: kind=%q source=%q", doc.Kind, doc.KindSource)
		}
	})

	t.Run("spec alias normalizes to proposal", func(t *testing.T) {
		doc, err := ParseDocHeaders(".tusker/specs/change.md", []byte("---\nkind: spec\nsubject: change\npart_of: overview\nstatus: proposed\n---\n# Change\n"))
		if err != nil {
			t.Fatalf("ParseDocHeaders() error = %v", err)
		}
		if doc.Kind != KindProposal || doc.KindSource != KindSourceExplicit {
			t.Fatalf("spec alias not normalized: kind=%q source=%q", doc.Kind, doc.KindSource)
		}
	})

	t.Run("unknown kind is rejected but corpus still loads", func(t *testing.T) {
		doc, err := ParseDocHeaders("docs/system/guide.md", []byte("---\nkind: essay\nsubject: guide\npart_of: overview\nstatus: current\n---\n# Guide\n"))
		if err != nil {
			t.Fatalf("ParseDocHeaders() must keep the inferred kind so the corpus loads: %v", err)
		}
		if doc.Kind != KindCanonical {
			t.Fatalf("unknown kind should keep inferred kind, got %q", doc.Kind)
		}
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
		writeDoc(t, root, "docs/system/guide.md", "---\nkind: essay\nsubject: guide\npart_of: overview\nstatus: current\n---\n# Guide\n")
		issues, err := ValidateRepository(root)
		if err != nil {
			t.Fatalf("ValidateRepository() error = %v", err)
		}
		assertIssue(t, issues, "DOC_KIND_INVALID", "docs/system/guide.md", "unknown document kind")
	})

	t.Run("lifecycle is kind specific", func(t *testing.T) {
		valid := []struct{ kind, status string }{
			{"doc", "current"},
			{"doc", "superseded"},
			{"proposal", "proposed"},
			{"proposal", "accepted"},
			{"proposal", "implemented"},
			{"decision", "proposed"},
			{"decision", "accepted"},
		}
		for _, tc := range valid {
			if !ValidLifecycle(Kind(tc.kind), KindSourceExplicit, tc.status) {
				t.Fatalf("ValidLifecycle(%q, explicit, %q) = false, want true", tc.kind, tc.status)
			}
		}
		invalid := []struct{ kind, status string }{
			{"doc", "proposed"},
			{"doc", "accepted"},
			{"doc", "implemented"},
			{"proposal", "current"},
			{"decision", "current"},
			{"decision", "implemented"},
		}
		for _, tc := range invalid {
			if ValidLifecycle(Kind(tc.kind), KindSourceExplicit, tc.status) {
				t.Fatalf("ValidLifecycle(%q, explicit, %q) = true, want false", tc.kind, tc.status)
			}
		}
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
		writeDoc(t, root, "docs/system/guide.md", "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: proposed\n---\n# Guide\n")
		issues, err := ValidateRepository(root)
		if err != nil {
			t.Fatalf("ValidateRepository() error = %v", err)
		}
		assertIssue(t, issues, "DOC_LIFECYCLE_INVALID", "docs/system/guide.md", "not valid for")
	})

	t.Run("legacy canonical is accepted but never translated", func(t *testing.T) {
		if !ValidLifecycle(KindSpec, KindSourceLegacy, "canonical") {
			t.Fatal("legacy canonical status must remain accepted for compatibility")
		}
		root := t.TempDir()
		writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
		writeDoc(t, root, ".tusker/specs/legacy.md", "---\nsubject: legacy-spec\npart_of: overview\nstatus: canonical\n---\n# Legacy\n")
		corpus, _, err := LoadRepository(root)
		if err != nil {
			t.Fatalf("LoadRepository() error = %v", err)
		}
		for _, doc := range corpus.Documents {
			if doc.KindSource != KindSourceLegacy {
				t.Fatalf("legacy document without explicit kind must stay legacy-sourced: %#v", doc)
			}
			if doc.Status == "accepted" || doc.Status == "implemented" || doc.CodeConformance == "matches" {
				t.Fatalf("legacy canonical must not translate into approval or conformance: %#v", doc)
			}
		}
		diags := MigrationDiagnostics(corpus)
		assertIssue(t, diags, "DOC_LIFECYCLE_LEGACY", ".tusker/specs/legacy.md", "legacy lifecycle")
		assertIssue(t, diags, "DOC_KIND_LEGACY", ".tusker/specs/legacy.md", "inferred from the file path")
	})

	t.Run("matches requires stamp and scope", func(t *testing.T) {
		cases := []struct {
			name      string
			headers   string
			wantIssue bool
		}{
			{
				name:      "unstamped matches rejected",
				headers:   "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: current\ncode_conformance: matches\ndescribes: [internal/docgraph]\n---\n",
				wantIssue: true,
			},
			{
				name:      "unscoped matches rejected",
				headers:   "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: current\ncode_conformance: matches\nlast_verified: '2026-09-01 @ abc1234'\n---\n",
				wantIssue: true,
			},
			{
				name:      "unknown conformance rejected",
				headers:   "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: current\ncode_conformance: proven\ndescribes: [internal/docgraph]\nlast_verified: '2026-09-01 @ abc1234'\n---\n",
				wantIssue: true,
			},
			{
				name:      "stamped scoped matches accepted",
				headers:   "---\nkind: doc\nsubject: guide\npart_of: overview\nstatus: current\ncode_conformance: matches\ndescribes: [internal/docgraph]\nlast_verified: '2026-09-01 @ abc1234'\n---\n",
				wantIssue: false,
			},
			{
				name:      "accepted proposal stays unverified without implying drift",
				headers:   "---\nkind: proposal\nsubject: change\npart_of: overview\nstatus: accepted\ndescribes: [internal/docgraph]\n---\n",
				wantIssue: false,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				root := t.TempDir()
				writeDoc(t, root, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
				writeDoc(t, root, "docs/system/guide.md", tc.headers+"# Guide\n")
				issues, err := ValidateRepository(root)
				if err != nil {
					t.Fatalf("ValidateRepository() error = %v", err)
				}
				if tc.wantIssue {
					assertIssue(t, issues, "DOC_CONFORMANCE_INVALID", "docs/system/guide.md", "code_conformance")
				} else {
					assertNoIssue(t, issues, "DOC_CONFORMANCE_INVALID", "docs/system/guide.md", "code_conformance")
				}
			})
		}
	})
}
