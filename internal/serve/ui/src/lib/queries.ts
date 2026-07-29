/*
  TanStack Query hooks. One hook per read; all keys funnel through `qk` so
  invalidation after an action (resolve a need, retry a run) is centralized.

  Live views are invalidated by /api/stream. The interval remains only as a
  degraded fallback while the stream is disconnected.
*/

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import {
  patchDocFrontmatter,
  patchTaskFrontmatter,
  type FrontmatterUpdateInput,
} from "@/lib/frontmatter";
import { liveRefetchInterval } from "@/lib/stream";
import { projectQueryScope } from "@/lib/queryScope";
import type { DeliveryPlanList, DeliveryReview, DeliveryStartResult, DocContent, DocListEntry, ExecutionBindingPreview, ExecutionGraph, ExecutionInbox, ExecutionTimeline, RunDetail, TaskCapsule, TaskDetail } from "@/types/domain";
import type {
  DocgraphDocDetail,
  DocgraphSavePayload,
  DocgraphSaveResponse,
} from "@/features/knowledge/types";

/** Query-key factory. */
export const qk = {
  daemon: ["daemon"] as const,
  projects: ["projects"] as const,
  factoryOperations: (projectId?: string) => ["factory-operations", ...projectQueryScope(projectId)] as const,
  needs: (projectId?: string) => ["needs", ...projectQueryScope(projectId)] as const,
  runs: (projectId?: string) => ["runs", ...projectQueryScope(projectId)] as const,
  reviewBatch: (projectId?: string) => ["review", "batch", ...projectQueryScope(projectId)] as const,
  run: (taskId: string, projectId?: string) => ["run", projectId ?? "all", taskId] as const,
  epics: (projectId?: string) => ["epics", projectId ?? "all"] as const,
  waves: (projectId?: string) => ["waves", projectId ?? "all"] as const,
  gates: (taskId?: string, projectId?: string) => ["gates", projectId ?? "all", taskId ?? "all"] as const,
  evidence: (taskId?: string, projectId?: string) => ["evidence", projectId ?? "all", taskId ?? "all"] as const,
  decisions: (epicId?: string, projectId?: string) => ["decisions", projectId ?? "all", epicId ?? "all"] as const,
  feedback: (projectId?: string) => ["feedback", projectId ?? "all"] as const,
  attempts: (taskId?: string, projectId?: string) => ["attempts", projectId ?? "all", taskId ?? "all"] as const,
  tasks: (projectId?: string) => ["tasks", projectId ?? "all"] as const,
  task: (id: string, projectId?: string) => ["task", projectId ?? "all", id] as const,
  docs: (projectId?: string) => ["docs", projectId ?? "all"] as const,
  doc: (path: string, projectId?: string) => ["doc", projectId ?? "all", path] as const,
  docgraph: (projectId?: string) => ["docgraph", projectId ?? "all"] as const,
  docgraphDoc: (projectId: string, subject: string) => ["docgraph", "doc", projectId, subject] as const,
  deliveryReview: (plan: string, projectId?: string) => ["delivery", "review", projectId ?? "all", plan] as const,
  deliveryPlans: (projectId?: string) => ["delivery", "plans", projectId ?? "all"] as const,
  executions: (projectId: string, params: Record<string, string | undefined>) => ["executions", projectId, params] as const,
  executionInbox: (projectId: string) => ["executions", "inbox", projectId] as const,
  executionTimeline: (projectId: string, execution: string, params: Record<string, string | undefined>) => ["executions", "timeline", projectId, execution, params] as const,
};

export const useExecutions = (projectId: string, params: Record<string, string | undefined>) =>
  useQuery<ExecutionGraph>({ queryKey: qk.executions(projectId, params), queryFn: () => api.executions(params, projectId), refetchInterval: liveRefetchInterval });
export const useExecutionInbox = (projectId: string) =>
  useQuery<ExecutionInbox>({ queryKey: qk.executionInbox(projectId), queryFn: () => api.executionInbox(projectId), refetchInterval: liveRefetchInterval });
