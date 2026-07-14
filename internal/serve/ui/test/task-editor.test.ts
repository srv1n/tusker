import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import type { TaskDetail } from "../src/types/domain";
import {
  readMarkdownSection,
  replaceMarkdownSection,
  taskDetailToDocContent,
  taskDocPath,
  taskIdFromDocPath,
} from "../src/features/docs/taskMarkdown";

const task = {
  id: "SRV-T-1234",
  title: "Unified task editor",
  epicId: "SRV",
  epicTitle: "Serve",
  status: "ready",
  readiness: "ready",
  priority: "p1",
  risk: "medium",
  hasGate: false,
  updatedAt: "2026-07-07T00:00:00Z",
  intent: "Edit **task prose** in place.",
  acceptance: [{ id: "a1", text: "Intent uses DocEditor", proof: "pending" }],
  nonGoals: ["Do not autosave yet."],
  verification: [{ id: "v1", command: "bun test task-editor", result: "pending" }],
  evidence: [],
  knowledgeDelta: "DocReader is no longer the task edit detour.",
  deps: [],
  gates: [],
  runHistory: [],
} satisfies TaskDetail;

describe("task editor markdown sections", () => {
  test("maps task ids to raw task source paths", () => {
    expect(taskDocPath(task.id)).toBe(".tusker/work/tasks/SRV-T-1234.md");
    expect(taskIdFromDocPath(".tusker/work/tasks/SRV-T-1234.md")).toBe("SRV-T-1234");
    expect(taskIdFromDocPath("docs/specs/10-tusker-serve.md")).toBeNull();
  });

  test("builds a task doc fallback with prose sections", () => {
    const doc = taskDetailToDocContent(task);

    expect(doc.path).toBe(taskDocPath(task.id));
    expect(doc.kind).toBe("task");
    expect(readMarkdownSection(doc.markdown, "Intent")).toBe(task.intent);
    expect(readMarkdownSection(doc.markdown, "Knowledge delta")).toBe(task.knowledgeDelta);
  });

  test("replaces one prose section without damaging structured sections", () => {
    const original = taskDetailToDocContent(task).markdown;
    const next = replaceMarkdownSection(original, "Intent", "New in-place prose.");

    expect(readMarkdownSection(next, "Intent")).toBe("New in-place prose.");
    expect(next).toContain("## Acceptance");
    expect(next).toContain("| Intent uses DocEditor | pending |");
    expect(next).toContain("## Verification");
  });

  test("appends a missing prose section for later save through the same doc", () => {
    const next = replaceMarkdownSection("# Title\n\n## Intent\n\nBody", "Knowledge delta", "New canon.");

    expect(readMarkdownSection(next, "Intent")).toBe("Body");
    expect(readMarkdownSection(next, "Knowledge delta")).toBe("New canon.");
  });
});

describe("TaskContract editor surface", () => {
  const source = readFileSync("src/features/docs/TaskContract.tsx", "utf8");
  const documentView = readFileSync("src/features/docs/DocumentView.tsx", "utf8");

  test("uses DocEditor for prose and removes the raw textarea path", () => {
    expect(source).toContain("<DocEditor");
    expect(source).toContain("data-task-prose-section");
    expect(source).not.toContain("EditableProse");
    expect(source).not.toContain("<textarea");
  });

  test("starts editing from the prose click target, not an Edit button", () => {
    expect(source).toContain("onMouseDown={startFromPointer}");
    expect(source).toContain("ed.startEdit()");
    expect(source).not.toContain(">Edit<");
  });

  test("demotes the markdown route to read-only View source", () => {
    expect(source).toContain("View source");
    expect(source).not.toContain("Open markdown");
    expect(documentView).toContain('view === "source"');
    expect(documentView).toContain("DocSourceView");
  });

  test("uses the dedicated discard preflight instead of a raw cancelled status", () => {
    expect(source).toContain("Review discard impact");
    expect(source).toContain("Resolve downstream dependencies");
    expect(source).toContain("typeToConfirm: task.id");
    expect(source).toContain("useDiscardTask");
    expect(source).toContain('Status is managed by lifecycle actions; use Discard task for cancelled work.');
    expect(source).not.toContain('["cancelled", "Cancelled"]');
  });
});
