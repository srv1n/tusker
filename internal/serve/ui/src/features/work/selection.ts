/*
  Multi-select state for the Work views. The operator reviews a wave of
  completed tasks in one pass, so the board and table share one selection of
  task ids that survives a board↔table toggle. Pure React — no external deps.
*/

import { useCallback, useMemo, useState } from "react";

export interface Selection {
  /** Currently selected task ids. */
  selectedIds: ReadonlySet<string>;
  /** Number selected. */
  count: number;
  isSelected: (id: string) => boolean;
  /** Toggle a single id on/off. */
  toggle: (id: string) => void;
  /**
   * Add or remove a batch of ids in one update — powers the group / select-all
   * header checkboxes without a re-render per id.
   */
  setMany: (ids: string[], selected: boolean) => void;
  /** Drop every selection. */
  clear: () => void;
}

export function useSelection(): Selection {
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());

  const toggle = useCallback((id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const setMany = useCallback((ids: string[], selected: boolean) => {
    if (ids.length === 0) return;
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (selected) for (const id of ids) next.add(id);
      else for (const id of ids) next.delete(id);
      return next;
    });
  }, []);

  const clear = useCallback(() => {
    setSelectedIds((prev) => (prev.size === 0 ? prev : new Set<string>()));
  }, []);

  const isSelected = useCallback((id: string) => selectedIds.has(id), [selectedIds]);

  return useMemo<Selection>(
    () => ({ selectedIds, count: selectedIds.size, isSelected, toggle, setMany, clear }),
    [selectedIds, isSelected, toggle, setMany, clear],
  );
}
