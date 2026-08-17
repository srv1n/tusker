---
title: "Delivery plans and waves (author → review → import → arm)"
subject: delivery-and-waves
keywords: [delivery, delivery plan, wave, arm, preflight, authorization, frontier, cross-scope, doctor, delivery start]
part_of: overview
status: canonical
read_when: "Authoring or debugging a delivery plan (v2 schema, doctor codes, cross-scope deps, capabilities), or reasoning about how a wave gets imported, preflighted, armed, paused, or drained into a frontier."
skip_when: "You only need how the daemon picks the next task off an already-armed wave ([[orchestration]]) or how build/test and human gates adjudicate a change ([[gates]])."
sources:
  - cmd/tusker/delivery_cmd.go
  - cmd/tusker/delivery_v2.go
  - cmd/tusker/delivery_context_cmd.go
  - cmd/tusker/delivery_doctor.go
  - cmd/tusker/delivery_review_cmd.go
  - cmd/tusker/delivery_start_cmd.go
  - cmd/tusker/delivery_cross_scope.go
  - cmd/tusker/delivery_phase_readiness.go
  - cmd/tusker/delivery_verification_contract.go
  - cmd/tusker/delivery_capabilities.go
  - cmd/tusker/delivery_rollout.go
  - cmd/tusker/v7_wave_cmd.go
  - cmd/tusker/v7_wave_authorization.go
  - cmd/tusker/v7_wave_brief.go
  - cmd/tusker/v7_delivery_unit.go
  - cmd/tusker/armed_wave.go
---

# Delivery plans and waves

A **delivery plan** is a YAML proposal: requirements, tasks, acceptance, proof,
human gates, resource/overlap strategy. Importing one atomically materializes a
**wave** (`W-NNNN`) plus its member tasks, gates, and epic. The wave is the
authorization boundary: nothing dispatches until it is *armed*, and arming
requires an exact fingerprint match against reviewed material.

Nothing on this page dispatches work. Scheduling of an armed wave belongs to
[[orchestration]]; build/test proof and human-gate semantics belong to
[[gates]]; task records, proof modes, and evidence belong to
[[tasks-and-proof]].

## Command surface

Dispatched at `cmd/tusker/cli.go:323-365`; see also [[cli]].

| Command | Writes? | Does |
|---|---|---|
| `tusker delivery context --spec <ref> [--scope]` | no | Emit the bounded planning context + `context_fingerprint` (`deliveryPlanningContextCmd`) |
| `tusker delivery plan --spec <ref> --out <f>` | scratch only | Emit an **inert** placeholder v2 template bound to the current context (`deliveryPlanCmd`) |
| `tusker delivery doctor --plan <f>` | no | Contract + operational safety findings (`deliveryDoctorCmd`) |
| `tusker delivery review --plan <f>` | no | Operator-facing projection + three-phase readiness (`deliveryReviewCmd`) |
| `tusker delivery import --plan <f> [--dry-run]` | yes | Atomic held import → disarmed wave (`deliveryImportCmd`) |
| `tusker delivery start --plan <f> --confirm <fp>` | yes | Held import **then** exact-wave arm, as one refusable transaction (`deliveryStartCmd`) |
| `tusker delivery rollout doctor\|repair` | repair only | Fleet-level project/service registration + policy repair (`deliveryRolloutCmd`) |
| `tusker wave create\|add\|remove\|show\|brief` | mixed | Manual wave records and read surfaces (`v7_wave_cmd.go`, `v7_wave_brief.go`) |
| `tusker wave preflight\|arm\|pause\|resume\|disarm` | arm/pause/resume/disarm | Authorization lifecycle (`v7_wave_authorization.go:73-105`) |

## Plan schema versions

| | v1 `tusker.delivery-plan/v1` | v2 `tusker.delivery-plan/v2` |
|---|---|---|
| Epic | must already exist | `epic` **or** `epic_contract` (creates it), mutually exclusive |
| Requirements | none | `requirements[]` required; every task needs `requirement_refs` |
| Context binding | none | `context_fingerprint` + factory-intake schema/version/fingerprint required |
| Human gates | none | `human_gates[]` → real `*-G-NNNN` gate records |
| Cross-scope deps | rejected (`KnownFields(true)`) | `dependencies[].scope` allowed, hard-only |
| Doctor | not run | run **inside** import; unsafe plans refuse |
| Emitted by `delivery plan` | no | yes |

`deliveryImportCmd` sniffs `schema` first (`deliveryPlanSchemaAt`) and routes v2
to its own strict decoder before the v1 path, so v1 stays exactly the old model.

