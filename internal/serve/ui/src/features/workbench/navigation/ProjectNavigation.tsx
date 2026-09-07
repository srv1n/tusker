import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronRight, ListOrdered, Plus } from "lucide-react";
import { cn } from "@/lib/cn";
import type { ProjectSummary } from "@/types/domain";
import {
  MAX_WAVE_SHORTCUTS,
  moveProject,
  orderProjects,
  projectIdFromPath,
  projectLandingPath,
  projectWorkPath,
  readNavigationState,
  recordProjectVisit,
  reorderProject,
  setExpandedProjects,
  toggleProjectExpanded,
  waveSectionKind,
  writeNavigationState,
  type NavigationState,
  type WaveReadSnapshot,
} from "./navigationState";

export interface WaveLink {
  id: string;
  title: string;
  href: string;
  active: boolean;
}

export type WaveReadState = WaveReadSnapshot;

export interface ProjectNavigationProps {
  projects: ProjectSummary[];
  waveLinks: Record<string, WaveLink[]>;
  waveReads: Record<string, WaveReadState>;
  currentPath: string;
  onNavigate: (href: string) => void;
  onAddProject: () => void;
  onExpandedProjectsChange: (ids: string[]) => void;
}

function guardedStorage(): Storage | null {
  try {
    if (typeof window === "undefined" || !window.localStorage) return null;
    return window.localStorage;
  } catch {
    return null;
  }
}

function announceText(name: string, position: number, total: number): string {
  return `${name}, ${position} of ${total}`;
}

/**
 * ProjectNavigation — the persistent project and wave navigator (WUX-T-0003).
 *
 * Reorderable, expandable project list with up to five wave shortcuts per
 * expanded project. Order, expansion, active project and per-project last
 * screens persist in local storage via navigationState helpers. Selecting a
 * wave expands its parent; wave reads stay cached per project and only the
 * expanded subset is requested through onExpandedProjectsChange.
 */
