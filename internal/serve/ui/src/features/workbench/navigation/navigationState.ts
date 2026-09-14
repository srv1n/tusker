/*
  WUX-T-0015 — persistent project strip state.

  One small versioned record holding the saved project order, pinned IDs,
  cross-mount recency, the active project, the last internal route per project,
  and opaque per-project view state.

  Persistence is local device/profile web storage keyed by stable project ID,
  never by title or branch. Blocked storage or malformed data must never
  prevent navigation: every reader is tolerant and every writer is guarded.
  All helpers are pure except read/writeNavigationState, which take an
  explicit storage handle so tests can pass fakes and the component can pass
  a guarded window.localStorage.
*/

export const NAVIGATION_STORAGE_KEY = "tusker.wux.navigation.v1";
export const NAVIGATION_STATE_VERSION = 2;
export const NAVIGATION_CHANGED_EVENT = "tusker.navigation.changed";

export const PROJECT_ICON_NAMES = [
  "audio",
  "auto",
  "code",
  "database",
  "folder",
  "globe",
  "mic",
  "music",
  "package",
  "phone",
  "sparkles",
  "terminal",
  "text",
] as const;
export type ProjectIconName = (typeof PROJECT_ICON_NAMES)[number];

/** Maximum wave shortcuts shown per expanded project. */
export const MAX_WAVE_SHORTCUTS = 5;

export interface NavigationState {
  version: number;
  orderedProjectIds: string[];
  pinnedProjectIds: string[];
  recentProjectIds: string[];
  expandedProjectIds: string[];
  activeProjectId: string | null;
  lastPathByProject: Record<string, string>;
  projectIconById: Record<string, ProjectIconName>;
  /** Opaque per-project view state (Work view/filter, wave view, selection, graph transform). */
  viewStateByProject: Record<string, unknown>;
}

export interface WorkspaceScrollOffset {
  top: number;
  left: number;
  [key: string]: unknown;
}

export interface WorkspaceDocsState {
  contextOpen?: boolean;
  contextWidth?: number;
  scrollBySubject?: Record<string, WorkspaceScrollOffset>;
  [key: string]: unknown;
}

export interface WorkspaceWaveViewState {
  view?: "flow" | "results";
  scrollTop?: number;
  scrollLeft?: number;
  selectedTaskId?: string;
  [key: string]: unknown;
}

export interface WorkspaceWavesState {
  contextOpen?: boolean;
  contextWidth?: number;
  overviewQuery?: string;
  showCompleted?: boolean;
  overviewScrollTop?: number;
  byId?: Record<string, WorkspaceWaveViewState>;
  [key: string]: unknown;
}

export interface WorkspaceBoardState {
  mode?: "board" | "list";
  scrollTop?: number;
  scrollLeft?: number;
  selectedTaskId?: string;
  selectedTags?: string[];
  [key: string]: unknown;
}

export interface WorkspaceViewState {
  lastDocsPath?: string;
  docs?: WorkspaceDocsState;
  waves?: WorkspaceWavesState;
  board?: WorkspaceBoardState;
  [key: string]: unknown;
}

export interface WorkspaceDocsStatePatch {
  contextOpen?: boolean;
  contextWidth?: number;
  scrollBySubject?: Record<string, WorkspaceScrollOffset | null>;
  [key: string]: unknown;
}

export interface WorkspaceWavesStatePatch {
  contextOpen?: boolean;
  contextWidth?: number;
  overviewQuery?: string;
  showCompleted?: boolean;
  overviewScrollTop?: number;
  byId?: Record<string, Partial<WorkspaceWaveViewState> | null>;
  [key: string]: unknown;
}

export interface WorkspaceBoardStatePatch {
  mode?: "board" | "list";
  scrollTop?: number;
  scrollLeft?: number;
  selectedTaskId?: string;
  selectedTags?: string[];
  [key: string]: unknown;
}

export interface WorkspaceViewStatePatch {
  lastDocsPath?: string | null;
  docs?: WorkspaceDocsStatePatch | null;
  waves?: WorkspaceWavesStatePatch | null;
  board?: WorkspaceBoardStatePatch | null;
}

export interface StorageLike {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem?(key: string): void;
}

