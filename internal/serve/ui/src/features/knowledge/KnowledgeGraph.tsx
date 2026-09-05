import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { Minus, Network, Plus } from "lucide-react";
import { Mono } from "@/components/ui/primitives";
import { EmptyState, QueryBoundary } from "@/components/ui/states";
import { renderMermaid, subscribeMermaidTheme } from "@/features/editor/mermaid";
import { useDocgraph } from "@/lib/queries";
import { layoutDocGraph, type PositionedEdge } from "./layout";
import { kindMeta, KIND_ORDER } from "./bits";
import { KnowledgeShell, SectionToolbar, ViewSwitch } from "./KnowledgeShell";
import { docGraphMermaid } from "./graphMermaid";
import type { DocgraphResponse, EdgeKind } from "./types";

const route = getRouteApi("/p/$projectId/knowledge/graph");

interface Transform {
  x: number;
  y: number;
  k: number;
}

// Wide corpora need a little more room than the normal 40% floor to fit every
// node in the viewport without hiding the first or last document.
const MIN_K = 0.3;
const MAX_K = 2.5;
const clamp = (v: number, lo: number, hi: number): number => Math.max(lo, Math.min(hi, v));

interface EdgeStyle {
  color: string;
  dash?: string;
  linecap?: "round";
  marker: string;
  label: string;
}

const EDGE_STYLE: Record<EdgeKind, EdgeStyle> = {
  part_of: { color: "var(--k-faint)", marker: "url(#kg-arrow)", label: "part of" },
  updates: { color: "var(--k-faint)", dash: "6 5", marker: "url(#kg-arrow)", label: "updates" },
  source: { color: "var(--k-info)", dash: "2 4", linecap: "round", marker: "url(#kg-arrow)", label: "source" },
  decides_for: { color: "var(--k-faint)", dash: "1.5 5", linecap: "round", marker: "url(#kg-arrow)", label: "decides for" },
  superseded_by: { color: "var(--k-warn)", dash: "6 5", marker: "url(#kg-arrow-warn)", label: "superseded by" },
  link: { color: "var(--k-accent)", dash: "1.5 3.5", linecap: "round", marker: "url(#kg-arrow)", label: "link" },
};

const EDGE_ORDER: EdgeKind[] = ["part_of", "updates", "source", "decides_for", "superseded_by", "link"];

export function KnowledgeGraph() {
  const { projectId } = route.useParams();
  const q = useDocgraph(projectId);
  const counts = q.data;
  return (
    <KnowledgeShell projectId={projectId}>
      <div className="flex h-full flex-col">
        <SectionToolbar
          left={
            <div className="flex min-w-0 items-center gap-2">
              <span className="font-serif text-[14px] font-semibold text-ink">Document graph</span>
              {counts && (
                <Mono className="text-[11px] text-fainter">
                  {counts.graph.nodes.length} docs · {counts.graph.edges.length} links
                  {counts.issues.length > 0 ? ` · ${counts.issues.length} needs attention` : ""}
                </Mono>
              )}
              <span className="hidden font-mono text-[10.5px] text-fainter lg:inline">
                · drag to pan · scroll to zoom
              </span>
            </div>
          }
          right={<ViewSwitch projectId={projectId} active="graph" />}
        />
        <div className="relative min-h-0 flex-1 overflow-hidden">
          <QueryBoundary q={q}>
            {(data) => <GraphCanvas projectId={projectId} data={data} />}
          </QueryBoundary>
        </div>
      </div>
    </KnowledgeShell>
  );
}

function truncate(text: string, max: number): string {
  return text.length > max ? text.slice(0, max - 1).trimEnd() + "…" : text;
}

