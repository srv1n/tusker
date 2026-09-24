package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func s46MigrationRepo(t *testing.T) (repo, vault string) {
	t.Helper()
	repo = t.TempDir()
	vault = filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, ".tusker/specs/flow-plan.md", strings.Join([]string{
		"---",
		"subject: flow-plan",
		"part_of: overview",
		"status: canonical",
		"---",
		"# Flow plan",
		"",
		"See [details](./flow-details.md) and [external](https://example.com/x) and [anchor](#lifecycle).",
		"",
	}, "\n"))
	writeTestDoc(t, repo, ".tusker/specs/flow-details.md", strings.Join([]string{
		"---",
		"subject: flow-details",
		"part_of: overview",
		"status: canonical",
		"---",
		"# Flow details",
		"",
	}, "\n"))
	writeTestDoc(t, repo, ".tusker/specs/decisions/flow-choice.md", strings.Join([]string{
		"---",
		"subject: flow-choice",
		"part_of: flow-plan",
		"decides_for: flow-plan",
		"status: canonical",
		"---",
		"# Flow choice",
		"",
	}, "\n"))
	writeTestDoc(t, repo, ".tusker/knowledge/domains/billing/00-index.md", strings.Join([]string{
		"---",
		"subject: billing-overview",
		"part_of: overview",
		"---",
		"# Billing",
		"",
	}, "\n"))
	return repo, vault
}

func s46MigrationOriginals(t *testing.T, repo string) map[string]string {
	t.Helper()
	originals := map[string]string{}
	for _, rel := range []string{
		".tusker/specs/flow-plan.md",
		".tusker/specs/flow-details.md",
		".tusker/specs/decisions/flow-choice.md",
		".tusker/knowledge/domains/billing/00-index.md",
	} {
		originals[rel] = string(mustReadFile(t, filepath.Join(repo, filepath.FromSlash(rel))))
	}
	return originals
}

