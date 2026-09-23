/*
  run-session-continuity — isolated browser qualification for one real run.

  Required environment:
    TUSKER_RUN_SESSION_BASE_URL=http://127.0.0.1:7420
    TUSKER_RUN_SESSION_PROJECT=<disposable-project-id>
    TUSKER_RUN_SESSION_TASK=<fixture-task-id>
    TUSKER_RUN_SESSION_STATE_ROOT=/absolute/path/to/disposable/state
    TUSKER_RUN_SESSION_DISPOSABLE=1

  The explicit disposable marker and state-root declaration are mandatory. A
  resident Serve URL or live project is never an implicit default. Set
  TUSKER_RUN_SESSION_OUT to retain screenshots. Mutating controls require the
  separate TUSKER_RUN_SESSION_ALLOW_ACTIONS=1 opt-in.
*/

import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";

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
    // Use another already-installed Playwright module.
  }
}
if (!playwright) {
  console.error("READINESS Playwright is not installed; set TUSKER_PLAYWRIGHT_MODULE to an existing module.");
  process.exit(2);
}

const baseUrl = (process.env.TUSKER_RUN_SESSION_BASE_URL ?? "").replace(/\/$/, "");
const projectId = process.env.TUSKER_RUN_SESSION_PROJECT ?? "";
const taskId = process.env.TUSKER_RUN_SESSION_TASK ?? "";
const stateRoot = process.env.TUSKER_RUN_SESSION_STATE_ROOT ?? "";
const disposable = process.env.TUSKER_RUN_SESSION_DISPOSABLE === "1";
const allowActions = process.env.TUSKER_RUN_SESSION_ALLOW_ACTIONS === "1";
const outDir = resolve(process.env.TUSKER_RUN_SESSION_OUT ?? "docs/reports/run-session-continuity-ui");

function readinessFail(...lines) {
  for (const line of lines) console.error(line);
  console.error("READINESS result: isolated run-session browser qualification cannot proceed.");
  process.exit(2);
}

if (!baseUrl || !projectId || !taskId || !stateRoot || !disposable) {
  readinessFail(
    "READINESS required isolated inputs are missing: base URL, project, task, state root, and TUSKER_RUN_SESSION_DISPOSABLE=1 are required.",
    "READINESS this script never falls back to a resident database or live project.",
  );
}
if (!isAbsolute(stateRoot)) {
  readinessFail(`READINESS state root must be absolute: ${stateRoot}`);
}

mkdirSync(outDir, { recursive: true });
const results = [];
function record(id, name, status, detail = "") {
  results.push({ id, name, status, detail });
  console.log(`${status} ${id} ${name}${detail ? ` — ${detail}` : ""}`);
}

async function api(path, init) {
  const response = await fetch(`${baseUrl}/api/${path}`, init);
  const payload = await response.json().catch(() => null);
  if (!response.ok) throw new Error(`API ${path} -> ${response.status}: ${JSON.stringify(payload)}`);
  return payload;
}

let projects, run;
try {
  projects = await api("projects");
  if (!Array.isArray(projects) || !projects.some((project) => project.id === projectId)) {
    readinessFail(`READINESS disposable project ${projectId} is not registered on ${baseUrl}.`);
  }
  run = await api(`runs/${encodeURIComponent(taskId)}?project=${encodeURIComponent(projectId)}`);
} catch (error) {
  readinessFail(`READINESS built Serve/readback is unavailable: ${error.message}`);
}
if (!run || run.taskId !== taskId) readinessFail(`READINESS run ${taskId} did not read back from the declared project.`);
const activity = Array.isArray(run.events) ? run.events.filter((event) => event.activity) : [];
if (activity.length === 0) readinessFail("READINESS fixture run has no readable message/tool activity.");
record("A2", "Serve readback exposes the latest activity", "PASS", `${activity.length} activity events`);

const { chromium } = playwright;
let browser;
try {
  browser = await chromium.launch({ channel: "chrome", headless: true });
} catch (error) {
  console.error(`BROWSER headless Chrome could not start: ${error.message.split("\n")[0]}`);
  process.exit(1);
}

