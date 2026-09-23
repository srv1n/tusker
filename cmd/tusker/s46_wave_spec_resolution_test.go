package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/docgraph"
)

func writeS46RepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// s46WaveFixture builds a repo whose spec mirrors the W-0040 authoring
// defect: the wave and its task reference the governing spec by bare subject
// (portable-project-documentation) instead of by file path.
func s46WaveFixture(t *testing.T) (vault string, idx v7Index, wave Note) {
	t.Helper()
	root := t.TempDir()
	writeS46RepoFile(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n\n# Overview\n")
	writeS46RepoFile(t, root, ".tusker/specs/portable-project-documentation.md", `---
subject: portable-project-documentation
title: "S46: Portable project documentation"
part_of: overview
---

# S46: Portable project documentation

## Outcome and authority

Portable knowledge lives under docs/system.
`)
	writeS46RepoFile(t, root, "docs/system/proposals/new-home.md", "---\nsubject: new-home\nkind: proposal\nstatus: accepted\npart_of: overview\n---\n\n# New home\n")
	writeS46RepoFile(t, root, ".tusker/specs/legacy.md", "---\nstatus: superseded\nsuperseded_by: new-home\n---\n\nSee [the new home](../../docs/system/proposals/new-home.md).\n")
	writeS46RepoFile(t, root, "docs/system/domains/waves/lifecycle.md", "---\nsubject: wave-lifecycle\nkind: doc\nstatus: current\npart_of: overview\n---\n\n# Lifecycle\n")

	vault = filepath.Join(root, ".tusker")
	task := Note{Data: map[string]any{
		"id": "TSK-T-0055", "title": "model", "spec_refs": []string{"portable-project-documentation"},
	}}
	wave = Note{Data: map[string]any{
		"id": "W-0040", "members": []string{"TSK-T-0055"},
		"spec_refs": []string{"portable-project-documentation"},
	}}
	idx = v7Index{
		Tasks: map[string]Note{"TSK-T-0055": task},
		Waves: map[string]Note{"W-0040": wave},
	}
	return vault, idx, wave
}

func s46Fingerprint(t *testing.T, vault string, idx v7Index, wave Note) (string, []string) {
	t.Helper()
	fingerprint, issues := waveMaterialFingerprint(vault, idx, wave)
	return fingerprint, issues
}

// TestS46WaveSpecResolution covers acceptance A3: wave material hashing
// resolves the same subject/path/section and legacy-forwarded documents as
// authoring and packet readers, hashes actual current source bytes, and
// preserves authorization invalidation on change without any bypass.
func TestS46WaveSpecResolution(t *testing.T) {
	vault, idx, wave := s46WaveFixture(t)

	// The W-0040 refusal is repaired: a bare subject ref resolves and the
	// material is armable with no issues.
	before, issues := s46Fingerprint(t, vault, idx, wave)
	if len(issues) != 0 {
		t.Fatalf("subject spec_ref should resolve: %#v", issues)
	}

	// A subject ref and its concrete path ref hash the same source file.
	repoRoot := v7RepoRoot(vault)
	var corpusLoadedCheck = func() (string, string) {
		t.Helper()
		corpus, _, err := docgraph.LoadRepository(repoRoot)
		if err != nil {
			t.Fatal(err)
		}
		subjectPath, subjectIssue := waveMaterialSpecFile(vault, "portable-project-documentation", corpus, true)
		pathPath, pathIssue := waveMaterialSpecFile(vault, ".tusker/specs/portable-project-documentation.md", corpus, true)
		if subjectIssue != "" || pathIssue != "" {
			t.Fatalf("subject/path resolution mismatch: %q %q", subjectIssue, pathIssue)
		}
		return subjectPath, pathPath
	}
	if subjectPath, pathPath := corpusLoadedCheck(); subjectPath != pathPath {
		t.Fatalf("subject and path refs hash different files: %q vs %q", subjectPath, pathPath)
	}

	// Changed spec content invalidates authorization: no bypass.
	specPath := filepath.Join(repoRoot, ".tusker", "specs", "portable-project-documentation.md")
	original, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, append(original, []byte("\nMaterial intent change.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, issues := s46Fingerprint(t, vault, idx, wave)
	if len(issues) != 0 || changed == before {
		t.Fatalf("spec content change escaped authorization: before=%s after=%s issues=%#v", before, changed, issues)
	}
	if err := os.WriteFile(specPath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// A missing subject stays unresolvable.
	missing := idx
	missing.Tasks = cloneNoteMap(idx.Tasks)
	missingTask := missing.Tasks["TSK-T-0055"]
	missingTask.Data = cloneMap(missingTask.Data)
	missingTask.Data["spec_refs"] = []string{"no-such-document"}
	missing.Tasks["TSK-T-0055"] = missingTask
	if _, issues := s46Fingerprint(t, vault, missing, wave); !containsIssue(issues, "spec_ref does not resolve: no-such-document") {
		t.Fatalf("missing subject accepted: %#v", issues)
	}

	// Ambiguous duplicate subjects fail instead of silently picking a route.
	writeS46RepoFile(t, repoRoot, "docs/system/duplicate.md", "---\nsubject: portable-project-documentation\npart_of: overview\n---\n\n# Duplicate\n")
	if _, issues := s46Fingerprint(t, vault, idx, wave); !containsIssue(issues, "spec_ref is ambiguous: portable-project-documentation") {
		t.Fatalf("ambiguous subject silently resolved: %#v", issues)
	}
	if err := os.Remove(filepath.Join(repoRoot, "docs", "system", "duplicate.md")); err != nil {
		t.Fatal(err)
	}

	// A moved legacy path forwards to the current document and hashes its
	// bytes: editing the successor invalidates, editing the stub does not.
	forwarded := idx
	forwarded.Waves = cloneNoteMap(idx.Waves)
	forwardedWave := forwarded.Waves["W-0040"]
	forwardedWave.Data = cloneMap(forwardedWave.Data)
	forwardedWave.Data["spec_refs"] = []string{".tusker/specs/legacy.md"}
	forwarded.Waves["W-0040"] = forwardedWave
	forwardedBase, issues := s46Fingerprint(t, vault, idx, forwardedWave)
	if len(issues) != 0 {
		t.Fatalf("legacy-forwarded ref should resolve: %#v", issues)
	}
	newHome := filepath.Join(repoRoot, "docs", "system", "proposals", "new-home.md")
	newHomeOriginal, err := os.ReadFile(newHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newHome, append(newHomeOriginal, []byte("\nSuccessor intent change.\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if after, issues := s46Fingerprint(t, vault, idx, forwardedWave); len(issues) != 0 || after == forwardedBase {
		t.Fatalf("successor change escaped authorization: %#v", issues)
	}
	if err := os.WriteFile(newHome, newHomeOriginal, 0o644); err != nil {
		t.Fatal(err)
	}
	stubPath := filepath.Join(repoRoot, ".tusker", "specs", "legacy.md")
	stubOriginal, err := os.ReadFile(stubPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stubPath, append(stubOriginal, []byte("\n<!-- pointer polish -->\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if after, issues := s46Fingerprint(t, vault, idx, forwardedWave); len(issues) != 0 || after != forwardedBase {
		t.Fatalf("stub-only edit invalidated authorization: %#v", issues)
	}
	if err := os.WriteFile(stubPath, stubOriginal, 0o644); err != nil {
		t.Fatal(err)
	}

	// Section anchors resolve against the current document body.
	anchored := idx
	anchored.Waves = cloneNoteMap(idx.Waves)
	anchoredWave := anchored.Waves["W-0040"]
	anchoredWave.Data = cloneMap(anchoredWave.Data)
	anchoredWave.Data["spec_refs"] = []string{"portable-project-documentation#Outcome and authority"}
	anchored.Waves["W-0040"] = anchoredWave
	if _, issues := s46Fingerprint(t, vault, idx, anchoredWave); len(issues) != 0 {
		t.Fatalf("valid section anchor rejected: %#v", issues)
	}
	anchoredWave.Data = cloneMap(anchoredWave.Data)
	anchoredWave.Data["spec_refs"] = []string{"portable-project-documentation#No Such Section"}
	if _, issues := s46Fingerprint(t, vault, idx, anchoredWave); !containsIssue(issues, "spec_ref does not resolve: portable-project-documentation#No Such Section") {
		t.Fatalf("bogus section anchor accepted: %#v", issues)
	}

	// Wrong kinds fail visibly: a doc-kind chapter is not governing material.
	wrongKind := idx
	wrongKind.Waves = cloneNoteMap(idx.Waves)
	wrongKindWave := wrongKind.Waves["W-0040"]
	wrongKindWave.Data = cloneMap(wrongKindWave.Data)
	wrongKindWave.Data["spec_refs"] = []string{"wave-lifecycle"}
	wrongKind.Waves["W-0040"] = wrongKindWave
	if _, issues := s46Fingerprint(t, vault, idx, wrongKindWave); !containsIssue(issues, "spec_ref does not resolve: wave-lifecycle") {
		t.Fatalf("doc-kind spec_ref accepted as governing material: %#v", issues)
	}
}

func containsIssue(issues []string, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}
