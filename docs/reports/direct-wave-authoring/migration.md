# Delivery-plan to direct-wave migration

`migrate direct-waves` converts legacy delivery-plan material — durable
tasks/waves/gates carrying `delivery_*` fields, and unimported
`tusker.delivery-plan/v2` files under `.tusker/specs/` — into canonical
direct-wave/task records. Inspection is read-only; apply is gated by an
explicit confirmation fingerprint.

## Commands

```bash
tusker migrate direct-waves inspect [--json]
tusker migrate direct-waves apply --confirm sha256:<inspection-fingerprint> \
    [--allow-partial] [--fail-after-unit <n>] [--json]
```

- `inspect` writes nothing: no namespaces, scratch files, reports,
  receipts, or runtime rows. It emits a `tusker.direct-wave-migration/v1`
  report whose `fingerprint` covers the sorted disposition list.
- `apply` reruns inspection under the material epoch lock and refuses if
  the fingerprint changed. Default apply is all-or-nothing: every
  convertible unit's record and migrated-event writes are prepared and
  merged first, conflicting writes are rejected, and a single guarded
  commit lands them together — a preparation or write failure leaves every
  unit byte-identical with no partial records or events.
- `--allow-partial` commits unit-by-unit — each unit's records and events
  in one transaction — converting unrelated units while leaving blocked
  units (e.g. `ACTIVE_ATTEMPT`) untouched.
- `--fail-after-unit <n>` deterministically fails after `n` committed
  units for interruption testing; it is valid only with `--allow-partial`.
  Each unit is transactional and idempotent, so re-running
  `inspect`/`apply` resumes without duplicate IDs, gates, waves, evidence,
  or runtime rows.

### Restart example

```bash
tusker migrate direct-waves apply --confirm sha256:AAA... --allow-partial --fail-after-unit 1
# fails after the first committed unit
tusker migrate direct-waves inspect          # new fingerprint reflects remaining units
tusker migrate direct-waves apply --confirm sha256:BBB... --allow-partial
# remaining units convert; already-converted units report already_current
```

## Report

```yaml
schema: tusker.direct-wave-migration/v1
fingerprint: sha256:...
ready: true|false
dispositions:
  - unit: W-0009                  # durable ID, spec file name, or gate:/epic:<id>
    kind: wave|standalone_task|plan_input|workflow_policy
    source: <relative path>
    action: convert|materialize|preserve_historical|delete_after_conversion|blocked|already_current
    targets: [<record paths>]
    blockers: [<exact repair strings>]
```

Units, targets, and blockers are sorted before fingerprinting, so the
report and fingerprint are deterministic. All unit, source, and target
paths are repo-relative, so the fingerprint does not vary with checkout
location.

## Legacy membership inventory

Every legacy task is accounted for exactly once. Inspection builds the
forward (`task.wave`) and reverse (`wave.members`) associations before
constructing units. A consistent association — `task.wave` names an
existing wave, that wave lists the task exactly once, and no other wave
lists it — places the task in that wave's unit. Anything else produces an
explicit blocked disposition naming the task and the exact defect:

- `WAVE_ASSOCIATION_MISSING <task>` — `task.wave` names a wave that does
  not exist.
- `WAVE_ASSOCIATION_MISMATCH <task>` — `task.wave` names a wave that does
  not list the task.
- `WAVE_ASSOCIATION_REVERSE_ONLY <task>` — a wave lists the task but the
  task names no wave; the association is blocked, never inferred.
- `WAVE_ASSOCIATION_DUPLICATE <task>` — the wave lists the task more than
  once.
- `WAVE_ASSOCIATION_COMPETING <task>` — more than one wave lists the task.

A task with no forward wave and no reverse membership is a standalone
unit. No task is ever silently omitted or assigned to a guessed wave.

## Migration receipts

Materialized and converted waves carry an immutable historical receipt in
frontmatter:

```yaml
migration_receipts:
  - schema: tusker.migration-receipt/v1
    source_kind: delivery_plan_v2
    source_fingerprint: sha256:<fingerprint of the exact input bytes, or the imported reviewed fingerprint>
    target_wave_id: W-0001
    target_task_ids: [ABC-T-0001, ...]
    target_gate_ids: [ABC-G-0001, ...]
```

