/*
  task-authoring-ui-acceptance — read-only browser checks for the authored
  tier, route, provenance, wave and human-action contract.

  This deliberately consumes the real Serve API and the built UI. It does not
  seed records, click a mutating control, or use sample-data React fixtures.
  Missing project/scenario prerequisites return exit 2 so they cannot be
  mistaken for a UI pass.
*/

import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";

const playwrightCandidates = [
  process.env.TUSKER_PLAYWRIGHT_MODULE,
  "/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs",
  "/tmp/tusker-ui-spec-inspection/node_modules/playwright/index.mjs",
].filter(Boolean);

let playwright;
for (const candidate of playwrightCandidates) {
  try {
    playwright = await import(candidate);
    break;
  } catch {
    // Use the next already-installed location.
  }
}
if (!playwright) {
  console.error("READINESS Playwright is not installed; set TUSKER_PLAYWRIGHT_MODULE to an existing module.");
  process.exit(2);
}

const { chromium } = playwright;
const baseUrl = (process.env.TUSKER_REALWORK_BASE_URL ?? "").replace(/\/$/, "");
const projectId = process.env.TUSKER_REALWORK_PROJECT ?? "";
const requestedTaskID = process.env.TUSKER_AUTHORING_SCENARIO_TASK_ID ?? process.env.TUSKER_AUTHORING_TASK_ID ?? "";
const requestedWaveID = process.env.TUSKER_AUTHORING_SCENARIO_WAVE_ID ?? process.env.TUSKER_AUTHORING_WAVE_ID ?? "";
const outDir = resolve(process.env.TUSKER_REALWORK_OUT ?? "docs/reports/task-authoring/browser");

const requiredBodyHeadings = ["## Intent", "## Implementation notes", "## Acceptance", "## Verification"];

function readiness(message) {
  console.error(`READINESS ${message}`);
  console.error("READINESS result: authoring browser qualification did not run.");
  process.exit(2);
}

if (!baseUrl || !projectId) readiness("set TUSKER_REALWORK_BASE_URL and TUSKER_REALWORK_PROJECT");

const results = [];
function record(id, name, status, detail = "") {
  results.push({ id, name, status, detail });
  console.log(`${status} ${id} ${name}${detail ? ` — ${detail}` : ""}`);
}

async function api(path) {
  const response = await fetch(`${baseUrl}/api/${path}`);
  if (!response.ok) throw new Error(`GET /api/${path} -> ${response.status}`);
  return response.json();
}

let projects;
let tasks;
let waves;
try {
  projects = await api("projects");
  if (!Array.isArray(projects) || !projects.some((project) => project.id === projectId)) {
    readiness(`project ${projectId} is not registered at ${baseUrl}`);
  }
  [tasks, waves] = await Promise.all([
    api(`tasks?project=${encodeURIComponent(projectId)}`),
    api(`waves?project=${encodeURIComponent(projectId)}`),
  ]);
} catch (error) {
  readiness(`Serve API is not reachable: ${error.message}`);
}
if (!Array.isArray(tasks) || tasks.length === 0) readiness("registered project has no tasks");

let builtHTML;
try {
  const response = await fetch(`${baseUrl}/`);
  if (!response.ok) readiness(`built UI entry returned HTTP ${response.status}`);
  builtHTML = await response.text();
} catch (error) {
  readiness(`built UI entry is not reachable: ${error.message}`);
}
if (!/\/assets\//.test(builtHTML) || /\/src\//.test(builtHTML)) {
  readiness("service does not expose a built asset entry; use the candidate Serve/UI build");
}
record("B0", "candidate service exposes built UI assets", "PASS", baseUrl);

