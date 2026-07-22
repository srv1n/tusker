/*
  A `[[ref]]` / `[[ref|label]]` inline node for the corpus editor (SRV-T-0002).

  The rendered document is the editor, so wiki-links must look and behave exactly
  as the read-only reader (Markdown.tsx) drew them: a resolved ref is info-tinted
  and navigable; an unresolved ref is muted with a "no such document" hint.

  Resolution comes from the injected resolver (the API `links` array), the same
  source the reader uses. The `raw` `[[…]]` text is preserved verbatim and
  written straight back out, so serialization is byte-exact regardless of any
  internal spacing or alias.
*/

import { Node, mergeAttributes, nodeInputRule, PasteRule } from "@tiptap/core";
import type { MarkdownNodeSpec } from "tiptap-markdown";
import type { DocLinkRef } from "./types";

export interface KnowledgeWikilinkOptions {
  /** Resolve a `[[ref]]` against the corpus (the doc detail's `links`). */
  resolve: (ref: string) => DocLinkRef | undefined;
}

interface WikilinkAttrs {
  raw: string;
  ref: string;
  label: string;
}

const WIKILINK_RE = /^\[\[([^\]\n|]+)(?:\|([^\]\n]*))?\]\]/;

/** Parse a single `[[…]]` token into its stored attributes. */
function parseWikilink(token: string): WikilinkAttrs {
  const m = WIKILINK_RE.exec(token);
  if (!m) return { raw: token, ref: token.replace(/^\[\[|\]\]$/g, "").trim(), label: "" };
  return { raw: m[0], ref: (m[1] ?? "").trim(), label: (m[2] ?? "").trim() };
}

function displayOf(a: WikilinkAttrs): string {
  return a.label || a.ref || a.raw || "[[link]]";
}

// Match the reader's two treatments exactly (Markdown.tsx WikiLink):
// resolved → info, dotted underline, navigable; unresolved → muted, dotted, help.
const RESOLVED_CLASS =
  "kg-wikilink font-mono text-[0.92em] text-info decoration-info/40 underline decoration-dotted underline-offset-2 hover:decoration-info cursor-pointer";
const UNRESOLVED_CLASS =
  "kg-wikilink font-mono text-[0.92em] text-muted decoration-muted/50 underline decoration-dotted underline-offset-2 cursor-help";

export const KnowledgeWikilink = Node.create<KnowledgeWikilinkOptions>({
  name: "kgWikilink",
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,

  addOptions() {
    return { resolve: () => undefined };
  },

  addAttributes() {
    return {
      raw: { default: "" },
      ref: { default: "" },
      label: { default: "" },
    };
  },

  parseHTML() {
    return [
      {
        tag: "span[data-kg-wikilink]",
        getAttrs: (el) => ({
          raw: el.getAttribute("data-kg-raw") || "",
          ref: el.getAttribute("data-kg-ref") || "",
          label: el.getAttribute("data-kg-label") || "",
        }),
      },
    ];
  },

  renderHTML({ node }) {
    const a = node.attrs as WikilinkAttrs;
    const link = a.ref ? this.options.resolve(a.ref) : undefined;
    const resolved = !!link?.resolved;
    return [
      "span",
      mergeAttributes({
        "data-kg-wikilink": "",
        "data-kg-raw": a.raw,
        "data-kg-ref": a.ref,
        "data-kg-label": a.label,
        "data-kg-subject": resolved ? (link?.subject ?? a.ref) : "",
        "data-kg-resolved": resolved ? "true" : "false",
        class: resolved ? RESOLVED_CLASS : UNRESOLVED_CLASS,
        title: resolved
          ? (link?.path ?? "")
          : "No such document — this reference does not resolve in the corpus",
        contenteditable: "false",
      }),
      displayOf(a),
    ];
  },

  // The atom serializes to its own display text when copied as plain text.
  renderText({ node }) {
    return (node.attrs as WikilinkAttrs).raw;
  },

  addStorage() {
    const markdown: MarkdownNodeSpec = {
      // Byte-exact: write the original `[[…]]` token straight back out.
      serialize(state, node) {
        state.write((node.attrs as WikilinkAttrs).raw);
      },
      parse: {
        setup(markdownit) {
          installWikilinkRule(markdownit as unknown as MarkdownItLike);
        },
      },
    };
    return { markdown };
  },

  addInputRules() {
    return [
      nodeInputRule({
        find: /(\[\[[^\]\n]+?\]\])$/,
        type: this.type,
        getAttributes: (match) => parseWikilink(match[1] || match[0] || ""),
      }),
    ];
  },

  addPasteRules() {
    return [
      new PasteRule({
        find: /(\[\[[^\]\n]+?\]\])/g,
        handler: ({ chain, range, match }) => {
          chain()
            .deleteRange(range)
            .insertContent({ type: this.name, attrs: parseWikilink(match[1] || match[0] || "") })
            .run();
        },
      }),
    ];
  },
});

// ---------------------------------------------------------------------------
// markdown-it inbound rule. tiptap-markdown renders markdown → HTML via
// markdown-it, then feeds that HTML through parseHTML(); we intercept `[[…]]`
// before markdown-it's own `link` rule and emit the span parseHTML matches.
// A minimal structural surface avoids the @types/markdown-it dual-package clash.
// ---------------------------------------------------------------------------

interface InlineToken {
  meta: unknown;
  content: string;
}
interface InlineState {
  src: string;
  pos: number;
  posMax: number;
  push(type: string, tag: string, nesting: number): InlineToken;
}
type InlineRule = (state: InlineState, silent: boolean) => boolean;
interface MarkdownItLike {
  utils: { escapeHtml(value: string): string };
  inline: { ruler: { before(before: string, name: string, rule: InlineRule): void } };
  renderer: { rules: Record<string, unknown> };
}

const BRACKET = 0x5b; // [

function installWikilinkRule(md: MarkdownItLike): void {
  const guarded = md as MarkdownItLike & { __kgWikilinkInstalled?: boolean };
  if (guarded.__kgWikilinkInstalled) return;
  guarded.__kgWikilinkInstalled = true;

  const escape = md.utils.escapeHtml;

  const rule: InlineRule = (state, silent) => {
    if (state.src.charCodeAt(state.pos) !== BRACKET) return false;
    const m = WIKILINK_RE.exec(state.src.slice(state.pos, state.posMax));
    if (!m) return false;
    if (!silent) {
      const token = state.push("kg_wikilink", "", 0);
      token.meta = parseWikilink(m[0]);
      token.content = m[0];
    }
    state.pos += m[0].length;
    return true;
  };

  // Before `link` so `[[` wins over markdown-it's `[` link parsing.
  md.inline.ruler.before("link", "kg_wikilink", rule);

  md.renderer.rules.kg_wikilink = (tokens: InlineToken[], idx: number): string => {
    const a = tokens[idx]?.meta as WikilinkAttrs;
    return (
      `<span data-kg-wikilink` +
      ` data-kg-raw="${escape(a.raw)}"` +
      ` data-kg-ref="${escape(a.ref)}"` +
      ` data-kg-label="${escape(a.label)}">` +
      `${escape(displayOf(a))}</span>`
    );
  };
}
