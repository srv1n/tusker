package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// supersedeFixture builds a committed git repo with an overview, a decision
// d-old, and a doc g-old, all created through docs new.
func supersedeFixture(t *testing.T) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\ncode_conformance: not_applicable\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\n---\n# Overview\n")
	for _, a := range []Args{
		supersedeArgs(vault, "d-old", "decision", ""),
		supersedeArgs(vault, "g-old", "doc", ""),
	} {
		if err := docsNewCmd(a); err != nil {
			t.Fatalf("seed %v: %v", a["_pos"], err)
		}
	}
	runDocsGit(t, repoRoot, "init", "-q")
	runDocsGit(t, repoRoot, "add", "-A")
	runDocsGit(t, repoRoot, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "seed")
	return repoRoot, vault
}

func supersedeArgs(vault, subject, kind, supersedes string) Args {
	a := Args{"read-when": "Reading fixture.", "skip-when": "Reading elsewhere.", "vault": vault, "_pos": subject, "kind": kind, "quiet": "true"}
	if supersedes != "" {
		a["supersedes"] = supersedes
	}
	return a
}

func readDoc(t *testing.T, repoRoot, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDocsSupersedeWritesBothLinks(t *testing.T) {
	repoRoot, vault := supersedeFixture(t)
	oldBefore := readDoc(t, repoRoot, "docs/system/decisions/d-old.md")

	// --print previews both writes without writing.
	out := captureStdout(t, func() {
		a := supersedeArgs(vault, "d-next", "decision", "d-old")
		a["print"] = "true"
		if err := docsNewCmd(a); err != nil {
			t.Fatalf("print: %v", err)
		}
	})
	if !strings.Contains(out, "supersedes: d-old") || !strings.Contains(out, "superseded_by: d-next") {
		t.Fatalf("preview missing a write:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "docs/system/decisions/d-next.md")); !os.IsNotExist(err) {
		t.Fatalf("--print wrote the new doc: %v", err)
	}
	if readDoc(t, repoRoot, "docs/system/decisions/d-old.md") != oldBefore {
		t.Fatal("--print rewrote the old doc")
	}

	if err := docsNewCmd(supersedeArgs(vault, "d-next", "decision", "d-old")); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	newDoc := readDoc(t, repoRoot, "docs/system/decisions/d-next.md")
	if !strings.Contains(newDoc, "\nsupersedes: d-old\n") || !strings.Contains(newDoc, "Supersedes [d-old](d-old.md).") {
		t.Fatalf("new doc missing supersedes links:\n%s", newDoc)
	}
	oldDoc := readDoc(t, repoRoot, "docs/system/decisions/d-old.md")
	if !strings.Contains(oldDoc, "\nstatus: superseded\n") || !strings.Contains(oldDoc, "\nsuperseded_by: d-next\n") {
		t.Fatalf("old doc not flipped:\n%s", oldDoc)
	}
	_, bodyBefore, _ := strings.Cut(strings.TrimPrefix(oldBefore, "---\n"), "\n---\n")
	_, bodyAfter, _ := strings.Cut(strings.TrimPrefix(oldDoc, "---\n"), "\n---\n")
	if bodyBefore != bodyAfter {
		t.Fatalf("old body changed:\n%q\n%q", bodyBefore, bodyAfter)
	}
	var checkErr error
	report := captureStdout(t, func() { checkErr = docsCheckCmd(Args{"vault": vault, "json": "true"}) })
	if checkErr != nil {
		t.Fatalf("docs check after supersede: %v\n%s", checkErr, report)
	}
}

func TestDocsSupersedeRefusalsWriteNothing(t *testing.T) {
	cases := []struct {
		name, kind, old string
		prep            func(t *testing.T, repoRoot, vault string)
		want            string
	}{
		{name: "missing", kind: "decision", old: "nope", want: "no document has subject"},
		{name: "family", kind: "proposal", old: "d-old", want: "cannot supersede"},
		{name: "doc-vs-decision", kind: "decision", old: "g-old", want: "cannot supersede"},
		{name: "dirty", kind: "decision", old: "d-old", want: "uncommitted changes", prep: func(t *testing.T, repoRoot, _ string) {
			path := filepath.Join(repoRoot, "docs/system/decisions/d-old.md")
			raw := readDoc(t, repoRoot, "docs/system/decisions/d-old.md")
			if err := os.WriteFile(path, []byte(raw+"\nedit\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "already", kind: "decision", old: "d-old", want: `already superseded by "d-mid"`, prep: func(t *testing.T, repoRoot, vault string) {
			if err := docsNewCmd(supersedeArgs(vault, "d-mid", "decision", "d-old")); err != nil {
				t.Fatal(err)
			}
			runDocsGit(t, repoRoot, "add", "-A")
			runDocsGit(t, repoRoot, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "mid")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot, vault := supersedeFixture(t)
			if tc.prep != nil {
				tc.prep(t, repoRoot, vault)
			}
			before := docsGitOutput(t, repoRoot, "status", "--porcelain")
			err := docsNewCmd(supersedeArgs(vault, "x-new", tc.kind, tc.old))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if after := docsGitOutput(t, repoRoot, "status", "--porcelain"); after != before {
				t.Fatalf("refusal wrote files:\nbefore=%s\nafter=%s", before, after)
			}
		})
	}
}

// An edit landing between preparing the predecessor update and committing it
// must refuse the supersession rather than be overwritten.
func TestDocsSupersedeRefusesConcurrentPredecessorEdit(t *testing.T) {
	repoRoot, vault := supersedeFixture(t)
	oldPath := filepath.Join(repoRoot, "docs/system/decisions/d-old.md")
	edited := readDoc(t, repoRoot, "docs/system/decisions/d-old.md") + "\nconcurrent edit\n"
	docsSupersedeBeforeCommit = func() {
		if err := os.WriteFile(oldPath, []byte(edited), 0o644); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { docsSupersedeBeforeCommit = nil })
	err := docsNewCmd(supersedeArgs(vault, "d-next", "decision", "d-old"))
	if err == nil || !strings.Contains(err.Error(), "changed after it was read") {
		t.Fatalf("err = %v, want concurrent-edit refusal", err)
	}
	if got := readDoc(t, repoRoot, "docs/system/decisions/d-old.md"); got != edited {
		t.Fatalf("concurrent edit erased:\n%s", got)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, "docs/system/decisions/d-next.md")); !os.IsNotExist(statErr) {
		t.Fatalf("successor written despite refusal: %v", statErr)
	}
}

func TestDocsSupersedeRefusesSymlinkedDocsRoot(t *testing.T) {
	repoRoot := t.TempDir()
	outside := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	writeDocsFixture(t, outside, "system/00-overview.md", "---\nkind: doc\nsubject: overview\nstatus: current\ncode_conformance: not_applicable\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\n---\n# Overview\n")
	if err := os.Symlink(outside, filepath.Join(repoRoot, "docs")); err != nil {
		t.Fatal(err)
	}
	legacy := "---\nsubject: d-old\npart_of: overview\ndecides_for: overview\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\n---\n# Decision\n"
	writeDocsFixture(t, repoRoot, ".tusker/specs/decisions/d-old.md", legacy)
	err := docsNewCmd(supersedeArgs(vault, "d-next", "decision", "d-old"))
	if err == nil {
		t.Fatal("supersession through a symlinked docs/ must be refused")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "system/decisions/d-next.md")); !os.IsNotExist(statErr) {
		t.Fatalf("successor written outside the repository: %v", statErr)
	}
	if got := readDoc(t, repoRoot, ".tusker/specs/decisions/d-old.md"); got != legacy {
		t.Fatalf("predecessor rewritten:\n%s", got)
	}
}

func TestDocsSupersedeRefusesDirtyPredecessorInLinkedWorktree(t *testing.T) {
	repoRoot, _ := supersedeFixture(t)
	worktree := filepath.Join(t.TempDir(), "wt")
	runDocsGit(t, repoRoot, "worktree", "add", "-q", worktree)
	vault := filepath.Join(worktree, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(worktree, "docs/system/decisions/d-old.md")
	dirty := readDoc(t, worktree, "docs/system/decisions/d-old.md") + "\nedit\n"
	if err := os.WriteFile(oldPath, []byte(dirty), 0o644); err != nil {
		t.Fatal(err)
	}
	err := docsNewCmd(supersedeArgs(vault, "d-next", "decision", "d-old"))
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("err = %v, want dirty refusal in linked worktree", err)
	}
	if got := readDoc(t, worktree, "docs/system/decisions/d-old.md"); got != dirty {
		t.Fatalf("dirty predecessor rewritten:\n%s", got)
	}
}
