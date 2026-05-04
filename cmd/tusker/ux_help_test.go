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

func TestNewHelpListsV5Commands(t *testing.T) {
	output := captureStdout(t, printNewHelp)
	for _, expected := range []string{
		"tusker new epic",
		"tusker new task",
		"tusker new bug",
		"tusker new doc",
		"--priority p0|p1|p2|p3",
		"--kind <type>",
		"--node <route>",
		"--publish-lane <lane>",
		"Examples:",
		"tusker new task --vault ./tusker --epic APP",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("new help missing %q:\n%s", expected, output)
		}
	}
}

func TestNewDocHelpListsPublicationFlags(t *testing.T) {
	output := captureStdout(t, printNewHelp)
	for _, expected := range []string{
		"tusker new doc",
		"--node <route>",
		"--publish-lane <lane>",
		"--no-publish",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("new doc help missing %q:\n%s", expected, output)
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
	for _, expected := range []string{"daemon", "projects", "runs", "refresh"} {
		if !strings.Contains(mainHelp, expected) {
			t.Fatalf("main help missing %q:\n%s", expected, mainHelp)
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
	os.Stdout = writer
	defer func() {
		os.Stdout = previous
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output)
}
