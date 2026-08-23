/*
  Doc-graph API contract. The daemon serves the documentation
  corpus — canonical system docs, specs, and decision logs — plus the edges that
  connect them. These types mirror the pinned /api/docgraph shape exactly.
*/

/** The three corpus kinds. Distinct from the vault's DocKind — do not conflate. */
export type DocgraphKind = "canonical" | "spec" | "decision";

/** How one document relates to another. */
export type EdgeKind = "part_of" | "updates" | "decides_for" | "superseded_by";

/** How a backlink reaches this doc (wiki-reference or a typed relation). */
export type BacklinkVia = "wiki" | "part_of" | "updates" | "decides_for" | "superseded_by";

export interface DocgraphDoc {
  subject: string;
  title: string;
  path: string;
  kind: DocgraphKind;
  status: string;
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
  status: string;
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
