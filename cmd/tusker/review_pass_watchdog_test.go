package main

import (
	"testing"
	"time"
)

func TestFeedWatchdogDuringKeepsBeatFreshUntilCap(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d := &Daemon{store: store}
	old := reviewPassWatchdogFeedInterval
	reviewPassWatchdogFeedInterval = 10 * time.Millisecond
	defer func() { reviewPassWatchdogFeedInterval = old }()

	stop := d.feedWatchdogDuring(200 * time.Millisecond)
	time.Sleep(60 * time.Millisecond)
	stop()
	stop()
	beat, err := store.GetSetting("daemon_watchdog_beat_at")
	if err != nil || beat == "" {
		t.Fatalf("watchdog beat not fed: %q err=%v", beat, err)
	}
	at, err := time.Parse(time.RFC3339Nano, beat)
	if err != nil || time.Since(at) > time.Second {
		t.Fatalf("watchdog beat is not fresh: %q", beat)
	}
	time.Sleep(50 * time.Millisecond)
	if after, _ := store.GetSetting("daemon_watchdog_beat_at"); after != beat {
		t.Fatalf("watchdog kept being fed after stop: %q -> %q", beat, after)
	}
}