const detailFor = async (id) => api(`tasks/${encodeURIComponent(id)}?project=${encodeURIComponent(projectId)}`);
const details = [];
for (const task of tasks) {
  try {
    details.push(await detailFor(task.id));
  } catch {
    // The selected task below reports an actionable readiness failure.
  }
}
function scenarioBlockers(task) {
  if (!task) return ["task detail is unavailable"];
  const blockers = [];
  if (!["light", "standard", "demanding"].includes(task.authoredWorkLevel)) blockers.push("missing authored work tier");
  if (!task.body?.trim()) blockers.push("missing canonical task body");
  if (!task.intent?.trim()) blockers.push("missing Intent");
  if (!Array.isArray(task.acceptance) || task.acceptance.length === 0) blockers.push("missing Acceptance");
  if (!Array.isArray(task.verification) || task.verification.length === 0) blockers.push("missing Verification");
  for (const heading of requiredBodyHeadings) {
    if (!task.body?.split("\n").some((line) => line.trim() === heading)) blockers.push(`missing ${heading}`);
  }
  if (!task.architect || !task.origin || !task.authoringProvenance) blockers.push("missing architect/origin/provenance");
  const routeBlockers = [
    ...(task.effectiveExecute?.blockers ?? []).map((reason) => `execute: ${reason}`),
    ...(task.effectiveReview?.blockers ?? []).map((reason) => `review: ${reason}`),
  ];
  if (!task.effectiveExecute?.profile || !task.effectiveReview?.profile || routeBlockers.length > 0) {
    blockers.push(`unresolved execute/review route${routeBlockers.length > 0 ? ` (${routeBlockers.join("; ")})` : ""}`);
  }
  return blockers;
}

const authored = details.filter((task) => ["light", "standard", "demanding"].includes(task.authoredWorkLevel));
const selected = requestedTaskID
  ? details.find((task) => task.id === requestedTaskID)
  : authored.find((task) => task.waveId && scenarioBlockers(task).length === 0);
if (!selected && !requestedTaskID) {
  const candidates = authored.length > 0
    ? authored.map((task) => {
      const blockers = [...scenarioBlockers(task), ...(task.waveId ? [] : ["missing wave membership"])]
      return `${task.id}: ${blockers.join(", ")}`;
    }).join(" | ")
    : "no task exposes a supported authored work tier";
  readiness(`no complete disposable authored scenario is available; ${candidates}. Seed/import a fresh scenario, then set TUSKER_AUTHORING_SCENARIO_TASK_ID and TUSKER_AUTHORING_SCENARIO_WAVE_ID`);
}
const selectedBlockers = scenarioBlockers(selected);
if (selectedBlockers.length > 0) {
  const target = requestedTaskID ? `task ${requestedTaskID}` : "a disposable authored scenario";
  readiness(`${target} is not ready for browser qualification: ${selectedBlockers.join(", ")}; select a fresh authored task from the disposable wave import (TUSKER_AUTHORING_SCENARIO_TASK_ID)${requestedTaskID ? " instead of a historical task" : ""}`);
}
record("B1", "task detail retains tier, routes and provenance", "PASS", `${selected.id} · Tier ${selected.authoredWorkLevel}`);

const selectedWave = (Array.isArray(waves) ? waves : []).find((wave) => wave.id === requestedWaveID || wave.id === selected.waveId);
if (!selectedWave) readiness(`no wave contains selected authored task ${selected.id}`);
if (!selectedWave.memberIds?.includes(selected.id)) readiness(`wave ${selectedWave.id} does not contain selected task ${selected.id}`);
const waveMember = selectedWave.members?.find((member) => member.id === selected.id);
if (!waveMember?.effectiveExecute?.profile || !waveMember?.effectiveReview?.profile) readiness(`wave ${selectedWave.id} does not expose both effective routes for ${selected.id}`);
if (waveMember.effectiveExecute.profile !== selected.effectiveExecute.profile || waveMember.effectiveReview.profile !== selected.effectiveReview.profile) {
  throw new Error(`wave/task route mismatch for ${selected.id}: task=${selected.effectiveExecute.profile}/${selected.effectiveReview.profile} wave=${waveMember.effectiveExecute.profile}/${waveMember.effectiveReview.profile}`);
}
record("B2", "wave membership and task routes agree", "PASS", `${selectedWave.id} · ${selected.id}`);

const waveReviewFor = async (waveId) => api(`projects/${encodeURIComponent(projectId)}/waves/${encodeURIComponent(waveId)}/review`);
let selectedReview;
try {
  selectedReview = await waveReviewFor(selectedWave.id);
} catch (error) {
  readiness(`direct wave review endpoint is not reachable: ${error.message}`);
}
if (selectedReview?.schema !== "tusker.wave-review/v1") readiness(`wave ${selectedWave.id} review did not return tusker.wave-review/v1`);
record("B5", "wave review API returns the canonical direct projection", "PASS", `${selectedWave.id} · ${selectedReview.state}/${selectedReview.authorization}`);

const standalone = authored.find((task) => !task.waveId && scenarioBlockers(task).length === 0);
if (!standalone) readiness("no standalone authored task is available for the no-synthetic-wave check");

