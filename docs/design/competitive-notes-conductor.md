# Competitive notes: Conductor (conductor.build)

Reviewed 2026-07-06 from the full public docs (sitemap-verified), the published
`settings.repo.schema.json`, and the changelog (0.0.16 Jul 2025 → 0.72.0 Jul 2026).
Conductor is a macOS app by Melty Labs that runs multiple coding agents (Claude
Code, Codex, Cursor, OpenCode) in parallel git worktrees ("workspaces"). It is
the closest shipping product to Tusker's runner layer, and it has a year of
production hardening. This note records what they built, what we adopt, and
what we deliberately skip.

## Their model in one paragraph

Workspace = worktree + branch + running environment, created under
`~/conductor/workspaces/<repo>/<city-name>`. The workspace is the unit of
delegation; the branch/PR is the unit of integration. Agents run un-sandboxed
with the user's permissions (tool approvals are opt-in). Review is an agent
action with a separately configurable model. Settings are TOML, layered
managed > repo-local override (gitignored) > repo shared (committed) > user
global > built-in defaults. There is no task system, no contracts, no proof
model, no routing rules, and no CLI/headless surface (deep links only).

## Validation: what they converged on that we already contracted

| Their feature | Our artifact |
|---|---|
| Full access by default, approvals opt-in, no sandbox | guarded-yolo preset (RUN-T-0002); `approval_policy: never` |
| Separate review model + effort (`models.review`) | CLN-D-0001 reviewer-is-not-author, frontier review lane |
| Settings layering, most specific wins | RUN-T-0002 profile precedence chain |
| Worktree per parallel stream, one branch per workspace | RUN-D-0001 / RUN-T-0005 |
| Agent-drafted PR descriptions, agent-assisted conflict help | RUN-T-0005 landing design |
| Status-grouped sidebar: backlog / in progress / in review / done | serve UX packet routes |
| `.context` folder for uncommitted per-workspace notes | `.tusker/scratch/<TASK-ID>/` |
| Issue → workspace (GitHub/Linear) | task → run (the task *is* the issue) |

## Adopt: gaps they exposed

1. **Workspace lifecycle scripts and environment (biggest gap → RUN-T-0006).**
   A fresh worktree starts with tracked files only: no `.env`, no installed
   deps, no build output. Conductor solves this with `scripts.setup` (runs on
   workspace creation), `scripts.run` (named long-running dev/test commands),
   `scripts.archive` (cleanup before archive), a files-to-copy mechanism for
   gitignored files (`.worktreeinclude` / `file_include_globs`, default
   `.env*`), injected env vars (`CONDUCTOR_WORKSPACE_PATH`, `CONDUCTOR_ROOT_PATH`,
   `CONDUCTOR_DEFAULT_BRANCH`, `CONDUCTOR_PORT`, workspace name), and a
   reserved 10-port range per workspace so concurrent dev servers never
   collide. Without an equivalent, RUN-T-0005's parallel worktrees fail on any
   project with local env files or dependencies.

2. **Machine-local config override layer (→ RUN-T-0002).** Their
   `.conductor/settings.local.toml` is a gitignored per-machine override that
   beats the committed repo settings. Tusker needs the same so secrets and
   personal preferences never enter the committed `tusker.yaml`, plus a
   user-global config for cross-project defaults.

3. **Opt-in agent-assisted conflict resolution (→ RUN-T-0005).** Their answer
   to merge conflicts is "ask an agent to resolve them" with a customizable
   prompt (`prompts.resolve_merge_conflicts`). We keep park-and-escalate as
   the default, but an opt-in conflict-resolution lane can take a first pass
   before the needs-me card is created.

4. **Serve UI patterns (→ UX packet addendum, SRV epic).** Per-turn
   checkpoints stored as private git refs with one-click restore; a
   merge-readiness "Checks" card aggregating git status, CI, review threads,
   and open todos; unread tracking with jump-to-next-needs-attention;
   todos-as-merge-blockers. Also their liveness detail: distinct sidebar
   icons for waiting-on-plan-approval vs waiting-on-input vs running.

5. **Harness config parity (→ RUN-T-0001).** Their "Sync Agent Configs"
   button copies skills, slash commands, and MCP servers between Claude Code
   and Codex so both harnesses see the same project context. Tusker should
   verify both runners get equivalent context at dispatch, not leave it to
   per-harness dotfiles drifting apart.

## Skip, with reasons

- **Cities naming.** Charming, but Tusker workspaces are keyed by task ID,
  which is already stable, meaningful, and traceable.
- **Spotlight testing** (sync a worktree back to root for testing). Inverse of
  our design; in-place-first (RUN-D-0001) makes it unnecessary at N=1, and at
  N>1 run scripts + port ranges cover it.
- **Message queues, steering, chat tabs.** Conductor is chat-first; Tusker is
  contract-first. Our equivalent of "queue another message" is "amend the
  contract and let rework dispatch."
- **Deep links.** Their only external automation surface. Tusker already has a
  full CLI; a `tusker://` scheme is not worth building before serve ships.
- **Checkpoint-restore deleting conversation history.** Their restore rewinds
  chat + code together. Tusker attempts are already separate, resumable, and
  auditable; we take the safe-point-ref idea (RUN-T-0005 A5) without the
  destructive chat rewind.

## Where Tusker is ahead

No equivalent exists in Conductor for: task contracts with acceptance +
verification rows, proof modes and evidence budgets, risk-tiered close
policies, routing rules (their model selection is manual per chat), file
claims (their overlap avoidance is "the human decides what to parallelize"),
a resident daemon dispatching autonomously, or any headless operation. Their
product assumes a human at the keyboard steering every workspace; Tusker's
assumes the human writes the contract and walks away. That difference is the
product.

## Work streams

- [[RUN-T-0006]] — workspace lifecycle scripts, files-to-copy, env vars, ports (new, from this review)
- [[RUN-T-0002]] — amended: machine-local + user-global config layers
- [[RUN-T-0005]] — amended: opt-in conflict-resolution lane, land cleanup policy
- [[RUN-T-0001]] — harness config parity note
- [[SRV-T-0002]] — UX packet addendum: checkpoints, checks card, needs-attention nav
