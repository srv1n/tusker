# Plugin compatibility

The skill works without any community plugins. Everything below is optional.

## Core: Bases (ships with Obsidian 1.9+)

Already used. `tusker bootstrap` writes five base files into `_system/views/`:

- `Epics.base` — all epics. Board (grouped by status), Active, Intake.
- `Stories.base` — all stories. Board (grouped by status), By risk, By epic, By assignee, Active, In review.
- `Bugs.base` — all bugs. Board (grouped by status), Open, By risk.
- `Docs.base` — all docs. Pipeline (grouped by status), Ready to publish, Published.
- `Attestation.base` — stories and bugs at `status: in_review` awaiting sign-off.

### Schema pitfall: grouping requires a table view

Obsidian's native Bases supports four view types: `table`, `cards`, `list`, `map`.

Only `table` supports `groupBy`. Using `groupBy:` on a `cards` view produces the loud error:

> Unable to parse your base file: 'groupBy' must be a object in view "..."

If you edit a base and want Kanban-style columns by status, use:

```yaml
- type: table
  name: "Board"
  groupBy:
    property: status
  order:
    - id
    - title
    - ...
```

Do not use `type: cards` with `groupBy`. Do not use `type: board` — that's third-party.

A dedicated Kanban UI requires a community plugin (Base Board, Kanban for Bases, etc). The skill ships a grouped-table "Board" view that renders cleanly in stock Obsidian and works on mobile.

### Filter syntax

Filters use a small expression language. Two currently-valid examples from the shipped bases:

```yaml
filters:
  and:
    - 'file.ext == "md"'
    - 'type == "story"'
    - 'status != "cancelled"'
```

Property references can use `note.<field>` prefix or bare field name (equivalent).

## Templater (community plugin)

Install when you want status-transition dates stamped automatically from the Obsidian UI instead of only via `tusker set-status`.

1. Install `templater-obsidian`.
2. Settings → Templater → User Script Functions Folder → set to `_system/snippets`.
3. `_system/snippets/status-hooks.js` exposes `tp.user.setStoryStatus(tp, "...")` plus `markActive`, `markInReview`, `markDone` helpers.
4. Bind to palette commands or hotkeys.

The CLI and Templater paths write the same frontmatter + transitions[] audit row — pick whichever fits the moment.

## Linter (community plugin — mgmeyers)

Automatically updates a timestamp field on save.

- Enable YAML → Date modified → field name: `updated`
- Enable YAML → Format yaml array style → `multi-line array style` (avoids reformatting our flow-style arrays)

Do NOT enable YAML key sorting — it fights our canonical frontmatter order.

## Metadata Menu (community plugin)

Inline frontmatter editor with typed fields. Useful when you want dropdowns.

Suggested field types:

- `status` → Select → `intake, active, blocked, in_review, done, cancelled`
- `change_type` → Select → `feature, bug, refactor, migration, security, docs, chore, research, incident`
- `risk` → Select → `low, medium, high, critical`
- `size` → Select → `s, m, l, xl`
- `priority` → Select → `p0, p1, p2, p3, icebox`
- `delegation` → Select → `execute, explore, escalate`
- `ai_assistance` → Select → `none, light, moderate, heavy`
- `due`, `started`, `completed`, `review_opened`, `blocked_since`, `cancelled_at` → Date

## Supercharged Links (community plugin)

Colors `[[wikilinks]]` based on the target note's frontmatter. Good for a link-heavy vault.

Suggested selectors:

- `type == "story"` AND `status == "in_review"` → yellow pill
- `type == "story"` AND `status == "done"` → green pill
- `type == "story"` AND `status == "blocked"` → red pill
- `type == "bug"` AND `risk == "critical"` → red pill

## What we do NOT recommend

- **`obsidian-kanban` (mgmeyers)** — uses a parallel file format (`kanban-plugin: basic` in frontmatter) that duplicates the source of truth. Use the shipped `Board` view in `Stories.base` instead.
- **YAML key-sorting anything** — breaks our canonical `FRONTMATTER_ORDER`.
- **Auto-archive plugins that move files** — they conflict with the canonical `epics/<ACR>/*` layout.
