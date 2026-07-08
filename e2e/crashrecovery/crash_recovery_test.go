//go:build !windows

package crashrecovery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const crashTaskID = "APP-T-0001"

const (
	crashRunWait    = 20 * time.Second
	crashRunnerWait = 15 * time.Second
)

var builtE2EBinaries struct {
	once       sync.Once
	dir        string
	tusker     string
	fakeRunner string
	err        error
}

func TestMain(m *testing.M) {
	if stale := fixtureProcesses(""); len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "crashrecovery preflight reaping stale fixture processes:\n%s\n", summarizeFixtureProcesses(stale))
		reapFixtureProcesses("")
		if survivors := waitForNoFixtureProcesses("", 5*time.Second); len(survivors) > 0 {
			fmt.Fprintf(os.Stderr, "crashrecovery preflight could not reap fixture processes:\n%s\n", summarizeFixtureProcesses(survivors))
			os.Exit(1)
		}
	}
	code := m.Run()
	if survivors := fixtureProcesses(""); len(survivors) > 0 {
		fmt.Fprintf(os.Stderr, "crashrecovery suite leaked fixture processes:\n%s\n", summarizeFixtureProcesses(survivors))
		reapFixtureProcesses("")
		code = 1
	}
	cleanupE2EBinaries()
	os.Exit(code)
}

func TestFixtureProcessCleanupReapsWrapperAndRunner(t *testing.T) {
	h := newHarness(t, "fixture-process-cleanup")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "hold",
		RunnerKind:     "codex_exec",
		StallTimeoutMS: 5000,
		MaxAttempts:    1,
	})
	h.createRunnableTask("fixture cleanup reaps wrapper and runner")

	daemon := h.startDaemon("daemon")
	run := h.waitRun(crashTaskID, crashRunnerWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") > 0
	})
	wrapperPID := runInt(run, "process_pid")
	childPID := h.waitRunnerPID(crashRunnerWait)
	daemon.kill(syscall.SIGKILL)
	if !processAlive(wrapperPID) {
		t.Fatalf("wrapper pid %d died before fixture cleanup could reap it", wrapperPID)
	}
	if !processAlive(childPID) {
		t.Fatalf("fake runner pid %d died before fixture cleanup could reap it", childPID)
	}

	h.reapFixtureProcesses()
	assertNoFixtureProcesses(t, h.tempRoot)
}

func TestHoldModeFakeRunnerSelfExpires(t *testing.T) {
	_, fakeRunner := e2eBinaries(t)
	tempRoot, err := os.MkdirTemp(shortTempParent(), "tusker-crash-hold-self-expire-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		reapFixtureProcesses(tempRoot)
		_ = os.RemoveAll(tempRoot)
	})
	readyFile := filepath.Join(tempRoot, "runner-ready")
	pidFile := filepath.Join(tempRoot, "runner.pid")
	var output bytes.Buffer
	cmd := exec.Command(fakeRunner,
		"--mode", "hold",
		"--ready-file", readyFile,
		"--pid-file", pidFile,
		"--heartbeat-every", "25ms",
		"--hold-timeout", "250ms",
	)
	cmd.Stdout = &output
	cmd.Stderr = &output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake runner: %v", err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return
		}
		killProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
	})
	eventually(t, 2*time.Second, 25*time.Millisecond, func() (bool, string) {
		if _, err := os.Stat(readyFile); err != nil {
			return false, err.Error()
		}
		return true, ""
	})
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 124 {
			t.Fatalf("hold fake runner should exit 124 after timeout, got err=%v output=%s", err, strings.TrimSpace(output.String()))
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("hold fake runner did not self-expire; output=%s", strings.TrimSpace(output.String()))
	}
	if !strings.Contains(output.String(), "hold timeout") {
		t.Fatalf("hold timeout exit should explain itself, output=%s", strings.TrimSpace(output.String()))
	}
	assertNoFixtureProcesses(t, tempRoot)
}

func TestNoSurvivingFixtureProcesses(t *testing.T) {
	assertNoFixtureProcesses(t, "")
}

