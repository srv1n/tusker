import { expect, test } from "bun:test";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { createServer } from "vite";
import { FactoryOperationsSurface } from "../src/features/ops/FactoryOperations";
import { qk } from "../src/lib/queries";
import { streamKeyToQueryKeys } from "../src/lib/stream";
import type {
  FactoryOperationsItem,
  FactoryOperationsProjection,
} from "../src/types/domain";

const source = readFileSync(new URL("../src/features/ops/FactoryOperations.tsx", import.meta.url), "utf8");
const projectOps = readFileSync(new URL("../src/features/ops/ProjectOps.tsx", import.meta.url), "utf8");
const api = readFileSync(new URL("../src/lib/api.ts", import.meta.url), "utf8");
const queries = readFileSync(new URL("../src/lib/queries.ts", import.meta.url), "utf8");
const stream = readFileSync(new URL("../src/lib/stream.ts", import.meta.url), "utf8");
const types = readFileSync(new URL("../src/types/domain.ts", import.meta.url), "utf8");

const operation = (id: string, state: string, overrides: Partial<FactoryOperationsItem> = {}): FactoryOperationsItem => ({
  id,
  kind: "task",
  taskId: id,
  waveId: "W-0001",
  title: `${state} product outcome`,
  state,
  productOutcome: `A customer can see the ${state} result.`,
  cause: state.includes("blocked") || state.includes("stale") || state.includes("parked")
    ? `Bounded ${state} cause.`
    : undefined,
  affectedTaskIds: [id],
  automaticNextAction: `Tusker automatically advances ${id} when its recorded condition changes.`,
  safeAction: `tusker show ${id} --capsule`,
  acceptedArtifacts: [],
  revisions: { stateRevision: `sha256:${id}`, workRevision: 2 },
  href: `/p/app/docs?path=${id}`,
  ...overrides,
});

const projection: FactoryOperationsProjection = {
  schema: "tusker.factory-operations/v1",
  readOnly: true,
  generatedAt: "2026-07-26T08:00:00Z",
  project: {
    id: "app",
    name: "App",
    registered: true,
    enabled: true,
    health: "healthy",
    automationEnabled: false,
    automationProvenance: "project config",
    dispatchScope: {
      effective: "all_eligible",
      provenance: "legacy enabled config without dispatch_scope",
      warning: "automation.dispatch_scope is absent on an enabled project; preserving legacy all_eligible authority",
      repair: "set automation.dispatch_scope: all_eligible to acknowledge legacy broad dispatch, or armed_waves to require an armed wave",
    },
    completionMode: {
      effective: "legacy",
      provenance: "legacy enabled config without completion_reactor.mode",
      warning: "automation.completion_reactor.mode is absent on an enabled project; preserving legacy completion authority",
      repair: "set automation.completion_reactor.mode: disabled, shadow, or authoritative",
    },
    promotionMode: {
      configured: true,
      mode: "promote",
      provenance: "project config",
      observe: true,
      stage: true,
      promote: true,
      release: false,
    },
  },
  authority: {
    defaultRef: "main",
    defaultSha: "0123456789abcdef",
    waves: [
      {
        waveId: "W-0001",
        title: "Factory wave",
        state: "stale",
        fingerprintHealth: "stale",
        currentFingerprint: "sha256:new",
        authorizedFingerprint: "sha256:old",
        integrationRef: "integration/W-0001",
        integrationSha: "fedcba9876543210",
        safeAction: "tusker wave start W-0001 --mode background --by human:$USER --json",
        href: "/p/app/ops#wave-W-0001",
      },
    ],
  },
  capacity: {
    global: { active: 2, limit: 2, available: 0 },
    project: { active: 1, limit: 2, available: 1 },
    resourceHolds: [
      { name: "gpu-a", purpose: "task dispatch APP-T-0008", projectId: "app", taskId: "APP-T-0008" },
    ],
  },
  sectionOrder: ["delivered", "workingNow", "reviewOrRework", "blocked", "needsYourDecision", "nextFrontier"],
  delivered: [
    operation("APP-T-0001", "integrated", {
      acceptedArtifacts: [{
        taskId: "APP-T-0001",
        taskHref: "/p/app/docs?path=APP-T-0001",
        kind: "screenshot",
        priority: 1,
        summary: "Accepted factory surface",
        acceptanceIds: ["A1"],
        evidenceRef: "APP-T-0001-E-0001",
        artifactRef: "artifacts/factory.png",
        evidenceHref: "/p/app/docs?path=APP-T-0001-E-0001",
      }],
      revisions: {
        stateRevision: "sha256:state1",
        workRevision: 2,
        implementationSha: "impl1",
        resultRevision: "review1",
        integrationRef: "integration/W-0001",
        integrationSha: "integrated1",
      },
    }),
    operation("APP-T-0002", "shadow_validated"),
    operation("APP-T-0003", "staged_only"),
    operation("APP-T-0004", "promotion_committed", { revisions: { defaultRef: "main", defaultSha: "promoted4" } }),
  ],
  workingNow: [operation("APP-T-0005", "running")],
  reviewOrRework: [
    operation("APP-T-0006", "in_review"),
    operation("APP-T-0007", "rework"),
  ],
  blocked: [
    operation("APP-T-0008", "disarmed"),
    operation("APP-T-0009", "stale_authorization"),
    operation("APP-T-0010", "stale_run"),
    operation("APP-T-0011", "waiting_resource"),
    operation("APP-T-0012", "parked"),
  ],
  needsYourDecision: [
    {
      gateId: "APP-G-0001",
      owner: "human:product",
      action: "Choose the customer-visible retention policy.",
      verification: "The decision record names the selected policy.",
      whyHuman: "Only the accountable product owner can resolve the requirement conflict.",
      affectedTaskIds: ["APP-T-0013", "APP-T-0014"],
      automaticNextAction: "Tusker re-evaluates the affected closure after the gate changes.",
      safeAction: "Choose the customer-visible retention policy.",
      href: "/p/app/docs?path=APP-T-0010&gate=APP-G-0001",
    },
  ],
  nextFrontier: [
    operation("APP-T-0015", "idle"),
    operation("APP-T-0016", "waiting_capacity"),
  ],
};

