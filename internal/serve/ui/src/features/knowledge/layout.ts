/*
  Pure layered-DAG layout for the document graph. Kept free of React so it can be
  reasoned about (and tested) on its own; the component consumes its output and
  stays a thin renderer.

  Layering: depth is a BFS over `part_of` edges from the root (the node with no
  part_of — the overview). Layer 0 is the root at the top; children fall below.
  Within a layer nodes are ordered by a single barycenter pass (mean of parent
  column indices) to reduce crossings. Nodes with no part_of relation (orphans)
  drop to a bottom layer, ordered by the columns of whatever they *do* connect
  to (updates / decides_for / superseded_by) so they sit near their referents.
*/

import type { DocgraphEdge, DocgraphKind, DocgraphNode } from "./types";

export interface PositionedNode {
  subject: string;
  title: string;
  kind: DocgraphKind;
  status: string;
  x: number;
  y: number;
  w: number;
  h: number;
  depth: number;
}

export interface PositionedEdge {
  id: string;
  from: string;
  to: string;
  kind: DocgraphEdge["kind"];
  d: string;
}

export interface DocGraphLayout {
  nodes: PositionedNode[];
  edges: PositionedEdge[];
  width: number;
  height: number;
}

const NODE_W = 184;
const NODE_H = 52;
const H_GAP = 40;
const V_GAP = 96;
const PAD = 56;

const round = (n: number): number => Math.round(n * 10) / 10;