const humanTask = details.find((task) => (task.humanActions?.length ?? 0) > 0 || (task.humanAction && task.hasGate));
const blockedTask = details.find((task) => task.effectiveExecute?.blockers?.length || task.effectiveReview?.blockers?.length);
const requireHuman = process.env.TUSKER_AUTHORING_REQUIRE_HUMAN_GATE !== "0";
if (requireHuman && !humanTask) readiness("no open human action is available for the human boundary check");
if (!blockedTask) readiness("no route-blocked task is available for the fail-closed Play check");
if (humanTask) record("B4", "open human action is present in API detail", "PASS", `${humanTask.id} · ${humanTask.humanActions?.length ?? 1} action(s)`);
const routeBlockedWave = (Array.isArray(waves) ? waves : []).find((wave) =>
  wave.members?.some((member) => member.effectiveExecute?.blockers?.length || member.effectiveReview?.blockers?.length)
);
const humanGatedWave = humanTask && (Array.isArray(waves) ? waves : []).find((wave) => wave.memberIds?.includes(humanTask.id));
if (!routeBlockedWave) readiness("no route-blocked wave is available for the blocked-wave check");
if (requireHuman && !humanGatedWave) readiness("no human-gated wave is available for the blocked-wave check");

let browser;
try {
  browser = await chromium.launch({ channel: "chrome", headless: true });
} catch (error) {
  console.error(`BROWSER headless Chrome could not start: ${error.message.split("\n")[0]}`);
  process.exit(1);
}
mkdirSync(outDir, { recursive: true });

