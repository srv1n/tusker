---
subject: documents-experience
title: Documents experience and bounded CLI discovery
keywords: [documents, knowledge, docgraph, front matter, Mermaid, documentation CLI]
part_of: work-knowledge-and-retention
describes: [internal/serve/ui/src/features/knowledge, internal/docgraph, cmd/tusker/docs_cmd.go]
status: canonical
created: 2026-09-07
read_when: "Designing or implementing the Documents tree, reader, graph, and agent-facing documentation helpers."
skip_when: "Implementing Work screens, model routing, repeatable testing, or the older trust-roadmap contracts."
sources: [work-knowledge-and-retention.md, knowledge-and-feedback, tusker-trust-and-efficiency.md]
decisions_locked: false
capsule:
  what: "Polish the existing file-and-folder Documents experience and add bounded, structured CLI discovery without duplicating the docgraph."
  use_when: "Assigning or reviewing Documents UI and CLI work."
  skip_when: "Changing runner execution, task orchestration, or model settings."
---

# Documents experience and bounded CLI discovery

## Why this exists

Documents is the place a person returns to months later to recover how Tusker
works and why it works that way. The current file-and-folder layout is already
the correct mental model: a visible tree of real paths, clickable files, a
readable document, and an optional graph. The work here makes that experience
quieter and more useful while giving agents a small CLI route to the same
answers.

This is a focused delta. It does not replace the older trust-roadmap contracts
for document routing or graph consistency ([[tusker-trust-and-efficiency]]).
It supplies the immediate UI polish and bounded helpers needed by the current
product. Existing resolver and editor code remains the source of truth.

## Product behavior

### The Documents screen

- Keep the real folder/file tree. Do not migrate to invented topic folders or
  require a corpus rewrite.
- Preserve folder expansion, selected-file highlighting, auto-expansion of a
  selected file's ancestors, filtering by title/subject/filename, and the
  Files/Graph switch.
- Keep the tree usable with keyboard only: folders are controls, files are
  links, focus is visible, and the selected file is announced.
- On small screens, keep the explorer as an off-canvas rail with an explicit
  toggle. Opening a file closes the rail; deep links still open the right file.
- Keep long names discoverable with a tooltip or accessible full-name label.
  Truncation must not make two documents indistinguishable.

### Reading a document

- Put title, kind and current status first. A reader should understand what the
  page is before seeing implementation metadata.
- Keep the document body wide enough for comfortable reading and preserve the
  existing outline, internal links, backlinks, supersession notice and source
  view.
- Render supported Mermaid fenced blocks in reading mode with the existing
  renderer. A rendering error must leave the original source available and
  explain that rendering failed; it must not silently disappear or display an
  untrusted HTML fragment.
- Keep editing available through the existing CAS-backed knowledge editor.
  Do not create a second editor or a second save protocol.
- Collapse editable front matter behind a compact `Details` surface while
  reading. The values remain available and editable; routine readers do not
  pay the visual cost of a large metadata card.
- Keep provenance useful: path, last verification, status, and links to
  related decisions belong in the details or related-links area. Do not show
  raw YAML by default.

### Searching and routing

`docs find` remains the fast path for a known question. Its result is bounded,
ranked, and includes `read_when`/`skip_when` guidance. The new browse helper
handles a directory level; it must not dump the whole corpus into an agent
prompt. A search hit may jump straight to a document when hierarchy is not
needed.

Current answers and their rationale stay separate but linked: the current
document explains behavior and authority; a decision document preserves the
alternatives, evidence and choice. Superseded documents resolve forward while
remaining visible as history.

## CLI contract

The CLI is both helper and enforcer. It should return small structured answers,
stable error codes, and a next action. Human-readable output is for a person;
`--json` is the agent contract. All read operations are side-effect free.

The exact command spellings below are the proposed additions to the existing
`docs` command family. Implementers must first confirm parser conventions in
`cmd/tusker/docs_cmd.go`; if a name is already taken, preserve the behavior and
choose the smallest compatible spelling. Do not add a new dispatcher or
framework.

### `tusker docs browse <path>`

Returns only one directory level, with folders and direct managed Markdown
files. Default output is bounded to 40 children; `--limit N` is explicit and
must be capped by a safe maximum. Each entry contains:

```json
{
  "path": "docs/system/",
  "kind": "folder",
  "name": "system",
  "children_known": true,
  "summary": "Current system behavior and operator guidance"
}
```

For files, include `subject`, `title`, `kind`, `status`, `read_when`,
`skip_when`, `part_of`, and `path`. Include `truncated`, `limit`, and the
omitted count when bounded. A missing path, path escape, malformed header or
unknown managed root returns a stable code and a useful next command.

### `tusker docs read <subject-or-path>`

