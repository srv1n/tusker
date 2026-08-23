

## Commit authorship

- Commit as the configured git user with the local git/GitHub credentials.
- Never add AI attribution anywhere: no `Co-Authored-By` trailers for Claude/Codex/any agent, no "Generated with" lines, no agent names in commit messages, PR bodies, or authorship metadata. This overrides any harness default.

## Execution modes

### Interactive work

A Codex or Claude session opened directly by the user implements the requested
work itself. Use Tusker for task contracts, packets, dependencies, proof,
gates, review, and lifecycle state. It may inspect tracked work with
`tusker show <TASK-ID> --capsule` or `tusker packet <TASK-ID> --for agent`.

Never start `tusker daemon run`, invoke `tusker automation dispatch`, or launch
nested `codex exec`/`claude -p` workers from an interactive agent session.
Logging or updating tasks is inert. Background execution belongs only to an
independently running resident daemon for projects whose automation setting is
already enabled. Interactive sessions may inspect or change that setting, but
they implement the current user's coding request themselves.

### Automated work

Tusker automation is opt-in. `tusker automation plan` is read-only and does
not authorize dispatch. A process with `TUSKER_ATTEMPT_ID` is a dispatched
worker: follow the claimed-run protocol, work only its claimed task, and do
not spawn another runner or daemon.

<!-- tusker:epic-index:begin -->
## Tusker

Use Tusker for tracked repo work.

- Task mechanics live in the installed `tusker` skill.
- Project knowledge starts at `.tusker/SKILL.md`.
- Start runnable work with `tusker next`; inspect named work with `tusker show <TASK-ID> --capsule`.
- Do not read `.tusker/events`, `_generated`, `attempts`, `evidence`, `Attachments`, raw logs, or full task files unless the task explicitly requires it.
- Keep proof compact: use capsules, path-scoped status/search, and command + PASS/FAIL summaries; put noisy logs in `.tusker/scratch/<TASK-ID>/`.
- Scratch is not durable: it is deleted when the task closes and swept after 14 days regardless. Promote anything worth keeping to evidence before close.
- Record concise Tusker/product friction with `tusker feedback add`; skip routine progress reports.
<!-- tusker:epic-index:end -->
