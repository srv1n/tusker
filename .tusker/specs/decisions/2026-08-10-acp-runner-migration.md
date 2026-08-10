---
title: ACP runner migration decision
subject: acp-runner-migration-decision
keywords:
  - acp
  - runner
  - codex
  - claude
  - permissions
  - authority
  - transport
status: canonical
part_of: acp-runner-migration
created: 2026-08-10
read_when:
  - changing local coding-agent transport or runner lifecycle
  - adding an ACP adapter, capability, permission, or session feature
  - deciding whether direct Codex or Claude lifecycle code can be deleted
skip_when:
  - changing codex_cloud remote execution only
  - changing task, evidence, gate, wave, review, or release semantics
decides_for:
  - ACP v1 transport boundary
  - local Codex and Claude runner migration
  - authority and identity separation
  - fallback and deletion gates
---

# ACP runner migration decision

## Context

Tusker currently has a useful `Runner` boundary, but local provider integrations still own separate wire protocols: Codex app-server JSON-RPC and Claude stream/control JSON. That duplicates initialization, session lifecycle, permission mapping, cancellation, event normalization, scanner limits, and failure handling. The runner factory, daemon, profiles, catalog, capabilities manifest, process wrapper, and receipts also share provider assumptions.

ACP offers a common client/agent lifecycle for local coding agents. Buzz demonstrates that Codex and Claude can sit behind ACP adapters, but most of Buzz's larger platform is irrelevant to Tusker. The migration is justified only if it makes Tusker's local runner transport smaller, more bounded, and easier to onboard—not if it imports another orchestration system.

The implementation contract is [docs/specs/26-acp-runner-migration.md](../../../docs/specs/26-acp-runner-migration.md).

## D1 — ACP is transport, never authority

**Asked:** What moves behind ACP?

**Locked:** Provider process initialization, authentication exchange, session creation/load/resume, prompt delivery, streaming execution observations, provider tool-permission requests, cancellation, and terminal turn results may move behind ACP.

Tusker keeps task state, automation opt-in, runner selection, claims, leases, attempt identity, workspace assignment, policy, sandbox ceiling, budgets, evidence acceptance, review, gates, waves, dispatch, spend, landing, and release. ACP messages cannot mutate those authorities directly.

**Consequence:** An ACP `end_turn` is not task completion. A provider tool call or subagent is not a Tusker actor. Session updates remain observations until existing Tusker collection and evidence rules evaluate them.

## D2 — Freeze ACP version 1 over stdio

**Asked:** Which protocol and transport become production dependencies?

**Locked:** ACP integer version `1`, JSON-RPC 2.0, newline-delimited UTF-8 over a local subprocess's stdin/stdout. Version agreement is exact during `initialize`. HTTP and later draft features require a new compatibility decision.

The subprocess is preinstalled and fingerprinted, receives an absolute assigned workspace, and is launched with an argument vector and environment allowlist. No shell launch, runtime package download, or Rust sidecar is introduced.

**Consequence:** `npx -y` may be useful in upstream examples but is not a production Tusker launch strategy. Stdout is protocol-only, stderr is bounded diagnostics, and every queue/frame/deadline/process tree has a finite conformance-tested bound.