### v2 plan fields

`deliveryPlanV2` (`delivery_v2.go:19`); tasks share `deliveryPlanTask`
(`delivery_cmd.go:86`) with v1.

| Field | Required | Notes |
|---|---|---|
| `schema`, `scope`, `title`, `spec_refs` | yes | scope charset `[A-Za-z0-9._/:-]`, non-blank (`deliveryScopeValid`) |
| `context_fingerprint` | yes | `sha256:<64 lowercase hex>`; must equal a recomputed scope-excluded context |
| `factory_intake_contract_{schema,version,fingerprint}` | yes (new plans) | must equal `embeddedFactoryIntakeContractProvenance()`; see [[factory-intake]] |
| `epic` \| `epic_contract{source_key,acronym_hint,title}` | exactly one | acronym collision is an issue |
| `requirements[]{id,outcome}` | ≥1 | every id must be covered by ≥1 task |
| `required_capabilities[]` | no | normalized+sorted; unknown/unavailable → hard refusal |
| `non_goals`, `assumptions`, `unresolved_decisions`, `summary` | no | authored facts; doctor must never synthesize them |
| `shared_resources[]{source_key,kind,capacity}` | no | every `resource_refs` entry must resolve here |
| `owned_path_overlaps[]{tasks,paths,strategy,integrator}` | no | `strategy` ∈ `serialize` \| `integrator` |
| `concurrency`, `runner_profile` | no | capped by widest frontier; profile must exist in config |
| `human_gates[]` | no | `source_key,title,kind,owner,task_source_key,acceptance_ids,action,verification,why_agent_cannot` |
| task: `source_key,title,outcome,acceptance[],verification[],artifact` | yes | |
| task: `owned_paths,generated_outputs,migration_keys,resource_refs,concurrency_group,knowledge_nodes,complexity,risk,priority,size,domains` | no | `complexity` ∈ routine\|standard\|complex\|frontier |

Every `verification[].check` must start with `command: ` or `manual proof: `,
and every acceptance ID must be covered (`validateDeliveryPlan`). Any empty
value, or one containing `tbd`, `todo`, `replace-me`, `replace with`, `<...>`,
or `placeholder`, refuses (`deliveryPlaceholder`). Artifact `path` must be
repo-relative — not absolute, not `..`, not under `.tusker/scratch/`
(`deliveryInvalidProductionPath`) — and `kind` must be in
`v7OperatorArtifactKinds`.

## Authoring contract projected to planners

`deliveryPlanValidationRules()` (`delivery_cmd.go:162-179`) is the
machine-readable rule set `delivery context` emits so a planner never
reverse-engineers the validator. Each rule carries `id`, `fields`,
`requirement`, `failure_code`, `remedy`: `bounded_scope`, `planning_context`,
`factory_intake_provenance`, `source_keys`, `requirement_coverage`,
`acceptance_proof`, `operator_artifact`, `acyclic_dag`, `human_gate_authority`,
`shared_resources`, `overlap_strategy`, `capacity`, `runner_profile`,
`assumptions`.

`delivery context` (`delivery_context_cmd.go:231-251`) is read-only and bounded
(max 8 docs / 8 domains / 8 epics / 16 task clues / 16 profiles / 32 path
clues). It reports project + integration base, governing specs/decisions, epic
and duplicate-task clues, knowledge domains, focused/integration test commands,
runner profiles and their proof capabilities, workspace/branch/gate policy,
likely owned paths, shared resources, open human gates, the plan contract,
runtime readiness, and an explicit `unknowns[]` list with remedies — never a
guess. Secrets are scrubbed by the `deliverySensitive*` regexes. The context
deliberately **excludes** the scope being planned, so importing a plan cannot
invalidate its own fingerprint.

## Doctor findings

`deliveryPlanDoctorBytes` (v2 only) runs `deliveryV2Prepare` → base validator →
`doctorOperationalFindings`, then sorts+dedupes. Findings are stable
`{code, path, source_keys, message, remedy, provenance}`.

