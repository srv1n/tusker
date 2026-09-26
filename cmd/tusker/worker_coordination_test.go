package main

import (
	"strings"
	"testing"
	"time"
)

func seedWorkerRun(t *testing.T, store *RuntimeStore, attemptID string, generation, revision int, provider, sessionID string) WorkerAttemptIdentity {
	t.Helper()
	if err := store.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1",
		Runner: "test", LeaseState: string(LeaseStateClaimed), ActiveAttemptID: attemptID,
		LeaseGeneration: generation, WorkRevision: revision,
	}); err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedExecution(ManagedExecutionInput{
		ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-1",
		AttemptID: attemptID, LeaseGeneration: generation, Provider: provider,
		ProviderSessionID: sessionID, Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
	return WorkerAttemptIdentity{
		ProjectID: "project-1", TaskID: "TASK-1", AttemptID: attemptID,
		Provider: provider, NativeSessionID: sessionID,
		WorkRevision: revision, AttemptGeneration: generation,
	}
}

func TestWorkerIdentityForResumedNativeSession(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	parent := seedWorkerRun(t, store, "attempt-1", 1, 2, "codex_exec", "native-1")
	if err := store.SaveAttempt(RunAttempt{AttemptID: parent.AttemptID, ProjectID: parent.ProjectID, RecordID: parent.TaskID, ItemID: parent.TaskID, Runner: parent.Provider, WorkRevision: 2}); err != nil {
		t.Fatal(err)
	}
	var parentExecution string
	if err := store.queryRowScan(`SELECT execution_id FROM execution_records WHERE attempt_id = ?`, []any{parent.AttemptID}, &parentExecution); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{ProjectID: parent.ProjectID, ExecutionID: parentExecution,
		Provider: parent.Provider, ProviderSessionID: parent.NativeSessionID}); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: parent.ProjectID, RecordID: parent.TaskID, ItemID: parent.TaskID, Runner: parent.Provider,
		ActiveAttemptID: "attempt-2", LeaseGeneration: 2, WorkRevision: 2, LeaseState: string(LeaseStateRunning), SessionRef: parent.NativeSessionID}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAttempt(RunAttempt{AttemptID: run.ActiveAttemptID, ParentAttemptID: parent.AttemptID, ProjectID: run.ProjectID,
		RecordID: run.RecordID, ItemID: run.ItemID, Runner: run.Runner, WorkRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateExecutionContinuation(ManagedExecutionInput{ProjectID: run.ProjectID, ParentExecutionID: parentExecution,
		TaskID: run.ItemID, AttemptID: run.ActiveAttemptID, LeaseGeneration: run.LeaseGeneration, Provider: run.Runner}, ExecutionResumeOf); err != nil {
		t.Fatal(err)
	}
	identity, err := store.WorkerIdentityForRun(run)
	if err != nil || identity == nil || identity.NativeSessionID != parent.NativeSessionID || identity.AttemptID != run.ActiveAttemptID {
		t.Fatalf("resumed identity=%#v err=%v", identity, err)
	}
	event, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: *identity, Kind: "progress", Milestone: "resumed"})
	if err != nil || event.Stale {
		t.Fatalf("resumed event=%#v err=%v", event, err)
	}
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: *identity, Kind: "question", Body: "continue?", IdempotencyKey: "resumed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatalf("resumed delivery claim=%v err=%v", claimed, err)
	}
	if err := store.MarkWorkerDeliveryReply(delivery.DeliveryID, event.EventID, "2026-08-01T00:00:00Z"); err != nil {
		t.Fatalf("resumed delivery reply: %v", err)
	}
	if err := store.ApplyWorkerDeliveryDecision(delivery.DeliveryID, time.Now()); err != nil {
		t.Fatalf("resumed delivery apply: %v", err)
	}
	for name, mutate := range map[string]func(*WorkerAttemptIdentity){
		"predecessor attempt": func(i *WorkerAttemptIdentity) {
			i.AttemptID = parent.AttemptID
			i.AttemptGeneration = parent.AttemptGeneration
		},
		"wrong project":    func(i *WorkerAttemptIdentity) { i.ProjectID = "other" },
		"wrong task":       func(i *WorkerAttemptIdentity) { i.TaskID = "OTHER" },
		"wrong provider":   func(i *WorkerAttemptIdentity) { i.Provider = "muse" },
		"wrong session":    func(i *WorkerAttemptIdentity) { i.NativeSessionID = "unrelated" },
		"wrong generation": func(i *WorkerAttemptIdentity) { i.AttemptGeneration++ },
		"wrong revision":   func(i *WorkerAttemptIdentity) { i.WorkRevision++ },
	} {
		invalid := *identity
		mutate(&invalid)
		stale, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: invalid, Kind: "progress"})
		if err != nil || !stale.Stale {
			t.Fatalf("%s event=%#v err=%v", name, stale, err)
		}
	}
	if _, err := store.exec(`UPDATE runs SET lease_state = ? WHERE project_id = ? AND record_id = ?`, string(LeaseStateUnclaimed), run.ProjectID, run.RecordID); err != nil {
		t.Fatal(err)
	}
	stale, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: *identity, Kind: "progress"})
	if err != nil || !stale.Stale {
		t.Fatalf("released lease event=%#v err=%v", stale, err)
	}
}

