package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContextAuditSummarizesTranscriptNoise(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"sess-1","cwd":"/repo"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"rg -n \\\"needle\\\" .\"}","call_id":"call-1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-1","output":"Chunk ID: abc\nOriginal token count: 123\nOutput:\nmatch\n"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"python3 - <<'PY'\\nfrom pathlib import Path\\nPath('/Users/sarav/.codex/sessions/2026/05/09/session.jsonl').read_text()\\nPY\"}","call_id":"call-2"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call-2","output":"Chunk ID: def\nOriginal token count: 75\nOutput:\nsummary\n"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":900,"output_tokens":50,"total_tokens":1050}}}}`,
	}, "\n") + "\n"
	if err := writeText(path, content); err != nil {
		t.Fatal(err)
	}

	audit, err := auditContextJSONL(path, 5)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "sess-1", audit.SessionID, "session id")
	assertEqual(t, "/repo", audit.CWD, "cwd")
	assertEqual(t, 2, len(audit.TopOutputs), "top output count")
	categories := map[string]bool{}
	for _, output := range audit.TopOutputs {
		categories[output.Category] = true
	}
	if !categories["broad rg output"] || !categories["ad hoc jsonl script"] {
		t.Fatalf("expected noisy categories, got %#v", audit.TopOutputs)
	}
	recommendations := strings.Join(audit.Recommendations, "\n")
	if len(audit.Recommendations) == 0 || !strings.Contains(recommendations, "rg -l") || !strings.Contains(recommendations, "one-off Python/Node") {
		t.Fatalf("expected rg recommendation, got %#v", audit.Recommendations)
	}

	output := captureStdout(t, func() {
		if err := contextAuditCmd(Args{"file": path}); err != nil {
			t.Fatal(err)
		}
	})
	assertContainsIndexTest(t, output, "Top output categories")
	assertContainsIndexTest(t, output, "broad rg output")
	assertContainsIndexTest(t, output, "ad hoc jsonl script")
	assertContainsIndexTest(t, output, "input=1000 cached=900 output=50 total=1050")
	assertContainsIndexTest(t, output, "Recommendations")
}
