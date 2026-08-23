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

const collapsed = new Set<string>();
let railOpen = false;
let version = 0;
const listeners = new Set<() => void>();

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
    emit();
  },
  /** Ensure a doc's ancestor folders are open (called when a doc is opened). */
  expandAncestors(ids: string[]): void {
    let changed = false;
    for (const id of ids) if (collapsed.delete(id)) changed = true;
    if (changed) emit();
  },
  isRailOpen(): boolean {
    return railOpen;
  },
  setRailOpen(open: boolean): void {
    if (railOpen === open) return;
    railOpen = open;
    emit();
  },
  toggleRail(): void {
    railOpen = !railOpen;
    emit();
  },
};

/** Subscribe a component to the store; returns the (stable) store handle. */
export function useTreeStore(): typeof treeStore {
  useSyncExternalStore(treeStore.subscribe, treeStore.getVersion, treeStore.getVersion);
  return treeStore;
}
