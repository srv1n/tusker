/*
  The reader/editor state machine. Saves are mocked but the *shape* is the real
  one: CAS check first (a stale rev → conflict), then note-level validation
  (errors reject, warnings annotate), then a round-tripped success. No backend
  is touched — see ./mock.ts.

  TODO(api): replace save()/reconcile() with POST /api/docs/*path carrying the
  base rev; a 409 returns the conflict payload, a 422 the validation issues.
*/

import { useEffect, useMemo, useRef, useState } from "react";
import type { DocContent } from "@/types/domain";
import type { ConflictDiff, EditorPhase, ReconcileChoice, ValidationIssue } from "./types";
import { conflictFor, mockValidate } from "./mock";

export type EditorBanner =
  | { type: "none" }
  | { type: "saved"; rev: number }
  | { type: "conflict"; conflict: ConflictDiff }
  | { type: "invalid"; issues: ValidationIssue[] };

export interface DocEditor {
  phase: EditorPhase;
  /** Markdown currently persisted (shown in read mode). */
  content: string;
  /** Edit buffer. */
  draft: string;
  banner: EditorBanner;
  /** Last validation result — feeds inline annotations. */
  issues: ValidationIssue[];
  stateRev: number;
  isDirty: boolean;
  startEdit: () => void;
  cancelEdit: () => void;
  setDraft: (v: string) => void;
  save: () => void;
  reconcile: (choice: ReconcileChoice) => void;
  dismissBanner: () => void;
}

function initialRev(doc: DocContent): number {
  const f = doc.frontmatter.find((x) => x.key === "state_rev");
  const n = f ? Number(f.value) : NaN;
  return Number.isFinite(n) ? n : 1;
}

export function useDocEditor(doc: DocContent): DocEditor {
  const baseRev = useMemo(() => initialRev(doc), [doc]);

  const [phase, setPhase] = useState<EditorPhase>("reading");
  const [content, setContent] = useState(doc.markdown);
  const [draft, setDraftState] = useState(doc.markdown);
  const [banner, setBanner] = useState<EditorBanner>({ type: "none" });
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [stateRev, setStateRev] = useState(baseRev);
  const conflictArmed = useRef(conflictFor(doc.path) != null);

  // Reset the whole machine when the underlying document changes.
  useEffect(() => {
    setPhase("reading");
    setContent(doc.markdown);
    setDraftState(doc.markdown);
    setBanner({ type: "none" });
    setIssues([]);
    setStateRev(baseRev);
    conflictArmed.current = conflictFor(doc.path) != null;
  }, [doc.path, doc.markdown, baseRev]);

  const startEdit = () => {
    setDraftState(content);
    setPhase("editing");
    setBanner({ type: "none" });
  };

  const cancelEdit = () => {
    setDraftState(content);
    setPhase("reading");
    setBanner({ type: "none" });
    setIssues([]);
  };

  const setDraft = (v: string) => {
    setDraftState(v);
    if (banner.type === "saved") setBanner({ type: "none" });
  };

  const save = () => {
    // 1. CAS check — did an agent advance the note while we edited?
    if (conflictArmed.current) {
      const conflict = conflictFor(doc.path);
      if (conflict) {
        setBanner({ type: "conflict", conflict });
        return;
      }
    }
    // 2. Note-level validation.
    const found = mockValidate(draft);
    setIssues(found);
    if (found.some((i) => i.severity === "error")) {
      setBanner({ type: "invalid", issues: found });
      return;
    }
    // 3. Success — round-trip to markdown, bump rev, return to reading.
    const next = stateRev + 1;
    setContent(draft);
    setStateRev(next);
    setPhase("reading");
    setBanner({ type: "saved", rev: next });
  };

  const reconcile = (choice: ReconcileChoice) => {
    const conflict = conflictFor(doc.path);
    if (!conflict) return;
    if (choice === "copy-mine") {
      navigator.clipboard?.writeText(draft).catch(() => {});
      return; // keep the banner so the human can still take/keep afterwards
    }
    if (choice === "take-theirs") {
      setContent(conflict.theirMarkdown);
      setDraftState(conflict.theirMarkdown);
    }
    // "keep-mine" rebases onto their rev but preserves the draft as-is.
    setStateRev(Number(conflict.toRev));
    conflictArmed.current = false;
    setBanner({ type: "none" });
  };

  const dismissBanner = () => setBanner({ type: "none" });

  return {
    phase,
    content,
    draft,
    banner,
    issues,
    stateRev,
    isDirty: phase === "editing" && draft !== content,
    startEdit,
    cancelEdit,
    setDraft,
    save,
    reconcile,
    dismissBanner,
  };
}
