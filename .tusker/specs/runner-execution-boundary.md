---
title: "Use the operator's installed coding agents"
subject: runner-execution-boundary
keywords: [runner, ACP, CLI, Codex, Muse, Claude]
status: canonical
part_of: spec-to-proof
sources: [docs/research/runner-harness-patterns.md, .tusker/specs/decisions/2026-09-07-runner-boundary.md]
updates: [docs/system/runners-and-acp.md]
decisions_locked: true
summary: "Tusker orchestrates installed agent tools; it does not bundle or manage their runtimes."
capsule:
  what: "Reusable installed-harness execution, policy admission, recovery, and onboarding conformance."
  use_when: "Implementing or onboarding a runner, or reviewing its release evidence."
  skip_when: "Authoring task content or changing unrelated UI."
read_when: "Changing runner discovery, dispatch, retries, prompts, or completion handling."
skip_when: "Working on task authoring or the visual design of the work area."
---

# Use the operator's installed coding agents

This is the implementation contract for the installed-harness boundary. FLW-T-0030
owns its delivery and conformance evidence.

## Product outcome

An operator registers an installed harness, runs one conformance command, and sees
which task permission presets it can safely execute. Compatible ACP endpoints and
CLIs using an already supported dialect require configuration only. A novel CLI
dialect requires a small provider adapter and fixtures, never changes to task
orchestration. CLI is a process interface, not a standard event or permission protocol;
arbitrary CLI compatibility cannot honestly be promised without that adapter.

The same admitted runner serves Execute, retry, and enabled daemon dispatch. A run
has a visible outcome even when the agent crashes or Tusker restarts. An execution
result and a verified task result remain separate facts.

Tusker is an orchestrator, not an agent distribution. It must not bundle, install,
upgrade, or select a private copy of Codex, Claude Code, Muse, or another coding
agent. Installation and authentication remain owned by the operator and the agent's
native tooling.

## Proven patterns to reuse

Research against Block Buzz and Zed narrows the design instead of expanding it.

| Decision | Pattern | Tusker ruling |
| --- | --- | --- |
| Copy | Buzz/Zed describe an external harness as a command plus structured arguments and environment, then resolve it from the host. | Keep one small installed-harness descriptor and pin its resolved executable for the attempt. |
| Copy | ACP agents are child processes speaking JSON-RPC over stdio and must complete an initialize/capability handshake before use. | Treat ACP as one transport, admitted only by a live handshake. |
| Copy | Child processes have explicit liveness, cancellation, stderr capture, and whole-process-tree termination. | Tusker owns supervision; a disappeared child becomes an honest terminal failure. |
| Adapt | Buzz PATH-probes preset/custom harnesses; Zed negotiates capabilities and enforces permissions at the host. | CLI routes get provider-specific version/flag probes. ACP routes get handshake plus host-side permission checks. |
| Adapt | Providers emit different native events. | Normalize only lifecycle facts Tusker needs: `started`, `progress`, `permission_request`, `completed`, `failed`, and `cancelled`; retain raw events for diagnosis. |
| Reject | Buzz can install selected first-party runtimes. | Tusker never installs an agent or adapter. Missing tools produce install guidance only. |
| Reject | Zed carries a full macOS/Linux/Windows sandbox subsystem. | Use the installed provider's verified sandbox for CLI V1. A separate Tusker OS sandbox is future work only if measured provider gaps require it. |
| Reject | Presence on PATH as proof of readiness. | Require a bounded version/health probe, and an ACP handshake when applicable. |

The reusable idea is the boundary, not either project's product machinery.

## Harness descriptor

Each admitted route resolves to one immutable attempt descriptor:

```text
provider + transport + executable + argv + model + effort + permission preset
```

The executable and argv are structured fields, never a shell command string. The
environment starts from a minimal inherited allowlist plus explicit provider values;
Tusker-owned variables and secrets cannot be overridden by project configuration.
Discovery may report unavailable candidates, but dispatch receives exactly one healthy
descriptor. Nothing in the descriptor authorizes installation or fallback.

## Lifecycle

```text
discover -> probe -> select/pin -> claim -> spawn -> stream -> verify -> close
```

- Failure through `select/pin` leaves the task unclaimed.
- Failure after claim closes the attempt with its exact transport error.
- Cancellation targets the child process tree, not only the immediate PID.
- Resume is exposed only when the selected transport proves it; otherwise retry starts a
  new attempt from the task contract and existing workspace.
