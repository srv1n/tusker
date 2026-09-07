---
subject: real-work-test-packets
title: "Real-product testing — three implementation assignments"
keywords: [repeatable testing, CLI parity, real execution, handoff]
part_of: repeatable-work-testing
status: canonical
created: 2026-09-07
read_when: "Implementing or assigning real-product testing — three implementation assignments."
skip_when: "Looking up already verified installed behavior; this is an implementation contract."
sources: [repeatable-work-testing.md, work-knowledge-and-retention.md]
decisions_locked: false
capsule:
  what: "Self-contained work packet with scope, ownership, acceptance and verification."
  use_when: "Assigning this bounded work to an implementation agent."
  skip_when: "Orchestrating unrelated tasks or redesigning the product."
---

# Real-product testing — assignment index

These are detailed, copyable implementation packets requested by the user. They are Markdown contracts, not claims of newly allocated Tusker task IDs. Give each worker its entire linked packet. The authoring session does not launch workers, start tests or manage execution.

## What the operator wants

One designated disposable test repository. A CLI command resets its Tusker-owned state and repopulates fictional documentation, a standalone task and three waves. The operator or an agent can start an individual task, see it progress/review/complete, then run waves and watch the real DAG. Two waves can run concurrently. Every domain operation needed for that journey is CLI-accessible, so agents need no computer access. The UI subsequently exercises the same repository and execution mechanisms. Start with real Codex Luna; later repeat a mixed Codex/Muse scenario using installed, configured profiles.

Existing demo code is useful groundwork. Its timer driver, special wave authorization and reviewer-close composition are not acceptance proof of the ordinary production pathway. Reuse the seed/scenario/reset machinery; do not build a competing runtime.

## Assignments

| Packet | Suggested existing owner | Output | Dependency |
|---|---|---|---|
| [[real-work-lifecycle-cli]] | Existing CLI/ACP execution agent | Consistent task/wave lifecycle and CLI parity, including truthful shared-checkout closeout | None for investigation/implementation |
| [[real-work-test-repository]] | Existing repeatable-test agent | Resettable real-harness test repository and machine-only acceptance script | Can seed/build immediately; final real execution depends on lifecycle contract |
| [[real-work-ui-acceptance]] | Existing Work/Documents UI agent | Live task/wave UI, SSE recovery and document-discovery acceptance | Can inspect/build tests immediately; final live run depends on lifecycle and seeded repository |

Start all three workers on their bounded implementation. Do not pretend their final acceptance is independent. Lifecycle owner publishes exact supported command/JSON/event contracts before the other two connect to them. Existing interfaces remain the starting point; a new schema or framework is not the default.

## Shared file coordination

The lifecycle owner owns non-demo runtime/command routing. The fixture owner owns demo implementation and fixture scripts. The UI owner owns Work/Documents UI and existing event/query integration. `cmd/tusker/cli.go`, `capabilities_cmd.go` and `docs/system/cli.md` are shared files with named sections: lifecycle owner integrates ordinary lifecycle commands; fixture owner supplies only demo-command changes; UI owner reports missing routes rather than editing dispatch. Changes must be narrow, reread immediately before patching, and preserve sibling edits. Each packet repeats this boundary.

## Completion standard

A short result report identifies source candidate, actual commands, actual harness/profile, work IDs, assertions and limits. Distinguish deterministic fixture success, genuine provider execution, UI integration and human acceptance. No silent model fallback, manually forged status, hidden demo substitute or completion based on a successful process exit alone. Existing task schema remains; diagnose lifecycle inconsistency rather than adding statuses.

Operational execution permissions remain governed by repository instructions and the user's assignment. In particular, writing these packets does not start a daemon, enable automation, launch paid work or reset any project. A later worker must use the supported execution owner and report precise missing prerequisites rather than launch nested workers contrary to repository rules.
