package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every checkout of a shared-mode project builds into the main checkout's
// cargo target dir: review worktrees, landing staging checkouts and checks.
func TestSharedModeSharesCargoTargetDir(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	t.Setenv("CARGO_TARGET_DIR", "")
	main := canonicalProjectPath(t.TempDir())
	runGitDir(t, main, "init", "-q")
	runGitDir(t, main, "config", "user.email", "t@example.com")
	runGitDir(t, main, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, main, "add", "-A")
	runGitDir(t, main, "commit", "-q", "-m", "base")
	staging := filepath.Join(t.TempDir(), "staging")
	runGitDir(t, main, "worktree", "add", "-q", "--detach", staging)
	want := filepath.Join(main, "target")

	if got := runnerEnvValue(sharedReviewCargoTargetEnv(nil, staging, ""), "CARGO_TARGET_DIR"); got != "" {
		t.Fatalf("non-shared project got CARGO_TARGET_DIR=%q", got)
	}
	if err := os.MkdirAll(filepath.Join(main, ".tusker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".tusker", "workspace.json"), []byte(`{"strategy":"shared"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runnerEnvValue(sharedReviewCargoTargetEnv(nil, staging, ""), "CARGO_TARGET_DIR"); got != want {
		t.Fatalf("staging checkout CARGO_TARGET_DIR=%q, want %q", got, want)
	}
	if got := runnerEnvValue(sharedReviewCargoTargetEnv(nil, main, ""), "CARGO_TARGET_DIR"); got != "" {
		t.Fatalf("shared checkout itself should keep cargo's default, got %q", got)
	}
	check := `test "$CARGO_TARGET_DIR" = "` + want + `"`
	if out, err := runV7LandingGateCommand(staging, check, false); err != nil {
		t.Fatalf("landing gate did not get the shared target dir: %v %s", err, out)
	}
	if obs, err := runV7VerificationCommand(staging, check, time.Minute); err != nil || obs.ExitCode != 0 {
		t.Fatalf("verification check did not get the shared target dir: exit=%d err=%v %s", obs.ExitCode, err, obs.Message)
	}
	if got := runnerEnvValue(sharedReviewCargoTargetEnv([]string{"CARGO_TARGET_DIR=/x"}, staging, ""), "CARGO_TARGET_DIR"); got != "/x" {
		t.Fatalf("explicit CARGO_TARGET_DIR overridden: %q", got)
	}
	if err := os.MkdirAll(filepath.Join(main, ".cargo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".cargo", "config.toml"), []byte("[build]\ntarget-dir = \"out\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runnerEnvValue(sharedReviewCargoTargetEnv(nil, staging, ""), "CARGO_TARGET_DIR"); got != "" {
		t.Fatalf("project cargo config target-dir overridden: %q", got)
	}
}

func TestDemoResetCommitsTrackerRecords(t *testing.T) {
	bin := demoTestBinary(t)
	demoTestEnv(t, bin)
	repo := t.TempDir()
	if code := demoRunInner(t, "demo seed", Args{"repo": repo, "scenario": demoScenario}); code != demoExitOK {
		t.Fatalf("seed exit %d", code)
	}
	before, err := demoGit(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if code := demoRunInner(t, "demo reset", Args{"repo": repo, "yes": "true"}); code != demoExitOK {
		t.Fatalf("reset exit %d", code)
	}
	status, err := demoGit(repo, "status", "--porcelain", "--", ".tusker/work")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(status) != "" {
		t.Fatalf("reset left tracker records uncommitted:\n%s", status)
	}
	after, _ := demoGit(repo, "rev-parse", "HEAD")
	subject, _ := demoGit(repo, "log", "-1", "--format=%s")
	if after == before || strings.TrimSpace(subject) != "demo task records" {
		t.Fatalf("reset did not commit records: head %q -> %q subject %q", before, after, subject)
	}
	tracked, err := demoGit(repo, "ls-files", ".tusker/work")
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range demoManifestForTest(t, repo).Tasks {
		if !strings.Contains(tracked, ".tusker/work/tasks/"+rec.TaskID+".md") {
			t.Fatalf("task record %s not committed after reset:\n%s", rec.TaskID, tracked)
		}
	}
}
