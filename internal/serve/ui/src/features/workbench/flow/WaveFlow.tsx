/*
  WUX-T-0005 — the wave dependency graph, a full-bleed pan/zoom canvas.

  Real prerequisite edges (TaskDetail.deps). The transform is a controlled
  viewport: selection and live updates never reset it. State is glyph +
  label; outside-wave references sit in a light "Earlier waves" lane.
  Unconfirmed outside references stay "Unresolved dependency".
*/

import { useEffect, useMemo, useRef } from "react";
import type { RunSummary, TaskDetail, WaveReviewMember } from "@/types/domain";
import {
  DISPLAY_STATE_LABEL,
  NODE_KIND_LABEL,
  NODE_WIDTH,
  TOP_DOWN_NODE_HEIGHT,
  buildFlowGraph,
  essentialFlowEdges,
  fitViewport,
  initialViewport,
  layoutTopDownFlowGraph,
  panViewport,
  zoomViewport,
  type DependencyFact,
  type FlowDisplayState,
  type FlowNode,
  type FlowViewport,
} from "./flowGraph";

export interface WaveFlowProps {
  memberIds: string[];
  tasks: TaskDetail[];
  runs: RunSummary[];
  dependencyFacts?: Record<string, DependencyFact>;
  reviewMembers?: WaveReviewMember[];
  /** Tasks with an open wave-level human action. */
  needsYouIds?: string[];
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

type Point = { x: number; y: number };

const PILL_WIDTH = 196;
const PILL_HEIGHT = 36;
const PILL_GAP = 12;
const LANE_HEIGHT = 20 + PILL_HEIGHT + 48;

function edgePath(x1: number, y1: number, x2: number, y2: number): string {
  const bend = Math.max(24, (y2 - y1) / 2);
  return `M ${x1} ${y1} C ${x1} ${y1 + bend}, ${x2} ${y2 - bend}, ${x2} ${y2 - 3}`;
}

const stateLabel = (node: FlowNode) => node.stateLabel ?? DISPLAY_STATE_LABEL[node.state];

export function WaveFlow(props: WaveFlowProps) {
  const { memberIds, tasks, runs, dependencyFacts, reviewMembers, selectedTaskId, onSelectTask, onViewportChange } = props;
  const viewport = props.viewport ?? initialViewport();
  const canvasRef = useRef<HTMLDivElement>(null);
  const drag = useRef<{ start: Point; origin: FlowViewport } | null>(null);
  // Native wheel listener: React's is passive, and ctrl+wheel must not zoom the page.
  const latest = useRef({ viewport, onViewportChange });
  latest.current = { viewport, onViewportChange };

  const graph = useMemo(
    () => buildFlowGraph({ memberIds, tasks, runs, dependencyFacts, reviewMembers }),
    [memberIds, tasks, runs, dependencyFacts, reviewMembers],
  );
  const members = useMemo(() => graph.nodes.filter((node) => node.kind === "task"), [graph.nodes]);
  const earlier = useMemo(() => graph.nodes.filter((node) => node.kind !== "task"), [graph.nodes]);
  const layout = useMemo(() => layoutTopDownFlowGraph({ ...graph, nodes: members }, memberIds), [graph, members, memberIds]);
  const edges = useMemo(() => essentialFlowEdges(graph), [graph]);
  const laneTop = earlier.length > 0 ? LANE_HEIGHT : 0;
  const width = Math.max(layout.width, earlier.length * (PILL_WIDTH + PILL_GAP) - PILL_GAP);
  const height = laneTop + layout.height;
  const memberAt = (id: string): Point | undefined => {
    const position = layout.positions[id];
    return position && { x: position.x, y: position.y + laneTop };
  };
  const pillAt = (index: number): Point => ({ x: index * (PILL_WIDTH + PILL_GAP), y: 20 });

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const { viewport: current, onViewportChange: change } = latest.current;
      if (event.ctrlKey || event.metaKey) {
        const box = canvas.getBoundingClientRect();
        change(zoomViewport(current, Math.exp(-event.deltaY / 300), { x: event.clientX - box.left, y: event.clientY - box.top }));
      } else change(panViewport(current, -event.deltaX, -event.deltaY));
    };
    canvas.addEventListener("wheel", onWheel, { passive: false });
    return () => canvas.removeEventListener("wheel", onWheel);
  }, [props.loading, props.error, memberIds.length]);

  if (props.loading) return <p role="status" aria-label="Loading wave graph" className="p-6 text-[13px] text-muted">Loading task dependencies…</p>;
  if (props.error) return <p role="alert" className="p-6 text-[13px] text-fail">Dependency graph unavailable. {props.error}</p>;
  if (memberIds.length === 0) return <p className="p-6 text-[13px] text-muted">No tasks in this wave.</p>;

  const fit = () => {
    const canvas = canvasRef.current;
    if (canvas) onViewportChange(fitViewport({ width, height }, { width: canvas.clientWidth, height: canvas.clientHeight }, 32));
  };
  const zoom = (factor: number) => {
    const canvas = canvasRef.current;
    onViewportChange(zoomViewport(viewport, factor, canvas ? { x: canvas.clientWidth / 2, y: canvas.clientHeight / 2 } : undefined));
  };
  const zoomButton = "h-7 min-w-7 px-2 text-[12px] text-muted hover:bg-hover hover:text-ink";

  return (
    <div className="relative flex min-h-0 flex-1 flex-col bg-surface" aria-label="Wave dependency graph">
      {graph.warnings.length > 0 && (
        <details className="absolute left-3 top-3 z-20 max-w-md rounded-md border border-line bg-raised px-3 py-1.5 shadow-sm">
          <summary className="cursor-pointer text-[12px] font-medium text-warn">
            {graph.warnings.length} dependency {graph.warnings.length === 1 ? "issue" : "issues"}
          </summary>
          <ul className="mt-2 space-y-1.5 pl-4" aria-label="Graph warnings">
            {graph.warnings.map((warning, index) => <li key={`${warning.kind}-${index}`} className="list-disc text-[12px] leading-5 text-muted">{warning.text}</li>)}
          </ul>
        </details>
      )}
      <div
        ref={canvasRef}
        className="relative min-h-0 flex-1 cursor-grab touch-none overflow-hidden active:cursor-grabbing"
        role="region"
        aria-label={`Dependency graph with ${members.length} tasks. Drag or scroll to pan; ctrl+scroll to zoom. Arrows point from prerequisites to dependent tasks.`}
        onPointerDown={(event) => {
          if ((event.target as HTMLElement).closest("button, details")) return;
          drag.current = { start: { x: event.clientX, y: event.clientY }, origin: viewport };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          const from = drag.current;
          if (from) onViewportChange(panViewport(from.origin, event.clientX - from.start.x, event.clientY - from.start.y));
        }}
        onPointerUp={() => { drag.current = null; }}
        onPointerCancel={() => { drag.current = null; }}
      >
        <div className="absolute left-0 top-0 origin-top-left" style={{ width, height, transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.scale})` }}>
          <svg width={width} height={height} className="absolute inset-0 overflow-visible" aria-hidden="true">
            <defs>
              <marker id="wave-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M 0 1 L 9 5 L 0 9" fill="none" stroke="var(--color-muted)" strokeWidth="1.4" />
              </marker>
              <marker id="wave-arrow-cycle" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto">
                <path d="M 0 1 L 9 5 L 0 9" fill="none" stroke="var(--color-warn)" strokeWidth="1.4" />
              </marker>
            </defs>
            {edges.map((edge) => {
              const to = memberAt(edge.to);
              const fromMember = memberAt(edge.from);
              const pill = earlier.findIndex((node) => node.id === edge.from);
              if (!to || (!fromMember && pill < 0)) return null;
              const from = fromMember
                ? { x: fromMember.x + NODE_WIDTH / 2, y: fromMember.y + TOP_DOWN_NODE_HEIGHT }
                : { x: pillAt(pill).x + PILL_WIDTH / 2, y: pillAt(pill).y + PILL_HEIGHT };
              const d = edgePath(from.x, from.y, to.x + NODE_WIDTH / 2, to.y);
              if (!fromMember) return <path key={`${edge.from}→${edge.to}`} d={d} fill="none" stroke="var(--color-faint)" strokeWidth={1} strokeDasharray="3 4" opacity="0.6" />;
              return <path key={`${edge.from}→${edge.to}`} d={d} fill="none" stroke={edge.cyclic ? "var(--color-warn)" : "var(--color-muted)"} strokeWidth={edge.cyclic ? 2 : 1.5} strokeDasharray={edge.cyclic ? "6 4" : undefined} markerEnd={edge.cyclic ? "url(#wave-arrow-cycle)" : "url(#wave-arrow)"} opacity="0.75" />;
            })}
          </svg>
          {earlier.length > 0 && <p className="absolute left-0 top-0 text-[11px] font-medium text-faint">Earlier waves</p>}
          {earlier.map((node, index) => {
            const at = pillAt(index);
            const label = stateLabel(node);
            return (
              <button
                key={node.id}
                type="button"
                onClick={() => onSelectTask(node.id)}
                aria-pressed={selectedTaskId === node.id}
                aria-label={`${node.title} (${node.id}), ${NODE_KIND_LABEL[node.kind]}, ${label}`}
                title={[node.id, node.context, label].filter(Boolean).join(" · ")}
                style={{ left: at.x, top: at.y, width: PILL_WIDTH, height: PILL_HEIGHT }}
                className={`absolute z-10 flex items-center gap-1.5 rounded-full border border-dashed bg-surface px-3 text-left text-[12px] text-muted hover:border-accent hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${selectedTaskId === node.id ? "border-accent" : "border-line"}`}
              >
                <StateGlyph state={node.state} />
                <span className="truncate">{node.title}</span>
              </button>
            );
          })}
          {members.map((node) => {
            const at = memberAt(node.id);
            if (!at) return null;
            const selected = selectedTaskId === node.id;
            const needsYou = node.needsYou || props.needsYouIds?.includes(node.id);
            const label = stateLabel(node);
            return (
              <button
                key={node.id}
                type="button"
                onClick={() => onSelectTask(node.id)}
                aria-pressed={selected}
                aria-label={`${node.title} (${node.id}), ${NODE_KIND_LABEL[node.kind]}, ${label}${needsYou ? ", needs you" : ""}${node.model ? `, model ${node.model}` : ""}${selected ? ", selected" : ""}`}
                style={{ left: at.x, top: at.y, width: NODE_WIDTH, height: TOP_DOWN_NODE_HEIGHT }}
                className={`absolute z-10 flex min-w-0 flex-col rounded-lg border bg-raised px-3 py-2.5 text-left shadow-sm transition-colors hover:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${selected ? "border-accent ring-2 ring-accent/40" : needsYou ? "border-warn ring-1 ring-warn/40" : "border-line"}`}
              >
                <span className="flex h-4 flex-none items-center justify-between gap-2 font-mono text-[10.5px] leading-4 text-faint">
                  <span className="truncate">{node.id}</span>
                  {needsYou && <span className="flex-none rounded-full bg-warn-soft px-1.5 font-sans text-[10.5px] font-medium text-warn">Needs you</span>}
                </span>
                <span title={node.title} className="mt-1 line-clamp-2 flex-none break-words text-[13.5px] font-semibold leading-[18px] text-ink">{node.title}</span>
                <span className="mt-auto flex h-[18px] flex-none items-center gap-1.5 text-[11.5px] leading-[18px] text-muted">
                  <StateGlyph state={node.state} />
                  <span className="font-medium">{label}</span>
                  {node.model && <span className="truncate text-faint">· {node.model}</span>}
                </span>
              </button>
            );
          })}
        </div>
      </div>
      <div className="absolute bottom-3 right-3 z-20 flex overflow-hidden rounded-md border border-line bg-raised shadow-sm" aria-label="Graph zoom">
        <button type="button" className={zoomButton} onClick={() => zoom(1 / 1.2)} aria-label="Zoom out">−</button>
        <button type="button" className={`${zoomButton} border-x border-line`} onClick={fit}>Fit</button>
        <button type="button" className={zoomButton} onClick={() => zoom(1.2)} aria-label="Zoom in">+</button>
      </div>
    </div>
  );
}
