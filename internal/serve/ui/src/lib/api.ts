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
import type { FrontmatterUpdateInput } from "@/lib/frontmatter";
import type {
  DaemonStatus,
  ActionResult,
  AttemptDetail,
  DecisionDoc,
  DocContent,
  DocListEntry,
  EpicSummary,
  EvidenceDoc,
  FeedbackDoc,
  GateDetail,
  InterruptResult,
  NeedItem,
  ProjectSummary,
  ProjectRegistrationResult,
  RedriveResult,
  RunDetail,
  RunSummary,
  TaskCapsule,
  TaskDetail,
  WaveSummary,
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

/**
 * The one mutating call: POST an action and return its JSON body. A refusal is
 * carried in the body (ok/refused/reason), NOT a non-2xx, so the caller can
 * surface the reason; only transport/5xx failures throw.
 */
async function post<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method: "POST",
    headers: { accept: "application/json", "content-type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new ApiError(res.status, `POST /api${path} → ${res.status}`);
  return (await res.json()) as T;
}

function withProject(path: string, projectId?: string): string {
  if (!projectId) return path;
  return `${path}${path.includes("?") ? "&" : "?"}project=${encodeURIComponent(projectId)}`;
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

  registerProject: (body: { repoRoot: string; vaultRoot?: string }): Promise<ProjectRegistrationResult> =>
    USE_MOCK
      ? delay({ ok: true, reason: "Project registered (mock)", projectId: "mock-project" })
      : post("/projects", body),

  setProjectAutomation: (projectId: string, enabled: boolean): Promise<ActionResult> =>
    USE_MOCK
      ? delay({ ok: true, reason: `Daemon automation ${enabled ? "enabled" : "disabled"}`, projectId })
      : post(`/projects/${projectId}/automation`, { enabled }),

  refreshProject: (projectId: string): Promise<ActionResult> =>
    USE_MOCK
      ? delay({ ok: true, reason: "Targeted project refresh queued (mock).", projectId })
      : post(`/projects/${projectId}/refresh`),

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

  reviewBatch: (projectId?: string): Promise<TaskCapsule[]> =>
    USE_MOCK ? delay(fx.taskCapsules.filter((task) => task.status === "review")) : real(withProject("/review/batch", projectId)),

  // GET /api/runs/:taskId
  run: (taskId: string, projectId?: string): Promise<RunDetail> => {
    if (!USE_MOCK) return real(withProject(`/runs/${taskId}`, projectId));
    const detail = fx.runDetailFor(taskId);
    if (!detail) return Promise.reject(new ApiError(404, `No run for ${taskId} yet.`));
    return delay(detail);
  },

  // POST /api/runs/:taskId/redrive — maps the Retry control to `tusker redrive`.
  // The result (requeue or refusal reason) is always returned so the UI can
  // surface it; the daemon must never retire a run behind a stale badge.
  redrive: (taskId: string, projectId?: string): Promise<RedriveResult> =>
    USE_MOCK
      ? delay({ ok: true, requeued: true, reason: "redrive requested (mock)", taskId })
      : post(withProject(`/runs/${taskId}/redrive`, projectId)),

  // POST /api/runs/:taskId/interrupt — shares `tusker runs interrupt` and
  // returns canonical lease/process readback rather than optimistic UI state.
  interrupt: (taskId: string, projectId?: string): Promise<InterruptResult> =>
    USE_MOCK
      ? delay({
          ok: true,
          interrupted: true,
          reason: "run interrupted (mock)",
          taskId,
          leaseState: "released",
          leaseStateRaw: "interrupted",
          processRunning: false,
        })
      : post(withProject(`/runs/${taskId}/interrupt`, projectId)),

  taskStatus: (taskId: string, body: { status: string; reason?: string; actor?: string; force?: boolean }, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: `status -> ${body.status}`, taskId }) : post(withProject(`/tasks/${taskId}/status`, projectId), body),

  closeTask: (taskId: string, body: { reason?: string; actor?: string; force?: boolean }, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "task accepted", taskId }) : post(withProject(`/tasks/${taskId}/close`, projectId), body),

  landTask: (taskId: string, body: { branch?: string; from?: string } = {}, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "landed task", taskId }) : post(withProject(`/tasks/${taskId}/land`, projectId), body),

  landWave: (waveId: string, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "landed wave" }) : post(withProject(`/waves/${waveId}/land`, projectId), {}),

  gateAction: (
    gateId: string,
    action: "satisfy" | "waive" | "obsolete",
    body: { reason?: string; evidence?: string; evidenceRefs?: string[]; actor?: string; force?: boolean },
    projectId?: string,
  ): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: `${gateId} ${action}`, gateId }) : post(withProject(`/gates/${gateId}/${action}`, projectId), body),

  addEvidence: (body: Record<string, unknown>, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "evidence added" }) : post(withProject("/evidence", projectId), body),

  addFeedback: (body: Record<string, unknown>, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "feedback added" }) : post(withProject("/feedback", projectId), body),

  daemonAction: (action: "start" | "stop" | "resume" | "limits", body: Record<string, unknown> = {}): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: `daemon ${action}` }) : post(`/daemon/${action}`, body),

  // POST /api/frontmatter/update — structured frontmatter action.
  // TODO(api SRV-T-0004): swap this mock action to post("/frontmatter/update", body).
  updateFrontmatter: (body: FrontmatterUpdateInput): Promise<ActionResult> =>
    delay({
      ok: true,
      reason: `${body.key} -> ${body.value}`,
      taskId: body.target.kind === "task" ? body.target.id : undefined,
    }),

  // GET /api/epics?project=
  epics: (projectId?: string): Promise<EpicSummary[]> =>
    USE_MOCK ? delay(fx.epics) : real(`/epics${projectId ? `?project=${projectId}` : ""}`),

  waves: (projectId?: string): Promise<WaveSummary[]> =>
    USE_MOCK ? delay([]) : real(`/waves${projectId ? `?project=${projectId}` : ""}`),

  gates: (taskId?: string, projectId?: string): Promise<GateDetail[]> =>
    USE_MOCK ? delay([]) : real(withProject(`/gates${taskId ? `?task=${taskId}` : ""}`, projectId)),

  evidence: (taskId?: string, projectId?: string): Promise<EvidenceDoc[]> =>
    USE_MOCK ? delay([]) : real(withProject(`/evidence${taskId ? `?task=${taskId}` : ""}`, projectId)),

  decisions: (epicId?: string, projectId?: string): Promise<DecisionDoc[]> =>
    USE_MOCK ? delay([]) : real(withProject(`/decisions${epicId ? `?epic=${epicId}` : ""}`, projectId)),

  feedback: (projectId?: string): Promise<FeedbackDoc[]> =>
    USE_MOCK ? delay([]) : real(withProject("/feedback", projectId)),

  attempts: (taskId?: string, projectId?: string): Promise<AttemptDetail[]> =>
    USE_MOCK ? delay([]) : real(withProject(`/attempts${taskId ? `?task=${taskId}` : ""}`, projectId)),

  // GET /api/tasks?project=
  tasks: (projectId?: string): Promise<TaskCapsule[]> =>
    USE_MOCK ? delay(fx.taskCapsules) : real(`/tasks${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/tasks/:id
  task: (id: string, projectId?: string): Promise<TaskDetail> => {
    if (!USE_MOCK) return real(withProject(`/tasks/${id}`, projectId));
    const detail = fx.taskDetails[id];
    if (!detail) return Promise.reject(new ApiError(404, `no task detail for ${id}`));
    return delay(detail);
  },

  // GET /api/docs?project=
  docs: (projectId?: string): Promise<DocListEntry[]> =>
    USE_MOCK ? delay(fx.docList) : real(`/docs${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/docs/*path
  doc: (path: string, projectId?: string): Promise<DocContent> => {
    if (!USE_MOCK) return real(withProject(`/docs/${encodeURI(path)}`, projectId));
    const content = fx.docContents[path];
    if (!content) return Promise.reject(new ApiError(404, `no doc at ${path}`));
    return delay(content);
  },
};
