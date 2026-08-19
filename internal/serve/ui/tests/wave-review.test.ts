import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const tasksSource = readFileSync(new URL("../src/features/v2/TaskScreens.tsx", import.meta.url), "utf8");
const waveSource = readFileSync(new URL("../src/features/work/WaveReview.tsx", import.meta.url), "utf8");
const workUtilsSource = readFileSync(new URL("../src/features/work/work-utils.ts", import.meta.url), "utf8");
const opsSource = readFileSync(new URL("../src/features/ops/ProjectOps.tsx", import.meta.url), "utf8");
const taskContractSource = readFileSync(new URL("../src/features/docs/TaskContract.tsx", import.meta.url), "utf8");
const routerSource = readFileSync(new URL("../src/router.tsx", import.meta.url), "utf8");

test("the live Tasks route mounts wave review and action surfaces", () => {
  expect(tasksSource).toContain("useReviewBatch(projectId)");
  expect(tasksSource).toContain("<WaveReviewGroups");
  expect(tasksSource).toContain("<BatchBar");
  expect(tasksSource).toContain("onSelectWave={(wave) => setSelectedIds(new Set(wave.members.filter(isBatchSelectable).map((task) => task.id)))}");
  expect(waveSource).toContain("ready for your review");
  expect(waveSource).toContain("disabled={disabled || !wave.readyForReview || selectable.length === 0}");
  expect(routerSource).toContain('path: "tasks"');
  expect(routerSource).toContain('"@/features/v2/TaskScreens"');
  expect(routerSource).toContain('"TasksV2"');
});

test("terminal waves cannot expose Land or enter BatchBar selection", () => {
  expect(opsSource).toContain("Boolean(wave.landedAt)");
  expect(opsSource).toContain('["closed", "cancelled", "superseded"].includes(wave.status)');
  expect(opsSource).not.toContain('wave.status === "landed"');
  expect(opsSource).toContain("!terminal");
  expect(workUtilsSource).toContain('task.status === "done" && task.waveTerminal');
  expect(tasksSource).toContain("isBatchSelectable(task)");
  expect(taskContractSource).toContain("const terminalWave = Boolean(task.waveTerminal)");
  expect(taskContractSource).toContain("!terminalWave && <option value=\"land\">");
  expect(taskContractSource).toContain('activeAction === "land" && !terminalWave');
});
