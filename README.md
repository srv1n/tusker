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
- Runtime and generated material: `.tusker-runtime/**` and `.tusker/_generated/**`

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

```bash
make check
```

That runs `go test ./...` and strict skill doctor validation. Run `go mod tidy` after first applying this cleanup on a machine with network access so `go.sum` is populated.
