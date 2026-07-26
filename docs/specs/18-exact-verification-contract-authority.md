---
capsule:
  what: "Makes exact V2 acceptance and verification contracts part of Tusker's deterministic review and completion authority."
  use_when:
    - "Implementing or reviewing imported delivery-plan proof, review, completion, or factory cutover safety."
  skip_when:
    - "Working on a legacy non-V2 task whose proof contract has not been explicitly adopted."
---

# Exact verification contract authority

Status: proposed implementation contract
Date: 2026-07-26

## Outcome

Requirements, acceptance, and important tests are the user's control surface for
the software factory. An imported strict V2 task therefore cannot be completed
because some plausible test passed. Tusker must preserve the exact reviewed
acceptance and verification rows as immutable task material, accept proof only
for those rows, bind independent review to that proof snapshot and
implementation revision, and recheck the same authority immediately before
integration and close.

This contract has a bootstrap boundary. The legacy generic engine is not trusted
to import, run, review, or close the plan that replaces its proof authority.
Plan 18 and strict consumers declare `strict_v2_proof_authority/v1`; until the
complete strict kernel exists, import and Start reject them before writes. The
kernel is landed and independently reviewed interactively with automation off.
Only after its adversarial foundation proof flips the capability is this plan's
provenance regenerated, reviewed again, and its held DAG imported strict. That
local interactive review is a human process boundary, not a cryptographic
identity claim or a substitute for strict proof.

The authority chain is:

```text
V2 plan
  -> canonical acceptance + verification contract
  -> armed-wave material fingerprint
  -> current exact proof snapshot
  -> independent typed review result
  -> deterministic completion CAS
  -> integration / close / successor wake
```

Every arrow is fail-closed and fingerprint-bound. Markdown remains the readable
surface, not a second source of authority. The first strict import happens only
after the separately reviewed bootstrap foundation exists; old generic receipts
do not cross this boundary.

## Bootstrap and capability fence

`required_capabilities` is a normalized V2 field. The importer, doctor, and
Start path deterministically refuse a plan requiring an unavailable capability,
rather than treating an unknown field, generic focused test, or doctor-green
report as authority. The staged kernel is deliberately external to the imported
Plan-18 DAG: a task cannot truthfully certify the authority needed to import or
complete itself.

| Stage | Automation | Capability | Required boundary |
|---|---|---|---|
| K0 | off | false | Parse/normalize `required_capabilities`; doctor, import, and Start refuse the exact unavailable capability before the material epoch or any record write. |
| K1 | off | false | Land and independently review canonical immutable import/proof-contract material. |
| K2 | off | false | Land and independently review exact proof writes and snapshots. |
| K3 | off | false | Land and independently review strict human control and close fencing. |
| K4 | off | false | Land and independently review typed review/completion binding. |
| K5 | off | true only after pass | Run the adversarial kernel E2E and record a deterministic foundation fingerprint. Regenerate/re-review Plan 18, then import it strict. |

No K-stage creates a Plan-18 task, generic proof row, review result, completion
receipt, gate waiver, wave authorization, daemon/runtime, dispatch, release,
spend, or ref movement. K5 is not a human actor string or a claim of
cryptographic identity; it is a bounded reviewed foundation fact. The imported
Plan-18 DAG begins only after K1-K5 exist, so its initial adoption tasks can use
the real strict proof/review/completion route instead of self-hosting one.

K0 is the narrow mergeable stage in this change: `delivery_capabilities.go`,
the V2 decoder, doctor, import, and Start only parse/normalize and refuse. It
does not stamp strict tasks or modify generic proof/review/close controls.

## Root cause

V2 import currently renders exact verification rows into the task body and
stores a fingerprint of the source task contract. It also initializes every
imported task with the generic proof requirement `focused_test`.

The armed-wave fingerprint trusts the stored delivery-contract fingerprint and
deliberately ignores live verification-table changes so result rows can move
from pending to pass. Review then fingerprints that live table. This has an
unsafe gap: a worker can weaken or replace an expected check before review
without changing the armed-wave fingerprint. A reviewer can honestly approve
the weakened live table because Tusker no longer has the canonical imported
rows to compare against.

This is not a reviewer-model problem. It is missing deterministic authority.

## Canonical contract

New and safely adopted V2 tasks carry:

- `delivery_proof_contract_schema`;
- a normalized acceptance contract of exact IDs and outcomes;
- a normalized verification contract of exact acceptance coverage, check type,
  and check text;
- a proof-contract fingerprint bound to the existing delivery task contract;
- the source plan scope, source key, and task contract fingerprint already used
  by V2 import.

The normalized verification check type is either:

- `command` — machine proof that must pass exactly; or
- `manual` — proof that must be supplied by the exact source-keyed human gate
  covering the same acceptance IDs.

Result, bounded notes, and typed failure details are mutable proof state. IDs,
outcomes, coverage, check type, and check text are immutable material.
Additional diagnostic rows may be retained for operator context, but never
satisfy canonical acceptance or completion authority.

The canonical projection and its fingerprint are included in armed-wave
material. A raw edit to the canonical projection, acceptance table, or expected
verification rows blocks dispatch/review/close and makes existing authorization
stale. Updating only a canonical row's result does not stale authorization.

## Exact proof writes

For a strict V2 task, `tusker verify add` and worker-result ingestion:

1. resolve the canonical proof contract;
2. require the exact canonical coverage and check;
3. refuse an agent-authored pass for a manual row;
4. refuse a renamed, broadened, substituted, or undeclared check;
5. update only mutable result and bounded diagnostic fields;
6. bind the proof snapshot to task ID, delivery proof-contract fingerprint,
   current work revision, current implementation SHA, attempt, and task material
   revision;
