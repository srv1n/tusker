/*
  WUX-T-0005 — readable interactive wave graph, pure graph model.

  Framework-free helpers for the WaveFlow leaf: dependency classification,
  truthful display states, cycle detection, layered layout, and viewport
  math. Kept free of JSX so the focused behavior checks can exercise the
  real logic. The React component in this directory renders this model.

  Interpretation rules (spec section 6, flow packet):
  - Dependency references come from TaskDetail.deps. memberIds distinguishes
    an unloaded wave member from an outside-wave reference.
  - Without caller-confirmed dependencyFacts, an outside-wave reference is
    "unresolved" — never claimed as missing or as a valid external task.
  - Model labels use supplied run facts only; roles are never mapped to
    models by guessing.
*/

import type { RunSummary, TaskDetail, TaskStatus, WaveReviewMember } from "@/types/domain";

/** Controlled pan/zoom transform. x/y are content-space offsets in CSS px. */
export interface FlowViewport {
  x: number;
  y: number;
  scale: number;
}

/** Caller-confirmed existence information for an outside-wave dependency. */
export interface DependencyFact {
  kind: "external" | "missing" | "unavailable";
  title?: string;
}

/** Truthful per-node progress. "failed" needs a terminal failed run; a quiet
 *  ready task with no run facts is ready, never "queued-by-inference". */
export type FlowDisplayState =
  | "completed"
  | "executing"
  | "awaiting_review"
  | "reviewing"
  | "proof_blocked"
  | "cancelled"
  | "ready"
  | "backlog"
  | "queued"
  | "blocked"
  | "failed"
  | "unknown";

export type FlowNodeKind = "task" | "external" | "unresolved" | "missing" | "unavailable";

export interface FlowNode {
  id: string;
  kind: FlowNodeKind;
  title: string;
  state: FlowDisplayState;
  /** Compact model name, present only for live runs with verified identity. */
  model?: string;
  /** Effective or authored work tier; routing identity remains in the inspector. */
  tier?: string;
  /** Direct prerequisite ids (edges point dep -> this node). */
  depIds: string[];
  /** True when the id is a wave member whose detail was not supplied. */
  missingDetail?: boolean;
}

export interface FlowEdge {
  from: string;
  to: string;
  /** True when the edge participates in a dependency cycle. */
  cyclic?: boolean;
}

export interface FlowWarning {
  kind: "cycle" | "unresolved" | "missing" | "unavailable" | "external";
  text: string;
  taskIds: string[];
}

export interface FlowGraph {
  nodes: FlowNode[];
  edges: FlowEdge[];
  warnings: FlowWarning[];
  /** Each cycle as an ordered id list (first id repeated at the end). */
  cycles: string[][];
}

/** Remove arrows already implied by another path without changing reachability. */
export function essentialFlowEdges(graph: Pick<FlowGraph, "nodes" | "edges">): FlowEdge[] {
  const outgoing = new Map<string, FlowEdge[]>();
  for (const node of graph.nodes) outgoing.set(node.id, []);
  for (const edge of graph.edges) outgoing.get(edge.from)?.push(edge);

  return graph.edges.filter((candidate) => {
    if (candidate.cyclic) return true;
    const seen = new Set([candidate.from]);
    const pending = (outgoing.get(candidate.from) ?? [])
      .filter((edge) => edge !== candidate)
      .map((edge) => edge.to);
    while (pending.length > 0) {
      const id = pending.pop()!;
      if (id === candidate.to) return false;
      if (seen.has(id)) continue;
      seen.add(id);
      for (const edge of outgoing.get(id) ?? []) pending.push(edge.to);
    }
    return true;
  });
}

export const DEFAULT_VIEWPORT: FlowViewport = { x: 0, y: 0, scale: 1 };
export const MIN_SCALE = 0.25;
export const MAX_SCALE = 2.5;

