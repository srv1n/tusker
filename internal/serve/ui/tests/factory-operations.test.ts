import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
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
const visualFixture = readFileSync(new URL("./fixtures/factory-operations.svg", import.meta.url), "utf8");

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
      configured: "armed_waves",
      effective: "armed_waves",
      provenance: "project config",
    },
    completionMode: {
      configured: "authoritative",
      effective: "authoritative",
      provenance: "project config",
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
    operation("APP-T-0002", "promoted", { revisions: { defaultRef: "main", defaultSha: "promoted2" } }),
  ],
  workingNow: [operation("APP-T-0003", "running")],
  reviewOrRework: [
    operation("APP-T-0004", "in_review"),
    operation("APP-T-0005", "rework"),
  ],
  blocked: [
    operation("APP-T-0006", "disarmed"),
    operation("APP-T-0007", "stale_authorization"),
    operation("APP-T-0008", "waiting_resource"),
    operation("APP-T-0009", "parked"),
  ],
  needsYourDecision: [
    {
      gateId: "APP-G-0001",
      owner: "human:product",
      action: "Choose the customer-visible retention policy.",
      verification: "The decision record names the selected policy.",
      whyHuman: "Only the accountable product owner can resolve the requirement conflict.",
      affectedTaskIds: ["APP-T-0010", "APP-T-0011"],
      automaticNextAction: "Tusker re-evaluates the affected closure after the gate changes.",
      safeAction: "Choose the customer-visible retention policy.",
      href: "/p/app/docs?path=APP-T-0010&gate=APP-G-0001",
    },
  ],
  nextFrontier: [
    operation("APP-T-0012", "idle"),
    operation("APP-T-0013", "waiting_capacity"),
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
    "human:product",
    "integrated",
    "promoted",
  ]) {
    expect(html).toContain(state);
  }
  expect(html).toContain("Accepted factory surface");
  expect(html).toContain("integration/W-0001");
  expect(html).toContain("tusker wave preflight W-0001 --json");
  expect(html).toContain("Choose the customer-visible retention policy.");
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

test("wide and narrow layouts remain semantic, wrapping, and visually inspectable", () => {
  for (const layoutContract of [
    "grid-cols-1",
    "sm:grid-cols-2",
    "xl:grid-cols-2",
    "min-w-0",
    "break-words",
    'aria-labelledby="factory-operations-title"',
  ]) {
    expect(source).toContain(layoutContract);
  }
  for (const fixtureText of [
    "Factory operations · wide · 1440 px",
    "Factory operations · narrow · 390 px",
    "Delivered",
    "Working now",
    "In review or rework",
    "Blocked",
    "Needs your decision",
    "Next frontier",
    "disabled · idle",
    "disarmed · stale · parked",
    "human gate",
    "integrated · promoted",
  ]) {
    expect(visualFixture).toContain(fixtureText);
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
});
