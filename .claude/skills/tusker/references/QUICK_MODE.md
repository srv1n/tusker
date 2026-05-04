# Quick Mode

The low-ceremony path for logging work, discovering follow-ups, and closing routine tasks.

## Principle

Act with defaults and show what you did. If defaults are wrong, the user corrects once and you remember it for the session.

## Log One Task

```bash
tusker list --type epic
tusker list --epic <ACR>
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
2. Run `tusker list --epic <ACR>` for the likely match.
3. Match the work to the nearest epic by subsystem.
4. If nothing fits and this is a real new workstream, create an epic.
5. If truly uncertain, ask one concrete question.

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

## What Not To Do

- Do not read the full skill to log one routine item.
- Do not load formal intake unless the work is risky or user-facing.
- Do not create a durable doc for a one-sentence note.
- Do not ask questions you can answer from cwd, the epic roster, or the active task.
