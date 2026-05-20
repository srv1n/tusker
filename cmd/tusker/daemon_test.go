package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonEnforcesPerStateDispatchCap(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "CAP", "title": "Caps", "summary": "Dispatch cap coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"First active task", "Second active task"} {
		if err := newV5Task(Args{"vault": vault, "epic": "CAP", "title": title, "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
			t.Fatal(err)
		}
	}
	if err := setStatus(Args{"vault": vault, "id": "CAP-T-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "CAP-T-0002", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	updateWorkflowForDaemonTest(t, vault, map[string]int{"active": 1}, 3, `python3 -c 'import time; time.sleep(5)'`)

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalActiveRunLimit(3); err != nil {
		t.Fatal(err)
	}

	firstNote, err := resolveNote(vault, "CAP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        trackerRecordID(firstNote),
		ItemID:          "CAP-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "active-attempt",
		ProcessPID:      os.Getpid(),
		WorkRevision:    intField(firstNote.Data, "work_revision"),
		AttemptCount:    1,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	activeRuns := 0
	secondState := ""
	secondAttempts := -1
	for _, run := range runs {
		if isDispatchingLeaseState(run.LeaseState) {
			activeRuns++
		}
		if run.ItemID == "CAP-T-0002" {
			secondState = run.LeaseState
			secondAttempts = run.AttemptCount
		}
	}
	assertEqual(t, 1, activeRuns, "active-state cap limits dispatching runs")
	assertEqual(t, string(LeaseStateUnclaimed), secondState, "second active task remains queued")
	assertEqual(t, 0, secondAttempts, "state-capped task does not burn an attempt")
}

func TestDaemonReleasesRunWhenTrackerStateBecomesIneligible(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "INT", "title": "Interrupts", "summary": "Ineligible state coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "epic": "INT", "title": "Move while running", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "INT-T-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	setWorkflowReviewerEnabled(t, vault, false)
	notePath := filepath.Join(vault, "epics", "INT", "INT-T-0001.md")
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "review"
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(notePath, content); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        firstNonEmpty(stringField(data, "record_id"), stringField(data, "id")),
		ItemID:          "INT-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "ineligible-attempt",
		WorkRevision:    intField(data, "work_revision"),
		AttemptCount:    1,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run, err := store.FindRun("INT-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected ineligible run row to remain inspectable")
	}
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "ineligible run released")
	assertEqual(t, string(AttemptOutcomeAbandoned), run.AttemptOutcome, "ineligible run outcome")
	if !strings.Contains(run.LastError, `tracker state "review" is not active`) {
		t.Fatalf("expected tracker-state release reason, got %q", run.LastError)
	}
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, firstNonEmpty(stringField(data, "record_id"), stringField(data, "id")))
	if err != nil {
		t.Fatal(err)
	}
	decision := requireSupervisorDecision(t, decisions, string(SupervisorDecisionStopForAudit))
	if !strings.Contains(decision.Reason, `tracker state "review" is not active`) {
		t.Fatalf("expected stop_for_audit tracker reason, got %q", decision.Reason)
	}
}

func TestDaemonReconcilesCompletedReviewHandoff(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "HND", "title": "Handoff", "summary": "Review handoff coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "epic": "HND", "title": "Complete into review", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "HND-T-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	setWorkflowReviewerEnabled(t, vault, false)
	notePath := filepath.Join(vault, "epics", "HND", "HND-T-0001.md")
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "review"
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(notePath, content); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(tempRoot, "runner.status.json")
	eventSinkPath := filepath.Join(tempRoot, "runner.events.jsonl")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	recordID := firstNonEmpty(stringField(data, "record_id"), stringField(data, "id"))
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        recordID,
		ItemID:          "HND-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "review-handoff-attempt",
		EventSinkPath:   eventSinkPath,
		StatusPath:      statusPath,
		WorkRevision:    intField(data, "work_revision"),
		AttemptCount:    1,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run, err := store.FindRun("HND-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected completed review handoff run row")
	}
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "completed review handoff releases run")
	assertEqual(t, string(AttemptOutcomeSucceeded), run.AttemptOutcome, "completed review handoff succeeds")
	assertEqual(t, "", run.LastError, "completed review handoff is not abandoned as inactive")
	packetPath := filepath.Join(vault, "Attachments", "HND-T-0001", "review-packet-review-handoff-attempt.md")
	assertExists(t, packetPath)
	_, updatedBody, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updatedBody, "review-packet-review-handoff-attempt.md") {
		t.Fatalf("expected note evidence to link review packet, got:\n%s", updatedBody)
	}
}