func TestDaemonKillNineAdoptsSurvivingWrapper(t *testing.T) {
	h := newHarness(t, "daemon-kill-adoption")
	releaseFile := filepath.Join(h.tempRoot, "release")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "hold-success",
		RunnerKind:     "codex_exec",
		ReleaseFile:    releaseFile,
		CompleteStatus: "review",
		StallTimeoutMS: 5000,
		MaxAttempts:    1,
	})
	h.createRunnableTask("adopt live runner after daemon kill")

	first := h.startDaemon("daemon-1")
	run := h.waitRun(crashTaskID, crashRunnerWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") > 0 && runString(run, "last_heartbeat_at") != ""
	})
	wrapperPID := runInt(run, "process_pid")
	leaseGeneration := runInt(run, "lease_generation")
	attemptCount := runInt(run, "attempt_count")
	childPID := h.waitRunnerPID(crashRunnerWait)
	if childPID == wrapperPID {
		t.Fatalf("expected wrapper pid and fake runner child pid to differ, both were %d", wrapperPID)
	}
	first.kill(syscall.SIGKILL)
	if !processAlive(wrapperPID) {
		t.Fatalf("wrapper pid %d died with daemon; D2 requires runner survival", wrapperPID)
	}
	if !processAlive(childPID) {
		t.Fatalf("runner child pid %d died with daemon; wrapper must preserve the child", childPID)
	}

	second := h.startDaemon("daemon-2")
	h.waitRun(crashTaskID, crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" &&
			runInt(run, "process_pid") == wrapperPID &&
			runInt(run, "lease_generation") == leaseGeneration &&
			runInt(run, "attempt_count") == attemptCount &&
			runString(run, "last_heartbeat_at") != ""
	})
	h.touch(releaseFile)
	h.waitRun(crashTaskID, crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "released" &&
			runString(run, "attempt_outcome") == "succeeded" &&
			runInt(run, "process_pid") == 0
	})
	second.stop()
}

func TestDaemonRestartRedispatchesDeadWrapper(t *testing.T) {
	h := newHarness(t, "daemon-restart-dead-wrapper")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "hold",
		RunnerKind:     "codex_exec",
		StallTimeoutMS: 5000,
		MaxAttempts:    2,
	})
	h.createRunnableTask("dead wrapper gets redispatched after daemon restart")

	first := h.startDaemon("daemon-1")
	run := h.waitRun(crashTaskID, crashRunnerWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") > 0 && runInt(run, "attempt_count") == 1
	})
	wrapperPID := runInt(run, "process_pid")
	childPID := h.waitRunnerPID(crashRunnerWait)
	first.kill(syscall.SIGKILL)
	h.killProcessGroup(wrapperPID, syscall.SIGKILL)
	h.killProcessGroup(childPID, syscall.SIGKILL)

	second := h.startDaemon("daemon-2")
	h.waitRun(crashTaskID, crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" &&
			runInt(run, "process_pid") > 0 &&
			runInt(run, "process_pid") != wrapperPID &&
			runInt(run, "attempt_count") == 2
	})
	second.stop()
}

func TestDeadRunnerMarkedInterruptedOnNextPoll(t *testing.T) {
	h := newHarness(t, "dead-runner-interrupted")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "hold",
		StallTimeoutMS: 5000,
		MaxAttempts:    1,
	})
	h.createRunnableTask("dead runner frees capacity")

	daemon := h.startDaemon("daemon")
	run := h.waitRun(crashTaskID, crashRunnerWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") > 0
	})
	h.killProcessGroup(runInt(run, "process_pid"), syscall.SIGKILL)

	h.waitRun(crashTaskID, crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "parked_no_progress" && runInt(run, "process_pid") == 0
	})
	status := h.automationStatus()
	if got := intFromPath(status, "status", "active_runs"); got != 0 {
		t.Fatalf("dead runner left active capacity occupied: active_runs=%d", got)
	}
	daemon.stop()
}

func TestNeverStartedRunnerKilledAtFirstEventDeadline(t *testing.T) {
	h := newHarness(t, "stdin-wedge-first-event")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "wedge",
		StallTimeoutMS: 300,
		MaxAttempts:    1,
	})
	h.createRunnableTask("stdin wedge gets killed")

	daemon := h.startDaemon("daemon")
	h.waitRun(crashTaskID, crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "parked_no_progress" &&
			runString(run, "attempt_outcome") == "blocked" &&
			runInt(run, "process_pid") == 0
	})
	run := h.latestRun(crashTaskID)
	if !strings.Contains(strings.ToLower(runString(run, "last_error")), "never started") {
		t.Fatalf("first-event deadline should preserve a never-started error, got %q", runString(run, "last_error"))
	}
	daemon.stop()
}

