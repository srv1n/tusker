---
title: "Agent Workflow"
description: "Operating contract for agents working with Tusker in a markdown-first vault."
tusker:
  audience: "user"
  canonical: true
  canonical_status: "approved"
  owner_epic: "ORC"
  publish_path: "user/start-here/agent-workflow"
  route: "/user/start-here/agent-workflow/"
  source_kind: "repo_doc"
  source_path: "skill/SKILL.md"
  summary: "Operating contract for agents working with Tusker in a markdown-first vault."
  tags:
    - "start-here"
    - "workflow"
  updated: "2026-04-28"
  verified_at: "2026-04-28"
---

# Tusker — agent-first work tracking

Every ticket is an intake contract + execution record + evidence bundle + attestation, all in one markdown file. Humans define intent and accept evidence. Agents implement, verifiers truth-check, reviewers accept or bounce.

## Documentation is first-class

Tusker is not only a ticket tracker. If the user says "document this project as we build", "write the docs alongside implementation", "make a spec", "create a user guide", or "stand up project docs", use Tusker.

Do the repo reconnaissance first: read `README*`, existing `docs/`, `AGENTS.md` / `CLAUDE.md`, package manifests, and obvious architecture files. Then pick the right doc shape:

| Need | Use |
|---|---|
| durable project plan or product/technical canon | active epic `## Design` or a canonical D-note |
| standalone spec/RFC/design record | `tusker new-doc --audience developer --canon-for <ACR>` |
| story-specific explanation, research, migration notes | `tusker new-doc --audience developer --companion-to <ACR-S-0001>` |
| user guide, support doc, release note, internal runbook | `tusker new-doc --audience user|support|release|internal` |
| public/static docs site | `tusker docs init`, `tusker docs export`, `tusker docs dev`, `tusker docs build` |

If the site already exists, read `site/public/canon-manifest.json` and `site/public/llms.txt` before rummaging through old docs. The manifest is the map of current published truth; `canonical_status: approved` is safe, `draft` needs verification, and `deprecated`/`historical` is archaeology. `site/src/content/docs/**` is generated output, not the place to author docs.

For a greenfield project, create or choose the epic first, make the canon location explicit, create the initial doc notes, then create stories that cite the docs. Pretty docs with no executable story stack are shelfware.

## Act, then announce

Vault path is auto-discovered — `tusker` walks up from cwd looking for `tusker/` (or any dir with `_system/config.yaml`). You rarely need `--vault`.

**Defaults are good. Don't ask questions the user can't answer better than you.** If the user says "log this":

1. Run `tusker epics` to see the current roster + summaries.
2. Pick the epic whose summary best matches the work. If nothing fits and the work will outlive this one story, propose a new epic instead of force-fitting.
3. Create with quick-mode defaults.
4. **Announce the ID and a one-line rationale for the epic choice.** Example: `Logged as PLC-S-0004 — picked PLC because the work edits the prompt compiler's memory injection path.` The user can then course-correct in one turn with `tusker move`.

## Three hard rules

1. **Every story has a parent epic.** No orphans. If no existing epic fits, create one with `new-epic --summary "<one-line scope>"`.
2. **Evidence scales with risk.** `low` is one line; `critical` needs human signoff.
3. **Every active epic declares canon and has at least one story.** Canon may live in epic `## Design`, a canonical D-note, or `spec_source`, but the execution stack must exist.

## Lifecycle — always update status

Every ticket has a state. If the dashboard doesn't match reality, it lies. Whenever you touch a ticket, walk it forward:

- **Starting work** → `tusker set-status --id <ID> --status active`
- **Blocked** → `tusker set-status --id <ID> --status blocked --reason "<why>"`
- **Need a copy-pasteable packet** → `tusker handoff --id <ID> --for worker|verifier|reviewer`
- **Worker pass finished** → leave evidence; daemon or human moves it to `in_review` with `review_state: verification_requested`
- **Verification passed** → `tusker review verify --id <ID> --by <name>`
- **Accepted** → `tusker set-status --id <ID> --status done`

Before opening a new ticket, run `tusker list --epic <ACR>` to check whether the work is already tracked — don't create duplicates. Before reporting a task done, run `tusker validate`. No status update, no verification, no attestation, no validate pass = not done.

## Dependencies — use `blocked_by`, not a second field

