package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// plainRunLog looks like ordinary agent output: build lines, prose, paths, no secrets.
func plainRunLog(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "ok  \tgithub.com/example/project/internal/pkg%d\t0.%03ds — Updated Handler for Request %d in cmd/server/main.go\n", i%40, i%1000, i)
	}
	return b.String()
}

// writeRunFixture writes a realistic event log and codex raw log for one attempt.
func writeRunFixture(tb testing.TB, dir string, records int) (RunStatus, []RunAttempt) {
	tb.Helper()
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	var events, raw strings.Builder
	for i := 0; i < records; i++ {
		ts := at.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		text := fmt.Sprintf("Reading the Handler for Request %d and updating the Tests in cmd/server", i)
		if i%25 == 0 {
			text += " with API_TOKEN=abc123secret"
		}
		line, _ := json.Marshal(map[string]any{"at": ts, "kind": "agent_message", "payload": map[string]any{"activity": true, "text": text}})
		events.Write(append(line, '\n'))
		item := map[string]any{"id": fmt.Sprintf("item_%d", i), "type": "command_execution", "command": "go test ./...", "status": "completed", "exit_code": 0, "aggregated_output": plainRunLog(20)}
		line, _ = json.Marshal(map[string]any{"timestamp": ts, "type": "item.completed", "item": item})
		raw.Write(append(line, '\n'))
	}
	eventPath, logPath := filepath.Join(dir, "events.jsonl"), filepath.Join(dir, "raw.jsonl")
	if err := os.WriteFile(eventPath, []byte(events.String()), 0o644); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(raw.String()), 0o644); err != nil {
		tb.Fatal(err)
	}
	return RunStatus{EventSinkPath: eventPath, RawLogPath: logPath}, []RunAttempt{{AttemptID: "a1"}}
}

// redactHookOutputAllRegex is the pre-gate redaction: every pattern on every text.
func redactHookOutputAllRegex(text string) string {
	text = hookPrivateKeyPattern.ReplaceAllString(text, `[REDACTED]`)
	text = hookUserInfoPattern.ReplaceAllString(text, `$1[REDACTED]@`)
	text = hookEnvSecretPattern.ReplaceAllString(text, `$1[REDACTED]`)
	text = hookJWTPattern.ReplaceAllString(text, `[REDACTED]`)
	text = hookSecretPattern.ReplaceAllString(text, `$1$2$3$4[REDACTED]`)
	text = hookQuerySecretPattern.ReplaceAllString(text, `$1[REDACTED]`)
	text = hookBearerPattern.ReplaceAllString(text, `Bearer [REDACTED]`)
	return hookKnownTokenPattern.ReplaceAllString(text, `[REDACTED]`)
}

func TestRedactHookOutputMatchesAllRegex(t *testing.T) {
	corpus := []string{
		`token: "quoted secret value"`,
		`password=space separated value`,
		`{"api_key":"json-secret","safe":"kept"}`,
		`Authorization: Bearer bearer-secret-value`,
		`https://example.test/?access_token=query-secret&safe=1`,
		`https://user:pass@example.test/repo.git`,
		`ghp_abcdefghijklmnopqrstuvwxyz123456 and SK-abcdefghijklmnopqrstuv`,
		`xoxb-1234567890-abc and github_pat_abcdefghijklmnopqrstuvwxyz`,
		`export GITHUB_TOKEN="abc def" AWS_SECRET_ACCESS_KEY=xyz db_password='p w'`,
		"API_\u212aEY=kelvin-secret",
		"\u017fecret: long-s-secret",
		"Capability=cap-secret; Authorization=basic",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----",
		"monkey business = fine, keyboard: ok",
		"plain — unicode prose ✓ with no secrets at all",
		"",
		plainRunLog(50),
	}
	for _, text := range corpus {
		if got, want := redactHookOutput(text), redactHookOutputAllRegex(text); got != want {
			t.Fatalf("redaction differs for %q:\n got %q\nwant %q", text, got, want)
		}
	}
}

func TestRunActivityRedactionCached(t *testing.T) {
	run, attempts := writeRunFixture(t, t.TempDir(), 50)
	first := serveRunEventsFromTails(loadRunStateTails(run, attempts))
	for _, event := range first {
		if strings.Contains(event.Text, "abc123secret") {
			t.Fatalf("event leaked secret: %q", event.Text)
		}
	}
	before := hookRedactRegexRuns.Load()
	second := serveRunEventsFromTails(loadRunStateTails(run, attempts))
	if runs := hookRedactRegexRuns.Load() - before; runs != 0 {
		t.Fatalf("unchanged logs ran redaction regexes %d times", runs)
	}
	if len(second) != len(first) || second[len(second)-1] != first[len(first)-1] {
		t.Fatalf("cached events differ: %d vs %d", len(second), len(first))
	}
	// A grown log must be re-read, not served from the cache.
	file, err := os.OpenFile(run.EventSinkPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(file, `{"at":"2026-10-19T13:00:00Z","kind":"agent_message","payload":{"activity":true,"text":"newest password=hunter2"}}`)
	file.Close()
	third := serveRunEventsFromTails(loadRunStateTails(run, attempts))
	if last := third[len(third)-1].Text; last != "newest password=[REDACTED]" {
		t.Fatalf("grown log not re-read, last event %q", last)
	}
}

func TestRedactHookOutputPlainLogFast(t *testing.T) {
	text := plainRunLog(200)
	if got := redactHookOutput(text); got != redactHookOutputAllRegex(text) {
		t.Fatal("plain log redaction differs from the all-regex path")
	}
	gated := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			redactHookOutput(text)
		}
	})
	full := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			redactHookOutputAllRegex(text)
		}
	})
	if gated.NsPerOp()*10 > full.NsPerOp() {
		t.Fatalf("gated redaction %dns/op is not 10x faster than all-regex %dns/op", gated.NsPerOp(), full.NsPerOp())
	}
}

func BenchmarkRedactHookOutputPlainLog(b *testing.B) {
	text := plainRunLog(200)
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		redactHookOutput(text)
	}
}

func BenchmarkRedactHookOutputPlainLogAllRegex(b *testing.B) {
	text := plainRunLog(200)
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		redactHookOutputAllRegex(text)
	}
}

// Cached: unchanged logs, as a polling UI sees them.
func BenchmarkServeRunEventsFromTails(b *testing.B) {
	run, attempts := writeRunFixture(b, b.TempDir(), 200)
	for b.Loop() {
		serveRunEventsFromTails(loadRunStateTails(run, attempts))
	}
}

// Uncached: a log that changed since the last poll.
func BenchmarkBuildRunEventsFromTails(b *testing.B) {
	run, attempts := writeRunFixture(b, b.TempDir(), 200)
	for b.Loop() {
		buildRunEventsFromTails(loadRunStateTails(run, attempts))
	}
}