Reads exactly one managed document. Default output is the title, path, kind,
status, discovery metadata, body and a compact relationship summary. `--json`
returns the parsed header, body, links, backlinks, successor and revision
already used by the docgraph API. `--section <heading>` returns only one exact
Markdown heading section plus its heading; missing sections fail with the
available headings, never with the whole document.

The command follows supersession only when `--current` is supplied. Without it,
the caller can inspect historical text and see its successor. No command may
invent summaries, verification dates, links or decision rationale.

### `tusker docs backlinks <subject-or-path>`

Returns the bounded set of incoming relationships from the shared resolver.
Each row contains source subject/path, relationship kind, and whether it is a
typed metadata edge or a body link. `--limit` is bounded and reports
truncation. Broken references are reported separately with path and stable
issue code.

### `tusker docs check [<subject-or-path>]`

Runs header, duplicate-subject, supersession and managed-link validation. With
an argument it reports only the selected document plus relationships that it
declares; without one it reports the corpus but remains compact. `--json`
returns named issues with `code`, `path`, `message`, and a suggested repair.
This is the validation primitive used by authoring helpers and must share the
same parser and resolver as `docs find`, `docs map`, and the server.

### `tusker docs new` and safe metadata assistance

Keep `docs new` as the single scaffold writer. Improve its scaffold and add a
non-destructive `--template`/`--print` mode only if the current argument parser
supports it without a second authoring path. A helper may show missing
recommended fields or print a patch-like suggestion, but it must never
overwrite handwritten front matter, manufacture `last_verified`, or turn a
placeholder into proof. Any write validates the proposed corpus before the
atomic write and uses the existing secure writer.

## Shared contracts and implementation boundaries

The UI and CLI must consume these existing primitives:

| Concern | Existing source to reuse | New behavior |
| --- | --- | --- |
| Parse and normalize headers | `internal/docgraph` | Expose bounded metadata without reparsing in each consumer |
| Subject/path/supersession resolution | `internal/docgraph/resolver.go` | Same canonical target in CLI, UI and server |
| Search ranking and guidance | `internal/docgraph/search.go` | Preserve bounded result and discovery metadata |
| Generated map | `internal/docgraph/docmap.go` | Keep `docs map` as the only generated-map writer |
| UI document tree | `features/knowledge/tree.ts`, `KnowledgeTree.tsx` | Polish existing tree and preserve path identity |
| Reader/editor | `KnowledgeReader.tsx`, `DocBodyEditor.tsx`, existing editor extensions | Details disclosure and safe Mermaid reading fallback |
| CAS editing | `/api/docgraph/doc` and `useDocgraphEditor` | No second save or conflict protocol |

The CLI slice may extend `cmd/tusker/docs_cmd.go` and `internal/docgraph`.
The model/execution stream owns `cmd/tusker/cli.go`, capabilities, runner
profiles, serve model APIs and settings APIs. The Documents work must not touch
those files. The UI slice owns the existing `features/knowledge/` files and
their focused tests/reports; it must not change task routing or runner state.

## Non-goals

- No embedded agent chat, new harness or provider installation.
- No automatic wave starts or orchestration changes.
- No new document database, graph service, search dependency or editor
  framework.
- No mandatory topic-folder migration; semantic `subject` and `part_of` remain
  useful metadata alongside physical paths.
- No automatic mass verification timestamps or generated decision rationale.
- No retention-policy implementation in these two slices; the approved
  seven-day disposable-output policy remains in [[work-knowledge-and-retention]]
  for a later backend-owned change.

## Evidence required

Both slices must provide focused behavioral tests and a short report with
command, revision, host, PASS/FAIL and first actionable failure. UI proof also
includes 1440x1000, 1024x768 and 390x844 screenshots, keyboard traversal, long
names, empty/loading/error states, Mermaid success/failure, and a fresh
screenshot-only critic. CLI proof includes a temporary corpus with nested
folders, long names, duplicate subjects, a superseded document, a broken link,
and a large synthetic corpus proving bounds. Output must show that `--json`
is deterministic and that read commands write nothing.

## Deferred (not now)

- Human outcome-review inbox and artifact retention scheduler.
- A polished visual theme beyond this Documents surface.
- Cross-device synchronization of tree collapse state.
- A full knowledge graph as the default navigation surface.
- Measured token-budget optimization after the repeatable testing stream
  establishes a baseline.

<!-- tusker:delivery-import:fd0f9d66b7b7fdae:begin -->

- `[[WUX-T-0014]]` implements delivery source `cli-discovery`.
- `[[WUX-T-0013]]` implements delivery source `ui-polish`.

- `[[W-0015]]` is the imported delivery wave.

<!-- tusker:delivery-import:fd0f9d66b7b7fdae:end -->
