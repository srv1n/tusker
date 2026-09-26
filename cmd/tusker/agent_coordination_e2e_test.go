package main

import (
	"testing"
	"time"
)

func TestAgentCoordinationE2EFiveTasksTwoWaves(t *testing.T) {
	s, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	architect := AgentAddress{Kind: "task", ID: "architect"}
	q, _, err := s.PutAgentMessage(AgentMessage{IdempotencyKey: "task4-question", ProjectID: "app", Sender: "task4", Recipient: architect, Kind: "question", Body: "Need decision", ReplyRequired: true, YieldSender: true})
	if err != nil {
		t.Fatal(err)
	}
	wake, dup, err := s.QueueAgentWakeup("app", architect, "blocking-clarification", "question:"+q.ID, []string{q.ID})
	if err != nil || dup || wake.ModelTurns != 0 {
		t.Fatalf("wake=%#v dup=%v err=%v", wake, dup, err)
	}
	duplicateWake, dup, err := s.QueueAgentWakeup("app", architect, "blocking-clarification", "question:"+q.ID, []string{q.ID})
	if err != nil || !dup || duplicateWake.ID != wake.ID {
		t.Fatalf("duplicate wake dup=%v err=%v", dup, err)
	}
	if _, _, err := s.QueueAgentWakeup("app", architect, "batch", "batch", []string{q.ID, "another"}); err == nil {
		t.Fatal("multi-message wakeup accepted")
	}
	report := ArchitectReport{ObjectiveID: "objective", Outcome: "stalled", Accepted: []string{"task1", "task2", "task3", "task5"}, Blockers: []string{"task4: question pending"}, PendingQuestions: []string{q.ID}, Evidence: []string{"checks pass"}}
	_, dup, err = s.RecordArchitectContinuation("app", "wave1-rev1", report)
	if err != nil || dup {
		t.Fatal(err)
	}
	firstID, _, _ := s.RecordArchitectContinuation("app", "wave1-rev2", report)
	duplicateID, dup, err := s.RecordArchitectContinuation("app", "wave1-rev2", report)
	if err != nil || !dup || duplicateID != firstID {
		t.Fatalf("next wave duplicated: %v", err)
	}
}

func TestClarificationWorkflowReplyBeforeYield(t *testing.T) {
	s, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	q, _, _ := s.PutAgentMessage(AgentMessage{IdempotencyKey: "q", ProjectID: "app", Sender: "worker", Recipient: AgentAddress{Kind: "task", ID: "architect"}, Kind: "question", Body: "?", ReplyRequired: true, YieldSender: true})
	_, _, err = s.PutAgentMessage(AgentMessage{IdempotencyKey: "a", ProjectID: "app", Sender: "architect", Recipient: AgentAddress{Kind: "task", ID: "worker"}, Kind: "answer", Body: "continue", ReplyTo: q.ID})
	if err != nil {
		t.Fatal(err)
	}
	q, err = s.AgentMessage("app", q.ID)
	if err != nil || q.AnsweredAt == "" {
		t.Fatal("answer was not visible before yield")
	}
}