Primary contracts: [ACP overview](https://agentclientprotocol.com/protocol/v1/overview), [initialization](https://agentclientprotocol.com/protocol/v1/initialization), and [stdio transport](https://agentclientprotocol.com/protocol/v1/transports).

## D3 — One ACP process per Tusker attempt

**Asked:** Should Tusker pool adapter processes or sessions?

**Locked:** No. The first production implementation starts one ACP adapter process per Tusker attempt and tears down the complete process group at the terminal boundary.

**Consequence:** Attempt/process/session attribution stays legible; environment and workspace policy cannot leak across attempts; cancellation can fail closed. Pooling is a later optimization requiring isolation, fairness, crash-domain, cross-attempt permission, resource-accounting, and shutdown evidence.

## D4 — Negotiate exact capabilities and report readiness as a vector

**Asked:** Is the existing capability/catalog model sufficient?

**Locked:** No. Tusker advertises only fully implemented ACP client capabilities and records the exact negotiated agent capability set. `session/load` and `session/resume` remain distinct even if a legacy view derives one conservative `ResumeSession` boolean.

Installed, configured, authenticated, protocol-compatible, conformant, authorized, and running are distinct readiness states. A `--version` result or static manifest is not proof of the rest.

**Consequence:** Unsupported protocol versions and missing required capabilities fail before prompt delivery. Every attempt receipt includes adapter identity/version, executable fingerprint, protocol version, auth method, and negotiated capabilities.

Primary contracts: [initialization](https://agentclientprotocol.com/protocol/v1/initialization) and [session setup](https://agentclientprotocol.com/protocol/v1/session-setup).

## D5 — Tusker is the permission broker

**Asked:** Who decides ACP `session/request_permission` requests?

**Locked:** Tusker evaluates them against the already resolved runner profile, sandbox, approval preset, deny rules, workspace, budget, and dispatch mode. Agent-provided options cannot expand that envelope.

Phase-one unattended execution returns `allow_once` only for a complete, deterministically permitted operation; all ambiguous, forbidden, unknown, or out-of-envelope requests reject. Interrupt resolves pending requests as cancelled. Tusker never automatically chooses `allow_always`.

Tusker advertises no phase-one client filesystem, terminal, elicitation, or MCP service. Provider-side tools remain provider-side and bounded by the resolved policy.

**Consequence:** Permission handling cannot hang unattended work or smuggle in authority through an adapter. Audit records are bounded and redacted.

Primary contract: [ACP tool calls and permissions](https://agentclientprotocol.com/protocol/v1/tool-calls).

## D6 — Delivery uncertainty and cancellation are typed failures

**Asked:** When can the supervisor retry a failed prompt?

**Locked:** The client records delivery phase. A failure after prompt transmission starts and before a trustworthy terminal result is `delivery_unknown`, never an automatic retry or resume. A cancellation that misses its drain deadline kills the process group and poisons the session.

ACP stop reasons map to typed runner outcomes: `end_turn`, `max_tokens`, `max_turn_requests`, `refusal`, and `cancelled` remain distinguishable. None is flattened into a generic successful exit.

**Consequence:** The system prefers a visible uncertain attempt over duplicate side effects. Retry is possible only when the ledger proves no ambiguous delivery and the existing supervisor policy permits it.

Primary contract: [ACP prompt turns](https://agentclientprotocol.com/protocol/v1/prompt-turn).

## D7 — Keep identities and provenance separate

**Asked:** Can ACP sessions or provider subagents stand in for Tusker actors and attempts?

**Locked:** No. Principal, Tusker actor, attempt, ACP process, ACP session, turn, tool call, and provider/subagent identities are separately namespaced and related by explicit provenance.

**Consequence:** An ACP session can be stored as a runner session reference but grants no claim, lease, resume right, task transition, evidence acceptance, or gate authority. Adapter `_meta` and subagent events are bounded observations, not delegation credentials.

## D8 — `codex_cloud` stays outside ACP

**Asked:** Should every Codex execution path look uniform behind ACP?

**Locked:** No. `codex_cloud` remains a separate remote runner with durable remote task identity and asynchronous `Start`/`Reconcile`/`Collect` semantics.

**Consequence:** Tusker does not synthesize a local PID or ACP session for cloud work, apply local EOF/process-tree rules to it, alias it to `codex-acp`, or delete it with direct local lifecycle code. Architectural symmetry is not worth lying about the execution model.

## D9 — Migrate with explicit fallbacks and deletion gates

**Asked:** Do existing runners disappear when the ACP client lands?

**Locked:** No. A distinct ACP runner kind/descriptor is introduced. Persisted direct runner kinds retain their meaning. Codex crosses a parity gate before Claude integration becomes the next shared-runtime change; direct fallbacks remain during a defined compatibility window.

Direct provider lifecycle code is deleted only after adversarial conformance, permission-policy coverage, live parity, redaction, cloud separation, default-on soak, rollback, packaging, and human cutover evidence pass. Ambiguous delivery never triggers automatic fallback.

**Consequence:** This is a strangler migration with a real exit. The end state must remove the superseded app-server/control-protocol lifecycle and reduce provider-specific code, not maintain two permanent stacks.

## D10 — Adopt Buzz's boundary lessons, reject its platform topology

**Asked:** What do we reuse from Buzz?

**Locked:** Reuse the demonstrated adapter boundary and learn from its ACP-over-stdio integration. Adopt typed acceptance/refusal/unknown delivery, explicit capability/version negotiation, bounded subprocess conformance, and provenance discipline.

Do not import Nostr identities or signatures, relays, channels, public-key distribution, Redis/Postgres/S3 topology, workflow DSL, or a Rust sidecar. Tusker already has the orchestration authority those pieces would compete with.

**Consequence:** The useful architectural seam is `Tusker -> ACP client -> provider adapter`; Buzz is evidence for the seam, not a codebase to transplant wholesale.

Reviewed primary snapshot: [Buzz ACP crate at commit `119a848`](https://raw.githubusercontent.com/block/buzz/119a84897f225c1e3213a09cd149abb37dcb3abc/crates/buzz-acp/README.md), with the [reviewed commit](https://github.com/block/buzz/commit/119a84897f225c1e3213a09cd149abb37dcb3abc). Target adapters are [`codex-acp`](https://github.com/agentclientprotocol/codex-acp) and [`claude-agent-acp`](https://github.com/agentclientprotocol/claude-agent-acp).

## Facts established from the current tree

- `Runner` is the correct outer seam, but its capability booleans cannot encode exact negotiated ACP features.
- Codex and Claude direct live runners duplicate provider-specific initialization, session, permission, cancellation, event, and scanner logic.
- The generic execution helper is designed around one-way stdin prompt delivery and terminal stdout; ACP needs a bounded bidirectional client rather than superficial reuse.
- The child-wrapper path is provider-specific rather than a generic ACP descriptor host.
- Runner profile validation and the static capability manifest disagree about the retired `codex_app_server` kind, demonstrating why static availability is insufficient.
- Provider execution observations already lack authority by design; ACP must preserve that boundary.

These facts justify the migration. They do not by themselves prove adapter parity or authorize deleting a runner.

## Supersession rule

This decision controls the ACP migration boundary and supersedes conflicting proposals to replace Tusker orchestration with ACP, route `codex_cloud` through ACP, introduce a Rust/Buzz sidecar, or remove direct runners before parity. It does not supersede existing Tusker task/runtime/evidence/gate authority contracts.