func TestSecondDaemonStartExitsLoudly(t *testing.T) {
	h := newHarness(t, "double-daemon")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "hold",
		StallTimeoutMS: 5000,
		MaxAttempts:    1,
	})
	h.createRunnableTask("double daemon guard")

	first := h.startDaemon("daemon-1")
	h.waitForAutomationStatus(crashRunnerWait)

	second := h.startDaemon("daemon-2")
	err, output, exited := second.waitWithin(1500 * time.Millisecond)
	if !exited {
		second.kill(syscall.SIGKILL)
		t.Fatalf("second daemon kept running; D6 requires a loud single-instance exit")
	}
	if err == nil {
		t.Fatalf("second daemon exited successfully; D6 requires a nonzero loud exit. output:\n%s", output)
	}
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "daemon") || !strings.Contains(lower, "pid") {
		t.Fatalf("second daemon error should name the incumbent daemon pid; output:\n%s", output)
	}
	if !first.alive() {
		t.Fatalf("first daemon was disturbed by the second start")
	}
	first.stop()
}

func TestRetryCapProducesTerminalRun(t *testing.T) {
	h := newHarness(t, "retry-cap-terminal")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:           "fail",
		ExitCode:       42,
		StallTimeoutMS: 5000,
		MaxAttempts:    2,
		BackoffMS:      []int{1, 1},
	})
	h.createRunnableTask("retry cap terminal flag")

	daemon := h.startDaemon("daemon")
	h.waitRun(crashTaskID, crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "parked_no_progress" && runInt(run, "attempt_count") == 2
	})
	run := h.latestRun(crashTaskID)
	if !strings.Contains(runString(run, "last_error"), "runner exited with code 42") {
		t.Fatalf("final runner error was not preserved: %q", runString(run, "last_error"))
	}
	if runString(run, "attempt_outcome") != "blocked" {
		t.Fatalf("expected blocked attempt outcome after retry cap, got %q", runString(run, "attempt_outcome"))
	}
	terminal, ok := runBool(run, "terminal")
	if !ok || !terminal {
		t.Fatalf("retry cap should set existing_run.terminal=true; run=%s", prettyJSON(run))
	}
	status := h.automationStatus()
	if got := intFromPath(status, "status", "active_runs"); got != 0 {
		t.Fatalf("terminal retry cap left active capacity occupied: active_runs=%d", got)
	}
	daemon.stop()
}

type fakeRunnerConfig struct {
	Mode           string
	RunnerKind     string
	ReleaseFile    string
	CompleteStatus string
	ExitCode       int
	HoldTimeout    time.Duration
	StallTimeoutMS int
	MaxAttempts    int
	BackoffMS      []int
}

type harness struct {
	t          *testing.T
	repoRoot   string
	tuskerBin  string
	fakeRunner string
	tempRoot   string
	stateRoot  string
	repoDir    string
	vaultDir   string
}

func newHarness(t *testing.T, name string) *harness {
	t.Helper()
	tuskerBin, fakeRunner := e2eBinaries(t)
	tempRoot, err := os.MkdirTemp(shortTempParent(), "tusker-crash-"+sanitizeName(name)+"-")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t:          t,
		repoRoot:   repoRoot(t),
		tuskerBin:  tuskerBin,
		fakeRunner: fakeRunner,
		tempRoot:   tempRoot,
		stateRoot:  filepath.Join(tempRoot, "state"),
		repoDir:    filepath.Join(tempRoot, "repo"),
	}
	t.Cleanup(func() {
		h.reapFixtureProcesses()
		_ = os.RemoveAll(tempRoot)
	})
	h.vaultDir = filepath.Join(h.repoDir, ".tusker")
	h.mustMkdir(h.repoDir)
	h.cliOK(h.repoDir, "init", "--yes", "--vault", h.vaultDir, "--quiet")
	h.disableReviewer()
	h.cliOK(h.repoDir, "new", "epic", "--vault", h.vaultDir, "--acronym", "APP", "--title", "Crash Recovery", "--summary", "Crash recovery e2e fixtures.", "--v7", "true", "--quiet")
	return h
}

func shortTempParent() string {
	if st, err := os.Stat("/tmp"); err == nil && st.IsDir() {
		return "/tmp"
	}
	return os.TempDir()
}