- Provider exit zero is not task success. Declared verification decides success.

## Execution contract

1. Select the provider, transport, model, and permission preset as separate fields.
   A model name must never imply a transport or safety policy.
2. Use ACP only when the operator-installed provider exposes a compatible ACP endpoint
   and a live handshake proves the capabilities needed by the task. Tusker must not
   download an ACP adapter or a bundled provider runtime.
3. Select the provider's installed CLI explicitly when using its native unattended
   command, such as `codex exec` or `claude -p`. A failed ACP admission does not
   automatically select CLI; a changed selection requires a new preflight.
4. Resolve executable candidates from the process environment. A broken or
   non-executable candidate does not hide a later healthy candidate.
5. Pin the healthy executable selected during preflight for that attempt. Do not
   silently change tools after dispatch begins.
6. When no healthy candidate exists, stop before claim and report every candidate
   checked plus the first actionable installation or authentication error.
7. A human-triggered run and its retry remain authorized even when background project
   automation is disabled.
8. The worker changes only its declared material paths and returns its native result.
   It does not mutate the canonical Tusker vault or learn tracker lifecycle commands.
9. Tusker owns attempt submission, task-declared verification, review transition, and
   terminal status after the agent process exits.
10. Give the worker only its task contract, owned paths, relevant knowledge routes, and
   exact checks. Do not inject unrelated repository-wide validation or generic reading.

## Transport decision

Transport is selected once before claim. Tusker must not start with one transport and
silently fall back to another after an attempt exists.

| Provider route | Transport now | Reason |
| --- | --- | --- |
| Codex native models | `codex exec --json` | The installed CLI exposes a supported unattended interface, structured events, model/effort selection, sandbox selection, and native resume. |
| Muse Spark 1.3 | `codex --profile muse exec --json` | Muse is reached through the operator's Codex profile and command-backed provider authentication. It is not a separate Tusker runtime or ACP endpoint. |
| Claude Code | `claude -p --output-format stream-json` | The installed CLI exposes unattended streaming and resume. Its permission mapping must pass provider-specific conformance before use. |
| Any future ACP provider | installed ACP endpoint | Admit only after handshake proves start, event stream, cancellation, resume behavior, tool authorization, final status, and usage reporting required by the selected task. |

The existing Tusker `codex_acp` bundle path is outside this contract because it pins
and installs an adapter runtime containing Codex. Keep it unavailable and remove it
after callers and migrations are inventoried. Do not make it a fallback.

Codex `app-server` is a native structured Codex transport, not ACP. It may be retained
as an explicitly named advanced transport if its installed binary and live capability
handshake pass. It must not be mislabeled as generic ACP.

## Permission presets

Unattended execution cannot stop for routine approval prompts. The preset determines
the containment boundary; provider flags are merely its compiled representation.

| Preset | Filesystem | Network | Routine approvals | Use |
| --- | --- | --- | --- | --- |
| `read-only` | no writes | off | never prompt; deny writes | review, research, log analysis |
| `workspace-write-offline` | selected workspace only | off | never prompt; denied operations fail | default implementation |
| `workspace-write-network` | selected workspace only | on | never prompt; denied operations fail | implementation that must fetch dependencies or call allowed services |
| `danger-full-access` | unrestricted | unrestricted | bypassed | only inside a separately isolated disposable machine or equivalent containment explicitly selected for the task |

Rules:

- Sandbox and network are independent. `workspace-write` does not prove network is off.
- “YOLO” means `danger-full-access`; it is never inferred from task importance, model,
  or provider. A worktree alone is not external containment.
- A CLI profile compiles to exact argv/config supported by the detected CLI version.
  Environment variables that the provider does not consume are diagnostics, not policy.
- Conflicting flags in a custom command fail preflight. Tusker does not guess which flag
  wins.
- If a provider cannot enforce the selected preset, that provider/profile combination is
  unavailable. Do not weaken the preset or silently switch to YOLO.
- `--approve-for-me` or equivalent automatic approval review is not the unattended
  default. It adds another model decision and can still pause or spend tokens. Use the
  bounded preset directly.

### Current provider compilation

