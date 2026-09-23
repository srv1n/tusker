package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestArchitectSessionInbox(t *testing.T) {
	_, store, project, stateRoot := externalRoutingFixture(t)
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	input := externalRoutingInput(project, "W-0001", "wave")
	input.Harness, input.Source, input.Provider = "claude-code", "direct_claude", "anthropic"
	contact, record, err := store.RegisterExternalAgentContact(input)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := store.ResolveAgentContactBinding(project, "TSK-T-0002", "architect", "")
	if err != nil || binding.State != "inbox" || binding.InheritedFrom != "W-0001" || !binding.Capabilities.RetrieveResponse || binding.Capabilities.MessageWhileRunning {
		t.Fatalf("inherited inbox binding=%#v err=%v", binding, err)
	}
	question, _, err := store.PutAgentMessage(AgentMessage{IdempotencyKey: "architect-question", ProjectID: project, Sender: "task:TSK-T-0002", Recipient: contact.Address, RecipientGeneration: 1, Kind: "question", Body: "Choose the schema", ReplyRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	report, _, err := store.PutAgentMessage(AgentMessage{IdempotencyKey: "architect-report", ProjectID: project, Sender: "execution:tusker-wave-W-0001", Recipient: contact.Address, RecipientGeneration: 1, Kind: "wave_result", Body: "Wave result", ReplyRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedGeneration = 1
	input.ConversationID = "conv-2"
	replacement, next, err := store.RegisterExternalAgentContact(input)
	if err != nil || replacement.Generation != 2 || next.ExecutionID == record.ExecutionID {
		t.Fatalf("replacement=%#v next=%#v err=%v", replacement, next, err)
	}
	read := func(session string) string {
		t.Helper()
		var output bytes.Buffer
		input := strings.NewReader(fmt.Sprintf(`{"hook_event_name":"UserPromptSubmit","session_id":%q}`, session))
		if err := runAgentMessageInbox(Args{"project": project, "format": "hook"}, input, &output); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	if old := read("conv-1"); old != "" {
		t.Fatalf("old session received replaced contact messages: %s", old)
	}
	output := read("conv-2")
	for _, want := range []string{question.ID, report.ID, "question", "wave_result", "Choose the schema", "Wave result"} {
		if !strings.Contains(output, want) {
			t.Fatalf("inbox omitted %q: %s", want, output)
		}
	}
	if again := read("conv-2"); again != "" {
		t.Fatalf("hook delivered messages twice: %s", again)
	}
	answer, _, err := store.PutAgentMessage(AgentMessage{IdempotencyKey: "architect-answer", ProjectID: project, Sender: "execution:" + next.ExecutionID, Recipient: AgentAddress{Kind: "task", ID: "TSK-T-0002"}, Kind: "answer", Body: "Use schema B", ReplyTo: question.ID})
	if err != nil || answer.Recipient.ID != "TSK-T-0002" {
		t.Fatalf("architect reply=%#v err=%v", answer, err)
	}
	updated, err := store.AgentMessage(project, question.ID)
	if err != nil || updated.AnsweredAt == "" {
		t.Fatalf("worker question did not receive answer: %#v err=%v", updated, err)
	}
}
