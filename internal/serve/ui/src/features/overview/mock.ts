/*
  Overview-local mock.

  The design's header eyebrow shows the project's checkout path and current
  branch, but `ProjectSummary` (the projects endpoint contract) carries neither.
  This keeps the shape local and typed until the API grows the fields.
*/

// TODO(api): GET /api/projects should return `path` and `branch` per project so
// the overview eyebrow can drop this local lookup.
export interface ProjectMeta {
  path: string;
  branch: string;
}

const META: Record<string, ProjectMeta> = {
  tusker: { path: "~/side/tusker", branch: "main" },
  "rzn-browser": { path: "~/side/rzn-browser", branch: "main" },
  headroom: { path: "~/side/headroom", branch: "develop" },
};

export function projectMeta(projectId: string): ProjectMeta {
  return META[projectId] ?? { path: `~/${projectId}`, branch: "main" };
}