func TestAgentClarificationDaemonSchedulesCurrentTaskOwnerWithoutModelBookkeeping(t *testing.T) {
	s, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: s}
	defer s.Close()
	if err = s.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertRun(RunStatus{ProjectID: "app", RecordID: "task4", ItemID: "task4", LeaseState: string(LeaseStateReleased)}); err != nil {
		t.Fatal(err)
	}
	q, _, err := s.PutAgentMessage(AgentMessage{IdempotencyKey: "answer", ProjectID: "app", Sender: "architect", Recipient: AgentAddress{Kind: "task", ID: "task4"}, Kind: "instruction", Body: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	w, _, err := s.QueueAgentWakeup("app", q.Recipient, "answer", "answer:"+q.ID, []string{q.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	var state string
	var turns int
	if err = s.queryRowScan(`SELECT state,model_turns FROM agent_wakeups WHERE id=?`, []any{w.ID}, &state, &turns); err != nil {
		t.Fatal(err)
	}
	if state != "scheduled" || turns != 0 {
		t.Fatalf("state=%s turns=%d", state, turns)
	}
	run, err := s.FindRun("task4")
	if err != nil || run.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("run=%#v err=%v", run, err)
	}
}

func TestAgentContinuationDoesNotReplaceBusyOwner(t *testing.T) {
	s, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	run := RunStatus{ProjectID: "app", RecordID: "architect", ItemID: "architect", LeaseState: string(LeaseStateRunning), LeaseOwner: "attempt-live", ActiveAttemptID: "attempt-live", LeaseGeneration: 7, LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	if err := s.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	changed, err := s.QueueAgentContinuation("app", "architect")
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	stored, err := s.FindRunScoped("app", "architect")
	if err != nil || stored.LeaseState != string(LeaseStateRunning) || stored.LeaseOwner != "attempt-live" || stored.LeaseGeneration != 7 {
		t.Fatalf("busy owner changed: %#v err=%v", stored, err)
	}
}

func TestAgentWakeupClaimIsExclusiveAndRecoverable(t *testing.T) {
	s, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, _, err := s.QueueAgentWakeup("app", AgentAddress{Kind: "task", ID: "worker"}, "message", "claim-test", []string{"m1"})
	if err != nil {
		t.Fatal(err)
	}
	claims := make(chan string, 2)
	go func() { claims <- s.ClaimAgentWakeup(w.ID) }()
	go func() { claims <- s.ClaimAgentWakeup(w.ID) }()
	a, b := <-claims, <-claims
	if (a == "") == (b == "") {
		t.Fatalf("claims=%q,%q; want exactly one winner", a, b)
	}
	winner := a
	if winner == "" {
		winner = b
	}
	if _, err = s.exec(`UPDATE agent_wakeups SET claimed_at=? WHERE id=?`, time.Now().UTC().Add(-2*time.Minute).Format(time.RFC3339Nano), w.ID); err != nil {
		t.Fatal(err)
	}
	recovered := s.ClaimAgentWakeup(w.ID)
	if recovered == "" || recovered == winner {
		t.Fatalf("recovered claim=%q original=%q", recovered, winner)
	}
	if err = s.SetAgentWakeupClaimState(w.ID, winner, "delivered"); err == nil {
		t.Fatal("stale claimant changed recovered wakeup")
	}
	if err = s.SetAgentWakeupClaimState(w.ID, recovered, "delivered"); err != nil {
		t.Fatal(err)
	}
}

func TestArchitectContinuationPausedRestartStaleAndSingleApplication(t *testing.T) {
	root := t.TempDir()
	s, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := s.RecordArchitectContinuation("app", "wave1", ArchitectReport{ObjectiveID: "obj", Outcome: "complete"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetArchitectProposal(id, ArchitectProposal{Kind: "next_wave", ContextRevision: "rev1", PlanPath: "next.yaml", AutoStart: true}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	apply := func(ArchitectProposal) (string, error) { calls++; return "W-0002", nil }
	if applied, err := s.ApplyArchitectContinuation(id, "rev1", ContinuationPolicy{}, apply); err != nil || applied || calls != 0 {
		t.Fatal("paused continuation ran")
	}
	emptyRevision, _, _ := s.RecordArchitectContinuation("app", "wave-empty-revision", ArchitectReport{ObjectiveID: "empty", Outcome: "complete"})
	_ = s.SetArchitectProposal(emptyRevision, ArchitectProposal{Kind: "next_wave", PlanPath: "next.yaml"})
	if _, err := s.ApplyArchitectContinuation(emptyRevision, "rev1", ContinuationPolicy{Enabled: true}, apply); err == nil {
		t.Fatal("mutating proposal without revision applied")
	}
	_ = s.Close()
	s, err = OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if applied, err := s.ApplyArchitectContinuation(id, "rev1", ContinuationPolicy{Enabled: true, MaxWaves: 2}, apply); err != nil || !applied || calls != 1 {
		t.Fatalf("applied=%v calls=%d err=%v", applied, calls, err)
	}
	if applied, err := s.ApplyArchitectContinuation(id, "rev1", ContinuationPolicy{Enabled: true}, apply); err != nil || applied || calls != 1 {
		t.Fatal("duplicate continuation applied")
	}
	stale, _, _ := s.RecordArchitectContinuation("app", "wave2", ArchitectReport{ObjectiveID: "obj", Outcome: "complete"})
	_ = s.SetArchitectProposal(stale, ArchitectProposal{Kind: "repair", ContextRevision: "old"})
	if _, err = s.ApplyArchitectContinuation(stale, "new", ContinuationPolicy{Enabled: true}, apply); err == nil {
		t.Fatal("stale proposal applied")
	}
}
