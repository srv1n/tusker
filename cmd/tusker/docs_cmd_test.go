package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	for _, field := range []string{"subject:", "keywords:", "part_of:", "describes:", "status:", "created:", "last_verified:", "read_when:", "skip_when:"} {
		if !strings.Contains(string(body), field) {
			t.Fatalf("scaffold missing required header field %q; body=\n%s", field, body)
		}
	}
}

func TestDocsNewRefusesSymlinkedRootParentAndLeaf(t *testing.T) {
	newArgs := func(vault string) Args {
		return Args{"vault": vault, "_pos": "new-doc", "_pos0": "new-doc"}
	}
	t.Run("root", func(t *testing.T) {
		realRoot := t.TempDir()
		if err := os.MkdirAll(filepath.Join(realRoot, ".tusker"), 0o755); err != nil {
			t.Fatal(err)
		}
		linkedRoot := filepath.Join(t.TempDir(), "repo")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Fatal(err)
		}
		if err := docsNewCmd(newArgs(filepath.Join(linkedRoot, ".tusker"))); err == nil {
			t.Fatalf("symlinked root accepted: %v", err)
		}
	})

	t.Run("parent", func(t *testing.T) {
		repoRoot := t.TempDir()
		vault := filepath.Join(repoRoot, ".tusker")
		if err := os.MkdirAll(vault, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(repoRoot, "docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(repoRoot, "docs", "system")); err != nil {
			t.Fatal(err)
		}
		if err := docsNewCmd(newArgs(vault)); err == nil {
			t.Fatalf("symlinked parent accepted: %v", err)
		}
	})

	t.Run("leaf", func(t *testing.T) {
		repoRoot := t.TempDir()
		vault := filepath.Join(repoRoot, ".tusker")
		if err := os.MkdirAll(filepath.Join(repoRoot, "docs", "system"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(vault, 0o755); err != nil {
			t.Fatal(err)
		}
		leaf := filepath.Join(repoRoot, "docs", "system", "new-doc.md")
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing.md"), leaf); err != nil {
			t.Fatal(err)
		}
		if err := docsNewCmd(newArgs(vault)); err == nil {
			t.Fatalf("symlinked leaf accepted: %v", err)
		}
	})
}

func TestDocsVerifyAndDocTouchWaiverParsing(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\nlast_verified: '2026-01-01'\n---\n# CLI\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "seed")
	expectedDate := time.Now().Local().Format("2006-01-02")
	expectedCommit := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	if err := docsVerifyCmd(Args{"vault": vault, "_pos": "cli"}); err != nil {
		t.Fatalf("docs verify: %v", err)
	}
	updated, err := os.ReadFile(filepath.Join(repoRoot, "docs/system/cli.md"))
	if err != nil || !strings.Contains(string(updated), "last_verified: "+expectedDate+" @ "+expectedCommit) {
		t.Fatalf("docs verify did not stamp today: %s", updated)
	}
	waivers := parseV7DocTouchWaivers("## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| doc_unchanged | cli | waived | The prose remains accurate. |\n")
	if !waivers["cli"] {
		t.Fatalf("waiver row was not parsed: %#v", waivers)
	}
}

func TestDocsAdoptRoutesThroughCLI(t *testing.T) {
	command, args := parseCLI([]string{"tusker", "docs", "adopt", "--help"})
	if command != "docs adopt" || !args.Bool("help") {
		t.Fatalf("docs adopt parse = %q %#v", command, args)
	}
	if !printCommandHelp(command) {
		t.Fatal("docs adopt has no help route")
	}
	if err := docsCmd("unknown", Args{}); err == nil {
		t.Fatal("unknown docs subcommand unexpectedly succeeded")
	}
}

func TestDocsAdoptDryRunAndLegacyApplyAliasesNeverWrite(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/legacy.md", "# Legacy\n\nKeep this source byte-for-byte.\n")
	source := filepath.Join(repoRoot, "docs/legacy.md")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := docsAdoptCmd(Args{"vault": vault, "dry-run": "true", "approve": "true"}); err != nil {
		t.Fatalf("approve dry-run: %v", err)
	}
	if after, readErr := os.ReadFile(source); readErr != nil || string(after) != string(before) {
		t.Fatalf("approve dry-run changed source: err=%v bytes=%q", readErr, after)
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, "docs/system/legacy.md")); !os.IsNotExist(statErr) {
		t.Fatalf("approve dry-run created canonical target: %v", statErr)
	}
	for _, alias := range []string{"yes", "apply"} {
		for _, value := range []string{"true", "false"} {
			if err := docsAdoptCmd(Args{"vault": vault, alias: value}); err == nil || !strings.Contains(err.Error(), "--approve only") {
				t.Fatalf("--%s was accepted as an apply alias: %v", alias, err)
			}
		}
	}
}

func TestDocTouchWarningIsNonBlocking(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\ndescribes: [pkg]\n---\n# CLI\n")
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "code")
	sha := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n\nvar X = 1\n")
	runDocsGit(t, repoRoot, "add", "pkg/code.go")
	runDocsGit(t, repoRoot, "commit", "-m", "touch")
	sha = strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))

	oldStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = writer
	err = v7DocTouchCheck(vault, Note{Data: map[string]any{"id": "APP-T-0001", "source_sha": sha}})
	_ = writer.Close()
	os.Stderr = oldStderr
	warning, _ := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !strings.Contains(string(warning), "doc-touch drift") {
		t.Fatalf("warning check = %v, output=%q", err, warning)
	}
}

