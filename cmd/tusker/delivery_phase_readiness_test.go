package main

import (
	"os"
	"strings"
	"testing"
)

func TestDeliveryPhaseReadinessSeparation(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	before := snapshotDeliveryRecords(t, vault)

	disabled := greenWaveEnvironment()
	disabled.ProjectEnabled = false
	review, err := buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(disabled))
	if err != nil {
		t.Fatal(err)
	}
	if !review.PlanValid || !review.ImportReady || review.StartReady || review.Ready {
		t.Fatalf("phases were conflated: plan=%t import=%t start=%t legacy=%t", review.PlanValid, review.ImportReady, review.StartReady, review.Ready)
	}
	if review.Start.Readiness != "blocked" {
		t.Fatalf("legacy Start projection concealed automation blocker: %#v", review.Start)
	}
	if review.Readiness.Schema != ReadinessContractSchema || review.Readiness.Dimensions.Contract.State != ReadinessStateReady || review.Readiness.Dimensions.Import.State != ReadinessStateReady || review.Readiness.Dimensions.Automation.State != ReadinessStateBlocked {
		t.Fatalf("readiness contract did not retain phase facts: %#v", review.Readiness)
	}
	if len(review.Readiness.Blockers) == 0 || review.Readiness.Blockers[0].Kind != ReadinessBlockerAutomationDisabled {
		t.Fatalf("Start refusal was not typed: %#v", review.Readiness.Blockers)
	}
	if err := captureDeliveryReviewCmd(t, Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatalf("semantically valid review must succeed despite Start blocker: %v", err)
	}
	assertEqual(t, before, snapshotDeliveryRecords(t, vault), "review remains read-only with Start blockers")

	// Held import intentionally ignores unattended environment health. It can
	// reconcile records but leaves them backlog/held and the wave disarmed.
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatalf("disabled-project held import: %v", err)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	wave := idx.Waves["W-0001"]
	if stringField(wave.Data, "authorization") != "disarmed" {
		t.Fatalf("held import changed authorization: %#v", wave.Data)
	}
	for _, id := range normalizeList(wave.Data["members"]) {
		if got := stringField(idx.Tasks[id].Data, "readiness"); got != "held" {
			t.Fatalf("held import made %s operational: %s", id, got)
		}
	}

	review, err = buildDeliveryReviewWithInspector(vault, path, fixedWaveEnvironmentInspector(disabled))
	if err != nil {
		t.Fatal(err)
	}
	if !review.PlanValid || !review.ImportReady || review.StartReady || review.Start.Authorization != "disarmed" {
		t.Fatalf("imported disabled review lost phase projection: %#v", review)
	}
	if review.Readiness.Dimensions.Authorization.State != ReadinessStateBlocked || !hasReadinessBlocker(review.Readiness.Blockers, ReadinessBlockerAuthorizationMissing) {
		t.Fatalf("disarmed wave was not typed as a Start authorization blocker: %#v", review.Readiness)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = deliveryStart(Args{"vault": vault, "plan": path, "by": "human:test", "confirm": deliveryFingerprint(raw), "quiet": "true"}, fixedWaveEnvironmentInspector(disabled))
	if err == nil || !strings.Contains(err.Error(), "project") {
		t.Fatalf("Start must retain disabled-project refusal: %v", err)
	}
	idx, _ = loadV7Index(vault)
	if stringField(idx.Waves["W-0001"].Data, "authorization") != "disarmed" {
		t.Fatal("blocked Start armed the held wave")
	}
}

func TestDeliveryReviewCLIRejectsImportBlocker(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	plan.Summary = "Changed plan material cannot reuse held lineage."
	path = writeDeliveryV2TestPlan(t, vault, plan)
	if err := captureDeliveryReviewCmd(t, Args{"vault": vault, "plan": path, "quiet": "true"}); err == nil {
		t.Fatal("CLI review accepted an import-lineage blocker")
	}
}

func captureDeliveryReviewCmd(t *testing.T, args Args) error {
	t.Helper()
	var result error
	captureStdout(t, func() { result = deliveryReviewCmd(args) })
	return result
}

func hasReadinessBlocker(blockers []ReadinessBlocker, kind ReadinessBlockerKind) bool {
	for _, blocker := range blockers {
		if blocker.Kind == kind {
			return true
		}
	}
	return false
}
