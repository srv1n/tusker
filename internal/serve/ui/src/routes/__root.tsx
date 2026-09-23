import { useEffect, useState } from "react";
import { Outlet, useLocation } from "@tanstack/react-router";
import { AlertTriangle } from "lucide-react";
import { CrashLoopCircuitBanner } from "@/components/CrashLoopCircuitBanner";
import { TaskSearch } from "@/features/search/TaskSearch";
import { useDaemon } from "@/lib/queries";
import { ProjectStrip } from "@/features/workbench/navigation/ProjectStrip";

const RAIL_LAYOUT_STORAGE_KEY = "tusker.rails.layout.v1";
type RailLayout = { projectExpanded: boolean };

function readRailLayout(): RailLayout {
  try {
    const saved = JSON.parse(localStorage.getItem(RAIL_LAYOUT_STORAGE_KEY) ?? "null") as Partial<RailLayout> | null;
    return { projectExpanded: saved?.projectExpanded !== false };
  } catch {
    return { projectExpanded: true };
  }
}

let shellMode = typeof window !== "undefined" &&
  new URLSearchParams(window.location.search).get("shell") === "1";

export function isTuskerShellMode(): boolean {
  if (typeof navigator !== "undefined" && navigator.userAgent.includes("TuskerShell/")) shellMode = true;
  return shellMode;
}

/**
 * App shell. Full-window chrome is a project rail plus the selected content
 * surface. The embedded panel keeps its compact shell.
 */
export function RootLayout() {
  const location = useLocation();
  const embedded = isTuskerShellMode() && location.pathname === "/panel";
  const [rails, setRails] = useState(readRailLayout);

  useEffect(() => {
    try { localStorage.setItem(RAIL_LAYOUT_STORAGE_KEY, JSON.stringify(rails)); } catch { /* preference is best effort */ }
  }, [rails]);

  useEffect(() => {
    const toggleOnShortcut = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "\\") {
        event.preventDefault();
        setRails((value) => ({ projectExpanded: !value.projectExpanded }));
      }
    };
    window.addEventListener("keydown", toggleOnShortcut);
    return () => window.removeEventListener("keydown", toggleOnShortcut);
  }, []);

  return (
    <div className="tusker-shell flex h-dvh w-full overflow-hidden bg-surface text-ink">
      {!embedded && <ProjectStrip expanded={rails.projectExpanded} onToggle={() => setRails((value) => ({ projectExpanded: !value.projectExpanded }))} />}
      <TaskSearch />

      <main className={`flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden ${embedded ? "" : "bg-raised"}`}>
        {/* The rail status row carries circuit state; the embedded panel has no rail, so it keeps the banner. */}
        {embedded && <CrashLoopCircuitBannerFromDaemon />}
        {!embedded && <EscalationBanner />}
        <div className="min-h-0 flex-1 overflow-hidden">
          <Outlet />
        </div>
      </main>
    </div>
  );
}

/**
 * Pass-through layout for project-scoped routes.
 *
 * Live updates come from the single shared subscription in main.tsx: the
 * unfiltered /api/stream channel already carries every project-scoped event
 * (the broker only narrows when a project filter is supplied), so a second
 * per-project connection here would double broker clients without ever
 * reading its messages into the query cache.
 */
export function ProjectLayout() {
	return <div className="h-full min-h-0"><Outlet /></div>;
}

function CrashLoopCircuitBannerFromDaemon() {
  const daemon = useDaemon();
  return <CrashLoopCircuitBanner circuit={daemon.data?.crashLoop} embedded />;
}

function EscalationBanner() {
  const daemon = useDaemon();
  if (daemon.data?.persistentEscalationBanner !== true) {
    return null;
  }
  return (
    <div className="flex flex-none items-center gap-2 border-b border-fail/30 bg-fail-soft px-4 py-2 text-[13px] font-medium text-fail">
      <AlertTriangle size={15} aria-hidden="true" />
      <span className="font-semibold">P0 escalation open</span>
      <span className="min-w-0 truncate text-fail/90">Open the morning digest for details.</span>
    </div>
  );
}
