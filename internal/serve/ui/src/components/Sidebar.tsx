import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { ChevronDown, Folder, FolderOpen, PanelLeftClose, PanelLeftOpen, Plus, RefreshCw, Settings, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { useProjectRefresh, useProjects, useRegisterProject } from "@/lib/queries";
import { projectContainsCheckout, projectVisibleInNavigation, type ProjectSummary } from "@/types/domain";
import { orderProjects, readNavigationState, setExpandedProjects, writeNavigationState, type NavigationState, type StorageLike } from "@/features/workbench/navigation/navigationState";

const PRIMARY_PROJECT_NAV = [
  { label: "Work", to: "/p/$projectId/waves" as const },
  { label: "Documents", to: "/p/$projectId/knowledge" as const },
  { label: "Settings", to: "/p/$projectId/settings" as const },
];

const SECONDARY_PROJECT_NAV = [
  { label: "Plan", to: "/p/$projectId/plan" as const },
  { label: "Board", to: "/p/$projectId/tasks" as const },
  { label: "Trains", to: "/p/$projectId/trains" as const },
  { label: "Diagnostics", to: "/p/$projectId/diagnostics" as const },
];

function guardedStorage(): StorageLike | null {
  try {
    return typeof window !== "undefined" && window.localStorage ? window.localStorage : null;
  } catch {
    return null;
  }
}

export function Sidebar({ open = false, collapsed = false, onClose, onToggleCollapsed }: { open?: boolean; collapsed?: boolean; onClose?: () => void; onToggleCollapsed?: () => void }) {
  const projects = useProjects();
  const activeProject = useParams({ strict: false }).projectId as string | undefined;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const [addingProject, setAddingProject] = useState(false);
  const [navigation, setNavigation] = useState<NavigationState | null>(null);
  const navigationProjectKey = useRef("");
  const asideRef = useRef<HTMLElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    openerRef.current = document.activeElement as HTMLElement | null;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); onClose?.(); return; }
      if (event.key !== "Tab") return;
      const root = asideRef.current;
      if (!root) return;
      const focusable = Array.from(root.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'));
      if (focusable.length === 0) return;
      const first = focusable[0]!; const last = focusable[focusable.length - 1]!;
      if (!root.contains(document.activeElement)) { event.preventDefault(); first.focus(); }
      else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    window.addEventListener("keydown", closeOnEscape);
    requestAnimationFrame(() => asideRef.current?.querySelector<HTMLElement>('a[href], button:not([disabled]), input:not([disabled])')?.focus());
    return () => window.removeEventListener("keydown", closeOnEscape);
  }, [open, onClose]);

  useEffect(() => {
    if (open) return;
    openerRef.current?.focus();
  }, [open]);

  useEffect(() => {
    if (!projects.data) return;
    const ids = projects.data.map((project) => project.id);
    const key = ids.join("\0");
    if (navigationProjectKey.current === key) return;
    navigationProjectKey.current = key;
    setNavigation(readNavigationState(guardedStorage(), ids));
  }, [projects.data]);

  useEffect(() => {
    if (navigation) writeNavigationState(guardedStorage(), navigation);
  }, [navigation]);

  const visibleProjects = projects.data?.filter(projectVisibleInNavigation) ?? [];
  return (
    <>
      {open && <button type="button" className="fixed inset-0 z-40 bg-black/35 lg:hidden" onClick={onClose} aria-label="Close navigation overlay" />}
      <aside
        ref={asideRef}
        tabIndex={-1}
        aria-label="Navigation"
        className={cn(
          "fixed inset-y-2 left-2 z-50 flex w-[256px] flex-none flex-col rounded-2xl border border-line bg-raised shadow-lg transition-[transform,width] duration-200 focus:outline-none lg:static lg:inset-auto lg:h-full lg:translate-x-0",
          collapsed ? "lg:w-[72px]" : "lg:w-[256px]",
          open ? "visible translate-x-0" : "invisible -translate-x-[calc(100%+1rem)] lg:visible lg:translate-x-0",
        )}
      >
        <div className={cn("flex h-12 shrink-0 items-center border-b border-line", collapsed ? "justify-center px-2" : "justify-end px-3.5") }>
          <div className={cn("ml-auto flex items-center gap-1", collapsed && "ml-0") }>
            <button
              type="button"
              onClick={() => void projects.refetch()}
              disabled={projects.isFetching}
              aria-busy={projects.isFetching}
              aria-label="Refresh projects"
              title={projects.isError ? "Refresh projects failed — try again" : "Refresh projects"}
              className="rounded-lg p-1.5 text-faint transition-colors hover:bg-hover hover:text-ink disabled:cursor-wait disabled:opacity-50"
            >
              <RefreshCw size={15} className={projects.isFetching ? "animate-spin" : ""} />
            </button>
            {onToggleCollapsed && (
              <button
                type="button"
                onClick={onToggleCollapsed}
                aria-label={collapsed ? "Expand navigation" : "Minimize navigation"}
                title={collapsed ? "Expand navigation" : "Minimize navigation"}
                className="hidden rounded-lg p-1.5 text-faint transition-colors hover:bg-hover hover:text-ink lg:block"
              >
                {collapsed ? <PanelLeftOpen size={16} /> : <PanelLeftClose size={16} />}
              </button>
            )}
            {onClose && (
              <button type="button" onClick={onClose} aria-label="Close navigation" className="rounded-lg p-1.5 text-faint hover:bg-hover hover:text-ink lg:hidden">
                <X size={16} />
              </button>
            )}
          </div>
        </div>

        <nav className={cn("tk-scroll flex-1 space-y-1 overflow-y-auto py-2", collapsed ? "px-2" : "px-2.5") }>
          <div className={cn("mb-1 mt-1 flex items-center justify-between px-3", collapsed && "sr-only") }>
            <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Projects</span>
            <span className="font-mono text-[10px] text-faint">{visibleProjects.length}</span>
          </div>

          {(navigation ? orderProjects(visibleProjects, navigation) : visibleProjects).map((project) => (
            <ProjectGroup
              key={project.id}
              project={project}
              active={projectContainsCheckout(project, activeProject ?? "")}
              pathname={pathname}
              activeProjectId={activeProject}
              collapsed={collapsed}
              expanded={navigation?.expandedProjectIds.includes(project.id) ?? project.id === activeProject}
              onExpandedChange={(expanded) => setNavigation((previous) => {
                if (!previous) return previous;
                const expandedProjectIds = new Set(previous.expandedProjectIds);
                if (expanded) expandedProjectIds.add(project.id);
                else expandedProjectIds.delete(project.id);
                return setExpandedProjects(previous, projects.data?.map((item) => item.id) ?? [], [...expandedProjectIds]);
              })}
            />
          ))}

          <button
            type="button"
            onClick={() => {
              if (collapsed) { onToggleCollapsed?.(); return; }
              setAddingProject((value) => !value);
            }}
            aria-label={addingProject ? "Close add project form" : "Add project"}
            title={collapsed ? "Expand navigation to add a project" : undefined}
            className={cn("mt-2 flex w-full items-center gap-2 rounded-lg py-2 text-left text-[12px] font-medium text-muted transition-colors hover:bg-hover hover:text-ink", collapsed ? "justify-center px-2" : "px-3")}
          >
            {addingProject ? <X size={13} /> : <Plus size={13} />}
            <span className={collapsed ? "sr-only" : undefined}>{addingProject ? "Cancel" : "Add project"}</span>
          </button>
          {addingProject && !collapsed && <AddProjectForm onDone={() => setAddingProject(false)} />}
        </nav>

        <div className={cn("space-y-1 border-t border-line py-3", collapsed ? "px-2" : "px-2.5") }>
          <Link to="/settings" title={collapsed ? "Settings" : undefined} className={cn("flex rounded-lg py-1.5 text-[12px] font-medium transition-colors", collapsed ? "justify-center px-2" : "px-3", pathname === "/settings" ? "bg-active font-semibold text-ink" : "text-muted hover:bg-hover hover:text-ink")}>
            <Settings size={14} className={collapsed ? undefined : "mr-2"} />
            <span className={collapsed ? "sr-only" : undefined}>Settings</span>
          </Link>
        </div>
      </aside>
    </>
  );
}

