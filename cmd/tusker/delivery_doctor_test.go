package main

import (
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeDoctorPlan(t *testing.T, vault string, plan deliveryPlanV2) string {
	t.Helper()
	raw, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "doctor.yaml")
	if err := writeText(path, string(raw)); err != nil {
		t.Fatal(err)
	}
	return path
}

func doctorPlan(t *testing.T) deliveryPlanV2 {
	t.Helper()
	plan := validDeliveryPlanV2()
	plan.Summary = "Import creates a held, requirements-traceable delivery wave."
	plan.HumanGates = nil
	return plan
}

func doctorCodes(report deliveryDoctorReport) map[string]bool {
	out := map[string]bool{}
	for _, finding := range report.Findings {
		out[finding.Code] = true
	}
	return out
}

func doctorTask(plan deliveryPlanV2, key string) deliveryPlanTask {
	task := plan.Tasks[0]
	task.SourceKey = key
	task.Title = "Deliver " + key
	task.Outcome = "The " + key + " outcome is observable."
	task.Acceptance = []deliveryAcceptance{{ID: "A1", Outcome: "The " + key + " behavior is observable."}}
	task.Verification = []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run '^TestDeliveryPlanDoctor' -count=1"}}
	task.Dependencies = nil
	task.Artifact = deliveryArtifactContract{Kind: "diff_summary", Path: "cmd/tusker/" + key + ".go", Summary: "Operator-visible " + key + " diff.", AcceptanceIDs: []string{"A1"}}
	task.OwnedPaths = nil
	task.GeneratedOutputs = nil
	task.MigrationKeys = nil
	task.ResourceRefs = nil
	task.RunnerProfile = ""
	return task
}

func requireDoctorCodes(t *testing.T, report deliveryDoctorReport, expected ...string) {
	t.Helper()
	codes := doctorCodes(report)
	for _, code := range expected {
		if !codes[code] {
			t.Errorf("missing finding %s in %#v", code, report.Findings)
		}
	}
}

func TestDeliveryPlanDoctorReportsFrontierCollisionsAndStaysReadOnly(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := doctorPlan(t)
	first := plan.Tasks[0]
	first.SourceKey, first.Title = "second", "Second import"
	first.OwnedPaths = []string{"cmd/tusker"}
	plan.Tasks[0].OwnedPaths = []string{"cmd/tusker/delivery_cmd.go"}
	plan.Tasks = append(plan.Tasks, first)
	path := writeDoctorPlan(t, vault, plan)
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if !doctorCodes(report)["OWNED_PATH_FRONTIER_CONFLICT"] {
		t.Fatalf("missing owned path conflict: %#v", report.Findings)
	}
	if fileExists(filepath.Join(vault, "work", "tasks", "VTP-T-0001.md")) {
		t.Fatal("doctor wrote task records")
	}
}

func TestDeliveryPlanDoctorRequiresExactHumanProofContract(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := doctorPlan(t)
	plan.Tasks[0].Verification = []deliveryVerification{{Covers: "A1", Check: "manual proof: product owner confirms the outcome"}}
	follow := plan.Tasks[0]
	follow.SourceKey, follow.Title = "follow", "Follow the owner decision"
	follow.Dependencies = []deliveryDependency{{Task: "import", Kind: "hard"}}
	plan.Tasks = append(plan.Tasks, follow)
	plan.HumanGates = []deliveryHumanGate{{SourceKey: "owner", Title: "Owner proof", Kind: "decision", Owner: "human:owner", TaskSourceKey: "import", AcceptanceIDs: []string{"A1"}, Action: "Confirm the outcome.", Verification: "Record the decision.", WhyAgentCannot: "Only the product owner has this authority."}}
	report, err := deliveryPlanDoctor(vault, writeDoctorPlan(t, vault, plan))
	if err != nil {
		t.Fatal(err)
	}
	if !doctorCodes(report)["HUMAN_GATE_CLOSURE_MISSING"] {
		t.Fatalf("missing closure finding: %#v", report.Findings)
	}
}

