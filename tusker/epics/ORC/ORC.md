---
schema: "tusker.epic/v5"
id: "ORC"
title: "Trustworthy Orchestration"
type: "epic"
status: "active"
owner: "sarav"
summary: "Symphony-aligned daemon work: honest isolation, safe policy, continuation, evidence, and operator visibility."
created: "2026-04-28"
updated: "2026-04-29"
tags:
  - "symphony"
  - "orchestration"
  - "trustworthy-throughput"
---

# ORC - Trustworthy Orchestration

## Thesis

ORC tracks task-native runtime orchestration work on top of the V5 markdown tracker.

## Scope

Keep durable task truth in markdown and keep runtime execution state outside canonical notes.

## Success metrics

- V5 tasks are the only executable work items.
- Runtime dispatch sees task notes.
- Evidence and docs gates remain visible in the vault.

## Canon

- docs/specs/00-product-modes.md
- docs/specs/01-vault-tracker.md
- docs/specs/03-daemon-and-registry.md

## Problem

Tusker already has the outlines of Symphony-style orchestration: a vault tracker, WORKFLOW.md, a daemon, workspaces, runners, runtime storage, and review discipline. The problem is that several advertised orchestration promises are not yet fully enforced. The daemon can prepare an isolated workspace and still launch the runner in the repo root. Workflow policy fields can exist without affecting behavior. The workflow body can read like a contract while the daemon uses a hard-coded prompt.

That is dangerous because it creates false confidence. This epic makes orchestration honest first, then adopts the Symphony worker model in Tusker's own lane: markdown-native, risk-aware, auditable, Obsidian-readable, and local-first.

## Scope and non-goals

In scope:

- Enforce workspace isolation and runner cwd.
- Move Codex trust policy into WORKFLOW.md.
- Make WORKFLOW.md body a strict prompt template and project runbook.
- Add attempt/turn runtime records for continuation and resume.
- Normalize Codex app-server events and usage telemetry.
- Add event-based stall detection and failure-class retry policy.
- Implement same-thread continuation turns.
- Add supervisor policy for continue/resume/fork/new-branch/stop decisions.
- Add dependency, priority, and risk-aware dispatch.
- Generate review packets and evidence summaries.
- Add workpad/follow-up runbooks and optional tool/MCP extension bridge.
- Publish operator docs and Obsidian views for run/session state.

Out of scope:

- Linear live adapter.
- Cloud dashboard.
- Phoenix-style dashboard or complex web UI.
- SSH worker pool.
- Multi-tenant control plane.
- Full Claude Code feature parity with Codex app-server.

Non-goals:

- Raw throughput at the cost of auditability.
- Auto-approve high-risk work by default.
- Store leases, retry timers, pids, or failure classes in note frontmatter.

## Success criteria

- [ ] Daemon-run agents always execute inside the prepared workspace cwd.
- [ ] Workflow policy controls Codex approval/sandbox, prompt body, caps, hooks, retry, and continuation where implemented.
- [ ] Operators can inspect attempts, turns, sessions, event tails, token totals, failure class, workspace, and resume status.
- [ ] Continuation runs on the same Codex thread within one attempt and stops at tracker/human gates.
- [ ] Dispatch respects dependencies, priority, and risk-tier trust profiles.
- [ ] Successful attempts generate review packets and evidence before review packet.
- [ ] Documentation and Obsidian views explain how to inspect, resume, retry, interrupt, and review runs.

## Design

Canonical design lives in docs/specs/08-symphony-alignment-and-orchestration-roadmap.md.

Locked decisions:

