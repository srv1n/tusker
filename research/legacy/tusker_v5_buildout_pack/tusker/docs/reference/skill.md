---
schema: tusker.doc/v5
id: reference/skill
title: Skill and AGENTS guidance
type: doc
node: reference/skill
audience: developer
kind: canon
domains: [skill, workflow, docs]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/skill
publish_description: SKILL.md and AGENTS progressive-disclosure guidance.
created: 2026-04-29
updated: 2026-04-29
---

# Skill and AGENTS guidance


Tusker v5 keeps `SKILL.md` short and treats it as a map, not an encyclopedia.

## Rules

- route the agent to the right reference file
- prefer repo-local docs over chat memory
- explain the task contract and close gate
- keep progressive disclosure explicit
- do not reload the entire world up front

## What the skill must teach

- the repo layout
- the primary CLI surface
- the task contract sections
- docs routing through `domains + doc_nodes`
- knowledge delta shape
- close-time docs check/apply/waive flow
- the fact that `status` is canonical in task frontmatter

## What the skill should stop teaching

- legacy story/bug language as the default
- giant frontmatter slabs
- “docs as optional afterthought”
