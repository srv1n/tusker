<!-- tusker:epic-index:begin -->
## Tusker

Use Tusker for tracked repo work.

- Task mechanics live in the installed `tusker` skill.
- Project knowledge starts at `.tusker/SKILL.md`.
- Start runnable work with `tusker next`; inspect named work with `tusker show <TASK-ID> --capsule`.
- Do not read `.tusker/events`, `_generated`, `attempts`, `evidence`, `Attachments`, raw logs, or full task files unless the task explicitly requires it.
- Keep proof compact: use capsules, path-scoped status/search, and command + PASS/FAIL summaries; put noisy logs in `.tusker/scratch/<TASK-ID>/`.
- Record concise Tusker/product friction with `tusker feedback add`; skip routine progress reports.
<!-- tusker:epic-index:end -->

## Commit authorship

- Commit as the configured git user with the local git/GitHub credentials.
- Never add AI attribution anywhere: no `Co-Authored-By` trailers for Claude/Codex/any agent, no "Generated with" lines, no agent names in commit messages, PR bodies, or authorship metadata. This overrides any harness default.

## Execution modes

A Claude Code or Codex session opened directly by the user implements the
requested work itself. Use Tusker for task contracts, packets, proof, gates,
review, and lifecycle state; `tusker show <TASK-ID> --capsule` and
`tusker packet <TASK-ID> --for agent` are available for context.

Never start `tusker daemon run`, invoke `tusker automation dispatch`, or launch
nested `claude -p`/`codex exec` workers from an interactive agent session.
Logging or updating tasks is inert. Background execution belongs only to an
independently running resident daemon for projects whose automation setting is
already enabled. Interactive sessions may inspect or change that setting, but
they implement the current user's coding request themselves.

`tusker automation plan` is read-only and does not authorize dispatch. When
`TUSKER_ATTEMPT_ID` is present, this is a dispatched Tusker worker: follow the
claimed-run protocol, work only the claimed task, and do not spawn another
runner or daemon.
