---
title: Factory intake contract
subject: factory-intake
part_of: overview
contract: tusker.factory-intake-contract/v1
contract_version: 1.1.0
source: skills/tusker/assets/factory-intake-contract.yaml
---

# Factory intake contract

The canonical, machine-auditable routing table is
`skills/tusker/assets/factory-intake-contract.yaml`. This page explains it;
callers and diagnostics load the YAML rather than duplicating its decisions.

Routing consumes semantic intent and repository-derived structural scope. It
does not scan a prompt for magic words. In particular, independently provable
outcomes, multiple domains (such as backend plus UI), concurrent lanes, shared
resources, rollout/recovery, multiple branches, and multiple reviewer packets
make implementation multi-unit work.

| Situation | Route | Authority |
| --- | --- | --- |
| Evaluation, audit, comparison, or critique | Read-only analysis | None |
| One small recorded follow-up | Direct singleton | None |
| One clearly bounded change requested now | Direct interactive delivery | That explicit bounded request |
| Planning/tasking or multi-unit implementation | Versioned delivery plan and held import | None until Start delivery |
| Unattended work, including one real outcome | Delivery plan (one node is valid) | One exact fingerprint-bound Start delivery action |

Plan creation and import are inert. They do not enable project automation,
install or start a daemon, dispatch workers, release software, spend money, or
satisfy human gates. A Start delivery action must revalidate the exact plan
fingerprint (a stale plan fails) and pass preflight; setup, daemon availability, runner policy, and
external authority remain separate prerequisites.

Tracked modifying work begins with `tusker work start`; a dispatched worker
verifies its already-injected claim instead of claiming again. Implementation
produces objective proof, review produces one typed verdict, and deterministic
Tusker handlers own merge, close, integration, and successor wake. Fresh
automation remains separately opt-in and defaults to
`automation.dispatch_scope: armed_waves`.

Ask users about product outcomes, acceptance, constraints, priorities, and
genuine authority or subjective decisions. The factory resolves task IDs,
epics, dependencies, runners, workspaces, proof mechanics, retries, and daemon
details. If a missing product fact materially changes the result, ask one
bounded question or create a real source-keyed gate; do not make factory
bookkeeping a product decision.
