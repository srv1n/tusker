package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

type crossScopePolicyFixture struct {
	Vault        string
	ProducerID   string
	ConsumerID   string
	ProducerWave string
	ConsumerWave string
	ConsumerPlan string
}

func TestCrossScopeWavePreflight(t *testing.T) {
	fixture := newCrossScopePolicyFixture(t, deliveryTestVault(t), false)
	idx, err := loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves[fixture.ConsumerWave]
	report := buildWavePreflight(fixture.Vault, idx, wave, greenWaveEnvironment())
	if !report.OK {
		t.Fatalf("qualified incomplete producer blocked wave preflight: %#v", report.Blockers)
	}
	external := listOfMaps(report.ExternalDependencies[fixture.ConsumerID])
	if len(external) != 1 || !boolFromAny(external[0]["qualified"]) || boolFromAny(external[0]["satisfied"]) {
		t.Fatalf("preflight did not distinguish resolved reference from incomplete lifecycle: %#v", external)
	}

	unrelatedIdx := idx
	unrelatedIdx.Tasks = cloneNoteMap(idx.Tasks)
	unrelated := idx.Tasks[fixture.ConsumerID]
	unrelated.Data = cloneMap(unrelated.Data)
	unrelated.Data["id"] = "OTH-T-0002"
	unrelated.Data["delivery_plan_scope"] = "unrelated-consumer/v1"
	projections, err := deliveryCrossScopeProjections(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	projections[0].TargetContractFingerprint = "sha256:unrelated-stale-material"
	unrelated.Data["delivery_cross_scope_dependencies"] = deliveryCrossScopeProjectionValue(projections)
	unrelatedIdx.Tasks["OTH-T-0002"] = unrelated
	unrelatedReport := buildWavePreflight(fixture.Vault, unrelatedIdx, wave, greenWaveEnvironment())
	if !unrelatedReport.OK {
		t.Fatalf("unrelated stale projection blocked a healthy consumer wave: %#v", unrelatedReport.Blockers)
	}
	idx = unrelatedIdx
	report = unrelatedReport
	idx.Waves = cloneNoteMap(idx.Waves)
	wave.Data = cloneMap(wave.Data)
	wave.Data["authorization"] = "armed"
	wave.Data["authorization_fingerprint"] = report.Fingerprint
	idx.Waves[fixture.ConsumerWave] = wave
	idx = crossScopeIndexWithTaskFields(idx, fixture.ConsumerID, map[string]any{"status": "ready", "readiness": "ready", "next_owner": "agent"})
	blocked := buildArmedWaveSnapshot(fixture.Vault, idx, wave, nil, deliveryImportNow().UTC())
	if containsString(blocked.Frontier, fixture.ConsumerID) || armedWaveStateMap(blocked)[fixture.ConsumerID] != armedWaveDependencyWaiting {
		t.Fatalf("armed consumer escaped its ordinary hard edge: %#v", blocked)
	}

	idx = crossScopeIndexWithTaskFields(idx, fixture.ProducerID, map[string]any{"status": "done", "readiness": "done", "proof_status": "satisfied"})
	unblocked := buildArmedWaveSnapshot(fixture.Vault, idx, wave, nil, deliveryImportNow().UTC())
	if !containsString(unblocked.Frontier, fixture.ConsumerID) {
		t.Fatalf("completed producer did not unlock the armed consumer frontier: %#v", unblocked)
	}
}

func TestCrossScopeDependencyEligibility(t *testing.T) {
	fixture := newCrossScopePolicyFixture(t, deliveryTestVault(t), false)
	base, err := loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	base = crossScopeIndexWithTaskFields(base, fixture.ConsumerID, map[string]any{"status": "ready", "readiness": "ready", "next_owner": "agent"})
	base.Tasks["OTH-T-0001"] = Note{Data: map[string]any{
		"schema": "tusker.task/v7", "kind": "task", "id": "OTH-T-0001", "project": v7ProjectID(fixture.Vault),
		"status": "ready", "readiness": "ready", "next_owner": "agent", "dependencies": []string{},
	}}

	assertProjection := func(name string, idx v7Index, wantConsumer string) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			consumer := idx.Tasks[fixture.ConsumerID]
			got := stringField(v7ProjectedTaskState(fixture.Vault, consumer, idx), "readiness")
			if got != wantConsumer {
				t.Fatalf("consumer readiness=%q, want %q", got, wantConsumer)
			}
			unrelated := idx.Tasks["OTH-T-0001"]
			if got := stringField(v7ProjectedTaskState(fixture.Vault, unrelated, idx), "readiness"); got != "ready" {
				t.Fatalf("unrelated task inherited cross-scope blocker: %q", got)
			}
		})
	}

	assertProjection("incomplete", base, "blocked_by_dependency")

	failed := crossScopeIndexWithTaskFields(base, fixture.ProducerID, map[string]any{
		"status": "done", "readiness": "done", "proof_status": "satisfied", buildFailedField: true,
	})
	assertProjection("failed", failed, "held")

	discarded := crossScopeIndexWithTaskFields(base, fixture.ProducerID, map[string]any{
		"status": "cancelled", "readiness": "cancelled", "discarded_at": "2026-07-26T00:00:00Z",
	})
	assertProjection("discarded", discarded, "blocked_by_dependency")

	stale := crossScopeIndexWithTaskFields(base, fixture.ProducerID, map[string]any{
		"status": "done", "readiness": "done", "delivery_contract_fingerprint": "sha256:stale-target-material",
	})
	assertProjection("stale-target", stale, "blocked_by_dependency")
	if edge, blocked := v7BlockingDependencyForReadiness(stale.Tasks[fixture.ConsumerID], stale); !blocked || edge.ID != fixture.ProducerID {
		t.Fatalf("stale target did not produce the stable ordinary-edge blocker: %#v blocked=%t", edge, blocked)
	}

	soft := crossScopeIndexWithTaskFields(stale, fixture.ConsumerID, map[string]any{
		"status": "review", "readiness": "waiting_on_review", "proof_status": "satisfied",
	})
	const softCallerID = "CON-T-0002"
	softCaller := soft.Tasks[fixture.ConsumerID]
	softCaller.Data = cloneMap(softCaller.Data)
	softCaller.Data["id"] = softCallerID
	softCaller.Data["delivery_source_key"] = "soft-caller"
	softCaller.Data["delivery_contract_fingerprint"] = "sha256:soft-caller-contract"
	softCaller.Data["status"] = "ready"
	softCaller.Data["readiness"] = "ready"
	softCaller.Data["next_owner"] = "agent"
	softCaller.Data["wave"] = "W-0099"
	softCaller.Data["dependencies"] = []string{fixture.ConsumerID + ":soft"}
	delete(softCaller.Data, "delivery_cross_scope_dependencies")
	soft.Tasks[softCallerID] = softCaller
	if edge, blocked := v7BlockingDependencyForReadiness(softCaller, soft); blocked {
		t.Fatalf("stale material beyond a provisional local soft edge relocked caller: %#v", edge)
	}
	if edge, blocked := v7BlockingDependencyForReadiness(soft.Tasks[fixture.ConsumerID], soft); !blocked || edge.ID != fixture.ProducerID {
		t.Fatalf("precise hard affected consumer lost its stale-material blocker: %#v blocked=%t", edge, blocked)
	}
	soft.Waves = cloneNoteMap(soft.Waves)
	softWave := soft.Waves[fixture.ConsumerWave]
	softWave.Data = cloneMap(softWave.Data)
	softWave.Data["id"] = "W-0099"
	softWave.Data["members"] = []string{softCallerID}
	softWave.Data["authorization"] = "disarmed"
	delete(softWave.Data, "authorization_fingerprint")
	soft.Waves["W-0099"] = softWave
	softReport := buildWavePreflight(fixture.Vault, soft, softWave, greenWaveEnvironment())
	if !softReport.OK {
		t.Fatalf("stale qualified material beyond local soft edge blocked preflight: %#v", softReport.Blockers)
	}

	recovered := crossScopeIndexWithTaskFields(base, fixture.ProducerID, map[string]any{
		"status": "done", "readiness": "done", "proof_status": "satisfied", buildFailedField: false,
	})
	assertProjection("recovered", recovered, "ready")

	authorized := recovered
	authorized.Waves = cloneNoteMap(recovered.Waves)
	wave := authorized.Waves[fixture.ConsumerWave]
	wave.Data = cloneMap(wave.Data)
	fingerprint, issues := waveMaterialFingerprint(fixture.Vault, authorized, wave)
	if len(issues) > 0 {
		t.Fatal(issues)
	}
	wave.Data["authorization"] = "armed"
	wave.Data["authorization_fingerprint"] = fingerprint
	authorized.Waves[fixture.ConsumerWave] = wave
	staleAuthorized := crossScopeIndexWithTaskFields(authorized, fixture.ProducerID, map[string]any{"delivery_contract_fingerprint": "sha256:drift-after-arm"})
	if got := stringField(waveAuthorizationProjection(fixture.Vault, staleAuthorized, staleAuthorized.Waves[fixture.ConsumerWave]), "state"); got != "stale" {
		t.Fatalf("affected consumer wave retained stale target authority: %q", got)
	}
	if _, blocked := v7BlockingDependencyForReadiness(staleAuthorized.Tasks["OTH-T-0001"], staleAuthorized); blocked {
		t.Fatal("unrelated task was blocked by another consumer's stale target")
	}
}

