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

import { useEffect, useMemo, useRef, useState } from "react";
import type { RunSummary, TaskDetail } from "@/types/domain";
import {
  DISPLAY_STATE_LABEL,
  GAP_X,
  NODE_HEIGHT,
  NODE_KIND_LABEL,
  NODE_WIDTH,
  buildFlowGraph,
  clampViewport,
  fitViewport,
  initialViewport,
  layoutFlowGraph,
  panViewport,
  topologyKey,
  zoomViewport,
  type DependencyFact,
  type FlowDisplayState,
  type FlowViewport,
} from "./flowGraph";

export interface WaveFlowProps {
  memberIds: string[];
  tasks: TaskDetail[];
  runs: RunSummary[];
  dependencyFacts?: Record<string, DependencyFact>;
  selectedTaskId?: string;
  viewport?: FlowViewport;
  onSelectTask: (id: string) => void;
  onViewportChange: (value: FlowViewport) => void;
  loading?: boolean;
  error?: string;
}

type ViewMode = "flow" | "list";

const ZOOM_STEP = 1.25;

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
      return (
        <svg width="14" height="14" viewBox="0 0 14 14" className={common} aria-hidden="true">
          <rect x="2" y="2" width="10" height="10" rx="2.5" fill="var(--color-warn-soft)" stroke="var(--color-warn)" strokeWidth="1.5" />
          <path d="M4.5 4.5l5 5M9.5 4.5l-5 5" stroke="var(--color-warn)" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
      );
    case "failed":
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
  const x1 = from.x + NODE_WIDTH;
  const y1 = from.y + NODE_HEIGHT / 2;
  const x2 = to.x;
  const y2 = to.y + NODE_HEIGHT / 2;
  const dx = Math.max(24, (x2 - x1) / 2);
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2 - 2} ${y2}`;
}

export function WaveFlow(props: WaveFlowProps) {
  const { memberIds, tasks, runs, dependencyFacts, selectedTaskId, onSelectTask, onViewportChange } = props;

  const graph = useMemo(
    () => buildFlowGraph({ memberIds, tasks, runs, dependencyFacts }),
    [memberIds, tasks, runs, dependencyFacts],
  );
  const layout = useMemo(() => layoutFlowGraph(graph, memberIds), [graph, memberIds]);
  const topology = useMemo(() => topologyKey(graph), [graph]);

  const [internal, setInternal] = useState<FlowViewport>(() => initialViewport());
  const controlled = props.viewport !== undefined;
  const view = controlled ? (props.viewport as FlowViewport) : internal;
  const [dragView, setDragView] = useState<FlowViewport | null>(null);
  const effective = dragView ?? view;

  const [mode, setMode] = useState<ViewMode>("flow");
  const containerRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ startX: number; startY: number; base: FlowViewport; moved: boolean } | null>(null);
  const wheelTimer = useRef<number | null>(null);
  const topologyRef = useRef(topology);

  const commit = (next: FlowViewport): void => {
    const clamped = clampViewport(next);
    if (!controlled) setInternal(clamped);
    onViewportChange(clamped);
  };

  const containerSize = (): { width: number; height: number } => {
    const el = containerRef.current;
    if (!el) return { width: 800, height: 480 };
    const rect = el.getBoundingClientRect();
    return {
      width: Math.max(1, rect.width),
      height: Math.max(1, rect.height),
    };
  };

  // Keep the selected node in view when selection changes; pan only, scale kept.
  useEffect(() => {
    if (!selectedTaskId || mode !== "flow") return;
    const position = layout.positions[selectedTaskId];
    const el = containerRef.current;
    if (!position || !el) return;
    const rect = el.getBoundingClientRect();
    const cx = (position.x + NODE_WIDTH / 2) * view.scale + view.x;
    const cy = (position.y + NODE_HEIGHT / 2) * view.scale + view.y;
    const margin = 40;
    let dx = 0;
    let dy = 0;
    if (cx < margin) dx = margin - cx;
    else if (cx > rect.width - margin) dx = rect.width - margin - cx;
    if (cy < margin) dy = margin - cy;
    else if (cy > rect.height - margin) dy = rect.height - margin - cy;
    if (dx !== 0 || dy !== 0) commit(panViewport(view, dx, dy));
    // Intentionally viewport-pinned to the selection event only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedTaskId, mode]);

  useEffect(() => {
    topologyRef.current = topology;
  }, [topology]);

  useEffect(() => {
    return () => {
      if (wheelTimer.current !== null) window.clearTimeout(wheelTimer.current);
    };
  }, []);

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

  const fit = (): void => {
    const size = containerSize();
    commit(fitViewport({ width: layout.width, height: layout.height }, size));
  };

  const reset = (): void => commit(initialViewport());

  const onCanvasPointerDown = (event: React.PointerEvent<HTMLDivElement>): void => {
    if (event.button !== 0) return;
    if ((event.target as HTMLElement).closest("button")) return;
    dragRef.current = { startX: event.clientX, startY: event.clientY, base: view, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const onCanvasPointerMove = (event: React.PointerEvent<HTMLDivElement>): void => {
    const drag = dragRef.current;
    if (!drag) return;
    const dx = event.clientX - drag.startX;
    const dy = event.clientY - drag.startY;
    if (!drag.moved && Math.hypot(dx, dy) < 3) return;
    drag.moved = true;
    setDragView(clampViewport({ ...drag.base, x: drag.base.x + dx, y: drag.base.y + dy }));
  };

  const onCanvasPointerUp = (): void => {
    const drag = dragRef.current;
    dragRef.current = null;
    if (drag?.moved && dragView) {
      const settled = dragView;
      setDragView(null);
      commit(settled);
    } else {
      setDragView(null);
    }
  };

  const onCanvasWheel = (event: React.WheelEvent<HTMLDivElement>): void => {
    const el = containerRef.current;
    if (!el) return;
    event.preventDefault();
    const rect = el.getBoundingClientRect();
    const center = { x: event.clientX - rect.left, y: event.clientY - rect.top };
    const factor = event.deltaY < 0 ? 1.12 : 1 / 1.12;
    const base = dragView ?? view;
    const next = zoomViewport(base, factor, center);
    setDragView(next);
    if (wheelTimer.current !== null) window.clearTimeout(wheelTimer.current);
    wheelTimer.current = window.setTimeout(() => {
      wheelTimer.current = null;
      setDragView((pending) => {
        if (pending) commit(pending);
        return null;
      });
    }, 140);
  };

  const onCanvasKeyDown = (event: React.KeyboardEvent<HTMLDivElement>): void => {
    const step = 48;
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      commit(panViewport(view, step, 0));
    } else if (event.key === "ArrowRight") {
      event.preventDefault();
      commit(panViewport(view, -step, 0));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      commit(panViewport(view, 0, step));
    } else if (event.key === "ArrowDown") {
      event.preventDefault();
      commit(panViewport(view, 0, -step));
    } else if (event.key === "+" || event.key === "=") {
      event.preventDefault();
      const size = containerSize();
      commit(zoomViewport(view, ZOOM_STEP, { x: size.width / 2, y: size.height / 2 }));
    } else if (event.key === "-") {
      event.preventDefault();
      const size = containerSize();
      commit(zoomViewport(view, 1 / ZOOM_STEP, { x: size.width / 2, y: size.height / 2 }));
    } else if (event.key === "0") {
      event.preventDefault();
      reset();
    }
  };

  const zoomAtCenter = (factor: number): void => {
    const size = containerSize();
    commit(zoomViewport(view, factor, { x: size.width / 2, y: size.height / 2 }));
  };

  const toolbarButton =
    "inline-flex min-h-8 items-center gap-1.5 rounded-md border border-line bg-surface px-2.5 text-[12.5px] font-medium text-ink-soft hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-50";

  return (
    <div className="overflow-hidden rounded-lg border border-line bg-panel">
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-3 py-2">
        <div className="flex items-center gap-1.5" role="group" aria-label="Graph zoom">
          <button type="button" className={toolbarButton} onClick={() => zoomAtCenter(ZOOM_STEP)} aria-label="Zoom in">
            +
          </button>
          <button
            type="button"
            className={toolbarButton}
            onClick={() => zoomAtCenter(1 / ZOOM_STEP)}
            aria-label="Zoom out"
            disabled={view.scale <= 0.26}
          >
            −
          </button>
          <button type="button" className={toolbarButton} onClick={fit}>
            Fit
          </button>
          <button type="button" className={toolbarButton} onClick={reset}>
            Reset
          </button>
        </div>
        <span className="font-mono text-[11px] text-faint" aria-live="off">
          {Math.round(view.scale * 100)}%
        </span>
        <div className="ml-auto flex items-center gap-1.5" role="group" aria-label="Graph representation">
          <button
            type="button"
            className={toolbarButton}
            onClick={() => setMode("flow")}
            aria-pressed={mode === "flow"}
          >
            Flow
          </button>
          <button
            type="button"
            className={toolbarButton}
            onClick={() => setMode("list")}
            aria-pressed={mode === "list"}
          >
            List
          </button>
        </div>
      </div>

      {graph.warnings.length > 0 && (
        <ul className="space-y-1.5 border-b border-line bg-surface px-3 py-2.5" aria-label="Graph warnings">
          {graph.warnings.map((warning, index) => (
            <li key={`${warning.kind}-${index}`} className="flex flex-wrap items-baseline gap-x-2 text-[12.5px] leading-5">
              <span className="font-semibold text-warn">
                {warning.kind === "cycle"
                  ? "Cycle"
                  : warning.kind === "unresolved"
                    ? "Unresolved"
                    : warning.kind === "missing"
                      ? "Missing"
                      : warning.kind === "unavailable"
                        ? "Unavailable"
                        : "External"}
                :
              </span>
              <span className="min-w-0 flex-1 text-muted">{warning.text}</span>
            </li>
          ))}
        </ul>
      )}

      {mode === "list" ? (
        <ul className="max-h-[480px] divide-y divide-line overflow-y-auto" aria-label="Wave tasks as a list">
          {graph.nodes.map((node) => (
            <li key={node.id}>
              <button
                type="button"
                onClick={() => onSelectTask(node.id)}
                aria-pressed={selectedTaskId === node.id}
                aria-label={`${node.title} (${node.id}), ${NODE_KIND_LABEL[node.kind]}, ${DISPLAY_STATE_LABEL[node.state]}${node.model ? `, model ${node.model}` : ""}`}
                className={`flex w-full flex-col gap-1 px-4 py-2.5 text-left hover:bg-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent ${
                  selectedTaskId === node.id ? "bg-accent-soft" : ""
                }`}
              >
                <span className="flex items-center gap-2">
                  <StateGlyph state={node.state} />
                  <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium text-ink" title={node.title}>
                    {node.title}
                  </span>
                  {node.kind !== "task" && (
                    <span className="flex-none font-mono text-[10.5px] uppercase tracking-wide text-faint">
                      {NODE_KIND_LABEL[node.kind]}
                    </span>
                  )}
                </span>
                <span className="pl-6 text-[12px] text-muted">
                  {DISPLAY_STATE_LABEL[node.state]}
                  {node.model ? ` · ${node.model}` : ""}
                  {node.depIds.length > 0
                    ? ` · depends on ${node.depIds.join(", ")}`
                    : " · no dependencies"}
                  {node.missingDetail ? " · details unavailable" : ""}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <div
          ref={containerRef}
          tabIndex={0}
          role="region"
          aria-label={`Wave dependency graph: ${graph.nodes.length} tasks, ${graph.edges.length} dependencies. Arrow keys pan, plus and minus zoom, zero resets. Tab reaches every task node.`}
          onPointerDown={onCanvasPointerDown}
          onPointerMove={onCanvasPointerMove}
          onPointerUp={onCanvasPointerUp}
          onPointerCancel={onCanvasPointerUp}
          onWheel={onCanvasWheel}
          onKeyDown={onCanvasKeyDown}
          className="relative h-[440px] cursor-grab touch-none select-none overflow-hidden bg-surface focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent active:cursor-grabbing sm:h-[480px]"
        >
          <div
            className="absolute left-0 top-0"
            style={{
              width: layout.width + GAP_X * 2,
              height: layout.height + 64,
              transform: `translate(${effective.x + GAP_X}px, ${effective.y + 32}px) scale(${effective.scale})`,
              transformOrigin: "0 0",
            }}
          >
            <svg
              width={layout.width}
              height={layout.height}
              className="absolute left-0 top-0 overflow-visible"
              aria-hidden="true"
            >
              <defs>
                <marker id="wux-flow-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                  <path d="M 0 1 L 9 5 L 0 9" fill="none" stroke="var(--color-muted)" strokeWidth="1.4" />
                </marker>
                <marker id="wux-flow-arrow-cycle" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                  <path d="M 0 1 L 9 5 L 0 9" fill="none" stroke="var(--color-warn)" strokeWidth="1.4" />
                </marker>
              </defs>
              {graph.edges.map((edge) => {
                const from = layout.positions[edge.from];
                const to = layout.positions[edge.to];
                if (!from || !to) return null;
                return (
                  <path
                    key={`${edge.from}→${edge.to}`}
                    d={edgePath(from, to)}
                    fill="none"
                    stroke={edge.cyclic ? "var(--color-warn)" : "var(--color-muted)"}
                    strokeWidth={edge.cyclic ? 2 : 1.5}
                    strokeDasharray={edge.cyclic ? "6 4" : undefined}
                    markerEnd={edge.cyclic ? "url(#wux-flow-arrow-cycle)" : "url(#wux-flow-arrow)"}
                    opacity={0.9}
                  />
                );
              })}
            </svg>
            {graph.nodes.map((node) => {
              const position = layout.positions[node.id];
              if (!position) return null;
              const selected = selectedTaskId === node.id;
              return (
                <button
                  key={node.id}
                  type="button"
                  onClick={() => onSelectTask(node.id)}
                  aria-pressed={selected}
                  aria-label={`${node.title} (${node.id}), ${NODE_KIND_LABEL[node.kind]}, ${DISPLAY_STATE_LABEL[node.state]}${node.model ? `, model ${node.model}` : ""}${selected ? ", selected" : ""}`}
                  title={node.title}
                  style={{ left: position.x, top: position.y, width: NODE_WIDTH, minHeight: NODE_HEIGHT }}
                  className={`absolute flex flex-col justify-center gap-1 rounded-lg border bg-panel px-3 py-2 text-left shadow-sm hover:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${
                    selected ? "border-accent ring-1 ring-accent" : "border-line"
                  } ${node.kind !== "task" ? "border-dashed" : ""}`}
                >
                  {node.kind !== "task" && (
                    <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-faint">
                      {NODE_KIND_LABEL[node.kind]}
                    </span>
                  )}
                  <span className="line-clamp-2 text-[14px] font-medium leading-snug text-ink">{node.title}</span>
                  <span className="flex items-center gap-1.5 text-[12px] text-muted">
                    <StateGlyph state={node.state} />
                    <span>{DISPLAY_STATE_LABEL[node.state]}</span>
                    {node.model && (
                      <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-faint">· {node.model}</span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>
          <p className="pointer-events-none absolute bottom-2 left-3 font-mono text-[10.5px] text-faint">
            {graph.nodes.length} tasks · {graph.edges.length} edges · drag to pan · scroll to zoom
          </p>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-line px-3 py-2">
        <span className="text-[12px] text-muted">
          {graph.nodes.length} tasks · {graph.edges.length} dependencies
          {graph.edges.length === 0 ? " · no dependency edges recorded" : ""}
        </span>
        {selectedTaskId && (
          <span className="text-[12px] text-muted">
            Selected: <span className="font-medium text-ink">{selectedTaskId}</span>
          </span>
        )}
      </div>
    </div>
  );
}