export const useExecutionTimeline = (projectId: string, execution: string, params: Record<string, string | undefined>) =>
  useQuery<ExecutionTimeline>({ enabled: !!execution, queryKey: qk.executionTimeline(projectId, execution, params), queryFn: () => api.executionTimeline(execution, params, projectId), refetchInterval: liveRefetchInterval });
export const useExecutionBindingPreview = (projectId: string, execution: string, taskId: string) =>
  useQuery<ExecutionBindingPreview>({ enabled: !!execution && !!taskId, queryKey: ["executions", "binding-preview", projectId, execution, taskId], queryFn: () => api.executionBindingPreview(execution, taskId, projectId) });
export const useExecutionRename = (projectId: string) => { const qc = useQueryClient(); return useMutation({ mutationFn: ({ execution, name }: { execution: string; name: string }) => api.executionRename(execution, name, projectId), onSettled: () => void qc.invalidateQueries({ queryKey: ["executions"] }) }); };
export const useExecutionBind = (projectId: string) => { const qc = useQueryClient(); return useMutation({ mutationFn: ({ execution, taskId }: { execution: string; taskId: string }) => api.executionBind(execution, taskId, projectId), onSettled: () => void qc.invalidateQueries({ queryKey: ["executions"] }) }); };

export const useDaemon = () =>
  useQuery({ queryKey: qk.daemon, queryFn: api.daemon, refetchInterval: liveRefetchInterval });

export const useProjects = () =>
  useQuery({ queryKey: qk.projects, queryFn: api.projects, refetchInterval: liveRefetchInterval });

export const useFactoryOperations = (projectId?: string) =>
  useQuery({
    queryKey: qk.factoryOperations(projectId),
    queryFn: () => api.factoryOperations(projectId),
    refetchInterval: liveRefetchInterval,
  });

export const useRegisterProject = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { repoRoot: string; vaultRoot?: string }) => api.registerProject(body),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: qk.projects });
      void qc.invalidateQueries({ queryKey: qk.daemon });
    },
  });
};

export const useProjectAutomation = (projectId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (enabled: boolean) => api.setProjectAutomation(projectId, enabled),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: qk.projects });
      void qc.invalidateQueries({ queryKey: qk.daemon });
    },
  });
};

export const useProjectSettings = (projectId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { workspaceMode?: string; maxActiveRunsPerProject?: number }) => api.setProjectSettings(projectId, body),
    onSettled: () => void qc.invalidateQueries({ queryKey: qk.projects }),
  });
};

export const useProjectRefresh = (projectId: string) =>
  useMutation({
    mutationFn: () => api.refreshProject(projectId),
  });

export const useNeeds = (projectId?: string, enabled = true) =>
  useQuery({
    queryKey: qk.needs(projectId),
    queryFn: () => api.needs(projectId),
    enabled,
    refetchInterval: liveRefetchInterval,
  });

export const useRuns = (projectId?: string, enabled = true) =>
  useQuery({
    queryKey: qk.runs(projectId),
    queryFn: () => api.runs(projectId),
    enabled,
    refetchInterval: liveRefetchInterval,
  });

export const useReviewBatch = (projectId?: string, enabled = true) =>
  useQuery({ queryKey: qk.reviewBatch(projectId), queryFn: () => api.reviewBatch(projectId), enabled, refetchInterval: liveRefetchInterval });

export function interruptedRunReadbackComplete(run: RunDetail | undefined): boolean {
  return run?.leaseStateRaw === "interrupted" && run.processRunning === false;
}

export function runRefetchInterval(
  refreshUntilInterrupted: boolean,
  run: RunDetail | undefined,
  hasQueryError: boolean,
  fallbackInterval: number | false,
): number | false {
  if (hasQueryError) return false;
  if (refreshUntilInterrupted && !interruptedRunReadbackComplete(run)) return 400;
  return fallbackInterval;
}

export const useRun = (taskId: string, refreshUntilInterrupted = false, projectId?: string) =>
  useQuery({
    queryKey: qk.run(taskId, projectId),
    queryFn: () => api.run(taskId, projectId),
    refetchInterval: (query) =>
      runRefetchInterval(
        refreshUntilInterrupted,
        query.state.data,
        query.state.error !== null,
        liveRefetchInterval(),
      ),
  });

