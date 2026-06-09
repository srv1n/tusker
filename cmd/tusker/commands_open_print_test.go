package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrintRendersPlainMarkdownSlices(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Build the app foundation.", "v7": "true"}, newV7Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Markdown task", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)

	full := captureStdout(t, func() {
		if err := printCmd(Args{"vault": vault, "id": "APP-T-0001", "plain": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, full, "# APP-T-0001")
	assertContainsIndexTest(t, full, "Markdown task")

	acceptance := captureStdout(t, func() {
		if err := printCmd(Args{"vault": vault, "id": "APP-T-0001", "acceptance": "true", "plain": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, acceptance, "## Acceptance")
	assertContainsIndexTest(t, acceptance, "| ID | Outcome | Proof |")

	capsule := captureStdout(t, func() {
		if err := printCmd(Args{"vault": vault, "id": "APP-T-0001", "capsule": "true", "plain": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, capsule, "APP-T-0001  task")
	assertContainsIndexTest(t, capsule, "Essence: Markdown task.")
}

func TestRenderTerminalMarkdownHonorsWidth(t *testing.T) {
	markdown := "# Long Markdown Heading For Width Detection\n\n- This bullet has enough text to prove Glamour receives the same terminal width as list tables.\n"

	rendered, err := renderTerminalMarkdown(markdown, Args{"width": "56", "style": "dark"})
	if err != nil {
		t.Fatal(err)
	}
	assertMaxLineWidthIndexTest(t, rendered, 56)
}

func TestOpenPrintsFileAndObsidianTargetsWithoutLaunching(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapV7Profile(vault, "v7"); err != nil {
		t.Fatal(err)
	}
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Build the app foundation.", "v7": "true"}, newV7Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Openable task", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	expectedPath := filepath.Join(vault, "work", "tasks", "APP-T-0001.md")

	pathOutput := captureStdout(t, func() {
		if err := openCmd(Args{"vault": vault, "id": "APP-T-0001", "path": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertEqual(t, expectedPath, strings.TrimSpace(pathOutput), "open --path output")

	obsidianOutput := captureStdout(t, func() {
		if err := openCmd(Args{"vault": vault, "id": "APP-T-0001", "obsidian": "true", "path": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, obsidianOutput, "obsidian://open?path=")
	assertContainsIndexTest(t, obsidianOutput, "APP-T-0001.md")
}

func TestOpenFallsBackToRegisteredProjectsOutsideRepo(t *testing.T) {
	tempRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(tempRoot, "state"))
	repo := filepath.Join(tempRoot, "repo")
	vault := filepath.Join(repo, "tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Build the app foundation.", "v7": "true"}, newV7Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Registered task", "risk": "low", "priority": "p2", "v7": "true"}, newV7Task)
	if err := projectsAddCmd(Args{"repo": repo, "vault": vault}); err != nil {
		t.Fatal(err)
	}

	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tempRoot, "outside")
	if err := ensureDir(outside); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	output := captureStdout(t, func() {
		if err := openCmd(Args{"id": "APP-T-0001", "path": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
}

func TestTerminalOutputWidthHonorsExplicitOverride(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	t.Setenv("TUSKER_WIDTH", "90")

	assertEqual(t, 52, terminalOutputWidth(Args{"width": "52"}), "explicit terminal width")
	assertEqual(t, 90, terminalOutputWidth(Args{}), "TUSKER_WIDTH fallback")
	assertEqual(t, 40, terminalWidthArg("12"), "minimum terminal width clamp")
}
