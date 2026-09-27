package main

import (
	"encoding/json"
	"testing"
)

func TestTaskStateTable(t *testing.T) {
	run := func(op, lease string, attempts int, code string) *taskStateRun {
		return &taskStateRun{Lane: "execute", LeaseState: lease, AttemptCount: attempts, ReasonCode: code, Operator: runOperatorState{State: op}}
	}
	armed := func(f taskStateFacts) taskStateFacts { f.WaveID, f.WaveAuth = "W-1", "armed"; return f }
	cases := []struct {
		name        string
		facts       taskStateFacts
		state, code string
	}{
		{"draft", taskStateFacts{Status: "backlog", Readiness: "draft"}, "backlog", "draft"},
		{"not armed", taskStateFacts{Status: "ready", Readiness: "ready", WaveID: "W-1", WaveAuth: "disarmed"}, "backlog", "not_armed"},
		{"backlog outside a wave", taskStateFacts{Status: "backlog", Readiness: "ready"}, "backlog", "not_planned"},
		{"queued", armed(taskStateFacts{Status: "ready", Readiness: "ready"}), "planned", "queued"},
		{"authorized wave", taskStateFacts{Status: "backlog", Readiness: "ready", WaveID: "W-1", WaveAuth: "authorized"}, "planned", "queued"},
		{"armed backlog member", armed(taskStateFacts{Status: "backlog", Readiness: "ready"}), "planned", "queued"},
		{"waiting on dep", armed(taskStateFacts{Status: "ready", Readiness: "ready", WaitingOn: "waiting on ALP-T-0001"}), "planned", "waiting_on"},
		{"unclaimed lease", taskStateFacts{Status: "ready", Run: run("queued", "unclaimed", 0, "")}, "planned", "queued"},
		{"first attempt", taskStateFacts{Status: "ready", Run: run("working", "running", 1, "")}, "working", "first_attempt"},
		{"quiet is still working", taskStateFacts{Status: "rework", Run: run("quiet", "running", 1, "")}, "working", "rework"},
		{"attempt 3", taskStateFacts{Status: "ready", Run: run("working", "running", 3, "")}, "working", "attempt"},
		{"retry queued", taskStateFacts{Status: "ready", Run: run("queued", "retry_queued", 2, "")}, "working", "retrying"},
		{"worker question beats queued lease", taskStateFacts{Status: "ready", Question: "Which port?", Run: run("queued", "retry_queued", 1, "")}, "needs_input", "question"},
		{"permission", taskStateFacts{Status: "ready", HasPermission: true, Permission: "write /etc", Run: run("waiting_on_you", "running", 1, "")}, "needs_input", "permission"},
		{"human gate", taskStateFacts{Status: "backlog", GateText: "Pick a name"}, "needs_input", "gate"},
		{"crashed", taskStateFacts{Status: "ready", Run: run("failed", "parked_no_progress", 6, "provider_error")}, "blocked", "crashed"},
		{"lost worker", taskStateFacts{Status: "ready", Run: run("lost", "released", 2, "process_lost")}, "blocked", "crashed"},
		{"outside problem", taskStateFacts{Status: "ready", Run: run("blocked", "released", 1, "auth_expired")}, "blocked", "outside_problem"},
		{"budget", taskStateFacts{Status: "ready", Run: run("failed", "parked_budget", 1, "")}, "blocked", "outside_problem"},
		{"not allowed", taskStateFacts{Status: "ready", Run: run("blocked", "released", 1, "sandbox_denied")}, "blocked", "not_allowed"},
		{"stopped by owner", taskStateFacts{Status: "ready", Run: run("stopped", "interrupted", 1, "")}, "blocked", "paused"},
		{"wave paused", taskStateFacts{Status: "ready", WaveID: "W-1", WaveAuth: "paused"}, "blocked", "paused"},
		{"reviewing", taskStateFacts{Status: "review", Run: run("finished", "released", 1, "")}, "in_review", "reviewing"},
		{"review lane running", taskStateFacts{Status: "review", Run: &taskStateRun{Lane: "review", LeaseState: "running", Operator: runOperatorState{State: "working"}}}, "in_review", "reviewing"},
		{"landing", taskStateFacts{Status: "review", GateText: "Land the wave"}, "in_review", "landing"},
		{"done", taskStateFacts{Status: "done", Run: run("failed", "released", 3, "")}, "done", ""},
		{"superseded", taskStateFacts{Status: "superseded", SupersededBy: "ALP-T-0009"}, "canceled", "superseded"},
	}
	for _, tc := range cases {
		got := deriveTaskState(tc.facts)
		if got.State != tc.state || got.ReasonCode != tc.code {
			t.Errorf("%s: got %s/%s (%q), want %s/%s", tc.name, got.State, got.ReasonCode, got.Reason, tc.state, tc.code)
		}
		if got.Label == "" || got.Category == "" || got.NextActor == "" {
			t.Errorf("%s: incomplete record %#v", tc.name, got)
		}
	}
	if got := deriveTaskState(taskStateFacts{Status: "ready", Question: "Which port?"}); got.Reason != "Which port?" {
		t.Fatalf("needs input reason must be the question, got %q", got.Reason)
	}
}

