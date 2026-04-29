---
schema_version: 2
record_id: ""
id: "{{id}}"
title: "{{title}}"
type: "bug"
status: "intake"
review_state: "none"
work_revision: 0
change_type: "bug"
epic: "[[{{epic}}]]"
epic_record_id: ""
size: "s"
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

## Summary

<!-- One paragraph: observed behavior, expected behavior, user impact. -->

## Repro

1.
2.
3.

Expected:

Observed:

## Environment

- Platform:
- Version:
- Data state:

## Root cause

<!-- Why this happens. Link to the offending code. -->

## Fix

<!-- The change that resolves it. Not the full diff — the idea. -->

## Verification plan

- [ ]
- [ ]

## Evidence

<!-- Required at risk ≥ medium. Test output, before/after screenshots, regression test link. -->

---

## Agent handoff

<!--
Above this line is human-authored spec. Below is agent execution material.
Agents may append; do not rewrite above the line without approval.
-->

Read first:
1.

Primary code anchors:
-

Stop conditions:
- Regression test added

## Work log

- {{date}} — tusker — bug created
