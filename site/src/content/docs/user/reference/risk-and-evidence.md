---
title: "Risk and evidence"
description: "How risk drives ceremony, what evidence each risk tier requires, and who can attest."
tusker:
  audience: "user"
  publish_path: "user/reference/risk-and-evidence"
  publish_section_title: "Reference"
  route: "/user/reference/risk-and-evidence/"
  source_kind: "repo_doc"
  source_path: "skill/references/RISK_AND_EVIDENCE.md"
  summary: "How risk drives ceremony, what evidence each risk tier requires, and who can attest."
  tags:
    - "reference"
  updated: "2026-04-22"
---

# Risk and evidence

How risk drives ceremony, what evidence each risk tier requires, and who can attest.

## Risk is ceremony, not effort

`risk` is orthogonal to `size`. A small typo fix can be `risk: low`; a one-liner that flips a feature flag in prod can be `risk: high`. Pick by blast radius, not by lines of code.

| Risk | When to pick it |
|---|---|
| `low` | typo, doc tweak, local refactor, dev-only script, anything reversible with a revert |
| `medium` | feature with tests, refactor of one module, migration with rollback, UI change on a non-critical path |
| `high` | security-sensitive change, cross-module refactor, data migration, anything with a staged rollout plan |
| `critical` | payment/auth/PII pathway, irreversible migration, incident response, anything where a bad deploy means user-visible harm |

## Section requirements

See `FORMAL_INTAKE.md` for the full matrix. Headline:

- `low` — Problem, Acceptance, Plan, Verification, Work log, Agent handoff
- `medium` — adds Canon, Code anchors, Evidence
- `high` — adds Considered and rejected, Decision, Rollout
- `critical` — adds Kill list, plus human attestation + code-owner signoff

## Evidence tiers

`## Evidence` holds artifacts *after* execution, not plans.

| Risk | Minimum evidence |
|---|---|
| `low` | one line: commands run, "tests pass," PR link |
| `medium` | test output OR screenshot/gif, plus PR link |
| `high` | test output, before/after, rollback notes, PR link |
| `critical` | all of high + security review summary |

### Conditional demo rule

If `change_type: feature` AND `surfaces` includes a UI surface (frontend/desktop/mobile) AND `risk ≥ medium`:

**`## Evidence` MUST include a demo asset** — video/gif/screenshot — linked from `Attachments/` or a stable external host. The validator enforces this.

### Where binaries live

Vault-relative: `Attachments/<STORY-ID>/<filename>`. The `tusker attach-evidence` command handles this:

```bash
tusker attach-evidence --id <STORY-ID> --kind <screenshot|video|log|bench|pr> \
  --path <file-or-url> [--note "..."]
```

Copies the file, appends a link to `## Evidence`, logs a Work log line.

## Attestation

Attestation asserts: **"I reviewed the evidence and understand the final behavior and edge cases."** Not a claim of having read every line.

| Risk | Required | Who |
|---|---|---|
| `low` | 1 attestation | agent peer OR human |
| `medium` | 1 attestation | agent peer OR human |
| `high` | 1 attestation | **human only** |
| `critical` | 1 attestation + 1 code-owner signoff | **human only** |

### Commands

```bash
tusker attest --id <ID> --by <name> --role <agent|human>
tusker signoff --id <ID> --by <owner>     # critical only
```

Both write frontmatter fields (`attested_by`, `attested_at`, `attested_role`, `signoff_by`, `signoff_at`) and append to Work log.

### Agent peer attestation

At `risk ≤ medium`, another agent (not the author) can attest. The peer must read the evidence and run the verification plan. Rubber-stamp attestation is worse than no attestation because it burns the trust signal.

### Human attestation at high/critical

A human must read the evidence. Agents cannot attest their own work at these risk levels. The validator refuses `status: done` without human attestation.

## What the validator checks

On worker handoff / `set-status active/in_review/done`:

- required sections exist AND have substance (not empty, not just "TODO")
- evidence meets the risk floor
- demo asset present if the conditional rule fires
- AI disclosure set
- attestation present for `done` per risk table

On `set-status done` at critical: signoff also required.

## Rollout and kill list

### Rollout (required at risk ≥ high)

Three parts:

- **Feature flag** — name it. `<scope>_<behavior>_enabled` is the convention. Default off on first build.
- **Staged rollout** — dev → bake → prod default. Name the bake duration.
- **Rollback plan** — literal command / toggle / revert. One sentence; prove it's fast.

### Kill list (required at risk = critical)

What gets deleted when this ships. Forces you to commit to the cleanup rather than leaving two code paths alive forever. One bullet per thing to delete, with the story or PR that will do the deletion.
