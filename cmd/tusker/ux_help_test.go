package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestMissingVaultGuidancePreservesStructuredContext(t *testing.T) {
	previousWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	expectedWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWD); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})

	_, err = resolveVaultPath(Args{}, false)
	if err == nil {
		t.Fatal("expected missing vault error")
	}
	issue := errorToIssue(err)
	if !strings.Contains(issue.Message, "No Tusker vault found.") {
		t.Fatalf("unexpected message: %s", issue.Message)
	}
	for _, expected := range []string{
		"tusker init --yes",
		"tusker --vault <path>",
	} {
		if !strings.Contains(issue.Message, expected) {
			t.Fatalf("message missing %q: %s", expected, issue.Message)
		}
	}
	context, ok := issue.Context.(map[string]any)
	if !ok {
		t.Fatalf("expected structured context, got %T", issue.Context)
	}
	assertEqual(t, "--vault", context["arg"], "missing vault context arg")
	assertEqual(t, expectedWD, context["cwd"], "missing vault context cwd")
	assertEqual(t, "tusker init --yes", context["repo_wiring"], "missing vault repo-wiring guidance")
	assertEqual(t, "--vault <path>", context["existing_vault"], "missing vault existing-vault guidance")
}

func TestNewHelpListsV7Defaults(t *testing.T) {
	output := captureStdout(t, printNewHelp)
	for _, expected := range []string{
		"tusker new epic",
		"tusker new task",
		"tusker new gate",
		"tusker new decision",
		"--priority p0|p1|p2|p3",
		"--evidence-required automated_test",
		"tusker legacy new",
		"Examples:",
		"tusker new task --vault ./tusker --epic APP",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("new help missing %q:\n%s", expected, output)
		}
	}
	for _, legacy := range []string{"tusker new bug", "tusker new doc", "--node <route>", "--publish-lane <lane>"} {
		if strings.Contains(output, legacy) {
			t.Fatalf("new help should quarantine legacy surface %q:\n%s", legacy, output)
		}
	}
}

func TestMainHelpQuarantinesLegacySurfaces(t *testing.T) {
	output := captureStdout(t, printHelp)
	for _, forbidden := range []string{
		"docs                ",
		"domain              ",
		"knowledge           ",
		"publish             ",
		"migrate             ",
		"verify              ",
		"V5 markdown",
		"V6 knowledge",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("main help should not advertise legacy surface %q:\n%s", forbidden, output)
		}
	}
	for _, expected := range []string{
		"Tusker - V7",
		"tusker.yaml",
		"tusker/work",
		"legacy",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("main help missing %q:\n%s", expected, output)
		}
	}
	legacyHelp := captureStdout(t, printLegacyHelp)
	for _, expected := range []string{"V5 tracker", "V6 knowledge", "docs-map", "tusker legacy new doc"} {
		if !strings.Contains(legacyHelp, expected) {
			t.Fatalf("legacy help missing %q:\n%s", expected, legacyHelp)
		}
	}
}

func TestUpdateHelpExplainsSkillRefresh(t *testing.T) {
	output := captureStdout(t, printUpdateHelp)
	for _, expected := range []string{
		"tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--repo-only] [--json]",
		"refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude",
		"with --repo, also refreshes repo-local .agents/.claude skill installs",
		"with --repo-only, skips user skill installs and touches only the repo",
		"from the currently running binary",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("update help missing %q:\n%s", expected, output)
		}
	}
}

func TestVaultHelpExplainsSharedObsidianMounts(t *testing.T) {
	output := captureStdout(t, printVaultHelp)
	for _, expected := range []string{
		"tusker vault set --path <obsidian-vault>",
		"tusker vault mount [--repo <path>] [--vault <path>] [--name <folder>]",
		"Link repo-local Tusker trackers into one shared Obsidian vault",
		"vault mount creates a symlink",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("vault help missing %q:\n%s", expected, output)
		}
	}
}

