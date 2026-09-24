import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { BookOpen, ChevronDown, ChevronUp, Ellipsis, FolderPlus, Inbox, Layers, PanelLeftClose, PanelLeftOpen, Pin, RefreshCw, Search, Settings } from "lucide-react";
import { cn } from "@/lib/cn";
import { openTaskSearch } from "@/features/search/TaskSearch";
import { useDaemon, useProjectRefresh, useProjects } from "@/lib/queries";
import { projectContainsCheckout, projectVisibleInNavigation, type CheckoutSummary, type ProjectSummary } from "@/types/domain";
import { AddProjectForm } from "@/components/Sidebar";
import {
  NAVIGATION_CHANGED_EVENT,
  movePinnedProject,
  orderProjects,
  projectIdFromPath,
  projectWorkPath,
  readNavigationState,
  recordProjectVisit,
  resolveNavigationTarget,
  sanitizeNavigationState,
  toggleProjectPinned,
  writeNavigationState,
  type NavigationState,
  type StorageLike,
} from "./navigationState";
import { PROJECT_ICON_CHANGED_EVENT, projectIconChoice, projectInitials } from "./projectIcons";
import { runnerStatus } from "./runnerStatus";
import "./ProjectStrip.css";

/** The three project sections. `match` receives the path after /p/<id>, without a trailing slash. */
const PROJECT_SECTIONS = [
  { label: "Inbox", to: "/p/$projectId" as const, icon: Inbox, match: (rest: string) => rest === "" },
  { label: "Work", to: "/p/$projectId/waves" as const, icon: Layers, match: (rest: string) => /^\/(waves|tasks|runs)(\/|$)/.test(rest) },
  { label: "Docs", to: "/p/$projectId/knowledge" as const, icon: BookOpen, match: (rest: string) => /^\/(docs|knowledge)(\/|$)/.test(rest) },
] as const;

const TONE_DOT = { fail: "bg-fail", warn: "bg-warn", pass: "bg-pass", muted: "bg-faint" } as const;

const MENU_ITEM = "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink disabled:opacity-40";

function guardedStorage(): StorageLike | null {
  try {
    return typeof window !== "undefined" && window.localStorage ? window.localStorage : null;
  } catch {
    return null;
  }
}

function routeIds(project: ProjectSummary): string[] {
  return [project.id, ...(project.checkouts ?? []).map((checkout) => checkout.id)];
}

function pathForProject(project: ProjectSummary, path: string | undefined): boolean {
  const routeId = path ? projectIdFromPath(path) : null;
  return routeId !== null && routeIds(project).includes(routeId);
}

function preferredRouteId(project: ProjectSummary): string {
  const ids = routeIds(project);
  try {
    const saved = window.localStorage.getItem(`tusker.navigation.checkout.${project.id}`);
    if (saved && ids.includes(saved)) return saved;
  } catch {
    // A blocked preference only loses checkout restoration.
  }
  return ids[0] ?? project.id;
}

function currentPath(pathname: string): string {
  if (typeof window === "undefined") return pathname;
  return `${window.location.pathname}${window.location.search}${window.location.hash}`;
}

function projectRailLabel(project: ProjectSummary, projects: ProjectSummary[]): string {
  const initials = projectInitials(project.name);
  const matching = projects.filter((item) => projectInitials(item.name) === initials);
  if (matching.length < 2) return initials;
  return `${initials}${matching.indexOf(project) + 1}`;
}

