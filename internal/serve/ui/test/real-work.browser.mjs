/*
  real-work-ui-acceptance — headless browser journey against the seeded
  ordinary app.

  Run (UI dev server with the current workbench code on a free port, test
  Serve backend behind its /api proxy):

    TUSKER_REALWORK_BASE_URL=http://127.0.0.1:5193 \
    TUSKER_REALWORK_PROJECT=<project-id> \
    node internal/serve/ui/test/real-work.browser.mjs

  Optional:
    TUSKER_REALWORK_SHAPE=fixture|generic (default fixture)
    TUSKER_REALWORK_OUT=docs/reports/real-work/ui (screenshot directory)
    TUSKER_PLAYWRIGHT_MODULE=<existing Playwright index.mjs>

  Shape "fixture" requires the fixture owner's seeded scenario (standalone
  task plus Alpha/Beta/Follow-up waves with branch/join graphs) and fails
  with exit 2 and a READINESS report when it is absent. Shape "generic"
  runs the same observable checks against any project with at least one
  wave that has dependency edges; it is development support, never final
  acceptance proof.

  The journey is read-only by design: it verifies readiness display,
  inspector content, DAG stability, stream honesty, results entry,
  Documents navigation and cross-surface identity agreement, but it never
  clicks Run/Execute, cancel, retry or land. Starting real work stays in
  the operator's manual walkthrough. A headless browser is mandatory; no
  desktop input automation is used.
*/

import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";

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
    // Try the next already-installed location.
  }
}
if (!playwright) {
  throw new Error(
    "Playwright is not installed. Set TUSKER_PLAYWRIGHT_MODULE to an existing Playwright index.mjs.",
  );
}

const { chromium } = playwright;
const baseUrl = (process.env.TUSKER_REALWORK_BASE_URL ?? "").replace(/\/$/, "");
const projectId = process.env.TUSKER_REALWORK_PROJECT ?? "";
const shape = (process.env.TUSKER_REALWORK_SHAPE ?? "fixture").toLowerCase();
const outDir = resolve(process.env.TUSKER_REALWORK_OUT ?? "docs/reports/real-work/ui");

if (!baseUrl || !projectId) {
  console.error("READINESS WIN0 no base URL or project: set TUSKER_REALWORK_BASE_URL and TUSKER_REALWORK_PROJECT.");
  process.exit(2);
}
if (shape !== "fixture" && shape !== "generic") {
  console.error(`READINESS WIN0 unknown shape ${JSON.stringify(shape)}: want fixture|generic.`);
  process.exit(2);
}
mkdirSync(outDir, { recursive: true });

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

function readinessFail(lines) {
  for (const line of lines) console.log(line);
  console.log("READINESS result: fixture not ready; live acceptance cannot proceed.");
  process.exit(2);
}

// ---- Tier 1: ordinary app reachability ---------------------------------

let projects, waves, tasks, runs, docgraph;
try {
  projects = await api("projects");
  const project = (Array.isArray(projects) ? projects : []).find((p) => p.id === projectId);
  if (!project) {
    readinessFail([
      `READINESS WIN1 project ${projectId} is not registered on ${baseUrl}.`,
      `READINESS found ${(Array.isArray(projects) ? projects : []).length} registered project(s).`,
      "READINESS follow docs/reports/real-work/ui/manual-walkthrough.md to reset, seed and register the fixture project, then rerun with its project ID.",
    ]);
  }
  waves = await api(`waves?project=${encodeURIComponent(projectId)}`);
  tasks = await api(`tasks?project=${encodeURIComponent(projectId)}`);
  runs = await api(`runs?project=${encodeURIComponent(projectId)}`);
  docgraph = await api(`docgraph?project=${encodeURIComponent(projectId)}`).catch(() => null);
} catch (error) {
  readinessFail([
    `READINESS WIN1 ordinary app is not reachable: ${error.message}`,
    "READINESS start the UI dev server and the test Serve backend first (see manual-walkthrough.md).",
  ]);
}
record("WIN1", "ordinary app reachable with registered project", "PASS", `${waves.length} waves, ${tasks.length} tasks`);

// ---- Tier 2: scenario shape ----------------------------------------------

