package main

import (
	"context"
	"testing"
)

func TestPendingWakeupPollsItsProjectWithoutWaitingForSchedule(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store, stateRoot: stateRoot}
	if polled, err := d.pollProjectsWithPendingWakeups(context.Background()); err != nil || polled {
		t.Fatalf("idle store polled=%v err=%v", polled, err)
	}
	run := RunStatus{ProjectID: "app", RecordID: "task-1", ItemID: "task-1", WorkRevision: 1, LeaseGeneration: 1, LeaseState: string(LeaseStateReleased), Terminal: true, AttemptOutcome: string(AttemptOutcomeWaitingForHuman)}
	mustUpsertRun(t, store, run)
	q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q", Sender: "task:task-1", Recipient: AgentAddress{Kind: "task", ID: "architect"}, OriginTaskID: "task-1", WorkRevision: 1, RouteGeneration: 1, Kind: "question", Body: "Choose", ReplyRequired: true, YieldSender: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "task:architect", Recipient: AgentAddress{Kind: "task", ID: "task-1"}, Kind: "answer", Body: "Use A", ReplyTo: q.ID}); err != nil {
		t.Fatal(err)
	}
	if pending, err := store.PendingAgentWakeupProjects(); err != nil || len(pending) != 1 || pending[0] != "app" {
		t.Fatalf("pending wakeup projects = %v, %v", pending, err)
	}
	polled, err := d.pollProjectsWithPendingWakeups(context.Background())
	if err != nil || !polled {
		t.Fatalf("pending wakeup polled=%v err=%v", polled, err)
	}
	if pending, _ := store.PendingAgentWakeupProjects(); len(pending) != 0 {
		t.Fatalf("wakeup still pending after poll: %v", pending)
	}
	woken, err := store.FindRunScoped("app", "task-1")
	if err != nil || woken == nil || woken.LeaseState == string(LeaseStateReleased) {
		t.Fatalf("answered worker was not woken: %#v %v", woken, err)
	}
}
