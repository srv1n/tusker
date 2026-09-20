package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoveryWaveReviewKeepsEmptyBlockersArray(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"blockers":[]`) {
		t.Fatalf("empty blockers must remain an array in the UI contract: %s", raw)
	}
}

func TestRecoveryPriorReviewDoesNotSuppressNewTaskSnapshot(t *testing.T) {
	_, store, project := authorityFixture(t)
	raw, err := json.Marshal(ReviewResult{ProjectID: project.ProjectID, TaskID: "APP-T-0001", WorkRevision: 1, AttemptID: "review-old", TaskStateRev: "state-old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`INSERT INTO review_results(project_id,task_id,work_revision,attempt_id,result_json) VALUES(?,?,?,?,?)`, project.ProjectID, "APP-T-0001", 1, "review-old", string(raw)); err != nil {
		t.Fatal(err)
	}
	if found, err := store.HasReviewResultForWork(project.ProjectID, "APP-T-0001", 1, "state-old"); err != nil || !found {
		t.Fatalf("exact review snapshot not found: found=%v err=%v", found, err)
	}
	if found, err := store.HasReviewResultForWork(project.ProjectID, "APP-T-0001", 1, "state-new"); err != nil || found {
		t.Fatalf("historical review suppressed a new snapshot: found=%v err=%v", found, err)
	}
}

func TestRecoveryProjectsStaleReviewSnapshotAsRetryableFailure(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "review"
		data["work_revision"] = 1
		return data, body
	})
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, Terminal: true, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeSucceeded), WorkRevision: 1}); err != nil {
		t.Fatal(err)
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Members) != 1 || review.Members[0].Phase != "failed" || !strings.Contains(review.Members[0].WaitingReason, "current task snapshot") {
		t.Fatalf("stale review snapshot did not expose recovery: %#v", review.Members)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, LeaseState: string(LeaseStateParkedNoProgress), AttemptOutcome: string(AttemptOutcomeBlocked), WorkRevision: 1, LastError: "review proposal snapshot drifted"}); err != nil {
		t.Fatal(err)
	}
	review, err = buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Members[0].Phase != "failed" || review.Members[0].WaitingReason != "review proposal snapshot drifted" {
		t.Fatalf("parked reviewer failure did not expose retry: %#v", review.Members[0])
	}
}

func TestRecoveryProofIdentity(t *testing.T) {
	task := Note{Data: map[string]any{"id": "APP-T-0001", "work_revision": 2, "source_sha": "source-1"}, Body: "# Task\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | Works. |\n\n## Verification\n\n| Covers | Check | Result | Notes |\n| --- | --- | --- | --- |\n| A1 | command: true | pass | |\n"}
	task.Data["contract_fingerprint"] = directWaveTaskContractFingerprint(task.Data, task.Body)
	row := parseV7VerificationRows(task.Body)[0]
	row.Notes = v7VerificationReceiptSchema + " contract=" + task.Data["contract_fingerprint"].(string) + " work_revision=2 source=source-1 material=material-1 row=" + v7VerificationRowFingerprint(row)
	if cause := v7VerificationReceiptInvalidationForMaterial(task, row, "material-1", nil); cause != nil {
		t.Fatalf("unchanged receipt invalidated: %#v", cause)
	}
	if cause := v7VerificationReceiptInvalidationForMaterial(task, row, "", errors.New("workspace unavailable")); cause == nil || cause.Kind != "unavailable" || cause.Dimension != "material" || !strings.Contains(cause.Explanation, "does not prove that work changed") {
		t.Fatalf("unavailable material mislabeled: %#v", cause)
	}
	changed := task
	changed.Data = cloneNoteData(task.Data)
	changed.Data["source_sha"] = "source-2"
	if cause := v7VerificationReceiptInvalidationForMaterial(changed, row, "material-1", nil); cause == nil || cause.Kind != "changed" || cause.Dimension != "source" || cause.Previous != "source-1" || cause.Current != "source-2" {
		t.Fatalf("source change not identified: %#v", cause)
	}
	row.Result = "fail"
	row.Notes += "; " + v7VerificationReceiptSchema + " contract=next work_revision=2 source=source-1 material=material-2 row=next"
	if cause := v7VerificationReceiptInvalidationForMaterial(task, row, "material-2", nil); cause == nil || cause.Kind != "failed" || cause.Previous != "material-1" || cause.Current != "material-2" {
		t.Fatalf("failed rerun did not retain prior receipt identity: %#v", cause)
	}
}

func TestRecoveryAdmission(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.exec(`UPDATE run_directives SET state='consumed' WHERE project_id=? AND record_id=?`, project.ProjectID, "APP-T-0001"); err != nil {
		t.Fatal(err)
	}
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "review"
		return data, body
	})
	task := walkthroughNote(t, vault, "APP-T-0001")
	wave, err := resolveV7Note(vault, "W-0001", "wave")
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: runLaneReview, LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), AttemptCount: 1, Terminal: true}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Minute)
	active := run
	active.LeaseState, active.LeaseOwner = string(LeaseStateClaimed), "reviewer:live"
	if err := store.UpsertRun(active); err != nil {
		t.Fatal(err)
	}
	if refused, err := queueReviewRecovery(store, task, wave, active, "operator:test", 3, now, true); err != nil || !refused.Refused || !strings.Contains(refused.Reason, "owner is already active") {
		t.Fatalf("active owner recovery=%#v err=%v", refused, err)
	}
	if stored, _ := store.FindRunScoped(project.ProjectID, "APP-T-0001"); stored.LeaseState != string(LeaseStateClaimed) || stored.LeaseOwner != "reviewer:live" {
		t.Fatalf("active owner was mutated by refused recovery: %#v", stored)
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	first, err := queueReviewRecovery(store, task, wave, run, "operator:test", 3, now)
	if err != nil || !first.Admitted || first.Lane != runLaneReview {
		t.Fatalf("first recovery=%#v err=%v", first, err)
	}
	second, err := queueReviewRecovery(store, task, wave, run, "operator:test", 3, now)
	if err != nil || second.Admitted || !second.OK {
		t.Fatalf("duplicate recovery=%#v err=%v", second, err)
	}
	stored, _ := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if stored.AttemptCount != 1 || stored.Lane != runLaneReview || stored.Terminal || stored.LeaseState != string(LeaseStateUnclaimed) {
		t.Fatalf("review retry changed lane/budget: %#v", stored)
	}
	run.Terminal = true
	run.UpdatedAt = now.Add(time.Minute).Format(time.RFC3339Nano)
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if recovered, err := queueReviewRecovery(store, task, wave, run, "operator:test", 3, now.Add(2*time.Minute)); err != nil || !recovered.Admitted {
		t.Fatalf("terminal run did not replace its superseded queued directive: %#v err=%v", recovered, err)
	}
	run.AttemptCount = 3
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if exhausted, _ := queueReviewRecovery(store, task, wave, run, "operator:test", 3, now); !exhausted.Refused || !strings.Contains(exhausted.Reason, "exhausted") {
		t.Fatalf("exhaustion=%#v", exhausted)
	}
}

func TestRecoveryVerificationUsesSubmittedWorkspaceAndRetainsMaterialFence(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{
		"source_sha":    "source-1",
		"work_revision": 7,
		"owned_paths":   []string{"submitted.txt"},
	})
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		rows := parseV7VerificationRows(body)
		rows[0].Check, rows[0].Result, rows[0].Notes = "command: test -f submitted.txt", "pending", ""
		return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
	})

	// The registered repository is deliberately missing the submitted file.
	// Only the durable implementation workspace has it.
	submitted := filepath.Join(t.TempDir(), "submitted")
	if err := ensureDir(submitted); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, submitted, "init", "-q")
	runGitDir(t, submitted, "config", "user.email", "test@example.com")
	runGitDir(t, submitted, "config", "user.name", "Recovery Test")
	if err := writeText(filepath.Join(submitted, "submitted.txt"), "durable implementation\n"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, submitted, "add", "submitted.txt")
	runGitDir(t, submitted, "commit", "-m", "submitted implementation")
	if _, err := os.Stat(filepath.Join(v7RepoRoot(vault), "submitted.txt")); !os.IsNotExist(err) {
		t.Fatalf("base checkout unexpectedly contains submitted file: %v", err)
	}

	material, err := workspaceTreeStateHashForPaths(submitted, []string{"submitted.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{
		AttemptID: "execute-submitted", ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Lane: runLaneExecute, WorkRevision: 7, WorkspacePath: submitted, Outcome: string(AttemptOutcomeSucceeded),
		EndState: RunEndState{Schema: "tusker.run-end-state/v2", HeadSHA: "source-1", WorktreePath: submitted, MaterialFingerprint: material, MaterialScope: []string{"submitted.txt"}},
	}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", WorkRevision: 7}
	task := walkthroughNote(t, vault, "APP-T-0001")
	workspace, err := recoveryCommandVerificationWorkspace(store, vault, task, run)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.Path != submitted {
		t.Fatalf("recovery bound %q, want submitted workspace %q", workspace.Path, submitted)
	}

	updated, _, failures, err := executeV7CommandVerificationRowsInWorkspace(vault, task, Args{"rerun-invalid": "true"}, "reviewer:test", true, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("submitted workspace verification failed: %#v", failures)
	}
	rows := parseV7VerificationRows(updated.Body)
	if len(rows) != 1 || rows[0].Result != "pass" {
		t.Fatalf("recovery did not record a passing command receipt: %#v", rows)
	}

	// The binding remains a concurrent-change fence after a successful rerun.
	if err := writeText(filepath.Join(submitted, "submitted.txt"), "changed after verification\n"); err != nil {
		t.Fatal(err)
	}
	if err := workspace.Verify(); err == nil || !strings.Contains(err.Error(), "material changed") {
		t.Fatalf("changed implementation workspace was not rejected: %v", err)
	}
}
