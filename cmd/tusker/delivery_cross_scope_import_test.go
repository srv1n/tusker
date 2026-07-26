package main

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func crossScopePlan(scope, acronym, source string) deliveryPlanV2 {
	plan := validDeliveryPlanV2()
	plan.Scope = scope
	plan.EpicContract = &deliveryEpicContract{SourceKey: scope + "-epic", AcronymHint: acronym, Title: scope + " epic"}
	plan.Tasks[0].SourceKey = source
	plan.Tasks[0].Dependencies = nil
	plan.HumanGates = nil
	plan.ContextFingerprint = ""
	return plan
}

func TestDeliveryCrossScopeImport(t *testing.T) {
	vault := deliveryTestVault(t)
	producer := crossScopePlan("producer/v1", "PRD", "provider")
	producerPath := writeDeliveryV2TestPlan(t, vault, producer)
	if err := deliveryV2ImportCmd(vault, producerPath, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	consumer := crossScopePlan("consumer/v1", "CON", "consumer")
	consumer.Tasks[0].Dependencies = []deliveryDependency{{Task: "provider", Kind: "hard"}}
	deliveryV2DependencyScope(&consumer.Tasks[0].Dependencies[0], "producer/v1")
	consumerPath := writeDeliveryV2TestPlan(t, vault, consumer)
	if err := deliveryV2ImportCmd(vault, consumerPath, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	var consumerNote Note
	for _, task := range idx.Tasks {
		if stringField(task.Data, "delivery_plan_scope") == "consumer/v1" {
			consumerNote = task
			break
		}
	}
	if consumerNote.Data == nil {
		t.Fatal("consumer was not imported")
	}
	assertEqual(t, []string{"PRD-T-0001:hard"}, normalizeList(consumerNote.Data["dependencies"]), "ordinary durable dependency")
	projections, err := deliveryCrossScopeProjections(consumerNote)
	if err != nil {
		t.Fatal(err)
	}
	if len(projections) != 1 {
		t.Fatalf("want one projection, got %#v", projections)
	}
	assertEqual(t, "producer/v1", projections[0].Scope, "projection scope")
	assertEqual(t, "provider", projections[0].Task, "projection source key")
	assertEqual(t, "PRD-T-0001", projections[0].TaskID, "projection durable id")
	assertEqual(t, "hard", projections[0].Kind, "projection kind")
	if projections[0].TargetContractFingerprint == "" {
		t.Fatal("projection omitted target contract fingerprint")
	}
	if strings.Contains(mustReadIndexTest(t, filepath.Join(vault, "work", "tasks", "CON-T-0001.md")), "target_state_rev") {
		t.Fatal("projection persisted target_state_rev")
	}
}

func TestDeliveryCrossScopeProjectionDoesNotRebindAndRefreshesAtomically(t *testing.T) {
	vault := deliveryTestVault(t)
	producer := crossScopePlan("producer/v1", "PRD", "provider")
	path := writeDeliveryV2TestPlan(t, vault, producer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	consumer := crossScopePlan("consumer/v1", "CON", "consumer")
	consumer.Tasks[0].Dependencies = []deliveryDependency{{Task: "provider", Kind: "hard"}}
	deliveryV2DependencyScope(&consumer.Tasks[0].Dependencies[0], "producer/v1")
	path = writeDeliveryV2TestPlan(t, vault, consumer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	// A contract rewrite must carry the inbound projection, not leave it stale.
	producer.Tasks[0].Outcome = "A changed but concrete producer contract."
	producer.ContextFingerprint = ""
	path = writeDeliveryV2TestPlan(t, vault, producer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	var consumerNote, producerNote Note
	for _, task := range idx.Tasks {
		switch stringField(task.Data, "delivery_plan_scope") {
		case "consumer/v1":
			consumerNote = task
		case "producer/v1":
			producerNote = task
		}
	}
	projections, err := deliveryCrossScopeProjections(consumerNote)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, stringField(producerNote.Data, "delivery_contract_fingerprint"), projections[0].TargetContractFingerprint, "rewrite refreshes inbound fingerprint")

	// Corruption must fail closed instead of resolving the familiar name again.
	data, body, err := parseFrontmatterMustRead(consumerNote.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	data["delivery_cross_scope_dependencies"] = deliveryCrossScopeProjectionValue([]deliveryCrossScopeDependency{{Scope: "producer/v1", Task: "provider", TaskID: "PRD-T-9999", Kind: "hard", TargetContractFingerprint: projections[0].TargetContractFingerprint}})
	data["state_rev"] = v7StateRev(data, body)
	content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(consumerNote.AbsolutePath, content); err != nil {
		t.Fatal(err)
	}
	consumer.ContextFingerprint = ""
	path = writeDeliveryV2TestPlan(t, vault, consumer)
	err = deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_EDGE_DRIFT") {
		t.Fatalf("want closed projection drift, got %v", err)
	}
}

func TestDeliveryPlanV1RejectsScopedDependency(t *testing.T) {
	_, err := readDeliveryPlanBytes([]byte("schema: tusker.delivery-plan/v1\ntasks:\n  - dependencies:\n      - task: local\n        scope: other/v1\n"))
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("V1 scope input was accepted: %v", err)
	}
}

func TestDeliveryPlanV2RejectsUnknownNestedField(t *testing.T) {
	raw := []byte("schema: tusker.delivery-plan/v2\nscope: example/v1\ntitle: Example\nepic: APP\nspec_refs: [docs/specs/example.md]\ncontext_fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000\ntasks:\n  - source_key: x\n    title: x\n    outcome: x\n    acceptance: []\n    verification: []\n    artifact:\n      kind: diff_summary\n      unknown_nested: true\n")
	var plan deliveryPlanV2
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	err := decoder.Decode(&plan)
	if err == nil || !strings.Contains(err.Error(), "unknown_nested") {
		t.Fatalf("V2 nested unknown field was accepted: %v", err)
	}
}

func TestDeliveryCrossScopeFingerprintSemanticTarget(t *testing.T) {
	task := validDeliveryPlanV2().Tasks[0]
	task.Dependencies = []deliveryDependency{{Task: "producer", Kind: "hard"}}
	deliveryV2DependencyScope(&task.Dependencies[0], "scope-a/v1")
	a := deliveryV2TaskFingerprint(task, nil)
	deliveryV2DependencyScope(&task.Dependencies[0], "scope-b/v1")
	b := deliveryV2TaskFingerprint(task, nil)
	if a == b {
		t.Fatal("semantic scope retarget did not change task contract fingerprint")
	}
	task.Dependencies[0].Kind = ""
	omitted := deliveryV2TaskFingerprint(task, nil)
	task.Dependencies[0].Kind = "hard"
	if omitted != deliveryV2TaskFingerprint(task, nil) {
		t.Fatal("default hard kind was not normalized")
	}
}

func TestDeliveryCrossScopeAtomicity(t *testing.T) {
	vault := deliveryTestVault(t)
	producer := crossScopePlan("producer/v1", "PRD", "provider")
	path := writeDeliveryV2TestPlan(t, vault, producer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	consumer := crossScopePlan("consumer/v1", "CON", "consumer")
	consumer.Tasks[0].Dependencies = []deliveryDependency{{Task: "provider", Kind: "hard"}}
	deliveryV2DependencyScope(&consumer.Tasks[0].Dependencies[0], "producer/v1")
	path = writeDeliveryV2TestPlan(t, vault, consumer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	consumerPath := filepath.Join(vault, "work", "tasks", "CON-T-0001.md")
	before := mustReadIndexTest(t, consumerPath)
	producerPath := filepath.Join(vault, "work", "tasks", "PRD-T-0001.md")
	deliveryCrossScopeAfterResolution = func() {
		raw := mustReadIndexTest(t, producerPath)
		if err := writeText(producerPath, raw+"\n<!-- raw drift -->\n"); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { deliveryCrossScopeAfterResolution = nil }()
	err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_EPOCH_STALE") {
		t.Fatalf("want stale epoch refusal, got %v", err)
	}
	assertEqual(t, before, mustReadIndexTest(t, consumerPath), "raw race cannot overwrite consumer")
}

func TestDeliveryPlanV1(t *testing.T) {
	vault := deliveryTestVault(t)
	issues, _ := validateDeliveryPlan(vault, validDeliveryPlan())
	if len(issues) != 0 {
		t.Fatalf("unchanged V1 plan rejected: %v", issues)
	}
	TestDeliveryPlanV1RejectsScopedDependency(t)
}