func s46WriteMigrationTable(t *testing.T, repo string, proposals []docsAdoptProposal) string {
	t.Helper()
	table := docsAdoptTable{Schema: docsAdoptTableSchema, ApprovedBy: "human:test", Proposals: proposals}
	table.Fingerprint = docsAdoptTableFingerprint(proposals)
	raw, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	tablePath := filepath.Join(repo, "adoption.json")
	if err := os.WriteFile(tablePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return tablePath
}

func s46ApproveMigration(t *testing.T, vault, tablePath string) error {
	t.Helper()
	return docsAdoptCmd(Args{"vault": vault, "table": tablePath, "approve": "true", "by": "human:test"})
}

func s46MigrationProposals(t *testing.T, repo string) []docsAdoptProposal {
	t.Helper()
	corpus, _, err := docgraph.LoadRepository(repo)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := inventoryDocsMigration(repo, corpus)
	if err != nil {
		t.Fatal(err)
	}
	return proposals
}

func s46TreeFiles(t *testing.T, repo string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(repo, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = docsAdoptBytesFingerprint(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// s46MigrationState snapshots the idempotency surface: the portable tree,
// the legacy sources, and the recovery journal. The append-only audit trail
// under .tusker/events records every approval by design and is excluded.
func s46MigrationState(t *testing.T, repo string) map[string]string {
	t.Helper()
	state := map[string]string{}
	for path, fp := range s46TreeFiles(t, repo) {
		if strings.HasPrefix(path, ".tusker/events/") {
			continue
		}
		state[path] = fp
	}
	return state
}

func s46AssertTreeUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("file tree changed size: %d -> %d", len(before), len(after))
	}
	for path, fp := range before {
		if after[path] != fp {
			t.Fatalf("preview changed %s", path)
		}
	}
}

// TestS46DocsMigration covers TSK-T-0058 A1: the migration preview is
// read-only, and the approved apply preserves links and history while
// preventing stale approvals, collisions, and unsafe paths.
func TestS46DocsMigration(t *testing.T) {
	t.Run("preview is read-only with explicit migration rows", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		before := s46TreeFiles(t, repo)
		output := captureStdout(t, func() {
			if err := docsAdoptCmd(Args{"vault": vault, "migration": "true", "json": "true"}); err != nil {
				t.Fatalf("migration preview: %v", err)
			}
		})
		s46AssertTreeUnchanged(t, before, s46TreeFiles(t, repo))
		var preview struct {
			Fingerprint string              `json:"fingerprint"`
			Proposals   []docsAdoptProposal `json:"proposals"`
		}
		if err := json.Unmarshal([]byte(output), &preview); err != nil {
			t.Fatalf("preview JSON: %v\n%s", err, output)
		}
		byPath := map[string]docsAdoptProposal{}
		for _, proposal := range preview.Proposals {
			byPath[proposal.Path] = proposal
		}
		plan := byPath[".tusker/specs/flow-plan.md"]
		if plan.Disposition != "promote" || plan.Target != "docs/system/proposals/flow-plan.md" {
			t.Fatalf("flow-plan row = %#v", plan)
		}
		if plan.Kind != "proposal" || plan.Lifecycle != "proposed" || plan.Conformance != "unverified" {
			t.Fatalf("flow-plan identity = kind %q lifecycle %q conformance %q", plan.Kind, plan.Lifecycle, plan.Conformance)
		}
		if !plan.StubSource || plan.SourceFingerprint == "" {
			t.Fatalf("flow-plan row is missing stub_source or source fingerprint: %#v", plan)
		}
		if !containsString(plan.Links, "./flow-details.md") {
			t.Fatalf("flow-plan links = %#v, want the relative co-migrated link", plan.Links)
		}
		if len(plan.ActiveRefs) != 0 {
			t.Fatalf("flow-plan active refs = %#v", plan.ActiveRefs)
		}
		choice := byPath[".tusker/specs/decisions/flow-choice.md"]
		if choice.Disposition != "promote" || choice.Target != "docs/system/decisions/flow-choice.md" || choice.Kind != "decision" {
			t.Fatalf("flow-choice row = %#v", choice)
		}
		index := byPath[".tusker/knowledge/domains/billing/00-index.md"]
		if index.Disposition != "promote" || index.Target != "docs/system/domains/billing/00-index.md" || index.Kind != "doc" || index.Lifecycle != "current" {
			t.Fatalf("billing index row = %#v", index)
		}
	})

	t.Run("approved apply preserves links and history", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		originals := s46MigrationOriginals(t, repo)
		tablePath := s46WriteMigrationTable(t, repo, s46MigrationProposals(t, repo))
		if err := s46ApproveMigration(t, vault, tablePath); err != nil {
			t.Fatal(err)
		}
		plan, err := docgraph.ParseDocHeaders("docs/system/proposals/flow-plan.md",
			mustReadFile(t, filepath.Join(repo, "docs/system/proposals/flow-plan.md")))
		if err != nil {
			t.Fatalf("promoted target does not parse: %v", err)
		}
		if string(plan.Kind) != "proposal" || plan.Status != "proposed" || plan.CodeConformance != "unverified" {
			t.Fatalf("promoted identity = kind %q status %q conformance %q", plan.Kind, plan.Status, plan.CodeConformance)
		}
		if plan.Subject != "flow-plan" || plan.PartOf != "overview" {
			t.Fatalf("promoted subject/part_of = %q/%q", plan.Subject, plan.PartOf)
		}
		if !strings.Contains(plan.Body, "[details](flow-details.md)") {
			t.Fatalf("co-migrated link was not repaired:\n%s", plan.Body)
		}
		if !strings.Contains(plan.Body, "[external](https://example.com/x)") || !strings.Contains(plan.Body, "[anchor](#lifecycle)") {
			t.Fatalf("unmigrated links were not preserved byte-identical:\n%s", plan.Body)
		}
		choice, err := docgraph.ParseDocHeaders("docs/system/decisions/flow-choice.md",
			mustReadFile(t, filepath.Join(repo, "docs/system/decisions/flow-choice.md")))
		if err != nil {
			t.Fatalf("migrated decision does not parse: %v", err)
		}
		if string(choice.Kind) != "decision" || choice.DecidesFor != "flow-plan" {
			t.Fatalf("migrated decision = kind %q decides_for %q", choice.Kind, choice.DecidesFor)
		}
		stubRaw := mustReadFile(t, filepath.Join(repo, ".tusker/specs/flow-plan.md"))
		stub, err := docgraph.ParseDocHeaders(".tusker/specs/flow-plan.md", stubRaw)
		if err != nil {
			t.Fatalf("forwarding stub does not parse: %v", err)
		}
		if !docgraph.IsForwardingStub(stub) {
			t.Fatalf("legacy source is not a subject-less stub: %#v", stub)
		}
		if strings.Contains(string(stubRaw), "[[") || !strings.Contains(string(stubRaw), "](../../docs/system/proposals/flow-plan.md)") {
			t.Fatalf("stub does not forward through a standard relative link:\n%s", stubRaw)
		}
		issues, err := docgraph.ValidateRepository(repo)
		if err != nil {
			t.Fatal(err)
		}
		for _, issue := range issues {
			if issue.Code == "DOC_DUPLICATE_SUBJECT" || issue.Code == "DOC_TOMBSTONE_SUCCESSOR_MISSING" ||
				issue.Code == "DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND" || issue.Code == "DOC_REQUIRED_FIELD_MISSING" {
				t.Fatalf("migrated corpus carries %s at %s: %s", issue.Code, issue.Path, issue.Message)
			}
		}
		corpus, _, err := docgraph.LoadRepository(repo)
		if err != nil {
			t.Fatal(err)
		}
		current, ok := docgraph.ResolveCurrentReference(corpus, ".tusker/specs/flow-plan.md")
		if !ok || current.Document.Subject != "flow-plan" || current.CanonicalRef != "docs/system/proposals/flow-plan.md" {
			t.Fatalf("legacy path did not forward to the portable document: %#v ok=%v", current, ok)
		}
		journalRaw, err := os.ReadFile(docsAdoptJournalPath(repo, s46TableFingerprint(t, tablePath)))
		if err != nil {
			t.Fatalf("recovery journal missing: %v", err)
		}
		var journal docsAdoptJournal
		if err := json.Unmarshal(journalRaw, &journal); err != nil || !journal.Complete {
			t.Fatalf("recovery journal incomplete: %v %s", err, journalRaw)
		}
		for _, row := range journal.Rows {
			if original, ok := originals[row.Path]; ok && string(row.OriginalSource) != original {
				t.Fatalf("journal lost the original bytes of %s", row.Path)
			}
		}
	})

	t.Run("stale approval names the changed source", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		tablePath := s46WriteMigrationTable(t, repo, s46MigrationProposals(t, repo))
		planPath := filepath.Join(repo, ".tusker/specs/flow-plan.md")
		raw, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(planPath, append(raw, []byte("\nStale edit.\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		err = s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "changed after review") || !strings.Contains(err.Error(), ".tusker/specs/flow-plan.md") {
			t.Fatalf("stale approval error = %v", err)
		}
	})

	t.Run("target collision leaves canonical bytes alone", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		writeTestDoc(t, repo, "docs/system/proposals/flow-plan.md", "---\nkind: proposal\nsubject: different-plan\npart_of: overview\nstatus: proposed\n---\n# Different\n")
		canonical := string(mustReadFile(t, filepath.Join(repo, "docs/system/proposals/flow-plan.md")))
		source, err := os.ReadFile(filepath.Join(repo, ".tusker/specs/flow-plan.md"))
		if err != nil {
			t.Fatal(err)
		}
		tablePath := s46WriteMigrationTable(t, repo, []docsAdoptProposal{{
			Path: ".tusker/specs/flow-plan.md", Subject: "flow-plan", Disposition: "promote",
			Target: "docs/system/proposals/flow-plan.md", Reason: "reviewed",
			Kind: "proposal", SourceFingerprint: docsAdoptBytesFingerprint(source),
		}})
		err = s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "canonical target collision") || !strings.Contains(err.Error(), "docs/system/proposals/flow-plan.md") {
			t.Fatalf("collision error = %v", err)
		}
		if got := string(mustReadFile(t, filepath.Join(repo, "docs/system/proposals/flow-plan.md"))); got != canonical {
			t.Fatalf("collision changed canonical bytes: %q", got)
		}
	})

	t.Run("duplicate subjects are refused with exact paths", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		first, err := os.ReadFile(filepath.Join(repo, ".tusker/specs/flow-plan.md"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(repo, ".tusker/specs/flow-details.md"))
		if err != nil {
			t.Fatal(err)
		}
		tablePath := s46WriteMigrationTable(t, repo, []docsAdoptProposal{
			{Path: ".tusker/specs/flow-plan.md", Subject: "flow-plan", Disposition: "promote",
				Target: "docs/system/proposals/flow-plan.md", Reason: "reviewed",
				Kind: "proposal", StubSource: true, SourceFingerprint: docsAdoptBytesFingerprint(first)},
			{Path: ".tusker/specs/flow-details.md", Subject: "flow-plan", Disposition: "promote",
				Target: "docs/system/proposals/flow-plan-alt.md", Reason: "reviewed",
				Kind: "proposal", StubSource: true, SourceFingerprint: docsAdoptBytesFingerprint(second)},
		})
		err = s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "duplicate subject") ||
			!strings.Contains(err.Error(), ".tusker/specs/flow-plan.md") || !strings.Contains(err.Error(), ".tusker/specs/flow-details.md") {
			t.Fatalf("duplicate subject error = %v", err)
		}
	})

	t.Run("symlinked source is refused with its path", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		outside := filepath.Join(t.TempDir(), "outside.md")
		if err := os.WriteFile(outside, []byte("# Outside\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(repo, ".tusker/specs/linked.md")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		tablePath := s46WriteMigrationTable(t, repo, []docsAdoptProposal{{
			Path: ".tusker/specs/linked.md", Subject: "Linked", Disposition: "promote",
			Target: "docs/system/proposals/linked.md", Reason: "reviewed",
			SourceFingerprint: docsAdoptBytesFingerprint([]byte("# Outside\n")),
		}})
		err := s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), ".tusker/specs/linked.md") {
			t.Fatalf("symlink error = %v", err)
		}
	})

	t.Run("dirty owned inputs are refused with exact paths", func(t *testing.T) {
		repo := t.TempDir()
		runGitDir(t, repo, "init", "-b", "main")
		vault := filepath.Join(repo, ".tusker")
		if err := os.MkdirAll(vault, 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
		writeTestDoc(t, repo, ".tusker/specs/flow-plan.md", "---\nsubject: flow-plan\npart_of: overview\n---\n# Flow plan\n")
		runGitDir(t, repo, "add", "-A")
		runGitDir(t, repo, "-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-m", "base")
		planPath := filepath.Join(repo, ".tusker/specs/flow-plan.md")
		raw, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(planPath, append(raw, []byte("\nUncommitted.\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		tablePath := s46WriteMigrationTable(t, repo, []docsAdoptProposal{{
			Path: ".tusker/specs/flow-plan.md", Subject: "flow-plan", Disposition: "promote",
			Target: "docs/system/proposals/flow-plan.md", Reason: "reviewed",
			Kind: "proposal", StubSource: true, SourceFingerprint: docsAdoptBytesFingerprint(source),
		}})
		err = s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "dirty owned inputs") || !strings.Contains(err.Error(), ".tusker/specs/flow-plan.md") {
			t.Fatalf("dirty input error = %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(repo, "docs/system/proposals/flow-plan.md")); !os.IsNotExist(statErr) {
			t.Fatal("dirty refusal still wrote the target")
		}
	})

	t.Run("conformance and lifecycle mapping needs evidence", func(t *testing.T) {
		cases := []struct {
			name        string
			kind        string
			lifecycle   string
			conformance string
		}{
			{"implemented without evidence", "proposal", "implemented", ""},
			{"matches without verify stamp", "proposal", "proposed", "matches"},
			{"drift without scope", "doc", "current", "drift"},
			{"unknown kind", "essay", "current", ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, _, _, err := docsAdoptRowIdentity(docsAdoptProposal{
					Path: ".tusker/specs/flow-plan.md", Kind: tc.kind,
					Lifecycle: tc.lifecycle, Conformance: tc.conformance,
				})
				if err == nil || !strings.Contains(err.Error(), ".tusker/specs/flow-plan.md") {
					t.Fatalf("identity error = %v", err)
				}
			})
		}
	})
}

