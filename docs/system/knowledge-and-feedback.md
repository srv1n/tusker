---
title: "Knowledge canon and the feedback loop"
subject: knowledge-and-feedback
keywords: [docs, docgraph, docmap, frontmatter, feedback, signals, canon, improve, logbook, digest, morning brief]
part_of: overview
status: canonical
read_when: "You are writing or validating a doc/spec front matter header, regenerating the docs map, or working anywhere in the feedback pipeline (add/ingest/signals/review/promote/canon), improve scan, logbook, digest, or morning brief."
skip_when: "You need task contracts, proof, gates, or runner orchestration — read [[tasks-and-proof]], [[gates]], or [[orchestration]] instead."
sources:
  - internal/docgraph/docgraph.go
  - internal/docgraph/docmap.go
  - internal/docgraph/search.go
  - cmd/tusker/docs_cmd.go
  - cmd/tusker/v7_domain_cmd.go
  - cmd/tusker/v7_feedback_cmd.go
  - cmd/tusker/v7_feedback_signal.go
  - cmd/tusker/v7_feedback_signals_cmd.go
  - cmd/tusker/v7_feedback_ingest.go
  - cmd/tusker/v7_feedback_review.go
  - cmd/tusker/v7_feedback_promote_plan.go
  - cmd/tusker/v7_feedback_canon.go
  - cmd/tusker/v7_improve_cmd.go
  - cmd/tusker/v7_logbook_cmd.go
  - cmd/tusker/v7_escalation_digest_cmd.go
  - cmd/tusker/morning_brief.go
---

# Knowledge canon and the feedback loop

Two layers, one loop. The **knowledge layer** (`internal/docgraph`, `tusker docs *`,
`tusker domain *`) holds durable truth and mechanically refuses duplicates, orphans,
and stale maps. The **feedback layer** (`tusker feedback *`, `improve`, `logbook`,
`digest`) turns friction observed during work back into tracked work and canon.

## The managed doc corpus

`scanRepository` (`internal/docgraph/docgraph.go:244`) walks exactly two roots and
nothing else: `docs/system/` and `.tusker/specs/`. `INDEX.md` is skipped (it is
generated). Path decides kind (`kindForPath`, docgraph.go:330):

| Path prefix | Kind | Answers |
|---|---|---|
| `docs/system/` | `canonical` | how it works today |
| `.tusker/specs/decisions/` | `decision` | what was said, what got locked |
| `.tusker/specs/` (else) | `spec` | what is changing and why |

Anything outside these roots parses as `DOC_PATH_UNMANAGED`. Front matter must open
on line 1 with `---\n` and close with a bare `---` line; otherwise
`DOC_HEADER_MISSING` / `DOC_HEADER_PARSE_ERROR` (docgraph.go:343).

## Front-matter field reference

Authority for every doc in `docs/system/` and `.tusker/specs/`. Scalar fields are
`fmt.Sprint`-then-trimmed; list fields accept a bare string (coerced to one item) or
a YAML sequence (`scalar`/`list`, docgraph.go:376).

| Field | Type | Required | Used by | Meaning |
|---|---|---|---|---|
| `subject` | scalar | **yes, all docs** | uniqueness, map nodes, edge targets, `docs find`, `docs new` | The unique key. One subject = one document, repo-wide. |
| `part_of` | scalar (a subject) | **yes, except root** | map hierarchy, mermaid edges, `part_of` graph edge | Parent document's `subject`. Root = subject `overview` or path `docs/system/00-overview.md` (`isRoot`, docgraph.go:326). |
| `decides_for` | scalar (a subject) | **yes for `.tusker/specs/decisions/`** | `decides_for` graph edge | The spec this decision log belongs to. |
| `keywords` | list | no | `docs find` ranking (exact > contains) | Search aliases a reader would type instead of the subject. |
| `status` | scalar | no | tombstone rules, map node `status` | `canonical`, or `superseded` (case-insensitive). |
| `superseded_by` | scalar (a subject) | required **iff** `status: superseded` | tombstone validation, `superseded_by` edge, `docs find` forward-resolution | Subject that replaced this one. |
| `updates` | list (subjects) | no | `updates` graph edges | Docs a spec will obsolete. Not validated for existence in `graph.json`. |
| `sources` | list | no | provenance only | Where the content came from. Parsed, never validated. |
| `title` | scalar | no (strongly recommended) | mermaid node label, INDEX description, `docs find` description | Human title. Falls back to `subject`. |
| `read_when` | scalar | no | `docs find` description, after `title` then first body heading (`describe`, search.go:161) | One dense sentence: when to open this. |
| `skip_when` | scalar | no | none mechanically | One dense sentence: when to look elsewhere. |
| `last_verified` | scalar | no | INDEX freshness column | Date string; absent renders `never` (docmap.go:389). |

