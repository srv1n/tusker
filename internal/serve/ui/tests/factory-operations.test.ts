import { expect, test } from "bun:test";
import { existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { spawn } from "node:child_process";
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
        safeAction: "tusker wave preflight W-0001 --json",
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
  expect(html).toContain("tusker wave preflight W-0001 --json");
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
  try {
    await server.listen();
    const address = server.httpServer?.address();
    if (!address || typeof address === "string") throw new Error("Vite did not expose a TCP test address");
    const url = `http://127.0.0.1:${address.port}/`;
    for (const width of [390, 1440]) {
      const proof = await chromiumLayoutProof(url, width, fixtureRoot);
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
}, 30_000);

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

async function chromiumLayoutProof(url: string, width: number, fixtureRoot: string): Promise<BrowserLayoutProof> {
  const browser = [
    process.env.CHROME_BIN,
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
    "/Applications/Chromium.app/Contents/MacOS/Chromium",
    "/usr/bin/google-chrome",
    "/usr/bin/chromium",
  ].find((candidate): candidate is string => Boolean(candidate && existsSync(candidate)));
  if (!browser) throw new Error("Chromium is required for the factory operations viewport acceptance test");
  const screenshot = resolve(fixtureRoot, `factory-operations-${width}.png`);
  const profile = mkdtempSync(resolve(tmpdir(), `tusker-factory-chrome-${width}-`));
  const child = spawn(browser, [
    "--headless=new",
    "--no-sandbox",
    "--disable-gpu",
    "--hide-scrollbars",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "about:blank",
  ], { stdio: ["ignore", "pipe", "pipe"] });
  let stderr = "";
  child.stderr.setEncoding("utf8");
  child.stderr.on("data", (chunk) => { stderr += chunk; });
  try {
    const activePortFile = resolve(profile, "DevToolsActivePort");
    for (let attempt = 0; attempt < 100 && !existsSync(activePortFile); attempt++) {
      if (child.exitCode !== null) throw new Error(`Chromium exited before DevTools was ready: ${stderr}`);
      await delayMs(50);
    }
    if (!existsSync(activePortFile)) throw new Error(`Chromium DevTools did not start: ${stderr}`);
    const port = Number(readFileSync(activePortFile, "utf8").split(/\r?\n/)[0]);
    const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) => response.json()) as Array<{ type: string; webSocketDebuggerUrl: string }>;
    const page = targets.find((target) => target.type === "page");
    if (!page) throw new Error("Chromium exposed no page target");
    const cdp = await connectCDP(page.webSocketDebuggerUrl);
    try {
      await cdp.call("Runtime.enable");
      await cdp.call("Page.enable");
      await cdp.call("Emulation.setDeviceMetricsOverride", {
        width,
        height: 1600,
        deviceScaleFactor: 1,
        mobile: false,
      });
      await cdp.call("Page.navigate", { url });
      let proofJSON = "";
      for (let attempt = 0; attempt < 100 && proofJSON === ""; attempt++) {
        await delayMs(50);
        const evaluated = await cdp.call("Runtime.evaluate", {
          expression: "document.getElementById('factory-proof')?.textContent ?? ''",
          returnByValue: true,
        }) as { result?: { value?: string } };
        proofJSON = evaluated.result?.value ?? "";
      }
      if (proofJSON === "") throw new Error(`Chromium ${width}px render omitted browser proof`);
      const captured = await cdp.call("Page.captureScreenshot", {
        format: "png",
        captureBeyondViewport: false,
      }) as { data?: string };
      if (!captured.data) throw new Error(`Chromium ${width}px render omitted screenshot data`);
      writeFileSync(screenshot, Buffer.from(captured.data, "base64"));
      if (statSync(screenshot).size === 0) throw new Error(`Chromium ${width}px screenshot was empty`);
      return JSON.parse(proofJSON) as BrowserLayoutProof;
    } finally {
      cdp.close();
    }
  } finally {
    child.kill("SIGTERM");
    if (child.exitCode === null) {
      const closed = await Promise.race([
        new Promise<boolean>((resolveExit) => child.once("close", () => resolveExit(true))),
        delayMs(2000).then(() => false),
      ]);
      if (!closed && child.exitCode === null) {
        child.kill("SIGKILL");
        await Promise.race([
          new Promise<void>((resolveExit) => child.once("close", () => resolveExit())),
          delayMs(2000),
        ]);
      }
    }
    rmSync(profile, { recursive: true, force: true });
  }
}

function delayMs(ms: number): Promise<void> {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, ms));
}

async function connectCDP(url: string): Promise<{
  call: (method: string, params?: Record<string, unknown>) => Promise<unknown>;
  close: () => void;
}> {
  const socket = new WebSocket(url);
  await new Promise<void>((resolveOpen, reject) => {
    socket.addEventListener("open", () => resolveOpen(), { once: true });
    socket.addEventListener("error", () => reject(new Error("Chromium DevTools WebSocket failed")), { once: true });
  });
  let sequence = 0;
  const pending = new Map<number, { resolve: (value: unknown) => void; reject: (error: Error) => void }>();
  socket.addEventListener("message", (event) => {
    const message = JSON.parse(String(event.data)) as {
      id?: number;
      result?: unknown;
      error?: { message?: string };
    };
    if (message.id === undefined) return;
    const waiter = pending.get(message.id);
    if (!waiter) return;
    pending.delete(message.id);
    if (message.error) waiter.reject(new Error(message.error.message ?? "Chromium DevTools command failed"));
    else waiter.resolve(message.result);
  });
  return {
    call(method, params = {}) {
      const id = ++sequence;
      return new Promise((resolveCall, rejectCall) => {
        pending.set(id, { resolve: resolveCall, reject: rejectCall });
        socket.send(JSON.stringify({ id, method, params }));
      });
    },
    close() {
      socket.close();
    },
  };
}
