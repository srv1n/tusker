import { useCallback, useEffect, useMemo, useRef } from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { ArrowUpRight } from "lucide-react";
import { Mono } from "@/components/ui/primitives";
import { SectionLabel } from "@/components/ui/page";
import { ErrorState, Skeleton } from "@/components/ui/states";
import { cn } from "@/lib/cn";
import { useDocgraph, useDocgraphDoc } from "@/lib/queries";
import type {
  BacklinkVia,
  DocBacklink,
  DocgraphDocDetail,
  DocgraphKind,
  DocLinkRef,
} from "./types";
import { KindGlyph, ViaChip } from "./bits";
import { KnowledgeShell, SectionToolbar, ViewSwitch } from "./KnowledgeShell";
import { HeaderCard } from "./HeaderCard";
import { DocBodyEditor } from "./DocBodyEditor";
import { useDocgraphEditor, type KnowledgeDocEditor } from "./useDocgraphEditor";
import { ConflictNotice, DefectsNotice, ErrorNotice, SavedNotice } from "./banners";

const route = getRouteApi("/p/$projectId/knowledge/$subject");

export function KnowledgeReader() {
  const { projectId, subject } = route.useParams();
  return <DocSection projectId={projectId} subject={subject} />;
}

/**
 * The two-pane doc section for one subject: the explorer rail + the document.
 * Exported so the Docs index route can open it on the resolved root doc without
 * a router change.
 */
export function DocSection({ projectId, subject }: { projectId: string; subject: string }) {
  return (
    <KnowledgeShell projectId={projectId} currentSubject={subject}>
      <DocPane key={subject} projectId={projectId} subject={subject} />
    </KnowledgeShell>
  );
}

function DocPane({ projectId, subject }: { projectId: string; subject: string }) {
  const q = useDocgraphDoc(projectId, subject);
  const doc = q.data;
  if (!doc) {
    return (
      <div className="flex h-full flex-col">
        <SectionToolbar
          left={<ContextLabel subject={subject} />}
          right={<ViewSwitch projectId={projectId} active="files" />}
        />
        {q.isLoading ? (
          <BodySkeleton />
        ) : (
          <div className="p-8">
            <ErrorState error={q.error} onRetry={() => q.refetch()} />
          </div>
        )}
      </div>
    );
  }
  return <DocBody projectId={projectId} doc={doc} refetch={() => q.refetch()} />;
}

/** Slim toolbar context: the doc's kind glyph + subject. */
function ContextLabel({ kind, subject }: { kind?: DocgraphKind; subject: string }) {
  return (
    <div className="flex min-w-0 items-center gap-1.5">
      {kind && <KindGlyph kind={kind} size={14} />}
      <Mono className="min-w-0 truncate text-[12px] text-muted">{subject}</Mono>
    </div>
  );
}

function DocBody({
  projectId,
  doc,
  refetch,
}: {
  projectId: string;
  doc: DocgraphDocDetail;
  refetch: () => void;
}) {
  const ed = useDocgraphEditor(doc, projectId, refetch);
  const navigate = useNavigate();
  const graph = useDocgraph(projectId);
  const subjects = useMemo(() => (graph.data?.docs ?? []).map((d) => d.subject), [graph.data]);

  const linkMap = useMemo(() => new Map(doc.links.map((l) => [l.ref, l])), [doc.links]);
  const resolve = useCallback(
    (ref: string): DocLinkRef | undefined => linkMap.get(ref.trim()),
    [linkMap],
  );

  // Cmd/Ctrl+S saves. Route through a ref so the listener binds once but always
  // calls the latest save (which no-ops unless dirty).
  const saveRef = useRef(ed.save);
  saveRef.current = ed.save;
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        saveRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="flex h-full flex-col">
      <SectionToolbar
        left={<ContextLabel kind={doc.kind} subject={doc.subject} />}
        right={
          <>
            <SaveButton dirty={ed.dirty} saving={ed.saving} onSave={ed.save} />
            <ViewSwitch projectId={projectId} active="files" />
          </>
        }
      />
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

          <SaveBanners ed={ed} />

          <HeaderCard
            kind={doc.kind}
            subject={doc.subject}
            path={doc.path}
            status={ed.status}
            onStatusChange={ed.setStatus}
            keywords={ed.keywords}
            onAddKeyword={ed.addKeyword}
            onRemoveKeyword={ed.removeKeyword}
            partOf={ed.partOf}
            onPartOfChange={ed.setPartOf}
            subjects={subjects}
          />

          {/* The rendered document is the editor — always editable, styled to
              match the reader. Re-key per subject/rev so a reload loads fresh. */}
          <DocBodyEditor
            key={`${doc.subject}:${doc.rev}`}
            initialMarkdown={doc.body}
            resolve={resolve}
            onReady={ed.onBodyReady}
            onChange={ed.onBodyChange}
            onOpenWikilink={(subject) =>
              navigate({ to: "/p/$projectId/knowledge/$subject", params: { projectId, subject } })
            }
          />

          <Backlinks projectId={projectId} backlinks={doc.backlinks} />
        </article>
      </div>
    </div>
  );
}

function SaveBanners({ ed }: { ed: KnowledgeDocEditor }) {
  switch (ed.banner.type) {
    case "saved":
      return <SavedNotice warnings={ed.banner.warnings} onDismiss={ed.dismissBanner} />;
    case "conflict":
      return <ConflictNotice currentRev={ed.banner.currentRev} onReload={ed.reload} />;
    case "defects":
      return <DefectsNotice defects={ed.banner.defects} onDismiss={ed.dismissBanner} />;
    case "error":
      return <ErrorNotice message={ed.banner.message} onDismiss={ed.dismissBanner} />;
    default:
      return null;
  }
}

const VIA_ORDER: BacklinkVia[] = ["part_of", "updates", "decides_for", "superseded_by", "wiki"];

function Backlinks({ projectId, backlinks }: { projectId: string; backlinks: DocBacklink[] }) {
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

function SaveButton({ dirty, saving, onSave }: { dirty: boolean; saving: boolean; onSave: () => void }) {
  const enabled = dirty && !saving;
  return (
    <div className="flex flex-none items-center gap-2">
      {dirty && !saving && <Mono className="hidden text-[10.5px] text-warn sm:inline">unsaved</Mono>}
      <button
        type="button"
        onClick={onSave}
        disabled={!enabled}
        title="Save (⌘S)"
        className={cn(
          "flex h-7 items-center rounded-lg px-3 text-[12px] font-semibold leading-none transition-colors",
          enabled
            ? "bg-pass text-surface hover:opacity-90"
            : "cursor-not-allowed border border-line text-faint opacity-70",
        )}
      >
        {saving ? "Saving…" : "Save"}
      </button>
    </div>
  );
}

function BodySkeleton() {
  return (
    <div className="mx-auto w-full max-w-[46rem] px-4 pt-7 sm:px-6">
      <Skeleton className="mb-8 h-28 w-full" />
      <Skeleton className="mb-3 h-4 w-full" />
      <Skeleton className="mb-3 h-4 w-full" />
      <Skeleton className="mb-3 h-4 w-2/3" />
    </div>
  );
}
