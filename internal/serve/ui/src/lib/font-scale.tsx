import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export type FontScale = "small" | "default" | "large";
export type FontFamily =
  | "System"
  | "Iowan Old Style"
  | "IBM Plex Sans"
  | "IBM Plex Serif"
  | "IBM Plex Mono"
  | "JetBrains Mono"
  | "iA Writer Duo"
  | "Source Serif"
  | "Geist"
  | "Geist Mono"
  | "Spectral";

const STORAGE_KEY = "tusker-font-scale";
const FONT_FAMILY_STORAGE_KEY = "tusker-font-family";
// Utilities are dense fixed-pixel sizes (mostly 11–13px); default renders them near 13–15px.
const VALUES: Record<FontScale, number> = { small: 1, default: 1.125, large: 1.25 };
const FAMILIES: Record<FontFamily, string> = {
  System: ' -apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro", system-ui, "Segoe UI", "Helvetica Neue", Helvetica, Arial, sans-serif',
  "Iowan Old Style": '"Iowan Old Style", Georgia, serif',
  "IBM Plex Sans": '"IBM Plex Sans", -apple-system, BlinkMacSystemFont, system-ui, sans-serif',
  "IBM Plex Serif": '"IBM Plex Serif", Georgia, serif',
  "IBM Plex Mono": '"IBM Plex Mono", "SF Mono", ui-monospace, monospace',
  "JetBrains Mono": '"JetBrains Mono", "SF Mono", ui-monospace, monospace',
  "iA Writer Duo": '"iA Writer Duo", "SF Mono", ui-monospace, monospace',
  "Source Serif": '"Source Serif 4", "Source Serif Pro", Georgia, serif',
  Geist: 'Geist, -apple-system, BlinkMacSystemFont, system-ui, sans-serif',
  "Geist Mono": '"Geist Mono", "SF Mono", ui-monospace, monospace',
  Spectral: 'Spectral, Georgia, serif',
};

export const FONT_FAMILY_OPTIONS = Object.keys(FAMILIES) as FontFamily[];

interface FontScaleCtx {
  scale: FontScale;
  setScale: (scale: FontScale) => void;
  family: FontFamily;
  setFamily: (family: FontFamily) => void;
}

const Ctx = createContext<FontScaleCtx | null>(null);

function readScale(): FontScale {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === "small" || value === "large") return value;
  } catch {
    /* Ignore unavailable storage. */
  }
  return "default";
}

function readFamily(): FontFamily {
  try {
    const value = localStorage.getItem(FONT_FAMILY_STORAGE_KEY);
    if (value && value in FAMILIES) return value as FontFamily;
  } catch {
    /* Ignore unavailable storage. */
  }
  return "System";
}

function apply(scale: FontScale) {
  document.documentElement.style.setProperty("--tk-font-scale", String(VALUES[scale]));
  try {
    localStorage.setItem(STORAGE_KEY, scale);
  } catch {
    /* Ignore unavailable storage. */
  }
}

function applyFamily(family: FontFamily) {
  const root = document.documentElement;
  root.style.setProperty("--tk-font-family", FAMILIES[family]);
  root.style.setProperty("--tk-display-font-family", FAMILIES[family]);
  try {
    localStorage.setItem(FONT_FAMILY_STORAGE_KEY, family);
  } catch {
    /* Ignore unavailable storage. */
  }
}

export function FontScaleProvider({ children }: { children: ReactNode }) {
  const [scale, setScaleState] = useState<FontScale>(readScale);
  const [family, setFamilyState] = useState<FontFamily>(readFamily);

  useEffect(() => apply(scale), [scale]);
  useEffect(() => applyFamily(family), [family]);

  const setScale = useCallback((next: FontScale) => {
    apply(next);
    setScaleState(next);
  }, []);
  const setFamily = useCallback((next: FontFamily) => {
    applyFamily(next);
    setFamilyState(next);
  }, []);

  const value = useMemo(() => ({ scale, setScale, family, setFamily }), [scale, setScale, family, setFamily]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useFontScale(): FontScaleCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useFontScale must be used within FontScaleProvider");
  return ctx;
}
