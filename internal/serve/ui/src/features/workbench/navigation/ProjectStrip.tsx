import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { Bell, BookOpen, ChevronDown, ChevronUp, Ellipsis, FolderPlus, LayoutGrid, ListOrdered, PanelLeftClose, PanelLeftOpen, Pin, RefreshCw, Search, Settings } from "lucide-react";
import { cn } from "@/lib/cn";
import { openTaskSearch } from "@/features/search/TaskSearch";
import { useNeeds, useProjectRefresh, useProjects } from "@/lib/queries";
import { projectContainsCheckout, projectVisibleInNavigation, type CheckoutSummary, type NeedItem, type ProjectSummary } from "@/types/domain";
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
import "./ProjectStrip.css";

const PROJECT_SECTION_NAV = [
  { label: "Waves", to: "/p/$projectId/waves" as const, icon: ListOrdered },
  { label: "Board", to: "/p/$projectId/tasks" as const, icon: LayoutGrid },
  { label: "Docs", to: "/p/$projectId/knowledge" as const, icon: BookOpen },
] as const;

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
  if (matching.length < 2) return projectInitials(project.name);
  return `${initials}${matching.indexOf(project) + 1}`;
}

export function ProjectStrip({ expanded, onToggle }: { expanded: boolean; onToggle: () => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { projectId: routeProjectId } = useParams({ strict: false }) as { projectId?: string };
  const projectsQ = useProjects();
  const needs = useNeeds();
  const [navigation, setNavigation] = useState<NavigationState | null>(null);
  const [mountedOrderIds, setMountedOrderIds] = useState<string[]>([]);
  const [addingProject, setAddingProject] = useState(false);
  const [iconEpoch, setIconEpoch] = useState(0);
  const [announcement, setAnnouncement] = useState("");
  const [menuProjectId, setMenuProjectId] = useState<string | null>(null);
  const projectRailRef = useRef<HTMLElement>(null);
  const projectSetKeyRef = useRef<string | null>(null);
  const firstPathRef = useRef(location.pathname);
  const initialRestoreDoneRef = useRef(false);
  const lastRecordedRouteRef = useRef("");
  const focusProjectRef = useRef<string | null>(null);
  const navigationRef = useRef<NavigationState | null>(null);

  const projects = useMemo(
    () => (projectsQ.data ?? []).filter(projectVisibleInNavigation),
    [projectsQ.data],
  );
  const projectIds = useMemo(() => projects.map((project) => project.id), [projects]);
  const projectSetKey = useMemo(() => [...projectIds].sort().join("\0"), [projectIds]);
  const routeOwners = useMemo(
    () => Object.fromEntries(projects.flatMap((project) => (project.checkouts ?? []).map((checkout) => [checkout.id, project.id]))),
    [projects],
  );
  const activeProject = projects.find((project) => projectContainsCheckout(project, routeProjectId ?? ""));
  const activeProjectId = activeProject?.id;
  const activeRouteId = routeProjectId ?? activeProjectId;
  const activeRefresh = useProjectRefresh(activeProjectId ?? "");
  const path = currentPath(location.pathname);

  useEffect(() => {
    if (!projectsQ.data) return;
    setNavigation((previous) => {
      const next = previous ? sanitizeNavigationState(previous, projectIds) : readNavigationState(guardedStorage(), projectIds);
      navigationRef.current = next;
      return next;
    });
  }, [projectSetKey, projectsQ.data, projectIds]);

  useEffect(() => {
    if (!navigation || projectSetKeyRef.current === projectSetKey) return;
    const previousKey = projectSetKeyRef.current;
    projectSetKeyRef.current = projectSetKey;
    setMountedOrderIds((previous) => {
      if (previousKey === null) return orderProjects(projects, navigation).map((project) => project.id);
      const known = new Set(projectIds);
      return [...previous.filter((id) => known.has(id)), ...projectIds.filter((id) => !previous.includes(id))];
    });
  }, [navigation, projectIds, projectSetKey, projects]);

  useEffect(() => {
    if (navigation) writeNavigationState(guardedStorage(), navigation);
  }, [navigation]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const sync = () => setNavigation(readNavigationState(guardedStorage(), projectIds));
    window.addEventListener(NAVIGATION_CHANGED_EVENT, sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener(NAVIGATION_CHANGED_EVENT, sync);
      window.removeEventListener("storage", sync);
    };
  }, [projectIds]);

  useEffect(() => {
    const bump = () => setIconEpoch((epoch) => epoch + 1);
    window.addEventListener(PROJECT_ICON_CHANGED_EVENT, bump);
    return () => window.removeEventListener(PROJECT_ICON_CHANGED_EVENT, bump);
  }, []);

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

  useEffect(() => {
    const id = focusProjectRef.current;
    if (!id) return;
    focusProjectRef.current = null;
    requestAnimationFrame(() => {
      const chip = projectRailRef.current?.querySelector<HTMLElement>(`[data-project-chip="${CSS.escape(id)}"]`) ?? null;
      chip?.focus();
      chip?.scrollIntoView({ block: "nearest" });
    });
  }, [mountedOrderIds, navigation?.pinnedProjectIds]);

  const orderedProjects = mountedOrderIds
    .map((id) => projects.find((project) => project.id === id))
    .filter((project): project is ProjectSummary => project !== undefined);

  const setMountedToState = (next: NavigationState) => {
    setMountedOrderIds(orderProjects(projects, next).map((project) => project.id));
  };

  const openProject = (project: ProjectSummary) => {
    const currentNavigation = navigationRef.current ?? navigation;
    if (!currentNavigation) return;
    const persistedNavigation = readNavigationState(guardedStorage(), projectIds);
    const saved = persistedNavigation.lastPathByProject[project.id] ?? currentNavigation.lastPathByProject[project.id];
    const target = pathForProject(project, saved) ? saved! : projectWorkPath(preferredRouteId(project));
    lastRecordedRouteRef.current = `${project.id}\0${target}`;
    const next = recordProjectVisit(currentNavigation, projectIds, project.id, target);
    navigationRef.current = next;
    setNavigation(next);
    void navigate({ to: target as "/" });
  };

  const togglePin = (projectId: string) => {
    const currentNavigation = navigationRef.current ?? navigation;
    if (!currentNavigation) return;
    const next = toggleProjectPinned(currentNavigation, projectIds, projectId);
    navigationRef.current = next;
    focusProjectRef.current = projectId;
    setNavigation(next);
    setMountedToState(next);
    setAnnouncement(`${projectId} ${next.pinnedProjectIds.includes(projectId) ? "pinned" : "unpinned"}.`);
  };

  const movePin = (projectId: string, direction: -1 | 1) => {
    const currentNavigation = navigationRef.current ?? navigation;
    if (!currentNavigation) return;
    const result = movePinnedProject(currentNavigation, projectIds, projectId, direction);
    if (!result.movedId) return;
    focusProjectRef.current = projectId;
    navigationRef.current = result.state;
    setNavigation(result.state);
    setMountedToState(result.state);
    setAnnouncement(`${projectId}, pinned position ${result.position} of ${result.total}.`);
  };

  return (
    <aside ref={projectRailRef} className={cn("project-rail relative flex h-full flex-none flex-col border-r border-line-soft bg-raised transition-[width] duration-200", expanded ? "w-52" : "w-14")} aria-label="Project navigation">
      <div className={cn("project-strip-track flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto overflow-x-hidden py-2", expanded ? "items-stretch px-2" : "items-center px-1")} data-project-strip>
          {orderedProjects.map((project) => {
            const selected = activeProjectId === project.id;
            const pinned = navigation?.pinnedProjectIds.includes(project.id) ?? false;
            const selectedIcon = projectIconChoice(navigation?.projectIconById[project.id]);
            return (
              <div key={project.id} className={cn("project-strip-group flex shrink-0 flex-col", expanded ? "w-full" : selected ? "w-12" : "w-10", selected && "items-center rounded-xl bg-panel p-1 ring-1 ring-inset ring-line")}>
                <button
                  type="button"
                  data-project-chip={project.id}
                  aria-current={selected ? "page" : undefined}
                  aria-label={selected ? `${project.name}, current project` : project.name}
                  title={project.name}
                  onClick={() => openProject(project)}
                  className={cn("project-strip-chip relative inline-flex h-10 shrink-0 items-center rounded-lg text-[10px] font-semibold outline-none transition-colors focus-visible:ring-2 focus-visible:ring-info", expanded ? "w-full justify-start gap-2 px-2 text-left" : "w-10 justify-center", selected ? "bg-raised text-ink shadow-sm" : "text-ink-soft hover:bg-hover")}
              >
                {selectedIcon ? <selectedIcon.Icon size={18} aria-hidden="true" className={cn("shrink-0", selectedIcon.className)} /> : <span className="relative flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-panel text-[10px] font-bold text-ink-soft">
                  <span aria-hidden="true">{projectRailLabel(project, orderedProjects)}</span>
                  <img src={`/api/projects/${encodeURIComponent(project.id)}/icon?v=${iconEpoch}`} alt="" aria-hidden="true" loading="lazy" decoding="async" className="absolute inset-0 h-full w-full rounded-lg object-cover" onError={(event) => { event.currentTarget.hidden = true; }} />
                </span>}
                <span className={expanded ? "min-w-0 truncate text-[12px] font-medium" : "sr-only"}>{project.name}</span>
                  {pinned && <Pin size={9} aria-hidden="true" className="absolute -right-1 -top-1 rounded-full bg-raised p-0.5 text-muted" />}
                  {project.needsCount > 0 && <span className={cn("absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full px-1 font-mono text-[9px] leading-none", selected ? "bg-fail text-white" : "bg-fail-soft text-fail")} aria-label={`${project.needsCount} need${project.needsCount === 1 ? "" : "s"} you`}>{project.needsCount}</span>}
                </button>
                {selected && <ProjectSubtree expanded={expanded} projectId={activeRouteId ?? project.id} pathname={location.pathname} />}
              </div>
            );
          })}
          {orderedProjects.length === 0 && <span className="px-1 text-center text-[10px] text-muted">No projects</span>}
      </div>
      <div className={cn("project-rail-footer flex h-36 w-full flex-none flex-col justify-end gap-2 p-2", expanded ? "items-stretch" : "items-center")}>
        <Link to="/settings" aria-label="App settings" title="App settings" className={cn("flex h-10 items-center rounded-lg text-muted hover:bg-hover hover:text-ink", expanded ? "w-full gap-2 px-2" : "w-10 justify-center")}><Settings size={18} aria-hidden="true" /><span className={expanded ? "text-[12px]" : "sr-only"}>Settings</span></Link>
        <div className="project-strip-menu relative">
          <button type="button" aria-expanded={menuProjectId === "app"} aria-label="App actions" title="App actions" onClick={() => setMenuProjectId((open) => open === "app" ? null : "app")} className={cn("flex h-10 items-center rounded-lg text-muted hover:bg-hover hover:text-ink", expanded ? "w-full gap-2 px-2" : "w-10 justify-center")}><Ellipsis size={18} aria-hidden="true" /><span className={expanded ? "text-[12px]" : "sr-only"}>More</span></button>
          {menuProjectId === "app" && <div className="project-app-popover absolute right-0 top-full z-30 mt-1 min-w-48 rounded-xl border border-line bg-raised p-1.5 shadow-lg">
            <button type="button" onClick={() => { setAddingProject(true); setMenuProjectId(null); }} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink"><FolderPlus size={13} aria-hidden="true" />Add project</button>
            <button type="button" onClick={() => void projectsQ.refetch()} disabled={projectsQ.isFetching} aria-busy={projectsQ.isFetching} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink disabled:opacity-40"><RefreshCw size={13} aria-hidden="true" className={projectsQ.isFetching ? "animate-spin" : undefined} />Refresh projects</button>
            <button type="button" onClick={() => { setMenuProjectId(null); openTaskSearch(); }} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink"><Search size={13} aria-hidden="true" />Search tasks</button>
            {activeProject && activeRouteId && <>
              <div className="my-1 border-t border-line-soft" />
              <div className="px-2.5 py-1"><ProjectContextControls compact={false} /></div>
              <button type="button" onClick={() => togglePin(activeProject.id)} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink"><Pin size={13} aria-hidden="true" />{navigation?.pinnedProjectIds.includes(activeProject.id) ? "Unpin project" : "Pin project"}</button>
              {navigation?.pinnedProjectIds.includes(activeProject.id) && <>
                <button type="button" onClick={() => movePin(activeProject.id, -1)} disabled={navigation.pinnedProjectIds.indexOf(activeProject.id) === 0} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink disabled:opacity-35"><ChevronUp size={13} aria-hidden="true" />Move pinned project up</button>
                <button type="button" onClick={() => movePin(activeProject.id, 1)} disabled={navigation.pinnedProjectIds.indexOf(activeProject.id) === navigation.pinnedProjectIds.length - 1} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink disabled:opacity-35"><ChevronDown size={13} aria-hidden="true" />Move pinned project down</button>
              </>}
              <button type="button" onClick={() => void activeRefresh.mutate()} disabled={activeRefresh.isPending} className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[12px] text-ink-soft hover:bg-hover hover:text-ink disabled:opacity-35"><RefreshCw size={13} aria-hidden="true" />Refresh project</button>
              {activeRefresh.error && <p role="alert" className="px-2.5 py-1 text-[11px] text-fail">Refresh failed — check this project’s source.</p>}
            </>}
          </div>}
        </div>
        <button type="button" onClick={onToggle} aria-label={expanded ? "Minimize project navigation" : "Expand project navigation"} title={expanded ? "Minimize project navigation" : "Expand project navigation"} className={cn("flex h-10 items-center rounded-lg text-muted hover:bg-hover hover:text-ink", expanded ? "w-full gap-2 px-2" : "w-10 justify-center")}>
          {expanded ? <PanelLeftClose size={18} aria-hidden="true" /> : <PanelLeftOpen size={18} aria-hidden="true" />}<span className={expanded ? "text-[12px]" : "sr-only"}>{expanded ? "Minimize" : "Expand"}</span>
        </button>
      </div>
      <NotificationControl needs={needs.data ?? []} error={needs.isError} open={menuProjectId === "notifications"} onOpenChange={(open) => setMenuProjectId(open ? "notifications" : null)} />
      {addingProject && <div className="absolute left-full top-2 z-40 ml-2 w-80"><AddProjectForm onDone={() => setAddingProject(false)} /></div>}
      <div aria-live="polite" role="status" className="sr-only">{announcement}</div>
    </aside>
  );
}

