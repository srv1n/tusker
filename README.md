# Tusker

Tusker is a V7 repo-local work protocol for human-authored requirements and agent-executed code work.

The canonical model is deliberately small:

```text
Task markdown     = executable contract
Runner adapter    = execution mechanism
Evidence          = curated proof, not raw logs
Gate              = explicit human/external blocker
Project skill     = routed repository canon
Runtime store     = leases, attempts, sessions, and logs
Generated views   = disposable UI surfaces
```

## Current source of truth

- Operator skill bundle: `skill/SKILL.md` and `skill/references/**`
- Project knowledge skill: `.tusker/SKILL.md`
- Project canon: `.tusker/knowledge/domains/**`
- Work contracts: `.tusker/work/**`
- Machine-local daemon runtime: `~/Library/Application Support/tusker/` on macOS
- Repo-generated views: `.tusker/_generated/**`

## Storage boundary

Tusker keeps portable project truth inside each repository and shared mutable
runtime outside every repository:

```text
~/Library/Application Support/tusker/   daemon DB, registry, logs, limits, workspaces, runtime binary
<project>/.tusker/WORKFLOW.md           project policy and runner contract
<project>/.tusker/work/                 tasks, gates, decisions, and curated evidence
<project>/.tusker/knowledge/            project-specific canon
```

On macOS, prefer repository roots such as `~/Developer`, `~/Code`, or
`~/Projects`. LaunchAgents may be denied access to projects under `Desktop`,
`Documents`, `Downloads`, or iCloud Drive even when the same command works in
Terminal. Tusker warns for those known protected roots during project
registration and blocks daemon-service startup before launch unless the
repository is moved or Full Disk Access was granted and
`--allow-protected-projects` is supplied. Other cloud, network, and removable
volumes may also require access; the startup health check remains the final
truth for those environments.

Legacy V5/V6 tracker/docs artifacts are intentionally not part of the default repo surface. Legacy execution is disabled from the default CLI surface. Migration should happen out-of-band and import only current V7 work, gates, evidence, and canon into `.tusker/`.

## Clean repo setup

For a repo that may contain stale Tusker state, dry-run the deletion plan first:

```bash
tusker purge --repo . --only-tusker-state
```

Then apply the scoped purge and initialize fresh V7 state:

```bash
tusker purge --repo . --only-tusker-state --yes
tusker init --yes --fresh
```

Equivalent one-shot reset:

```bash
tusker init --yes --fresh --purge-state
```

`purge` only targets generated Tusker state: `.tusker`, nested app-local
`.tusker` vaults, repo-local Tusker skill installs, managed AGENTS/CLAUDE
blocks, legacy root Tusker trackers, and matching workspace vault mounts. It is
not a product-source cleanup command.

For local canonical skills, sync repo installs as symlinks:

```bash
tusker skill sync --repo . --mode symlink --source /path/to/tusker
```

Use `--mode copy` or `skill bundle` only for portable handoff packets, CI,
cloud runners, or machines that cannot follow the local symlink target.

## Developer loop

Development and release builds require Go plus Bun 1.3.14. The check gate
installs the locked UI dependencies, tests and builds the embedded Serve UI,
then runs the Go checks and binary build.

```bash
make check
```

Run `go mod tidy` after first applying this cleanup on a machine with network
access so `go.sum` is populated.

## TuskerBar on macOS

Install the CLI, skills, and the signed Mac app with:

```bash
make install
```

`TuskerBar.app` is installed and opened from `~/Applications`. It provides both
a normal full-screen-capable window and a compact menu-bar panel. To install
just the app, run `make mac-install`; `make mac-open` reopens it and
`make mac-uninstall` removes it. The signed app bundles the current Tusker CLI;
opening it starts or reuses the local daemon and its Serve UI automatically.
Run `make mac-preview` to build, install, and open the current checkout in one
command. No separate `tusker serve` terminal is required.
