import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export type ThemePref = "light" | "dark" | "system";
export type ResolvedTheme = "light" | "dark";

const STORAGE_KEY = "tusker-theme";

interface ThemeCtx {
  pref: ThemePref;
  resolved: ResolvedTheme;
  setPref: (p: ThemePref) => void;
  /** Cycle light → dark → system. */
  cycle: () => void;
}

const Ctx = createContext<ThemeCtx | null>(null);

function readPref(): ThemePref {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === "light" || v === "dark") return v;
  } catch {
    /* ignore */
  }
  return "system";
}

function systemPrefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

function apply(pref: ThemePref) {
  const root = document.documentElement;
  if (pref === "system") {
    root.removeAttribute("data-theme");
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch {
      /* ignore */
    }
  } else {
    root.setAttribute("data-theme", pref);
    try {
      localStorage.setItem(STORAGE_KEY, pref);
    } catch {
      /* ignore */
    }
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [pref, setPrefState] = useState<ThemePref>(readPref);
  const [systemDark, setSystemDark] = useState<boolean>(() =>
    typeof window === "undefined" ? false : systemPrefersDark(),
  );

  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => setSystemDark(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  const setPref = useCallback((p: ThemePref) => {
    apply(p);
    setPrefState(p);
  }, []);

  const cycle = useCallback(() => {
    setPrefState((prev) => {
      const next: ThemePref =
        prev === "light" ? "dark" : prev === "dark" ? "system" : "light";
      apply(next);
      return next;
    });
  }, []);

  const resolved: ResolvedTheme =
    pref === "system" ? (systemDark ? "dark" : "light") : pref;

  const value = useMemo(
    () => ({ pref, resolved, setPref, cycle }),
    [pref, resolved, setPref, cycle],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme(): ThemeCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
