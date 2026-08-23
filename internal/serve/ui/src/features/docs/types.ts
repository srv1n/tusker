/*
  Local view-model types for the Document / Library screen. These describe the
  editing, conflict, and validation surfaces the packet (§4.6) requires but the
  shared domain model doesn't yet carry. Everything the *real* API must supply
  is marked `// TODO(api)` at the mock that produces it (see ./mock.ts).
*/

import type { DocKind } from "@/types/domain";

/** Reader vs. editor. */
export type EditorPhase = "reading" | "editing";

/** A note-level validation finding, surfaced inline + in the summary strip. */
export interface ValidationIssue {
  severity: "error" | "warn";
  message: string;
  /** Slug / wikilink id an inline annotation can attach to, when known. */
  anchor?: string;
}

/** One inline run of diff text; `mark` tints add/del spans. */
export interface DiffSpan {
  text: string;
  mark?: "add" | "del";
}

/** The CAS-conflict payload: what an agent saved while the human was editing. */
export interface ConflictDiff {
  /** Agent and task label. */
  agent: string;
  /** e.g. "40s ago" */
  agoLabel: string;
  fromRev: string;
  toRev: string;
  /** Column header for the changed region, e.g. "Retention". */
  hunkLabel: string;
  yours: DiffSpan[];
  theirs: DiffSpan[];
  /** Full markdown of their revision — used by "take theirs". */
  theirMarkdown: string;
}

/** Resolution of a CAS conflict. */
export type ReconcileChoice = "take-theirs" | "keep-mine" | "copy-mine";

/** A wikilink destination for hover-preview + validation. */
export interface WikilinkTarget {
  id: string;
  title: string;
  kind: DocKind;
  /** Vault path when it resolves to a doc; absent → resolve as a task id. */
  path?: string;
}

/** One row of the merge-readiness / closeout checklist (Conductor pattern). */
export interface MergeCheck {
  id: string;
  label: string;
  detail: string;
  state: "pass" | "fail" | "pending";
}

/** Context for the approve-spec banner (packet §4.1 / §5 moment 5). */
export interface ApprovalContext {
  /** Downstream tasks blocked on this approval. */
  blocked: number;
}