export const useEpics = (projectId?: string) =>
  useQuery({ queryKey: qk.epics(projectId), queryFn: () => api.epics(projectId), refetchInterval: liveRefetchInterval });

export const useWaves = (projectId?: string) =>
  useQuery({ queryKey: qk.waves(projectId), queryFn: () => api.waves(projectId), refetchInterval: liveRefetchInterval });

export const useGates = (taskId?: string, projectId?: string) =>
  useQuery({ queryKey: qk.gates(taskId, projectId), queryFn: () => api.gates(taskId, projectId), refetchInterval: liveRefetchInterval });

export const useEvidence = (taskId?: string, projectId?: string) =>
  useQuery({ queryKey: qk.evidence(taskId, projectId), queryFn: () => api.evidence(taskId, projectId), refetchInterval: liveRefetchInterval });

export const useDecisions = (epicId?: string, projectId?: string) =>
  useQuery({ queryKey: qk.decisions(epicId, projectId), queryFn: () => api.decisions(epicId, projectId), refetchInterval: liveRefetchInterval });

export const useFeedback = (projectId?: string) =>
  useQuery({ queryKey: qk.feedback(projectId), queryFn: () => api.feedback(projectId), refetchInterval: liveRefetchInterval });

export const useAttempts = (taskId?: string, projectId?: string) =>
  useQuery({ queryKey: qk.attempts(taskId, projectId), queryFn: () => api.attempts(taskId, projectId), refetchInterval: liveRefetchInterval });

export const useTasks = (projectId?: string) =>
  useQuery({ queryKey: qk.tasks(projectId), queryFn: () => api.tasks(projectId), refetchInterval: liveRefetchInterval });

export const useTask = (id: string, projectId?: string) =>
  useQuery({ queryKey: qk.task(id, projectId), queryFn: () => api.task(id, projectId), refetchInterval: liveRefetchInterval });

export const useDocList = (projectId?: string) =>
  useQuery({ queryKey: qk.docs(projectId), queryFn: () => api.docs(projectId), refetchInterval: liveRefetchInterval });

export const useDoc = (path: string, projectId?: string) =>
  useQuery({ queryKey: qk.doc(path, projectId), queryFn: () => api.doc(path, projectId), enabled: path.length > 0, refetchInterval: liveRefetchInterval });

export const useDocgraph = (projectId?: string) =>
  useQuery({ queryKey: qk.docgraph(projectId), queryFn: () => api.docgraph(projectId), refetchInterval: liveRefetchInterval });

export const useDocgraphDoc = (projectId: string, subject: string) =>
  useQuery({ queryKey: qk.docgraphDoc(projectId, subject), queryFn: () => api.docgraphDoc(projectId, subject), enabled: subject.length > 0, refetchInterval: liveRefetchInterval });

/**
 * Save an edited corpus document. On success the fresh detail (new rev, links,
 * backlinks, successor) is written straight into the doc cache, and the corpus
 * list + graph query is invalidated so a title/status change is reflected there
 * without a reload. A refused save (conflict/defects) rejects with a typed
 * DocSaveError the reader surfaces inline; nothing is written to the cache.
 */
export const useSaveDocgraphDoc = (projectId: string, subject: string) => {
  const qc = useQueryClient();
  return useMutation<DocgraphSaveResponse, unknown, DocgraphSavePayload>({
    mutationKey: ["docgraph", "save", projectId, subject],
    mutationFn: (payload) => api.saveDocgraphDoc(projectId, subject, payload),
    onSuccess: (data) => {
      // Strip the transient warnings before seeding the read cache — they belong
      // to this save response, not to the persisted document.
      const { warnings: _warnings, ...detail } = data;
      qc.setQueryData<DocgraphDocDetail>(qk.docgraphDoc(projectId, subject), detail);
      void qc.invalidateQueries({ queryKey: qk.docgraph(projectId) });
    },
  });
};