| Provider | Read-only / workspace write | Full access |
| --- | --- | --- |
| Codex CLI | Compile the preset to `--sandbox`, explicit no-prompt approval policy, and an explicit workspace-write network setting supported by the detected version. | `--dangerously-bypass-approvals-and-sandbox`, only after external-containment admission. |
| Muse via Codex | Same Codex flags, plus `--profile muse`; keep `muse-spark-1.3`. | Same containment rule as Codex. |
| Claude Code | Admit only a verified permission mode and sandbox/tool policy that enforce the exact preset; a closest approximation is unavailable. Never use `bypassPermissions` for bounded presets. | `bypassPermissions`, only after external-containment admission. |
| ACP | Host enforces each tool callback against the preset; unsupported permission or network semantics fail handshake. | Not admitted until full-access parity is explicitly tested. |

## Non-goals

- Shipping an agent binary, Node runtime, provider SDK, or credentials inside Tusker.
- Repairing or upgrading the operator's agent installation.
- Inventing a second provider configuration system.
- Treating PATH order alone as proof that an executable is usable.

## Reusable module contract

Implement `internal/runner` within the existing Go module. Reuse compatible process
and protocol helpers; keep `cmd/tusker` as the adapter to canonical task state.
Do not publish a separate library, plugin engine, or service for this delivery.

| Owner | Responsibilities |
| --- | --- |
| `internal/runner` | Installed executable discovery, bounded probes, policy compilation, immutable preparation, transport events, process supervision, execution receipt. |
| Tusker orchestration | Task authorization, workspace and lean prompt, claims/leases, durable receipt storage, recovery decisions, verification/review, canonical task closure. |
| Provider adapter | CLI dialect: probe parsing, exact policy projection, prompt/resume encoding, native event/result decoding. No claims, database, UI, retry scheduler, or tracker access. |
| ACP transport | One protocol implementation, live negotiation, permission callbacks, session lifecycle. Provider identity alone never grants capabilities. |

Dependencies flow from `cmd/tusker` to `internal/runner`, never back. The module
accepts opaque attempt correlation IDs but no task schemas, vault paths, wave IDs,
lease state, or task outcome types. Muse reuses the Codex adapter with a named profile.
Model and effort are explicit inputs, not a transport selector. Reuse `internal/acp`
where its contracts fit; do not duplicate protocol machinery.

The public surface has two stages (Go names may follow existing conventions):

```text
Prepare(context, HarnessDefinition, RunInput) -> PreparedLaunch | AdmissionError
Execute(context, PreparedLaunch, EventSink)  -> ExecutionReceipt
```

Cancellation is through context. Resume, when supported, is an explicit RunInput
with the original session identity and matching workspace/launch provenance; it
passes preparation again. Execute cannot rediscover, change policy, or pick a runner.
PreparedLaunch is opaque to callers and can only be constructed by Prepare.

| Record | Required fields |
| --- | --- |
| HarnessDefinition | Stable ID, provider, transport (`cli` or `acp_stdio`), executable, args array, allowed environment overrides, CLI dialect when applicable, optional provider profile, configuration schema version. |
| RunInput | Prompt, workspace, permission preset, model/effort, opaque attempt ID, optional resume identity, total deadline and output limits. |
| PreparedLaunch | Resolved physical executable and identity, version, exact argv/cwd, private environment, requested/effective policy, capability and auth evidence, configuration fingerprint, preparation timestamp, limits. |
| RunnerEvent | Attempt ID, monotonic sequence, observation timestamp, one of six lifecycle types, reason/data, bounded native-event reference. |
| ExecutionReceipt | Schema version, attempt/launch identity, start/end timestamps, provider/session identity, terminal outcome/reason, exit code if observed, native final status, requested/effective policy, log references/truncation, optional usage with availability state. |
| AdmissionError | Stable reason code, every candidate checked, failed check, actionable remedy; no claim or execution receipt is fabricated. |

Use existing configuration storage and typed validation. Reject unknown dialects,
shell expressions, conflicting policy flags/config overrides, reserved Tusker env
overrides, and ambiguous legacy commands. A one-time migration may parse an unambiguous
legacy command into structured fields; parsing shell strings is not the runtime API.
Never put prompt or credential values in public argv/receipts. Keep resolved secrets
private in memory and sanitize logs and diagnostics; record credential-source identity,
not its value. Provider subprocess dependencies and config/auth resolution must use
the same explicit environment at probe and execution time.

## Lifecycle, containment, and recovery invariants

1. Prepare checks executable/version, auth state, protocol/dialect, policy support,
   and required capabilities before claim. A metadata catalog lookup never spawns
   a provider. Auth checks are bounded and noninteractive; unknown auth is reported
   as unknown and blocks admission, never converted to authenticated.
