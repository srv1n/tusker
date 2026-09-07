package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsRead(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/guide.md", "---\ntitle: The Guide\nsubject: guide\npart_of: overview\nstatus: canonical\nread_when: changing the guide\n---\n# The Guide\n\n## Setup\n\nStart here.\n\n### Detail\n\nKeep this detail.\n\n## Next\n\nLater.\n\nSee [[overview]].\n")

	before, err := os.ReadFile(filepath.Join(repoRoot, "docs/system/guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := docsReadCmd(Args{"vault": vault, "_pos": "guide", "section": "Setup", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var result docsReadResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("docs read JSON: %v\n%s", err, output)
	}
	if result.Subject != "guide" || result.Title != "The Guide" || result.Path != "docs/system/guide.md" || result.Revision == "" {
		t.Fatalf("docs read identity = %#v", result)
	}
	if result.Section != "## Setup" || !strings.Contains(result.Body, "Keep this detail.") || strings.Contains(result.Body, "Later.") {
		t.Fatalf("docs read section = %#v", result)
	}
	if len(result.Links) != 1 || !result.Links[0].Resolved || result.Links[0].Subject != "overview" {
		t.Fatalf("docs read links = %#v", result.Links)
	}
	human := captureStdout(t, func() {
		if err := docsReadCmd(Args{"vault": vault, "_pos": "guide"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(human, "Read when: changing the guide") || !strings.Contains(human, "Relationships: 1 links, 0 backlinks") {
		t.Fatalf("docs read human output = %q", human)
	}
	after, err := os.ReadFile(filepath.Join(repoRoot, "docs/system/guide.md"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("read changed document: err=%v", err)
	}
}

func TestDocsReadCurrentSuperseded(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/old.md", "---\nsubject: old\npart_of: overview\nstatus: superseded\nsuperseded_by: current\n---\n# Old\n")
	writeDocsFixture(t, repoRoot, "docs/system/current.md", "---\nsubject: current\npart_of: overview\nstatus: canonical\n---\n# Current\n")
	output := captureStdout(t, func() {
		if err := docsReadCmd(Args{"vault": vault, "_pos": "old", "current": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var result docsReadResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Subject != "current" || result.ResolvedFrom != "old" {
		t.Fatalf("current read = %#v", result)
	}
}

func TestDocsBacklinks(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/guide.md", "---\nsubject: guide\npart_of: overview\nstatus: canonical\n---\n# Guide\nSee [[target]] and [[missing]].\n")
	writeDocsFixture(t, repoRoot, ".tusker/specs/target.md", "---\nsubject: target\npart_of: overview\nstatus: canonical\n---\n# Target\n")
	output := captureStdout(t, func() {
		if err := docsBacklinksCmd(Args{"vault": vault, "_pos": "target", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var result struct {
		Backlinks []docsReadBacklink `json:"backlinks"`
		Broken    []struct {
			Path string `json:"path"`
			Ref  string `json:"ref"`
		} `json:"broken"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("docs backlinks JSON: %v\n%s", err, output)
	}
	if len(result.Backlinks) != 1 || result.Backlinks[0].Subject != "guide" || result.Backlinks[0].Via != "link" || result.Backlinks[0].Typed {
		t.Fatalf("backlinks = %#v", result.Backlinks)
	}
	if len(result.Broken) != 1 || result.Broken[0].Path != "docs/system/guide.md" || result.Broken[0].Ref != "missing" {
		t.Fatalf("broken = %#v", result.Broken)
	}
}

func TestDocsCommandsRejectSymlinkedDocument(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\nstatus: canonical\n---\n# Overview\n")
	if err := os.Symlink("00-overview.md", filepath.Join(repoRoot, "docs/system/linked.md")); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		call func(Args) error
	}{
		{name: "read", call: docsReadCmd},
		{name: "backlinks", call: docsBacklinksCmd},
		{name: "check", call: docsCheckCmd},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.call(Args{"vault": vault, "_pos": "docs/system/linked.md", "json": "true"})
			var typed *TuskerError
			if !errors.As(err, &typed) || typed.Code != "DOC_PATH_SYMLINK" {
				t.Fatalf("symlink error = %v (%T)", err, err)
			}
		})
	}
}
