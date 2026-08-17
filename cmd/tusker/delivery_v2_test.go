package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func validDeliveryPlanV2() deliveryPlanV2 {
	return deliveryPlanV2{
		Schema: deliveryPlanV2Schema, Scope: "v2-delivery", Title: "V2 delivery", SpecRefs: []string{"docs/specs/delivery.md"},
		FactoryIntakeContractSchema: factoryIntakeContractSchema, FactoryIntakeContractVersion: "1.1.0", FactoryIntakeContractFingerprint: "sha256:15ec23480f22cb10b83bc945465abedd279e3954e777dcecb0815571799fbe18",
		Summary:      "Import a held V2 delivery wave with requirements and human-proof traceability.",
		EpicContract: &deliveryEpicContract{SourceKey: "v2-delivery", AcronymHint: "VTP", Title: "V2 delivery"},
		Requirements: []deliveryRequirement{{ID: "R1", Outcome: "The delivery plan traces executable work to requirements."}},
		Tasks:        []deliveryPlanTask{{SourceKey: "import", RequirementRefs: []string{"R1"}, Title: "Import V2", Outcome: "A V2 plan imports atomically.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "The import creates held source-keyed records."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run '^TestDeliveryPlanV2' -count=1"}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "cmd/tusker/delivery_v2.go", Summary: "V2 import contract.", AcceptanceIDs: []string{"A1"}}}},
		HumanGates:   []deliveryHumanGate{{SourceKey: "product-signoff", Title: "Confirm product choice", Kind: "decision", Owner: "human:sarav", TaskSourceKey: "import", AcceptanceIDs: []string{"A1"}, DependencyClosure: []string{}, Action: "Choose the preferred product option.", Verification: "The choice is recorded in the governing specification.", WhyAgentCannot: "A product owner must make this preference decision.", Suggestion: "Use the option described by the requirement outcome."}},
	}
}

func writeDeliveryV2TestPlan(t *testing.T, vault string, plan deliveryPlanV2) string {
	t.Helper()
	for _, destination := range []string{filepath.Join(v7RepoRoot(vault), ".agents", "skills", "tusker"), filepath.Join(v7RepoRoot(vault), ".claude", "skills", "tusker")} {
		if err := installSkillPayloadCopy(destination); err != nil {
			t.Fatal(err)
		}
	}
	if plan.ContextFingerprint == "" {
		plan.ContextFingerprint = deliveryPlanV2ContextFingerprint(t, vault, plan)
	}
	raw, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "delivery-plan-v2.yaml")
	if err := writeText(path, string(raw)); err != nil {
		t.Fatal(err)
	}
	return path
}

func deliveryPlanV2ContextFingerprint(t *testing.T, vault string, plan deliveryPlanV2) string {
	t.Helper()
	context, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
	if err != nil {
		t.Fatal(err)
	}
	return context.ContextFingerprint
}

func operationalDeliveryPlanV2() deliveryPlanV2 {
	plan := validDeliveryPlanV2()
	plan.Summary = "Persist the exact operational contract on held tasks and their wave."
	plan.NonGoals = []string{"No release automation is included in this delivery."}
	plan.Concurrency = 1
	plan.SharedResources = []deliverySharedResource{{SourceKey: "schema-slot", Kind: "migration", Capacity: 1}}
	plan.Assumptions = []deliveryPlanAssumption{{SourceKey: "toolchain-ready", Statement: "The configured Go toolchain is available."}}
	plan.UnresolvedDecisions = []deliveryUnresolvedDecision{{SourceKey: "rollout-owner", Question: "Who owns the downstream rollout?"}}

	first := plan.Tasks[0]
	first.OwnedPaths = []string{"cmd/tusker/shared_import.go"}
	first.GeneratedOutputs = []string{"generated/import_api.go"}
	first.MigrationKeys = []string{"schema-0042"}
	first.ResourceRefs = []string{"schema-slot"}

	second := first
	second.SourceKey = "integrate"
	second.Title = "Integrate V2"
	second.Outcome = "The imported operational contract is integrated."
	second.Acceptance = []deliveryAcceptance{{ID: "A1", Outcome: "The integrated records retain every operational claim."}}
	second.Dependencies = []deliveryDependency{{Task: "import", Kind: "hard"}}
	second.Artifact = deliveryArtifactContract{Kind: "diff_summary", Path: "cmd/tusker/delivery_v2_test.go", Summary: "Operational persistence regression.", AcceptanceIDs: []string{"A1"}}
	plan.Tasks = []deliveryPlanTask{first, second}
	plan.OwnedPathOverlaps = []deliveryOverlapStrategy{{
		SourceKey: "integrated-import", Tasks: []string{"import", "integrate"},
		Paths: []string{"cmd/tusker/shared_import.go"}, GeneratedOutputs: []string{"generated/import_api.go"},
		MigrationKeys: []string{"schema-0042"}, Resources: []string{"schema-slot"}, Strategy: "integrator", Integrator: "integrate",
	}}
	plan.HumanGates[0].DependencyClosure = []string{"integrate"}
	return plan
}

