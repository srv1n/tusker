/*
  The docs explorer rail is a file explorer.

  Derives a filesystem-literal folder tree from the docgraph list's real paths,
  keeps the current document highlighted with its ancestor folders expanded, and
  offers a filter over title / subject / filename. Collapse state lives in the
  module store (treeStore) so it survives the per-route remount; the filter view
  is derived and never mutates that state, so clearing the filter restores it.

  Rows are flat, single-line, and left-aligned: [indent][chevron (folders)][icon]
  [label]. Files show their filename (the title is the tooltip). Indentation is a
  fixed step per depth with thin guide lines drawn per row, so a selected row can
  still highlight full-bleed across the rail.
*/

import { useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { AlertTriangle, ChevronRight, FileText, Folder, FolderOpen, Search, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { useDocgraph } from "@/lib/queries";
import { kindMeta } from "./bits";
import { buildDocTree, docMatches, ancestorFolderIds, type TreeFolder, type TreeNode } from "./tree";
import { useTreeStore } from "./treeStore";

const INDENT_STEP = 12;
const ROW_BASE_PAD = 8;
const CHEVRON_SLOT = 14;

function rowPad(depth: number): number {
  return ROW_BASE_PAD + depth * INDENT_STEP;
}

/** Thin vertical guide per ancestor level, aligned under each chevron slot. */
function Guides({ depth }: { depth: number }) {
  if (depth === 0) return null;
  return (
    <>
      {Array.from({ length: depth }, (_, i) => (
        <span
          key={i}
          aria-hidden
          className="pointer-events-none absolute inset-y-0 w-px bg-line-soft"
          style={{ left: ROW_BASE_PAD + i * INDENT_STEP + CHEVRON_SLOT / 2 }}
        />
      ))}
    </>
  );
}

export function KnowledgeTree({
  projectId,
  currentSubject,
}: {
  projectId: string;
  currentSubject?: string;
}) {
  const q = useDocgraph(projectId);
  const store = useTreeStore();
  const [filter, setFilter] = useState("");
  const needle = filter.trim().toLowerCase();
  const filtering = needle !== "";

  const docs = q.data?.docs ?? [];
  const currentPath = currentSubject
    ? docs.find((d) => d.subject === currentSubject)?.path
    : undefined;

  // Auto-expand the open document's ancestor folders (A2). Collapse keys are
  // scoped by projectId so state never bleeds across projects (every project has
  // a docs/system, a .tusker/specs, …).
  useEffect(() => {
    if (currentPath) {
      store.expandAncestors(ancestorFolderIds(currentPath).map((id) => `${projectId}:${id}`));
    }
  }, [currentPath, projectId, store]);

  const roots = useMemo(() => {
    const source = filtering ? docs.filter((d) => docMatches(d, needle)) : docs;
    return buildDocTree(source);
  }, [docs, filtering, needle]);

  return (
    <div
      aria-label="Documents explorer"
      aria-busy={q.isLoading && docs.length === 0}
      className="flex h-full min-h-0 w-full flex-col bg-panel/40"
      role="region"
    >
      <div className="flex-none border-b border-line px-1.5 py-1.5">
        <label className="flex h-8 items-center gap-1.5 rounded-lg border border-line bg-surface px-2.5 transition-colors focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/10">
          <Search size={12} className="flex-none text-faint" />
          <input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter"
            aria-label="Filter documents"
            className="w-full min-w-0 bg-transparent text-[12px] text-ink placeholder:text-faint focus:outline-none"
          />
          {filter !== "" && (
            <button
              type="button"
              onClick={() => setFilter("")}
              aria-label="Clear filter"
              className="flex-none text-faint transition-colors hover:text-ink"
            >
              <X size={12} />
            </button>
          )}
        </label>
      </div>

      <div className="tk-scroll min-h-0 flex-1 overflow-y-auto px-1.5 py-2">
        {q.isError && docs.length === 0 ? (
          <ErrorRail error={q.error} onRetry={() => q.refetch()} />
        ) : docs.length === 0 ? (
          q.isLoading ? <LoadingRail /> : <EmptyRail label="No documents" />
        ) : roots.length === 0 ? (
          <EmptyRail label="No matches" />
        ) : (
          roots.map((root) => (
            <TreeNodeRow
              key={root.id}
              node={root}
              projectId={projectId}
              currentSubject={currentSubject}
              filtering={filtering}
              store={store}
            />
          ))
        )}
      </div>
    </div>
  );
}

function TreeNodeRow({
  node,
  projectId,
  currentSubject,
  filtering,
  store,
}: {
  node: TreeNode;
  projectId: string;
  currentSubject?: string;
  filtering: boolean;
  store: ReturnType<typeof useTreeStore>;
}) {
  if (node.type === "doc") {
    const active = node.subject === currentSubject;
    const filename = node.path.split("/").pop() ?? node.title;
    return (
      <Link
        to="/p/$projectId/knowledge/$subject"
        params={{ projectId, subject: node.subject }}
        onClick={() => store.setRailOpen(false)}
        aria-current={active ? "page" : undefined}
        aria-label={`Open ${node.title}`}
        data-selected={active ? "true" : undefined}
        title={node.title}
        className={cn(
          "relative flex min-h-7 items-center rounded-md pr-2 transition-colors focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50",
          active ? "bg-active text-ink" : "text-ink-soft hover:bg-hover",
        )}
        style={{ paddingLeft: rowPad(node.depth) }}
      >
        <Guides depth={node.depth} />
        <span className="flex-none" style={{ width: CHEVRON_SLOT }} />
        <FileText
          size={14}
          strokeWidth={1.75}
          className="flex-none opacity-80"
          style={{ color: `var(${kindMeta[node.kind].cssVar})` }}
        />
        <span
          className={cn(
            "ml-1 min-w-0 flex-1 truncate text-left text-[12.5px]",
            active && "font-medium",
          )}
        >
          {filename}
        </span>
      </Link>
    );
  }

  return <FolderRow node={node} projectId={projectId} currentSubject={currentSubject} filtering={filtering} store={store} />;
}

function FolderRow({
  node,
  projectId,
  currentSubject,
  filtering,
  store,
}: {
  node: TreeFolder;
  projectId: string;
  currentSubject?: string;
  filtering: boolean;
  store: ReturnType<typeof useTreeStore>;
}) {
  // During filtering, folders are forced open and non-interactive so a stray
  // click cannot mutate the collapse state the operator will return to.
  const folderKey = `${projectId}:${node.id}`;
  const expanded = filtering || !store.isCollapsed(folderKey);
  const inner = (
    <>
      <Guides depth={node.depth} />
      <span className="flex flex-none items-center justify-center" style={{ width: CHEVRON_SLOT }}>
        <ChevronRight
          size={12}
          strokeWidth={2}
          className={cn("text-faint transition-transform", expanded && "rotate-90")}
        />
      </span>
      {expanded ? (
        <FolderOpen size={14} strokeWidth={1.75} className="flex-none text-faint" />
      ) : (
        <Folder size={14} strokeWidth={1.75} className="flex-none text-faint" />
      )}
      <span className="ml-1 min-w-0 flex-1 truncate text-left text-[12.5px] text-ink-soft">
        {node.name}
      </span>
    </>
  );
  const childrenId = folderDomId(projectId, node.id);
  return (
    <>
      {filtering ? (
        <div
          className="relative flex h-6 items-center pr-2"
          style={{ paddingLeft: rowPad(node.depth) }}
          title={node.name}
        >
          {inner}
        </div>
      ) : (
        <button
          type="button"
          onClick={() => store.toggleFolder(folderKey)}
          aria-expanded={expanded}
          aria-controls={childrenId}
          aria-label={`${expanded ? "Collapse" : "Expand"} ${node.name}`}
          title={node.name}
          className="relative flex min-h-7 w-full items-center rounded-md pr-2 transition-colors hover:bg-hover focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
          style={{ paddingLeft: rowPad(node.depth) }}
        >
          {inner}
        </button>
      )}
      {expanded && (
        <div id={childrenId}>
          {node.children.map((child) => (
            <TreeNodeRow
              key={child.id}
              node={child}
              projectId={projectId}
              currentSubject={currentSubject}
              filtering={filtering}
              store={store}
            />
          ))}
        </div>
      )}
    </>
  );
}

function folderDomId(projectId: string, id: string): string {
  return `documents-folder-${projectId}-${id}`.replace(/[^a-zA-Z0-9_-]/g, "-");
}

function EmptyRail({ label }: { label: string }) {
  return (
    <div role="status" className="flex flex-col items-center gap-2 px-4 py-10 text-center text-faint">
      <FileText size={18} strokeWidth={1.5} />
      <span className="text-[12px]">{label}</span>
    </div>
  );
}

function LoadingRail() {
  return (
    <div role="status" aria-label="Loading documents" className="flex flex-col gap-2 px-1 py-2">
      {Array.from({ length: 6 }, (_, index) => (
        <div
          key={index}
          aria-hidden="true"
          className={cn("h-7 animate-pulse rounded-md bg-hover", index % 3 === 2 ? "ml-5 w-4/5" : "w-full")}
        />
      ))}
    </div>
  );
}

function ErrorRail({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <div role="alert" className="flex flex-col gap-2.5 px-2 py-8 text-center">
      <AlertTriangle size={18} strokeWidth={1.75} className="mx-auto text-warn" />
      <p className="text-[12px] font-medium text-ink-soft">Documents unavailable</p>
      <p className="break-words text-[11px] leading-relaxed text-muted">
        {error instanceof Error ? error.message : "The documentation corpus could not be loaded."}
      </p>
      <button
        type="button"
        onClick={onRetry}
        className="mx-auto rounded-md border border-line px-2.5 py-1.5 text-[11.5px] font-medium text-ink-soft transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50"
      >
        Retry
      </button>
    </div>
  );
}
