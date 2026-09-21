import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

test("direct wave authority is projected, not re-derived", () => {
  const source = readFileSync("src/features/workbench/integration/WaveAuthority.tsx", "utf8");

  expect(source).toContain("WaveAuthorityControls");
  expect(source).toContain("useWaveReview");
  expect(source).toContain("useWaveControl");
  expect(source).toContain("data-wave-authority");
  expect(source).toContain("data-wave-state");
  expect(source).toContain("data-wave-control");
  expect(source).not.toContain("Authorize delivery");
  expect(source).not.toContain("planFingerprint");
  expect(source).not.toContain("DeliveryError");
});

test("work surfaces expose direct task start and wave controls", () => {
  const tasks = readFileSync("src/features/product/TaskScreens.tsx", "utf8");
  const delivery = readFileSync("src/features/product/DeliveryScreens.tsx", "utf8");
  const api = readFileSync("src/lib/api.ts", "utf8");

  expect(tasks).toContain("useTaskStart(taskId, projectId)");
	expect(tasks).toContain('aria-label={`Run task ${detail.id}`}');
	expect(tasks).toContain('aria-label={`${RECOVERY_ACTION_LABEL} ${detail.id}`}');
	expect(tasks).toContain('run.data?.outcome === "outcome-unknown"');
	expect(tasks).toContain("ActionResultLine");
	expect(tasks).toContain('directiveQueued ? "Queued"');
  expect(delivery).toContain('<ProductSection title="Tickets"');
  expect(delivery).toContain('<ProductSection title="Dependency DAG">');
  expect(delivery).toContain("renderMermaid(source)");
  expect(delivery).toContain("WaveAuthorityControls");
  expect(api).toContain('/waves/${encodeURIComponent(waveId)}/review');
  expect(api).toContain('/actions/projects/${encodeURIComponent(projectId)}/waves/');
  expect(api).toContain('/actions/projects/${encodeURIComponent(projectId)}/tasks/');
  expect(delivery).not.toContain("Play is blocked");
});
