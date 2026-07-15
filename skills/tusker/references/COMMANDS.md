# Tusker Commands

## Pre-dispatch

```bash
tusker automation plan <TASK-ID> --json
tusker packet <TASK-ID> --for agent
tusker packet <TASK-ID> --for explainer
```

Use `automation plan` as the canonical decision surface. It explains whether the task can dispatch, which runner/lane/workspace will be used, what blocks it, and which skill/domain files should be read.

Use `packet --for explainer` when a human needs to understand a change before or during review. It is an understanding aid, not proof or approval.

## Wave authorization

```bash
tusker wave preflight W-0001 --json
tusker wave arm W-0001 --by human:<name>
tusker wave pause W-0001 --reason "<reason>"
tusker wave resume W-0001 --by human:<name>
tusker wave disarm W-0001 --reason "<reason>"
```

Preflight reads the whole batch without changing it. Arm fingerprints the
governing specs and material task/gate/dependency contracts, then promotes only
held wave members. Proof progress does not stale authorization; an intent change
does. Pause and disarm stop future daemon claims without terminating live work.

## Work State

```bash
tusker status <TASK-ID> idea|backlog|ready|review|rework|superseded --by <actor> --reason "<reason>"
tusker discard <TASK-ID> --dry-run
tusker discard <TASK-ID> --reason "<reason>" [--dependents detach|discard] --by <actor>
tusker finish <TASK-ID> --request-review
tusker reconcile
```

Do not set a V7 task to `active`. That is legacy vocabulary and must not be reintroduced.

Do not set `cancelled` directly. `tusker discard` is the abandonment ceremony:
it preserves the tombstone and history, retires runtime execution, obsoletes
open gates, and refuses unresolved downstream dependencies until the operator
explicitly detaches them or discards the downstream closure. Run `--dry-run`
before committing when a task may have dependents.

## Skill Installs

```bash
tusker skill sync --repo . --mode symlink --source /path/to/tusker
tusker skill sync --repo . --mode copy
tusker skill bundle --repo . --out .tusker/_generated/skill-bundle
```

Use `skill sync --mode symlink` for normal local development. Pass `--source` when running from outside the canonical Tusker checkout. Use `skill bundle` or `--mode copy` for portable handoff packets, cloud runners, CI, or machines that cannot follow local symlinks.

Never patch generated install copies directly. Patch canonical skill source for generic behavior, or patch repo-local `.tusker/**` / `.chatgpt-handoff.*` files for project memory.

## Clean Setup

```bash
tusker purge --repo . --only-tusker-state
tusker purge --repo . --only-tusker-state --yes
tusker init --yes --fresh --purge-state
```

`purge` defaults to dry-run and removes only known Tusker-generated state:
`.tusker`, nested app-local `.tusker`, repo-local Tusker skill installs,
managed AGENTS/CLAUDE blocks, and matching workspace vault mounts. It must not
delete product source files.

## Proof

```bash
tusker verify add <TASK-ID> --covers A1 --check "<exact command or manual proof>" --result pass --note "<bounded note>"
tusker evidence add <TASK-ID> --kind verification_summary --covers A1 --summary "<bounded proof>"
tusker proof status <TASK-ID>
```

Use inline verification for normal code tasks. Use evidence cards/artifacts/audit only when the task proof mode requires them.

### Verification "Check" grammar

A task cannot go `ready` (and will not dispatch) until every row in its `## Verification` table names an exact proof. A `Check` cell is exact when it starts with one of two markers:

- `command: <exact shell command>` — anything runnable, tool-agnostic. Compound commands are fine: `command: cd internal/serve/ui && bun test`, `command: swift build -c release`, `command: go test ./cmd/tusker -run TestFoo -count=1`.
- `manual proof: <exact steps a human runs>` — for outcomes only a human can confirm: `manual proof: launch the bundle and confirm the menu item opens the browser`.

Passing vs failing:

| Check cell | Exact? | Why |
|---|---|---|
| `command: cd apps/mac && swift build -c release` | ✅ | starts with `command:`, real command |
| `manual proof: open /panel at 420×640 and confirm no horizontal scroll` | ✅ | starts with `manual proof:`, real steps |
| `bun test` | ✅ | bare command still accepted (legacy), but prefer the `command:` marker |
| `command: <exact command that proves A1>` | ❌ | unfilled `<...>` placeholder |
| `TBD` / `Run the tests` | ❌ | no marker, no exact command |

The marker must be in the **Check** column, not Notes. Freshly created tasks (`tusker new task`) ship a placeholder row in this grammar; replace the `<...>` before promoting to `ready`. Run `tusker validate` to lint rows before promotion — the warning and the dispatch blocker both restate this grammar with an example.

## Human Stop

```bash
tusker gate new --task <TASK-ID> --kind auth --owner human:<name> --blocking true --action "<exact human action>" --verification "<how we know it is done>"
tusker closeout <TASK-ID> --emit-packet --validate "<command>"
tusker closeout status <TASK-ID> --json
```

When a human gate blocks the work, stop after creating or updating the gate and closeout packet.
