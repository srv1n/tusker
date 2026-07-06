/*
  Runtime configuration injected into the rich editor. Kept deliberately small
  and backend-agnostic: the editor never imports the mock or the router directly.
  The doc surfaces (DocReader, TaskContract) supply the vault resolver + an
  open-wikilink callback so navigation and link resolution stay swappable when
  the real daemon API lands.
*/

/** A wiki-link destination — the subset the editor needs to render + resolve. */
export interface WikilinkTargetLite {
  id: string;
  title: string;
  kind: string;
  /** Present when it resolves to a vault doc; absent → treat `id` as a task id. */
  path?: string;
}

/** Emitted when a reader clicks a resolved wiki-link. */
export interface WikilinkOpenPayload {
  target: string;
  anchor?: string;
  resolved?: WikilinkTargetLite;
}

export interface EditorRuntimeConfig {
  /** Synchronous resolver against the vault index (mock today, API later). */
  resolveWikilink: (id: string) => WikilinkTargetLite | undefined;
  /** Full index, for the `[[` autocomplete menu. */
  wikilinkIndex: WikilinkTargetLite[];
  /** Navigate to a resolved wiki-link target (routes via TanStack in the host). */
  onOpenWikilink?: (payload: WikilinkOpenPayload) => void;
  /** Placeholder shown in an empty editable doc. */
  placeholder?: string;
}
