package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	programCutoverCloseAuthorityCheck = "command: go test ./cmd/tusker -run '^TestProgramCutoverCloseAuthorityFence$' -count=1"
	dogfoodStatusCheck                = "command: tusker program cutover status --scope program-cutover-convergence/v1 --source-key low-risk-authoritative-dogfood --require dogfood-integrated --json"
	dogfoodManualProof                = "manual proof: accountable operator runs `tusker program cutover authorize-dogfood --scope program-cutover-convergence/v1 --source-key low-risk-authoritative-dogfood --project <project> --wave <wave> --expires-at <rfc3339> --by human:operator --json` and records the resulting grant ID"
	handoffStatusCheck                = "command: tusker program cutover status --scope program-cutover-convergence/v1 --source-key production-promotion-handoff --require release-handoff-current --dogfood-receipt <receipt> --json"
	handoffManualProof                = "manual proof: accountable production authority runs `tusker program cutover authorize-release-handoff --scope program-cutover-convergence/v1 --source-key production-promotion-handoff --project <project> --wave <wave> --dogfood-receipt <receipt> --expires-at <rfc3339> --by human:operator --json` and records the resulting handoff ID"
)

func TestCrossScopePlanRewiring(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	plans := map[string]deliveryPlanV2{
		"12": readCrossScopeProgramPlan(t, filepath.Join(repo, "docs", "plans", "12-opt-in-scheduled-promotion-v2.yaml")),
		"14": readCrossScopeProgramPlan(t, filepath.Join(repo, "docs", "plans", "14-opt-in-factory-execution-control-v2.yaml")),
		"15": readCrossScopeProgramPlan(t, filepath.Join(repo, "docs", "plans", "15-opt-in-full-gate-lifecycle-provider-v2.yaml")),
		"17": readCrossScopeProgramPlan(t, filepath.Join(repo, "docs", "plans", "17-program-cutover-convergence-v2.yaml")),
		"18": readCrossScopeProgramPlan(t, filepath.Join(repo, "docs", "plans", "18-exact-verification-contract-authority-v2.yaml")),
	}

	assertCrossScopeProgramTask(t, plans["12"], "scheduled-promotion-shadow-ready", []deliveryDependency{{Task: "shadow-cutover-e2e", Kind: "hard"}})
	assertNoProgramAuthorityGate(t, plans["12"])
	assertCrossScopeProgramTask(t, plans["14"], "factory-control-shadow-ready", []deliveryDependency{{Task: "factory-execution-dogfood-cutover", Kind: "hard"}})
	assertNoProgramAuthorityGate(t, plans["14"])

	providerDoctor := crossScopeProgramTask(t, plans["15"], "provider-cutover-doctor")
	if !stringContainsAll(providerDoctor.Outcome, "shadow readiness", "live-smoke") {
		t.Fatalf("provider doctor must distinguish shadow readiness and live smoke: %q", providerDoctor.Outcome)
	}
	if gate := crossScopeProgramGate(t, plans["15"], "choose-and-start-local-runtime"); gate.Kind != "setup" || gate.TaskSourceKey != "provider-live-smoke" {
		t.Fatalf("provider live smoke must remain setup-gated: %#v", gate)
	}
	exactAuthority := plans["18"]
	if exactAuthority.Scope != "exact-verification-authority/v1" {
		t.Fatalf("Plan 18 scope = %q", exactAuthority.Scope)
	}
	assertCrossScopeProgramTask(t, exactAuthority, "exact-verification-authority-e2e", []deliveryDependency{
		{Task: "strict-verification-write-path", Kind: "hard"},
		{Task: "review-completion-proof-chain", Kind: "hard"},
		{Task: "proof-operations-surface", Kind: "hard"},
		{Task: "canonical-proof-guidance", Kind: "hard"},
	})

	program := plans["17"]
	if program.Scope != "program-cutover-convergence/v1" {
		t.Fatalf("Plan 17 scope = %q", program.Scope)
	}
	assertCrossScopeProgramTask(t, program, "shadow-convergence", []deliveryDependency{
		crossScopeProgramDependency("scheduled-promotion-shadow-ready", "scheduled-promotion/v1"),
		crossScopeProgramDependency("factory-control-shadow-ready", "factory-execution-control/v1"),
		crossScopeProgramDependency("provider-cutover-doctor", "full-gate-lifecycle-provider/v1"),
	})
	transaction := crossScopeProgramTask(t, program, "program-cutover-transaction-contract")
	assertCrossScopeProgramTask(t, program, "program-cutover-transaction-contract", []deliveryDependency{
		{Task: "shadow-convergence", Kind: "hard"},
		crossScopeProgramDependency("exact-verification-authority-e2e", "exact-verification-authority/v1"),
	})
	assertCrossScopeProgramVerification(t, transaction, "A1,A2,A3,A4", programCutoverCloseAuthorityCheck)
	for _, path := range []string{"cmd/tusker/v7_control_cmd.go", "cmd/tusker/v7_proof_cmd.go", "cmd/tusker/v7_closeout_cmd.go", "cmd/tusker/v7_completion_receipt.go", "cmd/tusker/program_cutover_close_authority_test.go"} {
		if !containsString(transaction.OwnedPaths, path) {
			t.Fatalf("transaction contract must own close-authority path %q: %#v", path, transaction.OwnedPaths)
		}
	}
	if len(transaction.Acceptance) != 4 || !stringContainsAll(transaction.Acceptance[2].Outcome, "gate satisfy --evidence", "replacement generic test row", "status prose", "cannot close, unlock, release, or move a ref") || !stringContainsAll(transaction.Acceptance[3].Outcome, "cutover receipts", "does not claim generic V2 exact-proof coverage") {
		t.Fatalf("transaction close-authority contract drifted: %#v", transaction.Acceptance)
	}
	assertCrossScopeProgramTask(t, program, "low-risk-authoritative-dogfood", []deliveryDependency{
		{Task: "program-cutover-transaction-contract", Kind: "hard"},
		crossScopeProgramDependency("provider-live-smoke", "full-gate-lifecycle-provider/v1"),
	})
	assertCrossScopeProgramVerification(t, crossScopeProgramTask(t, program, "low-risk-authoritative-dogfood"), "A1,A3", dogfoodStatusCheck)
	assertCrossScopeProgramVerification(t, crossScopeProgramTask(t, program, "low-risk-authoritative-dogfood"), "A2", dogfoodManualProof)
	if task := crossScopeProgramTask(t, program, "low-risk-authoritative-dogfood"); task.Artifact.Path != "cmd/tusker/program_cutover_close_authority_test.go" {
		t.Fatalf("low-risk-authoritative-dogfood artifact path = %q, want predecessor-owned close-authority test", task.Artifact.Path)
	}
	assertCrossScopeProgramTask(t, program, "production-promotion-handoff", []deliveryDependency{{Task: "low-risk-authoritative-dogfood", Kind: "hard"}})
	assertCrossScopeProgramVerification(t, crossScopeProgramTask(t, program, "production-promotion-handoff"), "A1,A3", handoffStatusCheck)
	assertCrossScopeProgramVerification(t, crossScopeProgramTask(t, program, "production-promotion-handoff"), "A2", handoffManualProof)
	if task := crossScopeProgramTask(t, program, "production-promotion-handoff"); task.Artifact.Path != "cmd/tusker/program_cutover_close_authority_test.go" {
		t.Fatalf("production-promotion-handoff artifact path = %q, want predecessor-owned close-authority test", task.Artifact.Path)
	}

	if gate := crossScopeProgramGate(t, program, "authorize-low-risk-program-dogfood"); gate.Kind != "auth" || gate.TaskSourceKey != "low-risk-authoritative-dogfood" || !stringContainsAll(gate.Action, "authorize-dogfood", "--project <project>", "--wave <wave>", "--expires-at <rfc3339>") || !stringContainsAll(gate.Verification, "require grant-current", "does not claim or execute dogfood") || len(gate.DependencyClosure) != 1 || gate.DependencyClosure[0] != "production-promotion-handoff" {
		t.Fatalf("Plan 17 dogfood auth gate drifted: %#v", gate)
	}
	if gate := crossScopeProgramGate(t, program, "authorize-production-promotion-handoff"); gate.Kind != "release" || gate.TaskSourceKey != "production-promotion-handoff" || !stringContainsAll(gate.Action, "authorize-release-handoff", "--dogfood-receipt <receipt>") || !stringContainsAll(gate.Verification, "require release-handoff-current", "does not move a ref or release") || len(gate.DependencyClosure) != 0 {
		t.Fatalf("Plan 17 production release gate drifted: %#v", gate)
	}
	for _, path := range []string{
		filepath.Join(repo, "docs", "plans", "12-opt-in-scheduled-promotion-v2.yaml"),
		filepath.Join(repo, "docs", "plans", "14-opt-in-factory-execution-control-v2.yaml"),
		filepath.Join(repo, "docs", "plans", "15-opt-in-full-gate-lifecycle-provider-v2.yaml"),
		filepath.Join(repo, "docs", "plans", "17-program-cutover-convergence-v2.yaml"),
		filepath.Join(repo, "docs", "plans", "18-exact-verification-contract-authority-v2.yaml"),
	} {
		assertCrossScopeProgramPlanCurrent(t, filepath.Join(repo, ".tusker"), path)
	}

	assertCrossScopeProgramImportMigration(t)
}

