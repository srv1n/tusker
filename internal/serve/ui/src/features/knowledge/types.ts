/*
  Doc-graph API contract. The daemon serves the documentation
  corpus — portable system docs, proposals, and decisions — plus the edges that
  connect them. These types mirror the pinned /api/docgraph shape exactly.

  Kinds are portable: new documents declare `doc | proposal | decision`.
  `canonical` and `spec` remain as legacy compatibility spellings for
  documents that predate explicit kinds. Lifecycle (`status`) is
  kind-specific and never implies code conformance; `code_conformance` is
  the independent verification claim with its `last_verified` stamp and
  `describes` scope.
*/

/**
 * The corpus kinds. `doc`/`proposal`/`decision` are the portable S46 kinds;
 * `canonical`/`spec` are legacy compatibility spellings. Distinct from the
 * vault's DocKind — do not conflate.
 */
export type DocgraphKind = "doc" | "proposal" | "decision" | "canonical" | "spec";

/** Kind-specific lifecycle values by portable kind (legacy kinds included). */
export const LIFECYCLE_BY_KIND: Record<DocgraphKind, string[]> = {
  doc: ["current", "superseded"],
  canonical: ["current", "superseded"],
  proposal: ["proposed", "accepted", "implemented", "superseded"],
  spec: ["proposed", "accepted", "implemented", "superseded"],
  decision: ["proposed", "accepted", "superseded"],
};

/** Independent code-conformance states. Never inferred from lifecycle. */
export type CodeConformance = "unverified" | "matches" | "drift" | "not_applicable";

export const CONFORMANCE_VALUES: CodeConformance[] = ["unverified", "matches", "drift", "not_applicable"];

/** How one document relates to another in the six-kind semantic graph. */
export type EdgeKind = "part_of" | "updates" | "source" | "decides_for" | "superseded_by" | "link";

/** How a backlink reaches this doc (wiki-reference or a typed relation). */
export type BacklinkVia = "wiki" | "part_of" | "updates" | "source" | "decides_for" | "superseded_by" | "link";

export interface DocgraphGenerated {
  index: string;
  graph: string;
}

export interface DocgraphDoc {
  subject: string;
  title: string;
  path: string;
  kind: DocgraphKind;
  status: string;
  /** Kind-specific lifecycle mirror of status. */
  lifecycle: string;
  kind_source?: string;
  /** Independent verification claim — never inferred from lifecycle. */
  code_conformance: string;
  /** `YYYY-MM-DD @ <commit>` stamp backing a `matches` claim. */
  last_verified?: string;
  /** Stated verification scope backing a `matches` claim. */
  describes?: string[];
  keywords: string[];
  part_of?: string;
  updates?: string[];
  decides_for?: string;
  superseded_by?: string;
}

export interface DocgraphNode {
  subject: string;
  kind: DocgraphKind;
  path: string;
  title: string;
  status: string;
  lifecycle?: string;
  code_conformance?: string;
}

export interface DocgraphEdge {
  from: string;
  to: string;
  kind: EdgeKind;
}

export interface DocgraphIssue {
  code: string;
  path: string;
  message: string;
}

export interface DocgraphResponse {
  docs: DocgraphDoc[];
  graph: { nodes: DocgraphNode[]; edges: DocgraphEdge[]; graph_generated: boolean };
  issues: DocgraphIssue[];
  generated: DocgraphGenerated;
}

/** A `[[ref]]` occurrence in a doc body, with its resolution against the corpus. */
export interface DocLinkRef {
  ref: string;
  subject: string;
  path: string;
  resolved: boolean;
}

export interface DocBacklink {
  subject: string;
  title: string;
  path: string;
  kind: DocgraphKind;
  via: BacklinkVia;
}

export interface DocgraphDocDetail {
  subject: string;
  title: string;
  path: string;
  kind: DocgraphKind;
  kind_source?: string;
  status: string;
  /** Kind-specific lifecycle mirror of status. */
  lifecycle: string;
  /** Independent verification claim — never inferred from lifecycle. */
  code_conformance: string;
  /** `YYYY-MM-DD @ <commit>` stamp backing a `matches` claim. */
  last_verified?: string;
  /** Stated verification scope backing a `matches` claim. */
  describes?: string[];
  /** The requested reference when an old deep link forwarded here. */
  resolved_from?: string;
  /** Parsed front-matter. Rendered as a typed header card, never as raw YAML. */
  header: Record<string, unknown>;
  /** Markdown body with the front-matter already stripped. */
  body: string;
  links: DocLinkRef[];
  backlinks: DocBacklink[];
  successor: { subject: string; path: string } | null;
  /** sha256 hex of the on-disk file bytes — the optimistic-concurrency token. */
  rev: string;
}

/*
  Save contract. PUT /api/docgraph/doc?project=&subject=.
  Send `body` only when the body is dirty and `header` only when the header is
  dirty (at least one); a body-only save leaves the on-disk YAML bytes untouched.
*/
export interface DocgraphSavePayload {
  base_rev: string;
  body?: string;
  header?: Record<string, unknown>;
}

/** 200 response — a fresh {@link DocgraphDocDetail} plus any advisory warnings. */
export interface DocgraphSaveResponse extends DocgraphDocDetail {
  warnings: string[];
}

/** 409 body — the file changed on disk since it was loaded. */
export interface DocSaveConflict {
  error: string;
  code: "DOC_SAVE_CONFLICT";
  current_rev: string;
}

/** One named header-rule defect from a refused save (422). */
export interface DocSaveDefect {
  code: string;
  path: string;
  message: string;
}