/** Readable node geometry: 220-280px text widths, >=14px titles. */
export const NODE_WIDTH = 248;
export const NODE_HEIGHT = 84;
export const GAP_X = 80;
export const GAP_Y = 28;

const LIVE_LEASES = new Set(["claimed", "starting", "running"]);

/** A run earns the "executing" label only with a held lease and fresh heartbeat. */
export function isLiveRun(run: RunSummary | undefined): boolean {
  return (
    !!run &&
    !run.terminal &&
    run.liveness === "fresh" &&
    LIVE_LEASES.has(run.leaseStateRaw ?? "")
  );
}

function isFailedRun(run: RunSummary | undefined): boolean {
  if (!run) return false;
  if (!run.terminal) return false;
  return run.outcome === "failed" || run.outcome === "interrupted";
}

/** Map durable status + run facts to one honest display state. A live run
 *  proves current activity, so it wins over the durable status; the lane
 *  keeps the worker stage distinct from the reviewer stage. */
export function displayStateFor(
  status: TaskStatus,
  run: RunSummary | undefined,
): FlowDisplayState {
  if (status === "done") return "completed";
  if (run && isLiveRun(run)) return run.lane === "review" ? "reviewing" : "executing";
  if (status === "review") return "reviewing";
  if (status === "blocked") return "blocked";
  if (isFailedRun(run)) return "failed";
  if (status === "ready") {
    if (!run) return "ready";
    return "unknown";
  }
  if (status === "backlog") {
    return "backlog";
  }
  return "unknown";
}

/** The wave review is the status authority when it is available. */
export function reviewDisplayStateFor(member: WaveReviewMember | undefined): FlowDisplayState | undefined {
  if (!member) return undefined;
  if (member.phase === "completed" || member.state === "completed") return "completed";
  if (member.phase === "failed") return "failed";
  if (member.phase === "executing" || member.state === "running") return "executing";
  if (member.phase === "reviewing") return "reviewing";
  if (member.phase === "awaiting_review") return "awaiting_review";
  if (member.phase === "proof_blocked") return "proof_blocked";
  if (member.state === "cancelled") return "cancelled";
  if (member.state === "blocked") return "blocked";
  if (member.state === "ready") return "ready";
  if (member.state === "waiting") return "queued";
  return undefined;
}

/** Model identity only from a live run; never inferred from roles or lanes. */
export function modelFor(run: RunSummary | undefined): string | undefined {
  if (!isLiveRun(run)) return undefined;
  const model = run?.model?.trim() ?? "";
  return model ? model : undefined;
}

export interface BuildFlowInput {
  memberIds: string[];
  tasks: TaskDetail[];
  runs?: RunSummary[];
  dependencyFacts?: Record<string, DependencyFact>;
  reviewMembers?: WaveReviewMember[];
}

/**
 * Build the render model. Internal edges come from TaskDetail.deps between
 * wave members; outside-wave references become external/missing/unavailable
 * nodes only with caller-confirmed facts, otherwise "unresolved".
 */
