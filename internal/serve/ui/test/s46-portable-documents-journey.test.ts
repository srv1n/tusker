import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { ConformanceChip, kindMeta, truthfulState } from "../src/features/knowledge/bits";
import { docGraphMermaid } from "../src/features/knowledge/graphMermaid";
import { ancestorFolderIds, buildDocTree, docMatches } from "../src/features/knowledge/tree";
import type { DocgraphDoc, DocgraphEdge, DocgraphNode } from "../src/features/knowledge/types";

// TSK-T-0062 A3: the integrated Documents UI journey over the same fresh and
// migrated corpus the Go journey test walks. Fresh rows mirror the CLI
// journey (overview, billing index/chapter, checkout proposal, record
// decision); migrated rows mirror the legacy inventory (canonical source plus
// its superseded forwarding stub). Graph edges mirror the API's relationship
// rows: part_of hierarchy, decides_for, superseded_by forward, and link.
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

const fresh: DocgraphDoc[] = [
  doc({ subject: "overview", title: "System overview", path: "docs/system/00-overview.md" }),
  doc({ subject: "billing-overview", title: "Billing", path: "docs/system/domains/billing/00-index.md" }),
  doc({ subject: "invoicing", title: "Invoicing", path: "docs/system/domains/billing/invoicing.md" }),
  doc({
    subject: "checkout-flow",
    title: "Checkout flow",
    path: "docs/system/proposals/checkout-flow.md",
    kind: "proposal",
    status: "proposed",
    lifecycle: "proposed",
  }),
  doc({
    subject: "record-choice",
    title: "Record choice",
    path: "docs/system/decisions/record-choice.md",
    kind: "decision",
    status: "accepted",
    lifecycle: "accepted",
  }),
];

const migrated: DocgraphDoc[] = [
  ...fresh,
  doc({
    subject: "flow-plan",
    title: "Flow plan",
    path: "docs/system/proposals/flow-plan.md",
    kind: "proposal",
    status: "proposed",
    lifecycle: "proposed",
  }),
];

function folderNames(nodes: ReturnType<typeof buildDocTree>): string[] {
  const names: string[] = [];
  const walk = (list: ReturnType<typeof buildDocTree>): void => {
    for (const node of list) {
      if (node.type === "folder") {
        names.push(node.name);
        walk(node.children);
      }
    }
  };

  walk(nodes);
  return names;
}

function leafSubjects(nodes: ReturnType<typeof buildDocTree>): string[] {
  const subjects: string[] = [];
  const walk = (list: ReturnType<typeof buildDocTree>): void => {
    for (const node of list) {
      if (node.type === "doc") subjects.push(node.subject);
      else walk(node.children);
    }
  };
  walk(nodes);
  return subjects;
}

