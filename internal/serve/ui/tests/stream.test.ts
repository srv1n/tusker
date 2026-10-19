import { expect, test } from "bun:test";
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import {
  LIVE_STREAM_FALLBACK_MS,
  connectLiveStream,
  getStreamStatus,
  invalidateStreamEvent,
  liveRefetchInterval,
  streamKeyToQueryKeys,
} from "../src/lib/stream";
import { qk } from "../src/lib/queries";

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent<string>) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }

  open() {
    this.onopen?.({} as Event);
  }

  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>);
  }

  error() {
    this.onerror?.({} as Event);
  }

  close() {
    this.closed = true;
  }
}

function recorder() {
  const invalidations: unknown[] = [];
  return {
    invalidations,
    client: {
      invalidateQueries(args: unknown) {
        invalidations.push(args);
        return Promise.resolve();
      },
    },
  };
}

test("stream keys map to live query invalidations", () => {
  expect(streamKeyToQueryKeys("tasks:AGX-T-0005", "tusker")).toEqual([
    ["tasks", "tusker"],
    ["task", "tusker", "AGX-T-0005"],
    // c5a5da52 invalidates wave lists when a member task changes.
    ["wave-list", "tusker"],
    qk.needs("tusker"),
    ["projects"],
  ]);
  expect(streamKeyToQueryKeys("runs:AGX-T-0005", "tusker")).toEqual([qk.runs("tusker"), ["run", "tusker", "AGX-T-0005"]]);
  expect(streamKeyToQueryKeys("docs", "tusker")).toEqual([
    ["docs", "tusker"],
    ["doc", "tusker"],
    ["docgraph", "tusker"],
    ["docgraph", "doc", "tusker"],
  ]);
  expect(streamKeyToQueryKeys("review:batch", "tusker")).toEqual([qk.needs("tusker"), ["tasks", "tusker"], qk.runs("tusker"), ["projects"], qk.reviewBatch("tusker"), ["wave-review", "tusker"]]);

  const { client, invalidations } = recorder();
  invalidateStreamEvent(client, {
    kind: "task_status_change",
    keys: ["tasks:AGX-T-0005", "tasks:AGX-T-0005", "runs"],
    project: "tusker",
  });
  expect(invalidations).toEqual([
    { queryKey: ["tasks", "tusker"], exact: false },
    { queryKey: ["task", "tusker", "AGX-T-0005"], exact: false },
    { queryKey: ["wave-list", "tusker"], exact: false },
    { queryKey: qk.needs("tusker"), exact: false },
    { queryKey: ["projects"], exact: false },
    { queryKey: qk.runs("tusker"), exact: false },
    { queryKey: ["run"], exact: false },
    { queryKey: ["processes"], exact: false },
  ]);
});

test("stream connection disables fast polling, falls back on error, and recovers on reconnect", () => {
  FakeEventSource.instances = [];
  let now = 1_000;
  const { client, invalidations } = recorder();
  const disconnect = connectLiveStream(client, {
    EventSourceImpl: FakeEventSource,
    now: () => now,
    debounceMs: 0,
  });
  const source = FakeEventSource.instances[0];

  expect(source.url).toBe("/api/stream");
  source.open();
  expect(getStreamStatus().connected).toBe(true);
  expect(liveRefetchInterval()).toBe(false);

  source.message({ kind: "lease_transition", keys: ["runs:SRV-T-0008"], project: "tusker" });
  expect(getStreamStatus().lastEventAt).toBe(1_000);
  expect(invalidations).toContainEqual({ queryKey: ["run", "tusker", "SRV-T-0008"], exact: false });
  const taskInvalidationsBeforeError = invalidations.filter(
    (entry) => JSON.stringify(entry) === JSON.stringify({ queryKey: ["tasks"], exact: false }),
  ).length;

  now = 2_000;
  source.error();
  expect(getStreamStatus().connected).toBe(false);
  expect(liveRefetchInterval()).toBe(LIVE_STREAM_FALLBACK_MS);
  expect(invalidations.slice(-3)).toEqual([
    { queryKey: ["daemon"], exact: false },
    { queryKey: ["projects"], exact: false },
    { queryKey: ["processes"], exact: false },
  ]);
  expect(
    invalidations.filter((entry) => JSON.stringify(entry) === JSON.stringify({ queryKey: ["tasks"], exact: false }))
      .length,
  ).toBe(taskInvalidationsBeforeError);
  expect(source.closed).toBe(false);

  source.open();
  expect(getStreamStatus().connected).toBe(true);
  expect(liveRefetchInterval()).toBe(false);

  disconnect();
  expect(source.closed).toBe(true);
});

