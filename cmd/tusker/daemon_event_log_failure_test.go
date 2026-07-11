package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEventLogPersistenceFailureOpensInvariantCircuit(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sinkPath := filepath.Join(stateRoot, "events.jsonl")
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", EventSinkPath: sinkPath}); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{store: store}

	daemon.tripEventLogPersistenceCircuit("supervisor_decision", "APP-T-0001", errors.New("disk full"))
	status, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Open || status.Reason != "event_log_persistence_failure" {
		t.Fatalf("event failure did not open circuit: %#v", status)
	}
	if !strings.Contains(status.Summary, "APP-T-0001") || !strings.Contains(status.Summary, "disk full") {
		t.Fatalf("event failure is not actionable: %#v", status)
	}
	blocker, err := daemon.invariantDispatchBlocker()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(blocker, "event_log_persistence_failure") && !strings.Contains(blocker, "cannot persist supervisor_decision") {
		t.Fatalf("event failure is not exposed to dispatch/status: %q", blocker)
	}
	if !strings.Contains(blocker, "repair event-log storage") || !strings.Contains(blocker, "tusker daemon resume") {
		t.Fatalf("event failure repair hint is not actionable: %q", blocker)
	}
	failures, err := store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || failures[0].EventSinkPath != sinkPath || !strings.Contains(failures[0].Reason, "disk full") {
		t.Fatalf("failed sink was not persisted separately: %#v", failures)
	}
}

func TestEventLogPersistenceFailureSurvivesSentinelRefresh(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sinkPath := filepath.Join(stateRoot, "events.jsonl")
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001", EventSinkPath: sinkPath}); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{store: store}
	daemon.tripEventLogPersistenceCircuit("supervisor_decision", "APP-T-0001", errors.New("disk full"))
	workflow := defaultWorkflow()
	workflow.Runtime.Sentinel.Checks = []string{"injected_unrelated_violation"}
	status, err := daemon.refreshInvariantCircuitStatus(runtimeSentinelSnapshot{
		Projects: []runtimeSentinelProjectSnapshot{{Project: RegisteredProject{ProjectID: "project-1"}, Workflow: workflow}},
		Now:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Open || status.Reason != "event_log_persistence_failure" || !strings.Contains(status.Summary, sinkPath) {
		t.Fatalf("sentinel refresh overwrote event failure: %#v", status)
	}
	checks := strings.Join(status.Checks, ",")
	if !strings.Contains(checks, invariantCheckEventLogPersistence) || !strings.Contains(checks, "injected_unrelated_violation") {
		t.Fatalf("sentinel refresh did not merge circuit causes: %#v", status.Checks)
	}
}

func TestEventLogPersistenceResumeProbesEverySinkAndClearsOnlyAfterSuccess(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{store: store}
	paths := []string{filepath.Join(stateRoot, "a-events.jsonl"), filepath.Join(stateRoot, "b-events.jsonl")}
	for index, path := range paths {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		recordID := fmt.Sprintf("APP-T-%04d", index+1)
		if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: recordID, ItemID: recordID, EventSinkPath: path}); err != nil {
			t.Fatal(err)
		}
		saveSupervisorDecisionForEventLogFailureTest(t, store, "project-1", recordID, fmt.Sprintf("decision-%d", index+1))
		daemon.tripEventLogPersistenceCircuit("supervisor_decision", recordID, errors.New("injected sink failure"))
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.ResumeInvariantCircuit(); err == nil || !strings.Contains(err.Error(), "cannot resume daemon") {
		t.Fatalf("resume cleared circuit while one sink remained broken: %v", err)
	}
	firstRaw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("repaired sink was not probed after another sink failed: %v", err)
	}
	if !strings.Contains(string(firstRaw), "event_log_persistence_probe") {
		t.Fatalf("repaired sink did not receive a durable probe: %s", firstRaw)
	}
	failures, err := store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 2 {
		t.Fatalf("failed sink registry cleared before all probes passed: %#v", failures)
	}
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatalf("resume after repairing every sink: %v", err)
	}
	failures, err = store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("successful probes did not clear failed sink registry: %#v", failures)
	}
	status, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Open {
		t.Fatalf("successful probes did not close invariant circuit: %#v", status)
	}
}

func TestSupervisorDecisionAppendFailureIsNotDiscarded(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := RunStatus{
		ProjectID: "project-1", RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		LeaseState: string(LeaseStateRunning), EventSinkPath: filepath.Join(stateRoot, "event-sink-directory"),
	}
	if err := ensureDir(run.EventSinkPath); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{store: store}
	daemon.emitSupervisorDecision(SupervisorDecision{
		DecisionID: "decision-1", ProjectID: run.ProjectID, RecordID: run.RecordID,
		Kind: string(SupervisorDecisionStopForAudit), Reason: "test",
	})
	status, err := store.ReadInvariantCircuitStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Open || !strings.Contains(status.Summary, "supervisor_decision") {
		t.Fatalf("supervisor event failure was discarded: %#v", status)
	}
	decisions, err := store.ListSupervisorDecisionsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("canonical supervisor decision was not retained: %#v", decisions)
	}
}

