import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const read = (path: string) => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("task detail renders every human gate and can focus a searched gate", () => {
  const router = read("src/router.tsx");
  const documentView = read("src/features/docs/DocumentView.tsx");
  const task = read("src/features/docs/TaskContract.tsx");

  expect(router).toContain("gate?: string");
  expect(documentView).toContain("focusGateId={gate}");
  expect(task).toContain("task.humanActions");
  expect(task).toContain("humanActions.map");
  expect(task).toContain("b.gateId === focusGateId");
});

test("gate action cards identify blockers and expose satisfy, rework, waive, and obsolete", () => {
  const card = read("src/features/human-action/HumanActionCard.tsx");
  expect(card).toContain("action.gateId");
  expect(card).toContain("blocks ");
  expect(card).toContain('action: "satisfy"');
  expect(card).toContain('status: "rework"');
  expect(card).toContain('disposeGate("waive")');
  expect(card).toContain('disposeGate("obsolete")');
});

test("compact triage keys actions by gate", () => {
  const panel = read("src/features/panel/Panel.tsx");
  expect(panel).toContain("key={humanActionIdentity(item)}");
});