test("stream connection debounces and deduplicates burst invalidations", async () => {
  FakeEventSource.instances = [];
  const { client, invalidations } = recorder();
  const disconnect = connectLiveStream(client, {
    EventSourceImpl: FakeEventSource,
    debounceMs: 20,
  });
  const source = FakeEventSource.instances[0];
  source.message({ kind: "task_status_change", keys: ["tasks:APP-T-0001"], project: "app" });
  source.message({ kind: "projection_refreshed", keys: ["tasks", "needs"], project: "app" });
  expect(invalidations).toHaveLength(0);
  await Bun.sleep(35);
  expect(invalidations.filter((entry) => JSON.stringify(entry) === JSON.stringify({ queryKey: ["tasks", "app"], exact: false }))).toHaveLength(1);
  expect(invalidations.filter((entry) => JSON.stringify(entry) === JSON.stringify({ queryKey: qk.needs("app"), exact: false }))).toHaveLength(1);
  disconnect();
});

test("literal project all invalidates only its scoped panel caches", () => {
  expect(qk.needs()).not.toEqual(qk.needs("all"));
  expect(qk.runs()).not.toEqual(qk.runs("all"));
  expect(qk.reviewBatch()).not.toEqual(qk.reviewBatch("all"));

  const projectKeys = streamKeyToQueryKeys("review:batch", "all");
  expect(projectKeys).toContainEqual(qk.needs("all"));
  expect(projectKeys).toContainEqual(qk.runs("all"));
  expect(projectKeys).toContainEqual(qk.reviewBatch("all"));
  expect(projectKeys).not.toContainEqual(qk.needs());
  expect(projectKeys).not.toContainEqual(qk.runs());
  expect(projectKeys).not.toContainEqual(qk.reviewBatch());

  const { client, invalidations } = recorder();
  invalidateStreamEvent(client, { kind: "review_changed", keys: ["review:batch"], project: "all" });
  expect(invalidations).toContainEqual({ queryKey: qk.reviewBatch("all"), exact: false });
  expect(invalidations).not.toContainEqual({ queryKey: qk.reviewBatch(), exact: false });
});

test("unscoped stream events retain family-wide invalidation for warm project caches", () => {
  expect(streamKeyToQueryKeys("needs")).toEqual([["needs"]]);
  expect(streamKeyToQueryKeys("runs")).toEqual([["runs"], ["run"]]);
  expect(streamKeyToQueryKeys("review:batch")).toEqual([
    ["needs"],
    ["tasks"],
    ["runs"],
    ["projects"],
    ["review", "batch"],
    ["wave-review"],
  ]);
});

