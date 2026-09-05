package main

import (
	"strings"
	"testing"
)

func strictVerificationPlan() deliveryPlanV2 {
	plan := validDeliveryPlanV2()
	plan.RequiredCapabilities = []string{strictV2ProofAuthorityCapability}
	plan.Tasks[0].Acceptance = []deliveryAcceptance{{ID: "a1", Outcome: "The exact outcome remains canonical."}, {ID: "A2", Outcome: "The human choice remains bound."}}
	plan.Tasks[0].Verification = []deliveryVerification{
		{Covers: "A1", Check: "command: go test ./cmd/tusker -run '^TestExact$' -count=1"},
		{Covers: "A2", Check: "manual proof: product owner records the choice"},
	}
	plan.HumanGates[0].AcceptanceIDs = []string{"a2"}
	return plan
}

func TestDeliveryCanonicalVerificationContract(t *testing.T) {
	plan := strictVerificationPlan()
	contract, err := deliveryCanonicalProofContract(plan, plan.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	if contract.Schema != deliveryProofContractSchema || contract.Scope != plan.Scope || contract.SourceKey != "import" || contract.DeliveryTaskFingerprint != deliveryV2TaskFingerprint(plan.Tasks[0], plan.HumanGates) || contract.Fingerprint == "" {
		t.Fatalf("missing authoritative binding: %#v", contract)
	}
	if got := contract.Acceptance[0]; got.ID != "A1" || got.Outcome != "The exact outcome remains canonical." {
		t.Fatalf("acceptance normalization=%#v", got)
	}
	var manual deliveryCanonicalVerification
	for _, row := range contract.Verification {
		if row.Type == "manual" {
			manual = row
		}
	}
	if manual.Text != "product owner records the choice" || manual.ManualGateSourceKey != "product-signoff" || strings.Join(manual.Covers, ",") != "A2" {
		t.Fatalf("manual mapping=%#v", manual)
	}
	ungated := plan
	ungated.HumanGates = append([]deliveryHumanGate(nil), plan.HumanGates...)
	ungated.HumanGates[0].TaskSourceKey = "other-task"
	if _, err := deliveryCanonicalProofContract(ungated, ungated.Tasks[0]); err == nil {
		t.Fatal("manual proof without the exact source-keyed gate was accepted")
	}
	changed := plan
	changed.Tasks = append([]deliveryPlanTask(nil), plan.Tasks...)
	changed.Tasks[0].Verification = append([]deliveryVerification(nil), plan.Tasks[0].Verification...)
	changed.Tasks[0].Verification[0].Check = "command: go test ./cmd/tusker -run '^Substituted$' -count=1"
	drifted, err := deliveryCanonicalProofContract(changed, changed.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	if drifted.Fingerprint == contract.Fingerprint {
		t.Fatal("substituted command retained proof authority")
	}
	if deliveryCapabilityAvailable(strictV2ProofAuthorityCapability) || deliveryRequireCapabilities(plan.RequiredCapabilities) == nil {
		t.Fatal("canonical seam opened the public strict capability fence")
	}
}

func TestWaveCanonicalVerificationMaterial(t *testing.T) {
	plan := strictVerificationPlan()
	contract, err := deliveryCanonicalProofContract(plan, plan.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := deliveryStrictLineageFor(plan, plan.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	waveLineage, err := deliveryStrictWaveLineageFor(deliveryPlan{v2: &plan, Tasks: plan.Tasks}, deliveryImportReport{TaskMapping: map[string]string{"import": "VTP-T-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	task := Note{Data: map[string]any{"id": "VTP-T-0001", "title": "strict", "epic": "VTP", "risk": "critical", "dependencies": []string{}, "spec_refs": []string{}, "delivery_contract_fingerprint": contract.DeliveryTaskFingerprint, "delivery_proof_contract": contract, "delivery_proof_contract_fingerprint": contract.Fingerprint, "delivery_proof_results": deliveryProofResultsFor(contract), "delivery_strict_import_lineage": lineage, "delivery_strict_import_lineage_fingerprint": lineage.Fingerprint, "proof_mode": "inline", "proof_required": []string{}}, Body: "# strict"}
	wave := Note{Data: map[string]any{"id": "VTP-W-0001", "members": []string{"VTP-T-0001"}, "delivery_strict_import_lineage": waveLineage, "delivery_strict_import_lineage_fingerprint": waveLineage.Fingerprint}}
	idx := v7Index{Tasks: map[string]Note{"VTP-T-0001": task}}
	first, issues := waveMaterialFingerprint(t.TempDir(), idx, wave)
	if len(issues) != 0 {
		t.Fatalf("material issues=%v", issues)
	}
	results := task.Data["delivery_proof_results"].(deliveryProofResultsProjection)
	results.Rows[0].Result = "passed"
	results.Rows[0].Notes = "mutable operator note"
	task.Data["delivery_proof_results"] = results
	idx.Tasks["VTP-T-0001"] = task
	second, _ := waveMaterialFingerprint(t.TempDir(), idx, wave)
	if second != first {
		t.Fatalf("mutable result projection staled wave: %s != %s", second, first)
	}
	task.Data["delivery_proof_contract_fingerprint"] = "sha256:substituted"
	idx.Tasks["VTP-T-0001"] = task
	third, _ := waveMaterialFingerprint(t.TempDir(), idx, wave)
	if third == first {
		t.Fatal("canonical contract drift did not stale wave material")
	}
	task.Data["delivery_proof_contract_fingerprint"] = contract.Fingerprint
	task.Data["delivery_proof_contract"] = deliveryProofContract{Schema: deliveryProofContractSchema, Fingerprint: contract.Fingerprint}
	idx.Tasks["VTP-T-0001"] = task
	fourth, _ := waveMaterialFingerprint(t.TempDir(), idx, wave)
	if fourth == first {
		t.Fatal("canonical projection substitution did not stale wave material")
	}
	task.Data["delivery_proof_contract"] = contract
	idx.Tasks["VTP-T-0001"] = task
	wave.Data["delivery_strict_import_lineage_fingerprint"] = "sha256:lineage-substitution"
	fifth, _ := waveMaterialFingerprint(t.TempDir(), idx, wave)
	if fifth == first {
		t.Fatal("strict import lineage substitution did not stale wave material")
	}
}

func TestDeliveryVerificationContractAdoption(t *testing.T) {
	plan := strictVerificationPlan()
	contract, err := deliveryCanonicalProofContract(plan, plan.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]any{"status": "backlog", "readiness": "held"}
	installed := map[string]any{}
	if err := deliveryInstallStrictProofContract(installed, held, true, contract); err != nil {
		t.Fatal(err)
	}
	if _, err := deliveryProofContractFromData(installed); err != nil {
		t.Fatal(err)
	}
	lineage, err := deliveryStrictLineageFor(plan, plan.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	strictPlan := deliveryPlan{v2: &plan, Tasks: plan.Tasks}
	waveLineage, err := deliveryStrictWaveLineageFor(strictPlan, deliveryImportReport{TaskMapping: map[string]string{"import": "VTP-T-0001"}})
	if err != nil {
		t.Fatal(err)
	}
	taskData := map[string]any{}
	for key, value := range installed {
		taskData[key] = value
	}
	taskData["id"] = "VTP-T-0001"
	taskData["delivery_strict_import_lineage"] = lineage
	taskData["delivery_strict_import_lineage_fingerprint"] = lineage.Fingerprint
	waveData := map[string]any{"delivery_strict_import_lineage": waveLineage, "delivery_strict_import_lineage_fingerprint": waveLineage.Fingerprint}
	if state, err := deliveryStrictAuthorityFor(Note{Data: taskData}, Note{Data: waveData}); state != deliveryStrictAuthorityCurrent || err != nil {
		t.Fatalf("strict authority resolution=%s err=%v", state, err)
	}
	delete(taskData, "delivery_proof_contract")
	if state, _ := deliveryStrictAuthorityFor(Note{Data: taskData}, Note{Data: waveData}); state != deliveryStrictAuthorityCorrupt {
		t.Fatalf("deleted proof contract downgraded strict task to %s", state)
	}
	taskData["delivery_proof_contract"] = installed["delivery_proof_contract"]
	delete(taskData, "delivery_strict_import_lineage")
	if state, _ := deliveryStrictAuthorityFor(Note{Data: taskData}, Note{Data: waveData}); state != deliveryStrictAuthorityCorrupt {
		t.Fatalf("deleted task lineage downgraded strict task to %s", state)
	}
	taskData["delivery_strict_import_lineage"] = lineage
	delete(waveData, "delivery_strict_import_lineage_fingerprint")
	if state, _ := deliveryStrictAuthorityFor(Note{Data: taskData}, Note{Data: waveData}); state != deliveryStrictAuthorityCorrupt {
		t.Fatalf("deleted wave lineage downgraded strict task to %s", state)
	}
	second := map[string]any{}
	if err := deliveryInstallStrictProofContract(second, installed, true, contract); err != nil {
		t.Fatal(err)
	}
	if stringField(second, "delivery_proof_contract_fingerprint") != contract.Fingerprint {
		t.Fatal("held re-import did not preserve canonical contract")
	}
	changed := plan
	changed.Tasks = append([]deliveryPlanTask(nil), plan.Tasks...)
	changed.Tasks[0].Verification = append([]deliveryVerification(nil), plan.Tasks[0].Verification...)
	changed.Tasks[0].Verification[0].Check = "command: go test ./cmd/tusker -run '^Fresh$' -count=1"
	fresh, err := deliveryCanonicalProofContract(changed, changed.Tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	reimported := map[string]any{}
	if err := deliveryInstallStrictProofContract(reimported, second, true, fresh); err != nil {
		t.Fatal(err)
	}
	if results := reimported["delivery_proof_results"].(deliveryProofResultsProjection); results.ContractFingerprint != fresh.Fingerprint || results.Rows[0].Result != "pending" {
		t.Fatalf("changed held source retained mutable legacy result: %#v", results)
	}
	progressed := map[string]any{"status": "done", "readiness": "done", "delivery_contract_fingerprint": contract.DeliveryTaskFingerprint}
	if err := deliveryInstallStrictProofContract(map[string]any{}, progressed, false, contract); err == nil || !strings.Contains(err.Error(), "reopen/rework") {
		t.Fatalf("pre-strict terminal task was adopted without rework: %v", err)
	}
	tampered := map[string]any{"delivery_proof_contract": installed["delivery_proof_contract"], "delivery_proof_contract_fingerprint": "sha256:marker-substitution"}
	if _, err := deliveryProofContractFromData(tampered); err == nil {
		t.Fatal("mutable marker substitution was accepted")
	}
}
