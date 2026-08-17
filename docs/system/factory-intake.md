---
title: Factory intake and operations
subject: factory-intake
keywords: [intake, routing, delivery-plan, contract, fingerprint, guardrails, factory operations]
part_of: overview
status: canonical
read_when: "You need the canonical rule for routing a request to analysis, a singleton, direct interactive delivery, or a delivery plan, or you need the shape and guarantees of the read-only `tusker factory operations` projection."
skip_when: "You want skill install/provenance mechanics ([[skills]]), plan execution and wave authorization ([[orchestration]]), or per-task lifecycle and proof ([[tasks-and-proof]])."
sources:
  - skills/tusker/assets/factory-intake-contract.yaml
  - cmd/tusker/factory_intake_contract.go
  - cmd/tusker/factory_operations.go
  - cmd/tusker/skill_compatibility.go
  - cmd/tusker/capabilities_cmd.go
---

# Factory intake and operations

Two separate things share the "factory" name: a **static routing contract**
(what kind of work a request becomes) and a **read-only operations projection**
(what the factory is currently doing).

## The intake contract

Canonical file: `skills/tusker/assets/factory-intake-contract.yaml`, schema
`tusker.factory-intake-contract/v1`, `contract_version: 1.1.0`. It is shipped
inside the embedded skill payload and read back via
`skillbundle.GetAsset("factory-intake-contract.yaml")`.

Top-level keys (`factoryIntakeContract` in `cmd/tusker/factory_intake_contract.go`):
`schema`, `contract_version`, `title`, `description`, `product_questions`,
`factory_mechanics`, `decision_table`, `structural_multi_unit_signals`,
`guardrails`. `product_questions`, `factory_mechanics`,
`structural_multi_unit_signals`, and `guardrails` must be non-empty.
`loadFactoryIntakeContract` decodes with `KnownFields(true)`, but the path every
production caller uses (`factoryIntakeContractProvenanceFromRaw`) uses plain
`yaml.Unmarshal` — it validates shape while silently ignoring unknown keys.

### Routing inputs

Routing takes semantic intent plus structural facts. `FactoryIntakeRequest`
carries **no prompt text** — language understanding happens before routing, and
routing is never keyword matching.

| Intent (`FactoryIntakeIntent`) | Meaning |
| --- | --- |
| `analysis` | Evaluation, audit, comparison, critique. |
| `singleton_record` | One small recorded follow-up. |
| `implementation` | A change requested now. |
| `planning_or_tasking` | Produce tracked work. |
| `unattended_delivery` | Work to run without a human at the keyboard. |

`FactoryIntakeScope` collapses to `singleton` or `multi_unit`.
`FactoryIntakeScope.multiUnit()` returns true if **any** of:
`IndependentlyProvableOutcomes >= 2`, `Domains >= 2`, `ConcurrentLanes`,
`SharedScarceResource`, `RolloutOrRecoveryPhase`, `ImplementationBranches >= 2`,
`ReviewerPackets >= 2`.

### The decision table

`RouteFactoryIntake` picks the first row whose `intent` matches and whose
`scope` is empty (matches either) or equal. The six rows are hard-coded as the
expected shape in `validateFactoryIntakeContract` — the YAML may not add,
remove, rename, or re-point a route. Row *order* is load-bearing for that
first-match scan but is not validated.

| id | intent | scope | route | durable_mutation | execution_authority |
| --- | --- | --- | --- | --- | --- |
| `analysis` | `analysis` | any | `read_only_analysis` | `none` | `none` |
| `unattended` | `unattended_delivery` | any | `delivery_plan` | `versioned_plan_and_held_import` | `fingerprint_bound_start_delivery` |
| `planned_or_structural_multi_unit` | `planning_or_tasking` | any | `delivery_plan` | `versioned_plan_and_held_import` | `none` |
| `singleton_record` | `singleton_record` | `singleton` | `direct_singleton` | `held_or_backlog_singleton` | `none` |
| `bounded_direct_implementation` | `implementation` | `singleton` | `direct_interactive` | `singleton_contract_only` | `explicit_direct_request` |
| `implementation_with_multi_unit_scope` | `implementation` | `multi_unit` | `delivery_plan` | `versioned_plan_and_held_import` | `fingerprint_bound_start_delivery` |

`singleton_record` has no `multi_unit` row, so that combination returns an
explicit "no route" error rather than silently downgrading. Every other intent
is covered for both scopes.

Each row also carries a free-text `remedy` (required, not shape-checked) stating
the correct action, e.g. "Plan first; unattended execution requires the exact
Start delivery action after preflight."

### Guardrails

All fifteen must be present by exact name or the contract fails validation:

