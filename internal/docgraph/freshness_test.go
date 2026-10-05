package docgraph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsFreshnessOrdersNeverVerifiedThenCommitCount(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/never.md", "---\nsubject: never\npart_of: overview\ndescribes: [pkg]\n---\n# Never\n")
	writeDoc(t, root, "docs/system/stale.md", "---\nsubject: stale\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-01-01'\n---\n# Stale\n")
	writeDoc(t, root, "docs/system/unknown.md", "---\nsubject: unknown\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-01 @ deadbeef'\n---\n# Unknown\n")
	writeDoc(t, root, "pkg/code.go", "package pkg\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	seed := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
	writeDoc(t, root, "docs/system/stale.md", "---\nsubject: stale\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-18 @ "+seed+"'\n---\n# Stale\n")
	writeDoc(t, root, "pkg/code.go", "package pkg\n\nvar X = 1\n")
	runGit(t, root, "add", "pkg/code.go")
	runGit(t, root, "commit", "-m", "touch code")

	rows, err := DocsFreshness(root)
	if err != nil {
		t.Fatalf("DocsFreshness() error = %v", err)
	}
	if len(rows) != 4 || !rows[0].NeverVerified || !rows[1].NeverVerified || !rows[2].NeverVerified || rows[3].Document.Subject != "stale" || rows[3].TouchingCommits != 1 {
		t.Fatalf("unexpected freshness ordering: %#v", rows)
	}
	if !rows[3].Stale || rows[0].Stale || rows[1].Stale || rows[2].Stale {
		t.Fatalf("only the commit-stamped, touched doc is stale: %#v", rows)
	}
	writeDoc(t, root, "docs/system/stamped.md", "---\nsubject: stamped\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-18 @ "+seed+"'\n---\n# Stamped\n")
	rows, err = DocsFreshness(root)
	if err != nil {
		t.Fatalf("DocsFreshness() after commit stamp error = %v", err)
	}
	for _, row := range rows {
		if row.Document.Subject == "stamped" && row.TouchingCommits != 1 {
			t.Fatalf("commit anchored freshness = %d, want 1", row.TouchingCommits)
		}
	}
}

func TestDocsFreshnessStaleOnlyWhenDescribedPathChangedAfterStamp(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "pkg/code.go", "package pkg\n")
	writeDoc(t, root, "other/code.go", "package other\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	c := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
	writeDoc(t, root, "docs/system/touched.md", "---\nsubject: touched\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-18 @ "+c+"'\n---\n# Touched\n")
	writeDoc(t, root, "docs/system/untouched.md", "---\nsubject: untouched\npart_of: overview\ndescribes: [other]\nlast_verified: '2026-08-18 @ "+c+"'\n---\n# Untouched\n")
	writeDoc(t, root, "docs/system/nodescribes.md", "---\nsubject: nodescribes\npart_of: overview\nlast_verified: '2026-08-18 @ "+c+"'\n---\n# None\n")
	writeDoc(t, root, "pkg/code.go", "package pkg\n\nvar X = 1\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "docs and code")

	rows, err := DocsFreshness(root)
	if err != nil {
		t.Fatalf("DocsFreshness() error = %v", err)
	}
	want := map[string]bool{"touched": true, "untouched": false, "nodescribes": false, "overview": false}
	for _, row := range rows {
		if stale, ok := want[row.Document.Subject]; ok && row.Stale != stale {
			t.Fatalf("%s stale = %v, want %v", row.Document.Subject, row.Stale, stale)
		}
	}
}

func TestStampDocumentUpdatesExistingHeaderAndRefusesMissing(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/cli.md", "---\nsubject: cli\npart_of: overview\n---\n# CLI\n")
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	path, err := StampDocument(root, "cli", "2026-08-18")
	if err != nil || path != "docs/system/cli.md" {
		t.Fatalf("StampDocument() = %q, %v", path, err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	commit := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
	if err != nil || !strings.Contains(string(raw), "last_verified: 2026-08-18 @ "+commit) {
		t.Fatalf("stamp missing from document: %s", raw)
	}
	if _, err := StampDocument(root, "missing", "2026-08-18"); err == nil {
		t.Fatal("StampDocument() accepted an unknown subject")
	}
}

func TestWriteDocumentFileRejectsSymlinkedRootParentAndLeaf(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		realRoot := t.TempDir()
		linkedRoot := filepath.Join(t.TempDir(), "repo")
		if err := os.Symlink(realRoot, linkedRoot); err != nil {
			t.Fatal(err)
		}
		if err := WriteDocumentFile(linkedRoot, "docs/system/cli.md", []byte("changed\n")); err == nil || !strings.Contains(err.Error(), "real directory") {
			t.Fatalf("symlinked root accepted: %v", err)
		}
	})

	t.Run("parent", func(t *testing.T) {
		realRoot := t.TempDir()
		external := t.TempDir()
		externalFile := filepath.Join(external, "cli.md")
		if err := os.WriteFile(externalFile, []byte("outside\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(realRoot, "docs", "system", "linked")
		if err := os.MkdirAll(filepath.Dir(parent), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, parent); err != nil {
			t.Fatal(err)
		}
		if err := WriteDocumentFile(realRoot, "docs/system/linked/cli.md", []byte("changed\n")); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlinked parent accepted: %v", err)
		}
		if got, err := os.ReadFile(externalFile); err != nil || string(got) != "outside\n" {
			t.Fatalf("external parent target changed: %q (%v)", got, err)
		}
	})

	t.Run("leaf", func(t *testing.T) {
		realRoot := t.TempDir()
		external := filepath.Join(t.TempDir(), "cli.md")
		if err := os.WriteFile(external, []byte("outside\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		leaf := filepath.Join(realRoot, "docs", "system", "cli.md")
		if err := os.MkdirAll(filepath.Dir(leaf), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, leaf); err != nil {
			t.Fatal(err)
		}
		if err := WriteDocumentFile(realRoot, "docs/system/cli.md", []byte("changed\n")); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlinked leaf accepted: %v", err)
		}
		if got, err := os.ReadFile(external); err != nil || string(got) != "outside\n" {
			t.Fatalf("external leaf target changed: %q (%v)", got, err)
		}
	})
}

func TestDocsCoverageGapsListsUnclaimedTopLevelAreas(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "docs/system/pkg.md", "---\nsubject: pkg\npart_of: overview\ndescribes: [pkg]\n---\n# Pkg\n")
	writeDoc(t, root, "pkg/code.go", "package pkg\n")
	writeDoc(t, root, "internal/code.go", "package internal\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	gaps, err := DocsCoverageGaps(root)
	if err != nil {
		t.Fatalf("DocsCoverageGaps() error = %v", err)
	}
	if len(gaps) != 1 || gaps[0] != "internal" {
		t.Fatalf("coverage gaps = %#v, want [internal]", gaps)
	}
}

func gitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestDocsFreshnessRebasedAwayStampIsNeverVerified(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	writeDoc(t, root, "docs/system/00-overview.md", "---\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "pkg/code.go", "package pkg\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "seed")
	runGit(t, root, "checkout", "-b", "side")
	writeDoc(t, root, "pkg/side.go", "package pkg\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "side")
	orphan := strings.TrimSpace(gitOutput(t, root, "rev-parse", "HEAD"))
	runGit(t, root, "checkout", "main")
	writeDoc(t, root, "docs/system/doc.md", "---\nsubject: doc\npart_of: overview\ndescribes: [pkg]\nlast_verified: '2026-08-18 @ "+orphan+"'\n---\n# Doc\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "doc")

	rows, err := DocsFreshness(root)
	if err != nil {
		t.Fatalf("DocsFreshness() error = %v", err)
	}
	for _, row := range rows {
		if row.Document.Subject == "doc" && !row.NeverVerified {
			t.Fatalf("stamp on a non-ancestor commit must be never verified: %#v", row)
		}
	}
}
