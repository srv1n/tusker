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

## Roles

- The interactive assistant session is the planner: design, specs, task contracts, UX packets, review, and orchestration. It does not write implementation code.
- All implementation is done by dispatched runners (Codex app-server, Claude Code) working Tusker tasks.
