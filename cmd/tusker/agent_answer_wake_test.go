package main

import (
	"testing"
	"time"
)

func TestAgentAnswerWakeNotifiesProjectReconcile(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	previous := daemonControlOneWaySender
	defer func() { daemonControlOneWaySender = previous }()
	var notifications []daemonControlRequest
	daemonControlOneWaySender = func(_ string, req daemonControlRequest, _ time.Duration) error {
		notifications = append(notifications, req)
		return nil
	}
	question, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:worker", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, IdempotencyKey: "question", Kind: "question", Body: "Choose"})
	if err != nil {
		t.Fatal(err)
	}
	answer := AgentMessage{ProjectID: "app", Sender: "operator:operator", Recipient: AgentAddress{Kind: "task", ID: "worker"}, IdempotencyKey: "answer", Kind: "answer", Body: "A", ReplyTo: question.ID}
	if _, _, err := store.PutAgentMessageAsOperator(answer); err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 1 || notifications[0].Command != "reconcile_project" || notifications[0].ProjectID != "app" {
		t.Fatalf("answer notifications = %#v", notifications)
	}
	if !cliCommandMutatesVault("message reply") {
		t.Fatal("CLI reply does not use mutation notification path")
	}
}

func TestAnswerWakeContinuesPastAmbiguousRun(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.UpsertProject(RegisteredProject{ProjectID: "app", ProjectKey: "app", Name: "app", RepoRoot: t.TempDir(), VaultRoot: t.TempDir(), Enabled: true, Health: projectHealthHealthy}); err != nil {
		t.Fatal(err)
	}
	for _, run := range []RunStatus{
		{ProjectID: "app", RecordID: "one", ItemID: "duplicate", LeaseState: string(LeaseStateReleased)},
		{ProjectID: "app", RecordID: "two", ItemID: "duplicate", LeaseState: string(LeaseStateReleased)},
		{ProjectID: "app", RecordID: "worker", ItemID: "worker", Runner: "codex_exec", LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeWaitingForHuman)},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}
	for _, task := range []string{"duplicate", "worker"} {
		q, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "q-" + task, Sender: "task:" + task, Recipient: AgentAddress{Kind: "operator", ID: "operator"}, Kind: "question", Body: "Choose", YieldSender: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "a-" + task, Sender: "operator:operator", Recipient: AgentAddress{Kind: "task", ID: task}, Kind: "answer", Body: "A", ReplyTo: q.ID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&Daemon{store: store}).processAgentWakeups("app"); err != nil {
		t.Fatal(err)
	}
	run, err := store.FindRunScoped("app", "worker")
	if err != nil || run.LeaseState != string(LeaseStateRetryQueued) {
		t.Fatalf("answer did not queue yielded worker: %#v, %v", run, err)
	}
	var held, scheduled string
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE recipient_id='duplicate'`, nil, &held); err != nil {
		t.Fatal(err)
	}
	if err := store.queryRowScan(`SELECT state FROM agent_wakeups WHERE recipient_id='worker'`, nil, &scheduled); err != nil {
		t.Fatal(err)
	}
	if held != "held" || scheduled != "scheduled" {
		t.Fatalf("wakeup states = %q, %q", held, scheduled)
	}
}
