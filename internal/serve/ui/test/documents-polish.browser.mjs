/*
  Browser proof for WUX-T-0013. This intentionally runs outside Bun's unit
  suite: it uses the already-installed Playwright module and drives the real
  Documents route with an isolated, in-memory API fixture.

  Start the UI first, then run:
    node internal/serve/ui/test/documents-polish.browser.mjs

  Override TUSKER_DOCUMENTS_BASE_URL when the Vite server uses another URL.
  TUSKER_PLAYWRIGHT_MODULE can point at any existing Playwright installation;
  no project dependency is added for this proof.
*/

import assert from "node:assert/strict";

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
const baseUrl = (process.env.TUSKER_DOCUMENTS_BASE_URL ?? "http://127.0.0.1:5191").replace(/\/$/, "");
const projectId = process.env.TUSKER_DOCUMENTS_PROJECT ?? "documents-browser-proof";

const project = {
  id: projectId,
  name: "browser documents",
  repoRoot: "/tmp/tusker-documents-proof",
  vaultRoot: "/tmp/tusker-documents-proof/.tusker",
  automationEnabled: false,
  health: "Healthy",
  needsCount: 0,
  activeRuns: 0,
  worstLiveness: null,
  daemonConnected: true,
};

const docs = [
  {
    subject: "overview",
    title: "Overview",
    path: "docs/system/overview.md",
    kind: "canonical",
    status: "canonical",
    keywords: ["system"],
  },
  {
    subject: "target-doc",
    title: "Target doc",
    path: "docs/system/target-doc.md",
    kind: "spec",
    status: "draft",
    keywords: [],
  },
  {
    subject: "decision-log",
    title: "Decision log",
    path: ".tusker/specs/decision-log.md",
    kind: "decision",
    status: "accepted",
    keywords: [],
  },
];

function overviewDoc() {
  return {
    subject: "overview",
    title: "Overview",
    path: "docs/system/overview.md",
    kind: "canonical",
    status: "canonical",
    header: { status: "canonical", keywords: ["system"] },
    body: `# Overview

Read [[target-doc]].

\`\`\`mermaid
flowchart LR
  A[Start] --> B[Finish]
\`\`\`

\`\`\`mermaid
this is intentionally invalid mermaid
\`\`\``,
    links: [{ ref: "target-doc", subject: "target-doc", path: "docs/system/target-doc.md", resolved: true }],
    backlinks: [],
    successor: null,
    rev: "rev-1",
  };
}

function targetDoc() {
  return {
    subject: "target-doc",
    title: "Target doc",
    path: "docs/system/target-doc.md",
    kind: "spec",
    status: "draft",
    header: { status: "draft" },
    body: "# Target doc\n\nTarget content.",
    links: [],
    backlinks: [],
    successor: null,
    rev: "target-1",
  };
}

async function installFixture(page, { saveMode = "success" } = {}) {
  const state = {
    current: overviewDoc(),
    latest: null,
    putCount: 0,
    putPayloads: [],
  };

  await page.route(/\/api\/stream(?:\?|$)/, (route) => route.abort());
  await page.route(/\/api\/projects(?:\?|$)/, (route) => route.fulfill({ json: [project] }));
  await page.route(/\/api\/daemon(?:\?|$)/, (route) => route.fulfill({ json: { connected: true } }));
  await page.route(/\/api\/needs(?:\?|$)/, (route) => route.fulfill({ json: [] }));
  await page.route(/\/api\/capability(?:\?|$)/, (route) => route.fulfill({
    json: { capability: "documents-browser-proof", operatorActor: "human:test" },
  }));

  await page.route(/\/api\/docgraph(?:\/|\?)/, async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname.endsWith("/docgraph")) {
      return route.fulfill({
        json: {
          docs,
          graph: { nodes: [], edges: [], graph_generated: false },
          issues: [],
        },
      });
    }

    if (!url.pathname.endsWith("/docgraph/doc")) return route.continue();
    const subject = url.searchParams.get("subject");
    if (request.method() === "PUT") {
      state.putCount += 1;
      const payload = JSON.parse(request.postData() ?? "{}");
      state.putPayloads.push(payload);
      if (saveMode === "conflict") {
        return route.fulfill({
          status: 409,
          contentType: "application/json",
          body: JSON.stringify({
            error: "The document changed on disk.",
            code: "DOC_SAVE_CONFLICT",
            current_rev: "rev-2",
          }),
        });
      }

      // Deliberately normalize the draft response to prove the editor adopts
      // server truth and does not schedule another autosave.
      state.current = {
        ...state.current,
        status: "active",
        header: { status: "active", keywords: ["normalized"] },
        rev: "rev-2",
      };
      return route.fulfill({ json: { ...state.current, warnings: [] } });
    }

    if (subject === "overview") return route.fulfill({ json: state.latest ?? state.current });
    if (subject === "target-doc") return route.fulfill({ json: targetDoc() });
    return route.fulfill({ status: 404, json: { error: "document not found" } });
  });

  return state;
}

