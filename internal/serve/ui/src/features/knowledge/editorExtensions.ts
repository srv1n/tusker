/*
  Extension set for the corpus document editor (SRV-T-0002).

  Mirrors the app's proven doc-editor stack (features/editor) at the subset the
  documentation corpus needs: StarterKit's marks + list/heading/blockquote/code
  stack, GFM tables, and the `tiptap-markdown` round-trip — plus the corpus
  wiki-link node. StarterKit's plain code block is kept (fences round-trip through
  tiptap-markdown's default code_block serializer); mermaid/lowlight are out of
  scope here, so a fenced block simply renders as a code slab.
*/

import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "tiptap-markdown";
import { Table } from "@tiptap/extension-table";
import { TableRow } from "@tiptap/extension-table-row";
import { TableHeader } from "@tiptap/extension-table-header";
import { TableCell } from "@tiptap/extension-table-cell";
import type { Extensions } from "@tiptap/core";
import { KnowledgeWikilink } from "./wikilink";
import type { DocLinkRef } from "./types";

export function buildKnowledgeExtensions(
  resolve: (ref: string) => DocLinkRef | undefined,
): Extensions {
  return [
    StarterKit.configure({
      link: {
        openOnClick: false,
        autolink: true,
        defaultProtocol: "https",
        HTMLAttributes: { rel: "noopener noreferrer nofollow", class: "tk-link" },
      },
    }),
    Markdown.configure({
      html: false,
      tightLists: true,
      linkify: false,
      breaks: false,
      transformPastedText: true,
      transformCopiedText: true,
    }),
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    KnowledgeWikilink.configure({ resolve }),
  ];
}