test("live updates use one shared unfiltered subscription, not a connection per project", () => {
  // real-work-ui-acceptance: one shared subscription pattern. The unfiltered
  // /api/stream channel already carries every project-scoped event, so a
  // second per-project connection would double broker clients without ever
  // reading its messages into the query cache.
  FakeEventSource.instances = [];
  const { client, invalidations } = recorder();
  const disconnect = connectLiveStream(client, {
    EventSourceImpl: FakeEventSource,
    debounceMs: 0,
  });
  expect(FakeEventSource.instances).toHaveLength(1);
  const source = FakeEventSource.instances[0];
  expect(source.url).toBe("/api/stream");
  expect(source.url).not.toContain("project=");

  // Project-scoped traffic still invalidates project-scoped queries through
  // the shared channel.
  source.message({ kind: "lease_transition", keys: ["runs:SRV-T-0008"], project: "tusker" });
  expect(invalidations).toContainEqual({ queryKey: ["run", "tusker", "SRV-T-0008"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: qk.runs("tusker"), exact: false });

  disconnect();
  expect(source.closed).toBe(true);
  expect(FakeEventSource.instances).toHaveLength(1);
});

test("hidden pages and native panels stop live reads and reconcile on resume without replacing the stream", async () => {
  const savedDocument = globalThis.document;
  const savedWindow = globalThis.window;
  const document = Object.assign(new EventTarget(), { visibilityState: "visible" }) as unknown as Document;
  const window = Object.assign(new EventTarget(), { tuskerShell: { isVisible: true } }) as unknown as Window;
  globalThis.document = document;
  globalThis.window = window;
  FakeEventSource.instances = [];
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  let reads = 0;
  const observer = new QueryObserver(client, { queryKey: ["tasks", "app"], queryFn: async () => ++reads });
  const unsubscribe = observer.subscribe(() => {});
  const disconnect = connectLiveStream(client, { EventSourceImpl: FakeEventSource, debounceMs: 10 });
  try {
    const source = FakeEventSource.instances[0]!;
    source.open();
    await Bun.sleep(20);
    const initialReads = reads;
    expect(initialReads).toBeGreaterThan(0);

    source.message({ id: 7, kind: "task_status_change", keys: ["tasks:T-1"], project: "app" });
    Object.defineProperty(document, "visibilityState", { configurable: true, value: "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    source.message({ id: 8, kind: "task_status_change", keys: ["tasks:T-2"], project: "app" });
    source.error();
    await Bun.sleep(25);
    expect(reads).toBe(initialReads);
    expect(liveRefetchInterval()).toBe(false);
    expect(getStreamStatus().connected).toBe(false);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
    await Bun.sleep(20);
    expect(reads).toBe(initialReads + 1);
    expect(liveRefetchInterval()).toBe(LIVE_STREAM_FALLBACK_MS);
    expect(getStreamStatus().connected).toBe(false);
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(source.closed).toBe(false);

    source.open();
    await Bun.sleep(20);
    const connectedReads = reads;
    window.tuskerShell!.isVisible = false;
    window.dispatchEvent(new Event("tusker-shell-visibility"));
    source.message({ id: 9, kind: "task_status_change", keys: ["tasks:T-3"], project: "app" });
    await Bun.sleep(25);
    expect(reads).toBe(connectedReads);
    window.tuskerShell!.isVisible = true;
    window.dispatchEvent(new Event("tusker-shell-visibility"));
    await Bun.sleep(20);
    expect(reads).toBe(connectedReads + 1);
    expect(liveRefetchInterval()).toBe(false);
    expect(getStreamStatus().connected).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(1);
  } finally {
    disconnect();
    unsubscribe();
    client.clear();
    globalThis.document = savedDocument;
    globalThis.window = savedWindow;
  }
});

test("runtime refreshes update state capsules and summary counters without refetching notes", () => {
  const { client, invalidations } = recorder();
  invalidateStreamEvent(client, { kind: "runtime_projection_refreshed", keys: ["runs", "needs", "attempts"], project: "app" });
  expect(invalidations).toContainEqual({ queryKey: ["tasks", "app"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["task", "app"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["waves", "app"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["wave", "app"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["wave-list", "app"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["wave-review", "app"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["projects"], exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["daemon"], exact: false });
  for (const queryKey of [["epics", "app"], ["attempt", "app"], ["agent-access-approvals", "app"], qk.reviewBatch("app"), qk.factoryOperations("app"), qk.reviewBatch(), qk.factoryOperations(), qk.needs(), qk.runs(), ["executions", "app"], qk.executionInbox("app"), ["executions", "timeline", "app"]]) {
    expect(invalidations).toContainEqual({ queryKey, exact: false });
  }
  expect(invalidations).not.toContainEqual({ queryKey: ["docs", "app"], exact: false });
  expect(invalidations).not.toContainEqual({ queryKey: ["waves"], exact: false });
});

test("a runtime refresh that changed only needs refetches needs and counters, not task or wave state", () => {
  const { client, invalidations } = recorder();
  invalidateStreamEvent(client, { kind: "runtime_projection_refreshed", keys: ["needs"], project: "app" });
  expect(invalidations).toContainEqual({ queryKey: qk.needs("app"), exact: false });
  expect(invalidations).toContainEqual({ queryKey: ["projects"], exact: false });
  for (const queryKey of [["tasks", "app"], ["waves", "app"], ["wave-list", "app"], ["daemon"], ["processes"], qk.runs("app")]) {
    expect(invalidations).not.toContainEqual({ queryKey, exact: false });
  }
});

test("connection and replay reconciliation refresh runtime detail families", () => {
  const { client, invalidations } = recorder();
  FakeEventSource.instances = [];
  const disconnect = connectLiveStream(client, { EventSourceImpl: FakeEventSource, debounceMs: 0 });
  try {
    const source = FakeEventSource.instances[0]!;
    for (const reconcile of [() => source.open(), () => source.message({ kind: "stream_replay_miss", keys: [] })]) {
      invalidations.length = 0;
      reconcile();
      for (const queryKey of [["attempt"], ["agent-access-approvals"], ["executions"], ["factory-operations"]]) {
        expect(invalidations).toContainEqual({ queryKey, exact: false });
      }
    }
  } finally {
    disconnect();
  }
});
