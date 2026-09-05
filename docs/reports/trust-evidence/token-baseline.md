---
title: FLW-T-0008 token baseline
status: measured
read_when: Reviewing measured context and provider boundaries.
---

# FLW-T-0008 token baseline

This Wave 0 denominator measures returned bytes and process timings from read-only CLI projections in validated disposable vaults; fixture input bytes are inventory only.

## Evidence identity

- Script: [`scripts/measure-agent-workflows.py`](../../../scripts/measure-agent-workflows.py), version `flw-t-0008-measurement-v2`, SHA-256 `sha256:4c8720b316e47abedc2fb297d038c00e304c8e515e4fa3904caf92a98607b18e`.
- CLI: `/tmp/tusker-flw`; version `v0.0.0-20260903052830-03201019308f+dirty`, revision `03201019308fbc533e6aeace9f8c612e8b2237aa`, binary SHA-256 `25a345b4912b54b1443e520d2de8f8a428c218e95e36052f2a0bf895bd95751e`.
- Source snapshot captured at measurement: revision `03201019308fbc533e6aeace9f8c612e8b2237aa`; dirty `True`; status SHA-256 `sha256:32231097f7470cc0f0f7d765741c20d76cfc183dea99509c875e7b642607ae7c`; host `Darwin 25.5.0 arm64`; Python `3.12.9`.
- Observed at: `2026-09-05T08:06:58Z`.
- Fixtures: [`fixtures-v2.json`](../agent-efficiency/fixtures-v2.json), manifest SHA-256 `sha256:0d7607e7052585d1b2769a5f5f5c0a6f983350c062fa05399c22b704aa1d156f`.
- Baseline JSON: [`token-baseline.json`](../agent-efficiency/token-baseline.json).

## Fixture coverage

Temporary vaults pass canonical frontmatter validation before observation: small/medium/large contracts, a 120-note long contract, branching DAG, human gate, failed proof, handoff recovery and completion candidate. Corpora contain exactly 10, 100 and 1,000 documents.

| Fixture | Documents | Contract classes | Scenario records |
| --- | ---: | --- | --- |
| `docs-10`, `docs-100`, `docs-1000` | 10, 100, 1,000 | small / medium / large | long contract / branching DAG / blocked gate / failed proof / handoff recovery |

## Commands and observed semantic outcomes

These are read-only CLI projections. Exit zero means a result was emitted; it does not make a blocked, partial or continuing workflow pass.

| Stage | Condition | Command | Expected observed outcome |
| --- | --- | --- | --- |
| `bootstrap` | `cold_process` | `'<cli>' show SML-T-0001 --vault .tusker --capsule --json` | `task:SML-T-0001` |
| `bootstrap` | `repeat_fresh_process` | `'<cli>' show SML-T-0001 --vault .tusker --capsule --json` | `task:SML-T-0001` |
| `discovery` | `steady` | `'<cli>' docs find 'token baseline' --vault .tusker --json` | `match:.tusker/specs/token-baseline.md` |
| `next` | `steady` | `'<cli>' next --vault .tusker --epic NXT --json` | `item:NXT-T-0001` |
| `blocked-gate` | `steady` | `'<cli>' next --vault .tusker --epic BLK --explain --json` | `blocked:item:null` |
| `packet` | `steady` | `'<cli>' packet LRG-T-0001 --vault .tusker --for agent --force --json` | `task:LRG-T-0001` |
| `verification` | `steady` | `'<cli>' proof status FLR-T-0001 --vault .tusker --json` | `proof:partial` |
| `review` | `steady` | `'<cli>' packet BRN-T-0001 --vault .tusker --for reviewer --json` | `task:BRN-T-0001` |
| `recovery` | `steady` | `'<cli>' show RES-T-0001-A-0001 --vault .tusker --full` | `attempt:RES-T-0001-A-0001` |
| `completion` | `steady` | `'<cli>' closeout status CMP-T-0001 --vault .tusker --json` | `closeout:continue` |

## Measured rows

`input_bytes` is normalized argv JSON; `output_bytes` is stdout+stderr; `agent_context_bytes` is stdout. Fixture input bytes are separate inventory. Each sample is one fresh process invocation; OS/filesystem cache state is uncontrolled, and no warm in-process cache is measured. Agent tool calls, turns and retries are not observed.

