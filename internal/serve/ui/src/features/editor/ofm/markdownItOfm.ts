/*
  markdown-it plugin: OFM inline syntax → HTML the TipTap schema parses.

  This is the INBOUND half of the round-trip. `tiptap-markdown` renders markdown
  with markdown-it, then feeds the HTML to ProseMirror via each node's
  `parseHTML()`. We add an inline rule that intercepts `[[…]]`, `![[…]]`, and
  `==…==` (before markdown-it's own `[`/`![` link/image rules) and renderer rules
  that emit the `data-ofm-kind` spans / `<mark>` that Wikilink / Embed / Highlight
  match. The OUTBOUND half (ProseMirror → markdown) is each node's
  `storage.markdown.serialize`.
*/

import { parseOFMReference, type OFMReference } from "./ofm";

// Minimal structural types. Typing against `@types/markdown-it` directly is
// brittle here: tiptap-markdown and prosemirror-markdown each vendor their own
// copy, so a nominal `MarkdownIt` from one is not assignable to the other. A
// structural surface covering only what we touch sidesteps the duplicate.
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

const BANG = 0x21; // !
const BRACKET = 0x5b; // [
const EQUALS = 0x3d; // =

const EMBED_RE = /^!\[\[[^\]\n]+?\]\]/;
const WIKILINK_RE = /^\[\[[^\]\n]+?\]\]/;
const HIGHLIGHT_RE = /^==([^=\n](?:[^=\n]|=(?!=))*)==/;

function attr(value: string, escape: (s: string) => string): string {
  return escape(value || "");
}

function renderWikilink(ref: OFMReference, escape: (s: string) => string): string {
  const display = ref.displayText || ref.raw || "[[link]]";
  return (
    `<span data-ofm-kind="wikilink"` +
    ` data-ofm-raw="${attr(ref.raw, escape)}"` +
    ` data-ofm-target="${attr(ref.target, escape)}"` +
    ` data-ofm-alias="${attr(ref.alias, escape)}"` +
    ` data-ofm-anchor="${attr(ref.anchor, escape)}"` +
    ` data-ofm-block="${attr(ref.block, escape)}"` +
    ` data-ofm-heading="${attr(ref.heading, escape)}">` +
    `${escape(display)}</span>`
  );
}

function renderEmbed(ref: OFMReference, escape: (s: string) => string): string {
  const display = ref.displayText || ref.raw || "";
  return (
    `<span data-ofm-kind="embed"` +
    ` data-ofm-raw="${attr(ref.raw, escape)}"` +
    ` data-ofm-target="${attr(ref.target, escape)}"` +
    ` data-ofm-alias="${attr(ref.alias, escape)}"` +
    ` data-ofm-anchor="${attr(ref.anchor, escape)}"` +
    ` data-ofm-block="${attr(ref.block, escape)}"` +
    ` data-ofm-heading="${attr(ref.heading, escape)}"` +
    ` data-ofm-embed-kind="${attr(ref.embedKind, escape)}">` +
    `${escape(display)}</span>`
  );
}

export function markdownItOFM(md: MarkdownItLike): void {
  // Several nodes register this via their `parse.setup`; install exactly once so
  // the inline ruler doesn't get a duplicate rule name.
  const guarded = md as MarkdownItLike & { __ofmInstalled?: boolean };
  if (guarded.__ofmInstalled) return;
  guarded.__ofmInstalled = true;

  const escape = md.utils.escapeHtml;

  const rule = (state: InlineState, silent: boolean): boolean => {
    const { src, pos } = state;
    const ch = src.charCodeAt(pos);
    if (ch !== BANG && ch !== BRACKET && ch !== EQUALS) return false;

    const rest = src.slice(pos, state.posMax);

    // Embed must be tested before wiki-link (shares the `[[` opener).
    if (ch === BANG) {
      const m = EMBED_RE.exec(rest);
      if (!m) return false;
      if (!silent) {
        const token = state.push("ofm_embed", "", 0);
        token.meta = parseOFMReference(m[0]);
        token.content = m[0];
      }
      state.pos += m[0].length;
      return true;
    }

    if (ch === BRACKET) {
      const m = WIKILINK_RE.exec(rest);
      if (!m) return false;
      if (!silent) {
        const token = state.push("ofm_wikilink", "", 0);
        token.meta = parseOFMReference(m[0]);
        token.content = m[0];
      }
      state.pos += m[0].length;
      return true;
    }

    // ch === EQUALS
    const m = HIGHLIGHT_RE.exec(rest);
    if (!m) return false;
    if (!silent) {
      const token = state.push("ofm_highlight", "", 0);
      token.content = m[1] ?? "";
    }
    state.pos += m[0].length;
    return true;
  };

  // Intercept before `link` (and therefore before `image`) so `[[`/`![[` win.
  md.inline.ruler.before("link", "ofm", rule);

  md.renderer.rules.ofm_wikilink = (tokens: InlineToken[], idx: number) =>
    renderWikilink(tokens[idx]?.meta as OFMReference, escape);
  md.renderer.rules.ofm_embed = (tokens: InlineToken[], idx: number) =>
    renderEmbed(tokens[idx]?.meta as OFMReference, escape);
  md.renderer.rules.ofm_highlight = (tokens: InlineToken[], idx: number) =>
    `<mark>${escape(tokens[idx]?.content ?? "")}</mark>`;
}
