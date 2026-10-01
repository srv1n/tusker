package main

import (
	"io"
	"os"
	"strings"
	"sync"
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
		"tusker install [--bin-dir <path>] [--bin] [--no-bin] [--all-user-skills] [--codex-user] [--claude-user] [--repo <path>] [--repo-only] [--skill-mode copy|symlink] [--source <checkout>] [--json]",
		"refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude only",
		"with --all-user-skills",
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

func TestWaveCreateHelpShowsAuthoringSchema(t *testing.T) {
	output := captureStdout(t, func() { printCommandHelp("wave create") })
	for _, expected := range []string{"tusker.wave-authoring/v1", "human_actions:", "owned_paths:", "kind:", "command:", "ledger:"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("wave create help missing %q:\n%s", expected, output)
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
	mainHelp := captureStdout(t, func() { printHelp(false) })
	for _, expected := range []string{"daemon", "projects", "runs", "refresh", "search", "show"} {
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

// stdoutCaptureMu serializes captures: os.Stdout is process-wide, so two
// overlapping captures in parallel tests would swap each other's pipes.
var stdoutCaptureMu sync.Mutex

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()
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

func TestParseCLIJoinsOnlyTableSubcommands(t *testing.T) {
	cases := []struct {
		argv    []string
		command string
		pos0    string
	}{
		{[]string{"tusker", "daemon", "status"}, "daemon status", ""},
		{[]string{"tusker", "runs", "inspect", "ORC-T-0018"}, "runs inspect", "ORC-T-0018"},
		{[]string{"tusker", "work", "start", "ORC-T-0018", "--by", "agent:codex"}, "work start", "ORC-T-0018"},
		{[]string{"tusker", "config", "resolve", "automation.profiles", "--json"}, "config resolve", "automation.profiles"},
		{[]string{"tusker", "gate", "satisfy", "G-0001"}, "gate", "satisfy"},
		{[]string{"tusker", "hook", "install"}, "hook", "install"},
		{[]string{"tusker", "help", "runs", "events"}, "help", "runs"},
	}
	for _, tc := range cases {
		command, args := parseCLI(tc.argv)
		assertEqual(t, tc.command, command, "parsed command")
		assertEqual(t, tc.pos0, args.String("_pos0"), "first positional")
	}
}

// Every command must answer --help from the table without opening a vault,
// and carry the use/avoid guidance agents grep for.
func TestEveryCommandHasInertGuidedHelp(t *testing.T) {
	groups := map[string]bool{}
	for _, g := range cliGroups {
		groups[g.Name] = true
	}
	for _, c := range cliCommands {
		if !groups[c.Group] {
			t.Errorf("%s: unknown group %q", c.Name, c.Group)
		}
		if c.Summary == "" || !strings.HasPrefix(c.Use, "Use ") || c.Avoid == "" {
			t.Errorf("%s: missing summary or use/avoid guidance: %#v", c.Name, c)
		}
		output := captureStdout(t, func() {
			if code, err := runInner(c.Name, Args{"help": "true"}); code != 0 || err != nil {
				t.Errorf("%s --help: code=%d err=%v", c.Name, code, err)
			}
		})
		if !strings.Contains(output, "tusker "+c.Name+" — ") {
			t.Errorf("%s --help is not command-specific:\n%s", c.Name, output)
		}
	}
}

func TestMainHelpListsEveryVisibleCommandOnOneGreppableLine(t *testing.T) {
	output := captureStdout(t, func() { printHelp(false) })
	for _, c := range cliCommands {
		has := strings.Contains(output, "tusker "+c.Name+" ")
		if has == c.Hidden {
			t.Errorf("%s: hidden=%t but listed=%t", c.Name, c.Hidden, has)
		}
	}
	if strings.Contains(output, "\n  hook ") || printCommandHelp("hook") {
		t.Fatal("removed hook command is still advertised")
	}
}

func TestUnknownAndRemovedCommandsPointToReplacements(t *testing.T) {
	if _, err := runInner("claim", Args{"json": "true"}); err == nil || !strings.Contains(errorToIssue(err).Hint, "tusker work start") {
		t.Fatalf("removed claim did not name its replacement: %v", err)
	}
	if got := suggestCommands("runs inspct"); len(got) == 0 || got[0] != "runs inspect" {
		t.Fatalf("suggestions for a typo = %v", got)
	}
	if code, _ := runInner("runs bogus", Args{}); code == 0 {
		t.Fatal("unknown subcommand of a family succeeded")
	}
}
