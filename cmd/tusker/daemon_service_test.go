package main

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRenderDaemonServicePlist(t *testing.T) {
	config := daemonServiceConfig{
		Label:            daemonServiceLabel,
		SourceExecutable: "/Applications/Tusker & Tools/tusker",
		Executable:       "/tmp/tusker & state/bin/tusker-daemon",
		StateRoot:        "/tmp/tusker & state",
		Path:             "/opt/homebrew/bin:/usr/local/bin:/usr/bin",
		LaunchAgentDir:   "/Users/example/Library/LaunchAgents",
	}
	plist := renderDaemonServicePlist(config)

	var document struct {
		XMLName xml.Name `xml:"plist"`
	}
	if err := xml.Unmarshal([]byte(plist), &document); err != nil {
		t.Fatalf("rendered plist must be valid XML: %v\n%s", err, plist)
	}
	if document.XMLName.Local != "plist" {
		t.Fatalf("expected plist root, got %q", document.XMLName.Local)
	}
	for _, expected := range []string{
		"<string>/tmp/tusker &amp; state/bin/tusker-daemon</string>",
		"<string>daemon</string>",
		"<string>run</string>",
		"<key>RunAtLoad</key>\n<true/>",
		"<key>KeepAlive</key>\n<dict>\n<key>SuccessfulExit</key>\n<false/>",
		"<key>ThrottleInterval</key>\n<integer>10</integer>",
		"<key>PATH</key>\n<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin</string>",
		"<key>TUSKER_STATE_ROOT</key>\n<string>/tmp/tusker &amp; state</string>",
		"<key>" + daemonLaunchdEnvKey + "</key>\n<string>1</string>",
		"<key>StandardOutPath</key>\n<string>/tmp/tusker &amp; state/logs/daemon.log</string>",
		"<key>StandardErrorPath</key>\n<string>/tmp/tusker &amp; state/logs/daemon.log</string>",
	} {
		if !strings.Contains(plist, expected) {
			t.Fatalf("plist missing %q:\n%s", expected, plist)
		}
	}
	if strings.Contains(plist, "<key>SuccessfulExit</key>\n<true/>") {
		t.Fatalf("plist must restart only after abnormal exits:\n%s", plist)
	}
}

func TestEnforceDaemonServiceLogBoundTrimsLiveInode(t *testing.T) {
	dir := t.TempDir()
	config := daemonServiceConfig{StateRoot: dir}
	if err := ensureDir(config.logDir()); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte(strings.Repeat("x", daemonLogMaxBytes)), []byte("TAIL\n")...)
	if err := os.WriteFile(config.stdoutPath(), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := enforceDaemonServiceLogBound(config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(config.stdoutPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > daemonLogMaxBytes/2 {
		t.Fatalf("live daemon log was not bounded: %d", info.Size())
	}
	got, err := os.ReadFile(config.stdoutPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "TAIL\n") {
		t.Fatalf("log tail evidence was lost: suffix=%q", string(got[max(0, len(got)-8):]))
	}
}

func TestLaunchdInstallUninstallIdempotentPublicCommands(t *testing.T) {
	clearAgentSessionEnvForTest(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "source-tusker")
	if err := os.WriteFile(source, []byte("fixture executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := daemonServiceConfig{
		Label: daemonServiceLabel, SourceExecutable: source,
		Executable: filepath.Join(dir, "state", "bin", "tusker-daemon"),
		StateRoot:  filepath.Join(dir, "state"), Home: filepath.Join(dir, "home"),
		Path: "/usr/bin:/bin", LaunchAgentDir: filepath.Join(dir, "home", "Library", "LaunchAgents"),
	}
	originalGOOS, originalConfig := daemonServiceGOOS, daemonServiceConfigCurrent
	originalRun, originalWait := daemonServiceCommandRun, daemonServiceWaitReady
	t.Cleanup(func() {
		daemonServiceGOOS, daemonServiceConfigCurrent = originalGOOS, originalConfig
		daemonServiceCommandRun, daemonServiceWaitReady = originalRun, originalWait
	})
	daemonServiceGOOS = "darwin"
	daemonServiceConfigCurrent = func() (daemonServiceConfig, error) { return config, nil }
	daemonServiceWaitReady = func(daemonServiceConfig, time.Time, time.Duration) error { return nil }
	var commands []string
	daemonServiceCommandRun = func(command daemonServiceCommand, _ daemonServiceConfig) ([]byte, error) {
		commands = append(commands, command.String())
		return nil, nil
	}

	for i := 0; i < 2; i++ {
		if _, err := run("daemon install", Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	}
	plist, err := os.ReadFile(config.plistPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plist), daemonLaunchdEnvKey) {
		t.Fatalf("managed marker missing from plist:\n%s", plist)
	}
	for i := 0; i < 2; i++ {
		if _, err := run("daemon uninstall", Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(config.plistPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plist survived idempotent uninstall: %v", err)
	}
	joined := strings.Join(commands, "\n")
	if strings.Count(joined, " bootstrap ") != 2 || strings.Count(joined, " bootout ") != 4 {
		t.Fatalf("unexpected fixture lifecycle commands:\n%s", joined)
	}
}

func TestPlanDaemonService(t *testing.T) {
	config := daemonServiceConfig{
		Label:            daemonServiceLabel,
		SourceExecutable: "/usr/local/bin/tusker",
		Executable:       "/Users/example/Library/Application Support/tusker/bin/tusker-daemon",
		StateRoot:        "/Users/example/Library/Application Support/tusker",
		Path:             "/usr/local/bin:/usr/bin",
		LaunchAgentDir:   "/Users/example/Library/LaunchAgents",
	}
	target := config.serviceTarget()
	domain := config.domainTarget()

	cases := []struct {
		action string
		want   []daemonServiceCommand
	}{
		{"install", []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"bootout", target}}, {Name: daemonLaunchctlPath, Args: []string{"bootstrap", domain, config.plistPath()}}}},
		{"start", []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"bootstrap", domain, config.plistPath()}}}},
		{"stop", []daemonServiceCommand{{Name: config.Executable, Args: []string{"daemon", "stop"}}, {Name: daemonLaunchctlPath, Args: []string{"bootout", target}}}},
		{"status", []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"print", target}}}},
		{"uninstall", []daemonServiceCommand{{Name: daemonLaunchctlPath, Args: []string{"bootout", target}}}},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			got, err := planDaemonService(tc.action, config)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("plan mismatch:\nwant: %#v\n got: %#v", tc.want, got)
			}
		})
	}
}

