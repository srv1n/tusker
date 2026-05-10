---
title: "Quick Mode"
description: "The low-ceremony path for logging work, discovering follow-ups, and closing routine tasks."
tusker:
  audience: "user"
  publish_path: "user/reference/quick-mode"
  publish_section_title: "Reference"
  route: "/user/reference/quick-mode/"
  source_kind: "repo_doc"
  source_path: "skill/references/QUICK_MODE.md"
  summary: "The low-ceremony path for logging work, discovering follow-ups, and closing routine tasks."
  tags:
    - "reference"
  updated: "2026-05-10"
---

# Quick Mode

The low-ceremony path for logging work, discovering follow-ups, and closing routine tasks.

## Principle

Act with defaults and show what you did. If defaults are wrong, the user corrects once and you remember it for the session.

## Log One Task

```bash
tusker list --type epic
tusker list --epic <ACR> --type task --open
tusker new task --epic <ACR> --title "<what happened>" \
  --kind chore --size s --risk low --priority p2 \
  --domains cli
```

Then tell the user: `Logged as <EPIC>-T-NNNN under <EPIC>. Picked <EPIC> because <one concrete reason>.`

## Defaults

- `kind: chore` unless it is clearly `bug`, `docs`, `research`, or `security`
- `size: s`
- `risk: low`
- `priority: p2`
- `domains`: the broad area touched
- `doc_nodes`: only when durable docs should be checked or updated

## Pick The Epic

1. Run `tusker list --type epic` and scan summaries.
2. Run `tusker search "<term>" --type task` before creating anything that might duplicate existing work.
3. Run `tusker list --epic <ACR> --type task --open` for the likely match only when the search/list result is not enough.
4. Match the work to the nearest epic by subsystem.
5. If nothing fits and this is a real new workstream, create an epic.
6. If truly uncertain, ask one concrete question.

`tusker search` is the default tracker lookup tool. It skips `Attachments/**`,
`_system/**`, runtime logs, and generated indexes. Use shell `rg` only when you
need to search source code or non-tracker files.

## Follow-Ups

```bash
tusker new task --epic <CURRENT-EPIC> --title "<follow-up>" \
  --kind chore --size s --risk low --priority p3 \
  --domains <domain>
```

Keep follow-ups in `draft` or `ready`. Agents propose follow-ups; humans activate them.

The follow-up body must include:

```markdown
Discovered from: [[CURRENT-TASK-ID]]

This is out of scope because <one concrete reason>.
```

## Dependencies

Use relation fields:

```yaml
blocked_by:
  - "[[ABC-T-0001]]"
blocks:
  - "[[ABC-T-0007]]"
```

- Unstarted task with unmet prerequisites: keep it in `draft` or `ready`.
- Started task that cannot continue: move it to `blocked`.
- Add both sides of the link when practical.

Do not invent `prerequisites`.

## Close A Task

```bash
tusker evidence <TASK-ID> pr <url>
tusker docs check <TASK-ID>
tusker status <TASK-ID> review
tusker verify <TASK-ID> --by <name>
tusker close <TASK-ID> --by <reviewer>
tusker validate
```

For quick-mode `risk: low`, one evidence line plus a lightweight verification pass is usually enough. The worker must not certify its own truthfulness.

If `WORKFLOW.md` enables `reviewer`, tasks in `review` can be picked up by the configured reviewer lane. Use `reviewer.actor` (default `agent-reviewer`) for low/medium auto-close when every gate passes. For high/critical tasks, the reviewer leaves advisory evidence and a human runs `verify`/`close`.

## What Not To Do

- Do not read the full skill to log one routine item.
- Do not read the full vault README when `tusker list --type epic` is enough.
- Do not load formal intake unless the work is risky or user-facing.
- Do not create a durable doc for a one-sentence note.
- Do not ask questions you can answer from cwd, the epic roster, or the active task.
- Do not read `Attachments/**`, generated JSON, or raw logs while looking for duplicate tasks.
- Do not paste full build/test output into the chat. Save full logs as files and read a tight summary or tail.