async function openDocument(page, subject = "overview") {
  const response = await page.goto(`${baseUrl}/p/${projectId}/knowledge/${subject}`, {
    waitUntil: "domcontentloaded",
  });
  assert.equal(response?.status(), 200, `document route should load (${subject})`);
  await page.locator('main[aria-label="Document reader"] .tk-prose').waitFor({ state: "visible", timeout: 15_000 });
  await page.locator("h1").first().waitFor({ state: "visible", timeout: 15_000 });
  await page.waitForTimeout(2_200);
}

async function withPage(browser, viewport, fixture, callback) {
  const context = await browser.newContext({ viewport });
  const page = await context.newPage();
  const state = await installFixture(page, fixture);
  try {
    await callback(page, state);
  } finally {
    await context.close();
  }
}

async function testDetailsKeyboardAndMermaid(browser) {
  await withPage(browser, { width: 1_440, height: 1_000 }, {}, async (page) => {
    await openDocument(page);

    const details = page.locator("details").filter({ has: page.getByText("Details", { exact: true }) });
    assert.equal(await details.count(), 1, "front matter has one Details disclosure");
    assert.equal(await details.getAttribute("open"), null, "Details starts closed");
    await details.locator("summary").click();
    assert.equal(await details.getAttribute("open"), "", "Details opens on activation");

    const mermaids = page.locator('.tk-mermaid');
    await page.locator('.tk-mermaid[data-status="ready"]').waitFor({ state: "visible", timeout: 15_000 });
    await page.locator(".tk-mermaid-fallback").waitFor({ state: "visible", timeout: 15_000 });
    assert.equal(await mermaids.count(), 2, "valid and invalid Mermaid blocks remain visible");

    const folder = page.getByRole("button", { name: "Collapse docs/system" });
    assert.equal(await folder.getAttribute("aria-expanded"), "true", "folder starts expanded");
    await folder.press("Enter");
    assert.equal(await page.getByRole("button", { name: "Expand docs/system" }).getAttribute("aria-expanded"), "false", "folder toggles by keyboard");
  });
}

async function testMobileTrapAndRestore(browser) {
  await withPage(browser, { width: 390, height: 844 }, {}, async (page) => {
    await openDocument(page);
    const opener = page.getByRole("button", { name: "Toggle docs explorer" });
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "Documents explorer" });
    await dialog.waitFor({ state: "visible" });
    await page.waitForFunction(() => document.activeElement?.getAttribute("role") === "dialog");
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("role")), "dialog", "drawer receives focus");

    await page.keyboard.press("Shift+Tab");
    assert.equal(
      await page.evaluate(() => document.querySelector('[role="dialog"]')?.contains(document.activeElement)),
      true,
      "reverse Tab from initial dialog focus stays inside the drawer",
    );
    await page.keyboard.press("Tab");
    assert.equal(
      await page.evaluate(() => document.querySelector('[role="dialog"]')?.contains(document.activeElement)),
      true,
      "Tab stays inside the drawer",
    );
    await page.keyboard.press("Shift+Tab");
    assert.equal(
      await page.evaluate(() => document.querySelector('[role="dialog"]')?.contains(document.activeElement)),
      true,
      "reverse Tab stays inside the drawer",
    );

    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("aria-label")), "Toggle docs explorer", "Escape restores focus to opener");

    await opener.click();
    await dialog.waitFor({ state: "visible" });
    await dialog.getByRole("link", { name: "Open Target doc" }).click();
    await dialog.waitFor({ state: "hidden" });
    await page.getByRole("heading", { name: "Target doc" }).waitFor({ state: "visible", timeout: 15_000 });
    await page.waitForFunction(() => document.activeElement?.getAttribute("aria-label") === "Toggle docs explorer");
    assert.equal(await page.evaluate(() => document.activeElement?.getAttribute("aria-label")), "Toggle docs explorer", "file selection restores focus to opener");
    const details = page.locator('section[aria-label="Document details"] summary');
    await details.focus();
    await page.waitForTimeout(150);
    assert.equal(await details.evaluate((element) => document.activeElement === element), true, "restoration stops after the user focuses another control");
  });
}

