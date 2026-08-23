import { useMemo } from "react";
import { useNavigate } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { Skeleton, ErrorState } from "@/components/ui/states";
import { useDoc, useDocList } from "@/lib/queries";
import { relativeTime } from "@/lib/time";
import type { DocContent, DocListEntry } from "@/types/domain";
import { DocEditor, type EditorRuntimeConfig, type WikilinkTargetLite } from "@/features/editor";
import { DocShell } from "./DocShell";
import { Outline } from "./Outline";
import { PropertyPanel } from "./PropertyPanel";
import { KindEyebrow } from "./bits";

/** A live vault entry → the lite wikilink target the editor resolves/links. */
function docToWikilink(d: DocListEntry): WikilinkTargetLite {
  const isFile = d.path.includes("/") || /\.md$/i.test(d.path);
  const id = isFile ? d.path.replace(/\.md$/i, "").split("/").pop() ?? d.path : d.path;
  // A bare id (task path) leaves `path` absent so the editor treats it as a task.
  return isFile ? { id, title: d.title, kind: d.kind, path: d.path } : { id, title: d.title, kind: d.kind };
}

export function DocReader({ projectId, path }: { projectId: string; path: string }) {
  const q = useDoc(path, projectId);
  const doc = q.data;

  if (!doc) {
    if (q.isLoading) return <ReaderSkeleton />;
    return (
      <div className="p-8">
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      </div>
    );
  }
  return <ReaderBody key={doc.path} projectId={projectId} doc={doc} />;
}

function fm(doc: DocContent, key: string): string | undefined {
  return doc.frontmatter.find((f) => f.key === key)?.value;
}

/** Split the leading `# Title` off so the title renders in the design's scale. */
function stripLeadingH1(md: string): string {
  const lines = md.split("\n");
  if (!lines[0]?.startsWith("# ")) return md;
  let i = 1;
  while (i < lines.length && (lines[i] ?? "").trim() === "") i++;
  return lines.slice(i).join("\n");
}

/**
 * Like {@link stripLeadingH1}, but also returns the stripped prefix (the `# `
 * line plus its immediately-following blank lines) for callers that need to
 * render the body without duplicating the title.
 */
function splitLeadingH1(md: string): { prefix: string; body: string } {
  const body = stripLeadingH1(md);
  if (body === md) return { prefix: "", body };
  const lines = md.split("\n");
  const bodyLineCount = body === "" ? 0 : body.split("\n").length;
  const prefix = lines.slice(0, lines.length - bodyLineCount).join("\n");
  return { prefix, body };
}

