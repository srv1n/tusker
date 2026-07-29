---
capsule:
  what: "Contract for separating Tusker planning, interactive work, unattended execution, fleet health, and skill disclosure so inert work never requires runtime authority."
  use_when:
    - "Changing delivery review/import readiness, interactive work admission, rollout doctor/repair, binary-skill compatibility, or the Tusker operator skill."
  skip_when:
    - "Implementing an already-correct task inside one established execution mode."
---

# Contract convergence and skill disclosure

Status: proposed implementation contract
Date: 2026-07-29

## Outcome

Tusker exposes one truthful contract across its CLI, runtime, project fleet,
and installed operator skill:

- planning and held import remain inert;
- interactive work requires task ownership, not daemon authority;
- unattended execution requires explicit project, wave, runner, workspace, and
  daemon readiness;
- fleet diagnostics report independent health dimensions instead of collapsing
  optional integrations into project quarantine;
- binary, workflow, and skill compatibility is machine-readable and repaired
  through explicit scopes;
- the always-loaded Tusker skill is a compact router that progressively
  discloses only the selected operating mode.

The result removes permission hacks and makes every refusal name the exact
layer, blocker kind, affected IDs, and safe remedy.

## Requirements

| ID | Outcome |
|---|---|
| R1 | One typed readiness contract represents semantic validity, held-import readiness, interactive readiness, unattended configuration, wave authorization, runtime health, and optional integration health as independent facts with structured blockers. |
| R2 | Delivery context, doctor, review, dry-run, and held import succeed without project enablement, a daemon, wave authorization, runner readiness, or a clean live integration lane; only Start applies unattended operational preflight. |
| R3 | A user-opened interactive work session ignores registry polling, automation enablement, daemon liveness, wave authorization, and automated critical-risk dispatch policy while retaining task status, dependency, genuine human gate, ownership, revision, and workspace safety. |
| R4 | Fleet doctor and repair distinguish core compatibility, interactive readiness, automation configuration, authorization, runtime, and optional integrations; optional-provider drift cannot quarantine core project work, and repair never widens authority outside an explicit scope. |
| R5 | Binary capabilities, workflow support, operator-skill source, generated installs, and factory contracts share one machine-readable compatibility fingerprint with deterministic provenance and repair. |
| R6 | The Tusker operator skill uses progressive disclosure: a dense router selects plan, work, or operate guidance; rare workflows remain one-hop references; duplicated normative prose is removed and realistic prompts prove bounded context loading. |
| R7 | Disabled-project, daemon-off, disarmed-wave, stale-optional-provider, missing-skill, legacy-runner, and mixed-fleet fixtures prove the complete contract without enabling automation, starting a daemon, arming work, dispatching a model, moving a ref, or calling a provider. |

## Readiness model

Readiness is a product of independent dimensions, not one boolean:

| Dimension | Answers | May block |
|---|---|---|
| `contract` | Is the plan/task/schema semantically valid? | review, import, work, Start |
| `import` | Can held records reconcile safely? | held import, Start |
| `interactive` | Can this user-directed owner safely claim the task? | `work start` |
| `automation` | Is unattended execution configured? | Start and daemon claims only |
| `authorization` | Is the exact wave fingerprint armed? | Start and future daemon claims only |
| `runtime` | Are daemon, runner, workspace, and integration lane healthy? | Start and unattended execution only |
| `integrations` | Are optional handoff/provider adapters usable? | only workflows that require that integration |

Every blocker is typed. Human gates, dependency blockers, ownership conflicts,
terminal state, unsafe workspaces, infrastructure failures, and optional
integration drift must never be inferred from message substrings.

## Delivery phase boundaries

`tusker delivery review` is a read-only product projection. A semantically
valid plan returns a successful projection even when Start is unavailable.
The projection reports `planValid`, `importReady`, and `startReady`
independently.

`tusker delivery import` validates the contract, bounded context, stable scope,
existing held records, and atomic write safety. It creates or reconciles held,
disarmed records only.

`tusker delivery start` alone checks project opt-in, unattended runner policy,
daemon liveness, workspace isolation, integration cleanliness, exact
authorization material, and the human-confirmed fingerprint.

## Interactive work boundary

Interactive work is direct execution under a user-opened session. It requires:

- a ready or rework task;
- satisfied hard dependencies;
- no genuine open human gate;
- no healthy conflicting owner or owned-path lease;
- a safe exact workspace, branch, source revision, and work revision.

It does not require registry polling, automation enablement, a resident daemon,
an armed wave, or unattended critical-risk authorization. Opening the session
must not enable, arm, dispatch, launch, release, land, or spend.