func TestTaskStateWaveMostUrgent(t *testing.T) {
	got := aggregateWaveState([]taskState{
		newTaskState("working", "", "", ""), newTaskState("working", "", "", ""),
		newTaskState("blocked", "crashed", "", ""), newTaskState("done", "", "", ""),
	})
	if got.State != "blocked" || got.Reason != "1 blocked, 2 working, 1 done" {
		t.Fatalf("wave state = %s %q", got.State, got.Reason)
	}
}

// S5: the default cap is 6 attempts; a run parked at the cap shows Blocked crashed.
func TestTaskStateCrashCapSixAttempts(t *testing.T) {
	if wf := defaultWorkflow(); wf.Retry.MaxAttempts != 6 {
		t.Fatalf("default retry cap = %d, want 6", wf.Retry.MaxAttempts)
	}
	wf := defaultWorkflow()
	d := &Daemon{}
	parked, capped := d.enforceAttemptCreationCap(wf, RunStatus{AttemptCount: 6, LeaseState: string(LeaseStateRetryQueued)}, attemptCreationRetry, "provider failed")
	if !capped || LeaseState(parked.LeaseState) != LeaseStateParkedNoProgress {
		t.Fatalf("attempt 6 not parked: %#v", parked)
	}
	if _, capped := d.enforceAttemptCreationCap(wf, RunStatus{AttemptCount: 5}, attemptCreationRetry, ""); capped {
		t.Fatal("attempt 5 must still retry")
	}
	state := taskStateRunFor(parked, false, false)
	got := deriveTaskState(taskStateFacts{Status: "ready", Run: state, MaxAttempts: 6})
	if got.State != "blocked" || got.ReasonCode != "crashed" {
		t.Fatalf("parked run state = %#v (operator %s)", got, state.Operator.State)
	}
}

// S2: tusker show --json and tusker next --json carry the state record.
func TestTaskStateShowAndNextJSON(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "State task", "risk": "low", "priority": "p1", "owned-paths": "src", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001", Lane: "execute", LeaseState: string(LeaseStateRetryQueued), AttemptCount: 2, NextRetryAt: "2099-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	var shown struct {
		State *taskState `json:"state"`
	}
	out := captureStdout(t, func() {
		if err := showCmd(Args{"vault": vault, "id": "APP-T-0001", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.State == nil || shown.State.State != "working" || shown.State.ReasonCode != "retrying" {
		t.Fatalf("show --json state = %#v", shown.State)
	}
	var next struct {
		State taskState `json:"state"`
	}
	out = captureStdout(t, func() {
		if err := nextCmd(Args{"vault": vault, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	if err := json.Unmarshal([]byte(out), &next); err != nil {
		t.Fatal(err)
	}
	if next.State.State != "working" || next.State.Label != "Working" {
		t.Fatalf("next --json state = %#v", next.State)
	}
}
