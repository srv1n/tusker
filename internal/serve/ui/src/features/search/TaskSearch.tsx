import { useEffect, useMemo, useRef, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ArrowRight, Search, X } from "lucide-react";
import { Mono } from "@/components/ui/primitives";
import { api } from "@/lib/api";
import { qk, useProjects } from "@/lib/queries";
import type { GateDetail, TaskCapsule } from "@/types/domain";
import { gateDetailPath, searchRecords, taskDetailPath, type SearchRecord } from "./taskSearchModel";

export const TASK_SEARCH_OPEN_EVENT = "tusker:open-task-search";

export function openTaskSearch(): void {
  window.dispatchEvent(new Event(TASK_SEARCH_OPEN_EVENT));
}

export function TaskSearch() {
  const navigate = useNavigate();
  const projectsQ = useProjects();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const dialogRef = useRef<HTMLElement>(null);
  const openerRef = useRef<HTMLElement | null>(null);
  const projects = projectsQ.data ?? [];
  const searchableProjects = projects.filter((project) => project.health !== "error");
  const taskQueries = useQueries({
    queries: searchableProjects.map((project) => ({
      queryKey: qk.tasks(project.id),
      queryFn: () => api.tasks(project.id),
      enabled: open,
      staleTime: 10_000,
    })),
  });
  const gateQueries = useQueries({
    queries: searchableProjects.map((project) => ({
      queryKey: qk.gates(undefined, project.id),
      queryFn: () => api.gates(undefined, project.id),
      enabled: open,
      staleTime: 10_000,
    })),
  });

  useEffect(() => {
    const show = () => { openerRef.current = document.activeElement as HTMLElement | null; setOpen(true); };
    const shortcut = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLocaleLowerCase() === "k") {
        event.preventDefault();
        openerRef.current = document.activeElement as HTMLElement | null;
        setOpen(true);
      }
    };
    window.addEventListener(TASK_SEARCH_OPEN_EVENT, show);
    window.addEventListener("keydown", shortcut);
    return () => {
      window.removeEventListener(TASK_SEARCH_OPEN_EVENT, show);
      window.removeEventListener("keydown", shortcut);
    };
  }, []);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setActiveIndex(0);
    requestAnimationFrame(() => inputRef.current?.focus());
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setOpen(false);
        requestAnimationFrame(() => openerRef.current?.focus());
        return;
      }
      if (event.key !== "Tab") return;
      const root = dialogRef.current;
      if (!root) return;
      const focusable = Array.from(root.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'));
      if (focusable.length === 0) return;
      const first = focusable[0]!; const last = focusable[focusable.length - 1]!;
      if (!root.contains(document.activeElement)) { event.preventDefault(); first.focus(); }
      else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  const items = useMemo<SearchRecord[]>(
    () => searchableProjects.flatMap((project, index): SearchRecord[] => [
      ...(taskQueries[index]?.data ?? []).map((task: TaskCapsule): SearchRecord => ({
        kind: "task",
        projectId: project.id,
        projectName: project.name,
        id: task.id,
        title: task.title,
        status: task.status,
        task,
      })),
      ...(gateQueries[index]?.data ?? []).map((gate: GateDetail): SearchRecord => ({
        kind: "gate",
        projectId: project.id,
        projectName: project.name,
        id: gate.id,
        title: gate.title || gate.action || gate.id,
        status: gate.status,
        gate,
      })),
    ]),
    [searchableProjects, taskQueries, gateQueries],
  );
  const results = useMemo(() => searchRecords(items, query), [items, query]);
  const loading = projectsQ.isPending || [...taskQueries, ...gateQueries].some((recordQuery) => recordQuery.isPending);
  const failedCount = [...taskQueries, ...gateQueries].filter((recordQuery) => recordQuery.isError).length;

  useEffect(() => {
    setActiveIndex((current) => Math.min(current, Math.max(0, results.length - 1)));
  }, [results.length]);

  if (!open) return null;

  const close = () => {
    setOpen(false);
    requestAnimationFrame(() => openerRef.current?.focus());
  };
  const openResult = (item: SearchRecord) => {
    const path = item.kind === "task"
      ? taskDetailPath(item.projectId, item.id)
      : gateDetailPath(item.projectId, item.gate);
    close();
    if (window.location.pathname === "/panel" && window.tuskerShell?.openFull) {
      window.tuskerShell.openFull(path);
      return;
    }
    if (item.kind === "gate" && item.gate.blocks.length === 0) {
      void navigate({ to: "/p/$projectId/ops", params: { projectId: item.projectId } });
      return;
    }
    const taskId = item.kind === "task" ? item.id : item.gate.blocks[0]!;
    void navigate({
      to: "/p/$projectId/docs",
      params: { projectId: item.projectId },
      search: { path: taskId, gate: item.kind === "gate" ? item.id : undefined },
    });
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      close();
    } else if (event.key === "ArrowDown" && results.length > 0) {
      event.preventDefault();
      setActiveIndex((index) => (index + 1) % results.length);
    } else if (event.key === "ArrowUp" && results.length > 0) {
      event.preventDefault();
      setActiveIndex((index) => (index - 1 + results.length) % results.length);
    } else if (event.key === "Enter" && results[activeIndex]) {
      event.preventDefault();
      openResult(results[activeIndex]);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center bg-black/35 px-3 pt-[min(14vh,120px)] backdrop-blur-[2px]"
      onMouseDown={(event) => { if (event.target === event.currentTarget) close(); }}
    >
      <section
        role="dialog"
        aria-modal="true"
        ref={dialogRef}
        aria-label="Search tasks"
        className="w-full max-w-[640px] overflow-hidden rounded-xl border border-line bg-raised shadow-lg"
      >
        <div className="flex items-center gap-3 border-b border-line px-4">
          <Search size={17} className="flex-none text-faint" aria-hidden="true" />
          <input
            ref={inputRef}
            value={query}
            onChange={(event) => { setQuery(event.target.value); setActiveIndex(0); }}
            onKeyDown={onKeyDown}
            placeholder="Search tasks or gates by ID or title…"
            aria-label="Search tasks or gates by ID or title"
            aria-controls="task-search-results"
            aria-activedescendant={results[activeIndex] ? `task-search-result-${activeIndex}` : undefined}
            className="h-14 min-w-0 flex-1 bg-transparent text-[15px] text-ink outline-none placeholder:text-fainter"
          />
          <Mono className="hidden rounded border border-line px-1.5 py-0.5 text-[10px] text-faint sm:block">esc</Mono>
          <button type="button" onClick={close} aria-label="Close task search" className="rounded p-1.5 text-faint hover:bg-hover hover:text-ink">
            <X size={15} />
          </button>
        </div>

        <div id="task-search-results" role="listbox" className="tk-scroll max-h-[min(62vh,520px)] overflow-y-auto p-2">
          {!query.trim() ? (
            <SearchState title="Jump to work or a human gate" detail="Type a task or gate ID such as SRV-T-0030 or AOS-G-0001." />
          ) : loading && items.length === 0 ? (
            <SearchState title="Loading tasks…" detail="Reading task indexes from registered projects." />
          ) : results.length === 0 ? (
            <SearchState
              title="No matching task"
              detail={failedCount > 0 ? `${failedCount} project ${failedCount === 1 ? "index is" : "indexes are"} unavailable.` : "Check the ID or try part of the title."}
            />
          ) : (
            results.map((item, index) => (
              <button
                id={`task-search-result-${index}`}
                key={`${item.projectId}:${item.kind}:${item.id}`}
                type="button"
                role="option"
                aria-selected={index === activeIndex}
                onMouseEnter={() => setActiveIndex(index)}
                onClick={() => openResult(item)}
                className={`flex w-full items-center gap-3 rounded-lg px-3 py-3 text-left ${index === activeIndex ? "bg-hover" : "hover:bg-hover"}`}
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className={`rounded px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-[0.08em] ${item.kind === "gate" ? "bg-warn-soft text-warn" : "bg-hover text-muted"}`}>
                      {item.kind}
                    </span>
                    <Mono className="text-[12px] font-semibold text-ink">{item.id}</Mono>
                    <span className="truncate text-[11px] text-faint">{item.projectName}</span>
                  </div>
                  <div className="mt-1 truncate text-[13.5px] text-ink-soft">{item.title}</div>
                  {item.kind === "gate" && item.gate.blocks.length > 0 && (
                    <Mono className="mt-1 block truncate text-[10px] text-warn">blocks {item.gate.blocks.join(", ")}</Mono>
                  )}
                </div>
                <ArrowRight size={14} className="flex-none text-fainter" aria-hidden="true" />
              </button>
            ))
          )}
        </div>
        <div className="flex items-center justify-between border-t border-line-soft px-4 py-2 text-[10.5px] text-faint">
          <span>{failedCount > 0 && results.length > 0 ? `${failedCount} record ${failedCount === 1 ? "index unavailable" : "indexes unavailable"}` : "Gate results open the blocked task’s action"}</span>
          <Mono>↑↓ select · ↵ open</Mono>
        </div>
      </section>
    </div>
  );
}

function SearchState({ title, detail }: { title: string; detail: string }) {
  return (
    <div className="px-5 py-12 text-center">
      <div className="font-serif text-[18px] font-semibold text-ink-soft">{title}</div>
      <div className="mt-1.5 text-[12.5px] text-faint">{detail}</div>
    </div>
  );
}
