import type { QueryKey } from "@tanstack/react-query";

export function projectQueryScope(projectId?: string) {
  return projectId === undefined
    ? (["scope", "all"] as const)
    : (["scope", "project", projectId] as const);
}

export function scopedProjectQueryKey(prefix: readonly string[], projectId?: string): QueryKey {
  return [...prefix, ...projectQueryScope(projectId)];
}