func TestDeliveryPlanV2OperationalPersistence(t *testing.T) {
	originalNow := deliveryImportNow
	currentNow := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	deliveryImportNow = func() time.Time { return currentNow }
	t.Cleanup(func() { deliveryImportNow = originalNow })

	vault := deliveryTestVault(t)
	plan := operationalDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	beforeDryRun := snapshotDeliveryV2Records(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "dry-run": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeDryRun, snapshotDeliveryV2Records(t, vault), "V2 dry-run remains read only")

	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertDeliveryV2OperationalProjection(t, vault, plan)

	firstImport := snapshotDeliveryV2Records(t, vault)
	currentNow = currentNow.Add(time.Hour)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, firstImport, snapshotDeliveryV2Records(t, vault), "unchanged held operational import converges")

	changed := plan
	changed.Summary = "Persist the revised operational contract without stale claims."
	changed.SharedResources = []deliverySharedResource{{SourceKey: "schema-slot-v2", Kind: "migration", Capacity: 2}}
	changed.Assumptions = []deliveryPlanAssumption{{SourceKey: "toolchain-ready-v2", Statement: "The configured Go toolchain and generator are available."}}
	changed.UnresolvedDecisions = []deliveryUnresolvedDecision{{SourceKey: "rollout-owner-v2", Question: "Who owns the staged downstream rollout?"}}
	for i := range changed.Tasks {
		changed.Tasks[i].GeneratedOutputs = []string{"generated/import_api_v2.go"}
		changed.Tasks[i].MigrationKeys = []string{"schema-0043"}
		changed.Tasks[i].ResourceRefs = []string{"schema-slot-v2"}
	}
	changed.OwnedPathOverlaps = []deliveryOverlapStrategy{{
		SourceKey: "integrated-import-v2", Tasks: []string{"import", "integrate"},
		Paths: []string{"cmd/tusker/shared_import.go"}, GeneratedOutputs: []string{"generated/import_api_v2.go"},
		MigrationKeys: []string{"schema-0043"}, Resources: []string{"schema-slot-v2"}, Strategy: "integrator", Integrator: "integrate",
	}}
	path = writeDeliveryV2TestPlan(t, vault, changed)
	currentNow = currentNow.Add(time.Hour)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertDeliveryV2OperationalProjection(t, vault, changed)
	for record, content := range snapshotDeliveryV2Records(t, vault) {
		for _, stale := range []string{"generated/import_api.go", "schema-0042", "schema-slot\"", "integrated-import\"", "toolchain-ready\"", "rollout-owner\""} {
			if strings.Contains(content, stale) {
				t.Fatalf("changed held import retained stale operational value %q in %s:\n%s", stale, record, content)
			}
		}
	}

	changedImport := snapshotDeliveryV2Records(t, vault)
	currentNow = currentNow.Add(time.Hour)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, changedImport, snapshotDeliveryV2Records(t, vault), "revised held operational import converges")
}

