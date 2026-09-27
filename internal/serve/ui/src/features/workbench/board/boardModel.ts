import type { TaskCapsule, TaskStateCode } from "@/types/domain";

/** Board columns are the task states, most urgent first. Labels come from each task's state record. */
export const boardGroups: Array<{ key: TaskStateCode; collapsed?: boolean }> = [
  { key: "needs_input" },
  { key: "blocked" },
  { key: "working" },
  { key: "in_review" },
  { key: "planned" },
  { key: "backlog", collapsed: true },
  { key: "done", collapsed: true },
  { key: "canceled", collapsed: true },
];

export const boardGroupFor = (task: TaskCapsule): TaskStateCode => task.state.state;

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