function GraphCanvas({ projectId, data }: { projectId: string; data: DocgraphResponse }) {
  const navigate = useNavigate();
  const layout = useMemo(
    () => layoutDocGraph(data.graph.nodes, data.graph.edges),
    [data],
  );
  const mermaidSource = useMemo(
    () => docGraphMermaid(data.graph.nodes, data.graph.edges),
    [data.graph.edges, data.graph.nodes],
  );

  const containerRef = useRef<HTMLDivElement>(null);
  const [tf, setTf] = useState<Transform>({ x: 0, y: 0, k: 1 });
  const tfRef = useRef(tf);
  tfRef.current = tf;
  const [hovered, setHovered] = useState<string | null>(null);
  const [view, setView] = useState<"map" | "mermaid">("map");
  const drag = useRef<{ px: number; py: number; base: Transform; moved: boolean } | null>(null);

  // Undirected adjacency for hover highlighting.
  const adjacency = useMemo(() => {
    const m = new Map<string, Set<string>>();
    const add = (a: string, b: string): void => {
      const s = m.get(a);
      if (s) s.add(b);
      else m.set(a, new Set([b]));
    };
    for (const e of layout.edges) {
      add(e.from, e.to);
      add(e.to, e.from);
    }
    return m;
  }, [layout]);

  const fit = useCallback(() => {
    const el = containerRef.current;
    if (!el || layout.width === 0) return;
    const { width: cw, height: ch } = el.getBoundingClientRect();
    if (cw === 0 || ch === 0) return;
    const k = clamp(Math.min((cw - 48) / layout.width, (ch - 48) / layout.height, 1.4), MIN_K, MAX_K);
    setTf({ k, x: (cw - layout.width * k) / 2, y: (ch - layout.height * k) / 2 });
  }, [layout]);

  useLayoutEffect(() => {
    fit();
  }, [fit]);

  const onPointerMove = useCallback((e: PointerEvent) => {
    const d = drag.current;
    if (!d) return;
    const dx = e.clientX - d.px;
    const dy = e.clientY - d.py;
    if (Math.abs(dx) > 3 || Math.abs(dy) > 3) d.moved = true;
    setTf({ k: d.base.k, x: d.base.x + dx, y: d.base.y + dy });
  }, []);

  const onPointerUp = useCallback(() => {
    window.removeEventListener("pointermove", onPointerMove);
    window.removeEventListener("pointerup", onPointerUp);
    // Defer clearing so the click that follows a pan can still read `moved`.
    window.setTimeout(() => {
      drag.current = null;
    }, 0);
  }, [onPointerMove]);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      drag.current = { px: e.clientX, py: e.clientY, base: tfRef.current, moved: false };
      window.addEventListener("pointermove", onPointerMove);
      window.addEventListener("pointerup", onPointerUp);
    },
    [onPointerMove, onPointerUp],
  );

  // Wheel zoom about the cursor. Native + non-passive so preventDefault sticks.
  useEffect(() => {
    const el = containerRef.current;
    if (!el || view !== "map") return;
    const handler = (e: WheelEvent): void => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const mx = e.clientX - rect.left;
      const my = e.clientY - rect.top;
      const base = tfRef.current;
      const k = clamp(base.k * Math.exp(-e.deltaY * 0.0015), MIN_K, MAX_K);
      const ratio = k / base.k;
      setTf({ k, x: mx - (mx - base.x) * ratio, y: my - (my - base.y) * ratio });
    };
    el.addEventListener("wheel", handler, { passive: false });
    return () => el.removeEventListener("wheel", handler);
  }, [view]);

  useEffect(
    () => () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
    },
    [onPointerMove, onPointerUp],
  );

  const zoomBy = (factor: number): void => {
    const el = containerRef.current;
    if (!el) return;
    const { width: cw, height: ch } = el.getBoundingClientRect();
    const base = tfRef.current;
    const k = clamp(base.k * factor, MIN_K, MAX_K);
    const ratio = k / base.k;
    setTf({ k, x: cw / 2 - (cw / 2 - base.x) * ratio, y: ch / 2 - (ch / 2 - base.y) * ratio });
  };

  const openDoc = (subject: string): void => {
    if (drag.current?.moved) return;
    void navigate({ to: "/p/$projectId/knowledge/$subject", params: { projectId, subject } });
  };

  if (layout.nodes.length === 0) {
    return (
      <div className="flex h-full items-center justify-center p-8">
        <EmptyState
          icon={<Network size={22} strokeWidth={1.5} />}
          title="No document graph yet"
          hint="Once the vault holds canonical docs, specs, or decision logs with doc-graph headers, their connections render here."
        />
      </div>
    );
  }

  const neighbors = hovered ? adjacency.get(hovered) : undefined;
  const nodeOpacity = (subject: string): number => {
    if (!hovered) return 1;
    if (subject === hovered || neighbors?.has(subject)) return 1;
    return 0.16;
  };

  return (
    <div
      ref={containerRef}
      onPointerDown={view === "map" ? onPointerDown : undefined}
      className={`relative h-full w-full select-none overflow-hidden bg-panel/40 ${view === "map" ? "touch-none" : ""}`}
    >
      <GraphViewSwitch view={view} onChange={setView} />

      {view === "map" ? (
        <>
          <svg className="absolute inset-0 h-full w-full" style={{ cursor: "grab" }}>
        <defs>
          <marker id="kg-arrow" viewBox="0 0 9 9" refX="8" refY="4.5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto">
            <path d="M0 0 L9 4.5 L0 9 z" fill="var(--k-faint)" />
          </marker>
          <marker id="kg-arrow-warn" viewBox="0 0 9 9" refX="8" refY="4.5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto">
            <path d="M0 0 L9 4.5 L0 9 z" fill="var(--k-warn)" />
          </marker>
        </defs>
        <g transform={`translate(${tf.x} ${tf.y}) scale(${tf.k})`}>
          {layout.edges.map((e) => (
            <EdgeLine key={e.id} edge={e} hovered={hovered} />
          ))}
          {layout.nodes.map((n) => {
            const meta = kindMeta[n.kind];
            const color = `var(${meta.cssVar})`;
            const superseded = n.status === "superseded";
            return (
              <g
                key={n.subject}
                className="kg-node"
                role="link"
                tabIndex={0}
                aria-label={`Open ${n.title}, ${meta.label}`}
                transform={`translate(${n.x} ${n.y})`}
                onPointerEnter={() => setHovered(n.subject)}
                onPointerLeave={() => setHovered((h) => (h === n.subject ? null : h))}
                onClick={() => openDoc(n.subject)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    openDoc(n.subject);
                  }
                }}
                style={{ opacity: nodeOpacity(n.subject), transition: "opacity 0.15s ease", cursor: "pointer" }}
              >
                {/* Colored base peeks out on the left as a kind-coded border. */}
                <rect width={n.w} height={n.h} rx={11} fill={color} opacity={0.9} />
                <rect
                  x={4}
                  width={n.w - 4}
                  height={n.h}
                  rx={10}
                  fill="var(--k-raised)"
                  stroke={hovered === n.subject ? color : "var(--k-line)"}
                  strokeWidth={hovered === n.subject ? 1.5 : 1}
                />
                <text
                  x={16}
                  y={21}
                  style={{ fontFamily: "var(--font-sans)", fontSize: 12.5, fontWeight: 600, fill: superseded ? "var(--k-muted)" : "var(--k-ink)", textDecoration: superseded ? "line-through" : "none" }}
                >
                  {truncate(n.title, 24)}
                </text>
                <text x={16} y={37} style={{ fontFamily: "var(--font-mono)", fontSize: 10, fill: "var(--k-faint)" }}>
                  {truncate(n.subject, 26)}
                </text>
                {superseded && <circle cx={n.w - 11} cy={11} r={3} fill="var(--k-warn)" />}
              </g>
            );
          })}
        </g>
          </svg>

          <Legend />

          <div className="absolute bottom-4 right-4 flex items-center gap-1 rounded-xl border border-line bg-surface/90 p-1 backdrop-blur-md">
        <button
          type="button"
          onClick={() => zoomBy(1 / 1.25)}
          aria-label="Zoom out"
          className="flex h-7 w-7 items-center justify-center rounded-lg text-muted transition-colors hover:bg-hover hover:text-ink"
        >
          <Minus size={14} />
        </button>
        <Mono className="w-10 text-center text-[11px] text-faint">{Math.round(tf.k * 100)}%</Mono>
        <button
          type="button"
          onClick={() => zoomBy(1.25)}
          aria-label="Zoom in"
          className="flex h-7 w-7 items-center justify-center rounded-lg text-muted transition-colors hover:bg-hover hover:text-ink"
        >
          <Plus size={14} />
        </button>
        <button
          type="button"
          onClick={fit}
          className="ml-0.5 rounded-lg px-2.5 py-1 text-[11.5px] font-semibold text-muted transition-colors hover:bg-hover hover:text-ink"
        >
          Fit
        </button>
          </div>
        </>
      ) : (
        <MermaidGraph source={mermaidSource} />
      )}

      <GraphNotices projectId={projectId} data={data} />
    </div>
  );
}

