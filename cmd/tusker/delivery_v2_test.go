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
		Summary:      "Import a held V2 delivery wave with requirements and human-proof traceability.",
		EpicContract: &deliveryEpicContract{SourceKey: "v2-delivery", AcronymHint: "VTP", Title: "V2 delivery"},
		Requirements: []deliveryRequirement{{ID: "R1", Outcome: "The delivery plan traces executable work to requirements."}},
		Tasks:        []deliveryPlanTask{{SourceKey: "import", RequirementRefs: []string{"R1"}, Title: "Import V2", Outcome: "A V2 plan imports atomically.", Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: "The import creates held source-keyed records."}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run '^TestDeliveryPlanV2' -count=1"}}, Artifact: deliveryArtifactContract{Kind: "diff_summary", Path: "cmd/tusker/delivery_v2.go", Summary: "V2 import contract.", AcceptanceIDs: []string{"A1"}}}},
		HumanGates:   []deliveryHumanGate{{SourceKey: "product-signoff", Title: "Confirm product choice", Kind: "decision", Owner: "human:sarav", TaskSourceKey: "import", AcceptanceIDs: []string{"A1"}, DependencyClosure: []string{}, Action: "Choose the preferred product option.", Verification: "The choice is recorded in the governing specification.", WhyAgentCannot: "A product owner must make this preference decision.", Suggestion: "Use the option described by the requirement outcome."}},
	}
}

func writeDeliveryV2TestPlan(t *testing.T, vault string, plan deliveryPlanV2) string {
	t.Helper()
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
