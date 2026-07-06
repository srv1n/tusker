/*
  TanStack Query hooks. One hook per read; all keys funnel through `qk` so
  invalidation after an action (resolve a need, retry a run) is centralized.

  Live views (needs, runs) poll on an interval to approximate the push updates
  the daemon will eventually stream — see BACKEND-GAPS.md (SSE/websocket).
*/

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

const LIVE_REFETCH_MS = 4000;

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
  useQuery({ queryKey: qk.daemon, queryFn: api.daemon, refetchInterval: LIVE_REFETCH_MS });

export const useProjects = () =>
  useQuery({ queryKey: qk.projects, queryFn: api.projects });

export const useNeeds = (projectId?: string) =>
  useQuery({
    queryKey: qk.needs(projectId),
    queryFn: () => api.needs(projectId),
    refetchInterval: LIVE_REFETCH_MS,
  });

export const useRuns = (projectId?: string) =>
  useQuery({
    queryKey: qk.runs(projectId),
    queryFn: () => api.runs(projectId),
    refetchInterval: LIVE_REFETCH_MS,
  });

export const useRun = (taskId: string) =>
  useQuery({
    queryKey: qk.run(taskId),
    queryFn: () => api.run(taskId),
    refetchInterval: LIVE_REFETCH_MS,
  });

export const useEpics = (projectId?: string) =>
  useQuery({ queryKey: qk.epics(projectId), queryFn: () => api.epics(projectId) });

export const useTasks = (projectId?: string) =>
  useQuery({ queryKey: qk.tasks(projectId), queryFn: () => api.tasks(projectId) });

export const useTask = (id: string) =>
  useQuery({ queryKey: qk.task(id), queryFn: () => api.task(id) });

export const useDocList = (projectId?: string) =>
  useQuery({ queryKey: qk.docs(projectId), queryFn: () => api.docs(projectId) });

export const useDoc = (path: string) =>
  useQuery({ queryKey: qk.doc(path), queryFn: () => api.doc(path), enabled: path.length > 0 });