function GraphViewSwitch({ view, onChange }: { view: "map" | "mermaid"; onChange: (view: "map" | "mermaid") => void }) {
  const button = (next: "map" | "mermaid", label: string) => (
    <button
      type="button"
      aria-pressed={view === next}
      onClick={() => onChange(next)}
      className={view === next
        ? "rounded-md bg-raised px-2.5 py-1 text-[12px] font-semibold text-ink shadow-sm"
        : "rounded-md px-2.5 py-1 text-[12px] font-medium text-muted hover:text-ink-soft"}
    >
      {label}
    </button>
  );
  return (
    <div className="absolute right-4 top-4 z-10 inline-flex items-center gap-0.5 rounded-lg border border-line bg-surface/90 p-0.5 backdrop-blur-md">
      <span className="sr-only">Graph view</span>
      {button("map", "Map")}
      {button("mermaid", "Mermaid")}
    </div>
  );
}

function MermaidGraph({ source }: { source: string }) {
  const [themeVersion, setThemeVersion] = useState(0);
  const [rendered, setRendered] = useState<{ source: string; svg?: string; error?: string } | null>(null);

  useEffect(() => subscribeMermaidTheme(() => setThemeVersion((version) => version + 1)), []);

  useEffect(() => {
    let active = true;
    setRendered(null);
    void renderMermaid(source).then((result) => {
      if (active) setRendered({ source, ...result });
    });
    return () => {
      active = false;
    };
  }, [source, themeVersion]);

  const ready = rendered?.source === source ? rendered : null;
  return (
    <div className="absolute inset-0 overflow-y-auto px-4 pb-16 pt-20 sm:px-8">
      <div className="mx-auto flex min-h-full w-full max-w-[70rem] flex-col gap-4">
        <div className="rounded-xl border border-line bg-panel/70 p-4 text-[12.5px] leading-relaxed text-muted">
          <p className="font-medium text-ink">Readable relationship map</p>
          <p className="mt-1">This view uses the same document subjects and relationship edges as the graph JSON.</p>
        </div>
        {!ready ? (
          <div role="status" aria-live="polite" className="rounded-xl border border-line bg-panel p-5 font-mono text-[12px] text-faint">
            Rendering Mermaid map…
          </div>
        ) : ready.svg ? (
          <div
            role="img"
            aria-label="Mermaid document relationship map"
            className="kg-mermaid rounded-xl border border-line bg-panel p-5"
            // renderMermaid sanitizes the SVG before this trusted injection.
            dangerouslySetInnerHTML={{ __html: ready.svg }}
          />
        ) : (
          <div role="status" aria-live="polite" className="rounded-xl border border-warn/40 bg-warn-soft/40 p-4">
            <p className="text-[13px] font-medium text-ink">The map could not render.</p>
            <p className="mt-1 text-[12px] leading-relaxed text-muted">The Mermaid source is still available below.</p>
            <pre className="mt-3 max-h-[22rem] overflow-auto rounded-lg border border-line bg-surface p-3 font-mono text-[11px] leading-relaxed text-ink-soft">{source}</pre>
            {ready.error && <p className="mt-2 font-mono text-[11px] text-warn">{ready.error}</p>}
          </div>
        )}
        <details className="rounded-xl border border-line bg-surface/80 px-4 py-3">
          <summary className="cursor-pointer text-[12px] font-semibold text-ink-soft">Show Mermaid source</summary>
          <pre className="mt-3 overflow-x-auto whitespace-pre-wrap font-mono text-[11px] leading-relaxed text-muted">{source}</pre>
        </details>
      </div>
    </div>
  );
}

