import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { Bell, ChevronDown, Plus, RefreshCw, Search, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { openTaskSearch } from "@/features/search/TaskSearch";
import { useDaemon, useProjectRefresh, useProjects, useRegisterProject } from "@/lib/queries";
import type { ProjectSummary } from "@/types/domain";

const PROJECT_NAV = [
  { label: "Today", to: "/p/$projectId" as const },
  { label: "Plan", to: "/p/$projectId/plan" as const },
  { label: "Epics", to: "/p/$projectId/epics" as const },
  { label: "Waves", to: "/p/$projectId/waves" as const },
  { label: "Tasks", to: "/p/$projectId/tasks" as const },
  { label: "Trains", to: "/p/$projectId/trains" as const },
  { label: "Knowledge", to: "/p/$projectId/knowledge" as const },
];

export function Sidebar({ open = false, onClose }: { open?: boolean; onClose?: () => void }) {
  const projects = useProjects();
  const daemon = useDaemon();
  const activeProject = useParams({ strict: false }).projectId as string | undefined;
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const [addingProject, setAddingProject] = useState(false);
  const [notificationsOpen, setNotificationsOpen] = useState(false);
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

  const health = daemon.isPending || projects.isPending
    ? "Checking"
    : !daemon.data?.connected
      ? "Offline"
      : projects.data?.some((project) => project.health === "error")
      ? "Limited"
      : "Healthy";
  const healthTone = health === "Healthy" ? "text-pass" : health === "Limited" ? "text-warn" : health === "Offline" ? "text-fail" : "text-muted";

  return (
    <>
      {open && <button type="button" className="fixed inset-0 z-40 bg-black/35 lg:hidden" onClick={onClose} aria-label="Close navigation overlay" />}
      <aside
        ref={asideRef}
        tabIndex={-1}
        aria-label="Navigation"
        className={cn(
          "fixed inset-y-0 left-0 z-50 flex w-[240px] flex-none flex-col border-r border-line bg-panel transition-transform duration-200 focus:outline-none lg:visible lg:static lg:z-auto lg:translate-x-0",
          open ? "visible translate-x-0" : "max-lg:invisible -translate-x-full lg:translate-x-0",
        )}
      >
        <div className="flex h-[64px] items-center border-b border-line px-4">
          <Link to="/" className="flex items-center gap-2.5">
            <img src="/tusker-icon.png" alt="" aria-hidden="true" className="h-6.5 w-6.5 rounded-lg object-cover shadow-2xs" />
            <span className="text-[18px] font-bold tracking-[-0.035em] text-ink">tusker</span>
            <span className="rounded bg-hover px-1.5 py-0.5 font-mono text-[9.5px] uppercase tracking-[0.14em] text-faint">factory</span>
          </Link>
          {onClose && (
            <button type="button" onClick={onClose} aria-label="Close navigation" className="ml-auto rounded-lg p-1.5 text-faint hover:bg-hover hover:text-ink lg:hidden">
              <X size={16} />
            </button>
          )}
        </div>

        <nav className="tk-scroll flex-1 overflow-y-auto px-2.5 py-4 space-y-1">
          <RailLink active={pathname === "/"} to="/">
            <span>Today</span>
            <span className="font-mono text-[10px] text-faint">{projects.data?.reduce((sum, project) => sum + project.needsCount, 0) || ""}</span>
          </RailLink>
          <button
            type="button"
            onClick={openTaskSearch}
            className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-[13px] font-medium text-muted hover:bg-hover hover:text-ink transition-colors"
          >
            <Search size={14} />
            <span>Search</span>
            <span className="ml-auto rounded border border-line bg-surface px-1.5 py-0.5 font-mono text-[9px] text-faint shadow-2xs">⌘K</span>
          </button>
          <button
            type="button"
            onClick={() => setNotificationsOpen((value) => !value)}
            aria-expanded={notificationsOpen}
            className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left text-[13px] font-medium text-muted hover:bg-hover hover:text-ink transition-colors"
            title="Notification history is not exposed by the current Serve API"
          >
            <Bell size={14} />
            <span>Notifications</span>
          </button>
          {notificationsOpen && (
            <p role="status" className="mx-1 mt-1 rounded-lg border border-warn/30 bg-warn-soft px-2.5 py-2 text-[11px] leading-4 text-warn">
              Notification history is not exposed by the current Serve API.
            </p>
          )}

          <div className="mb-1.5 mt-6 flex items-center justify-between px-3">
            <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.14em] text-faint">Projects</span>
            <span className="font-mono text-[10px] text-faint">{projects.data?.length ?? 0}</span>
          </div>

          {projects.data?.map((project) => (
            <ProjectGroup
              key={project.id}
              project={project}
              active={project.id === activeProject}
              pathname={pathname}
            />
          ))}

          <button
            type="button"
            onClick={() => setAddingProject((value) => !value)}
            aria-label={addingProject ? "Close add project form" : "Add project"}
            className="mt-2 flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-[12px] font-medium text-muted hover:bg-hover hover:text-ink transition-colors"
          >
            {addingProject ? <X size={13} /> : <Plus size={13} />}
            {addingProject ? "Cancel" : "Add project"}
          </button>
          {addingProject && <AddProjectForm onDone={() => setAddingProject(false)} />}
        </nav>

        <div className="border-t border-line px-2.5 py-3 space-y-1">
          <Link to="/settings" className={cn("block rounded-lg px-3 py-1.5 text-[12px] font-medium transition-colors", pathname === "/settings" ? "bg-active font-semibold text-ink" : "text-muted hover:bg-hover hover:text-ink")}>
            Settings
          </Link>
          {activeProject && (
            <Link
              to="/p/$projectId/diagnostics"
              params={{ projectId: activeProject }}
              className={cn("block rounded-lg px-3 py-1.5 text-[12px] font-medium transition-colors", pathname.includes("/diagnostics") ? "bg-active font-semibold text-ink" : "text-muted hover:bg-hover hover:text-ink")}
            >
              Diagnostics
            </Link>
          )}
          <div className="mt-2 flex items-center justify-between border-t border-line-soft px-3 pt-2.5 text-[11px]">
            <span className="text-muted">Factory health</span>
            <span className={cn("flex items-center gap-1.5 font-semibold", healthTone)}>
              <span className={cn("h-1.5 w-1.5 rounded-full", health === "Healthy" ? "bg-pass" : health === "Limited" ? "bg-warn" : "bg-fail")} />
              {health}
            </span>
          </div>
        </div>
      </aside>
    </>
  );
}

