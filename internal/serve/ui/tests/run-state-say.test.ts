import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

test("Say uses server route and preserves retry key until acknowledgement", () => {
  const source = readFileSync("src/features/runs/detail/RunSayBox.tsx", "utf8");
  expect(source).toContain("run.sayRoute?.available");
  expect(source).toContain("crypto.randomUUID()");
  expect(source).toContain("api.sayRun(run.taskId, message, key.current, run.projectId)");
  expect(source).toContain("Result uncertain. Retry uses the same message key.");
});

test("operator state drives the header and Continue availability", () => {
  const header = readFileSync("src/features/runs/detail/RunHeader.tsx", "utf8");
  const say = readFileSync("src/features/runs/detail/RunSayBox.tsx", "utf8");
  expect(header).toContain('quiet: "Quiet"');
  expect(header).toContain('waiting_on_you: "Waiting on you"');
  expect(say).toContain('action.action === "continue"');
});