func TestWorkerCoordinationIdentityFence(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")

	accepted, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{
		Identity: identity, Kind: "progress", Milestone: "m1", Status: "working", OccurredAt: "2026-08-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Stale {
		t.Fatal("exact identity must be accepted as current")
	}

	mutations := map[string]func(WorkerAttemptIdentity) WorkerAttemptIdentity{
		"wrong session":    func(i WorkerAttemptIdentity) WorkerAttemptIdentity { i.NativeSessionID = "devin-other"; return i },
		"wrong provider":   func(i WorkerAttemptIdentity) WorkerAttemptIdentity { i.Provider = "muse"; return i },
		"wrong attempt":    func(i WorkerAttemptIdentity) WorkerAttemptIdentity { i.AttemptID = "attempt-2"; return i },
		"wrong generation": func(i WorkerAttemptIdentity) WorkerAttemptIdentity { i.AttemptGeneration = 2; return i },
		"wrong revision":   func(i WorkerAttemptIdentity) WorkerAttemptIdentity { i.WorkRevision = 2; return i },
	}
	for name, mutate := range mutations {
		event, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{
			Identity: mutate(identity), Kind: "progress", Milestone: "late", OccurredAt: "2026-08-01T00:05:00Z",
		})
		if err != nil {
			t.Fatalf("%s: stale evidence must still be stored: %v", name, err)
		}
		if !event.Stale {
			t.Fatalf("%s: event must be stored with stale=true", name)
		}
	}

	attention, events, _, err := store.WorkerAttemptStatus(identity, time.Date(2026, 8, 1, 0, 1, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if attention.LastMilestone != "m1" {
		t.Fatalf("stale events must not update attention milestone, got %q", attention.LastMilestone)
	}
	if len(events) != 1 {
		t.Fatalf("status must list only events for the exact attempt identity, got %d", len(events))
	}
}

func TestWorkerAttentionSilenceAndProviderActivityClears(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	t0 := time.Now().UTC()

	if _, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{
		Identity: identity, Kind: "progress", Milestone: "m1", OccurredAt: t0.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	attention, err := store.EvaluateWorkerAttention(identity, t0.Add(11*time.Minute), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !attention.AttentionRequired {
		t.Fatal("silence past the interval must require attention")
	}
	if attention.RecommendedAction != "inspect provider session devin:devin-1" {
		t.Fatalf("unexpected recommended action %q", attention.RecommendedAction)
	}
	if attention.SilenceSince == "" {
		t.Fatal("silence_since must record when silence began")
	}
	run, err := store.FindRun("TASK-1")
	if err != nil || run == nil {
		t.Fatal(err)
	}
	if run.LeaseState != string(LeaseStateClaimed) || run.AttemptOutcome != string(AttemptOutcomeNone) || run.ActiveAttemptID != "attempt-1" || run.NextRetryAt != "" {
		t.Fatalf("attention evaluation must not mutate lease, outcome, or retry: %#v", run)
	}

	if _, _, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity: identity, ProviderEventID: "msg-1", Source: "devin", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	attention, err = store.EvaluateWorkerAttention(identity, time.Now().UTC().Add(time.Minute), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if attention.AttentionRequired {
		t.Fatal("provider activity must clear attention")
	}
}

func TestWorkerDeliveryIdempotencyAndTransitions(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")

	delivery, duplicate, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "continue?", IdempotencyKey: "k1"})
	if err != nil || duplicate {
		t.Fatalf("first put: %v duplicate=%v", err, duplicate)
	}
	again, duplicate, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "continue?", IdempotencyKey: "k1"})
	if err != nil || !duplicate || again.DeliveryID != delivery.DeliveryID {
		t.Fatalf("identical replay must be a duplicate of the same delivery: %v %#v", err, again)
	}
	if _, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "different", IdempotencyKey: "k1"}); err == nil {
		t.Fatal("same key with a different request must be rejected")
	}
	if _, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: strings.Repeat("x", workerDeliveryBodyLimit+1), IdempotencyKey: "big"}); err == nil {
		t.Fatal("body over 32KiB must be rejected")
	}

	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatalf("claim must take the stored delivery: claimed=%v err=%v", claimed, err)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, map[string]any{"session_id": "devin-1", "request_id": "req-1"}, "2026-08-01T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryReply(delivery.DeliveryID, "evt-reply", "2026-08-01T00:00:02Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryUncertain(delivery.DeliveryID, "late ambiguity"); err == nil {
		t.Fatal("uncertain must not follow replied (transitions are monotonic)")
	}
	if err := store.ApplyWorkerDeliveryDecision(delivery.DeliveryID, time.Date(2026, 8, 1, 0, 0, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	stored, found, err := store.WorkerDelivery(delivery.DeliveryID)
	if err != nil || !found {
		t.Fatal(err)
	}
	if stored.State != "applied" || stored.DecisionAppliedAt == "" {
		t.Fatalf("expected applied delivery, got %#v", stored)
	}
	if err := store.MarkWorkerDeliveryUncertain(delivery.DeliveryID, "nope"); err == nil {
		t.Fatal("applied delivery must not regress to uncertain")
	}
}

func TestWorkerUncertainDeliveryIsTerminalForSending(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "do it", IdempotencyKey: "k-uncertain"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatalf("claim must take the stored delivery: claimed=%v err=%v", claimed, err)
	}
	if err := store.MarkWorkerDeliveryUncertain(delivery.DeliveryID, "post acknowledgement lost"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, nil, workerNow()); err == nil {
		t.Fatal("uncertain must not transition back to accepted without observation")
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || claimed {
		t.Fatal("uncertain delivery must not be claimable for resend")
	}
}

func TestWorkerSupersededAttemptKeepsStaleEvidence(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")

	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: old, Kind: "question", Body: "?", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, nil, "2026-08-01T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryReply(delivery.DeliveryID, "evt-1", "2026-08-01T00:00:02Z"); err != nil {
		t.Fatal(err)
	}

	if err := store.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1",
		Runner: "test", LeaseState: string(LeaseStateClaimed), ActiveAttemptID: "attempt-2",
		LeaseGeneration: 2, WorkRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "root-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedExecution(ManagedExecutionInput{
		ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-1",
		AttemptID: "attempt-2", LeaseGeneration: 2, Provider: "devin", ProviderSessionID: "devin-2", Source: "test",
	}); err != nil {
		t.Fatal(err)
	}

	late, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{
		Identity: old, Kind: "completed", Milestone: "done", OccurredAt: "2026-08-01T00:10:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !late.Stale {
		t.Fatal("late event for a superseded attempt must be stored stale")
	}
	if err := store.ApplyWorkerDeliveryDecision(delivery.DeliveryID, time.Date(2026, 8, 1, 0, 11, 0, 0, time.UTC)); err == nil {
		t.Fatal("stale delivery must not apply a decision")
	}
	stored, _, _ := store.WorkerDelivery(delivery.DeliveryID)
	if stored.DecisionAppliedAt != "" || stored.State != "replied" {
		t.Fatalf("stale delivery must keep decision_applied_at empty, got %#v", stored)
	}
	attention, err := store.EvaluateWorkerAttention(old, time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if attention.AttentionRequired {
		t.Fatal("attention only applies to the active current identity")
	}
}