The receipt is evidence only — never runtime or start authority.
Inspection matches a still-present plan file to a receipt by
`source_fingerprint`, then validates the receipt's immutable identity:
`target_wave_id` equals the wave ID, the task target count equals the
plan task count, every target task/gate exists, each target task is still
a member of the receipt wave with a matching `wave` field, and
`plan.human_gates[i]` maps positionally to `target_gate_ids[i]` — that
exact gate must still block the mapped task (a swapped or moved gate
blocks with `PLAN_RECEIPT_TARGET_DRIFTED`). Receipt `target_task_ids`
correspond positionally to `plan.tasks` — identity is the immutable
receipt target list; mutable body text is never rematched.

When a plan file's fingerprint matches no receipt, an explicit one-time
`--map-plan <repo-relative-path>=<WAVE-ID>` flag (CSV for several) on
`inspect` and `apply` names the wave the file was converted into. The
mapping is transient CLI input — never persisted — and apply reruns
inspection under the same map, so the confirmation fingerprint covers it.
The named wave must hold exactly one `delivery_plan_v2` receipt, be fully
converted (no legacy fields on wave/targets), share the plan title, and
satisfy the positional task title/count, exact wave membership/`wave`
field, and positional gate associations — any mismatch blocks with
`PLAN_MAPPING_INVALID`; a valid mapping reports `delete_after_conversion`
and apply writes no records for it. Missing mappings continue ordinary
materialize behavior; nothing is inferred by title.

When the wave and every receipt target carry none of the legacy plan-only
fields migration removes (`directWaveConversionComplete`), no content-drift
check runs: a current direct task fingerprint/body may have changed, and
that is durable amendment — the source fingerprint plus the immutable
receipt plus intact associations prove the plan's content was
materialized, so the file remains `delete_after_conversion` and
member reordering does not disturb it. If any receipt target still
carries legacy fields (pre-conversion imported material), the
fail-closed positional content validation runs and the unit blocks with
`PLAN_CONTENT_DRIFT`/`PLAN_CONVERSION_INCOMPLETE` — an old import receipt
can never authorize deletion before conversion. Missing, moved, wrong-wave,
unlinked-gate, ambiguous, or corrupt receipts still block precisely. This
is what makes apply idempotent: after conversion the durable records no
longer carry scope/source keys, so the receipt — not the removed fields —
proves the file was already imported.

## Field mapping

Legacy durable fields are removed from runtime records and replaced by:

| Legacy | Canonical |
| --- | --- |
| `delivery_plan_scope`, `delivery_source_key`, `delivery_plan_fingerprint`, `context_fingerprint`, factory/context fields | removed (provenance lives in history, not runtime lookups) |
| `delivery_contract_fingerprint` | `contract_fingerprint` — recomputed from instructions, acceptance, verification, proof requirements, dependencies, gates, work/review classification, owned paths, and generated outputs; stable across status/readiness/`state_rev`/timestamp changes |
| `delivery_cross_scope_dependencies` | ordinary `dependencies` (`TASK-ID:hard|soft`) plus `dependency_contracts` (`task_id`, `kind`, `target_contract_fingerprint`) — resolved only when scope+source_key identifies exactly one durable task. A missing target record is not a blocker when exactly one legacy row pins that `task_id` with a matching hard/soft kind and nonempty `target_contract_fingerprint`; conversion writes the canonical edge and the pinned fingerprint into `dependency_contracts` while the absent target record stays absent, so the task remains non-runnable and direct review reports the dependency target missing. Duplicate, kind-conflicting, or malformed pins block (`DEPENDENCY_TARGET_AMBIGUOUS`, `DEPENDENCY_TARGET_MISSING`, `CROSS_SCOPE_PROJECTION_INVALID`); migration never creates the target |
| `delivery_proof_contract` (+ fingerprint) | `proof_contract` (`tusker.proof-contract/v1`) — written only when the complete legacy strict authority validates as `strict_current`; one-sided, missing, corrupt, or mismatched strict data blocks. `manual_gate_source_key` is rebound to the durable `manual_gate_id` |
| `delivery_proof_results` | `proof_results` (`tusker.proof-results/v1`) — every result row, note, and fingerprint preserved |
| `delivery_strict_import_lineage` (+ fingerprint) | `strict_proof_lineage` (`tusker.strict-proof-lineage/v1`) — `imported_receipt_fingerprint` is preserved as historical evidence, never a runtime source lookup |
| risk/priority/size-driven review policy | `close_policy_snapshot` — explicit `required_acceptor`, `required_evidence`, `required_gates` computed at migration time and consumed first by review/closeout |

