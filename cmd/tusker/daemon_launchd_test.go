package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLaunchdInstallUninstallIdempotent(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("HOME", home)

	var calls []string
	oldLaunchctl := launchctlRun
	launchctlRun = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	t.Cleanup(func() { launchctlRun = oldLaunchctl })

	for i := 0; i < 2; i++ {
		var err error
		_ = captureStdout(t, func() {
			err = daemonInstallCmd(Args{"json": "true"})
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	plistPath := filepath.Join(home, "Library", "LaunchAgents", daemonLaunchdLabel+".plist")
	raw, err := readLaunchdPlistForTest(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"<key>Label</key>",
		"<string>" + daemonLaunchdLabel + "</string>",
		"<key>KeepAlive</key>",
		"<key>SuccessfulExit</key>",
		"<false/>",
		"<key>ThrottleInterval</key>",
		"<integer>10</integer>",
		"<key>StandardOutPath</key>",
		"<string>" + daemonLogPath(stateRoot) + "</string>",
		"<key>StandardErrorPath</key>",
		"<key>" + daemonLaunchdEnvKey + "</key>",
		"<string>1</string>",
		"<key>TUSKER_STATE_ROOT</key>",
		"<string>" + stateRoot + "</string>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist missing %q:\n%s", want, text)
		}
	}
	if countStringPrefix(calls, "bootstrap ") != 2 || countStringPrefix(calls, "kickstart -k ") != 2 {
		t.Fatalf("expected idempotent launchctl bootstrap/kickstart calls, got %#v", calls)
	}

	for i := 0; i < 2; i++ {
		var err error
		_ = captureStdout(t, func() {
			err = daemonUninstallCmd(Args{"json": "true"})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if fileExists(plistPath) {
		t.Fatalf("expected plist removed: %s", plistPath)
	}
}

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
		status, err = store.recordDaemonAbnormalStart(daemonRestartCauseStalePID, now.Add(time.Duration(i)*time.Minute))
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

	resumed, closed, err := daemon.ResumeCrashLoopCircuit()
	if err != nil {
		t.Fatal(err)
	}
	if !closed || resumed.Open {
		t.Fatalf("expected resume to close crash loop, closed=%v status=%#v", closed, resumed)
	}
}

func TestDaemonManualModeUnaffectedByLaunchd(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	clearAgentSessionEnvForTest(t)
	t.Setenv(daemonLaunchdEnvKey, "")
	oldLaunchctl := launchctlRun
	launchctlRun = func(args ...string) error {
		t.Fatalf("manual mode must not call launchctl, got %#v", args)
		return nil
	}
	t.Cleanup(func() { launchctlRun = oldLaunchctl })

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

func readLaunchdPlistForTest(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		_, err := decoder.Token()
		if err == nil {
			continue
		}
		if err == io.EOF {
			break
		}
		return nil, err
	}
	return raw, nil
}

func countStringPrefix(values []string, prefix string) int {
	count := 0
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			count++
		}
	}
	return count
}
