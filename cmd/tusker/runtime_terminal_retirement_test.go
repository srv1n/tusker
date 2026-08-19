package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveRunnerRetirementRefusesBeforeIdentityClear(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", LeaseState: string(LeaseStateRunning), ProcessPID: os.Getpid(), ProcessPGID: processGroupID(os.Getpid())}
	mustUpsertRun(t, store, run)
	_, err = retireCanonicalRuntimeRows(store, DefaultStateRoot(), project.ProjectID, run.ItemID, "done", "test", "live", time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "runner is still live") {
		t.Fatalf("expected live refusal, got %v", err)
	}
	latest := latestRunForRecord(t, store, project.ProjectID, run.RecordID)
	if latest.ProcessPID == 0 {
		t.Fatalf("live process identity was cleared: %#v", latest)
	}
}

func TestCloseRetiresHeldRuntimeRow(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Close runtime", "risk": "low", "priority": "p0", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestCloseRetiresHeldRuntimeRow -count=1", "result": "pass", "note": "Close retirement fixture passed."}, v7TestVerificationMutation)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:test"}, statusV7Cmd)
	project := registerAutomationTestProject(t, vault)

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	mustUpsertRun(t, store, RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        "APP-T-0001",
		ItemID:          "APP-T-0001",
		Runner:          string(RunnerCodexExec),
		Lane:            runLaneExecute,
		LeaseState:      string(LeaseStateRetryQueued),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-close-retire",
		SessionRef:      "session-close-retire",
		AttemptCount:    1,
		UpdatedAt:       "2026-07-08T00:00:00Z",
	})
	_ = store.Close()

	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}

	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := latestRunForRecord(t, store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "closed runtime lease")
	assertEqual(t, true, run.Terminal, "closed runtime terminal")
	if !strings.Contains(run.LastError, "retired by close ceremony: canonical status done") {
		t.Fatalf("expected close ceremony audit reason, got %#v", run)
	}
	events, err := readText(run.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(events, "run_retired") || !strings.Contains(events, "close ceremony") || !strings.Contains(events, "canonical status done") {
		t.Fatalf("retire event missing close audit fields: %s", events)
	}
}

func TestReconcileRetiresTerminalRuntimeRows(t *testing.T) {
	for _, status := range []string{"done", "cancelled", "backlog"} {
		t.Run(status, func(t *testing.T) {
			vault := automationTestVault(t)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Reconcile runtime", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			fields := map[string]any{
				"status":     status,
				"readiness":  status,
				"next_owner": "none",
			}
			if status == "backlog" {
				fields["readiness"] = "held"
			}
			setAutomationV7TaskFields(t, vault, "APP-T-0001", fields)
			project := registerAutomationTestProject(t, vault)
			store, err := OpenRuntimeStore(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			mustUpsertRun(t, store, RunStatus{
				ProjectID:       project.ProjectID,
				RecordID:        "APP-T-0001",
				ItemID:          "APP-T-0001",
				Runner:          string(RunnerCodexExec),
				Lane:            runLaneExecute,
				LeaseState:      string(LeaseStateRetryQueued),
				AttemptOutcome:  string(AttemptOutcomeNone),
				ActiveAttemptID: "attempt-reconcile-retire",
				SessionRef:      "session-reconcile-retire",
				AttemptCount:    1,
				UpdatedAt:       "2026-07-08T00:00:00Z",
			})
			_ = store.Close()

			daemon, err := NewDaemon(DefaultStateRoot())
			if err != nil {
				t.Fatal(err)
			}
			defer daemon.Close()
			if err := daemon.PollOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
			assertEqual(t, string(LeaseStateReleased), run.LeaseState, "reconciled runtime lease")
			assertEqual(t, true, run.Terminal, "reconciled runtime terminal")
			want := "retired by daemon:reconcile: canonical status " + status
			if !strings.Contains(run.LastError, want) {
				t.Fatalf("expected reconcile audit reason %q, got %#v", want, run)
			}
			events, err := readText(run.EventSinkPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(events, "run_retired") || !strings.Contains(events, "daemon:reconcile") || !strings.Contains(events, "canonical status "+status) {
				t.Fatalf("retire event missing reconcile audit fields: %s", events)
			}
		})
	}
}

func TestCloseRetirementKeepsInvariantCircuitOpen(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Close circuit", "risk": "low", "priority": "p0", "proof-mode": "inline", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Review still trips", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "_pos1": "APP-T-0001", "covers": "A1", "check": "go test ./cmd/tusker -run TestCloseRetirementKeepsInvariantCircuitOpen -count=1", "result": "pass", "note": "Close circuit fixture passed."}, v7TestVerificationMutation)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "status": "review", "by": "agent:test"}, statusV7Cmd)
	project := registerAutomationTestProject(t, vault)

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC)
	mustUpsertRun(t, store, RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0001",
		ItemID:         "APP-T-0001",
		Runner:         string(RunnerCodexExec),
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateRetryQueued),
		AttemptOutcome: string(AttemptOutcomeNone),
		AttemptCount:   1,
		UpdatedAt:      now.Format(time.RFC3339),
	})
	_ = store.Close()

	if err := closeV7Cmd(Args{"vault": vault, "quiet": "true", "id": "APP-T-0001", "by": "reviewer:agent", "reason": "accepted", "local": "true"}); err != nil {
		t.Fatal(err)
	}

	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store}
	clean, err := daemon.refreshInvariantCircuitStatus(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckHeldLeaseDispatchEligible}, "2026-07-08T00:00:00Z", "2026-07-08T00:00:01Z", now, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, false, clean.Open, "close-retired row keeps circuit closed")

	setAutomationV7TaskFields(t, vault, "APP-T-0002", map[string]any{"status": "review", "readiness": "waiting_on_review", "next_owner": "reviewer"})
	mustUpsertRun(t, store, RunStatus{
		ProjectID:      project.ProjectID,
		RecordID:       "APP-T-0002",
		ItemID:         "APP-T-0002",
		Runner:         string(RunnerCodexExec),
		Lane:           runLaneExecute,
		LeaseState:     string(LeaseStateRetryQueued),
		AttemptOutcome: string(AttemptOutcomeNone),
		AttemptCount:   1,
		UpdatedAt:      now.Format(time.RFC3339),
	})
	open, err := daemon.refreshInvariantCircuitStatus(sentinelSnapshotForTest(t, store, project, vault, []string{invariantCheckHeldLeaseDispatchEligible}, "2026-07-08T00:00:01Z", "2026-07-08T00:00:02Z", now, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, open.Open, "unreached ineligible row still opens circuit")
	if len(open.Violations) == 0 || !strings.Contains(open.Violations[0].Detail, "not dispatch-eligible") {
		t.Fatalf("expected last-resort circuit violation, got %#v", open.Violations)
	}
}
