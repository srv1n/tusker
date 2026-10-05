package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const docsTouchCLIDoc = "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: cli\npart_of: overview\ndescribes: [pkg]\n---\n# CLI\n"

// docsTouchRepo commits docs + code as "base", then changes code in a second
// commit so base...HEAD touches pkg/ but not the cli doc.
func docsTouchRepo(t *testing.T, rule string) (string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "config.yaml"), []byte("docs:\n  touch_rule: "+rule+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", docsTouchCLIDoc)
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "base")
	runDocsGit(t, repoRoot, "tag", "base")
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n\nvar Changed = 1\n")
	runDocsGit(t, repoRoot, "commit", "-am", "touch code")
	return repoRoot, vault
}

func runDocsTouch(t *testing.T, argv ...string) (string, error) {
	t.Helper()
	oldStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	runErr := docsCmd("touch", parseArgs(argv))
	_ = writer.Close()
	os.Stderr = oldStderr
	output, _ := io.ReadAll(reader)
	_ = reader.Close()
	return string(output), runErr
}

func TestDocsTouchBlockAndWarnReportDrift(t *testing.T) {
	_, vault := docsTouchRepo(t, "block")
	output, err := runDocsTouch(t, "--vault", vault, "--since", "base")
	if issue := errorToIssue(err); issue.Code != "DOC_TOUCH_DRIFT" || !strings.Contains(output, "cli (docs/system/cli.md)") {
		t.Fatalf("block: issue=%#v output=%q", issue, output)
	}

	_, vault = docsTouchRepo(t, "warn")
	output, err = runDocsTouch(t, "--vault", vault, "--since", "base")
	if err != nil || !strings.Contains(output, "cli (docs/system/cli.md)") {
		t.Fatalf("warn: err=%v output=%q", err, output)
	}

	_, vault = docsTouchRepo(t, "off")
	if output, err = runDocsTouch(t, "--vault", vault, "--since", "base"); err != nil || output != "" {
		t.Fatalf("off: err=%v output=%q", err, output)
	}
}

func TestDocsTouchDocEditWaiverAndBadRef(t *testing.T) {
	repoRoot, vault := docsTouchRepo(t, "block")
	if _, err := runDocsTouch(t, "--vault", vault, "--since", "base", "--waive", "other", "--waive", "cli"); err != nil {
		t.Fatalf("repeated --waive should clear drift: %v", err)
	}
	if _, err := runDocsTouch(t, "--vault", vault, "--since", "no-such-ref"); err == nil || !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("unresolvable ref = %v", err)
	}
	// An uncommitted doc edit counts as touched.
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", docsTouchCLIDoc+"\nUpdated behavior.\n")
	if output, err := runDocsTouch(t, "--vault", vault, "--since", "base"); err != nil || output != "" {
		t.Fatalf("doc edit should clear drift: err=%v output=%q", err, output)
	}
	// Uncommitted untracked code is also in scope.
	runDocsGit(t, repoRoot, "commit", "-am", "doc")
	runDocsGit(t, repoRoot, "tag", "-f", "base")
	writeDocsFixture(t, repoRoot, "pkg/new.go", "package pkg\n")
	if issue := errorToIssue(func() error { _, err := runDocsTouch(t, "--vault", vault, "--since", "base"); return err }()); issue.Code != "DOC_TOUCH_DRIFT" {
		t.Fatalf("untracked code change: issue=%#v", issue)
	}
}
