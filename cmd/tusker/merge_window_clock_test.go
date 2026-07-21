package main

import (
	"testing"
	"time"
)

func waitBatchGateSettled(t *testing.T, store *RuntimeStore, projectID string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		latest, err := store.latestBatchGateRun(projectID)
		if err != nil {
			t.Fatalf("latest batch gate run: %v", err)
		}
		if latest != nil && latest.Status != "running" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("batch gate run did not settle")
}

func countBatchGateRuns(t *testing.T, store *RuntimeStore, projectID string) int {
	t.Helper()
	var count int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM batch_gate_runs WHERE project_id = ?`, []any{projectID}, &count); err != nil {
		t.Fatalf("count batch gate runs: %v", err)
	}
	return count
}

func TestMergeWindowParse(t *testing.T) {
	windows, err := parseMergeWindows([]string{"13:00", "19:00", "03:00"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	want := []mergeWindow{{3, 0}, {13, 0}, {19, 0}}
	if len(windows) != len(want) {
		t.Fatalf("expected %d windows, got %#v", len(want), windows)
	}
	for i, w := range want {
		if windows[i] != w {
			t.Fatalf("window %d = %#v, want %#v (order not ascending?)", i, windows[i], w)
		}
	}
	for _, bad := range []string{"25:00", "1300", "12:60", "-1:00", "", "12:0", "9:00"} {
		_, err := parseMergeWindows([]string{"13:00", bad})
		if err == nil {
			t.Fatalf("malformed entry %q was silently accepted", bad)
		}
		typed, ok := err.(*TuskerError)
		if !ok || typed.Code != errorConfigInvalid {
			t.Fatalf("malformed entry %q not rejected with named config error: %#v", bad, err)
		}
	}
}

func TestMergeWindowNextAcrossMidnight(t *testing.T) {
	windows, err := parseMergeWindows([]string{"13:00", "19:00", "03:00"})
	if err != nil {
		t.Fatal(err)
	}
	loc := time.Local
	at := func(h, m int) time.Time { return time.Date(2026, 7, 21, h, m, 0, 0, loc) }

	// From 20:00 the next window is 03:00 the next day.
	if got := mergeWindowNext(windows, at(20, 0)); !got.Equal(time.Date(2026, 7, 22, 3, 0, 0, 0, loc)) {
		t.Fatalf("from 20:00 next = %v, want 03:00 next day", got)
	}
	// From 03:30 the next window is 13:00 the same day.
	if got := mergeWindowNext(windows, at(3, 30)); !got.Equal(at(13, 0)) {
		t.Fatalf("from 03:30 next = %v, want 13:00 same day", got)
	}
	// Exact boundary resolves deterministically to the following window.
	if got := mergeWindowNext(windows, at(13, 0)); !got.Equal(at(19, 0)) {
		t.Fatalf("from exact 13:00 next = %v, want 19:00", got)
	}
	// Single-window set still crosses midnight correctly.
	single, err := parseMergeWindows([]string{"03:00"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mergeWindowNext(single, at(4, 0)); !got.Equal(time.Date(2026, 7, 22, 3, 0, 0, 0, loc)) {
		t.Fatalf("single-window from 04:00 next = %v, want 03:00 next day", got)
	}

	// most-recent companion is used by the scheduler; verify the mirror cases.
	if got := mergeWindowMostRecent(windows, at(20, 0)); !got.Equal(at(19, 0)) {
		t.Fatalf("from 20:00 most-recent = %v, want 19:00 same day", got)
	}
	if got := mergeWindowMostRecent(windows, at(3, 30)); !got.Equal(at(3, 0)) {
		t.Fatalf("from 03:30 most-recent = %v, want 03:00 same day", got)
	}
	// Before the first window of the day, most-recent is yesterday's last.
	if got := mergeWindowMostRecent(windows, at(2, 0)); !got.Equal(time.Date(2026, 7, 20, 19, 0, 0, 0, loc)) {
		t.Fatalf("from 02:00 most-recent = %v, want 19:00 prior day", got)
	}
}

func TestMergeWindowDaemonOnce(t *testing.T) {
	repo := orchestrationGitRepo(t)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{store: store}
	wf := defaultWorkflow()
	wf.Orchestration.BatchGate = BatchGatePolicy{Enabled: true, Windows: []string{"13:00", "19:00", "03:00"}, Commands: []string{"true"}}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo}
	loc := time.Local
	at := func(h, m int) time.Time { return time.Date(2026, 7, 21, h, m, 0, 0, loc) }

	// In/past an unconsumed window: fires once.
	if err := daemon.scheduleBatchGateIfDue(project, wf, at(13, 30)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store, "app"); got != 1 {
		t.Fatalf("expected 1 run after first window, got %d", got)
	}
	// Same window, later: no-op (at most once per window per day).
	if err := daemon.scheduleBatchGateIfDue(project, wf, at(13, 45)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store, "app"); got != 1 {
		t.Fatalf("window fired more than once per day, got %d runs", got)
	}
	// Between windows (before 19:00): still no-op.
	if err := daemon.scheduleBatchGateIfDue(project, wf, at(18, 59)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store, "app"); got != 1 {
		t.Fatalf("fired between windows, got %d runs", got)
	}
	// Let the first gate finish so the next-window re-fire is gated by the
	// window boundary, not by the still-running guard.
	waitBatchGateSettled(t, store, "app")
	// Next window reached: fires again.
	if err := daemon.scheduleBatchGateIfDue(project, wf, at(19, 5)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store, "app"); got != 2 {
		t.Fatalf("expected 2 runs after second window, got %d", got)
	}
	waitBatchGateSettled(t, store, "app")

	// A gate still running from before a window boundary must not be joined by
	// a second concurrent spawn when the next window is reached.
	store2, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	daemon2 := &Daemon{store: store2}
	proj2 := RegisteredProject{ProjectID: "stuck", RepoRoot: repo}
	// Started at 12:00 (before the 13:00 window) and still running.
	if err := store2.saveBatchGateRun(BatchGateRun{ID: "batch-stuck", ProjectID: "stuck", Status: "running", StartedAt: at(12, 0).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := daemon2.scheduleBatchGateIfDue(proj2, wf, at(13, 30)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store2, "stuck"); got != 1 {
		t.Fatalf("running run spanning a window boundary spawned a concurrent gate, got %d runs", got)
	}

	// A run stuck past the grace bound no longer suppresses: the schedule
	// recovers rather than wedging forever.
	store3, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store3.Close()
	daemon3 := &Daemon{store: store3}
	proj3 := RegisteredProject{ProjectID: "wedged", RepoRoot: repo}
	stuckStart := at(13, 30).Add(-(mergeWindowRunningGrace + time.Hour))
	if err := store3.saveBatchGateRun(BatchGateRun{ID: "batch-wedged", ProjectID: "wedged", Status: "running", StartedAt: stuckStart.UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := daemon3.scheduleBatchGateIfDue(proj3, wf, at(13, 30)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store3, "wedged"); got != 2 {
		t.Fatalf("run stuck past grace bound wedged the schedule, got %d runs", got)
	}
	waitBatchGateSettled(t, store3, "wedged")
}

func TestMergeWindowFallbackPeriodHours(t *testing.T) {
	repo := orchestrationGitRepo(t)
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{store: store}
	wf := defaultWorkflow()
	// No windows configured: period_hours path governs scheduling.
	wf.Orchestration.BatchGate = BatchGatePolicy{Enabled: true, PeriodHours: 24, Commands: []string{"true"}}
	project := RegisteredProject{ProjectID: "app", RepoRoot: repo}
	now := time.Date(2026, 7, 21, 13, 30, 0, 0, time.Local)

	if err := daemon.scheduleBatchGateIfDue(project, wf, now); err != nil {
		t.Fatal(err)
	}
	// Within the period: no second run.
	if err := daemon.scheduleBatchGateIfDue(project, wf, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store, "app"); got != 1 {
		t.Fatalf("period path fired more than once inside period, got %d runs", got)
	}
	// Let the first gate finish so the period re-fire is not blocked by the
	// still-running guard rather than by the period itself.
	waitBatchGateSettled(t, store, "app")
	// Past the period: fires again.
	if err := daemon.scheduleBatchGateIfDue(project, wf, now.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := countBatchGateRuns(t, store, "app"); got != 2 {
		t.Fatalf("period path did not fire after period elapsed, got %d runs", got)
	}
	time.Sleep(50 * time.Millisecond)
}
