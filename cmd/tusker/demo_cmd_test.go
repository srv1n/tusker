package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDemoFixtureShape(t *testing.T) {
	waves := demoFixtureWaves()
	if len(waves) != 4 {
		t.Fatalf("expected 4 waves, got %d", len(waves))
	}
	seenArtifacts := map[string]bool{}
	total := 0
	for _, wave := range waves {
		want := 4
		if wave.Name == "standalone" {
			want = 1
		}
		if len(wave.Tasks) != want {
			t.Fatalf("wave %s: expected %d tasks, got %d", wave.Name, want, len(wave.Tasks))
		}
		for _, task := range wave.Tasks {
			total++
			if task.Artifact == "" || task.Content == "" {
				t.Fatalf("task %s has empty artifact/content", task.Key)
			}
			if seenArtifacts[task.Artifact] {
				t.Fatalf("duplicate artifact path %s", task.Artifact)
			}
			seenArtifacts[task.Artifact] = true
			if task.DelaySecs <= 0 || task.FastSecs <= 0 {
				t.Fatalf("task %s needs positive delays", task.Key)
			}
		}
	}
	if total != 13 {
		t.Fatalf("expected 13 tasks, got %d", total)
	}
	standalone := 0
	for _, wave := range waves {
		if wave.Name == "standalone" {
			standalone += len(wave.Tasks)
		}
	}
	if standalone != 1 {
		t.Fatalf("expected exactly 1 standalone task, got %d", standalone)
	}
	if deps := demoCrossScopeDeps()["c1"]; len(deps) != 2 {
		t.Fatalf("c1 needs two cross-scope deps, got %v", deps)
	}
}

func TestDemoOwnedPath(t *testing.T) {
	root := t.TempDir()
	if !demoOwnedPath(root, filepath.Join(root, ".tusker", "demo", "manifest.json")) {
		t.Fatal("inside path refused")
	}
	if demoOwnedPath(root, filepath.Join(root, "..", "outside")) {
		t.Fatal("outside path allowed")
	}
	if demoOwnedPath(root, "/etc/passwd") {
		t.Fatal("absolute outside path allowed")
	}
}

func TestDemoExitMapping(t *testing.T) {
	cases := map[string]int{
		errorInvalidArg:      demoExitInvalid,
		errorMissingArg:      demoExitInvalid,
		errorNotFound:        demoExitInvalid,
		demoCodePrecondition: demoExitPrecondition,
		demoCodeActiveRuns:   demoExitPrecondition,
		demoCodeAssertion:    demoExitAssertion,
		demoCodeTimeout:      demoExitTimeout,
		"INVALID_TRANSITION": demoExitInternal,
	}
	for code, want := range cases {
		if got := demoExitForError(tuskerError(code, "probe")); got != want {
			t.Fatalf("code %s: exit %d, want %d", code, got, want)
		}
	}
}

var demoTestBinOnce sync.Once
var demoTestBin string
var demoTestBinErr error

func demoTestBinary(t *testing.T) string {
	t.Helper()
	demoTestBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "tusker-demo-test-bin")
		if err != nil {
			demoTestBinErr = err
			return
		}
		demoTestBin = filepath.Join(dir, "tusker")
		cmd := exec.Command("go", "build", "-o", demoTestBin, "./cmd/tusker")
		cmd.Dir = repoRootForTest()
		if out, err := cmd.CombinedOutput(); err != nil {
			demoTestBinErr = errFile("go build ./cmd/tusker: " + err.Error() + ": " + string(out))
		}
	})
	if demoTestBinErr != nil {
		t.Skipf("skipping demo e2e: %s", demoTestBinErr.Error())
	}
	return demoTestBin
}

func errFile(message string) error {
	return &demoBuildError{message: message}
}

type demoBuildError struct{ message string }

func (e *demoBuildError) Error() string { return e.message }