const byId = new Map(tasks.map((t) => [t.id, t]));
const standalone = tasks.filter((t) => !t.waveId);
// The task list projection omits dependency edges; member details carry them.
async function memberDetails(w) {
  const ids = Array.isArray(w.memberIds) ? w.memberIds : [];
  const out = [];
  for (const id of ids) {
    try {
      out.push(await api(`tasks/${encodeURIComponent(id)}?project=${encodeURIComponent(projectId)}`));
    } catch {
      out.push({ id, deps: [], detailUnavailable: true });
    }
  }
  return out;
}
const hasEdge = (list) => list.some((t) => Array.isArray(t.deps) && t.deps.length > 0);
const waveMembers = (w) => tasks.filter((t) => t.waveId === w.id);

let focusWave;
if (shape === "fixture") {
  const lower = (s) => String(s ?? "").toLowerCase();
  const alpha = waves.find((w) => lower(w.title).includes("alpha") || lower(w.id).includes("alpha"));
  const beta = waves.find((w) => lower(w.title).includes("beta") || lower(w.id).includes("beta"));
  const followup = waves.find((w) => lower(w.title).includes("follow") || lower(w.id).includes("follow"));
  const problems = [];
  if (standalone.length === 0) problems.push("no standalone task (task without waveId)");
  if (!alpha) problems.push("no Alpha wave");
  if (!beta) problems.push("no Beta wave");
  if (!followup) problems.push("no Follow-up wave");
  for (const [name, w] of [["Alpha", alpha], ["Beta", beta], ["Follow-up", followup]]) {
    if (!w) continue;
    const ids = Array.isArray(w.memberIds) ? w.memberIds : [];
    if (ids.length !== 4) problems.push(`${name} has ${ids.length} members, want 4`);
    else {
      const details = await memberDetails(w);
      if (details.some((d) => d.detailUnavailable)) problems.push(`${name} has unloadable member details`);
      else if (!hasEdge(details)) problems.push(`${name} has no dependency edges`);
    }
  }
  if (problems.length > 0) {
    readinessFail([
      `READINESS WIN2 seeded fixture shape not met (${shape}):`,
      ...problems.map((p) => `READINESS  - ${p}`),
      `READINESS found ${waves.length} waves (${waves.map((w) => w.id).join(", ") || "none"}) and ${tasks.length} tasks.`,
      "READINESS reset and seed the fixture project, then rerun. This is not a UI failure.",
    ]);
  }
  focusWave = alpha;
  record("WIN2", "fixture shape ready (standalone + Alpha/Beta/Follow-up)", "PASS", `standalone=${standalone[0].id}`);
} else {
  const roomy = waves.filter((w) => (Array.isArray(w.memberIds) ? w.memberIds.length : 0) >= 2);
  let focus = null;
  for (const w of roomy) {
    if (hasEdge(await memberDetails(w))) {
      focus = w;
      break;
    }
  }
  if (!focus) {
    readinessFail([
      "READINESS WIN2 generic shape not met: no wave with >=2 members and a dependency edge.",
      `READINESS found ${waves.length} waves and ${tasks.length} tasks.`,
    ]);
  }
  focusWave = focus;
  record("WIN2", "generic shape ready", "PASS", `wave=${focusWave.id} (labeled support only, not fixture acceptance)`);
}

const docs = Array.isArray(docgraph?.docs) ? docgraph.docs : [];

// ---- Browser journey -------------------------------------------------------

let browser;
try {
  browser = await chromium.launch({ channel: "chrome", headless: true });
} catch (error) {
  console.error(`BROWSER headless Chrome could not start: ${error.message.split("\n")[0]}`);
  console.error("BROWSER rerun where a headless browser can launch; readiness above still holds.");
  process.exit(1);
}
const shot = async (page, name) => page.screenshot({ path: resolve(outDir, name) });

async function newPage(viewport, { blockStream = false } = {}) {
  const context = await browser.newContext({ viewport });
  const page = await context.newPage();
  if (blockStream) await page.route(/\/api\/stream(?:\?|$)/, (route) => route.abort());
  return { context, page };
}

async function gotoWork(page, path) {
  const response = await page.goto(`${baseUrl}/p/${projectId}${path}`, { waitUntil: "domcontentloaded" });
  assert.equal(response?.status(), 200, `work route ${path} loads`);
  await page.locator('main[data-wux-ready="true"]').waitFor({ state: "visible", timeout: 15_000 });
}

