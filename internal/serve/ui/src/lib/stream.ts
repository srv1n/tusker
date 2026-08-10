import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { scopedProjectQueryKey } from "@/lib/queryScope";

export const LIVE_STREAM_FALLBACK_MS = 45_000;
export const STREAM_INVALIDATION_DEBOUNCE_MS = 75;

export interface StreamEvent {
  id?: number;
  kind: string;
  keys: string[];
  project?: string;
  task_id?: string;
  title?: string;
  urgency?: "info" | "attention" | "critical";
  deep_link_path?: string;
  occurred_at?: string;
  replay_miss?: boolean;
}

export interface StreamStatus {
  connected: boolean;
  lastEventAt: number | null;
  lastErrorAt: number | null;
}

type StreamListener = () => void;
type EventSourceLike = {
  onopen: ((event: Event) => void) | null;
  onmessage: ((event: MessageEvent<string>) => void) | null;
  onerror: ((event: Event) => void) | null;
  close: () => void;
};
type EventSourceCtor = new (url: string) => EventSourceLike;
type QueryInvalidator = Pick<QueryClient, "invalidateQueries">;

let status: StreamStatus = { connected: false, lastEventAt: null, lastErrorAt: null };
const listeners = new Set<StreamListener>();

export function getStreamStatus(): StreamStatus {
  return status;
}

