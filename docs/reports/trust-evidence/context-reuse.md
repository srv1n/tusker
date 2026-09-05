---
title: FLW-T-0023 context reuse
status: provisional
read_when: Reviewing safe context reuse and harness-session invalidation.
---

# FLW-T-0023 context reuse

The current runtime already persists session identity and rejects reuse when
project, record, runner, work revision, resumability, or workspace identity
changes. Codex ACP additionally binds provider sessions to its adapter
fingerprint and authority context. Generic ACP still reports no resumable
session or usage metrics, and the durable generic session row does not carry
specification or capability fingerprints. A warm provider-context reuse claim
therefore remains unsupported until the execution owner supplies those
capabilities and measurements.

## Diagnosis

- `RunnerSession` persists project/record/runner, session and message refs,
  workspace, current item, work revision, and state. The existing
  `incompatibleResumeSessionReason` guard compares the durable project,
  record, runner, work revision, resumability, and workspace before reuse.
- The generic session row has no task/spec/capability fingerprint or authority
  epoch. Reusing a matching tuple still cannot prove all of A2.
- Codex ACP session references include adapter fingerprint, runner profile,
  authority principal digest, originating attempt, and work revision; the
  adapter rejects mismatches before provider observation.
- Generic ACP and Codex ACP currently advertise `ResumeSession: false` and
  `UsageMetrics: false`; no native prompt-cache capability is reported.
- `daemon.go` now keeps the full prompt as the default, then rewrites the
  prompt only after `resolveResumeSession` accepts a compatible native session.
  The compact resume message carries task/state/work revisions, project,
  record, workspace, runner profile/harness/model/effort, policy authority,
  attempt identity, and the prior outcome/error. Missing identity falls back
  to the full packet, so a fresh or invalidated session never receives an
  unexplained delta.
- Full and compact prompt artifacts carry a stable `sha256` context marker
  over the current project, workflow, task body/data/revisions, workspace,
  runner identity, and policy fingerprints. The resolver compares that marker
  with the `PromptPath` of the session's durable `LastAttemptID`; missing,
  malformed, unavailable, or changed markers force the complete prompt path.
  This catches body, workflow, harness, model, effort, and policy drift
  without adding a runtime schema or cache layer.
- Full-content fallback remains the safe path. No delta or cache framework was
  added in this workstream.

## Evidence identity

- Source revision: `03201019308fbc533e6aeace9f8c612e8b2237aa`.
- Host: `Darwin 25.5.0 arm64`; Go: `go1.26.5`.
- W0 fixture-file method and command observations: [`token-baseline.md`](token-baseline.md).
- Supplied Muse trace: `.tusker/scratch/FLW-T-0006/muse-events.jsonl`; observed
  input `9,046,450`, cached input `8,933,292`, output `24,389`.

The W0 `fixture_inputs_bytes` values are selected fixture-file byte sums, not
agent tokens or provider billing. The W0 `agent_context_bytes` values are
observed CLI stdout bytes. The `cold_process` and
`repeat_fresh_process` labels are fresh subprocess labels, not evidence of a
warm runtime cache. The Muse counters are observed values, not billed-dollar
assumptions, and the trace does not identify a compact-cli stage or a
reusable-context boundary.

## Candidate observation

The v2 harness ran `/tmp/tusker-trust-current` (binary SHA-256
`f29a3c3cf2d28133cd68aa8e89fd243ba7611d7a35e584f0c50f956d3cee1ef8`) over the
same 30-row, 20-repetition matrix as W0. All 600 processes returned the
expected semantic outcomes and the fixture manifest matched. Twenty-nine
rows had identical median observed stdout/output bytes; discovery increased
from 229 to 275 bytes due explicit result metadata. Both conditions still
launch fresh processes, so this run demonstrates no warm runtime reuse or
30% context reduction. A direct 1,000-document packet comparison retained
all 120 contract requirement notes in both old and candidate output.

The generated candidate artifacts are temporarily available at
`/tmp/tusker-flw-current-v2.WYLj3Z/`. The harness's `agent_context_bytes` is
observed CLI stdout, not provider tokens. The supplied Muse receipt remains a
separate aggregate observation with no per-stage reuse attribution.

This candidate snapshot predates the route-pointer and verified native resume
prompt edits now in the working tree. The focused resume test and a rebuilt
candidate matrix are still required before reporting measured prompt bytes or
any end-to-end reduction.

## Acceptance status

| Acceptance | Status | Evidence or gap |
| --- | --- | --- |
| A1 | partial | The candidate matrix is actual output evidence, and the daemon now reuses verified native session history with a bounded current delta. The 29/30 unchanged medians and fresh-process harness still provide no end-to-end token reduction evidence. |
| A2 | partial | `TestTrustContextReuse` now exercises the real resolver with a durable attempt/session pair and rejects task-body, workflow, harness, policy, and missing-marker drift. Provider capability fingerprints and stale-cursor checks remain with the execution owner. |
| A3 | partial | Generic ACP truthfully exposes no resume/cache/usage support and keeps full-content fallback. No adapter supplied per-stage cached/uncached counters, so native prompt caching remains unproven. |

## Required verification

`go test ./cmd/tusker -run ^TestTrustContextReuse$ -count=1 -v` remains
pending during the source-coherence hold. The test covers deterministic cold /
warm packet reconstruction, complete refresh after a material edit,
exact full-versus-resume prompt byte reduction, real resolver acceptance for
unchanged durable context, and full fallback after session invalidation or
context drift. Full acceptance still requires comparable provider token and
tool-call measurements and execution-owner coverage for provider capability
fingerprints, stale cursors, and native cache receipts. No tool call should be
added merely to conceal context.
