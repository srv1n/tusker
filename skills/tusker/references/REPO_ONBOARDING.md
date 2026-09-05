# Existing Repository Onboarding

Inspect source, build files, tests, user instructions, and Git status. Identify
entry points, verification commands, supported platforms, local services, and
unresolved facts. Old plans are not evidence of current behavior.

## Storage Boundary

`.tusker/` holds tracker state and canon; product source stays outside it.
Never reset an existing tracker without explicit authorization and a verified
export. `tusker purge --repo . --only-tusker-state` only previews deletion
until `--yes`; inspect every proposed path.

## Initialize without global side effects

Before initialization, choose a writable project-local runtime in sandboxed
sessions; never redirect `HOME`:

```sh
export TUSKER_STATE_ROOT="$PWD/.tusker/runtime-state"
install -d -m 700 "$TUSKER_STATE_ROOT"
tusker init --help
```

For a new tracker only:

```sh
tusker init --vault ./.tusker --yes --fresh --with-pointers --with-contract --no-mount
```

Do not register or enable automation unless requested. Keep the same state
root throughout this project session.

## Workspace and runner

Read-only Git metadata is not permission to widen sandbox access. Request a
writable workspace, or have the operator select shared mode only when
`git status --short` is clean outside `.tusker/` and `owned_paths` are narrow:

```yaml
# .tusker/config.local.yaml (operator-owned)
automation:
  workspace:
    strategy: shared
```

```sh
tusker config resolve automation.workspace.strategy --vault ./.tusker --json
```

Commit or ignore fixture artifacts before checking cleanliness. Fresh setup uses `codex_exec`;
inspect `tusker config resolve automation.profiles --vault ./.tusker --json`.
ACP requires operator `tusker acp setup`, never an agent fallback.

## Canon and delivery

Governing contracts belong in `.tusker/specs/`, not `docs/specs/`:

```sh
tusker docs new auth --kind spec --vault ./.tusker
tusker docs find auth --vault ./.tusker
```

Record source-backed facts in `.tusker/knowledge/domains/project/CANON.md`;
track delivery work as tasks. An open, disarmed, backlog/held wave with no
attempts or reviews can be amended without changing scope or source keys:

```sh
tusker delivery import --plan <plan.yaml> --dry-run --vault ./.tusker --json
tusker delivery import --plan <plan.yaml> --vault ./.tusker
```

Progressed plans need explicit rework/control, not repeated import.

## Validate

```sh
tusker reconcile --vault ./.tusker
tusker validate --vault ./.tusker --json
tusker skill doctor --strict --json
tusker docs map --vault ./.tusker
tusker docs status --vault ./.tusker --json
```

Registration, when requested, is machine state: `tusker projects add --repo .
--vault ./.tusker`. It does not enable automation; leave automation disabled.