func TestDeliveryPlanDoctorCompleteFindingMatrixOnePassStableAndReadOnly(t *testing.T) {
	vault := deliveryTestVault(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	workflow := defaultWorkflow()
	workflow.Agents.MaxConcurrentAgents = 1
	workflow.Runtime.MaxActiveRunsPerProject = 1
	rawWorkflow, err := yaml.Marshal(workflow)
	if err != nil {
		t.Fatal(err)
	}
	workflowBody := "\n## Routing\n\nTest routing.\n\n## Prompt\n\nTest prompt.\n\n## Retry policy\n\nTest retry policy.\n\n## Human override policy\n\nTest override policy.\n"
	if err := writeText(workflowPath(vault), "---\n"+strings.TrimSpace(string(rawWorkflow))+"\n---\n"+workflowBody); err != nil {
		t.Fatal(err)
	}

	plan := doctorPlan(t)
	plan.Concurrency = 20
	plan.RunnerProfile = "runner-that-does-not-exist"
	plan.Requirements = append(plan.Requirements, deliveryRequirement{ID: "R-UNCOVERED", Outcome: "An uncovered outcome remains visible."})
	plan.SharedResources = []deliverySharedResource{{SourceKey: "migration-slot", Kind: "migration", Capacity: 1}}
	plan.Assumptions = []deliveryPlanAssumption{{SourceKey: "clock-assumption", Statement: "Clocks are perfectly synchronized."}}

	broken := doctorTask(plan, "broken")
	broken.Outcome = "Clocks are perfectly synchronized."
	broken.Acceptance = append(broken.Acceptance, deliveryAcceptance{ID: "A2", Outcome: "A second outcome is observable."})
	broken.Verification = []deliveryVerification{
		{Covers: "A1", Check: "inspect it somehow"},
		{Covers: "NOT-DECLARED", Check: "command: true"},
	}
	broken.Dependencies = []deliveryDependency{{Task: "not-a-task", Kind: "hard"}}
	broken.Artifact.Path = "../outside"
	broken.OwnedPaths = []string{"cmd/tusker/shared"}
	broken.GeneratedOutputs = []string{"generated/api.go"}
	broken.MigrationKeys = []string{"schema-0042"}
	broken.ResourceRefs = []string{"migration-slot"}

	noProof := doctorTask(plan, "no-proof")
	noProof.Verification = nil

	collider := doctorTask(plan, "collider")
	collider.OwnedPaths = []string{"cmd/tusker/shared/file.go"}
	collider.GeneratedOutputs = []string{"generated/api.go"}
	collider.MigrationKeys = []string{"schema-0042"}
	collider.ResourceRefs = []string{"migration-slot"}

	cycleA := doctorTask(plan, "cycle-a")
	cycleB := doctorTask(plan, "cycle-b")
	cycleA.Dependencies = []deliveryDependency{{Task: "cycle-b", Kind: "hard"}}
	cycleB.Dependencies = []deliveryDependency{{Task: "cycle-a", Kind: "hard"}}

	human := doctorTask(plan, "human")
	human.Acceptance = append(human.Acceptance, deliveryAcceptance{ID: "A2", Outcome: "A human confirms the second outcome."})
	human.Verification = []deliveryVerification{{Covers: " a1, a2 ", Check: "manual proof: product owner confirms both outcomes"}}
	humanChild := doctorTask(plan, "human-child")
	humanChild.Dependencies = []deliveryDependency{{Task: "human", Kind: "hard"}}

	plan.Tasks = []deliveryPlanTask{broken, noProof, collider, cycleA, cycleB, human, humanChild}
	plan.HumanGates = []deliveryHumanGate{{
		SourceKey: "human-proof", Title: "Human proof", Kind: "decision", TaskSourceKey: "human",
		AcceptanceIDs: []string{" a1 "}, DependencyClosure: []string{"not-the-child"},
	}}
	plan.OwnedPathOverlaps = []deliveryOverlapStrategy{{
		SourceKey: "fake-serialization", Tasks: []string{"broken", "collider"},
		Paths: []string{"cmd/tusker/shared"}, GeneratedOutputs: []string{"generated/api.go"},
		MigrationKeys: []string{"schema-0042"}, Resources: []string{"migration-slot"}, Strategy: "serialize",
	}}

	before := snapshotDeliveryV2Records(t, vault)
	path := writeDoctorPlan(t, vault, plan)
	first, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := deliveryPlanDoctor(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("doctor findings are not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if !reflect.DeepEqual(before, snapshotDeliveryV2Records(t, vault)) {
		t.Fatal("doctor mutated durable delivery records")
	}
	requireDoctorCodes(t, first,
		"REQUIREMENT_UNCOVERED",
		"ACCEPTANCE_UNMAPPED",
		"PROOF_UNFILLED",
		"PROOF_UNSUPPORTED",
		"PROOF_ACCEPTANCE_UNKNOWN",
		"ARTIFACT_INVALID",
		"DEPENDENCY_CYCLE",
		"DEPENDENCY_DANGLING",
		"ASSUMPTION_PRESENTED_AS_FACT",
		"UNSUPPORTED_RUNNER",
		"OWNED_PATH_FRONTIER_CONFLICT",
		"GENERATED_OUTPUT_FRONTIER_CONFLICT",
		"MIGRATION_FRONTIER_CONFLICT",
		"RESOURCE_FRONTIER_CONFLICT",
		"RESOURCE_CAPACITY_EXCEEDED",
		"OVERLAP_SERIALIZATION_UNPROVEN",
		"HUMAN_GATE_PROOF_INVALID",
		"HUMAN_GATE_ACCEPTANCE_MISMATCH",
		"HUMAN_GATE_CLOSURE_MISMATCH",
		"HUMAN_PROOF_GATE_MISSING",
		"CONCURRENCY_CAP_EXCEEDED",
	)
	for _, finding := range first.Findings {
		if finding.Code == "" || finding.Path == "" || finding.Remedy == "" {
			t.Errorf("finding lacks stable code/path/remedy: %#v", finding)
		}
		if !sort.StringsAreSorted(finding.SourceKeys) {
			t.Errorf("finding source keys are not canonical: %#v", finding)
		}
	}
}

func TestDeliveryPlanDoctorOverlapStrategiesMustProveSafety(t *testing.T) {
	tests := []struct {
		name       string
		strategy   deliveryOverlapStrategy
		integrator bool
		want       []string
		notWant    []string
	}{
		{
			name:     "serialize label without ordering",
			strategy: deliveryOverlapStrategy{SourceKey: "serial", Tasks: []string{"left", "right"}, Paths: []string{"shared"}, Strategy: "serialize"},
			want:     []string{"OVERLAP_SERIALIZATION_UNPROVEN", "OWNED_PATH_FRONTIER_CONFLICT"},
		},
		{
			name:     "unknown integrator",
			strategy: deliveryOverlapStrategy{SourceKey: "merge", Tasks: []string{"left", "right"}, Paths: []string{"shared"}, Strategy: "integrator", Integrator: "missing"},
			want:     []string{"OVERLAP_INTEGRATOR_UNKNOWN", "OWNED_PATH_FRONTIER_CONFLICT"},
		},
		{
			name:       "ordered real integrator",
			strategy:   deliveryOverlapStrategy{SourceKey: "merge", Tasks: []string{"left", "right"}, Paths: []string{"shared"}, Strategy: "integrator", Integrator: "merge"},
			integrator: true,
			notWant:    []string{"OVERLAP_INTEGRATOR_UNKNOWN", "OVERLAP_INTEGRATOR_UNORDERED", "OVERLAP_INTEGRATOR_SCOPE_MISMATCH", "OWNED_PATH_FRONTIER_CONFLICT"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vault := deliveryTestVault(t)
			plan := doctorPlan(t)
			left, right := doctorTask(plan, "left"), doctorTask(plan, "right")
			left.OwnedPaths, right.OwnedPaths = []string{"shared/left.go"}, []string{"shared"}
			plan.Tasks = []deliveryPlanTask{left, right}
			if tc.integrator {
				merge := doctorTask(plan, "merge")
				merge.OwnedPaths = []string{"shared"}
				merge.Dependencies = []deliveryDependency{{Task: "left", Kind: "hard"}, {Task: "right", Kind: "hard"}}
				plan.Tasks = append(plan.Tasks, merge)
			}
			plan.OwnedPathOverlaps = []deliveryOverlapStrategy{tc.strategy}
			report, err := deliveryPlanDoctor(vault, writeDoctorPlan(t, vault, plan))
			if err != nil {
				t.Fatal(err)
			}
			requireDoctorCodes(t, report, tc.want...)
			for _, code := range tc.notWant {
				if doctorCodes(report)[code] {
					t.Errorf("unexpected %s: %#v", code, report.Findings)
				}
			}
		})
	}
}

func TestDeliveryPlanDoctorIntegratorCannotRepairConcurrentResourceUse(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := doctorPlan(t)
	plan.SharedResources = []deliverySharedResource{{SourceKey: "migration-slot", Kind: "migration", Capacity: 1}}
	left, right := doctorTask(plan, "left"), doctorTask(plan, "right")
	left.ResourceRefs, right.ResourceRefs = []string{"migration-slot"}, []string{"migration-slot"}
	integrator := doctorTask(plan, "integrator")
	integrator.ResourceRefs = []string{"migration-slot"}
	integrator.Dependencies = []deliveryDependency{{Task: "left", Kind: "hard"}, {Task: "right", Kind: "hard"}}
	plan.Tasks = []deliveryPlanTask{left, right, integrator}
	plan.OwnedPathOverlaps = []deliveryOverlapStrategy{{
		SourceKey: "late-resource-integrator", Tasks: []string{"left", "right"},
		Resources: []string{"migration-slot"}, Strategy: "integrator", Integrator: "integrator",
	}}
	report, err := deliveryPlanDoctor(vault, writeDoctorPlan(t, vault, plan))
	if err != nil {
		t.Fatal(err)
	}
	requireDoctorCodes(t, report, "RESOURCE_CAPACITY_EXCEEDED", "RESOURCE_FRONTIER_CONFLICT")
}

func TestDeliveryPlanDoctorCanonicalizesAcceptanceIDs(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := doctorPlan(t)
	task := doctorTask(plan, "human")
	task.Acceptance = []deliveryAcceptance{{ID: " a1 ", Outcome: "A human confirms the outcome."}}
	task.Verification = []deliveryVerification{{Covers: "A1", Check: "manual proof: product owner confirms the outcome"}}
	task.Artifact.AcceptanceIDs = []string{"a1"}
	plan.Tasks = []deliveryPlanTask{task}
	plan.HumanGates = []deliveryHumanGate{{
		SourceKey: "human-proof", Title: "Human proof", Kind: "decision", Owner: "human:owner", TaskSourceKey: "human",
		AcceptanceIDs: []string{" a1 "}, Action: "Confirm the outcome.", Verification: "Record the confirmation.",
		WhyAgentCannot: "Only the product owner can make this decision.",
	}}
	report, err := deliveryPlanDoctor(vault, writeDoctorPlan(t, vault, plan))
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"ACCEPTANCE_UNMAPPED", "PROOF_ACCEPTANCE_UNKNOWN", "ARTIFACT_INVALID", "HUMAN_PROOF_GATE_MISSING", "HUMAN_GATE_ACCEPTANCE_MISMATCH"} {
		if doctorCodes(report)[code] {
			t.Errorf("canonical acceptance IDs produced %s: %#v", code, report.Findings)
		}
	}
}
