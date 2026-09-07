package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsCheck(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/guide.md", "---\nsubject: overview\npart_of: overview\nstatus: canonical\n---\n# Guide\nSee [[missing]].\n")
	output := captureStdout(t, func() {
		err := docsCheckCmd(Args{"vault": vault, "json": "true"})
		var checkFailure *docsCheckFailure
		if !errors.As(err, &checkFailure) || checkFailure.Count < 2 {
			t.Fatalf("docs check failure = %v", err)
		}
	})
	var result docsCheckResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("docs check JSON: %v\n%s", err, output)
	}
	if result.Valid || len(result.Issues) < 2 {
		t.Fatalf("docs check result = %#v", result)
	}
	for _, issue := range result.Issues {
		if issue.Repair == "" {
			t.Fatalf("issue lacks repair guidance: %#v", issue)
		}
	}
	output = captureStdout(t, func() {
		code, err := runInner("docs check", Args{"vault": vault, "json": "true"})
		if code != 1 || err != nil {
			t.Fatalf("runInner docs check exit = %d, err=%v", code, err)
		}
	})
	var routed docsCheckResult
	if err := json.Unmarshal([]byte(output), &routed); err != nil {
		t.Fatalf("routed docs check JSON: %v\n%s", err, output)
	}
	if routed.Valid {
		t.Fatalf("routed docs check unexpectedly valid: %#v", routed)
	}

	output = captureStdout(t, func() {
		err := docsCheckCmd(Args{"vault": vault, "_pos": "docs/system/guide.md", "json": "true"})
		var checkFailure *docsCheckFailure
		if !errors.As(err, &checkFailure) || checkFailure.Count != 2 {
			t.Fatalf("targeted docs check failure = %v", err)
		}
	})
	var targeted docsCheckResult
	if err := json.Unmarshal([]byte(output), &targeted); err != nil {
		t.Fatal(err)
	}
	if targeted.Path != "docs/system/guide.md" || len(targeted.Issues) != 2 {
		t.Fatalf("targeted check = %#v", targeted)
	}
}

func TestDocsScaffold(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	output := captureStdout(t, func() {
		if err := docsNewCmd(Args{"vault": vault, "_pos": "new guide", "json": "true", "print": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var result struct {
		Written bool   `json:"written"`
		Content string `json:"content"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("docs scaffold JSON: %v\n%s", err, output)
	}
	if result.Written || result.Path != "docs/system/new-guide.md" || !strings.Contains(result.Content, "part_of: overview") {
		t.Fatalf("docs scaffold = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "docs/system/new-guide.md")); !os.IsNotExist(err) {
		t.Fatalf("--print wrote scaffold: %v", err)
	}

	if err := docsNewCmd(Args{"vault": vault, "_pos": "new guide"}); err != nil {
		t.Fatalf("docs new: %v", err)
	}
	created, err := os.ReadFile(filepath.Join(repoRoot, "docs/system/new-guide.md"))
	if err != nil || !strings.Contains(string(created), "part_of: overview") {
		t.Fatalf("created scaffold: err=%v body=%s", err, created)
	}
}
