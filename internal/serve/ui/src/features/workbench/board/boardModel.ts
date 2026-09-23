import type { RunSummary, TaskCapsule, TaskStatus } from "@/types/domain";
import { DISPLAY_STATE_LABEL } from "../flow/flowGraph";

export const boardGroups: Array<{ key: string; label: string; statuses: TaskStatus[]; collapsed?: boolean }> = [
  { key: "active", label: DISPLAY_STATE_LABEL.executing, statuses: ["in_progress"] },
  { key: "review", label: DISPLAY_STATE_LABEL.reviewing, statuses: ["review"] },
  { key: "ready", label: DISPLAY_STATE_LABEL.ready, statuses: ["ready"] },
  { key: "blocked", label: DISPLAY_STATE_LABEL.blocked, statuses: ["blocked"] },
  { key: "planned", label: DISPLAY_STATE_LABEL.backlog, statuses: ["backlog"], collapsed: true },
  { key: "done", label: DISPLAY_STATE_LABEL.completed, statuses: ["done"], collapsed: true },
];

/** An open gate names a human decision on this task. */
export const needsYou = (task: TaskCapsule) => Boolean(task.hasGate || task.openGates?.length);

export function isLiveTask(task: TaskCapsule, runs: RunSummary[]): boolean {
  return task.liveRun === true || runs.some(
    (run) => run.taskId === task.id && run.liveness === "fresh" && run.leaseState === "held" && run.outcome === "running",
  );
}

export function boardGroupFor(task: TaskCapsule, runs: RunSummary[]): string {
  if (isLiveTask(task, runs)) return "active";
  return boardGroups.find((group) => group.statuses.includes(task.status))?.key ?? "planned";
}

export function matchesAllSelectedTags(taskId: string, selectedTags: string[], tagsByTaskId: Record<string, string[]> = {}): boolean {
  const taskTags = new Set(tagsByTaskId[taskId] ?? []);
  return selectedTags.every((tag) => taskTags.has(tag));
}

export function filterBoardTasks({
  tasks,
  selectedTags,
  tagsByTaskId,
  tagsAvailable,
}: {
  tasks: TaskCapsule[];
  selectedTags: string[];
  tagsByTaskId?: Record<string, string[]>;
  tagsAvailable: boolean;
}): TaskCapsule[] {
  if (!tagsAvailable || selectedTags.length === 0) return tasks;
  return tasks.filter((task) => matchesAllSelectedTags(task.id, selectedTags, tagsByTaskId));
}

export function availableTags(tagsByTaskId: Record<string, string[]> = {}): string[] {
  return [...new Set(Object.values(tagsByTaskId).flat())].sort((a, b) => a.localeCompare(b));
}

export function statusLabel(task: TaskCapsule, runs: RunSummary[]): string {
  const key = boardGroupFor(task, runs);
  return boardGroups.find((group) => group.key === key)?.label ?? DISPLAY_STATE_LABEL.backlog;
}
