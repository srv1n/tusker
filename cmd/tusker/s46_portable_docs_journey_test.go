package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	"tusker/internal/docgraph"
)

// TestS46PortableDocsJourney covers TSK-T-0062 A1/A3 for the fresh and
// migrated journeys: initialize, create domain/chapter/proposal/decision,
// link them, create a task with governing refs, read its packet, inspect CLI
// search/browse/backlinks, then read/edit via the actual docgraph API. The
// migrated half previews a legacy inventory read-only, applies a reviewed
// mapping, reopens old references, and proves packet plus CAS saves; injected
// stale-input and interrupted-apply cases must preserve data.
func TestS46PortableDocsJourney(t *testing.T) {
	t.Run("fresh journey reaches CLI packet and API consumers", func(t *testing.T) {
		repo := t.TempDir()
		vault := filepath.Join(repo, ".tusker")
		mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Journey policy.", "v7": "true"}, newV7Epic)
		writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")

		newDoc := func(subject, kind, domain string, extra Args) string {
			t.Helper()
			args := Args{"vault": vault, "_pos": subject, "_pos0": subject, "json": "true"}
			if kind != "" {
				args["kind"] = kind
			}
			if domain != "" {
				args["domain"] = domain
			}
			for key, value := range extra {
				args[key] = value
			}
			output := captureStdout(t, func() {
				if err := docsNewCmd(args); err != nil {
					t.Fatalf("docs new %s: %v", subject, err)
				}
			})
			var created struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			}
			if err := json.Unmarshal([]byte(output), &created); err != nil {
				t.Fatalf("docs new JSON: %v\n%s", err, output)
			}
			return created.Path
		}

		if path := newDoc("billing-overview", "", "billing", Args{"index": "true"}); path != "docs/system/domains/billing/00-index.md" {
			t.Fatalf("domain index path = %q", path)
		}
		if path := newDoc("invoicing", "", "billing", nil); path != "docs/system/domains/billing/invoicing.md" {
			t.Fatalf("domain chapter path = %q", path)
		}
		if path := newDoc("checkout-flow", "proposal", "", nil); path != "docs/system/proposals/checkout-flow.md" {
			t.Fatalf("proposal path = %q", path)
		}
		if path := newDoc("record-choice", "decision", "", Args{"decides-for": "checkout-flow"}); path != "docs/system/decisions/record-choice.md" {
			t.Fatalf("decision path = %q", path)
		}

		// Link the fresh tree with ordinary relative Markdown links so the
		// CLI, packet, API, and tool-independent readers all see one corpus.
		appendDoc(t, repo, "docs/system/00-overview.md", "\n- [Billing](domains/billing/00-index.md)\n- [Checkout](proposals/checkout-flow.md)\n")
		appendDoc(t, repo, "docs/system/domains/billing/00-index.md", "\n- [Invoicing](invoicing.md)\n")
		appendDoc(t, repo, "docs/system/domains/billing/invoicing.md", "\nSee [Checkout](../../proposals/checkout-flow.md).\n")
		appendDoc(t, repo, "docs/system/proposals/checkout-flow.md", "\nSee [Record choice](../decisions/record-choice.md).\n")

		// A task with governing refs resolves both subjects and carries them
		// into the agent packet at their portable paths.
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Checkout rollout",
			"domains": "billing", "spec-refs": "checkout-flow,record-choice"}, newV7Task)
		task, err := resolveV7Note(vault, "APP-T-0001", "task")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"docs/system/proposals/checkout-flow.md", "docs/system/decisions/record-choice.md"} {
			if !containsString(automationPlanRequiredReads(vault, task), want) {
				t.Fatalf("required reads omit %s", want)
			}
		}
		packet := captureStdout(t, func() {
			if err := packetV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "force": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		for _, want := range []string{"docs/system/proposals/checkout-flow.md", "docs/system/decisions/record-choice.md"} {
			if !strings.Contains(packet, want) {
				t.Fatalf("packet missing governing path %s", want)
			}
		}

		// CLI search, read, browse, and backlinks agree on the same corpus.
		found := captureStdout(t, func() {
			if err := docsFindCmd(Args{"vault": vault, "_pos": "checkout", "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(found, "docs/system/proposals/checkout-flow.md") {
			t.Fatalf("docs find missed the proposal:\n%s", found)
		}
		read := captureStdout(t, func() {
			if err := docsReadCmd(Args{"vault": vault, "_pos": "checkout-flow", "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(read, "Record choice") {
			t.Fatalf("docs read missed the linked decision reference:\n%s", read)
		}
		browsed := captureStdout(t, func() {
			if err := docsBrowseCmd(Args{"vault": vault, "_pos": "docs/system/proposals", "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(browsed, "checkout-flow.md") {
			t.Fatalf("docs browse missed the proposal:\n%s", browsed)
		}
		backlinks := captureStdout(t, func() {
			if err := docsBacklinksCmd(Args{"vault": vault, "_pos": "checkout-flow", "json": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(backlinks, "invoicing") {
			t.Fatalf("docs backlinks missed the chapter link:\n%s", backlinks)
		}

		checked := captureStdout(t, func() {
			if err := docsCheckCmd(Args{"vault": vault, "json": "true"}); err != nil {
				t.Fatalf("docs check: %v", err)
			}
		})
		if !strings.Contains(checked, `"valid":true`) {
			t.Fatalf("fresh corpus is not valid:\n%s", checked)
		}

		// Regenerate maps once; the packet consumer above already ran, and
		// the API consumer below reads the regenerated graph location.
		mapped := captureStdout(t, func() {
			if err := docsMapCmd(Args{"vault": vault, "json": "true"}); err != nil {
				t.Fatalf("docs map: %v", err)
			}
		})
		if !strings.Contains(mapped, `"ok":true`) {
			t.Fatalf("docs map did not report ok:\n%s", mapped)
		}
		for _, rel := range []string{".tusker/_generated/docs/INDEX.md", ".tusker/_generated/docs/graph.json"} {
			if !fileExists(filepath.Join(repo, filepath.FromSlash(rel))) {
				t.Fatalf("map regeneration missed %s", rel)
			}
		}

		// The actual docgraph HTTP API reads and edits the journey corpus
		// with optimistic-concurrency protection: a good save lands and a
		// stale base_rev is refused without touching the file.
		server := newServeFixture(t)
		s46CopyDocsTree(t, repo, server.repoRoot)
		var detail serveDocgraphDetail
		serveDecode(t, server, "/api/docgraph/doc?project=app&subject=checkout-flow", &detail)
		if detail.Subject != "checkout-flow" || detail.Path != "docs/system/proposals/checkout-flow.md" {
			t.Fatalf("API detail = %#v", detail)
		}
		if detail.Rev == "" {
			t.Fatal("API detail returned an empty rev")
		}
		before := string(mustReadFile(t, filepath.Join(server.repoRoot, "docs/system/proposals/checkout-flow.md")))
		code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=checkout-flow",
			mustJSON(t, map[string]any{"base_rev": detail.Rev, "body": "# Checkout flow\n\nEdited through the journey API.\n"}))
		if code != http.StatusOK {
			t.Fatalf("API save status = %d: %s", code, raw)
		}
		var saved serveDocgraphSaveResponse
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatalf("API save decode: %v\n%s", err, raw)
		}
		if saved.Rev == detail.Rev || !strings.Contains(saved.Body, "journey API") {
			t.Fatalf("API save did not advance the revision: %#v", saved)
		}
		if code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=checkout-flow",
			mustJSON(t, map[string]any{"base_rev": detail.Rev, "body": "# Stale\n"})); code != http.StatusConflict {
			t.Fatalf("stale API save status = %d, want 409: %s", code, raw)
		}
		if got := string(mustReadFile(t, filepath.Join(server.repoRoot, "docs/system/proposals/checkout-flow.md"))); !strings.Contains(got, "journey API") || strings.Contains(got, "# Stale") {
			t.Fatalf("stale save mutated the file (before %d bytes, after %d bytes)", len(before), len(got))
		}
	})

	t.Run("migrated journey preserves data across stale and interrupted apply", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "Migration policy.", "v7": "true"}, newV7Epic)

		// Preview is read-only and fingerprinted before any write.
		before := s46MigrationState(t, repo)
		preview := captureStdout(t, func() {
			if err := docsAdoptCmd(Args{"vault": vault, "migration": "true", "json": "true"}); err != nil {
				t.Fatalf("migration preview: %v", err)
			}
		})
		var table struct {
			Fingerprint string              `json:"fingerprint"`
			Proposals   []docsAdoptProposal `json:"proposals"`
		}
		if err := json.Unmarshal([]byte(preview), &table); err != nil {
			t.Fatalf("preview JSON: %v\n%s", err, preview)
		}
		if table.Fingerprint == "" || len(table.Proposals) == 0 {
			t.Fatalf("preview is missing fingerprint or rows:\n%s", preview)
		}
		s46AssertTreeUnchanged(t, before, s46MigrationState(t, repo))

		// Apply the reviewed mapping, then reopen the old reference at its
		// new portable home through CLI, packet, and API consumers.
		tablePath := s46WriteMigrationTable(t, repo, s46MigrationProposals(t, repo))
		if err := s46ApproveMigration(t, vault, tablePath); err != nil {
			t.Fatal(err)
		}
		corpus, _, err := docgraph.LoadRepository(repo)
		if err != nil {
			t.Fatal(err)
		}
		current, ok := docgraph.ResolveCurrentReference(corpus, ".tusker/specs/flow-plan.md")
		if !ok || current.CanonicalRef != "docs/system/proposals/flow-plan.md" {
			t.Fatalf("legacy reference does not reopen at the portable target: %#v ok=%v", current, ok)
		}
		legacy := captureStdout(t, func() {
			if err := docsReadCmd(Args{"vault": vault, "_pos": ".tusker/specs/flow-plan.md", "json": "true"}); err != nil {
				t.Fatalf("docs read legacy path: %v", err)
			}
		})
		if !strings.Contains(legacy, "docs/system/proposals/flow-plan.md") || !strings.Contains(legacy, "flow-details.md") {
			t.Fatalf("legacy read missed the forward or repaired link:\n%s", legacy)
		}
		mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Migrated rollout",
			"spec-refs": "flow-plan"}, newV7Task)
		packet := captureStdout(t, func() {
			if err := packetV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "for": "agent", "force": "true"}); err != nil {
				t.Fatal(err)
			}
		})
		if !strings.Contains(packet, "docs/system/proposals/flow-plan.md") {
			t.Fatalf("packet still routes the migrated subject at a legacy path:\n%s", packet)
		}
		server := newServeFixture(t)
		s46CopyDocsTree(t, repo, server.repoRoot)
		rev := docRev(t, server, "flow-plan")
		if code, raw := servePut(t, server, "/api/docgraph/doc?project=app&subject=flow-plan",
			mustJSON(t, map[string]any{"base_rev": rev, "body": "# Flow plan\n\nSaved with CAS after migration.\n"})); code != http.StatusOK {
			t.Fatalf("migrated CAS save status = %d: %s", code, raw)
		}

		// Stale input is refused by path and the target is never created.
		staleRepo, staleVault := s46MigrationRepo(t)
		staleTable := s46WriteMigrationTable(t, staleRepo, s46MigrationProposals(t, staleRepo))
		planPath := filepath.Join(staleRepo, ".tusker/specs/flow-plan.md")
		raw, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(planPath, append(raw, []byte("\nStale edit.\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		err = s46ApproveMigration(t, staleVault, staleTable)
		if err == nil || !strings.Contains(err.Error(), "changed after review") || !strings.Contains(err.Error(), ".tusker/specs/flow-plan.md") {
			t.Fatalf("stale approval error = %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(staleRepo, "docs/system/proposals/flow-plan.md")); !os.IsNotExist(statErr) {
			t.Fatal("stale approval wrote the target")
		}

		// An interrupted apply resumes to completion without losing the
		// already-applied row or writing the pending one twice.
		recoveryRepo, recoveryVault := s46RecoveryRepo(t)
		recoveryTable := s46WriteMigrationTable(t, recoveryRepo, s46MigrationProposals(t, recoveryRepo))
		t.Setenv("TUSKER_DOCS_ADOPT_FAIL_AFTER", "1")
		err = s46ApproveMigration(t, recoveryVault, recoveryTable)
		if err == nil || !strings.Contains(err.Error(), "injected docs adopt failure") {
			t.Fatalf("injected failure error = %v", err)
		}
		journal, err := loadDocsAdoptJournal(docsAdoptJournalPath(recoveryRepo, s46TableFingerprint(t, recoveryTable)))
		if err != nil || journal == nil || journal.Complete {
			t.Fatalf("journal after partial failure = %#v err=%v", journal, err)
		}
		t.Setenv("TUSKER_DOCS_ADOPT_FAIL_AFTER", "")
		if err := s46ApproveMigration(t, recoveryVault, recoveryTable); err != nil {
			t.Fatalf("resume: %v", err)
		}
		for _, rel := range []string{"docs/system/proposals/spec-a.md", "docs/system/proposals/spec-b.md"} {
			if _, statErr := os.Stat(filepath.Join(recoveryRepo, filepath.FromSlash(rel))); statErr != nil {
				t.Fatalf("resumed apply missed %s: %v", rel, statErr)
			}
		}
	})
}

func appendDoc(t *testing.T, repo, relative, suffix string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(relative))
	raw, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, append(raw, []byte(suffix)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func s46CopyDocsTree(t *testing.T, srcRepo, dstRepo string) {
	t.Helper()
	src := filepath.Join(srcRepo, "docs")
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstRepo, "docs", rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, raw, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestS46PortableDocsWithoutTusker covers TSK-T-0062 A2: a disposable copy of
// the migrated docs tree without .tusker stays navigable through ordinary
// relative links, assets, and anchors, and representative overview, domain,
// proposal, and decision pages render through the existing Markdown renderer
// with no Tusker runtime and no generated graph files.
func TestS46PortableDocsWithoutTusker(t *testing.T) {
	repo := t.TempDir()
	writeTestDoc(t, repo, "docs/system/00-overview.md", strings.Join([]string{
		"---",
		"kind: doc",
		"subject: overview",
		"status: current",
		"---",
		"# System overview",
		"",
		"- [Billing](domains/billing/00-index.md)",
		"- [Checkout](proposals/checkout-flow.md)",
		"",
	}, "\n"))
	writeTestDoc(t, repo, "docs/system/domains/billing/00-index.md", strings.Join([]string{
		"---",
		"kind: doc",
		"subject: billing-index",
		"part_of: overview",
		"status: current",
		"---",
		"# Billing",
		"",
		"- [Invoicing](invoicing.md)",
		"- [Overview](../../00-overview.md)",
		"",
	}, "\n"))
	writeTestDoc(t, repo, "docs/system/domains/billing/invoicing.md", strings.Join([]string{
		"---",
		"kind: doc",
		"subject: invoicing",
		"part_of: billing-index",
		"status: current",
		"---",
		"# Invoicing",
		"",
		"See [Checkout](../../proposals/checkout-flow.md), its [lifecycle](../../proposals/checkout-flow.md#lifecycle), and the [Billing index](00-index.md).",
		"",
	}, "\n"))
	writeTestDoc(t, repo, "docs/system/proposals/checkout-flow.md", strings.Join([]string{
		"---",
		"kind: proposal",
		"subject: checkout-flow",
		"part_of: overview",
		"status: proposed",
		"updates:",
		"  - docs/system/domains/billing/invoicing.md",
		"---",
		"# Checkout flow",
		"",
		"## Lifecycle",
		"",
		"Proposed change, not yet accepted.",
		"",
		"See [Record choice](../decisions/record-choice.md) and the billing chart below.",
		"",
		"![Billing chart](../domains/billing/assets/chart.png)",
		"",
	}, "\n"))
	writeTestDoc(t, repo, "docs/system/decisions/record-choice.md", strings.Join([]string{
		"---",
		"kind: decision",
		"subject: record-choice",
		"part_of: overview",
		"decides_for: checkout-flow",
		"status: accepted",
		"---",
		"# Record choice",
		"",
		"Decided for [Checkout](../proposals/checkout-flow.md).",
		"",
		"## Rationale",
		"",
		"Settled by review; see the [top](#rationale) again.",
		"",
	}, "\n"))
	chart := filepath.Join(repo, filepath.FromSlash("docs/system/domains/billing/assets/chart.png"))
	if err := os.MkdirAll(filepath.Dir(chart), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chart, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Disposable copy without .tusker and without generated outputs.
	disposable := t.TempDir()
	s46CopyDocsTree(t, repo, disposable)
	if _, err := os.Stat(filepath.Join(disposable, ".tusker")); !os.IsNotExist(err) {
		t.Fatal("disposable copy carries a .tusker directory")
	}
	if err := filepath.WalkDir(disposable, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == "_generated" || entry.Name() == ".tusker") {
			t.Fatalf("generated/runtime directory ships with portable docs: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	mdLink := regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)\)`)
	checked := 0
	err := filepath.WalkDir(filepath.Join(disposable, "docs"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)
		if strings.Contains(body, ".tusker/") || strings.Contains(body, "_generated/") {
			t.Fatalf("%s depends on Tusker runtime paths", path)
		}
		dir := filepath.Dir(path)
		for _, match := range mdLink.FindAllStringSubmatch(body, -1) {
			target := match[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if strings.HasPrefix(target, "/") {
				t.Fatalf("%s uses a root-absolute link %q that breaks in a portable copy", path, target)
			}
			filePart, fragment, _ := strings.Cut(target, "#")
			resolved := path
			if filePart != "" {
				resolved = filepath.Join(dir, filepath.FromSlash(filePart))
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Fatalf("%s links to missing file %q", path, target)
			}
			if fragment != "" {
				anchorBody, err := os.ReadFile(resolved)
				if err != nil {
					return err
				}
				if !s46HeadingAnchorExists(string(anchorBody), fragment) {
					t.Fatalf("%s links to missing anchor %q", path, target)
				}
			}
			checked++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 8 {
		t.Fatalf("only %d portable links verified, want at least 8", checked)
	}

	// Representative pages render through the existing Markdown renderer.
	rendered := map[string]string{}
	for _, rel := range []string{
		"docs/system/00-overview.md",
		"docs/system/domains/billing/00-index.md",
		"docs/system/domains/billing/invoicing.md",
		"docs/system/proposals/checkout-flow.md",
		"docs/system/decisions/record-choice.md",
	} {
		raw, err := os.ReadFile(filepath.Join(disposable, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		var html bytes.Buffer
		if err := goldmark.New().Convert(s46StripFrontMatter(raw), &html); err != nil {
			t.Fatalf("render %s: %v", rel, err)
		}
		rendered[rel] = html.String()
	}
	expectHTML := map[string][]string{
		"docs/system/00-overview.md": {
			"System overview", "domains/billing/00-index.md", "proposals/checkout-flow.md",
		},
		"docs/system/domains/billing/00-index.md": {
			"Invoicing", "invoicing.md", "../../00-overview.md",
		},
		"docs/system/domains/billing/invoicing.md": {
			"Invoicing", "checkout-flow.md#lifecycle", "00-index.md",
		},
		"docs/system/proposals/checkout-flow.md": {
			"Lifecycle", "../decisions/record-choice.md", "chart.png",
		},
		"docs/system/decisions/record-choice.md": {
			"Rationale", "../proposals/checkout-flow.md", "#rationale",
		},
	}
	for rel, wants := range expectHTML {
		for _, want := range wants {
			if !strings.Contains(rendered[rel], want) {
				t.Fatalf("rendered %s omits %q:\n%s", rel, want, rendered[rel])
			}
		}
	}
}

func s46StripFrontMatter(raw []byte) []byte {
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) == 0 || string(bytes.TrimSpace(lines[0])) != "---" {
		return raw
	}
	for i := 1; i < len(lines); i++ {
		if string(bytes.TrimSpace(lines[i])) == "---" {
			return bytes.Join(lines[i+1:], []byte("\n"))
		}
	}
	return raw
}

func s46HeadingAnchorExists(body, fragment string) bool {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		var slug strings.Builder
		for _, r := range strings.ToLower(text) {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
				slug.WriteRune(r)
			case r == ' ' || r == '-':
				slug.WriteRune('-')
			}
		}
		if slug.String() == fragment {
			return true
		}
	}
	return false
}
