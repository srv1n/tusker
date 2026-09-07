import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

test("delivery start says what it authorizes and where execution is observed", () => {
  const source = readFileSync("src/features/delivery/DeliveryReview.tsx", "utf8");

  expect(source).toContain('"Authorize delivery"');
  expect(source).toContain("does not launch a runner in this request");
  expect(source).toContain('"Delivery authorized"');
  expect(source).toContain("Open delivery status for the DAG, task status, and logs.");
  expect(source).not.toContain("choose Run once");
  expect(source).not.toContain('"Delivery started"');
});

test("work surfaces expose real task execution and an honest wave gap", () => {
  const tasks = readFileSync("src/features/product/TaskScreens.tsx", "utf8");
  const delivery = readFileSync("src/features/product/DeliveryScreens.tsx", "utf8");
  const api = readFileSync("src/lib/api.ts", "utf8");

  expect(tasks).toContain("useRunTask(taskId, projectId)");
  expect(tasks).toContain('aria-label={`Execute ${detail.id} once`}');
  expect(tasks).toContain("ActionResultLine");
  expect(delivery).toContain('<ProductSection title="Tickets"');
  expect(delivery).toContain('<ProductSection title="Dependency DAG">');
  expect(delivery).toContain("renderMermaid(source)");
  expect(api).toContain('`/waves/${encodeURIComponent(waveId)}/execute`');
  expect(delivery).toContain('wave.authorization.fingerprint ?? ""');
  expect(delivery).not.toContain("Confirm the current authorization fingerprint");
  expect(delivery).toContain("Queues the latest version of this wave for daemon dispatch.");
  expect(delivery).toContain("alreadyQueuedTaskIds");
});
