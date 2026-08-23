/*
  Editor state for one corpus document.

  Holds the header draft (status / keywords / part_of) and the body draft
  (serialized markdown), tracks dirtiness against the loaded document, and drives
  the save. Dirtiness is measured against the editor's *baseline* serialization —
  the markdown it produces for the loaded body — so an unedited document is never
  dirty and never issues a save, even if tiptap-markdown normalizes list markers.

  The request sends `body` only when the body changed and `header` only when the
  header changed (a body-only save leaves the on-disk YAML bytes untouched). The
  fresh detail returned on success is written to the cache by the mutation hook;
  this hook only owns the transient save banner.
*/

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { DocSaveError } from "@/lib/api";
import { useSaveDocgraphDoc } from "@/lib/queries";
import type { DocgraphDocDetail, DocgraphSavePayload, DocSaveDefect } from "./types";

export type SaveBanner =
  | { type: "none" }
  | { type: "saved"; warnings: string[] }
  | { type: "conflict"; currentRev: string }
  | { type: "defects"; defects: DocSaveDefect[] }
  | { type: "error"; message: string };

function headerString(header: Record<string, unknown>, key: string): string {
  const v = header[key];
  return typeof v === "string" ? v : "";
}

function headerArray(header: Record<string, unknown>, key: string): string[] {
  const v = header[key];
  // YAML permits a scalar where a list is expected (keywords: overview);
  // treating it as [] would silently drop the field on the next header save.
  if (typeof v === "string" && v.trim() !== "") return [v.trim()];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

function sameList(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((v, i) => v === b[i]);
}

export interface KnowledgeDocEditor {
  status: string;
  setStatus: (v: string) => void;
  keywords: string[];
  addKeyword: (v: string) => void;
  removeKeyword: (v: string) => void;
  partOf: string;
  setPartOf: (v: string) => void;
  onBodyReady: (markdown: string) => void;
  onBodyChange: (markdown: string) => void;
  bodyDirty: boolean;
  headerDirty: boolean;
  dirty: boolean;
  saving: boolean;
  // Quiet autosave indicator for the toolbar; autosaves never raise `banner`.
  saveState: "idle" | "saving" | "saved";
  // Bumped when the editor must remount with fresh content (explicit reload, or
  // an external change arriving while clean). The body editor keys on this —
  // never on the doc rev, or our own autosaves would remount it mid-typing.
  reloadGen: number;
  banner: SaveBanner;
  save: () => void;
  reload: () => void;
  dismissBanner: () => void;
}

// Idle window before an autosave fires. Long enough not to interrupt typing,
// short enough to feel automatic; re-armed on every edit.
const AUTOSAVE_IDLE_MS = 1500;

export function useDocgraphEditor(
  doc: DocgraphDocDetail,
  projectId: string,
  refetch: () => void,
): KnowledgeDocEditor {
  const initStatus = headerString(doc.header, "status") || doc.status;
  const initKeywords = headerArray(doc.header, "keywords");
  const initPartOf = headerString(doc.header, "part_of");

  const [status, setStatus] = useState(initStatus);
  const [keywords, setKeywords] = useState<string[]>(initKeywords);
  const [partOf, setPartOf] = useState(initPartOf);
  const [banner, setBanner] = useState<SaveBanner>({ type: "none" });

  // Autosave surfaces only a quiet tick, never the full "saved" banner.
  // `autoSaving` is an in-flight auto save; `autoSavedAt` marks the last clean
  // autosave so the tick reads "Saved" until the next edit dirties the doc.
  const [autoSaving, setAutoSaving] = useState(false);
  const [autoSavedAt, setAutoSavedAt] = useState<number | null>(null);
  const [reloadGen, setReloadGen] = useState(0);

  // The revision the editor's CONTENT derives from — advanced only by our own
  // successful saves and by editor remounts, never by background refetches. The
  // doc query polls, so `doc.rev` can absorb an external edit's rev while the
  // editor still holds content based on the old file; saving with that live rev
  // would silently overwrite the external change. Pinning base_rev here turns
  // that save into an honest 409 instead.
  const baseRev = useRef(doc.rev);
  const liveRev = useRef(doc.rev);
  liveRev.current = doc.rev;

  // Baseline is the editor's serialization of the loaded body; body dirtiness is
  // measured against it (not the raw file), so tiptap-markdown's list-marker
  // normalization never counts as an edit. `null` until the editor has emitted
  // its first serialization, so an unopened doc is never dirty. This is written
  // ONLY by onBodyReady — the body editor is re-keyed per subject/reloadGen, so
  // a fresh baseline is captured on every reload without a parent effect that
  // could race (and clobber) the child's onReady.
  const baseline = useRef<string | null>(null);
  const [body, setBody] = useState<string>("");

  // Header drafts follow the loaded document. Only the header is reset here (it
  // has no child editor to race); after a save the fresh detail's values flow in
  // so a server-normalized header does not read as still-dirty.
  useEffect(() => {
    setStatus(initStatus);
    setKeywords(initKeywords);
    setPartOf(initPartOf);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [doc.subject, reloadGen]);

  const save = useSaveDocgraphDoc(projectId, doc.subject);

  const onBodyReady = useCallback((markdown: string) => {
    baseline.current = markdown;
    // A (re)mounted editor rendered whatever the cache held at that moment, so
    // its content now derives from the live rev.
    baseRev.current = liveRev.current;
    setBody(markdown);
  }, []);

  const onBodyChange = useCallback((markdown: string) => {
    setBody(markdown);
    setBanner((b) => (b.type === "saved" ? { type: "none" } : b));
  }, []);

  const bodyDirty = baseline.current !== null && body !== baseline.current;
  const headerDirty =
    status !== initStatus || partOf !== initPartOf || !sameList(keywords, initKeywords);
  const dirty = bodyDirty || headerDirty;

  const mergedHeader = useMemo(() => {
    const next: Record<string, unknown> = { ...doc.header };
    next.status = status;
    if (keywords.length > 0) next.keywords = keywords;
    else delete next.keywords;
    if (partOf.trim() !== "") next.part_of = partOf.trim();
    else delete next.part_of;
    return next;
  }, [doc.header, status, keywords, partOf]);

  const doSave = useCallback(
    (source: "auto" | "manual") => {
      if (!dirty || save.isPending) return;
      if (source === "auto") setAutoSaving(true);
      const payload: DocgraphSavePayload = { base_rev: baseRev.current };
      if (bodyDirty) payload.body = body;
      if (headerDirty) payload.header = mergedHeader;
      save.mutate(payload, {
        onSuccess: (data) => {
          // The saved body is now the on-disk truth: advance the baseline so the
          // control reads clean, and pin base_rev to the rev we just produced.
          baseline.current = body;
          baseRev.current = data.rev;
          if (source === "auto") {
            setAutoSaving(false);
            setAutoSavedAt(Date.now());
          } else {
            // Manual save keeps the full banner as the sole signal.
            setAutoSavedAt(null);
            setBanner({ type: "saved", warnings: data.warnings ?? [] });
          }
        },
        onError: (err) => {
          if (source === "auto") setAutoSaving(false);
          // A conflict/defects/error banner suspends autosave (see effect below)
          // so a refused save never loops; only a manual save or reload resumes.
          if (err instanceof DocSaveError && err.conflict) {
            setBanner({ type: "conflict", currentRev: err.conflict.current_rev });
          } else if (err instanceof DocSaveError && err.defects) {
            setBanner({ type: "defects", defects: err.defects });
          } else {
            setBanner({ type: "error", message: err instanceof Error ? err.message : String(err) });
          }
        },
      });
    },
    [dirty, save, bodyDirty, headerDirty, body, mergedHeader],
  );

  // Autosave: once the editor has been idle ~1.5s while dirty, run the save path
  // unprompted. `doSave` changes identity on every edit, so this effect re-arms
  // (debounces) per keystroke. Suspended whenever a conflict/defects/error
  // banner is up so a refused save never loops. Never fires mid-save; a keystroke
  // during an in-flight save keeps the doc dirty (baseline logic) and this
  // re-arms for the next cycle once `save.isPending` clears.
  const autosaveSuspended =
    banner.type === "conflict" || banner.type === "defects" || banner.type === "error";
  useEffect(() => {
    if (!dirty || save.isPending || autosaveSuspended) return;
    const t = setTimeout(() => doSave("auto"), AUTOSAVE_IDLE_MS);
    return () => clearTimeout(t);
  }, [dirty, save.isPending, autosaveSuspended, doSave]);

  // Toolbar tick: the in-flight auto save, then a quiet "Saved" while the doc
  // stays clean. Editing dirties the doc and drops it back to idle.
  const saveState: "idle" | "saving" | "saved" = autoSaving
    ? "saving"
    : autoSavedAt !== null && !dirty
      ? "saved"
      : "idle";

  // A clean editor follows external changes: when the polled query brings a rev
  // we did not produce and nothing is unsaved, remount to show it. A dirty
  // editor never remounts — its pinned base_rev turns the next save into a 409
  // (conflict banner) instead of losing either side's words. Our own saves are
  // never "external": baseRev advances synchronously in onSuccess, before the
  // seeded cache re-renders.
  useEffect(() => {
    if (!dirty && !save.isPending && doc.rev !== baseRev.current) {
      setReloadGen((g) => g + 1);
    }
  }, [dirty, save.isPending, doc.rev]);

  const reload = useCallback(() => {
    if (dirty && !window.confirm("Discard your unsaved changes and reload the latest version?")) {
      return;
    }
    setBanner({ type: "none" });
    // Remount only after the refetch lands — the remounting editor reads the
    // cache, which must already hold the fresh body.
    void Promise.resolve(refetch()).then(() => setReloadGen((g) => g + 1));
  }, [dirty, refetch]);

  return {
    status,
    setStatus,
    keywords,
    addKeyword: (v) => {
      const t = v.trim();
      setKeywords((ks) => (t === "" || ks.includes(t) ? ks : [...ks, t]));
    },
    removeKeyword: (v) => setKeywords((ks) => ks.filter((k) => k !== v)),
    partOf,
    setPartOf,
    onBodyReady,
    onBodyChange,
    bodyDirty,
    headerDirty,
    dirty,
    reloadGen,
    saving: save.isPending,
    saveState,
    banner,
    save: () => doSave("manual"),
    reload,
    dismissBanner: () => setBanner({ type: "none" }),
  };
}