func TestPlanDaemonServiceRejectsUnknownAction(t *testing.T) {
	if _, err := planDaemonService("restart", daemonServiceConfig{}); err == nil || !strings.Contains(err.Error(), "install, start, stop, status, or uninstall") {
		t.Fatalf("expected actionable invalid action error, got %v", err)
	}
}

func TestEnvironmentWithReplacesOnlyRequestedKey(t *testing.T) {
	got := environmentWith([]string{"PATH=/one", "TUSKER_STATE_ROOT=/old", "OTHER=value", "TUSKER_STATE_ROOT=/duplicate"}, "TUSKER_STATE_ROOT", "/new")
	want := []string{"PATH=/one", "OTHER=value", "TUSKER_STATE_ROOT=/new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment mismatch:\nwant: %#v\n got: %#v", want, got)
	}
}

func TestInstallDaemonServiceExecutableCopiesToStableRuntimePath(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source-tusker")
	if err := os.WriteFile(source, []byte("daemon binary fixture"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := daemonServiceConfig{
		SourceExecutable: source,
		Executable:       filepath.Join(dir, "state", "bin", "tusker-daemon"),
		StateRoot:        filepath.Join(dir, "state"),
	}
	if err := installDaemonServiceExecutable(config); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(config.Executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "daemon binary fixture" {
		t.Fatalf("service executable content = %q", string(got))
	}
	info, err := os.Stat(config.Executable)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		t.Fatalf("service executable mode = %v", info.Mode())
	}
}

func TestDaemonServiceRuntimeHealthRequiresFreshPoll(t *testing.T) {
	startedAt := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		health daemonServiceRuntimeHealth
		want   bool
	}{
		{name: "fresh live poll", health: daemonServiceRuntimeHealth{Alive: true, StartedAt: startedAt, LastPollAt: startedAt}, want: true},
		{name: "prior process poll", health: daemonServiceRuntimeHealth{Alive: true, StartedAt: startedAt, LastPollAt: startedAt.Add(-time.Nanosecond)}, want: false},
		{name: "no poll", health: daemonServiceRuntimeHealth{Alive: true, StartedAt: startedAt}, want: false},
		{name: "dead process", health: daemonServiceRuntimeHealth{StartedAt: startedAt, LastPollAt: startedAt.Add(time.Second)}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.health.readySince(startedAt); got != tc.want {
				t.Fatalf("readySince = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestDaemonServiceRuntimeHealthRejectsPriorProcessPoll(t *testing.T) {
	now := time.Date(2026, 7, 10, 10, 5, 0, 0, time.UTC)
	health := daemonServiceRuntimeHealth{Alive: true, StartedAt: now, LastPollAt: now.Add(-time.Second)}
	if health.healthyAt(now) {
		t.Fatal("a fresh-looking poll from a prior process must not report healthy")
	}
}

func TestDaemonServiceBootstrapRetryable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{err: nil, want: false},
		{err: os.ErrPermission, want: false},
		{err: &execErrorStub{message: "launchctl bootstrap: Bootstrap failed: 5: Input/output error"}, want: true},
	} {
		if got := daemonServiceBootstrapRetryable(tc.err); got != tc.want {
			t.Fatalf("retryable = %t, want %t for %v", got, tc.want, tc.err)
		}
	}
}

func TestDaemonServiceStateRootUsesApplicationSupportAndRejectsCustomServiceRoot(t *testing.T) {
	home := filepath.Join(string(os.PathSeparator), "Users", "example")
	want := filepath.Join(home, "Library", "Application Support", "tusker")
	got, err := daemonServiceStateRoot(home, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("state root = %q, want %q", got, want)
	}
	_, err = daemonServiceStateRoot(home, filepath.Join(home, "Downloads", "tusker-state"))
	var typed *TuskerError
	if err == nil || !errors.As(err, &typed) || !strings.Contains(typed.Hint, "unset TUSKER_STATE_ROOT") {
		t.Fatalf("expected actionable custom service-root refusal, got %v", err)
	}
}

type execErrorStub struct{ message string }

func (e *execErrorStub) Error() string { return e.message }
