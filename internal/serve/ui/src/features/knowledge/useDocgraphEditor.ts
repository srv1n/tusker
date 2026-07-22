/*
  Editor state for one corpus document (SRV-T-0002).

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
  banner: SaveBanner;
  save: () => void;
  reload: () => void;
  dismissBanner: () => void;
}

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

  // Baseline is the editor's serialization of the loaded body; body dirtiness is
  // measured against it (not the raw file), so tiptap-markdown's list-marker
  // normalization never counts as an edit. `null` until the editor has emitted
  // its first serialization, so an unopened doc is never dirty. This is written
  // ONLY by onBodyReady — the body editor is re-keyed per subject/rev, so a fresh
  // baseline is captured on every reload without a parent effect that could race
  // (and clobber) the child's onReady.
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
  }, [doc.subject, doc.rev]);

  const save = useSaveDocgraphDoc(projectId, doc.subject);

  const onBodyReady = useCallback((markdown: string) => {
    baseline.current = markdown;
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

  const doSave = useCallback(() => {
    if (!dirty || save.isPending) return;
    const payload: DocgraphSavePayload = { base_rev: doc.rev };
    if (bodyDirty) payload.body = body;
    if (headerDirty) payload.header = mergedHeader;
    save.mutate(payload, {
      onSuccess: (data) => {
        // The saved body is now the on-disk truth; advance the baseline so the
        // control reads clean immediately, before the re-keyed editor remounts.
        baseline.current = body;
        setBanner({ type: "saved", warnings: data.warnings ?? [] });
      },
      onError: (err) => {
        if (err instanceof DocSaveError && err.conflict) {
          setBanner({ type: "conflict", currentRev: err.conflict.current_rev });
        } else if (err instanceof DocSaveError && err.defects) {
          setBanner({ type: "defects", defects: err.defects });
        } else {
          setBanner({ type: "error", message: err instanceof Error ? err.message : String(err) });
        }
      },
    });
  }, [dirty, save, doc.rev, bodyDirty, headerDirty, body, mergedHeader]);

  const reload = useCallback(() => {
    if (dirty && !window.confirm("Discard your unsaved changes and reload the latest version?")) {
      return;
    }
    setBanner({ type: "none" });
    refetch();
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
    saving: save.isPending,
    banner,
    save: doSave,
    reload,
    dismissBanner: () => setBanner({ type: "none" }),
  };
}
