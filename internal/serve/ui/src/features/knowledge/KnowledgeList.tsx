import { useState } from "react";
import { getRouteApi, Link } from "@tanstack/react-router";
import { AlertTriangle, ChevronRight, Network, Search } from "lucide-react";
import { Chip, Mono } from "@/components/ui/primitives";
import { PageHeader, PageScroll, SectionLabel } from "@/components/ui/page";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { useDocgraph } from "@/lib/queries";
import type { DocgraphDoc, DocgraphResponse } from "./types";
import { DocStatusChip, KindGlyph, KIND_ORDER, kindMeta } from "./bits";

const route = getRouteApi("/p/$projectId/knowledge");

export function KnowledgeList() {
  const { projectId } = route.useParams();
  return <KnowledgeListView projectId={projectId} />;
}

function KnowledgeListView({ projectId }: { projectId: string }) {
  const q = useDocgraph(projectId);
  const [query, setQuery] = useState("");

  return (
    <PageScroll>
      <PageHeader
        eyebrow={<SectionLabel>Knowledge</SectionLabel>}
        title="Documentation"
        subtitle="Canonical system docs, specs, and decision logs — and how they connect."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <label className="flex h-8.5 w-full items-center gap-2 rounded-lg border border-line bg-surface px-2.5 focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/20 sm:w-auto">
              <Search size={14} className="flex-none text-faint" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Filter title, subject, keyword…"
                className="w-full min-w-0 bg-transparent text-[13px] text-ink placeholder:text-faint focus:outline-none sm:w-56"
              />
            </label>
            <Link
              to="/p/$projectId/knowledge/graph"
              params={{ projectId }}
              className="inline-flex h-8.5 flex-none items-center gap-1.5 rounded-lg bg-ink px-3 text-[13px] font-semibold text-surface transition-opacity hover:opacity-90"
            >
              <Network size={14} strokeWidth={2} />
              Graph
            </Link>
          </div>
        }
      />
      <QueryBoundary q={q}>
        {(data) => <Corpus projectId={projectId} data={data} query={query} />}
      </QueryBoundary>
    </PageScroll>
  );
}

function Corpus({
  projectId,
  data,
  query,
}: {
  projectId: string;
  data: DocgraphResponse;
  query: string;
}) {
  const needle = query.trim().toLowerCase();
  const docs = needle
    ? data.docs.filter(
        (d) =>
          d.title.toLowerCase().includes(needle) ||
          d.subject.toLowerCase().includes(needle) ||
          d.keywords.some((k) => k.toLowerCase().includes(needle)),
      )
    : data.docs;

  if (data.docs.length === 0) {
    return (
      <EmptyState
        icon={<Network size={22} strokeWidth={1.5} />}
        title="No documentation corpus yet"
        hint="Canonical system docs, specs, and decision logs with doc-graph headers will appear here once the vault has them."
      />
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <GraphCta projectId={projectId} data={data} />

      {needle && docs.length === 0 ? (
        <EmptyState
          icon={<Search size={22} strokeWidth={1.5} />}
          title="No documents match"
          hint="Try a different title, subject, or keyword."
        />
      ) : (
        <div className="flex flex-col gap-7">
          {KIND_ORDER.map((kind) => {
            const group = docs.filter((d) => d.kind === kind);
            if (group.length === 0) return null;
            return (
              <section key={kind}>
                <div className="mb-2.5 flex items-center gap-2">
                  <SectionLabel>{kindMeta[kind].group}</SectionLabel>
                  <span className="font-mono text-[10.5px] text-fainter">{group.length}</span>
                </div>
                <div className="flex flex-col gap-1.5">
                  {group.map((d) => (
                    <DocRow key={d.subject} doc={d} projectId={projectId} />
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      )}
    </div>
  );
}

/** A first-class entry point into the graph, carrying corpus + lint counts. */
function GraphCta({ projectId, data }: { projectId: string; data: DocgraphResponse }) {
  const nodes = data.graph.nodes.length;
  const edges = data.graph.edges.length;
  const issues = data.issues.length;
  return (
    <Link
      to="/p/$projectId/knowledge/graph"
      params={{ projectId }}
      className="group flex items-center gap-4 rounded-xl border border-line bg-raised px-4 py-3.5 transition-colors hover:border-line-soft hover:bg-hover"
    >
      <span className="flex h-10 w-10 flex-none items-center justify-center rounded-lg bg-accent-soft text-accent">
        <Network size={19} strokeWidth={1.75} />
      </span>
      <div className="min-w-0 flex-1">
        <div className="text-[14px] font-semibold text-ink">Explore the document graph</div>
        <div className="mt-0.5 text-[12.5px] text-muted">
          <Mono className="text-[11.5px]">{nodes}</Mono> documents ·{" "}
          <Mono className="text-[11.5px]">{edges}</Mono> connections
        </div>
      </div>
      {issues > 0 && (
        <Chip tone="warn" variant="soft">
          <AlertTriangle size={12} strokeWidth={2} />
          {issues} {issues === 1 ? "issue" : "issues"}
        </Chip>
      )}
      <ChevronRight size={16} className="flex-none text-fainter transition-colors group-hover:text-muted" />
    </Link>
  );
}

function DocRow({ doc, projectId }: { doc: DocgraphDoc; projectId: string }) {
  const superseded = doc.status === "superseded";
  return (
    <Link
      to="/p/$projectId/knowledge/$subject"
      params={{ projectId, subject: doc.subject }}
      className="group flex items-center gap-3.5 rounded-xl border border-line bg-raised px-4 py-3 transition-colors hover:border-line-soft hover:bg-hover"
    >
      <KindGlyph kind={doc.kind} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span
            className={cnTitle(superseded)}
            title={doc.title}
          >
            {doc.title}
          </span>
          <DocStatusChip status={doc.status} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
          <Mono className="text-[11px] text-faint">{doc.subject}</Mono>
          {doc.keywords.slice(0, 4).map((k) => (
            <span
              key={k}
              className="rounded bg-hover px-1.5 py-px font-mono text-[10px] text-muted"
            >
              {k}
            </span>
          ))}
        </div>
      </div>
      <ChevronRight size={15} className="flex-none text-fainter transition-colors group-hover:text-muted max-lg:text-muted" />
    </Link>
  );
}

function cnTitle(superseded: boolean): string {
  return superseded
    ? "min-w-0 truncate text-[14px] font-medium text-muted line-through"
    : "min-w-0 truncate text-[14px] font-medium text-ink";
}
