# Workflow

Short reference for the story lifecycle. `SKILL.md` has the full decision tree and commands; this file covers mechanics that agents need while working a ticket.

## Canonical lifecycle

```
intake → active → (blocked ↔ active) → in_review[verification_requested]
                                  ↘ cancelled               ↓
                                              in_review[requested] → done
                                                          ↘ rework → active
```

Every transition writes a row to the note's `transitions[]` frontmatter array (from Symphony borrowings):

```yaml
transitions:
  - from: "intake"
    to: "active"
    at: "2026-04-21T14:02:11Z"
    by: "sarav"
    reason: null
```

Never hand-edit `transitions[]`. Use `tusker set-status` (or `tusker pickup` / `tusker release` for dispatcher-owned transitions).

## Status stamp fields

`set-status` also writes one dedicated date field per transition so Bases views can sort without parsing the audit log:

- `started` — first entry to `active` (set-once; preserved across later transitions)
- `review_opened` — most recent entry to `in_review`
- `completed` — entry to `done`
- `cancelled_at` — entry to `cancelled`
- `blocked_since` — most recent entry to `blocked` (cleared when leaving `blocked`)

## Dispatch state (Symphony borrowings)

Orthogonal to `status`. Tracks whether an agent run is in flight.

- `dispatch_state: unclaimed` — available for pickup. `status: active` + nothing claimed.
- `dispatch_state: claimed` — a dispatcher has atomically claimed this ticket. `claimed_by` + `claimed_at` set. Agent session hasn't started yet, or is starting.
- `dispatch_state: running` — the spawned agent is actively working.
- `dispatch_state: done` — the run finished cleanly.
- `dispatch_state: failed` — the run ended unsuccessfully and needs classification.
- `dispatch_state: stalled` — heartbeat missing / stuck. Dispatcher marks and moves on; a human can retry.
- `dispatch_state: cancelled` — the run was intentionally stopped.

Commands:

- `tusker pickup --id <ID> --by <agent>` — atomic. Loser of a race gets `ALREADY_CLAIMED`.
- `tusker release --id <ID> --to <state>` — advance dispatch_state. `--failure-class transient|deterministic|stuck|blocked-by-human|budget-exceeded` is allowed when releasing to `stalled`/`failed`.

The dispatcher binary (`dist/tusker`, built via `go build -o dist/tusker ./cmd/tusker`) reads `_system/generated/dashboard.json` and drives this loop. See `docs/DISPATCHER_PSEUDOCODE.md`.

## Gates

- `validate` is the full note-integrity pass.
- worker success or `set-status active → in_review`: evidence gate must pass.
- `in_review + verification_requested → in_review + requested`: verifier confirms the ticket matches the current tree.
- `in_review → done`: `set-status` enforces attestation per risk table, with signoff for critical.
- epic `done`: `set-status` refuses if child stories/bugs are unfinished.

## Dependency semantics

Tusker uses relation fields, not a separate prerequisite object:

- `blocked_by` = this story or bug depends on these work items
- `blocks` = these work items depend on this one

State rules:

- `intake` + unmet `blocked_by` is normal. The item exists, but it is not ready.
- `blocked` means execution was underway and progress stopped on a real blocker.
- When decomposing a spec into several stories, wire dependency links when the stories are created, not later after the board turns into a mess.

Operational rule: run `tusker validate` before lifecycle moves. Then run `tusker set-status` for the transition itself.

## Failure classes

When `release --to stalled|failed` is called, use one of these as `failure_class`:

- `transient` — flaky network, external service hiccup. Safe to retry.
- `deterministic` — code defect or logic error. Retry without changes will not help.
- `stuck` — no heartbeat, no progress. Likely needs human to inspect.
- `blocked-by-human` — waiting on human approval, input, or product decision.
- `budget-exceeded` — hit time, token, or cost limit.

See `docs/FAILURE_CLASSES.md` for full taxonomy and retry policy.

## Demo flow

If `change_type: feature` + `surfaces` contains a UI surface (`frontend`/`desktop`/`mobile`) + `risk ≥ medium`, `## Evidence` MUST contain a demo asset (video/gif/screenshot) at `in_review`/`done`. Validator fails otherwise.

Assets live in `Attachments/<STORY-ID>/`. Use `tusker attach-evidence --kind video|screenshot|gif ...` to copy + link + log.

## Workpad, review, and rework

For ORC-style agent runs, use one `## Workpad` section as the live progress surface. Edit that section in place across turns; do not bury current state in append-only work logs. The workpad should carry the current plan, checklist, assumptions, blockers, verification notes, and draft follow-ups.

End-of-run proof belongs under `## Evidence` as a review packet: changed files, diff summary, commands and results, artifacts, risks, and follow-ups. The workpad says what is happening now; the review packet says why the result should be trusted.

PR feedback sweep:

1. Classify unresolved feedback as `must-fix`, `question`, `nit`, or `follow-up`.
2. Track the sweep in the workpad checklist.
3. Fix in-scope `must-fix` items before requesting review again.
4. Draft out-of-scope work as follow-up notes in `intake`; do not silently expand scope.

Rework reset:

- Keep prior evidence.
- Rewrite the workpad around the new plan.
- Increment `work_revision` when acceptance criteria or scope changed.
- Start a new attempt for the new revision. Do not resume an old runner session across changed scope.

See `docs/ORCHESTRATION_RUNBOOK.md` for the full operator runbook.

## Decision promotion

When a story lands a durable architectural decision:

```
tusker promote-decision --id <STORY-ID> --summary "<one-line>" --target architecture
```

Appends a dated bullet to `architecture.md` with a wikilink back to the story. The full rationale stays in the story's `## Decision` section.

## Conventions

- Wikilinks for cross-references. Never copy-paste context between notes.
- Every attachment lives under `Attachments/` (vault-root or per-story subfolder).
- `_system/generated/` is derived; never edit by hand — `tusker reindex` rewrites it.
- Prefer `tusker list --json` + piping for scripted workflows; hand-parsing markdown is a regression.

## Operator intervention

When a run gets stuck and the CLI won't let you out (e.g. retry budget exhausted, dispatcher crashed mid-transition), see `docs/OPERATOR_INTERVENTION.md` for the small set of manual overrides that are safe, and the audit expectations that go with them.
