import { expect, test } from "bun:test";
import {
  LIVE_STREAM_FALLBACK_MS,
  connectLiveStream,
  getStreamStatus,
  invalidateStreamEvent,
  liveRefetchInterval,
  streamKeyToQueryKeys,
} from "../src/lib/stream";

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
    ["needs", "tusker"],
    ["projects"],
  ]);
  expect(streamKeyToQueryKeys("runs:AGX-T-0005", "tusker")).toEqual([["runs", "tusker"], ["run", "tusker", "AGX-T-0005"]]);
  expect(streamKeyToQueryKeys("review:batch", "tusker")).toEqual([["needs", "tusker"], ["tasks", "tusker"], ["runs", "tusker"], ["projects"], ["review", "batch", "tusker"]]);

  const { client, invalidations } = recorder();
  invalidateStreamEvent(client, {
    kind: "task_status_change",
    keys: ["tasks:AGX-T-0005", "tasks:AGX-T-0005", "runs"],
    project: "tusker",
  });
  expect(invalidations).toEqual([
    { queryKey: ["tasks", "tusker"], exact: false },
    { queryKey: ["task", "tusker", "AGX-T-0005"], exact: false },
    { queryKey: ["needs", "tusker"], exact: false },
    { queryKey: ["projects"], exact: false },
    { queryKey: ["runs", "tusker"], exact: false },
    { queryKey: ["run"], exact: false },
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
  expect(invalidations.slice(-2)).toEqual([
    { queryKey: ["daemon"], exact: false },
    { queryKey: ["projects"], exact: false },
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
  expect(invalidations.filter((entry) => JSON.stringify(entry) === JSON.stringify({ queryKey: ["needs", "app"], exact: false }))).toHaveLength(1);
  disconnect();
});
