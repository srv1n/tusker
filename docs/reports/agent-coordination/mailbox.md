---
title: "Mailbox behavior"
read_when: "Recovering the mailbox behavior proof table and TestAgentMessages result for agent coordination (historical evidence record)."
skip_when: "You need mailbox design or contract details; read agent-coordination/contracts.md or the current docs/system chapter."
---

# Mailbox behavior

| Case | Result |
|---|---|
| reopen after question/reply | preserved |
| duplicate enqueue | original returned |
| forged reply recipient | refused |
| unsupported address kind | refused |
| delivery without transport proof | remains pending/unknown |

Verification: `TestAgentMessages` — PASS.