`analysis_is_read_only`, `import_is_inert`,
`tracked_modifying_work_requires_work_start`,
`dispatched_worker_verifies_existing_claim`,
`reviewer_submits_typed_result_only`,
`deterministic_handlers_own_close_and_successor_wake`,
`epic_is_never_execution_authority`,
`project_automation_is_separate_explicit_opt_in`,
`fresh_dispatch_scope_is_armed_waves`,
`start_does_not_enable_project_automation`,
`start_does_not_start_or_install_daemon`,
`start_does_not_authorize_release_or_paid_work`,
`start_does_not_satisfy_human_gates`,
`start_does_not_include_unrelated_work`,
`start_requires_current_plan_fingerprint`.

In prose: plan creation and import are inert. They do not enable automation,
install or start a daemon, dispatch workers, release software, spend money, or
satisfy human gates. A Start delivery action must revalidate the exact plan
fingerprint — a stale plan fails — and pass preflight. Tracked modifying work
begins with `tusker work start`; a dispatched worker verifies its injected claim
instead of claiming again. Review emits one typed verdict; deterministic
handlers own merge, close, integration, and successor wake. Fresh automation is
separately opt-in and defaults to `automation.dispatch_scope: armed_waves`.

### Versioning and fingerprints

`factoryIntakeContractFingerprint` is `sha256:` over the **raw YAML bytes**, so
any byte change — including comments and whitespace — is a new fingerprint. It
deliberately hashes only this file, not the skill tree: hashing the tree would
make the value self-referential once `compatibility.yaml` embeds it.

`embeddedFactoryIntakeContractProvenance` yields the triple
`{schema, version, fingerprint}`. That triple is duplicated into
`skills/tusker/assets/compatibility.yaml` under `factory_intake_contract`, is
projected by `tusker capabilities --json`, and is written into every
materialized install's `.tusker-skill-provenance.yaml`. Comparison
(`factoryContractStatus`) is three-valued:

| Result | Condition |
| --- | --- |
| `incompatible` | Any field empty, or schema differs. |
| `stale` | Schema matches but version or fingerprint differs. |
| `current` | All three equal. |

**Editing the contract requires updating the fingerprint in
`compatibility.yaml` in the same change**, or every install classifies `stale`
and `tusker skill sync` refuses the canonical source. See [[skills]] for how
those statuses are produced and repaired.

### What actually runs this

`RouteFactoryIntake`, `loadFactoryIntakeContract`, and
`factoryIntakeContractPath` have no non-test callers. The routing table is
enforced today as a **validated, fingerprinted contract that agents read**, not
as an executed dispatcher: the binary validates its shape, binds its
fingerprint into compatibility and provenance, and the skill guides carry the
same rules in prose. Treat the YAML as the authority when the prose and the
table disagree.

## `tusker factory operations`

`tusker factory operations [--json]` (`cmd/tusker/factory_operations.go`) emits
one read-only `tusker.factory-operations/v1` projection, shared by the CLI and
Serve. Argument validation is strict: `--json` is a valueless switch,
positionals are rejected, and any other flag — including `--vault` — is refused
because the command is read-only. It never dispatches work, changes lifecycle
state, or moves Git refs.

The projection carries `project` (registration, enablement, health, automation
enabled + provenance, dispatch scope, completion mode, scheduled-promotion
mode), `authority` (default ref/SHA and per-wave fingerprint health:
`current`, `stale`, `not_armed`, `not_authorized`), `capacity` (global and
project active/limit/available plus fresh resource holds), and six ordered
sections.

| Section | Contains |
| --- | --- |
| `delivered` | `done`/`closed` tasks and committed/staged/shadow-validated departures. |
| `workingNow` | Fresh non-review implementation leases and in-flight promotion departures. |
| `reviewOrRework` | Active review leases, `review` status, `rework`, queued review retries. |
| `blocked` | Stale runs, parked runs, retries blocked by project health, completion-repair states, promotion-blocked departures, and blocked frontier items. |
| `needsYourDecision` | Genuine human gates, with owner, action, verification, why-human, and the affected dependent closure. |
| `nextFrontier` | Idle/ready work and queued implementation retries. |

Every item states an `automaticNextAction` (what Tusker will do unattended) and
a `safeAction` — usually a read-only command (`tusker runs inspect <ID> --json`,
`tusker show <ID> --capsule`), but for human gates it is the gate's free-text
action, and for wave-authority entries it can be `tusker wave pause|resume`.
Tasks in a gate's affected closure are dropped as items and survive only as IDs
in that gate's `affectedTaskIds`; `cancelled` and `superseded` tasks are dropped.
Free text is truncated through `safePacketText`, and sections are sorted
deterministically.