func assertCrossScopeProgramImportMigration(t *testing.T) {
	t.Helper()
	vault := deliveryTestVault(t)
	legacy12 := crossScopeProgramFixturePlan("scheduled-promotion/v1", []deliveryPlanTask{crossScopeProgramFixtureTask("shadow-cutover-e2e")}, []deliveryHumanGate{crossScopeProgramFixtureGate("legacy-conductor-production-handoff", "release", "shadow-cutover-e2e", nil)})
	legacy14 := crossScopeProgramFixturePlan("factory-execution-control/v1", []deliveryPlanTask{crossScopeProgramFixtureTask("factory-execution-dogfood-cutover")}, []deliveryHumanGate{crossScopeProgramFixtureGate("low-risk-authoritative-factory-cutover", "auth", "factory-execution-dogfood-cutover", nil)})
	reviewed12 := crossScopeProgramFixturePlan("scheduled-promotion/v1", []deliveryPlanTask{
		crossScopeProgramFixtureTask("shadow-cutover-e2e"),
		crossScopeProgramFixtureTask("scheduled-promotion-shadow-ready", deliveryDependency{Task: "shadow-cutover-e2e", Kind: "hard"}),
	}, nil)
	reviewed14 := crossScopeProgramFixturePlan("factory-execution-control/v1", []deliveryPlanTask{
		crossScopeProgramFixtureTask("factory-execution-dogfood-cutover"),
		crossScopeProgramFixtureTask("factory-control-shadow-ready", deliveryDependency{Task: "factory-execution-dogfood-cutover", Kind: "hard"}),
	}, nil)
	provider15 := crossScopeProgramFixturePlan("full-gate-lifecycle-provider/v1", []deliveryPlanTask{
		crossScopeProgramFixtureTask("provider-cutover-doctor"),
		crossScopeProgramFixtureTask("provider-live-smoke", deliveryDependency{Task: "provider-cutover-doctor", Kind: "hard"}),
	}, []deliveryHumanGate{crossScopeProgramFixtureGate("choose-and-start-local-runtime", "setup", "provider-live-smoke", nil)})
	exact18 := crossScopeProgramFixturePlan("exact-verification-authority/v1", []deliveryPlanTask{
		crossScopeProgramFixtureTask("exact-verification-authority-e2e"),
	}, nil)
	transaction := crossScopeProgramFixtureTask("program-cutover-transaction-contract",
		deliveryDependency{Task: "shadow-convergence", Kind: "hard"},
		crossScopeProgramDependency("exact-verification-authority-e2e", "exact-verification-authority/v1"))
	transaction.Verification = []deliveryVerification{{Covers: "A1", Check: programCutoverCloseAuthorityCheck}}
	dogfood := crossScopeProgramFixtureTask("low-risk-authoritative-dogfood",
		deliveryDependency{Task: "program-cutover-transaction-contract", Kind: "hard"},
		crossScopeProgramDependency("provider-live-smoke", "full-gate-lifecycle-provider/v1"))
	dogfood.Acceptance = []deliveryAcceptance{{ID: "A1", Outcome: "The handler receipt is current."}, {ID: "A2", Outcome: "A human creates a typed dogfood grant."}, {ID: "A3", Outcome: "The status verifier proves the default-off boundary."}}
	dogfood.Verification = []deliveryVerification{{Covers: "A1,A3", Check: dogfoodStatusCheck}, {Covers: "A2", Check: dogfoodManualProof}}
	dogfood.Artifact.AcceptanceIDs = []string{"A1", "A2", "A3"}
	handoff := crossScopeProgramFixtureTask("production-promotion-handoff", deliveryDependency{Task: "low-risk-authoritative-dogfood", Kind: "hard"})
	handoff.Acceptance = []deliveryAcceptance{{ID: "A1", Outcome: "The handoff binds a current dogfood receipt."}, {ID: "A2", Outcome: "A human creates a typed release handoff."}, {ID: "A3", Outcome: "The status verifier remains read-only."}}
	handoff.Verification = []deliveryVerification{{Covers: "A1,A3", Check: handoffStatusCheck}, {Covers: "A2", Check: handoffManualProof}}
	handoff.Artifact.AcceptanceIDs = []string{"A1", "A2", "A3"}
	dogfoodGate := crossScopeProgramFixtureGate("authorize-low-risk-program-dogfood", "auth", "low-risk-authoritative-dogfood", []string{"production-promotion-handoff"})
	dogfoodGate.AcceptanceIDs = []string{"A2"}
	dogfoodGate.Action = "Run tusker program cutover authorize-dogfood --project <project> --wave <wave> --expires-at <rfc3339> --json."
	dogfoodGate.Verification = "Read-only status requires grant-current and does not claim or execute dogfood."
	handoffGate := crossScopeProgramFixtureGate("authorize-production-promotion-handoff", "release", "production-promotion-handoff", []string{})
	handoffGate.AcceptanceIDs = []string{"A2"}
	handoffGate.Action = "Run tusker program cutover authorize-release-handoff --project <project> --wave <wave> --dogfood-receipt <receipt> --expires-at <rfc3339> --json."
	handoffGate.Verification = "Read-only status requires release-handoff-current and does not move a ref or release."
	program17 := crossScopeProgramFixturePlan("program-cutover-convergence/v1", []deliveryPlanTask{
		crossScopeProgramFixtureTask("shadow-convergence",
			crossScopeProgramDependency("scheduled-promotion-shadow-ready", "scheduled-promotion/v1"),
			crossScopeProgramDependency("factory-control-shadow-ready", "factory-execution-control/v1"),
			crossScopeProgramDependency("provider-cutover-doctor", "full-gate-lifecycle-provider/v1")),
		transaction,
		dogfood,
		handoff,
	}, []deliveryHumanGate{dogfoodGate, handoffGate})

	programPath := writeDeliveryV2TestPlan(t, vault, program17)
	if report, err := deliveryPlanDoctor(vault, programPath); err != nil || !report.OK {
		t.Fatalf("hermetic Plan 17 doctor = %#v, %v", report, err)
	}
	beforeMissingProducer := snapshotDeliveryV2Records(t, vault)
	err := deliveryV2ImportCmd(vault, programPath, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_PRODUCER_MISSING") {
		t.Fatalf("Plan 17 import without producers = %v, want missing producer refusal", err)
	}
	assertEqual(t, beforeMissingProducer, snapshotDeliveryV2Records(t, vault), "missing producer import is atomic")

	crossScopeProgramImportFixturePlan(t, vault, legacy12)
	crossScopeProgramImportFixturePlan(t, vault, legacy14)
	crossScopeProgramImportFixturePlan(t, vault, reviewed12)
	crossScopeProgramImportFixturePlan(t, vault, reviewed14)
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	assertCrossScopeProgramObsoleteGate(t, idx, "scheduled-promotion/v1", "legacy-conductor-production-handoff")
	assertCrossScopeProgramObsoleteGate(t, idx, "factory-execution-control/v1", "low-risk-authoritative-factory-cutover")

	programPath = writeDeliveryV2TestPlan(t, vault, program17)
	err = deliveryV2ImportCmd(vault, programPath, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_PRODUCER_MISSING") || !strings.Contains(err.Error(), "provider-cutover-doctor") {
		t.Fatalf("Plan 17 import before Plan 15 = %v, want provider-first refusal", err)
	}

	crossScopeProgramImportFixturePlan(t, vault, provider15)
	programPath = writeDeliveryV2TestPlan(t, vault, program17)
	beforeMissingExactAuthority := snapshotDeliveryV2Records(t, vault)
	err = deliveryV2ImportCmd(vault, programPath, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_PRODUCER_MISSING") || !strings.Contains(err.Error(), "exact-verification-authority-e2e") {
		t.Fatalf("Plan 17 import before Plan 18 = %v, want exact-authority-first refusal", err)
	}
	assertEqual(t, beforeMissingExactAuthority, snapshotDeliveryV2Records(t, vault), "missing exact-authority producer import is atomic")

	crossScopeProgramImportFixturePlan(t, vault, exact18)
	programPath = writeDeliveryV2TestPlan(t, vault, program17)
	beforeProspectiveReview := snapshotDeliveryV2Records(t, vault)
	review, err := buildDeliveryReview(vault, programPath)
	if err != nil || !review.ReadOnly || len(review.Flow.CrossScopeDependencies) != 5 {
		t.Fatalf("pre-import Plan 17 review = %#v, %v", review, err)
	}
	for _, dependency := range review.Flow.CrossScopeDependencies {
		if dependency.Scope == "" || dependency.SourceKey == "" || dependency.TaskID == "" || dependency.Kind != "hard" {
			t.Fatalf("pre-import review did not resolve a current qualified producer: %#v", dependency)
		}
	}
	assertEqual(t, beforeProspectiveReview, snapshotDeliveryV2Records(t, vault), "pre-import producer review is inert")
	crossScopeProgramImportFixturePlan(t, vault, program17)
	idx, err = loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateDeliveryCrossScopeIndex(idx, v7ProjectID(vault)); err != nil {
		t.Fatalf("imported program graph is not globally valid: %v", err)
	}
	assertCrossScopeProgramResolvedEdges(t, idx)
	assertCrossScopeProgramImportedChecks(t, idx)

	beforeReadOnly := snapshotDeliveryV2Records(t, vault)
	report, err := deliveryPlanDoctor(vault, programPath)
	if err != nil || !report.OK {
		t.Fatalf("program doctor = %#v, %v", report, err)
	}
	if _, err := buildDeliveryReview(vault, programPath); err != nil {
		t.Fatalf("program review: %v", err)
	}
	if err := deliveryV2ImportCmd(vault, programPath, Args{"vault": vault, "dry-run": "true", "quiet": "true"}); err != nil {
		t.Fatalf("program dry run: %v", err)
	}
	assertEqual(t, beforeReadOnly, snapshotDeliveryV2Records(t, vault), "doctor, review, and dry run are inert")

	if err := deliveryV2ImportCmd(vault, programPath, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatalf("program re-import: %v", err)
	}
	assertEqual(t, beforeReadOnly, snapshotDeliveryV2Records(t, vault), "program re-import is idempotent")

	crossScopeProgramDriftProducerContract(t, vault, "exact-verification-authority/v1", "exact-verification-authority-e2e")
	beforeDriftRefusal := snapshotDeliveryV2Records(t, vault)
	err = deliveryV2ImportCmd(vault, programPath, Args{"vault": vault, "quiet": "true"})
	if err == nil || !strings.Contains(err.Error(), "CROSS_SCOPE_TARGET_DRIFT") || !strings.Contains(err.Error(), "exact-verification-authority-e2e") {
		t.Fatalf("Plan 17 import after Plan 18 drift = %v, want exact-authority drift refusal", err)
	}
	assertEqual(t, beforeDriftRefusal, snapshotDeliveryV2Records(t, vault), "exact-authority drift refusal is atomic")
}