| Group | Codes |
|---|---|
| Schema/contract | `UNSUPPORTED_PLAN_SCHEMA`, `PLAN_CONTRACT_INVALID`, `REQUIRED_CAPABILITY_UNAVAILABLE` |
| Traceability | `REQUIREMENT_UNCOVERED`, `REQUIREMENT_REFERENCE_UNKNOWN`, `ACCEPTANCE_INVALID`, `ACCEPTANCE_UNMAPPED`, `PROOF_UNFILLED`, `PROOF_UNSUPPORTED`, `PROOF_ACCEPTANCE_UNKNOWN`, `ARTIFACT_INVALID` |
| Graph | `DEPENDENCY_CYCLE`, `DEPENDENCY_DANGLING` |
| Resources | `RESOURCE_DECLARATION_INVALID`, `RESOURCE_DECLARATION_DUPLICATE`, `RESOURCE_UNDECLARED`, `RESOURCE_CAPACITY_EXCEEDED`, `RESOURCE_FRONTIER_CONFLICT` |
| Overlap | `OVERLAP_STRATEGY_{INVALID,DUPLICATE,TASK_UNKNOWN,SCOPE_MISSING}`, `OVERLAP_SERIALIZATION_UNPROVEN`, `OVERLAP_INTEGRATOR_{UNKNOWN,UNORDERED,SCOPE_MISMATCH}`, `OWNED_PATH_FRONTIER_CONFLICT`, `GENERATED_OUTPUT_FRONTIER_CONFLICT`, `MIGRATION_FRONTIER_CONFLICT` |
| Human gates | `HUMAN_GATE_PROOF_INVALID`, `HUMAN_GATE_ACCEPTANCE_MISMATCH`, `HUMAN_GATE_CLOSURE_{MISSING,MISMATCH}`, `HUMAN_PROOF_GATE_MISSING` |
| Honesty/capacity | `ASSUMPTION_PRESENTED_AS_FACT`, `OPERATOR_SUMMARY_MISSING`, `CAPACITY_PROVENANCE_UNAVAILABLE`, `CONCURRENCY_CAP_EXCEEDED`, `UNSUPPORTED_RUNNER` |

Two tasks that can sit in the **same frontier** and touch the same owned path,
generated output, migration key, or scarce resource must be serialized (proven
by a total dependency order, `doctorTasksTotallyOrdered`) or handed to an
integrator owning the complete collision surface (`doctorIntegratorOwns`).
Import re-runs the doctor and refuses `delivery plan is operationally unsafe`
even when the caller skipped `delivery doctor` (`delivery_v2.go:361-368`).
Manual-proof verification rows must map exactly onto a source-keyed human gate
for the same task and acceptance IDs, and that gate must declare its full
downstream closure (`doctorGates`).

## Cross-scope dependencies

`dependencies[].scope` names a **producer plan scope**; it is v2-only, must be
non-blank, valid, different from the plan's own scope, unique, and
`kind: hard`. Resolution happens under the vault material epoch and only
targets an already-imported task in this vault (`delivery_cross_scope.go:21-95`).
Refusal codes, all prefixed `CROSS_SCOPE_`: `INVALID_SCOPE`, `SAME_SCOPE`,
`HARD_ONLY`, `DUPLICATE_DEPENDENCY`, `DUPLICATE_TARGET`,
`PRODUCER_{MISSING,DUPLICATE,FOREIGN_VAULT,DISCARDED,OBSOLETE,REMOVED,STALE}`,
`GLOBAL_CYCLE`, `EDGE_DRIFT`, `TARGET_DRIFT`, `UNPROJECTED_EDGE`,
`LOCAL_PROJECTION`, `PROJECTION_INVALID`,
`INBOUND_{REMOVAL,CAS_CONFLICT,CONSUMER_PROGRESS}`,
`NAMESPACE_{STALE,READ_FAILED}`, `EPOCH_STALE`, `SNAPSHOT_READ_FAILED`,
`STATE_REV_INVALID`.

The resolved edge is stored as a normal task-ID dependency; the
`delivery_cross_scope_dependencies` projection (scope, source key, task ID,
kind, `target_contract_fingerprint`) is *derived* and read-only. When a producer
re-imports, the whole inbound closure is rewritten in the same transaction
(`deliveryRefreshInboundProjectionWrites`) so a consumer never observes a
half-updated graph.

## Import is a document transaction

`applyDeliveryImportGuarded` (`delivery_cmd.go:637`) holds the V7 material epoch
lock, builds the **complete** write set in memory (tasks, wave, gates, epic,
spec work-stream blocks), drops writes that are semantically unchanged
(`convergeUnchangedDeliveryWrites` ignores `updated_at`/`updated_by`/`state_rev`),
then commits through `commitDeliveryWritesGuarded`:

- sorted per-document flock over write ∪ snapshot paths (already-held caller
  locks are skipped: flock is not recursive across descriptors);
