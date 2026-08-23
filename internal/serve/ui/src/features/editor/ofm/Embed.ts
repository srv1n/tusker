/*
  ofmEmbed — an inline atom node for `![[embeds]]`.

  Ported from cinta's `ofm-embed.js`. For now it renders as a labelled chip
  ("Embed: target"); resolving image/note/PDF previews against real assets is a
  later change after the daemon has an asset endpoint.
  Round-trips to `![[…]]` via `storage.markdown`.
*/

import { Node, mergeAttributes } from "@tiptap/core";
import type { MarkdownNodeSpec } from "tiptap-markdown";
import { buildOFMToken, parseOFMReference, type OFMEmbedKind } from "./ofm";
import { markdownItOFM } from "./markdownItOfm";

interface EmbedAttrs {
  raw: string;
  target: string;
  alias: string;
  anchor: string;
  block: string;
  heading: string;
  embedKind: OFMEmbedKind;
  displayText: string;
}

function createAttrs(token: string): EmbedAttrs {
  const ref = parseOFMReference(token);
  return {
    raw: ref.raw,
    target: ref.target,
    alias: ref.alias,
    anchor: ref.anchor,
    block: ref.block,
    heading: ref.heading,
    embedKind: ref.embedKind,
    displayText: ref.displayText,
  };
}

export const OFMEmbed = Node.create({
  name: "ofmEmbed",
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,

  addAttributes() {
    return {
      raw: { default: "" },
      target: { default: "" },
      alias: { default: "" },
      anchor: { default: "" },
      block: { default: "" },
      heading: { default: "" },
      embedKind: { default: "note" },
      displayText: { default: "" },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'span[data-ofm-kind="embed"]',
        getAttrs: (element) => ({
          raw: element.getAttribute("data-ofm-raw") || "",
          target: element.getAttribute("data-ofm-target") || "",
          alias: element.getAttribute("data-ofm-alias") || "",
          anchor: element.getAttribute("data-ofm-anchor") || "",
          block: element.getAttribute("data-ofm-block") || "",
          heading: element.getAttribute("data-ofm-heading") || "",
          embedKind: (element.getAttribute("data-ofm-embed-kind") || "note") as OFMEmbedKind,
          displayText: element.textContent?.replace(/^Embed:\s*/, "") || "",
        }),
      },
    ];
  },

  renderHTML({ node }) {
    const a = node.attrs as EmbedAttrs;
    const label = a.displayText || a.target || a.raw;
    return [
      "span",
      mergeAttributes({
        "data-ofm-kind": "embed",
        "data-ofm-raw": a.raw,
        "data-ofm-target": a.target,
        "data-ofm-alias": a.alias,
        "data-ofm-anchor": a.anchor,
        "data-ofm-block": a.block,
        "data-ofm-heading": a.heading,
        "data-ofm-embed-kind": a.embedKind,
        class: "tk-embed",
        contenteditable: "false",
      }),
      `Embed: ${label}`,
    ];
  },

  renderText({ node }) {
    const a = node.attrs as EmbedAttrs;
    return a.raw || buildOFMToken(true, a.target, a.anchor, a.alias);
  },

  addStorage() {
    const markdown: MarkdownNodeSpec = {
      serialize(state, node) {
        const a = node.attrs as EmbedAttrs;
        state.write(a.raw || buildOFMToken(true, a.target, a.anchor, a.alias));
      },
      parse: {
        setup(markdownit) {
          markdownItOFM(markdownit);
        },
      },
    };
    return { markdown };
  },

  addCommands() {
    return {};
  },
});

export { createAttrs as createEmbedAttrs };
