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
		"skills/tusker/SKILL.md",
		"skills/tusker/LICENSE",
		"skills/tusker/references/COMMANDS.md",
		"skills/tusker/references/WORKFLOW.md",
		"skills/tusker/references/ORCHESTRATION.md",
		".tusker/SKILL.md",
		".tusker/WORKFLOW.md",
		".tusker/knowledge/domains/project/CANON.md",
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
	for _, rel := range []string{
		"SKILL.md",
		"Dashboard.md",
		"work/tasks",
		"work/gates",
		"knowledge/domains/project/INDEX.md",
		"evidence",
		"attempts",
		"dashboards/human-actions.md",
		"dashboards/agent-ready.md",
		"_generated/bases/tasks.base",
		"_generated/bases/epics.base",
		"_generated/indexes/tasks.json",
		"_generated/indexes/summary.json",
	} {
		if !fileExists(filepath.Join(vault, filepath.FromSlash(rel))) {
			t.Fatalf("V7 init missing %s", rel)
		}
	}
	dashboard, err := os.ReadFile(filepath.Join(vault, "Dashboard.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(dashboard), "![[_generated/bases/tasks.base#Agent Ready]]") {
		t.Fatalf("V7 init Dashboard.md missing generated Bases embeds:\n%s", dashboard)
	}
	if strings.Contains(string(dashboard), "Legacy dashboard generation was removed") {
		t.Fatalf("V7 init Dashboard.md still contains legacy placeholder:\n%s", dashboard)
	}
	if strings.Contains(string(dashboard), "## Docs catalog") || strings.Contains(string(dashboard), "<!-- tusker:dashboard-runs:begin -->") {
		t.Fatalf("V7 init Dashboard.md contains legacy dashboard scaffolding:\n%s", dashboard)
	}
	if fileExists(filepath.Join(vault, "_config", "docs-map.yaml")) {
		t.Fatal("V7 init must not create legacy docs-map")
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.AutomationEnabled {
		t.Fatal("fresh V7 init must keep daemon automation opt-in")
	}
}

func TestFreshCloneBaselineIgnoresTaskAttachmentGoSources(t *testing.T) {
	repoRoot := repoRootForFreshCloneTest(t)
	attachmentsRoot := filepath.Join(repoRoot, ".tusker", "Attachments")
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
		if fileExists(filepath.Join(current, "go.mod")) && fileExists(filepath.Join(current, "skills", "tusker", "bundle.go")) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("could not find repository root")
		}
		current = parent
	}
}