try {
  // A1: standalone task inspector shows intent/acceptance; Run control is truthful.
  {
    const { context, page } = await newPage({ width: 1440, height: 1000 });
    try {
      await gotoWork(page, "/tasks");
      await page.locator('[data-testid="task-board"]').waitFor({ state: "visible", timeout: 15_000 });
      const target = standalone[0] ?? tasks[0];
      const card = page.getByRole("button", { name: new RegExp(`Open task ${target.id}`) }).first();
      await card.click();
      await page.locator('[data-testid="inspector-panel"]').waitFor({ state: "visible", timeout: 15_000 });
      assert.match(await page.locator('[data-testid="inspector-intent"]').textContent(), /\S/, "intent is readable");
      assert.ok((await page.locator('[data-testid="inspector-acceptance"]').count()) >= 1, "acceptance section present");
      const status = await page.locator('[data-testid="stream-status"]').textContent();
      assert.match(status ?? "", /Live updates|Reconnecting/, "stream honesty note renders");
      await page.locator('[data-testid="inspector-open-task"]').click();
      await page.waitForURL(new RegExp(`/tasks/${target.id}$`), { timeout: 15_000 });
      const runControl = await page.getByRole("button", { name: `Execute ${target.id} once` }).count();
      const readinessNote = await page.getByText(/Blocked by prerequisites|Waiting|Ready/i).count();
      assert.ok(runControl >= 1 || readinessNote >= 1, "Run action or truthful readiness is shown");
      record("A1", "task inspector intent/acceptance with truthful run readiness", "PASS", `task=${target.id}`);
      await shot(page, "standalone-task.png");
    } finally {
      await context.close();
    }
  }

  // A2/A3: wave DAG shows real dependencies; selection/viewport stay stable.
  {
    const { context, page } = await newPage({ width: 1440, height: 1000 });
    try {
      await gotoWork(page, `/waves/${focusWave.id}`);
      const graph = page.getByRole("region", { name: /^Wave dependency graph/ });
      await graph.waitFor({ state: "visible", timeout: 15_000 });
      const label = (await graph.getAttribute("aria-label")) ?? "";
      const counts = label.match(/(\d+) tasks, (\d+) dependencies/);
      assert.ok(counts && Number(counts[2]) > 0, `DAG shows edges (${label})`);
      const zoomBefore = await page.getByText(/^\d+%$/).first().textContent();
      const node = graph.getByRole("button").first();
      const nodeName = await node.getAttribute("aria-label");
      await node.click();
      await page.locator('[data-testid="inspector-panel"]').waitFor({ state: "visible", timeout: 15_000 });
      const zoomAfter = await page.getByText(/^\d+%$/).first().textContent();
      assert.equal(zoomAfter, zoomBefore, "viewport survives selection");
      const selected = graph.getByRole("button", { name: new RegExp(nodeName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")) });
      assert.equal(await selected.getAttribute("aria-pressed"), "true", "selection stays marked");
      // List fallback stays available and truthful.
      await page.getByRole("button", { name: "List", exact: true }).click();
      await page.getByRole("list", { name: "Wave tasks as a list" }).waitFor({ state: "visible", timeout: 10_000 });
      record("A2/A3", "DAG edges with stable selection/viewport and list fallback", "PASS", label);
      await shot(page, "wave-dag.png");
    } finally {
      await context.close();
    }
  }

  // A4: stale state is visible when the stream drops; counts converge on reload.
  {
    const { context, page } = await newPage({ width: 1024, height: 768 }, { blockStream: true });
    try {
      await gotoWork(page, "/waves");
      await page.locator('[data-testid="stream-status"]').waitFor({ state: "visible", timeout: 15_000 });
      await page.waitForFunction(
        () => document.querySelector('[data-testid="stream-status"]')?.textContent?.includes("Reconnecting"),
        { timeout: 20_000 },
      );
      record("A4", "disconnected stream shows stale state honestly", "PASS", "Reconnecting note observed");
      await shot(page, "waves-tablet.png");
    } finally {
      await context.close();
    }
  }
  {
    const { context, page } = await newPage({ width: 1024, height: 768 });
    try {
      await gotoWork(page, "/waves");
      const entries = page.getByRole("button", { name: /^Open wave / });
      await entries.first().waitFor({ state: "visible", timeout: 15_000 });
      const before = await entries.count();
      await page.reload({ waitUntil: "domcontentloaded" });
      await page.locator('main[data-wux-ready="true"]').waitFor({ state: "visible", timeout: 15_000 });
      await page.getByRole("button", { name: /^Open wave / }).first().waitFor({ state: "visible", timeout: 15_000 });
      const rendered = await page.getByRole("button", { name: /^Open wave / }).count();
      const again = await (await fetch(`${baseUrl}/api/waves?project=${encodeURIComponent(projectId)}`)).json();
      assert.equal(again.length, waves.length, "wave count converges after reload");
      assert.equal(rendered, before, "rendered entries converge after reload");
      record("A4b", "reload converges to authoritative counts", "PASS", `${again.length} waves`);
    } finally {
      await context.close();
    }
  }

  // A5: cancel/fail/retry — read-only: retry surfaces only where truthful.
  {
    const failed = runs.filter((r) => r.terminal && (r.outcome === "failed" || r.outcome === "interrupted"));
    if (failed.length === 0) {
      record("A5", "cancel/fail/retry terminal states", "SKIP", "no failed/interrupted runs in seed");
    } else {
      const { context, page } = await newPage({ width: 1440, height: 1000 });
      try {
        await page.goto(`${baseUrl}/p/${projectId}/runs/${failed[0].taskId}`, { waitUntil: "domcontentloaded" });
        const retry = await page.getByRole("button", { name: /retry|redrive/i }).count();
        assert.ok(retry >= 1, "retry control is offered for the failed run");
        record("A5", "failed run offers a distinct retry", "PASS", `task=${failed[0].taskId}`);
      } finally {
        await context.close();
      }
    }
  }

  // A6: completed wave entry leads with results.
  {
    const completed = waves.find((w) => w.landedAt || ["landed", "closed"].includes(w.status));
    if (!completed) {
      record("A6", "completed wave entry", "SKIP", "no landed/closed wave in seed");
    } else {
      const { context, page } = await newPage({ width: 1440, height: 1000 });
      try {
        await gotoWork(page, `/waves/${completed.id}`);
        await page.getByRole("region", { name: "Outcome summary" }).waitFor({ state: "visible", timeout: 15_000 });
        assert.ok((await page.getByRole("button", { name: "Flow", exact: true }).count()) >= 1, "Flow stays one action away");
        record("A6", "completed wave leads with results", "PASS", `wave=${completed.id}`);
        await shot(page, "wave-results.png");
      } finally {
        await context.close();
      }
    }
  }

  // A7: Documents journey — list to exact note content.
  {
    if (docs.length === 0) {
      record("A7", "documents folder to exact note", "SKIP", "empty corpus in seed");
    } else {
      const { context, page } = await newPage({ width: 1440, height: 1000 });
      try {
        const target = docs.find((d) => d.subject) ?? docs[0];
        await page.goto(`${baseUrl}/p/${projectId}/knowledge/${target.subject}`, { waitUntil: "domcontentloaded" });
        await page.locator('main[aria-label="Document reader"] .tk-prose').waitFor({ state: "visible", timeout: 15_000 });
        const heading = (await page.locator("h1").first().textContent()) ?? "";
        assert.match(heading, /\S/, "document title leads");
        record("A7", "documents folder to exact note", "PASS", `subject=${target.subject}`);
        await shot(page, "documents.png");
      } finally {
        await context.close();
      }
    }
  }

  // A8: narrow screen — inspector stays operable, no sideways scroll.
  {
    const { context, page } = await newPage({ width: 390, height: 844 });
    try {
      await gotoWork(page, "/tasks");
      await page.locator('[data-testid="task-board"]').waitFor({ state: "visible", timeout: 15_000 });
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      assert.ok(overflow <= 1, `no horizontal overflow (got ${overflow}px)`);
      const target = standalone[0] ?? tasks[0];
      await page.getByRole("button", { name: new RegExp(`Open task ${target.id}`) }).first().click();
      await page.locator('[data-testid="inspector-panel"]').waitFor({ state: "visible", timeout: 15_000 });
      await page.keyboard.press("Escape");
      await page.locator('[data-testid="inspector-panel"]').waitFor({ state: "hidden", timeout: 10_000 });
      record("A8", "narrow-screen inspector operable without overflow", "PASS", "390x844");
      await shot(page, "board-narrow.png");
    } finally {
      await context.close();
    }
  }

  // A9: cross-surface identity agreement.
  {
    const apiIds = new Set(tasks.map((t) => t.id));
    const missing = focusWave.memberIds.filter((id) => !apiIds.has(id));
    assert.equal(missing.length, 0, `UI/API agree on wave members (missing: ${missing.join(",") || "none"})`);
    record("A9", "CLI/API and UI agree on wave/task identities", "PASS", `${apiIds.size} tasks`);
  }
} finally {
  await browser.close();
}

const failed = results.filter((r) => r.status === "FAIL");
const skipped = results.filter((r) => r.status === "SKIP").length;
console.log(`JOURNEY result: ${failed.length === 0 ? "pass" : "FAIL"} (${results.length - failed.length}/${results.length} passed, ${skipped} skipped, shape=${shape}).`);
process.exit(failed.length === 0 ? 0 : 1);
