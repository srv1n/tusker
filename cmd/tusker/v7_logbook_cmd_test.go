package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func logbookTestTask(t *testing.T, vault, id, title string) {
	t.Helper()
	if err := newV7Task(Args{"owned-paths": "cmd/tusker",
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

func TestLogbookComposesFromRecords(t *testing.T) {
	vault := pickupV7TestVault(t)
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "BGR", "title": "Batch gate repairs", "summary": "Repair work.", "v7": "true"}); err != nil {
		t.Fatalf("create BGR epic: %v", err)
	}
	logbookTestTask(t, vault, "APP-T-0001", "Claim conflict refusal")
	if err := newV7Task(Args{"owned-paths": "cmd/tusker", "vault": vault, "quiet": "true", "epic": "BGR", "id": "BGR-T-0001",
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
	makeV7TaskDispatchableForTest(t, vault, "BGR-T-0001")
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

func mustDay(t *testing.T, day string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	return parsed
}