function ProjectSubtree({ expanded, projectId, pathname }: { expanded: boolean; projectId: string; pathname: string }) {
  const selected = (to: string) => pathname === to || pathname.startsWith(`${to}/`);
  const linkClass = (active: boolean) => cn(
    "flex h-10 items-center rounded-lg text-[12px] font-medium transition-colors",
    expanded ? "w-full gap-2 px-2" : "w-10 justify-center",
    active ? "bg-info-soft text-info" : "text-muted hover:bg-hover hover:text-ink",
  );
  const route = (to: string) => to.replace("$projectId", projectId);
  const secondarySelected = ["/p/$projectId/trains", "/p/$projectId/diagnostics"].some((to) => selected(route(to)));

  return (
    <nav aria-label={`${projectId} destinations`} className={cn("project-subtree mt-1 flex flex-col gap-1", expanded ? "ml-5 border-l border-line-soft pl-2" : "w-10")}>
      {PROJECT_SECTION_NAV.map(({ label, to, icon: Icon }) => (
        <Link key={label} to={to} params={{ projectId }} aria-current={selected(route(to)) ? "page" : undefined} title={label} className={linkClass(selected(route(to)))}>
          <Icon size={18} aria-hidden="true" />
          <span className={expanded ? undefined : "sr-only"}>{label}</span>
        </Link>
      ))}
      <Link to="/p/$projectId/settings" params={{ projectId }} aria-current={selected(route("/p/$projectId/settings")) ? "page" : undefined} title="Project settings" className={linkClass(selected(route("/p/$projectId/settings")))}>
        <Settings size={18} aria-hidden="true" />
        <span className={expanded ? undefined : "sr-only"}>Settings</span>
      </Link>
      <details className={cn("project-subtree-more group", !expanded && "relative")}>
        <summary aria-label="More project destinations" title="More project destinations" className={cn(linkClass(secondarySelected), "cursor-pointer list-none") }>
          <Ellipsis size={18} aria-hidden="true" />
          <span className={expanded ? undefined : "sr-only"}>More</span>
        </summary>
        <div className={cn("mt-1 space-y-1", expanded ? "" : "absolute bottom-0 left-full z-30 ml-2 hidden min-w-40 rounded-lg border border-line bg-raised p-1 shadow-lg group-hover:block group-focus-within:block group-open:block")}>
          <Link to="/p/$projectId/trains" params={{ projectId }} className="block rounded-md px-2.5 py-2 text-[12px] text-ink-soft hover:bg-hover hover:text-ink">Trains</Link>
          <Link to="/p/$projectId/diagnostics" params={{ projectId }} className="block rounded-md px-2.5 py-2 text-[12px] text-ink-soft hover:bg-hover hover:text-ink">Diagnostics</Link>
        </div>
      </details>
    </nav>
  );
}