function GraphNotices({ projectId, data }: { projectId: string; data: DocgraphResponse }) {
  if (data.issues.length === 0) return null;
  const docsByPath = new Map(data.docs.map((doc) => [doc.path, doc]));
  return (
    <aside
      aria-label="Graph notices"
      role="status"
      className="absolute bottom-4 left-1/2 z-10 w-[min(34rem,calc(100%-2rem))] -translate-x-1/2 rounded-xl border border-warn/40 bg-surface/95 p-3 shadow-lg backdrop-blur-md"
    >
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-[12px] font-semibold text-ink">Needs attention</h2>
        <Mono className="text-[10.5px] text-warn">{data.issues.length}</Mono>
      </div>
      <p className="mt-1 text-[11.5px] leading-relaxed text-muted">Some document connections need a source check before they can be shown as complete.</p>
      <ul className="mt-2 max-h-32 space-y-1.5 overflow-y-auto">
        {data.issues.map((issue, index) => {
          const doc = docsByPath.get(issue.path);
          return (
            <li key={`${issue.code}:${issue.path}:${index}`} className="flex items-start gap-2 text-[11.5px] leading-snug text-ink-soft">
              <span className="mt-1 h-1.5 w-1.5 flex-none rounded-full bg-warn" aria-hidden="true" />
              <span className="min-w-0 flex-1">
                <span className="block">{issue.message}</span>
                <Mono className="text-[10px] text-faint">{issue.path}</Mono>
              </span>
              {doc && (
                <Link
                  to="/p/$projectId/knowledge/$subject"
                  params={{ projectId, subject: doc.subject }}
                  className="flex-none rounded-md px-1.5 py-0.5 font-semibold text-accent hover:bg-accent-soft"
                >
                  Open
                </Link>
              )}
            </li>
          );
        })}
      </ul>
    </aside>
  );
}