Task/wave IDs, wave membership, bodies, gate IDs/owners/actions/status/
evidence, status/readiness, attempts, reviews, and authorization state are
preserved. Migration never arms, claims, accepts proof, or satisfies/waives
a gate. `risk`/`priority`/`size` remain as inert historical metadata; a
missing or unknown risk never maps to a less restrictive policy, and a
custom policy that cannot be represented blocks with
`CUSTOM_POLICY_UNSUPPORTED <path>=<value>`.

## Unimported plan input

Files under `.tusker/specs/` whose parsed top-level `schema` is exactly
`tusker.delivery-plan/v2` are materialized with the existing strict
decoder: backlog/held tasks, a disarmed wave, preserved acceptance,
checks, implementation notes, dependencies, gates, explicit IDs, and
work/review levels. Materialization is a one-time semantic conversion —
the old strict capability is not re-checked for availability; a strict V2
plan produces canonical `proof_contract`, pending `proof_results`, and
task/wave `strict_proof_lineage` directly. An `epic_contract` whose
acronym collides with an existing epic of a different title blocks with
`EPIC_ACRONYM_COLLISION`. No plan path, bytes, scope, or source keys land
in the resulting records. The file is reported `delete_after_conversion` only
after every authored element has a unique durable target and the semantic
comparison passes; physical deletion is deferred to D7. Malformed
exact-schema files block and are never deleted. Any other file is ignored.

## Active runtime

An admitted nonterminal attempt/run on a candidate task marks its unit
`ACTIVE_ATTEMPT` and blocks it. Terminal attempt/review/evidence rows are
preserved untouched, and apply performs no runtime state or authorization
mutation.

## Proof labels

All test coverage is repository-fixture proof: legacy records and plan
files are constructed inside temporary vaults and converted under test.
`TestDirectWaveMigrationReceiptIdempotenceAfterConversion` proves a fresh
inspect after conversion reports `delete_after_conversion` with no
candidate ambiguity, and that amending a target's durable body/fingerprint
or reordering `wave.members` keeps the receipt identity stable.
`TestDirectWaveMigrationReceiptMissingTargetStaysBlocked` proves a
removed target keeps `PLAN_RECEIPT_TARGET_MISSING` and refuses deletion.
`TestDirectWaveMigrationReceiptPreConversionBlocked` proves a receipt
whose targets still carry legacy fields blocks with
`PLAN_CONVERSION_INCOMPLETE` and leaves the plan file in place.
`TestDirectWaveMigrationPinnedMissingDependencyTarget` proves an exact
pinned missing dependency converts with the pinned fingerprint and no
target creation; `TestDirectWaveMigrationPinnedMissingDependencyRefusals`
covers unpinned/duplicate/kind-conflicting/malformed pins.
`TestDirectWaveMigrationReceiptMovedGateStaysBlocked` proves a moved gate
association keeps `PLAN_RECEIPT_TARGET_DRIFTED`.
`TestDirectWaveMigrationExplicitPlanMap` proves an evolved plan file maps
to its wave only via the explicit flag and stays
`delete_after_conversion` through apply without new records;
`TestDirectWaveMigrationExplicitPlanMapInvalid` covers wrong wave, title,
count, order, membership, moved gate, path escape, and unknown path.
Provider-
live migration against a production vault is **NOT RUN** in this
environment; the real-vault apply/inspect correctness check is owned by
the lead.
