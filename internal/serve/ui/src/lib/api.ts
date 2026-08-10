/*
  API seam.

  Every screen reads data through this module. Production mode uses the daemon's
  JSON API; the in-browser fixture path remains an explicit UI-only opt-in for
  isolated development. The return types are the contract and must not change.
  See BACKEND-GAPS.md for the remaining endpoint checklist.
*/

import * as fx from "@/mock/fixtures";
import { deriveNeeds } from "@/features/inbox/deriveNeeds";
import type { FrontmatterUpdateInput } from "@/lib/frontmatter";
import type {
  DocgraphDocDetail,
  DocgraphResponse,
  DocgraphSavePayload,
  DocgraphSaveResponse,
  DocSaveConflict,
  DocSaveDefect,
} from "@/features/knowledge/types";
import type {
  DaemonStatus,
  ActionResult,
  DeliveryErrorPayload,
  DeliveryPlanList,
  DeliveryReview,
  DeliveryStartResult,
  AttemptDetail,
  DecisionDoc,
  DocContent,
  DocListEntry,
  EpicSummary,
  EvidenceDoc,
  FactoryOperationsProjection,
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
  ExecutionGraph,
  ExecutionInbox,
  ExecutionTimeline,
  ExecutionBindingPreview,
} from "@/types/domain";

export type ServeCapabilityClass =
  | "authoritative_mutable"
  | "authoritative_read_only"
  | "cached_projection"
  | "local_preference"
  | "unavailable";
export interface ServeCapability { id: string; class: ServeCapabilityClass; mutable?: boolean; description: string }
export interface ServeCapabilities { schema: string; capabilities: ServeCapability[] }

/** Toggle to true for UI-only Vite work against the in-browser fixture set. */
export const USE_MOCK = false;

const LATENCY_MS = 260;
let capabilityPromise: Promise<string> | null = null;

export function resetServeCapabilityCache(): void {
  capabilityPromise = null;
}

async function serveCapability(): Promise<string> {
  if (capabilityPromise === null) {
    capabilityPromise = fetch("/api/capability", { headers: { accept: "application/json" }, credentials: "same-origin" })
      .then(async (res) => {
        if (!res.ok) throw new ApiError(res.status, `GET /api/capability → ${res.status}`);
        const payload = await res.json() as { capability?: string };
        if (!payload.capability) throw new ApiError(500, "Serve capability bootstrap was empty");
        return payload.capability;
      })
      .catch((error) => { capabilityPromise = null; throw error; });
  }
  return capabilityPromise;
}

async function capabilityAuthRefusal(response: Response): Promise<boolean> {
  if (response.status !== 403) return false;
  const payload = await response.clone().json().catch(() => null) as { reason?: string; error?: string } | null;
  return /serve capability/i.test(payload?.reason ?? payload?.error ?? "");
}

async function capabilityFetch(
  input: RequestInfo | URL,
  init: RequestInit,
  retried = false,
): Promise<Response> {
  const capability = await serveCapability();
  const headers = new Headers(init.headers);
  headers.set("X-Tusker-Capability", capability);
  const response = await fetch(input, { ...init, headers });
  if (!retried && await capabilityAuthRefusal(response)) {
    resetServeCapabilityCache();
    return capabilityFetch(input, init, true);
  }
  return response;
}

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
 * carried in the body (ok/refused/reason), NOT necessarily a non-2xx. Convert
 * it here so every caller observes refusal as a rejected mutation.
 */
