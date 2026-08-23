import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const card = readFileSync(new URL("../src/features/human-action/HumanActionCard.tsx", import.meta.url), "utf8");
const task = readFileSync(new URL("../src/features/docs/TaskContract.tsx", import.meta.url), "utf8");
const panel = readFileSync(new URL("../src/features/panel/Panel.tsx", import.meta.url), "utf8");

test("human action card renders the served contract and one contextual completion path", () => {
  expect(card).toContain("action.action");
  expect(card).toContain("action.whyAgentCannot");
  expect(card).toContain("action.completionCondition");
  expect(card).toContain("action.acceptance");
  expect(card).toContain('action: "satisfy"');
  expect(card).toContain('status: "rework"');
  expect(card).toContain("Why should this go back?");
  expect(card).toContain("Mark complete");
  expect(card).toContain("Return to rework");
  expect(card).toContain("blocks ");
  expect(card).toContain("action.gateId");
  expect(card).toContain('disposeGate("waive")');
  expect(card).toContain('disposeGate("obsolete")');
  expect(card).not.toContain("evidenceKind");
  expect(card).not.toContain("actor");
});

test("full task page places the shared card before task prose and hides generic gate controls", () => {
  expect(task).toContain("<HumanActionCard");
  expect(task.indexOf("<HumanActionCard")).toBeLessThan(task.indexOf('<Section label="Intent">'));
  expect(task).toContain("humanActions.map");
  expect(task).toContain("humanActions.length === 0 && <TaskActionPanel");
});

test("compact panel consumes the same live human-action payload", () => {
  expect(panel).toContain("item.humanAction");
  expect(panel).toContain("<HumanActionCard");
  expect(panel).toContain("compact");
  expect(panel).toContain(">Your action</h2>");
});
