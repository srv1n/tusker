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

func TestUpdateHelpExplainsSkillRefresh(t *testing.T) {
	output := captureStdout(t, printUpdateHelp)
	for _, expected := range []string{
		"tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--repo-only] [--skill-mode copy|symlink] [--source <checkout>] [--json]",
		"refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude",
		"with --repo, refreshes the repo-local .agents skill install",
		"installs default to symlink mode",
		"--source points symlink mode at a canonical Tusker checkout",
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
		{[]string{"tusker", "daemon", "resume"}, "daemon resume", ""},
		{[]string{"tusker", "projects", "add", "--repo", "."}, "projects add", ""},
		{[]string{"tusker", "runs", "inspect", "ORC-T-0018"}, "runs inspect", "ORC-T-0018"},
		{[]string{"tusker", "work", "start", "ORC-T-0018", "--by", "agent:codex"}, "work start", "ORC-T-0018"},
		{[]string{"tusker", "work", "status", "ORC-T-0018"}, "work status", "ORC-T-0018"},
		{[]string{"tusker", "work", "heartbeat", "ORC-T-0018"}, "work heartbeat", "ORC-T-0018"},
		{[]string{"tusker", "work", "submit", "ORC-T-0018"}, "work submit", "ORC-T-0018"},
		{[]string{"tusker", "work", "fail", "ORC-T-0018"}, "work fail", "ORC-T-0018"},
		{[]string{"tusker", "work", "release", "ORC-T-0018"}, "work release", "ORC-T-0018"},
		{[]string{"tusker", "config", "resolve", "automation.profiles", "--json"}, "config resolve", "automation.profiles"},
		{[]string{"tusker", "help", "runs", "events"}, "help runs events", ""},
		{[]string{"tusker", "help", "work", "start"}, "help work start", ""},
		{[]string{"tusker", "help", "config", "resolve"}, "help config resolve", ""},
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
		"--width <cols>",
		"compact epic table",
		"drops low-value columns",
		"without dumping note bodies",
		"tusker list --vault ./.tusker --type epic",
		"tusker list --vault ./.tusker --epic ORC --type task --open",
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
