---
workflow_version: 1
tracker_schema_version: 7
tracker:
  kind: tusker_vault
  dispatch_states:
    - ready
    - rework
  review_states:
    - review
  terminal_states:
    - done
    - cancelled
    - superseded
agents:
  default: codex_app_server
  enabled:
    - codex_app_server
    - codex_exec
    - claude-code
  max_concurrent_agents: 2
  max_concurrent_agents_by_state:
    rework: 1
runtime:
  poll_interval_ms: 5000
  lease_ttl_ms: 900000
  max_active_runs_per_project: 1
workspace:
  root: ../.tusker-worktrees
  strategy: worktree
retry:
  max_attempts: 3
  backoff_ms:
    - 30000
    - 120000
    - 600000
reviewer:
  enabled: true
  runner: codex_app_server
  actor: reviewer:agent
  auto_close_risks:
    - low
    - medium
  human_required_risks:
    - high
    - critical
  prompt: "Review only. Do not edit implementation files. Verify acceptance, proof, gates, and docs impact. Return rework for any unmet acceptance item."
runners:
  codex_app_server:
    kind: codex_app_server
    command: codex app-server
    approval_policy: on-request
    thread_sandbox: workspace-write
    turn_sandbox_policy: workspace-write
    turn_timeout_ms: 600000
    read_timeout_ms: 30000
    stall_timeout_ms: 120000
    max_turns: 1
  codex_exec:
    kind: codex_exec
    command: codex exec --skip-git-repo-check -
  claude-code:
    kind: claude-code
    command: claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions
codex:
  command: codex app-server
  approval_policy: on-request
  thread_sandbox: workspace-write
  turn_sandbox_policy: workspace-write
  turn_timeout_ms: 600000
  read_timeout_ms: 30000
  stall_timeout_ms: 120000
  max_turns: 1
claude:
  command: claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions
extensions:
  enabled: false
  allowed_tools: []
  allowed_mcps: []
  allow_tusker_read_tools: false
hooks:
  after_workspace_create: []
  before_workspace_remove: []
fanout:
  enabled: false
  max_children: 0
  allowed_child_types: []
  merge_rule: manual_review
---

## Routing

Dispatch only from durable task states `ready` and `rework`. Do not create or use a durable `active` task status. Runtime activity belongs to run leases and attempts.

## Hard stop check

Before doing work, run `tusker closeout status {{ note.id }} --json` when closeout data exists. If it reports `agent_action=stop_until_human_response`, stop. Do not validate again, inspect unrelated files, spawn agents, or mutate Tusker records. Reply with the pending human gate or proof item.

Revalidate only after files changed, a task/gate/evidence state changed, the closeout fingerprint changed, or a human explicitly requested fresh validation.

## Prompt

Use the installed Tusker operator skill for task semantics. Use `.tusker/SKILL.md` for this repository's routing. Work inside `{{ workspace.path }}`. Treat `{{ repo.root }}` as source context unless the task says otherwise.

Item: {{ note.title }}
Record: {{ note.record_id }}
Type: {{ note.type }}
Attempt: {{ attempt.number }}
Workflow: {{ workflow.path }}
Vault: {{ vault.path }}

## Command budget

Use the smallest command that proves or locates the next fact. Prefer `tusker automation plan`, `tusker packet`, path-scoped search, and exact verification commands. Report validation as command + PASS/FAIL plus the first actionable failure. Never paste raw transcripts into task markdown.

## Completion contract

Satisfy the task proof mode. For `proof_mode=inline`, record concise verification rows with `tusker verify add`; do not create evidence files. For `card`, `artifact`, or `audit`, create only the evidence records required by the task.

When machine work is complete and only human-owned proof or gates remain, run `tusker closeout <task-id> --emit-packet --validate "<command>"`, then stop. When work is ready for review, use `tusker finish <task-id> --request-review` so the task reaches `review` or a branch-safe proposal is created. Attempt handoff alone is not a review request.

## Reviewer contract

If `reviewer.enabled` is true, review tasks may dispatch to `reviewer.runner`. The reviewer must not edit implementation files. Low/medium risks can close after all gates pass. High/critical risks stay in `review` for human acceptance.

## Retry policy

Retry only transient infrastructure failures. Human-directed rework creates a new ready/rework task revision.

## Human override policy

Humans may edit task contracts. Runtime state belongs to the daemon/runtime store.
