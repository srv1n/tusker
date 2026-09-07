package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsBrowse(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/architecture/00-index.md", "---\ntitle: Architecture\nsubject: architecture\npart_of: overview\nstatus: canonical\n---\n# Architecture\n")
	writeDocsFixture(t, repoRoot, "docs/system/architecture/guide.md", "---\nsubject: guide\npart_of: architecture\nread_when: changing the guide\nstatus: canonical\n---\n# Guide\n")
	writeDocsFixture(t, repoRoot, "docs/system/zeta.md", "---\nsubject: zeta\npart_of: overview\nstatus: canonical\n---\n# Zeta\n")

	command, parsed := parseCLI([]string{"tusker", "docs", "browse", "docs/system", "--limit", "2", "--json"})
	if command != "docs browse" || parsed.String("_pos") != "docs/system" {
		t.Fatalf("docs browse parse = %q %#v", command, parsed)
	}
	before, err := os.ReadFile(filepath.Join(repoRoot, "docs/system/zeta.md"))
	if err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := docsBrowseCmd(Args{"vault": vault, "_pos": "docs/system", "limit": "2", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var result struct {
		Path    string `json:"path"`
		Entries []struct {
			Path    string `json:"path"`
			Kind    string `json:"kind"`
			Summary string `json:"summary"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
		Limit     int  `json:"limit"`
		Omitted   int  `json:"omitted"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("docs browse JSON: %v\n%s", err, output)
	}
	if result.Path != "docs/system/" || len(result.Entries) != 2 || !result.Truncated || result.Omitted != 1 || result.Limit != 2 {
		t.Fatalf("docs browse result = %#v", result)
	}
	if result.Entries[0].Kind != "folder" || result.Entries[0].Summary != "Architecture" {
		t.Fatalf("folder result = %#v", result.Entries[0])
	}
	after, err := os.ReadFile(filepath.Join(repoRoot, "docs/system/zeta.md"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("read browse changed document: err=%v", err)
	}
}

func TestDocsBrowseRejectsEscapes(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	err := docsBrowseCmd(Args{"vault": vault, "_pos": "../outside"})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("escape browse error = %v", err)
	}
	var typed *TuskerError
	if !errors.As(err, &typed) || typed.Code != "DOC_PATH_ESCAPE" {
		t.Fatalf("escape browse typed error = %#v", err)
	}
}

func TestDocsCommandRoutes(t *testing.T) {
	for _, test := range []struct {
		argv []string
		want string
	}{
		{[]string{"tusker", "docs", "browse", "docs/system"}, "docs browse"},
		{[]string{"tusker", "docs", "read", "overview", "--section", "Intro"}, "docs read"},
		{[]string{"tusker", "docs", "backlinks", "overview", "--limit", "4"}, "docs backlinks"},
		{[]string{"tusker", "docs", "check", "--json"}, "docs check"},
	} {
		command, _ := parseCLI(test.argv)
		if command != test.want {
			t.Fatalf("parseCLI(%v) = %q, want %q", test.argv, command, test.want)
		}
	}
	help := captureStdout(t, printDocsHelp)
	for _, want := range []string{
		"tusker docs browse [<managed-directory>] [--limit <n>] [--json]",
		"tusker docs read <subject-or-path> [--section <heading>] [--current] [--json]",
		"tusker docs backlinks <subject-or-path> [--limit <n>] [--json]",
		"tusker docs check [<subject-or-path>] [--json]",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("docs help omitted %q:\n%s", want, help)
		}
	}
}
