---
subject: real-work-test-repository
title: "Build a resettable test repository for actual coding-agent work"
keywords: [repeatable testing, CLI parity, real execution, handoff]
part_of: real-work-test-packets
status: canonical
created: 2026-09-07
read_when: "Implementing or assigning build a resettable test repository for actual coding-agent work."
skip_when: "Looking up already verified installed behavior; this is an implementation contract."
sources: [repeatable-work-testing.md, work-knowledge-and-retention.md]
decisions_locked: false
capsule:
  what: "Self-contained work packet with scope, ownership, acceptance and verification."
  use_when: "Assigning this bounded work to an implementation agent."
  skip_when: "Orchestrating unrelated tasks or redesigning the product."
---

# Build a resettable test repository for actual coding-agent work

## Assignment

You own the reusable test repository, its reset/seed commands and the CLI scenario driver. Extend the existing demo work; do not replace it. Suggested work level: Standard, with routing/authorization changes handed to the lifecycle owner.

## User problem and target

The operator needs to test new Tusker changes without manually creating tasks, documentation and execution plans each time. They want to tell an agent to reset one designated test repository, seed it and run the CLI journey. After agent checks pass, the same repository is reset again so the human can press normal UI Run controls and watch work happen.

All content may be fictional. The tasks must execute through real installed coding agents, initially configured Codex Luna. A fake timer driver can remain a cheap regression tool but is not the final proof. The user specifically wants visible, roughly sixty-second work so task progress and DAG transitions can be observed.

## Current implementation to reuse

`cmd/tusker/demo_cmd.go`, `demo_fixture.go`, `demo_state.go`, `demo_run.go`, `demo_exec.go`, `demo_check.go` and `demo_cmd_test.go` already provide seed/status/run/wait/check/reset, three waves/twelve tasks, idempotence, deterministic delays, variants and safety checks. Existing `TestDemoRepeatableE2E` passed in a focused local run; that result does not establish real provider execution.

Known demo limitations from its handoff: demo-specific wave authorization, deterministic reviewer-close composition, demo-only profiles and no native evidence-expiry operation. Preserve cheap deterministic tests but make the real-harness lane use the ordinary lifecycle supplied by the sibling lifecycle owner. Do not hide unsupported production behavior behind another special-case success path.

## Seeded project contract

Use one explicitly named disposable repository supplied by the caller. Seed may initialize a missing empty dedicated repository; refuse an arbitrary populated real repository. Mark ownership/version in a manifest and record every created path and registration. No credentials in the manifest.

Create a minimal runnable project using a language/toolchain already installed on the host. Prefer tiny scripts and standard-library tests. Include a short README describing the test purpose and exact run/reset commands.

Create fictional knowledge:

- Root/system overview explaining the sample product and how its pieces fit.
- At least two nested folders with concise introductions/index notes.
- A product-intent note, a technical explanation and a decision record containing alternatives and rationale.
- Valid subject/title/parent/status/read_when/skip_when metadata and meaningful backlinks.
- At least one long file/title and one superseded note linked to the current answer.
- Invalid metadata/broken-link examples only in explicit negative-test variants; the default corpus should validate.

Create **one standalone task plus three four-task waves**: thirteen tasks total. The earlier twelve-task demo did not include this separate individual-task journey; add it explicitly.

| Work | Graph and owned output |
|---|---|
| Standalone smoke task | Create a tiny module and test in `sample/standalone/`, run the supplied progress helper, verify and submit |
| Alpha | A1 → A2 and A3 → A4; separate files under `sample/alpha/` |
| Beta | B1 → B2 and B3 → B4; separate files under `sample/beta/` |
| Follow-up | A4 and B4 → C1 → C2 and C3 → C4; own `sample/followup/` |

Root tasks generate data fixtures. Branches add a small transformation and a validator with tests in non-overlapping files. Joins combine both outputs and check them. Follow-up assembles a report. Branch ordering must emerge from native dependencies, not sleeps in the scenario driver.

Every task contains useful bounded context, files to inspect, owned paths, non-goals, acceptance, exact verification and review requirements. Link to the shared fictional spec rather than copying it everywhere. Assign the three supported work levels as appropriate; use explicit configured profile mappings and report actual resolution.

## Observable jobs

Ship a standard-library helper that emits bounded timestamped progress roughly every five seconds, waits approximately sixty seconds by default, handles interruption and exits deterministically. Jobs invoke it as a tool; do not ask a model to invent a waiting mechanism or burn model turns counting seconds.

Support an explicit short-duration mode for cheap tests. Normal human demos retain enough time to see running and review states. File generation and test execution remain real work performed by the configured coding agent. Preserve task stdout/progress through the ordinary run projection, with bounded output.

## Reset and seed behavior

Keep the existing documented `demo seed/status/run/wait/check/reset` verbs where possible. Return stable JSON identities and actionable nonzero errors. Reset preview is non-mutating; explicit apply removes only manifest-owned Tusker state/generated sample artifacts and reseeds an equivalent scenario with fresh attempt identities. Preserve caller credentials, unrelated files/projects/global settings, and any explicitly exported results outside the reset-owned area.

