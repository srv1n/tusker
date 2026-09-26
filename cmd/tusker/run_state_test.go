package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunOperatorState(t *testing.T) {
	now := time.Date(2026, 9, 23, 15, 0, 0, 0, time.UTC)
	base := runOperatorFacts{LeaseState: string(LeaseStateRunning), OwnerAlive: true, LastActivityAt: now.Add(-10 * time.Minute).Format(time.RFC3339Nano), LastHeartbeatAt: now.Add(-5 * time.Second).Format(time.RFC3339Nano)}
	blocked := &runOperatorReason{Code: "usage_limit", Class: "blocked", Guidance: "Wait, then Continue.", Source: "driver"}
	tests := []struct {
		name, want string
		change     func(*runOperatorFacts)
	}{
		{"heartbeat alone is quiet", "quiet", func(*runOperatorFacts) {}},
		{"in flight tool is working", "working", func(f *runOperatorFacts) { f.ToolInFlight = true }},
		{"fresh activity is working", "working", func(f *runOperatorFacts) { f.LastActivityAt = now.Add(-time.Second).Format(time.RFC3339Nano) }},
		{"future activity is working", "working", func(f *runOperatorFacts) { f.LastActivityAt = now.Add(time.Second).Format(time.RFC3339Nano) }},
		{"open question wins", "waiting_on_you", func(f *runOperatorFacts) { f.OpenQuestionID = "q1"; f.ToolInFlight = true }},
		{"stale question does not pin lost owner", "lost", func(f *runOperatorFacts) { f.OpenQuestionID = "q1"; f.OwnerAlive = false }},
		{"answered question falls through", "working", func(f *runOperatorFacts) {
			f.OpenQuestionID = ""
			f.LastActivityAt = now.Add(-time.Second).Format(time.RFC3339Nano)
		}},
		{"provider permission wait wins", "waiting_on_you", func(f *runOperatorFacts) { f.PermissionWait = true }},
		{"waiting outcome wins", "waiting_on_you", func(f *runOperatorFacts) { f.Outcome = string(AttemptOutcomeWaitingForHuman) }},
		{"owner identity mismatch is lost", "lost", func(f *runOperatorFacts) { f.OwnerAlive = false }},
		{"unknown outcome is lost", "lost", func(f *runOperatorFacts) {
			f.OwnerAlive = false
			f.Terminal = true
			f.Outcome = string(AttemptOutcomeUnknown)
		}},
		{"blocked terminal", "blocked", func(f *runOperatorFacts) { f.OwnerAlive = false; f.Terminal = true; f.Reason = blocked }},
		{"failed terminal", "failed", func(f *runOperatorFacts) {
			f.OwnerAlive = false
			f.Terminal = true
			f.Outcome = string(AttemptOutcomeFailed)
		}},
		{"stopped terminal", "stopped", func(f *runOperatorFacts) {
			f.OwnerAlive = false
			f.Terminal = true
			f.Outcome = string(AttemptOutcomeCancelled)
		}},
		{"queued", "queued", func(f *runOperatorFacts) { f.OwnerAlive = false; f.LeaseState = string(LeaseStateRetryQueued) }},
		{"finished", "finished", func(f *runOperatorFacts) {
			f.OwnerAlive = false
			f.Terminal = true
			f.Outcome = string(AttemptOutcomeSucceeded)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts := base
			tt.change(&facts)
			got := deriveRunOperatorState(facts, now, defaultRunQuietAfter)
			if got.State != tt.want {
				t.Fatalf("state = %q, want %q", got.State, tt.want)
			}
			if got.Evidence.LastHeartbeatAt != facts.LastHeartbeatAt {
				t.Fatal("heartbeat evidence lost")
			}
			first, _ := json.Marshal(got)
			second, _ := json.Marshal(deriveRunOperatorState(facts, now, defaultRunQuietAfter))
			if string(first) != string(second) {
				t.Fatal("projection is not deterministic")
			}
		})
	}
	quiet := deriveRunOperatorState(base, now, defaultRunQuietAfter)
	for _, forbidden := range []string{"hung", "stalled", "dead", "stuck"} {
		if strings.Contains(strings.ToLower(runOperatorStateLine(quiet, now)), forbidden) {
			t.Fatalf("quiet guidance contains %q", forbidden)
		}
	}
	boundary := base
	boundary.LastActivityAt = now.Add(-3 * time.Minute).Format(time.RFC3339Nano)
	if got := deriveRunOperatorState(boundary, now, 4*time.Minute); got.State != "working" || got.QuietAfterSec != 240 {
		t.Fatalf("quiet override: %+v", got)
	}
}

