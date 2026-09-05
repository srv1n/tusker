---
title: "Spec-to-proof workflow audit"
status: draft
read_when: "Assessing core workflow repairs and the remaining route to product completion."
skip_when: "Looking for current command syntax or a single task's live status."
---

# Spec-to-proof workflow audit

The core pieces exist. Their boundaries lose information and sometimes
overstate success. The right next move is to repair those boundaries and
exercise one complete workflow, not add another orchestration framework.

Audit date: 5 September 2026. Base revision: `03201019`. The checkout already
contained test, Makefile, and UI changes. Those changes were preserved.
This report separates source findings, local repairs, and unfinished product work.

The [product spec](../../.tusker/specs/spec-to-proof.md) records the requested direction.

## What exists and what is missing

| Product need | Existing machinery | Finding |
| --- | --- | --- |
| Product intent and decisions | Markdown specs, decision records, spec references | Competing spec conventions; constraint quality is largely an authoring responsibility. |
| Bounded tasks | Acceptance IDs, exact checks, artifacts, owned paths | Packets drop or truncate contract material. Repaired in this change. |
| Dependency execution | DAG frontiers, gates, readiness, wave authorization | Core scheduling exists; dry-run and import disagree on frozen-wave amendments. |
| Provider handoff | ACP and direct CLI adapters, detached wrappers | Shared contract loss repaired; live provider switching and recovery remain untested. |
| Efficient workspaces | Shared checkout, worktrees, clone, concurrency limits | Workspace preparation can swallow Git errors. Repair and verification tracked below. |
| Honest evidence | Typed evidence, criterion coverage, review bindings | Filename substitution and display relabeling can misrepresent evidence. Repaired in this change. |
| Human decisions | Owned gates and actor checks | Generic gates and auth/release gates have different authority checks; verbal approval lacks a trusted receipt path. |
| Document discovery | Document graph, search, read/skip metadata | Search does not rank reading guidance or return both routing hints. Repair tracked below. |
| Human diagrams | Generated Mermaid and graph JSON | Mermaid and API graphs represent different relationships; the UI treats Mermaid as code. |
| Fast human acceptance | Wave brief and artifact contracts | Complete category-specific evidence enforcement and visual acceptance remain unfinished. |

## Repairs in this change

### Complete worker and reviewer contracts

`v7Packet` previously limited intent to eight lines, acceptance to eighteen,
and verification to twelve. It omitted non-goals, implementation notes,
artifact requirements, and ownership metadata. The same builder feeds CLI
packets, work sessions, daemon prompts, and reviewer closeout packets.

Packets now preserve the authored task body and include owned paths,
generated outputs, migration keys, and shared-resource references. Delivery
import now copies plan non-goals into each task. No new packet format or
parallel source of truth was introduced.

Regression checks exercise 24 acceptance/check rows, preserved shell
indentation, scope exclusions, ownership fields, and an actual temporary-vault
delivery import through both worker and reviewer packets.

Sources: [packet builder](../../cmd/tusker/commands_v7.go),
[delivery import](../../cmd/tusker/delivery_cmd.go),
[regression checks](../../cmd/tusker/v7_packet_contract_test.go).

### Honest evidence types

A `.png`, `.mp4`, or trace-like filename on unrelated evidence could satisfy
a screenshot, video, or trace requirement. The wave brief could also relabel
test evidence as whichever artifact the task promised.

Requirements now use the actual evidence type. The brief displays the actual
type and skips unknown types. A promise is no longer used as evidence metadata.
This does not yet prove the image content or enforce before/after measurements.

Sources: [proof matching](../../cmd/tusker/v7_proof_cmd.go),
[brief normalization](../../cmd/tusker/v7_wave_brief.go),
[proof regression](../../cmd/tusker/v7_proof_evidence_kind_test.go),
[brief regression](../../cmd/tusker/v7_wave_artifact_kind_test.go).

### Workspace preparation and document discovery

Worktree and clone preparation now propagate Git errors and preserve unrelated
destination files. The regression uses real temporary Git repositories and
invalid preparation requests. Orphan cleanup now also protects paths owned by
claimed/running records, including interactive records with no process PID.
Missing or corrupt runtime state preserves the workspace and counts it toward
the cap. Confirmed unowned orphans still use the existing cleanup path.

Document search now matches positive `read_when` metadata and returns both
read/skip guidance in JSON and readable output. Skip guidance never acts as a
positive search term. Existing raw metadata is reused without a schema migration.

Their contracts live in the
[hardening plan](../delivery/spec-to-proof-hardening.yaml).

## Remaining work, in order

1. **One contract preflight everywhere.** `status ready`, `next`, import preview,
   and actual import should agree about validity while keeping task validity
   separate from dependency readiness. Live observation: an amended held plan
   passed dry-run and failed import due to its frozen integration base. The
   original plan was restored byte-for-byte; extra tasks use a separate scope.
   Sources: [status transition](../../cmd/tusker/v7_control_cmd.go),
   [import boundary](../../cmd/tusker/delivery_cmd.go).
2. **A trusted human-answer path.** Human-owned decision gates currently differ
   from auth/release gates in their actor checks. Conversely, an agent cannot
   record a human actor even after a verbal answer. Define a receipt from the
   human interaction surface, bound to the named gate and decision; then use it
   consistently. Do not solve this by trusting an arbitrary `human:` string.
   Sources: [gate transitions](../../cmd/tusker/v7_control_cmd.go),
   [actor checks](../../cmd/tusker/actor_authority.go),
   [unavailable receipt boundary](../../cmd/tusker/actor_correction_cmd.go).
