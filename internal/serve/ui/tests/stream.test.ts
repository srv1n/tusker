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
  expect(streamKeyToQueryKeys("tasks:AGX-T-0005")).toEqual([
    ["tasks"],
    ["task", "AGX-T-0005"],
    ["needs"],
    ["projects"],
  ]);
  expect(streamKeyToQueryKeys("runs:AGX-T-0005")).toEqual([["runs"], ["run", "AGX-T-0005"]]);
  expect(streamKeyToQueryKeys("review:batch")).toEqual([["needs"], ["tasks"], ["runs"], ["projects"]]);

  const { client, invalidations } = recorder();
  invalidateStreamEvent(client, {
    kind: "task_status_change",
    keys: ["tasks:AGX-T-0005", "tasks:AGX-T-0005", "runs"],
  });
  expect(invalidations).toEqual([
    { queryKey: ["tasks"], exact: false },
    { queryKey: ["task", "AGX-T-0005"], exact: false },
    { queryKey: ["needs"], exact: false },
    { queryKey: ["projects"], exact: false },
    { queryKey: ["runs"], exact: false },
  ]);
});

test("stream connection disables fast polling, falls back on error, and recovers on reconnect", () => {
  FakeEventSource.instances = [];
  let now = 1_000;
  const { client, invalidations } = recorder();
  const disconnect = connectLiveStream(client, {
    EventSourceImpl: FakeEventSource,
    now: () => now,
  });
  const source = FakeEventSource.instances[0];

  expect(source.url).toBe("/api/stream");
  source.open();
  expect(getStreamStatus().connected).toBe(true);
  expect(liveRefetchInterval()).toBe(false);

  source.message({ kind: "lease_transition", keys: ["runs:SRV-T-0008"] });
  expect(getStreamStatus().lastEventAt).toBe(1_000);
  expect(invalidations).toContainEqual({ queryKey: ["run", "SRV-T-0008"], exact: false });

  now = 2_000;
  source.error();
  expect(getStreamStatus().connected).toBe(false);
  expect(liveRefetchInterval()).toBe(LIVE_STREAM_FALLBACK_MS);
  expect(source.closed).toBe(false);

  source.open();
  expect(getStreamStatus().connected).toBe(true);
  expect(liveRefetchInterval()).toBe(false);

  disconnect();
  expect(source.closed).toBe(true);
});

