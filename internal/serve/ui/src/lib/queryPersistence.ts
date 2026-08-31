import {
  dehydrate,
  hydrate,
  type Query,
  type QueryClient,
  type QueryKey,
} from "@tanstack/react-query";

export const STARTUP_QUERY_CACHE_KEY = "tusker.startup-queries.v1";
export const STARTUP_QUERY_CACHE_MAX_AGE_MS = 24 * 60 * 60 * 1_000;
export const STARTUP_QUERY_CACHE_MAX_CHARS = 1_500_000;

const STARTUP_QUERY_ROOTS = new Set([
  "projects",
  "factory-operations",
  "needs",
  "runs",
  "run",
  "review",
  "epics",
  "waves",
  "gates",
  "tasks",
]);

type StorageLike = Pick<Storage, "getItem" | "setItem" | "removeItem">;

interface PersistedStartupQueries {
  schema: 2;
  savedAt: number;
  clientState: ReturnType<typeof dehydrate>;
}

export function isStartupQueryKey(queryKey: QueryKey): boolean {
  return typeof queryKey[0] === "string" && STARTUP_QUERY_ROOTS.has(queryKey[0]);
}

function shouldPersistQuery(query: Query): boolean {
  return query.state.status === "success" && query.state.data !== undefined && isStartupQueryKey(query.queryKey);
}

function removeStoredCache(storage: StorageLike) {
  try {
    storage.removeItem(STARTUP_QUERY_CACHE_KEY);
  } catch {
    // Storage may be unavailable in a locked-down browser profile.
  }
}

export function saveStartupQueryCache(
  queryClient: QueryClient,
  storage: StorageLike,
  now = Date.now,
): boolean {
  const persisted: PersistedStartupQueries = {
    schema: 2,
    savedAt: now(),
    clientState: dehydrate(queryClient, { shouldDehydrateQuery: shouldPersistQuery }),
  };
  const serialized = JSON.stringify(persisted);
  if (serialized.length > STARTUP_QUERY_CACHE_MAX_CHARS) {
    removeStoredCache(storage);
    return false;
  }
  try {
    storage.setItem(STARTUP_QUERY_CACHE_KEY, serialized);
    return true;
  } catch {
    // A cache is an optimization, never a launch dependency.
    removeStoredCache(storage);
    return false;
  }
}

export function restoreStartupQueryCache(
  queryClient: QueryClient,
  storage: StorageLike,
  now = Date.now,
): boolean {
  let serialized: string | null;
  try {
    serialized = storage.getItem(STARTUP_QUERY_CACHE_KEY);
  } catch {
    return false;
  }
  if (!serialized) return false;
  if (serialized.length > STARTUP_QUERY_CACHE_MAX_CHARS) {
    removeStoredCache(storage);
    return false;
  }
  try {
    const persisted = JSON.parse(serialized) as Partial<PersistedStartupQueries>;
    if (
      persisted.schema !== 2 ||
      typeof persisted.savedAt !== "number" ||
      !persisted.clientState ||
      now() - persisted.savedAt > STARTUP_QUERY_CACHE_MAX_AGE_MS ||
      now() < persisted.savedAt
    ) {
      removeStoredCache(storage);
      return false;
    }
    hydrate(queryClient, persisted.clientState);
    // Hydration paints immediately. Mark those reads stale without starting
    // work yet; mounted screens will revalidate through their normal queries.
    void queryClient.invalidateQueries({
      predicate: (query) => isStartupQueryKey(query.queryKey),
      refetchType: "none",
    });
    return true;
  } catch {
    removeStoredCache(storage);
    return false;
  }
}

export function subscribeStartupQueryCache(
  queryClient: QueryClient,
  storage: StorageLike,
  delayMs = 250,
): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const unsubscribe = queryClient.getQueryCache().subscribe(() => {
    if (timer !== undefined) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = undefined;
      saveStartupQueryCache(queryClient, storage);
    }, delayMs);
  });
  return () => {
    if (timer !== undefined) {
      clearTimeout(timer);
      saveStartupQueryCache(queryClient, storage);
    }
    unsubscribe();
  };
}