export function buildFlowGraph(input: BuildFlowInput): FlowGraph {
  const memberIds = [...new Set(input.memberIds)];
  const members = new Set(memberIds);
  const tasksById = new Map(input.tasks.map((task) => [task.id, task]));
  const runsByTask = new Map((input.runs ?? []).map((run) => [run.taskId, run]));
  const reviewsByTask = new Map((input.reviewMembers ?? []).map((member) => [member.taskId, member]));
  const facts = input.dependencyFacts ?? {};

  const nodes: FlowNode[] = [];
  const nodeIds = new Set<string>();
  const edges: FlowEdge[] = [];
  const edgeKeys = new Set<string>();
  const warnings: FlowWarning[] = [];

  const ensureNode = (node: FlowNode): void => {
    if (nodeIds.has(node.id)) return;
    nodeIds.add(node.id);
    nodes.push(node);
  };

  const addEdge = (from: string, to: string): void => {
    const key = `${from}→${to}`;
    if (edgeKeys.has(key) || from === to) return;
    edgeKeys.add(key);
    edges.push({ from, to });
  };

  // Member nodes first, in stable source order.
  for (const id of memberIds) {
    const task = tasksById.get(id);
    const run = runsByTask.get(id);
    const reviewState = reviewDisplayStateFor(reviewsByTask.get(id));
    if (!task) {
      nodes.push({
        id,
        kind: "task",
        title: reviewsByTask.get(id)?.title || id,
        // The review is still authoritative while task details are loading.
        // Do not replace a known phase with an old run or an unknown node.
        state: reviewState ?? "unknown",
        depIds: [],
        missingDetail: true,
      });
      nodeIds.add(id);
      continue;
    }
    nodes.push({
      id,
      kind: "task",
      title: task.title || id,
      state: reviewState ?? displayStateFor(task.status, run),
      model: modelFor(run),
      tier: task.effectiveExecute?.work_level ?? task.authoredWorkLevel,
      depIds: task.deps.map((dep) => dep.id),
    });
    nodeIds.add(id);
  }

  const unresolvedRefs: Array<{ dep: string; by: string }> = [];

  // Dependency edges + reference nodes.
  for (const node of nodes.filter((candidate) => candidate.kind === "task" && !candidate.missingDetail)) {
    for (const depId of node.depIds) {
      if (members.has(depId)) {
        addEdge(depId, node.id);
        continue;
      }
      const fact = facts[depId];
      if (!fact) {
        ensureNode({
          id: depId,
          kind: "unresolved",
          title: depId,
          state: "unknown",
          depIds: [],
        });
        addEdge(depId, node.id);
        unresolvedRefs.push({ dep: depId, by: node.id });
        continue;
      }
      if (fact.kind === "external") {
        ensureNode({
          id: depId,
          kind: "external",
          title: fact.title?.trim() ? fact.title : depId,
          state: "unknown",
          depIds: [],
        });
        addEdge(depId, node.id);
        continue;
      }
      ensureNode({
        id: depId,
        kind: fact.kind,
        title: fact.title?.trim() ? fact.title : depId,
        state: "unknown",
        depIds: [],
      });
      addEdge(depId, node.id);
    }
  }

  const cycles = findCycles(memberIds, edges);
  if (cycles.length > 0) {
    const cyclicKeys = new Set<string>();
    for (const cycle of cycles) {
      for (let i = 0; i + 1 < cycle.length; i += 1) {
        cyclicKeys.add(`${cycle[i]}→${cycle[i + 1]}`);
      }
    }
    for (const edge of edges) {
      if (cyclicKeys.has(`${edge.from}→${edge.to}`)) edge.cyclic = true;
    }
    warnings.push({
      kind: "cycle",
      text: `Dependency cycle${cycles.length === 1 ? "" : "s"} detected: ${cycles
        .map((cycle) => cycle.join(" → "))
        .join("; ")}. Edges are shown dashed; no order is inferred inside the cycle.`,
      taskIds: [...new Set(cycles.flat())],
    });
  }

  const missingDetailIds = nodes.filter((node) => node.missingDetail).map((node) => node.id);
  if (missingDetailIds.length > 0) {
    warnings.push({
      kind: "unavailable",
      text: `${missingDetailIds.length} wave member${missingDetailIds.length === 1 ? " detail" : " details"} could not be loaded (${missingDetailIds.join(", ")}). Shown without dependencies; select to retry.`,
      taskIds: missingDetailIds,
    });
  }

  const byKind = (kind: FlowNodeKind): FlowNode[] => nodes.filter((node) => node.kind === kind);
  if (byKind("unresolved").length > 0) {
    const ids = byKind("unresolved").map((node) => node.id);
    warnings.push({
      kind: "unresolved",
      text: `Unresolved ${ids.length === 1 ? "dependency" : "dependencies"}: ${ids.join(", ")}. Existence is unconfirmed — not shown as missing or as valid external work.`,
      taskIds: [...new Set(unresolvedRefs.map((ref) => ref.by))],
    });
  }
  if (byKind("missing").length > 0) {
    const ids = byKind("missing").map((node) => node.id);
    warnings.push({
      kind: "missing",
      text: `Confirmed missing ${ids.length === 1 ? "reference" : "references"}: ${ids.join(", ")}.`,
      taskIds: ids,
    });
  }
  if (byKind("unavailable").length > 0) {
    const ids = byKind("unavailable").map((node) => node.id);
    warnings.push({
      kind: "unavailable",
      text: `Unavailable ${ids.length === 1 ? "reference" : "references"}: ${ids.join(", ")}.`,
      taskIds: ids,
    });
  }

  return { nodes, edges, warnings, cycles };
}

