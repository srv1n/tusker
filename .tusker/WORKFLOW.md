---
agents:
  default: "codex_exec"
  enabled:
    - "codex_exec"
    - "claude-code"
  max_concurrent_agents: 4
  max_concurrent_agents_by_state:
    rework: 1
claude:
  command: "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
codex:
  approval_policy: "never"
  command: "codex exec --json --skip-git-repo-check -"
  max_turns: 30
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
  runner: "codex_exec"
runners:
  claude-code:
    command: "claude -p --output-format stream-json --input-format stream-json --permission-mode bypassPermissions"
    kind: "claude-code"
  codex_exec:
    command: "codex exec --json --skip-git-repo-check -"
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

Dependency edges may be explicit `TASK-ID:hard` or `TASK-ID:soft`; plain `TASK-ID` stays valid and defaults by the dependency's risk. Dependencies on high/critical tasks default hard and require `done`. Dependencies on low/medium tasks default soft and may dispatch once the dependency is in `review` with `proof_status: satisfied`, or `done`. Review gates are for closing, not flowing: soft edges can unblock dispatch, but `tusker close` still requires every dependency to be `done`.

## Runner Exit Classification

| Class | Detection | Runtime result | Continuation |
|---|---|---|---|
| Completion | Runner exits 0 after the tracker leaves `ready`/`rework`, or reaches a human-wait state. | Attempt is `succeeded` or `waiting_for_human`; run releases. | No retry. |
| Declined dispatch | Plan/eligibility gate says no before an attempt is created. | No new attempt; blocker is recorded on the run/plan. | No automatic retry. |
| Turn cap exhausted | Codex exec emits more than `max_turns` `turn.started` events in one attempt. | Tusker kills the process and records attempt outcome `turn_cap_exhausted`. | Distinct from no-progress early exit. |
| Early exit | Runner exits, including exit 0, while tracker state is still active and the per-attempt turn cap was not exhausted. | Attempt is `early_exit`; run queues continuation or parks at `max_continuation_retries`. | Counts against the no-progress cap. |

The same tracker-aware classifier applies on every outcome write path, including daemon status observation and wrapper direct-store recording when a wrapper outlives the daemon.

## Waves

Use `tusker wave create "<title>" <TASK-ID>...` to record a named dispatch/review batch. Wave membership is canonical on the `kind: wave` record under `work/waves/`; task `wave:` fields are generated back-pointers maintained by `tusker reconcile`.

Wave state is derived, not hand-set. A wave is `open` while any member task is not `done`; it becomes `landed` when every member task is `done`, stamped with the latest member `closed_at`. Adding a non-done member reopens the wave. Review the batch boundary with `tusker wave show <W-####>` and filter task views with `tusker list --wave <W-####>`.

## Merge Lane

Do not push or merge directly to the default branch/main from runner worktrees. Use `tusker land <TASK-ID>` to queue task branches into the serialized wave lane; the lane gates the merged staging state, lands green branches into `integration/W-####`, kicks red branches back to `rework`, and lands a completed wave to main as one merge commit.

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

## Test Hygiene

E2E fixtures that spawn long-lived subprocesses must reap the whole process group during cleanup, give intentional hold modes a hard self-expiring timeout, and assert at suite teardown that no marker-matched fixture process survived.

## Completion contract

Satisfy the task proof mode. For `proof_mode=inline`, record concise verification rows with `tusker verify add`; do not create evidence files. For `card`, `artifact`, or `audit`, create only the evidence records required by the task.

When machine work is complete and only human-owned proof or gates remain, run `tusker closeout <task-id> --emit-packet --validate "<command>"`, then stop. When work is ready for review, use `tusker finish <task-id> --request-review` so the task reaches `review` or a branch-safe proposal is created. Attempt handoff alone is not a review request.

## Reviewer contract

If `reviewer.enabled` is true, review tasks may dispatch to `reviewer.runner`. The reviewer must not edit implementation files. Low/medium risks can close after all gates pass. High/critical risks stay in `review` for human acceptance.

## Retry policy

Retry only transient infrastructure failures. Human-directed rework creates a new ready/rework task revision.

Codex exec owns the inner agent loop. Tusker spawns one `codex exec --json` process per attempt, records JSONL `thread.started` and `turn.*` events, enforces budget and `max_turns` as process governors, and uses `codex exec resume <session-id>` only when creating a later continuation attempt. App-server is not part of the normal dispatch path.

`tusker redrive <TASK-ID>` is the operator reset for parked or terminal runtime rows. It starts a fresh counting window by resetting `runs.attempt_count` to `0`, clearing cooldown/active execution state, resetting the budget window, and queueing the run for the daemon while preserving prior attempts, turns, sessions, and event history. Each redrive records who/why in runtime audit state; if the task loops again, it must hit the normal caps again and require another explicit redrive.

## Human override policy

Humans may edit task contracts. Runtime state belongs to the daemon/runtime store.