export function ProjectNavigation({
  projects,
  waveLinks,
  waveReads,
  currentPath,
  onNavigate,
  onAddProject,
  onExpandedProjectsChange,
}: ProjectNavigationProps) {
  const projectIds = useMemo(() => projects.map((project) => project.id), [projects]);
  const [navState, setNavState] = useState<NavigationState>(() =>
    readNavigationState(guardedStorage(), projectIds),
  );
  const [announcement, setAnnouncement] = useState("");
  const [dragId, setDragId] = useState<string | null>(null);
  const [dropIndex, setDropIndex] = useState<number | null>(null);
  const restoreFocusId = useRef<string | null>(null);
  const expandedRef = useRef<string[]>(navState.expandedProjectIds);
  const onExpandedProjectsChangeRef = useRef(onExpandedProjectsChange);
  onExpandedProjectsChangeRef.current = onExpandedProjectsChange;

  // Reconcile saved state when the project set changes (added/removed projects).
  useEffect(() => {
    setNavState((prev) => {
      const known = new Set(projectIds);
      const ordered = prev.orderedProjectIds.filter((id) => known.has(id));
      for (const id of projectIds) {
        if (!ordered.includes(id)) ordered.push(id);
      }
      const expanded = prev.expandedProjectIds.filter((id) => known.has(id));
      const active = prev.activeProjectId !== null && known.has(prev.activeProjectId) ? prev.activeProjectId : null;
      if (
        ordered.length === prev.orderedProjectIds.length &&
        ordered.every((id, i) => id === prev.orderedProjectIds[i]) &&
        expanded.length === prev.expandedProjectIds.length &&
        active === prev.activeProjectId
      ) {
        return prev;
      }
      return { ...prev, orderedProjectIds: ordered, expandedProjectIds: expanded, activeProjectId: active };
    });
  }, [projectIds]);

  // Persist settled state; emit the expanded subset for bounded wave-link reads.
  useEffect(() => {
    writeNavigationState(guardedStorage(), navState);
  }, [navState]);

  useEffect(() => {
    const key = navState.expandedProjectIds.join("\0");
    if (expandedRef.current.join("\0") !== key) {
      expandedRef.current = navState.expandedProjectIds;
      onExpandedProjectsChangeRef.current(navState.expandedProjectIds);
    }
  }, [navState.expandedProjectIds]);

  // Restore focus to the moved project header after drag reorder re-renders.
  useEffect(() => {
    if (restoreFocusId.current === null) return;
    const id = restoreFocusId.current;
    restoreFocusId.current = null;
    document.querySelector<HTMLElement>(`[data-project-header="${CSS.escape(id)}"]`)?.focus();
  });

  const ordered = useMemo(() => orderProjects(projects, navState), [projects, navState]);
  const byId = useMemo(() => new Map(projects.map((project) => [project.id, project])), [projects]);

  const commitMove = (
    next: NavigationState,
    movedId: string | null,
    position: number,
    total: number,
    focusId: string | null,
  ) => {
    setNavState(next);
    if (movedId !== null) {
      const project = byId.get(movedId);
      setAnnouncement(announceText(project?.name ?? movedId, position, total));
    }
    if (focusId !== null) restoreFocusId.current = focusId;
  };

  const handleMove = (projectId: string, direction: -1 | 1) => {
    const result = moveProject(navState, projectIds, projectId, direction);
    commitMove(result.state, result.movedId, result.position, result.total, null);
  };

  const handleDrop = (targetIndex: number) => {
    if (dragId === null) return;
    const fromIndex = ordered.findIndex((project) => project.id === dragId);
    setDragId(null);
    setDropIndex(null);
    if (fromIndex < 0 || fromIndex === targetIndex) return;
    const result = reorderProject(navState, projectIds, fromIndex, targetIndex);
    commitMove(result.state, result.movedId, result.position, result.total, dragId);
  };

  const handleToggle = (projectId: string) => {
    setNavState((prev) => toggleProjectExpanded(prev, projectId));
  };

  const handleOpenProject = (projectId: string) => {
    const saved = navState.lastPathByProject[projectId];
    const target =
      saved !== undefined && projectIdFromPath(saved) === projectId
        ? saved
        : projectLandingPath(projectId);
    setNavState((prev) => recordProjectVisit(prev, projectIds, projectId, target));
    onNavigate(target);
  };

  const handleWaveOpen = (projectId: string, href: string) => {
    setNavState((prev) => {
      const expanded: string[] = prev.expandedProjectIds.includes(projectId)
        ? prev.expandedProjectIds
        : [...prev.expandedProjectIds, projectId];
      return recordProjectVisit(
        setExpandedProjects(prev, projectIds, expanded),
        projectIds,
        projectId,
        href,
      );
    });
    onNavigate(href);
  };

  return (
    <nav aria-label="Projects" className="flex min-h-0 flex-1 flex-col">
      <div aria-live="polite" role="status" className="sr-only">
        {announcement}
      </div>
      {ordered.length === 0 && (
        <div className="rounded-xl border border-dashed border-line px-3 py-4 text-center">
          <p className="text-[13px] font-medium text-ink">No projects yet</p>
          <p className="mt-1 text-[12px] text-muted">Add a project to see waves and work here.</p>
        </div>
      )}
      <ol className="tk-scroll min-h-0 flex-1 space-y-0.5 overflow-y-auto">
        {ordered.map((project, index) => {
          const expanded = navState.expandedProjectIds.includes(project.id);
          const links = dedupeLinks(waveLinks[project.id] ?? []).slice(0, MAX_WAVE_SHORTCUTS);
          const kind = waveSectionKind(waveReads[project.id], (waveLinks[project.id] ?? []).length);
          const read = waveReads[project.id];
          const isActive = navState.activeProjectId === project.id;
          return (
            <li
              key={project.id}
              data-project-item={project.id}
              onDragOver={(event) => {
                if (dragId === null || dragId === project.id) return;
                event.preventDefault();
                event.dataTransfer.dropEffect = "move";
                setDropIndex(index);
              }}
              onDrop={(event) => {
                event.preventDefault();
                handleDrop(index);
              }}
              className={cn(
                "rounded-lg",
                dropIndex === index && dragId !== null && "outline-2 outline-offset-[-2px] outline-info",
              )}
            >
              <div
                className={cn(
                  "group flex min-h-8 items-center gap-0.5 rounded-lg pr-1 text-sm transition-colors",
                  isActive ? "bg-hover/80 font-semibold text-ink" : "text-ink-soft hover:bg-hover/50",
                )}
              >
                <button
                  type="button"
                  draggable
                  data-project-header={project.id}
                  aria-label={`${project.name}${expanded ? ", expanded" : ", collapsed"}`}
                  title={project.name}
                  onClick={() => handleToggle(project.id)}
                  onDragStart={(event) => {
                    setDragId(project.id);
                    event.dataTransfer.effectAllowed = "move";
                    event.dataTransfer.setData("text/plain", project.id);
                  }}
                  onDragEnd={() => {
                    setDragId(null);
                    setDropIndex(null);
                  }}
                  onKeyDown={(event) => {
                    if (event.key === "ArrowUp" && (event.altKey || event.metaKey)) {
                      event.preventDefault();
                      handleMove(project.id, -1);
                    } else if (event.key === "ArrowDown" && (event.altKey || event.metaKey)) {
                      event.preventDefault();
                      handleMove(project.id, 1);
                    }
                  }}
                  className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-2 text-left text-sm outline-none focus-visible:ring-2 focus-visible:ring-info"
                >
                  {expanded ? (
                    <ChevronDown size={13} aria-hidden="true" className="shrink-0 text-faint" />
                  ) : (
                    <ChevronRight size={13} aria-hidden="true" className="shrink-0 text-faint" />
                  )}
                  <span className="min-w-0 flex-1 truncate" title={project.name}>
                    {project.name}
                  </span>
                  {project.needsCount > 0 && (
                    <span
                      aria-label={`${project.needsCount} needs you`}
                      className="shrink-0 rounded-full bg-fail-soft px-1.5 py-0.2 font-mono text-[10px] font-semibold text-fail"
                    >
                      {project.needsCount}
                    </span>
                  )}
                  {project.activeRuns > 0 && (
                    <span
                      aria-label={`${project.activeRuns} active runs`}
                      title={`${project.activeRuns} active runs`}
                      className="h-1.5 w-1.5 shrink-0 rounded-full bg-info"
                    />
                  )}
                </button>
                <span className="flex shrink-0 items-center opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
                  <button
                    type="button"
                    aria-label={`Move ${project.name} up`}
                    disabled={index === 0}
                    onClick={() => handleMove(project.id, -1)}
                    className="rounded p-1 text-[11px] font-semibold text-faint hover:bg-hover hover:text-ink focus-visible:opacity-100 disabled:opacity-30"
                  >
                    ↑
                  </button>
                  <button
                    type="button"
                    aria-label={`Move ${project.name} down`}
                    disabled={index === ordered.length - 1}
                    onClick={() => handleMove(project.id, 1)}
                    className="rounded p-1 text-[11px] font-semibold text-faint hover:bg-hover hover:text-ink focus-visible:opacity-100 disabled:opacity-30"
                  >
                    ↓
                  </button>
                </span>
              </div>

              {expanded && (
                <div className="ml-4 border-l border-line-soft pl-2">
                  <button
                    type="button"
                    onClick={() => handleOpenProject(project.id)}
                    className="block w-full rounded-md px-2.5 py-1.5 text-left text-[12px] font-medium text-muted hover:bg-hover hover:text-ink"
                  >
                    Work
                  </button>
                  <WaveShortcuts
                    projectId={project.id}
                    kind={kind}
                    links={links}
                    read={read}
                    currentPath={currentPath}
                    onOpen={(href) => handleWaveOpen(project.id, href)}
                    onShowMore={() => handleWaveOpen(project.id, projectWorkPath(project.id))}
                  />
                </div>
              )}
            </li>
          );
        })}
      </ol>
      <button
        type="button"
        onClick={onAddProject}
        className="mt-2 flex min-h-8 w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-[13px] font-medium text-muted hover:bg-hover hover:text-ink"
      >
        <Plus size={13} aria-hidden="true" />
        Add project
      </button>
      <p className="mt-1 flex items-center gap-1.5 px-3 text-[11px] text-faint">
        <ListOrdered size={11} aria-hidden="true" />
        Drag headers or use ↑ ↓ to reorder; order is saved.
      </p>
    </nav>
  );
}