function escaped(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

async function openPage(path, viewport) {
  const context = await browser.newContext({ viewport });
  const page = await context.newPage();
  const mutations = [];
  page.on("request", (request) => {
    if (!["GET", "HEAD"].includes(request.method())) mutations.push(`${request.method()} ${request.url()}`);
  });
  const response = await page.goto(`${baseUrl}/p/${projectId}${path}`, { waitUntil: "domcontentloaded" });
  assert.equal(response?.status(), 200, `${path} loads`);
  await page.locator('main[data-wux-ready="true"]').waitFor({ state: "visible", timeout: 15_000 });
  return { context, page, mutations };
}

try {
  {
    const { context, page, mutations } = await openPage(`/tasks/${selected.id}`, { width: 1440, height: 1000 });
    try {
      const main = page.locator("main");
      await main.getByText("Routing", { exact: true }).waitFor({ state: "visible", timeout: 15_000 });
      const text = await main.textContent();
      assert.match(text ?? "", /Will execute/);
      assert.match(text ?? "", /Will review/);
      assert.match(text ?? "", new RegExp(escaped(selected.architect)));
      assert.match(text ?? "", new RegExp(escaped(selected.origin)));
      assert.match(text ?? "", new RegExp(escaped(selected.effectiveExecute.profile)));
      assert.match(text ?? "", new RegExp(escaped(selected.effectiveReview.profile)));
      if (humanTask?.id === selected.id) assert.match(text ?? "", /Self implementation is unavailable while the named human action is open/);
      const contract = main.getByLabel("Full task contract");
      await contract.locator("summary").focus();
      await contract.locator("summary").press("Enter");
      assert.equal(await contract.getAttribute("open"), null, "Enter closes the task contract disclosure");
      await contract.locator("summary").press("Enter");
      assert.notEqual(await contract.getAttribute("open"), null, "Enter reopens the task contract disclosure");
      await page.reload({ waitUntil: "domcontentloaded" });
      await page.locator('main[data-wux-ready="true"]').waitFor({ state: "visible", timeout: 15_000 });
      const refreshed = await detailFor(selected.id);
      const refreshedText = await page.locator("main").textContent();
      assert.match(refreshedText ?? "", new RegExp(escaped(refreshed.effectiveExecute.profile)));
      assert.match(refreshedText ?? "", new RegExp(escaped(refreshed.effectiveReview.profile)));
      assert.deepEqual(mutations, [], "task detail performs no mutating request");
      record("A1", "task screen exposes fresh routes, provenance and keyboard disclosure", "PASS", selected.id);
      await page.screenshot({ path: resolve(outDir, "task-authoring-detail.png") });
    } finally {
      await context.close();
    }
  }

  {
    const { context, page, mutations } = await openPage(`/tasks/${blockedTask.id}`, { width: 1440, height: 1000 });
    try {
      const main = page.locator("main");
      await main.getByText("Routing", { exact: true }).waitFor({ state: "visible", timeout: 15_000 });
      assert.equal(await main.getByRole("button", { name: `Start task ${blockedTask.id}` }).count(), 0, "route blocker suppresses task Start");
      assert.match((await main.textContent()) ?? "", /blocked|unavailable|missing|disabled/i);
      assert.deepEqual(mutations, [], "blocked task inspection performs no mutating request");
      record("A4", "route blockers suppress task Start", "PASS", blockedTask.id);
    } finally {
      await context.close();
    }
  }

  if (humanTask) {
    const { context, page, mutations } = await openPage(`/tasks/${humanTask.id}`, { width: 1440, height: 1000 });
    try {
      const main = page.locator("main");
      await main.getByText(/Needs you|Action required|Human action/i).first().waitFor({ state: "visible", timeout: 15_000 });
      assert.equal(await main.getByRole("button", { name: `Start task ${humanTask.id}` }).count(), 0, "human action suppresses task Start");
      assert.deepEqual(mutations, [], "human task inspection performs no mutating request");
      record("A5", "human action is actionable and cannot trigger LLM execution", "PASS", humanTask.id);
    } finally {
      await context.close();
    }
  }

  {
    const { context, page, mutations } = await openPage(`/tasks/${standalone.id}`, { width: 1440, height: 1000 });
    try {
      const main = page.locator("main");
      await main.getByText("Routing", { exact: true }).waitFor({ state: "visible", timeout: 15_000 });
      const text = await main.textContent();
      assert.match(text ?? "", new RegExp(escaped(standalone.id)));
      assert.match(text ?? "", /Will execute/);
      assert.match(text ?? "", /Will review/);
      assert.deepEqual(mutations, [], "standalone task inspection performs no mutating request");
      record("B3", "standalone authored task works through the built task route", "PASS", `${standalone.id} · Tier ${standalone.authoredWorkLevel}`);
    } finally {
      await context.close();
    }
  }

  {
    const { context, page, mutations } = await openPage(`/waves/${selectedWave.id}`, { width: 1440, height: 1000 });
    try {
      const main = page.locator("main");
      await main.locator("[data-wave-authority]").first().waitFor({ state: "visible", timeout: 15_000 });
      const text = (await main.textContent()) ?? "";
      assert.match(text, new RegExp(escaped(selected.id)));
      assert.match(text, new RegExp(`Tier ${escaped(selected.authoredWorkLevel)}`, "i"));
      assert.match(text, new RegExp(escaped(waveMember.effectiveExecute.profile)));
      assert.match(text, new RegExp(escaped(waveMember.effectiveReview.profile)));
      assert.match(text, new RegExp(escaped(selectedReview.state), "i"), "wave surface shows the direct review state");
      for (const blocker of selectedReview.blockers ?? []) {
        assert.ok(text.includes(blocker.reason), `wave blocker reason visible: ${blocker.code}`);
        if (blocker.action) assert.ok(text.includes(blocker.action), `wave blocker action visible: ${blocker.code}`);
      }
      assert.ok(!/\bplan\b/i.test(text), "wave surface never asks for a plan");
      assert.ok(!/\bPlay\b/.test(text), "wave surface has no Play choreography");
      for (const control of (selectedReview.controls ?? []).filter((control) => control.enabled)) {
        const controlButton = main.locator(`[data-wave-control="${control.action}"]`).first();
        await controlButton.waitFor({ state: "visible", timeout: 15_000 });
        assert.ok(await controlButton.isEnabled(), `${control.action} must be usable`);
      }
      const memberWithInstructions = (selectedReview.members ?? []).find((member) => member.instructions);
      if (memberWithInstructions) {
        const disclosure = main.locator(`[data-wave-instructions="${memberWithInstructions.taskId}"] summary`).first();
        if (await disclosure.count()) {
          await disclosure.focus();
          await page.keyboard.press("Enter");
          assert.match((await main.textContent()) ?? "", new RegExp(escaped(memberWithInstructions.instructions.slice(0, 40))), "instructions disclosure opens on Enter");
        }
      }
      assert.deepEqual(mutations, [], "desktop wave inspection performs no mutating request");
      record("A2", "wave screen exposes direct state, controls, blockers and member instructions", "PASS", `${selectedWave.id} · ${selectedReview.state}`);
      await page.screenshot({ path: resolve(outDir, "task-authoring-wave.png") });
      await page.reload({ waitUntil: "domcontentloaded" });
      await page.locator('main[data-wux-ready="true"]').waitFor({ state: "visible", timeout: 15_000 });
      await main.locator("[data-wave-authority]").first().waitFor({ state: "visible", timeout: 15_000 });
      const refreshedReview = await waveReviewFor(selectedWave.id);
      if (refreshedReview?.schema !== "tusker.wave-review/v1") readiness("refreshed wave review did not return tusker.wave-review/v1");
      assert.equal(refreshedReview.materialFingerprint, selectedReview.materialFingerprint, "refreshed wave material fingerprint drifted without an edit");
      assert.match((await main.textContent()) ?? "", new RegExp(escaped(refreshedReview.state), "i"), "reloaded wave surface reflects refreshed review state");
      assert.deepEqual(mutations, [], "wave reload performs no mutating request");
      record("A7", "wave reload reflects the refreshed direct review", "PASS", `${selectedWave.id} · ${refreshedReview.materialFingerprint}`);
    } finally {
      await context.close();
    }
  }

  for (const actionBlockedWave of [...new Map([routeBlockedWave, humanGatedWave].filter(Boolean).map((wave) => [wave.id, wave])).values()]) {
    let blockedReview;
    try {
      blockedReview = await waveReviewFor(actionBlockedWave.id);
    } catch (error) {
      readiness(`blocked wave review is not reachable: ${error.message}`);
    }
    const { context, page, mutations } = await openPage(`/waves/${actionBlockedWave.id}`, { width: 1440, height: 1000 });
    try {
      const main = page.locator("main");
      await main.locator("[data-wave-authority]").first().waitFor({ state: "visible", timeout: 15_000 });
      const text = (await main.textContent()) ?? "";
      for (const control of await main.locator("[data-wave-control]").all()) {
        assert.ok(!(await control.isEnabled()), "blocked wave exposes no enabled authority control");
      }
      for (const blocker of blockedReview.blockers ?? []) {
        assert.ok(text.includes(blocker.reason), `blocked wave reason visible: ${blocker.code}`);
        if (blocker.action) assert.ok(text.includes(blocker.action), `blocked wave action visible: ${blocker.code}`);
      }
      assert.deepEqual(mutations, [], "blocked wave inspection performs no mutating request");
      const boundary = actionBlockedWave.id === humanGatedWave?.id ? "human-gated" : "route-blocked";
      record(`A6-${boundary}`, `${boundary} wave suppresses Start`, "PASS", actionBlockedWave.id);
    } finally {
      await context.close();
    }
  }

  {
    const { context, page, mutations } = await openPage(`/tasks/${selected.id}`, { width: 390, height: 844 });
    try {
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(overflow <= 1, `narrow task screen has no horizontal overflow (${overflow}px)`);
      assert.deepEqual(mutations, [], "narrow task detail performs no mutating request");
      record("A3", "narrow task authoring screen remains operable", "PASS", "390x844");
      await page.screenshot({ path: resolve(outDir, "task-authoring-narrow.png") });
    } finally {
      await context.close();
    }
  }

  {
    const { context, page, mutations } = await openPage(`/waves/${selectedWave.id}`, { width: 390, height: 844 });
    try {
      const main = page.locator("main");
      await main.locator("[data-wave-authority]").first().waitFor({ state: "visible", timeout: 15_000 });
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(overflow <= 1, `narrow wave screen has no horizontal overflow (${overflow}px)`);
      for (const control of await main.locator("[data-wave-control]").all()) {
        assert.ok(await control.isVisible(), "narrow wave control remains visible");
        await control.focus();
        assert.ok(await control.evaluate((element) => element === document.activeElement), "narrow wave control is focusable");
      }
      assert.deepEqual(mutations, [], "narrow wave inspection performs no mutating request");
      record("A8", "narrow wave screen remains operable", "PASS", "390x844");
      await page.screenshot({ path: resolve(outDir, "task-authoring-wave-narrow.png") });
    } finally {
      await context.close();
    }
  }

  {
    const { context, page, mutations } = await openPage("/work", { width: 1440, height: 1000 });
    try {
      assert.equal(await page.locator(`a[href$="/plan"], a[href*="/plan?"]`).count(), 0, "no navigation destination links to /plan");
      assert.deepEqual(mutations, [], "navigation inspection performs no mutating request");
      record("A9", "plan is no longer a navigation destination", "PASS", "/plan");
    } finally {
      await context.close();
    }
  }
} finally {
  await browser.close();
}

const failed = results.filter((result) => result.status === "FAIL");
console.log(`AUTHORING_BROWSER result: ${failed.length === 0 ? "PASS" : "FAIL"} (${results.length} checks)`);
process.exit(failed.length === 0 ? 0 : 1);