test("one ordered operations projection renders the full product state matrix", () => {
  expect(projection.sectionOrder).toEqual([
    "delivered",
    "workingNow",
    "reviewOrRework",
    "blocked",
    "needsYourDecision",
    "nextFrontier",
  ]);
  const html = renderToStaticMarkup(createElement(FactoryOperationsSurface, { projection }));
  const headings = [
    "Delivered",
    "Working now",
    "In review or rework",
    "Blocked",
    "Needs your decision",
    "Next frontier",
  ];
  let cursor = -1;
  for (const heading of headings) {
    const next = html.indexOf(`>${heading}<`);
    expect(next).toBeGreaterThan(cursor);
    cursor = next;
  }
  for (const state of [
    "disabled",
    "disarmed",
    "stale authorization",
    "idle",
    "running",
    "in review",
    "rework",
    "parked",
    "stale run",
    "human:product",
    "integrated",
    "shadow validated",
    "staged only",
    "promotion committed",
  ]) {
    expect(html).toContain(state);
  }
  expect(html).toContain("Accepted factory surface");
  expect(html).toContain("integration/W-0001");
  expect(html).toContain("tusker wave start W-0001 --mode background --by human:$USER --json");
  expect(html).toContain("Choose the customer-visible retention policy.");
  expect(html).toContain("preserving legacy all_eligible authority");
  expect(html).toContain("set automation.dispatch_scope: all_eligible");
  expect(html).toContain("preserving legacy completion authority");
  expect(html).toContain("set automation.completion_reactor.mode");
});

test("Serve and desktop consume the real read-only seam without mutation controls", () => {
  expect(types).toContain('schema: "tusker.factory-operations/v1"');
  expect(api).toContain('real(withProject("/factory-operations", projectId))');
  expect(queries).toContain("useFactoryOperations");
  expect(queries).toContain("qk.factoryOperations(projectId)");
  expect(stream).toContain('case "factory-operations"');
  expect(streamKeyToQueryKeys("factory-operations", "app")).toEqual([qk.factoryOperations("app")]);
  expect(projectOps).toContain("<FactoryOperationsSurface projection={projection} />");
  expect(projectOps).toContain("useFactoryOperations(projectId)");
  expect(source).toContain("project.registered");
  expect(source).toContain("project.enabled");
  expect(source).toContain("project.automationEnabled");
  for (const mutationControl of ["<button", "onClick=", "useMutation", "post("]) {
    expect(source).not.toContain(mutationControl);
  }
  expect(source).toContain("<Mono");
  expect(source).toContain("item.safeAction");
  expect(source).toContain("decision.safeAction");
});