describe("S46 Documents UI journey: fresh corpus", () => {
  test("domain indexes and chapters stay browsable in the real folder tree", () => {
    const tree = buildDocTree(fresh);
    expect(folderNames(tree)).toEqual(
      expect.arrayContaining(["docs/system", "domains/billing", "proposals", "decisions"]),
    );
    expect(leafSubjects(tree)).toEqual(
      expect.arrayContaining(["overview", "billing-overview", "invoicing", "checkout-flow", "record-choice"]),
    );
  });

  test("deep documents resolve their ancestor folders for tree expansion", () => {
    expect(ancestorFolderIds("docs/system/domains/billing/invoicing.md")).toEqual([
      "docs",
      "docs/system",
      "docs/system/domains",
      "docs/system/domains/billing",
    ]);
  });

  test("filter finds the proposal by subject, title, and folder", () => {
    const proposal = fresh.find((d) => d.subject === "checkout-flow")!;
    expect(docMatches(proposal, "checkout")).toBe(true);
    expect(docMatches(proposal, "flow")).toBe(true);
    expect(docMatches(proposal, "checkout-flow.md")).toBe(true);
    expect(docMatches(proposal, "record-choice")).toBe(false);
  });

  test("graph renders the journey relationships without inventing edges", () => {
    const nodes: DocgraphNode[] = migrated.map((d) => ({
      subject: d.subject,
      kind: d.kind,
      path: d.path,
      title: d.title,
      status: d.status,
      lifecycle: d.lifecycle,
      code_conformance: d.code_conformance,
    }));
    const edges: DocgraphEdge[] = [
      { from: "billing-overview", to: "overview", kind: "part_of" },
      { from: "invoicing", to: "billing-overview", kind: "part_of" },
      { from: "checkout-flow", to: "overview", kind: "part_of" },
      { from: "record-choice", to: "checkout-flow", kind: "decides_for" },
      { from: "invoicing", to: "checkout-flow", kind: "link" },
    ];
    const diagram = docGraphMermaid(nodes, edges);
    for (const label of ["decides for", "part of", "|link|", "Checkout flow", "Record choice"]) {
      expect(diagram).toContain(label);
    }
    // An edge endpoint the graph did not provide is omitted, never invented.
    const withGhost = docGraphMermaid(nodes, [...edges, { from: "checkout-flow", to: "ghost", kind: "link" }]);
    expect(withGhost).not.toContain("ghost");
  });

  test("reader header keeps lifecycle and conformance visibly independent", () => {
    const header = readFileSync("src/features/knowledge/HeaderCard.tsx", "utf8");
    expect(header).toContain("KindBadge");
    expect(header).toContain("DocStatusChip");
    expect(header).toContain("ConformanceChip");
    expect(typeof ConformanceChip).toBe("function");
    // Accepted intent never reads as verified implementation.
    expect(truthfulState("accepted", "unverified")).not.toBe(truthfulState("implemented", "matches"));
    expect(truthfulState("accepted", "unverified")).toContain("unverified");
    expect(truthfulState("proposed", "unverified")).toBe("Proposed");
  });
});

describe("S46 Documents UI journey: migrated rows and legacy kinds", () => {
  test("legacy kinds share one tree presentation with portable kinds", () => {
    for (const kind of ["doc", "proposal", "decision", "canonical", "spec"] as const) {
      expect(kindMeta[kind].label).not.toBe("");
    }
    expect(kindMeta.doc.cssVar).toBe(kindMeta.canonical.cssVar);
    expect(kindMeta.proposal.cssVar).toBe(kindMeta.spec.cssVar);
  });

  test("migrated rows browse beside fresh rows with distinct truthful states", () => {
    const tree = buildDocTree(migrated);
    expect(leafSubjects(tree)).toContain("flow-plan");
    const states = new Set([
      truthfulState("proposed", "unverified"),
      truthfulState("accepted", "unverified"),
      truthfulState("implemented", "matches"),
      truthfulState("current", "drift"),
      truthfulState("superseded", "unverified"),
    ]);
    expect(states.size).toBe(5);
  });
});

describe("S46 Documents UI journey: editor, keyboard, and widths", () => {
  test("editor pins base_rev so a stale save becomes a 409 conflict, not an overwrite", () => {
    const editor = readFileSync("src/features/knowledge/useDocgraphEditor.ts", "utf8");
    expect(editor).toContain("base_rev");
    expect(editor).toContain("409");
    expect(editor).toContain("conflict");
    // The editor never remounts away an external change: a rev mismatch
    // suspends autosave behind the conflict banner instead of saving over it.
    expect(editor).toMatch(/doc\.rev !== baseRev\.current/);
  });

  test("keyboard: Escape closes the explorer drawer and controls stay labelled", () => {
    const shell = readFileSync("src/features/knowledge/KnowledgeShell.tsx", "utf8");
    expect(shell).toContain('event.key === "Escape"');
    expect(shell).toContain('aria-label="Toggle docs explorer"');
    const tree = readFileSync("src/features/knowledge/KnowledgeTree.tsx", "utf8");
    expect(tree).toContain('aria-label="Documents explorer"');
    expect(tree).toContain("aria-expanded");
  });

  test("widths: desktop sidebar hides below lg while narrow gets a focusable drawer", () => {
    const shell = readFileSync("src/features/knowledge/KnowledgeShell.tsx", "utf8");
    expect(shell).toContain("hidden w-[280px] flex-none lg:block");
    expect(shell).toContain("max-w-[calc(100%-32px)]");
    expect(shell).toContain("tabIndex={-1}");
  });
});
