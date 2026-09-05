---
title: Trust orchestration status and acceptance matrix
status: current
read_when: "Operating or reviewing the current-only trust work through the supported interactive CLI lifecycle."
skip_when: "You need a task-specific contract, proof row, or implementation detail; read the task capsule instead."
---

# Trust orchestration status

Date: 5 September 2026. Scope: the current-only CFX dependency, FLW trust and efficiency work, and the current-only cleanup contract. This is an orchestration record. Every acceptance row below is **pending** until a real owner session, executor proof, independent review, and any named human or external gate produce durable evidence.

The pre-reset tracker, task contracts, evidence, attempts and leases are preserved outside the repository for provenance. The live tracker contains one fresh CFX task and 29 fresh FLW tasks. Source keys, acceptance criteria and ownership intent are preserved; old tracker IDs and temporary shadow scopes are external-only records. No wave is armed, no daemon dispatch is used, and no human actor is impersonated.

## Current source and tracker identity

| Item | Observed value |
| --- | --- |
| Current-source CLI | `/tmp/tusker-trust-current` |
| Version / revision | `v0.0.0-20260903052830-03201019308f+dirty` / `03201019308fbc533e6aeace9f8c612e8b2237aa` |
| Candidate SHA-256 | `9f4748ada8b8f717d59b48e38e946740d57cae40ee81b88d8b4c5c1a63890cc9` |
| Managed config | `.tusker/config.yaml`, SHA-256 `0fed51749e3a1abcfbfe3cba3cbd460fb10743981a23993f4dd9e9c3d14fa309` |
| Managed workflow | `.tusker/WORKFLOW.md`, SHA-256 `98b564069a3e138b74e72905aa03836b13b0b0bb71f63887850fa5c70dfd67c8` |
| Root legacy config | `tusker.yaml` absent after the authorized cleanup |
| Automation | `automation.enabled=false`; no daemon or automation dispatch |
| Registered Tusker project | `enabled=false`, `health=disabled`, `last_error=''`, zero active runs |

The installed `/Users/sarav/.local/bin/tusker` remains stale and is not used for current-source evidence. The candidate's live runner catalog reports `codex_exec` (`codex-cli 0.153.4`) and declared `claude-code`; no `codex_acp` bundle is installed. Config resolution returns `default_runner=codex_exec`, `automation.enabled=false`, and `execute-complex.harness=codex_exec` with `gpt-5.6-terra` at high effort. Execute routing for `FLW-T-0008` returns `profile=execute-complex`, `harness=codex_exec`, and `blockers=[]`.

## Reset and reimport evidence

The final external export is `/tmp/tusker-current-only-export-final2-20260905`. Its preserved pre-reset `.tusker` archive and current post-reset `.tusker` archives are readable, and its 51-file manifest verifies every listed size and SHA-256. The preserved pre-reset archive SHA-256 is `59879352f32a7d158c4612c2a7d086a51a133a7bd22c01db6fae200760b72187`; the source-key map is `9bf06440acd0a4ce64e1428848f4cf9ee216cb6e08ee50b6202d2155b4485d3a`; the fresh task list is `86c319f6739aed32fe63d2a8957a0fb04053d71536a3f2f0811a54dd451412a8`; and the preserved pre-reset task list is `e268d4913e150376629c622ebf4217278f1c7eb223dc552a52080039b9a04a53`. It records 54 pre-reset task files, including 24 temporary shadow IDs as external-only provenance, the complete current source plans, specs, reports, managed config and workflow, and the absence of the root `tusker.yaml`.

The supported dry-run was:

```text
rtk proxy /tmp/tusker-trust-current reset --dry-run --json --vault ./.tusker
```

It returned `ok=true`, removing only the generated repo-local Tusker skill links, the V7 `.tusker` vault, and managed Tusker bootstrap blocks in `AGENTS.md` and `CLAUDE.md`; it preserved `.tusker/specs`. The saved output is `reset-dry-run.json` in the external export. The authorized mutation was:

```text
rtk proxy /tmp/tusker-trust-current reset --yes --json --vault ./.tusker
```

