package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPollIntervalDefaultIsGlobalSafetyNet(t *testing.T) {
	t.Setenv(daemonPollIntervalEnv, "")
	if got := defaultWorkflow().Runtime.PollIntervalMS; got != int(defaultReconcileTick/time.Millisecond) {
		t.Fatalf("workflow default poll interval = %dms, want %dms", got, defaultReconcileTick/time.Millisecond)
	}
	// A nil store proves cadence selection no longer reads registered project
	// workflows, where a legacy 5s setting used to become the global minimum.
	if got := (&Daemon{}).nextPollInterval(); got != defaultReconcileTick {
		t.Fatalf("default poll interval = %s, want %s", got, defaultReconcileTick)
	}
}

func TestDaemonReconciliationCanBeDisabled(t *testing.T) {
	t.Setenv(daemonReconcileModeEnv, "")
	if !daemonPeriodicReconciliationEnabled() {
		t.Fatal("resident daemon should retain adaptive reconciliation by default")
	}
	for _, raw := range []string{"event", "manual", "off", "false", "0"} {
		t.Setenv(daemonReconcileModeEnv, raw)
		if daemonPeriodicReconciliationEnabled() {
			t.Fatalf("reconciliation mode %q did not disable periodic polling", raw)
		}
	}
}

func TestPollIntervalGlobalOverrideWinsAndClamps(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "override", raw: "45000", want: 45 * time.Second},
		{name: "floor", raw: "100", want: minimumReconcileTick},
		{name: "invalid", raw: "not-a-number", want: defaultReconcileTick},
		{name: "overflow", raw: "9223372036855", want: defaultReconcileTick},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(daemonPollIntervalEnv, tc.raw)
			if got := (&Daemon{}).nextPollInterval(); got != tc.want {
				t.Fatalf("interval for %q = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}

func TestWatchdogTracksEffectivePollInterval(t *testing.T) {
	t.Setenv(daemonPollIntervalEnv, "45000")
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: stateRoot, store: store}
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	interval := daemon.nextPollInterval()
	if err := store.SetSetting("daemon_watchdog_beat_at", now.Add(-3*interval).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	stale, _, err := daemon.watchdogStale(now, interval)
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatalf("watchdog must allow a beat exactly at 3 × effective interval (%s)", interval)
	}
	if err := store.SetSetting("daemon_watchdog_beat_at", now.Add(-3*interval-time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	stale, _, err = daemon.watchdogStale(now, interval)
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Fatalf("watchdog must reject a beat older than 3 × effective interval (%s)", interval)
	}
}
