import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { Link, Outlet, useLocation, useParams } from "@tanstack/react-router";
import { AlertTriangle, BookOpen, Ellipsis, LayoutGrid, ListOrdered, PanelLeftClose, PanelLeftOpen, Settings } from "lucide-react";
import { CrashLoopCircuitBanner } from "@/components/CrashLoopCircuitBanner";
import { TaskSearch } from "@/features/search/TaskSearch";
import { cn } from "@/lib/cn";
import { useDaemon } from "@/lib/queries";
import { ProjectStrip } from "@/features/workbench/navigation/ProjectStrip";

const RAIL_LAYOUT_STORAGE_KEY = "tusker.rails.layout.v1";
type RailLayout = { projectExpanded: boolean; sectionExpanded: boolean };
const RailLayoutContext = createContext<{ rails: RailLayout; setRails: React.Dispatch<React.SetStateAction<RailLayout>> } | null>(null);

function readRailLayout(): RailLayout {
  try {
    const saved = JSON.parse(localStorage.getItem(RAIL_LAYOUT_STORAGE_KEY) ?? "null") as Partial<RailLayout> | null;
    return { projectExpanded: saved?.projectExpanded !== false, sectionExpanded: saved?.sectionExpanded !== false };
  } catch {
    return { projectExpanded: true, sectionExpanded: true };
  }
}