export function subscribeStreamStatus(listener: StreamListener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function liveRefetchInterval(): number | false {
  return status.connected ? false : LIVE_STREAM_FALLBACK_MS;
}

function setStreamStatus(next: StreamStatus) {
  status = next;
  for (const listener of listeners) listener();
}

export function streamKeyToQueryKeys(key: string, project?: string): QueryKey[] {
  const [kind, id] = key.split(":", 2);
  const scoped = (name: string): QueryKey => project ? [name, project] : [name];
  const panelScoped = (...prefix: string[]): QueryKey => project === undefined
    ? prefix
    : scopedProjectQueryKey(prefix, project);
  switch (kind) {
    case "daemon":
      return [["daemon"]];
    case "projects":
      return [["projects"]];
    case "factory-operations":
      return [panelScoped("factory-operations")];
    case "needs":
      return [panelScoped("needs")];
    case "runs":
      return id && project ? [panelScoped("runs"), ["run", project, id]] : [panelScoped("runs"), ["run"]];
    case "tasks":
      return id && project
        ? [["tasks", project], ["task", project, id], panelScoped("needs"), ["projects"]]
        : [scoped("tasks"), ["task"], panelScoped("needs"), ["projects"]];
    case "epics":
      return [scoped("epics")];
    case "docs":
      return [scoped("docs"), project ? ["doc", project] : ["doc"]];
    case "waves":
      return [scoped("waves"), scoped("tasks"), panelScoped("needs")];
    case "gates":
      return [scoped("gates"), scoped("tasks"), panelScoped("needs")];
    case "evidence":
      return [scoped("evidence"), scoped("tasks")];
    case "decisions":
      return [scoped("decisions"), scoped("docs")];
    case "feedback":
      return [scoped("feedback")];
    case "attempts":
      return id && project
        ? [["attempts", project], panelScoped("runs"), ["run", project, id]]
        : [scoped("attempts"), panelScoped("runs"), ["run"]];
    case "review":
      return id === "batch"
        ? [panelScoped("needs"), scoped("tasks"), panelScoped("runs"), ["projects"], panelScoped("review", "batch")]
        : [];
    default:
      return [];
  }
}

export function invalidateStreamEvent(queryClient: QueryInvalidator, event: StreamEvent) {
  const seen = new Set<string>();
  for (const key of event.keys) {
    for (const queryKey of streamKeyToQueryKeys(key, event.project)) {
      const signature = JSON.stringify(queryKey);
      if (seen.has(signature)) continue;
      seen.add(signature);
      void queryClient.invalidateQueries({ queryKey, exact: false });
    }
  }
}

export function connectLiveStream(
  queryClient: QueryInvalidator,
  options: { enabled?: boolean; EventSourceImpl?: EventSourceCtor; url?: string; now?: () => number; debounceMs?: number } = {},
): () => void {
  if (options.enabled === false) return () => {};
  const EventSourceImpl = options.EventSourceImpl ?? globalThis.EventSource;
  if (!EventSourceImpl) return () => {};

  const now = options.now ?? (() => Date.now());
  const debounceMs = options.debounceMs ?? STREAM_INVALIDATION_DEBOUNCE_MS;
  const pending = new Map<string, QueryKey>();
  let invalidationTimer: ReturnType<typeof setTimeout> | null = null;
  const flushInvalidations = () => {
    invalidationTimer = null;
    for (const queryKey of pending.values()) {
      void queryClient.invalidateQueries({ queryKey, exact: false });
    }
    pending.clear();
  };
  const enqueueInvalidation = (event: StreamEvent) => {
    if (debounceMs <= 0) {
      invalidateStreamEvent(queryClient, event);
      return;
    }
    for (const key of event.keys) {
      for (const queryKey of streamKeyToQueryKeys(key, event.project)) {
        pending.set(JSON.stringify(queryKey), queryKey);
      }
    }
    if (invalidationTimer === null) {
      invalidationTimer = setTimeout(flushInvalidations, debounceMs);
    }
  };
  const source = new EventSourceImpl(options.url ?? "/api/stream");
  source.onopen = () => {
    setStreamStatus({ ...status, connected: true });
    invalidateStreamEvent(queryClient, {
      kind: "stream_open",
      keys: ["daemon", "projects", "factory-operations", "needs", "runs", "tasks", "epics", "docs", "waves", "gates", "evidence", "decisions", "feedback", "attempts", "review:batch"],
    });
  };
  source.onmessage = (message) => {
    let event: StreamEvent;
    try {
      event = JSON.parse(message.data) as StreamEvent;
    } catch {
      // A malformed event must not kill the reconnect loop; the fallback
      // refetch interval remains the convergence path.
      setStreamStatus({ connected: true, lastEventAt: status.lastEventAt, lastErrorAt: now() });
      return;
    }
    setStreamStatus({ connected: true, lastEventAt: now(), lastErrorAt: status.lastErrorAt });
    if (event.kind === "stream_replay_miss" || event.replay_miss === true) {
      // The broker could not replay the complete cursor window. Invalidate a
      // full summary surface so reconnect converges to authoritative state.
      invalidateStreamEvent(queryClient, {
        kind: "stream_replay_miss",
        keys: ["daemon", "projects", "needs", "runs", "tasks", "epics", "docs", "waves", "gates", "evidence", "decisions", "feedback", "attempts", "review:batch"],
        project: event.project,
      });
    }
    enqueueInvalidation(event);
  };
  source.onerror = () => {
    setStreamStatus({ ...status, connected: false, lastErrorAt: now() });
    invalidateStreamEvent(queryClient, {
      kind: "stream_error",
      keys: ["daemon", "projects"],
    });
  };
  return () => {
    if (invalidationTimer !== null) clearTimeout(invalidationTimer);
    pending.clear();
    source.close();
    setStreamStatus({ ...status, connected: false });
  };
}

export function connectProjectAttention(
  projectId: string,
  options: { enabled?: boolean; EventSourceImpl?: EventSourceCtor } = {},
): () => void {
  if (options.enabled === false || projectId.trim() === "") return () => {};
  const EventSourceImpl = options.EventSourceImpl ?? globalThis.EventSource;
  if (!EventSourceImpl) return () => {};
  const source = new EventSourceImpl(`/api/stream?project=${encodeURIComponent(projectId)}`);
  return () => source.close();
}

export function formatStreamAge(at: number | null, now = Date.now()): string {
  if (at === null) return "no events yet";
  const seconds = Math.max(0, Math.floor((now - at) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ago`;
}