func TestDaemonDispatchesReviewerLaneOnceForReviewTask(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "ARD", "title": "Agent review dispatch", "summary": "Reviewer lane coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "epic": "ARD", "title": "Review me", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "ARD-T-0001", "status": "review", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	updateWorkflowForDaemonTest(t, vault, map[string]int{}, 1, `python3 -c 'import sys; sys.exit(0)'`)

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run := waitForRunLeaseState(t, daemon, store, "ARD-T-0001", LeaseStateReleased)
	if run == nil {
		t.Fatal("expected reviewer run row")
	}
	assertEqual(t, runLaneReview, run.Lane, "review task dispatches reviewer lane")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "reviewer lane releases after clean exit")
	assertEqual(t, 1, run.AttemptCount, "reviewer lane uses one attempt")
	attempts, err := store.ListAttemptsForRun(project.ProjectID, "ARD-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected one review attempt, got %#v", attempts)
	}
	assertEqual(t, runLaneReview, attempts[0].Lane, "attempt records review lane")
	prompt, err := readText(attempts[0].PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"independent Tusker reviewer", "Auto-close allowed: yes", "tusker close ARD-T-0001 --by agent-reviewer"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected review prompt to contain %q, got:\n%s", expected, prompt)
		}
	}

	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err = store.FindRun("ARD-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 1, run.AttemptCount, "reviewer lane does not loop for the same review handoff")
}

func TestDaemonQueuesContinuationRetryWhenCleanExitLeavesNoteActive(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "CON", "title": "Continuation", "summary": "Continuation retry coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "epic": "CON", "title": "Stay active after clean exit", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "CON-T-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	note, err := resolveNote(vault, "CON-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(tempRoot, "runner.status.json")
	eventSinkPath := filepath.Join(tempRoot, "runner.events.jsonl")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        trackerRecordID(note),
		ItemID:          "CON-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "clean-active-attempt",
		EventSinkPath:   eventSinkPath,
		StatusPath:      statusPath,
		SessionRef:      "thread-1",
		WorkRevision:    intField(note.Data, "work_revision"),
		AttemptCount:    1,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run, err := store.FindRun("CON-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected continuation retry run row")
	}
	assertEqual(t, string(LeaseStateRetryQueued), run.LeaseState, "clean active exit queues continuation retry")
	assertEqual(t, string(AttemptOutcomeNone), run.AttemptOutcome, "clean active exit is not a failed attempt")
	if run.NextRetryAt == "" {
		t.Fatal("expected 1s continuation retry timestamp")
	}
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, trackerRecordID(note))
	if err != nil {
		t.Fatal(err)
	}
	decision := requireSupervisorDecision(t, decisions, string(SupervisorDecisionContinueThread))
	assertEqual(t, "clean-active-attempt", decision.ParentAttemptID, "continuation decision keeps parent attempt")
	assertEqual(t, "thread-1", decision.SessionRef, "continuation decision targets same session")
	eventText, err := readText(eventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eventText, `"kind":"supervisor_decision"`) || !strings.Contains(eventText, `"kind":"continue_thread"`) {
		t.Fatalf("expected supervisor decision event, got:\n%s", eventText)
	}
	updatedNote, err := resolveNote(vault, "CON-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "active", stringField(updatedNote.Data, "status"), "daemon does not force active note to review")
}

func TestDaemonCleanExitStopsForV7TerminalHumanWait(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Runtime stop.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Stop for human", "risk": "low", "priority": "p2", "status": "rework", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestDaemonCleanExitStopsForV7TerminalHumanWait -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos0": "APP-T-0001", "emit-packet": "true", "validate": "printf validation-ok"}, closeoutV7Cmd)
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(tempRoot, "runner.status.json")
	eventSinkPath := filepath.Join(tempRoot, "runner.events.jsonl")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        trackerRecordID(note),
		ItemID:          "APP-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "human-wait-attempt",
		EventSinkPath:   eventSinkPath,
		StatusPath:      statusPath,
		SessionRef:      "thread-human-wait",
		WorkRevision:    intField(note.Data, "work_revision"),
		AttemptCount:    1,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected run row")
	}
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "human wait releases lease")
	assertEqual(t, string(AttemptOutcomeWaitingForHuman), run.AttemptOutcome, "human wait outcome")
	assertEqual(t, "", run.NextRetryAt, "human wait does not queue retry")
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, trackerRecordID(note))
	if err != nil {
		t.Fatal(err)
	}
	requireSupervisorDecision(t, decisions, string(SupervisorDecisionStopForHuman))
}

