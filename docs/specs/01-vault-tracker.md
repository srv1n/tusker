---
capsule:
  what: "V5 vault tracker spec covering note model, indexes, and validation."
  use_when:
    - "Comparing legacy tracker assumptions with current V7 behavior."
  skip_when:
    - "Implementing current V7 task/domain schema."
---

# 01 - Vault Tracker

Tusker V5 stores durable work in Markdown. Frontmatter is the machine-readable current state; body sections are the human-readable contract and proof.

## Note Model

| Type | ID / node | Path | Purpose |
|---|---|---|---|
| Epic | `MEM` | `epics/MEM/MEM.md` | Workstream boundary, canon, success metrics, task stack |
| Task | `MEM-T-0001` | `epics/MEM/MEM-T-0001.md` | Executable change contract |
| Bug task | `MEM-T-0002`, `kind: bug` | `epics/MEM/MEM-T-0002.md` | Defect work using the task model |
| Doc | `reference/cli` | `docs/reference/cli.md` | Durable knowledge page |
| Note | no managed ID | any readable note path | Unmanaged local context |

The current managed work item is the task.

## Task Statuses

| Status | Meaning |
|---|---|
| `draft` | captured but not ready |
| `ready` | scoped and eligible |
| `active` | being worked |
| `blocked` | waiting on concrete input |
| `review` | work is ready for verification |
| `rework` | verification or review found changes |
| `done` | verified, docs impact resolved, closed |
| `cancelled` | intentionally stopped |

## Task Contract

Task frontmatter carries:

- `schema: tusker.task/v5`
- `id`, `title`, `type: task`, `kind`
- `epic`, `status`, `priority`, `risk`, `size`
- `delegation`, `ai_assistance`, and optional `ai_tools`
- optional `domains`, `doc_nodes`, `blocked_by`, `blocks`
- lifecycle stamps and close/verification fields

Task body sections carry:

- `Intent`
- `Scope`
- `Acceptance contract`
- `Canon` for high/critical work
- `Code/system anchors` for high/critical work
- `Constraints` for high/critical work
- `Deliverables`
- `Verification plan`
- `Knowledge delta` for high/critical work or when durable understanding changes
- `Evidence`
- `Verification log` for high/critical work

Default task bodies are capsule-first and risk-scaled. They do not carry
`Execution plan` or `Work log` sections by default; disposable planning belongs
in runner context, and durable truth belongs in acceptance, evidence,
verification/close summaries, transitions, and knowledge deltas.

## Docs Routing

`domains` are broad areas such as `schema`, `cli`, `docs`, `runtime`, `obsidian`, and `skill`.

`doc_nodes` are exact docs-map node IDs. If a task has `doc_nodes`, close is blocked until each node is applied, verified as no-op, or waived.

## Generated Outputs

Generated files are disposable rebuild products:

- `_system/generated/tasks.index.json`
- `_system/generated/docs.index.json`
- `_system/generated/epics.index.json`
- `_system/generated/dashboard.json`
- `_system/generated/links.index.json`
- `_system/generated/publication.index.json`

## Validation

Current Tusker notes must use `schema: tusker.<type>/v5`. Normal validation rejects non-V5 managed notes.