func assertCrossScopeProgramPlanCurrent(t *testing.T, vault, path string) {
	t.Helper()
	plan := readCrossScopeProgramPlan(t, path)
	context, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
	if err != nil {
		t.Fatalf("planning context %s: %v", path, err)
	}
	if plan.ContextFingerprint != context.ContextFingerprint {
		t.Fatalf("stale context fingerprint in %s: got %s want %s", path, plan.ContextFingerprint, context.ContextFingerprint)
	}
	report, err := deliveryPlanDoctor(vault, path)
	if err != nil || !report.OK {
		t.Fatalf("doctor %s = %#v, %v", path, report, err)
	}
}

func crossScopeProgramFixturePlan(scope string, tasks []deliveryPlanTask, gates []deliveryHumanGate) deliveryPlanV2 {
	plan := validDeliveryPlanV2()
	plan.Scope = scope
	plan.Epic = "APP"
	plan.EpicContract = nil
	plan.SpecRefs = []string{"docs/specs/delivery.md"}
	plan.Summary = "Hermetic cross-scope program rewiring fixture."
	plan.Tasks = tasks
	plan.HumanGates = gates
	plan.ContextFingerprint = ""
	return plan
}

func crossScopeProgramFixtureTask(key string, dependencies ...deliveryDependency) deliveryPlanTask {
	return deliveryPlanTask{
		SourceKey:       key,
		RequirementRefs: []string{"R1"},
		Title:           "Fixture " + key,
		Outcome:         "The fixture preserves default-off program rewiring semantics.",
		Acceptance:      []deliveryAcceptance{{ID: "A1", Outcome: "The fixture task has an observable dependency contract."}},
		Verification:    []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run '^TestCrossScopePlanRewiring' -count=1"}},
		Dependencies:    dependencies,
		Artifact:        deliveryArtifactContract{Kind: "behavior_matrix", Path: "cmd/tusker/cross_scope_plan_rewiring_test.go", Summary: "Hermetic cross-scope program rewiring fixture.", AcceptanceIDs: []string{"A1"}},
		Risk:            "critical",
		Priority:        "p0",
		Size:            "s",
		Domains:         []string{"project"},
	}
}

