# Agent Feedback Product Stories - 2026-05-21

Source feedback reviewed:

| Repo | Notes | Signal |
|---|---:|---|
| `tusker` | 1 | Feedback loop/bootstrap friction |
| `cinta_wt_phase1_test` | 4 | Protected status, CAS conflicts, legacy/V7 ID collisions |
| `rznapp` | 4 | Shared workspace verification noise, compile blockers, long status-like notes |
| `backend` | 0 | No agent feedback yet |
| `rzn-browser` | 0 | No agent feedback yet |
| `CarelessWhisper` | 0 | No agent feedback yet |

## Story AFS-001 - Prevent Legacy/V7 Task ID Collisions

Priority: P0

As an agent creating a task in a migrated or mixed-layout Tusker vault, I need `tusker new task` to check legacy and current task IDs before writing anything, so I do not leave the vault invalid and then spend a turn cleaning up a colliding task stub.

Acceptance criteria:

- `tusker new task` checks IDs across `tusker/work/tasks/**`, `tusker/epics/**`, and any configured legacy task roots before choosing or accepting an ID.
- If an ID is already present anywhere, the command fails before writing task, event, evidence, or generated files.
- The error prints both conflicting paths and the next safe ID candidate.
- `tusker validate` reports any existing mixed-layout ID collision with exact repair guidance.
- Regression covers a V5 task in `tusker/epics/<ACR>/<ID>.md` blocking creation of the same V7 ID in `tusker/work/tasks/<ID>.md`.

Source notes:

- `2026-05-21-codex-sbl-id-collision.md`
- `2026-05-21-wikilink-autocomplete.md`

## Story AFS-002 - Explain Protected-Branch Implementation Flow

Priority: P0

As an agent working on a protected branch, I need blocked durable state transitions to explain the correct V7 implementation flow, so I can continue with attempts, proof, and handoff without guessing or leaving a ready task that looks untouched.

Acceptance criteria:

- When `tusker status <TASK-ID> active` or another protected transition is rejected, the error explains the preferred path: `attempt start`, implementation, `verify add`, `attempt handoff`, and `finish --request-review` or proposal flow.
- `tusker show <TASK-ID> --capsule`, `tusker brief`, and `tusker next` surface active attempt/runtime state when a ready task has an in-progress or handoff attempt.
- Protected-transition errors avoid suggesting a durable `active` status for V7 tasks.
- Regression covers a protected branch where `new task` succeeds, `status active` is rejected, and the capsule still gives accurate next action.

Source notes:

- `2026-05-21-codex-protected-status-handoff.md`
- `2026-05-21-wikilink-autocomplete.md`

## Story AFS-003 - Make Same-Task Proof Writes Retryable

Priority: P0

As an agent adding inline verification, I need CAS conflicts on `tusker verify add` to be self-healing or exactly retryable, so parallel or near-parallel proof writes do not leave partial proof state.

Acceptance criteria:

- A stale `verify add` CAS error prints the exact command to retry after reload, including task ID, covers, check, result, and note.
- Same-task proof mutation detects concurrent writes and either serializes with a short lock or returns a purpose-built error instead of a generic CAS message.
- A batch mode exists for multiple verification rows on one task, so agents do not need parallel `verify add` calls.
- Regression simulates two same-task verification additions and proves the final task has both rows or a deterministic retry path.

Source notes:

- `2026-05-21-codex-tusker-proof-cas.md`

## Story AFS-004 - Add a First-Class Feedback Command

Priority: P1

As an agent finishing a meaningful work turn, I need a `tusker feedback add` command that writes a small structured note, so product feedback is collected without turning into long status reports.

Acceptance criteria:

- `tusker feedback add` writes to `tusker/feedback/agents/YYYY-MM-DD-<actor>-<slug>.md`.
- Required fields are `context`, `friction`, `product-idea`, `impact`, and `related`.
- The command rejects notes over a configured line or character budget unless `--allow-long` is passed.
- The command can be called from any repo with `--repo` or `--vault`.
- `tusker validate` warns when feedback notes are missing required fields or look like progress reports instead of product feedback.

Source notes:

- `2026-05-21-codex-feedback-loop.md`
- `2026-05-21-harness-roadmap-gap-closure.md`
- `2026-05-21-h012-h018-provider-lane-hardening.md`

