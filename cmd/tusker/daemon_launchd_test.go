package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchdogStallExit(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	if err := store.SetSetting("daemon_watchdog_beat_at", now.Add(-91*time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	var exitReason string
	oldExit := daemonWatchdogExit
	daemonWatchdogExit = func(reason string) {
		exitReason = reason
	}
	t.Cleanup(func() { daemonWatchdogExit = oldExit })

	daemon.checkWatchdogOrExit(now, 30*time.Second)
	if !strings.Contains(exitReason, "watchdog beat stale") {
		t.Fatalf("expected watchdog abnormal exit reason, got %q", exitReason)
	}
	cause, err := store.GetSetting(daemonLastRestartCauseKey)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, daemonRestartCauseWatchdog, cause, "last restart cause")

	exitReason = ""
	if err := store.SetSetting("daemon_watchdog_beat_at", now.Add(-89*time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	daemon.checkWatchdogOrExit(now, 30*time.Second)
	assertEqual(t, "", exitReason, "fresh watchdog should not exit")
}

func TestWatchdogStallExitRestartsThroughLaunchdFixture(t *testing.T) {
	if os.Getenv("TUSKER_WATCHDOG_LAUNCHD_FIXTURE") == "1" {
		stateRoot := os.Getenv("TUSKER_FIXTURE_STATE_ROOT")
		_ = os.Setenv("TUSKER_STATE_ROOT", stateRoot)
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			os.Exit(2)
		}
		defer store.Close()
		daemon := &Daemon{stateRoot: stateRoot, store: store}
		if os.Getenv("TUSKER_FIXTURE_GENERATION") == "1" {
			now := time.Now().UTC()
			if daemon.feedWatchdogBeat(now) != nil {
				os.Exit(3)
			}
			daemon.checkWatchdogOrExit(now.Add(31*time.Millisecond), 10*time.Millisecond)
			os.Exit(4)
		}
		status, err := store.beginManagedDaemonStart(false, time.Now().UTC())
		if err != nil || status.RestartCount != 1 || status.LastRestartCause != daemonRestartCauseWatchdog {
			fmt.Fprintf(os.Stderr, "watchdog fixture restart status=%#v err=%v launchd=%q pending=%q\n", status, err, os.Getenv(daemonLaunchdEnvKey), settingForTest(store, daemonPendingRestartCauseKey))
			os.Exit(5)
		}
		return
	}

	stateRoot := filepath.Join(t.TempDir(), "state")
	runFixture := func(generation string) error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestWatchdogStallExitRestartsThroughLaunchdFixture$")
		cmd.Env = append(scrubAgentSessionEnv(os.Environ()),
			"TUSKER_WATCHDOG_LAUNCHD_FIXTURE=1",
			"TUSKER_FIXTURE_GENERATION="+generation,
			"TUSKER_FIXTURE_STATE_ROOT="+stateRoot,
			daemonLaunchdEnvKey+"=1",
		)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	firstErr := runFixture("1")
	exitErr, ok := firstErr.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 70 {
		t.Fatalf("stalled watchdog fixture exit = %v, want abnormal code 70", firstErr)
	}
	if err := runFixture("2"); err != nil {
		t.Fatalf("launchd fixture did not restart cleanly: %v", err)
	}
}

func TestLaunchdFixtureRestartsDaemonAfterSIGKILL(t *testing.T) {
	if os.Getenv("TUSKER_DAEMON_LAUNCHD_FIXTURE") == "1" {
		_ = os.Setenv("TUSKER_STATE_ROOT", os.Getenv("TUSKER_FIXTURE_STATE_ROOT"))
		if err := daemonRunCmd(Args{}); err != nil {
			fmt.Fprintf(os.Stderr, "managed daemon fixture failed: %v\n", err)
			os.Exit(6)
		}
		return
	}
	stateRoot, err := os.MkdirTemp("/tmp", "tusker-run15-launchd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	start := func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLaunchdFixtureRestartsDaemonAfterSIGKILL$")
		cmd.Env = append(scrubAgentSessionEnv(os.Environ()),
			"TUSKER_DAEMON_LAUNCHD_FIXTURE=1",
			"TUSKER_FIXTURE_STATE_ROOT="+stateRoot,
			daemonLaunchdEnvKey+"=1",
		)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	waitAlive := func(previousPID int) daemonLiveness {
		var live daemonLiveness
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			live = readDaemonLiveness(stateRoot, time.Now().UTC())
			if live.Alive && live.ManagedByLaunchd && live.PID != previousPID {
				return live
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for managed daemon fixture after pid %d: %#v", previousPID, live)
		return live
	}
	first := start()
	firstLive := waitAlive(0)
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	second := start()
	secondLive := waitAlive(firstLive.PID)
	if secondLive.PID == firstLive.PID {
		t.Fatalf("launchd fixture did not replace killed daemon pid %d", firstLive.PID)
	}
	t.Cleanup(func() {
		if second.ProcessState == nil || !second.ProcessState.Exited() {
			_ = second.Process.Kill()
			_ = second.Wait()
		}
	})
	var stopErr error
	stopDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(stopDeadline) {
		if _, stopErr = sendDaemonControl(stateRoot, daemonControlRequest{Command: "stop"}); stopErr == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if stopErr != nil {
		t.Fatalf("restarted daemon control socket never became ready: %v", stopErr)
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("restarted daemon did not stop cleanly: %v", err)
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status, err := store.ReadCrashLoopStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.RestartCount != 1 || status.LastRestartCause != daemonRestartCauseStalePID {
		t.Fatalf("SIGKILL restart was not recorded exactly once: %#v", status)
	}
}

func TestCrashLoopPreRunFailuresLeaveSixthReplacementServingReads(t *testing.T) {
	if os.Getenv("TUSKER_PRE_RUN_LAUNCHD_FIXTURE") == "1" {
		_ = os.Setenv("TUSKER_STATE_ROOT", os.Getenv("TUSKER_FIXTURE_STATE_ROOT"))
		if os.Getenv("TUSKER_FIXTURE_FAIL_BEFORE_RUN") == "1" {
			daemonCreate = func(string) (*Daemon, error) {
				return nil, errors.New("fixture failure before Daemon.Run")
			}
		}
		if err := daemonRunCmd(Args{}); err != nil {
			if os.Getenv("TUSKER_FIXTURE_FAIL_BEFORE_RUN") == "1" {
				os.Exit(42)
			}
			fmt.Fprintf(os.Stderr, "crash-loop fixture failed: %v\n", err)
			os.Exit(43)
		}
		return
	}

	stateRoot, err := os.MkdirTemp("/tmp", "tusker-run15-crash-loop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	vault := pickupV7TestVault(t)
	writeDaemonServeWorkflow(t, vault, true, "127.0.0.1:0")
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Crash loop dispatch probe", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := newRegisteredProject(filepath.Dir(vault), vault)
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalActiveRunLimit(1); err != nil {
		t.Fatal(err)
	}
	// The first of six abnormal generations is an unconsumed watchdog exit.
	// Corrupt history must be quarantined without dropping that debt; five
	// subsequent guarded startup failures complete the six-start burst.
	if err := store.markManagedDaemonAbnormalExit(daemonRestartCauseWatchdog); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(daemonRestartTimestampsKey, "not-json"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	fixtureCommand := func(failBeforeRun bool) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCrashLoopPreRunFailuresLeaveSixthReplacementServingReads$")
		env := append(scrubAgentSessionEnv(os.Environ()),
			"TUSKER_PRE_RUN_LAUNCHD_FIXTURE=1",
			"TUSKER_FIXTURE_STATE_ROOT="+stateRoot,
			daemonLaunchdEnvKey+"=1",
		)
		if failBeforeRun {
			env = append(env, "TUSKER_FIXTURE_FAIL_BEFORE_RUN=1")
		}
		cmd.Env = env
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		return cmd
	}
	for generation := 1; generation <= 5; generation++ {
		err := fixtureCommand(true).Run()
		exitErr, ok := err.(*exec.ExitError)
		if !ok || exitErr.ExitCode() != 42 {
			t.Fatalf("managed pre-run generation %d exit=%v, want fixture failure 42", generation, err)
		}
	}
	replacement := fixtureCommand(false)
	if err := replacement.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if replacement.ProcessState == nil || !replacement.ProcessState.Exited() {
			_ = replacement.Process.Kill()
			_ = replacement.Wait()
		}
	})

	var live daemonLiveness
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		live = readDaemonLiveness(stateRoot, time.Now().UTC())
		if live.Alive && live.ManagedByLaunchd && live.ServeEnabled && live.ServeAddr != "" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !live.Alive || live.ServeAddr == "" {
		t.Fatalf("sixth replacement did not stay alive and serve reads: %#v", live)
	}

	readCrashLoop := func() daemonCrashLoopStatus {
		var lastErr error
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			live = readDaemonLiveness(stateRoot, time.Now().UTC())
			response, err := http.Get("http://" + live.ServeAddr + "/api/daemon")
			if err != nil {
				lastErr = err
				time.Sleep(25 * time.Millisecond)
				continue
			}
			var payload struct {
				CrashLoop daemonCrashLoopStatus `json:"crashLoop"`
			}
			err = json.NewDecoder(response.Body).Decode(&payload)
			_ = response.Body.Close()
			if err == nil {
				return payload.CrashLoop
			}
			lastErr = err
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("live daemon read failed: %v", lastErr)
		return daemonCrashLoopStatus{}
	}
	if status := readCrashLoop(); !status.Open || status.RestartCount != 6 || status.Reason != daemonCrashLoopReason {
		t.Fatalf("live sixth replacement did not expose crash-loop state: %#v", status)
	}

	store, err = OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if quarantined, err := store.GetSetting(daemonCorruptRestartHistoryKey); err != nil || quarantined != "not-json" {
		t.Fatalf("malformed restart history was not quarantined: value=%q err=%v", quarantined, err)
	}
	if pending, err := store.GetSetting(daemonPendingRestartCauseKey); err != nil || pending != "" {
		t.Fatalf("sixth replacement left consumed restart debt: value=%q err=%v", pending, err)
	}
	var blocked RunStatus
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := store.ListRuns()
		for _, run := range runs {
			if run.ItemID == "APP-T-0001" {
				blocked = run
			}
		}
		if strings.Contains(blocked.LastError, daemonCrashLoopReason) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if blocked.LeaseState != string(LeaseStateUnclaimed) || !strings.Contains(blocked.LastError, daemonCrashLoopReason) {
		t.Fatalf("circuit-open replacement dispatched work: %#v", blocked)
	}

	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	if err := daemonResumeCmd(Args{"json": "true"}); err != nil {
		t.Fatal(err)
	}
	if status := readCrashLoop(); status.Open {
		t.Fatalf("live read stayed circuit-open after explicit resume: %#v", status)
	}
	if blocker, err := (&Daemon{stateRoot: stateRoot, store: store}).crashLoopDispatchBlocker(); err != nil || blocker != "" {
		t.Fatalf("resume did not restore dispatch eligibility: blocker=%q err=%v", blocker, err)
	}
	stopManagedFixture(t, stateRoot, replacement)
}

func stopManagedFixture(t *testing.T, stateRoot string, command *exec.Cmd) {
	t.Helper()
	var stopErr error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, stopErr = sendDaemonControl(stateRoot, daemonControlRequest{Command: "stop"}); stopErr == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if stopErr != nil {
		t.Fatalf("managed fixture control socket never became ready: %v", stopErr)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("managed fixture did not stop cleanly: %v", err)
	}
}

func TestCrashLoopBreakerBlocksDispatchUntilResume(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Crash loop blocked", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	var status daemonCrashLoopStatus
	for i := 0; i < 6; i++ {
		if err := store.markManagedDaemonAbnormalExit(daemonRestartCauseRunError); err != nil {
			t.Fatal(err)
		}
		status, err = store.beginManagedDaemonStart(false, now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !status.Open || status.Reason != daemonCrashLoopReason || status.RestartCount != 6 {
		t.Fatalf("expected crash-loop circuit open after six starts, got %#v", status)
	}

	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateUnclaimed), run.LeaseState, "crash-loop blocked dispatch")
	if !strings.Contains(run.LastError, daemonCrashLoopReason) {
		t.Fatalf("expected crash-loop blocker, got %#v", run)
	}

	daemonStatus, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	crashLoop, ok := daemonStatus["crashLoop"].(daemonCrashLoopStatus)
	if !ok || !crashLoop.Open {
		t.Fatalf("daemon status must expose open crash loop, got %#v", daemonStatus["crashLoop"])
	}
	ctx, err := loadAutomationCommandContext(Args{"vault": vault})
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Close()
	note, err := resolveNote(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	explanation := ctx.explainTask(note)
	if explanation.Dispatchable || !strings.Contains(strings.Join(explanation.Blockers, "; "), daemonCrashLoopReason) {
		t.Fatalf("expected automation crash-loop blocker, got %#v", explanation.Blockers)
	}
	server := &serveServer{vaultPath: vault, repoRoot: filepath.Dir(vault), addr: defaultServeAddr, store: store, now: time.Now}
	response := httptest.NewRecorder()
	server.handleDaemon(response, httptest.NewRequest("GET", "/api/daemon", nil))
	if response.Code != 200 {
		t.Fatalf("daemon API status = %d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		CrashLoop daemonCrashLoopStatus `json:"crashLoop"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.CrashLoop.Open {
		t.Fatalf("serve API must expose open crash loop, got %#v", payload.CrashLoop)
	}

	var resumeErr error
	resumeOutput := captureStdout(t, func() {
		resumeErr = daemonResumeCmd(Args{"json": "true"})
	})
	if resumeErr != nil {
		t.Fatal(resumeErr)
	}
	resumed, err := store.ReadCrashLoopStatus()
	if err != nil || resumed.Open || !strings.Contains(resumeOutput, `"crash_loop_closed":true`) {
		t.Fatalf("explicit daemon resume did not close crash loop: output=%s status=%#v err=%v", resumeOutput, resumed, err)
	}
}

func TestManagedGuardLoserDoesNotPoisonRestartAccounting(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv(daemonLaunchdEnvKey, "1")
	originalAcquire := daemonGuardAcquire
	t.Cleanup(func() { daemonGuardAcquire = originalAcquire })
	daemonGuardAcquire = func(string) (*daemonGuard, error) {
		return nil, errors.New("fixture concurrent daemon owns guard")
	}
	if err := daemonRunCmd(Args{}); err == nil {
		t.Fatal("expected guard acquisition failure")
	}
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if pending, err := store.GetSetting(daemonPendingRestartCauseKey); err != nil || pending != "" {
		t.Fatalf("guard loser poisoned pending restart cause: pending=%q err=%v", pending, err)
	}
	status, err := store.ReadCrashLoopStatus()
	if err != nil || status.RestartCount != 0 {
		t.Fatalf("guard loser added restart debt: status=%#v err=%v", status, err)
	}
}

func TestManagedStartAccountingQuarantinesMalformedHistoryWithoutLosingPendingCause(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.markManagedDaemonAbnormalExit(daemonRestartCauseWatchdog); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(daemonRestartTimestampsKey, "not-json"); err != nil {
		t.Fatal(err)
	}
	status, err := store.beginManagedDaemonStart(false, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	pending, err := store.GetSetting(daemonPendingRestartCauseKey)
	if err != nil || pending != "" || status.RestartCount != 1 || status.LastRestartCause != daemonRestartCauseWatchdog {
		t.Fatalf("recovered accounting did not consume predecessor once: pending=%q status=%#v err=%v", pending, status, err)
	}
	quarantined, err := store.GetSetting(daemonCorruptRestartHistoryKey)
	if err != nil || quarantined != "not-json" {
		t.Fatalf("malformed history was not quarantined transactionally: value=%q err=%v", quarantined, err)
	}
}

func TestManagedExitJournalPreservesEveryUnconsumedCause(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, cause := range []string{daemonRestartCauseWatchdog, daemonRestartCauseRunError} {
		if err := store.markManagedDaemonAbnormalExit(cause); err != nil {
			t.Fatal(err)
		}
	}
	status, err := store.beginManagedDaemonStart(false, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if status.RestartCount != 2 || status.LastRestartCause != daemonRestartCauseRunError {
		t.Fatalf("unconsumed predecessor was overwritten: %#v", status)
	}
	pending, err := store.GetSetting(daemonPendingRestartCauseKey)
	if err != nil || pending != "" {
		t.Fatalf("consumed restart journal was not cleared: pending=%q err=%v", pending, err)
	}
}

func TestDaemonManualModeUnaffectedByLaunchd(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	clearAgentSessionEnvForTest(t)
	t.Setenv(daemonLaunchdEnvKey, "")

	var err error
	_ = captureStdout(t, func() {
		err = daemonRunCmd(Args{"once": "true"})
	})
	if err != nil {
		t.Fatal(err)
	}

	guard, err := acquireDaemonGuard(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	status, err := store.DaemonStatus()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, boolFromAny(status["daemon_alive"]), "manual daemon liveness")
	assertEqual(t, false, boolFromAny(status["daemon_managed_by_launchd"]), "manual launchd managed")
	assertEqual(t, "manual", stringValue(status["daemon_run_mode"]), "manual run mode")
}

func scrubAgentSessionEnv(environment []string) []string {
	blocked := []string{"TUSKER_ATTEMPT_ID=", "CODEX_SHELL=", "CODEX_THREAD_ID=", "CLAUDECODE=", "CLAUDE_CODE_ENTRYPOINT=", "TUSKER_STATE_ROOT=", "TUSKER_FIXTURE_STATE_ROOT=", daemonLaunchdEnvKey + "=", "TUSKER_WATCHDOG_LAUNCHD_FIXTURE=", "TUSKER_DAEMON_LAUNCHD_FIXTURE=", "TUSKER_PRE_RUN_LAUNCHD_FIXTURE=", "TUSKER_FIXTURE_FAIL_BEFORE_RUN=", "TUSKER_FIXTURE_GENERATION="}
	out := make([]string, 0, len(environment))
	for _, entry := range environment {
		skip := false
		for _, prefix := range blocked {
			if strings.HasPrefix(entry, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, entry)
		}
	}
	return out
}

func settingForTest(store *RuntimeStore, key string) string {
	value, _ := store.GetSetting(key)
	return value
}