func TestCrossScopeGateOrdering(t *testing.T) {
	fixture := newCrossScopePolicyFixture(t, deliveryTestVault(t), true)
	projectID := v7ProjectID(fixture.Vault)
	setupID := "CON-G-0001"
	authID := "CON-G-0002"
	releaseID := "CON-G-0003"
	unrelatedAuthID := "PRD-G-0001"
	server := newCrossScopeServeServer(t, fixture.Vault)

	if err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, setupID, "satisfied", "human:test"); err != nil {
		t.Fatalf("setup gate was unusable before producer completion: %v", err)
	}
	if err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, unrelatedAuthID, "satisfied", "human:test"); err != nil {
		t.Fatalf("independent empty hard closure was blocked: %v", err)
	}

	for _, gateID := range []string{authID, releaseID} {
		t.Run("direct-api-"+gateID, func(t *testing.T) {
			err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, gateID, "satisfied", "human:test")
			assertCrossScopeGateRefusal(t, err)
		})
		t.Run("cli-"+gateID, func(t *testing.T) {
			err := gateV7Cmd(Args{"vault": fixture.Vault, "_pos0": "satisfy", "_pos1": gateID, "evidence": "Premature authority.", "by": "human:test", "quiet": "true"})
			assertCrossScopeHumanReceiptRequired(t, err)
		})
		t.Run("serve-"+gateID, func(t *testing.T) {
			var result serveActionResult
			servePost(t, server, "/api/gates/"+gateID+"/satisfy", fmt.Sprintf(`{"projectId":%q,"evidence":"Premature authority.","by":"human:test"}`, projectID), &result)
			if !result.Refused || result.Issue == nil || result.Issue.Code != humanControlReceiptRequiredCode {
				t.Fatalf("Serve gate path did not preserve stable refusal: %#v", result)
			}
		})
	}

	crossScopeWriteTaskFields(t, fixture.Vault, fixture.ProducerID, map[string]any{
		"status": "done", "readiness": "done", "proof_status": "satisfied",
	})
	if err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, authID, "satisfied", "human:test"); err != nil {
		t.Fatalf("auth gate rejected completed closure: %v", err)
	}
	var releaseResult serveActionResult
	servePost(t, server, "/api/gates/"+releaseID+"/satisfy", fmt.Sprintf(`{"projectId":%q,"evidence":"Current closure released.","by":"human:test"}`, projectID), &releaseResult)
	if !releaseResult.Refused || releaseResult.Issue == nil || releaseResult.Issue.Code != humanControlReceiptRequiredCode {
		t.Fatalf("raw Serve release approval bypassed native receipt: %#v", releaseResult)
	}
	if err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, releaseID, "satisfied", "human:test"); err != nil {
		t.Fatalf("trusted release receipt rejected completed closure: %v", err)
	}

	idx, err := loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, gateID := range []string{authID, releaseID, unrelatedAuthID} {
		if !v7GateAuthorityReceiptCurrent(idx.Gates[gateID], idx) {
			t.Fatalf("fresh authority receipt is not current: %s", gateID)
		}
	}
	if !v7GateAuthorityReceiptCurrent(idx.Gates[setupID], idx) || stringField(idx.Gates[setupID].Data, "dependency_material_fingerprint") != "" {
		t.Fatal("setup gate gained or lost authority semantics")
	}
	if !v7CloseGateKindSatisfied(idx, fixture.ConsumerID, "auth") || !v7CloseGateKindSatisfied(idx, fixture.ConsumerID, "release") {
		t.Fatal("fresh auth/release receipt did not satisfy the canonical consumer")
	}
	authReceipt := stringField(idx.Gates[authID].Data, "dependency_material_fingerprint")
	releaseReceipt := stringField(idx.Gates[releaseID].Data, "dependency_material_fingerprint")
	if err := deliveryV2ImportCmd(fixture.Vault, fixture.ConsumerPlan, Args{"vault": fixture.Vault, "quiet": "true"}); err != nil {
		t.Fatalf("exact unchanged consumer re-import: %v", err)
	}
	idx, err = loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Gates[authID].Data, "dependency_material_fingerprint") != authReceipt ||
		stringField(idx.Gates[releaseID].Data, "dependency_material_fingerprint") != releaseReceipt ||
		!v7GateAuthorityReceiptCurrent(idx.Gates[authID], idx) ||
		!v7GateAuthorityReceiptCurrent(idx.Gates[releaseID], idx) {
		t.Fatal("exact unchanged re-import discarded or invalidated bound authority receipts")
	}

	preflight := buildWavePreflight(fixture.Vault, idx, idx.Waves[fixture.ConsumerWave], greenWaveEnvironment())
	if !preflight.OK {
		t.Fatalf("fresh receipt-bound wave did not pass preflight: %#v", preflight.Blockers)
	}
	armedWave := idx.Waves[fixture.ConsumerWave]
	armedWave.Data = cloneMap(armedWave.Data)
	armedWave.Data["authorization"] = "armed"
	armedWave.Data["authorization_fingerprint"] = preflight.Fingerprint
	crossScopeWriteTaskFields(t, fixture.Vault, fixture.ProducerID, map[string]any{"updated_at": "2026-07-26T12:34:56Z"})
	idx, err = loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	idx.Waves = cloneNoteMap(idx.Waves)
	idx.Waves[fixture.ConsumerWave] = armedWave
	for _, gateID := range []string{authID, releaseID} {
		if v7GateAuthorityReceiptCurrent(idx.Gates[gateID], idx) {
			t.Fatalf("material epoch drift did not stale affected receipt: %s", gateID)
		}
	}
	if v7CloseGateKindSatisfied(idx, fixture.ConsumerID, "auth") || v7CloseGateKindSatisfied(idx, fixture.ConsumerID, "release") {
		t.Fatal("stale authority receipt remained reusable")
	}
	if !v7GateAuthorityReceiptCurrent(idx.Gates[setupID], idx) || !v7GateAuthorityReceiptCurrent(idx.Gates[unrelatedAuthID], idx) {
		t.Fatal("producer drift invalidated setup or unrelated authority")
	}
	report := buildWavePreflight(fixture.Vault, idx, idx.Waves[fixture.ConsumerWave], greenWaveEnvironment())
	for _, gateID := range []string{authID, releaseID} {
		if !hasWaveBlocker(report.Blockers, v7GateAuthorityReceiptStaleCode+" "+gateID) {
			t.Fatalf("preflight omitted stale receipt %s: %#v", gateID, report.Blockers)
		}
	}
	if got := stringField(waveAuthorizationProjection(fixture.Vault, idx, idx.Waves[fixture.ConsumerWave]), "state"); got != "stale" {
		t.Fatalf("receipt material drift did not stale affected wave authority: %q", got)
	}

	if err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, authID, "satisfied", "human:test"); err != nil {
		t.Fatalf("current completed closure could not refresh stale receipt: %v", err)
	}
	idx, err = loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	if !v7GateAuthorityReceiptCurrent(idx.Gates[authID], idx) || v7GateAuthorityReceiptCurrent(idx.Gates[releaseID], idx) {
		t.Fatal("receipt refresh changed more than the selected authority gate")
	}
	refreshedAuthReceipt := stringField(idx.Gates[authID].Data, "dependency_material_fingerprint")

	crossScopeWriteTaskFields(t, fixture.Vault, fixture.ProducerID, map[string]any{buildFailedField: true})
	idx, err = loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	if v7GateAuthorityReceiptCurrent(idx.Gates[authID], idx) ||
		stringField(idx.Gates[authID].Data, "dependency_material_fingerprint") != refreshedAuthReceipt {
		t.Fatal("red done producer did not stale the previously current authority receipt")
	}
	if !v7GateAuthorityReceiptCurrent(idx.Gates[setupID], idx) || !v7GateAuthorityReceiptCurrent(idx.Gates[unrelatedAuthID], idx) {
		t.Fatal("red done producer invalidated setup or unrelated authority")
	}
	assertCrossScopeGateRefusal(t, gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, authID, "satisfied", "human:test"))
	assertCrossScopeHumanReceiptRequired(t, gateV7Cmd(Args{
		"vault": fixture.Vault, "_pos0": "waive", "_pos1": releaseID, "reason": "Red material must not be waived.", "by": "human:test", "quiet": "true",
	}))
	if err := gateV7TransitionWithTrustedHumanReceiptForTest(t, fixture.Vault, setupID, "satisfied", "human:test"); err != nil {
		t.Fatalf("setup gate inherited hard-closure policy: %v", err)
	}
	idx, err = loadV7Index(fixture.Vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Gates[authID].Data, "dependency_material_fingerprint") != refreshedAuthReceipt ||
		v7GateAuthorityReceiptCurrent(idx.Gates[authID], idx) ||
		v7GateAuthorityReceiptCurrent(idx.Gates[releaseID], idx) {
		t.Fatal("refused red-closure transitions mutated or refreshed authority receipts")
	}
	if !v7GateAuthorityReceiptCurrent(idx.Gates[setupID], idx) || !v7GateAuthorityReceiptCurrent(idx.Gates[unrelatedAuthID], idx) {
		t.Fatal("red-closure refusal changed setup or unrelated authority")
	}
}

