/*
  MermaidView — the React NodeView for a ```mermaid code block.

  Registered only for `codeBlock` nodes whose `language` is `mermaid` (see
  `../codeblock.ts`); every other code block falls through to CodeBlockLowlight's
  default rendering. Because the node stays a plain `codeBlock`, the markdown
  round-trip is untouched — this file only changes how the block is *displayed*,
  never the document model (so `tiptap-markdown` still serializes it back to a
  verbatim ```mermaid fence).

  WYSIWYG, mirroring cinta's BrainDump UX:
    - read mode (editor not editable) → always the rendered diagram;
    - edit mode, block not selected → the diagram (click / Enter to edit);
    - edit mode, cursor inside the block → the editable source (a real
      ProseMirror `NodeViewContent`, so typing/undo/IME all behave normally).
  The diagram is (re)rendered on mount, on leaving the source (blur), and on
  theme change — never on every keystroke (while editing we show the source and
  skip rendering entirely). Renders are cached in `../mermaid`, so redisplay is
  free. Nothing here throws: a render failure shows the raw source + a note.
*/

import { useEffect, useRef, useState } from "react";
import type { FC } from "react";
import {
  NodeViewContent,
  NodeViewWrapper,
  useEditorState,
} from "@tiptap/react";
import type { ReactNodeViewProps } from "@tiptap/react";
import { renderMermaid, subscribeMermaidTheme } from "../mermaid";

// `NodeViewContent`'s `as` prop is typed `NoInfer<T>` (T defaults to `"div"`),
// so a literal `as="code"` will not type-check without a generic type argument.
// Cast to a plain FC that accepts an `as` string — this keeps the JSX below
// vanilla (no `<Component<T>>` syntax) so every downstream transformer, not just
// tsc/esbuild, handles it. Runtime behavior is unchanged.
const CodeContent = NodeViewContent as unknown as FC<{
  as?: string;
  className?: string;
}>;

type DiagramState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "ready"; svg: string }
  | { status: "error"; message: string };

export function MermaidView({
  node,
  editor,
  selected,
  getPos,
}: ReactNodeViewProps) {
  const source = node.textContent;

  const editable = useEditorState({
    editor,
    selector: (snapshot) => snapshot.editor.isEditable,
  });
  // Show the editable source only while editing AND the cursor is in the block.
  const editing = !!editable && selected;

  const [diagram, setDiagram] = useState<DiagramState>({ status: "idle" });
  const [themeTick, setThemeTick] = useState(0);
  const firstPaint = useRef(true);

  // Re-render the diagram when the app theme flips (fresh token colors).
  useEffect(() => subscribeMermaidTheme(() => setThemeTick((t) => t + 1)), []);

  // Render the diagram whenever it is the visible face. While editing we show
  // the source, so we deliberately skip rendering there (keeps typing off the
  // render path). Debounced, except the first paint which is immediate.
  useEffect(() => {
    if (editing) return;

    const trimmed = source.trim();
    if (!trimmed) {
      setDiagram({ status: "idle" });
      return;
    }

    let active = true;
    // Keep any existing diagram on screen while a fresh one renders (no flash).
    setDiagram((prev) => (prev.status === "ready" ? prev : { status: "loading" }));

    const delay = firstPaint.current ? 0 : 180;
    firstPaint.current = false;

    const timer = window.setTimeout(() => {
      renderMermaid(trimmed)
        .then((result) => {
          if (!active) return;
          if (result.svg) setDiagram({ status: "ready", svg: result.svg });
          else
            setDiagram({
              status: "error",
              message: result.error ?? "render failed",
            });
        })
        .catch((error: unknown) => {
          if (!active) return;
          setDiagram({
            status: "error",
            message: error instanceof Error ? error.message : String(error),
          });
        });
    }, delay);

    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [source, editing, themeTick]);

  const enterEdit = () => {
    if (!editable) return;
    const pos = typeof getPos === "function" ? getPos() : undefined;
    if (typeof pos !== "number") return;
    // Place the caret inside the code block; `selectedOnTextSelection` then
    // flips `selected` → the source face appears with the cursor in it.
    editor.chain().focus().setTextSelection(pos + 1).run();
  };

  const onDiagramKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      enterEdit();
    }
  };

  return (
    <NodeViewWrapper
      as="div"
      className="tk-mermaid-nv"
      data-mermaid-editing={editing ? "true" : undefined}
    >
      {/* Source of truth: the ProseMirror-managed code. Always mounted so PM
          keeps the content model intact; only visible while editing. */}
      <pre
        className="tk-mermaid-source"
        data-language="mermaid"
        style={{ display: editing ? "block" : "none" }}
      >
        <CodeContent as="code" />
      </pre>

      {/* Rendered face: diagram, error+raw, or a brief loading note. */}
      {!editing && (
        <div
          className="tk-mermaid"
          contentEditable={false}
          data-status={diagram.status}
          role={editable ? "button" : undefined}
          tabIndex={editable ? 0 : undefined}
          aria-label={
            editable ? "Mermaid diagram. Activate to edit source." : "Mermaid diagram"
          }
          onClick={editable ? enterEdit : undefined}
          onKeyDown={editable ? onDiagramKeyDown : undefined}
        >
          {diagram.status === "ready" ? (
            <div
              className="tk-mermaid-svg"
              // Sanitized in `renderMermaid` via `sanitizeMermaidSvg`.
              dangerouslySetInnerHTML={{ __html: diagram.svg }}
            />
          ) : diagram.status === "error" ? (
            <div className="tk-mermaid-fallback">
              <pre className="tk-mermaid-raw">{source}</pre>
              <div className="tk-mermaid-note">Mermaid: {diagram.message}</div>
            </div>
          ) : diagram.status === "loading" ? (
            <div className="tk-mermaid-loading">Rendering diagram…</div>
          ) : (
            <pre className="tk-mermaid-raw">{source}</pre>
          )}
        </div>
      )}
    </NodeViewWrapper>
  );
}
