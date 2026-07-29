//go:build !windows

package crashrecovery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
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
	crashRunWait    = time.Minute
	crashRunnerWait = 15 * time.Second
)

var builtE2EBinaries struct {
	once       sync.Once
	dir        string
	tusker     string
	fakeRunner string
	err        error
}

func TestHarnessEnvScrubsAgentSessionMarkers(t *testing.T) {
	t.Setenv("CODEX_SHELL", "1")
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-parent")
	h := &harness{t: t, tempRoot: t.TempDir(), stateRoot: t.TempDir()}
	for _, entry := range h.env() {
		if strings.HasPrefix(entry, "CODEX_SHELL=") || strings.HasPrefix(entry, "TUSKER_ATTEMPT_ID=") {
			t.Fatalf("child daemon inherited agent-session marker %q", entry)
		}
	}
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
			runString(run, "attempt_outcome") == "waiting_for_review" &&
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

func TestArmedWaveCrashRestartConverges(t *testing.T) {
	h := newHarness(t, "armed-wave-crash-restart")
	releasePattern := filepath.Join(h.tempRoot, "release-{task}")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode: "hold-success", RunnerKind: "codex_exec", ReleaseFile: releasePattern,
		CompleteStatus: "review", StallTimeoutMS: 5000, MaxAttempts: 2,
	})
	h.createRunnableTaskID("APP-T-0001", "armed root", "")
	h.createRunnableTaskID("APP-T-0002", "armed next frontier", "APP-T-0001:soft")
	h.installWaveCompatibleOperatorSkill()
	h.gitOK("init", "-b", "main")
	h.gitOK("config", "user.email", "crash@example.com")
	h.gitOK("config", "user.name", "Crash Recovery")
	h.gitOK("add", "-A")
	h.gitOK("commit", "-m", "armed wave fixture")
	h.cliOK(h.repoDir, "wave", "create", "Durable crash wave", "APP-T-0001", "APP-T-0002", "--vault", h.vaultDir, "--quiet")

	first := h.startDaemon("armed-daemon-1")
	h.waitForAutomationStatus(crashRunWait)
	arm := parseJSON(t, h.cliOK(h.repoDir, "wave", "arm", "W-0001", "--vault", h.vaultDir, "--by", "human:e2e", "--json"))
	armWave := mapAtPath(t, arm, "preflight")
	armFingerprint := runString(armWave, "fingerprint")
	if armFingerprint == "" {
		t.Fatalf("arm did not persist a material fingerprint: %s", prettyJSON(arm))
	}

	root := h.waitRun("APP-T-0001", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "attempt_count") == 1
	})
	rootChildPID := h.waitRunnerPID(crashRunnerWait)
	rootPID, rootGeneration := runInt(root, "process_pid"), runInt(root, "lease_generation")
	first.kill(syscall.SIGKILL)
	second := h.startDaemon("armed-daemon-2")
	h.waitRun("APP-T-0001", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") == rootPID &&
			runInt(run, "lease_generation") == rootGeneration && runInt(run, "attempt_count") == 1
	})

	h.touch(filepath.Join(h.tempRoot, "release-APP-T-0001"))
	h.waitRun("APP-T-0001", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "released" &&
			runString(run, "attempt_outcome") == "waiting_for_review" &&
			runInt(run, "attempt_count") == 1
	})
	// A copy workspace is a safe manual-mode fallback, not a mergeable landing
	// source. Model the explicit human checkpoint that records observed proof
	// and review readiness before the next frontier.
	h.cliOK(h.repoDir,
		"verify", "add", "APP-T-0001",
		"--vault", h.vaultDir,
		"--covers", "A1",
		"--check", "go test ./e2e/crashrecovery",
		"--result", "pass",
		"--note", "crash-recovery harness observed the root worker handoff",
		"--by", "human:e2e",
		"--local",
		"--quiet",
	)
	h.cliOK(h.repoDir,
		"status",
		"--id", "APP-T-0001",
		"--status", "review",
		"--vault", h.vaultDir,
		"--actor", "human:e2e",
		"--reason", "manual crash-recovery review checkpoint",
		"--local",
		"--quiet",
	)
	next := h.waitRun("APP-T-0002", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "attempt_count") == 1
	})
	nextPID := runInt(next, "process_pid")
	if nextPID <= 0 {
		t.Fatalf("next frontier did not receive exactly one claim: %s", prettyJSON(next))
	}
	nextChildPID := h.waitRunnerPIDChange(rootChildPID, crashRunnerWait)
	second.kill(syscall.SIGKILL)
	h.killProcessGroup(nextPID, syscall.SIGKILL)
	eventually(t, 3*time.Second, 25*time.Millisecond, func() (bool, string) {
		if processAlive(nextChildPID) {
			return false, fmt.Sprintf("runner child %d survived recorded wrapper group kill", nextChildPID)
		}
		return true, ""
	})
	third := h.startDaemon("armed-daemon-3")
	reclaimed := h.waitRun("APP-T-0002", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "attempt_count") == 2 && runInt(run, "process_pid") != nextPID
	})
	reclaimGeneration := runInt(reclaimed, "lease_generation")
	time.Sleep(350 * time.Millisecond)
	stable := h.latestRun("APP-T-0002")
	if runInt(stable, "attempt_count") != 2 || runInt(stable, "lease_generation") != reclaimGeneration {
		t.Fatalf("restart duplicated the reclaimed claim: %s", prettyJSON(stable))
	}

	wave := parseJSON(t, h.cliOK(h.repoDir, "wave", "show", "W-0001", "--vault", h.vaultDir, "--json"))
	authorization := mapAtPath(t, mapAtPath(t, wave, "wave"), "authorization")
	if runString(authorization, "state") != "armed" || runString(authorization, "authorizedFingerprint") != armFingerprint || runString(authorization, "actor") != "human:e2e" {
		t.Fatalf("restart lost or repeated wave authorization: %s", prettyJSON(wave))
	}
	h.touch(filepath.Join(h.tempRoot, "release-APP-T-0002"))
	h.waitRun("APP-T-0002", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "released" && runInt(run, "attempt_count") == 2
	})
	third.stop()
}

