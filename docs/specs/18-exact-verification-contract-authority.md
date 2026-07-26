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
the software factory. An imported V2 task therefore cannot be completed because
some plausible test passed. Tusker must preserve the exact reviewed acceptance
and verification rows as immutable task material, accept proof only for those
rows, bind independent review to that proof snapshot and implementation
revision, and recheck the same authority immediately before integration and
close.

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
surface, not a second source of authority.

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
machine check. Manual rows are green only from the exact current satisfied or
explicitly waived human gate, including its hard-closure material fingerprint.

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
a manual row passed, integrate, close, or wake successors.

## Adoption and compatibility

- New V2 imports are strict.
- Held V2 tasks may be re-imported idempotently.
- A progressed V2 task may receive a metadata-only strict-contract adoption
  only when the reviewed source plan still resolves and its exact existing
  delivery task fingerprint matches. Drift requires explicit rework; Tusker
  never guesses.
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
| Existing held V2 task is re-imported | Strict projection is added idempotently. |
| Existing progressed V2 task matches reviewed source exactly | Explicit metadata-only adoption is available. |
| Existing progressed V2 task drifted | Fail closed with exact rework/adoption action. |
| V1 or direct singleton task | Legacy behavior remains available and is labeled non-strict. |

## Acceptance

| ID | Observable requirement |
| --- | --- |
| R1 | V2 import preserves one immutable canonical acceptance/verification contract whose fingerprint participates in wave authority and whose readable Markdown projection cannot drift. |
| R2 | Proof, review, and completion accept only exact current machine rows and exact human gates bound to the current task, work, source, policy, and dependency material. |
| R3 | Operators and installed agents see the exact missing or stale contract in product language without logs or contract-weakening controls. |
| R4 | A hermetic adversarial fixture proves substitution, injection, manual spoof, stale source, crash replay, adoption, and legacy compatibility without live automation or ref movement. |

