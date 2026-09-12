package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func TestScaffoldDocumentationSystemCreatesMapAndTuskerSkill(t *testing.T) {
	repo := t.TempDir()
	writes, err := scaffoldDocumentationSystem(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		"docs/system/00-overview.md",
		"docs/system/INDEX.md",
		"docs/system/graph.json",
		".tusker/specs",
		".tusker/specs/decisions",
		".agents/skills/tusker/SKILL.md",
		".agents/skills/tusker/references/SPECS.md",
		".claude/skills/tusker/references/SPECS.md",
	} {
		path := filepath.Join(repo, filepath.FromSlash(relative))
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("scaffold missing %s: %v", relative, err)
		}
	}
	if len(writes) != 8 {
		t.Fatalf("unexpected write report: %#v", writes)
	}
	if _, err := os.Stat(filepath.Join(repo, "docs/system/00-overview.md")); err != nil {
		t.Fatal(err)
	}
	if issues, err := checkScaffoldMap(repo); err != nil || len(issues) != 0 {
		t.Fatalf("fresh scaffold map validation: issues=%#v err=%v", issues, err)
	}
}

func TestScaffoldDocumentationSystemPreservesExistingSpecSkill(t *testing.T) {
	repo := t.TempDir()
	for _, relative := range []string{filepath.Join(".agents", "skills", "spec", "SKILL.md"), filepath.Join(".claude", "skills", "spec", "SKILL.md")} {
		path := filepath.Join(repo, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("local spec skill\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writes, err := scaffoldDocumentationSystem(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 8 {
		t.Fatalf("existing external spec skills must not affect the Tusker scaffold: %#v", writes)
	}
	for _, relative := range []string{filepath.Join(".agents", "skills", "spec", "SKILL.md"), filepath.Join(".claude", "skills", "spec", "SKILL.md")} {
		got, err := os.ReadFile(filepath.Join(repo, relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "local spec skill\n" {
			t.Fatalf("init overwrote existing %s", relative)
		}
	}
}

func TestScaffoldDocumentationSystemRefusesSymlinkedDocumentationRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".tusker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(repo, "docs", "system")); err != nil {
		t.Fatal(err)
	}
	if _, err := scaffoldDocumentationSystem(repo); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked documentation root accepted: %v", err)
	}
}

func TestScaffoldDocumentationSystemIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	if _, err := scaffoldDocumentationSystem(repo); err != nil {
		t.Fatal(err)
	}
	overview := filepath.Join(repo, "docs/system/00-overview.md")
	before, err := os.ReadFile(overview)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := scaffoldDocumentationSystem(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 0 {
		t.Fatalf("second scaffold should report no writes: %#v", writes)
	}
	after, err := os.ReadFile(overview)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("second scaffold rewrote overview")
	}
}

func TestExternalDesignSkillRouting(t *testing.T) {
	repo := t.TempDir()
	if _, err := scaffoldDocumentationSystem(repo); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		filepath.Join(".agents", "skills", "tusker", "references", "SPECS.md"),
		filepath.Join(".claude", "skills", "tusker", "references", "SPECS.md"),
	} {
		body, err := os.ReadFile(filepath.Join(repo, relative))
		if err != nil {
			t.Fatalf("read materialized skill %s: %v", relative, err)
		}
		text := string(body)
		if !strings.Contains(text, "external design") {
			t.Fatalf("%s omits the external-design route", relative)
		}
		if !strings.Contains(text, "supplied adequate spec") {
			t.Fatalf("%s does not skip design discussion for settled intent", relative)
		}
		if !strings.Contains(text, "available writing skill") || !strings.Contains(text, "otherwise") {
			t.Fatalf("%s does not provide the missing optional-writing route", relative)
		}
		if !strings.Contains(text, "tusker docs find <query>") {
			t.Fatalf("%s omits the search-first rule", relative)
		}
		if !strings.Contains(text, "tusker docs new") {
			t.Fatalf("%s omits the create-through-command rule", relative)
		}
	}
	for _, relative := range []string{filepath.Join(".agents", "skills", "spec"), filepath.Join(".claude", "skills", "spec")} {
		if _, err := os.Stat(filepath.Join(repo, relative)); !os.IsNotExist(err) {
			t.Fatalf("fresh scaffold created duplicate spec skill %s: %v", relative, err)
		}
	}
}

func checkScaffoldMap(repo string) ([]docgraph.Issue, error) {
	return docgraph.CheckDocsMapFresh(repo)
}
