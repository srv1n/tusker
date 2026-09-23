package main

import "testing"

func TestMailboxAskWaitDelivery(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	run := RunStatus{ProjectID: "app", RecordID: "task-1", ItemID: "task-1", WorkRevision: 1, LeaseGeneration: 1, LeaseState: string(LeaseStateReleased), Terminal: true, AttemptOutcome: string(AttemptOutcomeWaitingForHuman)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q", Sender: "task:task-1", Recipient: AgentAddress{Kind: "task", ID: "architect"}, OriginTaskID: "task-1", WorkRevision: 1, RouteGeneration: 1, Kind: "question", Body: "Choose", ReplyRequired: true, YieldSender: true})
	if err != nil {
		t.Fatal(err)
	}
	if open, err := d.openYieldQuestion(run); err != nil || !open {
		t.Fatalf("open question = %v, %v", open, err)
	}
	a, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "task:architect", Recipient: AgentAddress{Kind: "task", ID: "task-1"}, Kind: "answer", Body: "Use A", ReplyTo: q.ID})
	if err != nil {
		t.Fatal(err)
	}
	if open, err := d.openYieldQuestion(run); err != nil || open {
		t.Fatalf("answered question still open = %v, %v", open, err)
	}
	if err := d.processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	wake, _, err := store.QueueAgentWakeup("app", AgentAddress{Kind: "task", ID: "task-1"}, "answer", "message:"+a.ID, []string{a.ID})
	if err != nil || wake.State != "scheduled" {
		t.Fatalf("answer wake = %#v, %v", wake, err)
	}
	updated, err := store.FindRunScoped("app", "task-1")
	if err != nil || updated.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("run = %#v, %v", updated, err)
	}
	if err := d.processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	if _, duplicate, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a", Sender: "task:architect", Recipient: AgentAddress{Kind: "task", ID: "task-1"}, Kind: "answer", Body: "Use A", ReplyTo: q.ID}); err != nil || !duplicate {
		t.Fatalf("duplicate = %v, %v", duplicate, err)
	}
}
