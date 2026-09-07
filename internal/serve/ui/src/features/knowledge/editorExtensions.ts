/*
  Extension set for the corpus document editor.

  Mirrors the app's proven doc-editor stack (features/editor) at the subset the
  documentation corpus needs: StarterKit's marks + list/heading/blockquote/code
  stack, GFM tables, and the `tiptap-markdown` round-trip — plus the corpus
  wiki-link node. Mermaid and ordinary code fences reuse the shared code-block
  extension, so the Markdown model stays identical to the main editor.
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
import { CodeBlockWithMermaid } from "@/features/editor/codeblock";

export function buildKnowledgeExtensions(
  resolve: (ref: string) => DocLinkRef | undefined,
): Extensions {
  return [
    StarterKit.configure({
      // Reuse the existing code block renderer so Mermaid diagrams render in
      // Documents without introducing a second diagram implementation.
      codeBlock: false,
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
    CodeBlockWithMermaid,
    Table.configure({ resizable: false }),
    TableRow,
    TableHeader,
    TableCell,
    KnowledgeWikilink.configure({ resolve }),
  ];
}