func TestRuntimeCommandParsingIncludesOperatorGroups(t *testing.T) {
	cases := []struct {
		argv    []string
		command string
		id      string
	}{
		{[]string{"tusker", "daemon", "status"}, "daemon status", ""},
		{[]string{"tusker", "daemon", "run", "--once"}, "daemon run", ""},
		{[]string{"tusker", "projects", "add", "--repo", "."}, "projects add", ""},
		{[]string{"tusker", "runs", "inspect", "ORC-T-0018"}, "runs inspect", "ORC-T-0018"},
		{[]string{"tusker", "help", "runs", "events"}, "help runs events", ""},
	}
	for _, tc := range cases {
		command, args := parseCLI(tc.argv)
		assertEqual(t, tc.command, command, "parsed command")
		if tc.id != "" {
			assertEqual(t, tc.id, args.String("_pos0"), "parsed positional id")
		}
	}
}

func TestParseCLIHandlesNoCommand(t *testing.T) {
	for _, argv := range [][]string{
		nil,
		{"tusker"},
	} {
		command, args := parseCLI(argv)
		assertEqual(t, "", command, "parsed command")
		assertEqual(t, 0, len(args), "parsed args")
	}
}

func TestRuntimeHelpExplainsOperatorSurface(t *testing.T) {
	for name, fn := range map[string]func(){
		"daemon":   printDaemonHelp,
		"projects": printProjectsHelp,
		"runs":     printRunsHelp,
		"refresh":  printRefreshHelp,
	} {
		output := captureStdout(t, fn)
		for _, expected := range []string{"Usage:", "Purpose:"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("%s help missing %q:\n%s", name, expected, output)
			}
		}
	}
	mainHelp := captureStdout(t, printHelp)
	for _, expected := range []string{"daemon", "projects", "runs", "refresh", "search", "show", "compact", "context"} {
		if !strings.Contains(mainHelp, expected) {
			t.Fatalf("main help missing %q:\n%s", expected, mainHelp)
		}
	}
}

func TestSearchHelpExplainsBoundedTrackerLookup(t *testing.T) {
	output := captureStdout(t, printSearchHelp)
	for _, expected := range []string{
		"tusker search <text>",
		"generated indexes",
		"Attachments",
		"--limit <n>",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("search help missing %q:\n%s", expected, output)
		}
	}
}

func TestShowHelpExplainsCapsuleFirst(t *testing.T) {
	output := captureStdout(t, printShowHelp)
	for _, expected := range []string{
		"tusker show <ID>",
		"--capsule",
		"--acceptance",
		"Defaults to --capsule",
		"small log tail",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("show help missing %q:\n%s", expected, output)
		}
	}
}

func TestCompactHelpExplainsDryRunFirst(t *testing.T) {
	output := captureStdout(t, printCompactHelp)
	for _, expected := range []string{
		"tusker compact <ID>",
		"--write",
		"Dry-run",
		"Execution plan",
		"Work log",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("compact help missing %q:\n%s", expected, output)
		}
	}
}

func TestContextHelpExplainsTranscriptAudit(t *testing.T) {
	output := captureStdout(t, printContextHelp)
	for _, expected := range []string{
		"tusker context audit",
		"codex-session.jsonl",
		"largest tool outputs",
		"--top <n>",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("context help missing %q:\n%s", expected, output)
		}
	}
}

func TestListHelpExplainsProgressiveDisclosure(t *testing.T) {
	output := captureStdout(t, printListHelp)
	for _, expected := range []string{
		"--open|--closed",
		"--limit <n>",
		"without reading note bodies",
		"tusker list --vault ./tusker --type epic",
		"tusker list --vault ./tusker --epic ORC --type task --open",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("list help missing %q:\n%s", expected, output)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	previous := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	type readResult struct {
		output []byte
		err    error
	}
	done := make(chan readResult, 1)
	go func() {
		output, err := io.ReadAll(reader)
		done <- readResult{output: output, err: err}
	}()
	os.Stdout = writer
	defer func() {
		os.Stdout = previous
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(result.output)
}
