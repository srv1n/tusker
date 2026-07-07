---
agents:
  default: "codex_app_server"
  enabled:
    - "codex_app_server"
    - "codex_exec"
    - "claude-code"
  max_concurrent_agents: 4
  max_concurrent_agents_by_state:
    rework: 1
claude:
  command: "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
codex:
  approval_policy: "never"
  command: "codex app-server"
  max_turns: 1
  read_timeout_ms: 30000
  stall_timeout_ms: 120000
  thread_sandbox: "danger-full-access"
  turn_sandbox_policy: "danger-full-access"
  turn_timeout_ms: 600000
extensions:
  allow_tusker_read_tools: false
  allowed_mcps: []
  allowed_tools: []
  enabled: false
fanout:
  allowed_child_types: []
  enabled: false
  max_children: 0
  merge_rule: "manual_review"
hooks:
  after_workspace_create: []
  before_workspace_remove: []
retry:
  backoff_ms:
    - 30000
    - 120000
    - 600000
  max_attempts: 3
reviewer:
  actor: "reviewer:agent"
  auto_close_risks:
    - "low"
    - "medium"
  enabled: true
  human_required_risks:
    - "high"
    - "critical"
  prompt: "Review only. Do not edit implementation files. Verify acceptance, proof, gates, and docs impact. Re-run verification rows as written in the contract; trust commands, not the runner's summary. Actively check for reward hacking: weakened or cherry-picked verification commands, tests edited to pass, shrunk validity ranges, or unmeasured behavior sacrificed to win a measured metric. An unproven acceptance row is rework, not done. Return rework for any unmet acceptance item."
  runner: "codex_app_server"
runners:
  claude-code:
    command: "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
    kind: "claude-code"
  codex_app_server:
    approval_policy: "never"
    command: "codex app-server"
    kind: "codex_app_server"
    max_turns: 1
    read_timeout_ms: 30000
    stall_timeout_ms: 120000
    thread_sandbox: "danger-full-access"
    turn_sandbox_policy: "danger-full-access"
    turn_timeout_ms: 600000
  codex_exec:
    command: "codex exec --skip-git-repo-check -"
    kind: "codex_exec"
runtime:
  budget:
    daily_input_tokens: 10000000000
    daily_output_tokens: 100000000
    enabled: true
    per_attempt_input_tokens: 50000000
    per_attempt_output_tokens: 500000
    per_task_input_tokens: 250000000
    per_task_output_tokens: 2500000
  lease_ttl_ms: 900000
  max_active_runs_per_project: 5
  max_continuation_retries: 3
  poll_interval_ms: 5000
tracker:
  dispatch_states:
    - "ready"
    - "rework"
  kind: "tusker_vault"
  review_states:
    - "review"
  terminal_states:
    - "done"
    - "cancelled"
    - "superseded"
tracker_schema_version: 7
workflow_version: 1
workspace:
  root: "../.tusker-worktrees"
  strategy: "worktree"
---

## Routing

Dispatch only from durable task states `ready` and `rework`. Do not create or use a durable `active` task status. Runtime activity belongs to run leases and attempts.

## Waves

Use `tusker wave create "<title>" <TASK-ID>...` to record a named dispatch/review batch. Wave membership is canonical on the `kind: wave` record under `work/waves/`; task `wave:` fields are generated back-pointers maintained by `tusker reconcile`.

Wave state is derived, not hand-set. A wave is `open` while any member task is not `done`; it becomes `landed` when every member task is `done`, stamped with the latest member `closed_at`. Adding a non-done member reopens the wave. Review the batch boundary with `tusker wave show <W-####>` and filter task views with `tusker list --wave <W-####>`.

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
