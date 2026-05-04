# Risk And Evidence

Risk drives ceremony. Size is effort; risk is blast radius.

| Risk | Use For | Minimum Evidence |
|---|---|---|
| `low` | typo, doc tweak, local refactor, dev-only script | one useful line: command run, PR link, or result |
| `medium` | ordinary feature, tested refactor, reversible migration | test output or demo asset, plus PR/diff link |
| `high` | security-sensitive change, cross-module refactor, data migration | tests, before/after proof, rollback notes, PR/diff link |
| `critical` | auth/payment/PII, irreversible migration, incident response | all high-risk evidence plus explicit human review notes |

## Required Sections

V5 task templates provide the sections. Fill them with substance; do not leave placeholders.

- `low`: `Intent`, `Acceptance contract`, `Evidence`
- `medium`: add `Scope`, `Deliverables`, `Verification plan`
- `high`: add `Canon`, `Code/system anchors`, `Constraints`, `Knowledge delta`
- `critical`: add rollback detail and explicit human review notes

## Evidence Command

```bash
tusker evidence <TASK-ID> <screenshot|video|log|bench|pr|packet> <file-or-url> [--note "..."]
```

Local files are copied under `Attachments/<TASK-ID>/`; URLs are linked as-is. Evidence is proof after execution, not a plan.

## Demo Rule

If `kind: feature`, the task touches a UI surface, and `risk >= medium`, attach a screenshot, GIF, or video.

## Knowledge Delta

When a task changes durable understanding, fill:

| Topic | Before | After | Audience | Target doc nodes |
|---|---|---|---|---|

The row must explain the reader-facing change. “Updated docs” is not a knowledge delta.

## Close Gate

Before close:

```bash
tusker docs check <TASK-ID>
tusker status <TASK-ID> review
tusker verify <TASK-ID> --by <name>
tusker close <TASK-ID> --by <reviewer>
tusker validate
```

`close` refuses tasks with unresolved `doc_nodes`.
