---
schema_version: 2
record_id: ""
id: "{{id}}"
title: "{{title}}"
type: "story"
status: "intake"
review_state: "none"
work_revision: 0
change_type: "feature"
epic: "[[{{epic}}]]"
epic_record_id: ""
size: "m"
risk: "medium"
priority: "p2"
delegation: "execute"
surfaces: []
assignee: ""
requester: ""
ai_assistance: "heavy"
ai_tools: []
ai_session_log: ""
attested_by: ""
attested_at: ""
attested_role: ""
signoff_by: ""
signoff_at: ""
dod_code_complete: false
dod_user_verified: false
created: "{{date}}"
updated: "{{date}}"
due: ""
started: ""
review_requested_at: ""
verified_by: ""
verified_at: ""
reviewed_by: ""
reviewed_at: ""
completed: ""
cancelled_at: ""
blocked_since: ""
prs: []
related: []
related_record_ids: []
blocks: []
blocks_record_ids: []
blocked_by: []
blocked_by_record_ids: []
transitions: []
tags: []
---

# {{id}} · {{title}}

## Problem

<!-- What is broken, missing, or unclear. Who is the user. What do they need. -->

## Scope and non-goals

In scope:

-

Out of scope:

-

Non-goals:

-

## Acceptance criteria

- [ ]
- [ ]

## Canon

<!--
Required at risk >= medium.
List the exact canon the agent must read before coding, in read order.
Use repo paths, note links, RFC names, and frozen decisions. Do not paste the whole spec.
-->

- Epic: [[{{epic}}]]
-

## Code anchors

<!--
Required at risk >= medium.
Concrete files, symbols, or line ranges the agent should start from.
If an anchor is unknown, say why; do not leave this as hand-wavy repo fog.
-->

-

## Plan

<!--
The approach, not the spec.
Ordered, mergeable steps. If this reads like "do everything," split the story.
-->

1.
2.
3.

## Considered and rejected

<!-- Required at risk ≥ high. What you considered, why you rejected it. -->

- **<alternative>** — <one sentence why not>

## Decision

<!-- Required at risk ≥ high. The chosen path and the trade-off you accepted. -->

## Verification plan

<!-- How you will prove acceptance criteria. Tests, manual steps, benchmarks. -->

-

## Evidence

<!--
Required at risk ≥ medium. Append artifacts after execution, not plans.
Feature + UI surface + risk ≥ medium → must include a demo asset (video/gif/screenshot).
-->

## Kill list

<!-- Required at risk = critical. What gets deleted after this ships. -->

-

## Rollout

<!-- Required at risk ≥ high. Flag strategy, staged rollout, rollback plan. -->

---

## Agent handoff

<!--
Above this line is human-authored spec. Below is agent execution material.
Agents may append; do not rewrite above the line without approval.
-->

Read first:
1.
2.

Do not change:
-

Implement in order:
1.
2.

Primary code anchors:
-

Stop conditions:
-

## Work log

- {{date}} — tusker — story created
