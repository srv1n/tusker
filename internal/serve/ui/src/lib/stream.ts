import type { QueryClient, QueryKey } from "@tanstack/react-query";

export const LIVE_STREAM_FALLBACK_MS = 45_000;

export interface StreamEvent {
  kind: string;
  keys: string[];
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

export function streamKeyToQueryKeys(key: string): QueryKey[] {
  const [kind, id] = key.split(":", 2);
  switch (kind) {
    case "daemon":
      return [["daemon"]];
    case "projects":
      return [["projects"]];
    case "needs":
      return [["needs"]];
    case "runs":
      return id ? [["runs"], ["run", id]] : [["runs"]];
    case "tasks":
      return id ? [["tasks"], ["task", id], ["needs"], ["projects"]] : [["tasks"], ["needs"], ["projects"]];
    case "epics":
      return [["epics"]];
    case "docs":
      return [["docs"], ["doc"]];
    case "waves":
      return [["waves"], ["tasks"], ["needs"]];
    case "review":
      return id === "batch" ? [["needs"], ["tasks"], ["runs"], ["projects"]] : [];
    default:
      return [];
  }
}

export function invalidateStreamEvent(queryClient: QueryInvalidator, event: StreamEvent) {
  const seen = new Set<string>();
  for (const key of event.keys) {
    for (const queryKey of streamKeyToQueryKeys(key)) {
      const signature = JSON.stringify(queryKey);
      if (seen.has(signature)) continue;
      seen.add(signature);
      void queryClient.invalidateQueries({ queryKey, exact: false });
    }
  }
}

export function connectLiveStream(
  queryClient: QueryInvalidator,
  options: { enabled?: boolean; EventSourceImpl?: EventSourceCtor; url?: string; now?: () => number } = {},
): () => void {
  if (options.enabled === false) return () => {};
  const EventSourceImpl = options.EventSourceImpl ?? globalThis.EventSource;
  if (!EventSourceImpl) return () => {};

  const now = options.now ?? (() => Date.now());
  const source = new EventSourceImpl(options.url ?? "/api/stream");
  source.onopen = () => {
    setStreamStatus({ ...status, connected: true });
    invalidateStreamEvent(queryClient, {
      kind: "stream_open",
      keys: ["daemon", "projects", "needs", "runs", "tasks", "epics", "docs", "waves", "review:batch"],
    });
  };
  source.onmessage = (message) => {
    const event = JSON.parse(message.data) as StreamEvent;
    setStreamStatus({ connected: true, lastEventAt: now(), lastErrorAt: status.lastErrorAt });
    invalidateStreamEvent(queryClient, event);
  };
  source.onerror = () => {
    setStreamStatus({ ...status, connected: false, lastErrorAt: now() });
    invalidateStreamEvent(queryClient, {
      kind: "stream_error",
      keys: ["daemon", "projects", "needs", "runs", "tasks", "epics", "docs", "waves", "review:batch"],
    });
  };
  return () => {
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