func assertDeliveryV2OperationalProjection(t *testing.T, vault string, plan deliveryPlanV2) {
	t.Helper()
	if plan.ContextFingerprint == "" {
		plan.ContextFingerprint = deliveryPlanV2ContextFingerprint(t, vault, plan)
	}
	for i, planned := range plan.Tasks {
		taskID := "VTP-T-" + padNumber(i+1)
		task, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", taskID+".md"))
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, planned.GeneratedOutputs, normalizeList(task["generated_outputs"]), taskID+" generated outputs")
		assertEqual(t, planned.MigrationKeys, normalizeList(task["migration_keys"]), taskID+" migration keys")
		assertEqual(t, planned.ResourceRefs, normalizeList(task["resource_refs"]), taskID+" resource refs")
	}

	wave, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, plan.Summary, stringField(wave, "summary"), "wave delivery summary")
	rawPlan, err := yaml.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, plan.Scope, stringField(wave, "delivery_plan_scope"), "wave delivery plan scope provenance")
	assertEqual(t, deliveryPlanV2Schema, stringField(wave, "delivery_plan_schema"), "wave delivery plan schema provenance")
	assertEqual(t, deliveryFingerprint(rawPlan), stringField(wave, "delivery_plan_fingerprint"), "wave delivery plan fingerprint provenance")
	assertEqual(t, plan.ContextFingerprint, stringField(wave, "context_fingerprint"), "wave planning context fingerprint provenance")
	assertEqual(t, plan.NonGoals, normalizeList(wave["non_goals"]), "wave authored non-goals")
	requirement := deliveryV2TestRow(t, wave, "requirements", 0)
	assertEqual(t, plan.Requirements[0].ID, stringField(requirement, "id"), "structured requirement id")
	assertEqual(t, plan.Requirements[0].Outcome, stringField(requirement, "outcome"), "structured requirement outcome")
	resource := deliveryV2TestRow(t, wave, "shared_resources", 0)
	assertEqual(t, plan.SharedResources[0].SourceKey, stringField(resource, "source_key"), "shared resource source key")
	assertEqual(t, plan.SharedResources[0].Kind, stringField(resource, "kind"), "shared resource kind")
	assertEqual(t, plan.SharedResources[0].Capacity, intField(resource, "capacity"), "shared resource capacity")
	overlap := deliveryV2TestRow(t, wave, "owned_path_overlaps", 0)
	assertEqual(t, plan.OwnedPathOverlaps[0].SourceKey, stringField(overlap, "source_key"), "overlap source key")
	assertEqual(t, plan.OwnedPathOverlaps[0].Tasks, normalizeList(overlap["tasks"]), "overlap tasks")
	assertEqual(t, plan.OwnedPathOverlaps[0].Paths, normalizeList(overlap["paths"]), "overlap paths")
	assertEqual(t, plan.OwnedPathOverlaps[0].GeneratedOutputs, normalizeList(overlap["generated_outputs"]), "overlap generated outputs")
	assertEqual(t, plan.OwnedPathOverlaps[0].MigrationKeys, normalizeList(overlap["migration_keys"]), "overlap migration keys")
	assertEqual(t, plan.OwnedPathOverlaps[0].Resources, normalizeList(overlap["resources"]), "overlap resources")
	assertEqual(t, plan.OwnedPathOverlaps[0].Strategy, stringField(overlap, "strategy"), "overlap strategy")
	assertEqual(t, plan.OwnedPathOverlaps[0].Integrator, stringField(overlap, "integrator"), "overlap integrator")
	assumption := deliveryV2TestRow(t, wave, "assumptions", 0)
	assertEqual(t, plan.Assumptions[0].SourceKey, stringField(assumption, "source_key"), "assumption source key")
	assertEqual(t, plan.Assumptions[0].Statement, stringField(assumption, "statement"), "assumption statement")
	decision := deliveryV2TestRow(t, wave, "unresolved_decisions", 0)
	assertEqual(t, plan.UnresolvedDecisions[0].SourceKey, stringField(decision, "source_key"), "unresolved decision source key")
	assertEqual(t, plan.UnresolvedDecisions[0].Question, stringField(decision, "question"), "unresolved decision question")

	gate, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "VTP-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, plan.HumanGates[0].DependencyClosure, normalizeList(gate["dependency_closure"]), "human gate dependency closure")
}

