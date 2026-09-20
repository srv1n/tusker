package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func walkthroughAdmitRun(t *testing.T, vault string, store *RuntimeStore, project RegisteredProject, waveID, taskID, itemID, lane, owner string) directStartResult {
	t.Helper()
	start, err := directWaveStart(vault, store, waveID, "human:test")
	if err != nil {
		t.Fatal(err)
	}
	directive, err := store.RunDirective(project.ProjectID, taskID)
	if err != nil || directive == nil {
		t.Fatalf("directive=%#v err=%v", directive, err)
	}
	now := time.Now().UTC()
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: taskID, ItemID: itemID, Lane: lane,
		LeaseState: string(LeaseStateRunning), LeaseOwner: owner, LeaseGeneration: 1,
		ActiveAttemptID: owner,
		LeaseExpiresAt:  now.Add(time.Hour).Format(time.RFC3339), LastHeartbeatAt: now.Format(time.RFC3339),
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{
		ProjectID: project.ProjectID, RecordID: taskID, LeaseGeneration: 1, AttemptID: owner,
		Source: "human_run_directive", Actor: "human:test",
		DirectiveWaveID: waveID, DirectiveAuthorizationFingerprint: start.MaterialFingerprint,
		DirectiveWaveAuthorizedAt: directive.WaveAuthorizedAt,
	}); err != nil {
		t.Fatal(err)
	}
	return start
}

func walkthroughSetRun(t *testing.T, store *RuntimeStore, run RunStatus) {
	t.Helper()
	if _, err := store.exec(`UPDATE runs SET lane=?, lease_state=?, lease_owner=?, lease_generation=?, lease_expires_at=?, attempt_outcome=?, active_attempt_id=?, last_error=?, terminal=? WHERE project_id=? AND record_id=?`,
		run.Lane, run.LeaseState, run.LeaseOwner, run.LeaseGeneration, run.LeaseExpiresAt, run.AttemptOutcome, run.ActiveAttemptID, run.LastError, boolToInt(run.Terminal), run.ProjectID, run.RecordID); err != nil {
		t.Fatal(err)
	}
}

func walkthroughMember(t *testing.T, review directWaveReview, taskID string) directWaveReviewMember {
	t.Helper()
	for _, member := range review.Members {
		if member.TaskID == taskID {
			return member
		}
	}
	t.Fatalf("member %s missing from %#v", taskID, review.Members)
	return directWaveReviewMember{}
}

func walkthroughNote(t *testing.T, vault, id string) Note {
	t.Helper()
	note, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	return note
}

