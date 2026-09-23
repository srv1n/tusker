package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func s46CLIRepo(t *testing.T) (repoRoot, vault string) {
	t.Helper()
	repoRoot = t.TempDir()
	vault = filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\n---\n# Overview\n")
	return repoRoot, vault
}

func s46CLIParse(t *testing.T, repoRoot, relative string) docgraph.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("created document missing at %s: %v", relative, err)
	}
	doc, err := docgraph.ParseDocHeaders(relative, raw)
	if err != nil {
		t.Fatalf("created document %s does not parse: %v", relative, err)
	}
	return doc
}

// TestS46PortableDocsCLI covers TSK-T-0056 A1: a fresh repository creates and
// discovers every kind and a domain index at the exact portable paths.
func TestS46PortableDocsCLI(t *testing.T) {
	repoRoot, vault := s46CLIRepo(t)
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
	if path := newDoc("legacy-alias", "spec", "", nil); path != "docs/system/proposals/legacy-alias.md" {
		t.Fatalf("spec alias path = %q", path)
	}
	if path := newDoc("record-choice", "decision", "", Args{"decides-for": "checkout-flow"}); path != "docs/system/decisions/record-choice.md" {
		t.Fatalf("decision path = %q", path)
	}

	cases := []struct {
		relative, kind, status, parent string
	}{
		{"docs/system/domains/billing/00-index.md", "doc", "current", "overview"},
		{"docs/system/domains/billing/invoicing.md", "doc", "current", "billing-overview"},
		{"docs/system/proposals/checkout-flow.md", "proposal", "proposed", "overview"},
		{"docs/system/proposals/legacy-alias.md", "proposal", "proposed", "overview"},
		{"docs/system/decisions/record-choice.md", "decision", "proposed", "overview"},
	}
	for _, tc := range cases {
		doc := s46CLIParse(t, repoRoot, tc.relative)
		if string(doc.Kind) != tc.kind || doc.Status != tc.status || doc.PartOf != tc.parent {
			t.Fatalf("%s parsed as kind=%q status=%q part_of=%q", tc.relative, doc.Kind, doc.Status, doc.PartOf)
		}
		if doc.CodeConformance == "matches" {
			t.Fatalf("%s claims conformance at creation", tc.relative)
		}
		raw, _ := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(tc.relative)))
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "status:") && !strings.Contains(trimmed, tc.status) {
				t.Fatalf("%s declares %q, want lifecycle %q", tc.relative, trimmed, tc.status)
			}
			if strings.HasPrefix(trimmed, "code_conformance:") && !strings.Contains(trimmed, "unverified") {
				t.Fatalf("%s claims conformance at creation: %q", tc.relative, trimmed)
			}
		}
	}
	decision := s46CLIParse(t, repoRoot, "docs/system/decisions/record-choice.md")
	if decision.DecidesFor != "checkout-flow" {
		t.Fatalf("decision decides_for = %q", decision.DecidesFor)
	}

	corpus, _, err := docgraph.LoadRepository(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"billing-overview", "invoicing", "checkout-flow", "legacy-alias", "record-choice"} {
		if _, ok := docgraph.ResolveReference(corpus, subject); !ok {
			t.Fatalf("created subject %q is not discoverable", subject)
		}
	}
	browse, err := docgraph.Browse(repoRoot, "docs/system/domains/billing", 50)
	if err != nil {
		t.Fatalf("browse domain: %v", err)
	}
	if len(browse.Entries) != 2 {
		t.Fatalf("domain browse entries = %#v", browse.Entries)
	}
	text := captureStdout(t, func() {
		if err := docsBrowseCmd(Args{"vault": vault, "_pos": "docs/system/domains/billing"}); err != nil {
			t.Fatalf("browse text: %v", err)
		}
	})
	for _, want := range []string{"[doc/current]", "billing-overview", "invoicing"} {
		if !strings.Contains(text, want) {
			t.Fatalf("browse text omits %q:\n%s", want, text)
		}
	}
}