## Story AFS-005 - Generate Feedback Digests Across Repos

Priority: P1

As a product maintainer, I need a `tusker feedback digest` command that summarizes feedback across multiple repos, so I can periodically review agent friction without manually opening every note.

Acceptance criteria:

- `tusker feedback digest --since <date> --repo <path>...` reads feedback notes from multiple repo-local vaults.
- Output groups notes by theme, source repo, priority hint, and affected command.
- Digest flags malformed or overly long notes separately from actionable product feedback.
- Digest can emit Markdown to `tusker/feedback/digests/<date>.md`.
- The command excludes `README.md` templates and starter examples by default.

Source notes:

- All feedback notes reviewed in this pass.

## Story AFS-006 - Install and Update Feedback Templates Automatically

Priority: P1

As a Tusker operator rolling out the lightweight agent bootstrap, I need install/update to create the feedback folder and README template, so downstream repos collect feedback consistently without hand editing.

Acceptance criteria:

- `tusker install --repo`, `tusker update --repo`, and `tusker sync-repo-contract` ensure `tusker/feedback/agents/README.md` exists.
- Managed `AGENTS.md` and `CLAUDE.md` bootstrap blocks include the concise feedback instruction.
- Existing feedback notes are never overwritten.
- Re-running install/update is idempotent.
- Regression covers repos with and without an existing `tusker/SKILL.md`.

Source notes:

- `2026-05-21-codex-feedback-loop.md`
## Story AFS-007 - Provide Scoped Verification Recipes

Priority: P1

As an agent in a shared multi-worker repo, I need Tusker to expose scoped verification recipes for my owned files or lane, so proof does not fail because unrelated active edits are formatted or compiling elsewhere.

Acceptance criteria:

- Repos can define verification recipes in a small config, for example `tusker/verification-recipes.yaml`.
- `tusker proof recipe <TASK-ID>` or `tusker verify recipe <TASK-ID>` suggests commands based on touched files, task domains, and risk.
- Recipes can declare ownership scope, file globs, package/module scope, and expected noise.
- `tusker verify add` accepts `--result blocked` with `--blocked-by <path-or-task>` for unrelated compile failures.
- Documentation explains when a scoped recipe is acceptable versus when broad validation is required.

Source notes:

- `2026-05-21-worker-e-h022-h023.md`
- `2026-05-21-h022-h023-control-surface-compile-blocker.md`

## Story AFS-008 - Attribute Shared-Workspace Compile Blockers

Priority: P1

As an agent whose owned changes typecheck far enough but broad validation is blocked by another lane, I need Tusker to capture blocker ownership, so handoff is honest without making my task look unverified.

Acceptance criteria:

- `tusker verify add --result blocked` records a blocker row that does not satisfy proof but clearly attributes external ownership.
- Blocker rows can link to another task, file path, gate, or owner.
- `proof status` separates machine gaps owned by the current task from external/shared-workspace blockers.
- `finish --request-review` can proceed only when policy allows external blockers or when a gate records the blocked proof.
- Closeout status reports whether the agent should continue, create a gate, or stop for human/shared-owner action.

Source notes:

- `2026-05-21-h022-h023-control-surface-compile-blocker.md`
- `2026-05-21-worker-e-h022-h023.md`

## Story AFS-009 - Migrate Critical Root Guidance Into Project Skill

Priority: P2

As a repo maintainer adopting lightweight `AGENTS.md`, I need a supported way to move critical root guidance into `tusker/SKILL.md` or domain canon, so bootstrap cleanup does not hide non-negotiable project rules.

Acceptance criteria:

- `tusker skill audit-agent-guidance` detects non-managed guidance outside the Tusker bootstrap block before replacement.
- The command classifies guidance as project knowledge, workflow rule, verification recipe, or stale prompt baggage.
- It can write a migration draft under `tusker/feedback/agents/` or `tusker/knowledge/` for human review.
- Install/update warns when a repo lacks `tusker/SKILL.md` and root agent files are being flattened.
- Regression covers a repo with critical non-managed `AGENTS.md` content and no project skill file.

Source notes:

- `2026-05-21-codex-feedback-loop.md`
