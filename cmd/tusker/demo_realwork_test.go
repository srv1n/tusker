package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoExecuteWaveUsesSupportedServeAction(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/capability", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"capability":"test-token"}`))
	})
	mux.HandleFunc("/api/waves/W-0001/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("project") != "project-1" || r.Header.Get(serveCapabilityHeader) != "test-token" {
			t.Fatalf("execute request missing project or capability: %s %#v", r.URL.String(), r.Header)
		}
		_, _ = w.Write([]byte(`{"ok":true,"execution":{"waveId":"W-0001","queuedTaskIds":["APP-T-0001"]}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	previous := demoServeBaseURL
	demoServeBaseURL = server.URL
	t.Cleanup(func() { demoServeBaseURL = previous })

	receipt, err := demoExecuteWave(context.Background(), "project-1", "W-0001")
	if err != nil || receipt.WaveID != "W-0001" || len(receipt.QueuedTaskIDs) != 1 {
		t.Fatalf("supported Execute Wave action failed: receipt=%#v err=%v", receipt, err)
	}
}

// TestRealWorkFixtureShape pins the standalone-task addition: thirteen tasks
// under sample/ with unique artifacts, all three work levels covered, and
// bounded per-task context linking the shared fictional spec.
func TestRealWorkFixtureShape(t *testing.T) {
	waves := demoFixtureWaves()
	total := 0
	byKey := map[string]demoTaskDef{}
	for _, wave := range waves {
		for _, task := range wave.Tasks {
			total++
			byKey[task.Key] = task
			if !strings.HasPrefix(task.Artifact, "sample/") {
				t.Fatalf("task %s artifact %q is not under sample/", task.Key, task.Artifact)
			}
		}
	}
	if total != 13 {
		t.Fatalf("expected 13 tasks, got %d", total)
	}
	s1, ok := byKey["s1"]
	if !ok {
		t.Fatal("standalone task s1 missing")
	}
	if len(s1.Deps) != 0 {
		t.Fatalf("s1 must be dependency-free, got %v", s1.Deps)
	}
	levels := map[string]bool{}
	for _, task := range byKey {
		levels[demoWorkLevel(task.Complexity)] = true
	}
	for _, level := range []string{"light", "standard", "demanding"} {
		if !levels[level] {
			t.Fatalf("fixture does not cover work level %s", level)
		}
	}
	for _, wave := range waves {
		for _, task := range wave.Tasks {
			ctx := demoTaskContext(wave, task)
			for _, want := range []string{
				".tusker/specs/fixture-cafe/overview.md",
				task.Artifact,
				"never edit task status files",
				"python3 sample/tools/wait_progress.py",
				"independent review lane",
			} {
				if !strings.Contains(ctx, want) {
					t.Fatalf("task %s context missing %q", task.Key, want)
				}
			}
		}
	}
}

func TestDemoConfigureUnattendedWorkflow(t *testing.T) {
	vault := filepath.Join(t.TempDir(), ".tusker")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := demoConfigureUnattendedWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil || wf.Data.Codex.ApprovalPolicy != "never" {
		t.Fatalf("demo workflow retained interactive approval policy: %#v err=%v", wf.Data.Codex, err)
	}
}

// TestRealWorkFixtureStaticFiles checks the seeded sample project and
// knowledge corpus writers without a built binary.
func TestRealWorkFixtureStaticFiles(t *testing.T) {
	files := demoRealWorkFiles()
	for _, want := range []string{
		"sample/README.md",
		"sample/tools/wait_progress.py",
		".tusker/specs/fixture-cafe/overview.md",
		".tusker/specs/fixture-cafe/catalog/index.md",
		".tusker/specs/fixture-cafe/catalog/blend-intent.md",
		".tusker/specs/fixture-cafe/catalog/legacy-blend.md",
		".tusker/specs/fixture-cafe/ops/index.md",
		".tusker/specs/fixture-cafe/ops/brewing.md",
		".tusker/specs/fixture-cafe/ops/grind-decision.md",
	} {
		if _, ok := files[want]; !ok {
			t.Fatalf("seeded file %s missing", want)
		}
	}
	helper := files["sample/tools/wait_progress.py"]
	for _, want := range []string{"--duration", "--interval", "--short", "128 +", "MAX_LINES"} {
		if !strings.Contains(helper, want) {
			t.Fatalf("progress helper missing %q", want)
		}
	}
	readme := files["sample/README.md"]
	for _, want := range []string{"demo seed", "demo run", "demo check", "demo reset", "fixture-cafe"} {
		if !strings.Contains(readme, want) {
			t.Fatalf("sample README missing %q", want)
		}
	}
}

// TestRealWorkProgressHelper executes the seeded helper with short timings.
// It is skipped only when python3 is unavailable.
func TestRealWorkFixtureProgressHelper(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	dir := t.TempDir()
	helper := filepath.Join(dir, "wait_progress.py")
	if err := os.WriteFile(helper, []byte(demoProgressHelper()), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, helper, "--duration", "1", "--interval", "0.2", "--label", "probe")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper failed: %s\n%s", err.Error(), string(out))
	}
	text := string(out)
	if !strings.Contains(text, "done in") {
		t.Fatalf("helper output missing completion line:\n%s", text)
	}
	if lines := strings.Count(text, "\n"); lines > 720 {
		t.Fatalf("helper output not bounded: %d lines", lines)
	}
	bad := exec.Command(python, helper, "--duration", "0")
	if out, err := bad.CombinedOutput(); err == nil {
		t.Fatalf("zero duration unexpectedly passed:\n%s", string(out))
	}
}

func realWorkSeedForTest(t *testing.T, repo string) *demoManifest {
	t.Helper()
	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("seed exit %d", code)
	}
	return demoManifestForTest(t, repo)
}

// TestRealWorkFixtureSeedE2E covers seed content and manifest/profile
// behavior: static files on disk, docs-check-clean corpus, project
// registration, idempotent reseed, and a runnable helper.
func TestRealWorkFixtureSeedE2E(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()
	manifest := realWorkSeedForTest(t, repo)
	if len(manifest.Waves) != 4 || len(manifest.Tasks) != 13 {
		t.Fatalf("seed mapping: %d waves %d tasks", len(manifest.Waves), len(manifest.Tasks))
	}
	if manifest.Tasks["s1"].Complexity == "" {
		t.Fatal("s1 has no recorded complexity")
	}
	if len(manifest.CreatedPaths) == 0 {
		t.Fatal("seed recorded no created paths")
	}
	for _, rel := range manifest.CreatedPaths {
		if !fileExists(filepath.Join(repo, filepath.FromSlash(rel))) {
			t.Fatalf("created path missing on disk: %s", rel)
		}
	}
	if manifest.LocalProjectID == "" {
		t.Fatal("seed recorded no local project ID")
	}
	if !manifest.RuntimeRegistered || manifest.RuntimeProjectID == "" {
		t.Fatalf("seed did not register the runtime-scope project: %s", manifest.RuntimeProblem)
	}
	if manifest.RuntimeScope == "" {
		t.Fatal("seed recorded no runtime scope")
	}
	// The default corpus validates: docs check must report valid.
	exec := demoNewExec()
	vaultPath := demoVaultPath(repo)
	if got := backpressureCommands(vaultPath); len(got) != 1 || got[0] != "git diff --check" {
		t.Fatalf("demo landing gate must be repository-neutral, got %v", got)
	}
	checked, err := exec.run(repo, "docs", "check", "--vault", vaultPath)
	if err != nil {
		t.Fatalf("docs check failed: %s", err.Error())
	}
	if valid, _ := checked["valid"].(bool); !valid {
		raw, _ := json.Marshal(checked["issues"])
		t.Fatalf("seeded corpus does not validate: %s", string(raw))
	}
	route, err := exec.run(repo, "runner", "route", manifest.Tasks["s1"].TaskID, "--lane", "execute", "--vault", vaultPath)
	if err != nil || demoStringField(route, "profile") != "execute-fast" || demoStringField(route, "model") != "gpt-5.6-luna" {
		t.Fatalf("standalone route is not configured Luna: route=%v err=%v", route, err)
	}
	reviewRoute, err := exec.run(repo, "runner", "route", manifest.Tasks["s1"].TaskID, "--lane", "review", "--vault", vaultPath)
	if err != nil || demoStringField(reviewRoute, "profile") != "review-independent" {
		t.Fatalf("standalone review route is not independent: route=%v err=%v", reviewRoute, err)
	}
	// Reseed without reset is idempotent: no duplicates, same identities.
	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("reseed exit %d", code)
	}
	again := demoManifestForTest(t, repo)
	for key, task := range manifest.Tasks {
		if again.Tasks[key].TaskID != task.TaskID {
			t.Fatalf("reseed changed mapping for %s", key)
		}
	}
	if again.RuntimeProjectID != manifest.RuntimeProjectID {
		t.Fatal("reseed changed the runtime project registration")
	}
}

// TestRealWorkFixtureRealRefusal covers real-mode refusal with no
// substitution: unknown harnesses, profile mismatches, and offline-only
// flags are precondition/invalid errors, and no timer run is recorded.
func TestRealWorkFixtureRealRefusal(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()
	manifest := realWorkSeedForTest(t, repo)
	runsBefore := len(manifest.Runs)

	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "standalone", "mode": "real", "require-harness": "no-such-harness-xyz"}); code != demoExitPrecondition {
		t.Fatalf("unknown harness exit %d, want %d", code, demoExitPrecondition)
	}
	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "standalone", "mode": "real", "profile": "no-such-profile-xyz"}); code != demoExitPrecondition {
		t.Fatalf("profile mismatch exit %d, want %d", code, demoExitPrecondition)
	}
	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "standalone", "mode": "real"}); code != demoExitInvalid {
		t.Fatalf("missing harness/profile exit %d, want %d", code, demoExitInvalid)
	}
	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "standalone", "mode": "real", "fast": "true", "require-harness": "x"}); code != demoExitInvalid {
		t.Fatalf("real+fast exit %d, want %d", code, demoExitInvalid)
	}
	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "standalone", "mode": "real", "fail-once": "s1", "require-harness": "x"}); code != demoExitInvalid {
		t.Fatalf("real+fail-once exit %d, want %d", code, demoExitInvalid)
	}
	if code, _ := runInner("demo run", Args{"repo": repo, "waves": "standalone", "profile": "x"}); code != demoExitInvalid {
		t.Fatalf("offline+profile exit %d, want %d", code, demoExitInvalid)
	}
	after := demoManifestForTest(t, repo)
	if len(after.Runs) != runsBefore {
		t.Fatal("refused real run recorded a run: silent fallback is forbidden")
	}
	for _, run := range after.Runs {
		if run.Executor == "demo-timer" && len(after.Runs) > runsBefore {
			t.Fatal("refused real run fell back to the timer lane")
		}
	}
}

// TestRealWorkFixtureActiveResetRefused covers the active-run guard: reset
// refuses while a lease is held, changes nothing, names the supported
// cancellation command, and proceeds once the owner releases.
func TestRealWorkFixtureActiveResetRefused(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()
	manifest := realWorkSeedForTest(t, repo)
	vaultPath := demoVaultPath(repo)
	s1 := manifest.Tasks["s1"].TaskID
	if code, _ := runInner("work start", Args{"id": s1, "by": "agent:test-guard", "vault": vaultPath}); code != 0 {
		t.Fatalf("work start exit %d", code)
	}
	if code, _ := runInner("demo reset", Args{"repo": repo, "yes": "true"}); code != demoExitPrecondition {
		t.Fatalf("reset with active run exit %d, want %d", code, demoExitPrecondition)
	}
	still := demoManifestForTest(t, repo)
	if still.Tasks["s1"].TaskID != manifest.Tasks["s1"].TaskID {
		t.Fatal("refused reset mutated the mapping")
	}
	if code, _ := runInner("work release", Args{"id": s1, "by": "agent:test-guard", "reason": "test done", "vault": vaultPath}); code != 0 {
		t.Fatalf("work release exit %d", code)
	}
	if code := demoRunInner(t, "demo reset", Args{"repo": repo, "yes": "true"}); code != demoExitOK {
		t.Fatalf("reset after release exit %d", code)
	}
}

// TestRealWorkFixtureResetE2E covers reset safety for the new content:
// preview is non-mutating, apply removes manifest-owned paths and the
// runtime registration, reseed restores everything with fresh identities.
func TestRealWorkFixtureResetE2E(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()
	manifest := realWorkSeedForTest(t, repo)
	oldIDs := map[string]string{}
	for key, task := range manifest.Tasks {
		oldIDs[key] = task.TaskID
	}
	oldRuntime := manifest.RuntimeProjectID

	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "standalone", "fast": "true"}); code != demoExitOK {
		t.Fatalf("standalone run exit %d", code)
	}
	before, _ := json.Marshal(demoManifestForTest(t, repo).Tasks)
	if code := demoRunInner(t, "demo reset", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("reset preview exit %d", code)
	}
	previewRaw, _ := json.Marshal(demoManifestForTest(t, repo).Tasks)
	if string(before) != string(previewRaw) {
		t.Fatal("reset preview mutated the mapping")
	}
	if code := demoRunInner(t, "demo reset", Args{"repo": repo, "yes": "true"}); code != demoExitOK {
		t.Fatalf("reset apply exit %d", code)
	}
	fresh := demoManifestForTest(t, repo)
	// Reseed restores an equivalent scenario: same contract identities, no
	// duplicates, run ledger preserved with a reset watermark.
	seenIDs := map[string]string{}
	for key, task := range fresh.Tasks {
		if task.TaskID != oldIDs[key] {
			t.Fatalf("reseed changed the contract identity for %s: %s -> %s", key, oldIDs[key], task.TaskID)
		}
		if prev, dup := seenIDs[task.TaskID]; dup {
			t.Fatalf("duplicate task identity %s for %s and %s", task.TaskID, prev, key)
		}
		seenIDs[task.TaskID] = key
	}
	if fresh.RunsAtReset != 1 {
		t.Fatalf("RunsAtReset = %d, want 1 (one run before reset)", fresh.RunsAtReset)
	}
	for _, rel := range fresh.CreatedPaths {
		if !fileExists(filepath.Join(repo, filepath.FromSlash(rel))) {
			t.Fatalf("reseed did not restore %s", rel)
		}
	}
	if fresh.RuntimeProjectID == "" || !fresh.RuntimeRegistered {
		t.Fatal("reseed did not restore the runtime registration")
	}
	if fresh.RuntimeProjectID == oldRuntime {
		t.Logf("runtime scope reused the project ID (idempotent registration)")
	}
	if fileExists(filepath.Join(repo, "sample", "standalone", "smoke.txt")) {
		t.Fatal("reset left the standalone artifact behind")
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("post-reset check exit %d", code)
	}
	if code := demoRunInner(t, "demo run", Args{"repo": repo, "waves": "standalone", "fast": "true"}); code != demoExitOK {
		t.Fatalf("post-reset standalone run exit %d", code)
	}
	if code := demoRunInner(t, "demo check", Args{"repo": repo}); code != demoExitOK {
		t.Fatalf("final check exit %d", code)
	}
	// Freshness lives in attempts: no runtime attempt ID may be reused
	// across runs, and the post-reset run must record its own.
	final := demoManifestForTest(t, repo)
	if len(final.Runs) < 2 {
		t.Fatalf("expected at least 2 runs, got %d", len(final.Runs))
	}
	seen := map[string]int{}
	for i, run := range final.Runs {
		for _, interval := range run.Intervals {
			if interval.Attempt == "" {
				continue
			}
			if prev, dup := seen[interval.Attempt]; dup && prev != i {
				t.Fatalf("attempt %s reused across runs %d and %d", interval.Attempt, prev, i)
			}
			seen[interval.Attempt] = i
		}
	}
	lastAttempts := 0
	for _, interval := range final.Runs[len(final.Runs)-1].Intervals {
		if interval.Attempt != "" {
			lastAttempts++
		}
	}
	if lastAttempts == 0 {
		t.Fatal("post-reset run recorded no attempt IDs")
	}
}