2. Immediately before spawn, recheck executable and relevant configuration identity.
   A change produces a terminal `launch_changed` failure after claim, with no provider
   spawn. A physical path alone does not prove the file at that path stayed unchanged.
3. Persist attempt identity and launch intent before spawning. Persist enough process
   identity (including start identity, not PID alone) and session metadata to reconcile
   after restart. Reuse existing durable attempt/fencing machinery.
4. `started` is emitted only after spawn; zero or more `progress` and
   `permission_request` events follow. Exactly one execution terminal event is
   accepted: `completed`, `failed`, or `cancelled`. Pre-spawn execution failure may
   emit `failed` without `started`. Late/duplicate terminal events are idempotent and
   stale attempts cannot close newer attempts.
5. `completed` requires the dialect's valid final result plus successful process exit
   where the protocol requires it. EOF, exit zero without a required final result,
   malformed required frames, or contradictory failure evidence produce `failed`.
   Optional unknown native events may be retained without inventing lifecycle facts.
   ACP session completion may precede process shutdown; cleanup must finish before
   the receipt is finalized. Usage/resume are required only when requested by the task.
6. Unattended permission requests are denied through the native protocol. The runner
   never waits indefinitely or invokes an approval classifier. A denied optional tool
   is observable but need not itself fail the task; verification decides sufficiency.
   A transport unable to deny safely fails with `policy_unenforceable`.
7. Wall-clock deadline, output bounds, and cancellation apply to probes and attempts.
   Drain stdout/stderr concurrently. Limit a protocol frame to 1 MiB and retained
   diagnostic output to 10 MiB per attempt by default; oversized frames fail explicitly,
   retained-log truncation is labeled. Do not silently truncate protocol input into a
   valid-looking event. Default probe deadline is 15 seconds; attempts default to
   30 minutes unless task limits are tighter. Cancellation gets at most 2 seconds of
   graceful shutdown before forced cleanup; all limits are validated positive values.
8. Own and terminate descendants on cancel/timeout, then reap. Process-group signaling
   alone must not be advertised as an absolute guarantee for detached descendants.
   The supported-host test includes a grandchild and a detaching child. If cleanup
   cannot be proved, report `cleanup_failed`, block further use of that route until
   reconciled, and retain process identity for diagnosis. Never kill by stale PID alone.
   Timeout is `failed` with reason `timeout`; requested cancellation is `cancelled`
   only after cleanup succeeds. A pre-cancelled context starts no process.
9. On Tusker restart, reconcile the persisted intent and observed process/result.
   Collect a finished receipt without rerunning work; terminate or reconnect a known
   active process only through supported semantics. Ambiguous execution is interrupted
   with an explicit diagnostic and no automatic replay. Receipt-storage failure never
   authorizes repeating mutations. Verification may be resumed independently.
10. Bounded filesystem policy must protect the canonical tracker and pre-existing user
    work. Run in a disposable workspace without a writable canonical-vault path;
    withhold tracker write credentials and writable tools. A prompt prohibition is not
    enforcement. Inventory pre-existing changes, and compare resulting changed paths
    against ownership: out-of-scope changes prevent success and remain inspectable.
    Workspace containment does not imply a per-file sandbox. Permissions include
    symlink escape, subprocess, network, tool/MCP, and effective provider-config paths.
11. Network-off describes task tools and subprocesses; the harness's authenticated
    inference/control connection is an explicit exception. Document and test that
    distinction for every admitted route. A provider flag which blocks only one tool
    is insufficient. Handshake tool callbacks alone do not contain an ACP process's
    own filesystem/network access. Require an independently verified native boundary
    or report the bounded combination unsupported. No new OS sandbox subsystem here.
12. The UI reports execution state, verification state, requested/effective preset,
    harness/version, first actionable error, and conformance status separately.
    Missing or failed checks never turn green because an agent says it finished.

## Harness onboarding and conformance

The existing runner catalog/profile configuration exposes this surface:

```text
tusker runner conformance --harness <id> --preset <preset> --json
tusker runner conformance --harness <id> --preset <preset> --live --json
```

Without `--live`, validate configuration and recorded fixture conformance only; do
not launch a real agent or claim a task. Report readiness as unverified. `--live`
explicitly authorizes a bounded disposable canary using the installed authenticated
harness; print the policy, workspace and limits before launch. It does not enable
automation or claim a production task. Full-access canaries require explicit external
containment admission; otherwise refuse. The app's Test harness action uses the same
service and displays the same report, including the authorization to run a canary.