export const useDeliveryReview = (plan: string, projectId?: string) =>
  useQuery<DeliveryReview>({ queryKey: qk.deliveryReview(plan, projectId), queryFn: () => api.deliveryReview(plan, projectId), enabled: plan.trim().length > 0 });

export const useDeliveryPlans = (projectId?: string) =>
  useQuery<DeliveryPlanList>({ queryKey: qk.deliveryPlans(projectId), queryFn: () => api.deliveryPlans(projectId), refetchInterval: liveRefetchInterval });

export const useDeliveryStart = (projectId?: string) => {
  const qc = useQueryClient();
  return useMutation<DeliveryStartResult, unknown, { plan: string; confirm: string; planIdentity: string }>({
    mutationFn: (body) => api.deliveryStart(body, projectId),
    onSettled: (_data, _error, variables) => {
      void qc.invalidateQueries({ queryKey: qk.deliveryReview(variables?.plan ?? "", projectId) });
      void qc.invalidateQueries({ queryKey: qk.deliveryPlans(projectId) });
      void qc.invalidateQueries({ queryKey: ["waves"] });
      void qc.invalidateQueries({ queryKey: ["tasks"] });
    },
  });
};

/**
 * Redrive (Retry) a run. The mutation resolves with the API's result — a
 * requeue or a refusal reason — which the caller surfaces; on any resolution we
 * refresh the run, the runs list, and the tasks (canonical status may change),
 * so the badge and board never lag behind the action.
 */
export const useRedrive = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["redrive", taskId],
    mutationFn: () => api.redrive(taskId, projectId),
    onSettled: () => invalidateRunActionQueries(qc, taskId, projectId),
  });
};

/**
 * Acknowledge (retire) a settled failed run. Resolves with the API's
 * ActionResult on success; a still-active run is refused as a rejected ApiError
 * the caller surfaces inline. On any resolution the needs, runs, project (sidebar
 * badge), and run-detail queries are invalidated so the acknowledged card stays
 * gone everywhere — including the global "Needs me · all" inbox.
 */
export const useAcknowledgeRun = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["acknowledge", taskId],
    mutationFn: () => api.acknowledgeRun(taskId, projectId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["needs"] });
      void qc.invalidateQueries({ queryKey: ["runs"] });
      void qc.invalidateQueries({ queryKey: ["projects"] });
      void qc.invalidateQueries({ queryKey: qk.run(taskId, projectId) });
    },
  });
};

/**
 * Interrupt a run through the shared CLI transition. The detail read is
 * invalidated immediately; Run Detail then polls until raw lease + verified
 * process state prove the stop instead of trusting the mutation response.
 */
export const useInterrupt = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationKey: ["interrupt", taskId],
    mutationFn: () => api.interrupt(taskId, projectId),
    onSettled: () => invalidateRunActionQueries(qc, taskId, projectId),
  });
};

export async function invalidateRunActionQueries(
  qc: Pick<ReturnType<typeof useQueryClient>, "invalidateQueries">,
  taskId: string,
  projectId?: string,
): Promise<void> {
  await Promise.all([
    qc.invalidateQueries({ queryKey: qk.run(taskId, projectId) }),
    qc.invalidateQueries({ queryKey: ["runs"] }),
    qc.invalidateQueries({ queryKey: ["tasks"] }),
  ]);
}

function invalidateOperatorState(qc: ReturnType<typeof useQueryClient>, taskId?: string, projectId?: string) {
  void qc.invalidateQueries({ queryKey: ["projects"] });
  void qc.invalidateQueries({ queryKey: ["needs"] });
  void qc.invalidateQueries({ queryKey: ["tasks"] });
  void qc.invalidateQueries({ queryKey: ["runs"] });
  void qc.invalidateQueries({ queryKey: ["waves"] });
  void qc.invalidateQueries({ queryKey: ["gates"] });
  void qc.invalidateQueries({ queryKey: ["evidence"] });
  void qc.invalidateQueries({ queryKey: ["decisions"] });
  void qc.invalidateQueries({ queryKey: ["feedback"] });
  void qc.invalidateQueries({ queryKey: qk.daemon });
  if (taskId) {
    void qc.invalidateQueries({ queryKey: qk.task(taskId, projectId) });
    void qc.invalidateQueries({ queryKey: qk.run(taskId, projectId) });
    void qc.invalidateQueries({ queryKey: qk.attempts(taskId, projectId) });
  }
}