function ProjectGroup({ project, active, pathname, activeProjectId, collapsed, expanded, onExpandedChange }: { project: ProjectSummary; active: boolean; pathname: string; activeProjectId?: string; collapsed: boolean; expanded: boolean; onExpandedChange: (expanded: boolean) => void }) {
  const refresh = useProjectRefresh(project.id);
  const routeProjectId = active && activeProjectId ? activeProjectId : project.id;
  const selected = (to: string) => pathname === to || pathname.startsWith(`${to}/`);
  const secondarySelected = SECONDARY_PROJECT_NAV.some((item) => selected(item.to.replace("$projectId", routeProjectId)));

  return (
    <div className="mb-0.5">
      <div className={cn(
        "group flex items-center rounded-lg text-[13px] transition-colors",
        collapsed && "relative justify-center",
        active ? "bg-hover/80 font-semibold text-ink" : "text-ink-soft hover:bg-hover/50",
      )}>
        <Link
          to="/p/$projectId"
          params={{ projectId: project.id }}
          onClick={() => { if (!collapsed) onExpandedChange(active ? !expanded : true); }}
          aria-expanded={collapsed ? undefined : expanded}
          aria-label={collapsed ? `${project.name}${project.needsCount > 0 ? `, ${project.needsCount} items need you` : ""}` : undefined}
          title={collapsed ? project.name : undefined}
          className={cn("flex min-w-0 items-center gap-2 py-1.5 hover:opacity-90", collapsed ? "justify-center px-2" : "flex-1 px-3")}
        >
          {expanded ? <FolderOpen size={14} strokeWidth={1.75} className="shrink-0 text-faint" /> : <Folder size={14} strokeWidth={1.75} className="shrink-0 text-faint" />}
          <span className={cn("min-w-0 flex-1 truncate", collapsed && "sr-only")}>{project.name}</span>
          {project.needsCount > 0 && (
            <span className={cn("rounded-full bg-fail-soft px-1.5 py-0.2 font-mono text-[10px] font-semibold text-fail", collapsed && "absolute right-1 top-0.5 h-1.5 w-1.5 p-0 text-[0]")}>
              {project.needsCount}
            </span>
          )}
        </Link>
        <button
          type="button"
          onClick={() => refresh.mutate()}
          disabled={refresh.isPending}
          aria-busy={refresh.isPending}
          aria-label={`Refresh ${project.name}`}
          title={refresh.isError ? "Refresh failed — try again" : "Refresh project"}
          className={cn("mr-1 rounded p-1 text-fainter opacity-0 transition-opacity hover:bg-hover hover:text-ink focus-visible:opacity-100 group-hover:opacity-100 disabled:cursor-wait disabled:opacity-50", collapsed && "hidden")}
        >
          <RefreshCw size={12} className={refresh.isPending ? "animate-spin" : ""} />
        </button>
      </div>
      {!collapsed && refresh.error && (
        <p role="alert" className="ml-7 truncate px-2 pb-1 text-[10px] text-fail" title={String(refresh.error)}>
          Refresh failed — check this project’s source.
        </p>
      )}
      {!collapsed && expanded && (
        <div className="ml-5 mt-0.5 space-y-0.5 border-l border-line-soft pl-2">
          {PRIMARY_PROJECT_NAV.map((item) => {
            const href = item.to.replace("$projectId", routeProjectId);
            return (
              <Link
                key={item.label}
                to={item.to}
                params={{ projectId: routeProjectId }}
                className={cn(
                  "block rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors",
                  selected(href) ? "bg-active font-semibold text-ink shadow-2xs" : "text-muted hover:bg-hover hover:text-ink",
                )}
              >
                {item.label}
              </Link>
            );
          })}
          <details open={secondarySelected} className="group pt-1">
            <summary className="flex cursor-pointer list-none items-center gap-1 rounded-md px-2.5 py-1.5 text-[12px] font-medium text-muted hover:bg-hover hover:text-ink">
              <ChevronDown size={12} className="-rotate-90 transition-transform group-open:rotate-0" />
              More
            </summary>
            <div className="mt-0.5 space-y-0.5">
              {SECONDARY_PROJECT_NAV.map((item) => {
                const href = item.to.replace("$projectId", routeProjectId);
                return (
                  <Link
                    key={item.label}
                    to={item.to}
                    params={{ projectId: routeProjectId }}
                    className={cn(
                      "block rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors",
                      selected(href) ? "bg-active font-semibold text-ink shadow-2xs" : "text-muted hover:bg-hover hover:text-ink",
                    )}
                  >
                    {item.label}
                  </Link>
                );
              })}
            </div>
          </details>
        </div>
      )}
    </div>
  );
}

