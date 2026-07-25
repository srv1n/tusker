package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryPlanReview(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	plan.NonGoals = []string{"No background worker is started by review."}
	path := writeDeliveryV2TestPlan(t, vault, plan)
	review, err := buildDeliveryReview(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Ready || review.Start.Authorization != "not imported" || len(review.What) != 1 || len(review.Proof) != 1 {
		t.Fatalf("clean review=%#v", review)
	}
	if review.Start.ContextFingerprint == "" || len(review.NonGoals) != 1 || review.NonGoals[0] != plan.NonGoals[0] {
		t.Fatalf("review did not bind authored planning context/non-goals: %#v", review)
	}
	text := renderDeliveryReview(review)
	golden, err := os.ReadFile(filepath.Join("testdata", "delivery_review", "terminal.golden"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(golden)), "\n") {
		if line != "" && !strings.Contains(text, line) {
			t.Fatalf("terminal projection missed golden line %q: %s", line, text)
		}
	}
	for _, heading := range []string{"What will be delivered", "How it will be proven", "How work flows", "What needs your decision", "Start boundary"} {
		if !strings.Contains(text, heading) {
			t.Fatalf("missing primary section %q: %s", heading, text)
		}
	}
	first, _ := json.Marshal(review)
	second, _ := json.Marshal(review)
	if string(first) != string(second) {
		t.Fatalf("review JSON is not deterministic")
	}

	// Import is still inert; the review reports the disarmed state without
	// suggesting that either review or import starts a worker.
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	imported, err := buildDeliveryReview(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Start.Authorization != "disarmed" || imported.Start.Readiness != "blocked" || !strings.Contains(strings.Join(imported.Start.Blockers, "\n"), "operational preflight") {
		t.Fatalf("disarmed review=%#v", imported.Start)
	}
}

func TestDeliveryPlanReviewPreservesV1ReadOnlyCompatibility(t *testing.T) {
	vault := deliveryTestVault(t)
	path := writeDeliveryTestPlan(t, vault, validDeliveryPlan())
	review, err := buildDeliveryReview(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if !review.Ready || len(review.What) != 2 || review.Start.Authorization != "not imported" {
		t.Fatalf("V1 review=%#v", review)
	}
}

func TestDeliveryPlanReviewFailsClosedOnPlanDriftAndCoverageGap(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := validDeliveryPlanV2()
	plan.HumanGates = nil
	path := writeDeliveryV2TestPlan(t, vault, plan)
	if err := deliveryImportCmd(Args{"vault": vault, "plan": path, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	plan.Summary = "Changed after import."
	path = writeDeliveryV2TestPlan(t, vault, plan)
	review, err := buildDeliveryReview(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if review.Ready || !strings.Contains(strings.Join(review.Start.Blockers, "\n"), "plan fingerprint differs") || !strings.Contains(review.Start.NextAction, "tusker delivery import") {
		t.Fatalf("drift must fail closed: %#v", review.Start)
	}

	gap := validDeliveryPlanV2()
	gap.Requirements = append(gap.Requirements, deliveryRequirement{ID: "R2", Outcome: "An uncovered outcome must be visible."})
	gap.HumanGates = nil
	gapPath := writeDeliveryV2TestPlan(t, vault, gap)
	review, err = buildDeliveryReview(vault, gapPath)
	if err != nil {
		t.Fatal(err)
	}
	if review.Ready || !strings.Contains(strings.Join(review.Start.Blockers, "\n"), "not covered") {
		t.Fatalf("requirements gap hidden: %#v", review.Start)
	}

	contextVault := deliveryTestVault(t)
	contextPlan := validDeliveryPlanV2()
	contextPlan.HumanGates = nil
	contextPath := writeDeliveryV2TestPlan(t, contextVault, contextPlan)
	if err := newV7Task(Args{
		"vault": contextVault, "quiet": "true", "epic": "APP", "id": "APP-T-0001", "title": "New related work",
		"risk": "medium", "priority": "p1", "domains": "project", "spec-refs": contextPlan.SpecRefs[0], "v7": "true",
	}); err != nil {
		t.Fatal(err)
	}
	review, err = buildDeliveryReview(contextVault, contextPath)
	if err != nil {
		t.Fatal(err)
	}
	if review.Ready || review.Start.ContextFingerprint == "" || !strings.Contains(strings.Join(review.Start.Blockers, "\n"), "planning context fingerprint differs") {
		t.Fatalf("stale planning context must fail closed: %#v", review.Start)
	}
}

func TestDeliveryPlanReviewShowsOnlyHumanDecisionsAndMaterialWarnings(t *testing.T) {
	vault := deliveryTestVault(t)
	plan := operationalDeliveryPlanV2()
	path := writeDeliveryV2TestPlan(t, vault, plan)
	review, err := buildDeliveryReview(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Decisions) != 2 {
		t.Fatalf("expected human gate and unresolved product decision, got %#v", review.Decisions)
	}
	if len(review.Flow.SharedResources) != 1 || review.Flow.SharedResources[0].Capacity == nil || *review.Flow.SharedResources[0].Capacity != 1 {
		t.Fatalf("declared shared resource was hidden or misrepresented: %#v", review.Flow.SharedResources)
	}
	for _, decision := range review.Decisions {
		if strings.Contains(strings.ToLower(decision.Title), "task") {
			t.Fatalf("task mechanics leaked into decision: %#v", decision)
		}
	}

	// Removing the explicit overlap strategy turns concurrently-owned work into
	// a material warning rather than a hidden scheduling detail.
	plan.OwnedPathOverlaps = nil
	plan.Tasks[1].Dependencies = nil
	plan.Concurrency = 2
	path = writeDeliveryV2TestPlan(t, vault, plan)
	review, err = buildDeliveryReview(vault, path)
	if err != nil {
		t.Fatal(err)
	}
	if review.Ready || len(review.Flow.Warnings) == 0 {
		t.Fatalf("collision warning missing: %#v", review)
	}
	if len(review.Flow.SharedResources) != 1 || len(review.Flow.SharedResources[0].Constraints) == 0 {
		t.Fatalf("resource conflict was not projected onto its declared resource: %#v", review.Flow.SharedResources)
	}
}