func deliveryV2TestRow(t *testing.T, data map[string]any, field string, index int) map[string]any {
	t.Helper()
	rows, ok := data[field].([]any)
	if !ok || index < 0 || index >= len(rows) {
		t.Fatalf("%s is not a structured row list: %#v", field, data[field])
	}
	row, ok := rows[index].(map[string]any)
	if !ok {
		t.Fatalf("%s[%d] is not a structured row: %#v", field, index, rows[index])
	}
	return row
}

func TestDeliveryPlanV2RequirementsEpicGatesAndConvergence(t *testing.T) {
	originalNow := deliveryImportNow
	currentNow := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	deliveryImportNow = func() time.Time { return currentNow }
	t.Cleanup(func() { deliveryImportNow = originalNow })

	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, validDeliveryPlanV2())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	epic, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "epics", "VTP.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "v2-delivery", stringField(epic, "delivery_source_key"), "allocated epic source key")
	taskPath := filepath.Join(vault, "work", "tasks", "VTP-T-0001.md")
	task, _, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"R1"}, normalizeList(task["requirement_refs"]), "requirement trace")
	assertEqual(t, []string{"VTP-G-0001"}, normalizeList(task["gates"]), "source-keyed gate")
	gate, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "VTP-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, []string{"VTP-T-0001"}, normalizeList(gate["blocks"]), "gate exact task")
	assertEqual(t, []string{"A1"}, normalizeList(gate["covers"]), "gate exact acceptance")
	before := snapshotDeliveryV2Records(t, vault)
	currentNow = currentNow.Add(24 * time.Hour)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "unchanged import converges")
	changed := validDeliveryPlanV2()
	changed.Tasks[0].Outcome = "A changed V2 contract updates the allocated task."
	path = writeDeliveryV2TestPlan(t, vault, changed)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertContainsIndexTest(t, mustReadIndexTest(t, taskPath), "A changed V2 contract updates the allocated task.")
	changed.HumanGates = nil
	path = writeDeliveryV2TestPlan(t, vault, changed)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	obsolete, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "gates", "VTP-G-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "obsolete", stringField(obsolete, "status"), "removed source keyed gate is retained as obsolete history")
}

func TestDeliveryPlanV2ChangedGateCannotBypassProgressedTaskContract(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, validDeliveryPlanV2())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(vault, "work", "tasks", "VTP-T-0001.md")
	data, body, err := parseFrontmatterMustRead(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"], data["readiness"] = "ready", "ready"
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(taskPath, content); err != nil {
		t.Fatal(err)
	}
	before := snapshotDeliveryV2Records(t, vault)
	changed := validDeliveryPlanV2()
	changed.HumanGates[0].Action = "Choose a materially different product option."
	path = writeDeliveryV2TestPlan(t, vault, changed)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "progressed beyond held state") {
		t.Fatalf("expected progressed contract refusal, got %v", err)
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "gate contract refusal is atomic")
}

func TestDeliveryPlanV2RejectsUnknownAndInvalidRequirementsWithoutWrites(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, validDeliveryPlanV2())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\nwave_id: W-0099\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "field wave_id not found") {
		t.Fatalf("expected strict V2 unknown field rejection, got %v", err)
	}
	if fileExists(filepath.Join(vault, "work", "waves", "W-0001.md")) {
		t.Fatal("strict decode wrote records")
	}
	plan := validDeliveryPlanV2()
	plan.Tasks[0].RequirementRefs = []string{"R9"}
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "unknown requirement") {
		t.Fatalf("expected unknown requirement rejection, got %v", err)
	}
	plan = validDeliveryPlanV2()
	plan.ContextFingerprint = "sha256:not-a-context-fingerprint"
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "context_fingerprint") {
		t.Fatalf("expected invalid authored context rejection, got %v", err)
	}
	plan = validDeliveryPlanV2()
	plan.FactoryIntakeContractSchema = ""
	plan.FactoryIntakeContractVersion = ""
	plan.FactoryIntakeContractFingerprint = ""
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "requires current factory-intake contract") {
		t.Fatalf("expected unclaimed current V2 rejection, got %v", err)
	}
	plan = validDeliveryPlanV2()
	plan.NonGoals = []string{"TBD"}
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "non_goals") {
		t.Fatalf("expected invalid non-goal rejection, got %v", err)
	}
}

