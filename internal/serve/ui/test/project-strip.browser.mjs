/*
  Routed browser proof for WUX-T-0016/0017.

  Default mode uses an isolated in-memory fixture so the acceptance run is
  repeatable and never changes a real project. `--live` deliberately skips
  interception and requires TUSKER_PROJECT_STRIP_LIVE_URL.
*/

import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";

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
if (!playwright) throw new Error("Playwright is not installed. Set TUSKER_PLAYWRIGHT_MODULE to an existing index.mjs.");

const { chromium } = playwright;
const live = process.argv.includes("--live");
const desktopOnly = process.argv.includes("--case=desktop");
const baseUrl = (live ? process.env.TUSKER_PROJECT_STRIP_LIVE_URL : process.env.TUSKER_PROJECT_STRIP_BASE_URL ?? "http://127.0.0.1:5193")?.replace(/\/$/, "");
if (!baseUrl) throw new Error("Live proof requires TUSKER_PROJECT_STRIP_LIVE_URL; no live service was contacted.");

const outDir = fileURLToPath(new URL("../../../../docs/reports/project-strip/", import.meta.url));
mkdirSync(outDir, { recursive: true });

const makeProjects = (count) => Array.from({ length: count }, (_, index) => ({
  id: `strip-project-${index + 1}`,
  name: index === 12 ? "A project with a deliberately long registered name" : `Project ${index + 1}`,
  repoRoot: `/tmp/tusker-project-${index + 1}`,
  vaultRoot: `/tmp/tusker-project-${index + 1}/.tusker`,
  automationEnabled: false,
  health: "healthy",
  needsCount: index === 1 ? 2 : 0,
  activeRuns: 0,
  worstLiveness: null,
  daemonConnected: true,
}));
const projects = makeProjects(13);

const factoryOperations = {
  schema: "tusker.factory-operations/v1",
  readOnly: true,
  generatedAt: "2026-09-10T00:00:00Z",
  project: {
    id: "fixture",
    name: "Fixture",
    registered: true,
    enabled: true,
    health: "healthy",
    automationEnabled: false,
    automationProvenance: "Fixture",
    dispatchScope: { configured: "project", effective: "project", provenance: "Fixture" },
    completionMode: { configured: "review", effective: "review", provenance: "Fixture" },
    promotionMode: { configured: false, mode: "manual", provenance: "Fixture", observe: true, stage: false, promote: false, release: false },
  },
  authority: { defaultRef: "main", waves: [] },
  capacity: { global: { active: 0, limit: 4, available: 4 }, project: { active: 0, limit: 1, available: 1 }, resourceHolds: [] },
  sectionOrder: ["delivered", "workingNow", "reviewOrRework", "blocked", "needsYourDecision", "nextFrontier"],
  delivered: [], workingNow: [], reviewOrRework: [], blocked: [], needsYourDecision: [], nextFrontier: [],
};

async function installFixture(page, fixtureProjects = projects) {
  if (live) return;
  await page.route(/\/api\//, (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/stream") return route.abort();
    if (path === "/api/projects") return route.fulfill({ json: fixtureProjects });
    if (path === "/api/daemon") return route.fulfill({ json: { connected: true, addr: "127.0.0.1:7420", crashLoop: { open: false } } });
    if (path === "/api/factory-operations") return route.fulfill({ json: factoryOperations });
    if (path === "/api/capabilities") return route.fulfill({ json: { schema: "fixture", capabilities: [] } });
    if (path === "/api/review/batch") return route.fulfill({ json: { waves: [], unwaved: [] } });
    if (path.startsWith("/api/docgraph")) return route.fulfill({ json: { docs: [], graph: { nodes: [], edges: [], graph_generated: false }, issues: [] } });
    return route.fulfill({ json: [] });
  });
}

async function openPage(browser, viewport, path = "/", fixtureProjects = projects) {
  const context = await browser.newContext({ viewport });
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  await installFixture(page, fixtureProjects);
  await page.goto(`${baseUrl}${path}`, { waitUntil: "domcontentloaded" });
  await page.locator('[data-project-strip]').waitFor({ state: "visible", timeout: 15_000 }).catch(() => {
    if (!live) throw new Error("project strip did not render against the fixture");
  });
  return { context, page, pageErrors };
}