func TestSpecToWaveDelivery(t *testing.T) {
	h := newHarness(t, "spec-to-wave-delivery")
	h.configureFakeRunner(fakeRunnerConfig{Delivery: true, RunnerKind: "codex_exec", Reviewer: true, MaxActive: 3, WorkspaceStrategy: "worktree", StallTimeoutMS: 10000, MaxAttempts: 2})
	h.installWaveCompatibleOperatorSkill()
	h.writeFile(filepath.Join(h.repoDir, "docs", "specs", "delivery.md"), "# Disposable delivery\n\nApproved fixture spec.\n\n## Work streams\n")
	h.gitOK("init", "-b", "main")
	h.gitOK("config", "user.email", "delivery@example.com")
	h.gitOK("config", "user.name", "Delivery Fixture")
	h.gitOK("add", "-A")
	h.gitOK("commit", "-m", "fixture baseline")
	planPath := filepath.Join(h.tempRoot, "delivery-plan.yaml")
	h.writeFile(planPath, specToWaveDeliveryPlan())
	imported := parseJSON(t, h.cliOK(h.repoDir, "delivery", "import", "--plan", planPath, "--wave", "Disposable delivery", "--vault", h.vaultDir, "--json"))
	delivery := mapAtPath(t, imported, "delivery")
	mapping, _ := delivery["taskMapping"].(map[string]any)
	if intFromPath(delivery, "expectedConcurrency") != 3 || len(mapping) != 7 || len(sliceAt(delivery, "frontiers")) < 4 {
		t.Fatalf("import did not preserve the seven-task mixed DAG: %s", prettyJSON(imported))
	}
	h.gitOK("branch", "-f", "integration/W-0001", "HEAD")
	h.touch(filepath.Join(h.tempRoot, "delivery-control", "hold-APP-T-0001"))
	daemon := h.startDaemon("delivery-daemon")
	h.waitForAutomationStatus(crashRunWait)
	preflight := parseJSON(t, h.cliOK(h.repoDir, "wave", "preflight", "W-0001", "--vault", h.vaultDir, "--json"))
	if ok, _ := preflight["ok"].(bool); !ok {
		t.Fatalf("preflight failed: %s", prettyJSON(preflight))
	}
	arm := parseJSON(t, h.cliOK(h.repoDir, "wave", "arm", "W-0001", "--vault", h.vaultDir, "--by", "human:e2e", "--json"))
	if runString(mapAtPath(t, arm, "preflight"), "fingerprint") == "" {
		t.Fatalf("arm omitted fingerprint: %s", prettyJSON(arm))
	}
	root := h.waitRun("APP-T-0001", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") > 0
	})
	if runString(root, "worker_policy_fingerprint") == "" || runString(root, "execute_policy_fingerprint") == "" {
		t.Fatalf("authoritative execute dispatch must persist both exact policy fingerprints: %s", prettyJSON(root))
	}
	rootPID, rootGeneration, rootAttempts := runInt(root, "process_pid"), runInt(root, "lease_generation"), runInt(root, "attempt_count")
	daemon.kill(syscall.SIGKILL)
	daemon = h.startDaemon("delivery-daemon-restarted")
	adopted := h.waitRun("APP-T-0001", crashRunWait, func(run map[string]any) bool {
		return runString(run, "lease_state") == "running" && runInt(run, "process_pid") == rootPID && runInt(run, "lease_generation") == rootGeneration && runInt(run, "attempt_count") == rootAttempts
	})
	worktreeVault := filepath.Join(runString(adopted, "workspace_path"), ".tusker")
	attemptDir := filepath.Join(worktreeVault, "attempts", "APP-T-0001")
	attemptEntries, err := os.ReadDir(attemptDir)
	if err != nil {
		t.Fatalf("read adopted V7 attempts: %v", err)
	}
	if len(attemptEntries) != 1 || attemptEntries[0].Name() != "APP-T-0001-A-0001.md" {
		t.Fatalf("SIGKILL adoption must preserve exactly one V7 attempt, got %#v", attemptEntries)
	}
	attemptBody := h.readFile(filepath.Join(attemptDir, attemptEntries[0].Name()))
	if !strings.Contains(attemptBody, "runtime_attempt_id:") || !strings.Contains(attemptBody, runString(adopted, "active_attempt_id")) {
		t.Fatalf("V7 attempt is not bound to adopted runtime attempt:\n%s", attemptBody)
	}
	h.touch(filepath.Join(h.tempRoot, "delivery-control", "release-APP-T-0001"))
	polls := 0
	lastQueue := ""
	eventually(t, 3*time.Minute, time.Second, func() (bool, string) {
		polls++
		out, err := h.cli(h.repoDir, 10*time.Second, "wave", "brief", "W-0001", "--vault", h.vaultDir, "--json")
		if err != nil {
			return false, string(out)
		}
		payload := parseJSON(t, out)
		brief := mapAtPath(t, payload, "brief")
		outcome := mapAtPath(t, brief, "outcome")
		fully, _ := outcome["fullyDrained"].(bool)
		if !fully {
			if polls%10 == 0 {
				lastQueue = prettyJSON(h.automationQueue())
			}
			return false, prettyJSON(brief) + "\nqueue:\n" + lastQueue
		}
		return true, ""
	})
	briefPayload := parseJSON(t, h.cliOK(h.repoDir, "wave", "brief", "W-0001", "--vault", h.vaultDir, "--json"))
	brief := mapAtPath(t, briefPayload, "brief")
	if runString(brief, "schema") != "tusker.wave-brief/v1" || len(sliceAt(brief, "seeIt")) != 7 || len(sliceAt(brief, "landed")) != 7 || len(sliceAt(brief, "documentation")) != 7 || len(sliceAt(brief, "humanAction")) != 0 {
		t.Fatalf("artifact-first brief is incomplete: %s", prettyJSON(brief))
	}
	// This fixture arms continuous staging only. Its completed artifacts must be
	// on the exact integration ref; moving main is deliberately a separate,
	// explicitly configured scheduled-promotion concern.
	for i := 1; i <= 7; i++ {
		id := fmt.Sprintf("APP-T-%04d", i)
		path := "integration/W-0001:docs/delivery/" + strings.ToLower(id) + ".md"
		if got := string(h.gitOK("show", path)); !strings.Contains(got, "# "+id+" delivery") {
			t.Fatalf("%s documentation is absent from the integration snapshot: %q", id, got)
		}
	}
	wave := parseJSON(t, h.cliOK(h.repoDir, "wave", "show", "W-0001", "--vault", h.vaultDir, "--json"))
	auth := mapAtPath(t, mapAtPath(t, wave, "wave"), "authorization")
	if runString(auth, "actor") != "human:e2e" || runString(auth, "state") != "armed" {
		t.Fatalf("one-arm authorization was not preserved: %s", prettyJSON(wave))
	}
	daemon.stop()
	h.writeFile(filepath.Join(h.repoDir, "docs", "specs", "delivery.md"), h.readFile(filepath.Join(h.repoDir, "docs", "specs", "delivery.md"))+"\nMaterial post-arm change.\n")
	staleWave := parseJSON(t, h.cliOK(h.repoDir, "wave", "show", "W-0001", "--vault", h.vaultDir, "--json"))
	if runString(mapAtPath(t, mapAtPath(t, staleWave, "wave"), "authorization"), "state") != "stale" {
		t.Fatalf("material spec change did not stale authorization: %s", prettyJSON(staleWave))
	}
	runSpecToWaveFailureContainment(t)
	runSpecToWaveCredentialContainment(t)
}