/** Layer + place every node, then route an edge path between each connected pair. */
export function layoutDocGraph(nodes: DocgraphNode[], edges: DocgraphEdge[]): DocGraphLayout {
  if (nodes.length === 0) return { nodes: [], edges: [], width: 0, height: 0 };

  const bySubject = new Map(nodes.map((n) => [n.subject, n]));

  // part_of: child -> parent (edge.from is the child that "is part of" edge.to).
  const parentOf = new Map<string, string>();
  const childrenOf = new Map<string, string[]>();
  for (const e of edges) {
    if (e.kind !== "part_of") continue;
    if (!bySubject.has(e.from) || !bySubject.has(e.to)) continue;
    parentOf.set(e.from, e.to);
    const kids = childrenOf.get(e.to);
    if (kids) kids.push(e.from);
    else childrenOf.set(e.to, [e.from]);
  }

  const inTree = (s: string): boolean => parentOf.has(s) || childrenOf.has(s);

  // Depth by BFS from roots — tree members with no parent (typically the overview).
  const depth = new Map<string, number>();
  const queue: string[] = [];
  for (const n of nodes) {
    if (inTree(n.subject) && !parentOf.has(n.subject)) {
      depth.set(n.subject, 0);
      queue.push(n.subject);
    }
  }
  while (queue.length > 0) {
    const cur = queue.shift() as string;
    const d = depth.get(cur) as number;
    for (const child of childrenOf.get(cur) ?? []) {
      if (!depth.has(child)) {
        depth.set(child, d + 1);
        queue.push(child);
      }
    }
  }
  // A tree node unreached by BFS (broken chain) climbs to a depth of its own.
  for (const n of nodes) {
    if (!inTree(n.subject) || depth.has(n.subject)) continue;
    let d = 0;
    let cur = n.subject;
    const seen = new Set<string>();
    while (parentOf.has(cur) && !seen.has(cur)) {
      seen.add(cur);
      cur = parentOf.get(cur) as string;
      d += 1;
    }
    depth.set(n.subject, d);
  }

  const treeDepths = [...depth.values()];
  const maxTreeDepth = treeDepths.length > 0 ? Math.max(...treeDepths) : -1;
  const orphanLayer = maxTreeDepth + 1;
  for (const n of nodes) if (!depth.has(n.subject)) depth.set(n.subject, orphanLayer);

  const maxDepth = Math.max(...depth.values());
  const layers: string[][] = Array.from({ length: maxDepth + 1 }, () => []);
  for (const n of nodes) layers[depth.get(n.subject) as number].push(n.subject);
  // Deterministic seed order so the barycenter pass is stable run to run.
  for (const layer of layers) layer.sort((a, b) => a.localeCompare(b));

  // Undirected adjacency across all edges — used to place orphans sensibly.
  const neighbors = new Map<string, string[]>();
  const addNeighbor = (a: string, b: string): void => {
    const arr = neighbors.get(a);
    if (arr) arr.push(b);
    else neighbors.set(a, [b]);
  };
  for (const e of edges) {
    if (!bySubject.has(e.from) || !bySubject.has(e.to)) continue;
    addNeighbor(e.from, e.to);
    addNeighbor(e.to, e.from);
  }

  // Single top-down barycenter pass. `column` holds each node's index in its layer.
  const column = new Map<string, number>();
  layers.forEach((layer, d) => {
    if (d === 0) {
      layer.forEach((s, i) => column.set(s, i));
      return;
    }
    const scored = layer.map((s) => {
      const refs: number[] = [];
      const parent = parentOf.get(s);
      if (parent !== undefined && column.has(parent)) refs.push(column.get(parent) as number);
      if (refs.length === 0) {
        for (const nb of neighbors.get(s) ?? []) {
          if (column.has(nb)) refs.push(column.get(nb) as number);
        }
      }
      const bary = refs.length > 0 ? refs.reduce((a, b) => a + b, 0) / refs.length : layer.length;
      return { s, bary };
    });
    scored.sort((a, b) => a.bary - b.bary || a.s.localeCompare(b.s));
    scored.forEach(({ s }, i) => column.set(s, i));
    layer.splice(0, layer.length, ...scored.map((x) => x.s));
  });

  const layerWidths = layers.map(
    (l) => l.length * NODE_W + Math.max(0, l.length - 1) * H_GAP,
  );
  const maxWidth = Math.max(NODE_W, ...layerWidths);

  const positioned = new Map<string, PositionedNode>();
  const out: PositionedNode[] = [];
  layers.forEach((layer, d) => {
    const startX = PAD + (maxWidth - layerWidths[d]) / 2;
    layer.forEach((s, i) => {
      const n = bySubject.get(s) as DocgraphNode;
      const node: PositionedNode = {
        subject: s,
        title: n.title,
        kind: n.kind,
        status: n.status,
        x: round(startX + i * (NODE_W + H_GAP)),
        y: round(PAD + d * (NODE_H + V_GAP)),
        w: NODE_W,
        h: NODE_H,
        depth: d,
      };
      positioned.set(s, node);
      out.push(node);
    });
  });

  const positionedEdges: PositionedEdge[] = [];
  edges.forEach((e, idx) => {
    const a = positioned.get(e.from);
    const b = positioned.get(e.to);
    if (!a || !b) return;
    positionedEdges.push({
      id: `${e.from}~${e.to}~${e.kind}~${idx}`,
      from: e.from,
      to: e.to,
      kind: e.kind,
      d: edgePath(a, b),
    });
  });

  const width = round(maxWidth + PAD * 2);
  const height = round(PAD * 2 + (maxDepth + 1) * NODE_H + maxDepth * V_GAP);
  return { nodes: out, edges: positionedEdges, width, height };
}

/** A vertical cubic bezier between two node anchors; same-layer pairs arc sideways. */
function edgePath(a: PositionedNode, b: PositionedNode): string {
  const ax = a.x + a.w / 2;
  const bx = b.x + b.w / 2;

  if (a.y === b.y) {
    const leftFirst = a.x < b.x;
    const sx = leftFirst ? a.x + a.w : a.x;
    const ex = leftFirst ? b.x : b.x + b.w;
    const my = a.y + a.h / 2;
    const lift = 42;
    return `M ${round(sx)} ${round(my)} C ${round(sx + (ex - sx) * 0.4)} ${round(my - lift)}, ${round(ex - (ex - sx) * 0.4)} ${round(my - lift)}, ${round(ex)} ${round(my)}`;
  }

  let ay: number;
  let by: number;
  if (b.y < a.y) {
    ay = a.y;
    by = b.y + b.h;
  } else {
    ay = a.y + a.h;
    by = b.y;
  }
  const midY = (ay + by) / 2;
  return `M ${round(ax)} ${round(ay)} C ${round(ax)} ${round(midY)}, ${round(bx)} ${round(midY)}, ${round(bx)} ${round(by)}`;
}
