package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func logbookTestTask(t *testing.T, vault, id, title string) {
	t.Helper()
	if err := newV7Task(Args{
		"vault": vault, "quiet": "true", "epic": "APP", "id": id,
		"title": title, "risk": "low", "priority": "p2", "v7": "true",
	}); err != nil {
		t.Fatalf("create task %s: %v", id, err)
	}
}

// logbookSeedEvent writes an event stamped at a host-local time. The logbook
// buckets by the local calendar day, so tests build timestamps in time.Local
// to stay correct on any test host, not just UTC.
func logbookSeedEvent(t *testing.T, vault, object, objectKind, eventKind string, at time.Time, payload map[string]any) {
	t.Helper()
	id := newRecordID()
	event := map[string]any{
		"schema":      "tusker.event/v1",
		"id":          id,
		"project":     v7ProjectID(vault),
		"object":      object,
		"object_kind": objectKind,
		"event_kind":  eventKind,
		"actor":       "agent:test",
		"at":          at.Format(time.RFC3339),
	}
	if payload != nil {
		event["payload"] = payload
	}
	utc := at.UTC()
	name := object + "--" + utc.Format("20060102T150405Z") + "--" + id + ".json"
	path := filepath.Join(vault, "events", utc.Format("2006"), utc.Format("01"), name)
	if err := writeJSON(path, event); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

// logbookLocalTime returns noon on the given host-local date, offset by hours.
func logbookLocalTime(day time.Time, hour int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, time.Local)
}

func setStatus(t *testing.T, vault, id, status string) {
	t.Helper()
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "id": id, "status": status, "by": "agent:worker"}); err != nil {
		t.Fatalf("status %s -> %s: %v", id, status, err)
	}
}

func TestLogbookRenderAndWrite(t *testing.T) {
	vault := pickupV7TestVault(t)
	logbookTestTask(t, vault, "APP-T-0001", "Live stream board")
	day := "2026-06-15"
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "status_changed", logbookLocalTime(mustDay(t, day), 9), map[string]any{
		"from": "ready", "to": "review", "reason": "Board renders from runs and leases.",
	})

	output := captureStdout(t, func() {
		if err := logbookCmd(Args{"vault": vault, "date": day}); err != nil {
			t.Fatalf("logbook render: %v", err)
		}
	})
	assertContainsIndexTest(t, output, "# Morning Logbook — "+day)
	assertContainsIndexTest(t, output, "## What shipped")
	assertContainsIndexTest(t, output, "Live stream board")

	written := captureStdout(t, func() {
		if err := logbookCmd(Args{"vault": vault, "date": day, "write": "true"}); err != nil {
			t.Fatalf("logbook write: %v", err)
		}
	})
	path := filepath.Join(vault, "logbook", day+".md")
	if !fileExists(path) {
		t.Fatalf("expected %s to be written", path)
	}
	persisted := mustReadIndexTest(t, path)
	if strings.TrimSpace(persisted) != strings.TrimSpace(written) {
		t.Fatalf("written file and stdout diverged:\n---file---\n%s\n---stdout---\n%s", persisted, written)
	}

	// Default date resolves to today (host-local) without error.
	today := time.Now().Local().Format("2006-01-02")
	defaultOut := captureStdout(t, func() {
		if err := logbookCmd(Args{"vault": vault}); err != nil {
			t.Fatalf("logbook default date: %v", err)
		}
	})
	assertContainsIndexTest(t, defaultOut, "# Morning Logbook — "+today)
}

