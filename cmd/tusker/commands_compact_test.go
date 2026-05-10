package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactDryRunAndWriteTrimDisposableScaffolding(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Old noisy task", "risk": "low", "size": "s"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	taskPath := filepath.Join(vault, "epics", "APP", "APP-T-0001.md")
	original := mustReadIndexTest(t, taskPath)
	noisy := strings.Replace(original, "created:", "ai_tools: []\nassignee: \"\"\ndomains: []\ndoc_nodes: []\nblocked_by: []\nblock_reason: \"\"\nblocks: []\ncreated:", 1)
	noisy += "\n## Execution plan\n\n1.\n\n## Work log\n\n- 2026-05-10 — tusker — task created\n"
	if err := writeText(taskPath, noisy); err != nil {
		t.Fatal(err)
	}

	dryRun := captureStdout(t, func() {
		if err := compactCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, dryRun, "would compact")
	assertContainsIndexTest(t, dryRun, "dry run")
	if afterDryRun := mustReadIndexTest(t, taskPath); !strings.Contains(afterDryRun, "## Work log") {
		t.Fatal("dry run should not write the compacted note")
	}

	writeOutput := captureStdout(t, func() {
		if err := compactCmd(Args{"vault": vault, "id": "APP-T-0001", "write": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, writeOutput, "compacted")
	assertContainsIndexTest(t, writeOutput, "fields:")
	assertContainsIndexTest(t, writeOutput, "sections:")

	compacted := mustReadIndexTest(t, taskPath)
	for _, removed := range []string{"ai_tools: []", "assignee: \"\"", "domains: []", "doc_nodes: []", "blocked_by: []", "block_reason: \"\"", "blocks: []", "## Execution plan", "## Work log"} {
		assertNotContainsIndexTest(t, compacted, removed)
	}
	assertContainsIndexTest(t, compacted, "## Evidence")
}
