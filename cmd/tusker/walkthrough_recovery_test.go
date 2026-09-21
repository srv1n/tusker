package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWalkthroughWaveRecovery exercises the human-facing recovery contract end
// to end: capacity waiting is honest, failures project the one supported
// recovery action, pause/resume tells the truth about retry, redrive preserves
// durable attempt history, and packet/review/start validate the same canonical
// task bytes.
func TestWalkthroughWaveRecovery(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0003", "W-0001", nil)
	writeDirectTaskBody(t, vault, "APP-T-0004", "W-0001", map[string]any{"status": "rework", "readiness": "ready"}, directDispatchableTaskBody("APP-T-0004"))
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002", "APP-T-0003", "APP-T-0004"}, map[string]any{"concurrency": 1})

	// Wave start admits only the first frontier slot under concurrency=1.
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Authorization != "authorized" {
		t.Fatalf("started wave projected authorization=%q", review.Authorization)
	}
	if first := walkthroughMember(t, review, "APP-T-0001"); first.State != "waiting" || first.Phase != "queued" {
		t.Fatalf("admitted frontier member not projected as queued: %#v", first)
	}
	for _, id := range []string{"APP-T-0002", "APP-T-0003"} {
		member := walkthroughMember(t, review, id)
		if member.Phase != "capacity_wait" || member.Responsible != "daemon" || !strings.Contains(member.WaitingReason, "execution slot") {
			t.Fatalf("%s was not projected as waiting for a wave execution slot: %#v", id, member)
		}
	}
	if waiting := walkthroughMember(t, review, "APP-T-0004"); waiting.Phase != "rework" || !strings.Contains(waiting.WaitingReason, "execution slot") {
		t.Fatalf("rework member lost its phase or its capacity wait: %#v", waiting)
	}
	for _, blocker := range review.Blockers {
		if blocker.Code == "DEPENDENCY_WAITING" {
			t.Fatalf("ordinary frontier waiting surfaced as a diagnostic blocker: %#v", blocker)
		}
	}

	// Freeing the slot advances the frontier to the next member.
	if _, err := store.exec(`UPDATE run_directives SET state='consumed' WHERE project_id=? AND record_id=?`, project.ProjectID, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), Terminal: true}); err != nil {
		t.Fatal(err)
	}
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "done"
		return data, body
	})
	queued, err := queueAuthorizedWaveFrontier(vault, store, project.ProjectID, "W-0001", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0] != "APP-T-0002" {
		t.Fatalf("released capacity did not admit the next member: %#v", queued)
	}
	if directive, err := store.RunDirective(project.ProjectID, "APP-T-0002"); err != nil || directive == nil || directive.State != "queued" {
		t.Fatalf("next member has no queued directive: directive=%#v err=%v", directive, err)
	}

	// A terminal execution failure is a runtime failure with retry_task — not
	// a wave-setup problem. Durable attempt history must survive the redrive.
	if err := store.SaveAttempt(RunAttempt{AttemptID: "execute-failed-1", ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", Lane: runLaneExecute, Outcome: string(AttemptOutcomeFailed)}); err != nil {
		t.Fatal(err)
	}
	failedRun := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0002", ItemID: "APP-T-0002", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 1, Terminal: true, LastError: "workspace setup failed"}
	if err := store.UpsertRun(failedRun); err != nil {
		t.Fatal(err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := walkthroughMember(t, review, "APP-T-0002")
	if failed.State != "blocked" || failed.Phase != "failed" || failed.Lane != runLaneExecute || !strings.Contains(failed.WaitingReason, "workspace setup failed") {
		t.Fatalf("runtime failure misclassified: %#v", failed)
	}
	if failed.Recovery == nil || failed.Recovery.Action != "retry_task" || !failed.Recovery.Enabled {
		t.Fatalf("runtime failure did not expose an enabled retry_task: %#v", failed.Recovery)
	}
	storedFailed, err := store.FindRunScoped(project.ProjectID, "APP-T-0002")
	if err != nil || storedFailed == nil {
		t.Fatalf("failed run missing for redrive: %#v err=%v", storedFailed, err)
	}
	if _, err := redriveRuntimeRun(store, storedFailed, "operator:test", "retry after workspace failure", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if storedFailed.LeaseState != string(LeaseStateRetryQueued) || storedFailed.AttemptCount != 0 || storedFailed.Terminal {
		t.Fatalf("redrive did not reopen the execution window: %#v", storedFailed)
	}
	attempts, err := store.ListAttemptsForRun(project.ProjectID, "APP-T-0002")
	if err != nil || len(attempts) != 1 || attempts[0].AttemptID != "execute-failed-1" {
		t.Fatalf("redrive rewrote durable attempt history: %#v err=%v", attempts, err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if member := walkthroughMember(t, review, "APP-T-0002"); member.State != "waiting" || member.Phase != "queued" || member.Responsible != "daemon" {
		t.Fatalf("redriven member not projected as queued for dispatch: %#v", member)
	}

	// A failed review lane projects retry_review and queues only the review
	// lane — implementation does not rerun, and the attempt window is kept.
	rewriteTaskFile(t, vault, "APP-T-0003", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "review"
		data["work_revision"] = 1
		return data, body
	})
	reviewRun := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0003", ItemID: "APP-T-0003", Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 1, WorkRevision: 1, Terminal: true, LastError: "reviewer crashed"}
	if err := store.UpsertRun(reviewRun); err != nil {
		t.Fatal(err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	reviewMember := walkthroughMember(t, review, "APP-T-0003")
	if reviewMember.Phase != "failed" || reviewMember.Lane != runLaneReview {
		t.Fatalf("review-lane failure misclassified: %#v", reviewMember)
	}
	if reviewMember.Recovery == nil || reviewMember.Recovery.Action != "retry_review" || !reviewMember.Recovery.Enabled {
		t.Fatalf("review failure did not expose an enabled retry_review: %#v", reviewMember.Recovery)
	}
	task3 := walkthroughNote(t, vault, "APP-T-0003")
	wave, err := resolveV7Note(vault, "W-0001", "wave")
	if err != nil {
		t.Fatal(err)
	}
	recoveryNow := time.Now().UTC()
	recovered, err := queueReviewRecovery(store, task3, wave, reviewRun, "operator:test", 3, recoveryNow)
	if err != nil || !recovered.Admitted || recovered.Lane != runLaneReview {
		t.Fatalf("review recovery not admitted: %#v err=%v", recovered, err)
	}
	stored, _ := store.FindRunScoped(project.ProjectID, "APP-T-0003")
	if stored.AttemptCount != 1 || stored.Lane != runLaneReview {
		t.Fatalf("review recovery reset the attempt window or moved lanes: %#v", stored)
	}
	if directive, err := store.RunDirective(project.ProjectID, "APP-T-0003"); err != nil || directive == nil || directive.WaveID != "W-0001" {
		t.Fatalf("review recovery queued an unbound directive: directive=%#v err=%v", directive, err)
	}
	if again, err := queueReviewRecovery(store, task3, wave, reviewRun, "operator:test", 3, recoveryNow); err != nil || again.Admitted || !again.OK {
		t.Fatalf("duplicate review recovery requeued: %#v err=%v", again, err)
	}

	// Rework is ordinary implementation state: ready, redispatchable, and not
	// a diagnostic failure — even when the released review lane that produced
	// it still carries an interrupted outcome.
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0004", ItemID: "APP-T-0004", Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeInterrupted), WorkRevision: 1, LastError: "review verdict recorded; changes requested"}); err != nil {
		t.Fatal(err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	rework := walkthroughMember(t, review, "APP-T-0004")
	if rework.State != "ready" || rework.Phase != "rework" || !strings.Contains(rework.WaitingReason, "review requested changes") {
		t.Fatalf("review rework misclassified: %#v", rework)
	}
	for _, blocker := range review.Blockers {
		if blocker.TaskID == "APP-T-0004" && blocker.Code != "CONTRACT_FINGERPRINT_STALE" {
			t.Fatalf("rework member carried a diagnostic blocker: %#v", blocker)
		}
	}

	// Packet, review, and start agree on task integrity: the authored project
	// id differs from the registered runtime project, and dispatchability must
	// be validated against canonical bytes, not the rendering overlay.
	if registered := project.ProjectID; registered == "" || registered == v7ProjectID(vault) {
		t.Fatalf("fixture no longer exercises authored/registered project divergence: %q vs %q", v7ProjectID(vault), registered)
	}
	task4 := walkthroughNote(t, vault, "APP-T-0004")
	if reason := directWaveTaskContractStaleReason(task4); reason != "" {
		t.Fatalf("canonical task is stale before packet: %s", reason)
	}
	if blockers := v7TaskDispatchBlockers(vault, task4); len(blockers) != 0 {
		t.Fatalf("dispatchable task reported blockers: %#v", blockers)
	}
	if err := packetV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0004", "for": "agent"}); err != nil {
		t.Fatalf("packet validation disagreed with review/start on the same bytes: %v", err)
	}

	// Pause keeps admitted work and task-scoped start truthful; resume
	// re-admits queued work and says plainly that it does not retry failures.
	if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Authorization != "paused" {
		t.Fatalf("paused wave projected authorization=%q", review.Authorization)
	}
	pausedMember := walkthroughMember(t, review, "APP-T-0004")
	if pausedMember.Phase != "paused" || pausedMember.Responsible != "operator" || !strings.Contains(pausedMember.WaitingReason, "does not retry failures") {
		t.Fatalf("paused member not annotated truthfully: %#v", pausedMember)
	}
	taskControl := false
	for _, control := range review.Controls {
		if control.Action == "task start" && control.Scope == "APP-T-0004" {
			taskControl = control.Enabled && strings.Contains(control.Reason, "remains paused")
		}
	}
	if !taskControl {
		t.Fatalf("paused wave lost its task-scoped start control: %#v", review.Controls)
	}
	resumed, err := directWaveResume(vault, store, "W-0001", "human:test")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Authorization != "authorized" || !strings.Contains(resumed.Reason, "not retried automatically") {
		t.Fatalf("resume reason hid the no-retry semantics: %#v", resumed)
	}

	// Wave material drift after authorization makes recovery refuse with the
	// supported re-authorization action instead of silently queueing.
	rewriteTaskFile(t, vault, "APP-T-0003", func(data map[string]any, body string) (map[string]any, string) {
		data["title"] = "Retitled after authorization"
		return data, body
	})
	if err := store.UpsertRun(reviewRun); err != nil {
		t.Fatal(err)
	}
	refused, err := queueReviewRecovery(store, walkthroughNote(t, vault, "APP-T-0003"), wave, reviewRun, "operator:test", 3, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !refused.Refused || !strings.Contains(refused.Reason, "wave material changed since authorization") {
		t.Fatalf("stale wave authorization did not refuse review recovery: %#v", refused)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Authorization != "stale" {
		t.Fatalf("drifted wave did not project stale authorization: %#v", review.Authorization)
	}
}

func TestOutcomeUnknownRecoveryIsBoundedAndPreservesLineageIntent(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0099", "W-0099", nil)
	writeDirectWave(t, vault, "W-0099", []string{"APP-T-0099"}, map[string]any{"authorization": "armed", "authorization_fingerprint": "material-1", "authorized_at": "2026-01-01T00:00:00Z"})
	parent := RunAttempt{AttemptID: "attempt-unknown", ProjectID: project.ProjectID, RecordID: "APP-T-0099", ItemID: "APP-T-0099", Lane: runLaneExecute, Outcome: string(AttemptOutcomeFailed), LastError: `acp outcome delivery_unknown (write_complete): lost contact`, StartedAt: "2026-01-01T00:00:00Z", FinishedAt: "2026-01-01T00:01:00Z"}
	if err := store.SaveAttempt(parent); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0099", ItemID: "APP-T-0099", Lane: runLaneExecute, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), LastError: parent.LastError, LastHeartbeatAt: time.Now().UTC().Format(time.RFC3339Nano), Terminal: true, AttemptCount: 1}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	result, err := queueOutcomeUnknownRecovery(store, idx.Tasks["APP-T-0099"], Note{}, run, "human:test", time.Now().UTC())
	if err != nil || !result.Admitted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	stored, err := store.FindRunScoped(project.ProjectID, "APP-T-0099")
	if err != nil || stored == nil || stored.LeaseState != string(LeaseStateRetryQueued) || !strings.Contains(stored.LastError, outcomeUnknownRecoveryReasonPrefix+parent.AttemptID) {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}

	// Once a recovery child exists for this parent, the same uncertainty may
	// not create another child.
	if err := store.SaveAttempt(RunAttempt{AttemptID: "attempt-recovery", ProjectID: project.ProjectID, RecordID: "APP-T-0099", ItemID: "APP-T-0099", Lane: runLaneExecute, ParentAttemptID: parent.AttemptID, ChildType: "recovery", Outcome: string(AttemptOutcomeFailed), StartedAt: "2026-01-01T00:02:00Z"}); err != nil {
		t.Fatal(err)
	}
	stored.LeaseState, stored.AttemptOutcome, stored.LastError, stored.Terminal = string(LeaseStateReleased), string(AttemptOutcomeUnknown), parent.LastError, true
	if err := store.UpsertRun(*stored); err != nil {
		t.Fatal(err)
	}
	result, err = queueOutcomeUnknownRecovery(store, idx.Tasks["APP-T-0099"], idx.Waves["W-0099"], *stored, "human:test", time.Now().UTC())
	if err != nil || !result.Refused || !strings.Contains(result.Reason, "human review") {
		t.Fatalf("repeat result=%#v err=%v", result, err)
	}
}

// rerunChecksCommandTaskBody carries one deterministic command proof: the
// sentinel file decides pass or fail so the test can flip verification
// outcome without touching the task contract.
func rerunChecksCommandTaskBody(id string) string {
	return "# " + id + "\n\n## Intent\n\nDo " + id + ".\n\n## Acceptance\n\n| ID | Outcome | Proof |\n| --- | --- | --- |\n| A1 | The named behavior is observable in the changed files. | focused test output |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: test -f .recovery-pass | pending | |\n"
}

// TestWalkthroughRerunChecksFailedCommand pins the verification recovery
// ordering: a failed command must not move the task to review before the
// check succeeds — the member stays at its prior status and keeps offering an
// enabled rerun_checks instead of an awaiting-review dead end.
func TestWalkthroughRerunChecksFailedCommand(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTaskBody(t, vault, "APP-T-0001", "W-0001", map[string]any{
		"status": "done", "readiness": "done", "work_revision": 1,
		"source_sha": "deadbeef", "owned_paths": []any{"proofed.txt"},
	}, rerunChecksCommandTaskBody("APP-T-0001"))
	writeDirectTaskBody(t, vault, "APP-T-0002", "W-0001", map[string]any{
		"status": "review", "readiness": "waiting_on_review", "work_revision": 1,
		"source_sha": "deadbeef", "owned_paths": []any{"proofed.txt"},
	}, rerunChecksCommandTaskBody("APP-T-0002"))
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	workspaces := map[string]string{}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		workspace := t.TempDir()
		runGitDir(t, workspace, "init")
		if err := writeText(filepath.Join(workspace, "proofed.txt"), "implemented\n"); err != nil {
			t.Fatal(err)
		}
		material, err := workspaceTreeStateHashForPaths(workspace, []string{"proofed.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SaveAttempt(RunAttempt{
			AttemptID: "execute-" + id, ProjectID: project.ProjectID, RecordID: id, ItemID: id,
			Runner: "codex", Lane: runLaneExecute, WorkRevision: 1, WorkspacePath: workspace,
			Outcome:  string(AttemptOutcomeSucceeded),
			EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: "deadbeef", WorktreePath: workspace, MaterialFingerprint: material, MaterialScope: []string{"proofed.txt"}},
		}); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: id, ItemID: id, Lane: runLaneReview, LeaseState: string(LeaseStateReleased), WorkRevision: 1}); err != nil {
			t.Fatal(err)
		}
		workspaces[id] = workspace
	}

	// Both members are blocked on proof, not on a reviewer, before recovery.
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		member := walkthroughMember(t, review, id)
		if member.Phase != "proof_blocked" || member.Recovery == nil || member.Recovery.Action != "rerun_checks" || !member.Recovery.Enabled {
			t.Fatalf("%s was not projected as proof-blocked with an enabled rerun_checks: %#v", id, member)
		}
	}

	// A failing command refuses at the verify lane and leaves the task at its
	// prior status — done stays done, review stays review — with the same
	// enabled rerun_checks afterwards, never an awaiting-review dead end.
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		result, err := recoverVerificationChecks(vault, store, project.ProjectID, id, "operator:test", 3, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if !result.Refused || result.Lane != "verify" {
			t.Fatalf("%s: failed command was not refused at the verify lane: %#v", id, result)
		}
	}
	if status := stringField(walkthroughNote(t, vault, "APP-T-0001").Data, "status"); status != "done" {
		t.Fatalf("failed verification moved a completed task to %q", status)
	}
	if status := stringField(walkthroughNote(t, vault, "APP-T-0002").Data, "status"); status != "review" {
		t.Fatalf("failed verification moved a submitted task to %q", status)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		member := walkthroughMember(t, review, id)
		if member.Phase != "proof_blocked" || member.Recovery == nil || member.Recovery.Action != "rerun_checks" || !member.Recovery.Enabled {
			t.Fatalf("%s: failed verification hid the rerun_checks recovery: %#v", id, member)
		}
	}

	// Once the command actually passes, the receipt is recorded first and
	// only then does the member reopen for independent review.
	if err := writeText(filepath.Join(workspaces["APP-T-0001"], ".recovery-pass"), "go\n"); err != nil {
		t.Fatal(err)
	}
	result, err := recoverVerificationChecks(vault, store, project.ProjectID, "APP-T-0001", "operator:test", 3, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.Admitted || result.Lane != runLaneReview {
		t.Fatalf("passing command did not queue independent review: %#v", result)
	}
	if status := stringField(walkthroughNote(t, vault, "APP-T-0001").Data, "status"); status != "review" {
		t.Fatalf("successful verification did not reopen review: status=%q", status)
	}
	if directive, err := store.RunDirective(project.ProjectID, "APP-T-0001"); err != nil || directive == nil || directive.State != "queued" || directive.WaveID != "W-0001" {
		t.Fatalf("recovery queued no wave-bound review directive: directive=%#v err=%v", directive, err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if member := walkthroughMember(t, review, "APP-T-0001"); member.Phase != "awaiting_review" {
		t.Fatalf("verified member not projected as awaiting review: %#v", member)
	}
	if member := walkthroughMember(t, review, "APP-T-0002"); member.Phase != "proof_blocked" || member.Recovery == nil || member.Recovery.Action != "rerun_checks" || !member.Recovery.Enabled {
		t.Fatalf("sibling lost its rerun_checks recovery: %#v", member)
	}
}
