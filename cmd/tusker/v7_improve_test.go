package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestImproveScanShortlistsRepeatedWorkflowAndExistingCoverage(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	writeImproveTaskFixture(t, vault, "APP-T-0001", "Provider setup workflow", "2026-05-20")
	writeImproveTaskFixture(t, vault, "APP-T-0002", "Provider setup workflow", "2026-05-21")
	if err := writeText(filepath.Join(vault, "docs", "agents", "provider-setup-workflow.md"), "# Provider setup workflow\n"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := improveScanCmd(Args{"vault": vault, "date": "2026-05-25", "days": "30"}); err != nil {
			t.Fatal(err)
		}
	})

	for _, expected := range []string{
		"## Shortlist",
		"provider setup workflow",
		"2/medium",
		"extend existing",
		"Existing asset already covers this",
		"docs/agents/provider-setup-workflow.md",
		"Codex sessions: disabled - explicit opt-in required",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("improve scan output missing %q:\n%s", expected, output)
		}
	}
	if strings.Index(output, "## Shortlist") > strings.Index(output, "## Sources") {
		t.Fatalf("shortlist should appear before source details:\n%s", output)
	}
}

func TestImproveScanApplyCreatesNarrowAgentRunbookAndReport(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "tusker")
	writeImproveTaskFixture(t, vault, "APP-T-0001", "Provider setup workflow", "2026-05-20")
	writeImproveTaskFixture(t, vault, "APP-T-0002", "Provider setup workflow", "2026-05-21")

	output := captureStdout(t, func() {
		if err := improveScanCmd(Args{"vault": vault, "date": "2026-05-25", "days": "30", "apply": "true", "runner": "codex", "model": "gpt-5.3-codex-spark", "reasoning": "low"}); err != nil {
			t.Fatal(err)
		}
	})

	runbookPath := filepath.Join(vault, "docs", "agents", "provider-setup-workflow.md")
	assertExists(t, runbookPath)
	runbook, err := readText(runbookPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`id: "agents/provider-setup-workflow"`,
		"# Agent runbook: provider setup workflow",
		"## Supporting Evidence",
		"APP-T-0001",
		"APP-T-0002",
	} {
		if !strings.Contains(runbook, expected) {
			t.Fatalf("generated runbook missing %q:\n%s", expected, runbook)
		}
	}
	assertExists(t, filepath.Join(vault, "feedback", "improvements", "2026-05-25-improvement-scan.md"))
	if !strings.Contains(output, "Created docs/agents/provider-setup-workflow.md") {
		t.Fatalf("apply output did not report created runbook:\n%s", output)
	}
	if !strings.Contains(output, "runner=codex; model=gpt-5.3-codex-spark; reasoning=low") {
		t.Fatalf("apply output did not report runtime profile:\n%s", output)
	}
}

func TestImproveScanHelpDocumentsOptInSourcesAndApply(t *testing.T) {
	output := captureStdout(t, printImproveHelp)
	for _, expected := range []string{
		"tusker improve scan",
		"--apply",
		"--include-codex",
		"--include-claude",
		"--include-memories",
		"--include-chronicle",
		"raw private history is never read by default",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("improve help missing %q:\n%s", expected, output)
		}
	}
}

func writeImproveTaskFixture(t *testing.T, vault, id, title, date string) {
	t.Helper()
	content := strings.Join([]string{
		"---",
		`schema: "tusker.task/v7"`,
		`kind: "task"`,
		`id: "` + id + `"`,
		`project: "fixture"`,
		`title: "` + title + `"`,
		`epic: "APP"`,
		`status: "done"`,
		`readiness: "done"`,
		`priority: "p2"`,
		`risk: "medium"`,
		`size: "s"`,
		`proof_mode: "inline"`,
		`proof_status: "satisfied"`,
		`created_at: "` + date + `T00:00:00Z"`,
		`updated_at: "` + date + `T00:00:00Z"`,
		"---",
		"",
		"# " + id + " - " + title,
		"",
		"## Intent",
		"",
		"Repeatable provider setup workflow with stable inputs and a clear handoff output.",
		"",
	}, "\n")
	if err := writeText(filepath.Join(vault, "work", "tasks", id+".md"), content); err != nil {
		t.Fatal(err)
	}
}