Reject wrong roots, symlink escapes, active attempts and ambiguous ownership. Do not silently cancel active work to reset it. Explain the supported cancellation/wait command. Interrupted seed/reset should be safely retryable and report partial state. Reseed without reset is idempotent and creates no duplicate tasks or waves.

The project must be discoverable by the normal UI/runtime instance, not registered only in an invisible alternative state root. Return the correct project ID, repo path, runtime scope and view URL when available. Do not start another server on an already occupied port or redirect the user's existing project registration. If runtime registration is unavailable, seed may still prepare data but must report the missing step.

## CLI-driven acceptance journey

Deliver one script at `scripts/test-real-work-project.sh` (or a clearly documented equivalent following existing repo convention) with explicit repo/candidate/profile options. It must be runnable without browser access. It should:

1. Preview/apply reset and seed; assert exactly thirteen tasks and the expected graphs/documents.
2. Reseed and verify identities/counts are unchanged.
3. Inspect the standalone task's effective Codex Luna implementation/review profiles; refuse unavailable prerequisites without substitution.
4. Start the standalone task through the ordinary CLI and observe progress, review, accepted result and closed task.
5. Start Alpha and Beta through the ordinary CLI; assert actual overlap from recorded attempt intervals, both joins wait and artifacts are bound to real attempts.
6. Confirm Follow-up becomes ready but does not auto-start. Start it explicitly and verify its result.
7. Confirm no active run/lease remains and CLI task states agree with accepted results.
8. Reset and repeat the essential journey with fresh attempt identities.

Cancellation, fail-once and review-rejection variants can be separate explicit modes so the normal journey stays short. Each must use supported failure/retry semantics and avoid mutating task status files.

Keep an offline deterministic mode clearly labeled. Add an explicit real-harness mode whose output includes actual harness/profile/model/transport/attempt IDs; it must never silently fall back to the timer driver. A mixed Codex/Muse mode follows the successful Codex-only run and identifies which tasks used each. Use operator-provided configured profiles; no secret discovery, new account setup or hard-coded claims of model availability.

## Non-goals

No independent scheduler, task status writer, authorization layer, review implementation or new coding-agent harness. No UI automation in this packet. No retention-engine implementation; expiry coverage remains explicitly unavailable until supported. No broad deletion of the checkout, new project framework or general-purpose scenario DSL.

## Ownership and sibling boundary

Own the existing `cmd/tusker/demo_*.go` implementation/tests, new sample fixture files, scenario script and `docs/reports/real-work/fixture/`. Preserve current demo capabilities and public exit behavior unless deliberately documented. The lifecycle owner owns ordinary task/wave/review services. Request missing operations there; do not patch runtime internals to make your fixture pass.

Shared `cmd/tusker/cli.go`, `capabilities_cmd.go` and `docs/system/cli.md`: supply only narrow demo registration/help/documentation changes, agreed with lifecycle owner. Never rewrite whole shared files. UI owner consumes your real project ID and supported reset/run commands; no UI-only fake dataset for final acceptance.

## Acceptance criteria

| ID | Required result |
|---|---|
| A1 | One CLI reset/seed journey creates the designated project's runnable sample, documents, standalone task and three waves without manual authoring. |
| A2 | Reset/reseed safety, manifest ownership, no duplicates, active-run refusal and wrong-root/symlink guards pass. |
| A3 | Normal Codex Luna run visibly executes real file/test work and approximately sixty-second progress without a timer-driver substitute. |
| A4 | Standalone task completes through native review/closeout; Alpha/Beta overlap and follow native dependencies; Follow-up needs explicit start. |
| A5 | The script is machine-only, bounded, nonzero on failure, and retains useful IDs/proof without dumping raw logs by default. |
| A6 | Reset and second run pass with fresh attempt identities and no old active processes. |
| A7 | Mixed Codex/Muse results are separately identified; unavailable Muse is a reported prerequisite, not a claimed pass. |
| A8 | The normal UI can open the same project and the human receives exact reset/seed/start instructions. |

## Verification and deliverables

Extend existing tests rather than creating a new test framework:

```sh
go test ./cmd/tusker -run '^(TestDemoFixtureShape|TestDemoOwnedPath|TestDemoExitMapping|TestDemoRepeatableE2E|TestDemoFailureRetryE2E|TestDemoGuardsE2E)$' -count=1 -v
```

Add named `TestRealWorkFixture...` tests for the standalone-task addition, real-mode refusal/no-substitution and manifest/profile behavior; run with `go test ./cmd/tusker -run '^TestRealWorkFixture' -count=1 -v`. Count executed tests; zero matches and skipped builds are not passes.

Then run the documented real-harness script through the authorized runtime. If installed runtime/credentials/authority is missing, deliver the working script and exact prerequisite; do not label real execution passed. No daemon or nested agent launch contrary to repository instructions.

Deliver `docs/reports/real-work/fixture/report.md` with fixture version, candidate identity, exact commands, profiles, work IDs, observed durations/overlap, outputs, reset results and failed/unavailable cases. Give the human a short runbook to reset, open the same project, start the standalone task and then waves. The deliverable is a reusable test mechanism, not just a one-time successful log.

## Dependencies

Seed/job/script development starts independently. Publish manifest/identity/reset contract early. Final real execution relies on the lifecycle owner's command/authorization/closeout fixes. Do not call the whole packet complete based solely on the existing offline demo tests.
