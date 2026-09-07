import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = (path: string) => readFileSync(path, "utf8");

test("project navigation keeps primary destinations and exposes secondary routes, errors, and actions", () => {
  const sidebar = source("src/components/Sidebar.tsx");
  const delivery = source("src/features/product/DeliveryScreens.tsx");

  for (const label of ["Work", "Documents", "Settings", "More", "Board", "Plan", "Trains", "Diagnostics"]) expect(sidebar).toContain(label);
  for (const destination of ["/p/$projectId/waves", "/p/$projectId/knowledge", "/p/$projectId/settings", "/p/$projectId/tasks", "/p/$projectId/diagnostics"]) expect(sidebar).toContain(destination);
  expect(sidebar).toContain("open={secondarySelected}");
  expect(sidebar).toContain("Refresh failed — check this project’s source.");
  expect(sidebar).toContain("Repair in Settings");
  expect(delivery).toContain("wave.brief.humanAction");
  expect(delivery).toContain("action.gateHref");
  expect(delivery).toContain("artifact.evidenceHref");
  expect(delivery).toContain('to="/p/$projectId/runs/$taskId"');
});