function dedupeLinks(links: WaveLink[]): WaveLink[] {
  const seen = new Set<string>();
  return links.filter((link) => {
    if (seen.has(link.id)) return false;
    seen.add(link.id);
    return true;
  });
}

function WaveShortcuts({
  projectId,
  kind,
  links,
  read,
  currentPath,
  onOpen,
  onShowMore,
}: {
  projectId: string;
  kind: ReturnType<typeof waveSectionKind>;
  links: WaveLink[];
  read: WaveReadState | undefined;
  currentPath: string;
  onOpen: (href: string) => void;
  onShowMore: () => void;
}) {
  if (kind === "loading") {
    return (
      <p role="status" data-wave-state="loading" className="animate-pulse-soft px-2.5 py-1.5 text-[12px] text-faint">
        Loading waves…
      </p>
    );
  }
  if (kind === "error") {
    return (
      <p role="alert" data-wave-state="error" className="px-2.5 py-1.5 text-[12px] text-warn" title={read?.error ?? ""}>
        Waves unavailable{read?.error ? ` — ${read.error}` : ""}
      </p>
    );
  }
  if (kind === "empty") {
    return (
      <p data-wave-state="empty" className="px-2.5 py-1.5 text-[12px] text-faint">
        No waves yet
      </p>
    );
  }
  return (
    <ul data-wave-state="ready" aria-label="Recent waves" className="space-y-0.5">
      {links.map((link) => {
        const selected = link.href === currentPath || link.active;
        return (
          <li key={link.id}>
            <button
              type="button"
              onClick={() => onOpen(link.href)}
              aria-current={selected ? "page" : undefined}
              aria-label={`Wave ${link.title}`}
              title={link.title}
              className={cn(
                "block w-full truncate rounded-md px-2.5 py-1.5 text-left text-[12px] transition-colors",
                selected ? "bg-active font-semibold text-ink" : "text-muted hover:bg-hover hover:text-ink",
              )}
            >
              {link.title}
            </button>
          </li>
        );
      })}
      <li>
        <button
          type="button"
          onClick={onShowMore}
          aria-label={`Show all waves in project ${projectId}`}
          className="block w-full rounded-md px-2.5 py-1.5 text-left text-[12px] font-medium text-info hover:underline"
        >
          Show more
        </button>
      </li>
    </ul>
  );
}