func e2eBinaries(t *testing.T) (string, string) {
	t.Helper()
	builtE2EBinaries.once.Do(func() {
		root := repoRoot(t)
		dir, err := os.MkdirTemp("", "tusker-crash-e2e-bin-")
		if err != nil {
			builtE2EBinaries.err = err
			return
		}
		builtE2EBinaries.dir = dir
		tuskerBin := filepath.Join(dir, "tusker")
		fakeRunner := filepath.Join(dir, "fake-runner")
		if err := runBuild(root, tuskerBin, "./cmd/tusker"); err != nil {
			builtE2EBinaries.err = err
			return
		}
		if err := runBuild(root, fakeRunner, "./e2e/crashrecovery/fixtures/fake-runner"); err != nil {
			builtE2EBinaries.err = err
			return
		}
		builtE2EBinaries.tusker = tuskerBin
		builtE2EBinaries.fakeRunner = fakeRunner
	})
	if builtE2EBinaries.err != nil {
		t.Fatalf("build e2e binaries: %v", builtE2EBinaries.err)
	}
	return builtE2EBinaries.tusker, builtE2EBinaries.fakeRunner
}

func runBuild(root, out, pkg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool(), "build", "-o", out, pkg)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout building %s", pkg)
	}
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", strings.Join(cmd.Args, " "), err, output)
	}
	return nil
}

