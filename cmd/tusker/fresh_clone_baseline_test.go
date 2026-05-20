package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshCloneBaselineHasModuleAndEmbeddedSkillAssets(t *testing.T) {
	repoRoot := repoRootForFreshCloneTest(t)
	for _, rel := range []string{
		"go.mod",
		"go.sum",
		"skill/SKILL.md",
		"skill/LICENSE",
		"skill/references/COMMANDS.md",
		"skill/references/WORKFLOW.md",
		"tusker/docs/agents/use-tusker.md",
		"tusker/docs/spec/tusker-v7-repo-local-work-tracker-spec.md",
	} {
		if !fileExists(filepath.Join(repoRoot, filepath.FromSlash(rel))) {
			t.Fatalf("fresh clone baseline missing required file: %s", rel)
		}
	}
}

func TestFreshCloneBaselineCLIRunsHelpAndV7Init(t *testing.T) {
	repoRoot := repoRootForFreshCloneTest(t)
	help := exec.Command("go", "run", "./cmd/tusker", "--help")
	help.Dir = repoRoot
	output, err := help.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./cmd/tusker --help failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "V7 repo-local work tracking") {
		t.Fatalf("help output does not advertise V7 default:\n%s", output)
	}

	temp := t.TempDir()
	vault := filepath.Join(temp, "tusker")
	init := exec.Command("go", "run", "./cmd/tusker", "init", "--vault", vault, "--yes", "--vault-only", "--no-mount")
	init.Dir = repoRoot
	output, err = init.CombinedOutput()
	if err != nil {
		t.Fatalf("go run ./cmd/tusker init failed: %v\n%s", err, output)
	}
	for _, rel := range []string{"SKILL.md", "work/tasks", "work/gates", "knowledge/domains/project/INDEX.md", "evidence", "attempts"} {
		if !fileExists(filepath.Join(vault, filepath.FromSlash(rel))) {
			t.Fatalf("V7 init missing %s", rel)
		}
	}
	if fileExists(filepath.Join(vault, "_config", "docs-map.yaml")) {
		t.Fatal("V7 init must not create legacy docs-map")
	}
}

func TestFreshCloneBaselineIgnoresTaskAttachmentGoSources(t *testing.T) {
	repoRoot := repoRootForFreshCloneTest(t)
	attachmentsRoot := filepath.Join(repoRoot, "tusker", "Attachments")
	if !fileExists(attachmentsRoot) {
		return
	}

	var buildable []string
	if err := filepath.WalkDir(attachmentsRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := strings.TrimLeft(string(raw), "\ufeff \t\r\n")
		if !strings.HasPrefix(text, "//go:build ignore\n") {
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				rel = path
			}
			buildable = append(buildable, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(buildable) > 0 {
		t.Fatalf("task attachment Go files must use //go:build ignore so go test ./... stays fresh-clone safe: %s", strings.Join(buildable, ", "))
	}
}

func repoRootForFreshCloneTest(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if fileExists(filepath.Join(current, "go.mod")) && fileExists(filepath.Join(current, "skill", "bundle.go")) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("could not find repository root")
		}
		current = parent
	}
}
