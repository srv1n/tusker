package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunActivityTailRecordLimitAndChronology(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var data strings.Builder
	for i := 0; i < 240; i++ {
		fmt.Fprintf(&data, "{\"seq\":%d}\n", i)
	}
	data.WriteString(`{"seq":240}`) // valid JSON, but the writer has not finished the record
	if err := os.WriteFile(path, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	events := runActivityTail(path)
	if len(events) != 200 {
		t.Fatalf("tail has %d records, want 200", len(events))
	}
	for i, event := range events {
		if event["seq"] != float64(i+40) {
			t.Fatalf("record %d = %#v, want seq %d", i, event, i+40)
		}
	}
}

func TestRunActivityTailSkipsNewestPartial(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       int
	}{
		{"empty", "", 0},
		{"only partial", `{"seq":1}`, 0},
		{"partial after complete", "{\"seq\":1}\n" + `{"seq":2}`, 1},
		{"partial spans chunks", "{\"seq\":1}\n" + `{"text":"` + strings.Repeat("x", 130<<10), 1},
		{"blank and CRLF", "\n{\"seq\":1}\r\n\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			events := runActivityTail(path)
			if len(events) != tc.want || tc.want > 0 && events[0]["seq"] != float64(1) {
				t.Fatalf("complete records = %#v", events)
			}
		})
	}
}

func TestRunActivityTailRecordSizeBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	prefix, suffix := `{"text":"`, `"}`
	text := strings.Repeat("x", (1<<20)-len(prefix)-len(suffix))
	data := prefix + text + suffix + "\n" + prefix + text + "x" + suffix + "\n" + "{\"seq\":2}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	events := runActivityTail(path)
	if len(events) != 3 || events[0]["text"] != text || events[1]["kind"] != "truncated" || events[2]["seq"] != float64(2) {
		t.Fatalf("record-size boundary: count=%d", len(events))
	}
}

func TestRunActivityTailOversizedBudgetEdge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := `{"text":"` + strings.Repeat("x", 9<<20) + "\"}\n{\"seq\":2}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	events := runActivityTail(path)
	if len(events) != 2 || events[0]["kind"] != "truncated" || events[1]["seq"] != float64(2) {
		t.Fatalf("budget-edge truncation = %#v", events)
	}
}

func TestRunActivityTailSharesByteBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	data := "{\"seq\":1}\n{\"seq\":2}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := int64(2 * len(data))
	first := runActivityTailWithBudget(path, &budget, 1)
	if len(first) != 1 || first[0]["seq"] != float64(2) || budget != int64(len(data)) {
		t.Fatalf("first tail = %#v, remaining budget = %d", first, budget)
	}
	second := runActivityTailWithBudget(path, &budget, 2)
	if len(second) != 2 || budget != 0 {
		t.Fatalf("second tail = %#v, remaining budget = %d", second, budget)
	}
	if got := runActivityTailWithBudget(path, &budget, 2); len(got) != 0 {
		t.Fatalf("exhausted budget returned %#v", got)
	}
	budget = int64(len(data) / 2)
	if got := runActivityTailWithBudget(path, &budget, 2); len(got) != 1 || got[0]["kind"] != "truncated" || budget != 0 {
		t.Fatalf("window without a complete left boundary = %#v, budget=%d", got, budget)
	}
}

func TestRunActivityTailMalformedRecordsCountTowardLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"seq\":1}\ninvalid\n{\"seq\":2}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := int64(8 << 20)
	events := runActivityTailWithBudget(path, &budget, 2)
	if len(events) != 1 || events[0]["seq"] != float64(2) {
		t.Fatalf("malformed records widened the tail = %#v", events)
	}
}

func TestRunActivityTailOversizedAllocationBound(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete=%t", complete), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			data := `{"text":"` + strings.Repeat("x", 9<<20) + `"}`
			if complete {
				data += "\n"
			}
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			events := runActivityTail(path)
			runtime.ReadMemStats(&after)
			if len(events) != 1 || events[0]["kind"] != "truncated" {
				t.Fatalf("oversized window = %#v", events)
			}
			// Covers all scratch growth and parsing; the old concatenation path
			// allocated hundreds of MiB for the same bounded 8MiB read window.
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
				t.Fatalf("tail allocated %d bytes; want <= 4MiB", allocated)
			}
		})
	}
}

func BenchmarkRunActivityTailOversized(b *testing.B) {
	path := filepath.Join(b.TempDir(), "events.jsonl")
	data := `{"text":"` + strings.Repeat("x", 9<<20) + "\"}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(8 << 20)
	b.ResetTimer()
	for b.Loop() {
		if events := runActivityTail(path); len(events) != 1 || events[0]["kind"] != "truncated" {
			b.Fatalf("oversized window = %#v", events)
		}
	}
}