test("real Chromium render has no horizontal overflow and exposes an accessible 390px/1440px structure", async () => {
  const playwright = await import("/Users/sarav/.bun/install/global/node_modules/playwright/index.mjs").catch(() => undefined);
  if (!playwright) return;
  const uiRoot = resolve(import.meta.dir, "..");
  const fixtureRoot = mkdtempSync(resolve(uiRoot, ".factory-operations-browser-"));
  const projectionJSON = JSON.stringify(projection).replaceAll("<", "\\u003c");
  writeFileSync(resolve(fixtureRoot, "index.html"), `<!doctype html>
<html lang="en">
  <head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"><title>Factory operations browser proof</title></head>
  <body class="m-0 bg-surface p-0 text-ink"><main id="root" class="min-w-0 p-3 sm:p-6"></main><script>window.__PROJECTION__=${projectionJSON};</script><script type="module" src="/entry.tsx"></script></body>
</html>`);
  writeFileSync(resolve(fixtureRoot, "entry.tsx"), `
import React from "react";
import { createRoot } from "react-dom/client";
import { FactoryOperationsSurface } from "@/features/ops/FactoryOperations";
import "@/styles/app.css";

const projection = window.__PROJECTION__;
const root = document.getElementById("root");
createRoot(root).render(<FactoryOperationsSurface projection={projection} />);

requestAnimationFrame(() => requestAnimationFrame(() => {
  const surface = document.querySelector('section[aria-labelledby="factory-operations-title"]');
  const expected = ["Delivered", "Working now", "In review or rework", "Blocked", "Needs your decision", "Next frontier"];
  const sectionHeadings = [...surface.querySelectorAll("h3")].map((node) => node.textContent.trim());
  const ids = [...document.querySelectorAll("[id]")].map((node) => node.id);
  const labelledByIssues = [...surface.querySelectorAll("[aria-labelledby]")].flatMap((node) =>
    node.getAttribute("aria-labelledby").split(/\\s+/).filter((id) => !document.getElementById(id)).map((id) => "missing label " + id)
  );
  const linkIssues = [...surface.querySelectorAll("a")].flatMap((node) => {
    const issues = [];
    if (!node.textContent.trim() && !node.getAttribute("aria-label")) issues.push("unnamed link");
    if (!node.getAttribute("href")) issues.push("link without href");
    return issues;
  });
  const advisoryIssues = [...surface.querySelectorAll("aside")].flatMap((node) => {
    const issues = [];
    if (!node.getAttribute("aria-label")) issues.push("unnamed advisory");
    if (!node.textContent.includes("Repair:")) issues.push("advisory without repair");
    return issues;
  });
  const a11yIssues = [
    ...(surface ? [] : ["surface missing"]),
    ...(document.querySelector("#factory-operations-title")?.textContent.trim() === "Factory operations" ? [] : ["title missing"]),
    ...(JSON.stringify(sectionHeadings) === JSON.stringify(expected) ? [] : ["section heading order"]),
    ...labelledByIssues,
    ...linkIssues,
    ...advisoryIssues,
    ...(new Set(ids).size === ids.length ? [] : ["duplicate ids"]),
    ...(surface.querySelectorAll("button").length === 0 ? [] : ["unexpected mutation control"]),
  ];
  const proof = {
    viewport: window.innerWidth,
    documentWidth: document.documentElement.scrollWidth,
    bodyWidth: document.body.scrollWidth,
    surfaceWidth: Math.ceil(surface.getBoundingClientRect().width),
    horizontalOverflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    sectionHeadings,
    advisoryCount: surface.querySelectorAll("aside[aria-label$='compatibility warning']").length,
    a11yIssues,
  };
  const output = document.createElement("output");
  output.id = "factory-proof";
  output.textContent = JSON.stringify(proof);
  document.body.append(output);
}));
`);

  const server = await createServer({
    root: fixtureRoot,
    configFile: false,
    plugins: [react(), tailwindcss()],
    resolve: { alias: { "@": resolve(uiRoot, "src") } },
    server: { host: "127.0.0.1", port: 0, strictPort: false, fs: { allow: [uiRoot, fixtureRoot] } },
  });
  let browser: Awaited<ReturnType<typeof playwright.chromium.launch>> | undefined;
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string") throw new Error("Vite did not expose a TCP test address");
    const url = `http://127.0.0.1:${address.port}/`;
    browser = await playwright.chromium.launch({ channel: "chrome", headless: true });
    const page = await browser.newPage();
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 1600 });
      await page.goto(url);
      await page.locator("#factory-proof").waitFor();
      const proof = JSON.parse(await page.locator("#factory-proof").innerText()) as BrowserLayoutProof;
      expect(proof.viewport).toBe(width);
      expect(proof.horizontalOverflow).toBe(false);
      expect(proof.documentWidth).toBeLessThanOrEqual(width);
      expect(proof.bodyWidth).toBeLessThanOrEqual(width);
      expect(proof.surfaceWidth).toBeLessThanOrEqual(width);
      expect(proof.sectionHeadings).toEqual([
        "Delivered",
        "Working now",
        "In review or rework",
        "Blocked",
        "Needs your decision",
        "Next frontier",
      ]);
      expect(proof.advisoryCount).toBe(2);
      expect(proof.a11yIssues).toEqual([]);
    }
  } finally {
    await browser?.close();
    await server.close();
    rmSync(fixtureRoot, { recursive: true, force: true });
  }

  for (const exhaust of [
    "rawLogPath",
    "promptPath",
    "sessionRef",
    "lastHeartbeatAt",
    "tokenTotal",
    "transcript",
    "frontmatter",
  ]) {
    expect(source).not.toContain(exhaust);
  }
}, 60_000);

interface BrowserLayoutProof {
  viewport: number;
  documentWidth: number;
  bodyWidth: number;
  surfaceWidth: number;
  horizontalOverflow: boolean;
  sectionHeadings: string[];
  advisoryCount: number;
  a11yIssues: string[];
}
