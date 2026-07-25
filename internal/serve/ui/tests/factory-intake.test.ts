import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = readFileSync(new URL("../src/features/delivery/DeliveryReview.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");

test("delivery review renders every canonical section with exact fingerprint confirmation", () => {
  for (const section of ["What will be delivered", "How it will be proven", "How work flows", "What needs your decision", "Start boundary"]) expect(source).toContain(section);
  expect(source).toContain('aria-label="Exact plan fingerprint confirmation"');
  expect(source).toContain("confirmation.trim() === data.startBoundary.planFingerprint");
  expect(source).toContain("This cannot enable automation");
});

test("delivery surface keeps a narrow layout and renders blocked, stale, and started truthfully", () => {
  expect(source).toContain("xl:grid-cols-[minmax(0,1fr)_22rem]");
  expect(source).toContain("sm:grid-cols-2");
  expect(source).toContain('title="Blocked"');
  expect(source).toContain('title={stale ? "Review is stale"');
  expect(source).toContain('title={startResult.replayed ? "Already started" : "Delivery started"}');
  expect(api).toContain('withProject(`/delivery/review?plan=${encodeURIComponent(plan)}`, projectId)');
  expect(api).toContain('withProject("/delivery/start", projectId)');
});
