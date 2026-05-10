# ORC-T-0019 adoption prompt and status evidence

Follow-up scope:

- Added reviewer-lane guidance to the source Tusker skill entry point.
- Added reviewer-lane guidance to quick mode, commands, risk/evidence, workflow, operator intervention, orchestration runbook, and cheat sheet assets.
- Updated the default `WORKFLOW.md` body with a reviewer contract so new vaults explain the post-review behavior beside the runner prompt.
- Refreshed repo-local downstream skill copies through the local source build:
  - `.agents/skills/tusker`
  - `.claude/skills/tusker`
- Added visible status summaries to the repo `README.md`, vault `README.md`, `Dashboard.md`, and `CHEATSHEET.md`.
- Added the dashboard workflow status table to the default dashboard template and generated V5 template code.
- Updated durable docs for the skill contract, agent recipe, and Obsidian operator surface.
- Regenerated published docs output with `docs export` and `docs build`.

Key policy now visible to adopters:

- `review` remains the task status.
- `reviewer.enabled` can launch an independent reviewer lane from `review`.
- `reviewer.runner` is configurable and runner-neutral; Codex is only the current default live runner.
- `reviewer.actor` records attribution, defaulting to `agent-reviewer`.
- Low/medium risk work can be verified and closed by the configured reviewer.
- High/critical risk work remains human-gated.

Verification:

- `go run ./cmd/tusker update --repo . --repo-only --no-bin`
- `go run ./cmd/tusker docs export --site ./site`
- `go run ./cmd/tusker docs build --site ./site`
- `go run ./cmd/tusker reindex`
- `go test ./cmd/tusker -count=1`
- `go run ./cmd/tusker validate`
- `git diff --check`
