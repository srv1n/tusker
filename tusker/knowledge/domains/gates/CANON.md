---
schema: "tusker.domain-canon/v7"
kind: "domain_canon"
id: "gates/canon"
project: "tusker"
domain: "gates"
title: "gates Canon"
status: "current"
summary: "Current durable truth for gates."
source_of_truth:
  - "knowledge/domains/gates/CANON.md"
created_at: "2026-05-19T05:18:02Z"
updated_at: "2026-05-29T14:00:10Z"
state_rev: "sha256:9659f18e7ae01269555d797692baefc05f690692d4527264b89e064f7a2058a0"
---

# gates Canon

## Current Truth

- Gates are explicit blockers or approval requirements, not vague human tickets.
- Human/external blocking gates require owner, blocked task, concrete action, observable verification, and `why_agent_cannot`.
- Human gates are valid for credentials/secrets/account setup, physical device or unavailable environment access, external service/CI state, product/security/release/final signoff, and contradictory or unusable product/spec decisions.
- Code review, diffs, code comparison, test/log inspection, docs review, and implementation judgment are agent/reviewer-owned. Use an independent reviewer or subagent instead of a human gate.
- A human-owned spec/API/product conflict must be a `decision` gate with a `suggestion` naming the agent's recommended resolution.
- Placeholder actions such as "Resolve this gate" or "Owner confirms the gate is satisfied" are invalid.

## Stable Interfaces

- `tusker new gate --blocks <TASK-ID> --kind <gate-kind> --owner <owner> --action <action> --verification <proof>`.
- Human/external gates add `--why-agent-cannot <boundary>`.
- Human/external decision gates also add `--suggestion <recommended resolution>`.

## Constraints

- Keep this canon short enough to read before implementation.
- Move obsolete details to Deprecated Or Stale instead of deleting useful history.

## Deprecated Or Stale

- _None known._

## Open Questions

- _None yet._