func TestLogbookComposesFromRecords(t *testing.T) {
	vault := pickupV7TestVault(t)
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "BGR", "title": "Batch gate repairs", "summary": "Repair work.", "v7": "true"}); err != nil {
		t.Fatalf("create BGR epic: %v", err)
	}
	logbookTestTask(t, vault, "APP-T-0001", "Claim conflict refusal")
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "BGR", "id": "BGR-T-0001",
		"title": "Repair board regression", "risk": "low", "priority": "p2", "v7": "true"}); err != nil {
		t.Fatalf("create repair task: %v", err)
	}
	day := "2026-06-15"
	d := mustDay(t, day)

	// Landed task with a one-line outcome.
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "status_changed", logbookLocalTime(d, 8), map[string]any{
		"from": "review", "to": "done", "reason": "Conflicting claims now refused at claim time.",
	})
	// Gate/proof results: two passes, one failure (a defect).
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "verification_added", logbookLocalTime(d, 9), map[string]any{
		"check": "command: go test ./cmd/tusker -run X", "result": "pass",
	})
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "verification_added", logbookLocalTime(d, 10), map[string]any{
		"check": "command: go test ./cmd/tusker -run Y", "result": "pass",
	})
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "verification_added", logbookLocalTime(d, 11), map[string]any{
		"check": "command: go test ./cmd/tusker -run Z", "result": "fail",
	})
	// Auto-created repair task on red.
	logbookSeedEvent(t, vault, "BGR-T-0001", "task", "created", logbookLocalTime(d, 12), nil)
	// Off-day event must be ignored.
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "status_changed", logbookLocalTime(d.AddDate(0, 0, -1), 8), map[string]any{
		"from": "backlog", "to": "ready",
	})

	logbook, err := buildTuskerLogbook(vault, d, time.Now().UTC())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(logbook.Shipped) != 1 || logbook.Shipped[0].TaskID != "APP-T-0001" {
		t.Fatalf("expected 1 shipped task APP-T-0001, got %+v", logbook.Shipped)
	}
	if logbook.Shipped[0].Outcome != "Conflicting claims now refused at claim time." {
		t.Fatalf("outcome not sourced from record: %q", logbook.Shipped[0].Outcome)
	}
	if logbook.Meaning.ChecksTotal != 3 || logbook.Meaning.ChecksPassed != 2 || logbook.Meaning.Defects != 1 {
		t.Fatalf("gate result counts wrong: %+v", logbook.Meaning)
	}
	if len(logbook.Meaning.Repairs) != 1 || logbook.Meaning.Repairs[0].TaskID != "BGR-T-0001" {
		t.Fatalf("expected repair task BGR-T-0001, got %+v", logbook.Meaning.Repairs)
	}
	// A task currently in review must land in the human section.
	setStatus(t, vault, "BGR-T-0001", "ready")
	setStatus(t, vault, "BGR-T-0001", "review")
	logbook, err = buildTuskerLogbook(vault, d, time.Now().UTC())
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	foundReview := false
	for _, need := range logbook.NeedsHuman {
		if need.Kind == "review" && strings.Contains(need.Label, "Repair board regression") {
			foundReview = true
		}
	}
	if !foundReview {
		t.Fatalf("expected review task in needs-human, got %+v", logbook.NeedsHuman)
	}
}

func TestLogbookPlainLanguageShape(t *testing.T) {
	vault := pickupV7TestVault(t)
	logbookTestTask(t, vault, "APP-T-0001", "Live stream board")
	day := "2026-06-15"
	d := mustDay(t, day)
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "status_changed", logbookLocalTime(d, 9), map[string]any{
		"from": "review", "to": "done", "reason": "Board renders from runs and leases.",
	})
	logbookSeedEvent(t, vault, "APP-T-0001", "task", "verification_added", logbookLocalTime(d, 10), map[string]any{
		"check": "command: go test ./cmd/tusker -run Board", "result": "pass",
	})

	logbook, err := buildTuskerLogbook(vault, d, time.Now().UTC())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	output := renderTuskerLogbookMarkdown(logbook)

	for _, section := range []string{"## What shipped", "## What it means", "## What needs your attention"} {
		assertContainsIndexTest(t, output, section)
	}
	// No raw command transcripts leak into the prose.
	assertNotContainsIndexTest(t, output, "go test")
	assertNotContainsIndexTest(t, output, "command:")

	// Task IDs appear only inside link targets or the References footnotes.
	idPattern := regexp.MustCompile(`[A-Z]{2,}-T-\d{4}`)
	linkTarget := regexp.MustCompile(`\([^)]*\)`)
	inReferences := false
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "## References") {
			inReferences = true
			continue
		}
		if inReferences {
			continue
		}
		stripped := linkTarget.ReplaceAllString(line, "")
		if idPattern.MatchString(stripped) {
			t.Fatalf("task ID leaked into prose: %q", line)
		}
	}
	// Evidence and work references are rendered as markdown links.
	if !strings.Contains(output, "](work/tasks/APP-T-0001.md)") {
		t.Fatalf("expected a markdown link to the task, got:\n%s", output)
	}
}

func TestLogbookEmptyDay(t *testing.T) {
	vault := pickupV7TestVault(t)
	day := "2026-06-15"

	output := captureStdout(t, func() {
		if err := logbookCmd(Args{"vault": vault, "date": day}); err != nil {
			t.Fatalf("empty day should not error: %v", err)
		}
	})
	assertContainsIndexTest(t, output, "# Morning Logbook — "+day)
	assertContainsIndexTest(t, output, "quiet day")
	assertContainsIndexTest(t, output, "## What shipped")
	assertContainsIndexTest(t, output, "Nothing needs you right now")
}

func mustDay(t *testing.T, day string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	return parsed
}
