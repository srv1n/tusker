---
title: "Tusker CLI reference"
subject: cli
keywords: [cli, commands, reference]
part_of: overview
status: canonical
read_when: "You need to know which `tusker` command does a thing, what it means in plain terms, and when to reach for it."
skip_when: "You want the mental model or architecture — read the [overview](00-overview.md) first."
---

# Tusker CLI reference

Every command below is dispatched by `cmd/tusker/cli.go`. One-line descriptions
come from each command's own help text (`tusker help` / `tusker <cmd>` usage).
Run `tusker help <cmd>` for full usage; not every subcommand registers a help
page yet.

Global: if `--vault <path>` is omitted, Tusker walks up from the current
directory to find the repo-local `.tusker/` vault. Most read commands support
`--json`.

## Create & inspect — get work in, look at it

| Command | Plain language | When to reach for it |
|---|---|---|
| `init` | Initialize or refresh a repo vault | First time setting up Tusker in a repo (`tusker init --yes`). |
| `new epic\|task\|bug\|doc\|gate\|decision` | Create a V7 record | Cutting new tracked work; `new task --epic APP --size m --risk medium`. |
| `list` | List work records | Quick scan of what exists; supports filters. |
| `search` | Search tracker notes (skips generated files/attachments) | Find a task by words without dredging raw logs. |
| `show` | Show a bounded note *capsule* or one section | The default way to read a task cheaply (`show <id> --capsule`). |
| `print` | Render a note as terminal-friendly Markdown | You want the fuller note, not just the capsule. |
| `next` | Show the next pickable task | "What should I work on?" Ranked p0-before-p1, then risk, then id. `--explain` shows why others were skipped. |

## Lifecycle — move a task through its states

Durable statuses: `idea, backlog, ready, review, rework, superseded`
(plus terminal `done`, `cancelled`). Runtime activity is a lease/run, not a status.

| Command | Plain language | When to reach for it |
|---|---|---|
| `status <id> <status>` | Move a task through its workflow | Hand-move e.g. to `ready` or `review`; use `discard`/`close` for terminal moves. |
| `claim` / `next --claim --as <who>` | Take a local lease on a task | Start work; the claim marks it as being worked (and, interactively, "outside the daemon"). |
| `verify` / `evidence add` | Record proof rows / evidence cards | Attach the PASS/FAIL and artifacts that prove acceptance. |
| `close` | Close a task after gates + evidence pass | The task is genuinely done and proven. |
| `accept <id> --by reviewer:name` | Confirm green proof, record acceptor, and close in one move | A reviewer signs off finished, already-green work. **`--by` must be `reviewer:<name>` or `human:<name>`** — it refuses a bare/default actor and never rubber-stamps unproven proof. |
| `discard` | Abandon work safely, preserving history | Cancel a task without leaving dangling deps/gates/leases. Prefer this over setting `cancelled` directly. |

## Orchestration — the daemon and automated dispatch

| Command | Plain language | When to reach for it |
|---|---|---|
| `daemon run\|install\|status\|stop\|limits\|resume` | The operator loop for registered local projects | Run/inspect the resident background worker. Interactive sessions must **not** start `daemon run`. |
| `automation status\|queue\|explain\|plan\|dispatch` | Plan, inspect, and manually dispatch daemon work | `plan`/`explain`/`queue`/`status` are read-only (`plan` is the canonical dispatch decision); `dispatch` actually hands a task to a worker after eligibility checks. |
| `projects add\|list\|enable\|disable\|remove` | Register repos for daemon pickup | Tell the daemon which repos it may work, and toggle automation per project. |
| `runs inspect\|logs\|events\|interrupt\|release\|retire\|redrive` | Inspect and manage daemon runs/leases | Tail, stop, or replay a running/stuck attempt. |
| `refresh` | Run one daemon poll tick | Nudge the loop once without a resident daemon. |
| `gate-run --changed --base <ref>` | Run gate-tier proof in harvest mode | Per-change gate: `--changed` runs only the scopes the diff touched (requires configured `orchestration.gate.scopes`); `--base` sets the diff base. Without `--changed`, runs the full harvest. |
| `gate-ledger check\|record` | Check or record tree-keyed gate results | Skip re-running a gate whose result for this exact tree hash is already recorded green. |
| `closeout <id> [--emit-packet] [--validate ...]` / `closeout status` | Emit or inspect terminal human-wait checkpoints | Produce/inspect the final review checkpoint at the end of a task. |

## Views — read the state as a human

| Command | Plain language | When to reach for it |
|---|---|---|
| `logbook` | Render a plain-language daily digest for a product reader | Daily "what happened" in human words. `--date <YYYY-MM-DD>` (host-local, default today); `--write` persists it under `.tusker/logbook/`. |
| `digest` | Machine/agent activity digest | Broader/older activity summary (`--since <ts>`, `--json`). |
| `streams` | Show the generated live/landed orchestration lane board | See what is running vs landed across the lanes. |
| `dashboard build\|open` | Build/open generated dashboards | Regenerate or open the dashboard files under `.tusker/dashboards/`. |
| `brief` / `packet <id> --for agent\|reviewer\|explainer` | Print a human brief / generate an agent or reviewer packet | Hand a worker or reviewer exactly the context they need. |

## Hygiene — keep the vault honest

| Command | Plain language | When to reach for it |
|---|---|---|
| `validate` | Check vault invariants | Before trusting state, or after editing knowledge/contracts (`validate --json`). |
| `reconcile` | Recompute readiness and next-action projections | State looks stale after manual edits or merges. |
| `reindex` | Rebuild generated indexes | Generated indexes are out of sync with the notes. |
| `compact` | Remove empty optional metadata and disposable scaffolding | Trim note noise so contracts stay readable. |
| `feedback add\|digest\|ingest\|review\|promote` | Add agent feedback notes and generate digests | Record concise Tusker/product friction; later triage and promote it. |
| `skill doctor\|route\|pack\|sync\|bundle` | Doctor/route/pack the V7 project skills | Keep the installed operator and project-knowledge skills healthy. |
| `setup doctor\|repair` | Diagnose and repair local onboarding drift | Local install/link/hook drift is suspected. |
| `state sync\|import\|export` | Sync runtime state to/from a git branch | Move runtime state between machines. |
| `hook install` | Install optional local git hooks | Enforce branch/validate policy at commit/push time. |

## Notes on the biggest workflows

- **Reviewer lane.** After a worker submits, an independent reviewer (a different
  model where configured) checks the diff; a finding returns the task to
  `rework` with the reason attached, and only a `reviewer:`/`human:` actor can
  `accept`/`close`. See [gates.md](gates.md) and [orchestration.md](orchestration.md).
- **Gate tiers.** Per-change gates run on touched scopes only; a wave-end gate
  runs a collective compile+lint+test; the full suite runs nightly. See
  [gates.md](gates.md).

TODO-verify: `gate-run` and `streams` do not register a `tusker help` page;
descriptions above are drawn from `cli.go` and `gate_tier.go`. Update if help
pages are added.
