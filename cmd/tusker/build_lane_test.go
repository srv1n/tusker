package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func buildLaneFixture(t *testing.T, script string) string {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "tools")
	if err := os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "cargo"), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TUSKER_BUILD_LANE_STATE_ROOT", filepath.Join(root, "state"))
	t.Setenv("TUSKER_CONFIG", filepath.Join(root, "missing.yaml"))
	return filepath.Join(root, "state")
}

func buildLaneRecords(t *testing.T, root string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "build-lane", "builds.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
	}
	return records
}

func TestBuildLaneCompilingAndDigest(t *testing.T) {
	w := &buildLaneStderr{}
	_, _ = w.Write([]byte("   Compiling own v0.1.0 (/tmp/own)\nCompiling dep v1.0.0\nCom"))
	_, _ = w.Write([]byte("piling gitdep v2.0.0 (https://example.com/repo)\n"))
	if w.local != 1 || w.deps != 2 {
		t.Fatalf("local=%d deps=%d", w.local, w.deps)
	}
	t.Setenv("RUSTFLAGS", "-C opt-level=1")
	cmd1, a := buildLaneSummary("cargo", []string{"build", "--release", "src/a.rs"})
	cmd2, b := buildLaneSummary("cargo", []string{"build", "src/b.rs", "--release"})
	if cmd1 != cmd2 || a != b {
		t.Fatalf("file path changed summary: %q %q", cmd1, cmd2)
	}
	t.Setenv("RUSTFLAGS", "-C opt-level=2")
	_, c := buildLaneSummary("cargo", []string{"build", "--release", "src/b.rs"})
	if c == a {
		t.Fatal("RUSTFLAGS did not change digest")
	}
}

func TestBuildLaneNestedSkipsLock(t *testing.T) {
	root := buildLaneFixture(t, "exit 0")
	lock, _, err := buildLaneSlot(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	t.Setenv("TUSKER_BUILD_LANE_HELD", "1")
	done := make(chan int, 1)
	go func() { done <- runBuildLane("cargo", []string{"build"}) }()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("nested build waited on lock")
	}
	if _, err := os.Stat(filepath.Join(root, "build-lane", "builds.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("nested build logged: %v", err)
	}
}

func TestBuildLaneSerializes(t *testing.T) {
	root := buildLaneFixture(t, "sleep 0.2")
	t.Setenv("TUSKER_BUILD_LANE_HELD", "")
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			if code := runBuildLane("cargo", []string{"build"}); code != 0 {
				t.Errorf("exit %d", code)
			}
		}()
	}
	wg.Wait()
	records := buildLaneRecords(t, root)
	if len(records) != 2 || records[0]["wait_ms"].(float64) <= 0 && records[1]["wait_ms"].(float64) <= 0 {
		t.Fatalf("builds did not serialize: %#v", records)
	}
}

func TestBuildLaneSharedCheckoutGuard(t *testing.T) {
	buildLaneFixture(t, "exit 0")
	t.Setenv("TUSKER_SHARED_CHECKOUT", "1")
	if code := runBuildLane("cargo", []string{"fmt"}); code != 2 {
		t.Fatalf("fmt exit %d", code)
	}
	if code := runBuildLane("cargo", []string{"fmt", "--", "src/main.rs"}); code != 0 {
		t.Fatalf("file fmt exit %d", code)
	}
}

func TestBuildLaneWorkerEnv(t *testing.T) {
	root := buildLaneFixture(t, "exit 0")
	t.Setenv("TUSKER_STATE_ROOT", root)
	env := buildLaneWorkerEnv([]string{"PATH=/usr/bin"})
	dir := filepath.Join(root, "build-lane", "bin")
	if !strings.HasPrefix(runnerEnvValue(env, "PATH"), dir+string(os.PathListSeparator)) {
		t.Fatalf("PATH=%q", runnerEnvValue(env, "PATH"))
	}
	for _, name := range []string{"cargo", "swift", "xcodebuild"} {
		if _, err := os.Readlink(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if runnerEnvValue(env, "TUSKER_BUILD_LANE_STATE_ROOT") != root {
		t.Fatal("state root not passed")
	}
	acpEnv := acpRunnerEnvironment(StartRequest{AttemptID: "attempt", ItemID: "task"}, "/workspace", CodexPolicy{})
	if !strings.HasPrefix(runnerEnvValue(acpEnv, "PATH"), dir+string(os.PathListSeparator)) {
		t.Fatal("ACP worker PATH lacks build lane")
	}
}

func TestBuildLaneHeavySkipsToolchainAndGlobalFlags(t *testing.T) {
	if !buildLaneHeavy("cargo", []string{"+nightly", "-q", "check"}) || buildLaneHeavy("cargo", []string{"-q", "metadata"}) {
		t.Fatal("subcommand detection must skip +toolchain and global flags")
	}
}

func TestBuildLaneUnquietKeepsTestHarnessQuiet(t *testing.T) {
	cases := []struct {
		in, want []string
		quiet    bool
	}{
		{[]string{"test", "-q", "-p", "alpha"}, []string{"test", "-p", "alpha", "--", "--quiet"}, true},
		{[]string{"+nightly", "--quiet", "test", "--", "name"}, []string{"+nightly", "test", "--", "--quiet", "name"}, true},
		{[]string{"check", "-q"}, []string{"check"}, true},
		{[]string{"test", "--", "-q"}, []string{"test", "--", "-q"}, false},
	}
	for _, c := range cases {
		got, quiet := buildLaneUnquiet(c.in)
		if quiet != c.quiet || strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Fatalf("buildLaneUnquiet(%q) = %q, %v; want %q, %v", c.in, got, quiet, c.want, c.quiet)
		}
	}
	for line, status := range map[string]bool{"   Compiling alpha v0.1.0 (/x)": true, "    Finished `test` profile": true, "warning: unused": false, "error[E0425]: x": false, "test result: ok": false} {
		if cargoStatusLine.MatchString(line) != status {
			t.Fatalf("cargoStatusLine(%q) != %v", line, status)
		}
	}
}
