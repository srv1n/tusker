package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractFirstRefStreamingFirstMatchAndFinalLine(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
	}{
		{"first match", "invalid\n\n  {\"session_id\":\"first\"}\r\n{\"session_id\":\"second\"}\n", "first"},
		{"final line", "invalid\n{\"session_id\":\"final\"}", "final"},
		{"no match", "invalid\n{\"type\":\"other\"}\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "raw.log")
			if err := os.WriteFile(path, []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := extractFirstRef(path, extractSessionRefFromJSON); got != tc.want {
				t.Fatalf("first ref=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractRefsStreamingSegmentOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.log")
	refs := []string{"123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174001", "123e4567-e89b-12d3-a456-426614174002"}
	for i, suffix := range []string{"", ".prev", ".head"} {
		data := `{"session_id":"session` + suffix + `","message_id":"` + refs[i] + `"}`
		if err := os.WriteFile(path+suffix, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := extractSessionRef(path); got != "session.head" {
		t.Fatalf("session ref=%q, want head segment", got)
	}
	if got := extractMessageRef(path); got != refs[0] {
		t.Fatalf("message ref=%q, want current segment", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := extractMessageRef(path); got != refs[1] {
		t.Fatalf("fallback message ref=%q, want previous segment", got)
	}
	if err := os.Remove(path + ".prev"); err != nil {
		t.Fatal(err)
	}
	if got := extractMessageRef(path); got != refs[2] {
		t.Fatalf("fallback message ref=%q, want head segment", got)
	}
}

func TestExtractFirstRefStreamingStopsBeforeLargeSuffix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"session_id\":\"early\"}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Truncate(32 << 20); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := extractFirstRef(path, extractSessionRefFromJSON)
	runtime.ReadMemStats(&after)
	if got != "early" {
		t.Fatalf("first ref=%q, want early", got)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("early ref allocated %d bytes for a 32MiB log; want <=1MiB", allocated)
	}
}

func TestExtractFirstRefStreamingRecordLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.log")
	prefix, suffix := `{"session_id":"at-cap","padding":"`, `"}`
	data := prefix + strings.Repeat("x", int(completionAuthoritativeRawLogMaxBytes)-len(prefix)-len(suffix)) + suffix
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := extractFirstRef(path, extractSessionRefFromJSON); got != "at-cap" {
		t.Fatalf("full-cap final record ref=%q", got)
	}
	data += "x\n{\"session_id\":\"after-oversized-record\"}\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := extractFirstRef(path, extractSessionRefFromJSON); got != "" {
		t.Fatalf("oversized record did not stop bounded scan: ref=%q", got)
	}
}
