# Tusker Commands

## Pre-dispatch

```bash
tusker automation plan <TASK-ID> --json
tusker packet <TASK-ID> --for agent
tusker packet <TASK-ID> --for explainer
```

Use `automation plan` as the canonical decision surface. It explains whether the task can dispatch, which runner/lane/workspace will be used, what blocks it, and which skill/domain files should be read.

Use `packet --for explainer` when a human needs to understand a change before or during review. It is an understanding aid, not proof or approval.

## Work State

```bash
tusker status <TASK-ID> ready|review|rework|done|cancelled|superseded --by <actor> --reason "<reason>"
tusker finish <TASK-ID> --request-review
tusker reconcile
```

Do not set a V7 task to `active`. That is legacy vocabulary and must not be reintroduced.

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

## Human Stop

```bash
tusker gate new --task <TASK-ID> --kind auth --owner human:<name> --blocking true --action "<exact human action>" --verification "<how we know it is done>"
tusker closeout <TASK-ID> --emit-packet --validate "<command>"
tusker closeout status <TASK-ID> --json
```

When a human gate blocks the work, stop after creating or updating the gate and closeout packet.
