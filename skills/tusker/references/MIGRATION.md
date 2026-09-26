# Migrating a Repository to the Canonical Layout

`docs/system/` is the only managed documentation root. Tusker does not
resolve `spec_refs`, links, or domain canon from `docs/design/`, `specs/`,
`rfcs/`, wiki exports, or legacy `.tusker/specs/`. A repository that keeps
product knowledge anywhere else migrates it; it does not configure extra
roots or add pointer documents.

Use this reference when a repository has product specs, design docs, ADRs,
or decision logs outside `docs/system/`, or when `spec_refs` fail with
`missing target` or `wrong kind`.

## Target layout

| Content | Destination | `kind` |
|---|---|---|
| Current behavior, architecture, reference chapters | `docs/system/` or `docs/system/domains/<domain>/` | `doc` |
| Change specifications, RFCs, design proposals | `docs/system/proposals/` | `proposal` |
| Settled product decisions, ADRs | `docs/system/decisions/` | `decision` |
| Tracker state, thin pointers | `.tusker/` | none |

`spec_refs` resolve only to `proposal` and `decision` documents (and legacy
`spec`). A migrated design doc left at the default `kind: doc` still fails
with `wrong kind`. Set the kind in the adoption table.

## Procedure

1. Inventory. Run `git status --short` and stop if files outside `.tusker/`
   are dirty. List every Markdown tree holding product knowledge. Record
   which documents active tasks reference: `tusker list --type task --json`
   and each task's `spec_refs`.
2. Preview. Run `tusker docs adopt --migration --dry-run --json > adopt.json`.
   Nothing is written.
3. Edit each row of `adopt.json`:
   - `target`: the destination from the table above. Rename to a stable
     slug; keep one subject per file.
   - `kind`: `proposal` for specs and RFCs, `decision` for ADRs, `doc` for
     current-behavior chapters.
   - `lifecycle`: `proposed`, `accepted`, `implemented`, or `superseded`
     for proposals; `current` or `superseded` for docs.
   - `conformance`: `unverified` unless you checked the code.
     `implemented`, `matches`, and `drift` need `evidence`.
   - `disposition`: `promote` to move into place, `merge` when a canonical
     owner already exists, `tombstone` to turn the source into a
     superseded signpost, `leave` for files that are not product knowledge
     (changelogs, vendored docs, READMEs for code).
   Do not change `fingerprint` or `source_fingerprint`.
4. Get approval. Show the edited table to the owner. Set `approved_by`.
   Apply with
   `tusker docs adopt --table adopt.json --approve --by human:<name> --json`,
   or the `user-session:<id>` path in an interactive session. Every row is
   preflighted before any write; conflicts are refused with exact paths.
5. Repoint work. For each task whose `spec_refs` name an old path, run
   `tusker show <TASK-ID> --json` to read `state_rev`, then
   `tusker task update <TASK-ID> --if-revision <state_rev> --spec-refs <new-paths> --by <actor>`.
   Repoint epic `spec_refs` the same way.
6. Retire the old tree. After review, tombstone or delete the originals in
   a separate commit. Promote keeps sources, so skipping this step leaves
   two copies. Delete nothing without owner approval.
7. Verify:

   ```sh
   tusker docs check
   tusker docs map
   tusker validate --json
   ```

   Every `spec_ref` resolves, no `DOC_LINK_DANGLING` or
   `SPEC_REF_DANGLING` remains, and no pointer documents are left in
   `docs/system/`.

## Rules

- Never write a pointer document in `docs/system/` to make an outside file
  resolve. Migrate the file.
- Migration is a docs change. Land it on its own before authoring waves
  that reference the migrated specs.
- Keep code-adjacent READMEs where they are. Only product knowledge moves.
- A repository with a large tree may migrate in batches, one domain per
  approved table. Unmigrated documents stay unreferenceable until moved.

## Prompt for an agent

Paste this into a session in the repository being migrated:

> Migrate this repository's product documentation to Tusker's canonical
> layout, following `skills/tusker/references/MIGRATION.md` from the Tusker
> skill. Inventory every Markdown tree outside `docs/system/` that holds
> specs, designs, ADRs, or decisions. Produce the
> `tusker docs adopt --migration --dry-run --json` table, set `target`,
> `kind`, `lifecycle`, and `disposition` for every row, and show me the
> table for approval before applying. After I approve, apply it, repoint
> every task and epic `spec_refs` to the new paths, and run
> `tusker docs check` and `tusker validate`. Do not create pointer
> documents and do not delete originals without asking.