- preimage capture with symlink/regular-file checks on target *and* parent dir;
- per-file temp-write → fsync → CAS-checked rename → parent dir sync, then
  post-write byte verification, with guard `Verify`/`SnapshotVerify` before,
  between, and after every write;
- on failure, reverse-order rollback restoring only **transaction-owned** bytes
  (`restoreDeliveryWritePreimagesOwned`); third-party bytes are preserved and
  reported as an *unproven rollback*, a fail-closed error demanding repair from
  version control.

Imported tasks land `status: backlog`, `readiness: held`, `proof_mode: inline`,
`proof_status: pending`, and carry `delivery_source_key`, `delivery_plan_scope`,
and `delivery_contract_fingerprint`. Re-import of a task that has **progressed**
past held with a changed contract fingerprint refuses and demands an explicit
rework/control transition. The wave record freezes `integration_base_sha` (a
read-only snapshot of the configured default branch); import never creates the
integration ref. Gates removed from a plan become `status: obsolete`, never
deleted.

## `delivery start`: held import then exact arm

`deliveryStartWithPlanSource` (`delivery_start_cmd.go:112-320`) performs exactly
two mutations and re-verifies plan identity at every seam:

1. read plan + `--confirm <fingerprint>`, build bounded context, take the
   material lock, re-read the plan under it — any byte difference refuses;
2. run the v2 import with `skip-integration-branch`,
   `expected-plan-fingerprint`, `expected-integration-base-sha`;
3. capture `deliveryStartAuthority`: wave ID, sorted members, per-member
   baseline + readiness, `waveMaterialFingerprint`, authorization tuple;
4. release the lock, re-read plan and context, re-derive the wave, compare
   fingerprint + membership, then `buildWavePreflight` — not OK means refuse and
   roll the import back;
5. `mutateWaveAuthorizationWithInspector(..., "armed", authority)` re-locks the
   wave *and every member task*, rebuilds preflight, and refuses if wave
   identity, material fingerprint, membership, authorization tuple, or any
   member baseline/readiness moved since import.

A Serve-style descriptor-bound Start (`deliveryPlanSource`) keeps both write
commits invisible until arming succeeds and restores exact preimages on refusal.
`deliveryStartResult` exposes wave ID, plan/context/authorization fingerprints,
first frontier, expected concurrency, integration lane, and status link — never
daemon or runner control.

### Three-phase readiness

`delivery review` never returns one boolean. `finalizeDeliveryReviewReadiness`
(`delivery_phase_readiness.go`) splits causes into three phases and projects a
`ReadinessContract`:

| Phase | Flag | Blocked by |
|---|---|---|
| Contract | `planValid` | schema/traceability/doctor issues |
| Import | `importReady` | held-import material failing `specDag`/`taskContracts`/`artifacts` |
| Start | `startReady` | automation, authorization, runtime dimensions |

Runtime blockers are typed, not prose-parsed
(`deliveryReviewAddEnvironmentStartBlockers`): project registration, automation
off, project health, workflow/skill compatibility, daemon alive, daemon
reconciling, runner compatibility, approval-free execution, isolated workspace,
clean integration lane. A `completed` delivery clears all Start blockers.

## Wave records

Frontmatter `schema: tusker.wave/v7` — `id`, `title`, `status`, `authorization`,
`members[]`, `integration_branch`, `concurrency`, `spec_refs`,
`delivery_plan_{schema,scope,fingerprint}`, `integration_base_sha`, the v2
contract block (`deliveryV2WaveContractData`), and `landings[]` audit rows.

| `status` | Meaning |
|---|---|
| `open` | any member not `done` (derived, `v7DerivedWaveState`) |
| `landed` | every member `done`; `landed_at` = latest member close time |

Membership rules (`validateV7WaveMembership`): member IDs must match
`v7TaskIDPattern`, exist, be unique, and belong to no other **open** wave;
`wave remove` cannot empty a wave. Reconciliation derives `status`/`landed_at`
and rewrites task `wave` back-pointers, preferring open waves.

| `authorization` | Entered by | Effect |
|---|---|---|
| `disarmed` | create / import default | no dispatch |
| `armed` | `wave arm` / `wave resume` / `delivery start` | members eligible for the frontier |
| `paused` | `wave pause` (refused on a disarmed wave) | dispatch suspended, authorization retained |
| `stale` | *derived*: `authorization_fingerprint` ≠ recomputed material | must re-preflight and re-arm |

