---
subject: documents-cli-discovery-report
title: Documents CLI discovery implementation report
keywords: [documents, CLI, browse, read, backlinks, check]
part_of: documents-experience
status: canonical
created: 2026-09-07
read_when: "Reviewing or verifying the bounded Documents CLI helpers."
skip_when: "Changing the Documents UI or task and runner execution."
---

# Documents CLI discovery implementation report

## Delivered command matrix

| Command | Scope | Bounds and behavior |
| --- | --- | --- |
| `tusker docs browse [<managed-directory>] [--limit N] [--json]` | One real folder level plus direct managed Markdown files | Defaults to 40 entries and caps `N` at 200. Reports `truncated`, `limit`, and `omitted`. Skips generated `INDEX.md`, non-Markdown files, and symlinks. |
| `tusker docs read <subject-or-path> [--section <heading>] [--current] [--json]` | Exactly one document | Resolves through the shared parser/resolver. `--section` returns one exact heading section; `--current` follows supersession. Revision is the SHA-256 of the raw file. |
| `tusker docs backlinks <subject-or-path> [--limit N] [--json]` | Incoming typed metadata and body relationships | Defaults to 40 and caps at 200. Reports `truncated`, `limit`, `omitted`; dangling managed references are returned separately with `broken_truncated`/`broken_omitted` and the stable `DOC_LINK_DANGLING` code. |
| `tusker docs check [<subject-or-path>] [--json]` | Whole corpus or one document | Reuses parser, resolver and repository validation. JSON issues contain `code`, `path`, `message`, and `repair`. It never writes. |
| `tusker docs new <subject> --print [--json]` | Candidate scaffold only | Parses and validates the candidate against the current corpus, then prints it with `written: false`. Ordinary `docs new` remains the only writer and validates before the existing secure write. |

Managed routes are limited to `docs/system` and `.tusker/specs`. Empty,
escaping, unmanaged, missing, non-directory, symlinked, and malformed routes
return stable `DOC_PATH_*` or parser error codes with a next command hint.
Typed-invalid YAML values return `DOC_HEADER_TYPE_INVALID` instead of being
coerced into a subject or relationship.

## JSON examples

Browse is intentionally shallow and compact:

```json
{
  "schema": "tusker.docs-browse/v1",
  "path": "docs/system/",
  "entries": [
    {"path":"docs/system/architecture/","name":"architecture","kind":"folder","children_known":true,"summary":"Architecture"},
    {"path":"docs/system/cli.md","name":"cli.md","kind":"file","subject":"cli","title":"CLI reference","document_kind":"canonical","status":"canonical","read_when":"Looking up an exact command"}
  ],
  "truncated": true,
  "limit": 2,
  "omitted": 3
}
```

Read returns one parsed document and only its direct relationship summary:

```json
{
  "schema":"tusker.docs-read/v1",
  "subject":"documents-experience",
  "path":".tusker/specs/documents-experience.md",
  "kind":"spec",
  "status":"canonical",
  "revision":"<64 lowercase hex characters>",
  "header":{"subject":"documents-experience","part_of":"work-knowledge-and-retention"},
  "body":"...",
  "links":[{"ref":"work-knowledge-and-retention","subject":"work-knowledge-and-retention","resolved":true}],
  "backlinks":[],
  "backlinks_truncated":false,
  "backlinks_omitted":0
}
```

Backlinks preserve relationship provenance:

```json
{
  "schema":"tusker.docs-backlinks/v1",
  "subject":"target",
  "backlinks":[{"subject":"guide","path":"docs/system/guide.md","kind":"canonical","via":"link","typed":false}],
  "broken":[{"from":"guide","path":"docs/system/guide.md","kind":"link","ref":"missing"}],
  "truncated":false,
  "limit":40,
  "omitted":0,
  "broken_truncated":false,
  "broken_omitted":0
}
```

## Fixtures and evidence

The focused fixtures cover a nested folder, folder summary, deterministic
folder/file ordering, direct metadata, long-lived section selection with nested
headings and fenced examples, subject and path reads, superseded-to-current
resolution, typed and body backlinks, dangling relative Markdown routes,
duplicate subjects, typed-invalid front matter, bounded output, path escape
rejection, symlinked documents and folder summaries, stable symlink errors from
read/backlinks/targeted-check, and non-destructive scaffold output.

## Measured proof

| Check | Host / revision | Result |
| --- | --- | --- |
| `go test ./internal/docgraph -run '^(TestBrowse\|TestReadSection\|TestBacklinks\|TestDocsMetadata)$' -count=1 -v` | macOS arm64, Go 1.26.5, `f9d2b2b1a28ce3a568373882b71c1d49c22a02c1` | PASS |
| `go test ./internal/docgraph -count=1` | macOS arm64, Go 1.26.5, `f9d2b2b1a28ce3a568373882b71c1d49c22a02c1` | PASS |
| `go test ./cmd/tusker -run '^(TestDocsBrowse\|TestDocsRead\|TestDocsBacklinks\|TestDocsCheck\|TestDocsScaffold\|TestDocsCommandRoutes)$' -count=1 -v` | macOS arm64, Go 1.26.5, `f9d2b2b1a28ce3a568373882b71c1d49c22a02c1` | PASS |
| `go test ./cmd/tusker -run '^(TestDocsFindOutputIncludesDiscoveryGuidance\|TestValidateFlagsStaleDocsMap\|TestValidationIssueScoping)$' -count=1 -v` | macOS arm64, Go 1.26.5, `f9d2b2b1a28ce3a568373882b71c1d49c22a02c1` | PASS |
| `go run ./cmd/tusker docs map --json` | macOS arm64, Go 1.26.5, `f9d2b2b1a28ce3a568373882b71c1d49c22a02c1` | PASS; generated map refreshed through the existing writer |
| `git diff --check` | macOS arm64, Go 1.26.5, `f9d2b2b1a28ce3a568373882b71c1d49c22a02c1` | PASS |

The task's literal compatibility selector
`^(TestDocsFind\|TestDocsMap\|TestDocsValidation)$` matched no tests in the
current package. The nearest existing compatibility tests above ran and
passed; this is recorded instead of presenting a no-test run as proof.

Read commands were checked against file bytes before and after invocation.
The command fixtures passed with byte-identical files. `docs new --print`
returned `written:false` and no file; the ordinary scaffold path was separately
verified to create a validated file.

An invalid check prints the structured report and exits with status 1. The
dispatcher consumes the internal check sentinel so JSON mode emits one report
line rather than a successful report followed by a second generic error.

## First actionable failure

There was no failure in the final owned focused checks. Early attempts were
temporarily blocked by unrelated concurrent `demo_*.go` compile errors and a
repository validation lock held by another test process. After those processes
finished, the exact focused commands above passed without a lock override. No
demo files were edited by this delivery.