func TestDocTouchRuleConfigDefaultsAndOverrides(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	rule, err := v7DocsTouchRule(vault)
	if err != nil || rule != "warn" {
		t.Fatalf("default docs.touch_rule = %q, %v", rule, err)
	}
	if err := os.WriteFile(filepath.Join(vault, "config.yaml"), []byte("docs:\n  touch_rule: block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rule, err = v7DocsTouchRule(vault)
	if err != nil || rule != "block" {
		t.Fatalf("configured docs.touch_rule = %q, %v", rule, err)
	}
}

func TestDocTouchBlockFailsClosedWithoutRangeAuthority(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\ndescribes: [pkg]\n---\n# CLI\n")
	if err := os.WriteFile(filepath.Join(vault, "config.yaml"), []byte("docs:\n  touch_rule: block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := v7DocTouchCheck(vault, Note{Data: map[string]any{"id": "APP-T-0001"}, Body: ""})
	if errorToIssue(err).Code != "DOC_TOUCH_AUTHORITY_UNAVAILABLE" {
		t.Fatalf("missing source range error = %v", err)
	}
}

func TestDocTouchBlockRequiresDocEditOrExplicitWaiver(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\ndescribes: [pkg]\n---\n# CLI\n")
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "base")
	base := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n\nvar Changed = 1\n")
	runDocsGit(t, repoRoot, "add", "pkg/code.go")
	runDocsGit(t, repoRoot, "commit", "-m", "touch code")
	head := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(vault, "config.yaml"), []byte("docs:\n  touch_rule: block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := Note{Data: map[string]any{"id": "APP-T-0001", "source_sha": head, "base_sha": base}}
	if issue := errorToIssue(v7DocTouchCheck(vault, task)); issue.Code != "DOC_TOUCH_DRIFT" {
		t.Fatalf("block-mode drift issue = %#v, want DOC_TOUCH_DRIFT", issue)
	}
	waived := task
	waived.Body = "## Verification\n\n| Covers | Check | Result | Notes |\n|---|---|---|---|\n| doc_unchanged | cli | waived | Existing prose remains accurate. |\n"
	if err := v7DocTouchCheck(vault, waived); err != nil {
		t.Fatalf("explicit doc-touch waiver should satisfy block mode: %v", err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\ndescribes: [pkg]\n---\n# CLI\n\nUpdated behavior.\n")
	runDocsGit(t, repoRoot, "add", "docs/system/cli.md")
	runDocsGit(t, repoRoot, "commit", "-m", "update docs")
	head = strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	task.Data["source_sha"] = head
	if err := v7DocTouchCheck(vault, task); err != nil {
		t.Fatalf("doc edit in the same range should satisfy block mode: %v", err)
	}
}

func TestDocTouchUnclaimedPathsStayNonBlockingAndDocsStatusReportsGap(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\ndescribes: [pkg]\n---\n# CLI\n")
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n")
	writeDocsFixture(t, repoRoot, "internal/code.go", "package internal\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "base")
	base := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	writeDocsFixture(t, repoRoot, "internal/code.go", "package internal\n\nvar Changed = 1\n")
	runDocsGit(t, repoRoot, "add", "internal/code.go")
	runDocsGit(t, repoRoot, "commit", "-m", "touch unclaimed code")
	head := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(vault, "config.yaml"), []byte("docs:\n  touch_rule: block\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := v7DocTouchCheck(vault, Note{Data: map[string]any{"id": "APP-T-0001", "source_sha": head, "base_sha": base}}); err != nil {
		t.Fatalf("unclaimed code path must not block doc-touch close: %v", err)
	}
	output := captureStdout(t, func() {
		if err := docsStatusCmd(Args{"vault": vault, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var report struct {
		CoverageGaps []string `json:"coverage_gaps"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("docs status JSON: %v\n%s", err, output)
	}
	if len(report.CoverageGaps) != 1 || report.CoverageGaps[0] != "internal" {
		t.Fatalf("coverage gaps = %#v, want [internal]", report.CoverageGaps)
	}
}

func TestDocTouchChangedPathsUsesRecordedBaseRange(t *testing.T) {
	repoRoot := t.TempDir()
	vault := filepath.Join(repoRoot, ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDocsFixture(t, repoRoot, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n")
	runDocsGit(t, repoRoot, "init", "-b", "main")
	runDocsGit(t, repoRoot, "config", "user.email", "test@example.com")
	runDocsGit(t, repoRoot, "config", "user.name", "Test")
	runDocsGit(t, repoRoot, "add", ".")
	runDocsGit(t, repoRoot, "commit", "-m", "base")
	base := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	writeDocsFixture(t, repoRoot, "pkg/code.go", "package pkg\n\nvar First = 1\n")
	runDocsGit(t, repoRoot, "add", "pkg/code.go")
	runDocsGit(t, repoRoot, "commit", "-m", "first")
	writeDocsFixture(t, repoRoot, "pkg/other.go", "package pkg\n\nvar Second = 2\n")
	runDocsGit(t, repoRoot, "add", "pkg/other.go")
	runDocsGit(t, repoRoot, "commit", "-m", "second")
	head := strings.TrimSpace(docsGitOutput(t, repoRoot, "rev-parse", "HEAD"))
	paths, ok := v7DocTouchChangedPaths(vault, Note{Data: map[string]any{"id": "APP-T-0001", "source_sha": head, "base_sha": base}})
	if !ok || len(paths) != 2 || paths[0] != "pkg/code.go" || paths[1] != "pkg/other.go" {
		t.Fatalf("recorded base range paths = %#v, ok=%v", paths, ok)
	}
}

func runDocsGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func docsGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
