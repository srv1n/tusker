# Orchestration

## Plan First

```bash
tusker automation plan <TASK-ID> --json
```

The plan is the stable control-plane answer for daemon pickup, manual dispatch, Obsidian views, and future UI. It should be trusted over ad hoc inspection.

## Plan Shape

```json
{
  "decision": "dispatch",
  "task": "APP-T-0001",
  "lane": "execute",
  "runner": "codex_app_server",
  "workspace": "...",
  "blockers": [],
  "required_reads": ["SKILL.md", "work/tasks/APP-T-0001.md", "knowledge/domains/project/INDEX.md", "knowledge/domains/project/CANON.md"]
}
```

## Runner Rule

Runners return normalized results: summary, patch/diff/artifacts, verification rows, questions, usage, and outcome. Runners should not directly close high/critical tasks.

## Browser-backed ChatGPT Rule

Treat browser-backed ChatGPT as a reasoning worker and artifact producer. It can return patches, analysis, attachments, and suggested verification. Tusker imports the normalized result and local tools verify it. Browser sessions must not become the durable state machine.

## Fanout Rule

Fanout is opt-in. A parent task must explicitly allow child work. Invisible subagents are not acceptable proof. Child work should create child tasks, proposals, or bounded artifacts.