func s46TableFingerprint(t *testing.T, tablePath string) string {
	t.Helper()
	raw, err := os.ReadFile(tablePath)
	if err != nil {
		t.Fatal(err)
	}
	var table docsAdoptTable
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	return table.Fingerprint
}

func s46RecoveryRepo(t *testing.T) (repo, vault string) {
	t.Helper()
	repo = t.TempDir()
	vault = filepath.Join(repo, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestDoc(t, repo, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeTestDoc(t, repo, ".tusker/specs/spec-a.md", "---\nsubject: spec-a\npart_of: overview\nstatus: canonical\n---\n# Spec A\n")
	writeTestDoc(t, repo, ".tusker/specs/spec-b.md", "---\nsubject: spec-b\npart_of: overview\nstatus: canonical\n---\n# Spec B\n")
	return repo, vault
}

// TestS46DocsMigrationRecovery covers TSK-T-0058 A2: an injected partial
// failure is recoverable through the journal, a repeated apply is
// idempotent, and task reference changes keep their proof authority.
func TestS46DocsMigrationRecovery(t *testing.T) {
	t.Run("partial failure resumes to completion", func(t *testing.T) {
		repo, vault := s46RecoveryRepo(t)
		originalA := string(mustReadFile(t, filepath.Join(repo, ".tusker/specs/spec-a.md")))
		tablePath := s46WriteMigrationTable(t, repo, s46MigrationProposals(t, repo))
		t.Setenv("TUSKER_DOCS_ADOPT_FAIL_AFTER", "1")
		err := s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "injected docs adopt failure") {
			t.Fatalf("injected failure error = %v", err)
		}
		journal, err := loadDocsAdoptJournal(docsAdoptJournalPath(repo, s46TableFingerprint(t, tablePath)))
		if err != nil || journal == nil || journal.Complete {
			t.Fatalf("journal after partial failure = %#v err=%v", journal, err)
		}
		if len(journal.Rows) != 2 {
			t.Fatalf("journal rows = %#v", journal.Rows)
		}
		if !journal.Rows[0].Done || journal.Rows[1].Done {
			t.Fatalf("journal did not record the partial apply: %#v", journal.Rows)
		}
		if string(journal.Rows[0].OriginalSource) != originalA {
			t.Fatal("journal lost the original bytes of the applied row")
		}
		if _, statErr := os.Stat(filepath.Join(repo, "docs/system/proposals/spec-b.md")); !os.IsNotExist(statErr) {
			t.Fatal("failed apply wrote the second target")
		}
		t.Setenv("TUSKER_DOCS_ADOPT_FAIL_AFTER", "")
		if err := s46ApproveMigration(t, vault, tablePath); err != nil {
			t.Fatalf("resume: %v", err)
		}
		for _, rel := range []string{"docs/system/proposals/spec-a.md", "docs/system/proposals/spec-b.md"} {
			if _, statErr := os.Stat(filepath.Join(repo, filepath.FromSlash(rel))); statErr != nil {
				t.Fatalf("resumed apply missed %s: %v", rel, statErr)
			}
		}
		journal, err = loadDocsAdoptJournal(docsAdoptJournalPath(repo, s46TableFingerprint(t, tablePath)))
		if err != nil || journal == nil || !journal.Complete {
			t.Fatalf("journal after resume = %#v err=%v", journal, err)
		}
	})

	t.Run("repeated apply changes no bytes", func(t *testing.T) {
		repo, vault := s46RecoveryRepo(t)
		tablePath := s46WriteMigrationTable(t, repo, s46MigrationProposals(t, repo))
		if err := s46ApproveMigration(t, vault, tablePath); err != nil {
			t.Fatal(err)
		}
		before := s46MigrationState(t, repo)
		if err := s46ApproveMigration(t, vault, tablePath); err != nil {
			t.Fatalf("repeat apply: %v", err)
		}
		s46AssertTreeUnchanged(t, before, s46MigrationState(t, repo))
	})

	t.Run("externally changed row refuses resume with its path", func(t *testing.T) {
		repo, vault := s46RecoveryRepo(t)
		tablePath := s46WriteMigrationTable(t, repo, s46MigrationProposals(t, repo))
		t.Setenv("TUSKER_DOCS_ADOPT_FAIL_AFTER", "1")
		if err := s46ApproveMigration(t, vault, tablePath); err == nil {
			t.Fatal("injected failure did not fire")
		}
		t.Setenv("TUSKER_DOCS_ADOPT_FAIL_AFTER", "")
		target := filepath.Join(repo, "docs/system/proposals/spec-a.md")
		raw, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, append(raw, []byte("\nOutside edit.\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		err = s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "changed during recovery") || !strings.Contains(err.Error(), ".tusker/specs/spec-a.md") {
			t.Fatalf("recovery refusal error = %v", err)
		}
	})

	t.Run("task references keep proof authority", func(t *testing.T) {
		repo, vault := s46MigrationRepo(t)
		taskRel := ".tusker/work/tasks/APP-T-0001.md"
		taskBody := strings.Join([]string{
			"---",
			`schema: "tusker.task/v7"`,
			`id: "APP-T-0001"`,
			`status: "backlog"`,
			`state_rev: "sha256:0000000000000000000000000000000000000000000000000000000000000000"`,
			"spec_refs:",
			`  - ".tusker/specs/flow-plan.md"`,
			"---",
			"",
			"# Governed task",
			"",
		}, "\n")
		writeTestDoc(t, repo, taskRel, taskBody)
		proposals := s46MigrationProposals(t, repo)
		for _, proposal := range proposals {
			if proposal.Path == ".tusker/specs/flow-plan.md" && !containsString(proposal.ActiveRefs, taskRel) {
				t.Fatalf("inventory missed the active task reference: %#v", proposal)
			}
		}
		tablePath := s46WriteMigrationTable(t, repo, proposals)
		err := s46ApproveMigration(t, vault, tablePath)
		if err == nil || !strings.Contains(err.Error(), "overlapping active work") || !strings.Contains(err.Error(), taskRel) {
			t.Fatalf("active work error = %v", err)
		}
		// Historical task bindings are never rewritten by migration: closing
		// the task through ordinary means lets the same table apply, the
		// recorded bytes stay identical, and the old path still resolves.
		closedBody := strings.Replace(taskBody, `status: "backlog"`, `status: "done"`, 1)
		writeTestDoc(t, repo, taskRel, closedBody)
		if err := s46ApproveMigration(t, vault, tablePath); err != nil {
			t.Fatalf("apply after task close: %v", err)
		}
		if got := string(mustReadFile(t, filepath.Join(repo, filepath.FromSlash(taskRel)))); got != closedBody {
			t.Fatalf("migration rewrote the task file:\n%s", got)
		}
		corpus, _, err := docgraph.LoadRepository(repo)
		if err != nil {
			t.Fatal(err)
		}
		current, ok := docgraph.ResolveCurrentReference(corpus, ".tusker/specs/flow-plan.md")
		if !ok || current.CanonicalRef != "docs/system/proposals/flow-plan.md" {
			t.Fatalf("closed task reference no longer resolves: %#v ok=%v", current, ok)
		}
	})
}

// TestS46DocsResetBoundary covers TSK-T-0058 A3: tracker reset and purge
// preserve portable knowledge in docs/system.
func TestS46DocsResetBoundary(t *testing.T) {
	repo := t.TempDir()
	runGitDir(t, repo, "init", "-b", "main")
	vault := filepath.Join(repo, defaultRepoVaultDir)
	portable := map[string]string{
		"docs/system/00-overview.md":              "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n",
		"docs/system/proposals/flow-plan.md":      "---\nkind: proposal\nsubject: flow-plan\npart_of: overview\nstatus: proposed\n---\n# Flow plan\n",
		"docs/system/decisions/flow-choice.md":    "---\nkind: decision\nsubject: flow-choice\npart_of: overview\ndecides_for: flow-plan\nstatus: proposed\n---\n# Flow choice\n",
		"docs/system/domains/billing/00-index.md": "---\nkind: doc\nsubject: billing-overview\npart_of: overview\nstatus: current\n---\n# Billing\n",
		".tusker/specs/keep.md":                   "# Keep this spec\n",
		".tusker/work/tasks/APP-T-0001.md":        "stale task\n",
		"src/keep.go":                             "package src\n",
	}
	for rel, body := range portable {
		writeFileForResetTest(t, filepath.Join(repo, filepath.FromSlash(rel)), body)
	}
	output := captureStdout(t, func() {
		if err := resetCmd(Args{"repo": repo, "dry-run": "true", "json": "true"}); err != nil {
			t.Fatalf("reset dry-run: %v", err)
		}
	})
	var plan struct {
		Preserved []string            `json:"preserved"`
		Actions   []tuskerPurgeAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(output), &plan); err != nil {
		t.Fatalf("reset dry-run JSON: %v\n%s", err, output)
	}
	if !containsString(plan.Preserved, filepath.Join(repo, "docs", "system")) {
		t.Fatalf("reset dry-run does not preserve portable knowledge: %#v", plan.Preserved)
	}
	for _, action := range plan.Actions {
		rel, err := filepath.Rel(repo, action.Path)
		if err != nil {
			continue
		}
		if rel == "docs" || strings.HasPrefix(filepath.ToSlash(rel), "docs/") {
			t.Fatalf("reset plans to touch portable knowledge: %#v", action)
		}
	}
	purgeActions, err := planTuskerPurge(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range purgeActions {
		rel, err := filepath.Rel(repo, action.Path)
		if err != nil {
			continue
		}
		if rel == "docs" || strings.HasPrefix(filepath.ToSlash(rel), "docs/") {
			t.Fatalf("purge plans to touch portable knowledge: %#v", action)
		}
	}
	if err := resetCmd(Args{"repo": repo, "yes": "true"}); err != nil {
		t.Fatal(err)
	}
	for rel, body := range portable {
		if !strings.HasPrefix(rel, "docs/") {
			continue
		}
		if got, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(rel))); err != nil || string(got) != body {
			t.Fatalf("portable knowledge %s changed across reset: %q %v", rel, got, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(vault, "specs", "keep.md")); err != nil || string(got) != "# Keep this spec\n" {
		t.Fatalf("legacy specs were not preserved: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(vault, "work", "tasks", "APP-T-0001.md")); !os.IsNotExist(err) {
		t.Fatalf("stale tracker task survived reset: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "src", "keep.go")); err != nil || string(got) != "package src\n" {
		t.Fatalf("source changed: %q %v", got, err)
	}
	if _, _, err := docgraph.LoadRepository(repo); err != nil {
		t.Fatalf("migrated corpus does not load after reset: %v", err)
	}
}