export const useTaskStatusAction = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { status: string; reason?: string; actor?: string; force?: boolean }) => api.taskStatus(taskId, body, projectId),
    onSettled: () => invalidateOperatorState(qc, taskId, projectId),
  });
};

export const useRunTask = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.runTask(taskId, projectId),
    onSettled: () => invalidateOperatorState(qc, taskId, projectId),
  });
};

export const useDiscardTask = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { dryRun?: boolean; reason?: string; actor?: string; dependents?: "detach" | "discard" }) =>
      api.discardTask(taskId, body, projectId),
    onSettled: () => invalidateOperatorState(qc, taskId, projectId),
  });
};

export const useCloseTask = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { reason?: string; actor?: string; force?: boolean }) => api.closeTask(taskId, body, projectId),
    onSettled: () => invalidateOperatorState(qc, taskId, projectId),
  });
};

export const useLandTask = (taskId: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body?: { branch?: string; from?: string }) => api.landTask(taskId, body ?? {}, projectId),
    onSettled: () => invalidateOperatorState(qc, taskId, projectId),
  });
};

export const useGateAction = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { gateId: string; action: "satisfy" | "waive" | "obsolete"; body: { reason?: string; evidence?: string; evidenceRefs?: string[]; actor?: string; force?: boolean }; taskId?: string; projectId?: string }) =>
      api.gateAction(input.gateId, input.action, input.body, input.projectId),
    onSettled: (_data, _err, input) => invalidateOperatorState(qc, input?.taskId, input?.projectId),
  });
};

export const useEvidenceAdd = (taskId?: string, projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.addEvidence(taskId ? { ...body, taskId } : body, projectId),
    onSettled: () => invalidateOperatorState(qc, taskId, projectId),
  });
};

export const useFeedbackAdd = (projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.addFeedback(body, projectId),
    onSettled: () => invalidateOperatorState(qc, undefined, projectId),
  });
};

export const useLandWave = (projectId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (waveId: string) => api.landWave(waveId, projectId),
    onSettled: () => invalidateOperatorState(qc, undefined, projectId),
  });
};

export const useDaemonAction = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { action: "start" | "stop" | "resume" | "limits"; body?: Record<string, unknown> }) => api.daemonAction(input.action, input.body ?? {}),
    onSettled: () => invalidateOperatorState(qc),
  });
};

export const useFrontmatterUpdate = () => {
  const qc = useQueryClient();

  const apply = (input: FrontmatterUpdateInput) => {
    if (input.target.kind === "task") {
      const taskId = input.target.id;
      qc.setQueryData<TaskDetail>(qk.task(taskId), (task) =>
        task ? patchTaskFrontmatter(task, input.key, input.value) : task,
      );
      qc.setQueriesData<TaskCapsule[]>({ queryKey: ["tasks"] }, (tasks) =>
        tasks?.map((task) =>
          task.id === taskId ? patchTaskFrontmatter(task, input.key, input.value) : task,
        ),
      );
      qc.setQueryData<DocContent>(qk.doc(`.tusker/work/tasks/${taskId}.md`), (doc) =>
        doc ? patchDocFrontmatter(doc, input.key, input.value) : doc,
      );
      return;
    }

    const docPath = input.target.path;
    qc.setQueryData<DocContent>(qk.doc(docPath), (doc) =>
      doc ? patchDocFrontmatter(doc, input.key, input.value) : doc,
    );
    if (input.key === "title") {
      qc.setQueriesData<DocListEntry[]>({ queryKey: ["docs"] }, (docs) =>
        docs?.map((doc) =>
          doc.path === docPath ? { ...doc, title: input.value } : doc,
        ),
      );
    }
  };

  return useMutation({
    mutationFn: (input: FrontmatterUpdateInput) => api.updateFrontmatter(input),
    onSuccess: (result, input) => {
      if (result.ok) apply(input);
    },
  });
};
