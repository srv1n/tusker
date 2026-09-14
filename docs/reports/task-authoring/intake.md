# Task authoring intake contract

This report is the agent-facing contract for the current Tusker intake path. It
keeps planning decisions in the typed delivery plan and lets import generate
lifecycle bookkeeping and durable links.

## Minimal classified plan

The author supplies the work decision once. Model and harness names stay in
configured profiles.

```yaml
schema: tusker.delivery-plan/v2
scope: auth-rollout
title: Add account sign-in
spec_refs:
  - .tusker/specs/auth.md#sign-in-contract
context_fingerprint: sha256:<64 lowercase hex>
factory_intake_contract_schema: tusker.factory-intake-contract/v1
factory_intake_contract_version: <current contract version>
factory_intake_contract_fingerprint: sha256:<64 lowercase hex>
epic: APP
requirements:
  - id: R1
    outcome: A user can sign in with the approved account flow.
tasks:
  - source_key: sign-in
    requirement_refs: [R1]
    title: Implement the approved sign-in flow
    outcome: The sign-in flow accepts the approved account and failure states.
    implementation_notes: >-
      Surrounding work: consume the approved account contract and coordinate
      shared auth ownership here. Escalate changed product decisions,
      unavailable routing, or blocked dependencies to the architect.
    work_level: standard
    acceptance:
      - id: A1
        outcome: The approved account reaches the signed-in state.
    verification:
      - covers: A1
        check: command: go test ./internal/auth -run TestSignIn -count=1
    artifact:
      kind: behavior_matrix
      path: docs/reports/auth/sign-in.md
      summary: Sign-in behavior and proof matrix.
      acceptance_ids: [A1]
    owned_paths: [internal/auth]
human_gates:
  - source_key: staging-credentials
    title: Provide staging credentials
    kind: credentials
    owner: human:operator
    task_source_key: sign-in
    acceptance_ids: [A1]
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
in V2 authoring; configure those through operator routing. A human action is represented
by `human_gates`; it is a blocking condition for its task and downstream
closure, never a fourth model tier.

The governing reference remains exactly `.tusker/specs/auth.md#sign-in-contract`
through import and packet projection. If an anchor is supplied, it must resolve
to a real section. Packets retain the task body, exact governing references,
dependencies, ownership, contacts, and classification.

## Generated output

Batch import returns the durable wave and task paths in its JSON receipt:

```json
{
  "wavePath": ".tusker/work/waves/W-0001.md",
  "taskPaths": {"sign-in": ".tusker/work/tasks/APP-T-0001.md"}
}
```

The wave body contains an intended result, linked members, DAG-ordered stages,
human or product blockers, setup status, closure requirements, and non-goals.
An eligible re-import deterministically regenerates the managed brief while
retaining authored wave prose outside that block, the wave identity, and
progressed lifecycle fields. A standalone `new task` record remains valid
without a synthetic wave.

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
Unknown V2 plan keys remain strict decode errors so a misspelled contract cannot
silently disappear.

## Readiness rejection

A fresh V2 agent task without classification fails validation with the precise
authoring gap:

```text
<source_key>: work_level is required for agent work; use light, standard, or demanding
```

The plan remains inspectable as a held draft. Add a valid level, or model the
operator-owned action as a complete named `human_gates` record; do not put a
vendor model name or runtime `next_owner` value in the tier field. For
compatibility, an older task whose outcome already contains an `##
Implementation notes` section satisfies the notes projection; new V2 plans
should use `implementation_notes` directly.