| Docs | Condition | Stage | N | Process OK | Median ms | p95 ms | Median input bytes | Agent context bytes | Output bytes | Fixture input bytes |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | cold_process | bootstrap | 20 | 20 | 61.82 | 91.636 | 70 | 743 | 743 | 3627 |
| 10 | repeat_fresh_process | bootstrap | 20 | 20 | 61.812 | 73.726 | 70 | 743 | 743 | 3627 |
| 10 | steady | discovery | 20 | 20 | 53.38 | 69.943 | 69 | 229 | 229 | 1764 |
| 10 | steady | next | 20 | 20 | 70.906 | 93.717 | 60 | 878 | 878 | 31335 |
| 10 | steady | blocked-gate | 20 | 20 | 77.146 | 163.331 | 72 | 1748 | 1748 | 4226 |
| 10 | steady | packet | 20 | 20 | 54.149 | 82.819 | 86 | 14817 | 14817 | 14914 |
| 10 | steady | verification | 20 | 20 | 46.123 | 92.675 | 68 | 981 | 981 | 3666 |
| 10 | steady | review | 20 | 20 | 71.898 | 115.504 | 79 | 2726 | 2726 | 3383 |
| 10 | steady | recovery | 20 | 20 | 65.576 | 125.068 | 65 | 367 | 367 | 1967 |
| 10 | steady | completion | 20 | 20 | 63.502 | 142.711 | 71 | 601 | 601 | 3486 |
| 100 | cold_process | bootstrap | 20 | 20 | 72.523 | 162.619 | 70 | 743 | 743 | 3627 |
| 100 | repeat_fresh_process | bootstrap | 20 | 20 | 59.502 | 104.247 | 70 | 743 | 743 | 3627 |
| 100 | steady | discovery | 20 | 20 | 57.442 | 77.144 | 69 | 229 | 229 | 17784 |
| 100 | steady | next | 20 | 20 | 82.368 | 113.991 | 60 | 878 | 878 | 31335 |
| 100 | steady | blocked-gate | 20 | 20 | 92.298 | 155.084 | 72 | 1748 | 1748 | 4226 |
| 100 | steady | packet | 20 | 20 | 66.096 | 107.267 | 86 | 14817 | 14817 | 14914 |
| 100 | steady | verification | 20 | 20 | 61.145 | 115.642 | 68 | 981 | 981 | 3666 |
| 100 | steady | review | 20 | 20 | 76.685 | 178.602 | 79 | 2726 | 2726 | 3383 |
| 100 | steady | recovery | 20 | 20 | 55.372 | 87.862 | 65 | 367 | 367 | 1967 |
| 100 | steady | completion | 20 | 20 | 62.263 | 86.661 | 71 | 601 | 601 | 3486 |
| 1000 | cold_process | bootstrap | 20 | 20 | 139.002 | 202.764 | 70 | 743 | 743 | 3627 |
| 1000 | repeat_fresh_process | bootstrap | 20 | 20 | 114.231 | 154.424 | 70 | 743 | 743 | 3627 |
| 1000 | steady | discovery | 20 | 20 | 135.582 | 198.992 | 69 | 229 | 229 | 177984 |
| 1000 | steady | next | 20 | 20 | 139.769 | 195.8 | 60 | 878 | 878 | 31335 |
| 1000 | steady | blocked-gate | 20 | 20 | 264.888 | 394.751 | 72 | 1748 | 1748 | 4226 |
| 1000 | steady | packet | 20 | 20 | 122.654 | 181.965 | 86 | 14817 | 14817 | 14914 |
| 1000 | steady | verification | 20 | 20 | 94.926 | 185.199 | 68 | 981 | 981 | 3666 |
| 1000 | steady | review | 20 | 20 | 100.624 | 185.185 | 79 | 2726 | 2726 | 3383 |
| 1000 | steady | recovery | 20 | 20 | 86.087 | 137.81 | 65 | 367 | 367 | 1967 |
| 1000 | steady | completion | 20 | 20 | 95.869 | 153.822 | 71 | 601 | 601 | 3486 |

## Token and provider boundary

No tokenizer is exposed by the installed CLI or stdlib harness. Token fields are therefore absent from sample rows rather than filled with byte-derived estimates. Fixture input size and CLI output size are not provider-billed usage.

The durable supplied Muse Spark 1.3 receipt is [`muse-usage-receipt.json`](../agent-efficiency/muse-usage-receipt.json) with SHA-256 `sha256:df2a3775438b6a87edabbdae19273ef70995c09ffb28e991c1b262acd3d239f5`. It contains 2 unassigned observations; none has exact fixture-stage blame.
Selected source session metadata, usage, token-count and completion records are archived in [`muse-usage-events.jsonl`](../agent-efficiency/muse-usage-events.jsonl) with SHA-256 `sha256:9f088371683a70b4456a222a01c79360a9adb47464e716309a32c1925449b382`; original scratch event hashes remain unavailable-after-reset references.
- `FLW-T-0006` thread `01a0705e-3f54-7723-96fa-6a43ab39bbdb`: in `9046450`, cached `8933292`, uncached `113158`, out `24389`, reasoning `9359`; `232` events; src `sha256:9f088371683a70b4456a222a01c79360a9adb47464e716309a32c1925449b382`.
- `FLW-cleanup` thread `01a07083-f511-7ce0-b66a-df5457613b8c`: in `1842309`, cached `1746087`, uncached `96222`, out `11588`, reasoning `6139`; `55` events; src `sha256:9f088371683a70b4456a222a01c79360a9adb47464e716309a32c1925449b382`.

## Frozen targets

Targets in `.tusker/specs/tusker-trust-and-efficiency.md` stay frozen: context -30%; warm bootstrap <=1,200; next/status <=350; capsule <=500; routing <=50/node with 800-token shortlist; p95 regression <=10%. No target pass is claimed without tokenizer data and an optimized comparison.

## Reproduction and limits

```text
python3 scripts/measure-agent-workflows.py --cli /tmp/tusker-flw
python3 scripts/measure-agent-workflows.py --check
go test ./cmd/tusker -run ^TestTrustTokenBaseline$ -count=1 -v
```

Self-check: `python3 scripts/measure-agent-workflows.py --check` -> `PASS`. Go regression: held during the current no-build hold. Read-only projections only; bytes/timings are recorded, while warm cache, live recovery, human acceptance, exact tokens and old/new savings remain open. Follow-ons FLW-T-0021/23/24 should rerun with tokenizer/provider counters.
