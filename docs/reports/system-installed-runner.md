---
title: "System-installed runner delivery evidence"
task: FLW-T-0030
date: 2026-09-07
status: implemented
---

# System-installed runner delivery evidence

## Delivered boundary

`internal/runner` exposes the complete reusable API:

```go
Prepare(context.Context, HarnessDefinition, RunInput) (PreparedLaunch, error)
Execute(context.Context, PreparedLaunch, EventSink) (ExecutionReceipt, error)
Conformance(context.Context, HarnessDefinition, RunInput, bool) (ConformanceReport, error)
```

The short agent-facing command is:

```sh
tusker runner test <harness> [--preset read-only|workspace-write-offline|workspace-write-network] [--live] [--exercise print|timer] [--script <executable>] [--json|--quiet]
```

It defaults to local, read-only checks. `--quiet` uses only the exit status. `--json`
returns the versioned report. `runner conformance --harness ...` is the descriptive
alias used by the HTTP/UI service. Neither route claims task state. Normal Execute,
retry, daemon dispatch, verification, and closure use the same prepared launch.

## Acceptance evidence

| ID | Result | Evidence |
| --- | --- | --- |
| A1 | pass | `TestRunnerPreclaimHealth*` probes version/auth before claim and `TestCompletionCodexBindingUsesPhysicalExecutableWithoutLoginShell` checks the pinned physical executable. |
| A2 | pass | `TestHarnessAdmissionConformance` and preclaim-health fixtures put a broken executable before a healthy PATH candidate and select the latter. |
| A3 | pass | Admission returns a typed `runtime_missing`/`auth_missing` error with candidates and remediation; daemon persists an infrastructure block without claiming. |
| A4 | pass | `TestServeWaveExecuteQueuesOnlyExactArmedWaveWithAutomationOff` and redrive guards prove explicit execution remains available while background automation is off. |
| A5 | pass | Early-exit, completion, and runner lifecycle tests keep task verification and closure in Tusker; the runner package has no task mutation API. |
| A6 | pass | Render-attempt-prompt tests prove the task packet contains owned paths and declared checks without unrelated repository gates. |
| A7 | blocked | The signed installed macOS app bundle exists and embeds the built UI, and the same backend/UI contract passes. Interactive observation was not performed because the operator explicitly disallowed Computer Use access. |
| A8 | pass | Live catalog on this host: Codex `0.153.4` available, Muse through the installed Codex profile available, Claude `2.1.261` unavailable because native authentication reports no session. ACP is profile-specific and requires a live handshake. |
| A9 | pass | Exact policy/compiler tests cover Codex, Muse, Claude, and ACP. Unsupported Claude workspace-write and ACP capability combinations fail admission. |
| A10 | pass | Module-boundary test rejects task/vault/orchestration dependencies; compatible CLI dialects and ACP endpoints onboard through configuration. |
| A11 | pass | Admission tests reject missing auth/runtime, changed executable identity, reserved environment, shell expressions, unknown dialects, and policy conflicts before execution. |
| A12 | pass | Policy tests cover start/resume arguments. Live Codex read-only evidence: two native denials, no workspace write, no vault sentinel write, and no network marker. Workspace offline/online live reports also passed with the expected inside/network differences. |
| A13 | pass | Event tests normalize only six lifecycle types and reject malformed, oversized, partial, missing, and duplicate terminal results with bounded output. |
| A14 | pass | Supervision tests cancel/timeout the process group, wait for descendants, and surface cleanup failure rather than reporting success. |
| A15 | pass | Recovery conformance fences stale attempts and preserves terminal/ambiguous receipts without replay. |
| A16 | pass | CLI and Settings call `runRunnerConformance`; reports distinguish pass/fail/unsupported/blocked/not-run and expire after 24 hours or executable/config/policy identity changes. |
| A17 | blocked | CLI live functional execution is proven, including a one-second timer and policy enforcement. Completion, failed verification, scoped changes, and retry have focused integration coverage. Installed-app click-through remains unobserved under the operator's no-Computer-Use instruction. |
| A18 | pass | Public adapter install/setup routes are retired; migrations, help, system docs, and configuration-only onboarding are covered by focused tests. |

## Live host receipts

| Route / preset | Result | Native evidence |
| --- | --- | --- |
| Codex / read-only / timer | ready | `functional_exercise=timer completed`; `native_denials=2 inside_write=false outside_write=false network_write=false` |
| Codex / workspace-write-offline | ready | `native_denials=1 inside_write=true outside_write=false network_write=false` |
| Codex / workspace-write-network | ready | `native_denials=1 inside_write=true outside_write=false network_write=true` |
| Muse / read-only | ready | Same Codex policy compiler through the installed `muse` profile; live result passed. |
| Muse / workspace-write-offline | ready | Inside write allowed; sentinel and network writes denied. |
| Muse / workspace-write-network | ready | Inside and network writes allowed; sentinel write denied. |
| Claude / read-only | blocked before claim | Installed CLI responds as `2.1.261`, but `claude auth status --json` reports `loggedIn: false`; no provider work started. |

The latest live Codex timer receipt was generated at `2026-09-07T06:39:22Z` and
is valid for 24 hours. The pinned executable identity was
`sha256:65a9bf8c7a2474688eef10baea6a0e388fdfc2717fef12cc808108156f6af582`.

## Verification summary

- `go test ./internal/runner/... -count=1`: pass.
- Focused `cmd/tusker` admission, catalog, policy, recovery, migration, lifecycle, and capability tests: pass.
- `sh scripts/test-installed-runner-conformance.sh`: pass.
- UI: 168 tests pass, 0 fail; TypeScript check and production build pass.
- `go vet ./internal/runner/... ./cmd/tusker`: pass.
- `make mac-preview`: built, signed, installed, and launched TuskerBar `0.1.0`; code signature is Developer ID `3CQX5AJP6M`.
- Broad `go test ./... -count=1`: 1,383 pass, 43 fail, 3 skip. The failures are existing repository-wide delivery/state fixtures, including armed-wave safety, missing root `tusker.yaml`, cross-scope gates, and nested validation timeouts. They do not invalidate the focused runner gates and are not promoted to pass here.

## Configuration-only onboarding

A new route using the Codex CLI dialect needs one runner profile with a structured
command and permission preset. Muse is the proof: it reuses the Codex adapter and
adds only `--profile muse`. A new ACP endpoint needs an installed executable,
structured arguments, declared native containment, and a passing live handshake.
A CLI with novel event or permission semantics still needs a small dialect adapter;
Tusker never treats an arbitrary shell command as a trustworthy harness protocol.