func TestDaemonDispatchesReviewerForV7HumanClosePolicyWithoutCloseout(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	updateWorkflowForDaemonTest(t, vault, map[string]int{}, 1, `python3 -c 'import sys; sys.exit(0)'`)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Reviewer dispatch.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Human close policy review", "risk": "high", "priority": "p1", "status": "review", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestDaemonDispatchesReviewerForV7HumanClosePolicyWithoutCloseout -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	task := mustV7Task(t, vault, "APP-T-0001")
	idx := mustIndex(t, vault)
	if _, ok := v7LatestValidTerminalCloseout(vault, task, idx); ok {
		t.Fatal("test setup should not have a closeout")
	}
	projected := v7ProjectedTaskState(vault, task, idx)
	assertEqual(t, "waiting_on_review", stringField(projected, "readiness"), "projection stays review-owned until closeout")
	if stringField(projected, "agent_action") == "stop_until_human_response" {
		t.Fatalf("projection must not advertise human stop without closeout: %#v", projected)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run := waitForRunLeaseState(t, daemon, store, "APP-T-0001", LeaseStateReleased)
	if run == nil {
		t.Fatal("expected reviewer run row")
	}
	assertEqual(t, runLaneReview, run.Lane, "review task dispatches reviewer lane without closeout")
	assertEqual(t, string(AttemptOutcomeSucceeded), run.AttemptOutcome, "reviewer lane completed")
}

func TestDaemonCleanExitDoesNotStopForV7HumanWaitWithoutCloseout(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true"}, bootstrap)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "acronym": "APP", "title": "App V7", "summary": "Runtime stop.", "v7": "true"}, newV7Epic)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "No checkpoint", "risk": "low", "priority": "p2", "status": "rework", "proof-mode": "inline", "proof-required": "human_signoff", "v7": "true"}, newV7Task)
	mustV7Proof(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestDaemonCleanExitDoesNotStopForV7HumanWaitWithoutCloseout -count=1", "result": "pass", "note": "Machine proof passed."}, verifyV7AddCmd)
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(tempRoot, "runner.status.json")
	if err := writeRunnerStatusFile(statusPath, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        trackerRecordID(note),
		ItemID:          "APP-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateRunning),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "human-wait-attempt",
		StatusPath:      statusPath,
		SessionRef:      "thread-human-wait",
		WorkRevision:    intField(note.Data, "work_revision"),
		AttemptCount:    1,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRun("APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(LeaseStateRetryQueued), run.LeaseState, "missing closeout queues continuation")
	assertEqual(t, string(AttemptOutcomeNone), run.AttemptOutcome, "missing closeout outcome")
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, trackerRecordID(note))
	if err != nil {
		t.Fatal(err)
	}
	requireSupervisorDecision(t, decisions, string(SupervisorDecisionContinueThread))
}

func TestDaemonEmitsNewRevisionDecisionAndClearsOldSession(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "REV", "title": "Revision", "summary": "Revision reset coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "epic": "REV", "title": "Reset stale runtime", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "REV-T-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(vault, "epics", "REV", "REV-T-0001.md")
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		t.Fatal(err)
	}
	data["work_revision"] = 1
	data["risk"] = "critical"
	content, err := serializeDocument(data, body, frontmatterOrderForType("task"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(notePath, content); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := newRegisteredProject(repo, vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	recordID := firstNonEmpty(stringField(data, "record_id"), stringField(data, "id"))
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        recordID,
		ItemID:          "REV-T-0001",
		Runner:          "codex",
		LeaseState:      string(LeaseStateReleased),
		AttemptOutcome:  string(AttemptOutcomeSucceeded),
		ActiveAttemptID: "old-attempt",
		WorkspacePath:   filepath.Join(stateRoot, "workspaces", "old"),
		SessionRef:      "old-thread",
		WorkRevision:    0,
		AttemptCount:    2,
		StartedAt:       now,
		LastEventAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatal(err)
	}

	daemon := &Daemon{stateRoot: stateRoot, store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run, err := store.FindRun("REV-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected revision reset run row")
	}
	assertEqual(t, 1, run.WorkRevision, "run observes new work_revision")
	assertEqual(t, 0, run.AttemptCount, "new revision resets attempt count")
	assertEqual(t, "", run.SessionRef, "new revision clears old session ref")
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, recordID)
	if err != nil {
		t.Fatal(err)
	}
	decision := requireSupervisorDecision(t, decisions, string(SupervisorDecisionNewRevision))
	assertEqual(t, "old-attempt", decision.ParentAttemptID, "new_revision records parent attempt")
	assertEqual(t, "old-thread", decision.ParentSessionRef, "new_revision records old session")
	if !strings.Contains(decision.Reason, "work_revision changed from 0 to 1") {
		t.Fatalf("expected new_revision reason, got %q", decision.Reason)
	}
}

func TestResolveResumeSessionRequiresRevisionRunnerAndWorkspaceCompatibility(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project := RegisteredProject{ProjectID: "project-1"}
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.SaveSession(RunnerSession{
		ProjectID: project.ProjectID, RecordID: "record-1", Runner: string(RunnerCodex), SessionRef: "thread-ok",
		LastMessageRef: "msg-1", WorkspacePath: workspace, CurrentItemID: "RES-T-0001", WorkRevision: 2,
		LastAttemptID: "attempt-old", State: "open", Resumable: true, StartedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "record-1", ItemID: "RES-T-0001", Runner: string(RunnerCodex),
		LeaseState: string(LeaseStateRetryQueued), SessionRef: "thread-ok", WorkspacePath: workspace, WorkRevision: 2,
	}
	resolved, err := daemon.resolveResumeSession(project, Note{}, run)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "thread-ok", resolved.SessionRef, "compatible stored session resolves")
	assertEqual(t, string(SupervisorDecisionContinueThread), resolved.DecisionKind, "retry-queued same session is a continuation")

	staleRevision := run
	staleRevision.WorkRevision = 3
	resolved, err = daemon.resolveResumeSession(project, Note{}, staleRevision)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", resolved.SessionRef, "stale work_revision refuses resume")

	wrongWorkspace := run
	wrongWorkspace.WorkspacePath = filepath.Join(t.TempDir(), "other-workspace")
	if err := os.MkdirAll(wrongWorkspace.WorkspacePath, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err = daemon.resolveResumeSession(project, Note{}, wrongWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", resolved.SessionRef, "workspace mismatch refuses resume")

	if err := store.SaveSession(RunnerSession{
		ProjectID: project.ProjectID, RecordID: "record-1", Runner: string(RunnerClaude), SessionRef: "claude-session",
		WorkspacePath: workspace, CurrentItemID: "RES-T-0001", WorkRevision: 2,
		LastAttemptID: "claude-attempt", State: "open", Resumable: true, StartedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	claudeRun := run
	claudeRun.Runner = string(RunnerClaude)
	claudeRun.SessionRef = "claude-session"
	resolved, err = daemon.resolveResumeSession(project, Note{}, claudeRun)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolved.Reason, "best-effort") {
		t.Fatalf("expected Claude resume reason to mark best-effort continuity, got %q", resolved.Reason)
	}
}

func TestRetryClassificationDoesNotQueueHumanConfigAuthFailures(t *testing.T) {
	wf := defaultWorkflow()
	wf.Retry.MaxAttempts = 3
	wf.Retry.BackoffMS = []int{1}
	for _, reason := range []string{
		"authentication failed: invalid token",
		"sandbox approval denied by operator",
		"unsupported runner: local-human",
		"context window exceeded",
		"budget limit reached",
	} {
		run := RunStatus{LeaseState: string(LeaseStateRunning), AttemptCount: 1}
		updated := (&Daemon{}).scheduleRetry(run, wf, reason)
		assertEqual(t, string(LeaseStateReleased), updated.LeaseState, reason)
		assertEqual(t, string(AttemptOutcomeBlocked), updated.AttemptOutcome, reason)
		assertEqual(t, "", updated.NextRetryAt, reason)
	}

	transient := (&Daemon{}).scheduleRetry(RunStatus{LeaseState: string(LeaseStateRunning), AttemptCount: 1}, wf, "runner stalled: no codex events since 2026-04-28T00:00:00Z")
	assertEqual(t, string(LeaseStateRetryQueued), transient.LeaseState, "stalls remain retryable")
	assertEqual(t, string(AttemptOutcomeFailed), transient.AttemptOutcome, "stalls keep failed outcome")
}

func TestContextWindowFailureEmitsForkThreadDecision(t *testing.T) {
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: t.TempDir(), store: store}
	wf := defaultWorkflow()
	run := RunStatus{
		ProjectID: "project-1", RecordID: "record-1", ItemID: "ITEM-1",
		Runner: string(RunnerCodex), LeaseState: string(LeaseStateRunning),
		ActiveAttemptID: "attempt-1", SessionRef: "thread-1", WorkspacePath: "/workspace", AttemptCount: 1,
	}
	updated := daemon.scheduleRetry(run, wf, "context window exceeded")
	assertEqual(t, string(LeaseStateReleased), updated.LeaseState, "context pressure stops current attempt")
	decisions, err := store.ListSupervisorDecisionsForRun("project-1", "record-1")
	if err != nil {
		t.Fatal(err)
	}
	decision := requireSupervisorDecision(t, decisions, string(SupervisorDecisionForkThread))
	assertEqual(t, "context_pressure", decision.ContextSignal, "context pressure signal")
	assertEqual(t, "attempt-1", decision.ParentAttemptID, "fork decision parent attempt")
	assertEqual(t, "thread-1", decision.ParentSessionRef, "fork decision parent session")
}

func TestDispatchEligibilityBlocksUnresolvedDependenciesAndCriticalRisk(t *testing.T) {
	blocker := Note{Data: map[string]any{"id": "DEP-T-0001", "record_id": "rec-blocker", "status": "active"}}
	blocked := Note{Data: map[string]any{
		"id":                    "DEP-T-0002",
		"risk":                  "medium",
		"blocked_by":            []any{"[[DEP-T-0001]]"},
		"blocked_by_record_ids": []any{"rec-blocker"},
	}}
	reason := dispatchEligibilityReason(blocked, map[string]Note{"DEP-T-0001": blocker}, map[string]Note{"rec-blocker": blocker})
	if !strings.Contains(reason, "waiting on DEP-T-0001") {
		t.Fatalf("expected unresolved dependency reason, got %q", reason)
	}
	blocker.Data["status"] = "done"
	reason = dispatchEligibilityReason(blocked, map[string]Note{"DEP-T-0001": blocker}, map[string]Note{"rec-blocker": blocker})
	assertEqual(t, "", reason, "resolved dependency allows dispatch")

	critical := Note{Data: map[string]any{"id": "DEP-T-0003", "risk": "critical"}}
	reason = dispatchEligibilityReason(critical, map[string]Note{}, map[string]Note{})
	if !strings.Contains(reason, "critical risk") {
		t.Fatalf("expected critical risk block, got %q", reason)
	}
}

func TestWriteReviewPacketEvidenceCreatesSubstantiveEvidence(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrapLegacy(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Epic(Args{"vault": vault, "acronym": "PKT", "title": "Packets", "summary": "Review packet coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newV5Task(Args{"vault": vault, "epic": "PKT", "title": "Generate packet", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "feature"); err != nil {
		t.Fatal(err)
	}
	note, err := resolveNote(vault, "PKT-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTurn(RunTurn{
		AttemptID: "attempt-1", ProjectID: "project-1", RecordID: trackerRecordID(note),
		TurnID: "turn-1", TurnIndex: 0, Status: "completed", TotalTokens: 42, LastEventAt: "2026-04-28T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveRuntimeSupervisorDecision(RuntimeSupervisorDecision{
		DecisionID:       "decision-1",
		ProjectID:        "project-1",
		RecordID:         trackerRecordID(note),
		AttemptID:        "attempt-1",
		ParentAttemptID:  "attempt-0",
		SessionRef:       "thread-1",
		ParentSessionRef: "thread-0",
		Kind:             string(SupervisorDecisionResumeSession),
		Reason:           "resumed compatible stored session",
		ContextSignal:    "restart_recovery",
		TotalTokens:      42,
		CreatedAt:        "2026-04-28T00:00:01Z",
	}); err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(t.TempDir(), "attempt.events.jsonl")
	eventLog := NewEventLog(eventPath)
	if err := eventLog.Append("file_change", "attempt-1", RunnerCodex, map[string]any{
		"path":    "cmd/tusker/daemon.go",
		"turn_id": "turn-1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := eventLog.Append("verification_command", "attempt-1", RunnerCodex, map[string]any{
		"command":   "go test ./cmd/tusker -run TestWriteReviewPacketEvidenceCreatesSubstantiveEvidence -count=1",
		"status":    "passed",
		"exit_code": 0,
		"turn_id":   "turn-1",
	}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID: "project-1", RecordID: trackerRecordID(note), ItemID: "PKT-T-0001",
		Runner: string(RunnerCodex), ActiveAttemptID: "attempt-1", WorkspacePath: "/workspace", SessionRef: "thread-1",
		EventSinkPath: eventPath,
		RawLogPath:    filepath.Join(t.TempDir(), "attempt.raw.log"),
		StatusPath:    filepath.Join(t.TempDir(), "attempt.status.json"),
		PromptPath:    filepath.Join(t.TempDir(), "attempt.prompt.md"),
		WorkRevision:  0, StartedAt: "2026-04-28T00:00:00Z", LastEventAt: "2026-04-28T00:00:01Z",
	}
	if err := writeReviewPacketEvidence(vault, note, run, store); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(vault, "Attachments", "PKT-T-0001", "review-packet-attempt-1.md")
	assertExists(t, packetPath)
	packet, err := readText(packetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packet, "tokens=42") {
		t.Fatalf("expected review packet to include turn usage, got:\n%s", packet)
	}
	for _, expected := range []string{
		"- Attempt: attempt-1",
		"- Work revision: 0",
		"- Turns: 1",
		"## Runtime artifacts",
		"## Changed files",
		"`cmd/tusker/daemon.go` (event:file_change)",
		"## Verification",
		"`go test ./cmd/tusker -run TestWriteReviewPacketEvidenceCreatesSubstantiveEvidence -count=1` result=passed exit_code=0 turn=turn-1",
		"## Open risks",
	} {
		if !strings.Contains(packet, expected) {
			t.Fatalf("expected review packet to include %q, got:\n%s", expected, packet)
		}
	}
	if !strings.Contains(packet, "## Supervisor decisions") || !strings.Contains(packet, "`resume_session` reason=resumed compatible stored session") {
		t.Fatalf("expected review packet to include supervisor decisions, got:\n%s", packet)
	}
	_, body, err := parseFrontmatterMustRead(note.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "review-packet-attempt-1.md") {
		t.Fatalf("expected note evidence to link packet, got:\n%s", body)
	}
}

func updateWorkflowForDaemonTest(t *testing.T, vault string, stateCaps map[string]int, projectLimit int, command string) {
	t.Helper()
	filePath := workflowPath(vault)
	text, err := readText(filePath)
	if err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		t.Fatal(err)
	}
	agents, ok := data["agents"].(map[string]any)
	if !ok || agents == nil {
		agents = map[string]any{}
	}
	agents["max_concurrent_agents_by_state"] = stateCaps
	data["agents"] = agents
	runtimeBlock, ok := data["runtime"].(map[string]any)
	if !ok || runtimeBlock == nil {
		runtimeBlock = map[string]any{}
	}
	runtimeBlock["max_active_runs_per_project"] = projectLimit
	data["runtime"] = runtimeBlock
	codexBlock, ok := data["codex"].(map[string]any)
	if !ok || codexBlock == nil {
		codexBlock = map[string]any{}
	}
	codexBlock["command"] = command
	data["codex"] = codexBlock
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filePath, fm+"\n"+strings.TrimLeft(body, "\n")); err != nil {
		t.Fatal(err)
	}
}

func setWorkflowReviewerEnabled(t *testing.T, vault string, enabled bool) {
	t.Helper()
	filePath := workflowPath(vault)
	text, err := readText(filePath)
	if err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatter(text)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, ok := data["reviewer"].(map[string]any)
	if !ok || reviewer == nil {
		reviewer = map[string]any{}
	}
	reviewer["enabled"] = enabled
	data["reviewer"] = reviewer
	fm, err := stringifyFrontmatter(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(filePath, fm+"\n"+strings.TrimLeft(body, "\n")); err != nil {
		t.Fatal(err)
	}
}

func waitForRunLeaseState(t *testing.T, daemon *Daemon, store *RuntimeStore, identity string, state LeaseState) *RunStatus {
	t.Helper()
	var run *RunStatus
	for i := 0; i < 20; i++ {
		current, err := store.FindRun(identity)
		if err != nil {
			t.Fatal(err)
		}
		if current != nil && LeaseState(current.LeaseState) == state {
			return current
		}
		run = current
		time.Sleep(50 * time.Millisecond)
		if err := daemon.PollOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	return run
}

func requireSupervisorDecision(t *testing.T, decisions []SupervisorDecision, kind string) SupervisorDecision {
	t.Helper()
	for _, decision := range decisions {
		if decision.Kind == kind {
			return decision
		}
	}
	t.Fatalf("expected supervisor decision %q, got %#v", kind, decisions)
	return SupervisorDecision{}
}