func runSpecToWaveFailureContainment(t *testing.T) {
	h := newSmallDeliveryWave(t, false)
	h.touch(filepath.Join(h.tempRoot, "delivery-control", "fail-APP-T-0002"))
	daemon := h.startDaemon("failure-daemon")
	h.waitForAutomationStatus(crashRunWait)
	h.cliOK(h.repoDir, "wave", "arm", "W-0001", "--vault", h.vaultDir, "--by", "human:e2e", "--json")
	eventually(t, 90*time.Second, 200*time.Millisecond, func() (bool, string) {
		failed := h.latestRun("APP-T-0002")
		briefRaw, err := h.cli(h.repoDir, 10*time.Second, "wave", "brief", "W-0001", "--vault", h.vaultDir, "--json")
		if err != nil {
			return false, string(briefRaw)
		}
		brief := mapAtPath(t, parseJSON(t, briefRaw), "brief")
		return runString(failed, "lease_state") == "parked_no_progress" && len(sliceAt(brief, "landed")) >= 2 && len(sliceAt(brief, "reworkParked")) == 1, prettyJSON(brief)
	})
	daemon.stop()
}

func runSpecToWaveCredentialContainment(t *testing.T) {
	h := newSmallDeliveryWave(t, true)
	daemon := h.startDaemon("credential-daemon")
	h.waitForAutomationStatus(crashRunWait)
	h.cliOK(h.repoDir, "wave", "arm", "W-0001", "--vault", h.vaultDir, "--by", "human:e2e", "--json")
	eventually(t, 90*time.Second, 200*time.Millisecond, func() (bool, string) {
		raw, err := h.cli(h.repoDir, 10*time.Second, "wave", "brief", "W-0001", "--vault", h.vaultDir, "--json")
		if err != nil {
			return false, string(raw)
		}
		brief := mapAtPath(t, parseJSON(t, raw), "brief")
		actions := sliceAt(brief, "humanAction")
		if len(actions) != 1 || len(sliceAt(brief, "landed")) < 2 {
			return false, prettyJSON(brief)
		}
		action, _ := actions[0].(map[string]any)
		return runString(action, "resumeId") == "APP-G-0001" && strings.Contains(runString(action, "action"), "fixture credential"), prettyJSON(brief)
	})
	daemon.stop()
}

