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
		"tusker bootstrap --vault ./tusker",
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
	assertEqual(t, "tusker bootstrap --vault ./tusker", context["tracker_only"], "missing vault tracker-only guidance")
	assertEqual(t, "tusker init --yes", context["repo_wiring"], "missing vault repo-wiring guidance")
	assertEqual(t, "--vault <path>", context["existing_vault"], "missing vault existing-vault guidance")
}

func TestNewStoryHelpListsFormalIntakeFlags(t *testing.T) {
	output := captureStdout(t, printNewStoryHelp)
	for _, expected := range []string{
		"--priority p0|p1|p2|p3|icebox",
		"--assignee <name>",
		"--requester <name>",
		"--delegation execute|explore|escalate",
		"--change-type feature|refactor|migration|security|docs|chore|research|incident|bug",
		"--surfaces <csv>",
		"--ai-assistance none|light|moderate|heavy",
		"--ai-tools <csv>",
		"--ai-session-log <path>",
		"--due <date>",
		"--related <links>",
		"--blocks <links>",
		"--blocked-by <links>",
		"--tags <csv>",
		"Examples:",
		"tusker new-story --vault ./tusker --epic APP",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("new-story help missing %q:\n%s", expected, output)
		}
	}
}

func TestNewDocHelpListsPublicationFlags(t *testing.T) {
	output := captureStdout(t, printNewDocHelp)
	for _, expected := range []string{
		"--status draft|review|approved|published|archived",
		"--publish true|false",
		"--publish-path <route>",
		"--publish-description <text>",
		"--publish-order <n>",
		"--publish-section-title <text>",
		"--publish-url <url>",
		"--tags <csv>",
		"--publish true requires --status approved|published",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("new-doc help missing %q:\n%s", expected, output)
		}
	}
}

func TestUpdateHelpExplainsSkillRefresh(t *testing.T) {
	output := captureStdout(t, printUpdateHelp)
	for _, expected := range []string{
		"tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--json]",
		"refreshes existing user skills in ~/.agents, ~/.codex, and ~/.claude",
		"with --repo, also refreshes repo-local .agents/.claude skill installs",
		"from the currently running binary",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("update help missing %q:\n%s", expected, output)
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
