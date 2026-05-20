# Quick Mode

The low-ceremony path for logging work, finding follow-ups, and closing routine tasks without burning context.

## Principle

Act with defaults and show what you did. Do not escalate lookup/bookkeeping into closeout ceremony.

## Log one task

```bash
tusker list --type epic
tusker search "<duplicate clue>" --type task
tusker new task --epic <ACR> --title "<what happened>" \
  --kind chore --size s --risk low --priority p2 \
  --domains cli
```

Then tell the user: `Logged as <EPIC>-T-NNNN under <EPIC>. Picked <EPIC> because <one concrete reason>.`

## Defaults

- `kind: chore` unless it is clearly `bug`, `docs`, `research`, `security`, `migration`, or `incident`.
- `size: s`.
- `risk: low`.
- `priority: p2`.
- `domains`: broad area touched.
- `proof_mode: inline` for low-risk routine work.
- knowledge updates only when durable repo understanding changes.

## Pick the epic

1. Run `tusker list --type epic` and scan summaries.
2. Run `tusker search "<term>" --type task` before creating possible duplicate work.
3. Run `tusker list --epic <ACR> --type task --open` only for the likely match.
4. Match the work to the nearest subsystem/workstream.
5. If nothing fits and this is a real new workstream, create an epic.
6. If still uncertain, ask one concrete question.

Use shell `rg` only for source-code search. Use `tusker search` for tracker lookup.

## Follow-ups

```bash
tusker new task --epic <CURRENT-EPIC> --title "<follow-up>" \
  --kind chore --size s --risk low --priority p3 \
  --domains <domain>
```

Keep speculative follow-ups in `idea` or `backlog`. Do not make agents chase every discovered follow-up.

The follow-up body should include:

```markdown
Discovered from: [[CURRENT-TASK-ID]]

This is out of scope because <one concrete reason>.
```

## Dependencies and blockers

Use relation fields for task dependencies:

```yaml
blocked_by:
  - "[[ABC-T-0001]]"
blocks:
  - "[[ABC-T-0007]]"
```

Use gates for human/external blockers:

```bash
tusker new gate --blocks <TASK-ID> --kind verification --owner human:<name> \
  --action "Run smoke check in <env>" \
  --verification "Human accepts or waives the smoke result."
```

Do not hide blockers only in body text.

## Close a low-risk task

```bash
tusker verify add <TASK-ID> --covers A1 --check "<focused check>" --result pass --note "<proof>"
tusker finish <TASK-ID> --request-review --summary "<what changed and where proof lives>"
tusker validate --json
```

If the closeout status says only human/reviewer gates remain, stop. Do not keep validating.

## What not to do

- Do not read the full skill to log one routine item.
- Do not read the whole vault when epic list plus search is enough.
- Do not load formal intake unless the work is risky, user-facing, or cross-cutting.
- Do not create durable docs for a one-sentence note.
- Do not ask questions answerable from cwd, epic roster, or current task.
- Do not read attachments, generated indexes, raw logs, or full transcripts by default.
- Do not paste full build/test output into chat. Save logs as files and read a tight summary or tail.
- Do not revalidate unchanged human-wait states.
