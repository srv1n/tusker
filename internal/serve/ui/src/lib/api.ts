/*
  API seam.

  Every screen reads data through this module. Production mode uses the daemon's
  JSON API. The return types are the contract and must not change.
  Keep this client aligned with cmd/tusker/serve_command.go.
*/

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
  ConfigResolution,
  SetupDoctorResult,
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
  ReviewBatch,
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

type ServeCapabilityBootstrap = { capability: string; operatorActor?: string };
let capabilityPromise: Promise<ServeCapabilityBootstrap> | null = null;

export function resetServeCapabilityCache(): void {
  capabilityPromise = null;
}

async function serveCapability(): Promise<string> {
	if (capabilityPromise === null) {
		capabilityPromise = fetch("/api/capability", { headers: { accept: "application/json" }, credentials: "same-origin" })
		  .then(async (res) => {
			if (!res.ok) throw new ApiError(res.status, `GET /api/capability → ${res.status}`);
			const payload = await res.json() as { capability?: string; operatorActor?: string };
			if (!payload.capability) throw new ApiError(500, "Serve capability bootstrap was empty");
			return { capability: payload.capability, operatorActor: payload.operatorActor };
		  })
		  .catch((error) => { capabilityPromise = null; throw error; });
	}
	return (await capabilityPromise).capability;
}