This delivery builds on the existing
`factory-execution-control/v1/universal-work-session` contract rather than
creating a second work-session protocol.

## Fleet diagnosis and repair

Fleet output preserves every finding but classifies its authority domain.
Missing vaults and incompatible core schemas may quarantine core work. A stale
optional ChatGPT handoff catalog may disable that adapter but cannot quarantine
Tusker planning or interactive work.

Repair is explicit and idempotent:

```text
tusker delivery rollout repair --scope core
tusker delivery rollout repair --scope automation
tusker delivery rollout repair --scope service
tusker delivery rollout repair --scope integrations
```

The default scope is `core`. No scope enables project automation, arms a wave,
changes credentials, calls a provider, or grants release/spending authority.
Service start and automation enablement remain separate explicit operations.

## Skill disclosure contract

The installed package uses this shape:

```text
skills/tusker/
├── SKILL.md
└── references/
    ├── PLAN.md
    ├── WORK.md
    ├── OPERATE.md
    └── <rare-specialized-topic>.md
```

`SKILL.md` is a router, not the manual:

- frontmatter contains only `name` and a trigger-complete `description`;
- the body is at most 900 words and 140 lines;
- it keeps only the capability check, mode selection, universal authority
  boundaries, hard-stop rule, and one compact default loop;
- plan, interactive/dispatched work, and daemon/review/integration mechanics
  live in the three primary one-hop guides;
- rare workflows such as onboarding, Xcode diagnosis, docs publication, or
  Obsidian are linked directly and loaded only when selected;
- a normative rule has one owning file and is not repeated across guides.

Compatibility version and content fingerprints belong to a machine-readable
manifest exposed through `tusker capabilities --json`, not prose duplicated in
skill frontmatter.

## Verification strategy

Focused tests cover each readiness dimension and refusal class. Golden skill
tests measure router words/lines, verify one-hop routes, reject duplicated
normative blocks, and exercise representative prompts for planning,
interactive work, dispatched implementation, dispatched review, human wait,
onboarding, and Xcode failure.

A hermetic mixed-fleet scenario proves:

- review and held import with automation disabled and no daemon;
- interactive work on a disarmed wave;
- exact dependency and human-gate refusals;
- unattended Start remaining blocked until all operational facts pass;
- optional integration drift remaining local;
- core-only repair leaving automation, service, credentials, providers, refs,
  release, and spending untouched;
- compatible binary and skill provenance after sync/copy/bundle installation.

## Non-goals

- This work does not enable automation, arm a wave, start or install a daemon,
  dispatch a model, invoke a provider, read secrets, move a ref, land, release,
  or spend.
- It does not repair the current registered project fleet while authoring or
  importing these tasks.
- It does not weaken unattended Start, wave authorization, runner, workspace,
  integration, release, or spending controls.
- It does not replace the runtime store, work-session ownership service,
  delivery-plan schema, or project knowledge skill.
- It does not turn optional integration failures into ignored diagnostics;
  they remain explicit blockers for workflows that require them.

<!-- tusker:delivery-import:b63a5b4f149e3cf4:begin -->

## Work streams

- `[[ORC-T-0082]]` implements delivery source `contract-convergence-e2e`.
- `[[ORC-T-0078]]` implements delivery source `delivery-phase-separation`.
- `[[ORC-T-0080]]` implements delivery source `fleet-health-and-scoped-repair`.
- `[[ORC-T-0079]]` implements delivery source `interactive-readiness-separation`.
- `[[ORC-T-0081]]` implements delivery source `skill-contract-and-progressive-disclosure`.
- `[[ORC-T-0077]]` implements delivery source `typed-readiness-contract`.

- `[[W-0008]]` is the imported delivery wave.

<!-- tusker:delivery-import:b63a5b4f149e3cf4:end -->

<!-- tusker:delivery-import:9ccfaf3fac394a48:begin -->

- `[[ORC-T-0088]]` implements delivery source `contract-convergence-e2e`.
- `[[ORC-T-0084]]` implements delivery source `delivery-phase-separation`.
- `[[ORC-T-0086]]` implements delivery source `fleet-health-and-scoped-repair`.
- `[[ORC-T-0085]]` implements delivery source `interactive-readiness-separation`.
- `[[ORC-T-0087]]` implements delivery source `skill-contract-and-progressive-disclosure`.
- `[[ORC-T-0083]]` implements delivery source `typed-readiness-contract`.

- `[[W-0009]]` is the imported delivery wave.

<!-- tusker:delivery-import:9ccfaf3fac394a48:end -->
