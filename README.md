# Tusker

Tusker tracks work in a Git repository. It stores task contracts, proof, gates,
and project knowledge with the source code.

## Start

Install Go and Bun 1.3.14. Then run:

```sh
make check
```

Create or refresh the tracker:

```sh
tusker init --yes
```

Register the repository for local status polling:

```sh
tusker projects add --repo . --vault ./.tusker
```

Registration does not enable automation. An operator must enable the project.

## Work loop

1. Read `.tusker/knowledge/domains/project/CANON.md`.
2. Create one task for one bounded result.
3. Make the contract ready.
4. Change the repository.
5. Record each check with `tusker verify add`.
6. Send the task to review.
7. Close the task only after the close checks pass.

Use `tusker next` to find runnable work. Use
`tusker show <TASK-ID> --capsule` to read one task.

## Current authority

| Subject | Authority |
| --- | --- |
| Commands and flags | `cmd/tusker/cli.go` and `tusker capabilities --json` |
| Record fields and states | `internal/v7schema/schema.go` |
| Proof and close policy | `internal/v7policy/` and `cmd/tusker/v7_closeout_cmd.go` |
| Repository tracker state | `.tusker/` |
| Shared daemon state | `cmd/tusker/runtime_store.go` |
| Serve API | `cmd/tusker/serve_command.go` and `cmd/tusker/serve_*.go` |
| Serve web app | `internal/serve/ui/src/` |
| macOS app | `apps/mac/TuskerBar/` |
| Operator instructions | `skills/tusker/` |
| System documentation | `docs/system/` |

Names such as `tusker.task/v7` are exact file-format identifiers. They do not
name product generations. Do not use them as product labels.

## Documentation

Start with [the system overview](docs/system/00-overview.md). Each system page
names the source files that support its statements.

Use short sentences. Use active voice. Use one term for one idea. Keep paths,
states, and commands exact. Run `tusker docs map` after a system page changes.

## Build and test

| Command | Purpose |
| --- | --- |
| `make fmt-check` | Check Go format. |
| `make test` | Run Go tests. |
| `make vet` | Run `go vet`. |
| `make ui-test` | Run Serve UI tests. |
| `make ui-build` | Build the web files that the Go binary embeds. |
| `make validate` | Check tracker and document state. |
| `make skill-doctor` | Check project skills. |
| `make check` | Run the full local gate. |

## Platforms

- macOS builds the CLI, daemon, Serve, and TuskerBar.
- Linux on amd64 and arm64 builds the CLI, daemon, and Serve.
- The release script can build a Windows archive. The installer, daemon
  service, and TuskerBar do not have a Windows support path.

The CI and release workflows are `.github/workflows/ci.yml` and
`.github/workflows/release.yml`.

## macOS app

Run `make mac-preview` to build, install, and open TuskerBar from this checkout.
The app probes `http://127.0.0.1:7420`. It reuses a healthy local daemon or
starts the bundled daemon. A custom remote base URL is not managed by the app.

Run `make mac-uninstall` to remove the installed app.

## Reset tracker state

Preview the repository cleanup:

```sh
tusker purge --repo . --only-tusker-state
```

Apply it only after the plan names this repository:

```sh
tusker purge --repo . --only-tusker-state --yes
tusker init --vault ./.tusker --yes --fresh --with-pointers --with-contract --no-mount
```

This cleanup removes repository tracker state. It does not remove source code.
It also does not clear all project-keyed rows from the shared runtime database.
Inspect `tusker projects list --json` and the runtime store before and after a
full project reset.