/** Iterative Tarjan-lite cycle detection over member-only edges. Deterministic. */
export function findCycles(memberIds: string[], edges: FlowEdge[]): string[][] {
  const members = new Set(memberIds);
  const outgoing = new Map<string, string[]>();
  for (const id of memberIds) outgoing.set(id, []);
  for (const edge of edges) {
    if (!members.has(edge.from) || !members.has(edge.to)) continue;
    outgoing.get(edge.from)?.push(edge.to);
  }
  for (const list of outgoing.values()) list.sort();

  const color = new Map<string, 0 | 1 | 2>();
  const stack: string[] = [];
  const cycles: string[][] = [];
  const seen = new Set<string>();

  const visit = (start: string): void => {
    const work: Array<{ id: string; child: number }> = [{ id: start, child: 0 }];
    color.set(start, 1);
    stack.push(start);
    while (work.length > 0) {
      const top = work[work.length - 1]!;
      const next = outgoing.get(top.id) ?? [];
      if (top.child < next.length) {
        const peer = next[top.child]!;
        top.child += 1;
        const peerColor = color.get(peer) ?? 0;
        if (peerColor === 0) {
          color.set(peer, 1);
          stack.push(peer);
          work.push({ id: peer, child: 0 });
        } else if (peerColor === 1) {
          const at = stack.indexOf(peer);
          if (at >= 0) {
            const cycle = [...stack.slice(at), peer];
            const key = normalizeCycle(cycle);
            if (!seen.has(key)) {
              seen.add(key);
              cycles.push(cycle);
            }
          }
        }
      } else {
        color.set(top.id, 2);
        stack.pop();
        work.pop();
      }
    }
  };

  for (const id of memberIds) {
    if ((color.get(id) ?? 0) === 0) visit(id);
  }
  return cycles;
}

function normalizeCycle(cycle: string[]): string {
  const core = cycle.slice(0, -1);
  const rotations = core.map((_, i) => [...core.slice(i), ...core.slice(0, i)].join("→"));
  rotations.sort();
  return rotations[0] ?? "";
}

/**
 * Stable topology identity: sorted node ids plus sorted edges. A live state
 * update that changes only statuses keeps the same key, so the parent can
 * prove the viewport was preserved.
 */
export function topologyKey(graph: Pick<FlowGraph, "nodes" | "edges">): string {
  const nodePart = graph.nodes.map((node) => `${node.kind}:${node.id}`).sort().join(",");
  const edgePart = graph.edges
    .map((edge) => `${edge.from}→${edge.to}${edge.cyclic ? "~" : ""}`)
    .sort()
    .join(",");
  return `${nodePart}|${edgePart}`;
}

export interface FlowLayout {
  positions: Record<string, { x: number; y: number }>;
  width: number;
  height: number;
  layers: string[][];
}

export const TOP_DOWN_NODE_HEIGHT = 128;
export const TOP_DOWN_GAP_X = 20;
export const TOP_DOWN_GAP_Y = 88;
export const TOP_DOWN_STAGE_GUTTER = 0;