async function assertHealthy(page, pageErrors) {
  assert.deepEqual(pageErrors, [], "route emitted no uncaught browser errors");
  assert.equal(await page.getByText("Something went wrong!", { exact: true }).count(), 0, "route did not render the error boundary");
}

async function screenshot(page, name) {
  await page.screenshot({ path: `${outDir}/${name}`, fullPage: false });
}

async function projectNames(page) {
  return page.locator('[data-project-chip]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute("title")));
}

async function waitForShell(page) {
  await page.locator('[data-project-strip]').waitFor({ state: "visible", timeout: 15_000 });
  await page.waitForTimeout(100);
}

async function testProjectStripDesktop(browser) {
  const { context, page } = await openPage(browser, { width: 1440, height: 1000 }, "/p/strip-project-1/waves");
  try {
    await waitForShell(page);
    assert.equal(await page.locator('[data-project-chip]').count(), 13, "every fixture project appears exactly once");
    assert.equal(await page.locator("aside.project-rail").count(), 1, "project rail is present");
    assert.equal(await page.locator('nav[aria-label="Project sections"]').count(), 1, "section rail is present");
    await page.getByRole("button", { name: "App actions", exact: true }).click();
    assert.equal(await page.getByRole("button", { name: "Search tasks", exact: true }).count(), 1, "search is icon-only with an accessible name");
    assert.equal(await page.getByRole("link", { name: "App settings", exact: true }).count(), 1, "global settings is icon-only with an accessible name");
    assert.equal(await page.getByRole("link", { name: "Work", exact: true }).count(), 0, "global scope has no project-local row");
    assert.equal(await page.locator(".project-notification-control").evaluate((node) => getComputedStyle(node).position), "fixed", "notifications float outside both rails");
    await page.getByRole("button", { name: "App actions", exact: true }).click();
    const geometry = await page.evaluate(() => ({
      bodyWidth: document.body.scrollWidth,
      viewportWidth: window.innerWidth,
      projectRailWidth: document.querySelector(".project-rail")?.getBoundingClientRect().width ?? 0,
      sectionRailWidth: document.querySelector(".section-rail")?.getBoundingClientRect().width ?? 0,
      projectTop: document.querySelector("[data-project-chip]")?.getBoundingClientRect().top ?? 0,
      sectionTop: document.querySelector(".section-rail > div:first-child > a")?.getBoundingClientRect().top ?? 0,
      projectFooter: document.querySelector(".project-rail-footer")?.getBoundingClientRect().height ?? 0,
      sectionFooter: document.querySelector(".section-rail > :last-child")?.getBoundingClientRect().height ?? 0,
      projectTargets: [...document.querySelectorAll("[data-project-chip]")].map((node) => [node.getBoundingClientRect().width, node.getBoundingClientRect().height]),
      sectionTargets: [...document.querySelectorAll(".section-rail > div:first-child > a")].map((node) => node.getBoundingClientRect().height),
    }));
    assert.ok(geometry.bodyWidth <= geometry.viewportWidth, "the page has no horizontal overflow");
    assert.equal(geometry.projectRailWidth, 160, "the expanded project rail is 160px");
    assert.equal(geometry.sectionRailWidth, 160, "the expanded section rail is 160px");
    assert.equal(geometry.projectTop, 8, "project items start at the top grid inset");
    assert.equal(geometry.sectionTop, 8, "section items start at the same top grid inset");
    assert.equal(geometry.projectFooter, 144, "the project rail ends on the three-row footer grid");
    assert.equal(geometry.sectionFooter, 144, "the section rail ends on the three-row footer grid");
    assert.deepEqual(new Set(geometry.projectTargets.map(([, height]) => height)), new Set([40]), "project targets use one 40px row module");
    assert.deepEqual(new Set(geometry.sectionTargets), new Set([40]), "section targets use the same 40px module");
    await screenshot(page, "after-expanded.png");

    await page.getByRole("button", { name: "Minimize project navigation", exact: true }).click();
    await page.getByRole("button", { name: "Minimize section navigation", exact: true }).click();
    await page.waitForTimeout(250);
    const collapsed = await page.evaluate(() => ({
      project: document.querySelector(".project-rail")?.getBoundingClientRect().width ?? 0,
      section: document.querySelector(".section-rail")?.getBoundingClientRect().width ?? 0,
      projectTargets: [...document.querySelectorAll("[data-project-chip]")].map((node) => [node.getBoundingClientRect().width, node.getBoundingClientRect().height]),
    }));
    assert.deepEqual(collapsed, { project: 56, section: 56, projectTargets: Array(13).fill([40, 40]) }, "both rails independently collapse onto the same icon grid");
    await screenshot(page, "after-collapsed.png");

    await page.getByRole("link", { name: "Docs", exact: true }).click();
    await page.waitForURL(/strip-project-1\/knowledge$/);
    const contextGeometry = await page.locator('aside[aria-label="Documents explorer"]:visible').evaluate((pane) => {
      const filter = pane.querySelector('input[aria-label="Filter documents"]')?.closest("div");
      return {
        width: pane.getBoundingClientRect().width,
        headerHeight: filter?.getBoundingClientRect().height ?? 0,
      };
    });
    assert.equal(contextGeometry.width, 280, "the context pane is seven 40px modules wide");
    assert.equal(contextGeometry.headerHeight, 56, "the context filter shares the 56px header datum");
    await screenshot(page, "after-docs-grid.png");
  } finally {
    await context.close();
  }
}

async function testOverflowAndKeyboard(browser) {
  const { context, page } = await openPage(browser, { width: 390, height: 844 });
  try {
    await waitForShell(page);
    const overflow = await page.locator('[data-project-strip]').evaluate((element) => ({ scrollHeight: element.scrollHeight, clientHeight: element.clientHeight, bodyWidth: document.body.scrollWidth, viewportWidth: window.innerWidth }));
    assert.ok(overflow.scrollHeight >= overflow.clientHeight, "project rail owns vertical overflow");
    assert.ok(overflow.bodyWidth <= overflow.viewportWidth, "narrow layout does not scroll the page horizontally");
    assert.equal(await page.getByRole("button", { name: "Scroll projects right", exact: true }).count(), 0, "horizontal overflow arrows are removed");
    await screenshot(page, "after-overflow.png");
    await screenshot(page, "after-narrow.png");

    const last = page.locator('[data-project-chip]').last();
    await last.focus();
    const reveal = await page.locator('[data-project-strip]').evaluate((track) => ({ scrollTop: track.scrollTop, scrollHeight: track.scrollHeight, clientHeight: track.clientHeight }));
    assert.ok(reveal.scrollTop >= 0 && reveal.scrollTop + reveal.clientHeight <= reveal.scrollHeight + 2, "keyboard focus remains inside the vertical rail");
  } finally {
    await context.close();
  }
}

async function testProjectCounts(browser) {
  for (const count of [0, 1, 50]) {
    const { context, page, pageErrors } = await openPage(browser, { width: 390, height: 844 }, "/", makeProjects(count));
    try {
      await waitForShell(page);
      assert.equal(await page.locator('[data-project-chip]').count(), count, `${count} projects render exactly once`);
      assert.equal(await page.getByRole("button", { name: "Scroll projects right", exact: true }).count(), 0, `${count} projects has no horizontal overflow controls`);
      if (count === 1) {
        await page.getByRole("button", { name: "Project 1", exact: true }).click();
        await page.getByRole("button", { name: "App actions", exact: true }).click();
        await page.getByRole("button", { name: "Pin project", exact: true }).click();
        assert.equal((await projectNames(page))[0], "Project 1", "the single project can be pinned without moving");
      }
      if (count === 50) {
        const last = page.locator('[data-project-chip]').last();
        await last.focus();
        assert.ok(await page.locator('[data-project-strip]').evaluate((track) => track.scrollTop >= 0), "the fiftieth project is focus-revealed");
      }
      await assertHealthy(page, pageErrors);
    } finally {
      await context.close();
    }
  }
}

async function testPersistenceAndPins(browser) {
  const { context, page } = await openPage(browser, { width: 1440, height: 900 });
  try {
    await waitForShell(page);
    const initial = await projectNames(page);
    await page.getByRole("button", { name: "Project 2", exact: true }).click();
    await page.waitForURL(/strip-project-2\/waves$/);
    assert.deepEqual(await projectNames(page), initial, "switching does not reshuffle the mounted strip");

    await page.getByRole("button", { name: "Project 1", exact: true }).click();
    await page.waitForURL(/strip-project-1\/waves$/);
    await page.getByRole("button", { name: "App actions", exact: true }).click();
    await page.getByRole("button", { name: "Pin project", exact: true }).click();
    await waitForShell(page);
    assert.equal((await projectNames(page))[0], "Project 1", "pin moves the project to the front");
    await page.reload({ waitUntil: "domcontentloaded" });
    await waitForShell(page);
    assert.equal((await projectNames(page))[0], "Project 1", "pin persists across reload");

    await page.goto(`${baseUrl}/p/strip-project-1/knowledge`, { waitUntil: "domcontentloaded" });
    await waitForShell(page);
    await page.waitForTimeout(400);
    await page.getByRole("button", { name: "Project 2", exact: true }).click();
    await page.waitForURL(/strip-project-2\/waves$/);
    await page.getByRole("button", { name: "Project 1", exact: true }).click();
    await page.waitForURL(/strip-project-1\/knowledge$/);
  } finally {
    await context.close();
  }
}

async function testScopeAndRegression(browser) {
  const { context, page, pageErrors } = await openPage(browser, { width: 1024, height: 900 });
  const methods = new Set();
  page.on("request", (request) => methods.add(request.method()));
  try {
    await waitForShell(page);
    await page.getByRole("link", { name: "App settings", exact: true }).click();
    await page.waitForURL(/\/settings$/);
    await page.getByText("Appearance", { exact: true }).waitFor();
    assert.equal(await page.locator('[aria-current="page"][data-project-chip]').count(), 0, "global settings clears project-current styling");
    assert.equal(await page.locator('nav[aria-label*="project navigation"]').count(), 0, "global settings hides local navigation");
    await assertHealthy(page, pageErrors);
    await screenshot(page, "after-global-settings.png");

    await page.goto(`${baseUrl}/p/strip-project-2/waves`, { waitUntil: "domcontentloaded" });
    await waitForShell(page);
    assert.equal(await page.locator('[aria-current="page"][data-project-chip]').count(), 1, "project route has one current chip");
    assert.equal(await page.getByRole("link", { name: "Waves", exact: true }).count(), 1, "Waves remains directly reachable");
    assert.equal(await page.getByRole("link", { name: "Docs", exact: true }).count(), 1, "Docs remains directly reachable");
    assert.equal(await page.getByRole("link", { name: "Settings", exact: true }).count(), 1, "project settings keeps its scope");
    await page.getByRole("link", { name: "Settings", exact: true }).click();
    await page.waitForURL(/\/p\/strip-project-2\/settings$/);
    await page.getByText("Background work", { exact: true }).waitFor();
    await assertHealthy(page, pageErrors);
    await screenshot(page, "after-project-settings.png");
    await page.goto(`${baseUrl}/p/strip-project-2/waves`, { waitUntil: "domcontentloaded" });
    await waitForShell(page);
    await page.getByRole("button", { name: "App actions", exact: true }).click();
    const actions = page.locator(".project-strip-menu");
    for (const label of ["Plan", "Trains", "Diagnostics", "Refresh project"]) assert.equal(await actions.getByText(label, { exact: true }).count(), 1, `${label} remains in secondary actions`);

    await page.getByRole("button", { name: "Add project", exact: true }).click();
    assert.equal(await page.locator("[data-add-project-form]").count(), 1, "registration remains reachable");
    await page.getByRole("button", { name: "Close add project form", exact: true }).click();
    await page.getByRole("button", { name: "Search tasks", exact: true }).click();
    assert.ok(await page.getByRole("dialog").count() >= 1, "task search remains reachable");
    await page.keyboard.press("Escape");

    await page.goto(`${baseUrl}/p/strip-project-1/knowledge`, { waitUntil: "domcontentloaded" });
    await waitForShell(page);
    await page.getByRole("button", { name: "Project 2", exact: true }).click();
    await page.waitForURL(/strip-project-2\/waves$/);
    await page.goBack();
    await page.waitForURL(/strip-project-1\/knowledge$/);
    await page.locator('[data-project-chip="strip-project-1"][aria-current="page"]').waitFor();
    await page.goForward();
    await page.waitForURL(/strip-project-2\/waves$/);
    await page.locator('[data-project-chip="strip-project-2"][aria-current="page"]').waitFor();

    assert.deepEqual([...methods].filter((method) => method !== "GET"), [], "browser proof performs no mutating request");

    const embeddedContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
    const embeddedPage = await embeddedContext.newPage();
    await installFixture(embeddedPage);
    await embeddedPage.goto(`${baseUrl}/panel?shell=1`, { waitUntil: "domcontentloaded" });
    await embeddedPage.waitForTimeout(1_000);
    assert.equal(await embeddedPage.locator('[data-project-strip]').count(), 0, "embedded panel keeps its compact shell contract");
    assert.equal(await embeddedPage.getByText("Tusker triage", { exact: true }).count(), 1, "embedded panel remains reachable");
    await embeddedContext.close();
  } finally {
    await context.close();
  }
}

async function testLiveReadOnly(browser) {
  const { context, page, pageErrors } = await openPage(browser, { width: 1440, height: 900 });
  const methods = new Set();
  page.on("request", (request) => methods.add(request.method()));
  try {
    await waitForShell(page);
    const first = page.locator('[data-project-chip]').first();
    assert.ok(await first.count(), "live service exposes at least one project");
    const projectName = (await first.getAttribute("title")) ?? "";
    await first.click();
    await page.waitForURL(/\/p\/[^/]+\/waves$/);
    await page.locator('[data-project-chip][aria-current="page"]').waitFor();
    await page.getByRole("link", { name: "Docs", exact: true }).click();
    await page.waitForURL(/\/p\/[^/]+\/knowledge$/);
    await page.getByRole("link", { name: "App settings", exact: true }).click();
    await page.waitForURL(/\/settings$/);
    assert.equal(await page.locator('[data-project-chip][aria-current="page"]').count(), 0, "live global Settings clears project scope");
    await page.goBack();
    await page.waitForURL(/\/p\/[^/]+\/knowledge$/);
    await page.locator('[data-project-chip][aria-current="page"]').waitFor();
    await page.getByRole("link", { name: "Settings", exact: true }).click();
    await page.waitForURL(/\/p\/[^/]+\/settings$/);
    await page.goBack();
    await page.waitForURL(/\/p\/[^/]+\/knowledge$/);
    await page.goForward();
    await page.waitForURL(/\/p\/[^/]+\/settings$/);
    await screenshot(page, "live-after-desktop.png");
    await assertHealthy(page, pageErrors);
    assert.deepEqual([...methods].filter((method) => method !== "GET"), [], "live qualification performs GET requests only");
  } finally {
    await context.close();
  }
}

const browser = await chromium.launch({ channel: "chrome", headless: true });
try {
  if (live) {
    await testLiveReadOnly(browser);
    console.log(`PASS live read-only switching, scope and history at ${baseUrl}`);
  } else {
    await testProjectStripDesktop(browser);
    console.log("PASS project strip desktop");
    if (desktopOnly) process.exitCode = 0;
    else {
    await testOverflowAndKeyboard(browser);
    console.log("PASS project strip overflow and keyboard");
    await testProjectCounts(browser);
    console.log("PASS project strip 0, 1 and 50 project matrix");
    await testPersistenceAndPins(browser);
    console.log("PASS project strip persistence and pins");
    await testScopeAndRegression(browser);
    console.log("PASS project strip scope and regression");
    }
  }
} finally {
  await browser.close();
}
