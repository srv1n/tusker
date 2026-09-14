---
subject: repeatable-work-testing
title: Repeatable work scenarios and CLI parity
keywords: [demo, seed, reset, scenarios, CLI, testing, parallel waves]
part_of: work-area-redesign
status: canonical
created: 2026-09-07
read_when: "Building repeatable CLI-driven task/wave scenarios or auditing UI and CLI capability parity."
skip_when: "Starting real customer work or interpreting sample runs as provider conformance."
sources: [work-area-redesign.md, work-knowledge-and-retention.md]
decisions_locked: false
updates: [docs/system/cli.md, docs/system/tasks-and-proof.md, docs/system/runners-and-acp.md]
capsule:
  what: "A disposable project with real deterministic executions, repeatable reset and machine-readable controls."
  use_when: "Implementing end-to-end Work testing without computer access or paid model calls."
  skip_when: "Reviewing static component screenshots alone."
---

# Repeatable work scenarios and CLI parity

## Outcome

An agent can create a disposable project, populate a realistic work structure, manually start two independent waves together, observe actual progress and review, inspect evidence, and reset the project for another run entirely through Tusker's CLI. A human can open that same project's normal UI and observe the same state. No UI permission, GUI scripting or paid provider is needed to exercise this scenario.

The user requested this specification, including two parallel waves containing simple timer-based tasks, and broadened the design session to include backend and CLI behavior. This document specifies future work; it does not start a daemon, dispatch workers, seed data, or delete anything. The two-wave capability is a new test requirement beyond the earlier single-wave pilot, not a claim that current scheduling supports it.

## Reuse and boundaries

Reuse the native task contracts, direct task and wave authoring, wave authorization, attempt lifecycle, event stream, review/proof and query paths. Existing `e2e/agent_journey/fixture` provides a disposable offline repository and two-task contract; extend or compose that pattern. Existing WUX component previews remain useful visual fixtures but cannot prove real transitions; the integration preview currently has no-op task/wave navigation callbacks.

Implement a tiny deterministic test runner through the existing runner boundary. It executes fixed local fixture operations (delay, write an owned file, emit bounded progress, return a declared result), not an LLM or new general agent harness. Use existing executable-runner facilities if they cover this. Keep test profiles explicitly labeled and scoped to demo projects. A test run is a real Tusker attempt with a simulated coding agent; it proves orchestration, not ACP/provider compatibility or independent judgment quality.

Never hand-edit lifecycle front matter or fabricate successful runs, reviews or proof. The test reviewer is a separate attempt that checks the declared file/result and submits through the normal review protocol. Its deterministic checks exercise reviewer routing and acceptance without claiming AI review quality.

## Default fixture: three waves, twelve tasks

A seed creates a named demo project, a short overview, a spec, one decision record, tags, configured test profiles for all three work levels, and the following graph. Domain values and reports are explicitly fictional. Retain backend-required grouping only as compatibility bookkeeping, never an Epic step in the human flow.

| Wave | Tasks and dependencies | Initial state |
|---|---|---|
| Alpha: assemble a small report | A1 → A2 and A3 → A4 | Ready to start |
| Beta: assemble an independent report | B1 → B2 and B3 → B4 | Ready to start |
| Follow-up: combine the reports | A4 and B4 → C1 → C2 and C3 → C4 | Planned, with actual unmet dependencies |

Alpha and Beta own separate directories in the same repository. The follow-up owns a third directory. Each task creates/checks a small deterministic text/JSON artifact. Proposed visible delays: 3 seconds for roots, 6 and 9 seconds for branches, 3 seconds for joins; automation tests can request shorter delays. Wall-clock exactness is not a correctness assumption. Review runs also create observable stage transitions.

The main project uses all three levels with explicit mappings to test implementation/review profiles; the execution view reports those actual profiles. An optional second tiny project supports project-switching/persistence checks without enlarging the main scenario. Ready state must come from real readiness rules, not from the scenario label. If current contracts cannot express readiness, seeding must report that unmet capability.