func goTool() string {
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	if goroot := runtime.GOROOT(); goroot != "" {
		candidate := filepath.Join(goroot, "bin", "go")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "go"
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	return root
}

func (h *harness) configureFakeRunner(cfg fakeRunnerConfig) {
	h.t.Helper()
	if cfg.Mode == "" {
		cfg.Mode = "hold-success"
	}
	if cfg.StallTimeoutMS <= 0 {
		cfg.StallTimeoutMS = 5000
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.RunnerKind == "" {
		cfg.RunnerKind = "codex"
	}
	if len(cfg.BackoffMS) == 0 {
		cfg.BackoffMS = []int{1}
	}
	readyFile := filepath.Join(h.tempRoot, "runner-ready")
	pidFile := filepath.Join(h.tempRoot, "runner.pid")
	parts := []string{
		shellQuote(h.fakeRunner),
		"--mode", shellQuote(cfg.Mode),
		"--ready-file", shellQuote(readyFile),
		"--pid-file", shellQuote(pidFile),
		"--tusker-bin", shellQuote(h.tuskerBin),
	}
	if cfg.ReleaseFile != "" {
		parts = append(parts, "--release-file", shellQuote(cfg.ReleaseFile))
	}
	if cfg.CompleteStatus != "" {
		parts = append(parts, "--complete-status", shellQuote(cfg.CompleteStatus))
	}
	if cfg.ExitCode != 0 {
		parts = append(parts, "--exit-code", strconv.Itoa(cfg.ExitCode))
	}
	if cfg.HoldTimeout > 0 {
		parts = append(parts, "--hold-timeout", cfg.HoldTimeout.String())
	}
	command := strings.Join(parts, " ")
	config := fmt.Sprintf(`schema: tusker.config/v1
project_id: crash-recovery-e2e

storage:
  root: .tusker
  generated_root: .tusker/_generated
  evidence_root: .tusker/evidence
  events_root: .tusker/events
  attempts_root: .tusker/attempts

runtime:
  lease_backend: local
  lease_ttl_minutes: 120

automation:
  enabled: true
  trigger_states: [ready, rework]
  default_runner: codex
  enabled_runners: [codex]
  workspace:
    strategy: copy
    root: workspaces
  concurrency:
    max_active_runs: 1
    max_active_runs_per_project: 1
    max_concurrent_by_state: {}
  runners:
    codex:
      kind: %s
      command: >-
        %s
      approval_policy: never
      thread_sandbox: danger-full-access
      turn_sandbox_policy: danger-full-access
      turn_timeout_ms: 1000
      read_timeout_ms: 100
      stall_timeout_ms: %d
      max_turns: 1
`, cfg.RunnerKind, command, cfg.StallTimeoutMS)
	h.writeFile(filepath.Join(h.repoDir, "tusker.yaml"), config)

	workflow := h.readFile(filepath.Join(h.vaultDir, "WORKFLOW.md"))
	workflow = replaceYAMLScalarUnder(workflow, "runtime:", "  poll_interval_ms:", "  poll_interval_ms: 100")
	workflow = replaceYAMLScalarUnder(workflow, "runtime:", "  max_active_runs_per_project:", "  max_active_runs_per_project: 1")
	workflow = replaceYAMLScalarUnder(workflow, "serve:", "    enabled:", "    enabled: false")
	workflow = replaceYAMLScalarUnder(workflow, "retry:", "  max_attempts:", fmt.Sprintf("  max_attempts: %d", cfg.MaxAttempts))
	workflow = replaceYAMLListUnder(workflow, "retry:", "  backoff_ms:", cfg.BackoffMS)
	h.writeFile(filepath.Join(h.vaultDir, "WORKFLOW.md"), workflow)

	h.cliOK(h.repoDir, "projects", "add", "--repo", h.repoDir, "--vault", h.vaultDir, "--json")
	h.cliOK(h.repoDir, "daemon", "limits", "--max-active-runs", "1", "--json")
}

func (h *harness) createRunnableTask(title string) {
	h.t.Helper()
	h.cliOK(h.repoDir, "new", "task",
		"--vault", h.vaultDir,
		"--epic", "APP",
		"--title", title,
		"--risk", "low",
		"--priority", "p0",
		"--status", "ready",
		"--readiness", "ready",
		"--next-owner", "agent:codex",
		"--proof-mode", "inline",
		"--proof-required", "focused_test",
		"--force-ready",
		"--v7",
		"--quiet",
	)
	taskPath := filepath.Join(h.vaultDir, "work", "tasks", crashTaskID+".md")
	body := h.readFile(taskPath)
	body = replaceSection(body, "## Acceptance", strings.TrimSpace(`| ID | Outcome | Proof |
|---|---|---|
| A1 | The fake runner reaches the scenario-specific terminal behavior. | E2E harness assertion |`))
	body = replaceSection(body, "## Verification", strings.TrimSpace(`| Covers | Check | Result | Notes |
|---|---|---|---|
| A1 | go test ./e2e/crashrecovery | pending | Crash-recovery e2e scenario observes daemon state through the public CLI. |`))
	h.writeFile(taskPath, body)
	h.cliOK(h.repoDir, "reconcile", "--vault", h.vaultDir, "--local", "--quiet")
}

func (h *harness) disableReviewer() {
	h.t.Helper()
	path := filepath.Join(h.vaultDir, "WORKFLOW.md")
	text := h.readFile(path)
	text = replaceYAMLScalarUnder(text, "reviewer:", "  enabled:", "  enabled: false")
	h.writeFile(path, text)
}

func (h *harness) cliOK(dir string, args ...string) []byte {
	h.t.Helper()
	out, err := h.cli(dir, 20*time.Second, args...)
	if err != nil {
		h.t.Fatalf("tusker %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (h *harness) cli(dir string, timeout time.Duration, args ...string) ([]byte, error) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.tuskerBin, args...)
	cmd.Dir = dir
	cmd.Env = h.env()
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, fmt.Errorf("timeout")
	}
	return output, err
}

func (h *harness) automationStatus() map[string]any {
	h.t.Helper()
	out := h.cliOK(h.repoDir, "automation", "status", "--json")
	return parseJSON(h.t, out)
}

func (h *harness) automationQueue() map[string]any {
	h.t.Helper()
	out := h.cliOK(h.repoDir, "automation", "queue", "--repo", h.repoDir, "--json")
	return parseJSON(h.t, out)
}

func (h *harness) latestRun(taskID string) map[string]any {
	h.t.Helper()
	payload := h.automationQueue()
	task := findQueueTask(h.t, payload, taskID)
	run, _ := task["existing_run"].(map[string]any)
	if run == nil {
		h.t.Fatalf("queue task %s has no existing_run: %s", taskID, prettyJSON(task))
	}
	return run
}

func (h *harness) waitRun(taskID string, timeout time.Duration, accept func(map[string]any) bool) map[string]any {
	h.t.Helper()
	var last map[string]any
	eventually(h.t, timeout, 100*time.Millisecond, func() (bool, string) {
		payload := h.automationQueue()
		task := findQueueTask(h.t, payload, taskID)
		run, _ := task["existing_run"].(map[string]any)
		if run != nil {
			last = run
			if accept(run) {
				return true, ""
			}
			return false, prettyJSON(run)
		}
		return false, "existing_run is null for " + taskID
	})
	return last
}

func (h *harness) waitForAutomationStatus(timeout time.Duration) {
	h.t.Helper()
	eventually(h.t, timeout, 100*time.Millisecond, func() (bool, string) {
		payload := h.automationStatus()
		if status, ok := payload["status"].(map[string]any); ok && projectHasPoll(status) {
			return true, ""
		}
		return false, prettyJSON(payload)
	})
}

func projectHasPoll(status map[string]any) bool {
	for _, item := range sliceAt(status, "projects") {
		project, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if runString(project, "last_poll_at") != "" {
			return true
		}
	}
	return false
}

func (h *harness) startDaemon(name string) *daemonProcess {
	h.t.Helper()
	logPath := filepath.Join(h.tempRoot, name+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command(h.tuskerBin, "daemon", "run")
	cmd.Dir = h.repoDir
	cmd.Env = h.env()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		h.t.Fatalf("start daemon: %v", err)
	}
	proc := &daemonProcess{t: h.t, cmd: cmd, done: make(chan error, 1), logPath: logPath, logFile: logFile}
	go func() {
		proc.done <- cmd.Wait()
		_ = logFile.Close()
	}()
	h.t.Cleanup(func() {
		if h.t.Failed() {
			if output := strings.TrimSpace(proc.output()); output != "" {
				h.t.Logf("%s output:\n%s", name, output)
			}
			h.logRunnerArtifacts()
		}
		proc.stop()
	})
	return proc
}

func (h *harness) env() []string {
	return append(os.Environ(),
		"TUSKER_STATE_ROOT="+h.stateRoot,
		"TUSKER_CONFIG="+filepath.Join(h.tempRoot, "config", "tusker", "config.yaml"),
		"TUSKER_WRAPPER_HEARTBEAT_MS=100",
		"TUSKER_WRAPPER_STOP_TIMEOUT_MS=1000",
	)
}

func (h *harness) waitRunnerPID(timeout time.Duration) int {
	h.t.Helper()
	pidPath := filepath.Join(h.tempRoot, "runner.pid")
	var pid int
	eventually(h.t, timeout, 100*time.Millisecond, func() (bool, string) {
		raw, err := os.ReadFile(pidPath)
		if err != nil {
			return false, err.Error()
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || parsed <= 0 {
			return false, strings.TrimSpace(string(raw))
		}
		pid = parsed
		return true, ""
	})
	return pid
}

func (h *harness) touch(path string) {
	h.t.Helper()
	h.mustMkdir(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) killProcessGroup(pid int, sig syscall.Signal) {
	h.t.Helper()
	if pid <= 0 {
		h.t.Fatalf("invalid process pid %d", pid)
	}
	killProcessGroup(pid, sig)
}

func (h *harness) reapFixtureProcesses() {
	h.t.Helper()
	reapFixtureProcesses(h.tempRoot)
	if survivors := waitForNoFixtureProcesses(h.tempRoot, 3*time.Second); len(survivors) > 0 {
		h.t.Fatalf("surviving fixture processes:\n%s", summarizeFixtureProcesses(survivors))
	}
}

func (h *harness) readFile(path string) string {
	h.t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatal(err)
	}
	return string(raw)
}

func (h *harness) writeFile(path, content string) {
	h.t.Helper()
	h.mustMkdir(filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) logRunnerArtifacts() {
	h.t.Helper()
	root := filepath.Join(h.stateRoot, "runs")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		switch {
		case strings.HasSuffix(path, ".raw.log"), strings.HasSuffix(path, ".status.json"), strings.HasSuffix(path, ".events.jsonl"):
			if raw, readErr := os.ReadFile(path); readErr == nil && len(raw) > 0 {
				h.t.Logf("%s:\n%s", path, strings.TrimSpace(string(raw)))
			}
		}
		return nil
	})
}

func (h *harness) mustMkdir(path string) {
	h.t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		h.t.Fatal(err)
	}
}

type daemonProcess struct {
	t       *testing.T
	cmd     *exec.Cmd
	done    chan error
	logPath string
	logFile *os.File
}

func (p *daemonProcess) waitWithin(timeout time.Duration) (error, string, bool) {
	p.t.Helper()
	select {
	case err := <-p.done:
		return err, p.output(), true
	case <-time.After(timeout):
		return nil, p.output(), false
	}
}

func (p *daemonProcess) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if !p.alive() {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		p.kill(syscall.SIGKILL)
	}
}

func (p *daemonProcess) kill(sig syscall.Signal) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(sig)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
}

