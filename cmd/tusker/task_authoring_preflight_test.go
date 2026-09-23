package main

import (
	"strings"
	"testing"
)

func TestTaskAuthoringRouteGateReportsMissingReviewRoute(t *testing.T) {
	idx := v7Index{Tasks: map[string]Note{
		"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001"}},
	}}
	inspector := func(_ Note, lane string) error {
		if lane == runLaneReview {
			return tuskerError(errorInvalidTransition, "standard review profile mapping is empty")
		}
		return nil
	}
	blockers := directRouteBlockersWithInspector(idx, []string{"APP-T-0001"}, inspector)
	if len(blockers) != 1 || !strings.Contains(blockers[0], "APP-T-0001: review route blocked: standard review profile mapping is empty") {
		t.Fatalf("missing review route was not a per-member blocker: %#v", blockers)
	}
}

func TestTaskAuthoringPreflightPreservesPipedVerificationCommand(t *testing.T) {
	section := "| Covers | Check | Result |\n|---|---|---|\n| A1 | command: printf 'left\\|right' | pending |\n"
	rows := waveMaterialTable(section, []int{0, 1})
	if got := strings.Join(rows, "\n"); !strings.Contains(got, "left|right") {
		t.Fatalf("escaped pipe was split: %#v", rows)
	}
}

func TestTaskAuthoringRouteGateChecksBothLanes(t *testing.T) {
	idx := v7Index{Tasks: map[string]Note{
		"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001"}},
	}}
	inspector := func(_ Note, lane string) error {
		if lane == runLaneReview {
			return tuskerError(errorConfigInvalid, "standard review profile mapping is empty", withHint("configure automation.model_levels.standard.review"))
		}
		return nil
	}
	blockers := directRouteBlockersWithInspector(idx, []string{"APP-T-0001"}, inspector)
	if len(blockers) != 1 || !strings.Contains(blockers[0], "APP-T-0001: review route blocked") || !strings.Contains(blockers[0], "configure automation.model_levels.standard.review") {
		t.Fatalf("wave route gate lost the lane or repair action: %#v", blockers)
	}
}

func TestTaskAuthoringRouteGateMalformedWorkflowIsPerLaneActionable(t *testing.T) {
	idx := v7Index{Tasks: map[string]Note{
		"APP-T-0001": {Data: map[string]any{"id": "APP-T-0001"}},
	}}
	inspector := unavailableDirectTaskRouteInspector(tuskerError(errorConfigInvalid, "failed to parse WORKFLOW.md", withPath("WORKFLOW.md")))
	blockers := directRouteBlockersWithInspector(idx, []string{"APP-T-0001"}, inspector)
	joined := strings.Join(blockers, " ")
	if !strings.Contains(joined, "APP-T-0001: execute route blocked") || !strings.Contains(joined, "APP-T-0001: review route blocked") || !strings.Contains(joined, "repair WORKFLOW.md") {
		t.Fatalf("malformed workflow route failure was not actionable per lane: %#v", blockers)
	}
}

func TestTaskAuthoringProductionInspectorReportsMalformedWorkflow(t *testing.T) {
	vault := automationTestVault(t)
	if err := writeText(workflowPath(vault), "---\nworkflow_version: [\n---\n"); err != nil {
		t.Fatal(err)
	}
	inspector := directTaskRouteInspectorForVault(vault)
	for _, lane := range []string{runLaneExecute, runLaneReview} {
		err := inspector(Note{Data: map[string]any{"id": "APP-T-0001"}}, lane)
		if err == nil || !strings.Contains(err.Error(), "repair WORKFLOW.md") || !strings.Contains(err.Error(), "workflow route resolution unavailable") {
			t.Fatalf("malformed workflow lane %s was not actionable: %v", lane, err)
		}
	}
}

func TestTaskAuthoringProductionInspectorRejectsDisabledTierProfile(t *testing.T) {
	wf := defaultWorkflow()
	wf.RunnerProfiles = map[string]RunnerProfileDefinition{
		"disabled": {Harness: string(RunnerCodexExec), Model: "fixture-model", Disabled: true},
	}
	wf.ModelLevels = map[string]ModelLevelDefinition{
		"standard": {Execute: []string{"disabled"}, Review: []string{"disabled"}},
	}
	inspector := directTaskRouteInspectorForWorkflow(wf)
	for _, lane := range []string{runLaneExecute, runLaneReview} {
		if err := inspector(Note{Data: map[string]any{"id": "APP-T-0001", "work_level": "standard"}}, lane); err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("disabled %s profile passed wave admission: %v", lane, err)
		}
	}
}

