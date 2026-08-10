---
title: "ACP runner migration: one bounded local agent transport"
subject: acp-runner-migration
keywords: [acp, runner, codex, claude, permissions, transport]
part_of: software-factory
status: canonical
created: 2026-08-10
read_when: "Changing local Codex or Claude runner transport, permissions, lifecycle, capability negotiation, or ACP onboarding."
skip_when: "Working on codex_cloud, task semantics, evidence, gates, waves, or release packaging."
decisions_locked: true
sources:
  - "[[2026-08-10-acp-runner-migration]]"
  - "docs/specs/26-acp-runner-migration.md"
---

# ACP runner migration

Tusker will standardize local Codex and Claude session transport on ACP v1
over bounded stdio. Tusker remains the sole authority for tasks, claims,
leases, attempts, workspaces, policy, budgets, evidence, gates, review, waves,
dispatch, landing, and release. ACP sessions and provider events are execution
observations, never authority.

The full implementation contract, lifecycle, permission rules, delivery DAG,
file ownership, conformance matrix, fallback policy, and deletion gates live in
[`docs/specs/26-acp-runner-migration.md`](../../docs/specs/26-acp-runner-migration.md).

Locked boundaries:

- ACP version 1 over local stdio, one preinstalled fingerprinted adapter process
  per Tusker attempt, launched by argv with no shell or runtime download.
- Phase one advertises no client filesystem, terminal, elicitation, or MCP
  capability.
- Tusker's resolved policy is the permission ceiling; ambiguous or wider
  provider requests fail closed.
- Prompt delivery uncertainty is typed `delivery_unknown` and never retried
  automatically.
- `codex_cloud` retains its separate remote `Start`/`Reconcile`/`Collect`
  lifecycle.
- Direct Codex and Claude fallbacks remain until provider parity, soak,
  rollback, packaging, and human cutover gates pass; the migration succeeds
  only when superseded provider-specific lifecycle code can then be deleted.
