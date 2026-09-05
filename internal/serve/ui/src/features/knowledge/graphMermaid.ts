import type { DocgraphEdge, DocgraphNode } from "./types";

const EDGE_LABEL: Record<DocgraphEdge["kind"], string> = {
  part_of: "part of",
  updates: "updates",
  source: "source",
  decides_for: "decides for",
  superseded_by: "superseded by",
  link: "link",
};

/** Keep user-authored document titles readable without giving Mermaid syntax. */
function mermaidLabel(value: string): string {
  return value
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .replace(/["\\]/g, "'")
    .replace(/[<>|]/g, (character) => character === "<" ? "‹" : character === ">" ? "›" : "¦")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 120);
}

/**
 * Build the UI's Mermaid view from the same graph nodes and edges returned by
 * Serve. Unknown edge endpoints are omitted so the renderer can never invent
 * a relationship that the authoritative graph did not provide.
 */
export function docGraphMermaid(nodes: DocgraphNode[], edges: DocgraphEdge[]): string {
  const nodeIds = new Map(nodes.map((node, index) => [node.subject, `doc${index}`]));
  const lines = ["flowchart TD"];

  for (const [index, node] of nodes.entries()) {
    const id = `doc${index}`;
    const title = mermaidLabel(node.title || node.subject) || mermaidLabel(node.subject) || "Untitled";
    lines.push(`  ${id}["${title}"]`);
  }

  for (const edge of edges) {
    const from = nodeIds.get(edge.from);
    const to = nodeIds.get(edge.to);
    if (!from || !to) continue;
    lines.push(`  ${from} -->|${EDGE_LABEL[edge.kind]}| ${to}`);
  }

  return lines.join("\n");
}
