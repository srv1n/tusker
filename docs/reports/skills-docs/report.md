---
title: "Combined skills and documentation check"
subject: reports/skills-docs/combined-check
part_of: skills-and-documentation
status: report
created: 2026-09-11
---

# Combined skills and documentation check

`FLW-T-0036` is not ready to close. Fresh scaffold, package and provenance
checks mostly pass, but the TRACK disclosure route exceeds its word budget and
the documentation graph is blocked by an unrelated malformed decision note.
No active installation, runtime setting, daemon, provider or user skill was
changed.

## Acceptance results

| ID | Result | Evidence and limit |
| --- | --- | --- |
| A1 | partial | Fresh-project scaffold, package, symlink and materialized-copy preservation tests passed. The combined suite still fails A1 because TRACK loads 690 words against a 650-word budget. |
| A2 | partial | The five reader exercises below produce the expected actions from bounded current sources. No independent review result is recorded. |
| A3 | fail | `tusker docs map` refused `.tusker/specs/decisions/2026-09-11-project-registration-and-visibility-grill.md`; `tusker docs check --json` reports missing `subject`, `decides_for`, and `part_of`. That file is outside this task. |
| A4 | met for reporting | This report separates executed source/package checks, reader judgment, installed-version drift and unrun live journeys. It does not claim unmeasured savings or product acceptance. |

## Five fresh-reader exercises

| Exercise | Bounded route | Result |
| --- | --- | --- |
| Choose a design method | `skills/tusker/SKILL.md` → design/writing composition; `methods.md` → host state | Use the installed `grilling` skill for unresolved choices, then return settled decisions or open questions to Tusker. |
| Handle a missing optional writing method | `skills/tusker/SKILL.md` → local prose rule; `methods.md` → pinned sources | Report `technical-writing`/`unslop` unavailable and apply the local rule. Do not claim use or install them. |
| Accept settled intent | `skills/tusker/SKILL.md` → design/writing composition | Skip another interview; preserve the supplied spec and convert it into tracked contracts. |
| Find task and wave procedure | `skills/tusker/SKILL.md` → `references/TRACK.md`; `docs/system/delivery-and-waves.md` for the wave guide | Use `tusker work` for interactive task lifecycle; wave preparation and authorization remain separate. |
| Distinguish authored from executed proof | `skills/tusker/SKILL.md` → `references/RUN.md`; `workflows.md` command comparison | Author command evidence as `pending`; only a qualified executor records `pass` or `fail`. |

The exercise used four distinct operating documents plus the two prerequisite
reports named above. Command discovery required targeted `work --help`,
`status --help`, `capabilities --json`, and the task capsule. No before/after
time or token baseline exists, so no savings percentage is claimed.

## Executed checks

| Check | Result | First actionable finding |
| --- | --- | --- |
| `go test ./cmd/tusker -run 'TestExternalDesignSkillRouting|TestScaffoldDocumentationSystem|TestScaffoldSkills|TestAgentSkillPackage|TestSkill.*|TestTuskerSkillProgressiveDisclosure|TestMaterializedSkillProvenance|TestSymlinkProvenance' -count=1 -v` | FAIL | `TestTuskerSkillProgressiveDisclosure`: TRACK loaded 690 words, budget 650. The obsolete human-approval wording assertion was corrected and now passes. |
| `tusker docs map` using the checkout CLI | FAIL | `DOCS_MAP_ORPHAN` for the unowned September 11 project-registration decision note. No generated map update was produced by this run. |
| `tusker docs check --json` using the checkout CLI | FAIL | The same note lacks `subject`, `decides_for`, and `part_of`; 42 documents inspected. |
| `git diff --check` over the six owned paths | PASS | No whitespace errors. |

The installed binary is
`archive/pre-convergence-main-20260727-420-g4b5d2076-dirty` at revision
`4b5d20769b6cf6bd221752bdfb946e5242d98349`; the tested checkout CLI reports
`dev`. The installed binary cannot read the current registry because it expects
an older schema (`no such column: visible`), so package checks used the checkout
source without replacing the installed binary.

## Prerequisite artifacts and routed findings

- [External methods](methods.md): source links, missing optional methods,
  scaffold behavior and preserved user-owned copies. Route-budget failure
  returns to `FLW-T-0031`.
- [Everyday workflows](workflows.md): task/proof examples and executor-owned
  results.
- [Current design](current-design.md): settled direction and bounded reading
  path.
- [Worker recipes](recipes.md): verification and knowledge routes.
- [Slash prose lint](prose-lint.md): ordinary slash examples remain prose.

The malformed project-registration decision note belongs to its current owner;
this integration task does not repair it.

## Still unrun

- Independent reviewer completion of the five exercises.
- Live task execution, wave scheduling, message delivery, provider/model use,
  installed-app behavior and full `FLW-T-0025` runtime journey.
- `tusker validate --json` did not complete in the bounded command window after
  the preceding document failures and is not a pass.

## Tracking state

The user stated that all five prerequisite deliverables are complete, but
Tusker still records `FLW-T-0031`, `FLW-T-0032`, `FLW-T-0034`, and
`FLW-T-0035` as backlog with pending proof. Tusker refused moving this task to
ready, and an interactive agent cannot impersonate a human actor. This report
records the integration result without rewriting those lifecycle records or
manufacturing proof.
