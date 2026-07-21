package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A1: the text board shows a plain "hand-run" label on lanes that were picked
// up by hand, and leaves the automated lanes unlabelled.
func TestStreamBoardShowsOutsideDaemon(t *testing.T) {
	board := renderStreamBoard([]streamRow{
		{Lane: "execute", TaskID: "APP-T-0001", Runner: "codex", Status: "live", HandRun: true},
		{Lane: "execute", TaskID: "APP-T-0002", Runner: "codex", Status: "live", HandRun: false},
	})

	var handLine, autoLine string
	for _, line := range strings.Split(board, "\n") {
		switch {
		case strings.Contains(line, "APP-T-0001"):
			handLine = line
		case strings.Contains(line, "APP-T-0002"):
			autoLine = line
		}
	}
	if handLine == "" || autoLine == "" {
		t.Fatalf("board missing expected lane rows:\n%s", board)
	}
	if !strings.Contains(handLine, "hand-run") {
		t.Fatalf("hand-run lane must carry the hand-run label, got %q", handLine)
	}
	if strings.Contains(autoLine, "hand-run") {
		t.Fatalf("automated lane must not be labelled hand-run, got %q", autoLine)
	}
}

// A2,A3: the serve run payload carries the hand-run marker for lanes picked up
// by hand, and omits it for lanes the automation line handed out.
func TestServeRunsExposeOutsideDaemon(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	if err := markHandRun(server.vaultPath, "APP-T-0001", "agent:codex"); err != nil {
		t.Fatal(err)
	}
	snap := serveSnapshot{
		projectID: "app",
		project:   RegisteredProject{ProjectID: "app", VaultRoot: server.vaultPath},
		notesByID: map[string]Note{},
	}

	hand := server.runSummary(snap, RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001"})
	if !hand.HandRun {
		t.Fatal("run payload must expose handRun=true for a hand-run lane")
	}

	auto := server.runSummary(snap, RunStatus{ProjectID: "app", RecordID: "APP-T-0002", ItemID: "APP-T-0002"})
	if auto.HandRun {
		t.Fatal("automated lane must not report handRun=true")
	}
}

// A daemon-landed historical row must keep NO hand-run tag even after the SAME
// task is later hand-claimed (which stamps the task-keyed marker). The row
// carries its own per-run origin (HandRun=false, stamped), so it must render
// from the stamp and ignore the now-present marker. Proves the marker is no
// longer the render source and fixes the false-positive on historical rows.
func TestStreamBoardHistoricalDaemonRowIgnoresLaterHandMarker(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	// A later hand-claim of the task leaves a task-keyed marker on disk.
	if err := markHandRun(server.vaultPath, "APP-T-0001", "agent:codex"); err != nil {
		t.Fatal(err)
	}
	// The historical daemon run carries an authoritative per-run stamp.
	daemonRow := RunStatus{ItemID: "APP-T-0001", HandRun: false, HandRunStamped: true}
	if runHandRunOrigin(daemonRow, server.vaultPath) {
		t.Fatal("daemon-landed historical row must not inherit a later hand-claim marker")
	}
	board := renderStreamBoard([]streamRow{{
		Lane: "execute", TaskID: "APP-T-0001", Runner: "codex", Status: "landed",
		HandRun: runHandRunOrigin(daemonRow, server.vaultPath),
	}})
	if strings.Contains(board, "hand-run") {
		t.Fatalf("historical daemon row must render without a hand-run tag, got:\n%s", board)
	}
}

// A hand-run landed row must KEEP its hand-run tag even after a daemon re-claim
// of the same task clears the task-keyed marker. The row's per-run stamp
// (HandRun=true, stamped) is authoritative, so render must not depend on the
// (now absent) marker. Proves the fix for the false-negative on hand-run rows.
func TestServeRunsHandRunRowKeepsTagAfterDaemonReclaim(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	// A daemon re-claim would have cleared the marker: none exists on disk.
	if hasHandRunMarker(server.vaultPath, "APP-T-0001") {
		t.Fatal("precondition: no hand-run marker should exist")
	}
	snap := serveSnapshot{
		projectID: "app",
		project:   RegisteredProject{ProjectID: "app", VaultRoot: server.vaultPath},
		notesByID: map[string]Note{},
	}
	handRow := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", HandRun: true, HandRunStamped: true}
	if !server.runSummary(snap, handRow).HandRun {
		t.Fatal("hand-run landed row must keep handRun=true after the marker is cleared")
	}
}

// ClaimRunLease stamps the hand-run origin per run: a hand claim persists
// hand_run=true and a later daemon re-claim of the same row persists
// hand_run=false. The stamp round-trips through ListRuns/FindRun as an
// authoritative (stamped) value, independent of any task-keyed marker.
func TestClaimRunLeaseStampsHandRunPerRun(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 7, 20, 5, 0, 0, 0, time.UTC)

	base := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute,
		LeaseState: string(LeaseStateUnclaimed), UpdatedAt: now.Format(time.RFC3339),
	}
	if err := store.UpsertRun(base); err != nil {
		t.Fatal(err)
	}

	// Hand claim -> hand_run stamped true.
	claimed, err := store.ClaimRunLease("project-1", "APP-T-0001", "hand-owner", 1, defaultRunLeaseTTL, now, true, true, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseStateUnclaimed, ExpectedOwner: "", ExpectedLeaseGeneration: 0, ExpectedWorkRevision: 0,
	})
	if err != nil || !claimed {
		t.Fatalf("hand claim did not take: claimed=%v err=%v", claimed, err)
	}
	stored, err := store.FindRun("APP-T-0001")
	if err != nil || stored == nil {
		t.Fatalf("load run: %#v %v", stored, err)
	}
	if !stored.HandRunStamped || !stored.HandRun {
		t.Fatalf("hand claim must stamp hand_run=true, got stamped=%v handRun=%v", stored.HandRunStamped, stored.HandRun)
	}

	// Release the hand claim so the row is re-claimable; the stamp must survive
	// an intervening non-claim upsert.
	released := *stored
	released.LeaseState = string(LeaseStateReleased)
	released.UpdatedAt = now.Add(time.Minute).Format(time.RFC3339)
	if err := store.UpsertRun(released); err != nil {
		t.Fatal(err)
	}
	if again, err := store.FindRun("APP-T-0001"); err != nil || again == nil || !again.HandRun {
		t.Fatalf("release upsert must preserve the hand_run stamp, got %#v %v", again, err)
	}

	// Daemon re-claim of the same row -> hand_run stamped false (tag cleared).
	claimed, err = store.ClaimRunLease("project-1", "APP-T-0001", "daemon-owner", 2, defaultRunLeaseTTL, now.Add(2*time.Minute), true, false, RuntimeLeaseClaimPrecondition{
		ExpectedLeaseState: LeaseStateReleased, ExpectedOwner: "hand-owner", ExpectedLeaseGeneration: 1, ExpectedWorkRevision: 0,
	})
	if err != nil || !claimed {
		t.Fatalf("daemon re-claim did not take: claimed=%v err=%v", claimed, err)
	}
	reclaimed, err := store.FindRun("APP-T-0001")
	if err != nil || reclaimed == nil {
		t.Fatalf("load reclaimed run: %#v %v", reclaimed, err)
	}
	if !reclaimed.HandRunStamped || reclaimed.HandRun {
		t.Fatalf("daemon re-claim must stamp hand_run=false, got stamped=%v handRun=%v", reclaimed.HandRunStamped, reclaimed.HandRun)
	}
}