Version report schema as `tusker.runner-conformance/v1`. Include harness/dialect,
transport, host OS/architecture, executable/config/policy fingerprints, suite version,
timestamps, per-case result and bounded evidence, and overall readiness. Case results
are `pass`, `fail`, `unsupported`, `blocked`, or `not_run`. Exit 0 means every required
case in the requested scope passed; 1 means failure/unsupported; 2 means blocked,
incomplete or invalid input. Offline success explicitly says `ready: false`.

Readiness is per harness + preset + host, never a provider-wide badge. An explicitly
unsupported capability can be excluded only when the requested task/preset does not
require it. Live passes expire after 24 hours by default and invalidate immediately
on executable, relevant configuration, policy compiler, or suite changes. Normal
Prepare still performs cheap fresh checks; a stale report blocks admission and gives
the explicit retest command. UI reads cached reports without repeating live probes.

Required onboarding workflow:

1. Register structured installed-command configuration and choose an existing CLI
   dialect or ACP transport. Show missing runtime/auth/policy support clearly.
2. Run fixture conformance; inspect exact compiled policy and rejected combinations.
3. Run live conformance for each preset to advertise. Check denial behavior using
   harmless temporary fixtures: requested outside write, symlink escape, canonical
   tracker sentinel write, and task-tool network access. A model declining to try
   is not sandbox proof: require a boundary rejection, or report the case blocked.
4. Run a disposable task through installed Mac Execute and verify the actual artifact,
   task result, logs, and cleanup. Only then label that route ready.
5. Add a second compatible harness using configuration only and rerun the same suite.
   No provider-specific conditional may be added to claims, verification, UI, or daemon.

Novel CLI adapters implement the same small probe/compile/encode/decode contract and
run the common suite plus native protocol fixtures. Compatible ACP uses the common
handshake suite. No expectation of zero code for unknown CLI semantics or ACP extensions.
Ship a worked configuration example for both routes and one minimal new-dialect guide.

## Required test matrix and delivery proof

Build one reusable suite with fake child executables and captured/synthetic native
frames. Unit/fixture tests require no provider installation, network, credentials, or
model spend. Live tests are separate and explicit. Test selectors below are deliverables,
not evidence that the tests exist today. Every named test must exist and execute;
`no tests to run`, skipped live tests, and fixtures are never live acceptance evidence.

| ID | Required cases | Named check to implement |
| --- | --- | --- |
| A10 | Package dependency boundary; configuration-only second harness; unknown dialect rejection; same service used by CLI/app/daemon | `TestRunnerModuleBoundary`, `TestHarnessRegistrationConformance` |
| A11 | Missing/broken first executable, later healthy executable, auth unknown/expired, timeout, changed executable/config, shell/flag/env conflicts; all before claim where possible | `TestHarnessAdmissionConformance` |
| A12 | Exact effective policy, override precedence, bounded Claude never bypasses on start or resume; required ACP capability missing; subprocess/network/symlink/tracker denial | `TestHarnessPolicyConformance` plus live conformance policy cases |
| A13 | Chunked frames, stderr flood, oversize/malformed frames, optional unknown events, missing/duplicate/conflicting final, nonzero exit, prompt redaction, unavailable usage | `TestHarnessEventConformance` |
| A14 | Cancel before spawn/during stream, deadlines, child/grandchild/detached child, pipe cleanup, late events, cleanup failure quarantine | `TestHarnessSupervisionConformance` |
| A15 | Crash after claim/before spawn, after spawn/before process record, after native final/before receipt, after receipt/before verification; stale attempt closure and no replay | `TestRunnerRecoveryConformance` |
| A16 | Report schema, non-live never spawns, missing live credentials blocked, stale report invalidation, per-preset readiness, CLI/app parity | `TestHarnessConformanceReport` |
| A17 | Installed-agent artifact verified, failed verification stays failed, ownership violation retained, automation-off direct retry, Test harness report visible in installed Mac app | `TestInstalledAgentCanaryLifecycle` plus installed-app observation |
| A18 | No bundled installer/runtime route or silent fallback; old configured routes migrate explicitly or fail actionably; operator onboarding and current-system docs match shipped behavior | `TestRunnerMigrationConformance` plus documentation review |