func newCrossScopePolicyFixture(t *testing.T, vault string, withGates bool) crossScopePolicyFixture {
	t.Helper()
	producer := crossScopePlan("producer/v1", "PRD", "provider")
	if withGates {
		producer.HumanGates = []deliveryHumanGate{crossScopePolicyGate("producer-auth", "auth", "provider")}
	}
	path := writeDeliveryV2TestPlan(t, vault, producer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	consumer := crossScopePlan("consumer/v1", "CON", "consumer")
	consumer.Tasks[0].Dependencies = []deliveryDependency{{Task: "provider", Kind: "hard"}}
	deliveryV2DependencyScope(&consumer.Tasks[0].Dependencies[0], "producer/v1")
	if withGates {
		consumer.HumanGates = []deliveryHumanGate{
			crossScopePolicyGate("consumer-setup", "setup", "consumer"),
			crossScopePolicyGate("consumer-auth", "auth", "consumer"),
			crossScopePolicyGate("consumer-release", "release", "consumer"),
		}
	}
	path = writeDeliveryV2TestPlan(t, vault, consumer)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	fixture := crossScopePolicyFixture{Vault: vault, ConsumerPlan: path}
	for id, task := range idx.Tasks {
		switch stringField(task.Data, "delivery_plan_scope") {
		case "producer/v1":
			fixture.ProducerID = id
			fixture.ProducerWave = stringField(task.Data, "wave")
		case "consumer/v1":
			fixture.ConsumerID = id
			fixture.ConsumerWave = stringField(task.Data, "wave")
		}
	}
	if fixture.ProducerID == "" || fixture.ConsumerID == "" || fixture.ProducerWave == "" || fixture.ConsumerWave == "" {
		t.Fatalf("incomplete cross-scope fixture: %#v", fixture)
	}
	return fixture
}

func crossScopePolicyGate(sourceKey, kind, taskSourceKey string) deliveryHumanGate {
	action := "Record the " + kind + " boundary decision."
	verification := "The decision is recorded against the current dependency material."
	why := "Only the designated human authority can complete this credential or release boundary."
	if kind == "setup" {
		action = "Prepare the isolated credential-bearing runtime."
		verification = "The inaccessible runtime prerequisite is ready."
	}
	return deliveryHumanGate{
		SourceKey: sourceKey, Title: kind + " boundary", Kind: kind, Owner: "human:test",
		TaskSourceKey: taskSourceKey, AcceptanceIDs: []string{"A1"}, Action: action,
		Verification: verification, WhyAgentCannot: why,
	}
}

func crossScopeIndexWithTaskFields(idx v7Index, id string, fields map[string]any) v7Index {
	idx.Tasks = cloneNoteMap(idx.Tasks)
	task := idx.Tasks[id]
	task.Data = cloneMap(task.Data)
	for key, value := range fields {
		task.Data[key] = value
	}
	idx.Tasks[id] = task
	return idx
}

func crossScopeWriteTaskFields(t *testing.T, vault, id string, fields map[string]any) {
	t.Helper()
	task, err := resolveV7Note(vault, id, "task")
	if err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
	if err != nil {
		t.Fatal(err)
	}
	baseRev := stringField(data, "state_rev")
	for key, value := range fields {
		data[key] = value
	}
	if _, err := saveV7DocumentCAS(task.AbsolutePath, data, body, v7FrontmatterOrder["task"], baseRev); err != nil {
		t.Fatal(err)
	}
}

func newCrossScopeServeServer(t *testing.T, vault string) *serveServer {
	t.Helper()
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	projectID := v7ProjectID(vault)
	project := RegisteredProject{
		ProjectID: projectID, ProjectKey: projectID, Name: projectID, RepoRoot: v7RepoRoot(vault), VaultRoot: vault,
		WorkflowPath: workflowPath(vault), Enabled: true, Health: projectHealthHealthy,
	}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	server := newServeServer(vault, v7RepoRoot(vault), defaultServeAddr, store, nil)
	server.operatorActor = "human:test"
	return server
}

func assertCrossScopeGateRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil || errorToIssue(err).Code != v7GateHardDependencyIncompleteCode {
		t.Fatalf("want %s, got %v", v7GateHardDependencyIncompleteCode, err)
	}
}

func assertCrossScopeHumanReceiptRequired(t *testing.T, err error) {
	t.Helper()
	if err == nil || errorToIssue(err).Code != humanControlReceiptRequiredCode {
		t.Fatalf("want %s, got %v", humanControlReceiptRequiredCode, err)
	}
}
