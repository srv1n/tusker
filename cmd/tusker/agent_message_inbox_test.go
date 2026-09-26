package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMessageHookPrint(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exe); resolveErr == nil {
		exe = resolved
	}
	if !filepath.IsAbs(exe) {
		t.Fatalf("test binary path is not absolute: %q", exe)
	}
	var out bytes.Buffer
	if err := runAgentMessageHookPrint(Args{"harness": "claude", "project": "app"}, &out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.HasPrefix(text, "Paste into .claude/settings.json") {
		t.Fatalf("missing paste target line: %q", text)
	}
	var snippet struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(text[strings.Index(text, "{"):]), &snippet); err != nil {
		t.Fatalf("snippet is not JSON: %v\n%s", err, text)
	}
	for _, event := range []string{"UserPromptSubmit", "Stop"} {
		entries := snippet.Hooks[event]
		if len(entries) != 1 || len(entries[0].Hooks) != 1 || entries[0].Hooks[0].Type != "command" {
			t.Fatalf("%s entries=%#v", event, entries)
		}
		command := entries[0].Hooks[0].Command
		if !strings.Contains(command, exe) || !strings.Contains(command, "message inbox --project 'app' --format hook") {
			t.Fatalf("%s command=%q", event, command)
		}
	}
	out.Reset()
	if err := runAgentMessageHookPrint(Args{"harness": "codex"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hooks.json") || !strings.Contains(out.String(), "unverified") || !strings.Contains(out.String(), "<project-id>") {
		t.Fatalf("codex output=%q", out.String())
	}
	out.Reset()
	if err := runAgentMessageHookPrint(Args{"harness": "codex", "project": "app", "json": "true"}, &out); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json output=%q err=%v", out.String(), err)
	}
	if payload["harness"] != "codex" || payload["verified"] != false || payload["binary"] != exe {
		t.Fatalf("json payload=%v", payload)
	}
	out.Reset()
	if err := runAgentMessageHookPrint(Args{"harness": "muse"}, &out); err == nil {
		t.Fatal("unsupported harness was admitted")
	}
	if err := agentMessageHookCmd(Args{"harness": "claude"}); err == nil {
		t.Fatal("missing print positional was admitted")
	}
}

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

func TestMessageInboxOversized(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", root)
	store, err := OpenRuntimeStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var ids []string
	for i, body := range []string{strings.Repeat("x", 30<<10), "second", "third"} {
		m, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", IdempotencyKey: string(rune('a' + i)), Sender: "task:a", Recipient: AgentAddress{Kind: "task", ID: "b"}, Kind: "notice", Body: body})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, m.ID)
	}
	var out bytes.Buffer
	if err := runAgentMessageInbox(Args{"project": "app", "for": "task:b", "format": "hook"}, strings.NewReader(`{"hook_event_name":"PostToolUse"}`), &out); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !strings.Contains(out.String(), id) {
			t.Fatalf("missing %s: %s", id, out.String())
		}
	}
	if !strings.Contains(out.String(), "full text: tusker message show --project") {
		t.Fatal(out.String())
	}
	for _, id := range ids {
		m, err := store.AgentMessage("app", id)
		if err != nil || m.TransportState != "delivered" {
			t.Fatalf("%s: %v %v", id, m, err)
		}
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
	// Soft in-turn delivery is a declared runner capability, not a harness-name switch.
	for runner, want := range map[RunnerName]bool{RunnerClaude: true, RunnerCodexExec: false, RunnerMuse: false, RunnerDevin: false} {
		if got := nativeResumeRunnerCapabilities(runner).SoftSay; got != want {
			t.Fatalf("%s SoftSay = %v, want %v", runner, got, want)
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
