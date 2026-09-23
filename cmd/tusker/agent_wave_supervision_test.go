package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestArchitectWaveSupervisionReports(t *testing.T) {
	vault, store, project := authorityFixture(t)
	project.Enabled, project.Health = true, projectHealthHealthy
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	const taskID = "APP-T-0001"
	writeDirectTask(t, vault, taskID, "W-0001", map[string]any{"status": "ready", "readiness": "ready"})
	writeDirectWave(t, vault, "W-0001", []string{taskID}, nil)
	armWaveForTest(t, vault)
	architect := AgentAddress{Kind: "task", ID: "architect"}
	if _, err := store.PutAgentContact(AgentContact{ProjectID: project.ProjectID, TaskID: "W-0001", Role: "architect", Address: architect}, 0); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{store: store}
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	park := func(reason, retry string) {
		t.Helper()
		if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: taskID, ItemID: taskID, LeaseState: string(LeaseStateParkedNoProgress), LastError: reason, UpdatedAt: old, NextRetryAt: retry}); err != nil {
			t.Fatal(err)
		}
	}
	reports := func() []ArchitectReport {
		t.Helper()
		messages, err := store.ListAgentMessages(project.ProjectID, architect.Kind, architect.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := []ArchitectReport{}
		for _, message := range messages {
			if message.Kind != "wave_result" {
				continue
			}
			var report ArchitectReport
			if err := json.Unmarshal([]byte(message.Body), &report); err != nil {
				t.Fatal(err)
			}
			out = append(out, report)
		}
		return out
	}
	poll := func() {
		t.Helper()
		if err := d.processArchitectWaveReports(project.ProjectID); err != nil {
			t.Fatal(err)
		}
	}

	park("attempt cap reached", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	poll()
	if got := reports(); len(got) != 0 {
		t.Fatalf("future retry reported as stalled: %#v", got)
	}
	park("attempt cap reached", "")
	poll()
	got := reports()
	if len(got) != 1 || got[0].Outcome != "stalled" || len(got[0].PendingQuestions) != 0 || len(got[0].Blockers) != 1 || !strings.Contains(got[0].Blockers[0], "attempt cap reached") {
		t.Fatalf("registered architect did not receive one structural stall report: %#v", got)
	}
	for i := 0; i < 100; i++ {
		poll()
	}
	if got := reports(); len(got) != 1 {
		t.Fatalf("unchanged polls duplicated report: %d", len(got))
	}
	park("new failure reason", "")
	poll()
	got = reports()
	if len(got) != 2 || !strings.Contains(got[1].Blockers[0], "new failure reason") {
		t.Fatalf("material blocker change did not produce one report: %#v", got)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: taskID, ItemID: taskID, LeaseState: string(LeaseStateRunning), LeaseOwner: "live", ActiveAttemptID: "live", LeaseExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	poll()
	if got := reports(); len(got) != 2 {
		t.Fatalf("running member generated a report: %#v", got)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: project.ProjectID, RecordID: taskID, ItemID: taskID, LeaseState: string(LeaseStateUnclaimed), UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	poll()
	if got := reports(); len(got) != 2 {
		t.Fatalf("runnable member generated a report: %#v", got)
	}
	park("paused blocker", "")
	if _, err := directWavePause(vault, store, "W-0001", "human:test"); err != nil {
		t.Fatal(err)
	}
	poll()
	if got := reports(); len(got) != 2 {
		t.Fatalf("paused wave generated a stall report: %#v", got)
	}
}

func TestArchitectWaveSupervisionReportsHardDependencyClosure(t *testing.T) {
	now := time.Now().UTC()
	idx := v7Index{Tasks: map[string]Note{
		"member":   {Data: map[string]any{"id": "member", "status": "ready", "dependencies": []string{"external:hard"}}},
		"external": {Data: map[string]any{"id": "external", "status": "ready"}},
	}}
	snapshot := armedWaveSnapshot{Authorization: "armed", Members: []armedWaveMember{{ID: "member", State: armedWaveDependencyWaiting, Reason: "waiting for dependency external"}}}
	runs := map[string]RunStatus{"external": {LeaseState: string(LeaseStateParkedNoProgress), LastError: "external task failed"}}
	stalled, blockers := architectWaveStalled(snapshot, idx, runs, now)
	if !stalled || len(blockers) != 1 || !strings.Contains(blockers[0], "external task failed") {
		t.Fatalf("hard dependency closure not reported: stalled=%v blockers=%#v", stalled, blockers)
	}
	idx.Tasks["member"] = Note{Data: map[string]any{"id": "member", "status": "ready", "dependencies": []string{"external:soft"}}}
	if stalled, _ := architectWaveStalled(snapshot, idx, runs, now); stalled {
		t.Fatal("soft dependency should not count as parked hard closure")
	}
}

func TestArchitectWaveStalledOnYieldQuestion(t *testing.T) {
	now := time.Now().UTC()
	snapshot := armedWaveSnapshot{Authorization: "armed", Members: []armedWaveMember{{ID: "task", State: armedWaveRunnable}}}
	runs := map[string]RunStatus{"task": {LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeWaitingForHuman), UpdatedAt: now.Format(time.RFC3339Nano)}}
	stalled, blockers := architectWaveStalled(snapshot, v7Index{}, runs, now, map[string]string{"task": "msg-question"})
	if !stalled || len(blockers) != 1 || !strings.Contains(blockers[0], "msg-question") {
		t.Fatalf("yield question not reported: %v %#v", stalled, blockers)
	}
	if stalled, _ := architectWaveStalled(snapshot, v7Index{}, runs, now); stalled {
		t.Fatal("missing question reported as stalled")
	}
}
