---
title: "Risk And Evidence"
description: "Evidence exists to prove acceptance, not to narrate effort."
tusker:
  audience: "user"
  publish_path: "user/reference/risk-and-evidence"
  route: "/user/reference/risk-and-evidence/"
  source_kind: "repo_doc"
  source_path: "skill/references/RISK_AND_EVIDENCE.md"
  summary: "Evidence exists to prove acceptance, not to narrate effort."
  tags:
    - "reference"
  updated: "2026-05-18"
---

# Risk and Evidence

Evidence exists to prove acceptance, not to narrate effort.

## Risk levels

| Risk | Meaning | Proof bar |
|---|---|---|
| `low` | small local change, easy rollback | inline verification is usually enough |
| `medium` | cross-file behavior or user-visible change | inline or card proof plus focused test/check |
| `high` | migration, security-sensitive edge, cross-module refactor, manual/UI proof | card/artifact proof, knowledge delta, independent review |
| `critical` | auth/payment/PII, irreversible migration, incident response | audit proof, rollback, explicit human signoff |

## Proof modes

| Mode | Use when | Evidence behavior |
|---|---|---|
| `inline` | small low-risk work | verification rows in task; no evidence file |
| `card` | durable acceptance summary is useful | one concise evidence card |
| `artifact` | proof needs screenshot/video/trace/CI/manual artifact | one evidence card plus selected artifact |
| `audit` | high/critical or migration/release proof | checklist, gates, evidence packet, human/reviewer policy |

## Proof ownership

Every missing proof item must have an owner:

```json
{
  "machine_missing": [],
  "reviewer_missing": [],
  "human_missing": ["proof_required:human_signoff"],
  "external_missing": [],
  "agent_action": "stop_until_human_response"
}
```

Default owners:

| Proof requirement | Owner |
|---|---|
| `focused_test`, `broad_test`, `typecheck`, `lint`, `static_check` | machine |
| `human_signoff`, `manual_smoke`, `product_approval`, `security_approval`, `release_approval` | human |
| `ci_green`, unavailable environment/device/service | external unless runnable locally |

If ownership is ambiguous, create a gate. Do not guess and loop.

## Evidence quality

Good evidence:

- names the acceptance item covered;
- says what was checked;
- gives a concise result;
- points to a small artifact only when needed;
- includes caveats when the proof is partial.

Bad evidence:

- full passing logs;
- copied source files;
- generated indexes;
- terminal transcripts;
- screenshots that do not prove the acceptance item;
- repeated negative searches;
- agent diary entries.

## Closeout proof rule

`finish` or closeout should fail or stop when required acceptance IDs lack one of:

- inline verification;
- accepted evidence;
- satisfied gate;
- explicit waiver;
- human/external gate with `agent_action: stop_until_human_response`.

The last case is not “done”; it is machine-complete waiting for human/external action.

## Human proof

Human proof should be a gate or explicitly owned proof requirement.

Preferred gate:

```yaml
kind: gate
owner: human:sarav
gate_kind: signoff
blocks:
  - SAM-T-0008
action: Review the source moves, shims, and smoke-manifest evidence.
verification: Human accepts or waives this gate.
```

Do not keep `manual_smoke` or `human_signoff` as a floating unowned proof gap.

## Test discipline

Tests are proof tools, not rituals.

Before adding a test, identify:

- acceptance item covered;
- failure class prevented;
- smallest useful test level;
- exact command to run it.

Small work usually needs zero or one focused test plus the relevant existing suite. After three failed focused attempts, stop and summarize or gate instead of thrashing.
