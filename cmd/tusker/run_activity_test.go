package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunActivityACPThroughServe(t *testing.T) {
	_, req := setupACPRunnerRuntime(t, "activity")
	if _, err := runnerWrapperStartChild(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, req.Start.StatusPath)
	events := serveRunEvents(RunStatus{EventSinkPath: req.Start.EventSinkPath, RawLogPath: req.Start.RawLogPath}, nil)
	var messages []string
	for _, event := range events {
		if event.Activity {
			messages = append(messages, event.Text)
		}
	}
	want := []string{"Running fixture tests.\npassword=[REDACTED]\n", "cargo test selected_native_fixture_\ncompleted\n2 passed", "Tests finished."}
	if strings.Join(messages, "|") != strings.Join(want, "|") {
		t.Fatalf("activity = %#v", messages)
	}
	var tools []serveRunEvent
	for _, event := range events {
		if event.Kind == "tool_call" {
			tools = append(tools, event)
		}
	}
	if len(tools) != 1 || tools[0].ID != "acp:tool:test-1" || !strings.Contains(tools[0].Text, "2 passed") {
		t.Fatalf("tool lifecycle was not coalesced by ACP identity: %#v", tools)
	}
	raw, err := os.ReadFile(req.Start.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-value", "private reasoning", "test prompt"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("persisted %q", forbidden)
		}
	}
}

func TestACPThoughtChunksNotPersisted(t *testing.T) {
	_, req := setupACPRunnerRuntime(t, "activity")
	if _, err := runnerWrapperStartChild(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	waitForStatusFile(t, req.Start.StatusPath)
	raw, err := os.ReadFile(req.Start.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "private reasoning") || strings.Contains(string(raw), "agent_thought_chunk") {
		t.Fatal("thought chunk persisted")
	}
}

func TestRunActivityCLIMessagesAndTools(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		count           int
	}{
		{"codex message", `{"type":"item.completed","item":{"id":"a","type":"agent_message","text":"Running tests\nnow"}}`, "Running tests\nnow", 1},
		{"codex command", `{"type":"item.completed","item":{"id":"b","type":"command_execution","command":"cargo test","status":"completed","aggregated_output":"2 passed"}}`, "cargo test\ncompleted\n2 passed", 1},
		{"claude message", `{"type":"assistant","message":{"content":[{"type":"text","text":"Checking files"},{"type":"thinking","thinking":"hidden"}]}}`, "Checking files", 1},
		{"claude tool", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"cargo test","token":"private"}}]}}`, "Bash\ncargo test", 1},
		{"claude result", `{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"text","text":"2 passed"}]}]}}`, "2 passed", 1},
		{"prompt excluded", `{"type":"user","message":{"content":[{"type":"text","text":"private prompt"}]}}`, "", 0},
		{"reasoning excluded", `{"type":"item.completed","item":{"type":"reasoning","text":"private"}}`, "", 0},
		{"secrets redacted", `{"type":"item.completed","item":{"type":"agent_message","text":"token=private"}}`, "token=[REDACTED]", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var record map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &record); err != nil {
				t.Fatal(err)
			}
			events := cliRunActivity(record)
			if len(events) != tc.count {
				t.Fatalf("events = %#v", events)
			}
			if tc.count > 0 && (events[0].Text != tc.want || !events[0].Activity) {
				t.Fatalf("event = %#v", events[0])
			}
		})
	}
}

func TestCLIRunActivityMuse(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		count           int
	}{
		{"message", `{"payload_type":"assistant.message","payload":{"text":"Checking tests"}}`, "Checking tests", 1},
		{"output", `{"payload_type":"runtime.output","payload":{"output":"token=private"}}`, "token=[REDACTED]", 1},
		{"terminal", `{"payload_type":"run.terminal.failed","payload":{"reason":"rate limit exceeded"}}`, "rate limit exceeded", 1},
		{"unknown", `{"payload_type":"runtime.unknown","payload":{"opaque":"private"}}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var record map[string]any
			if err := json.Unmarshal([]byte(tc.raw), &record); err != nil {
				t.Fatal(err)
			}
			events := cliRunActivity(record)
			if len(events) != tc.count {
				t.Fatalf("events = %#v", events)
			}
			if tc.count > 0 && (events[0].Kind != "agent_message" || events[0].Text != tc.want) {
				t.Fatalf("event = %#v", events[0])
			}
		})
	}
}

func TestRunActivityClaudeToolCompletionUsesToolIdentity(t *testing.T) {
	start := cliRunActivity(map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "id": "toolu-1", "name": "Bash", "input": map[string]any{"command": "cargo test"}}},
	}})
	finish := cliRunActivity(map[string]any{"type": "user", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu-1", "content": "2 passed"}},
	}})
	events := appendRunActivity(nil, start[0])
	events = appendRunActivity(events, finish[0])
	if len(events) != 1 || events[0].ID != "cli:tool:toolu-1" || events[0].Kind != "tool_call" || events[0].Text != "Bash\ncargo test\n2 passed" {
		t.Fatalf("Claude tool lifecycle was not coalesced: %#v", events)
	}
}

func TestRunActivityReadsLatestAndCoalescesSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	// Larger than the tail window: neither the oldest records nor partial edge
	// records may substitute for the newest messages.
	data := strings.Repeat(`{"kind":"old","payload":{"text":"`+strings.Repeat("x", 1024)+`"}}`+"\n", 1100)
	data += `{"kind":"agent_message","payload":{"activity":true,"message_id":"acp:1","text":"Checking"}}` + "\n"
	data += `{"kind":"agent_message","payload":{"activity":true,"message_id":"acp:1","text":"Checking fixture tests"}}` + "\n"
	data += strings.Repeat("{\"kind\":\"heartbeat\"}\n", 70)
	data += `{"kind":"agent_message","payload":{"text":"partial`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	events := serveRunEvents(RunStatus{EventSinkPath: path}, nil)
	if len(events) != 51 || events[0].Text != "Checking fixture tests" {
		t.Fatalf("latest events = %#v", events)
	}
}

func TestRunActivityFailedCommandAndTextBound(t *testing.T) {
	events := cliRunActivity(map[string]any{"type": "item.completed", "item": map[string]any{"id": "cmd", "type": "command_execution", "command": "cargo test", "status": "completed", "exit_code": 1}})
	if len(events) != 1 || events[0].Level != "error" || !strings.Contains(events[0].Text, "exit 1") {
		t.Fatalf("failed command = %#v", events)
	}
	text := runActivityText(strings.Repeat("界", runActivityTextLimit+100))
	if len([]rune(text)) > runActivityTextLimit+20 || !strings.HasSuffix(text, "[truncated]") {
		t.Fatal("message not bounded")
	}
}

func TestRunActivityCompletionMovesToLatestPosition(t *testing.T) {
	events := []serveRunEvent{{ID: "tool", Text: "started"}, {ID: "message", Text: "working"}}
	events = appendRunActivity(events, serveRunEvent{ID: "tool", Text: "completed"})
	if len(events) != 2 || events[0].ID != "message" || events[1].Text != "completed" {
		t.Fatalf("latest order = %#v", events)
	}
}

func TestRunActivityToolMergeAcrossPersistedACPEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := `{"kind":"tool_call","payload":{"activity":true,"message_id":"acp:tool:1","text":"Run tests\ncargo test\nin_progress"}}` + "\n" +
		`{"kind":"tool_call","payload":{"activity":true,"message_id":"acp:tool:1","text":"Run tests\ncompleted\n2 passed"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	events := serveRunEvents(RunStatus{EventSinkPath: path}, nil)
	if len(events) != 1 || events[0].Text != "Run tests\ncargo test\ncompleted\n2 passed" {
		t.Fatalf("persisted tool lifecycle lost command or result: %#v", events)
	}
}

func TestRunActivityCLIUsesCurrentAttemptAndLatestOutput(t *testing.T) {
	dir := t.TempDir()
	old, current := filepath.Join(dir, "old.log"), filepath.Join(dir, "current.log")
	if err := os.WriteFile(old, []byte("{\"type\":\"result\",\"result\":\"wrong attempt\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data := "{\"type\":\"item.started\",\"item\":{\"id\":\"1\",\"type\":\"command_execution\",\"command\":\"cargo test\",\"status\":\"in_progress\"}}\n"
	data += "{\"type\":\"item.completed\",\"item\":{\"id\":\"1\",\"type\":\"command_execution\",\"command\":\"cargo test\",\"status\":\"completed\",\"aggregated_output\":\"2 passed\"}}\n"
	if err := os.WriteFile(current, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	attempts := []RunAttempt{{AttemptID: "old", RawLogPath: old}, {AttemptID: "current", RawLogPath: current}}
	events := serveRunEvents(RunStatus{ActiveAttemptID: "current"}, attempts)
	if len(events) != 1 || !strings.Contains(events[0].Text, "2 passed") {
		t.Fatalf("current activity = %#v", events)
	}
	if events := serveRunEvents(RunStatus{ActiveAttemptID: "missing"}, attempts); len(events) != 0 {
		t.Fatalf("fell back to old attempt: %#v", events)
	}
}

func TestRunActivityCLIErrors(t *testing.T) {
	for _, record := range []map[string]any{
		{"type": "turn.failed", "error": map[string]any{"message": "usage limit"}},
		{"type": "error", "message": "auth expired"},
		{"type": "item.completed", "item": map[string]any{"type": "error", "message": "sandbox denied"}},
		{"type": "result", "is_error": true, "subtype": "error_max_turns"},
		{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "is_error": true, "tool_use_id": "x"}}}},
	} {
		events := cliRunActivity(record)
		if len(events) != 1 || events[0].Level != "error" || events[0].Text == "" {
			t.Fatalf("missing error activity for %#v: %#v", record, events)
		}
	}
}

func TestRunActivityTailOversizedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := `{"type":"result","result":"earlier"}` + "\n" + `{"type":"item.completed","item":{"type":"agent_message","text":"` + strings.Repeat("x", 3<<20) + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	records := runActivityTail(path)
	if len(records) != 2 || records[0]["type"] != "result" || records[1]["kind"] != "truncated" {
		t.Fatalf("oversized tail = %#v", records)
	}
}

func TestRunActivityRedaction(t *testing.T) {
	text := "AWS_SECRET_ACCESS_KEY=abc123 https://u:p@host/x -----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY----- eyJhbGci.eyJzdWI.sig tokenizer"
	redacted := runActivityText(text)
	for _, secret := range []string{"abc123", "u:p", "BEGIN RSA PRIVATE KEY", "eyJhbGci"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("leaked %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "tokenizer") {
		t.Fatal("ordinary text redacted")
	}
}
