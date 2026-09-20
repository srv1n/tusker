import { expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import type { TaskCapsule } from "../src/types/domain";
import { TaskBoard } from "../src/features/workbench/board/TaskBoard";
import { filterBoardTasks, matchesAllSelectedTags } from "../src/features/workbench/board/boardModel";

const task = (id: string, status: TaskCapsule["status"] = "backlog") => ({ id, status } as TaskCapsule);

test("board all selected tags match", () => {
  const tasks = [task("one"), task("two"), task("three")];
  expect(filterBoardTasks({
    tasks,
    tagsAvailable: true,
    selectedTags: ["frontend", "auth"],
    tagsByTaskId: { one: ["frontend", "auth"], two: ["frontend"], three: ["auth", "frontend", "docs"] },
  }).map(({ id }) => id)).toEqual(["one", "three"]);
  expect(matchesAllSelectedTags("two", ["frontend", "auth"], { two: ["frontend"] })).toBe(false);
});

test("board missing tags capability", () => {
  const tasks = [task("one"), task("two")];
  expect(filterBoardTasks({ tasks, tagsAvailable: false, selectedTags: ["frontend"], tagsByTaskId: { one: ["frontend"] } })).toBe(tasks);
});

test("board live state preserves durable status", () => {
  const durable = task("one", "ready");
  const next = { ...durable, liveRun: true };
  expect(next.status).toBe("ready");
  expect(filterBoardTasks({ tasks: [next], tagsAvailable: false, selectedTags: [] })).toEqual([next]);
});

test("unassigned scope renders only its task IDs", () => {
  const tasks = [{ ...task("one"), title: "Unassigned" }, { ...task("two"), title: "Assigned" }];
  const html = renderToStaticMarkup(createElement(TaskBoard, {
    tasks, runs: [], mode: "list", onModeChange: () => {}, onSelectTask: () => {},
    taskIds: ["one"], selectedTags: [], onSelectedTagsChange: () => {}, tagsAvailable: false,
  }));
  expect(html).toContain("Unassigned tasks");
  expect(html).toContain("Unassigned");
  expect(html).not.toContain("Assigned");
});
