package main

import (
	"encoding/json"
	"os"
	"os/exec"
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
	if err := os.WriteFile(filepath.Join(bin, "swift"), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
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

func TestBuildLaneDigest(t *testing.T) {
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
	go func() { done <- runBuildLane("swift", []string{"build"}) }()
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
			if code := runBuildLane("swift", []string{"build"}); code != 0 {
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

func TestBuildLaneWorkerEnv(t *testing.T) {
	root := buildLaneFixture(t, "exit 0")
	t.Setenv("TUSKER_STATE_ROOT", root)
	env := buildLaneWorkerEnv([]string{"PATH=/usr/bin"})
	dir := filepath.Join(root, "build-lane", "bin")
	if !strings.HasPrefix(runnerEnvValue(env, "PATH"), dir+string(os.PathListSeparator)) {
		t.Fatalf("PATH=%q", runnerEnvValue(env, "PATH"))
	}
	for _, name := range []string{"tusker-rustc", "swift", "xcodebuild"} {
		if _, err := os.Readlink(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if runnerEnvValue(env, "RUSTC_WRAPPER") != filepath.Join(dir, "tusker-rustc") || runnerEnvValue(env, "CARGO_BUILD_JOBS") == "" {
		t.Fatalf("cargo env: RUSTC_WRAPPER=%q CARGO_BUILD_JOBS=%q", runnerEnvValue(env, "RUSTC_WRAPPER"), runnerEnvValue(env, "CARGO_BUILD_JOBS"))
	}
	chained := buildLaneWorkerEnv([]string{"PATH=/usr/bin", "RUSTC_WRAPPER=sccache"})
	if runnerEnvValue(chained, "TUSKER_RUSTC_WRAPPER_NEXT") != "sccache" {
		t.Fatal("existing RUSTC_WRAPPER was not chained")
	}
	if runnerEnvValue(env, "TUSKER_BUILD_LANE_STATE_ROOT") != root {
		t.Fatal("state root not passed")
	}
	acpEnv := acpRunnerEnvironment(StartRequest{AttemptID: "attempt", ItemID: "task"}, "/workspace", CodexPolicy{})
	if !strings.HasPrefix(runnerEnvValue(acpEnv, "PATH"), dir+string(os.PathListSeparator)) {
		t.Fatal("ACP worker PATH lacks build lane")
	}
}

func TestBuildLaneHeavyOnlyForSwiftBuilds(t *testing.T) {
	if !buildLaneHeavy("swift", []string{"test"}) || buildLaneHeavy("swift", []string{"package", "resolve"}) || buildLaneHeavy("cargo", []string{"build"}) {
		t.Fatal("heavy detection")
	}
}

// TestRustcWrapperHelper is the child process for TestRustcWrapperQueuesAndLogs;
// the wrapper execs rustc, so it cannot run inside the test process.
func TestRustcWrapperHelper(t *testing.T) {
	if os.Getenv("TUSKER_RUSTC_HELPER") != "1" {
		t.Skip("helper process")
	}
	os.Exit(runRustcWrapper(strings.Split(os.Getenv("TUSKER_RUSTC_ARGS"), " ")))
}

func TestRustcWrapperQueuesAndLogs(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "ran")
	rustc := filepath.Join(root, "rustc")
	if err := os.WriteFile(rustc, []byte("#!/bin/sh\necho \"$@\" >> "+out+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	cargoHome := filepath.Join(root, "cargo-home")
	run := func(manifest string, args ...string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestRustcWrapperHelper$")
		cmd.Env = append(os.Environ(), "TUSKER_RUSTC_HELPER=1", "TUSKER_RUSTC_ARGS="+strings.Join(append([]string{rustc}, args...), " "),
			"TUSKER_BUILD_LANE_STATE_ROOT="+filepath.Join(root, "state"), "CARGO_HOME="+cargoHome, "CARGO_MANIFEST_DIR="+manifest, "TUSKER_PROJECT_ID=p", "TUSKER_RUSTC_WRAPPER_NEXT=")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("wrapper: %v\n%s", err, b)
		}
	}
	run("", "-vV")
	run(filepath.Join(root, "ws", "crates", "alpha"), "--crate-name", "alpha", "src/lib.rs", "--out-dir", filepath.Join(root, "ws", "target", "debug", "deps"))
	run(filepath.Join(cargoHome, "registry", "src", "serde-1.0.0"), "--crate-name", "serde", "src/lib.rs", "--out-dir", filepath.Join(root, "ws", "target", "debug", "deps"))
	ran, err := os.ReadFile(out)
	if err != nil || strings.Count(string(ran), "\n") != 3 {
		t.Fatalf("rustc runs: %q %v", ran, err)
	}
	records := buildLaneRecords(t, filepath.Join(root, "state"))
	if len(records) != 2 {
		t.Fatalf("probe must not log; got %#v", records)
	}
	if records[0]["crate"] != "alpha" || records[0]["local"] != true || records[1]["local"] != false || records[1]["target"] != filepath.Join(root, "ws", "target") {
		t.Fatalf("records: %#v", records)
	}
}

func TestBuildLaneSlotTakesFirstFreed(t *testing.T) {
	root := t.TempDir()
	a, _, err := buildLaneSlot(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, _, err := buildLaneSlot(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan error, 1)
	go func() {
		f, _, err := buildLaneSlot(root, 2)
		if err == nil {
			f.Close()
		}
		got <- err
	}()
	time.Sleep(50 * time.Millisecond)
	b.Close() // free the second slot; the first stays held
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not take the freed slot")
	}
}

func TestBuildLaneLoginShellsKeepShim(t *testing.T) {
	root := buildLaneFixture(t, "exit 0")
	t.Setenv("TUSKER_STATE_ROOT", root)
	home := t.TempDir()
	// Dotfiles that reorder PATH the way real ones do, plus a marker proving they still run.
	reorder := "export PATH=/usr/bin:$PATH\nexport TUSKER_TEST_DOTFILE=1\n"
	for _, f := range []string{".zprofile", ".profile"} {
		if err := os.WriteFile(filepath.Join(home, f), []byte(reorder), 0644); err != nil {
			t.Fatal(err)
		}
	}
	env := buildLaneWorkerEnv([]string{"HOME=" + home, "PATH=/usr/bin:/bin"})
	want := filepath.Join(root, "build-lane", "bin", "swift") + "\n1\n"
	ran := false
	for _, shell := range []string{"zsh", "bash"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			continue
		}
		ran = true
		cmd := exec.Command(path, "-lc", "command -v swift; echo $TUSKER_TEST_DOTFILE")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil || string(out) != want {
			t.Errorf("%s -lc: %q %v, want %q", shell, out, err, want)
		}
	}
	if !ran {
		t.Skip("no zsh or bash")
	}
}