The first two waves are independently manually startable. Starting Alpha must not implicitly start Beta or Follow-up. An explicit CLI request can start both Alpha and Beta, preserving per-wave results and failures. Follow-up can become ready after dependencies satisfy native completion rules, but never starts itself. Concurrent execution must be observable as overlapping attempt intervals, not merely queued tasks. Branches may execute concurrently within available capacity. Declare required wave/task/runner capacity in the fixture and report unmet capacity without modifying unrelated global settings.

## CLI contract

Every product operation and domain state available through the UI must be invocable or readable through the CLI with the same validation, permissions and effects. Pixel interactions such as hover are not separate commands; their persistent meaning (project order, expanded state, selection or saved graph position) must have a supported preference interface if retained as product state. Device/profile scope must be explicit; the CLI must not claim to control unrelated browser-local storage. Visual correctness still requires browser rendering.

The command spellings below are **proposed**, not existing executable commands. Before implementing, inventory `tusker capabilities --json`, current CLI commands and UI action endpoints. Reuse supported verbs and expose missing operations through shared domain services. Never introduce a second lifecycle implementation in demo code.

```text
tusker demo seed --repo <dedicated-demo-path> --scenario parallel-waves --json
tusker demo status --repo <dedicated-demo-path> --json
tusker demo run --repo <dedicated-demo-path> --waves alpha,beta --json
tusker demo wait --repo <dedicated-demo-path> --until terminal --timeout 120s --json
tusker demo check --repo <dedicated-demo-path> --json
tusker demo reset --repo <dedicated-demo-path> --dry-run --json
tusker demo reset --repo <dedicated-demo-path> --yes --json
```

`seed` is inert: no automatic start, provider invocation or mutation of non-demo projects. It returns scenario version, project/repo/vault identity, task/wave ID mapping, current settings, unmet capabilities and useful next commands. Repeating seed with the same fixture returns the existing mapping without duplicates; version mismatch requires explicit reset/migration. Validate schema and conflicts before applying; interrupted seed can resume or report its partial manifest clearly.

`run` explicitly authorizes only the named demo waves. It uses the supported execution owner; if the independently managed runtime is unavailable, report the exact precondition rather than launching an undocumented nested worker or daemon. Deterministic runner setup must obey the same execution ownership as other runners. Demo setup does not broadly enable resident automation. Each wave's start result is returned separately; partial success must not be hidden or automatically rolled back by discarding real progress.

`status` and `wait` expose wave/task stage, dependencies, actual profile/model/transport when known, attempt identity, freshness, artifacts and reasons for blocked/unavailable state. `wait` has a bounded timeout and supports interruption; its timeout is not a failed task. Prefer an existing watch/event facility to polling entire task documents. `check` reports named invariant results with expected/observed values and references, not a generic success message.

`reset` returns to the seeded initial state with new run identities and cleared demo preferences/artifacts where requested. Default is preview. Applying reset requires an exact demo ownership marker and path match, rejects real projects/symlink escapes, and refuses active attempts unless the caller explicitly stops them through supported cancellation and waits for termination first. It never follows artifact references outside owned demo storage. A failed cleanup reports remaining state and can be retried. Repeated reset is safe. Preserve optional run exports outside the reset-owned directory. Existing broad `purge` is not the implementation's safety boundary.

All commands support compact human output and structured JSON. Errors contain stable codes, actionable reasons and current next steps; never require text scraping. Proposed exit categories: success, invalid request, unmet precondition, failed scenario assertion, timeout and internal error. Choose numeric values consistent with existing CLI conventions. Command output must not print credentials or full noisy logs by default.

## UI/CLI parity inventory

| User capability | Required CLI equivalent |
|---|---|
| Add/select/configure a project | Registration, project details/settings and explicit selection scope |
| Reorder projects and restore navigation | Read/write scoped preferences; document device/profile ownership |
| Find/open/create/update documents and metadata | Bounded discovery, exact document reads, validated writes, backlink and freshness inspection |
| Prepare tasks and waves | Contracts/import, dependency inspection, work-level assignment and validation |
| Start/stop/retry work | Same guarded actions and recovery rules as the UI |
| Inspect graph and task panel | Nodes/edges, unresolved dependency facts, task detail and current attempt |
| Inspect results | Verification/review summaries and artifact listing/access/expiry metadata |
| Resolve an explicit human gate | Equivalent command preserving human identity/authority; no bypass |
| Configure model levels/fallbacks | Global/project settings, effective resolution and validation |
| Keep or expire evidence | Retention metadata and supported retention operations |

