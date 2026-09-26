package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewEpicRejectsUnresolvableSpecRefWithReason(t *testing.T) {
	vault := v7DirectTestVault(t)
	if err := writeText(filepath.Join(v7RepoRoot(vault), "docs", "notes.md"), "# Notes\n\nNo front matter.\n"); err != nil {
		t.Fatal(err)
	}
	err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "BAD", "title": "Bad", "summary": "Bad refs.", "spec-refs": "docs/notes.md"})
	if err == nil {
		t.Fatal("expected new epic to reject an unresolvable spec_ref")
	}
	issue := errorToIssue(err)
	if !strings.Contains(issue.Message, "spec_ref does not resolve: docs/notes.md (") {
		t.Fatalf("missing reason in message: %s", issue.Message)
	}
	if !strings.Contains(issue.Hint, "kind: spec|proposal|decision") {
		t.Fatalf("missing front matter hint: %s", issue.Hint)
	}
	if fileExists(filepath.Join(vault, "work", "epics", "BAD.md")) {
		t.Fatal("rejected epic was written")
	}
}

func TestWaveCreateSpecRefIssueCarriesReason(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
title: Bad ref wave
outcome: Refused.
spec_refs:
  - docs/missing.md
tasks:
  - key: only
    title: Only
    work_level: standard
    body: "# Only\n\nOnly body.\n"
`
	path := directAuthoringBodyPath(t, vault, "badref.yaml", request)
	err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "file": path, "request-key": "badref-v1"})
	if err == nil {
		t.Fatal("expected wave create to refuse an unresolvable spec_ref")
	}
	text := errorToIssue(err).Message
	if !strings.Contains(text, "spec_ref does not resolve: docs/missing.md (missing target") {
		t.Fatalf("wave create issue lacks the reason: %s", text)
	}
}

func TestWaveCreateWarnsAboutContractsReviewWillRefuse(t *testing.T) {
	vault := v7DirectTestVault(t)
	request := `schema: tusker.wave-authoring/v1
title: Manual proof wave
outcome: Warned.
tasks:
  - key: only
    title: Only
    work_level: standard
    body: |
      # Only

      ## Acceptance

      | ID | Outcome | Proof |
      | --- | --- | --- |
      | A1 | It works. | Verification A1 |

      ## Verification

      | Covers | Check | Result | Notes |
      | --- | --- | --- | --- |
      | A1 | manual: click the button | pending | |
`
	path := directAuthoringBodyPath(t, vault, "manual.yaml", request)
	var err error
	output := captureStdout(t, func() {
		err = waveV7CreateCmd(Args{"vault": vault, "file": path, "request-key": "manual-v1"})
	})
	if err != nil {
		t.Fatalf("warnings must not refuse creation: %v", err)
	}
	for _, expected := range []string{"warning: ", "MEMBER_CONTRACT_INVALID", "acceptance missing planned proof: A1", "manual proof:", "--check` will refuse Start"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output missing %q:\n%s", expected, output)
		}
	}
}
