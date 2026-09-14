package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPilotWaveFrontierDispatchesOnlyQualifiedWork(t *testing.T) {
	vault, idx, wave := pilotArmedWaveFixture(t)
	store := fairDispatchTestStore(t)
	project := RegisteredProject{
		ProjectID: "pilot", ProjectKey: "pilot", Enabled: true,
		RepoRoot: v7RepoRoot(vault), VaultRoot: vault,
	}
	wf := defaultWorkflow()
	wf.DispatchScope = defaultAutomationDispatchScope()
	wf.Workspace.Strategy = string(WorkspaceStrategyWorktree)
	daemon := &Daemon{store: store, stateRoot: store.stateRoot}

	initial := buildArmedWaveSnapshot(vault, idx, wave, nil, time.Unix(0, 0).UTC())
	assertEqual(t, []string{"APP-T-0001", "APP-T-0006"}, initial.Frontier, "manual wave starts only independent work")
	var dispatched []string
	daemon.fairDispatchRun = fairDispatchRecorder(&dispatched)
	if err := daemon.dispatchFairCandidates(context.Background(), pilotWaveCandidates(t, store, project, wf, idx, initial.Frontier...), 2); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"pilot/APP-T-0001", "pilot/APP-T-0006"}, dispatched, "scheduler consumes the armed frontier")

	root := fairDispatchFindRun(t, store, project.ProjectID, "APP-T-0001")
	root.LeaseState, root.LeaseOwner, root.AttemptOutcome = string(LeaseStateReleased), "", string(AttemptOutcomeSucceeded)
	if err := store.UpsertRun(root); err != nil {
		t.Fatal(err)
	}
	failed := fairDispatchFindRun(t, store, project.ProjectID, "APP-T-0006")
	failed.LeaseState, failed.LeaseOwner, failed.AttemptOutcome = string(LeaseStateParkedNoProgress), "", string(AttemptOutcomeFailed)
	failed.LastError, failed.Terminal = "pilot sibling failed", true
	if err := store.UpsertRun(failed); err != nil {
		t.Fatal(err)
	}
	writeTrustProofEvidence(t, vault, trustProofEvidence(t, vault, idx.Tasks["APP-T-0001"], "automated_test", []string{"A1"}, "pilot-diff.txt"))
	recordCompletionTestProof(t, vault, "APP-T-0001")
	setWaveTaskState(t, vault, "APP-T-0001", "done", "done", "2026-09-06T00:00:00Z")
	setWaveTaskState(t, vault, "APP-T-0002", "ready", "ready", "")

	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave = idx.Waves["W-0001"]
	runs, err := store.ListRuns()
	if err != nil {
		t.Fatal(err)
	}
	byRecord := map[string]RunStatus{}
	for _, run := range runs {
		byRecord[run.RecordID] = run
	}
	next := buildArmedWaveSnapshot(vault, idx, wave, byRecord, time.Unix(1, 0).UTC())
	assertEqual(t, []string{"APP-T-0002", "APP-T-0003"}, next.Frontier, "accepted prerequisite releases dependents despite failed sibling")

	dispatched = nil
	daemon.fairDispatchRun = fairDispatchRecorder(&dispatched)
	if err := daemon.dispatchFairCandidates(context.Background(), pilotWaveCandidates(t, store, project, wf, idx, next.Frontier...), 2); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"pilot/APP-T-0002", "pilot/APP-T-0003"}, dispatched, "scheduler dispatches released dependents")

	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Next manual wave", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0007")
	if err := waveV7CreateCmd(Args{"vault": vault, "quiet": "true", "_pos0": "Next", "_pos1": "APP-T-0007"}); err != nil {
		t.Fatal(err)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	nextWaveID := stringField(idx.Tasks["APP-T-0007"].Data, "wave")
	if auth := waveAuthorizationProjection(vault, idx, idx.Waves[nextWaveID]); stringField(auth, "state") == "armed" {
		t.Fatal("creating the next wave armed it without a manual authorization")
	}
	dispatched = nil
	daemon.fairDispatchRun = fairDispatchRecorder(&dispatched)
	if err := daemon.dispatchFairCandidates(context.Background(), pilotWaveCandidates(t, store, project, wf, idx, "APP-T-0007"), 3); err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 0 {
		t.Fatalf("unarmed next wave dispatched automatically: %#v", dispatched)
	}
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if auth := waveAuthorizationProjection(vault, idx, idx.Waves[nextWaveID]); stringField(auth, "state") == "armed" {
		t.Fatal("scheduler armed the next wave without a manual authorization")
	}
	blocked := fairDispatchFindRun(t, store, project.ProjectID, "APP-T-0007")
	if !strings.Contains(blocked.LastError, "wave constraint") {
		t.Fatalf("unarmed next wave lacks a stable scope refusal: %#v", blocked)
	}
}

func pilotArmedWaveFixture(t *testing.T) (string, v7Index, Note) {
	t.Helper()
	vault := v7DirectTestVault(t)
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	wf.Data.Runtime.MaxActiveRunsPerProject = 2
	writeWorkflowForPreflightTest(t, vault, wf.Data, wf.Body)
	if _, err := setProjectLocalConfigWithReadback(vault, "runtime.max_active_runs_per_project", 2); err != nil {
		t.Fatal(err)
	}

	newTask := func(id string, deps ...string) {
		extra := map[string]any{
			"status": "ready", "readiness": "ready", "next_owner": "agent", "work_revision": 1,
		}
		if len(deps) > 0 {
			extra["dependencies"] = deps
		}
		writeDirectTask(t, vault, id, "W-0001", extra)
	}
	newTask("APP-T-0001")
	newTask("APP-T-0002", "APP-T-0001:hard")
	newTask("APP-T-0003", "APP-T-0001:hard")
	newTask("APP-T-0004", "APP-T-0002:soft")
	newTask("APP-T-0005", "APP-T-0003:hard")
	newTask("APP-T-0006")
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002", "APP-T-0003", "APP-T-0004", "APP-T-0005", "APP-T-0006"}, map[string]any{"concurrency": 2})
	armWaveForTest(t, vault)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	return vault, idx, idx.Waves["W-0001"]
}

func pilotWaveCandidates(t *testing.T, store *RuntimeStore, project RegisteredProject, wf Workflow, idx v7Index, ids ...string) []daemonDispatchCandidate {
	t.Helper()
	notes, err := listOperationalNotes(project.VaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	notesByID, _ := daemonNoteMaps(notes)
	candidates := make([]daemonDispatchCandidate, 0, len(ids))
	for _, id := range ids {
		note := idx.Tasks[id]
		run := fairDispatchTestRun(project.ProjectID, id)
		run.WorkRevision = intField(note.Data, "work_revision")
		if err := store.UpsertRun(run); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, daemonDispatchCandidate{
			Project: project, Workflow: WorkflowFile{Data: wf}, Note: note, NotesByID: notesByID,
			Run: run, Lane: runLaneExecute, Status: stringField(note.Data, "status"), ProjectLimit: 3,
		})
	}
	return candidates
}
