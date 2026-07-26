import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../src/features/delivery/DeliveryReview.tsx", import.meta.url), "utf8");
const types = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");

test("delivery review shows semantic and durable cross-scope dependency truth", () => {
  expect(source).toContain("review.howWorkFlows.crossScopeDependencies");
  expect(source).toContain("Producer must come first");
  expect(source).toContain("dependency.scope}/{dependency.sourceKey");
  expect(source).toContain("Durable target:");
  expect(source).toContain("Persisted contract:");
  expect(source).toContain("dependency.producerState");
  expect(source).toContain("dependency.implication");
  expect(types).toContain("persistedContractFingerprint");
});

test("structural and lifecycle blockers render one product-language repair without frontmatter", () => {
  expect(source).toContain('dependency.blockerClass === "structural" ? "Repair structure:" : "Resolve producer:"');
  expect(source).toContain("dependency.targetIntegrity");
  expect(source).toContain("dependency.producerLifecycle");
  expect(source).toContain("dependency.repair");
  expect(source).not.toContain("delivery_cross_scope_dependencies");
  expect(source).not.toContain("target_contract_fingerprint");
});

test("cross-scope review stays inside the existing read-only delivery boundary", () => {
  expect(source).toContain("This review is read-only.");
  expect(source).toContain("This cannot enable automation");
  expect(types).toContain('kind: "hard"');
  expect(types).toContain('blockerClass: "none" | "structural" | "lifecycle"');
  expect(types).toContain('targetIntegrity: "resolved" | "missing" | "corrupt"');
});