func TestRunOperatorStatePrecedence(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name  string
		facts runOperatorFacts
		want  string
	}{
		{"finished stale question", runOperatorFacts{Terminal: true, Outcome: string(AttemptOutcomeSucceeded), OpenQuestionID: "old"}, "finished"},
		{"yield parked", runOperatorFacts{Terminal: true, Outcome: string(AttemptOutcomeWaitingForHuman)}, "waiting_on_you"},
		{"queued lost reason", runOperatorFacts{LeaseState: string(LeaseStateRetryQueued), Reason: &runOperatorReason{Class: "lost"}}, "queued"},
		{"queued unknown outcome", runOperatorFacts{LeaseState: string(LeaseStateRetryQueued), Outcome: string(AttemptOutcomeUnknown)}, "queued"},
		{"resolved owner permission", runOperatorFacts{LeaseState: string(LeaseStateRunning), OwnerAlive: true, PermissionWait: true}, "waiting_on_you"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveRunOperatorState(tc.facts, now, defaultRunQuietAfter).State; got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRunOperatorStateTypedToolStatus(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-TYPED")
	_ = store
	run.EventSinkPath = filepath.Join(t.TempDir(), "events.jsonl")
	for _, status := range []struct {
		value string
		want  bool
	}{{"in_progress", true}, {"completed", false}} {
		line := `{"kind":"tool_call","payload":{"tool_call_id":"tool-1","status":"` + status.value + `","text":"still running"}}` + "\n"
		if err := os.WriteFile(run.EventSinkPath, []byte(line), 0600); err != nil {
			t.Fatal(err)
		}
		if got := runToolInFlightFromTails(loadRunStateTails(run, nil)); got != status.want {
			t.Fatalf("status %s: in flight=%v", status.value, got)
		}
	}
}

func TestRunsInspectOperatorState(t *testing.T) {
	store, run := ownershipStoreFixture(t, "APP-T-STATE")
	started, ok := processStartTime(os.Getpid())
	if !ok {
		t.Skip("process start identity unavailable")
	}
	run.LeaseState = string(LeaseStateRunning)
	run.ProcessPID = os.Getpid()
	run.ProcessPGID = syscall.Getpgrp()
	run.ProcessStartedAt = started
	now := time.Now().UTC()
	run.RawLogPath = filepath.Join(t.TempDir(), "activity.jsonl")
	activityAt := now.Add(-3 * time.Minute).Format(time.RFC3339Nano)
	if err := os.WriteFile(run.RawLogPath, []byte(`{"type":"item.completed","timestamp":"`+activityAt+`","item":{"type":"agent_message","text":"done"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil || current == nil {
		t.Fatalf("find run: %v", err)
	}
	quiet, err := buildRunInspectionWithQuietAfter(store, current, now, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if quiet.OperatorState.State != "quiet" {
		t.Fatalf("default boundary: %+v", quiet.OperatorState)
	}
	working, err := buildRunInspectionWithQuietAfter(store, current, now, 4*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if working.OperatorState.State != "working" || working.OperatorState.QuietAfterSec != 240 {
		t.Fatalf("override boundary: %+v", working.OperatorState)
	}
	data, err := json.Marshal(working)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"operator_state"`, `"state":"working"`, `"reason":null`, `"evidence"`, `"owner_alive":true`, `"quiet_after_sec":240`} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("inspect JSON missing %s: %s", field, data)
		}
	}
}
