# Story decomposition

A design doc is not executable work.

Tusker expects a large RFC or design note to be turned into:

1. one epic that names the workstream
2. one canonical spec source
3. multiple stories that are individually executable

If the story title is effectively "implement the RFC", the split failed.

## Hard rules

- Split by mergeable semantic slice, not by person, team, or random file ownership.
- Put contract and state changes before shell polish and projections.
- Separate delegated/adaptor work when authority or provenance semantics differ.
- Keep migration/cleanup as an explicit story when leaving old behavior around would create two truths.
- A story must be understandable without rereading the whole RFC, but it must cite canon instead of copying the RFC body.
- If one story needs multiple PRs by design, it is usually two or more stories.

## What makes a good story

A good story has:

- one primary deliverable
- one reviewable acceptance packet
- one proof plan
- a clear stop line

Strong smell list:

- title starts with "Complete", "Finish", or "Implement all of"
- `## Canon` says "see RFC" and nothing else
- `## Code anchors` points at half the repo
- acceptance criteria are just "matches the spec"
- the agent handoff packet is mostly blank

## Default split order for load-bearing specs

Use this stack unless the spec clearly calls for a different order:

1. contract / shared types / protocol
2. durable state / persistence / storage truth
3. native runtime integration
4. delegated parity / adapters / external authority
5. shell projections / UX / operator surfaces
6. proof pack / migration cleanup / residual kill list

This is not ceremony for its own sake. It keeps later stories from building on mush.

## Decomposition algorithm

### 1. Find the canon

Pick one source:

- vault doc note, e.g. `[[HIT-D-0001]]`
- repo RFC path, e.g. `docs/specs/HUMAN_REQUEST_CONTROL_PLANE_RFC_V1.md`

Put that in epic `spec_source:` or the epic `## Design` section.

### 2. Extract frozen decisions

Pull out the decisions that no implementation story should reopen:

- vocabulary
- invariants
- policy precedence
- what gets deleted
- what is explicitly deferred

These become the story `## Canon` bullets and `Do not change` lines.

### 3. Identify semantic slices

Group work by semantics, not by file tree:

- contract
- storage
- runtime
- adapters
- shell
- proof

If a slice has its own risk profile or stop conditions, it deserves its own story.

### 4. Size and risk each slice

- `size` = execution effort
- `risk` = review/evidence burden

Do not use `xl` as an excuse to keep a bloated story. `xl` is often your cue to split again.

### 5. Write execution-grade stories

For each story, fill:

- `## Problem`
- `## Acceptance criteria`
- `## Canon`
- `## Code anchors`
- `## Plan`
- `## Verification plan`
- `## Agent handoff`

For risk `high` or `critical`, also fill:

- `## Considered and rejected`
- `## Decision`
- `## Rollout`

### 6. Only then mark stories active

Do not move a story to `active` if another agent would still need to rediscover the actual implementation contract.

## Worked example — HumanRequest control plane RFC

Canonical spec:

- `docs/specs/HUMAN_REQUEST_CONTROL_PLANE_RFC_V1.md`
- companion approval chapter: `docs/specs/HARNESS_APPROVAL_EXECUTION_GATE_AND_TRANSPORT_RFC_V1.md`

Recommended epic:

- `HIT` or `HRQ`, depending on existing vault vocabulary

Recommended story stack:

| Story | Why it is separate | Size | Risk | Delegation |
| --- | --- | --- | --- | --- |
| `HIT-S-0001` — Freeze shared `HumanRequest` types and app protocol | later stories should not keep debating nouns or transport shape | `m` | `high` | `execute` |
| `HIT-S-0002` — Add durable `human_requests` and `human_request_events` backing | persistence truth is its own seam and should land before shell migration | `l` | `high` | `execute` |
| `HIT-S-0003` — Migrate native approval and user-input creation onto the new store | native runtime integration is reviewable on its own | `l` | `high` | `execute` |
| `HIT-S-0004` — Converge approval and user-input adapters onto `human_request.respond` | transport normalization is a distinct contract slice | `m` | `high` | `execute` |
| `HIT-S-0005` — Fix delegated parity for Codex / external CLI paths | delegated authority and fake input behavior are their own risk bucket | `m` | `high` | `execute` |
| `HIT-S-0006` — Build desktop pending-request projections from the new backing store | shell projection should follow canonical truth, not define it | `m` | `medium` | `execute` |
| `HIT-S-0007` — Remove split pending truth and ship proof pack | cleanup and evidence deserve an explicit stop line | `m` | `high` | `execute` |

Suggested canon bullets for `HIT-S-0001`:

- umbrella seam is `HumanRequest`, not approval-only architecture
- no backward compatibility promise for `interactions` or split respond routes
- phase-1 request kinds are `approval` and `user_input`
- `HumanRequest` is control-plane truth, not a model-injected meta-tool

Suggested code anchors for the stack:

- shared types / protocol:
  - `rzn-runtime/crates/types/src/harness.rs`
  - `rzn-runtime/crates/app-protocol/src/lib.rs`
- durable backing:
  - `src-tauri/src/host/server.rs`
  - `src-tauri/src/host/server/persistence.rs`
  - `src-tauri/src/state_store.rs`
- native migration:
  - `src-tauri/src/commands/hitl.rs`
  - `src-tauri/src/commands/tool_approvals_cmd.rs`
  - `src-tauri/src/tools/approval_runtime.rs`
- delegated parity:
  - `src-tauri/src/commands/cli_agent.rs`
  - `src-tauri/src/commands/external_cli_chats.rs`
- shell projections:
  - `src/lib/events/types.ts`
  - `src/lib/events/unifiedReducer.ts`

## Anti-patterns

Do not do this:

- one epic with one giant story called "HumanRequest control plane"
- one story for "backend" and one story for "frontend" when the real split is contract vs projection
- a story that mixes storage migration with UX polish and delegated runtime cleanup
- a story whose only acceptance criterion is "RFC implemented"

The point of Tusker is to make handoff boring, not heroic.