func TestWorkerCoordinationSurvivesStoreReopen(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	if _, err := store.RecordWorkerCoordinationEvent(WorkerCoordinationEvent{Identity: identity, Kind: "progress", Milestone: "m1", OccurredAt: "2026-08-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{Identity: identity, ProviderEventID: "msg-1", Cursor: "cur-1", Source: "devin", OccurredAt: "2026-08-01T00:00:01Z"}); err != nil {
		t.Fatal(err)
	}
	delivery, _, err := store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "question", Body: "?", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := store.ClaimWorkerDelivery(delivery.DeliveryID); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := store.MarkWorkerDeliveryUncertain(delivery.DeliveryID, "lost"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EvaluateWorkerAttention(identity, time.Now().UTC().Add(time.Hour), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	attention, events, deliveries, err := reopened.WorkerAttemptStatus(identity, time.Now().UTC().Add(time.Hour), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Milestone != "m1" {
		t.Fatalf("events must survive reopen, got %#v", events)
	}
	if len(deliveries) != 1 || deliveries[0].State != "uncertain" {
		t.Fatalf("uncertain delivery must survive reopen, got %#v", deliveries)
	}
	if !attention.AttentionRequired {
		t.Fatal("persisted attention evaluation must survive reopen")
	}
	cursor, err := reopened.WorkerProviderCursor(identity)
	if err != nil || cursor != "cur-1" {
		t.Fatalf("cursor must survive reopen, got %q err=%v", cursor, err)
	}
}

func TestWorkerAttentionBaselineFromAttemptStart(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	identity := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	if err := store.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "test",
		LeaseState: string(LeaseStateRunning), ActiveAttemptID: "attempt-1", LeaseGeneration: 1, WorkRevision: 1,
		StartedAt: "2020-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	attention, err := store.EvaluateWorkerAttention(identity, time.Now().UTC(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !attention.AttentionRequired {
		t.Fatal("a current attempt with no activity and an old start must require attention")
	}
	if attention.LastActivityAt != "2020-01-01T00:00:00Z" {
		t.Fatalf("baseline must come from the run start, got %q", attention.LastActivityAt)
	}
}

func TestWorkerDuplicateProviderActivityAcrossGenerations(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gen1 := seedWorkerRun(t, store, "attempt-1", 1, 1, "devin", "devin-1")
	_, duplicate, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity: gen1, ProviderEventID: "evt-shared", Source: "devin", OccurredAt: "2026-08-01T00:00:00Z",
	})
	if err != nil || duplicate {
		t.Fatalf("first record: %v duplicate=%v", err, duplicate)
	}
	if err := store.UpsertRun(RunStatus{
		ProjectID: "project-1", RecordID: "TASK-1", ItemID: "TASK-1", Runner: "test",
		LeaseState: string(LeaseStateClaimed), ActiveAttemptID: "attempt-2", LeaseGeneration: 2, WorkRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	root, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", DisplayName: "root-2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedExecution(ManagedExecutionInput{
		ProjectID: "project-1", ParentExecutionID: root.ExecutionID, TaskID: "TASK-1",
		AttemptID: "attempt-2", LeaseGeneration: 2, Provider: "devin", ProviderSessionID: "devin-1", Source: "test",
	}); err != nil {
		t.Fatal(err)
	}
	gen2 := WorkerAttemptIdentity{ProjectID: "project-1", TaskID: "TASK-1", AttemptID: "attempt-2", Provider: "devin", NativeSessionID: "devin-1", WorkRevision: 1, AttemptGeneration: 2}
	replay, duplicate, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
		Identity: gen2, ProviderEventID: "evt-shared", Cursor: "cur-x", Source: "devin", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil || !duplicate {
		t.Fatalf("same provider event id must dedupe across generations: %v duplicate=%v", err, duplicate)
	}
	if replay.Identity.AttemptID != "attempt-1" {
		t.Fatal("dedupe must return the original immutable event")
	}
	cursor, err := store.WorkerProviderCursor(gen2)
	if err != nil || cursor != "" {
		t.Fatalf("duplicate from a prior generation must not advance generation 2 cursor, got %q", cursor)
	}
}
