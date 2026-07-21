package main

import (
	"path/filepath"
	"testing"
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