7. make stale, failed, blocked, and pending rows non-green.

Evidence cards and unrelated passing rows cannot substitute for a canonical
machine check. Manual rows are green only from the exact current typed
human-control receipt and satisfied (never lane-forged) source-keyed gate,
including its hard-closure material fingerprint. A literal `--by human:*` actor
string is not authority. The local interactive control path may record the
bounded receipt; execute and review lanes identified by `TUSKER_ATTEMPT_ID` or
lane policy cannot satisfy or waive it, even with `--force`. This is deliberately
not a claim of cryptographic human identity.

Tusker does not pretend a command receipt is cryptographic proof that the
underlying program is correct. The independent reviewer remains responsible for
verification judgment. The deterministic engine guarantees that the reviewer
and completion handler are judging the exact reviewed contract and exact
implementation revision.

## Review and completion

A typed review pass is authoritative only when it binds:

- the exact acceptance set;
- current delivery proof-contract fingerprint;
- current exact proof fingerprint;
- current gate fingerprint;
- current work revision and implementation SHA;
- the authenticated execute/review worker-policy chain already required by the
  completion reactor.

Any contract, task material, source revision, proof, gate, runner policy, or
dependency drift invalidates the pass. The completion transaction rechecks the
complete tuple under its existing serialized authority immediately before
integration, task close, audit, and successor wake. Crash replay converges on
one outcome and cannot reuse a stale pass.

No model may edit the canonical contract, choose its own substitute proof, mark
a manual row passed, integrate, close, or wake successors. Strict tasks also
reject legacy `accept`, `close`, `finish`, generic `gate satisfy`, and `--force`
shortcuts unless the deterministic completion handler verifies the current exact
proof, typed review, current gate closure, and strict completion receipt.

## Adoption, producers, and compatibility

- A held V2 task may be re-imported to install a strict pending contract
  idempotently.
- A progressed or terminal pre-strict V2 task remains legacy and is ineligible
  for strict dispatch, readiness, gate closure, closeout, or cross-scope
  production. It must be reopened/reworked, or explicit strict adoption must
  first invalidate its legacy proof and review, then rerun exact checks on the
  bound implementation, obtain independent typed review, and record an
  authenticated strict completion/adoption receipt. Metadata alone never
  adopts authority.
- Cross-scope projections, readiness, and gate closure bind the producer's
  delivery-contract fingerprint, delivery proof-contract fingerprint, and
  strict completion-authority fingerprint. Missing, legacy, stale, substituted,
  or drifted values fail closed; a familiar scope/source key is not a rebind.
- V1 plans, direct singleton tasks, and historical non-V2 work keep their
  existing proof behavior until explicitly adopted.
- Authoritative completion refuses a V2 task that claims strict factory
  execution but lacks a current canonical proof contract. Shadow mode reports
  the mismatch without mutating it.

## Operator and agent surface

Task capsules, packets, review packets, closeout, automation plan, and Serve
show:

- required exact checks;
- passed, pending, failed, blocked, or stale status;
- manual owner/gate when applicable;
- proof-contract and reviewed-revision currentness;
- the first exact next action.

They do not expose raw logs, secrets, full environment values, or an affordance
to weaken the contract.

Installed Tusker guidance must tell planners to emit exact V2 DAG verification
rows, implementation agents to run and record only those checks, reviewers to
judge the unchanged contract and implementation revision, and automated workers
to leave integration/close/successor scheduling to deterministic handlers.
Planning, import, doctor, review, and shadow remain inert and never enable a
project, arm a wave, start a daemon/runtime, release, spend, or move a ref.

## Failure matrix

| Scenario | Required result |
| --- | --- |
| Worker replaces a slow check with a cheap check | Contract drift; no review or completion authority. |
| Worker appends an unrelated green row | Retained as diagnostic only; canonical row remains missing. |
| Worker marks a manual row passed | Refused; exact human gate remains required. |
| Proof passed before implementation SHA changes | Stale proof; re-run and re-review required. |
| Review pass targets prior work revision | Rejected before completion. |
| Gate is satisfied before hard closure or then drifts | Existing gate guards reject or stale the authority. |
| Crash after review but before close | Replay rechecks exact current tuple and converges once. |
| Existing held V2 task is re-imported | Strict pending projection is installed idempotently. |
| Existing progressed/terminal pre-strict V2 task | Legacy-only and ineligible until strict rework or full receipt-backed adoption. |
| Cross-scope producer lacks strict proof/completion fingerprints | Consumer, readiness, and gate closure refuse. |
| Legacy importer/Start sees Plan 18 capability | Reject before writes; bootstrap remains interactive and automation-off. |
| V1 or direct singleton task | Legacy behavior remains available and is labeled non-strict. |

## Acceptance

| ID | Observable requirement |
| --- | --- |
| R1 | Strict V2 import preserves one immutable canonical acceptance/verification contract whose fingerprint participates in wave authority, while legacy completed work cannot become strict by metadata alone. |
| R2 | Proof, review, completion, cross-scope production, and manual gates accept only current fingerprints and typed receipts bound to the current task, work, source, policy, and dependency material. |
| R3 | Operators and installed agents see the exact missing or stale contract in product language without logs or contract-weakening controls. |
| R4 | Hermetic adversarial fixtures prove substitution, injection, forged manual control, legacy-done activation refusal, stale source, crash replay, receipt-backed adoption, and legacy compatibility without live automation or ref movement. |
| R5 | Legacy import/Start cannot bootstrap Plan 18; an interactive automation-off foundation precedes regenerated strict provenance and held-DAG import. |
