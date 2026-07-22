/*
  Code block extension — one `codeBlock` node that both syntax-highlights and
  pre-renders mermaid.

  It is CodeBlockLowlight (so non-mermaid fences get lowlight's `hljs-*` token
  decorations) extended with a *conditional* React NodeView: a block whose
  `language` is `mermaid` renders through `MermaidView` (a diagram); every other
  block returns `null` from the node-view factory, which ProseMirror reads as
  "use the default rendering" — i.e. the normal `<pre><code>` that the inherited
  Lowlight plugin decorates.

  Crucially the node keeps the name `codeBlock` and its `language` attribute, so
  `tiptap-markdown`'s default code-block serializer (keyed by node name) still
  emits a byte-stable ```<language> fence for BOTH mermaid and highlighted code.
  Mermaid rendering is a view concern only; the document model is unchanged, so
  the markdown round-trip needs no custom serializer here.
*/

import { CodeBlockLowlight } from "@tiptap/extension-code-block-lowlight";
import { ReactNodeViewRenderer } from "@tiptap/react";
import { createLowlight } from "lowlight";
import type { NodeViewRenderer } from "@tiptap/core";
import { MermaidView } from "./nodeviews/MermaidView";

import bash from "highlight.js/lib/languages/bash";
import diff from "highlight.js/lib/languages/diff";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import markdown from "highlight.js/lib/languages/markdown";
import python from "highlight.js/lib/languages/python";
import rust from "highlight.js/lib/languages/rust";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import yaml from "highlight.js/lib/languages/yaml";

/** Only the grammars this app's docs actually use. `common` pulls ~35 grammars
 *  into the editor chunk; registering an explicit set keeps that chunk lean.
 *  Unknown languages still render as plain (unhighlighted) code. */
const lowlight = createLowlight({
  bash,
  diff,
  go,
  javascript,
  json,
  markdown,
  python,
  rust,
  sql,
  swift,
  typescript,
  yaml,
});

export const CodeBlockWithMermaid = CodeBlockLowlight.extend({
  addNodeView() {
    // Created once; reused for every mermaid block this editor renders.
    const mermaidNodeView = ReactNodeViewRenderer(MermaidView, {
      // `selected` should also be true when the caret is merely *inside* the
      // block (a TextSelection), not only on a NodeSelection — that's how the
      // source face reveals itself while editing.
      selectedOnTextSelection: true,
    });

    const renderer: NodeViewRenderer = (props) => {
      const language = String(
        (props.node.attrs as { language?: string | null }).language ?? "",
      )
        .trim()
        .toLowerCase();

      if (language === "mermaid") {
        return mermaidNodeView(props);
      }

      // Fall back to default rendering + Lowlight decorations. ProseMirror
      // treats a falsy node-view result as "render this node the default way",
      // so returning `null` is exactly what keeps highlighted code blocks on
      // the standard path (see prosemirror-view NodeViewDesc.create).
      return null as unknown as ReturnType<NodeViewRenderer>;
    };

    return renderer;
  },
}).configure({ lowlight });
