import { useMemo, useRef, useState } from "react";
import { Pencil } from "lucide-react";
import { useNavigate } from "@tanstack/react-router";
import { cn } from "@/lib/cn";
import { Mono } from "@/components/ui/primitives";
import { Skeleton, ErrorState } from "@/components/ui/states";
import { useDoc } from "@/lib/queries";
import { relativeTime } from "@/lib/time";
import type { DocContent } from "@/types/domain";
import { DocEditor, type EditorRuntimeConfig } from "@/features/editor";
import { DocShell } from "./DocShell";
import { Outline } from "./Outline";
import { PropertyPanel } from "./PropertyPanel";
import { KindEyebrow } from "./bits";
import { ApproveBanner, ConflictBanner, SavedBanner, ValidationStrip } from "./banners";
import { useDocEditor } from "./editor";
import { approvalContextFor, localDocContents, resolveWikilink, wikilinkTargets } from "./mock";

const barBtn =
  "rounded-lg px-3.5 py-1.5 text-[12.5px] font-semibold leading-none transition-colors";

export function DocReader({ projectId, path }: { projectId: string; path: string }) {
  const q = useDoc(path);
  const local = localDocContents[path];
  const doc = q.data ?? local;

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
 * line plus its immediately-following blank lines) so a save can re-attach the
 * title the editor never renders. `joinLeadingH1` is the exact inverse.
 */
function splitLeadingH1(md: string): { prefix: string; body: string } {
  const body = stripLeadingH1(md);
  if (body === md) return { prefix: "", body };
  const lines = md.split("\n");
  const bodyLineCount = body === "" ? 0 : body.split("\n").length;
  const prefix = lines.slice(0, lines.length - bodyLineCount).join("\n");
  return { prefix, body };
}

/** Re-attach a split title prefix to an edited body. Inverse of splitLeadingH1. */
function joinLeadingH1(prefix: string, body: string): string {
  return prefix ? prefix + "\n" + body : body;
}

