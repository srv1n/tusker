package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mcpTestStore(t *testing.T) *RuntimeStore {
	t.Helper()
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "T1", ItemID: "T1", ActiveAttemptID: "attempt-1", LeaseGeneration: 2, LeaseState: string(LeaseStateRunning), WorkRevision: 3, Runner: string(RunnerCodexExec)}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_PROJECT_ID", "app")
	t.Setenv("TUSKER_ITEM_ID", "T1")
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	t.Setenv("TUSKER_LEASE_GENERATION", "2")
	t.Setenv("TUSKER_WORK_REVISION", "3")
	return store
}

func mcpCall(name string, arguments any) mcpToolCall {
	raw, _ := json.Marshal(arguments)
	return mcpToolCall{Name: name, Arguments: raw}
}

func TestMCPServeProtocol(t *testing.T) {
	store := mcpTestStore(t)
	input := strings.NewReader("{bad\n" +
		"1\n" +
		`{"jsonrpc":"2.0","id":5}` + "\n" +
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n" +
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}` + "\n" +
		`{"jsonrpc":"2.0","id":4,"method":"missing"}` + "\n")
	var output bytes.Buffer
	if err := serveMCP(input, &output, store, 0); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"code":-32700`, `"code":-32600`, `"protocolVersion":"2025-03-26"`, `"name":"ask"`, `"name":"post_update"`, `"name":"check_messages"`, `"code":-32601`} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("missing %s in %s", fragment, output.String())
		}
	}
}

func TestMCPServeFraming(t *testing.T) {
	store := mcpTestStore(t)
	input := strings.NewReader(strings.Repeat("x", 2<<20) + "\n" + `{"jsonrpc":"2.0","id":null,"method":"ping"}` + "\n" + `{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	var output bytes.Buffer
	if err := serveMCP(input, &output, store, 0); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"code":-32700`, `"code":-32600`, `"id":1`, `"result":{}`} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s: %s", want, output.String())
		}
	}
}