func crossScopeProgramFixtureGate(key, kind, task string, closure []string) deliveryHumanGate {
	return deliveryHumanGate{
		SourceKey: key, Title: "Fixture " + key, Kind: kind, Owner: "human:operator", TaskSourceKey: task,
		AcceptanceIDs: []string{"A1"}, DependencyClosure: closure,
		Action: "A human supplies the fixture's external authority.", Verification: "The fixture records the human-owned authority boundary.",
		WhyAgentCannot: "The fixture models authority that an agent cannot grant.",
	}
}

func crossScopeProgramImportFixturePlan(t *testing.T, vault string, plan deliveryPlanV2) {
	t.Helper()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryV2ImportCmd(vault, path, Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatalf("import %s: %v", plan.Scope, err)
	}
}

func crossScopeProgramDriftProducerContract(t *testing.T, vault, scope, sourceKey string) {
	t.Helper()
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range idx.Tasks {
		if stringField(task.Data, "delivery_plan_scope") != scope || stringField(task.Data, "delivery_source_key") != sourceKey {
			continue
		}
		data, body, err := parseFrontmatterMustRead(task.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		data["delivery_contract_fingerprint"] = "sha256:drifted-exact-verification-authority"
		data["state_rev"] = v7StateRev(data, body)
		content, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
		if err != nil {
			t.Fatal(err)
		}
		if err := writeText(task.AbsolutePath, content); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("missing producer %s/%s to drift", scope, sourceKey)
}

func assertCrossScopeProgramObsoleteGate(t *testing.T, idx v7Index, scope, sourceKey string) {
	t.Helper()
	for _, gate := range idx.Gates {
		if stringField(gate.Data, "delivery_plan_scope") == scope && stringField(gate.Data, "delivery_source_key") == sourceKey {
			if stringField(gate.Data, "status") != "obsolete" || stringField(gate.Data, "obsolete_reason") == "" {
				t.Fatalf("gate %s/%s was not retained as obsolete history: %#v", scope, sourceKey, gate.Data)
			}
			return
		}
	}
	t.Fatalf("missing obsolete historical gate %s/%s", scope, sourceKey)
}

func assertCrossScopeProgramResolvedEdges(t *testing.T, idx v7Index) {
	t.Helper()
	producerID := func(scope, sourceKey string) string {
		for id, task := range idx.Tasks {
			if stringField(task.Data, "delivery_plan_scope") == scope && stringField(task.Data, "delivery_source_key") == sourceKey {
				return id
			}
		}
		t.Fatalf("missing producer %s/%s", scope, sourceKey)
		return ""
	}
	consumer := func(sourceKey string) Note {
		for _, task := range idx.Tasks {
			if stringField(task.Data, "delivery_plan_scope") == "program-cutover-convergence/v1" && stringField(task.Data, "delivery_source_key") == sourceKey {
				return task
			}
		}
		t.Fatalf("missing Plan 17 consumer %s", sourceKey)
		return Note{}
	}
	assertCrossScopeProgramProjection(t, consumer("shadow-convergence"), map[string]string{
		"scheduled-promotion/v1/scheduled-promotion-shadow-ready":   producerID("scheduled-promotion/v1", "scheduled-promotion-shadow-ready"),
		"factory-execution-control/v1/factory-control-shadow-ready": producerID("factory-execution-control/v1", "factory-control-shadow-ready"),
		"full-gate-lifecycle-provider/v1/provider-cutover-doctor":   producerID("full-gate-lifecycle-provider/v1", "provider-cutover-doctor"),
	})
	assertCrossScopeProgramProjection(t, consumer("program-cutover-transaction-contract"), map[string]string{
		"exact-verification-authority/v1/exact-verification-authority-e2e": producerID("exact-verification-authority/v1", "exact-verification-authority-e2e"),
	})
	assertCrossScopeProgramProjection(t, consumer("low-risk-authoritative-dogfood"), map[string]string{
		"full-gate-lifecycle-provider/v1/provider-live-smoke": producerID("full-gate-lifecycle-provider/v1", "provider-live-smoke"),
	})
}

func assertCrossScopeProgramImportedChecks(t *testing.T, idx v7Index) {
	t.Helper()
	for sourceKey, checks := range map[string][]string{
		"program-cutover-transaction-contract": []string{programCutoverCloseAuthorityCheck},
		"low-risk-authoritative-dogfood":       []string{dogfoodStatusCheck, dogfoodManualProof},
		"production-promotion-handoff":         []string{handoffStatusCheck, handoffManualProof},
	} {
		found := false
		for _, task := range idx.Tasks {
			if stringField(task.Data, "delivery_plan_scope") == "program-cutover-convergence/v1" && stringField(task.Data, "delivery_source_key") == sourceKey {
				found = true
				body := mustReadIndexTest(t, task.AbsolutePath)
				for _, check := range checks {
					if !strings.Contains(body, check) {
						t.Fatalf("imported %s task body lost check %q", sourceKey, check)
					}
				}
				break
			}
		}
		if !found {
			t.Fatalf("missing imported Plan 17 task %s", sourceKey)
		}
	}
}

func assertCrossScopeProgramProjection(t *testing.T, consumer Note, want map[string]string) {
	t.Helper()
	projections, err := deliveryCrossScopeProjections(consumer)
	if err != nil {
		t.Fatal(err)
	}
	if len(projections) != len(want) {
		t.Fatalf("%s projections = %#v, want %d", stringField(consumer.Data, "id"), projections, len(want))
	}
	dependencies := normalizeList(consumer.Data["dependencies"])
	for _, projection := range projections {
		key := projection.Scope + "/" + projection.Task
		if want[key] != projection.TaskID || projection.Kind != "hard" || projection.TargetContractFingerprint == "" {
			t.Fatalf("%s projection %s = %#v, want durable %q", stringField(consumer.Data, "id"), key, projection, want[key])
		}
		if !containsString(dependencies, projection.TaskID+":hard") {
			t.Fatalf("%s does not retain durable edge for %#v: %#v", stringField(consumer.Data, "id"), projection, dependencies)
		}
	}
}

func readCrossScopeProgramPlan(t *testing.T, path string) deliveryPlanV2 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plan deliveryPlanV2
	if err := yaml.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return plan
}

func crossScopeProgramTask(t *testing.T, plan deliveryPlanV2, key string) deliveryPlanTask {
	t.Helper()
	for _, task := range plan.Tasks {
		if task.SourceKey == key {
			return task
		}
	}
	t.Fatalf("%s does not declare task %q", plan.Scope, key)
	return deliveryPlanTask{}
}

func assertCrossScopeProgramTask(t *testing.T, plan deliveryPlanV2, key string, want []deliveryDependency) {
	t.Helper()
	got := crossScopeProgramTask(t, plan, key).Dependencies
	if len(got) != len(want) {
		t.Fatalf("%s/%s dependencies = %#v, want %#v", plan.Scope, key, got, want)
	}
	for i := range want {
		if got[i].Task != want[i].Task || got[i].Kind != want[i].Kind || got[i].scope != want[i].scope || got[i].scopePresent != want[i].scopePresent {
			t.Fatalf("%s/%s dependency[%d] = %#v, want %#v", plan.Scope, key, i, got[i], want[i])
		}
	}
}

func assertCrossScopeProgramVerification(t *testing.T, task deliveryPlanTask, covers, check string) {
	t.Helper()
	for _, row := range task.Verification {
		if row.Covers == covers && row.Check == check {
			return
		}
	}
	t.Fatalf("%s verification does not map %q to %q: %#v", task.SourceKey, covers, check, task.Verification)
}

func crossScopeProgramDependency(task, scope string) deliveryDependency {
	dep := deliveryDependency{Task: task, Kind: "hard"}
	deliveryV2DependencyScope(&dep, scope)
	return dep
}

func crossScopeProgramGate(t *testing.T, plan deliveryPlanV2, key string) deliveryHumanGate {
	t.Helper()
	for _, gate := range plan.HumanGates {
		if gate.SourceKey == key {
			return gate
		}
	}
	t.Fatalf("%s does not declare gate %q", plan.Scope, key)
	return deliveryHumanGate{}
}

func assertNoProgramAuthorityGate(t *testing.T, plan deliveryPlanV2) {
	t.Helper()
	for _, gate := range plan.HumanGates {
		if gate.Kind == "auth" || gate.Kind == "release" {
			t.Fatalf("%s retained pre-program authority gate %#v", plan.Scope, gate)
		}
	}
}

func stringContainsAll(value string, want ...string) bool {
	for _, needle := range want {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