function notificationDetail(need: NeedItem) {
  if (need.humanAction) return need.humanAction.action;
  switch (need.kind) {
    case "clarify": return need.question;
    case "provision": return need.ask;
    case "approve-spec": return `Approve ${need.specTitle}.`;
    case "review": return "Review the available acceptance proof.";
    case "failed": return need.lastError || "The latest attempt failed.";
  }
}

function NotificationControl({ needs, error, open, onOpenChange }: { needs: NeedItem[]; error: boolean; open: boolean; onOpenChange: (open: boolean) => void }) {
  const count = needs.length;
  return (
    <div className="project-notification-control fixed right-4 top-4 z-40 shrink-0">
      <button type="button" onClick={() => onOpenChange(!open)} aria-expanded={open} aria-label={count > 0 ? `Notifications, ${count} items need you` : "Notifications"} title="Items needing you" className="relative flex h-10 w-10 items-center justify-center rounded-lg text-muted hover:bg-hover hover:text-ink">
        <Bell size={18} aria-hidden="true" />
        <span className="sr-only">Needs</span>
        {count > 0 && <span aria-hidden="true" className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-fail px-1 font-mono text-[9px] font-semibold leading-none text-surface">{count}</span>}
      </button>
      {open && <div className="project-notification-popover absolute right-0 top-full z-30 mt-2 w-80 overflow-hidden rounded-lg bg-raised shadow-lg ring-1 ring-ink/10">
        <p className="border-b border-line px-3 py-2 text-[11px] font-semibold text-ink">Needs your action</p>
        {error ? <p role="alert" className="px-3 py-2 text-[11px] leading-4 text-fail">Notifications could not be read.</p>
          : count === 0 ? <p role="status" className="px-3 py-2 text-[11px] text-muted">Nothing needs you.</p>
          : <div className="max-h-80 overflow-y-auto">{needs.map((need) => <Link key={need.id} to="/p/$projectId/docs" params={{ projectId: need.projectId }} search={{ path: need.taskId, ...(need.gateId ? { gate: need.gateId } : {}) }} onClick={() => onOpenChange(false)} className="block border-b border-line-soft px-3 py-2 last:border-0 hover:bg-hover">
            <p className="truncate text-[12px] font-medium text-ink">{need.taskTitle}</p>
            <p className="mt-0.5 line-clamp-2 text-[11px] leading-4 text-muted">{notificationDetail(need)}</p>
          </Link>)}</div>}
      </div>}
    </div>
  );
}

function ProjectContextControls({ compact }: { compact: boolean }) {
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
    <div className="project-context-controls flex min-w-0 items-center gap-2" data-project-context data-compact={compact}>
      <span aria-hidden="true" className={`h-1.5 w-1.5 shrink-0 rounded-full ${checkout.available ? checkout.activeRuns > 0 ? "bg-pass" : "bg-faint" : "bg-warn"}`} />
      {checkouts.length > 1 && <label className="sr-only" htmlFor="project-checkout">Registered checkout</label>}
      {checkouts.length > 1 ? (
        <select id="project-checkout" aria-label="Registered checkout" title={checkoutOptionLabel(checkout, branchCounts)} value={checkout.id} onChange={(event) => { try { window.localStorage.setItem(`tusker.navigation.checkout.${project.id}`, event.target.value); } catch { /* preference is best effort */ } window.location.assign(checkoutPath(event.target.value)); }} className="project-checkout-select max-w-[min(40vw,320px)] truncate rounded-md border border-line bg-surface px-2 py-1 font-mono text-[11px] text-ink outline-none focus:border-info">
          {checkouts.map((item) => <option key={item.id} value={item.id}>{checkoutOptionLabel(item, branchCounts)}</option>)}
        </select>
      ) : <span className="truncate font-mono text-[11px] text-faint">{checkoutOptionLabel(checkout, branchCounts)}</span>}
    </div>
  );

  function checkoutPath(nextID: string): string {
    if (typeof window === "undefined") return `/p/${encodeURIComponent(nextID)}`;
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
