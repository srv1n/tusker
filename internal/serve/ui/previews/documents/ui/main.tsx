import { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { ChevronRight, FileText, Folder, FolderOpen, Menu, Search } from "lucide-react";
import "@/styles/app.css";
import { ThemeProvider } from "@/lib/theme";
import { DocBodyEditor } from "@/features/knowledge/DocBodyEditor";
import { HeaderCard } from "@/features/knowledge/HeaderCard";
import { buildDocTree, type TreeNode } from "@/features/knowledge/tree";
import { kindMeta } from "@/features/knowledge/bits";
import type { DocgraphDoc, DocLinkRef } from "@/features/knowledge/types";

const DOCS: DocgraphDoc[] = [
  {
    subject: "overview",
    title: "System overview",
    path: "docs/system/00-overview.md",
    kind: "canonical",
    status: "canonical",
    keywords: ["system", "orientation"],
  },
  {
    subject: "runners-and-acp",
    title: "Runners and ACP",
    path: "docs/system/runners-and-acp.md",
    kind: "canonical",
    status: "canonical",
    keywords: ["agents", "ACP"],
    part_of: "overview",
  },
  {
    subject: "long-document",
    title: "A document with a long title that remains discoverable",
    path: "docs/system/a-document-with-a-long-name-that-truncates.md",
    kind: "spec",
    status: "draft",
    keywords: ["navigation"],
    part_of: "overview",
  },
  {
    subject: "work-area-redesign",
    title: "Tusker work experience",
    path: ".tusker/specs/work-area-redesign.md",
    kind: "spec",
    status: "canonical",
    keywords: ["work", "waves"],
    part_of: "overview",
  },
  {
    subject: "decision-log",
    title: "Decision log",
    path: ".tusker/specs/decisions/2026-09-07-choice.md",
    kind: "decision",
    status: "accepted",
    keywords: ["rationale"],
    part_of: "work-area-redesign",
  },
];

const BODY = `# System overview

Tusker is the management plane for work performed by installed coding agents. It keeps the intent, execution state, review result, and supporting documentation close enough to understand without opening another dashboard.

## Work moves through a wave

Tasks carry the contract. A wave makes their dependencies and progress visible. Agents implement and review by default; people step in only where the outcome needs a human judgment.

\`\`\`mermaid
flowchart LR
  Intent[Intent] --> Task[Task contract]
  Task --> Agent[Agent execution]
  Agent --> Review[Independent review]
  Review --> Result[Accepted result]
\`\`\`

## Source boundaries

The document tree points to real files. A document can link to another document with [[runners-and-acp]], while the graph remains an optional way to inspect relationships.

\`\`\`mermaid
this is intentionally invalid mermaid
\`\`\`
`;

function App() {
  const [subject, setSubject] = useState("overview");
  const [railOpen, setRailOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [status, setStatus] = useState("canonical");
  const [keywords, setKeywords] = useState(["system", "orientation"]);
  const [partOf, setPartOf] = useState("");
  const current = DOCS.find((doc) => doc.subject === subject) ?? DOCS[0]!;
  useEffect(() => {
    setStatus(current.status);
    setKeywords(current.keywords);
    setPartOf(current.part_of ?? "");
  }, [current]);
  useEffect(() => {
    if (!railOpen) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") setRailOpen(false);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [railOpen]);
  const filtered = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    return needle
      ? DOCS.filter((doc) => [doc.title, doc.subject, doc.path].some((value) => value.toLowerCase().includes(needle)))
      : DOCS;
  }, [filter]);

  return (
    <div data-documents-ready="true" className="flex min-h-screen bg-surface text-ink">
      <aside className="hidden w-64 flex-none border-r border-line bg-panel/35 lg:flex" aria-label="Documents explorer">
        <PreviewTree docs={filtered} filter={filter} onFilter={setFilter} subject={subject} onSelect={setSubject} />
      </aside>

      {railOpen && (
        <div className="fixed inset-0 z-40 lg:hidden">
          <button type="button" aria-label="Close documents explorer" className="absolute inset-0 cursor-default bg-black/25" onClick={() => setRailOpen(false)} />
          <aside role="dialog" aria-modal="true" tabIndex={-1} className="absolute inset-y-0 left-0 flex w-72 max-w-[85%] flex-col border-r border-line bg-surface shadow-lg" aria-label="Documents explorer">
            <PreviewTree docs={filtered} filter={filter} onFilter={setFilter} subject={subject} onSelect={(next) => { setSubject(next); setRailOpen(false); }} />
          </aside>
        </div>
      )}

      <main className="min-w-0 flex-1">
        <header className="sticky top-0 z-20 flex min-h-11 items-center gap-2 border-b border-line bg-surface/95 px-3 sm:px-5">
          <button type="button" className="flex h-8 w-8 items-center justify-center rounded-lg text-muted hover:bg-hover lg:hidden" aria-label="Open documents explorer" onClick={() => setRailOpen(true)}>
            <Menu size={16} />
          </button>
          <span className="font-mono text-[11px] text-muted">Documents</span>
          <span className="text-fainter">/</span>
          <span className="min-w-0 truncate font-mono text-[11px] text-faint">{current.subject}</span>
          <div className="ml-auto flex items-center gap-2">
            <span className="hidden font-mono text-[10px] uppercase tracking-[0.12em] text-fainter sm:inline">Sample data · preview</span>
            <div className="inline-flex rounded-lg border border-line bg-panel p-0.5" aria-label="Document view">
              <button type="button" className="rounded-md bg-raised px-2.5 py-1 text-[11.5px] font-semibold text-ink shadow-sm" aria-current="page">Files</button>
              <button type="button" className="rounded-md px-2.5 py-1 text-[11.5px] text-muted" onClick={() => setRailOpen(true)}>Graph</button>
            </div>
          </div>
        </header>

        <article className="mx-auto w-full max-w-[46rem] px-4 pb-24 pt-7 sm:px-6">
          <HeaderCard
            kind={current.kind}
            subject={current.subject}
            path={current.path}
            status={status}
            onStatusChange={setStatus}
            keywords={keywords}
            onAddKeyword={(keyword) => setKeywords((values) => values.includes(keyword) ? values : [...values, keyword])}
            onRemoveKeyword={(keyword) => setKeywords((values) => values.filter((value) => value !== keyword))}
            partOf={partOf}
            onPartOfChange={setPartOf}
            subjects={DOCS.map((doc) => doc.subject)}
          />
          <DocBodyEditor
            key={subject}
            initialMarkdown={subject === "overview" ? BODY : `# ${current.title}\n\nThis sample document keeps the same reader and editor surface.`}
            resolve={(ref): DocLinkRef | undefined => {
              const target = DOCS.find((doc) => doc.subject === ref.trim());
              return target ? { ref, subject: target.subject, path: target.path, resolved: true } : undefined;
            }}
            onReady={() => undefined}
            onChange={() => undefined}
            onOpenWikilink={setSubject}
            className="knowledge-prose"
          />
        </article>
      </main>
    </div>
  );
}

function PreviewTree({ docs, filter, onFilter, subject, onSelect }: { docs: DocgraphDoc[]; filter: string; onFilter: (filter: string) => void; subject: string; onSelect: (subject: string) => void }) {
  const roots = buildDocTree(docs);
  const [expanded, setExpanded] = useState<string[]>(["docs/system", ".tusker/specs/decisions"]);
  const isExpanded = (id: string) => expanded.includes(id);
  const toggle = (id: string) => setExpanded((values) => values.includes(id) ? values.filter((value) => value !== id) : [...values, id]);
  return (
    <div className="flex min-h-0 w-full flex-col">
      <div className="border-b border-line px-2 py-2">
        <label className="flex h-8 items-center gap-1.5 rounded-lg border border-line bg-surface px-2.5 focus-within:border-accent/50 focus-within:ring-2 focus-within:ring-accent/10">
          <Search size={12} className="text-faint" />
          <input aria-label="Filter documents" placeholder="Filter" value={filter} className="w-full min-w-0 bg-transparent text-[12px] outline-none placeholder:text-faint" onChange={(event) => onFilter(event.currentTarget.value)} />
        </label>
      </div>
      <nav className="tk-scroll min-h-0 flex-1 overflow-y-auto px-1.5 py-2" aria-label="Document files">
        {roots.length > 0 ? roots.map((root) => <PreviewNode key={root.id} node={root} subject={subject} expanded={isExpanded} onToggle={toggle} onSelect={onSelect} />) : <p className="px-2 py-8 text-center text-[11px] text-faint">No matches</p>}
      </nav>
    </div>
  );
}

function PreviewNode({ node, subject, expanded, onToggle, onSelect }: { node: TreeNode; subject: string; expanded: (id: string) => boolean; onToggle: (id: string) => void; onSelect: (subject: string) => void }) {
  if (node.type === "doc") {
    const filename = node.path.split("/").pop() ?? node.title;
    const meta = kindMeta[node.kind];
    return <button type="button" aria-current={node.subject === subject ? "page" : undefined} aria-label={`Open ${node.title}`} title={node.title} onClick={() => onSelect(node.subject)} className={`relative flex min-h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[12px] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50 ${node.subject === subject ? "bg-active text-ink" : "text-ink-soft hover:bg-hover"}`} style={{ paddingLeft: 8 + node.depth * 12 }}><FileText size={14} className="flex-none" style={{ color: `var(${meta.cssVar})` }} /><span className="min-w-0 truncate">{filename}</span></button>;
  }
  const open = expanded(node.id);
  return <div><button type="button" aria-expanded={open} aria-label={`${open ? "Collapse" : "Expand"} ${node.name}`} onClick={() => onToggle(node.id)} className="flex min-h-7 w-full items-center gap-1.5 rounded-md pr-2 text-left text-[12px] text-ink-soft hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/50" style={{ paddingLeft: 8 + node.depth * 12 }}><ChevronRight size={12} className={`flex-none text-faint transition-transform ${open ? "rotate-90" : ""}`} />{open ? <FolderOpen size={14} className="flex-none text-faint" /> : <Folder size={14} className="flex-none text-faint" />}<span className="truncate">{node.name}</span></button>{open && <div>{node.children.map((child) => <PreviewNode key={child.id} node={child} subject={subject} expanded={expanded} onToggle={onToggle} onSelect={onSelect} />)}</div>}</div>;
}

createRoot(document.getElementById("root")!).render(<ThemeProvider><App /></ThemeProvider>);
