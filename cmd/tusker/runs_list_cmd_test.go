package main

import (
	"strings"
	"testing"
)

func TestRunsListCmd(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []RunStatus{
		{ProjectID: "p", RecordID: "T-1", ActiveAttemptID: "A-1", RunnerProfile: "standard", LeaseState: string(LeaseStateRunning), StartedAt: "2026-09-26T10:00:00Z", AttemptCount: 2},
		{ProjectID: "p", RecordID: "T-2", LeaseState: string(LeaseStateReleased), StartedAt: "2026-09-27T10:00:00Z"},
	} {
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	output := captureStdout(t, func() {
		if err := runsListCmd(Args{"project": "p", "active": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "A-1  T-1  profile=standard  state=running") || strings.Contains(output, "T-2") {
		t.Fatalf("runs list: %q", output)
	}
	output = captureStdout(t, func() {
		if err := runsListCmd(Args{"project": "p", "limit": "1"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "T-2") || strings.Contains(output, "T-1") {
		t.Fatalf("newest limited run: %q", output)
	}
}