func TestTaskAuthoringPreflightRoutePreviewUsesLegacyLaneRunner(t *testing.T) {
	wf := defaultWorkflow()
	// With no named project policy, the resolver uses the built-in profile and
	// then applies the workflow's legacy runner compatibility fallback. Preview
	// must make the same choice as daemon admission for each lane.
	wf.RunnerProfiles = nil
	wf.ModelLevels = nil
	wf.RunnerDefaultProfile = ""
	wf.Agents.Default = "legacy-execute"
	wf.Reviewer.Runner = "legacy-review"
	note := Note{Data: map[string]any{"id": "APP-T-0001"}}

	execute := routePreviewForNote(note, wf, runLaneExecute)
	if execute.Harness != "legacy-execute" || !strings.Contains(execute.Reason, "legacy runner fallback") {
		t.Fatalf("execute preview did not preserve legacy runner selection: %#v", execute)
	}
	review := routePreviewForNote(note, wf, runLaneReview)
	if review.Harness != "legacy-review" || !strings.Contains(review.Reason, "legacy runner fallback") {
		t.Fatalf("review preview did not preserve legacy reviewer selection: %#v", review)
	}
}

func TestTaskAuthoringRouteGateExplicitUnclassifiedTierBlocksOverrides(t *testing.T) {
	wf := defaultWorkflow()
	wf.RunnerProfiles = map[string]RunnerProfileDefinition{
		"persisted-profile": {Harness: string(RunnerCodexExec), Model: "fixture-model", Effort: "medium"},
	}
	note := Note{Data: map[string]any{
		"id": "APP-T-0001", "work_level": "", "runner_profile": "persisted-profile",
		"execute_profile": "persisted-profile", "review_profile": "persisted-profile", "review_level": "standard",
	}}
	for _, lane := range []string{runLaneExecute, runLaneReview} {
		if _, err := resolveRunProfileForLane(note, wf, lane, "legacy-operator-runner"); err == nil || !strings.Contains(err.Error(), "work_level is required") {
			t.Fatalf("%s resolver allowed an explicit unclassified tier override: %v", lane, err)
		}
		preview := routePreviewForNote(note, wf, lane)
		if len(preview.Blockers) != 1 || !strings.Contains(preview.Blockers[0], "work_level is required") {
			t.Fatalf("%s preview did not retain the unclassified-tier blocker: %#v", lane, preview)
		}
	}

	idx := v7Index{Tasks: map[string]Note{"APP-T-0001": note}}
	blockers := directRouteBlockersWithInspector(idx, []string{"APP-T-0001"}, directTaskRouteInspectorForWorkflow(wf))
	if len(blockers) != 2 {
		t.Fatalf("wave admission did not block both lanes: %#v", blockers)
	}
}

func TestTaskAuthoringArmAcceptsValidDependencyBlockedMember(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001", "APP-T-0002"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"dependencies": []any{"APP-T-0001:hard"}})
	rewriteTaskFile(t, vault, "APP-T-0002", func(data map[string]any, body string) (map[string]any, string) {
		data["status"] = "ready"
		data["readiness"] = "blocked_by_dependency"
		data["next_owner"] = "blocked_dependency"
		data["next_source"] = "dependency"
		data["next_ref"] = "APP-T-0001"
		return data, body
	})

	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	blockers := directWaveArmContractBlockers(vault, idx, idx.Waves["W-0001"])
	for _, blocker := range blockers {
		if blocker.TaskID == "APP-T-0002" {
			t.Fatalf("valid dependency-waiting member was treated as a contract defect: %#v", blocker)
		}
	}
	review, err := buildDirectWaveReview(vault, store, project.ProjectID, "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	waveStartEnabled := false
	for _, control := range review.Controls {
		if control.Action == "wave start" {
			waveStartEnabled = control.Enabled
		}
	}
	if !waveStartEnabled {
		t.Fatalf("wave review disagreed with arm admission: %#v", review.Controls)
	}
	result, err := directWaveStart(vault, store, "W-0001", "human:test")
	if err != nil {
		t.Fatalf("valid dependency-waiting member prevented wave start: %v", err)
	}
	if len(result.QueuedTaskIDs) != 1 || result.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("wave start bypassed dependency admission: %#v", result.QueuedTaskIDs)
	}
}

func TestTaskAuthoringArmRejectsUnknownReadiness(t *testing.T) {
	vault, store, project := authorityFixture(t)
	writeDirectWave(t, vault, "W-0001", []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		data["readiness"] = "not_a_readiness_state"
		return data, body
	})

	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	blockers := directWaveArmContractBlockers(vault, idx, idx.Waves["W-0001"])
	if len(blockers) != 1 || blockers[0].TaskID != "APP-T-0001" || !strings.Contains(blockers[0].Reason, "readiness is not_a_readiness_state") {
		t.Fatalf("unknown readiness was not a member contract blocker: %#v", blockers)
	}
	if _, err := directWaveStart(vault, store, "W-0001", "human:test"); err == nil || !strings.Contains(err.Error(), "MEMBER_CONTRACT_INVALID") {
		t.Fatalf("wave start accepted malformed readiness: %v", err)
	}
	if got := queuedDirectives(t, store, project.ProjectID); len(got) != 0 {
		t.Fatalf("malformed readiness published directives: %#v", got)
	}
}
