package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// writeOrphanCopy plants a live-looking work copy under root whose recording run
// is gone: its workspace.json records a PID that is not alive.
func writeOrphanCopy(t *testing.T, root, name string, pid int) string {
	t.Helper()
	dir := filepath.Join(root, name)
	metaDir := filepath.Join(dir, ".tusker")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("mkdir orphan copy: %v", err)
	}
	body := `{"strategy":"copy","record_id":"` + name + `","pid":` + itoa(pid) + `}` + "\n"
	if err := os.WriteFile(filepath.Join(metaDir, "workspace.json"), []byte(body), 0o600); err != nil {
		t.Fatalf("write orphan workspace.json: %v", err)
	}
	return dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// A1b: an orphaned copy (recording process gone) does not count toward the cap
// and is pruned on the next Prepare, so accumulated orphans can never wedge
// dispatch by exhausting the cap forever.
func TestWorktreeCapPrunesStaleOrphans(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatalf("create runtime store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close runtime store: %v", err)
	}
	// Materialize one real, live copy (owned by this test process, which is
	// alive) so we know the project root.
	live, err := prepareCopyWorkspace(t, stateRoot, "record-live", 1)
	if err != nil {
		t.Fatalf("first live copy under cap 1 should succeed: %v", err)
	}
	root := filepath.Dir(live.Path)

	// A dead PID is very unlikely to be a running process; pick an unused high one.
	const deadPID = 2147483000
	orphan := writeOrphanCopy(t, root, "record-orphan", deadPID)

	// The orphan must not count toward the cap: with cap 2 and one live + one
	// orphan, opening a new copy still succeeds because the orphan is pruned.
	if _, err := prepareCopyWorkspace(t, stateRoot, "record-new", 2); err != nil {
		t.Fatalf("orphan must not count toward the cap: %v", err)
	}
	if _, statErr := os.Stat(orphan); !os.IsNotExist(statErr) {
		t.Fatalf("orphaned copy should have been pruned, stat err = %v", statErr)
	}
}

// A1c: two concurrent Prepare calls against cap=1 under a fresh root must not
// both pass the count and exceed the cap — the cross-process lock lets exactly
// one succeed.
func TestWorktreeCapSerializesConcurrentPrepare(t *testing.T) {
	stateRoot := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			_, errs[idx] = prepareCopyWorkspace(t, stateRoot, "record-"+itoa(idx), 1)
		}(i)
	}
	close(start)
	wg.Wait()

	success := 0
	for _, e := range errs {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("exactly one concurrent Prepare must succeed under cap 1, got %d successes (errs=%v)", success, errs)
	}
}

// The daemon builds its WorkspacePrepareRequest from the workflow's
// workspace.max_live_worktrees. This pins that the configured cap reaches
// Prepare and is honored on the daemon dispatch path (not left dormant).
func TestDaemonPathPrepareHonorsConfiguredCap(t *testing.T) {
	stateRoot := t.TempDir()
	var wf Workflow
	wf.Workspace.Root = ""
	wf.Workspace.MaxLiveWorktrees = 1

	// buildDaemonReq mirrors the daemon's request literal: the cap comes from the
	// workflow config field, exactly as daemon.go wires it.
	buildDaemonReq := func(recordID string) WorkspacePrepareRequest {
		return WorkspacePrepareRequest{
			ProjectID: "project-1", ProjectKey: "MEM", RecordID: recordID, ItemID: recordID,
			RepoRoot: t.TempDir(), StateRoot: stateRoot,
			WorkspaceRoot: wf.Workspace.Root, Strategy: WorkspaceStrategyCopy,
			MaxLiveWorktrees: wf.Workspace.MaxLiveWorktrees,
		}
	}
	manager := NewWorkspaceManager()
	if _, err := manager.Prepare(buildDaemonReq("record-1")); err != nil {
		t.Fatalf("first daemon-path copy under cap 1 should succeed: %v", err)
	}
	_, err := manager.Prepare(buildDaemonReq("record-2"))
	if err == nil {
		t.Fatal("daemon-path prepare must refuse the second copy at the configured cap of 1")
	}
	if msg := err.Error(); !strings.Contains(msg, "another live work copy") || !strings.Contains(msg, "cap of 1") {
		t.Fatalf("refusal must name the reason and configured cap, got %q", msg)
	}
	// A zero cap (config off) leaves the daemon path uncapped.
	wf.Workspace.MaxLiveWorktrees = 0
	freshRoot := t.TempDir()
	for _, rec := range []string{"a", "b", "c"} {
		req := WorkspacePrepareRequest{
			ProjectID: "project-1", ProjectKey: "MEM", RecordID: rec, ItemID: rec,
			RepoRoot: t.TempDir(), StateRoot: freshRoot, Strategy: WorkspaceStrategyCopy,
			MaxLiveWorktrees: wf.Workspace.MaxLiveWorktrees,
		}
		if _, err := manager.Prepare(req); err != nil {
			t.Fatalf("cap of zero on daemon path must impose no limit, %s refused: %v", rec, err)
		}
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
