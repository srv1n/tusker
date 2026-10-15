import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { useSyncExternalStore } from "react";
import { scopedProjectQueryKey } from "@/lib/queryScope";

export const LIVE_STREAM_FALLBACK_MS = 45_000;
export const STREAM_INVALIDATION_DEBOUNCE_MS = 75;
const RECONCILE_TOPICS = ["daemon", "projects", "factory-operations", "needs", "runs", "tasks", "epics", "docs", "waves", "gates", "evidence", "decisions", "feedback", "attempts", "review:batch", "agent-access-approvals", "executions"];

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
  return status.connected || !isLiveViewVisible() ? false : LIVE_STREAM_FALLBACK_MS;
}

export function isLiveViewVisible(): boolean {
  return (typeof document === "undefined" || document.visibilityState !== "hidden") &&
    (typeof window === "undefined" || window.tuskerShell?.isVisible !== false);
}

// Interval callbacks alone do not reschedule when connectivity changes. The
// primitive snapshot rerenders queries only when their fallback changes.
export function useLiveRefetchInterval(): number | false {
  return useSyncExternalStore(subscribeStreamStatus, liveRefetchInterval, liveRefetchInterval);
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
        ? [["tasks", project], ["task", project, id], scoped("wave-list"), panelScoped("needs"), ["projects"]]
        : [scoped("tasks"), ["task"], scoped("wave-list"), panelScoped("needs"), ["projects"]];
    case "epics":
      return [scoped("epics")];
    case "docs":
      return [
        scoped("docs"),
        project ? ["doc", project] : ["doc"],
        scoped("docgraph"),
        project ? ["docgraph", "doc", project] : ["docgraph", "doc"],
      ];
    case "waves":
      return [scoped("waves"), scoped("wave-list"), project ? ["wave", project] : ["wave"], scoped("tasks"), panelScoped("needs")];
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
        ? [["attempts", project], scoped("attempt"), panelScoped("runs"), ["run", project, id]]
        : [scoped("attempts"), scoped("attempt"), panelScoped("runs"), ["run"]];
    case "agent-access-approvals":
      return [scoped("agent-access-approvals")];
    case "executions":
      return project
        ? [["executions", project], ["executions", "inbox", project], ["executions", "timeline", project], ["executions", "binding-preview", project]]
        : [["executions"]];
    case "review":
      return id === "batch"
        ? [panelScoped("needs"), scoped("tasks"), panelScoped("runs"), ["projects"], panelScoped("review", "batch"), project ? ["wave-review", project] : ["wave-review"]]
        : [];
    default:
      return [];
  }
}

function eventQueryKeys(event: StreamEvent): QueryKey[] {
  const keys = event.keys.flatMap((key) => streamKeyToQueryKeys(key, event.project));
  if (event.kind !== "runtime_projection_refreshed") return keys;
  // Runtime refreshes have narrow server topics: state reads and summary
  // counters still depend on them, without treating notes/docs as changed.
  const scope = event.project ? [event.project] : [];
  return [...keys,
    ...["epics", "review:batch", "agent-access-approvals", "executions", "factory-operations"].flatMap((key) => streamKeyToQueryKeys(key, event.project)),
    ...(event.project ? [["factory-operations"], ["needs"], ["runs"], ["review", "batch"]].map((prefix) => scopedProjectQueryKey(prefix)) : []),
    ["tasks", ...scope],
    event.task_id && event.project ? ["task", event.project, event.task_id] : ["task", ...scope],
    ["waves", ...scope], ["wave", ...scope], ["wave-list", ...scope], ["wave-review", ...scope],
    ["projects"], ["daemon"],
  ];
}

export function invalidateStreamEvent(queryClient: QueryInvalidator, event: StreamEvent) {
  const seen = new Set<string>();
  for (const queryKey of eventQueryKeys(event)) {
    const signature = JSON.stringify(queryKey);
    if (seen.has(signature)) continue;
    seen.add(signature);
    void queryClient.invalidateQueries({ queryKey, exact: false });
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
  let visible = isLiveViewVisible();
  let invalidationTimer: ReturnType<typeof setTimeout> | null = null;
  const clearInvalidations = () => {
    if (invalidationTimer !== null) clearTimeout(invalidationTimer);
    invalidationTimer = null;
    pending.clear();
  };
  const flushInvalidations = () => {
    invalidationTimer = null;
    if (!isLiveViewVisible()) { pending.clear(); return; }
    for (const queryKey of pending.values()) {
      void queryClient.invalidateQueries({ queryKey, exact: false });
    }
    pending.clear();
  };
  const enqueueInvalidation = (event: StreamEvent) => {
    if (!isLiveViewVisible()) return;
    if (debounceMs <= 0) {
      invalidateStreamEvent(queryClient, event);
      return;
    }
    for (const queryKey of eventQueryKeys(event)) {
      pending.set(JSON.stringify(queryKey), queryKey);
    }
    if (invalidationTimer === null) {
      invalidationTimer = setTimeout(flushInvalidations, debounceMs);
    }
  };
  const reconcile = () => {
    if (!isLiveViewVisible()) return;
    invalidateStreamEvent(queryClient, {
      kind: "stream_resume",
      keys: RECONCILE_TOPICS,
    });
  };
  const onVisibilityChange = () => {
    const nextVisible = isLiveViewVisible();
    if (nextVisible === visible) return;
    visible = nextVisible;
    clearInvalidations();
    setStreamStatus({ ...status });
    if (visible) reconcile();
  };
  // Keep the single EventSource alive so its native Last-Event-ID reconnect
  // cursor survives hidden tabs/panels. Hidden events never trigger reads.
  const source = new EventSourceImpl(options.url ?? "/api/stream");
  source.onopen = () => {
    setStreamStatus({ ...status, connected: true });
    reconcile();
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
    if (isLiveViewVisible() && (event.kind === "stream_replay_miss" || event.replay_miss === true)) {
      // The broker could not replay the complete cursor window. Invalidate a
      // full summary surface so reconnect converges to authoritative state.
      invalidateStreamEvent(queryClient, {
        kind: "stream_replay_miss",
        keys: RECONCILE_TOPICS,
        project: event.project,
      });
    }
    enqueueInvalidation(event);
  };
  source.onerror = () => {
    setStreamStatus({ ...status, connected: false, lastErrorAt: now() });
    if (isLiveViewVisible()) invalidateStreamEvent(queryClient, {
      kind: "stream_error",
      keys: ["daemon", "projects"],
    });
  };
  if (typeof document !== "undefined") document.addEventListener("visibilitychange", onVisibilityChange);
  if (typeof window !== "undefined") window.addEventListener("tusker-shell-visibility", onVisibilityChange);
  return () => {
    clearInvalidations();
    if (typeof document !== "undefined") document.removeEventListener("visibilitychange", onVisibilityChange);
    if (typeof window !== "undefined") window.removeEventListener("tusker-shell-visibility", onVisibilityChange);
    source.close();
    setStreamStatus({ ...status, connected: false });
  };
}

export function formatStreamAge(at: number | null, now = Date.now()): string {
  if (at === null) return "no events yet";
  const seconds = Math.max(0, Math.floor((now - at) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  return `${minutes}m ago`;
}
