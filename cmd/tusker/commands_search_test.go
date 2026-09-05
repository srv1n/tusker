package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchSkipsAttachmentsAndGeneratedFiles(t *testing.T) {
	t.Parallel()
	vault := t.TempDir()
	writeSearchFixture(t, vault, "epics/APP/APP.md", `---
schema: "tusker.epic/v5"
id: "APP"
title: "App foundation"
type: "epic"
status: "active"
---

# APP
`)
	writeSearchFixture(t, vault, "epics/APP/APP-T-0001.md", `---
schema: "tusker.task/v5"
id: "APP-T-0001"
title: "Add durable search"
type: "task"
kind: "feature"
epic: "APP"
status: "ready"
---

# APP-T-0001

This task adds a cheap tracker search path for agents.
`)
	writeSearchFixture(t, vault, "Attachments/APP-T-0001/raw.log", "search path should not match from attachments")
	writeSearchFixture(t, vault, "_system/generated/tasks.index.json", `{"title":"search path should not match from generated indexes"}`)

	output := captureStdout(t, func() {
		if err := searchCmd(Args{"vault": vault, "query": "search path"}); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(output, "APP-T-0001") {
		t.Fatalf("expected task match:\n%s", output)
	}
	if strings.Contains(output, "Attachments") || strings.Contains(output, "_system") {
		t.Fatalf("search leaked skipped paths:\n%s", output)
	}
}

func TestSearchFiltersByEpicStatusAndType(t *testing.T) {
	t.Parallel()
	vault := t.TempDir()
	writeSearchFixture(t, vault, "epics/APP/APP.md", `---
schema: "tusker.epic/v5"
id: "APP"
title: "App foundation"
type: "epic"
status: "active"
---

# APP
`)
	writeSearchFixture(t, vault, "epics/APP/APP-T-0001.md", `---
schema: "tusker.task/v5"
id: "APP-T-0001"
title: "Ready search task"
type: "task"
kind: "feature"
epic: "APP"
status: "ready"
---

search needle
`)
	writeSearchFixture(t, vault, "epics/APP/APP-T-0002.md", `---
schema: "tusker.task/v5"
id: "APP-T-0002"
title: "Done search task"
type: "task"
kind: "feature"
epic: "APP"
status: "done"
---

search needle
`)

	results := searchNotes(mustListSearchNotes(t, vault), "search needle", searchFilters{
		Type:   "task",
		Epic:   "APP",
		Status: "ready",
		Limit:  20,
	})
	if len(results) != 1 {
		t.Fatalf("expected one filtered result, got %#v", results)
	}
	assertEqual(t, "APP-T-0001", results[0].ID, "filtered search result")
}

func TestSearchUsesAllPositionalQueryTerms(t *testing.T) {
	t.Parallel()
	vault := t.TempDir()
	writeSearchFixture(t, vault, "epics/APP/APP-T-0001.md", `---
schema: "tusker.task/v5"
id: "APP-T-0001"
title: "Durable tracker lookup"
type: "task"
kind: "feature"
epic: "APP"
status: "ready"
---

`)

	output := captureStdout(t, func() {
		if err := searchCmd(Args{"vault": vault, "_pos": "durable\ntracker"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "APP-T-0001") {
		t.Fatalf("expected unquoted multi-word positional query to match:\n%s", output)
	}
}

func TestSearchHelpExplainsBoundedScope(t *testing.T) {
	t.Parallel()
	output := captureStdout(t, printSearchHelp)
	for _, expected := range []string{
		"tusker search <text>",
		"without reading generated indexes",
		"Attachments",
		"--limit <n>",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("search help missing %q:\n%s", expected, output)
		}
	}
}

func writeSearchFixture(t *testing.T, vault, rel, content string) {
	t.Helper()
	path := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustListSearchNotes(t *testing.T, vault string) []Note {
	t.Helper()
	notes, err := listAllNotes(vault)
	if err != nil {
		t.Fatal(err)
	}
	return notes
}
