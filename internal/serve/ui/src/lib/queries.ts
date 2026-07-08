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