func TestDeliveryPlanV2RollbackIncludesEpicAndGate(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryV2TestPlan(t, vault, validDeliveryPlanV2())
	err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true", "fail-after-first-write": "true"})
	if err == nil || !strings.Contains(err.Error(), "forced") {
		t.Fatalf("expected forced failure, got %v", err)
	}
	for _, path := range []string{filepath.Join(vault, "work", "epics", "VTP.md"), filepath.Join(vault, "work", "tasks", "VTP-T-0001.md"), filepath.Join(vault, "work", "gates", "VTP-G-0001.md"), filepath.Join(vault, "work", "waves", "W-0001.md")} {
		if fileExists(path) {
			t.Fatalf("rollback retained %s", path)
		}
	}
}

func TestDeliveryPlanV2PreservesV1ImportPath(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "dry-run": "true", "quiet": "true"}); err != nil {
		t.Fatalf("V1 dry-run regressed: %v", err)
	}
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatalf("V1 import regressed: %v", err)
	}
	if !fileExists(filepath.Join(vault, "work", "tasks", "APP-T-0001.md")) {
		t.Fatal("V1 import did not allocate its existing task identity")
	}
}

func TestDeliveryPlanV2MigratesHeldV1WaveWithoutIdentityDrift(t *testing.T) {
	vault := deliveryTestVault(t)
	legacy := validDeliveryPlan()
	legacy.RunnerProfile = ""
	legacy.Scope = "legacy-wave/v1"
	legacyPath := writeDeliveryTestPlan(t, vault, legacy)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": legacyPath, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")

	v2 := deliveryPlanV2{
		Schema: deliveryPlanV2Schema, Scope: legacy.Scope, Title: legacy.Title, Epic: legacy.Epic, SpecRefs: legacy.SpecRefs,
		FactoryIntakeContractSchema: factoryIntakeContractSchema, FactoryIntakeContractVersion: "1.1.0", FactoryIntakeContractFingerprint: "sha256:15ec23480f22cb10b83bc945465abedd279e3954e777dcecb0815571799fbe18",
		Summary: "Migrate the held legacy wave without changing its allocated identities or dependency graph.",
		Requirements: []deliveryRequirement{
			{ID: "R1", Outcome: "The legacy schema work remains source-keyed and traceable."},
			{ID: "R2", Outcome: "The legacy CLI work retains its exact dependency contract."},
		},
		Tasks: legacy.Tasks,
	}
	v2.Tasks[0].RequirementRefs = []string{"R1"}
	v2.Tasks[1].RequirementRefs = []string{"R2"}
	v2Path := writeDeliveryV2TestPlan(t, vault, v2)
	beforeDryRun := snapshotDeliveryV2Records(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": v2Path, "dry-run": "true", "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, beforeDryRun, snapshotDeliveryV2Records(t, vault), "V1-to-V2 dry-run remains read only")
	if err := deliveryImportCmd(Args{"vault": vault, "plan": v2Path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	wave, _, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, deliveryPlanV2Schema, stringField(wave, "delivery_plan_schema"), "V2 wave provenance")
	assertEqual(t, v2.Scope, stringField(wave, "delivery_plan_scope"), "legacy scope retained")
	if stringField(wave, "context_fingerprint") == "" || stringField(wave, "factory_intake_contract_fingerprint") == "" {
		t.Fatalf("V2 provenance missing from migrated wave: %#v", wave)
	}
	assertEqual(t, []string{"APP-T-0001", "APP-T-0002"}, normalizeList(wave["members"]), "stable V1 task IDs")
	first, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(vault, "work", "tasks", "APP-T-0002.md")
	second, secondBody, err := parseFrontmatterMustRead(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "schema", stringField(first, "delivery_source_key"), "first source key")
	assertEqual(t, "cli", stringField(second, "delivery_source_key"), "second source key")
	assertEqual(t, []string{"APP-T-0001:hard"}, normalizeList(second["dependencies"]), "dependency kind and target retained")
	assertEqual(t, []string{"R1"}, normalizeList(first["requirement_refs"]), "first requirement provenance")
	assertEqual(t, []string{"R2"}, normalizeList(second["requirement_refs"]), "second requirement provenance")
	for _, pattern := range []string{filepath.Join(vault, "work", "waves", "W-*.md"), filepath.Join(vault, "work", "tasks", "APP-T-*.md")} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if want := 1; strings.Contains(pattern, "APP-T-") {
			want = 2
			assertEqual(t, want, len(paths), "migration allocated no duplicate tasks")
		} else {
			assertEqual(t, want, len(paths), "migration allocated no duplicate waves")
		}
	}

	second["status"], second["readiness"] = "ready", "ready"
	second["state_rev"] = v7StateRev(second, secondBody)
	content, err := serializeDocument(second, secondBody, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(secondPath, content); err != nil {
		t.Fatal(err)
	}
	beforeRefusal := snapshotDeliveryV2Records(t, vault)
	changed := v2
	changed.Tasks = append([]deliveryPlanTask(nil), v2.Tasks...)
	changed.Tasks[1].Outcome = "The changed V2 CLI contract must not overwrite progressed legacy work."
	v2Path = writeDeliveryV2TestPlan(t, vault, changed)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": v2Path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "progressed beyond held state") {
		t.Fatalf("expected progressed V2 migration refusal, got %v", err)
	}
	assertEqual(t, beforeRefusal, snapshotDeliveryV2Records(t, vault), "progressed V2 migration refusal is atomic")
}

