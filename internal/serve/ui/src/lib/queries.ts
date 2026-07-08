/*
  TanStack Query hooks. One hook per read; all keys funnel through `qk` so
  invalidation after an action (resolve a need, retry a run) is centralized.

  Live views are invalidated by /api/stream. The interval remains only as a
  degraded fallback while the stream is disconnected.
*/

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { liveRefetchInterval } from "@/lib/stream";

/** Query-key factory. */
export const qk = {
  daemon: ["daemon"] as const,
  projects: ["projects"] as const,
  needs: (projectId?: string) => ["needs", projectId ?? "all"] as const,
  runs: (projectId?: string) => ["runs", projectId ?? "all"] as const,
  run: (taskId: string) => ["run", taskId] as const,
  epics: (projectId?: string) => ["epics", projectId ?? "all"] as const,
  waves: (projectId?: string) => ["waves", projectId ?? "all"] as const,
  gates: (taskId?: string) => ["gates", taskId ?? "all"] as const,
  evidence: (taskId?: string) => ["evidence", taskId ?? "all"] as const,
  decisions: (epicId?: string) => ["decisions", epicId ?? "all"] as const,
  feedback: ["feedback"] as const,
  attempts: (taskId?: string) => ["attempts", taskId ?? "all"] as const,
  tasks: (projectId?: string) => ["tasks", projectId ?? "all"] as const,
  task: (id: string) => ["task", id] as const,
  docs: (projectId?: string) => ["docs", projectId ?? "all"] as const,
  doc: (path: string) => ["doc", path] as const,
};

export const useDaemon = () =>
  useQuery({ queryKey: qk.daemon, queryFn: api.daemon, refetchInterval: liveRefetchInterval });

export const useProjects = () =>
  useQuery({ queryKey: qk.projects, queryFn: api.projects, refetchInterval: liveRefetchInterval });

export const useNeeds = (projectId?: string) =>
  useQuery({
    queryKey: qk.needs(projectId),
    queryFn: () => api.needs(projectId),
    refetchInterval: liveRefetchInterval,
  });

export const useRuns = (projectId?: string) =>
  useQuery({
    queryKey: qk.runs(projectId),
    queryFn: () => api.runs(projectId),
    refetchInterval: liveRefetchInterval,
  });

export const useRun = (taskId: string) =>
  useQuery({
    queryKey: qk.run(taskId),
    queryFn: () => api.run(taskId),
    refetchInterval: liveRefetchInterval,
  });

export const useEpics = (projectId?: string) =>
  useQuery({ queryKey: qk.epics(projectId), queryFn: () => api.epics(projectId), refetchInterval: liveRefetchInterval });

export const useWaves = (projectId?: string) =>
  useQuery({ queryKey: qk.waves(projectId), queryFn: () => api.waves(projectId), refetchInterval: liveRefetchInterval });

export const useGates = (taskId?: string) =>
  useQuery({ queryKey: qk.gates(taskId), queryFn: () => api.gates(taskId), refetchInterval: liveRefetchInterval });

export const useEvidence = (taskId?: string) =>
  useQuery({ queryKey: qk.evidence(taskId), queryFn: () => api.evidence(taskId), refetchInterval: liveRefetchInterval });

export const useDecisions = (epicId?: string) =>
  useQuery({ queryKey: qk.decisions(epicId), queryFn: () => api.decisions(epicId), refetchInterval: liveRefetchInterval });

export const useFeedback = () =>
  useQuery({ queryKey: qk.feedback, queryFn: api.feedback, refetchInterval: liveRefetchInterval });

export const useAttempts = (taskId?: string) =>
  useQuery({ queryKey: qk.attempts(taskId), queryFn: () => api.attempts(taskId), refetchInterval: liveRefetchInterval });

export const useTasks = (projectId?: string) =>
  useQuery({ queryKey: qk.tasks(projectId), queryFn: () => api.tasks(projectId), refetchInterval: liveRefetchInterval });

