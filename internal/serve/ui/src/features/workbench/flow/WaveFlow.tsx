/*
  WUX-T-0005 — readable interactive wave graph.

  WaveFlow renders real prerequisite relationships (from TaskDetail.deps)
  as directed, readable nodes and edges. Graph transform is a controlled
  viewport: selecting a task or receiving a live state update never resets
  it. State is icon + label (color is supplementary); model names appear
  only from supplied live-run facts. Outside-wave references without
  caller-confirmed facts render as "Unresolved dependency", never as
  missing or valid external work.
*/

import { useMemo } from "react";
import type { RunSummary, TaskDetail, WaveReview, WaveReviewMember } from "@/types/domain";
import {
  DISPLAY_STATE_LABEL,
  NODE_KIND_LABEL,
  NODE_WIDTH,
  TOP_DOWN_NODE_HEIGHT,
  buildFlowGraph,
  crossWaveWaitSummary,
  essentialFlowEdges,
  layoutTopDownFlowGraph,
  type DependencyFact,
  type FlowDisplayState,
  type FlowViewport,
} from "./flowGraph";

export interface WaveFlowProps {
  memberIds: string[];
  tasks: TaskDetail[];
  runs: RunSummary[];
  dependencyFacts?: Record<string, DependencyFact>;
  reviewMembers?: WaveReviewMember[];
  authorization?: WaveReview["authorization"];
  startEnabled?: boolean;
  selectedTaskId?: string;
  viewport?: FlowViewport;
  onSelectTask: (id: string) => void;
  onViewportChange: (value: FlowViewport) => void;
  loading?: boolean;
  error?: string;
}

function StateGlyph({ state }: { state: FlowDisplayState }) {
  const common = "flex-none";
  switch (state) {
    case "completed":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <circle cx="7" cy="7" r="6" fill="var(--color-pass-soft)" stroke="var(--color-pass)" strokeWidth="1.5" />
          <path d="M4.5 7.2 6.2 8.9 9.6 5.2" fill="none" stroke="var(--color-pass)" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      );
    case "executing":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <circle cx="7" cy="7" r="6" fill="var(--color-info-soft)" stroke="var(--color-info)" strokeWidth="1.5" />
          <circle cx="7" cy="7" r="2.4" fill="var(--color-info)" />
        </svg>
      );
    case "awaiting_review":
    case "reviewing":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <circle cx="7" cy="7" r="6" fill="var(--color-accent-soft)" stroke="var(--color-accent)" strokeWidth="1.5" />
          <path d="M7 4.4a2.6 2.6 0 1 0 0 5.2 2.6 2.6 0 0 0 0-5.2Z" fill="var(--color-accent)" />
        </svg>
      );
    case "ready":
    case "queued":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <circle cx="7" cy="7" r="6" fill="none" stroke="var(--color-muted)" strokeWidth="1.5" />
          <circle cx="7" cy="7" r="1.6" fill="var(--color-muted)" />
        </svg>
      );
    case "blocked":
    case "proof_blocked":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <rect x="2" y="2" width="10" height="10" rx="2.5" fill="var(--color-warn-soft)" stroke="var(--color-warn)" strokeWidth="1.5" />
          <path d="M4.5 4.5l5 5M9.5 4.5l-5 5" stroke="var(--color-warn)" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
      );
    case "failed":
    case "cancelled":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <circle cx="7" cy="7" r="6" fill="var(--color-fail-soft)" stroke="var(--color-fail)" strokeWidth="1.5" />
          <path d="M5 5l4 4M9 5 5 9" stroke="var(--color-fail)" strokeWidth="1.6" strokeLinecap="round" />
        </svg>
      );
    case "unknown":
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <circle cx="7" cy="7" r="6" fill="none" stroke="var(--color-faint)" strokeWidth="1.5" strokeDasharray="2 2" />
          <text x="7" y="10.4" textAnchor="middle" fontSize="8" fill="var(--color-faint)">?</text>
        </svg>
      );
  }
}

