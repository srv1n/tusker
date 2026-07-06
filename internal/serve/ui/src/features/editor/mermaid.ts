/*
  Mermaid runtime — lazy, initialized-once, cached, theme-aware.

  Ported from cinta's BrainDump editor (bd* helpers in `TipTap/src/editor.js`)
  and adapted to this app's design tokens + the `sanitizeMermaidSvg` boundary.

  Design goals (see the editor slice contract):
    - The mermaid library is a SEPARATE async chunk: it is only pulled in the
      first time a diagram actually renders (`await import("mermaid")`), never in
      the initial bundle.
    - `mermaid.initialize()` runs exactly once per theme (module-scoped guard);
      when the app theme flips we re-initialize with fresh token colors.
    - Rendered SVG is scrubbed through `sanitizeMermaidSvg` BEFORE it is ever
      handed back to a view, and cached by a hash of (theme + source) so an
      identical diagram is never re-rendered.
    - Nothing here throws to its caller: every failure resolves to `{ error }`.
*/

import { sanitizeMermaidSvg } from "./sanitize";

/** The slice of the mermaid API we depend on (kept minimal + decoupled). */
interface MermaidApi {
  initialize(config: Record<string, unknown>): void;
  render(
    id: string,
    source: string,
  ): Promise<{ svg: string; bindFunctions?: (el: Element) => void }>;
}

/** Outcome of a render attempt. `svg` is already sanitized + safe to inject. */
export interface MermaidRenderResult {
  svg?: string;
  error?: string;
}

// --- FNV-1a 32-bit hash (source-key + theme-key) -----------------------------

function hashString(input: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < input.length; i += 1) {
    hash ^= input.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16);
}

// --- Lazy module load (separate chunk) ---------------------------------------

let mermaidModule: MermaidApi | null = null;
let mermaidModulePromise: Promise<MermaidApi> | null = null;

function loadMermaid(): Promise<MermaidApi> {
  if (mermaidModule) return Promise.resolve(mermaidModule);
  if (!mermaidModulePromise) {
    mermaidModulePromise = import("mermaid")
      .then((mod) => {
        const api = ((mod as { default?: unknown }).default ??
          mod) as unknown as MermaidApi;
        mermaidModule = api;
        return api;
      })
      .catch((error) => {
        // Allow a later diagram to retry the (possibly transient) chunk load.
        mermaidModulePromise = null;
        throw error;
      });
  }
  return mermaidModulePromise;
}

// --- Theme (derive mermaid variables from the live design tokens) ------------

function readToken(name: string, fallback: string): string {
  if (typeof document === "undefined") return fallback;
  try {
    const value = getComputedStyle(document.documentElement)
      .getPropertyValue(name)
      .trim();
    return value || fallback;
  } catch {
    return fallback;
  }
}

function prefersDark(): boolean {
  if (typeof document === "undefined") return false;
  const pinned = document.documentElement.getAttribute("data-theme");
  if (pinned === "dark") return true;
  if (pinned === "light") return false;
  try {
    return !!window.matchMedia?.("(prefers-color-scheme: dark)").matches;
  } catch {
    return false;
  }
}

interface MermaidTheme {
  key: string;
  config: Record<string, unknown>;
}

/** Build a mermaid `base`-theme config from the current design tokens. */
function currentTheme(): MermaidTheme {
  const dark = prefersDark();

  const ink = readToken("--color-ink", dark ? "#f0f0f2" : "#1a1a1a");
  const inkSoft = readToken("--color-ink-soft", dark ? "#d8d8dc" : "#2a2a2a");
  const muted = readToken("--color-muted", dark ? "#a2a2aa" : "#565656");
  const surface = readToken("--color-surface", dark ? "#0e0e11" : "#ffffff");
  const panel = readToken("--color-panel", dark ? "#16161a" : "#fafafa");
  const line = readToken("--color-line", dark ? "#2a2a31" : "#e6e6e6");
  const accent = readToken("--color-accent", dark ? "#8f7fea" : "#6b5ad1");
  const accentSoft = readToken(
    "--color-accent-soft",
    dark ? "#221f3a" : "#eeecfa",
  );
  const fontFamily = readToken(
    "--font-sans",
    '"Helvetica Neue", Helvetica, Arial, system-ui, sans-serif',
  );

  const themeVariables: Record<string, string | boolean> = {
    darkMode: dark,
    background: panel,
    fontFamily,
    fontSize: "14px",

    // Default (primary) node — reads like our neutral code slab.
    primaryColor: panel,
    primaryTextColor: ink,
    primaryBorderColor: line,
    secondaryColor: accentSoft,
    secondaryTextColor: ink,
    secondaryBorderColor: accent,
    tertiaryColor: surface,
    tertiaryTextColor: inkSoft,
    tertiaryBorderColor: line,

    lineColor: muted,
    textColor: inkSoft,
    titleColor: ink,

    // Flowchart
    mainBkg: panel,
    nodeBorder: line,
    nodeTextColor: ink,
    edgeLabelBackground: surface,
    clusterBkg: accentSoft,
    clusterBorder: line,

    // Sequence
    actorBkg: panel,
    actorBorder: line,
    actorTextColor: ink,
    actorLineColor: muted,
    signalColor: muted,
    signalTextColor: inkSoft,
    labelBoxBkgColor: panel,
    labelBoxBorderColor: line,
    labelTextColor: ink,
    loopTextColor: inkSoft,
    noteBkgColor: accentSoft,
    noteBorderColor: accent,
    noteTextColor: ink,
    activationBkgColor: accentSoft,
    activationBorderColor: accent,

    // Class / state
    classText: ink,
  };

  return {
    key: `${dark ? "d" : "l"}:${hashString(JSON.stringify(themeVariables))}`,
    config: {
      startOnLoad: false,
      securityLevel: "strict",
      theme: "base",
      themeVariables,
      // Node labels as native SVG <text>, never <foreignObject>. CRUCIAL:
      // htmlLabels must be false at BOTH the top level AND per-diagram. If only
      // `flowchart.htmlLabels` is false, the top-level default (true) still wins
      // and mermaid v11 emits empty labels / stray foreignObject. Consistent
      // `false` → clean SVG text that the (foreignObject-forbidding) sanitizer
      // in sanitize.ts passes through untouched.
      htmlLabels: false,
      flowchart: { htmlLabels: false },
      sequence: { htmlLabels: false },
    },
  };
}