It completed with five actions and reinitialized the vault. Reset restored factory `codex_acp` values, so the authorized narrow managed correction replaced 10 `codex_acp` occurrences in `.tusker/config.yaml` and 3 in `.tusker/WORKFLOW.md`; the duplicate runner entries introduced by that replacement were then removed. The before and after hashes were recorded in the coordinator run; the current hashes are listed above. No task, revision or evidence file was hand-edited.

After reset, `docs map --json` passed. The following dependency order was executed; each row passed `delivery doctor`, inert `delivery import --dry-run`, and inert `delivery import --by agent:lifecycle_steward`. The full command/result stream is `live-reimport.jsonl` in the external export.

The governing documents were subsequently moved from `docs/specs/` into the canonical `.tusker/specs/` directory. The moved files are `.tusker/specs/tusker-trust-and-efficiency.md` (SHA-256 `a22f0d7027f7d30396bd182529cbb91cdcfb5268336b513dd1c1fbc2bd6af281`) and `.tusker/specs/spec-to-proof.md` (SHA-256 `f704fc4f38465348be145d6705c36bb4bdfe247401397c661cf9498cc76fe738`); the source `docs/specs/` paths are absent. All 11 owned delivery plans now point at those canonical paths. Each amended plan passed `delivery doctor` and `delivery import --dry-run` with the existing scope and task mapping.

The first supported live amendment attempt was refused before any document write with `INVALID_TRANSITION`: the selected integration base caused the open, disarmed wave to be treated as frozen to its prior plan fingerprint. The final candidate corrected that predicate for untouched backlog/held waves while retaining refusal for armed, progressed, reviewed or attempt-bearing work. The bounded in-place amendment sequence then completed for all 11 plans with the existing scopes, waves and task IDs; no new scope, shadow task or reset was used.

On the final candidate, supported skill sync restored both repository skill links as symlinks to `skills/tusker`, and all three strict package doctors passed with zero errors and zero warnings. The live registry still contains exactly 30 tasks (`CFX-T-0001` and `FLW-T-0001` through `FLW-T-0029`), all `backlog`/`held`, across open waves `W-0001` through `W-0011`. `docs find` resolves each governing spec to exactly one canonical `.tusker/specs/` path. Final `validate` exits 1 with three `DOCS_MAP_STALE` errors for `docs/system/00-overview.md`, `docs/system/INDEX.md`, and `docs/system/graph.json`, plus 18 warnings; the bounded freeze stops here without regenerating the map.

| Plan | Scope | Fresh task IDs | Plan fingerprint |
| --- | --- | --- | --- |
| `cfx-immutable-context.yaml` | `cfx-immutable-context` | `CFX-T-0001` | `sha256:52b6257bb1e35cbe0dff77706a39b5763582b7a3e494df9b6ca97ac23bcdc938` |
| `spec-to-proof.yaml` | `delivery-2cf085bab753d6d4` | `FLW-T-0001` | `sha256:f008a492eefaf16b78d34ae973f5b1d01a9686e97447e4a86fa762af49ba2e74` |
| `spec-to-proof-hardening.yaml` | `spec-to-proof-hardening` | `FLW-T-0002..0004` | `sha256:f7c169c1c4dbc90f74dfa43628f950492641fad515664f1b67a7bb03fdb80551` |
| `trust-0-foundation.yaml` | `trust-foundation` | `FLW-T-0005..0007` | `sha256:b6478cac69509b1bd2c426a497e2223d82b103943017c2fc04a43227958aa031` |
| `trust-1-contracts.yaml` | `trust-contracts` | `FLW-T-0008..0012` | `sha256:84d16876c49fabba0838f91ec54c657f4d933e019e2c347fbd0bcdf8e450c6fb` |
| `trust-2-proof.yaml` | `trust-proof` | `FLW-T-0013..0015` | `sha256:c48982abfd573cacd7a0a233a22a77d9b1187eecbb42b8522cdf8cd7ce6daea5` |
| `trust-3-execution.yaml` | `trust-execution` | `FLW-T-0016..0019` | `sha256:75a62b44cd3ccbee81068e91a2228c9ec41e05062221fb6ea45ffd640267a848` |
| `trust-4-efficiency.yaml` | `trust-efficiency` | `FLW-T-0020..0023` | `sha256:612f8438ed8658c257b4603a539ca408967e750460750e96506f10c70150f92d` |
| `trust-5-experience.yaml` | `trust-experience` | `FLW-T-0024..0026` | `sha256:4d54b9e680f17d14d90e32d5223a312a38daa44939d0ea873ebb737a11590754` |
| `trust-6-release.yaml` | `trust-release` | `FLW-T-0027..0028` | `sha256:649115dba46fe5637825007f73af8746c8ceafa42ff2177493bd5ae9d91cd690` |
| `current-only-cleanup.yaml` | `current-only-cleanup` | `FLW-T-0029` | `sha256:c0aa095aa768351b565f9931f11f452edb74758585a409e6052f61a3f6940799` |

