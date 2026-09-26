import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const source = (path: string) => readFileSync(path, "utf8");

test("project navigation keeps primary destinations and exposes secondary routes, errors, and actions", () => {
  const strip = source("src/features/workbench/navigation/ProjectStrip.tsx");
  const delivery = source("src/features/product/DeliveryScreens.tsx");
  const today = source("src/features/product/TodayScreens.tsx");

  for (const label of ["Inbox", "Work", "Docs", "Settings"]) expect(strip).toContain(label);
  for (const destination of ["/p/$projectId/waves", "/p/$projectId/knowledge", "/p/$projectId/settings", "/p/$projectId/diagnostics"]) expect(strip).toContain(destination);
  expect(strip).toContain("Refresh failed — check this project’s source.");
  expect(strip).toContain("Refresh projects");
  expect(strip).not.toContain("Repair in Settings");
  expect(today).toContain("<ProjectRegistrationRepair project={project} needsAttention />");
  expect(delivery).toContain("wave.brief.humanAction");
  expect(delivery).toContain("action.gateHref");
  expect(delivery).toContain("artifact.evidenceHref");
  expect(delivery).toContain('to="/p/$projectId/runs/$taskId"');
});

test("sidebar keeps registration attention out of the footer", () => {
  const sidebar = source("src/features/workbench/navigation/ProjectStrip.tsx");

  expect(sidebar).not.toContain("need attention");
  expect(sidebar).not.toContain("Repair in Settings");
  expect(sidebar).not.toContain("Troubleshooting in Settings");
});