func TestWalkthroughWaveStatusCanonicalRunAndLifecycle(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	walkthroughAdmitRun(t, vault, store, project, "W-0001", "APP-T-0001", "worker-row-17", runLaneExecute, "worker:live")
	if err := store.UpsertRun(RunStatus{ProjectID: "other-project", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, LeaseState: string(LeaseStateRunning), LeaseOwner: "wrong-project", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	member := walkthroughMember(t, review, "APP-T-0001")
	if member.State != "running" || member.Phase != "executing" || member.Lane != runLaneExecute || !strings.Contains(member.WaitingReason, "worker:live") {
		t.Fatalf("canonical admitted worker not projected: %#v", member)
	}

	// A terminal row with a running-looking lease is history, not an owner.
	run, _ := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	run.Terminal = true
	walkthroughSetRun(t, store, *run)
	review, _ = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if got := walkthroughMember(t, review, "APP-T-0001"); got.State == "running" || got.State == "reviewing" {
		t.Fatalf("terminal attempt won live projection: %#v", got)
	}

	// Reclaiming the same record outside the wave must not inherit the old
	// consumed directive's authority.
	run.Terminal, run.LeaseGeneration, run.LeaseOwner, run.ActiveAttemptID = false, 2, "agent:outside", "attempt-outside"
	run.LeaseState, run.LeaseExpiresAt = string(LeaseStateRunning), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	walkthroughSetRun(t, store, *run)
	if err := store.SaveRunAuthorization(RunAuthorization{ProjectID: project.ProjectID, RecordID: "APP-T-0001", LeaseGeneration: 2, AttemptID: "attempt-outside", Source: "tusker_cli", Actor: "agent:outside"}); err != nil {
		t.Fatal(err)
	}
	review, _ = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	got := walkthroughMember(t, review, "APP-T-0001")
	if got.State == "running" || got.State == "reviewing" || !strings.Contains(got.WaitingReason, "outside") {
		t.Fatalf("outside reclaim inherited wave authority: %#v", got)
	}
}

func TestWalkthroughWaveStatusProofAndReviewPhases(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	walkthroughAdmitRun(t, vault, store, project, "W-0001", "APP-T-0001", "APP-T-0001", runLaneExecute, "worker:one")
	review, _ := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	for _, blocker := range review.Blockers {
		if blocker.Code == "STRICT_PROOF_STALE" {
			t.Fatalf("running work reported future proof as stale: %#v", blocker)
		}
	}

	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "review"
		return data, body
	})
	run, _ := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	run.LeaseState, run.LeaseOwner, run.LeaseExpiresAt = string(LeaseStateReleased), "", ""
	run.AttemptOutcome, run.Terminal, run.ActiveAttemptID = string(AttemptOutcomeSucceeded), false, ""
	walkthroughSetRun(t, store, *run)
	review, _ = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if got := walkthroughMember(t, review, "APP-T-0001"); got.Phase != "awaiting_review" {
		t.Fatalf("worker success did not await review: %#v", got)
	}

	now := time.Now().UTC()
	run.Lane, run.LeaseState, run.LeaseOwner, run.LeaseGeneration = runLaneReview, string(LeaseStateRunning), "reviewer:one", 2
	run.LeaseExpiresAt, run.LastHeartbeatAt, run.Terminal, run.ActiveAttemptID = now.Add(time.Hour).Format(time.RFC3339), now.Format(time.RFC3339), false, "reviewer:one"
	walkthroughSetRun(t, store, *run)
	directive, _ := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err := store.SaveRunAuthorization(RunAuthorization{ProjectID: project.ProjectID, RecordID: "APP-T-0001", LeaseGeneration: 2, AttemptID: "reviewer:one", Source: "human_run_directive", Actor: "human:test", DirectiveWaveID: "W-0001", DirectiveAuthorizationFingerprint: review.MaterialFingerprint, DirectiveWaveAuthorizedAt: directive.WaveAuthorizedAt}); err != nil {
		t.Fatal(err)
	}
	review, _ = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if got := walkthroughMember(t, review, "APP-T-0001"); got.State != "reviewing" || got.Phase != "reviewing" {
		t.Fatalf("active reviewer not projected: %#v", got)
	}

	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		return data, body
	})
	review, _ = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if review.State != "Completed" || walkthroughMember(t, review, "APP-T-0001").State != "completed" {
		t.Fatalf("accepted completion not projected: %#v", review)
	}
}

func TestWalkthroughWaveStatusStaleProofAndPausedPartialFailure(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writePendingDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0003", "", nil)
	writePendingDirectTask(t, vault, "APP-T-0004", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0005", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002", "APP-T-0003", "APP-T-0004", "APP-T-0005"}, nil)
	if code, _ := directWaveProofBlocker(vault, walkthroughNote(t, vault, "APP-T-0001")); code != "STRICT_PROOF_MISSING" {
		t.Fatalf("pending proof code=%q", code)
	}
	rewriteTaskFile(t, vault, "APP-T-0005", func(data map[string]any, body string) (map[string]any, string) {
		rows := parseV7VerificationRows(body)
		rows[0].Result, rows[0].Notes = "fail", "command exited 1"
		return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	})
	if code, _ := directWaveProofBlocker(vault, walkthroughNote(t, vault, "APP-T-0005")); code != "STRICT_PROOF_FAILED" {
		t.Fatalf("failed proof code=%q", code)
	}
	rewriteTaskFile(t, vault, "APP-T-0003", func(data map[string]any, body string) (map[string]any, string) {
		return data, strings.Replace(body, "Do APP-T-0003.", "Changed APP-T-0003.", 1)
	})
	if code, _ := directWaveProofBlocker(vault, walkthroughNote(t, vault, "APP-T-0003")); code != "STRICT_PROOF_STALE" {
		t.Fatalf("stale proof code=%q", code)
	}
	start := walkthroughAdmitRun(t, vault, store, project, "W-0001", "APP-T-0001", "APP-T-0001", runLaneExecute, "worker:live")
	_ = start
	for _, taskID := range []string{"APP-T-0001", "APP-T-0003", "APP-T-0004", "APP-T-0005"} {
		rewriteTaskFile(t, vault, taskID, func(data map[string]any, body string) (map[string]any, string) {
			data["status"] = "done"
			return data, body
		})
	}
	if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), LastError: "workspace setup failed", Terminal: true}); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Authorization != "paused" || review.State != "Paused" {
		t.Fatalf("paused admission lost: %#v", review)
	}
	if live := walkthroughMember(t, review, "APP-T-0001"); live.State != "running" || !live.CompletionReported {
		t.Fatalf("paused admitted work lost or falsely completed: %#v", live)
	}
	if failed := walkthroughMember(t, review, "APP-T-0002"); failed.State != "blocked" || failed.Phase != "failed" || !strings.Contains(failed.WaitingReason, "workspace setup failed") {
		t.Fatalf("partial setup failure hidden: %#v", failed)
	}
	for _, taskID := range []string{"APP-T-0003", "APP-T-0004", "APP-T-0005"} {
		blocked := walkthroughMember(t, review, taskID)
		if blocked.State != "waiting" || blocked.Phase != "proof_blocked" {
			t.Fatalf("done task %s with invalid proof became startable: %#v", taskID, blocked)
		}
	}
	for _, control := range review.Controls {
		if (control.Scope == "APP-T-0003" || control.Scope == "APP-T-0004" || control.Scope == "APP-T-0005") && control.Enabled {
			t.Fatalf("proof-blocked task has enabled Start: %#v", control)
		}
	}
	foundStale := false
	for _, blocker := range review.Blockers {
		foundStale = foundStale || blocker.Code == "STRICT_PROOF_MISSING"
	}
	if !foundStale {
		t.Fatalf("completed task's missing/stale proof was hidden: %#v", review.Blockers)
	}
}