The CFX task was then set through the supported proof command:

```text
rtk proxy /tmp/tusker-trust-current proof set-mode CFX-T-0001 card --required focused_test,broad_test --evidence-budget 1 --by agent:lifecycle_steward --isolated-vault --vault ./.tusker --json
```

It returned `CFX-T-0001 proof_mode=card proof_status=pending`. This records the required proof shape; it does not prove the implementation. Dashboard build passed. The live task count is exactly 30: `CFX-T-0001` and `FLW-T-0001` through `FLW-T-0029`. All are `status=backlog`, `readiness=held`, and attached to open waves `W-0001` through `W-0011` with `authorization=disarmed`.

## Acceptance matrix

| Wave | Current task | Source key | Acceptance | Next evidence |
| --- | --- | --- | --- | --- |
| W-0001 | CFX-T-0001 | `immutable-delivery-context` | **pending** | immutable-base proof and independent review |
| W-0002 | FLW-T-0001 | `complete-handoff` | **pending** | current owner proof and independent review |
| W-0003 | FLW-T-0002 | `typed-evidence` | **pending** | typed artifact proof and review |
| W-0003 | FLW-T-0003 | `workspace-failure` | **pending** | workspace failure scenario and review |
| W-0003 | FLW-T-0004 | `document-discovery` | **pending** | document discovery scenario and review |
| W-0004 | FLW-T-0005 | `baseline` | **pending** | six-state/three-baseline disposition and focused proof |
| W-0004 | FLW-T-0006 | `state-integrity` | **pending** | corrected durable evidence link and review |
| W-0004 | FLW-T-0007 | `token-baseline` | **pending** | v2 token measurement artifact and review |
| W-0005 | FLW-T-0008 | `preflight` | **pending** | actual readiness scenarios and review |
| W-0005 | FLW-T-0009 | `human-receipts` | **pending** | native receipt round trip or genuine blocker |
| W-0005 | FLW-T-0010 | `handoff` | **pending** | complete contract and identity proof |
| W-0005 | FLW-T-0011 | `cli-guide` | **pending** | executable CLI guide proof |
| W-0005 | FLW-T-0012 | `enforcement-lint` | **pending** | enforcement scenarios and review |
| W-0006 | FLW-T-0013 | `artifact-contract` | **pending** | typed artifact proof |
| W-0006 | FLW-T-0014 | `proof-categories` | **pending** | category-specific proof |
| W-0006 | FLW-T-0015 | `review-closeout` | **pending** | fresh independent review and receipt |
| W-0007 | FLW-T-0016 | `dag-leases` | **pending** | authorized frontier and lease proof |
| W-0007 | FLW-T-0017 | `workspace-recovery` | **pending** | crash, cap and shared-Git recovery proof |
| W-0007 | FLW-T-0018 | `adapter-contract` | **pending** | adapter capability proof |
| W-0007 | FLW-T-0019 | `daemon-process` | **pending** | process lifecycle scenario; no daemon here |
| W-0008 | FLW-T-0020 | `compact-cli` | **pending** | bounded read and action proof |
| W-0008 | FLW-T-0021 | `document-routing` | **pending** | source discovery and disclosure proof |
| W-0008 | FLW-T-0022 | `context-reuse` | **pending** | reuse measurement proof |
| W-0008 | FLW-T-0023 | `efficiency-gate` | **pending** | budget and completeness proof |
| W-0009 | FLW-T-0024 | `graph-ui` | **pending** | UI CUA inspection plus source proof |
| W-0009 | FLW-T-0025 | `fresh-agent` | **pending** | cold-start downstream trial |
| W-0009 | FLW-T-0026 | `docs-lifecycle` | **pending** | authoritative instruction-chain proof |
| W-0010 | FLW-T-0027 | `full-journey` | **pending** | current end-to-end execute/review receipt |
| W-0010 | FLW-T-0028 | `installed-pilot` | **pending** | installed runtime and downstream pilot |
| W-0011 | FLW-T-0029 | `current-only-cleanup` | **pending** | current-only source inventory and cleanup proof |