func TestDeliveryPlanV2RefusesFrozenV1WaveContractSwap(t *testing.T) {
	vault := deliveryTestVault(t)
	legacy := validDeliveryPlan()
	legacy.RunnerProfile = ""
	legacy.Scope = "frozen-legacy-wave/v1"
	legacyPath := writeDeliveryTestPlan(t, vault, legacy)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": legacyPath, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	wavePath := filepath.Join(vault, "work", "waves", "W-0001.md")
	wave, body, err := parseFrontmatterMustRead(wavePath)
	if err != nil {
		t.Fatal(err)
	}
	wave["integration_base_sha"] = "frozen-reviewed-base"
	wave["state_rev"] = v7StateRev(wave, body)
	content, err := serializeDocument(wave, body, v7FrontmatterOrder["wave"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(wavePath, content); err != nil {
		t.Fatal(err)
	}
	v2 := deliveryPlanV2{
		Schema: deliveryPlanV2Schema, Scope: legacy.Scope, Title: legacy.Title, Epic: legacy.Epic, SpecRefs: legacy.SpecRefs,
		FactoryIntakeContractSchema: factoryIntakeContractSchema, FactoryIntakeContractVersion: "1.1.0", FactoryIntakeContractFingerprint: "sha256:15ec23480f22cb10b83bc945465abedd279e3954e777dcecb0815571799fbe18",
		Summary: "A V2 contract cannot replace a frozen reviewed V1 wave without explicit controlled migration authority.",
		Requirements: []deliveryRequirement{
			{ID: "R1", Outcome: "The frozen legacy schema work remains source-keyed and traceable."},
			{ID: "R2", Outcome: "The frozen legacy CLI work retains its exact dependency contract."},
		},
		Tasks: legacy.Tasks,
	}
	v2.Tasks[0].RequirementRefs = []string{"R1"}
	v2.Tasks[1].RequirementRefs = []string{"R2"}
	v2Path := writeDeliveryV2TestPlan(t, vault, v2)
	before := snapshotDeliveryV2Records(t, vault)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": v2Path, "quiet": "true"}); err == nil || !strings.Contains(err.Error(), "frozen to a different reviewed plan") {
		t.Fatalf("expected frozen V1 contract refusal, got %v", err)
	}
	assertEqual(t, before, snapshotDeliveryV2Records(t, vault), "frozen V1 contract refusal is atomic")
}

func snapshotDeliveryV2Records(t *testing.T, vault string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{"epics", "tasks", "gates", "waves"} {
		paths, err := filepath.Glob(filepath.Join(vault, "work", dir, "*.md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			out[path] = mustReadIndexTest(t, path)
		}
	}
	return out
}