export function emptyNavigationState(): NavigationState {
  return {
    version: NAVIGATION_STATE_VERSION,
    orderedProjectIds: [],
    pinnedProjectIds: [],
    recentProjectIds: [],
    expandedProjectIds: [],
    activeProjectId: null,
    lastPathByProject: {},
    projectIconById: {},
    viewStateByProject: {},
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function knownIdList(value: unknown, projectIds: string[]): string[] {
  if (!Array.isArray(value)) return [];
  const known = new Set(projectIds);
  const seen = new Set<string>();
  const out: string[] = [];
  for (const entry of value) {
    if (typeof entry !== "string" || entry === "") continue;
    if (!known.has(entry) || seen.has(entry)) continue;
    seen.add(entry);
    out.push(entry);
  }
  return out;
}

export function isProjectIconName(value: unknown): value is ProjectIconName {
  return typeof value === "string" && (PROJECT_ICON_NAMES as readonly string[]).includes(value);
}

/**
 * Tolerant reader: unknown, duplicate or removed IDs are dropped; new
 * projects keep source order (appended by orderProjects); external or
 * malformed paths are ignored. Never throws.
 */
export function sanitizeNavigationState(raw: unknown, projectIds: string[]): NavigationState {
  const base = emptyNavigationState();
  if (!isRecord(raw)) return { ...base, orderedProjectIds: [...projectIds] };
  const known = new Set(projectIds);
  const ordered = knownIdList(raw.orderedProjectIds, projectIds);
  for (const id of projectIds) {
    if (!ordered.includes(id)) ordered.push(id);
  }
  const pinned = knownIdList(raw.pinnedProjectIds ?? raw.pinnedIds, projectIds);
  const recent = knownIdList(raw.recentProjectIds ?? raw.recentIds, projectIds);
  const expanded = knownIdList(raw.expandedProjectIds, projectIds);
  const active =
    typeof raw.activeProjectId === "string" && known.has(raw.activeProjectId)
      ? raw.activeProjectId
      : null;
  const lastPathByProject: Record<string, string> = {};
  if (isRecord(raw.lastPathByProject)) {
    for (const [id, path] of Object.entries(raw.lastPathByProject)) {
      if (known.has(id) && typeof path === "string" && isInternalPath(path)) {
        lastPathByProject[id] = path;
      }
    }
  }
  const projectIconById: Record<string, ProjectIconName> = {};
  if (isRecord(raw.projectIconById)) {
    for (const [id, value] of Object.entries(raw.projectIconById)) {
      if (known.has(id) && isProjectIconName(value)) projectIconById[id] = value;
    }
  }
  const viewStateByProject: Record<string, unknown> = {};
  if (isRecord(raw.viewStateByProject)) {
    for (const [id, value] of Object.entries(raw.viewStateByProject)) {
      // View state is also keyed by checkout route IDs. Keep it opaque so
      // hiding/removing a project does not silently erase its saved workspace.
      if (value !== undefined) viewStateByProject[id] = value;
    }
  }
  return {
    version: NAVIGATION_STATE_VERSION,
    orderedProjectIds: ordered,
    pinnedProjectIds: pinned,
    recentProjectIds: recent,
    expandedProjectIds: expanded,
    activeProjectId: active,
    lastPathByProject,
    projectIconById,
    viewStateByProject,
  };
}

/** Read persisted state; blocked storage or malformed data yields a fresh state. Never throws. */
export function readNavigationState(
  storage: StorageLike | null | undefined,
  projectIds: string[],
): NavigationState {
  if (!storage) return sanitizeNavigationState(null, projectIds);
  try {
    const raw = storage.getItem(NAVIGATION_STORAGE_KEY);
    if (!raw) return sanitizeNavigationState(null, projectIds);
    return sanitizeNavigationState(JSON.parse(raw) as unknown, projectIds);
  } catch {
    return sanitizeNavigationState(null, projectIds);
  }
}

/** Persist state; returns false when storage is unavailable. Never throws. */
export function writeNavigationState(
  storage: StorageLike | null | undefined,
  state: NavigationState,
): boolean {
  if (!storage) return false;
  try {
    storage.setItem(NAVIGATION_STORAGE_KEY, JSON.stringify(state));
    return true;
  } catch {
    return false;
  }
}

/**
 * Order only for a fresh shell mount. The rendered strip owns its order for
 * the lifetime of that mount so a click cannot reshuffle the targets below it.
 */
export function orderProjects<T extends { id: string }>(
  projects: T[],
  state: NavigationState,
): T[] {
  const pinRank = new Map(state.pinnedProjectIds.map((id, index) => [id, index]));
  const recentRank = new Map(state.recentProjectIds.map((id, index) => [id, index]));
  const savedRank = new Map(state.orderedProjectIds.map((id, index) => [id, index]));
  const sourceRank = new Map(projects.map((project, index) => [project.id, index]));
  return [...projects].sort((a, b) => {
    const pa = pinRank.get(a.id);
    const pb = pinRank.get(b.id);
    if (pa !== undefined || pb !== undefined) {
      if (pa === undefined) return 1;
      if (pb === undefined) return -1;
      return pa - pb;
    }
    const ra = recentRank.get(a.id);
    const rb = recentRank.get(b.id);
    if (ra !== undefined || rb !== undefined) {
      if (ra === undefined) return 1;
      if (rb === undefined) return -1;
      return ra - rb;
    }
    const sa = savedRank.get(a.id);
    const sb = savedRank.get(b.id);
    if (sa !== undefined || sb !== undefined) {
      if (sa === undefined) return 1;
      if (sb === undefined) return -1;
      if (sa !== sb) return sa - sb;
    }
    return (sourceRank.get(a.id) ?? 0) - (sourceRank.get(b.id) ?? 0);
  });
}

/** Toggle a stable project pin without changing the saved or recent order. */
export function setProjectPinned(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
  pinned: boolean,
): NavigationState {
  if (!projectIds.includes(projectId)) return state;
  const ids = state.pinnedProjectIds.filter((id) => id !== projectId && projectIds.includes(id));
  return {
    ...state,
    pinnedProjectIds: pinned ? [...ids, projectId] : ids,
  };
}

export function toggleProjectPinned(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
): NavigationState {
  return setProjectPinned(state, projectIds, projectId, !state.pinnedProjectIds.includes(projectId));
}

/** Store a local presentation choice without writing project or repository config. */
export function setProjectIcon(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
  icon: ProjectIconName,
): NavigationState {
  if (!projectIds.includes(projectId)) return state;
  const projectIconById = { ...state.projectIconById };
  if (icon === "auto") delete projectIconById[projectId];
  else projectIconById[projectId] = icon;
  return { ...state, projectIconById };
}

/** Move a project within the saved pin order; unpinned projects are unaffected. */
export function movePinnedProject(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
  direction: -1 | 1,
): ReorderResult {
  const pinned = state.pinnedProjectIds.filter((id) => projectIds.includes(id));
  const from = pinned.indexOf(projectId);
  if (from < 0) return { state, movedId: null, position: 0, total: pinned.length };
  const to = Math.min(pinned.length - 1, Math.max(0, from + direction));
  if (to === from) return { state, movedId: projectId, position: from + 1, total: pinned.length };
  const next = [...pinned];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved!);
  return {
    state: { ...state, pinnedProjectIds: next },
    movedId: projectId,
    position: to + 1,
    total: next.length,
  };
}

/** Replace the saved order; unknown/duplicate IDs are dropped, missing ones appended. */
export function setProjectOrder(
  state: NavigationState,
  projectIds: string[],
  nextOrder: string[],
): NavigationState {
  const known = new Set(projectIds);
  const seen = new Set<string>();
  const ordered: string[] = [];
  for (const id of nextOrder) {
    if (known.has(id) && !seen.has(id)) {
      seen.add(id);
      ordered.push(id);
    }
  }
  for (const id of projectIds) {
    if (!seen.has(id)) ordered.push(id);
  }
  return { ...state, orderedProjectIds: ordered };
}

export interface ReorderResult {
  state: NavigationState;
  movedId: string | null;
  /** 1-based position for screen-reader announcements. */
  position: number;
  total: number;
}

function orderWithNewAppended(state: NavigationState, projectIds: string[]): string[] {
  const ordered = state.orderedProjectIds.filter((id) => projectIds.includes(id));
  for (const id of projectIds) {
    if (!ordered.includes(id)) ordered.push(id);
  }
  return ordered;
}

/** Move one project up/down; drag and keyboard moves share this path so ordering is identical. */
export function moveProject(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
  direction: -1 | 1,
): ReorderResult {
  const ordered = orderWithNewAppended(state, projectIds);
  const from = ordered.indexOf(projectId);
  if (from < 0) {
    return { state, movedId: null, position: 0, total: ordered.length };
  }
  const to = Math.min(ordered.length - 1, Math.max(0, from + direction));
  if (to === from) {
    return { state, movedId: projectId, position: from + 1, total: ordered.length };
  }
  const next = [...ordered];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved!);
  return {
    state: { ...state, orderedProjectIds: next },
    movedId: projectId,
    position: to + 1,
    total: next.length,
  };
}