// TestS46PortableDocsInit covers TSK-T-0056 A2: repeated init, preview,
// invalid inputs and user-owned files preserve data and truthful defaults.
func TestS46PortableDocsInit(t *testing.T) {
	t.Run("repeated init preserves user material", func(t *testing.T) {
		repo := t.TempDir()
		userDoc := "---\nkind: doc\nsubject: handbook\npart_of: overview\nstatus: current\n---\n\n# Handbook\n\nKeep these bytes.\n"
		if _, err := scaffoldDocumentationSystem(repo); err != nil {
			t.Fatal(err)
		}
		overview := filepath.Join(repo, "docs/system/00-overview.md")
		beforeOverview, err := os.ReadFile(overview)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "docs/system/handbook.md"), []byte(userDoc), 0o644); err != nil {
			t.Fatal(err)
		}
		writes, err := scaffoldDocumentationSystem(repo)
		if err != nil {
			t.Fatal(err)
		}
		if len(writes) != 0 {
			t.Fatalf("second init must not rewrite user material: %#v", writes)
		}
		if after, _ := os.ReadFile(overview); string(after) != string(beforeOverview) {
			t.Fatal("second init rewrote the overview")
		}
		if after, _ := os.ReadFile(filepath.Join(repo, "docs/system/handbook.md")); string(after) != userDoc {
			t.Fatalf("second init touched user material: %q", after)
		}
		overviewRaw, err := os.ReadFile(overview)
		if err != nil {
			t.Fatal(err)
		}
		seed, err := docgraph.ParseDocHeaders("docs/system/00-overview.md", overviewRaw)
		if err != nil {
			t.Fatalf("fresh overview does not parse: %v", err)
		}
		if string(seed.Kind) != "doc" || seed.Status != "current" {
			t.Fatalf("fresh overview scaffolds legacy identity: kind=%q status=%q", seed.Kind, seed.Status)
		}
	})

	t.Run("preview never writes", func(t *testing.T) {
		_, vault := s46CLIRepo(t)
		output := captureStdout(t, func() {
			if err := docsNewCmd(Args{"vault": vault, "_pos": "preview-only", "kind": "decision", "print": "true", "json": "true"}); err != nil {
				t.Fatalf("preview: %v", err)
			}
		})
		if !strings.Contains(output, `"written":false`) || !strings.Contains(output, "docs/system/decisions/preview-only.md") {
			t.Fatalf("unexpected preview output: %s", output)
		}
	})

	t.Run("invalid inputs fail visibly", func(t *testing.T) {
		_, vault := s46CLIRepo(t)
		bad := []struct {
			name string
			args Args
			want string
		}{
			{"unknown kind", Args{"_pos": "x", "kind": "essay"}, "--kind"},
			{"bad domain", Args{"_pos": "x", "domain": "../escape"}, "--domain"},
			{"domain with proposal", Args{"_pos": "x", "kind": "proposal", "domain": "billing"}, "--domain applies to docs only"},
			{"chapter without index", Args{"_pos": "x", "domain": "billing"}, "--index"},
			{"index without domain", Args{"_pos": "x", "index": "true"}, "--domain"},
		}
		for _, tc := range bad {
			t.Run(tc.name, func(t *testing.T) {
				tc.args["vault"] = vault
				if tc.args["_pos0"] == "" {
					tc.args["_pos0"] = tc.args["_pos"]
				}
				err := docsNewCmd(tc.args)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
			})
		}
		if err := docsNewCmd(Args{"vault": vault, "_pos": "billing-overview", "domain": "billing", "index": "true"}); err != nil {
			t.Fatalf("create domain index: %v", err)
		}
		err := docsNewCmd(Args{"vault": vault, "_pos": "billing-overview", "domain": "billing", "index": "true"})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("recreate index error = %v", err)
		}
		err = docsNewCmd(Args{"vault": vault, "_pos": "billing-overview"})
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("duplicate subject error = %v", err)
		}
	})

	t.Run("symlinked domain refuses writes", func(t *testing.T) {
		repoRoot, vault := s46CLIRepo(t)
		target := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repoRoot, "docs/system/domains"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(repoRoot, "docs/system/domains", "billing")); err != nil {
			t.Fatal(err)
		}
		err := docsNewCmd(Args{"vault": vault, "_pos": "billing-overview", "_pos0": "billing-overview", "domain": "billing", "index": "true"})
		if err == nil {
			t.Fatal("symlinked domain accepted a write")
		}
	})
}
