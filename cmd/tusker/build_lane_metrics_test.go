package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeBuildAdmissionLog(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(root, "build-lane", "builds.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildAdmissionStats(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	var lines strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&lines, `{"at":%q,"project_id":"p","cmd":"cargo test %d","cold":%t,"wait_ms":%d,"compiled_deps":%d}`+"\n", now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339Nano), i%4, i < 2, i*1000, i%2)
	}
	for _, at := range []time.Time{now.Add(-31 * time.Minute), now.Add(time.Minute)} {
		fmt.Fprintf(&lines, `{"at":%q,"project_id":"p","cmd":"ignored","wait_ms":999999,"compiled_deps":1}`+"\n", at.Format(time.RFC3339Nano))
	}
	lines.WriteString("malformed\n")
	writeBuildAdmissionLog(t, root, lines.String())
	got := readBuildLaneStats(root, now)["p"]
	if got.Builds != 10 || got.WarmBuilds != 8 || got.MedianWaitMS != 4500 || got.DepRebuildRate != 0.5 {
		t.Fatalf("stats: %+v", got)
	}
	if strings.Join(got.Commands, ",") != "cargo test 1,cargo test 3" {
		t.Fatalf("commands: %v", got.Commands)
	}
	if other := readBuildLaneStats(root, now.Add(32*time.Minute)); len(other) != 0 {
		t.Fatalf("stale entries: %v", other)
	}
}

func TestBuildAdmissionCache(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	first := fmt.Sprintf(`{"at":%q,"project_id":"a","wait_ms":100}`, now.Format(time.RFC3339Nano)) + "\n"
	path := writeBuildAdmissionLog(t, root, first)
	if got := readBuildLaneStats(root, now)["a"].MedianWaitMS; got != 100 {
		t.Fatal(got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	second := strings.Replace(first, "100", "900", 1)
	writeBuildAdmissionLog(t, root, second)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := readBuildLaneStats(root, now)["a"].MedianWaitMS; got != 100 {
		t.Fatalf("unchanged metadata reread log: %d", got)
	}
	writeBuildAdmissionLog(t, root, second+second)
	if got := readBuildLaneStats(root, now)["a"].MedianWaitMS; got != 900 {
		t.Fatalf("changed metadata was not read: %d", got)
	}
}

func TestBuildAdmissionReasons(t *testing.T) {
	runs := []RunStatus{{ProjectID: "p", RecordID: "one", Lane: runLaneExecute, LeaseState: string(LeaseStateRunning)}}
	if !projectHasActiveExecute(runs, "p", "two") || projectHasActiveExecute(runs, "p", "one") || projectHasActiveExecute(runs, "other", "two") {
		t.Fatal("active execute detection")
	}
	runs[0].Lane = runLaneReview
	if projectHasActiveExecute(runs, "p", "two") {
		t.Fatal("review run counted as execute")
	}
	busy := buildLaneStats{Builds: 3, MedianWaitMS: 121000}
	if got := buildAdmissionReason(busy, runLaneExecute, false); got != "waiting: build queue busy (median wait 121s)" {
		t.Fatal(got)
	}
	if got := buildAdmissionReason(busy, runLaneReview, true); got != "" {
		t.Fatal(got)
	}
	rebuild := buildLaneStats{Builds: 5, WarmBuilds: 5, DepRebuildRate: 0.4, Commands: []string{"cargo test"}}
	if got := buildAdmissionReason(rebuild, runLaneExecute, true); !strings.Contains(got, "serial: dependency cache keeps rebuilding (commands: cargo test)") || !strings.Contains(got, "mismatched build flags") {
		t.Fatal(got)
	}
	if got := buildAdmissionReason(rebuild, runLaneExecute, false); got != "" {
		t.Fatal(got)
	}
	if got := buildAdmissionReason(rebuild, runLaneReview, true); got != "" {
		t.Fatal(got)
	}
	if got := buildAdmissionReason(buildLaneStats{}, runLaneExecute, true); got != "" {
		t.Fatal(got)
	}
	if stats := readBuildLaneStats(t.TempDir(), time.Now()); len(stats) != 0 {
		t.Fatal(stats)
	}
	root := t.TempDir()
	writeBuildAdmissionLog(t, root, "")
	if stats := readBuildLaneStats(root, time.Now()); len(stats) != 0 {
		t.Fatal(stats)
	}
}
