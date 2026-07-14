import type { ProjectSummary } from "@/types/domain";

export const PANEL_PROJECT_STORAGE_KEY = "tusker.panel.project";
export const ALL_PROJECTS_VALUE = "all-projects";
const PROJECT_VALUE_PREFIX = "project:";

export type ProjectSelection =
  | { kind: "all" }
  | { kind: "project"; projectId: string };

export const ALL_PROJECTS: ProjectSelection = { kind: "all" };

export function projectSelection(projectId: string): ProjectSelection {
  return { kind: "project", projectId };
}

export function projectIdOf(selection: ProjectSelection): string | undefined {
  return selection.kind === "project" ? selection.projectId : undefined;
}

export function projectSelectionValue(selection: ProjectSelection): string {
  return selection.kind === "all"
    ? ALL_PROJECTS_VALUE
    : `${PROJECT_VALUE_PREFIX}${encodeURIComponent(selection.projectId)}`;
}

export function projectSelectionFromValue(value: string | null): ProjectSelection {
  if (!value || value === ALL_PROJECTS_VALUE) return ALL_PROJECTS;
  if (!value.startsWith(PROJECT_VALUE_PREFIX)) return ALL_PROJECTS;
  try {
    return projectSelection(decodeURIComponent(value.slice(PROJECT_VALUE_PREFIX.length)));
  } catch {
    return ALL_PROJECTS;
  }
}

export function sameProjectSelection(left: ProjectSelection, right: ProjectSelection): boolean {
  if (left.kind === "all") return right.kind === "all";
  return right.kind === "project" && left.projectId === right.projectId;
}

export function resolveProjectSelection(selection: ProjectSelection, projects: ProjectSummary[]): ProjectSelection {
  if (selection.kind === "all") return ALL_PROJECTS;
  return projects.some((project) => project.id === selection.projectId) ? selection : ALL_PROJECTS;
}

export function projectOptionLabel(project: ProjectSummary): string {
  const needs = `${project.needsCount} ${project.needsCount === 1 ? "need" : "needs"}`;
  const active = `${project.activeRuns} active`;
  const health = project.health === "healthy" ? "healthy" : `⚠ ${project.health || "unhealthy"}`;
  return `${project.name} · ${needs} · ${active} · ${health}`;
}

export function projectOverviewPath(selection: ProjectSelection): string {
  return selection.kind === "all"
    ? "/"
    : `/p/${encodeURIComponent(selection.projectId)}`;
}