3. **Enforce the promised artifact.** Criterion coverage, artifact existence,
   type, and measurement conditions should agree before completion. Current
   generic artifact proof can accept any artifact-bearing evidence; benchmark
   and migration results lack a complete structured before/after contract.
   Start with the existing artifact contract and evidence records rather than
   adding a second task hierarchy. Source:
   [proof modes](../../cmd/tusker/v7_proof_cmd.go).
4. **Unify spec conventions and document relationships.** The spec skill,
   `docs new`, and traceability scanning disagree about roots and required
   metadata. Choose a compatible convention, preserve existing documents, and
   share relationship extraction between Mermaid, graph JSON, and UI. Do not
   enforce a new schema against all existing documents without a migration.
   Sources: [spec skill](../../skills/spec/SKILL.md),
   [document commands](../../cmd/tusker/docs_cmd.go),
   [traceability](../../cmd/tusker/v7_traceability.go),
   [graph map](../../internal/docgraph/docmap.go).
5. **Prove one complete product journey.** Use a representative temporary
   project: constraint → spec → dependent tasks → provider handoff → failed
   verification → correction → evidence → independent review → dependency
   release → updated documentation. Then exercise supported live providers
   and the visible Mac/browser flow. Offline unit tests cannot replace that.

## Verification

Host: local macOS arm64. Go tests run through the repository validation lock
with `GOMAXPROCS=2`, package parallelism 1, and test parallelism 1.

The combined repair command is:

```sh
GOMAXPROCS=2 go test -timeout=5m -p=1 -parallel=1 ./cmd/tusker ./internal/docgraph \
  -run 'TestWorkspacePrepare|TestWorktreeCap|TestV7Packet|TestDeliveryImportPreservesNonGoals|TestV7ProofCategory|TestWaveArtifacts|TestDocsFind|TestFind' -count=1
```

The broad suite is `GOMAXPROCS=2 go test -timeout=20m -p=1 -parallel=1 ./...`.
Full local output is in `.tusker/scratch/FLW-T-0002/go-test.log` and
`final-check.log`; scratch is temporary.

| Check | Result |
| --- | --- |
| Focused packet/capsule/spec-reference checks | PASS |
| Packet + V2 import/non-goal/operational persistence checks | PASS |
| Proof + wave brief + packet + V2 regression group | PASS |
| Workspace manager + new failure cases + document search + typed evidence | PASS |
| Delivery doctor, original and hardening plans | PASS |
| Vault validation using installed CLI | PASS: zero errors, five warnings |
| Complete Go suite | FAIL: 13 main-package failures and the convergence wrapper; see below |
| Final focused repairs, including active workspace ownership | PASS |
| Four corrected daemon dispatch fixtures | PASS: 8.469 seconds |
| `go vet -p=1 ./cmd/tusker ./internal/docgraph` | PASS |
| Local CLI build to `/tmp/tusker-flw` | PASS |
| Built CLI: metadata-only `docs find handing --json` and `docs map` | PASS |
| Baseline comparison for nine non-workspace failures | All nine reproduced without these source repairs |
| Installed CLI/app refresh, live providers, browser/video acceptance | Not run |

Plans and tracked records are inert. No daemon or provider was started and
no release was installed or published. The overall product vision remains
incomplete until the remaining workflow and live acceptance work is finished.

Tracker proof remains pending. The installed guide's `verify add --result pass`
example is rejected for command rows: only the verification executor may record
those results. Local test results above are actual command results, not a claim
that the tasks have passed tracker closeout. This guide/CLI mismatch and the
import-preview mismatch were recorded with `tusker feedback add`.

Validation warnings include existing CFX/DOC task issues, the discarded initial
FLW scaffold, and the new draft spec's `read_when`/`skip_when` metadata not matching
the separate capsule convention. They are not reported as a clean-vault result.

### Broad-suite failures

The main package took 897.580 seconds. Four dispatch tests attempted Git
worktrees from non-Git fixture directories; the new error correctly exposed
that setup. Those four fixtures now initialize a real committed repository.
Their combined focused rerun passed in 8.469 seconds. The full suite was not
repeated after that fixture-only correction; nine baseline failures remain.

Nine other failures reproduced using a Go source overlay that restores only
this change's tracked source files to their original versions and masks its new
tests. The initial dirty checkout was preserved; no reset or worktree was used.

| Area | Reproduced baseline failures |
| --- | --- |
| Help wording | `TestFreshCloneBaselineCLIRunsHelpAndV7Init` |
| Snapshot invalidation | `TestServeSnapshotCacheReusesBuildAndInvalidatesPerProject`, `TestServeSnapshotIdenticalRefreshEmitsNoProjectionEvent` |
| Skill contract | `TestSkillReservesHumanApprovalForHumanOnlyBoundaries`, `TestTuskerSkillProgressiveDisclosure` |
| State revision handling | `TestV7TargetedReconcileRepairsOnlySelectedTask`, `TestV7ReconcileDryRunEnumeratesInvalidStateRevsWithoutWrites`, `TestV7ReconcileRepairsStaleObjectStateRevAndEmitsEvent`, `TestV7SaveCASRejectsOnDiskBodyEditWithStaleStateRev` |

The convergence wrapper also fails on the same progressive-disclosure word-count
assertion: recorded 667 words versus measured 753. Baseline comparison output is
in `baseline-failures.log` beside the other scratch logs. These failures remain
open; the broad suite is not green.

## Tracked work

- [[FLW-T-0002]]: complete handoff, [original plan](../delivery/spec-to-proof.yaml).
- [[FLW-T-0003]]: typed evidence.
- [[FLW-T-0004]]: workspace preparation.
- [[FLW-T-0005]]: document discovery.

The last three tasks use the [hardening plan](../delivery/spec-to-proof-hardening.yaml).
