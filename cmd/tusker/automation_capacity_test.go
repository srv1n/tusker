package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
	for _, state := range []LeaseState{LeaseStateClaimed, LeaseStateRunning} {
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

func TestQueuedRunsDoNotConsumeActiveCap(t *testing.T) {
	vault := automationTestVault(t)
	taskIDs := make([]string, 0, 9)
	for i := 1; i <= 9; i++ {
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": fmt.Sprintf("Cap fixture %d", i), "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
		taskID := fmt.Sprintf("APP-T-%04d", i)
		makeV7TaskDispatchableForTest(t, vault, taskID)
		taskIDs = append(taskIDs, taskID)
	}
	setProjectActiveRunCapForCapacityTest(t, vault, 5)
	project := registerAutomationTestProject(t, vault)
	setGlobalActiveRunLimitForCapacityTest(t, 5)
	seedCapacityRunForTest(t, project, taskIDs[0], LeaseStateRunning)
	for _, taskID := range taskIDs[1:] {
		seedCapacityRunForTest(t, project, taskID, LeaseStateRetryQueued)
	}

	// Contract A1: with cap 5, one running row, and eight retry_queued rows the
	// queue must not block its own dispatch (the live-locked shape reported
	// "active run limit reached (9/5)").
	plan := automationPlanForTest(t, vault, taskIDs[1])
	if plan.Decision != "dispatch" {
		t.Fatalf("expected queued task to stay dispatchable with 1 running + 8 retry_queued at cap 5, got decision=%s blockers=%#v", plan.Decision, plan.Blockers)
	}
	if containsBlockerSubstring(plan.Blockers, "active run limit reached") {
		t.Fatalf("queued rows must not consume active-run capacity, got blockers %#v", plan.Blockers)
	}

	// In-tick accounting: each successful dispatch of a queued row increments the
	// running tally (retry_queued -> claimed is +1), so one pass dispatches until
	// 5 leases are claimed/running and the next queued row waits.
	runs := map[string]RunStatus{taskIDs[0]: {RecordID: taskIDs[0], LeaseState: string(LeaseStateRunning)}}
	for _, taskID := range taskIDs[1:] {
		runs[taskID] = RunStatus{RecordID: taskID, LeaseState: string(LeaseStateRetryQueued)}
	}
	active := countDispatchCapacityProjectRuns(runs)
	assertEqual(t, 1, active, "pre-dispatch active count")
	dispatched, waiting := 0, 0
	for _, taskID := range taskIDs[1:] {
		run := runs[taskID]
		if dispatchCapacityLimitReached(active, 5, run) {
			waiting++
			continue
		}
		before := run
		run.LeaseState = string(LeaseStateClaimed)
		runs[taskID] = run
		active += dispatchCapacityRunDelta(before, run)
		dispatched++
	}
	assertEqual(t, 4, dispatched, "queued rows dispatched until cap")
	assertEqual(t, 4, waiting, "queued rows waiting for slots")
	assertEqual(t, 5, active, "in-tick active tally at cap")
	assertEqual(t, 5, countDispatchCapacityProjectRuns(runs), "claimed/running rows at cap")
}

func TestActiveCapCountsRunningOnly(t *testing.T) {
	// Contract A2: the capacity counter ignores every non-executing lease state.
	runs := map[string]RunStatus{
		"T-RUNNING": {RecordID: "T-RUNNING", LeaseState: string(LeaseStateRunning)},
		"T-CLAIMED": {RecordID: "T-CLAIMED", LeaseState: string(LeaseStateClaimed)},
	}
	list := []RunStatus{runs["T-RUNNING"], runs["T-CLAIMED"]}
	for _, state := range []LeaseState{LeaseStateRetryQueued, LeaseStateReleased, LeaseStateInterrupted, LeaseStateParkedNoProgress, LeaseStateParkedBudget, LeaseStateUnclaimed} {
		record := "T-" + strings.ToUpper(string(state))
		run := RunStatus{RecordID: record, LeaseState: string(state)}
		runs[record] = run
		list = append(list, run)
	}
	assertEqual(t, 2, countDispatchCapacityProjectRuns(runs), "project capacity counts claimed and running only")
	assertEqual(t, 2, countDispatchCapacityRuns(list), "global capacity counts claimed and running only")

	// Blocker text stays honest: with project cap 1, one running row, and two
	// queued rows the blocker names the running count only, never a figure
	// inflated by queued rows.
	vault := automationTestVault(t)
	for i := 1; i <= 4; i++ {
		mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": fmt.Sprintf("Blocker fixture %d", i), "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
		makeV7TaskDispatchableForTest(t, vault, fmt.Sprintf("APP-T-%04d", i))
	}
	project := registerAutomationTestProject(t, vault)
	seedCapacityRunForTest(t, project, "APP-T-0001", LeaseStateRunning)
	seedCapacityRunForTest(t, project, "APP-T-0003", LeaseStateRetryQueued)
	seedCapacityRunForTest(t, project, "APP-T-0004", LeaseStateRetryQueued)

	plan := automationPlanForTest(t, vault, "APP-T-0002")
	if plan.Decision != "do_not_dispatch" {
		t.Fatalf("expected running row to hold the project cap, got decision=%s blockers=%#v", plan.Decision, plan.Blockers)
	}
	if !containsBlockerSubstring(plan.Blockers, "project active run limit reached (1/1)") {
		t.Fatalf("expected blocker to report the running count only, got %#v", plan.Blockers)
	}
	if containsBlockerSubstring(plan.Blockers, "global active run limit reached") {
		t.Fatalf("one running row must not trip the global limit of 2, got %#v", plan.Blockers)
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
	command := "python3 -c 'import time; time.sleep(5)'"
	wf := wfFile.Data
	wf.Agents.Default = string(RunnerCodex)
	if !containsString(wf.Agents.Enabled, string(RunnerCodex)) {
		wf.Agents.Enabled = append(wf.Agents.Enabled, string(RunnerCodex))
	}
	wf.Codex.Command = command
	raw, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(raw))+"\n---\n"+wfFile.Body); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(filepath.Dir(vault), "tusker.yaml")
	text, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	const defaultCommand = "command: codex exec --json --skip-git-repo-check -"
	if strings.Contains(text, defaultCommand) {
		text = strings.Replace(text, defaultCommand, "command: "+command, 1)
		if err := writeText(configPath, text); err != nil {
			t.Fatal(err)
		}
	}
}

func setProjectActiveRunCapForCapacityTest(t *testing.T, vault string, limit int) {
	t.Helper()
	// The bootstrap-written tusker.yaml automation config overlays WORKFLOW.md
	// (applyTuskerAutomationConfig), so the per-project cap must change there.
	configPath := filepath.Join(filepath.Dir(vault), "tusker.yaml")
	text, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	const defaultLine = "max_active_runs_per_project: 1"
	if !strings.Contains(text, defaultLine) {
		t.Fatalf("expected %q in %s", defaultLine, configPath)
	}
	text = strings.Replace(text, defaultLine, fmt.Sprintf("max_active_runs_per_project: %d", limit), 1)
	if err := writeText(configPath, text); err != nil {
		t.Fatal(err)
	}
}

func setGlobalActiveRunLimitForCapacityTest(t *testing.T, limit int) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetGlobalActiveRunLimit(limit); err != nil {
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
