/*
  The corpus document body as an always-on inline editor.

  There is no read/edit toggle: the ProseMirror surface renders with `.tk-prose`
  so it looks exactly like the reader, and the caret drops wherever it is clicked.
  Markdown is the source of truth — tiptap-markdown loads a markdown string and
  serializes back to one. The instance is uncontrolled after mount; the host
  re-keys it (per subject / after a reload) to load fresh content.

  Wiki-links stay navigable inside the editor: a click (or Cmd/Ctrl+click) on a
  resolved `[[ref]]` opens that doc rather than only placing the caret.
*/

import { useEffect, useRef } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import type { Editor } from "@tiptap/core";
import { buildKnowledgeExtensions } from "./editorExtensions";
import type { DocLinkRef } from "./types";

interface MarkdownStorageLike {
  getMarkdown(): string;
}

/** Read the outbound markdown serialization off the tiptap-markdown storage. */
function getMarkdown(editor: Editor): string {
  const storage = editor.storage as { markdown?: MarkdownStorageLike };
  return storage.markdown?.getMarkdown() ?? "";
}

export interface DocBodyEditorProps {
  /** Markdown body loaded once on mount (host re-keys to reload). */
  initialMarkdown: string;
  resolve: (ref: string) => DocLinkRef | undefined;
  /** Fired once after mount with the editor's baseline serialization. */
  onReady: (markdown: string) => void;
  /** Fired with serialized markdown on every edit. */
  onChange: (markdown: string) => void;
  /** Navigate to a resolved wiki-link's subject. */
  onOpenWikilink: (subject: string) => void;
  className?: string;
}

export function DocBodyEditor({
  initialMarkdown,
  resolve,
  onReady,
  onChange,
  onOpenWikilink,
  className,
}: DocBodyEditorProps) {
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;
  const onReadyRef = useRef(onReady);
  onReadyRef.current = onReady;
  const onOpenRef = useRef(onOpenWikilink);
  onOpenRef.current = onOpenWikilink;

  const editor = useEditor({
    editable: true,
    immediatelyRender: false,
    extensions: buildKnowledgeExtensions(resolve),
    content: initialMarkdown,
    editorProps: { attributes: { class: "tk-prose focus:outline-none" } },
    onCreate: ({ editor }) => onReadyRef.current(getMarkdown(editor)),
    onUpdate: ({ editor }) => onChangeRef.current(getMarkdown(editor)),
  });

  // Resolved wiki-link clicks navigate; plain and Cmd/Ctrl+click both open.
  useEffect(() => {
    if (!editor) return;
    const dom = editor.view.dom;
    const onClick = (event: MouseEvent): void => {
      const el = (event.target as HTMLElement | null)?.closest?.(
        "[data-kg-wikilink]",
      ) as HTMLElement | null;
      if (!el || el.getAttribute("data-kg-resolved") !== "true") return;
      const subject = el.getAttribute("data-kg-subject");
      if (!subject) return;
      event.preventDefault();
      onOpenRef.current(subject);
    };
    dom.addEventListener("click", onClick);
    return () => dom.removeEventListener("click", onClick);
  }, [editor]);

  return <EditorContent editor={editor} className={className} />;
}
