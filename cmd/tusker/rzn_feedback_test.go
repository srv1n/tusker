package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRZNVerificationTableRejectedAtAuthoring(t *testing.T) {
	vault := v7DirectTestVault(t)
	bad := "## Verification\n\n| Covers | Check | Result |\n| --- | --- | --- |\n| A4 | command: echo ok | cat | pending |\n"
	body := directAuthoringBodyPath(t, vault, "bad.md", bad)
	args := Args{"vault": vault, "quiet": "true", "title": "Table", "work-level": "standard", "owned-paths": "cmd/tusker", "body-file": body}
	if err := newAuthoredV7Task(args); err == nil || !strings.Contains(err.Error(), "A4") {
		t.Fatalf("creation accepted malformed table: %v", err)
	}
	args["body-file"] = directAuthoringBodyPath(t, vault, "good.md", strings.Replace(bad, "ok | cat", `ok \| cat`, 1))
	if err := newAuthoredV7Task(args); err != nil {
		t.Fatal(err)
	}
	if err := taskUpdateCLICmd(Args{"vault": vault, "id": "TSK-T-0001", "body-file": body, "by": "agent:builder"}); err == nil || !strings.Contains(err.Error(), "A4") {
		t.Fatalf("update accepted malformed table: %v", err)
	}
}

func TestRZNVerificationObservesExternalInputChanges(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(t.TempDir(), "external-check-input")
	if err := os.WriteFile(input, []byte("pass"), 0600); err != nil {
		t.Fatal(err)
	}
	command := "test \"$(cat " + shellSingleQuote(input) + ")\" = pass"
	if _, err := runV7VerificationCommand(root, command, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runV7VerificationCommand(root, command, time.Second, nil); err == nil {
		t.Fatal("unchanged source hid changed external verification input")
	}
}

func TestRZNQuietInteractiveTTL(t *testing.T) {
	now := time.Now().UTC()
	store, run := ownershipStoreFixture(t, "APP-T-QUIET-1")
	run.LeaseState, run.LeaseOwner, run.LeaseGeneration = string(LeaseStateClaimed), "agent:editor", 1
	run.LeaseExpiresAt = now.Add(-25 * time.Hour).Format(time.RFC3339)
	run.HandRun, run.ProcessPID, run.ProcessPGID = true, 99999999, 99999999
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE runs SET hand_run = 1 WHERE project_id = ? AND record_id = ?`, run.ProjectID, run.RecordID); err != nil {
		t.Fatal(err)
	}
	if liveness, blocking := holderLiveness(run, now); blocking || liveness != "quiet_interactive_expired" {
		t.Fatalf("expired holder: %s %v", liveness, blocking)
	}
	if changed, err := store.ReclaimExpiredRunLease(run.ProjectID, run.RecordID, now, defaultRunLeaseTTL, "quiet TTL"); err != nil || !changed {
		t.Fatalf("reclaim: %v %v", changed, err)
	}
	after, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil || after.LeaseState != string(LeaseStateReleased) || after.LeaseOwner != "" {
		t.Fatalf("expired session not released: %#v %v", after, err)
	}
	run.ProcessPID, run.ProcessPGID = os.Getpid(), 0
	if quietInteractiveLeaseExpired(run, now) {
		t.Fatal("live process expired")
	}
	if actual, ok := processStartTime(os.Getpid()); ok {
		run.ProcessStartedAt = actual
		if quietInteractiveLeaseExpired(run, now) {
			t.Fatal("matching live identity expired")
		}
		run.ProcessStartedAt = "1900-01-01T00:00:00Z"
		if !quietInteractiveLeaseExpired(run, now) {
			t.Fatal("unrelated reused PID kept quiet lease alive")
		}
	}
}

func TestRZNMissingRustModuleRemedy(t *testing.T) {
	vault, _, _ := adoptFixture(t, "APP-T-0001", map[string]any{"owned_paths": []string{"ingest.rs", "submitted.txt"}})
	if err := os.Mkdir(filepath.Join(v7RepoRoot(vault), "ingest"), 0700); err != nil {
		t.Fatal(err)
	}
	reason := doneRefusalRemedy(vault, "APP-T-0001", "agent:builder", "declared material scope path is unreadable or missing: ingest.rs")
	if !strings.Contains(reason, "--owned-paths 'ingest,submitted.txt'") {
		t.Fatal(reason)
	}
}

func TestRZNDoneWaveRunsChecksAndWritesTaskReceipts(t *testing.T) {
	vault, _, _ := adoptFixture(t, "APP-T-0001", nil)
	first := walkthroughNote(t, vault, "APP-T-0001")
	writeDirectTask(t, vault, "APP-T-0002", "", map[string]any{
		"status": "ready", "readiness": "ready", "source_sha": stringField(first.Data, "source_sha"),
		"work_revision": 1, "owned_paths": []string{"submitted.txt"},
	})
	counter := filepath.Join(t.TempDir(), "executions")
	command := "command: test -f submitted.txt && printf x >> " + shellSingleQuote(counter)
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		rewriteTaskFile(t, vault, id, func(data map[string]any, body string) (map[string]any, string) {
			body = directDispatchableTaskBody(id)
			rows := parseV7VerificationRows(body)
			rows[0].Check, rows[0].Result, rows[0].Notes = command, "pending", ""
			data["proof_status"] = "pending"
			return data, replaceSection(body, "## Verification", renderV7VerificationTable(rows))
		})
	}
	captureStdout(t, func() {
		if err := doneCmd(Args{"vault": vault, "_pos": "APP-T-0001 APP-T-0002", "by": "agent:builder", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	got, err := os.ReadFile(counter)
	if err != nil || string(got) != "xx" {
		t.Fatalf("per-task check executions %q: %v", got, err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		task := walkthroughNote(t, vault, id)
		rows := parseV7VerificationRows(task.Body)
		if stringField(task.Data, "status") != "review" || len(rows) != 1 || rows[0].Result != "pass" || v7VerificationReceiptFields(rows[0]) == nil {
			t.Fatalf("task lacks receipt: %s %#v", id, rows)
		}
		if strings.Contains(rows[0].Notes, "reused within this done invocation") {
			t.Fatalf("arbitrary check observation reused: %#v", rows[0])
		}
	}
}

func TestRZNDoneReclaimsOwnExpiredInteractiveSession(t *testing.T) {
	vault, store, project := adoptFixture(t, "APP-T-0001", nil)
	task := walkthroughNote(t, vault, "APP-T-0001")
	run := RunStatus{ProjectID: project.ProjectID, RecordID: trackerRecordID(task), ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute, WorkspacePath: v7RepoRoot(vault),
		LeaseState: string(LeaseStateClaimed), LeaseOwner: "agent:builder", LeaseGeneration: 1,
		LeaseExpiresAt: time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339),
		HandRun:        true, WorkRevision: 1, AttemptOutcome: string(AttemptOutcomeNone)}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE runs SET hand_run = 1 WHERE project_id = ? AND record_id = ?`, run.ProjectID, run.RecordID); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if err := doneCmd(Args{"vault": vault, "_pos": "APP-T-0001", "by": "agent:builder", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if status := stringField(adoptStatus(t, vault, "APP-T-0001"), "status"); status != "review" {
		t.Fatal("expired session blocked adoption: " + status)
	}
}
