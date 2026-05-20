---
schema: "tusker.epic/v5"
id: "VSK"
title: "V7 skill-shaped knowledge base"
type: "epic"
status: "done"
owner: "sarav"
summary: "Make V7 initialization, validation, documentation, and skill packaging treat repo knowledge as a first-class agent skill while preserving the Tusker operator skill."
created: "2026-05-14"
updated: "2026-05-14"
completed: "2026-05-14"
transitions:
  - at: "2026-05-14T12:58:08Z"
    kind: "status"
    from: "draft"
    to: "done"
    actor: "reviewer:codex"
    reason: "All VSK stories and follow-up bug tasks are implemented, verified, and closed."
---

# VSK · V7 skill-shaped knowledge base

## Thesis
V7 must make repo knowledge skill-shaped, not merely documented. A fresh repository setup should produce both the Tusker operator skill contract and a repository-specific project knowledge skill that coding agents can load, route through, update safely, and validate.

## Scope

In:
- V7 init/profile behavior for repo-local knowledge domains and skill entrypoints.
- V7 project skill export from `tusker/knowledge/domains/**`.
- CLI validation that checks the two-skill contract and generated routing surfaces.
- Documentation/spec updates that make the skill contract explicit and testable.
- End-to-end smoke coverage for init, skill export, route, packet, and validation.

Out:
- Remote object-store implementation.
- Collaborative editor/mobile mode.
- Rewriting the existing V6 knowledge system unless needed for compatibility shims.

## Success metrics

- A fresh V7 repo can be initialized with operator-skill guidance plus a project knowledge skill.
- The exported project skill contains `SKILL.md` and V7 domain canon from `tusker/knowledge/domains/**`.
- `tusker validate` can fail loudly when required skill/docs/knowledge surfaces are missing or stale.
- Agent packets point to the relevant project skill/domain canon instead of asking agents to browse everything.
- The V7 spec, operator docs, and generated project skill all describe the same contract.

## Canon

- `tusker/docs/spec/tusker-v7-repo-local-work-tracker-spec.md`
- `skill/SKILL.md`
- `tusker/SKILL.md`
- `cmd/tusker/commands_v7.go`
- `cmd/tusker/v7_validation.go`

## Task stack

_No open tasks. Use `tusker list --epic VSK --type task --status done` for closed history._

## Open questions

- Should the V7 project skill live at `skills/project/SKILL.md`, `tusker/SKILL.md`, or both with one generated from the other?
- Should `publish skill` become V7-aware by default when V7 knowledge exists, or require an explicit `--v7` flag during transition?
