/*
  Obsidian-Flavored Markdown — the pure token layer.

  A TypeScript port of the platform-neutral core of the BrainDump editor
  (`cinta/TipTap/src/ofm.js`). Only the pure parse/serialize/tokenize functions
  are ported; cinta's DOM-bridge preprocess/serialize pipeline (which existed to
  talk to a WKWebView) is replaced on the web by markdown-it rules
  (`./markdownItOfm.ts`) feeding TipTap nodes.

  Grammar handled here:
    [[target]]              wiki-link
    [[target#heading]]      heading anchor
    [[target#^block]]       block anchor
    [[target|alias]]        display alias
    ![[target]]             embed (kind inferred from extension)
    ==text==                highlight
*/

const IMAGE_EXTENSIONS = new Set([
  "png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "heic", "heif", "avif",
]);
const PDF_EXTENSIONS = new Set(["pdf"]);
const AUDIO_EXTENSIONS = new Set(["mp3", "m4a", "wav", "aac", "ogg", "flac"]);
const VIDEO_EXTENSIONS = new Set(["mp4", "mov", "m4v", "webm", "avi", "mkv"]);
const BASE_EXTENSIONS = new Set(["base"]);

/** Matches, in priority order, an embed, a wiki-link, or a highlight run. */
export const INLINE_TOKEN_REGEX =
  /!\[\[[^\]\n]+?\]\]|\[\[[^\]\n]+?\]\]|==[^=\n](?:[^=\n]|=(?!=))*==/g;

const OFM_MARKDOWN_ESCAPE_REGEX = /\\([\\`*_{}[\]()#+\-.!|~])/g;

export type OFMEmbedKind =
  | "image"
  | "pdf"
  | "audio"
  | "video"
  | "base"
  | "note"
  | "";

export interface OFMReference {
  /** Canonical `[[…]]` / `![[…]]` token text. */
  raw: string;
  isEmbed: boolean;
  target: string;
  alias: string;
  anchor: string;
  /** Set when the anchor is a `#heading` (not a block ref). */
  heading: string;
  /** Set when the anchor is a `#^block` ref (the id without the caret). */
  block: string;
  embedKind: OFMEmbedKind;
  /** What a reader should see: alias › target › inner. */
  displayText: string;
}

function trimString(value: unknown): string {
  return value == null ? "" : String(value).trim();
}

function unescapeOFMTokenPart(value: unknown): string {
  return String(value || "").replace(OFM_MARKDOWN_ESCAPE_REGEX, "$1");
}

/** Rebuild the canonical token text from its parts. */
export function buildOFMToken(
  isEmbed: boolean,
  target: string,
  anchor: string,
  alias: string,
): string {
  const cleanTarget = trimString(unescapeOFMTokenPart(target));
  const cleanAnchor = trimString(unescapeOFMTokenPart(anchor));
  const cleanAlias = trimString(unescapeOFMTokenPart(alias));
  const targetWithAnchor = cleanAnchor ? `${cleanTarget}#${cleanAnchor}` : cleanTarget;
  const option = cleanAlias ? `|${cleanAlias}` : "";
  return `${isEmbed ? "!" : ""}[[${targetWithAnchor}${option}]]`;
}

export function inferEmbedKind(target: string): OFMEmbedKind {
  const cleaned = trimString(target).split(/[?#]/, 1)[0] ?? "";
  const ext = cleaned.includes(".") ? (cleaned.split(".").pop() ?? "").toLowerCase() : "";

  if (IMAGE_EXTENSIONS.has(ext)) return "image";
  if (PDF_EXTENSIONS.has(ext)) return "pdf";
  if (AUDIO_EXTENSIONS.has(ext)) return "audio";
  if (VIDEO_EXTENSIONS.has(ext)) return "video";
  if (BASE_EXTENSIONS.has(ext)) return "base";
  return "note";
}

/** Parse a single `[[…]]` / `![[…]]` token into its structured reference. */
export function parseOFMReference(token: string): OFMReference {
  const raw = trimString(token);
  const isEmbed = raw.startsWith("![[");
  const inner = raw.replace(/^!?\[\[/, "").replace(/\]\]$/, "").trim();

  let targetAndAnchor = inner;
  let alias = "";
  const pipeIndex = inner.indexOf("|");
  if (pipeIndex >= 0) {
    targetAndAnchor = inner.slice(0, pipeIndex).trim();
    alias = unescapeOFMTokenPart(inner.slice(pipeIndex + 1).trim());
  }

  let target = targetAndAnchor;
  let anchor = "";
  const hashIndex = targetAndAnchor.indexOf("#");
  if (hashIndex >= 0) {
    target = unescapeOFMTokenPart(targetAndAnchor.slice(0, hashIndex).trim());
    anchor = unescapeOFMTokenPart(targetAndAnchor.slice(hashIndex + 1).trim());
  } else {
    target = unescapeOFMTokenPart(targetAndAnchor.trim());
  }

  const block = anchor.startsWith("^") ? anchor.slice(1) : "";
  const heading = anchor && !block ? anchor : "";
  const embedKind = isEmbed ? inferEmbedKind(target) : "";
  const displayText = alias || target || inner;
  const canonicalRaw = buildOFMToken(isEmbed, target, anchor, alias);

  return {
    raw: canonicalRaw,
    isEmbed,
    target,
    alias,
    anchor,
    heading,
    block,
    embedKind,
    displayText,
  };
}

export type OFMInlinePart =
  | { type: "text"; value: string }
  | { type: "wikilink"; value: string; attrs: OFMReference }
  | { type: "embed"; value: string; attrs: OFMReference }
  | { type: "highlight"; value: string };

/** Split a run of text into OFM tokens and the plain text between them. */
export function tokenizeOFMInline(text: string): OFMInlinePart[] {
  const input = String(text || "");
  const parts: OFMInlinePart[] = [];
  let cursor = 0;

  input.replace(INLINE_TOKEN_REGEX, (match, offset: number) => {
    if (offset > cursor) {
      parts.push({ type: "text", value: input.slice(cursor, offset) });
    }

    if (match.startsWith("![[")) {
      parts.push({ type: "embed", value: match, attrs: parseOFMReference(match) });
    } else if (match.startsWith("[[")) {
      parts.push({ type: "wikilink", value: match, attrs: parseOFMReference(match) });
    } else if (match.startsWith("==") && match.endsWith("==")) {
      parts.push({ type: "highlight", value: match.slice(2, -2) });
    } else {
      parts.push({ type: "text", value: match });
    }

    cursor = offset + match.length;
    return match;
  });

  if (cursor < input.length) {
    parts.push({ type: "text", value: input.slice(cursor) });
  }

  return parts.length ? parts : [{ type: "text", value: input }];
}