/** Transpose the dependency ranks into a top-to-bottom DAG for native scrolling. */
export function layoutTopDownFlowGraph(graph: FlowGraph, order: string[] = []): FlowLayout {
  const ranked = layoutFlowGraph(graph, order);
  const widest = Math.max(1, ...ranked.layers.map((layer) => layer.length));
  const contentWidth = widest * NODE_WIDTH + (widest - 1) * TOP_DOWN_GAP_X;
  const positions: FlowLayout["positions"] = {};

  ranked.layers.forEach((layer, depth) => {
    const rowWidth = layer.length * NODE_WIDTH + Math.max(0, layer.length - 1) * TOP_DOWN_GAP_X;
    const left = TOP_DOWN_STAGE_GUTTER + (contentWidth - rowWidth) / 2;
    layer.forEach((id, index) => {
      positions[id] = {
        x: left + index * (NODE_WIDTH + TOP_DOWN_GAP_X),
        y: depth * (TOP_DOWN_NODE_HEIGHT + TOP_DOWN_GAP_Y),
      };
    });
  });

  return {
    positions,
    width: TOP_DOWN_STAGE_GUTTER + contentWidth,
    height: ranked.layers.length * TOP_DOWN_NODE_HEIGHT + Math.max(0, ranked.layers.length - 1) * TOP_DOWN_GAP_Y,
    layers: ranked.layers,
  };
}

/**
 * Horizontal dependency flow: longest-path layers left to right, members in
 * source order within a layer, each layer vertically centered. Cycle-safe:
 * cyclic members stack after their acyclic predecessors instead of looping.
 */
export function layoutFlowGraph(graph: FlowGraph, order: string[] = []): FlowLayout {
  const rank = new Map(order.map((id, index) => [id, index]));
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));
  const internal = graph.edges.filter((edge) => byId.has(edge.from) && byId.has(edge.to) && !edge.cyclic);
  const preds = new Map<string, string[]>();
  for (const node of graph.nodes) preds.set(node.id, []);
  for (const edge of internal) preds.get(edge.to)?.push(edge.from);

  const layerOf = new Map<string, number>();
  const place = (id: string): number => {
    const known = layerOf.get(id);
    if (known !== undefined) return known;
    // Iterative: process prerequisites first.
    const chain: string[] = [id];
    while (chain.length > 0) {
      const current = chain[chain.length - 1]!;
      if (layerOf.has(current)) {
        chain.pop();
        continue;
      }
      const pending = (preds.get(current) ?? []).filter((dep) => !layerOf.has(dep));
      if (pending.length === 0) {
        const depth = (preds.get(current) ?? []).reduce(
          (max, dep) => Math.max(max, layerOf.get(dep) ?? 0),
          -1,
        );
        layerOf.set(current, depth + 1);
        chain.pop();
      } else {
        // Cycle guard: a prerequisite already on the chain is a back edge.
        const next = pending.find((dep) => !chain.includes(dep));
        if (next === undefined) {
          const depth = (preds.get(current) ?? []).reduce(
            (max, dep) => Math.max(max, layerOf.get(dep) ?? 0),
            -1,
          );
          layerOf.set(current, depth + 1);
          chain.pop();
        } else {
          chain.push(next);
        }
      }
    }
    return layerOf.get(id) ?? 0;
  };

  // Cyclic edges still order their endpoints: from stays left of to.
  for (const edge of graph.edges.filter((item) => item.cyclic)) {
    const fromLayer = place(edge.from);
    const toLayer = layerOf.get(edge.to);
    if (toLayer !== undefined && toLayer <= fromLayer) {
      layerOf.set(edge.to, fromLayer + 1);
    } else {
      place(edge.to);
    }
  }
  for (const node of graph.nodes) place(node.id);

  const depthOf = (id: string): number => layerOf.get(id) ?? 0;
  const maxLayer = Math.max(0, ...[...layerOf.values()]);
  const layers: string[][] = Array.from({ length: maxLayer + 1 }, () => []);
  for (const node of graph.nodes) layers[depthOf(node.id)]?.push(node.id);
  for (const layer of layers) {
    layer.sort((a, b) => (rank.get(a) ?? Number.MAX_SAFE_INTEGER) - (rank.get(b) ?? Number.MAX_SAFE_INTEGER) || (a < b ? -1 : 1));
  }

  const laneHeight = NODE_HEIGHT + GAP_Y;
  const height = Math.max(
    ...layers.map((layer) => layer.length * laneHeight - GAP_Y),
    NODE_HEIGHT,
  );
  const positions: Record<string, { x: number; y: number }> = {};
  layers.forEach((layer, depth) => {
    const layerHeight = layer.length * laneHeight - GAP_Y;
    const top = (height - layerHeight) / 2;
    layer.forEach((id, index) => {
      positions[id] = {
        x: depth * (NODE_WIDTH + GAP_X),
        y: top + index * laneHeight,
      };
    });
  });

  return {
    positions,
    width: layers.length * NODE_WIDTH + (layers.length - 1) * GAP_X,
    height,
    layers,
  };
}

