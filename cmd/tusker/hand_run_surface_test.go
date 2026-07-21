package main

import (
	"strings"
	"testing"
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
