package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunArtifactPurgeKeepsUnfinishedWorkAndExpiresCompletedFiles(t *testing.T) {
	state := t.TempDir()
	store, err := OpenRuntimeStore(state)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	path := func(record string) string { return filepath.Join(state, "runs", "project", record, "attempt.raw.log") }
	for _, tc := range []struct {
		name, record string
		terminal     bool
		ended        time.Time
	}{
		{"old", "OLD-T-0001", true, now.Add(-8 * 24 * time.Hour)},
		{"recent", "NEW-T-0001", true, now.Add(-2 * 24 * time.Hour)},
		{"unfinished", "OPEN-T-0001", false, now.Add(-8 * 24 * time.Hour)},
		{"parked", "PARK-T-0001", true, now.Add(-8 * 24 * time.Hour)},
	} {
		lease := string(LeaseStateReleased)
		if !tc.terminal {
			lease = string(LeaseStateRunning)
		} else if tc.name == "parked" {
			lease = string(LeaseStateParkedNoProgress)
		}
		p := path(tc.record)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(tc.name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, tc.ended, tc.ended); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRun(RunStatus{ProjectID: "project", RecordID: tc.record, ItemID: tc.record, LeaseState: lease, Terminal: tc.terminal, RawLogPath: p, UpdatedAt: now.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveAttempt(RunAttempt{AttemptID: tc.record + "-attempt", ProjectID: "project", RecordID: tc.record, ItemID: tc.record, RawLogPath: p, FinishedAt: tc.ended.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := purgeRunArtifacts(store, now, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path("OLD-T-0001")); !os.IsNotExist(err) {
		t.Fatalf("old terminal log should expire: %v", err)
	}
	for _, record := range []string{"NEW-T-0001", "OPEN-T-0001", "PARK-T-0001"} {
		if _, err := os.Stat(path(record)); err != nil {
			t.Fatalf("%s should remain: %v", record, err)
		}
	}
	if _, err := purgeRunArtifacts(store, now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path("NEW-T-0001")); !os.IsNotExist(err) {
		t.Fatalf("manual purge should remove recent terminal log: %v", err)
	}
	if _, err := os.Stat(path("OPEN-T-0001")); err != nil {
		t.Fatalf("manual purge removed unfinished work: %v", err)
	}
	if _, err := os.Stat(path("PARK-T-0001")); err != nil {
		t.Fatalf("manual purge removed parked work: %v", err)
	}
}

func TestSettingsRunArtifactPurgeAction(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	path := filepath.Join(server.store.stateRoot, "runs", "app", "APP-T-0001", "attempt.raw.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old output"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.store.UpsertRun(RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateReleased), Terminal: true, RawLogPath: path}); err != nil {
		t.Fatal(err)
	}
	var report runArtifactPurgeReport
	servePost(t, server, "/api/run-artifacts/purge", "{}", &report)
	if !report.OK || report.Files != 1 || report.Bytes != int64(len("old output")) {
		t.Fatalf("purge report: %+v", report)
	}
}
