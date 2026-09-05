---
title: FLW-T-0024 efficiency gate
status: provisional
read_when: Reviewing deterministic workflow cost and completeness gates.
---

# FLW-T-0024 efficiency gate

The W0 corpus and measurement script are supplied by the token-measurement
owner. This report records the frozen denominator and leaves the candidate
gate open until the current CLI, context-reuse runtime, and intentional
oversize/missing-contract fixtures have all run together.

## Evidence identity

- Source revision: `03201019308fbc533e6aeace9f8c612e8b2237aa`.
- Host: `Darwin 25.5.0 arm64`; Go: `go1.26.5`; Python: `3.12.9`.
- Measurement script: [`measure-agent-workflows.py`](../../../scripts/measure-agent-workflows.py),
  version `flw-t-0008-measurement-v2`; fixture artifact: `fixtures-v2.json`.
- Fixture manifest, baseline JSON, and W0 report are linked from
  [`token-baseline.md`](token-baseline.md).

The fixture harness records selected fixture-file bytes, elapsed time, calls,
turns, and repeated file identity. `fixture_inputs_bytes` is an inventory
signal; `agent_context_bytes` is observed CLI stdout. Neither is a tokenizer
or provider billing counter, so these bytes are not agent-token or billed-token
savings. The supplied Muse counters are input `9,046,450`, cached input
`8,933,292`, output `24,389`; they are not assigned to a fixture stage.

## Frozen W0 observations

Every W0 row has 20 repetitions and one process invocation; agent call and turn
counts were not available. The 10/100/1,000
document rows all report median bootstrap fixture-file input of 3,627 bytes
and median bootstrap output of 743 bytes. Median discovery fixture-file input
grows from 1,764 to 177,984 bytes across those fixtures, while the discovery
response stays at 229 bytes and contains one fixture-spec match. That output
does not prove complete discovery. Median next fixture-file input remains
31,335 bytes. No candidate token reduction is claimed; the
`cold_process`/`repeat_fresh_process` labels are fresh subprocess observations.

## Candidate observation

The v2 harness ran `/tmp/tusker-trust-current` (binary SHA-256
`f29a3c3cf2d28133cd68aa8e89fd243ba7611d7a35e584f0c50f956d3cee1ef8`) over the
same 30-row, 20-repetition matrix. The candidate completed 600/600 processes
with the expected semantic outcomes, and its fixture manifest matched W0.
Twenty-nine rows retained the W0 median observed stdout/output bytes; the
three discovery rows changed from 229 to 275 bytes because the candidate adds
`total_matches`, `truncated`, and `limit`. The 1,000-document packet stayed at
14,817 bytes and retained all 120 requirement notes in a direct old/candidate
comparison. The temporary candidate artifacts are in
`/tmp/tusker-flw-current-v2.WYLj3Z/`; no billed-token or 30% savings claim is
supported. This matrix predates the current route-pointer and verified native
resume prompt edits; a rebuilt candidate is required before those changes can
be included in measured comparisons.

## Acceptance status

| Acceptance | Status | Evidence or gap |
| --- | --- | --- |
| A1 | partial | Candidate v2 was measured against the same manifest with 600/600 process and semantic outcomes. The run is provisional until the final candidate rebuild and did not establish provider token savings. |
| A2 | partial | The direct large-packet comparison retained all 120 requirement notes; the compact test's over-budget and missing-contract gate remains pending under the source-coherence hold. |
| A3 | partial | `python3 scripts/measure-agent-workflows.py --check` passed for both supplied W0 artifacts and the temporary candidate artifacts. `TestTrustEfficiencyGate` remains pending. |

## Required verification

`go test ./cmd/tusker -run ^TestTrustEfficiencyGate$ -count=1 -v` is
pending. Release evidence must include input/output, calls, turns, latency,
completeness, exact script/fixture/CLI versions, and separate agent-job cost;
no tracker mutation, live spend, or provider installation is part of this
fixture gate.
