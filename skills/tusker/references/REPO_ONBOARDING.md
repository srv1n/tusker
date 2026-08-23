# Existing repository onboarding

Use this guide when a repository does not yet have a current `.tusker/`
tracker.

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

## Source commands

The onboarding behavior comes from `cmd/tusker/install.go`,
`cmd/tusker/purge.go`, `cmd/tusker/commands_index.go`, and
`cmd/tusker/daemon.go`.
