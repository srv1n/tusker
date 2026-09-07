/*
  Module-level store for the docs explorer rail.

  The three knowledge routes are separate code-split screens, so the rail
  remounts on every navigation between list / reader / graph. Keeping its
  collapse state (and the narrow-viewport open/closed toggle) in a plain
  module-level store — read through useSyncExternalStore — lets that state
  survive the remount without any router change or new dependency.

  Folders are expanded by default; the store holds only the set of *collapsed*
  folder ids. Filtering never mutates this set, so clearing a filter restores
  exactly the collapse state the operator left behind.
*/

import { useSyncExternalStore } from "react";

export interface TreeStorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export interface PersistedTreeState {
  collapsedIds: string[];
  railOpen: boolean;
}

export const TREE_STORAGE_KEY = "tusker.documents.tree.v1";

function browserStorage(): TreeStorageLike | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function readTreeState(storage: TreeStorageLike | null): PersistedTreeState {
  if (!storage) return { collapsedIds: [], railOpen: false };
  try {
    const raw = storage.getItem(TREE_STORAGE_KEY);
    if (!raw) return { collapsedIds: [], railOpen: false };
    const parsed = JSON.parse(raw) as { collapsedIds?: unknown; railOpen?: unknown };
    return {
      collapsedIds: Array.isArray(parsed.collapsedIds)
        ? parsed.collapsedIds.filter((id): id is string => typeof id === "string").slice(0, 1000)
        : [],
      railOpen: parsed.railOpen === true,
    };
  } catch {
    return { collapsedIds: [], railOpen: false };
  }
}

export function writeTreeState(storage: TreeStorageLike | null, state: PersistedTreeState): boolean {
  if (!storage) return false;
  try {
    storage.setItem(TREE_STORAGE_KEY, JSON.stringify({
      collapsedIds: state.collapsedIds.slice(0, 1000),
      railOpen: state.railOpen,
    }));
    return true;
  } catch {
    return false;
  }
}

const persisted = readTreeState(browserStorage());
const collapsed = new Set(persisted.collapsedIds);
let railOpen = persisted.railOpen;
let version = 0;
const listeners = new Set<() => void>();

function persist(): void {
  writeTreeState(browserStorage(), { collapsedIds: [...collapsed], railOpen });
}

function emit(): void {
  version += 1;
  for (const l of listeners) l();
}

export const treeStore = {
  subscribe(l: () => void): () => void {
    listeners.add(l);
    return () => {
      listeners.delete(l);
    };
  },
  getVersion(): number {
    return version;
  },
  isCollapsed(id: string): boolean {
    return collapsed.has(id);
  },
  toggleFolder(id: string): void {
    if (collapsed.has(id)) collapsed.delete(id);
    else collapsed.add(id);
    persist();
    emit();
  },
  /** Ensure a doc's ancestor folders are open (called when a doc is opened). */
  expandAncestors(ids: string[]): void {
    let changed = false;
    for (const id of ids) if (collapsed.delete(id)) changed = true;
    if (changed) {
      persist();
      emit();
    }
  },
  isRailOpen(): boolean {
    return railOpen;
  },
  setRailOpen(open: boolean): void {
    if (railOpen === open) return;
    railOpen = open;
    persist();
    emit();
  },
  toggleRail(): void {
    railOpen = !railOpen;
    persist();
    emit();
  },
};

/** Subscribe a component to the store; returns the (stable) store handle. */
export function useTreeStore(): typeof treeStore {
  useSyncExternalStore(treeStore.subscribe, treeStore.getVersion, treeStore.getVersion);
  return treeStore;
}
