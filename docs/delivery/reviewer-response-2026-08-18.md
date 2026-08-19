# Reviewer response — 2026-08-18

## Verdict

The reviewer was right at the review snapshot: the wave-review predicate and
the default docs-adoption path were ship blockers. Both are repaired in the
current source. The post-fix candidate is test-green, but it is not a release
declaration: provider authentication, ACP cutover, historical actor authority,
and installation remain human-owned.

The authoritative serial package run is green: **2,501 tests run, 2,498
passed, 3 skipped, 0 failed; 733.069 seconds**. Retained evidence:
`/private/tmp/tusker-cmd-authoritative-fixed.42166.json`. The skips are the
bugfix-wave scope contract, live Codex ACP smoke, and live RZN offline
workflow. No provider was contacted during this work.

## Disposition of the review

| Reviewer finding | Disposition and evidence |
|---|---|
| Wave review marked terminal/dead waves ready and admitted landed waves. | Fixed the readiness predicate and snapshot filtering. Historical terminal waves, cancelled/superseded members, and landed waves are excluded; a review batch requires a durable review member. Covered by the final package and the wave/Serve focused cluster. |
| `docs adopt` could tombstone or collide with live files; `--yes` acted as apply. | Fixed. Default and dry-run are additive/read-only; application requires an explicit fingerprinted reviewed table and `--by human:<name>`. No deletion is performed; tombstones require an explicit reviewed successor. Rooted no-follow writes, source/target CAS, serialized apply, and rollback are covered by focused docs and docgraph tests. |
| Proposal/actor paths could mint `human:$USER` from an agent session. | Fixed actor resolution at the shared mutation seams and Serve operator boundary. Agent sessions cannot claim a human actor. Existing historical records are immutable and were not rewritten. |
| FAC-T-0007 cited a non-existent command test; `verify add` accepted typed command claims. | Corrected the fixture/task row. Public `verify add` accepts command rows only as `pending`; the authoritative review/accept/close executor runs pending commands from the canonical repository root and records bounded receipts. Manual proof remains human proof. |
| `SPEC_UPDATES` used an unsafe timestamp heuristic. | Git-backed decisions are now anchored to the authority commit and require the same commit to touch the authority and target; dirty or unrelated later edits do not satisfy it. Non-git fallback remains explicit and tested. |
| Spec-reference policy was stricter than the documented tier-1 contract. | Tier 1 emits `TASK_SPEC_REF_REQUIRED` as a warning; tiers 2–5 and the default require a resolvable reference. The docs, packaged skill, and focused tests were aligned. |
| Installed `dist/tusker` was stale. | Confirmed, not silently repaired. The candidate and installed artifacts are listed below; no install, promotion, tag, or release was performed. |
| `last_verified`/runtime observability were dormant. | The source paths are real, but the current store has no live provider observations. This is an evidence boundary, not a claim of live acceptance. |

## Current candidate evidence

