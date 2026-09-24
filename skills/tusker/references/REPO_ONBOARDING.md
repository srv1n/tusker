# Existing Repository Onboarding

Inspect source, build files, tests, user instructions, and Git status. Identify
entry points, verification commands, supported platforms, local services, and
unresolved facts. Old plans are not evidence of current behavior.

## Storage Boundary

`.tusker/` holds tracker state and thin documentation pointers only;
product knowledge lives under `docs/system/` and product source stays
outside `.tusker/`.
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

Keep fixture artifacts in their owned disposable location. Inspect
`tusker config resolve automation.profiles --vault ./.tusker --json` for the
configured runner; do not assume a default or substitute a transport.

## Canon and delivery

One placement rule decides where a document lives. Current behavior goes in
`docs/system/` (per domain under `docs/system/domains/<domain>/`), change
specifications in `docs/system/proposals/`, and product decisions in
`docs/system/decisions/`:

```sh
tusker docs new auth --kind proposal --vault ./.tusker
tusker docs new auth-scope --kind decision --decides-for auth --vault ./.tusker
tusker docs find auth --vault ./.tusker
```

Cold-reader answers: document current behavior in `docs/system/` (new domain
chapter via `tusker docs new <subject> --kind doc --domain <name>`); propose
a change in `docs/system/proposals/`; record a settled product decision in
`docs/system/decisions/`; migrate old knowledge with
`tusker docs adopt --migration` (preview first, apply only an approved table).
`.tusker/` holds tracker state and thin pointers only. Product decisions move
to documentation; work and lifecycle decisions stay with the tracker.

Record source-backed facts in `docs/system/`;
track work as tasks. Author tasks directly or batch them with a wave
authoring request:

```sh
tusker new task --title "Fix billing" --work-level standard --vault ./.tusker
tusker wave create --file <request.yaml> --request-key <stable-key> --vault ./.tusker
```

Creation is inert. `tusker wave start`/`tusker task start` are the only
authorization actions; started waves need explicit control, not re-authoring.

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
