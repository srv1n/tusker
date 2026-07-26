---
capsule:
  what: "Defines optional scope-qualified V2 delivery dependencies that converge to durable task-ID edges without widening execution authority."
  use_when:
    - "A V2 delivery plan must depend on a source-keyed task imported by another plan scope."
    - "Changing delivery import, dependency validation, wave preflight, gate ordering, or delivery review."
  skip_when:
    - "A plan has only local dependencies or a caller needs no cross-scope composition."
---

# Cross-scope factory convergence

Status: proposed implementation contract  
Date: 2026-07-26

## 1. Outcome

V2 delivery plans can express a hard dependency on a task owned by a
different stable delivery scope. The reference resolves deterministically by
`(scope, delivery_source_key)`, then becomes the same ordinary durable task-ID
edge used by local dependencies. Re-imports converge; no task is dynamically
rebound by a later source-key collision or edit.

This is composition, not an authority feature. It does not enable automation,
arm a wave, dispatch work, satisfy a gate, move a ref, release, spend money, or
alter V1 behavior.

## 2. Terms and invariants

| Term | Meaning |
| --- | --- |
| plan scope | Stable `scope` at the root of a V2 plan. |
| local reference | A dependency without `scope`; it resolves in the importing plan's scope exactly as today. |
| qualified reference | A hard dependency with `scope`; it resolves in that explicit producer scope. |
| semantic target | The author-facing pair `(scope, delivery_source_key)`. |
| durable target | The allocated producer task ID stored in the consumer's ordinary dependency edge. |
| cross-scope projection | `delivery_cross_scope_dependencies`: durable metadata beside an ordinary edge exposing the semantic target that resolved to it. |
| material epoch | The checked snapshot of every participating scope's source-key map, task contract/state revisions, and dependency graph used by one import transaction. |

The following invariants are non-negotiable:

1. V1 decoding, validation, import, reconciliation, fingerprinting, review,
   and task records remain byte-for-byte compatible in meaning. V1 never gains
   source-key resolution outside its own current behavior.
2. Omitting `scope` preserves local V2 semantics. `scope` may not be blank,
   self-contradictory, malformed, or used to select an arbitrary task ID.
3. A qualified reference identifies one existing non-obsolete producer task by
   `(delivery_plan_scope, delivery_source_key)`. Zero or multiple matches fail
   closed with a stable finding; an importer never guesses.
4. The task record consumed by the scheduler contains the producer's normal
   durable ID. Runtime eligibility reads that ordinary edge; it never searches
   plans, scopes, or source keys.
5. Provenance is explanatory and integrity-checked, not a second dependency
   mechanism. Its mismatch with the durable edge is corruption, not a reason to
   silently re-resolve or repair an edge.
6. The global dependency graph is acyclic. Its authority and lifecycle
   checks are global too; scope is not an isolation escape hatch.

## 3. V2 authoring shape

Only V2 receives the optional field:

```yaml
tasks:
  - source_key: low-risk-authoritative-dogfood
    dependencies:
      - task: provider-cutover-doctor
        scope: full-gate-lifecycle-provider/v1
        kind: hard
      - task: local-proof-summary
        kind: soft
```

`task` remains a stable `delivery_source_key`, never a task ID. With no
`scope`, the resolver uses the consumer plan's root scope and local V2 keeps
its existing hard/soft semantics. With `scope`, the resolver uses the supplied
producer scope and `kind` **must** be `hard`. A scope-qualified `soft` edge is
rejected in this initial contract; cross-wave soft relock is intentionally out
of scope. Unknown fields and unknown kinds remain strict-decode errors.

The source key is deliberately not globally unique. The only stable semantic
identity is the pair:

```text
target scope + ":" + target delivery_source_key
```

The import writes one ordinary edge and one projection, conceptually:

```yaml
dependencies: [ORC-T-0142:hard]
delivery_cross_scope_dependencies:
  - scope: full-gate-lifecycle-provider/v1
    task: provider-cutover-doctor
    task_id: ORC-T-0142
    kind: hard
    target_contract_fingerprint: sha256:...
```

`delivery_cross_scope_dependencies` is the exact durable projection name and
has exactly the shown fields: `scope`, `task`, `task_id`, `kind`, and
`target_contract_fingerprint`. The ordinary `ORC-T-0142:hard` edge is canonical for all
eligibility, closure, discard, and integration operations. The projection is
shown in review/Serve and checked on read/import; it is not sufficient to
create eligibility by itself. It deliberately does **not** persist
`target_state_rev`: normal producer lifecycle changes must not make every
consumer look corrupt or stale.

## 4. Resolution and producer-before-consumer import

An import builds a candidate source-key map from the requested consumer plan
and the current global task index. It resolves every local or qualified
dependency before allocating/writing any consumer task.

