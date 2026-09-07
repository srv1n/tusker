import type { RunSummary, TaskCapsule, TaskStatus } from "@/types/domain";

export const boardGroups: Array<{ key: string; label: string; statuses: TaskStatus[] }> = [
  { key: "active", label: "Active", statuses: ["in_progress"] },
  { key: "review", label: "Reviewing", statuses: ["review"] },
  { key: "ready", label: "Ready", statuses: ["ready"] },
  { key: "blocked", label: "Blocked", statuses: ["blocked"] },
  { key: "planned", label: "Planned", statuses: ["backlog"] },
  { key: "done", label: "Completed", statuses: ["done"] },
];

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
  if (isLiveTask(task, runs)) return "Building now";
  switch (task.status) {
    case "in_progress": return "Building";
    case "review": return "Reviewing";
    case "ready": return "Ready to start";
    case "blocked": return "Blocked";
    case "done": return "Completed";
    default: return "Planned";
  }
}
