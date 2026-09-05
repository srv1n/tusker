import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { docGraphMermaid } from "../src/features/knowledge/graphMermaid";
import type { DocgraphEdge, DocgraphNode } from "../src/features/knowledge/types";

const nodes: DocgraphNode[] = [
  { subject: "system", title: "System <script>alert(1)</script>", kind: "canonical", path: "docs/system.md", status: "current" },
  { subject: "spec", title: "Trust \"flow\"", kind: "spec", path: "docs/spec.md", status: "current" },
  { subject: "decision", title: "Decision", kind: "decision", path: "docs/decision.md", status: "current" },
];

const edges: DocgraphEdge[] = [
  { from: "system", to: "spec", kind: "part_of" },
  { from: "spec", to: "decision", kind: "updates" },
  { from: "decision", to: "spec", kind: "source" },
  { from: "decision", to: "system", kind: "decides_for" },
  { from: "system", to: "decision", kind: "superseded_by" },
  { from: "spec", to: "system", kind: "link" },
  { from: "missing", to: "system", kind: "updates" },
];

test("Mermaid graph preserves every authoritative edge kind and ignores unknown endpoints", () => {
  const source = docGraphMermaid(nodes, edges);

  expect(source).toContain("flowchart TD");
  expect(source).toContain("part of");
  expect(source).toContain("updates");
  expect(source).toContain("source");
  expect(source).toContain("decides for");
  expect(source).toContain("superseded by");
  expect(source).toContain("link");
  expect(source).not.toContain("missing");
  expect(source).not.toContain("<script>");
  expect(source).toContain("System ‹script›alert(1)‹/script›");
});

test("graph UI exposes Mermaid fallback, unresolved notices, and keyboard navigation", () => {
  const graph = readFileSync(new URL("../src/features/knowledge/KnowledgeGraph.tsx", import.meta.url), "utf8");
  const reader = readFileSync(new URL("../src/features/knowledge/KnowledgeReader.tsx", import.meta.url), "utf8");
  const renderer = readFileSync(new URL("../src/features/editor/mermaid.ts", import.meta.url), "utf8");
  const sanitizer = readFileSync(new URL("../src/features/editor/sanitize.ts", import.meta.url), "utf8");

  expect(graph).toContain("renderMermaid(source)");
  expect(graph).toContain("Show Mermaid source");
  expect(graph).toContain('aria-label={`Open ${n.title}, ${meta.label}`}');
  expect(graph).toContain('event.key === "Enter" || event.key === " "');
  expect(graph).toContain("data.issues");
  expect(graph).toContain('source: { color: "var(--k-info)"');
  expect(graph).toContain('link: { color: "var(--k-accent)"');
  expect(reader).toContain("data-kg-unresolved");
  expect(reader).toContain("Document links");
  expect(reader).toContain("Referenced by");
  expect(reader).toContain('"source", "decides_for"');
  expect(reader).toContain('"superseded_by", "link", "wiki"');
  expect(renderer).toContain('securityLevel: "strict"');
  expect(renderer).toContain("sanitizeMermaidSvg");
  expect(sanitizer).toContain('"foreignObject"');
  expect(sanitizer).toContain('"xlink:href"');
});