Run fixture suites with `go test ./internal/runner/... -count=1` and integration
selectors with `go test ./cmd/tusker -run '^Test(RunnerModuleBoundary|RunnerRecoveryConformance|HarnessConformanceReport|RunnerMigrationConformance)$' -count=1`.
Run the installed lifecycle check explicitly with
`TUSKER_RUN_LIVE_TESTS=1 go test ./cmd/tusker -run '^TestInstalledAgentCanaryLifecycle$' -count=1`.
That opt-in test must fail actionably rather than skip when its required installation
or credentials are missing. A Go test alone cannot prove installed-app interaction:
record app build identity, host, selected harness/version/preset, action, terminal UI
result, and bounded screenshot/observation references in the delivery report.
Implement `sh scripts/test-installed-runner-conformance.sh` as the repeatable installed
app acceptance entry point. It must exercise the actual installed app through available
UI automation, collect those observations, run the live per-preset conformance and
configuration-only second-harness cases, and check the shipped onboarding documentation.
It fails actionably when the installed app, UI automation access, harness or credentials
are missing. A localhost API-only test or a report containing expected text cannot
substitute for observing the installed UI. This script is a required deliverable,
not an existing command or an instruction to start live work during specification.

`docs/reports/system-installed-runner.md` must map A1–A18 to executed checks, current
source/app identity, conformance report identifiers, pass/fail/blocked outcomes and
unsupported combinations. Include the exact onboarding example and command. Existing
raw_artifacts_allowed=false remains: use bounded inline summaries and private local
log references; no bulk raw provider transcripts in task records.

## Delivery order and handoff

FLW-T-0030 remains the single owner of the connected runner replacement, so the team
does not split competing edits across shared lifecycle code. Implement in order:

1. Extract the module and fixture suite; disable inadmissible bundled/policy routes.
2. Complete Codex CLI policy and lifecycle end to end; Muse shares that implementation.
3. Apply the same conformance contract to Claude and installed ACP. Report unsupported
   pairs honestly; an unavailable provider does not justify weakening containment.
4. Land durable recovery, onboarding command/app surface, installed Mac canary and docs.

These are sequential slices within one task, not independent parallel ownership.
Implementation notes/file map: `internal/runner/**` is new; reuse `internal/acp/**`;
`cmd/tusker/runner*.go`, runtime/daemon/wave call sites and provider catalog/config
schemas require caller inventory. Mac/app runner settings and API paths are best
guesses until inspected by the implementer. Confirm file ownership in the task before
editing outside its contract; preserve existing dirty work. Remove bundled adapter
packaging/setup references only after their callers and saved configurations are
inventoried. Update `docs/system/runners-and-acp.md` with shipped behavior and an
onboarding recipe in this same task; do not document planned commands as live today.

Deferred: public SDK/distribution, arbitrary shell harnesses, automatic installer or
updater, automatic CLI dialect inference, new OS sandbox, hosted/cloud execution
changes, worker pools/relays, and multi-host certification. Certify the installed Mac
route first; label every other host unverified until its live suite passes.

## Acceptance

- **A1 — External ownership:** Tusker's application bundle contains no coding-agent
  runtime, and dispatch uses an operator-installed ACP adapter or CLI.
- **A2 — Healthy selection:** with a broken first `codex` candidate and a healthy
  second candidate, preflight selects and pins the healthy candidate.
- **A3 — Honest failure:** with no healthy candidate, execution remains unclaimed and
  reports the checked candidates and actionable error.
- **A4 — Direct-run continuity:** Execute and retry work while project automation is
  disabled when a human explicitly initiated the run.
- **A5 — Correct closure:** a worker that produces the declared output and exits zero
  reaches a terminal successful state; missing or failed verification does not.
- **A6 — Lean handoff:** the generated prompt for the canary contains no unrelated Go
  gates, broad memory request, or documentation routes beyond its task contract.
- **A7 — End to end:** from the installed Mac app, one click runs the disposable
  canary through a system-installed agent and displays its terminal result.
- **A8 — Transport truth:** the catalog reports Codex, Muse, Claude, and ACP only when
  their installed transport and required live capabilities are actually available.
- **A9 — Permission truth:** every admitted provider/profile pair proves the exact
  sandbox, network, and approval behavior it advertises; unsupported combinations fail
  before claim without weakening the requested preset.

<!-- tusker:delivery-import:62c1c254e1a08397:begin -->

- `[[FLW-T-0030]]` implements delivery source `system-installed-runner-e2e`.

- `[[W-0012]]` is the imported delivery wave.

<!-- tusker:delivery-import:62c1c254e1a08397:end -->
