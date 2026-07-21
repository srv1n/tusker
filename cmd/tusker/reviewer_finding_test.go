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
	writeReviewLaneFindingWithMarker(t, vault, taskID, findingNote, "attempt-review")
}

// writeReviewLaneFindingWithMarker is like writeReviewLaneFindingForTest but
// stamps the failing row with the marker for an arbitrary review attempt, so a
// test can plant a stale fail row that the CURRENT round should ignore.
func writeReviewLaneFindingWithMarker(t *testing.T, vault, taskID, findingNote, markerAttempt string) {
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
	note := findingNote
	if strings.TrimSpace(markerAttempt) != "" {
		note += " " + reviewerFindingRowMarker(markerAttempt)
	}
	verification := strings.Join([]string{
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | go test ./cmd/tusker -count=1 | fail | " + note + " |",
	}, "\n")
	body = replaceSection(body, "## Verification", verification)
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
	return reconcileReviewFindingRunOpts(t, reviewFindingRunOpts{findingNote: findingNote})
}

// reviewFindingRunOpts tunes the review-lane reconcile driver.
type reviewFindingRunOpts struct {
	findingNote string
	// markerAttempt overrides the attempt id stamped on the failing row. Empty
	// means "attempt-review" (the current round). A different value plants a
	// stale row from another round; the empty-string sentinel below drops the
	// marker entirely.
	markerAttempt string
	// noMarker leaves the failing row unmarked, simulating a pre-existing fail
	// row not attributable to the current review attempt.
	noMarker bool
	// dirtyWorkspace writes a stray file into the review workspace so the
	// dirty-workspace guard fires.
	dirtyWorkspace bool
}

