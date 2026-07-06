/*
  Sanitization boundary.

  The one place untrusted-ish content becomes DOM is the mermaid renderer:
  mermaid turns diagram source into an SVG string, and that SVG is injected into
  the page. We scrub it with DOMPurify before it ever touches the DOM — the same
  posture as the BrainDump editor (cinta) — forbidding scripting, foreign
  objects, and external references.

  We render mermaid with htmlLabels:false (see mermaid.ts), so node labels are
  native SVG <text> and a diagram never legitimately contains <foreignObject>.
  Forbidding foreignObject is therefore both harmless (labels don't need it) and
  defense in depth (foreignObject is the classic SVG→HTML XSS bridge).

  Markdown body content does NOT pass through here: `tiptap-markdown` is
  configured with `html: false`, so raw HTML in a doc is inert, and the
  ProseMirror schema is itself an allow-list of node types.
*/

import DOMPurify from "dompurify";

const MERMAID_FORBID_TAGS = [
  "script",
  "foreignObject",
  "iframe",
  "object",
  "embed",
  "image",
];

const MERMAID_FORBID_ATTR = [
  "onerror",
  "onload",
  "onclick",
  "onmouseover",
  "href",
  "xlink:href",
];

/**
 * Sanitize a mermaid-rendered SVG string. Keeps the SVG drawing profile but
 * strips anything that could script or fetch. Returns a safe SVG string (empty
 * string if input is falsy).
 */
export function sanitizeMermaidSvg(svg: string): string {
  if (!svg) return "";
  return DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
    FORBID_TAGS: MERMAID_FORBID_TAGS,
    FORBID_ATTR: MERMAID_FORBID_ATTR,
    ADD_ATTR: ["viewBox", "preserveAspectRatio", "transform", "marker-end", "marker-start"],
  });
}

const SAFE_HREF = /^(https?:|mailto:|tel:)/i;

/** True for hrefs we allow an external link to navigate to. */
export function isSafeHref(url: string | null | undefined): boolean {
  if (!url) return false;
  const trimmed = url.trim();
  if (trimmed.startsWith("#") || trimmed.startsWith("/")) return true;
  return SAFE_HREF.test(trimmed);
}
