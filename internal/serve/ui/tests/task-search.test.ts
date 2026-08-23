import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { gateDetailPath, searchRecords, searchTasks, taskDetailPath, type SearchRecord, type TaskSearchItem } from "../src/features/search/taskSearchModel";
import type { GateDetail } from "../src/types/domain";

const root = fileURLToPath(new URL("../", import.meta.url));
const source = (path: string) => readFileSync(`${root}${path}`, "utf8");

const items: TaskSearchItem[] = [
  { projectId: "alpha", projectName: "Alpha", task: { id: "SRV-T-0030", title: "Search tasks" } as TaskSearchItem["task"] },
  { projectId: "beta", projectName: "Beta", task: { id: "SRV-T-0030", title: "Duplicate ID" } as TaskSearchItem["task"] },
  { projectId: "alpha", projectName: "Alpha", task: { id: "SRV-T-0130", title: "Another task" } as TaskSearchItem["task"] },
];

describe("task search", () => {
  test("ranks exact and prefix ID matches and retains duplicate project identities", () => {
    expect(searchTasks(items, "srv-t-0030").map((item) => `${item.projectId}:${item.task.id}`)).toEqual([
      "alpha:SRV-T-0030",
      "beta:SRV-T-0030",
    ]);
    expect(searchTasks(items, "SRV-T-0")).toHaveLength(3);
    expect(searchTasks(items, "duplicate")[0]?.projectId).toBe("beta");
  });

  test("builds the canonical actionable task-contract URL", () => {
    expect(taskDetailPath("my project", "SRV-T-0030")).toBe("/p/my%20project/docs?path=SRV-T-0030");
    const palette = source("src/features/search/TaskSearch.tsx");
    expect(palette).toContain('to: "/p/$projectId/docs"');
    expect(palette).toContain('item.kind === "gate"');
    expect(palette).toContain("gate: item.kind === \"gate\" ? item.id : undefined");
  });

  test("finds first-class gates and routes them to the blocked task action", () => {
    const gate = { id: "AOS-G-0001", title: "Review live session", status: "open", blocks: ["AOS-T-0006"] } as GateDetail;
    const records: SearchRecord[] = [{ kind: "gate", projectId: "backend", projectName: "Backend", id: gate.id, title: gate.title, status: gate.status, gate }];
    expect(searchRecords(records, "aos-g-0001")[0]?.kind).toBe("gate");
    expect(gateDetailPath("backend", gate)).toBe("/p/backend/docs?path=AOS-T-0006&gate=AOS-G-0001");
  });

  test("exposes the same palette globally, on mobile, in the sidebar, and in the panel", () => {
    expect(source("src/routes/__root.tsx")).toContain("<TaskSearch />");
    expect(source("src/routes/__root.tsx")).toContain("onClick={openTaskSearch}");
    expect(source("src/components/Sidebar.tsx")).toContain("onClick={openTaskSearch}");
    expect(source("src/features/panel/Panel.tsx")).toContain("onClick={openTaskSearch}");
  });

  test("supports keyboard open, movement, selection, and escape", () => {
    const palette = source("src/features/search/TaskSearch.tsx");
    expect(palette).toContain('event.key.toLocaleLowerCase() === "k"');
    expect(palette).toContain('event.key === "ArrowDown"');
    expect(palette).toContain('event.key === "ArrowUp"');
    expect(palette).toContain('event.key === "Enter"');
    expect(palette).toContain('event.key === "Escape"');
  });
});