`waveAuthorizationProjection` returns `{state, stale, fingerprint,
authorizedFingerprint, actor, at, action}` and the action string is the literal
remedy command.

### The authorization fingerprint

`waveMaterialFingerprint` (`v7_wave_authorization.go:464-529`) SHA-256s a
canonical YAML of: wave ID, plan schema, `integration_base_sha`, factory-intake
provenance, and per sorted member — title, epic, risk, dependencies, spec_refs,
`delivery_contract_fingerprint`, artifact contract, owned paths, runner profile,
complexity, the proof contract (mode/required/owner/budget/evidence), gates,
cross-scope projections and targets, plus a SHA of **every referenced spec
file's bytes**. For imported tasks the live Acceptance/Verification body tables
are deliberately *excluded* (the immutable contract fingerprint covers them), so
a reviewer appending proof rows does not invalidate a single arm.

### Preflight

`buildWavePreflight` is read-only and returns blockers plus `checks`:
`specDag`, `taskContracts`, `artifacts` (material), and `project`, `daemon`,
`runner`, `skill`, `workflow`, `approvalPolicy`, `workspaceIsolation`
(environment). It resolves member frontiers, refuses unresolved members,
unresolved spec refs, dangling dependencies, stale cross-scope integrity,
invalid artifact contracts, external dependencies neither satisfied nor
cross-scope-qualified, cycles, stale gate authority receipts, human gates with
no agent-boundary explanation, and human gates that own agent-capable work.
`arm` refuses unless preflight is fully green; re-arming an already-armed wave
whose stored fingerprint still matches is a no-op success.

### Armed-wave frontier

`buildArmedWaveSnapshot` (`armed_wave.go`) is recomputed from records + the
runtime store on every poll — there is no cursor to lose across daemon restarts.
Member states, in precedence order: `machine-parked` (missing record / failed
integration projection), `landed`, `review`, `stale-authorization`,
`human-blocked`, `machine-parked` (attempt policy exhausted), `running`,
`review` (status review/done), `dependency-waiting`, `runnable`. A parked or human-blocked member propagates to its **hard**
downstream closure only; soft edges keep running. The frontier is the runnable
set truncated to `concurrency − running`. Once members have landed on the
integration branch, their task state is read from that branch
(`armedWaveIntegrationTaskProjection`) rather than the working tree.

### Brief

`tusker wave brief` (`v7_wave_brief.go`) is the recovery/morning report, in
fixed section order: `outcome` (counts plus per-task
implementation/proof/review/landing/documentation and first actionable
failure), `seeIt`, `landed`, `reworkParked` (with affected dependent closure),
`humanAction`, `documentation`. `fullyDrained` requires every member proven,
reviewed, landed, and documented with no rework or human action outstanding. It
reads evidence off the integration branch and degrades to canonical state
rather than failing.

## Delivery units, capabilities, strict proof, rollout

- **Implicit singleton delivery unit** (`v7_delivery_unit.go`): when scheduled
  promotion is enabled with mode `stage` or `promote`, a reviewed standalone
  task (`status` ∈ review/done, no explicit wave) gets a normal **disarmed**
  wave carrying `delivery_unit: implicit_singleton`, `delivery_task`,
  `delivery_source_state_rev`, `execution_provenance: inherited`,
  `release_authorized: false`. It reuses the landing lane (see
  [[landing-and-completion]]) and is never a dispatch or release authorization.
  Explicit waves are untouched.
- **Capabilities** (`delivery_capabilities.go`): `required_capabilities` may
  only name capabilities this binary enforces. The only defined name is
  `strict_v2_proof_authority/v1`, and `deliveryCapabilityAvailable` currently
  returns **false for everything** — a plan requesting it is refused *before*
  the material lock, so no record, wave, or mutation-adjacent artifact is left
  behind. The refusal carries the installed-version projection and remedy.
- **Strict proof contracts** (`delivery_verification_contract.go`), gated behind
  that capability and therefore currently unreachable: an immutable
  source-derived `delivery_proof_contract` plus a separate mutable
  `delivery_proof_results` projection and mirrored task/wave
  `delivery_strict_import_lineage`. One lineage side missing or drifted is
  `strict_corrupt_or_missing` (reopen/rework); neither present is `legacy`.
- **Rollout** (`delivery_rollout.go`): fleet-scope `doctor` (read-only) and
  `repair --scope core|automation|service|integrations` over the project
  registry, emitting per-project findings + `ReadinessContract` plus a service
  readiness contract. `repair` calls `rejectAgentSpawn`: an agent session cannot
  apply it.
