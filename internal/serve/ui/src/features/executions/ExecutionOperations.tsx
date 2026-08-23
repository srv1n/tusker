import { useMemo, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { AlertTriangle, ChevronDown, ChevronRight, Filter, Inbox, RefreshCw, Search } from "lucide-react";
import { Card, Chip, Mono } from "@/components/ui/primitives";
import { Button, TextInput } from "@/components/ui/controls";
import { PageHeader, PageScroll, SectionLabel, Toolbar } from "@/components/ui/page";
import { EmptyState, QueryBoundary, SkeletonRows } from "@/components/ui/states";
import { useExecutionBind, useExecutionBindingPreview, useExecutionInbox, useExecutionRename, useExecutions, useExecutionTimeline } from "@/lib/queries";
import type { ExecutionGraph, ExecutionNode } from "@/types/domain";

type FilterName = "task" | "wave" | "provider_id" | "agent_type" | "source" | "binding" | "lifecycle";
const FILTERS: Array<{ key: FilterName; label: string }> = [
  { key: "task", label: "Task" }, { key: "wave", label: "Wave" }, { key: "provider_id", label: "Provider ID" },
  { key: "agent_type", label: "Agent type" }, { key: "source", label: "Source" }, { key: "binding", label: "Binding" }, { key: "lifecycle", label: "Lifecycle" },
];

/** Graph-specific drill-down nested under operations, never a competing product shell. */
export function ExecutionOperations() {
  const { projectId } = useParams({ strict: false }) as { projectId: string };
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Partial<Record<FilterName, string>>>({});
  const [selected, setSelected] = useState<string>("");
  const [showInbox, setShowInbox] = useState(false);
  const params = useMemo(() => ({ name: search || undefined, ...filters }), [search, filters]);
  const graph = useExecutions(projectId, params);
  const inbox = useExecutionInbox(projectId);
  // An inbox row may be excluded by the active graph filter; it must still
  // open its real detail and guarded binding form rather than becoming a dead
  // selection.
  const selectedNode = graph.data?.nodes.find((node) => node.execution_id === selected) ?? inbox.data?.executions.find((node) => node.execution_id === selected);

  return <PageScroll><div className="mx-auto max-w-[1180px] animate-rise">
    <PageHeader eyebrow={<Mono className="text-[11px] uppercase tracking-[0.14em] text-faint">operations / execution observability</Mono>} title="Executions" subtitle="Lineage, provider observations, and authority boundaries. Provider children are visible, never promoted into fake Tusker work." />
    <Toolbar className="mb-5">
      <label className="relative min-w-[14rem] flex-1"><Search aria-hidden="true" size={14} className="pointer-events-none absolute left-2.5 top-2.5 text-faint" /><TextInput value={search} onChange={(e) => setSearch(e.target.value)} aria-label="Search executions by name" placeholder="Search name, task, wave, provider ID…" className="w-full pl-8" /></label>
      <Button variant={showInbox ? "primary" : "default"} onClick={() => setShowInbox((value) => !value)}><Inbox size={14} /> Unbound inbox{inbox.data?.executions.length ? ` (${inbox.data.executions.length})` : ""}</Button>
    </Toolbar>
    <details className="mb-5 rounded-lg border border-line bg-panel px-3 py-2"><summary className="cursor-pointer text-[12px] font-medium text-muted"><Filter className="mr-1 inline" size={13} /> Filters</summary><div className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">{FILTERS.map(({ key, label }) => <label key={key} className="text-[10px] text-faint">{label}<TextInput aria-label={`Filter by ${label}`} value={filters[key] ?? ""} onChange={(e) => setFilters((old) => ({ ...old, [key]: e.target.value || undefined }))} className="mt-1 w-full" /></label>)}</div></details>
    {showInbox && <InboxPanel nodes={inbox.data?.executions ?? []} onSelect={setSelected} />}
    <QueryBoundary q={graph} loading={<SkeletonRows rows={7} />}>{(data) => <GraphPanel graph={data} selected={selected} onSelect={setSelected} />}</QueryBoundary>
    {selectedNode && <ExecutionDetail projectId={projectId} node={selectedNode} />}
  </div></PageScroll>;
}

function InboxPanel({ nodes, onSelect }: { nodes: ExecutionNode[]; onSelect: (id: string) => void }) {
  return <Card className="mb-5 border-warn/40 bg-warn-soft p-4"><SectionLabel>Unbound direct work</SectionLabel><p className="mt-1 text-[12px] text-muted">Binding starts a new authority generation. Earlier unbound history remains observable but never becomes proof eligible.</p>{nodes.length === 0 ? <p className="mt-3 text-[12px] text-muted">No unbound direct executions.</p> : <div className="mt-3 space-y-2">{nodes.map((node) => <button key={node.execution_id} type="button" onClick={() => onSelect(node.execution_id)} className="flex w-full items-center justify-between rounded border border-line bg-panel px-3 py-2 text-left hover:bg-hover"><span><strong className="text-[12px] text-ink">{node.effective_display_name || node.execution_id}</strong><Mono className="ml-2 text-[10px] text-faint">{node.execution_id}</Mono></span><Chip tone="warn">unbound</Chip></button>)}</div>}</Card>;
}

function GraphPanel({ graph, selected, onSelect }: { graph: ExecutionGraph; selected: string; onSelect: (id: string) => void }) {
  const children = new Map<string, ExecutionNode[]>(); const roots: ExecutionNode[] = [];
  for (const node of graph.nodes) { if (!node.parent_execution_id || !graph.nodes.some((x) => x.execution_id === node.parent_execution_id)) roots.push(node); else children.set(node.parent_execution_id, [...(children.get(node.parent_execution_id) ?? []), node]); }
  if (!graph.nodes.length) return <EmptyState title="No executions match these filters" hint="Try clearing a filter, or register a direct execution before the provider launch." />;
  return <section aria-label="Execution tree"><div className="mb-2 flex items-center justify-between"><SectionLabel>Execution tree</SectionLabel>{graph.partial_visibility && <span role="status" className="text-[11px] text-warn"><AlertTriangle className="mr-1 inline" size={13} /> Partial provider visibility</span>}</div><div className="space-y-1">{roots.map((node) => <TreeNode key={node.execution_id} node={node} depth={0} children={children} selected={selected} onSelect={onSelect} />)}</div>{graph.next_cursor && <p className="mt-3 text-[11px] text-muted">More executions exist. Refine the search to keep the graph relationship-complete.</p>}</section>;
}

function TreeNode({ node, depth, children, selected, onSelect }: { node: ExecutionNode; depth: number; children: Map<string, ExecutionNode[]>; selected: string; onSelect: (id: string) => void }) {
  const [open, setOpen] = useState(true); const kids = children.get(node.execution_id) ?? [];
  return <div style={{ marginLeft: `${Math.min(depth, 7) * 18}px` }}><div className={`flex min-w-0 items-center gap-2 rounded border px-2 py-2 ${selected === node.execution_id ? "border-info bg-info-soft" : "border-line bg-panel"}`}>
    <button type="button" onClick={() => setOpen(!open)} disabled={!kids.length} aria-label={`${open ? "Collapse" : "Expand"} ${node.effective_display_name || node.execution_id}`} className="text-faint disabled:invisible">{open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</button>
    <button type="button" onClick={() => onSelect(node.execution_id)} className="min-w-0 flex-1 text-left"><span className="block truncate text-[12px] font-semibold text-ink">{node.effective_display_name || node.execution_id}</span><Mono className="block truncate text-[10px] text-faint">{node.node_kind} · {node.execution_id}</Mono></button>
    <Chip tone={node.provider_owned ? "neutral" : "accent"}>{node.provider_owned ? "provider-owned" : "Tusker-managed"}</Chip>
    <Counts node={node} />
  </div>{open && kids.map((child) => <TreeNode key={child.execution_id} node={child} depth={depth + 1} children={children} selected={selected} onSelect={onSelect} />)}</div>;
}
function Counts({ node }: { node: ExecutionNode }) { const facts = [["active", node.active_children], ["failed", node.failed_children], ["attention", node.attention_children]].filter(([, n]) => n as number); return facts.length ? <span className="hidden gap-1 text-[10px] sm:flex">{facts.map(([label, n]) => <Chip key={label} tone={label === "failed" || label === "attention" ? "warn" : "accent"}>{label} {n}</Chip>)}</span> : null; }

function ExecutionDetail({ projectId, node }: { projectId: string; node: ExecutionNode }) {
  const [direction, setDirection] = useState<"tail" | "before" | "after">("tail"); const [cursor, setCursor] = useState<string | undefined>();
  const timeline = useExecutionTimeline(projectId, node.execution_id, { direction, cursor });
  const controls = node.controls;
  return <section className="mt-7 border-t-2 border-ink pt-6" aria-label="Execution detail"><PageHeader eyebrow={<Mono>{node.execution_id}</Mono>} title={node.effective_display_name || "Unnamed execution"} subtitle="Authoritative facts are shown by dimension; a provider update cannot claim, prove, or land work." />
    <div className="grid gap-5 lg:grid-cols-2"><Card className="p-4"><SectionLabel>Identity & lineage</SectionLabel><Facts values={{ root: node.root_execution_id, parent: node.parent_execution_id || "root", task: node.bound_task_id || node.task_id || "unbound", wave: node.bound_wave_id || node.wave_id || "unbound", provider: node.effective_provider_session_id || node.provider_child_handle || "not attached", agent: node.agent_type || "unknown", source: node.source || "unknown", binding: node.binding_generation ? `generation ${node.binding_generation} at ${node.binding_at}` : "no authority binding" }} /></Card>
      <Card className="p-4"><SectionLabel>Lifecycle dimensions</SectionLabel><Facts values={{ delivery: node.lifecycle.delivery_state, admission: node.lifecycle.admission_state, process: node.lifecycle.process_state, provider: node.lifecycle.provider_status, outcome: node.lifecycle.outcome_state, session: node.lifecycle.session_state, "child attention": node.lifecycle.child_attention_state, phase: node.lifecycle.derived_phase }} />{node.diagnostics.length > 0 && <p role="status" className="mt-3 border-l-2 border-warn pl-2 text-[11px] text-warn">{node.diagnostics.join(" · ")}</p>}</Card>
      <Card className="p-4"><SectionLabel>Controls & ownership</SectionLabel><p className="mt-1 text-[11px] text-muted">Only capability-proved controls are offered. This view never fabricates stop, resume, proof, claim, arm, land, release, or spending actions.</p><div className="mt-3 space-y-2">{controls.length ? controls.map((control) => <div key={`${control.action}-${control.target}`} className="flex items-center justify-between gap-3 text-[12px]"><span>{control.action} <span className="text-muted">{control.target}</span></span><Chip tone={control.available ? "accent" : "neutral"}>{control.available ? "available" : control.reason || "unavailable"}</Chip></div>) : <p className="text-[12px] text-muted">Capabilities are unknown or stale; no control is shown.</p>}</div></Card>
      <TimelinePanel timeline={timeline} direction={direction} onOlder={() => { setDirection("before"); setCursor(timeline.data?.previous_cursor); }} onNewer={() => { setDirection("after"); setCursor(timeline.data?.next_cursor); }} onReset={() => { setDirection("tail"); setCursor(undefined); }} /></div>
    {!node.proof_eligible && <BindingPanel projectId={projectId} node={node} />}
  </section>;
}
function BindingPanel({ projectId, node }: { projectId: string; node: ExecutionNode }) {
  const [name, setName] = useState(node.effective_display_name); const [taskId, setTaskId] = useState("");
  const rename = useExecutionRename(projectId); const bind = useExecutionBind(projectId); const preview = useExecutionBindingPreview(projectId, node.execution_id, taskId);
  return <Card className="mt-5 border-warn/40 bg-warn-soft p-4"><SectionLabel>Guarded binding</SectionLabel><p className="mt-1 text-[12px] text-muted">Rename and attach are audited operations. Binding checks canonical task/wave membership and live-owner conflicts; it creates a boundary, not retroactive proof.</p><div className="mt-3 grid gap-3 md:grid-cols-2"><label className="text-[11px] text-muted">Display name<TextInput aria-label="Execution display name" value={name} onChange={(e) => setName(e.target.value)} className="mt-1 w-full" /></label><div className="flex items-end"><Button variant="default" disabled={!name.trim() || rename.isPending} onClick={() => rename.mutate({ execution: node.execution_id, name })}>Rename</Button></div><label className="text-[11px] text-muted">Attach to task<TextInput aria-label="Task ID to bind execution" value={taskId} onChange={(e) => setTaskId(e.target.value)} placeholder="APP-T-0001" className="mt-1 w-full font-mono" /></label><div className="flex items-end"><Button variant="primary" disabled={!preview.data?.ok || bind.isPending} onClick={() => bind.mutate({ execution: node.execution_id, taskId })}>Bind from this point</Button></div></div>{taskId && <p role="status" className={`mt-3 text-[11px] ${preview.data?.ok ? "text-muted" : "text-warn"}`}>{preview.isLoading ? "Checking canonical wave and live owner…" : preview.data?.ok ? `Will bind to ${preview.data.task_id} / ${preview.data.wave_id}, generation ${preview.data.binding_generation}. ${preview.data.proof_boundary}` : preview.data?.error || "Binding is unavailable."}</p>}{(rename.error || bind.error) && <p role="alert" className="mt-2 text-[11px] text-fail">{String(rename.error || bind.error)}</p>}</Card>;
}
function Facts({ values }: { values: Record<string, string> }) { return <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[11px]"><>{Object.entries(values).map(([key, value]) => <div key={key} className="contents"><dt className="text-faint">{key}</dt><dd className="min-w-0 break-words font-mono text-muted">{value || "unknown"}</dd></div>)}</></dl>; }
function TimelinePanel({ timeline, direction, onOlder, onNewer, onReset }: { timeline: ReturnType<typeof useExecutionTimeline>; direction: string; onOlder: () => void; onNewer: () => void; onReset: () => void }) { return <Card className="p-4"><div className="flex items-center justify-between"><SectionLabel>Convergent timeline</SectionLabel><Button variant="ghost" onClick={onReset} aria-label="Reset timeline to authoritative tail"><RefreshCw size={13} /> Reset</Button></div><QueryBoundary q={timeline} loading={<SkeletonRows rows={3} />}>{(data) => <><p role="status" className="mt-1 text-[11px] text-muted">{data.reset || data.gap || data.stale_cursor ? "Cursor changed or has a gap. Reset fetches the authoritative tail." : "Ordered by source epoch and sequence; notifications are not authority."}</p><div className="mt-3 max-h-64 space-y-2 overflow-y-auto">{data.rows.length ? data.rows.map((row) => <div key={row.observation_id} className="border-l border-line pl-2 text-[11px]"><span className="font-medium text-ink">{row.status}</span> <span className="text-muted">{row.provider} · {row.occurred_at}</span><Mono className="block text-[9.5px] text-faint">{row.source_execution_id} / {row.source_epoch}:{row.source_sequence}</Mono></div>) : <p className="text-[12px] text-muted">No provider observations yet.</p>}</div><div className="mt-3 flex gap-2"><Button variant="default" disabled={!data.previous_cursor} onClick={onOlder}>Older</Button><Button variant="default" disabled={!data.next_cursor || direction === "after"} onClick={onNewer}>Newer</Button></div></>}</QueryBoundary></Card>; }
