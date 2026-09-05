# Existing Repository Onboarding

Use this guide when a repository does not yet have a current `.tusker/`
tracker.

## Storage Boundary

`.tusker/` holds tracker state and project canon. Product source stays outside
that directory; record durable facts in canon and delivery work in tasks.

## 1. Inspect the repository

Read the source, build files, tests, and user instructions. Check Git status.
Do not infer product behavior from old plans or tickets.

Record:

- the product entry points;
- the build and test commands;
- the main source areas;
- machine-local services and data;
- supported platforms; and
- facts that still need a person.

## 2. Preview init

Run:

```sh
tusker init --help
tusker purge --repo . --only-tusker-state
```

The purge command is a preview until `--yes` is present. Check that every path
belongs to the target repository.

## 3. Create the tracker

For a new tracker, run:

```sh
tusker init --vault ./.tusker --yes --fresh --with-pointers --with-contract --no-mount
```

Do not register or enable automation unless the user asks for it.

## Runtime and workspace setup

This is an operator setup step, not agent permission. If the default global
runtime registry is sandbox-protected, choose a writable per-project state root
before `init` and keep using it for this project shell:

```sh
export TUSKER_STATE_ROOT="$PWD/.tusker/runtime-state"
install -d -m 700 "$TUSKER_STATE_ROOT"
```

For a direct session in a read-only Git worktree, do not retry writes. The
operator may select the existing shared policy only after `git status --short`
is clean outside `.tusker/` and the task has narrow `owned_paths`:

```yaml
# .tusker/config.local.yaml (operator-owned)
automation:
  workspace:
    strategy: shared
```

```sh
tusker config resolve automation.workspace.strategy --vault ./.tusker --json
```

If source, docs, or skills are dirty, commit or otherwise obtain a clean
permitted Git workspace; an agent must request that provision rather than widen
its write permission. Fresh setup uses `codex_exec`; inspect it with
`tusker config resolve automation.profiles --vault ./.tusker --json`. ACP is
an operator choice after `tusker acp setup`, never an agent fallback.

Governing contracts belong in `.tusker/specs/`, not `docs/specs/`:

```sh
tusker docs new auth --kind spec --vault ./.tusker
tusker docs find auth --vault ./.tusker
```

## 4. Write project canon

Update `.tusker/knowledge/domains/project/CANON.md` with current facts only.
Name the code, schema, configuration, and workflow files that support each
fact. Put open delivery work in tasks, not canon.

## 5. Validate

Run:

```sh
tusker reconcile --vault ./.tusker
tusker validate --vault ./.tusker --json
tusker skill doctor --strict --json
```

If the project has system documentation, also run:

```sh
tusker docs map --vault ./.tusker
tusker docs status --vault ./.tusker --json
```

## 6. Register only when needed

Registration is machine state:

```sh
tusker projects add --repo . --vault ./.tusker
tusker projects list --json
```

Registration does not enable automation. Keep it disabled during setup.

## Amend before work starts

An open, disarmed delivery wave whose members remain `backlog`/`held` can be
amended without changing its plan `scope` or task `source_key`:

```sh
tusker delivery import --plan <plan.yaml> --dry-run --vault ./.tusker --json
tusker delivery import --plan <plan.yaml> --vault ./.tusker
```

Once any member progresses, preserve its identity and use the explicit
rework/control route instead of retrying import.

## Source commands

The onboarding behavior comes from `cmd/tusker/install.go`,
`cmd/tusker/purge.go`, `cmd/tusker/commands_index.go`, and
`cmd/tusker/daemon.go`.