func newSmallDeliveryWave(t *testing.T, credentialGate bool) *harness {
	h := newHarness(t, "spec-wave-small")
	h.configureFakeRunner(fakeRunnerConfig{Delivery: true, RunnerKind: "codex_exec", Reviewer: true, MaxActive: 2, WorkspaceStrategy: "worktree", StallTimeoutMS: 10000, MaxAttempts: 2})
	h.installWaveCompatibleOperatorSkill()
	h.writeFile(filepath.Join(h.repoDir, "docs", "specs", "delivery.md"), "# Containment delivery\n")
	h.writeFile(filepath.Join(h.repoDir, "artifacts", "delivery", "baseline.json"), "{}\n")
	h.createRunnableTaskID("APP-T-0001", "fixture root", "")
	h.createRunnableTaskID("APP-T-0002", "contained branch", "APP-T-0001:hard")
	h.createRunnableTaskID("APP-T-0003", "independent branch", "APP-T-0001:soft")
	h.setDeliveryFixtureVerificationContracts("APP-T-0001", "APP-T-0002", "APP-T-0003")
	if credentialGate {
		h.cliOK(h.repoDir, "new", "gate", "--vault", h.vaultDir, "--blocks", "APP-T-0002", "--kind", "auth", "--owner", "human:e2e", "--action", "Provide fixture credential.", "--verification", "Fixture credential probe succeeds.", "--why-agent-cannot", "Only the fixture account owner can provide this credential.", "--quiet")
	}
	h.gitOK("init", "-b", "main")
	h.gitOK("config", "user.email", "delivery@example.com")
	h.gitOK("config", "user.name", "Delivery Fixture")
	h.gitOK("add", "-A")
	h.gitOK("commit", "-m", "small delivery baseline")
	h.cliOK(h.repoDir, "wave", "create", "Containment wave", "APP-T-0001", "APP-T-0002", "APP-T-0003", "--vault", h.vaultDir, "--quiet")
	h.gitOK("add", "-A")
	h.gitOK("commit", "-m", "record containment wave")
	h.gitOK("branch", "-f", "integration/W-0001", "HEAD")
	return h
}

