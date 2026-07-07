package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTraceSchemaRoundTripExplicitNulls(t *testing.T) {
	record := TraceRecord{
		SchemaVersion: traceRecordSchemaVersion,
		TraceID:       "trace-1",
		WorkItemID:    "APP-T-0001",
		NodeID:        "turn-1",
		NodeType:      "model",
		CodeSHA:       "sha-fixture",
		CreatedAt:     "2026-07-07T00:00:00Z",
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"input",
		"output",
		"error",
		"model_provider",
		"model_name",
		"model_params",
		"prompt_version",
		"skill_versions",
		"tool_schema_version",
		"permission_scope",
		"retrieved_chunk_ids",
	} {
		value, ok := wire[key]
		if !ok {
			t.Fatalf("expected %s to be present", key)
		}
		if value != nil {
			t.Fatalf("expected %s to serialize as null, got %#v", key, value)
		}
	}
	var decoded TraceRecord
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqualTraceTest(t, raw, roundTrip)

	schema := TraceRecordJSONSchema()
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema required list has unexpected type: %#v", schema["required"])
	}
	for _, key := range []string{
		"trace_id",
		"work_item_id",
		"node_id",
		"node_type",
		"input",
		"output",
		"error",
		"model_provider",
		"model_name",
		"model_params",
		"prompt_version",
		"skill_versions",
		"code_sha",
		"tool_schema_version",
		"permission_scope",
		"retrieved_chunk_ids",
		"created_at",
	} {
		if !containsString(required, key) {
			t.Fatalf("schema required list missing %s", key)
		}
	}
}

func TestTraceRecorderFixtureEventStream(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	eventPath := filepath.Join(root, "attempt.events.jsonl")
	if err := writeText(eventPath, strings.Join([]string{
		`{"seq":1,"at":"2026-07-07T00:00:01Z","attempt_id":"attempt-1","runner":"codex","kind":"turn_started","payload":{"turn_id":"turn-1"}}`,
		`{"seq":2,"at":"2026-07-07T00:00:02Z","attempt_id":"attempt-1","runner":"codex","kind":"turn_completed","payload":{"turn_id":"turn-1","status":"completed","input_tokens":11,"output_tokens":7}}`,
		`{"seq":3,"at":"2026-07-07T00:00:03Z","attempt_id":"attempt-1","runner":"codex","kind":"extension_tool_result","payload":{"method":"item/tool/call","tool":"tusker.show_current"}}`,
		`{"seq":4,"at":"2026-07-07T00:00:04Z","attempt_id":"attempt-1","runner":"codex","kind":"extension_tool_denied","payload":{"method":"item/tool/call","tool":"missing.tool","reason":"unsupported tool"}}`,
		`{"seq":5,"at":"2026-07-07T00:00:05Z","attempt_id":"attempt-1","runner":"codex","kind":"codex_approval_decision","payload":{"method":"item/commandExecution/requestApproval","request_type":"command","decision":"accept","subject":"go test ./cmd/tusker","approval_policy":"never","thread_sandbox":"workspace-write","turn_sandbox_policy":"danger-full-access"}}`,
		`{"seq":6,"at":"2026-07-07T00:00:06Z","attempt_id":"attempt-1","runner":"codex","kind":"supervisor_decision","payload":{"kind":"continue_attempt"}}`,
	}, "\n")+"\n"); err != nil {
		t.Fatal(err)
	}

	appended, err := RecordAttemptTraces(TraceRecorderOptions{
		VaultPath:     vault,
		WorkItemID:    "APP-T-0001",
		AttemptID:     "attempt-1",
		EventSinkPath: eventPath,
		CodeSHA:       "sha-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 4, appended, "appended trace count")
	appended, err = RecordAttemptTraces(TraceRecorderOptions{
		VaultPath:     vault,
		WorkItemID:    "APP-T-0001",
		AttemptID:     "attempt-1",
		EventSinkPath: eventPath,
		CodeSHA:       "sha-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, appended, "idempotent appended trace count")

	records, err := readTraceRecords(traceAttemptPath(vault, "APP-T-0001", "attempt-1"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 4, len(records), "record count")
	assertEqual(t, "model", records[0].NodeType, "first node type")
	assertEqual(t, "tool", records[1].NodeType, "second node type")
	assertEqual(t, "tool", records[2].NodeType, "third node type")
	assertEqual(t, "tool", records[3].NodeType, "fourth node type")
	for i, record := range records {
		if record.WorkItemID != "APP-T-0001" {
			t.Fatalf("record %d work_item_id: %q", i, record.WorkItemID)
		}
		if record.CodeSHA != "sha-fixture" {
			t.Fatalf("record %d code_sha: %q", i, record.CodeSHA)
		}
		if record.CreatedAt == "" {
			t.Fatalf("record %d missing created_at", i)
		}
	}
	if string(records[2].Error) == "" || string(records[2].Error) == "null" {
		t.Fatalf("expected denied tool record to carry non-null error, got %s", records[2].Error)
	}
	if records[3].PermissionScope == nil || !strings.Contains(*records[3].PermissionScope, "approval_policy=never") {
		t.Fatalf("expected approval trace to synthesize permission_scope, got %#v", records[3].PermissionScope)
	}
}

func TestTraceCLIListShowEmptyAttempt(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := writeText(traceAttemptPath(vault, "APP-T-0001", "attempt-empty"), ""); err != nil {
		t.Fatal(err)
	}
	record := TraceRecord{
		SchemaVersion: traceRecordSchemaVersion,
		TraceID:       "trace-cli",
		WorkItemID:    "APP-T-0001",
		NodeID:        "turn-1",
		NodeType:      "model",
		Output:        json.RawMessage(`{"status":"completed"}`),
		CodeSHA:       "sha-fixture",
		CreatedAt:     "2026-07-07T00:00:00Z",
	}
	if err := appendTraceRecords(traceAttemptPath(vault, "APP-T-0001", "attempt-1"), []TraceRecord{record}); err != nil {
		t.Fatal(err)
	}

	listOutput := captureStdout(t, func() {
		if err := traceListCmd(Args{"vault": vault, "id": "APP-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		"attempt-1 count=1 types=model",
		"attempt-empty count=0 types=-",
	} {
		if !strings.Contains(listOutput, expected) {
			t.Fatalf("trace list missing %q:\n%s", expected, listOutput)
		}
	}

	showOutput := captureStdout(t, func() {
		if err := traceShowCmd(Args{"vault": vault, "id": "trace-cli"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, expected := range []string{
		`"trace_id": "trace-cli"`,
		`"input": null`,
		`"output": {`,
	} {
		if !strings.Contains(showOutput, expected) {
			t.Fatalf("trace show missing %q:\n%s", expected, showOutput)
		}
	}

	emptyOutput := captureStdout(t, func() {
		if err := traceListCmd(Args{"vault": vault, "id": "NO-T-0001"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(emptyOutput, "NO-T-0001 traces: no attempts") {
		t.Fatalf("empty trace list should not error, got:\n%s", emptyOutput)
	}
}

func assertJSONEqualTraceTest(t *testing.T, left, right []byte) {
	t.Helper()
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(leftValue, rightValue) {
		t.Fatalf("JSON mismatch:\nleft=%s\nright=%s", left, right)
	}
}