The complete old-ID to fresh-ID mapping, including the retained original FLW contracts and external-only shadow records, is `source-key-id-map.json` in the external export. The live tracker has no `FLW-T-0030+` task records.

## Supported interactive lifecycle

For a held or disarmed task, inspect with:

```text
rtk proxy /tmp/tusker-trust-current show <TASK-ID> --capsule
rtk proxy /tmp/tusker-trust-current packet <TASK-ID> --for agent --force --json
```

`--force` only permits read-only packet inspection. A real owner then uses this sequence:

```text
rtk proxy /tmp/tusker-trust-current status <TASK-ID> ready --by agent:<owner> --reason "<current adoption or implementation reason>"
rtk proxy /tmp/tusker-trust-current work start <TASK-ID> --by agent:<owner> --source codex --json
rtk proxy /tmp/tusker-trust-current work heartbeat <TASK-ID> --by agent:<owner> --revision <n> --json
rtk proxy /tmp/tusker-trust-current work submit <TASK-ID> --by agent:<owner> --revision <n> --json
rtk proxy /tmp/tusker-trust-current verify add <TASK-ID> --covers <A-ID> --check "command: <actual named test or scenario>" --result pending
rtk proxy /tmp/tusker-trust-current work review <TASK-ID> --by reviewer:<name> --source codex
rtk proxy /tmp/tusker-trust-current review submit <TASK-ID> ... --verdict pass|changes_requested|blocked
rtk proxy /tmp/tusker-trust-current close <TASK-ID> --by reviewer:<real-reviewer> --reason "<evidence-backed reason>" --json
```

The owner session must be genuine current-source adoption or implementation. Completed historical work is not retroactively represented as an execute session. The review packet's `next` command is authoritative and binds the review to the completed implementation workspace, source material fingerprint, proof fingerprint and gate fingerprint. A zero-match selector, wrapper that only invokes other tests, stale log, or invented human actor does not prove acceptance.

## Acceptance blockers and friction

- Root-held grouped Go validation and the final source-coherence window remain pending.
- Existing implementation changes need genuine current-source owner sessions, executor-recorded command results, and separate independent reviews. Fresh reviews must use the candidate's `work review` route.
- Context-efficiency measurements, fresh Muse lifecycle validation, UI graph inspection, downstream cold-start/pilot checks, and the final ChatGPT adversarial review remain pending.
- The native human-confirmation gesture is the only mandatory human gate. UI screenshot, keyboard and contrast inspection can be machine/UI-agent work; a locked Mac is an environment blocker when it prevents the actual gesture.
- Final-candidate validation exits 1 with three generated-doc-map freshness errors and 18 plan-hygiene warnings. The canonical spec lookups and bounded in-place amendments pass; the generated map remains a concrete metadata blocker for a later supported `docs map` repair.
- The held-packet route returns `INVALID_TRANSITION` without `--force`; that is an inspection friction, not a dispatch authorization. The first live canonical-spec amendment exposed a freeze-predicate bug; the final candidate corrected it and the in-place route now preserves every current scope and source-key ID while still refusing progressed or armed work.
- Reset currently restores unavailable `codex_acp` defaults. The managed correction is a repeatable narrow post-reset operation and should be fixed in the factory/default configuration in a future source change; automation remains disabled here. The current source locations are `cmd/tusker/commands_create.go` (generated config), `cmd/tusker/runner_catalog.go` (catalog profile promotion), and `cmd/tusker/runner_profiles.go` (built-in profile defaults).

No row is marked pass from import, reset, dashboard generation, static evidence, or this report. The next valid mutations are owner-scoped `ready` and genuine `work start` operations on the fresh IDs, followed by real proof, review and close checks.