func (p *daemonProcess) alive() bool {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	return processAlive(p.cmd.Process.Pid)
}

func (p *daemonProcess) output() string {
	raw, _ := os.ReadFile(p.logPath)
	return string(raw)
}

func parseJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, raw)
	}
	return payload
}

func findQueueTask(t *testing.T, payload map[string]any, taskID string) map[string]any {
	t.Helper()
	queue := mapAtPath(t, payload, "queue")
	for _, bucket := range []string{"eligible", "blocked"} {
		for _, item := range sliceAt(queue, bucket) {
			task, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if runString(task, "id") == taskID || runString(task, "record_id") == taskID {
				return task
			}
		}
	}
	t.Fatalf("queue task %s not found: %s", taskID, prettyJSON(payload))
	return nil
}

func mapAtPath(t *testing.T, value map[string]any, path ...string) map[string]any {
	t.Helper()
	current := value
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("missing JSON object %s in %s", strings.Join(path, "."), prettyJSON(value))
		}
		current = next
	}
	return current
}

func sliceAt(value map[string]any, key string) []any {
	items, _ := value[key].([]any)
	return items
}

func intFromPath(value map[string]any, path ...string) int {
	current := any(value)
	for _, key := range path {
		m, ok := current.(map[string]any)
		if !ok {
			return 0
		}
		current = m[key]
	}
	return anyInt(current)
}