/** Drag reorder by visible index; same ordering path as moveProject. */
export function reorderProject(
  state: NavigationState,
  projectIds: string[],
  fromIndex: number,
  toIndex: number,
): ReorderResult {
  const ordered = orderWithNewAppended(state, projectIds);
  const from = Math.min(ordered.length - 1, Math.max(0, fromIndex));
  const to = Math.min(ordered.length - 1, Math.max(0, toIndex));
  const movedId = ordered[from] ?? null;
  if (movedId === null || from === to) {
    return { state, movedId, position: from + 1, total: ordered.length };
  }
  const next = [...ordered];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved!);
  return {
    state: { ...state, orderedProjectIds: next },
    movedId,
    position: to + 1,
    total: next.length,
  };
}

export function toggleProjectExpanded(state: NavigationState, projectId: string): NavigationState {
  const expanded = state.expandedProjectIds.includes(projectId)
    ? state.expandedProjectIds.filter((id) => id !== projectId)
    : [...state.expandedProjectIds, projectId];
  return { ...state, expandedProjectIds: expanded };
}

/** Replace the expanded set; unknown IDs are dropped so removed projects never linger. */
export function setExpandedProjects(
  state: NavigationState,
  projectIds: string[],
  expanded: string[],
): NavigationState {
  const known = new Set(projectIds);
  return {
    ...state,
    expandedProjectIds: expanded.filter((id) => known.has(id)),
  };
}