Produce a checked parity manifest in the implementation report: UI action/read, existing command, missing command, shared backend operation, validation test. Do not promise unsupported CLI operations merely because a UI button exists. Agent access to a command does not grant authority to act as a human approver.

## Repeatable scenarios and assertions

| Scenario | Required observations |
|---|---|
| Fresh seed | Exactly three waves/twelve tasks, correct graph and profiles, no active runs; repeat seed adds nothing |
| Parallel happy path | Explicit Alpha/Beta start; overlapping wave execution; joins wait for both branches; implementation and review visible; accepted outputs match files |
| Follow-up | Unmet predecessors block start with reasons; later readiness does not auto-start; manual start completes normally |
| Deterministic failure | Configured branch fails once; dependent join stays blocked; unrelated work proceeds; supported retry succeeds without duplicate accepted output |
| Review rejection | Reviewer rejects a known incorrect artifact; no premature completion; supported correction/retry produces acceptance |
| Human gate | Explicit optional scenario gate alone causes Needs you; evidence attachments alone never create it; authorized resolution resumes the supported path |
| Unavailable profile | Missing configured runner yields an actionable state; no silent substitution; only explicitly configured fallback may run, with provenance |
| Stop and restart | Cancel a running delay; no leaked process or late success; runtime restart/recovery preserves truthful attempt state |
| Evidence | Actual downloadable artifact/report is bound to its attempt; expiry yields a visible expired reference while the compact result remains; Keep protects it |
| Reset | Preview makes no changes; active-run and wrong-root protections hold; applied reset restores initial counts/state; a second complete run passes |
| Machine-only use | Seed, run, inspect, check and reset complete without GUI, screenshots or real provider credentials |
| UI comparison | Normal UI renders the same IDs, states, graph, results and settings as CLI snapshots at matched revisions |

Failure/review/gate/expiry variants should be simple fixture options, not a combinatorial scenario framework. Use controlled time advancement for retention tests instead of waiting seven days. Reports record fixture version, candidate revision/version, scope, commands, timings, run IDs, PASS/FAIL and first actionable error. Keep screenshots separate from orchestration proof. A fresh screenshot-only critic remains required for visual changes under the Work spec.

## Acceptance and delivery slices

1. **CLI parity inventory and fixture contract:** inspect current commands and UI actions, name missing capabilities, validate the scenario manifest and proposed verb reuse. Deliver the mapping and runnable fixture validation.
2. **Deterministic execution and seed/reset:** native fixture import plus minimal test implementation/reviewer profiles, safe ownership, idempotent seed/reset. Test overlapping attempts, joins and cleanup protection. Runtime owner implements this slice.
3. **Scenario driver and assertions:** bounded wait/inspect/check using existing queries/events; repeat happy path and targeted failures on a fresh candidate without browser interaction. Report explicit unsupported cases rather than forging state.
4. **Human UI walkthrough:** use the same seeded project in the assembled application, headless browser where automation is needed; verify navigation, mixed states, inspector and results. Capture screenshots and critic findings, with no desktop input contention.

Slice 1 precedes command/interface implementation. Slices 2 and 3 share the agreed fixture contract; UI walkthrough follows a working seed and assembled UI. Each slice updates the current system documentation for the behavior it actually lands, including CLI usage, runner-test limitations and proof semantics. These are implementation slices in this spec, not newly imported or dispatched task records.

## Limits and remaining decisions

Real Codex/Claude/other-provider execution and ACP transport require a separate opt-in conformance run; deterministic timers cannot establish them. No new general harness, production mock mode, public demo service or bespoke scenario language. Proposed CLI spellings, fixture ownership paths and scheduler capacity integration require source-level agreement with the runtime owner. These do not change the user's confirmed requirement for a fully repeatable, CLI-driven two-wave exercise.
