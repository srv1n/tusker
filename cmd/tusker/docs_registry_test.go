package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryRegistryCanonicalMetadataCanBeOverriddenByFileFrontmatter(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "docs", "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "publication.yaml"), []byte(`repo_docs:
  - source: docs/specs
    include: "*.md"
    route_prefix: developer/specs
    audience: developer
    canonical: true
    canonical_status: draft
    owner_epic: ORC
    verified_at: "2026-04-28"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "specs", "live.md"), []byte("# Live Spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "docs", "specs", "old.md"), []byte(`---
canonical: false
canonical_status: historical
deprecated: true
superseded_by: /developer/specs/
---

# Old Spec
`), 0o644); err != nil {
		t.Fatal(err)
	}

	sources, err := loadRepoDocsPublicationSources(repoRoot, false)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]docsSourceDocument{}
	for _, source := range sources {
		byPath[source.SourcePath] = source
	}
	live := byPath["docs/specs/live.md"]
	if !live.Canonical || live.CanonicalStatus != "draft" || live.OwnerEpic != "ORC" || live.VerifiedAt != "2026-04-28" {
		t.Fatalf("expected directory canonical defaults on live spec, got %#v", live)
	}
	old := byPath["docs/specs/old.md"]
	if old.Canonical {
		t.Fatalf("expected file frontmatter canonical:false to opt out of directory canonical default: %#v", old)
	}
	if old.CanonicalStatus != "historical" || !old.Deprecated || old.SupersededBy != "/developer/specs/" {
		t.Fatalf("expected file lifecycle override on old spec, got %#v", old)
	}
}
