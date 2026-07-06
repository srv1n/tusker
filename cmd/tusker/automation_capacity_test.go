package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestAutomationPlanRetryQueuedSelfBlockAllowsContinuationCapacity(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Self parked retry", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	seedCapacityRunForTest(t, project, "APP-T-0001", LeaseStateRetryQueued)

	plan := automationPlanForTest(t, vault, "APP-T-0001")
	if plan.Decision != "dispatch" {
		t.Fatalf("expected self parked retry to dispatch, got decision=%s blockers=%#v", plan.Decision, plan.Blockers)
	}
	for _, blocker := range plan.Blockers {
		if strings.Contains(blocker, "active run limit reached") {
			t.Fatalf("self parked retry should not be capacity-blocked by itself, got blockers %#v", plan.Blockers)
		}
	}
}

func TestDaemonRetryQueuedSelfBlockDispatchesContinuationAtProjectLimit(t *testing.T) {
	vault := automationTestVault(t)
	writeCodexSleepWorkflowForCapacityTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Self parked daemon retry", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	seedCapacityRunForTest(t, project, "APP-T-0001", LeaseStateRetryQueued)

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	defer killRunProcess(run)
	if !isDispatchingLeaseState(run.LeaseState) {
		t.Fatalf("expected due self retry to dispatch continuation at project limit, got %#v", run)
	}
	if run.AttemptCount != 2 {
		t.Fatalf("expected continuation attempt count 2, got %#v", run)
	}
}

func TestAutomationPlanActiveRunLimitCountsDistinctCapacityRuns(t *testing.T) {
	for _, state := range []LeaseState{LeaseStateClaimed, LeaseStateRunning, LeaseStateRetryQueued} {
		t.Run(string(state), func(t *testing.T) {
			vault := automationTestVault(t)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "First task", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Second task", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
			makeV7TaskDispatchableForTest(t, vault, "APP-T-0002")
			project := registerAutomationTestProject(t, vault)
			seedCapacityRunForTest(t, project, "APP-T-0001", state)

			plan := automationPlanForTest(t, vault, "APP-T-0002")
			if plan.Decision != "do_not_dispatch" {
				t.Fatalf("expected distinct %s run to block second dispatch, got decision=%s blockers=%#v", state, plan.Decision, plan.Blockers)
			}
			if !containsBlockerSubstring(plan.Blockers, "project active run limit reached") {
				t.Fatalf("expected project active run limit blocker for distinct %s run, got %#v", state, plan.Blockers)
			}
		})
	}
}

func automationPlanForTest(t *testing.T, vault, taskID string) automationDispatchPlan {
	t.Helper()
	output := captureStdout(t, func() {
		if err := automationPlanCmd(Args{"vault": vault, "id": taskID, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Plan automationDispatchPlan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Plan
}

func seedCapacityRunForTest(t *testing.T, project RegisteredProject, recordID string, state LeaseState) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	run := RunStatus{
		ProjectID:       project.ProjectID,
		RecordID:        recordID,
		ItemID:          recordID,
		Runner:          string(RunnerCodex),
		Lane:            runLaneExecute,
		LeaseState:      string(state),
		AttemptOutcome:  string(AttemptOutcomeNone),
		ActiveAttemptID: "attempt-" + recordID,
		SessionRef:      "session-" + recordID,
		WorkRevision:    0,
		AttemptCount:    1,
		UpdatedAt:       now,
	}
	if state == LeaseStateRetryQueued {
		run.NextRetryAt = now
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(RunnerSession{
		ProjectID:      project.ProjectID,
		RecordID:       recordID,
		Runner:         run.Runner,
		SessionRef:     run.SessionRef,
		LastMessageRef: "message-" + recordID,
		CurrentItemID:  recordID,
		WorkRevision:   run.WorkRevision,
		LastAttemptID:  run.ActiveAttemptID,
		State:          sessionStateForLeaseState(state),
		Resumable:      true,
		StartedAt:      now,
		LastSeenAt:     now,
	}); err != nil {
		t.Fatal(err)
	}
}

func writeCodexSleepWorkflowForCapacityTest(t *testing.T, vault string) {
	t.Helper()
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf := wfFile.Data
	wf.Agents.Default = string(RunnerCodex)
	if !containsString(wf.Agents.Enabled, string(RunnerCodex)) {
		wf.Agents.Enabled = append(wf.Agents.Enabled, string(RunnerCodex))
	}
	wf.Codex.Command = "python3 -c 'import time; time.sleep(5)'"
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+wfFile.Body); err != nil {
		t.Fatal(err)
	}
}

func containsBlockerSubstring(blockers []string, needle string) bool {
	for _, blocker := range blockers {
		if strings.Contains(blocker, needle) {
			return true
		}
	}
	return false
}
