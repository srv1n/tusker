/*
  WUX-T-0003 — persistent project and wave navigator state.

  One small versioned record holding project order, expanded projects, the
  active project, the last internal route per project, and opaque per-project
  view state (written by integration through recordViewState — graph/view
  state lives in this same record, never in a second competing store).

  Persistence is local device/profile web storage keyed by stable project ID,
  never by title or branch. Blocked storage or malformed data must never
  prevent navigation: every reader is tolerant and every writer is guarded.
  All helpers are pure except read/writeNavigationState, which take an
  explicit storage handle so tests can pass fakes and the component can pass
  a guarded window.localStorage.
*/

export const NAVIGATION_STORAGE_KEY = "tusker.wux.navigation.v1";
export const NAVIGATION_STATE_VERSION = 1;

/** Maximum wave shortcuts shown per expanded project. */
export const MAX_WAVE_SHORTCUTS = 5;

export interface NavigationState {
  version: number;
  orderedProjectIds: string[];
  expandedProjectIds: string[];
  activeProjectId: string | null;
  lastPathByProject: Record<string, string>;
  /** Opaque per-project view state (Work view/filter, wave view, selection, graph transform). */
  viewStateByProject: Record<string, unknown>;
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
    expandedProjectIds: [],
    activeProjectId: null,
    lastPathByProject: {},
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
  const viewStateByProject: Record<string, unknown> = {};
  if (isRecord(raw.viewStateByProject)) {
    for (const [id, value] of Object.entries(raw.viewStateByProject)) {
      if (known.has(id) && value !== undefined) viewStateByProject[id] = value;
    }
  }
  return {
    version: NAVIGATION_STATE_VERSION,
    orderedProjectIds: ordered,
    expandedProjectIds: expanded,
    activeProjectId: active,
    lastPathByProject,
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

/** Order project records by saved order; unknown/new IDs append in source order. */
export function orderProjects<T extends { id: string }>(
  projects: T[],
  state: NavigationState,
): T[] {
  const rank = new Map(state.orderedProjectIds.map((id, index) => [id, index]));
  return [...projects].sort((a, b) => {
    const ra = rank.get(a.id) ?? Number.MAX_SAFE_INTEGER;
    const rb = rank.get(b.id) ?? Number.MAX_SAFE_INTEGER;
    return ra - rb;
  });
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
  return {
    ...state,
    activeProjectId: projectId,
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
    const deepProject = projectIdFromPath(deepLink);
    if (deepProject !== null && projectIds.includes(deepProject)) {
      return { projectId: deepProject, path: deepLink };
    }
    if (deepProject !== null && !projectIds.includes(deepProject)) {
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
    const savedProject = projectIdFromPath(saved);
    if (savedProject === null || savedProject === active || projectIds.includes(savedProject)) {
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
