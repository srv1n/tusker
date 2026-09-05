---
title: FLW-T-0021 compact CLI
status: provisional
read_when: Reviewing bounded Tusker reads and capsule routing.
---

# FLW-T-0021 compact CLI

This report records the compact-output source change and the remaining
behavioral gates. Mandatory task packets keep their complete contracts; the
supporting routed knowledge summaries are bounded and retain a path to every
omitted file.

## Source change

- `cmd/tusker/v7_capsule.go` limits routed file capsules to eight visible files.
- Repeated short routed capsules are now path pointers because the same
  summaries already appear in project/domain context; over-budget capsules
  retain an explicit shortened marker and their complete route path.
- Additional route files produce one continuation line containing every
  omitted path, so the remainder is directly navigable.
- `cmd/tusker/capsule.go` applies the same bound to one-line capsule summaries
  used by search and domain routing, with an ID/path continuation target.
- Task packet contract text is still emitted with `strings.TrimSpace(task.Body)`
  and is outside the supporting-capsule bound.

## Evidence identity

- Source revision: `03201019308fbc533e6aeace9f8c612e8b2237aa`.
- Host: `Darwin 25.5.0 arm64`; Go: `go1.26.5`; Python: `3.12.9`.
- Frozen W0 command: `/tmp/tusker-flw`, revision
  `03201019308fbc533e6aeace9f8c612e8b2237aa`, SHA-256
  `25a345b4912b54b1443e520d2de8f8a428c218e95e36052f2a0bf895bd95751e`.
- Baseline measurements and fixture identity: [`token-baseline.md`](token-baseline.md)
  and [`token-baseline.json`](../agent-efficiency/token-baseline.json); the
  current artifact set uses `flw-t-0008-measurement-v2` / `fixtures-v2.json`.

The installed baseline binary predates this working-tree change, so its
outputs are a denominator only. In v2, `fixture_inputs_bytes` is the UTF-8
size of selected fixture files and `agent_context_bytes` is observed CLI
stdout; neither is an agent or provider token count. No before/after savings
claim is made here.

## Candidate observation

The v2 harness ran the same validated 10/100/1,000-document corpus against
`/tmp/tusker-trust-current` (binary SHA-256
`f29a3c3cf2d28133cd68aa8e89fd243ba7611d7a35e584f0c50f956d3cee1ef8`). It
executed 30 rows with 20 fresh-process repetitions each: 600/600 processes
returned the expected semantic outcome and the fixture manifest matched W0.
Twenty-nine rows retained the W0 median `agent_context_bytes` and output
bytes. Discovery grew from 229 to 275 observed stdout bytes because the
candidate includes `total_matches`, `truncated`, and `limit`; that is not a
savings result. The long packet remained 14,817 observed bytes and retained
the complete 120-note contract in a direct old/candidate fixture comparison.
The generated candidate artifacts are in
`/tmp/tusker-flw-current-v2.WYLj3Z/`; this temporary path is evidence for the
run, not a durable report artifact.

That matrix predates the route-pointer deduplication and verified native
resume prompt source edits in the current working tree. A fresh candidate
binary must be measured before these latest source changes can support a new
before/after claim.

## Verification

- `gofmt -w cmd/tusker/capsule.go cmd/tusker/v7_capsule.go
  cmd/tusker/trust_compact_cli_test.go` — PASS (formatting completed).
- `python3 scripts/measure-agent-workflows.py --check --baseline
  /tmp/tusker-flw-current-v2.WYLj3Z/token-baseline.json --fixtures
  /tmp/tusker-flw-current-v2.WYLj3Z/fixtures-v2.json` — PASS (candidate
  matrix and manifest self-check).
- Required command:
  `go test ./cmd/tusker -run ^TestTrustCompactCli$ -count=1 -v` — pending the
  root source-coherence window; not claimed as passed.
- The focused test covers actual `list --json` truncation fields, a structured
  `show --json` capsule budget, over-budget routed capsules, and omitted route
  paths.

## Acceptance status

| Acceptance | Status | Evidence or gap |
| --- | --- | --- |
| A1 | partial | Existing list/show/capsule surfaces are reused. The `commands_v7.go` owner still needs to expose revision, reason, and next-action fields in the routine structured projection. |
| A2 | partial | List truncation and routed-capsule continuation are covered. `next --explain` still needs an owner change to bound skipped rows while retaining a follow-up action. |
| A3 | partial | Candidate v2 completed the comparable matrix with correct semantics, but observed stdout is a byte measure, the tokenizer is unavailable, and no token savings claim is supported. |

## Frozen W0 observations

From the measured v2 fixture corpus, the installed baseline recorded one
process invocation per row; agent call and turn counts were not available.
For 10/100/1,000 document fixtures, median `next` fixture-file input was
31,335 bytes and median output was 878 bytes; median
`blocked-gate` output was 1,748 bytes. Discovery fixture-file input grew from
1,764 to 177,984 bytes while the observed stdout response stayed at 229 bytes
and contained one fixture-spec match, so it is not evidence of complete
discovery workflow coverage. Both `cold_process` and
`repeat_fresh_process` are fresh subprocess runs; they do not demonstrate
runtime context reuse. These are observed command measurements, not token or
cost estimates. The supplied Muse trace separately reports input `9,046,450`,
cached input `8,933,292`, and output `24,389`; it has no per-stage blame.