### Cross-document links

Reference another managed document by its **`subject`**, as an Obsidian wikilink:
`[[tasks-and-proof]]`, `[[skills]]`, `[[cli]]`. The subject is authoritative, not the
filename: in `docs/system/` the two usually coincide, but two docs deliberately differ —
`00-overview.md` is subject `overview`, and `execution-observability.md` is subject
`execution-observability-system`, so `[[execution-observability-system]]` is the correct
link even though no such file exists (Obsidian resolves it through that doc's alias).
Do not use relative markdown links (`../specs/foo.md`) between managed docs —
they break the moment a file moves and carry no subject identity. Code paths are not
documents: keep them as inline code (`cmd/tusker/docs_cmd.go`,
`internal/docgraph/docmap.go`), never as wikilinks.

This is an authoring convention, not a mechanical one: `internal/docgraph` parses
front matter and Markdown headings only, and never resolves `[[...]]` in doc bodies.
The declared graph lives entirely in `part_of` / `updates` / `decides_for` /
`superseded_by`. Wikilinks in vault records (`[[TASK-ID]]`, `![[dashboards/...]]`,
e.g. `cmd/tusker/v7_state_runtime.go:501`) are a separate Obsidian mechanism over
`.tusker/`, unrelated to the doc corpus.

Unknown keys are preserved in `Document.Raw` and ignored. `docs new` scaffolds
`subject`, `keywords`, `part_of`, `status`, `created`, `read_when`, `skip_when`
(`docsScaffold`, docs_cmd.go:158) — add `title` and `sources` yourself.

## Validation rules

`tusker validate` calls `docgraph.ValidateRepository` then `CheckDocsMapFresh`
(`cmd/tusker/commands_index.go:803`). Issues are sorted by path, code, message.

| Code | Trigger |
|---|---|
| `DOC_PATH_UNMANAGED` | File parsed outside `docs/system/` or `.tusker/specs/`. |
| `DOC_HEADER_MISSING` | No opening `---` line. |
| `DOC_HEADER_PARSE_ERROR` | No closing `---`, or YAML that will not unmarshal. |
| `DOC_REQUIRED_FIELD_MISSING` | Empty `subject`; empty `part_of` on a non-root doc; empty `decides_for` on a decision log. |
| `DOC_DUPLICATE_SUBJECT` | Two or more docs declare the same `subject`. Emitted once per participating file, naming the others. |
| `DOC_TOMBSTONE_SUCCESSOR_MISSING` | `status: superseded` with no `superseded_by`. |
| `DOC_TOMBSTONE_SUCCESSOR_NOT_FOUND` | `superseded_by` names a subject no document declares. |
| `DOC_VERSIONED_FILENAME` | Basename ends in `-v2`, `_new`, `-final`, ` (2)`, etc. (`versionedFilename`, docgraph.go:76). Update in place or write an explicit tombstone. |
| `DOCS_MAP_STALE` | A generated artifact is missing or differs from a fresh render. |

`ValidateCorpus` (docgraph.go:186) runs the header + cross-document checks on an
in-memory corpus with no disk access — used by the Serve editor to validate a
speculative edit before writing (`cmd/tusker/serve_docgraph.go:439`). It does **not**
cover parse errors or versioned filenames; those are disk-scan concerns.

## Map generation

`tusker docs map` → `docgraph.WriteDocsMap` (docmap.go:75) regenerates three
artifacts deterministically (docs sorted by subject, so re-running is a no-op):

1. `docs/system/00-overview.md` — a mermaid `graph TD` of `part_of` edges injected
   between `<!-- tusker:docs-map:begin -->` / `:end` markers. Missing markers append
   the region at end of file; mismatched or reversed markers are a hard error.
2. `docs/system/INDEX.md` — fully generated table: Subject | File | Description |
   Freshness. Never hand-edit; it is excluded from the scan.
3. `docs/system/graph.json` — `{nodes:[{subject,kind,path,title,status}],
   edges:[{from,to,kind}]}`, edges sorted by from/kind/to, kinds `part_of`,
   `updates`, `decides_for`, `superseded_by`.

Generation **refuses the whole corpus** and writes nothing when any defect exists
(`findMapDefects`, docmap.go:182):

| Defect code | Trigger |
|---|---|
| `DOCS_MAP_DUPLICATE_SUBJECT` | Same subject twice — no node can be drawn. |
| `DOCS_MAP_ORPHAN` | Non-root doc with empty `part_of`. |
| `DOCS_MAP_DANGLING_EDGE` | `part_of` or `superseded_by` names an undeclared subject. |
| `DOCS_MAP_CYCLE` | `part_of` chain loops (DFS, every member reported once). |

`CheckDocsMapFresh` renders in memory and byte-compares against the committed files;
it returns map defects as issues and short-circuits to no-op when
`docs/system/00-overview.md` does not exist.

## docs commands

- `tusker docs find <query>` — deterministic keyword search, no embeddings
  (`search.go:41`). Terms are lowercased; multi-word queries require **all** terms,
  falling back to best-single-term. Rank: exact subject (5) > keyword exact (4) >
  keyword contains (3) > subject contains (2) > body heading contains (1); ties break
  canonical → spec → decision → path. Scoring short-circuits on the subject
  (`scoreTerm`, search.go:115): once the subject contains the term, keywords and
  headings are never consulted, so a subject-substring hit really scores 2 even when a
  keyword would have matched exactly. Superseded hits resolve forward to their
  successor and report `resolved from`. Zero matches never returns silence: up to 3
  closest subjects by shared-term count then Levenshtein distance. `--json` emits
  `{Query, Matches, Suggestions}`.
- `tusker docs new <subject> [--kind doc|spec]` — refuses if any document already
  declares that subject (case-insensitive) or the slug path exists; writes
  `docs/system/<slug>.md` or `.tusker/specs/<slug>.md`. Duplication is prevented at
  write time, not at review time.
- `tusker docs map` — regenerate the three artifacts.

- `tusker docs adopt [--dry-run] [--json]` — inventory Markdown outside the
  managed tree and emit a fingerprinted `tusker.docs-adopt/v1` proposal table;
  this path is read-only. A human reviews every disposition, sets
  `approved_by: human:<name>`, and applies the exact table with
  `tusker docs adopt --table <file> --approve --by human:<name>`. Apply
  preflights the complete batch before writing. Promote and merge preserve
  legacy sources; tombstone is permitted only as an explicit reviewed row and
  rewrites the source as a `status: superseded` signpost to an existing
  `docs/system/` successor. No disposition deletes a file, and generated map
  artifacts still require an explicit `tusker docs map` run.

All other `docs *` and `knowledge *` verbs are `legacyOnlyCommand` stubs
(`cmd/tusker/cli.go:532-675`), as is `domain graph`.

## Domain canon

Separate from the doc corpus: durable per-domain truth lives in the vault at
`.tusker/knowledge/domains/<id>/` with `INDEX.md` (`schema: tusker.domain/v7`) and
`CANON.md` (`tusker.domain-canon/v7`), both carrying an ordered `capsule`
(summary / use-when / skip-when) and `source_of_truth` (`v7_domain_cmd.go:52-99`).

- `tusker domain new <id>` — one portable path segment, no `/`; creates both files,
  emits a `domain created` event, refreshes the project skill.
- `tusker domain list` / `show <id> [--full] [--json]` — capsule by default.
- `tusker domain canon <id>` — prints the `## Current Truth` section, or the whole
  file with `--full`.

`CANON.md` also carries a `## Canon Entries` section maintained by
`feedback promote --domain` (below).

## Feedback pipeline

### 1. Notes — `tusker feedback add`

Writes `<vault>/feedback/agents/<YYYY-MM-DD>-<actor>-<slug>.md` as labeled bullets
(`v7_feedback_cmd.go:83`). Required: `--context --friction --product-idea --impact
--related`. Optional: `--theme --priority-hint --affected-command --dedupe-key`.

Three write-time guards, each with an explicit escape hatch:

| Guard | Default | Override |
|---|---|---|
| Length budget | 10 content lines / 1600 chars | `--allow-long`, or `feedback.note_max_lines`/`note_max_chars` (also `max_lines`/`max_chars`, or `validation.feedback_note_*`) in config, or `--max-lines`/`--max-chars` |
| Progress-report detector | ≥2 hits among "changed files, tests run, validation, implemented, completed, work log, summary:, next steps, diff, commit" | `--allow-progress-report` |
| Dedupe | same `--dedupe-key` within 14 days | `--allow-duplicate` |

Reading a note back (`parseFeedbackRecord`, v7_feedback_cmd.go:386) accepts YAML
front matter *or* `- label: value` body lines, normalizes aliases
(`normalizeFeedbackFieldName`, :511), and derives `theme`, `priority-hint` (regex
`P0-3`, else keyword heuristics), and `affected-command` (regex over `tusker <verb>`)
when not given. `validateFeedbackNotes` surfaces `FEEDBACK_MISSING_FIELD`,
`FEEDBACK_NOTE_LONG`, `FEEDBACK_PROGRESS_REPORT`, `FEEDBACK_DATE_INVALID/MISSING`,
`FEEDBACK_FRONTMATTER_INVALID` — plus `FEEDBACK_READ_FAILED` /
`FEEDBACK_VALIDATE_FAILED` when a note or the directory cannot be read — through
`tusker validate`.

### 2. Signals — `tusker feedback signals --since <date> [--write]`

A signal is the bounded, machine-derived unit: `tusker.feedback_signal/v1` JSON at
`<vault>/feedback/signals/<date>/<slug>-<hash10>.json` (v7_feedback_signal.go:220).
Fields: `id, date, project, task, attempt, source, category, severity, confidence,
dedupe_key, summary, observed_facts, occurrences, recommendation`.

Enumerations are closed: category ∈ {`review_loop`, `acceptance_quality`,
`token_burn`, `cli_friction`, `closeout_churn`, `workflow_repeat`,
`environment_setup`}; severity ∈ P0–P3 (default P2); confidence ∈ low/medium/high
(default medium).

**Signals must stay summary-safe.** `raw_payload` is forbidden outright
(`FEEDBACK_SIGNAL_RAW_PAYLOAD_FORBIDDEN`); `observed_facts` is required, capped at 16
keys, 180 chars per string, 12 list items, and rejects transcripts/logs/diffs/copied
source (`FEEDBACK_SIGNAL_FACT_KEY_INVALID`, `..._FACT_VALUE_INVALID`). `summary` and
`recommendation` must be one line under 240 chars. `writeFeedbackSignal` validates
before writing — an invalid signal never lands on disk.

Derivation is `deriveFeedbackSignals` (v7_feedback_signal.go:232) over task and event
inputs collected per vault (`v7_feedback_signals_cmd.go:333-455`): acceptance gaps,
review/rework transitions, token burn at or over 80,000 tokens
(`feedbackSignalTokenBurnThreshold`), contract labels.
Emissions are then collapsed by `feedbackSignalCollapseKey`, merging facts and
accumulating `occurrences`. `--write` persists; without it the command only reports.

### 3. Ingest — `tusker feedback ingest`

Lifts hand-written notes into the signal plane: each note becomes a signal plus a
`tusker.feedback_note_import/v1` record so the import is auditable
(`v7_feedback_ingest.go`). Targets resolve from `--vault`, `--repo` (multi), or
registered projects; unregistered/disabled/unhealthy targets produce
`feedbackTargetWarning`s rather than silent omission. Notes carrying any validation
issue are skipped, and each `source_ref` is imported at most once per run. Dry-run
unless `--write`/`--apply`; `--since` is required.

### 4. Review — `tusker feedback review --since <date> [--write]`

Loads persisted signals **and** freshly derived ones, groups them into findings, and
emits a review packet (markdown, or `<vault>/feedback/reviews/<date>.md` with
`--write`). A finding carries `likely_cause`, `recommendation`, `action_type`,
`prevention`, `signal_ids`, `task_ids`, `source_refs`, `frequency`, and splits into
`Actionable` vs `Ignored` (noise). Diagnostics report raw vs collapsed vs skipped
counts and vaults with no explicit notes, so an empty packet is legible.

### 5. Promote — `tusker feedback promote <ref>`

Two distinct modes on one verb (`feedbackPromoteCmd`, v7_feedback_signals_cmd.go:130):

**Work promotion** (default): resolves the ref to a signal, a review finding, or a
note, then plans outcomes. Dry-run unless `--write`/`--apply`. A source is eligible
only when `RepeatCount >= 2` or severity is P0/P1 (`feedbackPromoteEligible`,
promote_plan.go:547); anything else is a `skip` outcome. Target kind is
`OutcomeHint`, else gate / runbook / skill / cli_proposal / task by keyword
(`feedbackPromoteTargetKind`, :511). Sources whose text mentions ambiguity, conflict,
policy, legal, privacy, or security set `NeedsHumanDecision` and route to a decision
record rather than a task (:530). Duplicate detection scans **all** vault notes in
order — dedupe key, then source ref, then normalized title, then related task ID
(`matchFeedbackPromoteDuplicate`, :412) — and emits a `linked` outcome instead of a
second record. Applying writes the task/decision plus a promotion record.

**Canon promotion**: triggered when any of `--candidate --domain --class --sources
--source-notes --lesson --topic --supersedes` is present (`feedbackPromoteCanonRequested`,
v7_feedback_canon.go:315). Requires `--domain` and `--class`
(`prohibition|pattern|preference`) — candidates never auto-promote. Appends an entry
to `## Canon Entries` in that domain's `CANON.md` with id
`lesson-<YYYYMMDD>-<slug>`, `status: current`, source notes/repos, recurrence count,
date span; `--supersedes` marks named entries superseded and hard-fails on an unknown
id. Bumps `updated_at`/`state_rev` and emits a `canon updated` event.

`tusker feedback candidates [--threshold N]` (default 3) is the input: it fingerprints
notes into lesson groups and lists only those recurring at or above the threshold.
Notes carrying validation issues are excluded.

### 6. Digest — `tusker feedback digest --since <date> [--write]`

Rolls raw notes across one or more targets into a dated markdown digest, splitting
`Actionable` (clean) from `Flagged` (has issues). This is the human-readable roll-up
of the note layer, distinct from `tusker digest` (escalation board) below.

## Improve scan

`tusker improve scan` (`v7_improve_cmd.go:94`) answers "what should become a skill,
agent doc, subagent, or automation?" It pulls evidence from a 30-day window
(`--days`/`--since`/`--all`): task summaries, attempt summaries, feedback notes, plus
optional external summary files. It inventories what already exists (skills, agent
docs, subagents, automations), clusters evidence by significant tokens, then per
candidate emits confidence, recommended form, why, and either `WorthCreating` or a
skip reason pointing at existing coverage. Dry-run by default; `--apply` writes agent
docs and `--write`/`--apply` writes the scan report. Cheap by design: default profile
`cheap-discovery`, reasoning `low`.

## Operator surfaces

- **`tusker logbook [--date] [--write] [--json]`** (`v7_logbook_cmd.go:70`) — the
  narrative, PM-readable projection of one day: what shipped, what it means (checks
  passed, defects, repair tasks, evidence links), what needs the human. Pure
  projection over events/tasks/gates/escalations already on disk; no proof
  re-computation, no external calls. `--write` lands `<vault>/logbook/<date>.md`.
- **`tusker digest [--since]`** (`v7_escalation_digest_cmd.go:389`) — the terse
  dev/escalation board: open escalations, landed items, red/parked runs, pending hard
  gates, armed waves, with a watermark so the next run resumes. Pairs with
  `tusker escalate` / `escalate ack`, which dedupe on key, auto-bump severity past a
  staleness threshold (default 4h), and optionally notify.
- **`tusker logbook --scheduled-promotion` (aka `--morning-brief`)**
  (`morning_brief.go:89`) — read-only `tusker.scheduled-promotion-morning-brief/v1`
  projection shared with Serve; `--write` is rejected. Exactly three primary lists —
  landed last night, blocked or repairing, needs your decision — each with an explicit
  empty-state string so "nothing here" is never ambiguous.

## Notes loading

Everything above reads vault markdown through `cmd/tusker/notes.go`: a per-vault
cache keyed by path with mtime/size versioning and a hash re-check for
coarse-mtime filesystems, a frontmatter-only fast path
(`listAllNotesFrontmatter`, `listOperationalNotesFrontmatter`) for commands that
never touch bodies, and defensive deep-copy on load so callers cannot mutate cached
notes. Prefer the frontmatter-only variants and `loadOperationalNotesByPath` when
adding a command; full-body loads over a large vault are the usual latency bug.