export const useTask = (id: string) =>
  useQuery({ queryKey: qk.task(id), queryFn: () => api.task(id), refetchInterval: liveRefetchInterval });

export const useDocList = (projectId?: string) =>
  useQuery({ queryKey: qk.docs(projectId), queryFn: () => api.docs(projectId), refetchInterval: liveRefetchInterval });

export const useDoc = (path: string) =>
  useQuery({ queryKey: qk.doc(path), queryFn: () => api.doc(path), enabled: path.length > 0, refetchInterval: liveRefetchInterval });

/**
 * Redrive (Retry) a run. The mutation resolves with the API's result — a
 * requeue or a refusal reason — which the caller surfaces; on any resolution we
 * refresh the run, the runs list, and the tasks (canonical status may change),
 * so the badge and board never lag behind the action.
 */
export const useRedrive = (taskId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.redrive(taskId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: qk.run(taskId) });
      void qc.invalidateQueries({ queryKey: ["runs"] });
      void qc.invalidateQueries({ queryKey: ["tasks"] });
    },
  });
};

function invalidateOperatorState(qc: ReturnType<typeof useQueryClient>, taskId?: string) {
  void qc.invalidateQueries({ queryKey: ["projects"] });
  void qc.invalidateQueries({ queryKey: ["needs"] });
  void qc.invalidateQueries({ queryKey: ["tasks"] });
  void qc.invalidateQueries({ queryKey: ["runs"] });
  void qc.invalidateQueries({ queryKey: ["waves"] });
  void qc.invalidateQueries({ queryKey: ["gates"] });
  void qc.invalidateQueries({ queryKey: ["evidence"] });
  void qc.invalidateQueries({ queryKey: ["decisions"] });
  void qc.invalidateQueries({ queryKey: qk.feedback });
  void qc.invalidateQueries({ queryKey: qk.daemon });
  if (taskId) {
    void qc.invalidateQueries({ queryKey: qk.task(taskId) });
    void qc.invalidateQueries({ queryKey: qk.run(taskId) });
    void qc.invalidateQueries({ queryKey: qk.attempts(taskId) });
  }
}

export const useTaskStatusAction = (taskId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { status: string; reason?: string; actor?: string; force?: boolean }) => api.taskStatus(taskId, body),
    onSettled: () => invalidateOperatorState(qc, taskId),
  });
};

export const useCloseTask = (taskId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { reason?: string; actor?: string; force?: boolean }) => api.closeTask(taskId, body),
    onSettled: () => invalidateOperatorState(qc, taskId),
  });
};

export const useLandTask = (taskId: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body?: { branch?: string; from?: string }) => api.landTask(taskId, body ?? {}),
    onSettled: () => invalidateOperatorState(qc, taskId),
  });
};

export const useGateAction = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { gateId: string; action: "satisfy" | "waive" | "obsolete"; body: { reason?: string; evidence?: string; evidenceRefs?: string[]; actor?: string; force?: boolean }; taskId?: string }) =>
      api.gateAction(input.gateId, input.action, input.body),
    onSettled: (_data, _err, input) => invalidateOperatorState(qc, input?.taskId),
  });
};

export const useEvidenceAdd = (taskId?: string) => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.addEvidence(taskId ? { ...body, taskId } : body),
    onSettled: () => invalidateOperatorState(qc, taskId),
  });
};

export const useFeedbackAdd = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.addFeedback(body),
    onSettled: () => invalidateOperatorState(qc),
  });
};

export const useLandWave = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (waveId: string) => api.landWave(waveId),
    onSettled: () => invalidateOperatorState(qc),
  });
};

export const useDaemonAction = () => {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { action: "start" | "stop" | "resume" | "limits"; body?: Record<string, unknown> }) => api.daemonAction(input.action, input.body ?? {}),
    onSettled: () => invalidateOperatorState(qc),
  });
};
