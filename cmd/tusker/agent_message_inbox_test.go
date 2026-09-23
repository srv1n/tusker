package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestMessageInboxHookDelivery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "one", Sender: "task:sender", Recipient: AgentAddress{Kind: "execution", ID: "exec-1"}, Kind: "question", Body: "Need a decision?"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"PostToolUse", "UserPromptSubmit", "Stop"} {
		var out bytes.Buffer
		input := strings.NewReader(`{"hook_event_name":"` + event + `"}`)
		if err := runAgentMessageInbox(Args{"project": "app", "for": "execution:exec-1", "format": "hook"}, input, &out); err != nil {
			t.Fatal(err)
		}
		if event == "PostToolUse" {
			var got map[string]map[string]string
			if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["hookSpecificOutput"]["hookEventName"] != event || !strings.Contains(got["hookSpecificOutput"]["additionalContext"], m.ID) {
				t.Fatalf("hook=%s output=%q err=%v", event, out.String(), err)
			}
		} else if out.Len() != 0 {
			t.Fatalf("already delivered hook=%s output=%q", event, out.String())
		}
	}
	got, err := store.AgentMessage("app", m.ID)
	if err != nil || got.TransportState != "delivered" || got.ConsumedAt != "" {
		t.Fatalf("delivered=%#v err=%v", got, err)
	}
}

func TestMessageInboxReplyBodyFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	parent, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "question", Sender: "task:asker", Recipient: AgentAddress{Kind: "task", ID: "answerer"}, Kind: "question", Body: "Question?"})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	file := root + "/answer.txt"
	if err := os.WriteFile(file, []byte("Answer from file."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := agentMessageCmd("message reply", Args{"project": "app", "reply-to": parent.ID, "sender": "task:answerer", "key": "answer", "body-file": file}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	parent, err = store.AgentMessage("app", parent.ID)
	if err != nil || parent.AnsweredAt == "" {
		t.Fatalf("answer receipt=%#v err=%v", parent, err)
	}
}

func TestMessageInboxStopAndReadOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "stop", Sender: "task:a", Recipient: AgentAddress{Kind: "task", ID: "b"}, Kind: "notice", Body: "Check this."})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runAgentMessageInbox(Args{"project": "app", "for": "task:b", "format": "json"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.AgentMessage("app", m.ID); got.TransportState == "delivered" {
		t.Fatal("read-only view marked delivered")
	}
	out.Reset()
	if err := runAgentMessageInbox(Args{"project": "app", "for": "task:b", "format": "hook"}, strings.NewReader(`{"hook_event_name":"Stop"}`), &out); err != nil {
		t.Fatal(err)
	}
	var shape map[string]string
	if err := json.Unmarshal(out.Bytes(), &shape); err != nil || shape["decision"] != "block" || !strings.Contains(shape["reason"], m.ID) {
		t.Fatalf("stop=%q err=%v", out.String(), err)
	}
}

func TestMessageInboxConcurrentClaimAndCapabilities(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "race", Sender: "task:a", Recipient: AgentAddress{Kind: "task", ID: "b"}, Kind: "notice", Body: "Once."})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := store.markInboxDelivered("app", m.ID)
			if err != nil {
				t.Errorf("claim: %v", err)
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	count := 0
	for ok := range wins {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("claims=%d, want 1", count)
	}
	for _, tc := range []struct {
		harness string
		want    bool
	}{{"claude", true}, {"codex", false}, {"muse", false}, {"devin", false}} {
		got, reason := workerSoftDelivery(tc.harness)
		if got != tc.want || reason == "" {
			t.Fatalf("%s = %v, %q", tc.harness, got, reason)
		}
	}
}

func TestMessageInboxSessionLookupCurrentContact(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "app", Source: "direct_claude", Provider: "claude", ProviderSessionID: "old-session"})
	if err != nil {
		t.Fatal(err)
	}
	newExecution, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "app", Source: "direct_claude", Provider: "claude", ProviderSessionID: "new-session"})
	if err != nil {
		t.Fatal(err)
	}
	contact := AgentContact{ProjectID: "app", TaskID: "task-1", Role: "architect", Address: AgentAddress{Kind: "execution", ID: old.ExecutionID}}
	if _, err := store.PutAgentContact(contact, 0); err != nil {
		t.Fatal(err)
	}
	contact.Address.ID = newExecution.ExecutionID
	if _, err := store.PutAgentContact(contact, 1); err != nil {
		t.Fatal(err)
	}
	oldMessage, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "old", Sender: "task:a", Recipient: AgentAddress{Kind: "execution", ID: old.ExecutionID}, Kind: "notice", Body: "old"})
	if err != nil {
		t.Fatal(err)
	}
	newMessage, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: "new", Sender: "task:a", Recipient: AgentAddress{Kind: "execution", ID: newExecution.ExecutionID}, Kind: "notice", Body: "new"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		session string
		want    string
	}{{"old-session", ""}, {"new-session", newMessage.ID}, {"unregistered", ""}} {
		var out bytes.Buffer
		input := strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"` + tc.session + `"}`)
		if err := runAgentMessageInbox(Args{"project": "app", "format": "hook"}, input, &out); err != nil {
			t.Fatal(err)
		}
		if tc.want == "" && out.Len() != 0 || tc.want != "" && !strings.Contains(out.String(), tc.want) {
			t.Fatalf("session=%s output=%q", tc.session, out.String())
		}
	}
	if got, err := store.AgentMessage("app", oldMessage.ID); err != nil || got.TransportState == "delivered" {
		t.Fatalf("old message=%#v err=%v", got, err)
	}
}
