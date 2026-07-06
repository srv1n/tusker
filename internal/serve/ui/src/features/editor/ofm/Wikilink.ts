/*
  ofmWikilink — an inline atom node for `[[wiki-links]]`.

  Ported from cinta's `ofm-wikilink.js` and adapted to TipTap v3 +
  `tiptap-markdown`:
    - renderHTML resolves the target against an injected vault index and stamps
      `data-ofm-status` (resolved | broken | empty) for styling; the host wires
      clicks to navigation (see DocEditor).
    - `storage.markdown` gives the OUTBOUND serialization (node → `[[…]]`); the
      INBOUND side is the markdown-it rule in `./markdownItOfm.ts`, registered
      here via `parse.setup`.
*/

import { Node, mergeAttributes, nodeInputRule, PasteRule } from "@tiptap/core";
import type { MarkdownNodeSpec } from "tiptap-markdown";
import { buildOFMToken, parseOFMReference } from "./ofm";
import { markdownItOFM } from "./markdownItOfm";
import type { WikilinkTargetLite } from "../types";

export interface OFMWikilinkOptions {
  /** Resolve a link target to a vault entry (for status + tooltip). */
  resolve: (id: string) => WikilinkTargetLite | undefined;
}

interface WikilinkAttrs {
  raw: string;
  target: string;
  alias: string;
  anchor: string;
  block: string;
  heading: string;
  displayText: string;
}

function createAttrs(token: string): WikilinkAttrs {
  const ref = parseOFMReference(token);
  return {
    raw: ref.raw,
    target: ref.target,
    alias: ref.alias,
    anchor: ref.anchor,
    block: ref.block,
    heading: ref.heading,
    displayText: ref.displayText,
  };
}

function tokenFromMatch(match: RegExpMatchArray): string {
  return match[1] || match[0] || "";
}

export const OFMWikilink = Node.create<OFMWikilinkOptions>({
  name: "ofmWikilink",
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
      target: { default: "" },
      alias: { default: "" },
      anchor: { default: "" },
      block: { default: "" },
      heading: { default: "" },
      displayText: { default: "" },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'span[data-ofm-kind="wikilink"]',
        getAttrs: (element) => ({
          raw: element.getAttribute("data-ofm-raw") || "",
          target: element.getAttribute("data-ofm-target") || "",
          alias: element.getAttribute("data-ofm-alias") || "",
          anchor: element.getAttribute("data-ofm-anchor") || "",
          block: element.getAttribute("data-ofm-block") || "",
          heading: element.getAttribute("data-ofm-heading") || "",
          displayText: element.textContent || "",
        }),
      },
    ];
  },

  renderHTML({ node }) {
    const a = node.attrs as WikilinkAttrs;
    const resolved = a.target ? this.options.resolve(a.target) : undefined;
    const status = a.target ? (resolved ? "resolved" : "broken") : "empty";
    const display = a.displayText || a.alias || a.target || a.raw || "[[link]]";
    return [
      "span",
      mergeAttributes({
        "data-ofm-kind": "wikilink",
        "data-ofm-raw": a.raw,
        "data-ofm-target": a.target,
        "data-ofm-alias": a.alias,
        "data-ofm-anchor": a.anchor,
        "data-ofm-block": a.block,
        "data-ofm-heading": a.heading,
        "data-ofm-status": status,
        class: "tk-wikilink",
        title: resolved
          ? `${resolved.kind} · ${resolved.title}`
          : a.target
            ? "Unresolved reference — no such note in the vault"
            : "",
        contenteditable: "false",
      }),
      display,
    ];
  },

  renderText({ node }) {
    const a = node.attrs as WikilinkAttrs;
    return a.raw || `[[${a.target || a.displayText || ""}]]`;
  },

  addStorage() {
    const markdown: MarkdownNodeSpec = {
      serialize(state, node) {
        const a = node.attrs as WikilinkAttrs;
        state.write(a.raw || buildOFMToken(false, a.target, a.anchor, a.alias));
      },
      parse: {
        setup(markdownit) {
          markdownItOFM(markdownit);
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
        getAttributes: (match) => createAttrs(tokenFromMatch(match)),
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
            .insertContent({ type: this.name, attrs: createAttrs(tokenFromMatch(match)) })
            .run();
        },
      }),
    ];
  },
});
