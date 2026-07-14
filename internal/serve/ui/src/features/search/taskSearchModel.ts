import type { GateDetail, TaskCapsule } from "@/types/domain";

export interface TaskSearchItem {
  projectId: string;
  projectName: string;
  task: TaskCapsule;
}

export type SearchRecord =
  | {
      kind: "task";
      projectId: string;
      projectName: string;
      id: string;
      title: string;
      status: string;
      task: TaskCapsule;
    }
  | {
      kind: "gate";
      projectId: string;
      projectName: string;
      id: string;
      title: string;
      status: string;
      gate: GateDetail;
    };

function normalized(value: string): string {
  return value.trim().toLocaleUpperCase();
}

function matchRank(item: Pick<SearchRecord, "id" | "title">, query: string): number {
  const id = normalized(item.id);
  const title = normalized(item.title);
  if (id === query) return 0;
  if (id.startsWith(query)) return 1;
  if (id.includes(query)) return 2;
  if (title.includes(query)) return 3;
  return Number.POSITIVE_INFINITY;
}

export function searchRecords(items: SearchRecord[], rawQuery: string, limit = 30): SearchRecord[] {
  const query = normalized(rawQuery);
  if (!query) return [];

  return items
    .map((item) => ({ item, rank: matchRank(item, query) }))
    .filter(({ rank }) => Number.isFinite(rank))
    .sort((a, b) =>
      a.rank - b.rank ||
      a.item.id.localeCompare(b.item.id) ||
      a.item.projectName.localeCompare(b.item.projectName),
    )
    .slice(0, limit)
    .map(({ item }) => item);
}

export function searchTasks(items: TaskSearchItem[], rawQuery: string, limit = 30): TaskSearchItem[] {
  const records: SearchRecord[] = items.map((item) => ({
    kind: "task",
    projectId: item.projectId,
    projectName: item.projectName,
    id: item.task.id,
    title: item.task.title,
    status: item.task.status,
    task: item.task,
  }));
  const byIdentity = new Map(items.map((item) => [`${item.projectId}:${item.task.id}`, item]));
  return searchRecords(records, rawQuery, limit)
    .map((record) => byIdentity.get(`${record.projectId}:${record.id}`))
    .filter((item): item is TaskSearchItem => item !== undefined);
}

export function taskDetailPath(projectId: string, taskId: string): string {
  return `/p/${encodeURIComponent(projectId)}/docs?path=${encodeURIComponent(taskId)}`;
}

export function gateDetailPath(projectId: string, gate: GateDetail): string {
  const taskId = gate.blocks[0];
  if (!taskId) return `/p/${encodeURIComponent(projectId)}/ops`;
  return `${taskDetailPath(projectId, taskId)}&gate=${encodeURIComponent(gate.id)}`;
}
