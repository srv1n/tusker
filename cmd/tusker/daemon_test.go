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
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newEpic(Args{"vault": vault, "acronym": "CAP", "title": "Caps", "summary": "Dispatch cap coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"First active story", "Second active story"} {
		if err := createWorkItem(Args{"vault": vault, "epic": "CAP", "title": title, "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "story"); err != nil {
			t.Fatal(err)
		}
	}
	if err := setStatus(Args{"vault": vault, "id": "CAP-S-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "CAP-S-0002", "status": "active", "actor": "test"}); err != nil {
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

	firstNote, err := resolveNote(vault, "CAP-S-0001")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        stringField(firstNote.Data, "record_id"),
		ItemID:          "CAP-S-0001",
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
		if run.ItemID == "CAP-S-0002" {
			secondState = run.LeaseState
			secondAttempts = run.AttemptCount
		}
	}
	assertEqual(t, 1, activeRuns, "active-state cap limits dispatching runs")
	assertEqual(t, string(LeaseStateUnclaimed), secondState, "second active story remains queued")
	assertEqual(t, 0, secondAttempts, "state-capped story does not burn an attempt")
}

func TestDaemonReleasesRunWhenTrackerStateBecomesIneligible(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newEpic(Args{"vault": vault, "acronym": "INT", "title": "Interrupts", "summary": "Ineligible state coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := createWorkItem(Args{"vault": vault, "epic": "INT", "title": "Move while running", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "story"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "INT-S-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(vault, "epics", "INT", "INT-S-0001.md")
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "in_review"
	content, err := serializeDocument(data, body, frontmatterOrderForType("story"))
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
		RecordID:        stringField(data, "record_id"),
		ItemID:          "INT-S-0001",
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

	run, err := store.FindRun("INT-S-0001")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil {
		t.Fatal("expected ineligible run row to remain inspectable")
	}
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "ineligible run released")
	assertEqual(t, string(AttemptOutcomeAbandoned), run.AttemptOutcome, "ineligible run outcome")
	if !strings.Contains(run.LastError, `tracker state "in_review" is not active`) {
		t.Fatalf("expected tracker-state release reason, got %q", run.LastError)
	}
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, stringField(data, "record_id"))
	if err != nil {
		t.Fatal(err)
	}
	decision := requireSupervisorDecision(t, decisions, string(SupervisorDecisionStopForAudit))
	if !strings.Contains(decision.Reason, `tracker state "in_review" is not active`) {
		t.Fatalf("expected stop_for_audit tracker reason, got %q", decision.Reason)
	}
}

func TestDaemonQueuesContinuationRetryWhenCleanExitLeavesNoteActive(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newEpic(Args{"vault": vault, "acronym": "CON", "title": "Continuation", "summary": "Continuation retry coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := createWorkItem(Args{"vault": vault, "epic": "CON", "title": "Stay active after clean exit", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "story"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "CON-S-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	note, err := resolveNote(vault, "CON-S-0001")
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
		RecordID:        stringField(note.Data, "record_id"),
		ItemID:          "CON-S-0001",
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

	run, err := store.FindRun("CON-S-0001")
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
	decisions, err := store.ListSupervisorDecisionsForRun(project.ProjectID, stringField(note.Data, "record_id"))
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
	updatedNote, err := resolveNote(vault, "CON-S-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "active", stringField(updatedNote.Data, "status"), "daemon does not force active note to review")
}

func TestDaemonEmitsNewRevisionDecisionAndClearsOldSession(t *testing.T) {
	tempRoot := t.TempDir()
	stateRoot := filepath.Join(tempRoot, "state")
	vault := filepath.Join(tempRoot, "vault")
	repo := filepath.Join(tempRoot, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newEpic(Args{"vault": vault, "acronym": "REV", "title": "Revision", "summary": "Revision reset coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := createWorkItem(Args{"vault": vault, "epic": "REV", "title": "Reset stale runtime", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "story"); err != nil {
		t.Fatal(err)
	}
	if err := setStatus(Args{"vault": vault, "id": "REV-S-0001", "status": "active", "actor": "test"}); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(vault, "epics", "REV", "REV-S-0001.md")
	data, body, err := parseFrontmatterMustRead(notePath)
	if err != nil {
		t.Fatal(err)
	}
	data["work_revision"] = 1
	data["risk"] = "critical"
	content, err := serializeDocument(data, body, frontmatterOrderForType("story"))
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
	recordID := stringField(data, "record_id")
	if err := store.UpsertRun(RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        recordID,
		ItemID:          "REV-S-0001",
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

	run, err := store.FindRun("REV-S-0001")
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
		LastMessageRef: "msg-1", WorkspacePath: workspace, CurrentItemID: "RES-S-0001", WorkRevision: 2,
		LastAttemptID: "attempt-old", State: "open", Resumable: true, StartedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{
		ProjectID: project.ProjectID, RecordID: "record-1", ItemID: "RES-S-0001", Runner: string(RunnerCodex),
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
		WorkspacePath: workspace, CurrentItemID: "RES-S-0001", WorkRevision: 2,
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
	blocker := Note{Data: map[string]any{"id": "DEP-S-0001", "record_id": "rec-blocker", "status": "active"}}
	blocked := Note{Data: map[string]any{
		"id":                    "DEP-S-0002",
		"risk":                  "medium",
		"blocked_by":            []any{"[[DEP-S-0001]]"},
		"blocked_by_record_ids": []any{"rec-blocker"},
	}}
	reason := dispatchEligibilityReason(blocked, map[string]Note{"DEP-S-0001": blocker}, map[string]Note{"rec-blocker": blocker})
	if !strings.Contains(reason, "waiting on DEP-S-0001") {
		t.Fatalf("expected unresolved dependency reason, got %q", reason)
	}
	blocker.Data["status"] = "done"
	reason = dispatchEligibilityReason(blocked, map[string]Note{"DEP-S-0001": blocker}, map[string]Note{"rec-blocker": blocker})
	assertEqual(t, "", reason, "resolved dependency allows dispatch")

	critical := Note{Data: map[string]any{"id": "DEP-S-0003", "risk": "critical"}}
	reason = dispatchEligibilityReason(critical, map[string]Note{}, map[string]Note{})
	if !strings.Contains(reason, "critical risk") {
		t.Fatalf("expected critical risk block, got %q", reason)
	}
}

func TestWriteReviewPacketEvidenceCreatesSubstantiveEvidence(t *testing.T) {
	vault := filepath.Join(t.TempDir(), "vault")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := newEpic(Args{"vault": vault, "acronym": "PKT", "title": "Packets", "summary": "Review packet coverage.", "owner": "sarav", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := createWorkItem(Args{"vault": vault, "epic": "PKT", "title": "Generate packet", "size": "m", "risk": "medium", "priority": "p1", "delegation": "execute", "assignee": "codex", "quiet": "true"}, "story"); err != nil {
		t.Fatal(err)
	}
	note, err := resolveNote(vault, "PKT-S-0001")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTurn(RunTurn{
		AttemptID: "attempt-1", ProjectID: "project-1", RecordID: stringField(note.Data, "record_id"),
		TurnID: "turn-1", TurnIndex: 0, Status: "completed", TotalTokens: 42, LastEventAt: "2026-04-28T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveRuntimeSupervisorDecision(RuntimeSupervisorDecision{
		DecisionID:       "decision-1",
		ProjectID:        "project-1",
		RecordID:         stringField(note.Data, "record_id"),
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
	run := RunStatus{
		ProjectID: "project-1", RecordID: stringField(note.Data, "record_id"), ItemID: "PKT-S-0001",
		Runner: string(RunnerCodex), ActiveAttemptID: "attempt-1", WorkspacePath: "/workspace", SessionRef: "thread-1",
		WorkRevision: 0, StartedAt: "2026-04-28T00:00:00Z", LastEventAt: "2026-04-28T00:00:01Z",
	}
	if err := writeReviewPacketEvidence(vault, note, run, store); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(vault, "Attachments", "PKT-S-0001", "review-packet-attempt-1.md")
	assertExists(t, packetPath)
	packet, err := readText(packetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(packet, "tokens=42") {
		t.Fatalf("expected review packet to include turn usage, got:\n%s", packet)
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
