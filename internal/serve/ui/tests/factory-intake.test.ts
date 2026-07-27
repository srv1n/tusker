import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../src/features/delivery/DeliveryReview.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const types = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");

test("delivery review renders every canonical section with exact fingerprint confirmation", () => {
  for (const section of ["What will be delivered", "How it will be proven", "How work flows", "What needs your decision", "Start boundary"]) expect(source).toContain(section);
  expect(source).toContain('aria-label="Exact plan fingerprint confirmation"');
  expect(source).toContain("confirmation.trim() === reviewedFingerprint");
  expect(source).toContain("This cannot enable automation");
});

test("delivery surface keeps a narrow layout and renders the canonical state matrix truthfully", () => {
  expect(source).toContain("xl:grid-cols-[minmax(0,1fr)_22rem]");
  expect(source).toContain("sm:grid-cols-2");
  expect(source).toContain('>Blocked</p>');
  for (const state of ["held", "invalid", "changed", "disabled", "daemon-off", "runner-blocked", "shared-workspace", "gated", "armed", "running", "parked", "completed"]) expect(types).toContain(`"${state}"`);
  expect(source).toContain('data-delivery-state={starting ? "importing" : start.state}');
  expect(source).toContain("Wait while Tusker imports and authorizes this exact fingerprint.");
  expect(source).toContain('aria-live="polite"');
  expect(source).toContain("start.stateLabel");
  expect(source).toContain("start.nextAction");
  expect(source).toContain("Delivery ${phase} is stale");
  expect(source).toContain("Delivery ${phase} is blocked");
  expect(source).toContain("Delivery ${phase} is invalid");
  expect(source).toContain('title={startResult.replayed ? "Already started" : "Delivery started"}');
  expect(api).toContain('deliveryRequest("GET", withProject(`/delivery/review?plan=${encodeURIComponent(plan)}`, projectId))');
  expect(api).toContain('deliveryRequest("POST", withProject("/delivery/start", projectId), body)');
});

test("review keeps canonical relationships, links resolvable records, and clears cross-plan Start state", () => {
  for (const relation of ["item.links", "proof.taskHref", "proof.checks", "check.href", "proof.artifactRefs", "resource.taskLinks", "decision.gateHref"]) expect(source).toContain(relation);
  expect(source).toContain("proof.requirements.join");
  expect(source).toContain("artifact.acceptanceIds.join");
  expect(source).toContain("review.howWorkFlows.sharedResources");
  expect(source).toContain("start.reset()");
  expect(source).toContain("plan.trim() === submittedPlan");
  expect(source).toContain("start.variables?.plan === submittedPlan");
  expect(source).toContain("start.variables?.confirm === reviewedFingerprint");
  expect(source).toContain("mutationMatchesReview ? start.data : undefined");
  expect(source).toContain("mutationMatchesReview ? start.error : null");
  expect(api).toContain("throw new DeliveryError(res.status");
});

test("delivery waits for an explicit plan path and never shows a stale review for edited input", () => {
  expect(source).toContain('const defaultPlan = "";');
  expect(source).toContain("const inputMatchesReview = plan.trim() === submittedPlan");
  expect(source).toContain('placeholder="docs/plans/example-v2.yaml"');
  expect(source).toContain("disabled={!plan.trim()}");
  expect(source).toContain('title="Choose a delivery plan"');
  expect(source).toContain('title="Review this plan"');
  expect(source).toContain("inputMatchesReview && review.error");
  expect(source).toContain("inputMatchesReview && data");
});

test("delivery tolerates nullable collections from an older daemon", () => {
  expect(source).toContain("(proof.resourceRefs ?? []).length");
  expect(source).toContain("(review.howWorkFlows.warnings ?? []).map");
  expect(source).toContain("(start.blockers ?? []).length");
  expect(source).toContain("(start.blockers ?? []).map");
});
