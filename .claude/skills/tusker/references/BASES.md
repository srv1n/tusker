# Bases — editing and extending

The skill payload ships `.base` files in `assets/bases/`. Live vault views live in `_system/views/`.

Current caveat: the CLI init/migration path writes view defaults from `cmd/tusker/v5_templates.go`, not by reading `skill/assets/bases/*.base` directly. Keep the skill assets, live vault views, and code defaults synchronized. If you only edit the skill assets and live vault views, a later `tusker init` can still overwrite them with the hardcoded defaults until the code default is updated.

## View types that actually work

Obsidian 1.9+ supports four native view types:

| Type | Supports `groupBy`? |
|---|---|
| `table` | **yes** — native grouped-table renderer |
| `cards` | **no** — `groupBy` triggers parse error |
| `list` | no |
| `map` | no |

`board` is NOT a native type. Several third-party plugins (Base Board, Kanban for Bases, etc.) add a board renderer, but the skill only emits stock views.

## The groupBy pitfall

Obsidian's parse error is:

```
Unable to parse your base file: 'groupBy' must be a object in view "Board"
```

The grammar is misleading. The real message: "this view type does not accept a groupBy key." Use `type: table` instead:

```yaml
views:
  - type: table
    name: "Board"
    groupBy:
      property: status
    order:
      - id
      - title
```

`direction: ASC | DESC` inside `groupBy` is optional.

## Filter language

Filters are an expression grammar, not full JS. What works:

- `field.ext == "md"` — file property reference
- `this.file.folder == "" || file.inFolder(this.file.folder)` — when embedded from `Dashboard.md`, scope results to that project folder in a shared Obsidian vault while still working when the tracker is the vault root
- `type == "task"` — frontmatter reference (bare field name is fine)
- `status != "cancelled"` — negation
- `status == "active" || status == "review"` — disjunction
- String literals in double quotes
- Boolean literals: `true`, `false`
- `publish == true` — boolean comparisons

Filter blocks combine with `and:` / `or:`:

```yaml
filters:
  and:
    - 'this.file.folder == "" || file.inFolder(this.file.folder)'
    - 'file.ext == "md"'
    - 'type == "task"'
    - 'kind == "bug"'
    - 'status != "cancelled"'
```

Per-view filters stack on top of base-level filters (both must match).

## Shipped task views

`Tasks.base` and `BugTasks.base` use the corrected lifecycle model:

| View | Filter intent |
|---|---|
| `Active` | Work in `status: active`. This is the human-visible runnable state; runtime pickup requires a registered project and a local daemon tick. |
| `Board` | Current execution/review work grouped by status; excludes `done`, `cancelled`, `backlog`, `draft`, and `ready`. |
| `Ready` | `ready` work. CLI pickup still rejects unresolved blockers. |
| `Blocked` | `blocked` work with `blocked_by` and `block_reason` visible. |
| `Backlog` | Shaped future work with `status == "backlog"`. |
| `By Epic` | Open non-draft work grouped by epic. |
| `Needs Attention` | `draft`, `blocked`, `review`, and `rework` items that need shaping, unblocking, verification, or repair. |
| `Review` | `review` work waiting for human verification. |
| `Follow-up` | `rework` tasks. This is the follow-up state; there is no separate `follow-up` status. |
| `Archive` | `done` and `cancelled`. |

Do not encode parking as a priority. Priority remains `p0` through `p3`, and future shaped work belongs in `status: backlog`.

## Dashboard and live runs

`Dashboard.md` should reference only the shipped stock views:

- `Epics.base#Active`
- `Tasks.base#Active`
- `Tasks.base#Review`
- `Tasks.base#Follow-up`
- `Tasks.base#Ready`
- `Tasks.base#Blocked`
- `Tasks.base#Backlog`
- `Tasks.base#Board`
- `BugTasks.base#Board`
- `Docs.base#Pipeline`

Live-run state is not a Base view because leases, process ids, sessions, and event paths are runtime state, not task frontmatter. `tusker reindex` writes the dashboard's `<!-- tusker:live-runs:begin -->` block from the runtime store. It shows active runtime rows for the registered project when present and an explicit "No live runs right now" message otherwise.

There is no shipped `Orchestration.base`. Legacy `Stories.base`, `Bugs.base`, `Attestation.base`, `Verification.base`, and `Orchestration.base` files are stale V4/runtime experiments and should be removed by V5 init/migration rather than referenced from dashboards.

## Property reference forms

- `note.<field>` — note frontmatter property (explicit namespace)
- `<field>` — shorthand, assumes `note.<field>`
- `file.<attr>` — file attribute (`ext`, `name`, `path`, `size`, etc.)
- `formula.<name>` — computed via a named formula

### Clickable rows

If you want a table row to navigate back to the note, include `file.name` as a visible column.
Plain frontmatter fields like `id` and `title` are just text; they do not open the note.

## Column order

`order:` is an array of property names; absent properties are hidden in table views. It is NOT a sort — sort direction is configured per-view in the Obsidian UI and written back to the base file.

## Where to sync changes

When editing `assets/bases/*.base`:

1. Edit the source file in this repo.
2. Edit the live vault copy under `_system/views/` when this repository's own Obsidian surface should change immediately.
3. If `tusker init` or V5 migration should emit the new view, update `cmd/tusker/v5_templates.go` in the code-owner lane and rebuild.
4. For external live vaults, copy the updated `.base` files into `<vault>/_system/views/` after confirming the code defaults will not overwrite them.

## Shared Obsidian vaults

When multiple Tusker trackers are symlinked into one Obsidian vault, unscoped Bases will mix every project's notes. Keep this global filter in shipped dashboard bases:

```yaml
filters:
  and:
    - 'this.file.folder == "" || file.inFolder(this.file.folder)'
```

Obsidian resolves `this.file.folder` to the embedding dashboard note, so `rznapp/Dashboard.md` sees only `rznapp/**` and `tusker/Dashboard.md` sees only `tusker/**`. The empty-folder branch keeps standalone tracker vaults working when `Dashboard.md` sits at the vault root.

## Do not

- Do not use `type: cards` with `groupBy`.
- Do not use `type: board` (not native).
- Do not remove the `this.file.folder == "" || file.inFolder(this.file.folder)` filter from dashboard bases; shared Obsidian vaults will bleed projects together.
- Do not edit `_system/views/` in a live vault expecting changes to persist across init runs.
- Do not add a YAML key-sorting plugin — the property order in a base file is significant for UI layout.