func (h *harness) setDeliveryFixtureVerificationContracts(taskIDs ...string) {
	h.t.Helper()
	for _, taskID := range taskIDs {
		path := filepath.Join(h.vaultDir, "work", "tasks", taskID+".md")
		body := h.readFile(path)
		artifact := "artifacts/delivery/" + strings.ToLower(taskID) + ".json"
		if taskID == "APP-T-0001" {
			artifact = strings.TrimSuffix(artifact, ".json") + ".svg"
		}
		body = replaceSection(body, "## Verification", "| Covers | Check | Result | Notes |\n|---|---|---|---|\n| A1 | command: test -s "+artifact+" | pending | Fake runner records the pre-authorized artifact check. |")
		h.writeFile(path, body)
	}
	h.cliOK(h.repoDir, "reconcile", "--vault", h.vaultDir, "--local", "--quiet")
}

func specToWaveDeliveryPlan() string {
	type task struct {
		key, title, kind, path string
		deps                   []string
	}
	tasks := []task{
		{key: "root", title: "Root schema", kind: "screenshot", path: "artifacts/delivery/app-t-0001.svg"},
		{key: "api", title: "API branch", kind: "benchmark", path: "artifacts/delivery/app-t-0002.json", deps: []string{"root:hard"}},
		{key: "migration", title: "Migration branch", kind: "trace", path: "artifacts/delivery/app-t-0003.json", deps: []string{"root:hard"}},
		{key: "ui", title: "UI branch", kind: "behavior_matrix", path: "artifacts/delivery/app-t-0004.json", deps: []string{"api:hard"}},
		{key: "client", title: "Client branch", kind: "reliability_summary", path: "artifacts/delivery/app-t-0005.json", deps: []string{"api:soft"}},
		{key: "backfill", title: "Backfill branch", kind: "security_summary", path: "artifacts/delivery/app-t-0006.json", deps: []string{"migration:hard"}},
		{key: "final", title: "Integrated delivery", kind: "diff_summary", path: "artifacts/delivery/app-t-0007.json", deps: []string{"ui:hard", "client:soft", "backfill:hard"}},
	}
	var b strings.Builder
	b.WriteString("schema: tusker.delivery-plan/v1\nscope: disposable-spec-to-wave\ntitle: Disposable delivery\nepic: APP\nspec_refs: [docs/specs/delivery.md]\nconcurrency: 3\ntasks:\n")
	for index, item := range tasks {
		fmt.Fprintf(&b, "  - source_key: %s\n    title: %s\n    outcome: %s is objectively delivered and documented.\n    acceptance:\n      - id: A1\n        outcome: %s has a committed artifact and focused proof.\n    verification:\n      - covers: A1\n        check: 'command: fixture delivery assertion'\n", item.key, item.title, item.title, item.title)
		if len(item.deps) > 0 {
			b.WriteString("    dependencies:\n")
			for _, dep := range item.deps {
				parts := strings.Split(dep, ":")
				fmt.Fprintf(&b, "      - task: %s\n        kind: %s\n", parts[0], parts[1])
			}
		}
		fmt.Fprintf(&b, "    artifact:\n      kind: %s\n      path: %s\n      summary: Acceptance-linked %s artifact.\n      acceptance_ids: [A1]\n    owned_paths: [%s, docs/delivery/%s.md]\n    knowledge_nodes: [docs/delivery/%s.md]\n    risk: medium\n    priority: p1\n    domains: [project]\n", item.kind, item.path, item.kind, item.path, strings.ToLower("APP-T-"+fmt.Sprintf("%04d", index+1)), strings.ToLower("APP-T-"+fmt.Sprintf("%04d", index+1)))
	}
	return b.String()
}

