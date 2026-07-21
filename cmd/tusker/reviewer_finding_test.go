package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// writeReviewLaneFindingForTest puts the canonical task into review with a
// reviewer-recorded failing verification row, mirroring a reviewer that flagged
// a problem before its review run exited clean.
func writeReviewLaneFindingForTest(t *testing.T, vault, taskID, findingNote string) {
	t.Helper()
	path := filepath.Join(vault, "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "review"
	data["readiness"] = "waiting_on_review"
	data["next_owner"] = "reviewer"
	data["proof_status"] = "satisfied"
	verification := strings.Join([]string{
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | go test ./cmd/tusker -count=1 | fail | " + findingNote + " |",
	}, "\n")
	body = upsertMarkdownSection(body, "## Verification", verification)
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}
}

// reconcileReviewFindingRun drives one poll over a review-lane run that exited
// clean while the canonical task carries a reviewer finding, and returns the
// resulting run row and reloaded task note.
func reconcileReviewFindingRun(t *testing.T, findingNote string) (RunStatus, Note, RegisteredProject, *Daemon) {
	t.Helper()
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Reviewer finding", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	writeReviewLaneFindingForTest(t, vault, "APP-T-0001", findingNote)
	project := registerAutomationTestProject(t, vault)

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneReview,
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-review",
		SessionRef:      "session-review",
		WorkspacePath:   t.TempDir(),
		StatusPath:      statusPath,
		AttemptCount:    1,
		UpdatedAt:       "2026-07-21T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Close() })
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	return run, note, project, daemon
}

// TestReviewerFindingReturnsToImplementer covers FAC-T-0006 A1: a recorded
// reviewer finding moves the work off review and back to its author.
func TestReviewerFindingReturnsToImplementer(t *testing.T) {
	run, note, _, _ := reconcileReviewFindingRun(t, "assertion did not hold")

	assertEqual(t, "rework", stringField(note.Data, "status"), "finding returns task to rework")
	assertEqual(t, "agent", stringField(note.Data, "next_owner"), "finding hands work back to the implementer")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "review run releases after returning finding")
	if !strings.Contains(run.LastError, reviewerFindingReturnReason) {
		t.Fatalf("expected finding-return reason on run, got %q", run.LastError)
	}
}

// TestReviewerFindingPastedIntoTask covers FAC-T-0006 A2: the reviewer's note is
// pasted into the task body the author sees.
func TestReviewerFindingPastedIntoTask(t *testing.T) {
	_, note, _, _ := reconcileReviewFindingRun(t, "output missing trailing newline")

	if !strings.Contains(note.Body, reviewerFindingSection) {
		t.Fatalf("expected reviewer-findings section in task body, got:\n%s", note.Body)
	}
	finding := sectionContent(note.Body, reviewerFindingSection)
	if !strings.Contains(finding, "output missing trailing newline") {
		t.Fatalf("expected reviewer note pasted into task, got:\n%s", finding)
	}
	if !strings.Contains(finding, "A1") {
		t.Fatalf("expected failing acceptance id in pasted finding, got:\n%s", finding)
	}
}

// TestFindingBlocksCloseUntilAdjudicated covers FAC-T-0006 A3: while the finding
// stands, the work is off review and close is refused; only after the
// implementer settles it and re-requests review can close proceed.
func TestFindingBlocksCloseUntilAdjudicated(t *testing.T) {
	_, note, _, _ := reconcileReviewFindingRun(t, "regression in edge case")
	vault := filepath.Dir(filepath.Dir(filepath.Dir(note.AbsolutePath)))

	err := closeV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "quiet": "true"})
	if err == nil {
		t.Fatal("expected close to be refused while the finding is unsettled")
	}
	if !strings.Contains(err.Error(), "review") {
		t.Fatalf("expected close refusal to cite review status, got %v", err)
	}

	// Adjudicate: implementer settles the finding and re-requests review.
	if err := statusV7Cmd(Args{"vault": vault, "quiet": "true", "local": "true", "id": "APP-T-0001", "status": "review", "by": "agent:test", "reason": "finding addressed"}); err != nil {
		t.Fatal(err)
	}
	settled, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "review", stringField(settled.Data, "status"), "task returns to review after adjudication")
}
