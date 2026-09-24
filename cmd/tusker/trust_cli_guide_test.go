package main

import (
	"os"
	"strings"
	"testing"
)

func TestTrustCliGuideUsesExecutorRecordedCommandProof(t *testing.T) {
	help := captureStdout(t, printV7Help)
	newHelp := captureStdout(t, printNewHelp)
	if strings.Contains(help, `--check "go test ./..." --result pass`) || !strings.Contains(help, `--check "command: go test ./..." --result pending`) {
		t.Fatalf("v7 help advertises a forgeable command-proof route:\n%s", help)
	}

	guide, err := os.ReadFile("../../.agents/skills/tusker/references/TRACK.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(guide), `--check "command: go test ./..." --result pass`) || !strings.Contains(string(guide), "executor records PASS/FAIL") {
		t.Fatalf("tracker guide does not preserve the executor boundary:\n%s", guide)
	}
	if !strings.Contains(newHelp, "--owned-paths <csv>") || !strings.Contains(newHelp, "--generated-outputs <csv>") ||
		!strings.Contains(string(guide), "--owned-paths") || !strings.Contains(string(guide), "--generated-outputs") {
		t.Fatalf("shipped guide does not expose single-task source scope:\n%s", help)
	}
	onboarding, err := os.ReadFile("../../.agents/skills/tusker/references/REPO_ONBOARDING.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`export TUSKER_STATE_ROOT="$PWD/.tusker/runtime-state"`,
		"automation.workspace.strategy --vault ./.tusker --json",
		"strategy: shared",
		"tusker config resolve automation.profiles --vault ./.tusker --json",
		"tusker docs new auth --kind proposal --vault ./.tusker",
		"tusker wave create --file <request.yaml> --request-key <stable-key> --vault ./.tusker",
	} {
		if !strings.Contains(string(onboarding), want) {
			t.Fatalf("onboarding omits actionable downstream remedy %q", want)
		}
	}
}
