package main

import (
	"path/filepath"
	"testing"
)

func TestInteractiveWorkReadinessSeparation(t *testing.T) {
	t.Run("typed_dependency_with_human_gate_prose", func(t *testing.T) {
		err := workSessionStartBlocker(ReadinessBlocker{
			ID: "interactive-dependency:APP-T-0002:APP-T-0001", Kind: ReadinessBlockerDependencyIncomplete, Authority: ReadinessAuthorityContract,
			Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive, ReadinessDimensionContract}, TaskID: "APP-T-0002", DependencyTaskID: "APP-T-0001",
			Reason: "Dependency APP-T-0001 says a human must clear the gate.", Remedy: "Complete APP-T-0001.",
		})
		if issue := errorToIssue(err); issue.Code != "WORK_SESSION_DEPENDENCY_BLOCKED" {
			t.Fatalf("dependency prose changed refusal kind: %#v", issue)
		}
	})
	t.Run("typed_dependency_identity", TestInteractiveWorkReadinessUsesTypedCauses)
	t.Run("disarmed_wave_and_critical_risk_are_not_interactive_blockers", func(t *testing.T) {
		task := Note{Data: map[string]any{"id": "APP-T-0001", "schema": "tusker.task/v7", "kind": "task", "status": "ready", "risk": "critical", "wave": "APP-W-0001"}}
		idx := v7Index{
			Tasks: map[string]Note{"APP-T-0001": task},
			Waves: map[string]Note{
				"APP-W-0001": {Data: map[string]any{"id": "APP-W-0001", "authorization": "disarmed", "members": []string{"APP-T-0001"}}},
			},
		}
		if blockers := workSessionAdmissionBlockers(task, idx, nil, nil); len(blockers) != 0 {
			t.Fatalf("dispatch authority leaked into interactive readiness: %#v", blockers)
		}
	})
	t.Run("genuine_gate_and_typed_refusals", func(t *testing.T) {
		TestInteractiveWorkReadinessOnlyHonorsGenuineHumanGates(t)
		TestInteractiveWorkRefusalContextPreservesTypedOwnershipAndRevision(t)
	})
	t.Run("automation_and_capacity_do_not_block", TestInteractiveWorkStartIgnoresDispatchAuthorityAndCapacity)
}

func TestInteractiveWorkReadinessUsesTypedCauses(t *testing.T) {
	task := Note{Data: map[string]any{"id": "APP-T-0002", "schema": "tusker.task/v7", "kind": "task", "status": "ready", "dependencies": []string{"APP-T-0001:hard"}}}
	idx := v7Index{Tasks: map[string]Note{
		"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001", "status": "ready", "risk": "critical"}},
	}}
	blockers := workSessionAdmissionBlockers(task, idx, nil, nil)
	if len(blockers) != 1 {
		t.Fatalf("blockers = %#v", blockers)
	}
	blocker := blockers[0]
	if blocker.Kind != ReadinessBlockerDependencyIncomplete || blocker.DependencyTaskID != "APP-T-0001" || blocker.TaskID != "APP-T-0002" {
		t.Fatalf("dependency blocker = %#v", blocker)
	}
	if errorToIssue(workSessionStartBlocker(blocker)).Code != "WORK_SESSION_DEPENDENCY_BLOCKED" {
		t.Fatalf("dependency classification changed with prose: %#v", blocker)
	}
}

func TestInteractiveWorkReadinessOnlyHonorsGenuineHumanGates(t *testing.T) {
	task := Note{Data: map[string]any{"id": "APP-T-0001", "schema": "tusker.task/v7", "kind": "task", "status": "ready", "readiness": "waiting_on_human", "next_owner": "human:sarav"}}
	invalid := Note{Data: map[string]any{"id": "APP-G-0001", "status": "open", "blocking": true, "owner": "human:sarav", "gate_kind": "signoff", "blocks": []string{"APP-T-0001"}, "action": "Review the code diff.", "verification": "Human approves the code.", "why_agent_cannot": "A human should review this."}}
	idx := v7Index{Tasks: map[string]Note{"APP-T-0001": task}, Gates: map[string]Note{"APP-G-0001": invalid}}
	if blockers := workSessionAdmissionBlockers(task, idx, nil, nil); len(blockers) != 0 {
		t.Fatalf("invalid human-like gate blocked interactive work: %#v", blockers)
	}
	valid := invalid
	valid.Data = map[string]any{"id": "APP-G-0001", "status": "open", "blocking": true, "owner": "human:sarav", "gate_kind": "auth", "blocks": []string{"APP-T-0001"}, "action": "Authorize the account connection.", "verification": "The account authorization is recorded.", "why_agent_cannot": "Only the account owner has the required authorization."}
	idx.Gates["APP-G-0001"] = valid
	blockers := workSessionAdmissionBlockers(task, idx, nil, nil)
	if len(blockers) != 1 || blockers[0].Kind != ReadinessBlockerHumanGateOpen || blockers[0].GateID != "APP-G-0001" {
		t.Fatalf("genuine human gate = %#v", blockers)
	}
}

func TestInteractiveWorkRefusalContextPreservesTypedOwnershipAndRevision(t *testing.T) {
	owned := workSessionClaimRefusal("APP-T-0002", tuskerError("OWNED_PATH_CONFLICT", "claim refused", withContext(map[string]any{"task_id": "APP-T-0001", "holder": "agent:owner"})))
	ownedIssue := errorToIssue(owned)
	ownedContext := ownedIssue.Context.(map[string]any)["readiness_blocker"].(ReadinessBlocker)
	if ownedIssue.Code != "OWNED_PATH_CONFLICT" || ownedContext.Kind != ReadinessBlockerOwnedPathConflict || ownedContext.TaskID != "APP-T-0002" || ownedContext.ConflictingTaskID != "APP-T-0001" || ownedContext.Owner != "agent:owner" {
		t.Fatalf("owned-path blocker = %#v (%#v)", ownedIssue, ownedContext)
	}
	revision := errorToIssue(workSessionStaleRevisionError("WORK_SESSION_STALE", "APP-T-0002", 3, 4))
	revisionContext := revision.Context.(map[string]any)["readiness_blocker"].(ReadinessBlocker)
	if revision.Code != "WORK_SESSION_STALE" || revisionContext.Kind != ReadinessBlockerWorkRevisionStale || revisionContext.TaskID != "APP-T-0002" {
		t.Fatalf("revision blocker = %#v (%#v)", revision, revisionContext)
	}
}

func TestInteractiveWorkStartIgnoresDispatchAuthorityAndCapacity(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	vault, project := workSessionFixture(t, 2)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"risk": "critical"})
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	project.Enabled, project.Health = false, projectHealthDisabled
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:one"); err != nil {
		t.Fatalf("critical interactive work inherited dispatch authority: %v", err)
	}
	if err := startWorkSessionTest(t, vault, "APP-T-0002", "agent:two"); err != nil {
		t.Fatalf("interactive work inherited automated capacity policy: %v", err)
	}
}
