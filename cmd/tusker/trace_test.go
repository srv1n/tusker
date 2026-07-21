package main

import (
	"context"
	"encoding/json"
	"os"
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

func TestTraceReplayMock(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-replay-model", "turn-1", "model", "", `{
			"expected_transitions":[{"type":"lease_change","subject":"APP-T-0001","from":"ready","to":"review"}],
			"state_transitions":[{"type":"lease_change","subject":"APP-T-0001","from":"ready","to":"review"}],
			"text":"model text is not the comparison target"
		}`),
		replayTraceRecord("trace-replay-tool", "tool-1", "tool", `{"command":"printf should-not-run"}`, `{
			"expected_transitions":[{"type":"file_touch_set","files":["cmd/tusker/trace.go","cmd/tusker/trace_test.go"]}],
			"state_transitions":[{"type":"file_touch_set","files":["cmd/tusker/trace_test.go","cmd/tusker/trace.go"]}]
		}`),
	})

	report, err := ReplayTrace(context.Background(), TraceReplayOptions{
		VaultPath: vault,
		TraceID:   "trace-replay-model",
		Mode:      traceReplayModeMock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("expected mock replay pass, got %#v", report)
	}
	assertEqual(t, 0, report.ModelCalls, "mock model calls")
	assertEqual(t, 0, report.LiveToolCalls, "mock live tool calls")
	assertEqual(t, 0, report.NetworkCalls, "mock network calls")
	assertEqual(t, 2, len(report.ActualTransitions), "mock transition count")

	output := captureStdout(t, func() {
		if err := traceReplayCmd(Args{"vault": vault, "id": "trace-replay-model", "mode": traceReplayModeMock}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "trace replay trace-replay-model mode=mock PASS") {
		t.Fatalf("trace replay CLI did not report PASS:\n%s", output)
	}
}

func TestTraceReplayLiveTools(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-live-model", "turn-1", "model", "", `{"state_transitions":[]}`),
		replayTraceRecord("trace-live-tool", "tool-1", "tool", `{"command":"printf live"}`, `{"command":"printf recorded","exit_code":0,"stdout":"recorded","status":"success"}`),
	})

	report, err := ReplayTrace(context.Background(), TraceReplayOptions{
		VaultPath: vault,
		TraceID:   "trace-live-model",
		Mode:      traceReplayModeLiveTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatalf("expected live-tools replay to report drift")
	}
	assertEqual(t, 1, report.LiveToolCalls, "live tool calls")
	if !containsString(report.DivergentBoundaries, "trace-live-tool") {
		t.Fatalf("expected divergent tool boundary, got %#v", report.DivergentBoundaries)
	}
	if !strings.Contains(report.FirstDivergence, "trace-live-tool") {
		t.Fatalf("expected first divergence to name live tool boundary, got %q", report.FirstDivergence)
	}
}

func TestTraceReplayVerifyRow(t *testing.T) {
	vault := pickupV7TestVault(t)
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Replay row", "risk": "low", "priority": "p2", "proof-mode": "inline", "proof-required": "focused_test", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-bad-row", "turn-1", "model", "", `{
			"expected_transitions":[{"type":"verify_row","subject":"APP-T-0001","covers":"A1","check":"go test ./cmd/tusker -run ReplayVerifyRow -count=1","result":"pass"}],
			"state_transitions":[{"type":"verify_row","subject":"APP-T-0001","covers":"A1","check":"go test ./cmd/tusker -run ReplayVerifyRow -count=1","result":"fail"}]
		}`),
	})
	if err := verifyV7AddCmd(Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "replay:trace-bad-row", "result": "pass", "note": "Pinned replay row."}); err != nil {
		t.Fatal(err)
	}

	report, err := loadV7ProofReport(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if report.Status == "satisfied" {
		t.Fatalf("divergent replay row must not satisfy proof: %#v", report)
	}
	if len(report.InlineRows) != 1 {
		t.Fatalf("expected one inline row, got %#v", report.InlineRows)
	}
	row := report.InlineRows[0]
	assertEqual(t, "fail", row.Result, "evaluated replay row result")
	if !strings.Contains(row.Notes, "first divergent transition") || !strings.Contains(row.Notes, "transition #1") {
		t.Fatalf("expected first divergent transition in row notes, got %q", row.Notes)
	}
}

func TestTraceReplayStrippedEnv(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := ensureDir(filepath.Join(repo, ".git", "objects")); err != nil {
		t.Fatal(err)
	}
	vault := filepath.Join(root, ".tusker")
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-stripped", "turn-1", "model", "", `{"state_transitions":[]}`),
	})

	report, err := ReplayTrace(context.Background(), TraceReplayOptions{
		VaultPath:       vault,
		TraceID:         "trace-stripped",
		Mode:            traceReplayModeMock,
		RepoRoot:        repo,
		KeepEnvironment: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(report.Environment.WorktreePath)) })
	if report.Environment.GitHistoryPresent {
		t.Fatalf("mock replay worktree must not expose git history: %#v", report.Environment)
	}
	if dirExists(filepath.Join(report.Environment.WorktreePath, ".git")) {
		t.Fatalf("mock replay worktree contains .git: %s", report.Environment.WorktreePath)
	}
	assertEqual(t, "off", report.Environment.NetworkAccess, "mock network access")
	assertEqual(t, false, report.Environment.NetworkEnabled, "mock network enabled")
	assertEqual(t, 0, report.NetworkCalls, "mock network calls")
}

func TestReplayForAdjudicationReproducesSteps(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-adj-model", "turn-1", "model", "", `{
			"expected_transitions":[{"type":"lease_change","subject":"APP-T-0001","from":"ready","to":"review"}],
			"state_transitions":[{"type":"lease_change","subject":"APP-T-0001","from":"ready","to":"review"}]
		}`),
		replayTraceRecord("trace-adj-tool", "tool-1", "tool", `{"command":"printf should-not-run"}`, `{
			"expected_transitions":[{"type":"file_touch_set","files":["cmd/tusker/trace.go"]}],
			"state_transitions":[{"type":"file_touch_set","files":["cmd/tusker/trace.go"]}]
		}`),
	})

	report, err := ReplayForAdjudication(context.Background(), TraceReplayOptions{
		VaultPath: vault,
		TraceID:   "trace-adj-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("expected adjudication replay to pass, got %#v", report)
	}
	if !report.Adjudicated {
		t.Fatalf("expected report to be flagged as adjudicated")
	}
	assertEqual(t, traceReplayModeAdjudicate, report.Mode, "adjudication mode")
	// Both recorded boundaries are reproduced for the reviewer.
	assertEqual(t, 2, len(report.Boundaries), "reproduced boundary count")
	assertEqual(t, 2, len(report.ActualTransitions), "reproduced transition count")
	// The recorded tool boundary was replayed from its recording, not executed.
	assertEqual(t, 0, report.LiveToolCalls, "adjudication live tool calls")
}

func TestReplayReportsZeroModelCalls(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-zero-model", "turn-1", "model", "", `{"state_transitions":[]}`),
	})

	report, err := ReplayForAdjudication(context.Background(), TraceReplayOptions{
		VaultPath: vault,
		TraceID:   "trace-zero-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 0, report.ModelCalls, "adjudication model calls")
	assertEqual(t, 0, report.NetworkCalls, "adjudication network calls")

	output := captureStdout(t, func() {
		if err := traceReplayCmd(Args{"vault": vault, "id": "trace-zero-model", "adjudicate": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "adjudication: replayed from recording, 0 new model calls") {
		t.Fatalf("adjudication replay did not plainly report zero model calls:\n%s", output)
	}
}

func TestReplayFlagsIncompleteTrace(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	// A model boundary with neither a recorded output nor a recorded error: the
	// trail is missing this step, so an honest replay cannot reproduce it.
	writeReplayTraceFixture(t, vault, []TraceRecord{
		replayTraceRecord("trace-incomplete", "turn-1", "model", "", ""),
	})

	_, err := ReplayForAdjudication(context.Background(), TraceReplayOptions{
		VaultPath: vault,
		TraceID:   "trace-incomplete",
	})
	if err == nil {
		t.Fatalf("expected incomplete recording to fail adjudication")
	}
	tuskerErr, ok := err.(*TuskerError)
	if !ok {
		t.Fatalf("expected a TuskerError, got %T: %v", err, err)
	}
	if tuskerErr.Code != errorTraceReplayIncomplete {
		t.Fatalf("expected %s, got %s", errorTraceReplayIncomplete, tuskerErr.Code)
	}
	if !strings.Contains(tuskerErr.Message, "incomplete recording") {
		t.Fatalf("expected a clear incomplete-recording message, got %q", tuskerErr.Message)
	}
}

func replayTraceRecord(traceID, nodeID, nodeType, input, output string) TraceRecord {
	record := TraceRecord{
		SchemaVersion: traceRecordSchemaVersion,
		TraceID:       traceID,
		WorkItemID:    "APP-T-0001",
		NodeID:        nodeID,
		NodeType:      nodeType,
		CodeSHA:       "sha-fixture",
		CreatedAt:     "2026-07-07T00:00:00Z",
	}
	if strings.TrimSpace(input) != "" {
		record.Input = json.RawMessage(input)
	}
	if strings.TrimSpace(output) != "" {
		record.Output = json.RawMessage(output)
	}
	return record
}

func writeReplayTraceFixture(t *testing.T, vault string, records []TraceRecord) {
	t.Helper()
	if err := appendTraceRecords(traceAttemptPath(vault, "APP-T-0001", "attempt-replay"), records); err != nil {
		t.Fatal(err)
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
