import { useState } from "react";
import { ChevronRight, FileText, Search } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Mono } from "@/components/ui/primitives";
import { PageHeader, PageScroll, SectionLabel } from "@/components/ui/page";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { useDocList } from "@/lib/queries";
import { relativeTime } from "@/lib/time";
import type { DocKind, DocListEntry } from "@/types/domain";
import { KindGlyph, kindMeta } from "./bits";
import { localDocList } from "./mock";

const GROUP_ORDER: DocKind[] = ["spec", "decision", "knowledge", "task", "epic", "dashboard"];

function mergeDocs(remote: DocListEntry[]): DocListEntry[] {
  // TODO(api): the library index will include task contracts + all vault notes;
  // fixtures omit those, so screen-local entries fill the listing.
  const seen = new Set(remote.map((d) => d.path));
  const merged = [...remote, ...localDocList.filter((d) => !seen.has(d.path))];
  return merged.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
}

export function LibraryList({ projectId }: { projectId: string }) {
  const q = useDocList(projectId);
  const [query, setQuery] = useState("");

  return (
    <PageScroll>
      <PageHeader
        eyebrow={<SectionLabel>Library</SectionLabel>}
        title="Documents & contracts"
        subtitle="Specs, decisions, knowledge, and task contracts in the vault."
        actions={
          <label className="flex h-8.5 items-center gap-2 rounded-lg border border-line bg-surface px-2.5 focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/20">
            <Search size={14} className="text-faint" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Filter by title or path…"
              className="w-52 bg-transparent text-[13px] text-ink placeholder:text-faint focus:outline-none"
            />
          </label>
        }
      />
      <QueryBoundary q={q}>
        {(docs) => {
          const all = mergeDocs(docs);
          const needle = query.trim().toLowerCase();
          const filtered = needle
            ? all.filter(
                (d) =>
                  d.title.toLowerCase().includes(needle) || d.path.toLowerCase().includes(needle),
              )
            : all;

          if (filtered.length === 0) {
            return (
              <EmptyState
                icon={<FileText size={22} strokeWidth={1.5} />}
                title={needle ? "No documents match" : "The vault is empty"}
                hint={
                  needle
                    ? "Try a different title or path fragment."
                    : "Specs and decisions authored under tasks will appear here."
                }
              />
            );
          }

          return (
            <div className="flex flex-col gap-7">
              {GROUP_ORDER.map((kind) => {
                const group = filtered.filter((d) => d.kind === kind);
                if (group.length === 0) return null;
                return (
                  <section key={kind}>
                    <div className="mb-2.5 flex items-center gap-2">
                      <SectionLabel>{kindMeta[kind].label}</SectionLabel>
                      <span className="font-mono text-[10.5px] text-fainter">{group.length}</span>
                    </div>
                    <div className="flex flex-col gap-1.5">
                      {group.map((d) => (
                        <DocRow key={d.path} doc={d} projectId={projectId} />
                      ))}
                    </div>
                  </section>
                );
              })}
            </div>
          );
        }}
      </QueryBoundary>
    </PageScroll>
  );
}

function DocRow({ doc, projectId }: { doc: DocListEntry; projectId: string }) {
  return (
    <Link
      to="/p/$projectId/docs"
      params={{ projectId }}
      search={{ path: doc.path }}
      className="group flex items-center gap-3.5 rounded-xl border border-line bg-raised px-4 py-3 transition-colors hover:border-line-soft hover:bg-hover"
    >
      <KindGlyph kind={doc.kind} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[14px] font-medium text-ink">{doc.title}</div>
        <Mono className="truncate text-[11px] text-faint">{doc.path}</Mono>
      </div>
      <span className="flex-none text-[11.5px] text-faint tabular">{relativeTime(doc.updatedAt)}</span>
      <ChevronRight size={15} className="flex-none text-fainter transition-colors group-hover:text-muted" />
    </Link>
  );
}