async function testTreePersistenceAndLinkNavigation(browser) {
  await withPage(browser, { width: 1_440, height: 1_000 }, {}, async (page) => {
    await openDocument(page);
    const folder = page.getByRole("button", { name: "Collapse .tusker/specs" });
    await folder.click();
    assert.equal(await page.getByRole("button", { name: "Expand .tusker/specs" }).getAttribute("aria-expanded"), "false", "second folder collapses");
    await page.reload({ waitUntil: "domcontentloaded" });
    await page.locator('main[aria-label="Document reader"] .tk-prose').waitFor({ state: "visible", timeout: 15_000 });
    assert.equal(
      await page.getByRole("button", { name: "Expand .tusker/specs" }).getAttribute("aria-expanded"),
      "false",
      "collapsed folder survives reload",
    );
    assert.equal(await page.getByRole("link", { name: "Open Overview" }).getAttribute("aria-current"), "page", "selected document stays marked");

    await page.locator('[data-kg-wikilink][data-kg-resolved="true"]').click();
    await page.locator('main[aria-label="Document reader"] .tk-prose').waitFor({ state: "visible", timeout: 15_000 });
    await page.getByRole("heading", { name: "Target doc" }).waitFor({ state: "visible", timeout: 15_000 });
    assert.match(page.url(), /\/knowledge\/target-doc$/, "wiki-link navigates to target document");
  });
}

async function testMetadataSaveNormalization(browser) {
  await withPage(browser, { width: 1_440, height: 1_000 }, {}, async (page, state) => {
    await openDocument(page);
    const details = page.locator("details").filter({ has: page.getByText("Details", { exact: true }) });
    await details.locator("summary").click();
    await details.locator("select").first().selectOption("draft");
    const saveButton = page.getByRole("button", { name: "Save", exact: true });
    await saveButton.click();
    await page.getByText("Saved · written to disk.").waitFor({ state: "visible", timeout: 10_000 });
    assert.equal(state.putCount, 1, "metadata save sends one PUT");
    assert.equal(await details.locator("select").first().inputValue(), "active", "server-normalized status is adopted");
    assert.match(await details.textContent(), /normalized/, "server-normalized keyword is adopted");
    await page.waitForTimeout(2_000);
    assert.equal(state.putCount, 1, "normalized clean state does not autosave again");
  });
}

async function testConflictRetainsDraft(browser) {
  await withPage(browser, { width: 1_440, height: 1_000 }, { saveMode: "conflict" }, async (page, state) => {
    await openDocument(page);
    const details = page.locator("details").filter({ has: page.getByText("Details", { exact: true }) });
    await details.locator("summary").click();
    await details.locator("select").first().selectOption("draft");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await page.getByText("This document changed on disk").waitFor({ state: "visible", timeout: 10_000 });
    assert.equal(state.putCount, 1, "conflict save sends one PUT");
    assert.equal(await details.locator("select").first().inputValue(), "draft", "conflict retains the unsaved draft");
    await page.waitForTimeout(2_000);
    assert.equal(state.putCount, 1, "conflict suspends autosave");
  });
}

const browser = await chromium.launch({ channel: "chrome", headless: true });
try {
  await testDetailsKeyboardAndMermaid(browser);
  console.log("PASS Details, keyboard folder toggle, and Mermaid valid/error rendering");
  await testMobileTrapAndRestore(browser);
  console.log("PASS mobile dialog focus trap, Escape close, and focus restoration");
  await testTreePersistenceAndLinkNavigation(browser);
  console.log("PASS tree persistence, selected state, and wiki-link navigation");
  await testMetadataSaveNormalization(browser);
  console.log("PASS metadata save, server normalization, and autosave settling");
  await testConflictRetainsDraft(browser);
  console.log("PASS CAS conflict retains draft and suspends autosave");
} finally {
  await browser.close();
}