| Decision | Canon |
|---|---|
| Product lane | Tusker optimizes trustworthy unattended throughput, not raw agent throughput. |
| Tracker | Vault markdown remains the source of truth for task intent and human durable state. |
| Runtime | SQLite stores leases, attempts, turns, sessions, retry timers, telemetry, and failure classes. |
| Workflow | WORKFLOW.md frontmatter is machine policy; body is the agent runbook/prompt template. |
| Workspace | Runner cwd must equal prepared workspace path. Repo root is metadata only. |
| Attempt model | One attempt is one worker session; one attempt contains many turns. |
| Supervisor | Daemon owns continue/resume/fork/new-branch/stop decisions; model auditor is optional and policy-bound. |
| Branching | Separate branch can stay inside one ticket when the work is still in scope. |
| Writer split | Daemon writes narrow durable transitions; agent writes rich workpad/evidence; human writes verdicts. |
| Extensions | Tool/MCP injection is optional task-specific capability, not the core orchestration model. |
| Runner support | Codex and Claude Code both need same-ticket resume semantics where their native session model can prove continuity. |

Implementation sequence:

~~~mermaid
flowchart TD
  S1["ORC-T-0001 workspace cwd"] --> S2["ORC-T-0002 workflow trust policy"]
  S2 --> S3["ORC-T-0003 prompt template"]
  S3 --> S4["ORC-T-0004 review-state canon"]
  S3 --> S5["ORC-T-0005 per-state caps"]
  S1 --> S6["ORC-T-0006 state-change interruption"]
  S4 --> S6
  S4 --> S7["ORC-T-0007 attempts and turns"]
  S5 --> S7
  S6 --> S7
  S7 --> S8["ORC-T-0008 normalized events"]
  S8 --> S9["ORC-T-0009 run/session views"]
  S8 --> S10["ORC-T-0010 stall and failure class"]
  S10 --> S11["ORC-T-0011 continuation"]
  S11 --> S17["ORC-T-0017 supervisor policy"]
  S17 --> S13["ORC-T-0013 review packets"]
  S13 --> S14["ORC-T-0014 workpad runbooks"]
  S14 --> S15["ORC-T-0015 optional extension bridge"]
  S17 --> S15
  S9 --> S16["ORC-T-0016 operator docs"]
  S13 --> S16
~~~

## Task stack

_Open tasks only. Closed/cancelled work is intentionally omitted; use `tusker list --epic ORC --type task --status done` for closed history._

- [[ORC-T-0019]] — Add policy-driven agent reviewer close lane (review, p1, high)
- [[ORC-T-0011]] — Implement same-thread continuation loop (rework, p1, high)
- [[ORC-T-0017]] — Add run supervisor session and branch policy (rework, p1, high)
- [[ORC-T-0012]] — Add dependency priority and risk-aware dispatch (rework, p2, medium)
- [[ORC-T-0014]] — Add workpad and follow-up runbooks to the skill (rework, p2, low)
- [[ORC-T-0016]] — Publish operator docs and Obsidian views for orchestration state (rework, p2, medium)
- [[ORC-T-0006]] — Interrupt or park runs when tracker state becomes ineligible (blocked, p0, high)
- [[ORC-T-0007]] — Add attempt and turn runtime model (blocked, p1, high)
- [[ORC-T-0008]] — Normalize Codex app-server events and usage telemetry (blocked, p1, medium)
- [[ORC-T-0009]] — Add sessions and run viewing commands (blocked, p1, medium)
- [[ORC-T-0010]] — Add event-based stall detection and failure classification (blocked, p1, medium)
- [[ORC-T-0013]] — Generate review packets and evidence summaries (blocked, p2, medium)
- [[ORC-T-0015]] — Add optional tool and MCP extension bridge (blocked, p3, high)

## Open questions

1. Should review remain distinct from requested, or should Tusker collapse to one review state?
2. What exact default trust profiles should ship for low, medium, high, and critical risk?
3. Which template engine should become the strict workflow renderer?
4. Which Codex app-server events are mandatory for v2 event normalization?
5. What concrete thresholds cause same-thread continuation versus new thread fork versus new branch?
6. Which optional tool/MCP extensions are safe enough for the first bridge, after supervisor decision recording is visible?
