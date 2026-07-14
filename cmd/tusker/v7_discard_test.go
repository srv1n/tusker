package main

import (
	"path/filepath"
	"testing"
)

func TestV7DiscardDetachesDependentsObsoletesGatesAndPreservesHistory(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Discard me", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Keep dependent", "risk": "low", "priority": "p1", "dependencies": "APP-T-0001:hard", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{
		"vault": vault, "quiet": "true", "blocks": "APP-T-0001", "kind": "verification", "owner": "reviewer:agent",
		"action": "Review task.", "verification": "Task is reviewed.",
	}, newV7Gate)

	impact, err := v7DiscardImpactForTask(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, true, impact.RequiresResolution, "discard requires dependency resolution")
	assertEqual(t, 1, len(impact.DirectDependents), "direct dependent count")
	assertEqual(t, "APP-T-0002", impact.DirectDependents[0].ID, "direct dependent id")
	assertEqual(t, []string{"APP-G-0001"}, impact.OpenGates, "open gate impact")

	err = discardV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "reason": "No longer desired."})
	if err == nil || errorToIssue(err).Code != errorInvalidTransition {
		t.Fatalf("expected unresolved dependent refusal, got %v", err)
	}
	if err := discardV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "reason": "No longer desired.", "dependents": "detach", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	discarded := mustTaskData(t, vault, "APP-T-0001")
	assertEqual(t, "cancelled", stringField(discarded, "status"), "discarded status")
	assertEqual(t, "cancelled", stringField(discarded, "readiness"), "discarded readiness")
	assertEqual(t, "No longer desired.", stringField(discarded, "discard_reason"), "discard reason")
	assertEqual(t, "none", stringField(discarded, "next_owner"), "discarded next owner")
	dependent := mustTaskData(t, vault, "APP-T-0002")
	assertEqual(t, []string{}, normalizeList(dependent["dependencies"]), "dependent edge detached")
	gate, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "APP-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "obsolete", stringField(gate, "status"), "discarded task gate obsolete")
	if !fileExists(filepath.Join(vault, "work", "tasks", "APP-T-0001.md")) {
		t.Fatal("discard must preserve the task tombstone")
	}
}

func TestV7DiscardExplicitCascadeCancelsTransitiveDependents(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Root", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Child", "risk": "low", "priority": "p1", "dependencies": "APP-T-0001", "v7": "true"}, newV7Task)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Grandchild", "risk": "low", "priority": "p1", "dependencies": "APP-T-0002", "v7": "true"}, newV7Task)

	impact, err := v7DiscardImpactForTask(vault, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, 2, len(impact.CascadeDependents), "transitive discard count")
	if err := discardV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "reason": "Feature removed.", "dependents": "discard", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002", "APP-T-0003"} {
		assertEqual(t, "cancelled", stringField(mustTaskData(t, vault, id), "status"), id+" cascade status")
	}
}

func TestV7DiscardRequiresReasonAndRejectsDoneTask(t *testing.T) {
	vault := pickupV7TestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Root", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	if err := discardV7Cmd(Args{"vault": vault, "id": "APP-T-0001"}); err == nil || errorToIssue(err).Code != errorMissingArg {
		t.Fatalf("expected missing reason error, got %v", err)
	}
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "done", "readiness": "done"})
	if err := discardV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "reason": "Too late."}); err == nil || errorToIssue(err).Code != errorInvalidTransition {
		t.Fatalf("expected done task refusal, got %v", err)
	}
}

func TestV7DiscardRetiresRuntimeForDisabledProject(t *testing.T) {
	vault := automationTestVault(t)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Running task", "risk": "low", "priority": "p1", "v7": "true"}, newV7Task)
	project := registerAutomationTestProject(t, vault)
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetProjectEnabled(project.ProjectID, false); err != nil {
		t.Fatal(err)
	}
	mustUpsertRun(t, store, RunStatus{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", ItemID: "APP-T-0001",
		Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateRetryQueued),
		ActiveAttemptID: "attempt-discard", AttemptCount: 1, UpdatedAt: "2026-07-14T00:00:00Z",
	})
	_ = store.Close()

	if err := discardV7Cmd(Args{"vault": vault, "id": "APP-T-0001", "reason": "Stop this work.", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	store, err = OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run := latestRunForRecord(t, store, project.ProjectID, "APP-T-0001")
	assertEqual(t, string(LeaseStateReleased), run.LeaseState, "discarded runtime lease")
	assertEqual(t, true, run.Terminal, "discarded runtime terminal")
}
