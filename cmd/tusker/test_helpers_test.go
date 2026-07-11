package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func installEscalationTestNotifications() (func(), error) {
	notificationRoot, err := os.MkdirTemp("", "tusker-test-notifications-*")
	if err != nil {
		return nil, err
	}
	notificationPath := filepath.Join(notificationRoot, "notifications.tsv")
	_ = os.Setenv(escalationNotifierModeEnv, "record")
	_ = os.Setenv(escalationNotifierRecordEnv, notificationPath)
	notifyEscalationUser = func(title, message string) error {
		return recordEscalationNotification(title, message)
	}
	return func() { _ = os.RemoveAll(notificationRoot) }, nil
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertEqual(t *testing.T, expected, actual any, label string) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("%s: expected %#v, got %#v", label, expected, actual)
	}
}

func makeV7TaskDispatchableForTest(t *testing.T, vault, taskID string) {
	t.Helper()
	taskPath := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "ready"
	data["readiness"] = "ready"
	data["next_owner"] = "agent"
	body = replaceSection(body, "## Intent", "Exercise the focused test task.")
	body = replaceSection(body, "## Acceptance", "| ID | Outcome | Proof |\n|---|---|---|\n| A1 | Complete the focused test task. | Inline verification |")
	body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | command: go test ./cmd/tusker -run TestV7 -count=1 | pending | Focused test proof. |")
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	if hasV7DashboardProjection(vault) {
		idx, err := loadV7Index(vault)
		if err != nil {
			t.Fatal(err)
		}
		if err := buildV7Dashboards(vault, idx); err != nil {
			t.Fatal(err)
		}
	}
}