For a qualified reference, the producer scope must already be importable and
present in the same vault before the consumer is imported. The producer plan is
therefore imported/reconciled first; the consumer import does not recursively
import another plan, read an arbitrary YAML file, or invent a producer task.
This keeps every mutation attributable to one reviewed plan.

Resolution algorithm:

1. Normalize and validate the consumer scope, optional target scope, source
   key, and kind.
2. Derive `target_scope = dependency.scope || consumer_plan.scope`.
3. Look up exactly one current task with `delivery_plan_scope=target_scope` and
   `delivery_source_key=dependency.task`.
4. Refuse absent, duplicate, obsolete, terminally discarded, wrong-project, or
   stale-provenance targets with stable codes and a repair that names the
   producer scope/key.
5. Materialize the normal ID edge plus semantic provenance. A repeated import
   of unchanged material is idempotent.

There is no cross-vault or cross-project form. A separate project has an
independent task-ID namespace and requires an explicit higher-level protocol,
not this field.

## 5. Atomic global validation and convergence

Cross-scope import operates in a single transaction over an exact material
epoch. Acquire every participating scope/document lock in normalized lexical
scope order, read a fresh global source-key and dependency projection, validate
the proposed replacement, write all affected consumer records, and commit only
if every read revision still matches. Release in reverse order. A retry begins
with a new epoch; it never patches a partially stale view.

The transaction must validate all of the following before writes:

| Check | Required behavior |
| --- | --- |
| Global cycles | Traverse all current dependency edges (local hard/soft and qualified hard) plus proposed replacements, including incoming and outgoing cross-scope edges. Reject the complete cycle, not merely the importing plan's local subgraph. |
| Inbound removal | If a re-import removes, obsoletes, or changes the semantic identity of a producer task with any inbound durable edge from another scope, refuse before mutation and name every affected consumer. A producer cannot strand consumers. |
| Stale target | If the producer task's ID, contract fingerprint, state revision, scope, source key, or non-obsolete status differs from the epoch read during resolution, abort/retry. `state_rev` is an ephemeral compare-and-swap input only; it is never persisted in `delivery_cross_scope_dependencies`. Never rebind to a similarly named task. |
| Target rewrite | A producer contract may change only when every existing inbound consumer still names the same semantic target and the new target satisfies the edge's validation policy; update each affected `target_contract_fingerprint` in the same transaction or refuse with the inbound closure. |
| Atomicity | Failure, crash, lock loss, serialization failure, or compare-and-swap conflict leaves no new task, no rewritten dependency, and no partial `delivery_cross_scope_dependencies` projection. Recovery replays or rolls back idempotently. |

This protects the dangerous race: producer `P/x` is validated, another import
removes/replaces it, and consumer `C/y` would otherwise commit an edge pointing
at a stale or semantically different target. The epoch/CAS guard makes that
race a retry or refusal, never a hidden rebinding.

## 6. Eligibility, wave preflight, and gates

A structurally resolved cross-scope edge participates in the existing
dependency model exactly like a local durable edge:

- a hard consumer remains blocked until its producer reaches the existing
  integrated-done condition;
- all qualified cross-scope edges are hard-only; local soft edges retain their
  current provisional-unlock/relock behavior without gaining cross-wave scope;
- a producer terminal failure, discard, stale contract, or gate block projects
  a precise blocker through its consumer closure;
- unrelated tasks and waves remain independent.

An unresolved *state* is not an unresolved *reference*. After a qualified edge
has been validated and materialized, its producer may still be incomplete.
That must not prevent the consumer's otherwise valid wave from being armed:
arm/preflight validates graph integrity and authority, not completion of every
hard predecessor. The exact consumer task remains blocked and cannot be
claimed until its normal dependency condition is met.

Gate ordering is similarly explicit:

| Gate class | May be satisfied before hard dependencies? | Reason |
| --- | --- | --- |
| `setup` | Yes, when it only prepares a host, runtime, credential, or other external prerequisite and grants no task/release authority. | Setup must remain usable before producer work completes. |
| `auth` | No. | An authorization cannot be banked before the protected hard dependency closure exists and is complete. |
| `release` | No. | Release proof/authority must bind the completed hard dependency closure and current target material. |
| other blocking gate | Existing policy, but never used to bypass a hard dependency. | Dependency closure remains authoritative. |

The gate command and all alternate API/Serve paths must enforce the same
ordering with a stable blocker code. A previously satisfied auth/release gate
becomes stale when a relevant hard target is rewired or its material epoch
changes; it does not remain a reusable authority receipt.

## 7. Review and operator surface

Delivery review, wave preflight, task packets, Serve, and status projections
show both durable projection identities and join the producer's **current**
lifecycle state live for every qualified edge:

```text
Hard dependency: full-gate-lifecycle-provider/v1/provider-cutover-doctor
Durable target: ORC-T-0142 (integrated; contract sha256:…)
```

When blocked, the surface names the semantic producer and durable ID, its
state, the dependency kind, whether the block is structural or lifecycle, and
one exact repair. It must not make a user inspect raw frontmatter to distinguish
"missing producer" from "producer not done".

Product review includes cross-scope producer-before-consumer ordering, global
cycle/inbound implications, expected blocked consumer frontiers, and the fact
that import/review/preflight are inert. It never presents a Start action as
permission to satisfy gates or enable automation.

## 8. Hybrid program convergence after plans 12, 14, and 15

The initial implementation does not modify existing plans. After the schema,
atomic import, preflight/gate, and review work has landed, plans 12, 14, and 15
keep independent shadow implementation. A new Plan 17 program-cutover scope is
the sole cross-scope consumer and is imported last. This avoids reciprocal
producer-import dependencies and puts authority/release decisions in one
release-manager surface.

1. `scheduled-promotion/v1` (plan 12) gains
   `scheduled-promotion-shadow-ready`: an objective, default-off readiness
   task after the existing staged-promotion/recovery evidence. It grants no
   release or promotion authority.
2. `factory-execution-control/v1` (plan 14) gains
   `factory-control-shadow-ready`: objective, independent shadow proof with no
   authority transfer.
3. `full-gate-lifecycle-provider/v1` (plan 15) exposes provider-doctor shadow
   readiness without a host runtime. Its existing `provider-live-smoke` stays
   promote readiness, remains behind its `setup` gate, and is not mislabeled as
   a shadow-ready node.
4. New `program-cutover-convergence/v1` (Plan 17) is authored and imported
   after those producers. Its DAG is `producer shadow tasks` to
   `shadow-convergence` to `low-risk-authoritative-dogfood` to
   `production-promotion-handoff`. `low-risk-authoritative-dogfood` has hard
   qualified dependencies on all three shadow producers **and** plan 15's
   `provider-live-smoke`, and carries the only human `auth` gate. The final
   handoff carries the human `release` gate.

The target topology is:

```text
plan 12: scheduled-promotion-shadow-ready ─┐
plan 14: factory-control-shadow-ready ──────┼─> plan 17: shadow-convergence
plan 15: provider-doctor shadow readiness ──┘                       │
plan 15: provider-live-smoke ───────────────────────────────────────┤
                                                                     v
                    plan 17: low-risk-authoritative-dogfood [auth]
                                                                     │
                                                                     v
                    plan 17: production-promotion-handoff [release]
```

Plan 17 is reviewed/imported only after plans 12, 14, and 15 are each reviewed
and imported. The migration task owns those plans plus Plan 17's spec/plan, so
no concurrent lane edits their shared contracts. It moves or obsoletes the old
plan-12/plan-14 authority gates only in that reviewed, Plan-17-last migration;
shadow work remains unblocked before then.

## 9. Non-goals

- No default-on automation, daemon launch, wave arm, runtime setup, release,
  default-branch movement, paid model invocation, or notification authority.
- No V1 migration, V1 field extension, or V1 semantic change.
- No cross-project/vault edges, arbitrary task-ID references, remote plan
  import, implicit producer import, or automatic scope discovery.
- No removal of ordinary task-ID edges in favor of a source-key graph.
- No human gate in this delivery merely to approve code whose behavior is
  already decided here. This plan grants no runtime authority.

## 10. Acceptance matrix

| ID | Observable requirement |
| --- | --- |
| R1 | V2 plans can name a stable producer task in another scope without replacing the durable task-ID dependency model. |
| R2 | Cross-scope import globally validates and atomically converges under concurrent producer/consumer changes without cycles, stale targets, inbound stranding, or partial writes. |
| R3 | Preflight, eligibility, and gate ordering retain default-off authority boundaries while correctly projecting cross-scope dependency state. |
| R4 | Review and Serve show an operator both the semantic producer and durable task target with one actionable blocker or repair. |
| R5 | Plans 12, 14, and 15 retain independent shadow implementation while a new Plan 17 convergence scope composes their producer shadow tasks into an explicit, human-gated authority and release path. |
| R6 | A temporary-vault end-to-end fixture proves convergence, failure handling, and no-side-effect boundaries without a live daemon or external runtime. |

## 11. Delivery and proof

`docs/plans/16-cross-scope-factory-convergence-v2.yaml` is the still-inert,
two-lane delivery DAG. It contains no human gate because all implementation
facts are decided by this contract and it grants no runtime authority. Its
focused proof is deliberately fixture-driven in a temporary vault; no live
daemon, production vault, real runtime, release, or broad repository test is
required to author, doctor, review, or dry-run the plan.