| Check | Result | Retained evidence |
|---|---|---|
| Candidate build | PASS; `/private/tmp/tusker-final` built 20:54:37 from `a407d7768648+dirty`, SHA-256 `80d654d8dab7b0aeccc65f90a710de1c4564793ce14809f6ae4847b0d90bfd1a` | `/private/tmp/tusker-redteam-closure-build.35607.log` (empty PASS output) |
| Full `go test ./cmd/tusker -count=1 -timeout 40m -json` | 2,501 run / 2,498 pass / 3 skip / 0 fail; 733.069s | `/private/tmp/tusker-cmd-authoritative-fixed.42166.json` |
| `tusker validate --json` | `ok=true`, 0 errors, 136 warnings | `/private/tmp/tusker-final-validate-postfix.json` |
| strict skill doctor | `ok=true`, 0 errors, 98 warnings | `/private/tmp/tusker-final-doctor-postfix.json` |
| docs status | 28 documents, all 28 never verified; coverage gaps `cmd`, `e2e`, `internal`, `skills` | `/private/tmp/tusker-final-docs-status-postfix.json` |
| docs-adopt dry run | schema `tusker.docs-adopt/v1`; fingerprint `sha256:3855d6d91e1ecc1ec735e353cbdb910f9cbde84cd33efb55b174e4d0eb26209a`; 638 proposals: 565 leave, 72 promote, 1 merge; `approved=false`, `applied=false` | `/private/tmp/tusker-final-docs-adopt-postfix.json` |
| focused repair cluster | PASS, package 14.534s; actor/FAC closure PASS, package 9.297s; internal/docgraph PASS 1.229s; vet and build PASS | `/private/tmp/tusker-redteam-finalfocus.22797.log`, `/private/tmp/tusker-redteam-closure-focus.32250.log`, `/private/tmp/tusker-redteam-final-docgraph.23879.log`, `/private/tmp/tusker-redteam-closure-vet.35423.log`, `/private/tmp/tusker-redteam-closure-build.35607.log` |

These are source, focused, or synthetic proofs. They do not prove live
provider behavior, human acceptance, listening, installation, or public
release.

## Source/install parity

| Artifact | Version/revision | SHA-256 | Capability observation |
|---|---|---|---|
| Candidate `/private/tmp/tusker-final` | `v0.0.0-20260818043958-a407d7768648+dirty` (`a407d7768648+dirty`) | `80d654d8dab7b0aeccc65f90a710de1c4564793ce14809f6ae4847b0d90bfd1a` | 85 commands, including actor correction and current docs/wave surfaces |
| `dist/tusker` | `archive/pre-convergence-main-20260727-288-g04e7ed6d` (`04e7ed6d613c`) | `c33d4b7bb50ffba91d8be386b66b608f903dcf9068a0a9cda45fcdcbe82f58d4` | 84 commands; lacks the candidate actor-correction surface |
| `/Users/sarav/.local/bin/tusker` | symlink to `dist/tusker`; same as above | same as above | stale installed binary; unchanged |

The checkout has 216 dirty paths at report time. The candidate is therefore a
local dirty-tree artifact, not a clean release artifact.

## Open human gates and exact next actions

The authoritative gate snapshot contains three blocking gates, all owned by
`human:sarav`:

1. **ACP-G-0001 — Codex live authenticated smoke.** Select the exact sealed
   bundle, run the documented opt-in smoke, inspect its redacted receipt, and
   only then decide whether to satisfy the gate. The exact command and receipt
   schema are in
   [`docs/delivery/acp-gate-review-packet-2026-08-18.md`](acp-gate-review-packet-2026-08-18.md).
2. **ACP-G-0002 — Claude ACP live parity.** No maintained Claude ACP adapter,
   runner route, or live receipt producer exists in this source tree. Do not
   relabel `claude-code` or the generic Claude execution adapter as ACP proof;
   implement and pin the adapter first.
3. **ACP-G-0003 — default ACP cutover/direct-runner deletion.** Requires both
   provider receipts, soak and rollback evidence, separation of
   `codex_cloud`, deletion evidence, and an explicit human release decision.

Additional human-only boundaries:

- The runtime execution graph is currently empty of provider observations;
  no live session, credentials, or provider account was used here.
- Historical `AGX-P-0001` and `SRV-P-0002` actor records remain immutable. The
  new `actor correction apply` command fails closed with
  `HUMAN_CONTROL_RECEIPT_UNAVAILABLE` because this checkout has no
  non-forgeable typed human-control receipt producer/validator. Plan/list are
  read-only.
- The docs-adopt dry run must be reviewed by a human using its fingerprinted
  table before any explicit apply. Nothing was adopted or deleted here.
- Installation, dist regeneration, staging, commit, tag, and release are
  still separate human-authorized actions. None was performed.

This report records the evidence and its limits; it does not satisfy a gate or
authorize a release.
