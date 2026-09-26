import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  ancestorFolderIds,
  buildDocTree,
  contentsOf,
  docMatches,
} from "../src/features/knowledge/tree";
import {
  readTreeState,
  TREE_STORAGE_KEY,
  writeTreeState,
  type TreeStorageLike,
} from "../src/features/knowledge/treeStore";
import type { DocgraphDoc } from "../src/features/knowledge/types";

function memoryStorage(initial: Record<string, string> = {}): TreeStorageLike {
  const values = new Map(Object.entries(initial));
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
  };
}

const docs: DocgraphDoc[] = [
  {
    subject: "overview",
    title: "System overview",
    path: "docs/system/00-overview.md",
    kind: "canonical",
    status: "canonical",
    keywords: [],
  },
  {
    subject: "long-doc",
    title: "A document with a very long title that remains discoverable",
    path: "docs/system/a-document-with-a-very-long-name-that-truncates.md",
    kind: "spec",
    status: "draft",
    keywords: ["navigation"],
  },
  {
    subject: "decision",
    title: "Decision log",
    path: ".tusker/specs/decisions/2026-09-07-choice.md",
    kind: "decision",
    status: "accepted",
    keywords: [],
  },
];

describe("Documents tree behavior", () => {
  test("builds a real filesystem tree and preserves ancestor identity", () => {
    const tree = buildDocTree(docs);
    expect(tree.map((node) => node.type === "folder" ? node.name : node.path)).toEqual([
      "docs/system",
      ".tusker/specs/decisions",
    ]);
    expect(ancestorFolderIds(docs[0]!.path)).toEqual(["docs", "docs/system"]);
  });

  test("filters by filename, subject and title without rewriting paths", () => {
    expect(docMatches(docs[1]!, "long-name")).toBe(true);
    expect(docMatches(docs[1]!, "long-doc")).toBe(true);
    expect(docMatches(docs[1]!, "discoverable")).toBe(true);
    expect(docMatches(docs[0]!, "decision")).toBe(false);
    expect(buildDocTree(docs.filter((doc) => docMatches(doc, "decision")))[0]?.id).toBe(
      ".tusker/specs/decisions",
    );
  });
});

describe("Documents explorer persistence", () => {
  test("stores collapsed folders and rail state in a bounded JSON record", () => {
    const storage = memoryStorage();
    expect(writeTreeState(storage, { collapsedIds: ["p:docs/system"], railOpen: true })).toBe(true);
    expect(storage.getItem(TREE_STORAGE_KEY)).toContain("p:docs/system");
    expect(readTreeState(storage)).toEqual({ collapsedIds: ["p:docs/system"], railOpen: true });
  });

  test("malformed or unavailable storage falls back to an open explorer", () => {
    expect(readTreeState(memoryStorage({ [TREE_STORAGE_KEY]: "{broken" }))).toEqual({
      collapsedIds: [],
      railOpen: false,
    });
    expect(readTreeState(null)).toEqual({ collapsedIds: [], railOpen: false });
    expect(writeTreeState(null, { collapsedIds: [], railOpen: false })).toBe(false);
  });
});

describe("Documents static wiring guardrails", () => {
  const treeSource = readFileSync(new URL("../src/features/knowledge/KnowledgeTree.tsx", import.meta.url), "utf8");
  const shellSource = readFileSync(new URL("../src/features/knowledge/KnowledgeShell.tsx", import.meta.url), "utf8");
  const headerSource = readFileSync(new URL("../src/features/knowledge/HeaderCard.tsx", import.meta.url), "utf8");
  const readerSource = readFileSync(new URL("../src/features/knowledge/KnowledgeReader.tsx", import.meta.url), "utf8");
  const extensionsSource = readFileSync(new URL("../src/features/knowledge/editorExtensions.ts", import.meta.url), "utf8");
  const editorHookSource = readFileSync(new URL("../src/features/knowledge/useDocgraphEditor.ts", import.meta.url), "utf8");

  test("tree source keeps selected, expandable and failed-state wiring", () => {
    expect(treeSource).toContain('aria-current={active ? "page" : undefined}');
    expect(treeSource).toContain("aria-expanded={expanded}");
    expect(treeSource).toContain("Documents unavailable");
    expect(treeSource).toContain('aria-label="Filter documents"');
  });

  test("shell source keeps narrow explorer dialog wiring", () => {
    expect(shellSource).toContain('event.key === "Escape"');
    expect(shellSource).toContain('role="dialog"');
    expect(shellSource).toContain('aria-label="Documents explorer"');
  });

  test("reader source keeps front matter behind Details", () => {
    expect(headerSource).toContain('aria-label="Document details"');
    expect(headerSource).toContain("<details");
    expect(headerSource).toContain(">Details</span>");
    expect(headerSource).toContain("<StatusEditor");
    expect(headerSource).toContain("<KeywordsEditor");
  });

  test("reader source keeps links, CAS editor and shared Mermaid renderer connected", () => {
    expect(readerSource).toContain("OutgoingLinks");
    expect(readerSource).toContain("Backlinks");
    expect(readerSource).toContain("DocBodyEditor");
    expect(editorHookSource).toContain("base_rev");
    expect(extensionsSource).toContain("CodeBlockWithMermaid");
    expect(extensionsSource).toContain("codeBlock: false");
  });
});

describe("contentsOf", () => {
  test("numbers h2/h3 headings and skips the title and fenced code", () => {
    const body = "# Title\n\n## Summary\n\n## Architecture ##\n\n### Lease broker\n\n```md\n## not a heading\n```\n\n### Fan-out\n\n## Failure modes\n";
    expect(contentsOf(body)).toEqual([
      { level: 2, number: "1", text: "Summary" },
      { level: 2, number: "2", text: "Architecture" },
      { level: 3, number: "2.1", text: "Lease broker" },
      { level: 3, number: "2.2", text: "Fan-out" },
      { level: 2, number: "3", text: "Failure modes" },
    ]);
  });

  test("drops authored heading numbers but keeps leading years", () => {
    const body = "## 1. Why this exists\n\n### 1.2 Detail\n\n## 2026 roadmap\n";
    expect(contentsOf(body).map((e) => e.text)).toEqual(["Why this exists", "Detail", "2026 roadmap"]);
  });
});