function RailLink({ active, to, children }: { active: boolean; to: "/"; children: React.ReactNode }) {
  return (
    <Link
      to={to}
      className={cn(
        "flex items-center justify-between rounded-lg px-3 py-2 text-[13px] font-medium transition-colors",
        active ? "bg-ink font-semibold text-surface shadow-2xs" : "text-ink hover:bg-hover",
      )}
    >
      {children}
    </Link>
  );
}

function ProjectGroup({ project, active, pathname }: { project: ProjectSummary; active: boolean; pathname: string }) {
  const refresh = useProjectRefresh(project.id);

  return (
    <div className="mb-1">
      <div className={cn(
        "flex items-center rounded-lg text-[13px] transition-colors",
        active ? "bg-hover/80 font-semibold text-ink" : "text-ink-soft hover:bg-hover/50",
      )}>
        <Link
          to="/p/$projectId"
          params={{ projectId: project.id }}
          className="flex min-w-0 flex-1 items-center gap-2 px-3 py-2 hover:opacity-90"
        >
          <ChevronDown size={12} className={cn("text-faint transition-transform", active ? "" : "-rotate-90")} />
          <span className="min-w-0 flex-1 truncate">{project.name}</span>
          {project.needsCount > 0 && (
            <span className="rounded-full bg-fail-soft px-1.5 py-0.2 font-mono text-[10px] font-semibold text-fail">
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
          className="mr-2 rounded p-1 text-faint hover:bg-hover hover:text-ink disabled:cursor-wait disabled:opacity-50"
        >
          <RefreshCw size={12} className={refresh.isPending ? "animate-spin" : ""} />
        </button>
      </div>
      {refresh.error && (
        <p role="alert" className="ml-7 truncate px-2 pb-1 text-[10px] text-fail" title={String(refresh.error)}>
          Refresh failed — check this project’s source.
        </p>
      )}
      {project.health === "error" && (
        <Link
          to="/p/$projectId/settings"
          params={{ projectId: project.id }}
          aria-label={`Repair ${project.name} registration`}
          className="ml-7 block px-2 pb-1 text-[10.5px] font-semibold text-warn hover:text-ink"
        >
          Repair in Settings
        </Link>
      )}
      {active && (
        <div className="ml-4 mt-0.5 space-y-0.5 border-l border-line-soft pl-2">
          {PROJECT_NAV.map((item) => {
            const href = item.to.replace("$projectId", project.id);
            const selected = item.to.endsWith("$projectId") ? pathname === href : pathname.startsWith(href);
            return (
              <Link
                key={item.label}
                to={item.to}
                params={{ projectId: project.id }}
                className={cn(
                  "block rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors",
                  selected ? "bg-active font-semibold text-ink shadow-2xs" : "text-muted hover:bg-hover hover:text-ink",
                )}
              >
                {item.label}
              </Link>
            );
          })}
        </div>
      )}
    </div>
  );
}

function AddProjectForm({ onDone }: { onDone: () => void }) {
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
