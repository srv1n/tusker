# Bases — editing and extending

The shipped `.base` files live in `assets/bases/` and are embedded into the compiled `tusker` binary. On `tusker bootstrap`, they are written to `_system/views/` in the target vault.

If you need a new Kanban, filter, or group, edit the source under `assets/bases/`, rebuild the binary (`go build -o dist/tusker ./cmd/tusker`), and re-bootstrap the target vault. `writeEmbeddedTree` runs with `overwrite: true` for views — hand-edits inside `_system/views/` are replaced.

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
- `type == "story"` — frontmatter reference (bare field name is fine)
- `status != "cancelled"` — negation
- `status == "active" || status == "in_review"` — disjunction
- String literals in double quotes
- Boolean literals: `true`, `false`
- `publish == true` — boolean comparisons

Filter blocks combine with `and:` / `or:`:

```yaml
filters:
  and:
    - 'file.ext == "md"'
    - 'type == "bug"'
    - 'status != "cancelled"'
```

Per-view filters stack on top of base-level filters (both must match).

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
3. For live vaults, overwrite `_system/views/` — either by running `tusker bootstrap --vault <path>` against an existing vault (idempotent; will overwrite `_system/views/`), or by copying `assets/bases/*` into `<vault>/_system/views/`.

## Do not

- Do not use `type: cards` with `groupBy`.
- Do not use `type: board` (not native).
- Do not edit `_system/views/` in a live vault expecting changes to persist across `bootstrap` runs.
- Do not add a YAML key-sorting plugin — the property order in a base file is significant for UI layout.
