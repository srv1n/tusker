import { getRouteApi, Link } from "@tanstack/react-router";
import { ArrowLeft, ArrowUpRight, CornerDownRight, Network } from "lucide-react";
import { Card, Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { useDocgraphDoc, useProjects } from "@/lib/queries";
import type { BacklinkVia, DocBacklink, DocgraphDocDetail } from "./types";
import { DocStatusChip, KindBadge, KindGlyph, ViaChip } from "./bits";
import { KnowledgeMarkdown } from "./Markdown";

const route = getRouteApi("/p/$projectId/knowledge/$subject");

export function KnowledgeReader() {
  const { projectId, subject } = route.useParams();
  return <Reader key={subject} projectId={projectId} subject={subject} />;
}

function stringField(header: Record<string, unknown>, key: string): string | undefined {
  const v = header[key];
  return typeof v === "string" && v.trim() !== "" ? v : undefined;
}

function stringArray(header: Record<string, unknown>, key: string): string[] {
  const v = header[key];
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

function Reader({ projectId, subject }: { projectId: string; subject: string }) {
  const q = useDocgraphDoc(projectId, subject);
  const doc = q.data;

  if (!doc) {
    if (q.isLoading) return <ReaderSkeleton projectId={projectId} />;
    return (
      <div className="flex h-full flex-col">
        <TopBar projectId={projectId} path={subject} />
        <div className="p-8">
          <ErrorState error={q.error} onRetry={() => q.refetch()} />
        </div>
      </div>
    );
  }
  return <ReaderBody projectId={projectId} doc={doc} />;
}

function ReaderBody({ projectId, doc }: { projectId: string; doc: DocgraphDocDetail }) {
  const partOf = stringField(doc.header, "part_of");
  const keywords = stringArray(doc.header, "keywords");

  return (
    <div className="flex h-full flex-col">
      <TopBar projectId={projectId} path={doc.path} />
      <div className="tk-scroll flex-1 overflow-y-auto">
        <article className="mx-auto w-full max-w-[46rem] px-4 pb-24 pt-7 sm:px-6">
          {doc.successor && (
            <Link
              to="/p/$projectId/knowledge/$subject"
              params={{ projectId, subject: doc.successor.subject }}
              className="mb-6 flex items-center gap-2.5 rounded-xl border border-warn/40 bg-warn-soft px-4 py-3 transition-opacity hover:opacity-90"
            >
              <ArrowUpRight size={16} className="flex-none text-warn" />
              <span className="min-w-0 flex-1 text-[13.5px] text-ink-soft">
                Superseded — replaced by{" "}
                <Mono className="font-semibold text-warn">{doc.successor.subject}</Mono>
              </span>
              <span className="flex-none text-[12px] font-semibold text-warn">Open →</span>
            </Link>
          )}

          {/* Header card — front-matter as typed facts, never raw YAML. */}
          <Card className="mb-8 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <KindBadge kind={doc.kind} />
              <DocStatusChip status={doc.status} />
            </div>
            <Mono className="mt-3 block text-[11.5px] text-faint">{doc.subject}</Mono>
            <h1
              className={
                doc.status === "superseded"
                  ? "mt-1 font-serif text-[30px] font-semibold leading-[1.1] tracking-[-0.02em] text-muted line-through"
                  : "mt-1 font-serif text-[30px] font-semibold leading-[1.1] tracking-[-0.02em] text-ink"
              }
            >
              {doc.title}
            </h1>
            {(partOf || keywords.length > 0) && (
              <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2">
                {partOf && (
                  <Link
                    to="/p/$projectId/knowledge/$subject"
                    params={{ projectId, subject: partOf }}
                    className="inline-flex items-center gap-1.5 rounded-md border border-line px-2 py-0.5 text-[11.5px] text-muted transition-colors hover:bg-hover hover:text-ink"
                  >
                    <CornerDownRight size={12} className="text-faint" />
                    Part of <Mono className="text-[10.5px] text-ink-soft">{partOf}</Mono>
                  </Link>
                )}
                {keywords.map((k) => (
                  <span key={k} className="rounded bg-hover px-1.5 py-0.5 font-mono text-[10.5px] text-muted">
                    {k}
                  </span>
                ))}
              </div>
            )}
            <Mono className="mt-3 block truncate text-[10.5px] text-fainter">{doc.path}</Mono>
          </Card>

          <KnowledgeMarkdown body={doc.body} projectId={projectId} links={doc.links} />

          <Backlinks projectId={projectId} backlinks={doc.backlinks} />
        </article>
      </div>
    </div>
  );
}

const VIA_ORDER: BacklinkVia[] = ["part_of", "updates", "decides_for", "superseded_by", "wiki"];

function Backlinks({
  projectId,
  backlinks,
}: {
  projectId: string;
  backlinks: DocBacklink[];
}) {
  if (backlinks.length === 0) return null;
  const ordered = [...backlinks].sort(
    (a, b) => VIA_ORDER.indexOf(a.via) - VIA_ORDER.indexOf(b.via) || a.title.localeCompare(b.title),
  );
  return (
    <section className="mt-12 border-t border-line pt-6">
      <div className="mb-3 flex items-center gap-2">
        <SectionLabel>Referenced by</SectionLabel>
        <span className="font-mono text-[10.5px] text-fainter">{backlinks.length}</span>
      </div>
      <div className="flex flex-col gap-1.5">
        {ordered.map((b) => (
          <Link
            key={`${b.subject}~${b.via}`}
            to="/p/$projectId/knowledge/$subject"
            params={{ projectId, subject: b.subject }}
            className="group flex items-center gap-3 rounded-lg border border-line bg-raised px-3.5 py-2.5 transition-colors hover:border-line-soft hover:bg-hover"
          >
            <KindGlyph kind={b.kind} size={14} />
            <div className="min-w-0 flex-1">
              <div className="truncate text-[13px] font-medium text-ink-soft group-hover:text-ink">{b.title}</div>
              <Mono className="text-[10.5px] text-faint">{b.subject}</Mono>
            </div>
            <ViaChip via={b.via} />
          </Link>
        ))}
      </div>
    </section>
  );
}

/** Sticky context bar: back to the corpus list + the vault path + a graph jump. */
function TopBar({ projectId, path }: { projectId: string; path: string }) {
  const projects = useProjects();
  const projectName = projects.data?.find((p) => p.id === projectId)?.name ?? projectId;
  return (
    <header className="sticky top-0 z-20 flex items-center gap-3 border-b border-line bg-surface/85 px-4 py-3 backdrop-blur-md sm:px-6">
      <Link
        to="/p/$projectId/knowledge"
        params={{ projectId }}
        className="flex flex-none items-center gap-1.5 font-mono text-[11.5px] text-faint transition-colors hover:text-ink"
      >
        <ArrowLeft size={13} strokeWidth={2} />
        {projectName} · Docs
      </Link>
      <Mono className="min-w-0 flex-1 truncate text-[11px] text-fainter">/ {path}</Mono>
      <Link
        to="/p/$projectId/knowledge/graph"
        params={{ projectId }}
        className="flex flex-none items-center gap-1.5 rounded-lg border border-line px-2.5 py-1 text-[11.5px] font-medium text-muted transition-colors hover:bg-hover hover:text-ink"
      >
        <Network size={13} strokeWidth={2} />
        Graph
      </Link>
    </header>
  );
}

function ReaderSkeleton({ projectId }: { projectId: string }) {
  return (
    <div className="flex h-full flex-col">
      <TopBar projectId={projectId} path="…" />
      <div className="mx-auto w-full max-w-[46rem] px-4 pt-7 sm:px-6">
        <Skeleton className="mb-8 h-28 w-full" />
        <Skeleton className="mb-3 h-4 w-full" />
        <Skeleton className="mb-3 h-4 w-full" />
        <Skeleton className="mb-3 h-4 w-2/3" />
      </div>
    </div>
  );
}
