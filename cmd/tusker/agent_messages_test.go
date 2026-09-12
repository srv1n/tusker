package main

import "testing"

func TestAgentMessagesDurableCorrelatedDeduplicated(t *testing.T) {
	root := t.TempDir()
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	q := AgentMessage{IdempotencyKey: "ask-1", ProjectID: "app", Sender: "task:four", Recipient: AgentAddress{Kind: "task", ID: "task:architect"}, Kind: "question", Body: "Which contract wins?", ReplyRequired: true, YieldSender: true}
	got, dup, err := store.PutAgentMessage(q)
	if err != nil || dup {
		t.Fatalf("put=%#v dup=%v err=%v", got, dup, err)
	}
	again, dup, err := store.PutAgentMessage(q)
	if err != nil || !dup || again.ID != got.ID {
		t.Fatalf("duplicate=%#v dup=%v err=%v", again, dup, err)
	}
	answer := AgentMessage{IdempotencyKey: "reply-1", ProjectID: "app", Sender: "task:architect", Recipient: AgentAddress{Kind: "task", ID: "task:four"}, Kind: "answer", Body: "Use the recorded revision.", ReplyTo: got.ID}
	if _, _, err = store.PutAgentMessage(answer); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	store, err = OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	loaded, err := store.AgentMessage("app", got.ID)
	if err != nil || loaded.AnsweredAt == "" {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	listed, err := store.ListAgentMessages("app", "task", "task:architect")
	if err != nil || len(listed) != 1 {
		t.Fatalf("listed=%d err=%v", len(listed), err)
	}
}

func TestAgentMessagesRejectForgedReplyAndOversize(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	q, _, err := store.PutAgentMessage(AgentMessage{IdempotencyKey: "q", ProjectID: "app", Sender: "a", Recipient: AgentAddress{Kind: "task", ID: "b"}, Kind: "question", Body: "?"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.PutAgentMessage(AgentMessage{IdempotencyKey: "r", ProjectID: "app", Sender: "b", Recipient: AgentAddress{Kind: "task", ID: "not-a"}, Kind: "answer", Body: "x", ReplyTo: q.ID})
	if err == nil {
		t.Fatal("forged reply accepted")
	}
}