export function AddProjectForm({ onDone }: { onDone: () => void }) {
  const [repoRoot, setRepoRoot] = useState("");
  const [vaultRoot, setVaultRoot] = useState("");
  const [browsing, setBrowsing] = useState(false);
  const [browseHint, setBrowseHint] = useState<string | null>(null);
  const register = useRegisterProject();
  const navigate = useNavigate();
  const canBrowseFolders = typeof window.tuskerShell?.pickFolder === "function";
  const browseForFolder = async (setValue: (path: string) => void) => {
    const pickFolder = window.tuskerShell?.pickFolder;
    if (!pickFolder) {
      setBrowseHint("Browse is available in the Tusker macOS app. In a browser, enter the absolute path manually.");
      return;
    }
    setBrowseHint(null);
    setBrowsing(true);
    try {
      const path = await pickFolder();
      if (path) setValue(path);
    } finally {
      setBrowsing(false);
    }
  };
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    const result = await register.mutateAsync({
      repoRoot: repoRoot.trim(),
      ...(vaultRoot.trim() ? { vaultRoot: vaultRoot.trim() } : {}),
    });
    if (result.ok && result.projectId) {
      onDone();
      await navigate({ to: "/p/$projectId", params: { projectId: result.projectId } });
    }
  };
  return (
    <form onSubmit={submit} className="mx-1 mt-2 rounded-xl border border-line bg-raised p-3 shadow-2xs" data-add-project-form>
      <label className="font-mono text-[9.5px] uppercase tracking-[0.12em] text-faint">
        Repository path
        <div className="mt-1 flex">
          <input
            required
            autoFocus
            value={repoRoot}
            onChange={(event) => setRepoRoot(event.target.value)}
            placeholder="/Users/me/code/project"
            className="min-w-0 flex-1 rounded-l-md border border-line bg-surface px-2.5 py-1.5 font-mono text-[11px] normal-case tracking-normal text-ink outline-none focus:border-info"
          />
          <button type="button" onClick={() => void browseForFolder(setRepoRoot)} disabled={browsing} aria-label="Browse repository folder" className="rounded-r-md border border-l-0 border-line bg-panel px-2.5 text-[10px] font-medium normal-case tracking-normal text-muted hover:bg-hover hover:text-ink">
            Browse
          </button>
        </div>
      </label>
      <label className="mt-2.5 block font-mono text-[9.5px] uppercase tracking-[0.12em] text-faint">
        Vault path
        <div className="mt-1 flex">
          <input
            value={vaultRoot}
            onChange={(event) => setVaultRoot(event.target.value)}
            placeholder="defaults to .tusker"
            className="min-w-0 flex-1 rounded-l-md border border-line bg-surface px-2.5 py-1.5 font-mono text-[11px] normal-case tracking-normal text-ink outline-none focus:border-info"
          />
          <button type="button" onClick={() => void browseForFolder(setVaultRoot)} disabled={browsing} aria-label="Browse vault folder" className="rounded-r-md border border-l-0 border-line bg-panel px-2.5 text-[10px] font-medium normal-case tracking-normal text-muted hover:bg-hover hover:text-ink">
            Browse
          </button>
        </div>
      </label>
      {(!canBrowseFolders || browseHint) && <p className="mt-2 text-[10.5px] leading-4 text-faint">{browseHint ?? "Browse is available in the Tusker macOS app. In a browser, enter the absolute path manually."}</p>}
      <p className="mt-2 text-[10.5px] leading-4 text-faint">Registers only. Daemon automation stays off.</p>
      {register.data?.reason && <p className={cn("mt-2 text-[10.5px]", register.data.ok ? "text-pass" : "text-fail")}>{register.data.reason}</p>}
      <button
        type="submit"
        disabled={!repoRoot.trim() || register.isPending}
        className="mt-3 w-full rounded-lg bg-ink px-3 py-2 text-[11.5px] font-semibold text-surface shadow-2xs hover:opacity-90 disabled:opacity-40 transition-opacity"
      >
        {register.isPending ? "Registering…" : "Register project"}
      </button>
    </form>
  );
}
