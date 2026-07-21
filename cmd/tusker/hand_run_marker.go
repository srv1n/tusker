package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A hand-run marker records that a piece of work was picked up by hand in a
// live, interactive session rather than handed out by the automation daemon.
// The marker lives beside the lease so it survives later status changes (the
// lease record is rewritten on every status change, but this marker file is
// not) and stays put until the work closes. Daemon-dispatched work never writes
// this marker, so hand-run and handed-out work stay distinguishable.

// handRunMarkerPath is the durable marker file for a task's claim.
func handRunMarkerPath(vaultPath, taskID string) string {
	return filepath.Join(v7LeaseDir(vaultPath), taskID+".hand_run")
}

// claimIsHandRun reports whether the current claim is being made by hand in an
// interactive session. A dispatched Tusker worker carries TUSKER_ATTEMPT_ID and
// is not hand-run; anything else is a live, hands-on claim.
func claimIsHandRun() bool {
	return strings.TrimSpace(os.Getenv("TUSKER_ATTEMPT_ID")) == ""
}

// markHandRun stamps the durable hand-run marker for a task claimed by hand.
func markHandRun(vaultPath, taskID, owner string) error {
	if taskID == "" {
		return nil
	}
	if err := ensureDir(v7LeaseDir(vaultPath)); err != nil {
		return err
	}
	stamp := "owner: " + fallback(owner, "agent:"+defaultActorName()) + "\n" +
		"claimed_at: " + time.Now().UTC().Format(time.RFC3339) + "\n"
	return writeText(handRunMarkerPath(vaultPath, taskID), stamp)
}

// hasHandRunMarker reports whether a task carries the hand-run marker.
func hasHandRunMarker(vaultPath, taskID string) bool {
	return fileExists(handRunMarkerPath(vaultPath, taskID))
}