function ReaderBody({ projectId, doc }: { projectId: string; doc: DocContent }) {
  const navigate = useNavigate();
  const docsQ = useDocList(projectId);
  const liveDocs = docsQ.data;
  const docId = fm(doc, "id");
  const words = doc.markdown.trim().split(/\s+/).filter(Boolean).length;

  // The leading `# Title` renders separately (big serif). Keep it out of the
  // read-only body so it isn't duplicated.
  const { body } = useMemo(() => splitLeadingH1(doc.markdown), [doc.markdown]);

  const editorConfig = useMemo<EditorRuntimeConfig>(() => {
    const index: WikilinkTargetLite[] = (liveDocs ?? []).map(docToWikilink);
    const resolve = (id: string): WikilinkTargetLite | undefined => {
      const key = id.trim();
      return index.find((t) => t.id === key || t.path === key);
    };
    return {
      resolveWikilink: resolve,
      wikilinkIndex: index,
      placeholder: "Write…",
      onOpenWikilink: ({ target, resolved }) =>
        navigate({
          to: "/p/$projectId/docs",
          params: { projectId },
          search: { path: resolved?.path ?? resolved?.id ?? target },
        }),
    };
  }, [projectId, navigate, liveDocs]);

  const bodyEditor = (
    <DocEditor
      key={doc.path}
      initialMarkdown={body}
      editable={false}
      config={editorConfig}
    />
  );

  const actions = <Mono className="text-[10.5px] uppercase tracking-[0.08em] text-faint">read-only</Mono>;

  return (
    <DocShell projectId={projectId} path={doc.path} actions={actions}>
      <div className="mx-auto flex w-full max-w-[1180px] gap-9 px-4 pb-24 pt-7 sm:px-6 lg:px-11">
        <div className="hidden w-[188px] flex-none xl:block">
          <Outline entries={doc.outline} />
        </div>

        <article className="min-w-0 flex-1">
          <div className="mx-auto max-w-[42rem]">
            <div className="mb-6 flex items-center gap-2.5 rounded-xl border border-line bg-panel px-4 py-3">
              <span className="h-2 w-2 flex-none rounded-full bg-faint" />
              <span className="text-[13.5px] text-muted">
                This vault document is read-only here. Open the Knowledge editor for documents with durable CAS-backed editing.
              </span>
            </div>

            {/* Title block */}
            <KindEyebrow kind={doc.kind} className="mb-1.5" />
            {docId && <Mono className="mb-1 block text-[11.5px] text-faint">{docId}</Mono>}
            <h1 className="mb-4 font-serif text-[32px] font-semibold leading-[1.08] tracking-[-0.02em] text-ink">
              {doc.title}
            </h1>

            <PropertyPanel
              frontmatter={doc.frontmatter}
              readOnly
            />

            {/* Body — a read-only rendering of the vault markdown. */}
            <div>{bodyEditor}</div>
          </div>
        </article>

        {/* Right rail — reading meta + actions */}
        <aside className="hidden w-[248px] flex-none lg:block">
          <div className="sticky top-6">
            <div className="mb-4 overflow-hidden rounded-xl border border-line">
              <MetaRow k="Updated" v={relativeTime(doc.updatedAt)} />
              <MetaRow k="Revision" v={`state_rev ${fm(doc, "state_rev") ?? "unknown"}`} />
              <MetaRow k="Words" v={words.toLocaleString()} />
              <MetaRow k="Kind" v={doc.kind} last />
            </div>
            <div className="flex flex-col gap-2">
              <button
                className="w-full rounded-lg py-1.5 text-[12px] text-faint transition-colors hover:text-ink"
                onClick={() => navigator.clipboard?.writeText(doc.path).catch(() => {})}
              >
                Copy vault path
              </button>
            </div>
          </div>
        </aside>
      </div>
    </DocShell>
  );
}

function MetaRow({ k, v, last = false }: { k: string; v: string; last?: boolean }) {
  return (
    <div
      className={cn(
        "flex items-center justify-between gap-3 px-3.5 py-2.5",
        !last && "border-b border-line-soft",
      )}
    >
      <Mono className="text-[10.5px] text-faint">{k}</Mono>
      <Mono className="text-[11.5px] font-semibold text-ink-soft">{v}</Mono>
    </div>
  );
}

function ReaderSkeleton() {
  return (
    <div className="flex h-full flex-col">
      <div className="border-b border-line px-4 py-3.5 sm:px-6 lg:px-11">
        <Skeleton className="h-4 w-52" />
      </div>
      <div className="mx-auto flex w-full max-w-[1180px] gap-9 px-4 pt-7 sm:px-6 lg:px-11">
        <div className="hidden w-[188px] flex-none xl:block">
          <Skeleton className="h-40 w-full" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="mx-auto max-w-[42rem]">
            <Skeleton className="mb-3 h-3 w-20" />
            <Skeleton className="mb-5 h-9 w-3/4" />
            <Skeleton className="mb-7 h-16 w-full" />
            <Skeleton className="mb-3 h-4 w-full" />
            <Skeleton className="mb-3 h-4 w-full" />
            <Skeleton className="mb-3 h-4 w-2/3" />
          </div>
        </div>
        <div className="hidden w-[248px] flex-none lg:block">
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    </div>
  );
}