async function post<T extends { ok?: boolean; refused?: boolean; reason?: string; issue?: { code?: string } }>(path: string, body?: unknown): Promise<T> {
  const res = await capabilityFetch(`/api${path}`, {
    method: "POST",
    headers: { accept: "application/json", "content-type": "application/json" },
    credentials: "same-origin",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const capabilityRefused = !res.ok && await capabilityAuthRefusal(res);
  const payload = await res.json().catch(() => null) as (T & { error?: string }) | null;
  if (!res.ok) {
    if (payload && !capabilityRefused && (payload.refused === true || payload.ok === false)) {
      throw new ActionRefusalError(payload, res.status);
    }
    throw new ApiError(res.status, payload?.reason ?? payload?.error ?? `POST /api${path} → ${res.status}`);
  }
  if (payload === null) throw new ApiError(502, `POST /api${path} returned no JSON result`);
  return requireAccepted(payload);
}

export function withProject(path: string, projectId?: string): string {
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

/** A mutation reached Serve, but the control plane declined it in-band. */
export type ActionFailureKind = "refused" | "validation";
export class ActionRefusalError<T extends { reason?: string; refused?: boolean; ok?: boolean; issue?: { code?: string } }> extends ApiError {
  readonly kind: ActionFailureKind;
  constructor(public result: T, status = 409) {
    super(status, result.reason || "The control-plane refused this action.");
    this.name = "ActionRefusalError";
    this.kind = /invalid|validation|required/i.test(result.issue?.code ?? "") ? "validation" : "refused";
  }
}

/** Convert the wire-level action union into an accepted mutation or a typed refusal. */
export function requireAccepted<T extends { reason?: string; refused?: boolean; ok?: boolean }>(result: T): T {
  if (result.refused === true || result.ok === false) throw new ActionRefusalError(result);
  return result;
}

export class DeliveryError extends ApiError {
  constructor(status: number, public problem: DeliveryErrorPayload) {
    super(status, problem.error.message);
    this.name = "DeliveryError";
  }
}

async function deliveryRequest<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    headers: { accept: "application/json", "content-type": "application/json" },
    credentials: "same-origin",
    body: body === undefined ? undefined : JSON.stringify(body),
  };
  const res = method === "POST" ? await capabilityFetch(`/api${path}`, init) : await fetch(`/api${path}`, init);
  const payload = await res.json().catch(() => null) as T | DeliveryErrorPayload | { reason?: string; error?: string } | null;
  if (!res.ok) {
    if (payload === null) {
      throw new ApiError(res.status, `${method} /api${path} returned a non-JSON error response`);
    }
    if (typeof (payload as DeliveryErrorPayload).error === "object") {
      throw new DeliveryError(res.status, payload as DeliveryErrorPayload);
    }
    const failure = payload as { reason?: string; error?: string };
    throw new ApiError(res.status, failure.reason ?? failure.error ?? `${method} /api${path} → ${res.status}`);
  }
  if (payload === null) {
    throw new ApiError(502, `${method} /api${path} returned no JSON result`);
  }
  return payload as T;
}

/**
 * A refused document save. Unlike the read/action endpoints, the doc-save PUT
 * uses HTTP status to distinguish an on-disk conflict (409) from header-rule
 * defects (422); both carry a structured body the UI surfaces inline, so this
 * error preserves that body rather than collapsing it to a message string.
 */
export class DocSaveError extends ApiError {
  constructor(
    status: number,
    message: string,
    public conflict?: DocSaveConflict,
    public defects?: DocSaveDefect[],
  ) {
    super(status, message);
    this.name = "DocSaveError";
  }
}

// ----------------------------------------------------------------------------
// Reads
// ----------------------------------------------------------------------------

export const api = {
  capabilities: (): Promise<ServeCapabilities> => real("/capabilities"),
  executions: (params: Record<string, string | undefined>, projectId?: string): Promise<ExecutionGraph> => {
    const query = new URLSearchParams(Object.entries(params).filter(([, value]) => value) as [string, string][]).toString();
    return real(withProject(`/executions${query ? `?${query}` : ""}`, projectId));
  },
  executionInbox: (projectId?: string): Promise<ExecutionInbox> => real(withProject("/executions/inbox", projectId)),
  executionTimeline: (execution: string, params: Record<string, string | undefined>, projectId?: string): Promise<ExecutionTimeline> => {
    const query = new URLSearchParams({ execution, ...Object.fromEntries(Object.entries(params).filter(([, value]) => value)) }).toString();
    return real(withProject(`/executions/timeline?${query}`, projectId));
  },
  executionRename: (execution: string, name: string, projectId?: string) => post<{ ok: boolean }>(withProject(`/executions/${encodeURIComponent(execution)}/rename`, projectId), { name }),
  executionBindingPreview: (execution: string, taskId: string, projectId?: string): Promise<ExecutionBindingPreview> => real(withProject(`/executions/${encodeURIComponent(execution)}/binding-preview?task_id=${encodeURIComponent(taskId)}`, projectId)),
  executionBind: (execution: string, taskId: string, projectId?: string) => post<{ ok: boolean; proof_boundary: string }>(withProject(`/executions/${encodeURIComponent(execution)}/bind`, projectId), { task_id: taskId }),
  // GET /api/factory-operations?project=
  factoryOperations: (projectId?: string): Promise<FactoryOperationsProjection> =>
    real(withProject("/factory-operations", projectId)),

  deliveryPlans: (projectId?: string): Promise<DeliveryPlanList> =>
    real(withProject("/delivery/plans", projectId)),

  deliveryReview: (plan: string, projectId?: string): Promise<DeliveryReview> =>
    deliveryRequest("GET", withProject(`/delivery/review?plan=${encodeURIComponent(plan)}`, projectId)),

  deliveryStart: (body: { plan: string; confirm: string; planIdentity: string }, projectId?: string): Promise<DeliveryStartResult> =>
    deliveryRequest("POST", withProject("/delivery/start", projectId), body),

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

  setProjectSettings: (projectId: string, body: { workspaceMode?: string; maxActiveRunsPerProject?: number }): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "Project settings saved", projectId }) : post(`/projects/${projectId}/settings`, body),

  refreshProject: (projectId: string): Promise<ActionResult> =>
    USE_MOCK
      ? delay({ ok: true, reason: "Targeted project refresh queued (mock).", projectId })
      : post(`/projects/${projectId}/refresh`),

  // GET /api/needs  (DERIVED, ranked) — optional ?project=
  // Computed from the board via the closed five-signal rule (deriveNeeds); never
  // a stored or hand-flagged list. This derivation should move server-side once
  // the daemon exposes gates/rework/wave-boundary state (SRV-T-0002; BACKEND-GAPS).
  needs: (projectId?: string): Promise<NeedItem[]> => {
    if (!USE_MOCK) return real(withProject("/needs", projectId));
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
      : real(withProject("/runs", projectId)),

  reviewBatch: (projectId?: string): Promise<TaskCapsule[]> => {
    const review = fx.taskCapsules.filter((task) => task.status === "review");
    return USE_MOCK
      ? delay(projectId ? review.filter((task) => task.projectId === projectId) : review)
      : real(withProject("/review/batch", projectId));
  },

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

  // POST /api/runs/:taskId/acknowledge — retires a settled failed run via the
  // same path as `tusker runs retire`, clearing it from attention. Success
  // returns an ActionResult; a still-active run is refused with 409/400 whose
  // body carries the reason, surfaced here as an ApiError the card restores on.
  acknowledgeRun: async (taskId: string, projectId?: string): Promise<ActionResult> => {
    if (USE_MOCK) return delay({ ok: true, reason: "run acknowledged (mock)", taskId });
    const path = withProject(`/runs/${taskId}/acknowledge`, projectId);
    return post<ActionResult>(path, {});
  },

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

  runTask: (taskId: string, projectId?: string): Promise<ActionResult> =>
    USE_MOCK ? delay({ ok: true, reason: "queued for daemon dispatch", taskId }) : post(withProject(`/tasks/${taskId}/run`, projectId), {}),

  discardTask: (
    taskId: string,
    body: { dryRun?: boolean; reason?: string; actor?: string; dependents?: "detach" | "discard" },
    projectId?: string,
  ): Promise<ActionResult> =>
    USE_MOCK
      ? delay({
          ok: true,
          reason: body.dryRun ? "discard impact calculated" : "task discarded",
          taskId,
          discard: {
            taskId, title: taskId, status: "ready", directDependents: [], cascadeDependents: [],
            openGates: [], requiresResolution: false, preservesHistory: true,
          },
        })
      : post(withProject(`/tasks/${taskId}/discard`, projectId), body),

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

  // GET /api/docgraph?project= — the documentation corpus + its relation graph.
  // No fixture backs this; UI-only mock work surfaces the daemon/error state.
  docgraph: (projectId?: string): Promise<DocgraphResponse> =>
    USE_MOCK
      ? Promise.reject(new ApiError(404, "docgraph has no fixture"))
      : real(withProject("/docgraph", projectId)),

  // GET /api/docgraph/doc?project=&subject= — one rendered corpus document.
  docgraphDoc: (projectId: string, subject: string): Promise<DocgraphDocDetail> =>
    USE_MOCK
      ? Promise.reject(new ApiError(404, "docgraph has no fixture"))
      : real(withProject(`/docgraph/doc?subject=${encodeURIComponent(subject)}`, projectId)),

  // PUT /api/docgraph/doc?project=&subject= — write an edited corpus document.
  // 200 → fresh detail + warnings; 409 → on-disk conflict; 422 → header defects.
  // A refusal is a typed DocSaveError so the reader can surface it inline.
  saveDocgraphDoc: async (
    projectId: string,
    subject: string,
    payload: DocgraphSavePayload,
  ): Promise<DocgraphSaveResponse> => {
    const path = withProject(`/docgraph/doc?subject=${encodeURIComponent(subject)}`, projectId);
    const res = await capabilityFetch(`/api${path}`, {
      method: "PUT",
      headers: { accept: "application/json", "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(payload),
    });
    if (res.ok) return (await res.json()) as DocgraphSaveResponse;
    const body = (await res.json().catch(() => null)) as
      | (Partial<DocSaveConflict> & { defects?: DocSaveDefect[] })
      | null;
    if (res.status === 409) {
      throw new DocSaveError(res.status, body?.error ?? "The document changed on disk.", {
        error: body?.error ?? "The document changed on disk.",
        code: "DOC_SAVE_CONFLICT",
        current_rev: body?.current_rev ?? "",
      });
    }
    if (res.status === 422) {
      throw new DocSaveError(res.status, body?.error ?? "Save refused.", undefined, body?.defects ?? []);
    }
    throw new ApiError(res.status, body?.error ?? `PUT /api${path} → ${res.status}`);
  },
};
