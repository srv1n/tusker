/*
  Filesystem-literal folder tree for the docs explorer rail (SRV-T-0004).

  The rail mirrors the corpus on disk as obviously as possible: roots and nesting
  come straight from splitting each doc's `path` on "/", showing the real
  directory names (docs, system, .tusker, specs, decisions) with no invented
  groupings. A single-child folder chain (e.g. docs → system, with nothing else
  in docs) is collapsed into one row labeled with the joined real segments
  ("docs/system") so the tree reads cleanly without inventing names.

  Kept free of React so the layout math stays testable on its own.
*/

import type { DocgraphDoc, DocgraphKind } from "./types";

export interface TreeLeaf {
  type: "doc";
  /** Stable node id — the doc's repo-relative path (unique per doc). */
  id: string;
  subject: string;
  title: string;
  kind: DocgraphKind;
  status: string;
  path: string;
  /** Nesting depth for indentation (assigned by tree position). */
  depth: number;
}

export interface TreeFolder {
  type: "folder";
  /** Stable node id — the folder's real cumulative path (e.g. ".tusker/specs"). */
  id: string;
  /** Display label — the real path segment(s); joined when a chain is collapsed. */
  name: string;
  depth: number;
  children: TreeNode[];
}

export type TreeNode = TreeFolder | TreeLeaf;

interface MutableFolder {
  type: "folder";
  id: string;
  name: string;
  depth: number;
  children: MutableNode[];
  _index: Map<string, MutableFolder>;
}
type MutableNode = MutableFolder | TreeLeaf;

function newFolder(id: string, name: string): MutableFolder {
  return { type: "folder", id, name, depth: 0, children: [], _index: new Map() };
}

/** Walk each doc's path into a real directory tree, then collapse + sort it. */
export function buildDocTree(docs: DocgraphDoc[]): TreeNode[] {
  const root = newFolder("", "");

  for (const doc of docs) {
    const segments = doc.path.split("/").filter(Boolean);
    segments.pop(); // the last segment is the file — it becomes the leaf
    let folder = root;
    let acc = "";
    for (const seg of segments) {
      acc = acc ? `${acc}/${seg}` : seg;
      let child = folder._index.get(seg);
      if (!child) {
        child = newFolder(acc, seg);
        folder._index.set(seg, child);
        folder.children.push(child);
      }
      folder = child;
    }
    folder.children.push({
      type: "doc",
      id: doc.path,
      subject: doc.subject,
      title: doc.title,
      kind: doc.kind,
      status: doc.status,
      path: doc.path,
      depth: 0,
    });
  }

  const roots = root.children.map((c) => (c.type === "folder" ? collapse(c) : c));
  roots.forEach((n) => assignDepth(n, 0));
  roots.forEach((n) => {
    if (n.type === "folder") sortFolder(n);
  });
  roots.sort(compareNodes);
  return roots.map(strip);
}

/**
 * Collapse a single-child folder chain into one row: a folder whose only child
 * is another folder merges into it, joining the real segment names. The merged
 * node keeps the DEEPEST cumulative path as its id, so it is exactly one of the
 * path prefixes {@link ancestorFolderIds} yields — auto-expand keeps working.
 */
function collapse(folder: MutableFolder): MutableFolder {
  folder.children = folder.children.map((c) => (c.type === "folder" ? collapse(c) : c));
  while (folder.children.length === 1 && folder.children[0]!.type === "folder") {
    const only = folder.children[0] as MutableFolder;
    folder = { ...only, name: `${folder.name}/${only.name}` };
  }
  return folder;
}

function assignDepth(node: MutableNode, depth: number): void {
  node.depth = depth;
  if (node.type === "folder") for (const child of node.children) assignDepth(child, depth + 1);
}

/** Folders before files; folders dot-dirs last then A→Z; files by title. */
function compareNodes(a: MutableNode, b: MutableNode): number {
  if (a.type !== b.type) return a.type === "folder" ? -1 : 1;
  if (a.type === "folder" && b.type === "folder") {
    return dirRank(a.name) - dirRank(b.name) || a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  }
  const at = (a as TreeLeaf).title;
  const bt = (b as TreeLeaf).title;
  return at.localeCompare(bt, undefined, { sensitivity: "base" });
}

// Visible directories sort before dot-directories, so docs/system leads
// .tusker/specs — the order the corpus reads in on disk.
function dirRank(name: string): number {
  return name.startsWith(".") ? 1 : 0;
}

function sortFolder(folder: MutableFolder): void {
  folder.children.sort(compareNodes);
  for (const child of folder.children) if (child.type === "folder") sortFolder(child);
}

/** Drop the internal build index so the returned tree is a plain data shape. */
function strip(node: MutableNode): TreeNode {
  if (node.type !== "folder") return node;
  return {
    type: "folder",
    id: node.id,
    name: node.name,
    depth: node.depth,
    children: node.children.map(strip),
  };
}

/**
 * The folder ids that contain a doc, root → deepest, used to auto-expand its
 * ancestors on navigation. Returns every cumulative path prefix; ids that were
 * collapsed away (e.g. "docs" under "docs/system") simply aren't nodes, and
 * expanding a non-existent id is a harmless no-op.
 */
export function ancestorFolderIds(path: string): string[] {
  const segments = path.split("/").filter(Boolean);
  segments.pop(); // drop the filename
  const ids: string[] = [];
  let acc = "";
  for (const seg of segments) {
    acc = acc ? `${acc}/${seg}` : seg;
    ids.push(acc);
  }
  return ids;
}

/** Case-insensitive match on title, subject, or filename. */
export function docMatches(doc: DocgraphDoc, needle: string): boolean {
  const filename = doc.path.split("/").pop() ?? "";
  return (
    doc.title.toLowerCase().includes(needle) ||
    doc.subject.toLowerCase().includes(needle) ||
    filename.toLowerCase().includes(needle)
  );
}
