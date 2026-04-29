---
workflow_version: 1
tracker_schema_version: 2
tracker:
    kind: tusker_vault
    active_states:
        - active
        - rework
        - merging
    review_states:
        - in_review
    terminal_states:
        - done
        - cancelled
agents:
    default: codex
    enabled:
        - sarav
        - codex
        - claude-code
        - gemini
    max_concurrent_agents: 3
    max_concurrent_agents_by_state:
        rework: 1
runtime:
    poll_interval_ms: 5000
    lease_ttl_ms: 900000
    max_active_runs_per_project: 1
workspace:
    root: workspaces
    strategy: worktree
retry:
    max_attempts: 3
    backoff_ms:
        - 30000
        - 120000
        - 600000
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
hooks:
    after_workspace_create: []
    before_workspace_remove: []
---

## Routing

You are working on {{ note.id }} for {{ project.name }}. Dispatch only makes sense because this note is currently {{ note.status }} and the workspace is ready at {{ workspace.path }}.

## Prompt

Use the installed Tusker skill bundle for durable ticket semantics, evidence, and review discipline. Work inside {{ workspace.path }}. Treat {{ repo.root }} as the source repository root for context only unless the task explicitly requires comparing against it.

Item: {{ note.title }}
Record: {{ note.record_id }}
Type: {{ note.type }}
Attempt: {{ attempt.number }}
Workflow: {{ workflow.path }}
Vault: {{ vault.path }}

## Retry policy

Retry only transient infrastructure failures. Human-directed rework creates a new work revision.

## Human override policy

Humans may edit notes directly, but runtime state belongs to the daemon store.