func TestWalkthroughWaveStatusCancelledAndRuntimeUnavailable(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"status": "cancelled"})
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		return data, body
	})
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.State != "Cancelled" {
		t.Fatalf("cancelled wave projected %q: %#v", review.State, review)
	}
	for _, control := range review.Controls {
		if control.Enabled && (control.Action == "wave start" || control.Action == "task start") {
			t.Fatalf("cancelled wave or member is startable: %#v", control)
		}
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err == nil || !strings.Contains(err.Error(), "WAVE_TERMINAL") {
		t.Fatalf("cancelled wave start err=%v", err)
	}

	writeDirectTask(t, vault, "APP-T-0002", "W-0002", nil)
	writeDirectWave(t, vault, "W-0002", []string{"APP-T-0002"}, nil)
	review, err = buildDirectWaveReview(vault, nil, project.ProjectID, "W-0002", nil)
	if err != nil {
		t.Fatal(err)
	}
	foundRuntime, enabledStart := false, false
	for _, blocker := range review.Blockers {
		foundRuntime = foundRuntime || blocker.Code == "RUNTIME_UNAVAILABLE"
	}
	for _, control := range review.Controls {
		enabledStart = enabledStart || control.Action == "wave start" && control.Enabled
	}
	if !foundRuntime || enabledStart {
		t.Fatalf("missing runtime store looked usable: blockers=%#v controls=%#v", review.Blockers, review.Controls)
	}
}

func TestWalkthroughWaveStatusArmedTerminalWaveQueuesNothing(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE run_directives SET state='consumed' WHERE project_id=? AND record_id=?`, project.ProjectID, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")
	data, body, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "cancelled"
	data["state_rev"] = v7StateRev(data, body)
	raw, err := serializeDocument(data, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(wavePath, raw); err != nil {
		t.Fatal(err)
	}
	queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
	if err != nil || len(queued) != 0 {
		t.Fatalf("armed terminal wave queued=%#v err=%v", queued, err)
	}
	directive, err := store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil || directive == nil || directive.State != "consumed" {
		t.Fatalf("terminal queue path rearmed directive: directive=%#v err=%v", directive, err)
	}
}

func TestWalkthroughWaveStatusTaskStartRefusesTerminalWave(t *testing.T) {
	for _, status := range []string{"cancelled", "superseded"} {
		t.Run(status, func(t *testing.T) {
			vault, store, project := authorityFixture(t)
			writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
			writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, map[string]any{"status": status})
			result, err := directTaskBackgroundStart(vault, store, "APP-T-0001", "human:test")
			if err == nil || !strings.Contains(err.Error(), "WAVE_TERMINAL") || result.Authorization != "inert" {
				t.Fatalf("task start bypassed %s wave: result=%#v err=%v", status, result, err)
			}
			if directive, err := store.RunDirective(project.ProjectID, "APP-T-0001"); err != nil || directive != nil {
				t.Fatalf("terminal-wave task start wrote directive=%#v err=%v", directive, err)
			}
		})
	}
}

func TestWalkthroughWaveStatusQueueOccupancyUsesRecordIdentity(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"record_id": "record-17"})
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE run_directives SET state='consumed' WHERE project_id=? AND record_id=?`, project.ProjectID, "record-17"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "record-17", ItemID: "display-alias", LeaseState: string(LeaseStateRetryQueued)}); err != nil {
		t.Fatal(err)
	}
	queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 0 {
		t.Fatalf("record-occupied task was queued again: %#v", queued)
	}
	directive, err := store.RunDirective(project.ProjectID, "record-17")
	if err != nil || directive == nil || directive.State != "consumed" {
		t.Fatalf("record occupancy did not preserve consumed directive: directive=%#v err=%v", directive, err)
	}
}