function edgePath(from: { x: number; y: number }, to: { x: number; y: number }): string {
  const x1 = from.x + NODE_WIDTH / 2;
  const y1 = from.y + TOP_DOWN_NODE_HEIGHT;
  const x2 = to.x + NODE_WIDTH / 2;
  const y2 = to.y;
  const bend = Math.max(28, (y2 - y1) / 2);
  return `M ${x1} ${y1} C ${x1} ${y1 + bend}, ${x2} ${y2 - bend}, ${x2} ${y2 - 3}`;
}

export function WaveFlow(props: WaveFlowProps) {
  const { memberIds, tasks, runs, dependencyFacts, reviewMembers, selectedTaskId, onSelectTask } = props;

  const graph = useMemo(
    () => buildFlowGraph({ memberIds, tasks, runs, dependencyFacts, reviewMembers }),
    [memberIds, tasks, runs, dependencyFacts, reviewMembers],
  );
  const layout = useMemo(() => layoutTopDownFlowGraph(graph, memberIds), [graph, memberIds]);
  const nodes = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph.nodes]);
  const edges = useMemo(() => essentialFlowEdges(graph), [graph]);
  const executing = graph.nodes.filter((node) => node.state === "executing");
  const reviewing = graph.nodes.filter((node) => node.state === "reviewing");
  const awaitingReview = graph.nodes.filter((node) => node.state === "awaiting_review");
  const waitSummary = crossWaveWaitSummary(
    Object.values(dependencyFacts ?? {}),
    props.authorization ?? "inert",
    props.startEnabled ?? false,
  );
  const executionStatus = executing.length > 0
    ? `Executing now: ${executing.map((node) => node.title).join(", ")}`
    : reviewing.length > 0
      ? `Reviewing now: ${reviewing.map((node) => node.title).join(", ")}`
      : awaitingReview.length > 0
        ? `Awaiting review: ${awaitingReview.map((node) => node.title).join(", ")}`
      : "Nothing is executing now.";

  if (props.loading) {
    return (
      <div className="rounded-lg border border-line bg-panel p-6" role="status" aria-label="Loading wave graph">
        <p className="text-[13px] text-muted">Loading task dependencies…</p>
      </div>
    );
  }

  if (props.error) {
    return (
      <div className="rounded-lg border border-line bg-panel p-6" role="alert">
        <p className="text-[14px] font-medium text-ink">Dependency graph unavailable</p>
        <p className="mt-1 text-[13px] text-muted">{props.error}</p>
      </div>
    );
  }

  if (memberIds.length === 0) {
    return (
      <div className="rounded-lg border border-line bg-panel p-6">
        <p className="text-[14px] font-medium text-ink">No tasks in this wave</p>
        <p className="mt-1 text-[13px] text-muted">There are no wave members to arrange yet.</p>
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border border-line bg-panel" aria-label="Wave dependency graph">
      <div className="flex flex-wrap items-baseline justify-between gap-2 border-b border-line px-4 py-3">
        <div>
          <h3 className="text-[14px] font-semibold text-ink">Execution plan</h3>
          <p className="mt-0.5 text-[12px] text-muted" aria-live="polite">{executionStatus}</p>
        </div>
      </div>

      {waitSummary && (
        <section aria-label="Cross-wave dependency wait" className="border-b border-line bg-surface px-4 py-3">
          <h4 className="text-[13px] font-semibold text-ink">{waitSummary.title}</h4>
          <p className="mt-1 text-[12px] leading-5 text-muted">{waitSummary.body}</p>
          {waitSummary.hint && <p className="mt-1 text-[12px] leading-5 text-muted">{waitSummary.hint}</p>}
        </section>
      )}

      {graph.warnings.length > 0 && (
        <details className="border-b border-line bg-surface px-4 py-2.5">
          <summary className="cursor-pointer text-[12.5px] font-medium text-warn">
            {graph.warnings.length} dependency {graph.warnings.length === 1 ? "issue" : "issues"}
          </summary>
          <ul className="mt-2 space-y-1.5 pl-4" aria-label="Graph warnings">
            {graph.warnings.map((warning, index) => <li key={`${warning.kind}-${index}`} className="list-disc text-[12px] leading-5 text-muted">{warning.text}</li>)}
          </ul>
        </details>
      )}

      <div className="overflow-x-auto bg-surface p-5" role="region" aria-label={`Dependency graph with ${graph.nodes.length} tasks. Scroll normally; arrows point from prerequisites to dependent tasks.`}>
        <div className="relative mx-auto" style={{ width: layout.width, height: layout.height }}>
          <svg width={layout.width} height={layout.height} className="absolute inset-0" aria-hidden="true">
            <defs>
              <marker id="wave-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M 0 1 L 9 5 L 0 9" fill="none" stroke="var(--color-muted)" strokeWidth="1.4" />
              </marker>
              <marker id="wave-arrow-cycle" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M 0 1 L 9 5 L 0 9" fill="none" stroke="var(--color-warn)" strokeWidth="1.4" />
              </marker>
            </defs>
            {edges.map((edge) => {
              const from = layout.positions[edge.from];
              const to = layout.positions[edge.to];
              if (!from || !to) return null;
              return <path key={`${edge.from}→${edge.to}`} d={edgePath(from, to)} fill="none" stroke={edge.cyclic ? "var(--color-warn)" : "var(--color-muted)"} strokeWidth={edge.cyclic ? 2 : 1.5} strokeDasharray={edge.cyclic ? "6 4" : undefined} markerEnd={edge.cyclic ? "url(#wave-arrow-cycle)" : "url(#wave-arrow)"} opacity="0.75" />;
            })}
          </svg>
          {layout.layers.flatMap((layer) =>
            layer.map((id) => {
                const node = nodes.get(id);
                if (!node) return null;
                const selected = selectedTaskId === node.id;
                const active = node.state === "executing" || node.state === "reviewing";
                const position = layout.positions[id];
                return (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => onSelectTask(node.id)}
                  aria-pressed={selected}
                  aria-label={`${node.title} (${node.id}), ${NODE_KIND_LABEL[node.kind]}, ${node.stateLabel ?? DISPLAY_STATE_LABEL[node.state]}${node.model ? `, model ${node.model}` : ""}${selected ? ", selected" : ""}`}
                  style={{ left: position.x, top: position.y, width: NODE_WIDTH, height: TOP_DOWN_NODE_HEIGHT }}
                  className={`absolute z-10 min-w-0 rounded-lg border bg-panel p-3 text-left shadow-sm transition-colors hover:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${selected || active ? "border-accent ring-1 ring-accent" : "border-line"} ${node.kind !== "task" ? "border-dashed" : ""}`}
                >
                  <span className="flex items-center gap-1.5 text-[11.5px] text-muted">
                    <StateGlyph state={node.state} />
                    <span className="font-medium">{node.stateLabel ?? DISPLAY_STATE_LABEL[node.state]}</span>
                    {node.kind !== "task" && <span className="ml-auto font-mono text-[9.5px] uppercase tracking-wide text-faint">{NODE_KIND_LABEL[node.kind]}</span>}
                  </span>
                  <span className="mt-2 line-clamp-2 text-[13.5px] font-semibold leading-snug text-ink">{node.title}</span>
                  {node.context && <span className="mt-0.5 block truncate text-[11px] text-muted">{node.context}</span>}
                  <span className="mt-1.5 block truncate font-mono text-[10.5px] text-faint">{node.id}{node.tier ? ` · Tier ${{ light: 1, standard: 2, demanding: 3 }[node.tier] ?? node.tier}` : ""}{node.model ? ` · ${node.model}` : ""}</span>
                  {node.depIds.length > 0 && <span className="mt-2 block truncate border-t border-line pt-2 text-[10.5px] text-muted" title={node.depIds.join(", ")}>After {node.depIds.join(", ")}</span>}
                </button>
                );
              }),
          )}
        </div>
      </div>
    </div>
  );
}
