package main

import (
	"strings"
	"testing"
)

// prepareCopyWorkspace materializes one live work copy under the shared root for
// the given record, using the copy strategy so the test needs no git.
func prepareCopyWorkspace(t *testing.T, stateRoot, recordID string, max int) (WorkspacePrepareResult, error) {
	t.Helper()
	manager := NewWorkspaceManager()
	return manager.Prepare(WorkspacePrepareRequest{
		ProjectID: "project-1", ProjectKey: "MEM", RecordID: recordID, ItemID: recordID,
		RepoRoot: t.TempDir(), StateRoot: stateRoot, Strategy: WorkspaceStrategyCopy,
		MaxLiveWorktrees: max,
	})
}

// A1: opening a work copy past the configured live limit is refused with a named
// reason.
func TestWorktreeCapRefusesOverLimit(t *testing.T) {
	stateRoot := t.TempDir()
	const cap = 2
	for _, record := range []string{"record-1", "record-2"} {
		if _, err := prepareCopyWorkspace(t, stateRoot, record, cap); err != nil {
			t.Fatalf("preparing %s within cap should succeed: %v", record, err)
		}
	}
	_, err := prepareCopyWorkspace(t, stateRoot, "record-3", cap)
	if err == nil {
		t.Fatal("expected the over-limit work copy to be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "another live work copy") || !strings.Contains(msg, "cap of 2") {
		t.Fatalf("refusal must name the reason and the cap, got %q", msg)
	}
	// Reusing an already-live copy is not a new copy and must not be refused.
	if _, err := prepareCopyWorkspace(t, stateRoot, "record-1", cap); err != nil {
		t.Fatalf("reusing an existing work copy must not be refused: %v", err)
	}
}

// A2: a build-and-test started with free disk below the configured floor is
// refused, telling the operator what to reclaim.
func TestGateRefusesBelowDiskFloor(t *testing.T) {
	policy := GateTierPolicy{MinFreeDiskGB: 20}
	rt := gateTierRuntime{
		Workspace:  t.TempDir(),
		FreeDiskGB: func(string) (float64, error) { return 3.5, nil },
	}
	refusal := gateTierPreflight(policy, "", rt)
	if refusal == nil {
		t.Fatal("expected the below-floor gate to be refused")
	}
	if refusal.Cause != gateRefusalDiskHeadroom {
		t.Fatalf("refusal cause = %q, want %q", refusal.Cause, gateRefusalDiskHeadroom)
	}
	if !strings.Contains(refusal.Detail, "20.0 GB floor") {
		t.Fatalf("refusal must name the configured floor, got %q", refusal.Detail)
	}
	if !strings.Contains(strings.ToLower(refusal.Remedy), "reclaim") {
		t.Fatalf("refusal must tell the operator what to reclaim, got %q", refusal.Remedy)
	}
	// Above the floor there is no refusal.
	rt.FreeDiskGB = func(string) (float64, error) { return 40, nil }
	if r := gateTierPreflight(policy, "", rt); r != nil {
		t.Fatalf("above-floor gate must not be refused, got %+v", r)
	}
}

// A3: the limit and the floor come from configured values, not numbers
// hard-coded in the tool. A zero config disables each guardrail; a configured
// value drives the refusal.
func TestWorktreeCapAndFloorFromConfig(t *testing.T) {
	// Cap comes from config: zero means no cap, even with copies already live.
	stateRoot := t.TempDir()
	for _, record := range []string{"record-1", "record-2", "record-3"} {
		if _, err := prepareCopyWorkspace(t, stateRoot, record, 0); err != nil {
			t.Fatalf("cap of zero must impose no limit, %s refused: %v", record, err)
		}
	}
	// A configured cap of 1 refuses the second copy under a fresh root.
	freshRoot := t.TempDir()
	if _, err := prepareCopyWorkspace(t, freshRoot, "record-1", 1); err != nil {
		t.Fatalf("first copy under cap 1 should succeed: %v", err)
	}
	if _, err := prepareCopyWorkspace(t, freshRoot, "record-2", 1); err == nil {
		t.Fatal("configured cap of 1 must refuse the second copy")
	}

	// Floor comes from config: zero disables the disk check entirely.
	rt := gateTierRuntime{
		Workspace:  t.TempDir(),
		FreeDiskGB: func(string) (float64, error) { return 0.1, nil },
	}
	if r := gateTierPreflight(GateTierPolicy{MinFreeDiskGB: 0}, "", rt); r != nil {
		t.Fatalf("a zero floor must not refuse, got %+v", r)
	}
	// The refusal reflects whatever floor the config carries, not a literal.
	if r := gateTierPreflight(GateTierPolicy{MinFreeDiskGB: 7}, "", rt); r == nil ||
		!strings.Contains(r.Detail, "7.0 GB floor") {
		t.Fatalf("refusal must reflect the configured floor of 7, got %+v", r)
	}
}