/** Record a project visit: marks it active and remembers its last internal route. */
export function recordProjectVisit(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
  path: string,
): NavigationState {
  if (!projectIds.includes(projectId) || !isInternalPath(path)) return state;
  let recent = state.recentProjectIds.filter((id) => projectIds.includes(id));
  if (state.activeProjectId !== projectId) {
    recent = [projectId, ...recent.filter((id) => id !== projectId)];
  }
  return {
    ...state,
    activeProjectId: projectId,
    recentProjectIds: recent,
    lastPathByProject: { ...state.lastPathByProject, [projectId]: path },
  };
}

/** Store opaque per-project view state supplied by integration. */
export function recordViewState(
  state: NavigationState,
  projectIds: string[],
  projectId: string,
  value: unknown,
): NavigationState {
  if (!projectIds.includes(projectId)) return state;
  return {
    ...state,
    viewStateByProject: { ...state.viewStateByProject, [projectId]: value },
  };
}

const MAX_WORKSPACE_VISITS = 20;
const MIN_CONTEXT_WIDTH = 200;
const MAX_CONTEXT_WIDTH = 360;

function hasOwn(value: object, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function finiteNonnegative(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}

function contextWidth(value: unknown): value is number {
  return finiteNonnegative(value) && value >= MIN_CONTEXT_WIDTH && value <= MAX_CONTEXT_WIDTH;
}

function validDocsPath(path: unknown, routeProjectId: string): path is string {
  if (typeof path !== "string" || !routeProjectId || !isInternalPath(path)) return false;
  if (projectIdFromPath(path) !== routeProjectId) return false;
  return /^\/p\/[^/]+\/(?:docs|knowledge)(?:\/|$)/.test(path);
}

function copyUnknown(source: Record<string, unknown>, known: readonly string[]): Record<string, unknown> {
  const knownKeys = new Set(known);
  return Object.fromEntries(Object.entries(source).filter(([key]) => !knownKeys.has(key)));
}

function cappedEntries<T>(entries: Record<string, T>): Record<string, T> {
  const keys = Object.keys(entries);
  if (keys.length <= MAX_WORKSPACE_VISITS) return entries;
  return Object.fromEntries(keys.slice(-MAX_WORKSPACE_VISITS).map((key) => [key, entries[key]]));
}

function sanitizeScrollOffset(value: unknown): WorkspaceScrollOffset | null {
  if (!isRecord(value) || !finiteNonnegative(value.top) || !finiteNonnegative(value.left)) return null;
  return {
    ...copyUnknown(value, ["top", "left"]),
    top: value.top,
    left: value.left,
  };
}

function sanitizeScrollMap(value: unknown): Record<string, WorkspaceScrollOffset> | undefined {
  if (!isRecord(value)) return undefined;
  const entries: Record<string, WorkspaceScrollOffset> = {};
  for (const [key, offset] of Object.entries(value)) {
    if (!key) continue;
    const next = sanitizeScrollOffset(offset);
    if (next) entries[key] = next;
  }
  return cappedEntries(entries);
}

function sanitizeWaveView(value: unknown): WorkspaceWaveViewState | null {
  if (!isRecord(value)) return null;
  const next: WorkspaceWaveViewState = copyUnknown(value, ["view", "scrollTop", "scrollLeft", "selectedTaskId"]);
  if (value.view === "flow" || value.view === "results") next.view = value.view;
  if (finiteNonnegative(value.scrollTop)) next.scrollTop = value.scrollTop;
  if (finiteNonnegative(value.scrollLeft)) next.scrollLeft = value.scrollLeft;
  if (typeof value.selectedTaskId === "string" && value.selectedTaskId) next.selectedTaskId = value.selectedTaskId;
  return Object.keys(next).length > 0 ? next : null;
}

function sanitizeWaveMap(value: unknown): Record<string, WorkspaceWaveViewState> | undefined {
  if (!isRecord(value)) return undefined;
  const entries: Record<string, WorkspaceWaveViewState> = {};
  for (const [key, view] of Object.entries(value)) {
    if (!key) continue;
    const next = sanitizeWaveView(view);
    if (next) entries[key] = next;
  }
  return cappedEntries(entries);
}

function sanitizeDocsState(value: unknown): WorkspaceDocsState | undefined {
  if (!isRecord(value)) return undefined;
  const next: WorkspaceDocsState = copyUnknown(value, ["contextOpen", "contextWidth", "scrollBySubject"]);
  if (typeof value.contextOpen === "boolean") next.contextOpen = value.contextOpen;
  if (contextWidth(value.contextWidth)) next.contextWidth = value.contextWidth;
  const scrollBySubject = sanitizeScrollMap(value.scrollBySubject);
  if (scrollBySubject) next.scrollBySubject = scrollBySubject;
  return next;
}

function sanitizeWavesState(value: unknown): WorkspaceWavesState | undefined {
  if (!isRecord(value)) return undefined;
  const next: WorkspaceWavesState = copyUnknown(value, ["contextOpen", "contextWidth", "overviewQuery", "showCompleted", "overviewScrollTop", "byId"]);
  if (typeof value.contextOpen === "boolean") next.contextOpen = value.contextOpen;
  if (contextWidth(value.contextWidth)) next.contextWidth = value.contextWidth;
  if (typeof value.overviewQuery === "string") next.overviewQuery = value.overviewQuery;
  if (typeof value.showCompleted === "boolean") next.showCompleted = value.showCompleted;
  if (finiteNonnegative(value.overviewScrollTop)) next.overviewScrollTop = value.overviewScrollTop;
  const byId = sanitizeWaveMap(value.byId);
  if (byId) next.byId = byId;
  return next;
}

function sanitizeBoardState(value: unknown): WorkspaceBoardState | undefined {
  if (!isRecord(value)) return undefined;
  const next: WorkspaceBoardState = copyUnknown(value, ["mode", "scrollTop", "scrollLeft", "selectedTaskId", "selectedTags"]);
  if (value.mode === "board" || value.mode === "list") next.mode = value.mode;
  if (finiteNonnegative(value.scrollTop)) next.scrollTop = value.scrollTop;
  if (finiteNonnegative(value.scrollLeft)) next.scrollLeft = value.scrollLeft;
  if (typeof value.selectedTaskId === "string" && value.selectedTaskId) next.selectedTaskId = value.selectedTaskId;
  if (Array.isArray(value.selectedTags)) {
    next.selectedTags = [...new Set(value.selectedTags.filter((tag): tag is string => typeof tag === "string" && tag.length > 0))];
  }
  return next;
}

function sanitizeWorkspaceViewState(value: unknown, routeProjectId: string): WorkspaceViewState {
  if (!isRecord(value)) return {};
  const next: WorkspaceViewState = copyUnknown(value, ["lastDocsPath", "docs", "waves", "board"]);
  if (validDocsPath(value.lastDocsPath, routeProjectId)) next.lastDocsPath = value.lastDocsPath;
  const docs = sanitizeDocsState(value.docs);
  if (docs) next.docs = docs;
  const waves = sanitizeWavesState(value.waves);
  if (waves) next.waves = waves;
  const board = sanitizeBoardState(value.board);
  if (board) next.board = board;
  return next;
}

function mergeScrollMap(
  current: Record<string, WorkspaceScrollOffset> | undefined,
  patch: Record<string, WorkspaceScrollOffset | null> | undefined,
): Record<string, WorkspaceScrollOffset> | undefined {
  if (!patch) return current;
  const next = { ...(current ?? {}) };
  let changed = false;
  for (const [key, offset] of Object.entries(patch)) {
    if (!key) continue;
    const sanitized = offset === null ? null : sanitizeScrollOffset(offset);
    if (offset !== null && !sanitized) continue;
    delete next[key];
    if (sanitized) next[key] = sanitized;
    changed = true;
  }
  return changed ? cappedEntries(next) : current;
}

function mergeWaveMap(
  current: Record<string, WorkspaceWaveViewState> | undefined,
  patch: Record<string, Partial<WorkspaceWaveViewState> | null> | undefined,
): Record<string, WorkspaceWaveViewState> | undefined {
  if (!patch) return current;
  const next = { ...(current ?? {}) };
  let changed = false;
  for (const [key, value] of Object.entries(patch)) {
    if (!key) continue;
    if (value === null) {
      delete next[key];
      changed = true;
      continue;
    }
    if (!isRecord(value)) continue;
    const currentValue = current?.[key] ?? {};
    const merged = mergeWaveView(currentValue, value);
    if (!merged) continue;
    delete next[key];
    next[key] = merged;
    changed = true;
  }
  return changed ? cappedEntries(next) : current;
}

function mergeWaveView(
  current: WorkspaceWaveViewState,
  patch: Partial<WorkspaceWaveViewState>,
): WorkspaceWaveViewState | null {
  const next: WorkspaceWaveViewState = {
    ...current,
    ...copyUnknown(patch, ["view", "scrollTop", "scrollLeft", "selectedTaskId"]),
  };
  if (patch.view === "flow" || patch.view === "results") next.view = patch.view;
  if (finiteNonnegative(patch.scrollTop)) next.scrollTop = patch.scrollTop;
  if (finiteNonnegative(patch.scrollLeft)) next.scrollLeft = patch.scrollLeft;
  if (typeof patch.selectedTaskId === "string" && patch.selectedTaskId) next.selectedTaskId = patch.selectedTaskId;
  return Object.keys(next).length > 0 ? next : null;
}

function mergeDocsState(current: WorkspaceDocsState | undefined, patch: WorkspaceDocsStatePatch): WorkspaceDocsState {
  const next: WorkspaceDocsState = { ...(current ?? {}), ...copyUnknown(patch, ["contextOpen", "contextWidth", "scrollBySubject"]) };
  if (typeof patch.contextOpen === "boolean") next.contextOpen = patch.contextOpen;
  if (contextWidth(patch.contextWidth)) next.contextWidth = patch.contextWidth;
  const scrollBySubject = mergeScrollMap(current?.scrollBySubject, patch.scrollBySubject);
  if (scrollBySubject) next.scrollBySubject = scrollBySubject;
  return next;
}

function mergeWavesState(current: WorkspaceWavesState | undefined, patch: WorkspaceWavesStatePatch): WorkspaceWavesState {
  const next: WorkspaceWavesState = { ...(current ?? {}), ...copyUnknown(patch, ["contextOpen", "contextWidth", "overviewQuery", "showCompleted", "overviewScrollTop", "byId"]) };
  if (typeof patch.contextOpen === "boolean") next.contextOpen = patch.contextOpen;
  if (contextWidth(patch.contextWidth)) next.contextWidth = patch.contextWidth;
  if (typeof patch.overviewQuery === "string") next.overviewQuery = patch.overviewQuery;
  if (typeof patch.showCompleted === "boolean") next.showCompleted = patch.showCompleted;
  if (finiteNonnegative(patch.overviewScrollTop)) next.overviewScrollTop = patch.overviewScrollTop;
  const byId = mergeWaveMap(current?.byId, patch.byId);
  if (byId) next.byId = byId;
  return next;
}

function mergeBoardState(current: WorkspaceBoardState | undefined, patch: WorkspaceBoardStatePatch): WorkspaceBoardState {
  const next: WorkspaceBoardState = { ...(current ?? {}), ...copyUnknown(patch, ["mode", "scrollTop", "scrollLeft", "selectedTaskId", "selectedTags"]) };
  if (patch.mode === "board" || patch.mode === "list") next.mode = patch.mode;
  if (finiteNonnegative(patch.scrollTop)) next.scrollTop = patch.scrollTop;
  if (finiteNonnegative(patch.scrollLeft)) next.scrollLeft = patch.scrollLeft;
  if (typeof patch.selectedTaskId === "string" && patch.selectedTaskId) next.selectedTaskId = patch.selectedTaskId;
  if (Array.isArray(patch.selectedTags)) {
    next.selectedTags = [...new Set(patch.selectedTags.filter((tag): tag is string => typeof tag === "string" && tag.length > 0))];
  }
  return next;
}

/** Read the typed workspace namespace for one checkout without mutating state. */
export function getWorkspaceViewState(state: NavigationState, routeProjectId: string): WorkspaceViewState {
  const projectView = state.viewStateByProject[routeProjectId];
  return sanitizeWorkspaceViewState(isRecord(projectView) ? projectView.workspace : undefined, routeProjectId);
}

/**
 * Merge workspace preferences into the existing opaque per-checkout record.
 * Persistence remains the mounted root's responsibility: pass the returned
 * state to writeNavigationState through that existing authority.
 */
export function updateWorkspaceViewState(
  state: NavigationState,
  routeProjectId: string,
  patch: WorkspaceViewStatePatch,
): NavigationState {
  if (!routeProjectId || !isRecord(patch)) return state;
  const rawProjectView = state.viewStateByProject[routeProjectId];
  const projectView = isRecord(rawProjectView) ? rawProjectView : { value: rawProjectView };
  const current = sanitizeWorkspaceViewState(projectView.workspace, routeProjectId);
  const next: WorkspaceViewState = { ...current };

  if (hasOwn(patch, "lastDocsPath")) {
    if (patch.lastDocsPath === null) delete next.lastDocsPath;
    else if (validDocsPath(patch.lastDocsPath, routeProjectId)) next.lastDocsPath = patch.lastDocsPath;
  }
  if (patch.docs === null) delete next.docs;
  else if (isRecord(patch.docs)) next.docs = mergeDocsState(current.docs, patch.docs as WorkspaceDocsStatePatch);
  if (patch.waves === null) delete next.waves;
  else if (isRecord(patch.waves)) next.waves = mergeWavesState(current.waves, patch.waves as WorkspaceWavesStatePatch);
  if (patch.board === null) delete next.board;
  else if (isRecord(patch.board)) next.board = mergeBoardState(current.board, patch.board as WorkspaceBoardStatePatch);

  if (Object.keys(next).length === 0 && !hasOwn(projectView, "workspace")) return state;
  return {
    ...state,
    viewStateByProject: {
      ...state.viewStateByProject,
      [routeProjectId]: { ...projectView, workspace: next },
    },
  };
}

/** Mark a document as visited while retaining at most the last 20 offsets. */
export function recordWorkspaceDocumentVisit(
  state: NavigationState,
  routeProjectId: string,
  subject: string,
  scroll?: WorkspaceScrollOffset,
): NavigationState {
  if (!subject) return state;
  const current = getWorkspaceViewState(state, routeProjectId).docs?.scrollBySubject?.[subject];
  return updateWorkspaceViewState(state, routeProjectId, {
    docs: { scrollBySubject: { [subject]: scroll ?? current ?? { top: 0, left: 0 } } },
  });
}

/** Mark a wave as visited while retaining at most the last 20 wave views. */
export function recordWorkspaceWaveVisit(
  state: NavigationState,
  routeProjectId: string,
  waveId: string,
  view: Partial<WorkspaceWaveViewState> = {},
): NavigationState {
  if (!waveId) return state;
  return updateWorkspaceViewState(state, routeProjectId, { waves: { byId: { [waveId]: view } } });
}

/** Only same-origin app paths restore; external URLs and protocol tricks never do. */
export function isInternalPath(path: string): boolean {
  return path.startsWith("/") && !path.startsWith("//") && !path.includes("://");
}

/** The project overview (Work) destination — always opens fresh, clearing detail state. */
export function projectWorkPath(projectId: string): string {
  return `/p/${encodeURIComponent(projectId)}/waves`;
}

/** The project landing destination — restores that project's last screen. */
export function projectLandingPath(projectId: string): string {
  return `/p/${encodeURIComponent(projectId)}`;
}

const PROJECT_PATH_RE = /^\/p\/([^/]+)/;

/** Extract the project ID from an internal /p/:id… path, or null. */
export function projectIdFromPath(path: string): string | null {
  if (!isInternalPath(path)) return null;
  const match = PROJECT_PATH_RE.exec(path);
  if (!match?.[1]) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return null;
  }
}

