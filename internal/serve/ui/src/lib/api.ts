/*
  API seam.

  Every screen reads data through this module. Today it resolves the in-browser
  mock dataset with a little artificial latency so loading/skeleton states are
  exercised. When the daemon's JSON API lands, swap each body for a `fetch` to
  the corresponding endpoint (documented inline) — the return types are the
  contract and must not change. See BACKEND-GAPS.md for the endpoint checklist.
*/

import * as fx from "@/mock/fixtures";
import { deriveNeeds } from "@/features/inbox/deriveNeeds";
import type {
  DaemonStatus,
  DocContent,
  DocListEntry,
  EpicSummary,
  NeedItem,
  ProjectSummary,
  RunDetail,
  RunSummary,
  TaskCapsule,
  TaskDetail,
} from "@/types/domain";

/** Toggle to true for UI-only Vite work against the in-browser fixture set. */
export const USE_MOCK = false;

const LATENCY_MS = 260;

function delay<T>(value: T, ms = LATENCY_MS): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms));
}

/** Placeholder for the eventual real transport. */
async function real<T>(path: string): Promise<T> {
  const res = await fetch(`/api${path}`, { headers: { accept: "application/json" } });
  if (!res.ok) throw new ApiError(res.status, `GET /api${path} → ${res.status}`);
  return (await res.json()) as T;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// ----------------------------------------------------------------------------
// Reads
// ----------------------------------------------------------------------------

export const api = {
  // GET /api/daemon
  daemon: (): Promise<DaemonStatus> =>
    USE_MOCK ? delay(fx.daemon) : real("/daemon"),

  // GET /api/projects
  projects: (): Promise<ProjectSummary[]> => {
    if (!USE_MOCK) return real("/projects");
    // needsCount is DERIVED, not stored — keep the sidebar badge honest so it
    // can never claim "N needs you" when the five-signal rule says zero.
    const derived = deriveNeeds({
      capsules: fx.taskCapsules,
      details: fx.taskDetails,
      runs: fx.runs,
    });
    const withCounts = fx.projects.map((p) => ({
      ...p,
      needsCount: derived.filter((n) => n.projectId === p.id).length,
    }));
    return delay(withCounts);
  },

  // GET /api/needs  (DERIVED, ranked) — optional ?project=
  // Computed from the board via the closed five-signal rule (deriveNeeds); never
  // a stored or hand-flagged list. This derivation should move server-side once
  // the daemon exposes gates/rework/wave-boundary state (SRV-T-0002; BACKEND-GAPS).
  needs: (projectId?: string): Promise<NeedItem[]> => {
    if (!USE_MOCK) return real(`/needs${projectId ? `?project=${projectId}` : ""}`);
    const all = deriveNeeds({
      capsules: fx.taskCapsules,
      details: fx.taskDetails,
      runs: fx.runs,
    });
    return delay(projectId ? all.filter((n) => n.projectId === projectId) : all);
  },

  // GET /api/runs?project=
  runs: (projectId?: string): Promise<RunSummary[]> =>
    USE_MOCK
      ? delay(projectId ? fx.runs.filter((r) => r.projectId === projectId) : fx.runs)
      : real(`/runs${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/runs/:taskId
  run: (taskId: string): Promise<RunDetail> => {
    if (!USE_MOCK) return real(`/runs/${taskId}`);
    const detail = fx.runDetailFor(taskId);
    if (!detail) return Promise.reject(new ApiError(404, `No run for ${taskId} yet.`));
    return delay(detail);
  },

  // GET /api/epics?project=
  epics: (projectId?: string): Promise<EpicSummary[]> =>
    USE_MOCK ? delay(fx.epics) : real(`/epics${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/tasks?project=
  tasks: (projectId?: string): Promise<TaskCapsule[]> =>
    USE_MOCK ? delay(fx.taskCapsules) : real(`/tasks${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/tasks/:id
  task: (id: string): Promise<TaskDetail> => {
    if (!USE_MOCK) return real(`/tasks/${id}`);
    const detail = fx.taskDetails[id];
    if (!detail) return Promise.reject(new ApiError(404, `no task detail for ${id}`));
    return delay(detail);
  },

  // GET /api/docs?project=
  docs: (projectId?: string): Promise<DocListEntry[]> =>
    USE_MOCK ? delay(fx.docList) : real(`/docs${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/docs/*path
  doc: (path: string): Promise<DocContent> => {
    if (!USE_MOCK) return real(`/docs/${encodeURI(path)}`);
    const content = fx.docContents[path];
    if (!content) return Promise.reject(new ApiError(404, `no doc at ${path}`));
    return delay(content);
  },
};
