---
schema: "tusker.epic/v7"
kind: "epic"
id: "RUN"
project: "tusker"
title: "Runner parity and model profiles"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-05T11:32:32Z"
updated_at: "2026-07-12T17:56:10Z"
state_rev: "sha256:0e3dd2ae98d9926dde12d30053ab9b2bf0988b0d46d5367f6a1ca486e37a9382"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or runtime attempt."
  use_when: "Use to triage Codex/Claude runner behavior, profile policy, leases, and runner integration."
  what: "RUN epic for runner parity, model profiles, and future harness boundaries."
---

# RUN · Runner parity and model profiles

## Thesis

Claude Code and Codex as co-equal first-class runners, named profiles for model/effort/subagent policy, and a clean seam for future harnesses (Pi Mono, OpenCode).

## Success criteria

- [ ] Define success criteria.

## Current decision

TBD.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| [[RUN-G-0001]] | human:sarav | [[RUN-T-0001]] | Decide whether to waive broad_test for existing baseline failures or provide a branch/test baseline where go test ./... can pass. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[RUN-T-0001]] | backlog | human:sarav | Accept, waive, or return rework for RUN-G-0001. |
| [[RUN-T-0002]] | review | reviewer | Review evidence and close or return to rework. |
| [[RUN-T-0003]] | backlog | agent | Wait for dependency RUN-T-0002 to reach review with satisfied proof or done. |
| [[RUN-T-0005]] | backlog | agent | Wait for dependency RUN-T-0002 to reach review with satisfied proof or done. |
| [[RUN-T-0006]] | backlog | agent | Wait for dependency RUN-T-0004 to reach done. |
| [[RUN-T-0015]] | backlog | agent | Wait for dependency RUN-T-0011 to reach done. |
| [[RUN-T-0045]] | review | reviewer | Review evidence and close or return to rework. |
| [[RUN-T-0046]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[RUN-T-0004]] | reviewer:agent | 2026-07-06T16:02:51Z |
| [[RUN-T-0007]] | reviewer:agent | 2026-07-06T16:02:51Z |
| [[RUN-T-0008]] | reviewer:agent | 2026-07-06T16:02:52Z |
| [[RUN-T-0009]] | reviewer:agent | 2026-07-06T16:00:38Z |
| [[RUN-T-0010]] | human:sarav | 2026-07-06T17:43:24Z |
| [[RUN-T-0011]] | human:sarav | 2026-07-07T02:43:21Z |
| [[RUN-T-0012]] | reviewer:agent | 2026-07-07T02:49:09Z |
| [[RUN-T-0013]] | reviewer:agent | 2026-07-07T02:49:26Z |
| [[RUN-T-0014]] | human:sarav | 2026-07-07T07:28:00Z |
| [[RUN-T-0016]] | human:sarav | 2026-07-07T07:28:00Z |
| [[RUN-T-0017]] | human:sarav | 2026-07-07T13:32:52Z |
| [[RUN-T-0018]] | human:sarav | 2026-07-07T07:28:00Z |
| [[RUN-T-0021]] | human:sarav | 2026-07-07T10:40:35Z |
| [[RUN-T-0022]] | human:sarav | 2026-07-07T10:40:35Z |
| [[RUN-T-0023]] | human:sarav | 2026-07-07T10:40:35Z |
| [[RUN-T-0024]] | human:sarav | 2026-07-07T10:40:35Z |
| [[RUN-T-0025]] | human:sarav | 2026-07-07T10:40:35Z |
| [[RUN-T-0028]] | human:sarav | 2026-07-07T13:08:12Z |
| [[RUN-T-0029]] | human:sarav | 2026-07-07T13:08:12Z |
| [[RUN-T-0030]] | human:sarav | 2026-07-07T13:06:33Z |
| [[RUN-T-0034]] | human:sarav | 2026-07-08T03:01:47Z |
| [[RUN-T-0035]] | human:sarav | 2026-07-07T16:28:17Z |
| [[RUN-T-0036]] | human:sarav | 2026-07-07T16:56:07Z |
| [[RUN-T-0037]] | human:sarav | 2026-07-08T03:15:42Z |
| [[RUN-T-0038]] | human:sarav | 2026-07-08T05:10:07Z |
| [[RUN-T-0039]] | human:sarav | 2026-07-08T05:10:07Z |
| [[RUN-T-0040]] | human:sarav | 2026-07-08T05:10:07Z |
| [[RUN-T-0041]] | human:sarav | 2026-07-08T05:10:07Z |
| [[RUN-T-0042]] | human:sarav | 2026-07-08T06:14:48Z |
| [[RUN-T-0043]] | reviewer:agent | 2026-07-09T11:02:34Z |