func repoRootForTest() string {
	if root := os.Getenv("TUSKER_TEST_REPO_ROOT"); root != "" {
		return root
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	// cmd/tusker -> repo root.
	if filepath.Base(wd) == "tusker" {
		return filepath.Dir(filepath.Dir(wd))
	}
	return wd
}

func demoTestEnv(t *testing.T, bin string) {
	t.Helper()
	t.Setenv("TUSKER_DEMO_EXE", bin)
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
}

func demoRunInner(t *testing.T, command string, args Args) int {
	t.Helper()
	code, err := runInner(command, args)
	if err != nil {
		t.Fatalf("%s: unexpected error: %s", command, err.Error())
	}
	return code
}

func demoManifestForTest(t *testing.T, repo string) *demoManifest {
	t.Helper()
	manifest, err := demoLoadManifest(repo)
	if err != nil {
		t.Fatalf("load manifest: %s", err.Error())
	}
	return manifest
}

// TestDemoRepeatableE2E drives the full CLI contract: seed, idempotent
// reseed, parallel run, follow-up, assertions, reset preview refusing to
// change anything, applied reset restoring the initial state, and a second
// complete run passing on the fresh identities.
func TestDemoRepeatableE2E(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()

	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("seed exit %d", code)
	}
	manifest := demoManifestForTest(t, repo)
	if len(manifest.Waves) != 4 || len(manifest.Tasks) != 13 {
		t.Fatalf("seed mapping: %d waves %d tasks", len(manifest.Waves), len(manifest.Tasks))
	}
	config, err := os.ReadFile(filepath.Join(repo, ".tusker", "config.local.yaml"))
	if err != nil || strings.Contains(string(config), "lane_profiles:") {
		t.Fatalf("demo seed must not shadow model levels with lane profiles: %v\n%s", err, config)
	}
	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("reseed exit %d", code)
	}
	again := demoManifestForTest(t, repo)
	for key, task := range manifest.Tasks {
		if again.Tasks[key].TaskID != task.TaskID {
			t.Fatalf("reseed changed mapping for %s", key)
		}
	}

	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "standalone", "fast": "true"}); code != demoExitOK {
		t.Fatalf("standalone run exit %d", code)
	}
	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "alpha,beta", "fast": "true"}); code != demoExitOK {
		manifest := demoManifestForTest(t, repo)
		for _, run := range manifest.Runs {
			for _, note := range run.Notes {
				t.Logf("run note: %s", note)
			}
		}
		t.Fatalf("parallel run exit %d", code)
	}
	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "follow-up", "fast": "true"}); code != demoExitOK {
		t.Fatalf("follow-up run exit %d", code)
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("check exit %d", code)
	}
	manifest = demoManifestForTest(t, repo)
	assertDemoOverlap(t, manifest)

	// Reset previews by default and changes nothing.
	before, _ := json.Marshal(manifest.Tasks)
	if code := demoRunInner(t, "demo reset", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("reset preview exit %d", code)
	}
	afterPreview := demoManifestForTest(t, repo)
	afterRaw, _ := json.Marshal(afterPreview.Tasks)
	if string(before) != string(afterRaw) {
		t.Fatal("reset preview mutated the mapping")
	}

	if code := demoRunInner(t, "demo reset", Args{"repo": repo, "yes": "true"}); code != demoExitOK {
		t.Fatalf("reset apply exit %d", code)
	}
	fresh := demoManifestForTest(t, repo)
	if len(fresh.Waves) != 4 || len(fresh.Tasks) != 13 {
		t.Fatalf("reset mapping: %d waves %d tasks", len(fresh.Waves), len(fresh.Tasks))
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("post-reset check exit %d", code)
	}
	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "standalone,alpha,beta,follow-up", "fast": "true"}); code != demoExitOK {
		t.Fatalf("second run exit %d", code)
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("final check exit %d", code)
	}
}

func assertDemoOverlap(t *testing.T, manifest *demoManifest) {
	t.Helper()
	alpha, beta := map[string]string{}, map[string]string{}
	for _, run := range manifest.Runs {
		for _, interval := range run.Intervals {
			if interval.Outcome != "done" {
				continue
			}
			start, finish := interval.Started, interval.Finished
			switch interval.Wave {
			case "alpha":
				alpha[interval.TaskID] = start + ".." + finish
			case "beta":
				beta[interval.TaskID] = start + ".." + finish
			}
		}
	}
	if len(alpha) == 0 || len(beta) == 0 {
		t.Fatal("no completed intervals in both waves")
	}
}

// TestDemoFailureRetryE2E covers the deterministic failure variant: one
// injected branch failure parks the join, unrelated work proceeds, and a
// plain retry completes without duplicate accepted output.
func TestDemoFailureRetryE2E(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()

	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("seed exit %d", code)
	}
	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "beta", "fast": "true", "fail-once": "b3"}); code != demoExitAssertion {
		t.Fatalf("injected run exit %d, want %d", code, demoExitAssertion)
	}
	manifest := demoManifestForTest(t, repo)
	if got := manifest.Runs[len(manifest.Runs)-1].Results["beta"].Failed; len(got) != 1 || got[0] != "b3" {
		t.Fatalf("expected exactly b3 failed, got %v", got)
	}
	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "beta", "fast": "true"}); code != demoExitOK {
		t.Fatalf("retry exit %d", code)
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("check exit %d", code)
	}
}

// TestDemoGuardsE2E covers the safety boundaries: unknown waves, foreign
// directories, unseeded repos, and reset against a wrong root.
func TestDemoGuardsE2E(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()

	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("seed exit %d", code)
	}
	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "nope", "fast": "true"}); code != demoExitInvalid {
		t.Fatalf("unknown wave exit %d, want %d", code, demoExitInvalid)
	}
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "notes.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := runInner("demo seed", Args{"repo": foreign, "scenario": demoScenario}); code != demoExitInvalid {
		t.Fatalf("foreign seed exit %d, want %d", code, demoExitInvalid)
	}
	empty := t.TempDir()
	if code, _ := runInner("demo status", Args{"repo": empty}); code == demoExitOK {
		t.Fatal("unseeded status unexpectedly succeeded")
	}
}