const screenshot = async (page, name) => page.screenshot({ path: resolve(outDir, name), fullPage: true });
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  page.on("pageerror", (error) => console.error(`BROWSER pageerror: ${error.message}`));
  try {
    const response = await page.goto(`${baseUrl}/p/${encodeURIComponent(projectId)}/runs/${encodeURIComponent(taskId)}`, { waitUntil: "domcontentloaded" });
    assert.equal(response?.status(), 200, "run route is served by the built app");
    await page.locator("[data-run-operator-facts]").waitFor({ state: "visible", timeout: 15_000 });
    await page.getByText("Recent messages", { exact: true }).waitFor({ state: "visible", timeout: 15_000 });

    const bodyText = await page.locator("body").textContent();
    const visibleActivity = activity.find((event) => event.text && bodyText?.includes(event.text));
    assert.ok(visibleActivity, "latest API activity is visible in the reopened run UI");
    assert.doesNotMatch(bodyText ?? "", /private-value|private reasoning|password=private-value/, "private fixture content is not visible");
    assert.match(bodyText ?? "", /Ownership & resume|Ownership &amp; resume/, "ownership/resume readback is visible");
    await screenshot(page, "run-session-before-reopen.png");
    record("A2", "built browser shows messages and recovery facts", "PASS", `run=${taskId}`);

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.locator("[data-run-operator-facts]").waitFor({ state: "visible", timeout: 15_000 });
    const afterReload = await page.locator("body").textContent();
    assert.ok(visibleActivity && afterReload?.includes(visibleActivity.text), "latest activity survives browser reopen");
    await screenshot(page, "run-session-after-reopen.png");
    record("A2", "browser reopen converges on persisted activity", "PASS");

    const controls = page.getByRole("button", { name: /Interrupt|Retry|Redrive|Continue|Recover|Start fresh|Verify/i });
    const controlCount = await controls.count();
    if (controlCount === 0) {
      record("A2", "recovery control is truthful for fixture state", "BLOCKED", "no recovery action rendered for this run state");
    } else {
      record("A2", "recovery control is truthful for fixture state", "PASS", `${controlCount} control(s)`);
      if (allowActions) {
        const stop = page.locator("[data-run-session-controls]").getByRole("button", { name: "Stop", exact: true });
        if (await stop.count() && await stop.isEnabled()) {
          await stop.click();
          const dialog = page.getByRole("dialog").filter({ hasText: "Persists a durable stop intent" });
          await dialog.waitFor({ state: "visible", timeout: 5_000 });
          await dialog.getByRole("button", { name: "Stop run", exact: true }).click();
          await page.waitForTimeout(250);
          const afterAction = await api(`runs/${encodeURIComponent(taskId)}?project=${encodeURIComponent(projectId)}`);
          assert.ok(afterAction.taskId === taskId, "mutating action readback stays bound to the fixture task");
          const observedAction = afterAction.controls?.pending?.action
            ?? afterAction.controls?.readback?.action
            ?? afterAction.actionReadback?.action;
          const settledStop = afterAction.processRunning === false
            && afterAction.attempts?.some((attempt) => attempt.id === afterAction.activeAttemptId && attempt.finishedAt);
          assert.ok(observedAction === "stop" || settledStop, "durable stop intent reaches pending or settled canonical readback");
          record("A2", "authorized recovery action reaches canonical readback", "PASS", observedAction ? `action=${observedAction}` : "action=stop settled");
        } else {
          record("A2", "authorized recovery action reaches canonical readback", "BLOCKED", "fixture exposes no enabled Stop control");
        }
      } else {
        console.log("ACTION controls were observed read-only; set TUSKER_RUN_SESSION_ALLOW_ACTIONS=1 for a separately authorized fixture mutation.");
      }
    }
  } finally {
    await context.close();
  }
} finally {
  await browser.close();
}

const blocked = results.filter((result) => result.status === "BLOCKED");
const failed = results.filter((result) => result.status === "FAIL");
console.log(`JOURNEY result: ${failed.length === 0 && blocked.length === 0 ? "pass" : "BLOCKED"} (${results.length - failed.length - blocked.length}/${results.length} passed).`);
process.exit(failed.length > 0 ? 1 : blocked.length > 0 ? 2 : 0);
