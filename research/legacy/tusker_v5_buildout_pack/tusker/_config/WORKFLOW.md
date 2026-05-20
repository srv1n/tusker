---
schema: tusker.workflow/v5
name: default
states:
  - draft
  - ready
  - active
  - blocked
  - review
  - rework
  - done
  - cancelled

allowed_transitions:
  draft: [ready, cancelled]
  ready: [active, blocked, cancelled]
  active: [review, blocked, rework, cancelled]
  blocked: [ready, active, cancelled]
  review: [rework, done, blocked]
  rework: [active, blocked, cancelled]
  done: []
  cancelled: []

close_gate:
  require_verification: true
  require_docs_resolution_when_doc_nodes_present: true
  allow_explicit_waiver: true
  notes:
    - Closing a task with `doc_nodes` is a gated operation.
    - `tusker docs check` is agentic and may return no-op, patch, or waiver-required.
    - `tusker close` must fail if docs impact is unresolved.

audit_model:
  current_state_source: task frontmatter
  events: _system/events/*.jsonl
  runs: _system/runs/<TASK-ID>/<attempt>.json
  generated: _system/generated/*

risk_policy:
  low:
    required_sections: [Intent, Acceptance contract, Evidence]
  medium:
    required_sections: [Intent, Scope, Acceptance contract, Deliverables, Verification plan, Evidence]
  high:
    required_sections: [Intent, Scope, Acceptance contract, Canon, Code/system anchors, Constraints, Deliverables, Verification plan, Knowledge delta, Evidence, Verification log]
  critical:
    required_sections: [Intent, Scope, Acceptance contract, Canon, Code/system anchors, Constraints, Deliverables, Verification plan, Knowledge delta, Rollback, Evidence, Verification log]
---

# Workflow policy

Tusker v5 treats the tracker as an **executable contract system**.

## Rules

1. A task is ready to execute only when the acceptance contract and verification plan are specific enough that an agent can act without inventing the product requirements.
2. A task is not done until the verification result is written down and every affected documentation node is updated, verified as unchanged, or explicitly waived.
3. Events are an audit trail, not the current source of truth.
4. The daemon may read and append audit state, but humans can still understand the live state of work by opening the task Markdown file directly in Obsidian.
5. The workflow file is repo-owned policy. Change policy here instead of relying on oral tradition.

## Close flow

```text
review requested
  ↓
verify task result
  ↓
if doc_nodes non-empty:
    tusker docs check <id>
    → no-op verification
    → patch proposal
    → waiver required
  ↓
resolve docs outcome
  ↓
tusker close <id>
```

## Phase-1 hard validator rules

- `UNKNOWN_DOC_NODE`
- `DOCS_IMPACT_UNRESOLVED`
- `MISSING_KNOWLEDGE_DELTA` at `risk >= high`

Everything else may begin as warnings until the failure mode is observed in practice.
