package main

import "testing"

func TestAgentMessageOperatorRecipient(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	question, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:T1", IdempotencyKey: "operator-q", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, Kind: "question", Body: "Need a decision", ReplyRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if question.Recipient != (AgentAddress{Kind: "operator", ID: "operator"}) {
		t.Fatal(question.Recipient)
	}
	if _, err := parseAgentAddress("operator:operator"); err == nil {
		t.Fatal("contact parser accepted operator")
	}
	if err := agentMessageCmd("message ask", Args{"project": "app", "sender": "task:T1", "recipient-kind": "operator", "recipient": "operator:operator", "key": "operator-cli", "body": "CLI question"}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListAgentMessages("app", "operator", "operator")
	if err != nil || len(listed) != 2 {
		t.Fatalf("CLI operator messages=%v err=%v", listed, err)
	}
	if _, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "operator:operator", IdempotencyKey: "operator-a", Recipient: AgentAddress{Kind: "task", ID: "T1"}, Kind: "answer", Body: "Proceed", ReplyTo: question.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "operator:operator", IdempotencyKey: "operator-a2", Recipient: AgentAddress{Kind: "task", ID: "T1"}, Kind: "answer", Body: "Conflicting", ReplyTo: question.ID}); err == nil {
		t.Fatal("second answer accepted")
	}
}

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

func TestAgentAnswerInheritsQuestionRoute(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	question, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:T1", IdempotencyKey: "q", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, OriginTaskID: "T1", WorkRevision: 4, RouteGeneration: 7, Kind: "question", Body: "Choose"})
	if err != nil {
		t.Fatal(err)
	}
	answer := AgentMessage{ProjectID: "app", Sender: "operator:operator", IdempotencyKey: "a", Recipient: AgentAddress{Kind: "task", ID: "T1"}, Kind: "answer", Body: "A", ReplyTo: question.ID}
	got, dup, err := store.PutAgentMessage(answer)
	if err != nil || dup || got.WorkRevision != 4 || got.RouteGeneration != 7 || got.OriginTaskID != "T1" {
		t.Fatalf("answer=%#v duplicate=%v err=%v", got, dup, err)
	}
	again, dup, err := store.PutAgentMessage(answer)
	if err != nil || !dup || again.ID != got.ID {
		t.Fatalf("duplicate answer=%#v duplicate=%v err=%v", again, dup, err)
	}
}