// --- Init-once (per theme) guard ---------------------------------------------

let initializedThemeKey = "";

async function ensureInitialized(theme: MermaidTheme): Promise<MermaidApi> {
  const api = await loadMermaid();
  if (
    typeof api.initialize !== "function" ||
    typeof api.render !== "function"
  ) {
    throw new Error("mermaid module unavailable");
  }
  if (initializedThemeKey !== theme.key) {
    api.initialize(theme.config);
    initializedThemeKey = theme.key;
  }
  return api;
}

// --- Render + cache ----------------------------------------------------------

const svgCache = new Map<string, MermaidRenderResult>();
let renderSeq = 0;

function errorText(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string") return error;
  try {
    return String((error as { message?: unknown })?.message ?? error);
  } catch {
    return "render failed";
  }
}

/** A stable cache key for the current theme + source. Exposed for callers that
 *  want to check theme-scoped identity (the NodeView keys its effect on it). */
export function mermaidCacheKey(source: string): string {
  return `${currentTheme().key}:${hashString(source.trim())}`;
}

/**
 * Render mermaid `source` to a sanitized SVG string. Cache hits return
 * synchronously-resolved. Never rejects — failures resolve to `{ error }` and
 * the caller is expected to fall back to showing the raw source.
 */
export async function renderMermaid(
  source: string,
): Promise<MermaidRenderResult> {
  const trimmed = source.trim();
  if (!trimmed) return { error: "empty diagram" };

  const theme = currentTheme();
  const cacheKey = `${theme.key}:${hashString(trimmed)}`;
  const cached = svgCache.get(cacheKey);
  if (cached) return cached;

  const renderId = `tk-mermaid-${hashString(trimmed)}-${(renderSeq += 1).toString(36)}`;
  try {
    const api = await ensureInitialized(theme);
    const { svg } = await api.render(renderId, trimmed);
    const safe = sanitizeMermaidSvg(svg);
    const result: MermaidRenderResult = safe
      ? { svg: safe }
      : { error: "empty or unsafe SVG" };
    // Only cache successes: a cached error would permanently mask a transient
    // (e.g. chunk-load) failure the next render could recover from.
    if (result.svg) svgCache.set(cacheKey, result);
    return result;
  } catch (error) {
    return { error: errorText(error) };
  } finally {
    // Tidy any measurement/orphan nodes mermaid may leave behind on error.
    if (typeof document !== "undefined") {
      document.getElementById(renderId)?.remove();
      document.getElementById(`d${renderId}`)?.remove();
    }
  }
}

// --- Theme-change notifier (shared, module-scoped) ---------------------------

const themeListeners = new Set<() => void>();
let themeWatchStarted = false;

function notifyThemeChange(): void {
  themeListeners.forEach((listener) => {
    try {
      listener();
    } catch {
      /* a subscriber throwing must not break the others */
    }
  });
}

function ensureThemeWatch(): void {
  if (themeWatchStarted || typeof document === "undefined") return;
  themeWatchStarted = true;
  try {
    const observer = new MutationObserver(notifyThemeChange);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
  } catch {
    /* MutationObserver unavailable — theme pins simply won't re-render */
  }
  try {
    window
      .matchMedia?.("(prefers-color-scheme: dark)")
      .addEventListener("change", notifyThemeChange);
  } catch {
    /* matchMedia unavailable — system-theme swaps won't re-render */
  }
}

/**
 * Subscribe to app theme changes (`data-theme` pin or system scheme). The
 * NodeView uses this to re-render its diagram with fresh token colors. Returns
 * an unsubscribe function.
 */
export function subscribeMermaidTheme(listener: () => void): () => void {
  ensureThemeWatch();
  themeListeners.add(listener);
  return () => {
    themeListeners.delete(listener);
  };
}
