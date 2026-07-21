package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDocsFixture(t *testing.T, repoRoot, relative, content string) {
	t.Helper()
	path := filepath.Join(repoRoot, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDocsNewRefusesDuplicateSubject(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nkeywords: [system]\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/orchestration.md", "---\nsubject: orchestration\nkeywords: [worktree]\npart_of: overview\n---\n# Orchestration\n")

	args := Args{"vault": vault, "_pos": "orchestration", "_pos0": "orchestration"}
	err := docsNewCmd(args)
	if err == nil {
		t.Fatalf("docs new accepted an existing subject")
	}
	var tuskerErr *TuskerError
	if !errors.As(err, &tuskerErr) {
		t.Fatalf("unexpected error type: %T (%v)", err, err)
	}
	if tuskerErr.Code != errorAlreadyExists {
		t.Fatalf("code = %q, want %q", tuskerErr.Code, errorAlreadyExists)
	}
	if !strings.Contains(tuskerErr.Message, "docs/system/orchestration.md") {
		t.Fatalf("refusal message does not point at the existing file: %q", tuskerErr.Message)
	}

	args = Args{"vault": vault, "_pos": "worktree-lifecycle", "_pos0": "worktree-lifecycle"}
	if err := docsNewCmd(args); err != nil {
		t.Fatalf("docs new refused a fresh subject: %v", err)
	}
	created := filepath.Join(repoRoot, "docs/system/worktree-lifecycle.md")
	body, err := os.ReadFile(created)
	if err != nil {
		t.Fatalf("scaffold not written: %v", err)
	}
	for _, field := range []string{"subject:", "keywords:", "part_of:", "status:", "created:", "read_when:", "skip_when:"} {
		if !strings.Contains(string(body), field) {
			t.Fatalf("scaffold missing required header field %q; body=\n%s", field, body)
		}
	}
}
