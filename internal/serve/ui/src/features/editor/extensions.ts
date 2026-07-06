/*
  The extension set for the doc editor — one schema for both read and edit.

  StarterKit v3 already bundles the marks + list stack + Link + Underline, so we
  only add what it lacks: highlight, image, placeholder, tables, task lists, the
  OFM nodes, and the `tiptap-markdown` round-trip. Code highlighting + mermaid
  ride on `CodeBlockWithMermaid` (CodeBlockLowlight + a conditional mermaid
  NodeView): StarterKit's plain code block is disabled (`codeBlock: false`) and
  replaced by it. Both stay the `codeBlock` node, so ```<lang> / ```mermaid
  fences round-trip byte-stable through tiptap-markdown's default serializer.
*/

import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "tiptap-markdown";
import type { MarkdownMarkSpec } from "tiptap-markdown";
import Image from "@tiptap/extension-image";
import Highlight from "@tiptap/extension-highlight";
import Placeholder from "@tiptap/extension-placeholder";
import { Table } from "@tiptap/extension-table";
import { TableRow } from "@tiptap/extension-table-row";
import { TableHeader } from "@tiptap/extension-table-header";
import { TableCell } from "@tiptap/extension-table-cell";
import { TaskList } from "@tiptap/extension-task-list";
import { TaskItem } from "@tiptap/extension-task-item";
import type { Extensions } from "@tiptap/core";
import { OFMWikilink } from "./ofm/Wikilink";
import { OFMEmbed } from "./ofm/Embed";
import { CodeBlockWithMermaid } from "./codeblock";
import type { EditorRuntimeConfig } from "./types";

/** `==highlight==` round-trip for the stock Highlight mark. */
const HighlightWithMarkdown = Highlight.extend({
  addStorage() {
    const markdown: MarkdownMarkSpec = {
      serialize: { open: "==", close: "==", mixable: true, expelEnclosingWhitespace: true },
      parse: {},
    };
    return { markdown };
  },
});

export function buildExtensions(config: EditorRuntimeConfig): Extensions {
  return [
    StarterKit.configure({
      // Replaced by CodeBlockWithMermaid (lowlight highlighting + mermaid).
      codeBlock: false,
      link: {
        openOnClick: false,
        autolink: true,
        defaultProtocol: "https",
        HTMLAttributes: { rel: "noopener noreferrer nofollow", class: "tk-link" },
      },
    }),
    CodeBlockWithMermaid,
    Markdown.configure({
      html: false,
      tightLists: true,
      linkify: false,
      breaks: false,
      transformPastedText: true,
      transformCopiedText: true,
    }),
    HighlightWithMarkdown,
    Image.configure({
      allowBase64: false,
      HTMLAttributes: { class: "tk-img", loading: "lazy", decoding: "async" },
    }),
    Placeholder.configure({ placeholder: config.placeholder ?? "Write…" }),
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    TaskList,
    TaskItem.configure({ nested: true }),
    OFMWikilink.configure({ resolve: config.resolveWikilink }),
    OFMEmbed,
  ];
}
