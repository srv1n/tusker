import type { NeedItem } from "@/types/domain";

export function taskIdentity(projectId: string, taskId: string): string {
  return JSON.stringify([projectId, taskId]);
}

export function humanActionIdentity(item: NeedItem): string {
  return JSON.stringify([item.projectId, item.humanAction?.gateId ?? item.gateId ?? item.id]);
}

export function partitionPanelNeeds(needs: NeedItem[]): {
  humanActionRows: NeedItem[];
  attentionNeeds: NeedItem[];
} {
  const humanActionRows: NeedItem[] = [];
  const humanActions = new Set<string>();

  for (const item of needs) {
    const identity = humanActionIdentity(item);
    if (!item.humanAction || humanActions.has(identity)) continue;
    humanActions.add(identity);
    humanActionRows.push(item);
  }

  return {
    humanActionRows,
    attentionNeeds: needs.filter((item) => !item.humanAction),
  };
}