func reconcileReviewFindingRunOpts(t *testing.T, opts reviewFindingRunOpts) (RunStatus, Note, RegisteredProject, *Daemon) {
	t.Helper()
	vault := automationTestVault(t)
	disableReviewerForTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Reviewer finding", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	markerAttempt := opts.markerAttempt
	if markerAttempt == "" {
		markerAttempt = "attempt-review"
	}
	if opts.noMarker {
		markerAttempt = ""
	}
	writeReviewLaneFindingWithMarker(t, vault, "APP-T-0001", opts.findingNote, markerAttempt)
	project := registerAutomationTestProject(t, vault)

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(t.TempDir(), "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if opts.dirtyWorkspace {
		if err := writeText(filepath.Join(workspace, "stray-change.txt"), "uncommitted reviewer edit"); err != nil {
			t.Fatal(err)
		}
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
		WorkspacePath:   workspace,
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

// TestReviewerFindingBounceReportsBlocked covers finding 4: a findings bounce is
// a "review did not pass" outcome, not a success. The run must not be released
// as succeeded-with-LastError; it mirrors the dirty sibling branch (Blocked)
// and stays non-terminal so the bounce reads honestly.
func TestReviewerFindingBounceReportsBlocked(t *testing.T) {
	run, _, _, _ := reconcileReviewFindingRun(t, "assertion did not hold")

	assertEqual(t, string(AttemptOutcomeBlocked), run.AttemptOutcome, "findings bounce reports Blocked, not Succeeded")
	if run.Terminal {
		t.Fatal("findings bounce must not mark the run terminal-succeeded")
	}
}

// TestStaleFindingRowDoesNotBounce covers finding 2 (round/author gating): a
// pre-existing fail row not attributable to the current review attempt must NOT
// bounce fresh work.
func TestStaleFindingRowDoesNotBounce(t *testing.T) {
	// A fail row marked by an earlier round's attempt id.
	run, note, _, _ := reconcileReviewFindingRunOpts(t, reviewFindingRunOpts{
		findingNote:   "stale failure from a prior round",
		markerAttempt: "attempt-earlier-round",
	})
	if strings.EqualFold(stringField(note.Data, "status"), "rework") {
		t.Fatal("stale fail row from another round must not bounce the task to rework")
	}
	if strings.Contains(run.LastError, reviewerFindingReturnReason) {
		t.Fatalf("stale fail row must not trigger a findings bounce, got %q", run.LastError)
	}

	// An unmarked fail row (no attempt attribution) is likewise ignored.
	run2, note2, _, _ := reconcileReviewFindingRunOpts(t, reviewFindingRunOpts{
		findingNote: "unattributed failure",
		noMarker:    true,
	})
	if strings.EqualFold(stringField(note2.Data, "status"), "rework") {
		t.Fatal("unmarked fail row must not bounce the task to rework")
	}
	if strings.Contains(run2.LastError, reviewerFindingReturnReason) {
		t.Fatalf("unmarked fail row must not trigger a findings bounce, got %q", run2.LastError)
	}
}

// TestBlockedRowIsNotAFinding covers finding 1: blocked-on-external is a
// legitimate state, not a reviewer rejection, so a blocked row is not a finding.
func TestBlockedRowIsNotAFinding(t *testing.T) {
	marker := reviewerFindingRowMarker("attempt-review")
	body := strings.Join([]string{
		"## Verification",
		"",
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | external dependency | blocked | waiting on upstream " + marker + " |",
	}, "\n")
	if _, ok := reviewerFindingFromTask(Note{Body: body}, "attempt-review"); ok {
		t.Fatal("a blocked row must not count as a reviewer finding")
	}
}

// TestDirtyWorkspaceTakesPrecedenceOverFinding covers finding 3: when the
// reviewer workspace is dirty AND a finding exists, the dirty-workspace handling
// must run so reviewer changes are not silently abandoned; the task stays in
// review rather than bouncing to rework.
func TestDirtyWorkspaceTakesPrecedenceOverFinding(t *testing.T) {
	run, note, _, _ := reconcileReviewFindingRunOpts(t, reviewFindingRunOpts{
		findingNote:    "output missing trailing newline",
		dirtyWorkspace: true,
	})
	assertEqual(t, "review", stringField(note.Data, "status"), "dirty workspace keeps the task in review, not rework")
	assertEqual(t, string(AttemptOutcomeBlocked), run.AttemptOutcome, "dirty workspace blocks for audit")
	if strings.Contains(run.LastError, reviewerFindingReturnReason) {
		t.Fatalf("dirty workspace must take precedence over the findings bounce, got %q", run.LastError)
	}
	if !strings.Contains(run.LastError, "dirty") {
		t.Fatalf("expected dirty-workspace reason on run, got %q", run.LastError)
	}
}

// TestFindingBounceIsIdempotent covers finding 5: re-detecting the same finding
// on a task already in rework must not double-append the section or re-bounce.
func TestFindingBounceIsIdempotent(t *testing.T) {
	_, note, project, _ := reconcileReviewFindingRun(t, "regression in edge case")
	vault := project.VaultRoot

	before := sectionContent(note.Body, reviewerFindingSection)
	finding := "- A1: go test ./cmd/tusker -count=1 -> fail (regression in edge case)"
	if err := returnReviewerFindingToImplementer(vault, "APP-T-0001", finding, "daemon:reviewer-finding"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "rework", stringField(reloaded.Data, "status"), "task stays in rework after idempotent re-detection")
	if got := strings.Count(reloaded.Body, reviewerFindingGeneratedMarker); got != 1 {
		t.Fatalf("expected exactly one generated findings section, got %d", got)
	}
	after := sectionContent(reloaded.Body, reviewerFindingSection)
	if after == "" || before == "" {
		t.Fatalf("expected findings section present before and after, before=%q after=%q", before, after)
	}
}

// TestGeneratedSectionDoesNotClobberUserSection covers finding 6: a
// user-authored "## Reviewer findings" section carries no generated marker and
// must survive; the generated section is namespaced and lives independently.
func TestGeneratedSectionDoesNotClobberUserSection(t *testing.T) {
	userBody := strings.Join([]string{
		"## Reviewer findings",
		"",
		"My own notes about past reviews that the daemon must not touch.",
		"",
		"## Notes",
		"",
		"trailing content",
	}, "\n")
	out := upsertGeneratedReviewerFindingSection(userBody, reviewerFindingGeneratedMarker+"\n\ngenerated finding text")

	if !strings.Contains(out, "My own notes about past reviews that the daemon must not touch.") {
		t.Fatalf("user-authored section was clobbered:\n%s", out)
	}
	if !strings.Contains(out, "generated finding text") {
		t.Fatalf("generated section missing:\n%s", out)
	}
	if !strings.Contains(out, "trailing content") {
		t.Fatalf("trailing user content was lost:\n%s", out)
	}
	if got := generatedReviewerFindingContent(out); !strings.Contains(got, "generated finding text") {
		t.Fatalf("generated content lookup returned the wrong section: %q", got)
	}
	// Re-running must replace the generated section in place, not stack another.
	out2 := upsertGeneratedReviewerFindingSection(out, reviewerFindingGeneratedMarker+"\n\nsecond generated finding")
	if got := strings.Count(out2, reviewerFindingGeneratedMarker); got != 1 {
		t.Fatalf("expected one generated marker after re-upsert, got %d:\n%s", got, out2)
	}
	if !strings.Contains(out2, "My own notes about past reviews that the daemon must not touch.") {
		t.Fatalf("user section lost on re-upsert:\n%s", out2)
	}
}

// TestFindingScanIgnoresFencedCodeBlocks covers finding 6: fenced code blocks
// inside the verification section must not be parsed as live table rows or
// heading boundaries.
func TestFindingScanIgnoresFencedCodeBlocks(t *testing.T) {
	marker := reviewerFindingRowMarker("attempt-review")
	body := strings.Join([]string{
		"## Verification",
		"",
		"```",
		"| A9 | fenced sample | fail | this is documentation " + marker + " |",
		"## Not a real heading",
		"```",
		"",
		"| Covers | Check | Result | Notes |",
		"|---|---|---|---|",
		"| A1 | real check | fail | genuine finding " + marker + " |",
	}, "\n")
	finding, ok := reviewerFindingFromTask(Note{Body: body}, "attempt-review")
	if !ok {
		t.Fatal("expected the real fail row to be detected")
	}
	if strings.Contains(finding, "fenced sample") || strings.Contains(finding, "A9") {
		t.Fatalf("fenced code block row must be ignored, got:\n%s", finding)
	}
	if !strings.Contains(finding, "A1") || !strings.Contains(finding, "genuine finding") {
		t.Fatalf("expected the genuine finding, got:\n%s", finding)
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