export function ProjectStrip({ expanded, onToggle }: { expanded: boolean; onToggle: () => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { projectId: routeProjectId } = useParams({ strict: false }) as { projectId?: string };
  const projectsQ = useProjects();
  const daemon = useDaemon();
  const [navigation, setNavigation] = useState<NavigationState | null>(null);
  const [addingProject, setAddingProject] = useState(false);
  const [iconEpoch, setIconEpoch] = useState(0);
  const [announcement, setAnnouncement] = useState("");
  const [menuId, setMenuId] = useState<string | null>(null);
  const initialRestoreDoneRef = useRef(false);
  const firstPathRef = useRef(location.pathname);
  const lastRecordedRouteRef = useRef("");
  const navigationRef = useRef<NavigationState | null>(null);

  const projects = useMemo(() => (projectsQ.data ?? []).filter(projectVisibleInNavigation), [projectsQ.data]);
  const projectIds = useMemo(() => projects.map((project) => project.id), [projects]);
  const projectSetKey = useMemo(() => [...projectIds].sort().join("\0"), [projectIds]);
  const routeOwners = useMemo(
    () => Object.fromEntries(projects.flatMap((project) => (project.checkouts ?? []).map((checkout) => [checkout.id, project.id]))),
    [projects],
  );
  const activeProject = projects.find((project) => projectContainsCheckout(project, routeProjectId ?? ""));
  const activeProjectId = activeProject?.id;
  const activeRouteId = routeProjectId ?? activeProjectId;
  const path = currentPath(location.pathname);
  const sectionRest = location.pathname.replace(/^\/p\/[^/]+/, "").replace(/\/$/, "");

  const commit = (next: NavigationState) => {
    navigationRef.current = next;
    setNavigation(next);
  };

  useEffect(() => {
    if (!projectsQ.data) return;
    setNavigation((previous) => {
      const next = previous ? sanitizeNavigationState(previous, projectIds) : readNavigationState(guardedStorage(), projectIds);
      navigationRef.current = next;
      return next;
    });
  }, [projectSetKey, projectsQ.data, projectIds]);

  useEffect(() => {
    if (navigation) writeNavigationState(guardedStorage(), navigation);
  }, [navigation]);

  useEffect(() => {
    const sync = () => setNavigation(readNavigationState(guardedStorage(), projectIds));
    const bump = () => setIconEpoch((epoch) => epoch + 1);
    window.addEventListener(NAVIGATION_CHANGED_EVENT, sync);
    window.addEventListener("storage", sync);
    window.addEventListener(PROJECT_ICON_CHANGED_EVENT, bump);
    return () => {
      window.removeEventListener(NAVIGATION_CHANGED_EVENT, sync);
      window.removeEventListener("storage", sync);
      window.removeEventListener(PROJECT_ICON_CHANGED_EVENT, bump);
    };
  }, [projectIds]);

  useEffect(() => {
    if (!navigation || initialRestoreDoneRef.current) return;
    initialRestoreDoneRef.current = true;
    if (firstPathRef.current !== "/" || location.pathname !== "/" || navigation.activeProjectId === null) return;
    const target = resolveNavigationTarget(navigation, projectIds, { routeOwners });
    if (target.projectId !== null && target.path !== "/") void navigate({ to: target.path as "/" });
  }, [location.pathname, navigation, navigate, projectIds, routeOwners]);

  useEffect(() => {
    if (!navigation || !activeProjectId) return;
    const pathRouteId = projectIdFromPath(path);
    const pathProjectId = pathRouteId && projectIds.includes(pathRouteId) ? pathRouteId : pathRouteId ? routeOwners[pathRouteId] : undefined;
    if (pathProjectId !== activeProjectId) return;
    const routeKey = `${activeProjectId}\0${path}`;
    if (lastRecordedRouteRef.current === routeKey) return;
    lastRecordedRouteRef.current = routeKey;
    setNavigation((previous) => {
      const next = previous ? recordProjectVisit(previous, projectIds, activeProjectId, path) : previous;
      navigationRef.current = next;
      return next;
    });
  }, [activeProjectId, navigation, path, projectIds, routeOwners]);

  // Stable order: pins + saved order only. Visiting a project never reorders the rail.
  const orderedProjects = navigation ? orderProjects(projects, navigation) : projects;

  const openProject = (project: ProjectSummary) => {
    const current = navigationRef.current ?? navigation;
    if (!current) return;
    const saved = readNavigationState(guardedStorage(), projectIds).lastPathByProject[project.id] ?? current.lastPathByProject[project.id];
    const target = pathForProject(project, saved) ? saved! : projectWorkPath(preferredRouteId(project));
    lastRecordedRouteRef.current = `${project.id}\0${target}`;
    commit(recordProjectVisit(current, projectIds, project.id, target));
    void navigate({ to: target as "/" });
  };

  const togglePin = (project: ProjectSummary) => {
    const current = navigationRef.current ?? navigation;
    if (!current) return;
    const next = toggleProjectPinned(current, projectIds, project.id);
    commit(next);
    setAnnouncement(`${project.name} ${next.pinnedProjectIds.includes(project.id) ? "pinned" : "unpinned"}.`);
  };

  const movePin = (project: ProjectSummary, direction: -1 | 1) => {
    const current = navigationRef.current ?? navigation;
    if (!current) return;
    const result = movePinnedProject(current, projectIds, project.id, direction);
    if (!result.movedId) return;
    commit(result.state);
    setAnnouncement(`${project.name}, pinned position ${result.position} of ${result.total}.`);
  };

  const status = runnerStatus(daemon.data, activeProject);
  const statusText = status.reason ? `${status.label}: ${status.reason}` : status.label;
  const statusInner = <>
    <span aria-hidden="true" className="flex w-[15px] justify-center"><span className={cn("h-2 w-2 rounded-full", TONE_DOT[status.tone])} /></span>
    {expanded && <span className={cn("truncate", status.tone === "fail" && "font-semibold text-fail")}>{status.label}</span>}
  </>;
  const railButton = cn("flex items-center rounded-md text-muted transition-colors hover:bg-hover hover:text-ink", expanded ? "h-[30px] w-full gap-2 px-2 text-[13px]" : "h-9 w-9 justify-center");

  return (
    <aside className={cn("project-rail relative flex h-full flex-none flex-col border-r border-line-soft bg-panel text-[13px] transition-[width] duration-200", expanded ? "w-[220px]" : "w-14")} aria-label="Project navigation">
      <div className={cn("flex-none p-2", !expanded && "flex justify-center")}>
        <button type="button" onClick={() => openTaskSearch()} aria-label="Search tasks" title="Search (⌘K)" className={railButton}>
          <Search size={15} aria-hidden="true" />
          {expanded && <><span>Search</span><kbd className="ml-auto font-sans text-[11px] text-faint">⌘K</kbd></>}
        </button>
      </div>

      <nav className={cn("project-strip-track flex min-h-0 flex-1 flex-col gap-px overflow-y-auto overflow-x-hidden pb-2", expanded ? "px-2" : "items-center px-1")} data-project-strip>
        {orderedProjects.map((project) => {
          const selected = activeProjectId === project.id;
          const pinned = navigation?.pinnedProjectIds.includes(project.id) ?? false;
          const pinIndex = navigation?.pinnedProjectIds.indexOf(project.id) ?? -1;
          const icon = projectIconChoice(navigation?.projectIconById[project.id]);
          const tileSize = expanded ? "h-5 w-5 text-[9px]" : "h-[26px] w-[26px] text-[10px]";
          const tile = icon
            ? <span className={cn("flex shrink-0 items-center justify-center", tileSize)}><icon.Icon size={expanded ? 15 : 18} aria-hidden="true" className={icon.className} /></span>
            : <span className={cn("relative flex shrink-0 items-center justify-center rounded-md bg-raised font-semibold text-ink-soft", tileSize)}>
              <span aria-hidden="true">{projectRailLabel(project, orderedProjects)}</span>
              <img src={`/api/projects/${encodeURIComponent(project.id)}/icon?v=${iconEpoch}`} alt="" aria-hidden="true" loading="lazy" decoding="async" className="absolute inset-0 h-full w-full rounded-md object-cover" onError={(event) => { event.currentTarget.hidden = true; }} />
            </span>;
          // ponytail: only the red needs-you dot. No backend signal exists for "finished since last visit"; add a green dot when one does.
          const needsLabel = project.needsCount > 0 ? `, ${project.needsCount} need${project.needsCount === 1 ? "s" : ""} you` : "";
          return (
            <div key={project.id} className={cn("flex shrink-0 flex-col", expanded ? cn("w-full", selected && "my-1 rounded-lg border border-line bg-raised p-0.5 shadow-xs") : cn("w-10 items-center", selected && "my-1 w-11 gap-0.5 rounded-xl border border-line bg-raised py-1 shadow-xs"))}>
              <div className="group relative w-full">
                {selected && !expanded && <span aria-hidden="true" className="absolute -left-1.5 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r-full bg-ink" />}
                <button
                  type="button"
                  data-project-chip={project.id}
                  aria-current={selected ? "true" : undefined}
                  aria-label={`${project.name}${selected ? ", current project" : ""}${needsLabel}`}
                  title={project.name}
                  onClick={() => openProject(project)}
                  className={cn("relative flex items-center rounded-md outline-none transition-colors focus-visible:ring-2 focus-visible:ring-info", expanded ? "h-[34px] w-full gap-2 px-2 text-left" : "mx-auto h-9 w-9 justify-center", selected ? "font-semibold text-ink" : "text-ink-soft hover:bg-hover")}
                >
                  {tile}
                  {expanded && <span className="min-w-0 flex-1 truncate">{project.name}</span>}
                  {project.needsCount > 0 && <span aria-hidden="true" className={cn("h-1.5 w-1.5 shrink-0 rounded-full bg-fail", !expanded && "absolute right-1 top-1 ring-2 ring-panel")} />}
                </button>
                {expanded && <button type="button" aria-label={`Actions for ${project.name}`} aria-expanded={menuId === project.id} onClick={() => setMenuId((open) => open === project.id ? null : project.id)} className={cn("absolute right-1 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-md bg-panel text-muted hover:bg-hover hover:text-ink focus-visible:opacity-100 group-hover:opacity-100", menuId === project.id ? "opacity-100" : "opacity-0")}><Ellipsis size={14} aria-hidden="true" /></button>}
              </div>
              {expanded && menuId === project.id && (
                <div className="my-1 rounded-lg border border-line bg-raised p-1 shadow-sm" onClick={() => setMenuId(null)}>
                  <Link to="/p/$projectId/settings" params={{ projectId: selected && activeRouteId ? activeRouteId : project.id }} className={MENU_ITEM}><Settings size={13} aria-hidden="true" />Project settings</Link>
                  <button type="button" onClick={() => togglePin(project)} className={MENU_ITEM}><Pin size={13} aria-hidden="true" />{pinned ? "Unpin" : "Pin"}</button>
                  {pinned && <>
                    <button type="button" onClick={() => movePin(project, -1)} disabled={pinIndex === 0} className={MENU_ITEM}><ChevronUp size={13} aria-hidden="true" />Move up</button>
                    <button type="button" onClick={() => movePin(project, 1)} disabled={pinIndex === (navigation?.pinnedProjectIds.length ?? 0) - 1} className={MENU_ITEM}><ChevronDown size={13} aria-hidden="true" />Move down</button>
                  </>}
                  <RefreshProjectItem projectId={project.id} />
                  {selected && <div className="px-2 py-1" onClick={(event) => event.stopPropagation()}><ProjectContextControls /></div>}
                </div>
              )}
              {selected && activeRouteId && (
                <div className={cn("flex flex-col gap-px", expanded ? "mb-0.5 ml-[17px] border-l border-line pl-1" : "mt-0.5 w-8 items-center border-t border-line pt-1")}>
                  {PROJECT_SECTIONS.map(({ label, to, icon: Icon, match }) => {
                    const active = match(sectionRest);
                    const badge = label === "Inbox" && project.needsCount > 0 ? project.needsCount : 0;
                    return (
                      <Link
                        key={label}
                        to={to}
                        params={{ projectId: activeRouteId }}
                        aria-current={active ? "page" : undefined}
                        aria-label={expanded ? undefined : `${label}${badge ? `, ${badge} need you` : ""}`}
                        title={label}
                        className={cn("flex items-center rounded-md transition-colors", expanded ? "h-[30px] gap-2 pl-2 pr-2" : "h-7 w-7 justify-center", active ? "bg-hover font-semibold text-ink" : "text-muted hover:bg-hover hover:text-ink")}
                      >
                        <Icon size={expanded ? 15 : 14} aria-hidden="true" />
                        {expanded && <span className="flex-1">{label}</span>}
                        {expanded && badge > 0 && <span className="rounded-full bg-fail-soft px-1.5 font-mono text-[10px] font-semibold text-fail">{badge}</span>}
                      </Link>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
        {orderedProjects.length === 0 && <span className="px-2 py-2 text-[12px] text-muted">No projects</span>}
      </nav>

      <div className={cn("relative flex flex-none flex-col gap-px p-2", !expanded && "items-center")}>
        {activeRouteId ? (
          <Link to="/p/$projectId/diagnostics" params={{ projectId: activeRouteId }} aria-label={expanded ? undefined : statusText} title={statusText} className={railButton}>
            {statusInner}
          </Link>
        ) : (
          <span role="status" aria-label={statusText} title={statusText} className={cn(railButton, "hover:bg-transparent")}>
            {statusInner}
          </span>
        )}
        <div className={cn("group relative flex items-center", expanded && "w-full")}>
          <Link to="/settings" aria-label="App settings" title="App settings" className={cn(railButton, expanded && "pr-16")}>
            <Settings size={15} aria-hidden="true" />
            {expanded && <span>Settings</span>}
          </Link>
          {expanded && <button type="button" aria-label="Project list actions" aria-expanded={menuId === "app"} onClick={() => setMenuId((open) => open === "app" ? null : "app")} className="absolute right-8 flex h-6 w-6 items-center justify-center rounded-md text-muted hover:bg-hover hover:text-ink"><Ellipsis size={14} aria-hidden="true" /></button>}
          {expanded && <button type="button" onClick={onToggle} aria-label="Minimize project navigation" title="Collapse (⌘\)" className="absolute right-1 flex h-6 w-6 items-center justify-center rounded-md text-faint hover:bg-hover hover:text-ink"><PanelLeftClose size={14} aria-hidden="true" /></button>}
        </div>
        {!expanded && <button type="button" onClick={onToggle} aria-label="Expand project navigation" title="Expand (⌘\)" className={railButton}><PanelLeftOpen size={15} aria-hidden="true" /></button>}
        {menuId === "app" && expanded && (
          <div className="absolute bottom-full left-2 right-2 z-30 mb-1 rounded-lg border border-line bg-raised p-1 shadow-lg">
            <button type="button" onClick={() => { setAddingProject(true); setMenuId(null); }} className={MENU_ITEM}><FolderPlus size={13} aria-hidden="true" />Add project</button>
            <button type="button" onClick={() => void projectsQ.refetch()} disabled={projectsQ.isFetching} aria-busy={projectsQ.isFetching} className={MENU_ITEM}><RefreshCw size={13} aria-hidden="true" className={projectsQ.isFetching ? "animate-spin" : undefined} />Refresh projects</button>
          </div>
        )}
      </div>
      {addingProject && <div className="absolute left-full top-2 z-40 ml-2 w-80"><AddProjectForm onDone={() => setAddingProject(false)} /></div>}
      <div aria-live="polite" role="status" className="sr-only">{announcement}</div>
    </aside>
  );
}

function RefreshProjectItem({ projectId }: { projectId: string }) {
  const refresh = useProjectRefresh(projectId);
  return <>
    <button type="button" onClick={(event) => { event.stopPropagation(); refresh.mutate(); }} disabled={refresh.isPending} aria-busy={refresh.isPending} className={MENU_ITEM}><RefreshCw size={13} aria-hidden="true" className={refresh.isPending ? "animate-spin" : undefined} />Refresh project</button>
    {refresh.error && <p role="alert" className="px-2 py-1 text-[11px] text-fail">Refresh failed — check this project’s source.</p>}
  </>;
}

function ProjectContextControls() {
  const { projectId } = useParams({ strict: false }) as { projectId?: string };
  const projects = useProjects();
  const project = projects.data?.find((item) => projectContainsCheckout(item, projectId ?? ""));
  const checkout = project?.checkouts?.find((item) => item.id === projectId);
  const checkouts = project?.checkouts ?? [];

  useEffect(() => {
    if (!project || checkouts.length < 2 || project.id !== projectId || typeof window === "undefined") return;
    try {
      const saved = window.localStorage.getItem(`tusker.navigation.checkout.${project.id}`);
      if (saved && checkouts.some((item) => item.id === saved)) window.location.replace(checkoutPath(saved));
    } catch { /* Blocked storage must not prevent project navigation. */ }
  }, [checkouts, project, projectId]);

  useEffect(() => {
    if (!projectId || !project || typeof window === "undefined") return;
    try { window.localStorage.setItem(`tusker.navigation.checkout.${project.id}`, projectId); } catch { /* preference is best effort */ }
  }, [project, projectId]);

  if (!project || !checkout || checkouts.length < 2) return null;
  const branchCounts = new Map<string, number>();
  for (const item of checkouts) if (item.branch) branchCounts.set(item.branch, (branchCounts.get(item.branch) ?? 0) + 1);
  return (
    <select aria-label="Registered checkout" title={checkoutOptionLabel(checkout, branchCounts)} value={checkout.id} onChange={(event) => { try { window.localStorage.setItem(`tusker.navigation.checkout.${project.id}`, event.target.value); } catch { /* preference is best effort */ } window.location.assign(checkoutPath(event.target.value)); }} className="w-full truncate rounded-md border border-line bg-surface px-2 py-1 font-mono text-[11px] text-ink outline-none focus:border-info">
      {checkouts.map((item) => <option key={item.id} value={item.id}>{checkoutOptionLabel(item, branchCounts)}</option>)}
    </select>
  );

  function checkoutPath(nextID: string): string {
    return `${window.location.pathname.replace(`/p/${encodeURIComponent(projectId ?? "")}`, `/p/${encodeURIComponent(nextID)}`)}${window.location.search}`;
  }
}

function checkoutOptionLabel(checkout: CheckoutSummary, branchCounts: Map<string, number>): string {
  if (!checkout.available) return `Unavailable · ${checkout.label}`;
  if (checkout.detached) return `Detached · ${checkout.head ?? "unknown"} · ${checkout.label}`;
  if (!checkout.git) return `Non-Git · ${checkout.label}`;
  const branch = checkout.branch ?? "unknown branch";
  return branchCounts.get(branch) && branchCounts.get(branch)! > 1 ? `${branch} · ${checkout.label}` : branch;
}
