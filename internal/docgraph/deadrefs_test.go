package docgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deadRefDoc(subject, status, describes, body string) string {
	return "---\nread_when: \"Reading fixture.\"\nskip_when: \"Reading elsewhere.\"\nsubject: " + subject +
		"\npart_of: overview\nstatus: " + status + "\ndescribes: [" + describes + "]\n---\n" + body
}

func deadRefCodes(t *testing.T, root, path string) []string {
	t.Helper()
	issues, err := ValidateRepository(root)
	if err != nil {
		t.Fatalf("ValidateRepository() error = %v", err)
	}
	var codes []string
	for _, issue := range issues {
		if issue.Path == path && strings.HasPrefix(issue.Code, "DOC_") && strings.HasSuffix(issue.Code, "_DEAD") {
			codes = append(codes, issue.Code+" "+issue.Message)
		}
	}
	return codes
}

func TestDeadRefDescribes(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"r\"\nskip_when: \"s\"\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "cmd/tusker/main.go", "package main\n")
	writeDoc(t, root, "docs/system/ok.md", deadRefDoc("ok", "current", "cmd/tusker/main.go, cmd/tusker/*.go, cmd/**", "# OK\n"))
	writeDoc(t, root, "docs/system/dead.md", deadRefDoc("dead", "current", "cmd/gone.go", "# Dead\n"))
	writeDoc(t, root, "docs/system/glob.md", deadRefDoc("glob", "canonical", "internal/*.go", "# Glob\n"))

	if got := deadRefCodes(t, root, "docs/system/ok.md"); len(got) != 0 {
		t.Fatalf("live describes flagged: %v", got)
	}
	for _, path := range []string{"docs/system/dead.md", "docs/system/glob.md"} {
		got := deadRefCodes(t, root, path)
		if len(got) != 1 || !strings.HasPrefix(got[0], "DOC_DESCRIBES_DEAD") {
			t.Fatalf("%s: want one DOC_DESCRIBES_DEAD, got %v", path, got)
		}
	}
}

func TestDeadRefCodePaths(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"r\"\nskip_when: \"s\"\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "cmd/tusker/main.go", "package main\n")
	body := "# Doc\n\nSee `cmd/tusker/main.go:12`, `cmd/tusker/main.go#L4`, `cmd/tusker/*.go`, `https://x.io/a`,\n" +
		"`--flag a/b`, `$HOME/x`, `a b/c`, `/api/x`, `~/.cfg/x`, `a/<id>/`, `K=a/b`, `tusker.task/v7`, `nope/missing.go`.\n\n```\n`fenced/missing.go`\n```\n"
	writeDoc(t, root, "docs/system/current.md", deadRefDoc("current", "current", "", body))
	writeDoc(t, root, "docs/system/proposals/plan.md", "---\nkind: proposal\nread_when: \"r\"\nskip_when: \"s\"\nsubject: plan\npart_of: overview\nstatus: proposed\n---\n`nope/missing.go`\n")
	writeDoc(t, root, "docs/system/old.md", "---\nread_when: \"r\"\nskip_when: \"s\"\nsubject: old\npart_of: overview\nstatus: superseded\nsuperseded_by: current\n---\n`nope/missing.go`\n")
	writeDoc(t, root, "docs/system/ignored.md", deadRefDoc("ignored", "current", "", "<!-- docs-check: ignore-path nope/missing.go -->\n`nope/missing.go`\n"))

	got := deadRefCodes(t, root, "docs/system/current.md")
	if len(got) != 1 || !strings.Contains(got[0], "DOC_CODE_PATH_DEAD") || !strings.Contains(got[0], "nope/missing.go") {
		t.Fatalf("want only nope/missing.go dead, got %v", got)
	}
	for _, path := range []string{"docs/system/proposals/plan.md", "docs/system/old.md", "docs/system/ignored.md"} {
		if got := deadRefCodes(t, root, path); len(got) != 0 {
			t.Fatalf("%s: exempt doc flagged: %v", path, got)
		}
	}
}

func TestDeadRefGlobsNeedFilesAndRecursiveStars(t *testing.T) {
	root := t.TempDir()
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"r\"\nskip_when: \"s\"\nsubject: overview\n---\n# Overview\n")
	writeDoc(t, root, "internal/a/b/foo.go", "package b\n")
	if err := os.MkdirAll(filepath.Join(root, "empty", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, root, "docs/system/live.md", deadRefDoc("live", "current", "internal/**/foo.go, internal/**", "# Live\n"))
	writeDoc(t, root, "docs/system/empty.md", deadRefDoc("empty", "current", "empty/**", "# Empty\n"))
	writeDoc(t, root, "docs/system/dironly.md", deadRefDoc("dironly", "current", "empt*", "# Dir only\n"))
	if got := deadRefCodes(t, root, "docs/system/live.md"); len(got) != 0 {
		t.Fatalf("recursive glob flagged: %v", got)
	}
	for _, path := range []string{"docs/system/empty.md", "docs/system/dironly.md"} {
		if got := deadRefCodes(t, root, path); len(got) != 1 {
			t.Fatalf("%s: glob matching no file should be dead, got %v", path, got)
		}
	}
}

func TestDeadRefRejectsPathsOutsideRepo(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	writeDoc(t, parent, "sibling/file.go", "package sibling\n")
	writeDoc(t, root, "docs/system/00-overview.md", "---\nread_when: \"r\"\nskip_when: \"s\"\nsubject: overview\n---\n# Overview\n")
	abs := filepath.ToSlash(filepath.Join(parent, "sibling", "file.go"))
	writeDoc(t, root, "docs/system/escape.md", deadRefDoc("escape", "current", "../sibling/file.go, '"+abs+"'", "`../sibling/file.go`\n"))
	got := strings.Join(deadRefCodes(t, root, "docs/system/escape.md"), "\n")
	if strings.Count(got, "DOC_DESCRIBES_DEAD") != 2 || strings.Count(got, "DOC_CODE_PATH_DEAD") != 1 {
		t.Fatalf("out-of-repo paths must be dead, got:\n%s", got)
	}
}

func TestCodePathRefsLongFenceSurvivesShorterFenceLine(t *testing.T) {
	body := "````md\n```\n`inside/a.go`\n```\n````\n`outside/b.go`\n~~~\n```\n`tilde/c.go`\n~~~\n"
	got := codePathRefs(body)
	if len(got) != 1 || got[0] != "outside/b.go" {
		t.Fatalf("codePathRefs() = %v, want [outside/b.go]", got)
	}
}

func TestCodePathRefsIgnoresOverIndentedFence(t *testing.T) {
	body := "    ~~~\n`missing/file.go`\n"
	if got := codePathRefs(body); len(got) != 1 || got[0] != "missing/file.go" {
		t.Fatalf("codePathRefs() = %v, want [missing/file.go]", got)
	}
}