function EdgeLine({ edge, hovered }: { edge: PositionedEdge; hovered: string | null }) {
  const st = EDGE_STYLE[edge.kind];
  const connected = hovered ? edge.from === hovered || edge.to === hovered : true;
  const opacity = hovered ? (connected ? 1 : 0.07) : 0.85;
  return (
    <path
      d={edge.d}
      fill="none"
      stroke={st.color}
      strokeWidth={hovered && connected ? 2 : 1.5}
      strokeDasharray={st.dash}
      strokeLinecap={st.linecap}
      markerEnd={st.marker}
      style={{ opacity, transition: "opacity 0.15s ease" }}
    />
  );
}

function Legend() {
  return (
    <div className="absolute bottom-4 left-4 rounded-xl border border-line bg-surface/90 p-3 text-[11px] backdrop-blur-md">
      <div className="mb-1.5 font-mono text-[9px] font-medium uppercase tracking-[0.14em] text-fainter">Kinds</div>
      <div className="flex flex-col gap-1">
        {KIND_ORDER.map((kind) => (
          <div key={kind} className="flex items-center gap-2">
            <span className="h-2.5 w-2.5 flex-none rounded-full" style={{ background: `var(${kindMeta[kind].cssVar})` }} />
            <span className="text-muted">{kindMeta[kind].label}</span>
          </div>
        ))}
      </div>
      <div className="mb-1.5 mt-2.5 font-mono text-[9px] font-medium uppercase tracking-[0.14em] text-fainter">Edges</div>
      <div className="flex flex-col gap-1">
        {EDGE_ORDER.map((kind) => {
          const st = EDGE_STYLE[kind];
          return (
            <div key={kind} className="flex items-center gap-2">
              <svg width={22} height={8} className="flex-none">
                <line x1={1} y1={4} x2={21} y2={4} stroke={st.color} strokeWidth={1.75} strokeDasharray={st.dash} strokeLinecap={st.linecap} />
              </svg>
              <span className="text-muted">{st.label}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
