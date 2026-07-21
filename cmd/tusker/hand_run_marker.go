package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A hand-run marker records that a piece of work was picked up by hand in a
// live, interactive session rather than handed out by the automation daemon.
// The marker lives beside the lease and survives later status changes (the
// lease record is rewritten on every status change, but this marker file is
// not), so hand-run and handed-out work stay distinguishable.
//
// The marker is restamped on EVERY claim: a hand-run claim writes it, and a
// daemon-dispatched claim removes any stale marker left by an earlier hand-run
// claim of the same task. This keeps the on-disk marker in agreement with the
// hand_run flag on the claimed event, even when a task is hand-claimed,
// released, and later re-claimed by the daemon. There is no separate cleanup at
// close; restamp-on-claim is the sole authority.
//
// The marker is keyed by TASK, so it only ever reflects the LATEST claim of a
// task. Board and web rows are keyed by RUN and can be historical (a daemon
// row landed hours ago while the task was since hand-claimed, or vice versa),
// so they must NOT render from this marker. The authoritative per-run origin is
// the hand_run stamp persisted on the RunStatus at claim time
// (ClaimRunLease). runHandRunOrigin reads that stamp and only falls back to
// this marker for runs claimed before the stamp existed (RunStatus.HandRun
// unstamped). The marker remains for lease-level introspection.

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

// clearHandRun removes any hand-run marker for a task. It is a no-op when no
// marker exists, so daemon claims can call it unconditionally.
func clearHandRun(vaultPath, taskID string) error {
	if taskID == "" {
		return nil
	}
	if err := os.Remove(handRunMarkerPath(vaultPath, taskID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// restampHandRun brings the on-disk marker into agreement with the current
// claim: a hand-run claim writes the marker, a daemon-dispatched claim removes
// any stale marker from an earlier hand-run claim of the same task.
func restampHandRun(vaultPath, taskID, owner string) error {
	if claimIsHandRun() {
		return markHandRun(vaultPath, taskID, owner)
	}
	return clearHandRun(vaultPath, taskID)
}

// hasHandRunMarker reports whether a task carries the hand-run marker.
func hasHandRunMarker(vaultPath, taskID string) bool {
	return fileExists(handRunMarkerPath(vaultPath, taskID))
}

// runHandRunOrigin reports whether a specific RUN was claimed by hand. It reads
// the authoritative per-run stamp on the run record and only consults the
// task-keyed marker for legacy runs that predate the stamp (HandRunStamped is
// false), so a historical row no longer inherits a later claim's origin.
func runHandRunOrigin(run RunStatus, vaultPath string) bool {
	if run.HandRunStamped {
		return run.HandRun
	}
	return hasHandRunMarker(vaultPath, run.ItemID)
}
