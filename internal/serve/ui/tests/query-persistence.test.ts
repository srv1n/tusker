import { expect, test } from "bun:test";
import { QueryClient } from "@tanstack/react-query";
import {
  isStartupQueryKey,
  restoreStartupQueryCache,
  saveStartupQueryCache,
  STARTUP_QUERY_CACHE_KEY,
  STARTUP_QUERY_CACHE_MAX_AGE_MS,
} from "../src/lib/queryPersistence";

class MemoryStorage {
  values = new Map<string, string>();

  getItem(key: string) {
    return this.values.get(key) ?? null;
  }

  setItem(key: string, value: string) {
    this.values.set(key, value);
  }

  removeItem(key: string) {
    this.values.delete(key);
  }
}

test("startup persistence allows bounded main-screen reads only", () => {
  expect(isStartupQueryKey(["projects"])).toBe(true);
  expect(isStartupQueryKey(["needs", "tusker"])).toBe(true);
  expect(isStartupQueryKey(["tasks", "tusker"])).toBe(true);
  expect(isStartupQueryKey(["doc", "tusker", "private.md"])).toBe(false);
  expect(isStartupQueryKey(["docgraph", "tusker"])).toBe(false);
  expect(isStartupQueryKey(["mutation", "redrive"])).toBe(false);
});

test("startup query data restores immediately and is marked for live revalidation", async () => {
  const storage = new MemoryStorage();
  const source = new QueryClient();
  source.setQueryData(["projects"], [{ id: "tusker", name: "Tusker" }]);
  source.setQueryData(["needs", "tusker"], [{ id: "need-1" }]);
  source.setQueryData(["doc", "tusker", "private.md"], { body: "do not persist" });

  expect(saveStartupQueryCache(source, storage, () => 1_000)).toBe(true);
  const serialized = storage.getItem(STARTUP_QUERY_CACHE_KEY) ?? "";
  expect(serialized).toContain("need-1");
  expect(serialized).not.toContain("do not persist");

  const restored = new QueryClient();
  expect(restoreStartupQueryCache(restored, storage, () => 2_000)).toBe(true);
  await Promise.resolve();

  expect(restored.getQueryData(["projects"])).toEqual([{ id: "tusker", name: "Tusker" }]);
  expect(restored.getQueryData(["needs", "tusker"])).toEqual([{ id: "need-1" }]);
  expect(restored.getQueryState(["projects"])?.isInvalidated).toBe(true);
  expect(restored.getQueryData(["doc", "tusker", "private.md"])).toBeUndefined();
});

test("expired startup data is discarded instead of becoming durable state", () => {
  const storage = new MemoryStorage();
  const source = new QueryClient();
  source.setQueryData(["projects"], [{ id: "old" }]);
  saveStartupQueryCache(source, storage, () => 1_000);

  const restored = new QueryClient();
  expect(
    restoreStartupQueryCache(
      restored,
      storage,
      () => 1_000 + STARTUP_QUERY_CACHE_MAX_AGE_MS + 1,
    ),
  ).toBe(false);
  expect(storage.getItem(STARTUP_QUERY_CACHE_KEY)).toBeNull();
  expect(restored.getQueryData(["projects"])).toBeUndefined();
});
