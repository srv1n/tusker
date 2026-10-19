package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Perf fixtures measure per-poll and per-summary cost; run them with
// TUSKER_PERF=1 go test ./cmd/tusker -run 'TestPerf' -v -count=1.
func skipUnlessPerf(t *testing.T) {
	t.Helper()
	if os.Getenv("TUSKER_PERF") == "" {
		t.Skip("set TUSKER_PERF=1 to run perf fixtures")
	}
}

// pollPerfFixture registers a git-backed project with 100 tasks, a 20-member
// armed wave, 300 terminal runs and two queued live runs.
func pollPerfFixture(t *testing.T) *Daemon {
	t.Helper()
	vault := v7DirectTestVault(t)
	repo := filepath.Dir(vault)
	gitDirOutput(t, repo, "init", "-q")
	gitDirOutput(t, repo, "config", "user.email", "test@example.com")
	gitDirOutput(t, repo, "config", "user.name", "Test")
	members := []string{}
	for i := 1; i <= 100; i++ {
		id := fmt.Sprintf("APP-T-%04d", i)
		if i <= 20 {
			members = append(members, id)
			armedWaveDirectTask(t, vault, id, nil)
		} else {
			writeDirectTask(t, vault, id, "", map[string]any{"status": "done", "readiness": "done", "proof_status": "satisfied", "owned_paths": []any{"cmd/x.go"}})
		}
	}
	writeDirectWave(t, vault, "W-0001", members, map[string]any{"concurrency": 2})
	armWaveForTest(t, vault)
	gitDirOutput(t, repo, "add", "-A")
	gitDirOutput(t, repo, "commit", "-qm", "fixture")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		if err := store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: fmt.Sprintf("OLD-T-%04d", i), ItemID: fmt.Sprintf("OLD-T-%04d", i),
			Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateReleased),
			AttemptOutcome: string(AttemptOutcomeSucceeded), AttemptCount: 1, WorkRevision: 1, Terminal: true,
			UpdatedAt: "2026-01-01T00:00:00Z",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"APP-T-0021", "APP-T-0022"} {
		if err := store.UpsertRun(RunStatus{
			ProjectID: project.ProjectID, RecordID: id, ItemID: id, Runner: string(RunnerCodexExec), Lane: runLaneExecute,
			LeaseState: string(LeaseStateRetryQueued), AttemptCount: 1, WorkRevision: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = store.Close()
	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	daemon.fairDispatchRun = func(_ context.Context, _ RegisteredProject, _ WorkflowFile, _ Note, run RunStatus, _, _ string) (RunStatus, bool, bool, error) {
		return run, false, false, nil
	}
	return daemon
}

func TestPerfPollOnce(t *testing.T) {
	skipUnlessPerf(t)
	daemon := pollPerfFixture(t)
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := daemon.PollOnce(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
	t.Logf("PollOnce: %s %s", result, result.MemString())
}

func TestPerfRunSummary(t *testing.T) {
	skipUnlessPerf(t)
	server := newServeEmptyNeedsFixture(t)
	run, attempts := writeRunFixture(t, t.TempDir(), 400)
	run.ProjectID, run.RecordID, run.ItemID = "app", "APP-T-0001", "APP-T-0001"
	run.LeaseState, run.Lane, run.ActiveAttemptID = string(LeaseStateReleased), runLaneExecute, attempts[0].AttemptID
	run.StartedAt, run.UpdatedAt = "2026-10-19T12:00:00Z", "2026-10-19T12:10:00Z"
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	snap := serveSnapshot{projectID: "app", notesByID: map[string]Note{}}
	server.now = func() time.Time { return time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC) }
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			budget := int64(4 << 20)
			if _, _, err := server.runSummaryCheckedWithFacts(snap, run, true, &budget); err != nil {
				b.Fatal(err)
			}
		}
	})
	t.Logf("runSummaryCheckedWithFacts: %s %s", result, result.MemString())
}

func TestReceiptInvalidationReadsMaterialOnlyForPassReceipts(t *testing.T) {
	pending := Note{Data: map[string]any{"id": "APP-T-0001"}, Body: directTaskBody("APP-T-0001", "Do it.")}
	cause := v7VerificationReceiptInvalidationLazy(pending, func() (string, error) {
		t.Fatal("pending proof read scoped material")
		return "", nil
	})
	if cause == nil || cause.Kind != "missing" {
		t.Fatalf("pending proof: got %#v, want missing", cause)
	}

	vault := v7DirectTestVault(t)
	writeDirectTask(t, vault, "APP-T-0002", "", nil)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	cause = v7VerificationReceiptInvalidationLazy(idx.Tasks["APP-T-0002"], func() (string, error) {
		reads++
		return "", fmt.Errorf("no checkout")
	})
	if reads != 1 || cause == nil || cause.Kind != "unavailable" {
		t.Fatalf("receipted proof: reads=%d cause=%#v, want one read and unavailable", reads, cause)
	}
}

func TestRunStateTailsCacheReusesUnchangedLogs(t *testing.T) {
	run, attempts := writeRunFixture(t, t.TempDir(), 100)
	budgetA, budgetB := int64(1<<20), int64(1<<20)
	first := loadRunStateTailsWithBudget(run, attempts, &budgetA, 50)
	second := loadRunStateTailsWithBudget(run, attempts, &budgetB, 50)
	if len(first.events) == 0 || &first.events[0] != &second.events[0] {
		t.Fatal("unchanged logs were parsed again")
	}
	assertEqual(t, budgetA, budgetB, "cache hit consumes the same byte budget")

	if err := os.WriteFile(run.EventSinkPath, []byte(`{"at":"2026-10-19T13:00:00Z","kind":"agent_message","payload":{"text":"later"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	budgetC := int64(1 << 20)
	third := loadRunStateTailsWithBudget(run, attempts, &budgetC, 50)
	if len(third.events) != 1 || third.key == first.key {
		t.Fatalf("rewritten log served stale tails: %d events", len(third.events))
	}
}

func TestApplyInputRecordIDsNamesOnlyRecordsWithInputs(t *testing.T) {
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, input := range []RuntimeApplyInput{
		{ProjectID: "p", RecordID: "APP-T-0002", JobID: "job", Path: "/tmp/a.patch", Sha256: "a"},
		{ProjectID: "q", RecordID: "APP-T-0003", JobID: "job", Path: "/tmp/b.patch", Sha256: "b"},
	} {
		if _, err := store.UpsertApplyInput(input); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ApplyInputRecordIDs("p")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, map[string]bool{"APP-T-0002": true}, got, "records with apply inputs")
}