function ReaderBody({ projectId, doc }: { projectId: string; doc: DocContent }) {
  const ed = useDocEditor(doc);
  const navigate = useNavigate();
  const [approved, setApproved] = useState(false);
  const editorHostRef = useRef<HTMLDivElement>(null);

  const editing = ed.phase === "editing";
  const status = fm(doc, "status") ?? "";
  const docId = fm(doc, "id");
  const approval = approvalContextFor(doc.path, status);
  const words = ed.content.trim().split(/\s+/).filter(Boolean).length;

  // The leading `# Title` renders separately (big serif). Keep it out of the
  // editor so it isn't duplicated, and re-attach it on every change.
  const { prefix, body } = useMemo(() => splitLeadingH1(ed.content), [ed.content]);

  const editorConfig = useMemo<EditorRuntimeConfig>(
    () => ({
      resolveWikilink,
      wikilinkIndex: Object.values(wikilinkTargets),
      placeholder: "Write…",
      onOpenWikilink: ({ target, resolved }) =>
        navigate({
          to: "/p/$projectId/docs",
          params: { projectId },
          search: { path: resolved?.path ?? resolved?.id ?? target },
        }),
    }),
    [projectId, navigate],
  );

  // Focus the ProseMirror surface (e.g. the validation strip's "Fix errors").
  const focusEditor = () =>
    editorHostRef.current?.querySelector<HTMLElement>(".tk-prose")?.focus();

  const bodyEditor = (
    <DocEditor
      key={`${doc.path}:${ed.stateRev}:${editing ? "edit" : "read"}`}
      initialMarkdown={body}
      editable={editing}
      config={editorConfig}
      onChange={(bodyMd) => ed.setDraft(joinLeadingH1(prefix, bodyMd))}
      className={editing ? "tk-doc-editing" : undefined}
    />
  );

  const actions = editing ? (
    <>
      <Mono className="mr-1 text-[10.5px] text-warn">editing · markdown</Mono>
      <button className={cn(barBtn, "border border-line text-muted hover:bg-hover")} onClick={ed.cancelEdit}>
        Cancel
      </button>
      <button className={cn(barBtn, "bg-pass text-surface hover:opacity-90")} onClick={ed.save}>
        Save
      </button>
    </>
  ) : (
    <button
      className={cn(barBtn, "flex items-center gap-1.5 border border-line bg-raised text-ink-soft hover:border-line-soft hover:bg-hover")}
      onClick={ed.startEdit}
    >
      <Pencil size={13} strokeWidth={2} />
      Edit
    </button>
  );

  return (
    <DocShell projectId={projectId} path={doc.path} actions={actions}>
      <div className="mx-auto flex w-full max-w-[1180px] gap-9 px-11 pb-24 pt-7">
        <div className="hidden w-[188px] flex-none xl:block">
          {!editing && <Outline entries={doc.outline} />}
        </div>

        <article className="min-w-0 flex-1">
          <div className="mx-auto max-w-[42rem]">
            {/* Approve / approved confirmation */}
            {approved ? (
              <div className="mb-6 flex animate-rise items-center gap-2.5 rounded-xl border border-pass/30 bg-pass-soft px-4 py-3">
                <span className="h-2 w-2 flex-none rounded-full bg-pass" />
                <span className="text-[13.5px] text-ink-soft">
                  Spec approved · gate cleared. Downstream tasks unblocked.
                </span>
              </div>
            ) : (
              approval &&
              !editing && (
                <ApproveBanner
                  blocked={approval.blocked}
                  onApprove={() => setApproved(true)}
                  onRequestChanges={ed.startEdit}
                />
              )
            )}

            {/* Editor state banners */}
            {ed.banner.type === "conflict" && (
              <ConflictBanner conflict={ed.banner.conflict} onReconcile={ed.reconcile} />
            )}
            {ed.banner.type === "invalid" && (
              <ValidationStrip issues={ed.banner.issues} onFix={focusEditor} onDiscard={ed.cancelEdit} />
            )}
            {ed.banner.type === "saved" && <SavedBanner rev={String(ed.banner.rev)} />}

            {/* Title block */}
            <KindEyebrow kind={doc.kind} className="mb-1.5" />
            {docId && <Mono className="mb-1 block text-[11.5px] text-faint">{docId}</Mono>}
            <h1 className="mb-4 font-serif text-[32px] font-semibold leading-[1.08] tracking-[-0.02em] text-ink">
              {doc.title}
            </h1>

            <PropertyPanel frontmatter={doc.frontmatter} />

            {/* Body — one inline-WYSIWYG surface for both reading and editing.
                Markdown stays the source of truth; the editor round-trips it. */}
            <div ref={editorHostRef}>
              {editing ? (
                <div className="animate-rise">
                  <div className="mb-2 flex items-center justify-between px-0.5">
                    <Mono className="text-[9.5px] uppercase tracking-[0.12em] text-fainter">
                      Editing
                    </Mono>
                    {ed.isDirty && <Mono className="text-[10px] text-warn">unsaved</Mono>}
                  </div>
                  {bodyEditor}
                </div>
              ) : (
                bodyEditor
              )}
            </div>
          </div>
        </article>

        {/* Right rail — reading meta + actions */}
        <aside className="hidden w-[248px] flex-none lg:block">
          <div className="sticky top-6">
            <div className="mb-4 overflow-hidden rounded-xl border border-line">
              <MetaRow k="Updated" v={relativeTime(doc.updatedAt)} />
              <MetaRow k="Revision" v={`state_rev ${ed.stateRev}`} />
              <MetaRow k="Words" v={words.toLocaleString()} />
              <MetaRow k="Kind" v={doc.kind} last />
            </div>
            {!editing && (
              <div className="flex flex-col gap-2">
                {approval && !approved ? (
                  <button
                    className="w-full rounded-lg bg-info py-2.5 text-[13px] font-semibold text-surface transition-opacity hover:opacity-90"
                    onClick={() => setApproved(true)}
                  >
                    Approve spec
                  </button>
                ) : (
                  <button
                    className="w-full rounded-lg bg-ink py-2.5 text-[13px] font-semibold text-surface transition-opacity hover:opacity-90"
                    onClick={ed.startEdit}
                  >
                    Open in editor
                  </button>
                )}
                <button
                  className="w-full rounded-lg py-1.5 text-[12px] text-faint transition-colors hover:text-ink"
                  onClick={() => navigator.clipboard?.writeText(doc.path).catch(() => {})}
                >
                  Copy vault path
                </button>
              </div>
            )}
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
      <div className="border-b border-line px-11 py-3.5">
        <Skeleton className="h-4 w-52" />
      </div>
      <div className="mx-auto flex w-full max-w-[1180px] gap-9 px-11 pt-7">
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
