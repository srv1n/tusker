# W-0025 final handoff — direct task/wave authoring

Audience: the next agent picking this up. Read this first, then the detailed
reports listed at the bottom. Repo: `/Users/sarav/Downloads/side/tusker`.

## Bottom line

The code work for all eight tickets (FLW-T-0043 through FLW-T-0050) is
implemented and locally verified. The delivery-plan implementation is
physically deleted — commands, parser, Serve APIs, UI surfaces, fixtures —
with no compatibility layer. The real vault was migrated and all ten
`.tusker/specs/*.plan.*` artifacts were deleted only after conversion receipts
proved preservation.

**What is NOT done:**

1. **Independent review of the landed code.** Sol reviewed the first draft and
   found five real issues; those were corrected. The corrections plus
   FLW-T-0050/0048/0046/0049 have only had self-review. A fresh independent
   review of the final tree is the main open item.
2. **Tusker lifecycle.** All eight tickets remain `status: backlog`,
   `readiness: held`. No claim, proof, or closeout events were recorded — the
   current-workspace claim gate rejected this session
   (`INVALID_TRANSITION: --current-workspace requires a trusted interactive
   Codex or Claude session`), the user waived it for tracking purposes only,
   and no lifecycle events were fabricated. Closing the tickets requires
   recording genuine proof and advancing them through the normal lifecycle.
3. **Runtime proof classes** (explicitly NOT RUN):
   - Browser lane (`--mode browser`): no `TUSKER_REALWORK_BASE_URL`/project.
   - Installed-build trial.
   - Provider-live dispatch and external-architect send/reply: no authorized
     endpoint was supplied.
   - Resident `tusker daemon run` process: never started (per session rules).

## Per-ticket state

| Ticket | Scope | State |
|---|---|---|
| FLW-T-0043 | Direct `tusker new task` (no `--epic`), atomic `wave create --file` with `tusker.wave-authoring/v1`, immutable authoring receipts | Implemented, corrected after review |
| FLW-T-0044 | `migrate direct-waves inspect/apply`, idempotent receipts, pinned unresolved deps, `--map-plan` explicit mapping | Implemented; real vault migrated |
| FLW-T-0045 | Wave records sole review/execution authority; one-action Start; CAS task update with rework transition | Implemented, corrected after review |
| FLW-T-0046 | UI on shared `tusker.wave-review/v1` projection; Start/Pause/Resume controls; plan picker removed | Implemented |
| FLW-T-0047 | External architect registration + truthful capability routing | Implemented; live routing genuinely unsupported for pre-existing external conversations — documented, not faked |
| FLW-T-0048 | Agent-facing guidance + task packet projection rewritten for direct authoring | Implemented; shipped YAML example validated against the real decoder by test |
| FLW-T-0049 | Physical deletion of delivery-plan subsystem + migration artifacts | Implemented; verified below |
| FLW-T-0050 | Autonomous frontier progression via existing daemon poll; pause/resume semantics | Implemented; `PollOnce` covered by fixture test, no resident daemon run |

## Verification evidence (all local/fixture class)

- Focused direct-authoring gate: **67/67 PASS**
  (`TestDirectWaveRemoval|TestDirectWaveAuthoring|TestDirectWaveAuthority|
  TestDirectStartAuthority|TestDirectWaveAutonomous|TestDirectWavePacket|
  TestDirectWaveServe|TestExternalArchitectRouting`).
- WorkSession/V7Proof/V7Dependencies/TaskAuthoring batch: PASS (145.8s).
- Demo/real-work fixture lanes: PASS (85.4s).
- Q1–Q14 offline harness `scripts/test-task-authoring-journey.py --mode
  offline`: **59 matched cases, all PASS**. Browser mode NOT RUN.
- `go build ./cmd/tusker`: ok (re-verified after handoff).
- UI: `bun run typecheck` clean; 31 focused tests pass; `bun run build` ok.
- `tusker docs check`: 45 documents pass. `git diff --check`: clean.
- Removal boundary enforced by `TestDirectWaveRemovalScanFindsNoLegacySurfaces`
  — the only files still containing `tusker.delivery-plan`/`deliveryRequest`/
  `handleDeliveryStart` strings are that test itself and allowlisted
  historical evidence.

## Migration record (do not re-derive)

`real-vault-migration.json` is the frozen evidence. Apply fingerprint was
`sha256:754463ef…a6cd`; post-apply inspect fingerprint
`sha256:7e9523f2…b4911` (only ten `delete_after_conversion` units).

Preserved unresolved dependencies (targets absent by design, tasks still
blocked, nothing created or authorized):

- `ORC-T-0055 → ORC-T-0041` hard sha256:21eb60a4…
- `ORC-T-0066 → ORC-T-0041` hard sha256:21eb60a4…
- `ORC-T-0079 → ORC-T-0041` hard sha256:21eb60a4…
- `ORC-T-0081 → ORC-T-0048` hard sha256:47352771…

Explicit one-time mapping used: `planning-handoff-completion.plan.json` →
`W-0023` (targets FLW-T-0037–0041), validated positionally against the
immutable receipt.

## Known unrelated failures (reproduce at HEAD)

`go test ./cmd/tusker -count=1` exceeds the 10m package timeout under load.
The named failures were verified to reproduce at HEAD in a clean-tree check
(`/private/tmp/tusker-head-verify`): user-global `~/.config/tusker/config.yaml`
pins a `devin`-harness profile and a light-only `model_levels` map;
`TestSharedProjectLoaderAllEntryPoints` fails on pre-existing
`agent_coordination.go`. Not caused by this cutover. Do not "fix" them by
weakening tests or silently rewriting real profile mappings.

## What the next agent should do, in order

1. Run an independent review of the final tree (the corrections + 0046/0048/
   0049/0050 have never had one). Suggested focus: locking order in
   `direct_wave_authority.go`/`runtime_store.go`, receipt drift paths, the
   rework transition, and the migration deletion boundary.
2. Advance the Tusker lifecycle honestly: record the real proof that exists
   (fixture/local classes), label NOT-RUN classes, and move the eight tickets
   through review/close only where the evidence supports it. Do not arm
   W-0025, start a daemon, or dispatch automation from the session.
3. If runtime proof is wanted, supply a real browser endpoint
   (`TUSKER_REALWORK_*`) and/or an authorized provider target, then run the
   browser/live lanes and record receipts.
4. Commit: nothing has been committed. The tree is heavily dirty with
   unrelated concurrent work (W-0017, W-0023, project icons, ACP/runner) —
   do not reset/stash/clean; stage only cutover files if asked to commit.

## Report index

- `handoff.md` — original architect→implementation handoff
- `sol-review.md` — independent review of first draft (5 findings, all fixed)
- `direct-authoring.md`, `migration.md`, `authority.md`, `contacts.md`,
  `guidance.md`, `experience.md`, `autonomous.md` — per-ticket evidence
- `real-vault-migration.json` — frozen migration evidence
- `completion.md` — full deletion inventory + Q1–Q14 matrix + allowlist
