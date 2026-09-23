import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { ConformanceChip, KIND_ORDER, kindMeta, truthfulState } from "../src/features/knowledge/bits";
import { ancestorFolderIds, buildDocTree } from "../src/features/knowledge/tree";
import { CONFORMANCE_VALUES, LIFECYCLE_BY_KIND, type DocgraphDoc } from "../src/features/knowledge/types";

function doc(over: Partial<DocgraphDoc>): DocgraphDoc {
  return {
    subject: "s",
    title: "T",
    path: "docs/system/t.md",
    kind: "doc",
    status: "current",
    lifecycle: "current",
    code_conformance: "unverified",
    keywords: [],
    ...over,
  };
}

describe("S46 portable documents truthful status", () => {
  test("proposed, accepted-but-unverified, implemented-with-evidence, drift and superseded read differently", () => {
    const states = new Set([
      truthfulState("proposed", "unverified"),
      truthfulState("accepted", "unverified"),
      truthfulState("implemented", "matches"),
      truthfulState("current", "drift"),
      truthfulState("superseded", "unverified"),
    ]);
    expect(states.size).toBe(5);
  });

  test("acceptance never reads as verification", () => {
    expect(truthfulState("accepted", "unverified")).toContain("unverified");
    expect(truthfulState("accepted", "unverified")).not.toContain("verified · ");
    expect(truthfulState("accepted", "unverified")).not.toBe(truthfulState("implemented", "matches"));
  });

  test("lifecycle values are kind-specific", () => {
    expect(LIFECYCLE_BY_KIND.doc).toEqual(["current", "superseded"]);
    expect(LIFECYCLE_BY_KIND.proposal).toContain("implemented");
    expect(LIFECYCLE_BY_KIND.decision).not.toContain("implemented");
    expect(LIFECYCLE_BY_KIND.proposal).toContain("accepted");
    expect(LIFECYCLE_BY_KIND.decision).toContain("accepted");
  });

  test("conformance stays independent of lifecycle", () => {
    expect(CONFORMANCE_VALUES).toEqual(["unverified", "matches", "drift", "not_applicable"]);
  });

  test("portable and legacy kinds share one tree presentation", () => {
    for (const kind of ["doc", "proposal", "decision", "canonical", "spec"] as const) {
      expect(kindMeta[kind].label).not.toBe("");
      expect(kindMeta[kind].cssVar).toMatch(/^--k-/);
    }
    expect(kindMeta.doc.cssVar).toBe(kindMeta.canonical.cssVar);
    expect(kindMeta.proposal.cssVar).toBe(kindMeta.spec.cssVar);
    expect(KIND_ORDER).toEqual(["doc", "proposal", "decision"]);
  });

  test("ConformanceChip is exported for the reader header", () => {
    expect(typeof ConformanceChip).toBe("function");
  });
});

describe("S46 portable tree and moved links", () => {
  const docs: DocgraphDoc[] = [
    doc({ subject: "overview", title: "Overview", path: "docs/system/00-overview.md", kind: "doc", status: "current", lifecycle: "current" }),
    doc({ subject: "billing-index", title: "Billing", path: "docs/system/domains/billing/00-index.md", kind: "doc", status: "current", lifecycle: "current" }),
    doc({ subject: "checkout", title: "Checkout", path: "docs/system/proposals/checkout.md", kind: "proposal", status: "accepted", lifecycle: "accepted" }),
  ];

  test("domain indexes stay browsable in the real folder tree", () => {
    const tree = buildDocTree(docs);
    const folders: string[] = [];
    const walk = (nodes: ReturnType<typeof buildDocTree>): void => {
      for (const n of nodes) {
        if (n.type === "folder") {
          folders.push(n.name);
          walk(n.children);
        }
      }
    };
    walk(tree);
    expect(folders.some((f) => f.includes("domains/billing") || f === "billing")).toBe(true);
  });

  test("selected ancestors expand after a move to the portable path", () => {
    expect(ancestorFolderIds("docs/system/proposals/checkout.md")).toEqual([
      "docs",
      "docs/system",
      "docs/system/proposals",
    ]);
  });
});

describe("S46 Documents static wiring guardrails", () => {
  const readerSource = readFileSync(new URL("../src/features/knowledge/KnowledgeReader.tsx", import.meta.url), "utf8");
  const headerSource = readFileSync(new URL("../src/features/knowledge/HeaderCard.tsx", import.meta.url), "utf8");
  const editorHookSource = readFileSync(new URL("../src/features/knowledge/useDocgraphEditor.ts", import.meta.url), "utf8");
  const listSource = readFileSync(new URL("../src/features/knowledge/KnowledgeList.tsx", import.meta.url), "utf8");
  const graphSource = readFileSync(new URL("../src/features/knowledge/KnowledgeGraph.tsx", import.meta.url), "utf8");

  test("reader separates conformance from lifecycle and honors moved links", () => {
    expect(readerSource).toContain("TruthfulStatus");
    expect(readerSource).toContain('aria-label="Document status"');
    expect(readerSource).toContain("resolved_from");
    expect(readerSource).toContain("ConflictNotice");
    expect(readerSource).toContain("DocBodyEditor");
  });

  test("header edits kind-specific lifecycle plus independent conformance", () => {
    expect(headerSource).toContain("LIFECYCLE_BY_KIND");
    expect(headerSource).toContain("ConformanceEditor");
    expect(headerSource).toContain('aria-label="Last verified stamp"');
    expect(headerSource).toContain("<ConformanceChip");
  });

  test("editor sends new metadata with the pinned optimistic-concurrency rev", () => {
    expect(editorHookSource).toContain("code_conformance");
    expect(editorHookSource).toContain("last_verified");
    expect(editorHookSource).toContain("base_rev");
    expect(editorHookSource).toContain('type: "conflict"');
  });

  test("empty states describe the portable tree, not the legacy roots", () => {
    expect(listSource).toContain("docs/system");
    expect(graphSource).toContain("portable tree");
  });
});