export interface NavigationTarget {
  projectId: string | null;
  path: string;
  /** Brief human-readable explanation when a fallback was applied. */
  notice?: string;
}

export interface ResolveOptions {
  /** Explicit deep link — always wins over saved navigation when internal. */
  deepLink?: string | null;
  /** Route IDs such as checkout aliases mapped to their logical project ID. */
  routeOwners?: Readonly<Record<string, string>>;
}

function routeOwner(path: string, projectIds: string[], routeOwners?: Readonly<Record<string, string>>): string | null {
  const routeId = projectIdFromPath(path);
  if (routeId === null) return null;
  if (projectIds.includes(routeId)) return routeId;
  const owner = routeOwners?.[routeId];
  return owner && projectIds.includes(owner) ? owner : null;
}

/**
 * Single-pass restore resolution. Explicit deep links win; otherwise the
 * active project's last screen restores. Removed entities fall back to the
 * project Work view, then the first available project, then the Add-project
 * empty state. Pure and loop-free: it returns one target, never navigates.
 */
export function resolveNavigationTarget(
  state: NavigationState,
  projectIds: string[],
  options: ResolveOptions = {},
): NavigationTarget {
  const first = projectIds[0] ?? null;
  const deepLink = options.deepLink ?? null;

  if (deepLink !== null && isInternalPath(deepLink)) {
    const deepProject = routeOwner(deepLink, projectIds, options.routeOwners);
    if (deepProject !== null) {
      return { projectId: deepProject, path: deepLink };
    }
    if (projectIdFromPath(deepLink) !== null) {
      if (first !== null) {
        return {
          projectId: first,
          path: projectWorkPath(first),
          notice: "That project is no longer available.",
        };
      }
      return { projectId: null, path: "/", notice: "That project is no longer available." };
    }
    // App-level deep link outside /p/… (e.g. "/" or "/settings"): honor as-is.
    return { projectId: state.activeProjectId !== null && projectIds.includes(state.activeProjectId) ? state.activeProjectId : first, path: deepLink };
  }

  const active =
    state.activeProjectId !== null && projectIds.includes(state.activeProjectId)
      ? state.activeProjectId
      : first;
  if (active === null) {
    return { projectId: null, path: "/", notice: "Add a project to get started." };
  }
  const saved = state.lastPathByProject[active];
  if (saved !== undefined && isInternalPath(saved)) {
    const savedProject = routeOwner(saved, projectIds, options.routeOwners);
    if (savedProject === active) {
      return { projectId: active, path: saved };
    }
    // Saved route points at a removed project: fall back to this project's Work.
    return {
      projectId: active,
      path: projectWorkPath(active),
      notice: "The saved screen is gone; showing Work instead.",
    };
  }
  if (
    state.activeProjectId !== null &&
    !projectIds.includes(state.activeProjectId) &&
    state.activeProjectId !== active
  ) {
    return {
      projectId: active,
      path: projectWorkPath(active),
      notice: "That project is no longer available.",
    };
  }
  return { projectId: active, path: projectWorkPath(active) };
}

export type WaveSectionKind = "loading" | "empty" | "error" | "ready";

export interface WaveReadSnapshot {
  state: "loading" | "ready" | "error";
  error?: string;
}

/**
 * Keep loading, empty and failed wave reads distinct: a missing link list is
 * not proof of an empty project while its read is still in flight.
 */
export function waveSectionKind(
  read: WaveReadSnapshot | undefined,
  linkCount: number,
): WaveSectionKind {
  if (!read || read.state === "loading") return "loading";
  if (read.state === "error") return "error";
  return linkCount === 0 ? "empty" : "ready";
}
