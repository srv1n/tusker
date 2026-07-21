package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func handRunTestVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "vault")
	must := func(args Args, fn func(Args) error) {
		t.Helper()
		if err := fn(args); err != nil {
			t.Fatal(err)
		}
	}
	must(Args{"vault": vault, "quiet": "true"}, bootstrap)
	must(Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Hand-run marker smoke.", "v7": "true"}, newV7Epic)
	must(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Hand-run target", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	return vault
}

// A1: claiming in a hands-on session records a durable hand-run marker.
func TestInteractiveClaimMarksOutsideDaemon(t *testing.T) {
	vault := handRunTestVault(t)
	// A live interactive session carries no dispatched-attempt id.
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}
	if !hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("expected interactive claim to record the hand-run marker")
	}
}

// A2: daemon-dispatched work carries no hand-run marker.
func TestDaemonDispatchLeavesNoOutsideMarker(t *testing.T) {
	vault := handRunTestVault(t)
	// A dispatched Tusker worker claims under a live attempt id.
	t.Setenv("TUSKER_ATTEMPT_ID", "APP-T-0001-A-0001")
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}
	if hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("daemon-dispatched claim must not record the hand-run marker")
	}
}

// A3: the marker survives later status changes until the work closes.
func TestOutsideDaemonMarkerPersistsThroughStatus(t *testing.T) {
	vault := handRunTestVault(t)
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}
	if !hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("expected hand-run marker after interactive claim")
	}
	// Heartbeat and release rewrite the lease record; a reconcile reprojects
	// task state. The durable marker must ride through all of them.
	if _, err := writeV7Lease(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}, "active"); err != nil {
		t.Fatal(err)
	}
	if !hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("hand-run marker must survive a lease heartbeat")
	}
	if _, err := writeV7Lease(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}, "released"); err != nil {
		t.Fatal(err)
	}
	if err := reconcileV7Cmd(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if !hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("hand-run marker must survive later status changes until the work closes")
	}
}

// A4: a daemon re-claim after a hand-run claim of the same task must leave NO
// marker and emit hand_run:false — the on-disk marker and the claimed event's
// hand_run flag agree for the same claim.
func TestDaemonReclaimAfterHandRunClearsMarker(t *testing.T) {
	vault := handRunTestVault(t)

	// First: an interactive hand-run claim stamps the durable marker.
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}
	if !hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("expected hand-run marker after interactive claim")
	}

	// Release the hand-run claim so the task is claimable again.
	if _, err := writeV7Lease(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}, "released"); err != nil {
		t.Fatal(err)
	}

	// Then: the daemon re-claims the same task under a dispatched attempt id.
	t.Setenv("TUSKER_ATTEMPT_ID", "APP-T-0001-A-0001")
	if err := claimCmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "owner": "agent:codex"}); err != nil {
		t.Fatal(err)
	}
	if hasHandRunMarker(vault, "APP-T-0001") {
		t.Fatal("daemon re-claim must clear the stale hand-run marker")
	}
	if v := latestClaimedHandRun(t, vault, "APP-T-0001"); v != false {
		t.Fatalf("daemon re-claim must emit hand_run:false, got %v", v)
	}
}

// latestClaimedHandRun returns the hand_run flag on the most recent "claimed"
// event emitted for a task. Events are individual JSON files under
// <vault>/events/YYYY/MM/; the newest by modtime wins.
func latestClaimedHandRun(t *testing.T, vault, taskID string) bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(vault, "events", "*", "*", taskID+"--*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var newest string
	var newestAt time.Time
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event["event_kind"] != "claimed" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if newest == "" || info.ModTime().After(newestAt) {
			newest, newestAt = path, info.ModTime()
		}
	}
	if newest == "" {
		t.Fatal("no claimed event found for task")
	}
	raw, err := os.ReadFile(newest)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	payload, ok := event["payload"].(map[string]any)
	if !ok {
		t.Fatalf("claimed event %s has no payload", newest)
	}
	handRun, ok := payload["hand_run"].(bool)
	if !ok {
		t.Fatalf("claimed event %s payload has no hand_run bool", newest)
	}
	return handRun
}
