package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDisjointOwnershipDoesNotProbeProcesses(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(bin, "probed")
	if err := os.WriteFile(filepath.Join(bin, "ps"), []byte("#!/bin/sh\nprintf x >> "+shellSingleQuote(marker)+"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	now := time.Now().UTC()
	candidate := Note{Data: map[string]any{"id": "APP-T-CANDIDATE", "owned_paths": []string{"frontend"}}}
	notes := map[string]Note{"APP-T-HOLDER": {Data: map[string]any{"id": "APP-T-HOLDER", "owned_paths": []string{"backend"}}}}
	runs := []RunStatus{{ItemID: "APP-T-HOLDER", ProcessPID: os.Getpid(), ProcessStartedAt: "1900-01-01T00:00:00Z", LeaseState: string(LeaseStateRunning), LeaseExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)}}
	if _, found := ownedPathConflict(candidate, notes, runs, now); found {
		t.Fatal("disjoint paths conflict")
	}
	// A nil store is safe here because an unrelated lease must not be reclaimed.
	if taken, err := reclaimDeadOwnedPathHolders(nil, "", candidate, notes, runs, now); err != nil || len(taken) != 0 {
		t.Fatalf("disjoint holder reclaimed: %v %v", taken, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("disjoint ownership performed a process probe: %v", err)
	}
}

func TestQuietInteractiveExpiryDistinguishesReusedPID(t *testing.T) {
	now := time.Now().UTC()
	run := RunStatus{HandRun: true, ProcessPID: os.Getpid(), LeaseExpiresAt: now.Add(-25 * time.Hour).Format(time.RFC3339)}
	if quietInteractiveLeaseExpired(run, now) {
		t.Fatal("unknown live identity lost its ownership fence")
	}
	actual, ok := processStartTime(os.Getpid())
	if !ok {
		t.Skip("platform does not expose process start identity")
	}
	run.ProcessStartedAt = actual
	if quietInteractiveLeaseExpired(run, now) {
		t.Fatal("matching live identity expired")
	}
	run.ProcessStartedAt = "1900-01-01T00:00:00Z"
	if !quietInteractiveLeaseExpired(run, now) {
		t.Fatal("unrelated reused PID retained the expired lease")
	}
}