func (h *harness) installWaveCompatibleOperatorSkill() {
	h.t.Helper()
	for _, relative := range []string{"SKILL.md", filepath.Join("assets", "compatibility.yaml")} {
		raw, err := os.ReadFile(filepath.Join(h.repoRoot, "skills", "tusker", relative))
		if err != nil {
			h.t.Fatalf("read canonical operator skill %s: %v", relative, err)
		}
		h.writeFile(filepath.Join(h.repoDir, "skill", relative), string(raw))
	}
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

func TestCleanExitContinuationLoopParksAtCap(t *testing.T) {
	h := newHarness(t, "clean-exit-continuation-cap")
	h.configureFakeRunner(fakeRunnerConfig{
		Mode:                   "success",
		StallTimeoutMS:         5000,
		MaxAttempts:            5,
		BackoffMS:              []int{1, 1, 1},
		MaxContinuationRetries: 2,
	})
	h.createRunnableTask("clean exit loop parks")

	daemon := h.startDaemon("daemon")
	h.waitRun(crashTaskID, 40*time.Second, func(run map[string]any) bool {
		return runString(run, "lease_state") == "parked_no_progress" &&
			runInt(run, "attempt_count") == 3
	})
	run := h.latestRun(crashTaskID)
	if runString(run, "attempt_outcome") != "blocked" {
		t.Fatalf("expected blocked no-progress outcome, got %q", runString(run, "attempt_outcome"))
	}
	terminal, ok := runBool(run, "terminal")
	if !ok || !terminal {
		t.Fatalf("parked continuation cap should set terminal=true; run=%s", prettyJSON(run))
	}
	if !strings.Contains(runString(run, "last_error"), "continuation retry cap reached") {
		t.Fatalf("parked run should preserve cap reason, got %q", runString(run, "last_error"))
	}
	status := h.automationStatus()
	if got := intFromPath(status, "status", "active_runs"); got != 0 {
		t.Fatalf("parked no-progress run left active capacity occupied: active_runs=%d", got)
	}
	if got := intFromPath(status, "status", "parked_runs"); got != 1 {
		t.Fatalf("expected parked run in daemon status, got parked_runs=%d", got)
	}
	daemon.stop()
}

type fakeRunnerConfig struct {
	Mode                   string
	RunnerKind             string
	ReleaseFile            string
	CompleteStatus         string
	ExitCode               int
	HoldTimeout            time.Duration
	StallTimeoutMS         int
	MaxAttempts            int
	MaxContinuationRetries int
	BackoffMS              []int
	Delivery               bool
	Reviewer               bool
	MaxActive              int
	WorkspaceStrategy      string
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
	if cfg.MaxActive <= 0 {
		cfg.MaxActive = 1
	}
	if cfg.WorkspaceStrategy == "" {
		cfg.WorkspaceStrategy = "copy"
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
	if cfg.Delivery {
		parts = append(parts, "--delivery", "--delivery-control", shellQuote(filepath.Join(h.tempRoot, "delivery-control")))
	}
	command := strings.Join(parts, " ")
	if cfg.RunnerKind == "codex_exec" {
		binDir := filepath.Join(h.tempRoot, "bin")
		h.mustMkdir(binDir)
		codexShim := filepath.Join(binDir, "codex")
		h.writeFile(codexShim, "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then\n  echo codex-crashrecovery-shim\n  exit 0\nfi\nresume_ref=\nif [ \"$1\" = \"exec\" ] && [ \"$2\" = \"resume\" ]; then\n  for arg in \"$@\"; do\n    case \"$arg\" in\n      exec|resume|-|--*) ;;\n      *) resume_ref=\"$arg\" ;;\n    esac\n  done\nfi\nTUSKER_FAKE_SESSION_REF=\"$resume_ref\" exec "+command+"\n")
		if err := os.Chmod(codexShim, 0o755); err != nil {
			h.t.Fatal(err)
		}
		command = "codex exec --json --skip-git-repo-check -"
	}
	authoritativeAutomation := ""
	runnerName := "codex"
	if cfg.Delivery {
		runnerName = "codex_exec"
		authoritativeAutomation = `  completion_reactor:
    mode: authoritative
  default_profile: implementation-terra
  lane_profiles:
    execute: implementation-terra
    review: reviewer-terra
  profiles:
    implementation-terra:
      harness: codex_exec
      model: gpt-5.x
      effort: medium
      permission_preset: workspace-write-offline
      sandbox: {mode: workspace-write, network: false}
      subagents: {allowed: false, max_concurrent: 0}
    reviewer-terra:
      harness: codex_exec
      model: gpt-5.x
      effort: high
      permission_preset: read-only
      sandbox: {mode: read-only, network: false}
      subagents: {allowed: false, max_concurrent: 0}
`
	}
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
%s
  trigger_states: [ready, rework]
  default_runner: %s
  enabled_runners: [%s]
  workspace:
    strategy: %s
    root: workspaces
  concurrency:
    max_active_runs: %d
    max_active_runs_per_project: %d
    max_concurrent_by_state: {}
  runners:
    %s:
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
`, authoritativeAutomation, runnerName, runnerName, cfg.WorkspaceStrategy, cfg.MaxActive, cfg.MaxActive, runnerName, cfg.RunnerKind, command, cfg.StallTimeoutMS)
	if cfg.Delivery {
		config += "  validation:\n    commands:\n      - test -s docs/specs/delivery.md && test -d artifacts/delivery\n"
	}
	h.writeFile(filepath.Join(h.repoDir, "tusker.yaml"), config)

	workflow := h.readFile(filepath.Join(h.vaultDir, "WORKFLOW.md"))
	// The harness deliberately exercises daemon crash recovery. Keep the local
	// daemon opt-in explicit; a fresh workflow correctly defaults it off.
	if !strings.Contains(workflow, "automation_enabled: false") {
		h.t.Fatalf("crash-recovery fixture expected fresh workflow automation opt-in to be off")
	}
	workflow = strings.Replace(workflow, "automation_enabled: false", "automation_enabled: true", 1)
	workflow = replaceYAMLScalarUnder(workflow, "runtime:", "  poll_interval_ms:", "  poll_interval_ms: 100")
	workflow = replaceYAMLScalarUnder(workflow, "runtime:", "  max_active_runs_per_project:", fmt.Sprintf("  max_active_runs_per_project: %d", cfg.MaxActive))
	workflow = replaceYAMLScalarUnder(workflow, "workspace:", "  strategy:", "  strategy: "+cfg.WorkspaceStrategy)
	workflow = replaceYAMLScalarUnder(workflow, "workspace:", "  root:", "  root: workspaces")
	if cfg.Reviewer {
		workflow = replaceYAMLScalarUnder(workflow, "reviewer:", "  enabled:", "  enabled: true")
	}
	if cfg.MaxContinuationRetries > 0 {
		workflow = replaceYAMLScalarUnder(workflow, "runtime:", "  max_continuation_retries:", fmt.Sprintf("  max_continuation_retries: %d", cfg.MaxContinuationRetries))
	}
	workflow = replaceYAMLScalarUnder(workflow, "serve:", "    enabled:", "    enabled: false")
	workflow = replaceYAMLScalarUnder(workflow, "retry:", "  max_attempts:", fmt.Sprintf("  max_attempts: %d", cfg.MaxAttempts))
	workflow = replaceYAMLListUnder(workflow, "retry:", "  backoff_ms:", cfg.BackoffMS)
	h.writeFile(filepath.Join(h.vaultDir, "WORKFLOW.md"), workflow)

	h.cliOK(h.repoDir, "projects", "add", "--repo", h.repoDir, "--vault", h.vaultDir, "--json")
	limits := parseJSON(h.t, h.cliOK(h.repoDir, "daemon", "limits", "--json"))
	if runInt(limits, "max_active_runs") != cfg.MaxActive {
		h.cliOK(h.repoDir, "daemon", "limits", "--max-active-runs", strconv.Itoa(cfg.MaxActive), "--json")
	}
	h.cliOK(h.repoDir, "daemon", "limits", "--disk-pressure-enabled", "false", "--json")
}

func (h *harness) createRunnableTask(title string) {
	h.t.Helper()
	h.createRunnableTaskID(crashTaskID, title, "")
}

func (h *harness) createRunnableTaskID(expectedID, title, dependencies string) {
	h.t.Helper()
	args := []string{
		"new", "task",
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
	}
	if dependencies != "" {
		args = append(args, "--dependencies", dependencies)
	}
	h.cliOK(h.repoDir, args...)
	taskPath := filepath.Join(h.vaultDir, "work", "tasks", expectedID+".md")
	body := h.readFile(taskPath)
	body = replaceSection(body, "## Acceptance", strings.TrimSpace(`| ID | Outcome | Proof |
|---|---|---|
| A1 | The fake runner reaches the scenario-specific terminal behavior. | E2E harness assertion |`))
	body = replaceSection(body, "## Verification", strings.TrimSpace(`| Covers | Check | Result | Notes |
|---|---|---|---|
| A1 | go test ./e2e/crashrecovery | pending | Crash-recovery e2e scenario observes daemon state through the public CLI. |`))
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		h.t.Fatalf("task %s has no frontmatter end", expectedID)
	}
	end += 4
	body = body[:end] + "\nartifact_contract:\n  kind: reliability_timeline\n  path: e2e/crashrecovery/crash_recovery_test.go\n  summary: Process-boundary crash and convergence timeline.\n" + body[end:]
	h.writeFile(taskPath, body)
	h.cliOK(h.repoDir, "reconcile", "--vault", h.vaultDir, "--local", "--quiet")
}

func (h *harness) gitOK(args ...string) []byte {
	h.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = h.repoDir
	cmd.Env = h.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
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
	out, err := h.cli(dir, time.Minute, args...)
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
	env := make([]string, 0, len(os.Environ())+5)
	pathValue := filepath.Join(h.tempRoot, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	for _, entry := range os.Environ() {
		key := entry
		if index := strings.IndexByte(entry, '='); index >= 0 {
			key = entry[:index]
		}
		switch key {
		case "TUSKER_ATTEMPT_ID", "CODEX_SHELL", "CODEX_THREAD_ID", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT":
			continue
		case "PATH":
			env = append(env, "PATH="+pathValue)
			continue
		}
		env = append(env, entry)
	}
	if os.Getenv("PATH") == "" {
		env = append(env, "PATH="+pathValue)
	}
	return append(env,
		"TUSKER_STATE_ROOT="+h.stateRoot,
		"TUSKER_CONFIG="+filepath.Join(h.tempRoot, "config", "tusker", "config.yaml"),
		"TUSKER_WRAPPER_HEARTBEAT_MS=100",
		"TUSKER_WRAPPER_STOP_TIMEOUT_MS=1000",
		"TUSKER_POLL_INTERVAL_MS=100",
	)
}

func (h *harness) waitRunnerPID(timeout time.Duration) int {
	h.t.Helper()
	return h.waitRunnerPIDChange(0, timeout)
}

func (h *harness) waitRunnerPIDChange(previous int, timeout time.Duration) int {
	h.t.Helper()
	pidPath := filepath.Join(h.tempRoot, "runner.pid")
	var pid int
	eventually(h.t, timeout, 100*time.Millisecond, func() (bool, string) {
		raw, err := os.ReadFile(pidPath)
		if err != nil {
			return false, err.Error()
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || parsed <= 0 || parsed == previous {
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

// budgetScale returns the multiplier applied to every condition-wait ceiling in
// this suite. It is 1.0 by default, so unset environments keep the historical
// timing exactly. On an I/O-pressured host, exporting TUSKER_E2E_BUDGET_SCALE
// (e.g. "2" or "2.5") widens the generous hard ceilings without touching the
// poll cadence, so healthy-but-slow convergence is not flagged as a failure.
// Values <= 0 or unparseable fall back to 1.0. This never accelerates genuine
// non-convergence detection past the default and never shortens any budget.
func budgetScale() float64 {
	raw := strings.TrimSpace(os.Getenv("TUSKER_E2E_BUDGET_SCALE"))
	if raw == "" {
		return 1.0
	}
	scale, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 {
		return 1.0
	}
	return scale
}

// scaledBudget applies budgetScale to a ceiling, never returning less than the
// unscaled duration so a misconfigured scale can only widen, never tighten.
func scaledBudget(timeout time.Duration) time.Duration {
	scale := budgetScale()
	if scale <= 1.0 {
		return timeout
	}
	return time.Duration(float64(timeout) * scale)
}

func eventually(t *testing.T, timeout, tick time.Duration, fn func() (bool, string)) {
	t.Helper()
	timeout = scaledBudget(timeout)
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