async function serveOperatorActor(): Promise<string> {
	if (capabilityPromise === null) {
		await serveCapability();
	}
	const actor = (await capabilityPromise)?.operatorActor?.trim();
	if (!actor) throw new ApiError(412, "Serve operator identity is not configured; start Serve with --by human:<name>");
	return actor;
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
    serveOperatorActor().then((actor) => deliveryRequest("POST", withProject("/delivery/start", projectId), { ...body, actor })),

  // GET /api/daemon
  daemon: (): Promise<DaemonStatus> => real("/daemon"),

  // GET /api/projects
  projects: (): Promise<ProjectSummary[]> => real("/projects"),

  registerProject: (body: { repoRoot: string; vaultRoot?: string }): Promise<ProjectRegistrationResult> =>
    post("/projects", body),

  setProjectAutomation: (projectId: string, enabled: boolean): Promise<ActionResult> =>
    post(`/projects/${projectId}/automation`, { enabled }),

  setProjectSettings: (
    projectId: string,
    body: { workspaceMode?: string; maxActiveRunsPerProject?: number; key?: string; value?: unknown },
  ): Promise<ActionResult> =>
    post(`/projects/${projectId}/settings`, body),

  removeProject: (projectId: string): Promise<ActionResult> =>
    post(`/projects/${encodeURIComponent(projectId)}/remove`),

  // GET /api/config?key=&project= — layered resolution with provenance notes.
  configResolve: (key: string, projectId?: string): Promise<ConfigResolution> =>
    real(withProject(`/config?key=${encodeURIComponent(key)}`, projectId)),

  // POST /api/setup/doctor (read-only audit) or /api/setup/repair (apply
  // deterministic local repairs). Success carries the structured report.
  setupDoctor: (projectId: string, apply: boolean): Promise<SetupDoctorResult> =>
    post(apply ? "/setup/repair" : "/setup/doctor", { projectId }),

  refreshProject: (projectId: string): Promise<ActionResult> =>
    post(`/projects/${projectId}/refresh`),

  // GET /api/needs  (DERIVED, ranked) — optional ?project=
  needs: (projectId?: string): Promise<NeedItem[]> => real(withProject("/needs", projectId)),

  // GET /api/runs?project=
  runs: (projectId?: string): Promise<RunSummary[]> => real(withProject("/runs", projectId)),

  reviewBatch: (projectId?: string): Promise<ReviewBatch> => real(withProject("/review/batch", projectId)),

  // GET /api/runs/:taskId
  run: (taskId: string, projectId?: string): Promise<RunDetail> => real(withProject(`/runs/${taskId}`, projectId)),

  // POST /api/runs/:taskId/redrive — maps the Retry control to `tusker redrive`.
  // The result (requeue or refusal reason) is always returned so the UI can
  // surface it; the daemon must never retire a run behind a stale badge.
  redrive: (taskId: string, projectId?: string): Promise<RedriveResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/runs/${taskId}/redrive`, projectId), { actor })),

  // POST /api/runs/:taskId/acknowledge — retires a settled failed run via the
  // same path as `tusker runs retire`, clearing it from attention. Success
  // returns an ActionResult; a still-active run is refused with 409/400 whose
  // body carries the reason, surfaced here as an ApiError the card restores on.
  acknowledgeRun: async (taskId: string, projectId?: string): Promise<ActionResult> => {
    const path = withProject(`/runs/${taskId}/acknowledge`, projectId);
    const actor = await serveOperatorActor();
    return post<ActionResult>(path, { actor });
  },

  // POST /api/runs/:taskId/interrupt — shares `tusker runs interrupt` and
  // returns canonical lease/process readback rather than optimistic UI state.
  interrupt: (taskId: string, projectId?: string): Promise<InterruptResult> =>
    post(withProject(`/runs/${taskId}/interrupt`, projectId)),

  taskStatus: (taskId: string, body: { status: string; reason?: string; actor?: string; force?: boolean }, projectId?: string): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/tasks/${taskId}/status`, projectId), { ...body, actor })),

  runTask: (taskId: string, projectId?: string): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/tasks/${taskId}/run`, projectId), { actor })),

  discardTask: (
    taskId: string,
    body: { dryRun?: boolean; reason?: string; actor?: string; dependents?: "detach" | "discard" },
    projectId?: string,
  ): Promise<ActionResult> => {
    if (body.dryRun) return post(withProject(`/tasks/${taskId}/discard`, projectId), body);
    return serveOperatorActor().then((actor) => post(withProject(`/tasks/${taskId}/discard`, projectId), { ...body, actor }));
  },

  closeTask: (taskId: string, body: { reason?: string; actor?: string; force?: boolean }, projectId?: string): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/tasks/${taskId}/close`, projectId), { ...body, actor })),

  landTask: (taskId: string, body: { branch?: string; from?: string } = {}, projectId?: string): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/tasks/${taskId}/land`, projectId), { ...body, actor })),

  landWave: (waveId: string, projectId?: string): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/waves/${waveId}/land`, projectId), { actor })),

  gateAction: (
    gateId: string,
    action: "satisfy" | "waive" | "obsolete",
    body: { reason?: string; evidence?: string; evidenceRefs?: string[]; actor?: string; force?: boolean },
    projectId?: string,
  ): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject(`/gates/${gateId}/${action}`, projectId), { ...body, actor })),

  addEvidence: (body: Record<string, unknown>, projectId?: string): Promise<ActionResult> =>
    serveOperatorActor().then((actor) => post(withProject("/evidence", projectId), { ...body, actor })),

  addFeedback: (body: Record<string, unknown>, projectId?: string): Promise<ActionResult> =>
    post(withProject("/feedback", projectId), body),

  daemonAction: (action: "start" | "stop" | "resume" | "limits", body: Record<string, unknown> = {}): Promise<ActionResult> =>
    post(`/daemon/${action}`, body),

  // GET /api/epics?project=
  epics: (projectId?: string): Promise<EpicSummary[]> =>
    real(`/epics${projectId ? `?project=${projectId}` : ""}`),

  waves: (projectId?: string): Promise<WaveSummary[]> =>
    real(`/waves${projectId ? `?project=${projectId}` : ""}`),

  gates: (taskId?: string, projectId?: string): Promise<GateDetail[]> =>
    real(withProject(`/gates${taskId ? `?task=${taskId}` : ""}`, projectId)),

  evidence: (taskId?: string, projectId?: string): Promise<EvidenceDoc[]> =>
    real(withProject(`/evidence${taskId ? `?task=${taskId}` : ""}`, projectId)),

  decisions: (epicId?: string, projectId?: string): Promise<DecisionDoc[]> =>
    real(withProject(`/decisions${epicId ? `?epic=${epicId}` : ""}`, projectId)),

  feedback: (projectId?: string): Promise<FeedbackDoc[]> =>
    real(withProject("/feedback", projectId)),

  attempts: (taskId?: string, projectId?: string): Promise<AttemptDetail[]> =>
    real(withProject(`/attempts${taskId ? `?task=${taskId}` : ""}`, projectId)),

  // GET /api/tasks?project=
  tasks: (projectId?: string): Promise<TaskCapsule[]> =>
    real(`/tasks${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/tasks/:id
  task: (id: string, projectId?: string): Promise<TaskDetail> => real(withProject(`/tasks/${id}`, projectId)),

  // GET /api/docs?project=
  docs: (projectId?: string): Promise<DocListEntry[]> =>
    real(`/docs${projectId ? `?project=${projectId}` : ""}`),

  // GET /api/docs/*path
  doc: (path: string, projectId?: string): Promise<DocContent> =>
    real(withProject(`/docs/${encodeURI(path)}`, projectId)),

  // GET /api/docgraph?project= — the documentation corpus + its relation graph.
  docgraph: (projectId?: string): Promise<DocgraphResponse> =>
    real(withProject("/docgraph", projectId)),

  // GET /api/docgraph/doc?project=&subject= — one rendered corpus document.
  docgraphDoc: (projectId: string, subject: string): Promise<DocgraphDocDetail> =>
    real(withProject(`/docgraph/doc?subject=${encodeURIComponent(subject)}`, projectId)),

  // PUT /api/docgraph/doc?project=&subject= — write an edited corpus document.
  // 200 → fresh detail + warnings; 409 → on-disk conflict; 422 → header defects.
  // A refusal is a typed DocSaveError so the reader can surface it inline.
  saveDocgraphDoc: async (
    projectId: string,
    subject: string,
    payload: DocgraphSavePayload,
  ): Promise<DocgraphSaveResponse> => {
    const path = withProject(`/docgraph/doc?subject=${encodeURIComponent(subject)}`, projectId);
    const actor = await serveOperatorActor();
    const res = await capabilityFetch(`/api${path}`, {
      method: "PUT",
      headers: { accept: "application/json", "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ ...payload, actor }),
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
