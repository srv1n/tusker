/*
  DocEditor — one TipTap surface for both reading and editing a markdown body.

  Inline WYSIWYG: the rendered doc IS the editor; `editable` flips read↔edit in
  place. Markdown is the source of truth (via tiptap-markdown), so the host feeds
  a markdown string in and gets markdown back through `onChange`. The instance is
  uncontrolled after mount — the host re-keys it at save/cancel boundaries to
  reset content, which keeps typing smooth (no per-keystroke reconciliation).

  Wiki-link clicks navigate only in read mode; while editing, a click places the
  caret so the atom can be selected/deleted.
*/

import { useEffect, useRef } from "react";
import { EditorContent, useEditor } from "@tiptap/react";
import { buildExtensions } from "./extensions";
import { getMarkdown } from "./markdown";
import type { EditorRuntimeConfig } from "./types";

export interface DocEditorProps {
  /** Markdown to load once, on mount (host re-keys to reload). */
  initialMarkdown: string;
  editable: boolean;
  config: EditorRuntimeConfig;
  /** Fires with serialized markdown on every edit. */
  onChange?: (markdown: string) => void;
  className?: string;
}

export function DocEditor({
  initialMarkdown,
  editable,
  config,
  onChange,
  className,
}: DocEditorProps) {
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  const editor = useEditor({
    editable,
    immediatelyRender: false,
    extensions: buildExtensions(config),
    content: initialMarkdown,
    editorProps: {
      attributes: { class: "tk-prose focus:outline-none" },
    },
    onUpdate: ({ editor }) => onChangeRef.current?.(getMarkdown(editor)),
  });

  useEffect(() => {
    editor?.setEditable(editable);
  }, [editable, editor]);

  useEffect(() => {
    if (!editor) return;
    const dom = editor.view.dom;
    const onClick = (event: MouseEvent) => {
      if (editor.isEditable) return; // editing: let the click place the caret
      const el = (event.target as HTMLElement | null)?.closest?.(
        '[data-ofm-kind="wikilink"]',
      ) as HTMLElement | null;
      if (!el) return;
      event.preventDefault();
      const target = el.getAttribute("data-ofm-target") || "";
      const anchor = el.getAttribute("data-ofm-anchor") || undefined;
      config.onOpenWikilink?.({
        target,
        anchor,
        resolved: config.resolveWikilink(target),
      });
    };
    dom.addEventListener("click", onClick);
    return () => dom.removeEventListener("click", onClick);
  }, [editor, config]);

  return <EditorContent editor={editor} className={className} />;
}
