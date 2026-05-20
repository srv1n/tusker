package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestReindexKeepsReadmeEpicOnlyAndEpicTaskStackOpenOnly(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Active task", "status": "active", "risk": "medium"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closed task", "status": "done", "risk": "low"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	if err := reindex(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	readme := mustReadIndexTest(t, filepath.Join(vault, "README.md"))
	assertContainsIndexTest(t, readme, "Build the app foundation.")
	assertContainsIndexTest(t, readme, "tusker list --epic APP --type task --open")
	assertNotContainsIndexTest(t, readme, "Active task")
	assertNotContainsIndexTest(t, readme, "Closed task")

	epic := mustReadIndexTest(t, filepath.Join(vault, "epics", "APP", "APP.md"))
	assertContainsIndexTest(t, epic, "Open tasks only")
	assertContainsIndexTest(t, epic, "Active task")
	assertNotContainsIndexTest(t, epic, "Closed task")
}

func TestListOpenDrillsIntoOneEpicWithoutClosedTasks(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Active task", "status": "active"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closed task", "status": "done"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	output := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "epic": "APP", "type": "task", "open": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "Active task")
	assertNotContainsIndexTest(t, output, "Closed task")
	assertNotContainsIndexTest(t, output, "App foundation")
}

func TestListOpenDrillsIntoV7EpicWithKindAndScalarEpic(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Build the app foundation.", "v7": "true"}, newV7Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Ready V7 task", "status": "ready", "v7": "true"}, newV7Task)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Closed V7 task", "status": "done", "v7": "true"}, newV7Task)

	output := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "epic": "APP", "type": "task", "open": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "Ready V7 task")
	assertContainsIndexTest(t, output, "APP-T-0001      task")
	assertNotContainsIndexTest(t, output, "Closed V7 task")
	assertNotContainsIndexTest(t, output, "App V7")

	jsonOutput := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "epic": "APP", "type": "task", "open": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0]["type"] != "task" {
		t.Fatalf("expected V7 task JSON type to be task, got %#v", payload.Items)
	}

	epicOutput := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "type": "epic"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, epicOutput, "APP")
	assertContainsIndexTest(t, epicOutput, "open:1")
	assertContainsIndexTest(t, epicOutput, "done:1")
}

func TestListLimitCapsProgressiveDrillDown(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App foundation", "summary": "Build the app foundation."}, newV5Epic)
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "First active task", "status": "active"}, func(args Args) error {
		return newV5Task(args, "feature")
	})
	mustRunIndexTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Second active task", "status": "active"}, func(args Args) error {
		return newV5Task(args, "feature")
	})

	output := captureStdout(t, func() {
		if err := listCmd(Args{"vault": vault, "epic": "APP", "type": "task", "open": "true", "limit": "1"}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "First active task")
	assertNotContainsIndexTest(t, output, "Second active task")
	assertContainsIndexTest(t, output, "...and 1 more")
}

func mustRunIndexTest(t *testing.T, args Args, fn func(Args) error) {
	t.Helper()
	if err := fn(args); err != nil {
		t.Fatal(err)
	}
}

func mustReadIndexTest(t *testing.T, path string) string {
	t.Helper()
	content, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertContainsIndexTest(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q:\n%s", needle, haystack)
	}
}

func assertNotContainsIndexTest(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected output not to contain %q:\n%s", needle, haystack)
	}
}