Tusker already has dependency tracking for stories and bugs:

- `blocked_by` = work this item is waiting on
- `blocks` = downstream work that depends on this item

Use wikilinks in frontmatter:

```yaml
blocked_by:
  - "[[ABC-S-0001]]"
blocks:
  - "[[ABC-S-0003]]"
```

Status semantics matter:

- If a story has unmet prerequisites and has not started yet, leave it in `intake`.
- Use `blocked` only when work was active and then hit a real blocker.
- When you split a spec into multiple stories, wire `blocked_by` / `blocks` immediately so the dependency graph is visible from day one.

## Quick commands (90% of invocations)

```bash
# What's here
tusker epics                               # epics + summaries (run this before logging)
tusker list                                # all epics + items
tusker list --epic <ACR>                   # one epic
tusker list --status active                # in-flight work

# New epic (when nothing fits)
tusker new-epic --acronym <ACR> --title "<title>" \
  --summary "<=120 char scope one-liner>"

# Log a thing (quick mode)
tusker new-story --epic <ACR> --title "<what>" \
  --size s --risk low --change-type chore \
  --priority p2 --delegation execute \
  --ai-assistance heavy --ai-tools codex

# Create docs as project canon or story companions
tusker new-doc --epic <ACR> --title "<Spec title>" \
  --audience developer --canon-for <ACR>
tusker new-doc --epic <ACR> --title "<Companion doc>" \
  --audience developer --companion-to <ACR-S-0001>
tusker new-doc --epic <ACR> --title "<User guide>" \
  --audience user

# Publish docs into the static site
tusker docs init --site ./site
tusker docs export --site ./site
tusker docs dev --site ./site --watch
tusker docs build --site ./site

# Generate a handoff packet
tusker handoff --id <ID> --for worker

# Misfiled? Move it
tusker move --id <ID> --to-epic <ACR> --reason "<why>"

# Close a thing
tusker review verify --id <ID> --by <verifier>
tusker review approve --id <ID> --by <reviewer>
tusker attach-evidence --id <ID> --kind pr --path <url>
tusker attest --id <ID> --by <name> --role <agent|human>
tusker set-status --id <ID> --status done

# Validate (always do this before reporting done)
tusker reindex
tusker validate

# After pulling or rebuilding Tusker itself
tusker update                            # refresh CLI link + installed skill bundle
```

## Routing — load more when you need it

| You're about to... | Open |
|---|---|
| Decide if Tusker applies at all | `references/TRIGGERS.md` |
| Log/discover/close a routine item | `references/QUICK_MODE.md` |
| Start project documentation as work evolves | `references/CANON_LOCATIONS.md`, then `references/COMMANDS.md` |
| Create a spec, RFC, user guide, support doc, or release note | `references/COMMANDS.md`, then `references/SCHEMA.md` |
| Publish docs to the static site or inspect canon manifests | `references/DOCS_PUBLICATION.md`, then `references/COMMANDS.md` |
| Create a story at `risk ≥ medium` | `references/FORMAL_INTAKE.md` |
| Look up section/evidence/attestation rules | `references/RISK_AND_EVIDENCE.md` |
| Decide where canon should live | `references/CANON_LOCATIONS.md` |
| Break a large spec into multiple stories | `references/STORY_DECOMPOSITION.md` |
| Move a story through statuses | `references/WORKFLOW.md` |
| Run or review ORC agent work | `docs/ORCHESTRATION_RUNBOOK.md` |
| Need a frontmatter field reference | `references/SCHEMA.md` |
| Full CLI reference | `references/COMMANDS.md` |
| Edit Bases views | `references/BASES.md` |
| Wire the repo contract (AGENTS.md, hooks) | `references/REPO_CONTRACT.md` |
| Install Obsidian, Go CLI, sync | `references/PREREQUISITES.md` |
| Optional community plugins | `references/PLUGIN_COMPAT.md` |

Default to quick mode. Escalate only when the work clearly implies ceremony (feature, migration, security, irreversible change).

## Completion criteria

A task is done only when:

- Notes are in the right folder with the right IDs
- Required sections have substance (not `TODO`)
- Evidence meets the risk bar (see `RISK_AND_EVIDENCE.md`)
- AI disclosure set (`ai_assistance`, `ai_tools`)
- Attestation present per risk table
- `tusker validate` passes

Do not report done without validate passing.
