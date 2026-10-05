package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsStatusStaleStrictExitsNonzeroOnlyWhenStale(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	header := "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\n"
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", header+"subject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "seed")
	c := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", header+"subject: cli\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-18 @ "+c+"'\n---\n# CLI\n")
	writeDocsFixture(t, repoRoot, "docs/system/legacy.md", header+"subject: legacy\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-18'\n---\n# Legacy\n")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "docs")

	strict := Args{"vault": vault, "stale": "true", "strict": "true", "json": "true"}
	var err error
	out := captureStdout(t, func() { err = docsStatusCmd(strict) })
	if err != nil {
		t.Fatalf("no stale docs, strict must pass: %v\n%s", err, out)
	}
	text := captureStdout(t, func() { err = docsStatusCmd(Args{"vault": vault}) })
	if err != nil || !strings.Contains(text, "date-only stamp, run tusker docs verify legacy") || !strings.Contains(text, "overview — no describes") {
		t.Fatalf("text status err=%v:\n%s", err, text)
	}

	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n\nvar X = 1\n")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "touch code")
	out = captureStdout(t, func() { err = docsStatusCmd(strict) })
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale doc must fail strict, err=%v", err)
	}
	var report struct {
		Documents []struct {
			Subject string `json:"subject"`
			Stale   bool   `json:"stale"`
		} `json:"documents"`
	}
	if jerr := json.Unmarshal([]byte(out), &report); jerr != nil {
		t.Fatalf("json: %v\n%s", jerr, out)
	}
	if len(report.Documents) != 1 || report.Documents[0].Subject != "cli" || !report.Documents[0].Stale {
		t.Fatalf("--stale must list only stale docs: %#v", report.Documents)
	}
	text = captureStdout(t, func() { err = docsStatusCmd(Args{"vault": vault}) })
	if err != nil || !strings.Contains(text, "cli — STALE — 1 commits since") {
		t.Fatalf("text must mark STALE, err=%v:\n%s", err, text)
	}
}
