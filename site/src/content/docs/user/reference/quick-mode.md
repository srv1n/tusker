---
title: "Quick mode"
description: "The low-ceremony path. Use this for 90% of invocations: logging work, discovering follow-ups, closing things out."
tusker:
  audience: "user"
  publish_path: "user/reference/quick-mode"
  publish_section_title: "Reference"
  route: "/user/reference/quick-mode/"
  source_kind: "repo_doc"
  source_path: "skill/references/QUICK_MODE.md"
  summary: "The low-ceremony path. Use this for 90% of invocations: logging work, discovering follow-ups, closing things out."
  tags:
    - "reference"
  updated: "2026-04-28"
---

# Quick mode

The low-ceremony path. Use this for 90% of invocations: logging work, discovering follow-ups, closing things out.

## Principle

**Act with defaults, show what you did.** If defaults are wrong, the user corrects once and you remember it for the session. Do NOT ask "what type?", "what size?", "what epic?" up front — pick sane defaults and announce your choice.

## Discover the vault

If `--vault` is omitted, `tusker` walks up from cwd looking for `tusker/` (or any dir with `_system/config.yaml`). You only pass `--vault` when you're outside a repo or overriding.

## Log one thing

```bash
tusker list --epic <ACR>   # pick the right epic (one line of reasoning)
tusker new-story --epic <ACR> --title "<what happened>" \
  --size s --risk low --change-type chore --priority p2 \
  --delegation execute --ai-assistance heavy --ai-tools codex
```

Then tell the user: "Logged as `<EPIC>-S-NNNN` under `<EPIC>`. Promote risk/size if this is more than a note."

### Defaults to use for a quick log

- `size: s`
- `risk: low`
- `change_type: chore` (or `bug` if it's a defect report, `docs` if it's just writing something down)
- `priority: p2`
- `delegation: execute`
- `ai_assistance: heavy` (agent created it)
- `ai_tools: [<current-agent>]`

## Pick the right epic

1. Run `tusker list` and scan epic titles.
2. Match the new work to the nearest epic by subsystem.
3. If nothing fits AND this is a genuinely new workstream, create an epic. Otherwise use the closest match — don't create a new epic for a one-line log.
4. If truly uncertain, ask the user *one* concrete question: "Which epic? I see PLC (prompt compiler), HIT (human request), ABL (auth)."

## Discover a follow-up while working

You're executing a story and find something that needs to be tracked separately:

```bash
tusker new-story --epic <CURRENT-EPIC> --title "<follow-up>" \
  --size s --risk low --change-type chore --priority p3 \
  --delegation execute --ai-assistance heavy --ai-tools codex
```

Keep it in `intake`. Agents propose follow-ups; humans activate them.

The follow-up body must include:

```markdown
Discovered from: [[CURRENT-STORY-ID]]

This is out of scope because <one concrete reason>.
```

Add the ID to the current `## Workpad` under `Follow-up proposals`, then add a work-log line to the current story: `- <date> — <agent> — filed draft follow-up <NEW-STORY-ID>`.

Do not use a follow-up note as permission to keep expanding the active story. If the new work is required for the current acceptance criteria, it is not a follow-up; it is current scope or a blocker.

## Dependencies and prerequisites

If the new item depends on another story or bug, use the existing dependency fields:

```yaml
blocked_by:
  - "[[ABC-S-0001]]"
blocks:
  - "[[ABC-S-0007]]"
```

Use them like this:

- Unstarted item with unmet prerequisites: keep it in `intake`.
- Started item that cannot continue because of that prerequisite: move it to `blocked`.
- Add both sides of the link when you can. `blocked_by` tells the truth fastest; `blocks` makes the graph readable.

Do not invent a second field like `prerequisites`. `blocked_by` is the prerequisite list.

## Close a story

```bash
tusker handoff --id <STORY-ID> --for verifier
tusker review verify --id <STORY-ID> --by <name>
tusker review approve --id <STORY-ID> --by <reviewer>
tusker attach-evidence --id <STORY-ID> --kind pr --path <url>
# if risk ≤ medium, an agent verifier can attest; if ≥ high, ask the user
tusker attest --id <STORY-ID> --by <name> --role agent
tusker set-status --id <STORY-ID> --status done
```

For quick-mode (risk low), one evidence line plus a lightweight verification pass is usually enough. Do not let the worker certify its own truthfulness.

## What NOT to do in quick mode

- Don't read the full SKILL.md to log one thing.
- Don't load `FORMAL_INTAKE.md` unless the user said "this is risky" or "this needs a rollout plan."
- Don't create a doc note for a one-sentence note. Put it in the story body.
- Don't ask the user questions you can answer from context (cwd, last-used epic, current claimed story).

## When to escalate out of quick mode

Load `FORMAL_INTAKE.md` when the user says, or the work clearly implies:

- "this is a feature" / "this ships to users"
- "we need a rollback" / "this is a migration"
- "security-sensitive" / "touches credentials"
- risk of data loss, user-facing breakage, or cross-team coordination

Promote the story risk with an edit, or recreate with `--risk medium|high|critical`.