/** Clamp scale into the readable band; round to avoid transform jitter. */
export function clampViewport(viewport: FlowViewport): FlowViewport {
  const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, viewport.scale || 1));
  return {
    x: Math.round(viewport.x * 10) / 10,
    y: Math.round(viewport.y * 10) / 10,
    scale: Math.round(scale * 100) / 100,
  };
}

export function panViewport(viewport: FlowViewport, dx: number, dy: number): FlowViewport {
  return clampViewport({ ...viewport, x: viewport.x + dx, y: viewport.y + dy });
}

/** Zoom about a container point (defaults to origin); the anchor stays put. */
export function zoomViewport(
  viewport: FlowViewport,
  factor: number,
  center?: { x: number; y: number },
): FlowViewport {
  const next = clampViewport({ ...viewport, scale: viewport.scale * factor });
  const applied = next.scale / viewport.scale;
  if (applied === 1 || !center) return next;
  return clampViewport({
    scale: next.scale,
    x: center.x - (center.x - viewport.x) * applied,
    y: center.y - (center.y - viewport.y) * applied,
  });
}

/** Explicit Fit: whole graph visible with padding. May go below scale 1. */
export function fitViewport(
  graphSize: { width: number; height: number },
  containerSize: { width: number; height: number },
  padding = 24,
): FlowViewport {
  const availableW = Math.max(1, containerSize.width - padding * 2);
  const availableH = Math.max(1, containerSize.height - padding * 2);
  const scale = Math.min(MAX_SCALE, availableW / Math.max(1, graphSize.width), availableH / Math.max(1, graphSize.height));
  return clampViewport({
    scale,
    x: (containerSize.width - graphSize.width * scale) / 2,
    y: (containerSize.height - graphSize.height * scale) / 2,
  });
}

/** Initial view: readable scale 1 from the graph origin (never a postage stamp). */
export function initialViewport(): FlowViewport {
  return { ...DEFAULT_VIEWPORT };
}

/** Human label for each display state; color is always supplementary. */
export const DISPLAY_STATE_LABEL: Record<FlowDisplayState, string> = {
  completed: "Completed",
  executing: "Executing",
  awaiting_review: "Awaiting review",
  reviewing: "Reviewing",
  proof_blocked: "Verification required",
  cancelled: "Cancelled",
  ready: "Ready",
  backlog: "Backlog",
  queued: "Queued",
  blocked: "Blocked",
  failed: "Failed",
  unknown: "Unknown",
};

export const NODE_KIND_LABEL: Record<FlowNodeKind, string> = {
  task: "Task",
  external: "External",
  unresolved: "Unresolved dependency",
  missing: "Missing reference",
  unavailable: "Unavailable",
};
