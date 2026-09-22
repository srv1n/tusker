# Task authoring intake contract

This report is the agent-facing contract for the current Tusker intake path. It
keeps authoring decisions in a typed wave request and lets direct authoring
generate lifecycle bookkeeping and durable links.

## Minimal classified plan

The author supplies the work decision once. Model and harness names stay in
configured profiles.

```yaml
schema: tusker.wave-authoring/v1
request_key: auth-wave-v1
title: Add account sign-in
outcome: Users can sign in with the approved account flow.
spec_refs:
  - .tusker/specs/auth.md#sign-in-contract
tasks:
  - key: sign-in
    title: Implement the approved sign-in flow
    work_level: standard
    body: >-
      The sign-in flow accepts the approved account and failure states.
    implementation_notes: >-
      Surrounding work: consume the approved account contract and coordinate
      shared auth ownership here. Escalate changed product decisions,
      unavailable routing, or blocked dependencies to the architect.
    owned_paths:
      - internal/auth
human_actions:
  - key: staging-credentials
    task: sign-in
    owner: human:operator
    action: Provide the staging credential set.
    verification: The operator records the provider-ready result.
    why_agent_cannot: The credential material is controlled by the operator.
```

`work_level` is the only durable tier choice: `light` is Tier 1,
`standard` is Tier 2, and `demanding` is Tier 3. A reviewer uses the same level
unless `review_level` is deliberately supplied as an override with a
`review_reason`. A vendor model
name such as `Sol Medium` is rejected as a level. Model-specific
`runner_profile`, `execute_profile`, and `review_profile` values are rejected
in direct authoring; configure those through operator routing. A human action
uses a `human:<name>` owner and is represented by `human_actions`; it is a
blocking condition for its task and downstream closure, never a fourth model
tier.

The governing reference remains exactly `.tusker/specs/auth.md#sign-in-contract`
through import and packet projection. If an anchor is supplied, it must resolve
to a real section. Packets retain the task body, exact governing references,
dependencies, ownership, contacts, and classification.

## Generated output

Batch import returns the durable wave and task paths in its JSON receipt:

```json
{
  "ok": true,
  "inert": true,
  "wave": {
    "waveId": "W-0001",
    "wavePath": ".tusker/work/waves/W-0001.md",
    "taskPaths": {"sign-in": ".tusker/work/tasks/TSK-T-0001.md"}
  }
}
```

The wave body contains an intended result, linked members, DAG-ordered stages,
human blockers, and closure requirements. An eligible re-import deterministically
replays the immutable receipt while retaining authored wave prose, the wave
identity, and progressed lifecycle fields. A standalone `new task` record
remains valid without a synthetic wave.

## Omitted empty metadata

Only newly created CLI and imported task records omit these empty optional
values, before `state_rev` is calculated:

| Field | Why omission is safe |
|---|---|
| `architect`, `origin` | Optional identity overrides; empty means no verified binding. |
| `runner_profile`, `concurrency_group` | Optional routing/resource overrides; runtime configuration supplies defaults. |
| `peer_contacts` | No peers means no contact map. |
| `domains`, `dependencies`, `evidence_required`, `knowledge_nodes`, `owned_paths` | Empty lists carry no contract facts. |
| `gates` | No named gate is represented by an absent list. |

The writer retains explicit `evidence_budget: 0`, `raw_artifacts_allowed: false`,
`next_source`, `next_ref`, proof policy, timestamps, revisions, and all supplied
non-empty overrides. Existing imported or legacy records are not blanket-pruned.
Unknown direct-authoring keys remain strict decode errors so a misspelled
contract cannot silently disappear.

## Readiness rejection

A fresh direct-authoring agent task without classification fails validation with
the precise authoring gap:

```text
<task key>: work_level is required for agent work; use light, standard, or demanding
```

The request remains inspectable before any records are written. Add a valid
level, or model the operator-owned action as a complete named `human_actions`
record with a `human:<name>` owner; do not put a vendor model name or runtime
`next_owner` value in the tier field. `implementation_notes` is projected into
the task body as `## Implementation notes`; an existing body section is
replaced by the explicit field rather than duplicated.