func TestEventLogPersistenceResumeReplaysSupervisorDecisionBeforeProbe(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sinkPath := filepath.Join(stateRoot, "events.jsonl")
	recordID := "APP-T-0001"
	if err := os.Mkdir(sinkPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: recordID, ItemID: recordID, EventSinkPath: sinkPath}); err != nil {
		t.Fatal(err)
	}
	saved := saveSupervisorDecisionForEventLogFailureTest(t, store, "project-1", recordID, "decision-replay")
	daemon := &Daemon{store: store}
	daemon.tripEventLogPersistenceCircuit("supervisor_decision", recordID, errors.New("injected sink failure"))
	if err := os.Remove(sinkPath); err != nil {
		t.Fatal(err)
	}

	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatalf("resume after replayable event-sink repair: %v", err)
	}
	raw, err := os.ReadFile(sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	var replayIndex, probeIndex = -1, -1
	for index, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		switch event.Kind {
		case "supervisor_decision":
			if event.Payload["decision_id"] == saved.DecisionID {
				replayIndex = index
			}
		case "event_log_persistence_probe":
			probeIndex = index
		}
	}
	if replayIndex < 0 || probeIndex < 0 || replayIndex >= probeIndex {
		t.Fatalf("critical supervisor event was not replayed before probe: %s", raw)
	}
	failures, err := store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("replayed event failure remained registered: %#v", failures)
	}
}

func TestEventLogPersistenceResumeReplaysSupervisorDecisionWhenProbeSharesSink(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sinkPath := filepath.Join(stateRoot, "events.jsonl")
	recordID := "APP-T-0001"
	if err := os.Mkdir(sinkPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: recordID, ItemID: recordID, EventSinkPath: sinkPath}); err != nil {
		t.Fatal(err)
	}
	saved := saveSupervisorDecisionForEventLogFailureTest(t, store, "project-1", recordID, "decision-shared-sink")
	daemon := &Daemon{store: store}
	daemon.tripEventLogPersistenceCircuit("supervisor_decision", recordID, errors.New("supervisor sink failure"))
	daemon.tripEventLogPersistenceCircuit("runtime_event", recordID, errors.New("probe sink failure"))
	failures, err := store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 2 {
		t.Fatalf("shared sink collapsed critical and probe failures: %#v", failures)
	}
	if err := os.Remove(sinkPath); err != nil {
		t.Fatal(err)
	}

	if _, err := daemon.ResumeInvariantCircuit(); err != nil {
		t.Fatalf("resume after shared-sink repair: %v", err)
	}
	raw, err := os.ReadFile(sinkPath)
	if err != nil {
		t.Fatal(err)
	}
	var replayIndex, probeIndex = -1, -1
	for index, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		switch event.Kind {
		case "supervisor_decision":
			if event.Payload["decision_id"] == saved.DecisionID {
				replayIndex = index
			}
		case "event_log_persistence_probe":
			probeIndex = index
		}
	}
	if replayIndex < 0 || probeIndex < 0 || replayIndex >= probeIndex {
		t.Fatalf("probe recovery cleared without replaying the critical supervisor event first: %s", raw)
	}
	failures, err = store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 0 {
		t.Fatalf("recovered shared-sink failures remained registered: %#v", failures)
	}
}

func TestEventLogPersistenceResumeQuarantinesUnreplayableSupervisorDecision(t *testing.T) {
	stateRoot := t.TempDir()
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sinkPath := filepath.Join(stateRoot, "events.jsonl")
	recordID := "APP-T-0001"
	if err := os.Mkdir(sinkPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(RunStatus{ProjectID: "project-1", RecordID: recordID, ItemID: recordID, EventSinkPath: sinkPath}); err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{store: store}
	daemon.tripEventLogPersistenceCircuit("supervisor_decision", recordID, errors.New("injected sink failure"))
	if err := os.Remove(sinkPath); err != nil {
		t.Fatal(err)
	}

	if _, err := daemon.ResumeInvariantCircuit(); err == nil || !strings.Contains(err.Error(), "cannot resume daemon") {
		t.Fatalf("resume cleared an unreplayable supervisor event: %v", err)
	}
	failures, err := store.ReadEventLogPersistenceFailures()
	if err != nil {
		t.Fatal(err)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].Reason, "quarantined") {
		t.Fatalf("unreplayable event was not quarantined: %#v", failures)
	}
	if _, err := os.Stat(sinkPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quarantined event unexpectedly recreated sink: %v", err)
	}
}

func saveSupervisorDecisionForEventLogFailureTest(t *testing.T, store *RuntimeStore, projectID, recordID, decisionID string) SupervisorDecision {
	t.Helper()
	decision, err := store.SaveSupervisorDecision(SupervisorDecision{
		DecisionID: decisionID,
		ProjectID:  projectID,
		RecordID:   recordID,
		ItemID:     recordID,
		Kind:       string(SupervisorDecisionStopForAudit),
		Reason:     "test decision",
	})
	if err != nil {
		t.Fatal(err)
	}
	return decision
}
