package main

import (
	"bytes"
	"fmt"
	"os"
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

func TestDeliveryCrossScopePreRenameCASPreservesRawEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.md")
	if err := writeText(path, "old"); err != nil {
		t.Fatal(err)
	}
	deliveryImportBeforeRenameHook = func(string) {
		if err := writeText(path, "raw-edit"); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { deliveryImportBeforeRenameHook = nil }()
	err := writeDeliveryTransactionFileCAS(path, []byte("new"), deliveryWritePreimage{Content: []byte("old"), Mode: 0o644, Existed: true})
	if err == nil || !strings.Contains(err.Error(), "preimage changed") {
		t.Fatalf("want pre-rename CAS refusal, got %v", err)
	}
	assertEqual(t, "raw-edit", mustReadIndexTest(t, path), "raw edit must not be overwritten")
}

func TestDeliveryCrossScopeRollbackPreservesNonCooperativeBytes(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	if err := writeText(a, "old-a"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(b, "old-b"); err != nil {
		t.Fatal(err)
	}
	expected := map[string][]byte{a: []byte("old-a"), b: []byte("old-b")}
	guard := &deliveryImportWriteGuard{
		SnapshotVerify: func() error {
			for path, want := range expected {
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, want) {
					return fmt.Errorf("external drift: %s", path)
				}
			}
			return nil
		},
		SnapshotAdvance: func(path string, raw []byte) { expected[path] = append([]byte(nil), raw...) },
	}
	deliveryImportAfterWriteHook = func(index int, path string) {
		if index != 0 {
			return
		}
		if err := writeText(path, "raw-after-write"); err != nil {
			t.Fatal(err)
		}
		if err := writeText(b, "raw-unattempted"); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { deliveryImportAfterWriteHook = nil }()
	err := commitDeliveryWritesGuarded(map[string]string{a: "new-a", b: "new-b"}, 0, guard)
	if err == nil || !strings.Contains(err.Error(), "exact rollback could not be proven") {
		t.Fatalf("want fail-closed unproven rollback, got %v", err)
	}
	assertEqual(t, "raw-after-write", mustReadIndexTest(t, a), "attempted third-party bytes preserved")
	assertEqual(t, "raw-unattempted", mustReadIndexTest(t, b), "unattempted third-party bytes preserved")
}

func TestDeliveryCrossScopeAfterIndexGateEpicMutationRefuses(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	epicPath := filepath.Join(vault, "work", "epics", "VTP.md")
	gatePath := filepath.Join(vault, "work", "gates", "VTP-G-0001.md")
	deliveryCrossScopeAfterIndexLoad = func() {
		for _, target := range []string{gatePath, epicPath} {
			raw := mustReadIndexTest(t, target)
			if err := writeText(target, raw+"\n<!-- after-index raw edit -->\n"); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer func() { deliveryCrossScopeAfterIndexLoad = nil }()
	err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_STATE_REV_INVALID") {
		t.Fatalf("want indexed gate/epic byte binding refusal, got %v", err)
	}
	for _, target := range []string{gatePath, epicPath} {
		if !strings.Contains(mustReadIndexTest(t, target), "after-index raw edit") {
			t.Fatalf("raw edit overwritten: %s", target)
		}
	}
}

func TestDeliveryCrossScopeMissingNamespacesRemainDryRunReadOnly(t *testing.T) {
	vault := deliveryTestVault(t)
	gatesDir := filepath.Join(vault, "work", "gates")
	epicsDir := filepath.Join(vault, "work", "epics")
	if err := os.RemoveAll(gatesDir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(epicsDir); err != nil {
		t.Fatal(err)
	}
	plan := validDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "dry-run": "true", "quiet": "true"}); err != nil {
		t.Fatalf("dry-run with absent empty namespaces: %v", err)
	}
	for _, dir := range []string{gatesDir, epicsDir} {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Fatalf("dry-run created namespace %s: %v", dir, err)
		}
	}
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatalf("import with absent empty namespaces: %v", err)
	}
	for _, path := range []string{
		filepath.Join(gatesDir, "VTP-G-0001.md"),
		filepath.Join(epicsDir, "VTP.md"),
	} {
		if !fileExists(path) {
			t.Fatalf("import did not materialize %s", path)
		}
	}
}

func TestDeliveryCrossScopeRollbackFinalVerification(t *testing.T) {
	dir := t.TempDir()
	restored := filepath.Join(dir, "restored.md")
	if err := os.WriteFile(restored, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := deliveryWritePreimage{Content: []byte("old"), Mode: 0o600, Existed: true}
	if err := restoreDeliveryWritePreimagesOwned([]string{restored}, map[string]deliveryWritePreimage{restored: backup}, map[string]string{restored: "new"}); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "old", mustReadIndexTest(t, restored), "rollback bytes")
	info, err := os.Stat(restored)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("rollback mode = %v err=%v", info.Mode().Perm(), err)
	}

	deleted := filepath.Join(dir, "deleted.md")
	if err := writeText(deleted, "new"); err != nil {
		t.Fatal(err)
	}
	if err := restoreDeliveryWritePreimagesOwned([]string{deleted}, map[string]deliveryWritePreimage{deleted: {Mode: 0o644}}, map[string]string{deleted: "new"}); err != nil {
		t.Fatal(err)
	}
	if fileExists(deleted) {
		t.Fatal("rollback-created file still exists")
	}

	drifted := filepath.Join(dir, "drifted.md")
	if err := writeText(drifted, "new"); err != nil {
		t.Fatal(err)
	}
	deliveryImportRollbackAfterRestoreHook = func(path string) {
		if path == drifted {
			if err := writeText(path, "post-restore-drift"); err != nil {
				t.Fatal(err)
			}
		}
	}
	defer func() { deliveryImportRollbackAfterRestoreHook = nil }()
	err = restoreDeliveryWritePreimagesOwned([]string{drifted}, map[string]deliveryWritePreimage{drifted: {Content: []byte("old"), Mode: 0o644, Existed: true}}, map[string]string{drifted: "new"})
	if err == nil || !strings.Contains(err.Error(), "restored bytes differ") {
		t.Fatalf("post-restore drift was not detected: %v", err)
	}
	assertEqual(t, "post-restore-drift", mustReadIndexTest(t, drifted), "post-restore third-party bytes preserved")
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
	deliveryImportAfterPrecheckHook = func() {
		raw := mustReadIndexTest(t, producerPath)
		if err := writeText(producerPath, raw+"\n<!-- raw drift -->\n"); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { deliveryImportAfterPrecheckHook = nil }()
	err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_EPOCH_STALE") {
		t.Fatalf("want stale epoch refusal, got %v", err)
	}
	assertEqual(t, before, mustReadIndexTest(t, consumerPath), "raw race cannot overwrite consumer")
}