function useRailLayout() {
  const value = useContext(RailLayoutContext);
  if (!value) throw new Error("useRailLayout must be used inside RootLayout");
  return value;
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
  const railLayout = useMemo(() => ({ rails, setRails }), [rails]);

  useEffect(() => {
    try { localStorage.setItem(RAIL_LAYOUT_STORAGE_KEY, JSON.stringify(rails)); } catch { /* preference is best effort */ }
  }, [rails]);

  return (
    <RailLayoutContext.Provider value={railLayout}>
      <div className="tusker-shell flex h-dvh w-full overflow-hidden bg-surface text-ink">
        {!embedded && <ProjectStrip expanded={rails.projectExpanded} onToggle={() => setRails((value) => ({ ...value, projectExpanded: !value.projectExpanded }))} />}
        <TaskSearch />

        <main className={`flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden ${embedded ? "" : "bg-raised"}`}>
		  <CrashLoopCircuitBannerFromDaemon embedded={embedded} />
          {!embedded && <InvariantCircuitBanner />}
          {!embedded && <EscalationBanner />}
          <div className="min-h-0 flex-1 overflow-hidden">
            <Outlet />
          </div>
        </main>
      </div>
    </RailLayoutContext.Provider>
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
	const location = useLocation();
	const { projectId = "" } = useParams({ strict: false }) as { projectId?: string };
	const { rails, setRails } = useRailLayout();
	const expanded = rails.sectionExpanded;
	const railLinkClass = (selected: boolean) => cn(
		"flex h-10 w-full items-center rounded-lg text-[12px] font-medium transition-colors",
		expanded ? "gap-2 px-2" : "justify-center",
		selected ? "bg-ink text-surface" : "text-muted hover:bg-hover hover:text-ink",
	);
	return (
		<div className="flex h-full min-h-0 flex-col">
			<div className="flex min-h-0 flex-1">
				<nav aria-label="Project sections" className={cn("section-rail relative flex flex-none flex-col bg-surface transition-[width] duration-200", expanded ? "w-40" : "w-14")}>
					<div className="flex min-h-0 flex-1 flex-col gap-2 p-2">
						<Link to="/p/$projectId/waves" params={{ projectId }} aria-current={location.pathname.includes("/waves") ? "page" : undefined} className={railLinkClass(location.pathname.includes("/waves"))} title="Waves"><ListOrdered size={18} aria-hidden="true" /><span className={expanded ? undefined : "sr-only"}>Waves</span></Link>
						<Link to="/p/$projectId/tasks" params={{ projectId }} aria-current={location.pathname.includes("/tasks") ? "page" : undefined} className={railLinkClass(location.pathname.includes("/tasks"))} title="Board"><LayoutGrid size={18} aria-hidden="true" /><span className={expanded ? undefined : "sr-only"}>Board</span></Link>
						<Link to="/p/$projectId/knowledge" params={{ projectId }} aria-current={location.pathname.includes("/knowledge") ? "page" : undefined} className={railLinkClass(location.pathname.includes("/knowledge"))} title="Docs"><BookOpen size={18} aria-hidden="true" /><span className={expanded ? undefined : "sr-only"}>Docs</span></Link>
					</div>
					<div className="flex h-36 flex-none flex-col justify-end gap-2 p-2">
						<Link to="/p/$projectId/settings" params={{ projectId }} aria-current={location.pathname.includes("/settings") ? "page" : undefined} className={railLinkClass(location.pathname.includes("/settings"))} title="Project settings"><Settings size={18} aria-hidden="true" /><span className={expanded ? undefined : "sr-only"}>Settings</span></Link>
						<details className="group relative">
							<summary aria-label="More project destinations" title="More project destinations" className={cn("flex h-10 w-full cursor-pointer list-none items-center rounded-lg text-[12px] font-medium text-muted hover:bg-hover hover:text-ink", expanded ? "gap-2 px-2" : "justify-center")}><Ellipsis size={18} aria-hidden="true" /><span className={expanded ? undefined : "sr-only"}>More</span></summary>
							<div className="absolute bottom-0 left-full z-30 ml-2 hidden min-w-40 rounded-lg border border-line bg-raised p-1 shadow-lg group-hover:block group-focus-within:block group-open:block">
								<Link to="/p/$projectId/plan" params={{ projectId }} className="block rounded-md px-2.5 py-2 text-[12px] text-ink-soft hover:bg-hover hover:text-ink">Plan</Link>
								<Link to="/p/$projectId/trains" params={{ projectId }} className="block rounded-md px-2.5 py-2 text-[12px] text-ink-soft hover:bg-hover hover:text-ink">Trains</Link>
								<Link to="/p/$projectId/diagnostics" params={{ projectId }} className="block rounded-md px-2.5 py-2 text-[12px] text-ink-soft hover:bg-hover hover:text-ink">Diagnostics</Link>
							</div>
						</details>
						<button type="button" onClick={() => setRails((value) => ({ ...value, sectionExpanded: !value.sectionExpanded }))} aria-label={expanded ? "Minimize section navigation" : "Expand section navigation"} title={expanded ? "Minimize section navigation" : "Expand section navigation"} className={cn("flex h-10 w-full items-center rounded-lg text-muted hover:bg-hover hover:text-ink", expanded ? "gap-2 px-2" : "justify-center")}>
							{expanded ? <PanelLeftClose size={18} aria-hidden="true" /> : <PanelLeftOpen size={18} aria-hidden="true" />}<span className={expanded ? "text-[12px]" : "sr-only"}>{expanded ? "Minimize" : "Expand"}</span>
						</button>
					</div>
				</nav>
			<div className="min-h-0 flex-1"><Outlet /></div>
			</div>
		</div>
	);
}

function CrashLoopCircuitBannerFromDaemon({ embedded }: { embedded: boolean }) {
  const daemon = useDaemon();
  return <CrashLoopCircuitBanner circuit={daemon.data?.crashLoop} embedded={embedded} />;
}

function InvariantCircuitBanner() {
  const daemon = useDaemon();
  const circuit = daemon.data?.invariantCircuit;
  if (circuit?.open !== true) {
    return null;
  }
  const detail = circuit.violations?.[0]?.detail ?? circuit.summary ?? circuit.reason ?? "invariant_violation";
  return (
    <div className="flex flex-none items-center gap-2 border-b border-fail/30 bg-fail-soft px-4 py-2 text-[13px] font-medium text-fail">
      <AlertTriangle size={15} aria-hidden="true" />
      <span className="font-semibold">Invariant circuit open</span>
      <span className="min-w-0 truncate text-fail/90">{detail}</span>
    </div>
  );
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
