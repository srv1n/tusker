---
title: "Agent coordination interface"
read_when: "Recovering the evidence for the task inspector's agent-coordination display of architect, peers and durable questions."
skip_when: "You need backend mailbox or wakeup behavior; read the clarification evidence or current docs/system chapter."
---

# Agent coordination interface

The existing task inspector shows architect, origin, and peers separately from prerequisites, then renders durable questions/replies with their transport and lifecycle facts. It does not present a new chat surface or infer delivery.

Verification: `bun test test/agent-coordination.test.ts` and `bun run typecheck` — PASS.
