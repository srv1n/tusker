/*
  Thin typed accessor over the `tiptap-markdown` storage. The extension patches
  the editor to accept a markdown string as `content` and exposes
  `storage.markdown.getMarkdown()` for the outbound serialization — this wraps
  that so callers don't reach into loosely-typed `editor.storage`.
*/

import type { Editor } from "@tiptap/core";

interface MarkdownStorageLike {
  getMarkdown(): string;
}

export function getMarkdown(editor: Editor): string {
  const storage = editor.storage as { markdown?: MarkdownStorageLike };
  return storage.markdown?.getMarkdown() ?? "";
}
