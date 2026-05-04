# Bases — editing and extending

The shipped `.base` files live in `assets/bases/` and are embedded into the compiled `tusker` binary. `tusker init` writes them to `_system/views/` in the target vault.

If you need a new Kanban, filter, or group, edit the source under `assets/bases/`, rebuild the binary (`go build -o dist/tusker ./cmd/tusker`), and run `tusker init --vault <path> --yes` for the target vault.

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
| `Board` | Current execution work grouped by status; excludes `done`, `cancelled`, `backlog`, `draft`, and `ready`. |
| `Ready` | `ready` work. CLI pickup still rejects unresolved blockers. |
| `Blocked` | `blocked` work with `blocked_by` and `block_reason` visible. |
| `Backlog` | Shaped future work with `status == "backlog"`. |
| `Needs Attention` | `draft`, `blocked`, `review`, and `rework` items that need shaping, unblocking, verification, or repair. |
| `Archive` | `done` and `cancelled`. |

Do not encode parking as a priority. Priority remains `p0` through `p3`, and future shaped work belongs in `status: backlog`.

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
2. `go build -o dist/tusker ./cmd/tusker` — rebuilds `dist/tusker` with fresh embedded assets.
3. For live vaults, run `tusker init --vault <path> --yes` or copy `assets/bases/*` into `<vault>/_system/views/`.

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