func runString(run map[string]any, key string) string {
	value, _ := run[key].(string)
	return value
}

func runInt(run map[string]any, key string) int {
	return anyInt(run[key])
}

func runBool(run map[string]any, key string) (bool, bool) {
	value, ok := run[key]
	if !ok {
		return false, false
	}
	parsed, ok := value.(bool)
	return parsed, ok
}

func anyInt(value any) int {
	switch v := value.(type) {
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func eventually(t *testing.T, timeout, tick time.Duration, fn func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		ok, detail := fn()
		if ok {
			return
		}
		if detail != "" {
			last = detail
		}
		time.Sleep(tick)
	}
	t.Fatalf("condition not met within %s; last=%s", timeout, last)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

type fixtureProcess struct {
	PID     int
	PGID    int
	Command string
}

func fixtureProcesses(marker string) []fixtureProcess {
	out, err := exec.Command("ps", "-axo", "pid=,pgid=,command=").Output()
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var procs []fixtureProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		pgid, pgidErr := strconv.Atoi(fields[1])
		if pidErr != nil || pgidErr != nil || pid == self {
			continue
		}
		command := strings.Join(fields[2:], " ")
		if isFixtureProcessCommand(command, marker) {
			procs = append(procs, fixtureProcess{PID: pid, PGID: pgid, Command: command})
		}
	}
	return procs
}

func isFixtureProcessCommand(command, marker string) bool {
	if !strings.Contains(command, "tusker-crash-") {
		return false
	}
	if marker != "" && !strings.Contains(command, marker) {
		return false
	}
	return strings.Contains(command, "fake-runner") || strings.Contains(command, "runner-wrapper")
}

func reapFixtureProcesses(marker string) {
	procs := fixtureProcesses(marker)
	if len(procs) == 0 {
		return
	}
	signalFixtureProcesses(procs, syscall.SIGTERM)
	survivors := waitForNoFixtureProcesses(marker, time.Second)
	if len(survivors) == 0 {
		return
	}
	signalFixtureProcesses(survivors, syscall.SIGKILL)
	_ = waitForNoFixtureProcesses(marker, 2*time.Second)
}

func signalFixtureProcesses(procs []fixtureProcess, sig syscall.Signal) {
	selfGroup := syscall.Getpgrp()
	groups := map[int]bool{}
	pids := map[int]bool{}
	for _, proc := range procs {
		if proc.PGID > 0 && proc.PGID != selfGroup {
			groups[proc.PGID] = true
		}
		if proc.PID > 0 && proc.PID != os.Getpid() {
			pids[proc.PID] = true
		}
	}
	for pgid := range groups {
		_ = syscall.Kill(-pgid, sig)
	}
	for pid := range pids {
		_ = syscall.Kill(pid, sig)
	}
}

func waitForNoFixtureProcesses(marker string, timeout time.Duration) []fixtureProcess {
	deadline := time.Now().Add(timeout)
	var procs []fixtureProcess
	for time.Now().Before(deadline) {
		procs = fixtureProcesses(marker)
		if len(procs) == 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fixtureProcesses(marker)
}

func assertNoFixtureProcesses(t *testing.T, marker string) {
	t.Helper()
	if survivors := fixtureProcesses(marker); len(survivors) > 0 {
		t.Fatalf("surviving fixture processes:\n%s", summarizeFixtureProcesses(survivors))
	}
}

func summarizeFixtureProcesses(procs []fixtureProcess) string {
	const maxProcesses = 8
	var b strings.Builder
	for i, proc := range procs {
		if i >= maxProcesses {
			fmt.Fprintf(&b, "... and %d more\n", len(procs)-i)
			break
		}
		fmt.Fprintf(&b, "pid=%d pgid=%d cmd=%s\n", proc.PID, proc.PGID, trimProcessCommand(proc.Command))
	}
	return strings.TrimRight(b.String(), "\n")
}

func trimProcessCommand(command string) string {
	const maxCommand = 240
	if len(command) <= maxCommand {
		return command
	}
	return command[:maxCommand] + "..."
}

func killProcessGroup(pid int, sig syscall.Signal) {
	if pid <= 0 {
		return
	}
	selfGroup := syscall.Getpgrp()
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid > 0 && pgid != selfGroup {
		_ = syscall.Kill(-pgid, sig)
	}
	_ = syscall.Kill(pid, sig)
}

func cleanupE2EBinaries() {
	if builtE2EBinaries.dir != "" {
		_ = os.RemoveAll(builtE2EBinaries.dir)
	}
}

func prettyJSON(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(raw)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func sanitizeName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func replaceSection(doc, heading, replacement string) string {
	start := strings.Index(doc, heading)
	if start < 0 {
		return doc
	}
	contentStart := start + len(heading)
	for contentStart < len(doc) && (doc[contentStart] == '\n' || doc[contentStart] == '\r') {
		contentStart++
	}
	next := len(doc)
	if idx := strings.Index(doc[contentStart:], "\n## "); idx >= 0 {
		next = contentStart + idx
	}
	return strings.TrimRight(doc[:contentStart], "\n") + "\n\n" + strings.TrimSpace(replacement) + "\n\n" + strings.TrimLeft(doc[next:], "\n")
}

func replaceYAMLScalarUnder(doc, parent, key, replacement string) string {
	lines := strings.Split(doc, "\n")
	inParent := false
	parentIndent := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == strings.TrimSpace(parent) {
			inParent = true
			parentIndent = leadingSpaces(line)
			continue
		}
		if inParent && trimmed != "" && leadingSpaces(line) <= parentIndent {
			inParent = false
		}
		if inParent && strings.HasPrefix(trimmed, strings.TrimSpace(key)) {
			lines[i] = strings.Repeat(" ", leadingSpaces(line)) + strings.TrimSpace(replacement)
			break
		}
	}
	return strings.Join(lines, "\n")
}

func replaceYAMLListUnder(doc, parent, key string, values []int) string {
	lines := strings.Split(doc, "\n")
	out := make([]string, 0, len(lines)+len(values))
	inParent := false
	parentIndent := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == strings.TrimSpace(parent) {
			inParent = true
			parentIndent = leadingSpaces(line)
			out = append(out, line)
			continue
		}
		if inParent && trimmed != "" && leadingSpaces(line) <= parentIndent {
			inParent = false
		}
		if inParent && strings.HasPrefix(trimmed, strings.TrimSpace(key)) {
			keyIndent := leadingSpaces(line)
			out = append(out, strings.Repeat(" ", keyIndent)+strings.TrimSpace(key))
			for _, value := range values {
				out = append(out, fmt.Sprintf("%s- %d", strings.Repeat(" ", keyIndent+4), value))
			}
			for i+1 < len(lines) && leadingSpaces(lines[i+1]) > keyIndent && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- ") {
				i++
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func leadingSpaces(line string) int {
	for i, r := range line {
		if r != ' ' {
			return i
		}
	}
	return len(line)
}