func TestMCPServeCurrentRevisionMessages(t *testing.T) {
	store := mcpTestStore(t)
	for _, tc := range []struct {
		key      string
		revision int
	}{{"old", 2}, {"current", 3}} {
		if _, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:peer", IdempotencyKey: tc.key, Recipient: AgentAddress{Kind: "task", ID: "T1"}, WorkRevision: tc.revision, Kind: "notice", Body: tc.key}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := callMCPTool(context.Background(), store, mcpCall("check_messages", map[string]any{}), 0, func(time.Duration) {})
	if err != nil || !strings.Contains(got, "current") || strings.Contains(got, "old") {
		t.Fatalf("messages=%q err=%v", got, err)
	}
}

func TestMCPServeAskIdentityAndRetry(t *testing.T) {
	store := mcpTestStore(t)
	if _, err := store.PutAgentContact(AgentContact{ProjectID: "app", TaskID: "T1", Role: "architect", Address: AgentAddress{Kind: "task", ID: "architect"}}, 0); err != nil {
		t.Fatal(err)
	}
	first, err := callMCPTool(context.Background(), store, mcpCall("ask", map[string]any{"to": "architect", "question": "Which?"}), 0, func(time.Duration) {})
	if err != nil {
		t.Fatal(err)
	}
	second, err := callMCPTool(context.Background(), store, mcpCall("ask", map[string]any{"to": "architect", "question": "Which?", "wait_seconds": 1}), 0, func(time.Duration) {})
	if err != nil || first != second {
		t.Fatalf("retry first=%q second=%q err=%v", first, second, err)
	}
	messages, err := store.ListAgentMessages("app", "task", "architect")
	if err != nil || len(messages) != 1 {
		t.Fatalf("messages=%v err=%v", messages, err)
	}
	if messages[0].Sender != "task:T1" || messages[0].WorkRevision != 3 || messages[0].RouteGeneration != 2 {
		t.Fatal(messages[0])
	}
	if _, err := callMCPTool(context.Background(), store, mcpCall("ask", map[string]any{"to": "peer:missing", "question": "?"}), 0, func(time.Duration) {}); err == nil {
		t.Fatal("unresolved peer accepted")
	}
	if _, err := callMCPTool(context.Background(), store, mcpCall("ask", map[string]any{"to": "operator", "question": "Decision?"}), 0, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	operator, err := store.ListAgentMessages("app", "operator", "operator")
	if err != nil || len(operator) != 1 {
		t.Fatalf("operator=%v err=%v", operator, err)
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	if _, err := callMCPTool(context.Background(), store, mcpCall("check_messages", map[string]any{}), 0, func(time.Duration) {}); err == nil {
		t.Fatal("missing attempt accepted")
	}
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	t.Setenv("TUSKER_WORK_REVISION", "2")
	if _, err := callMCPTool(context.Background(), store, mcpCall("post_update", map[string]any{"text": "Stale revision"}), 0, func(time.Duration) {}); err == nil {
		t.Fatal("stale revision accepted")
	}
	t.Setenv("TUSKER_WORK_REVISION", "3")
	t.Setenv("TUSKER_LEASE_GENERATION", "1")
	if _, err := callMCPTool(context.Background(), store, mcpCall("ask", map[string]any{"to": "operator", "question": "Stale?"}), 0, func(time.Duration) {}); err == nil {
		t.Fatal("stale lease accepted")
	}
	t.Setenv("TUSKER_LEASE_GENERATION", "2")
	if err := store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "T2", ItemID: "T2", ActiveAttemptID: "attempt-1", LeaseGeneration: 2, LeaseState: string(LeaseStateRunning), WorkRevision: 3}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_ITEM_ID", "T2")
	if _, err := callMCPTool(context.Background(), store, mcpCall("ask", map[string]any{"to": "architect", "question": "Missing contact?"}), 0, func(time.Duration) {}); err == nil || !strings.Contains(err.Error(), "ask operator") {
		t.Fatalf("unresolved architect err=%v", err)
	}
	if messages, err := store.ListAgentMessagesForTask("app", "T2"); err != nil || len(messages) != 0 {
		t.Fatalf("unresolved architect wrote messages=%v err=%v", messages, err)
	}
}

func TestMCPServeAnswerAndConcurrentInbox(t *testing.T) {
	store := mcpTestStore(t)
	question, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:T1", IdempotencyKey: "q", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, Kind: "question", Body: "Question?", ReplyRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _, _ = store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "operator:operator", IdempotencyKey: "a", Recipient: AgentAddress{Kind: "task", ID: "T1"}, Kind: "answer", Body: "Answer", ReplyTo: question.ID})
	}()
	got, err := waitMCPAnswer(context.Background(), store, "app", "T1", question.ID, 3*time.Second, func(time.Duration) {})
	if err != nil || !strings.Contains(got, "Answer") {
		t.Fatalf("got=%q err=%v", got, err)
	}
	answer, err := store.agentMessageByKey("app", "operator:operator", "a")
	if err != nil || answer.ConsumedAt == "" {
		t.Fatalf("answer=%v err=%v", answer, err)
	}
	_, _, err = store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:peer", IdempotencyKey: "notice", Recipient: AgentAddress{Kind: "task", ID: "T1"}, WorkRevision: 3, Kind: "notice", Body: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, _ := callMCPTool(context.Background(), store, mcpCall("check_messages", map[string]any{}), 0, func(time.Duration) {})
			results <- value
		}()
	}
	wg.Wait()
	close(results)
	count := 0
	for result := range results {
		if strings.Contains(result, "Hello") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("notice delivered %d times", count)
	}
}

func TestMCPServeUpdateAndCancelledWait(t *testing.T) {
	store := mcpTestStore(t)
	sink := filepath.Join(t.TempDir(), "events.jsonl")
	t.Setenv("TUSKER_EVENT_SINK", sink)
	if _, err := callMCPTool(context.Background(), store, mcpCall("post_update", map[string]any{"text": "Build complete"}), 0, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(sink)
	if err != nil || !strings.Contains(string(raw), `"kind":"worker_update"`) || !strings.Contains(string(raw), "Build complete") {
		t.Fatalf("event=%s err=%v", raw, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	question, _, err := store.PutAgentMessage(AgentMessage{ProjectID: "app", Sender: "task:T1", IdempotencyKey: "cancel-q", Recipient: AgentAddress{Kind: "operator", ID: "operator"}, Kind: "question", Body: "Question?", ReplyRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	result, err := waitMCPAnswer(ctx, store, "app", "T1", question.ID, time.Minute, func(time.Duration) {})
	if err != nil || !strings.Contains(result, "pending") {
		t.Fatalf("cancel result=%q err=%v", result, err)
	}
	stored, err := store.AgentMessage("app", question.ID)
	if err != nil || stored.ConsumedAt != "" {
		t.Fatalf("question=%v err=%v", stored, err)
	}
}
