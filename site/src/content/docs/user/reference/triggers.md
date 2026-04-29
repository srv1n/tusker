---
title: "Triggers"
description: "When to invoke the Tusker skill, and when to skip it."
tusker:
  audience: "user"
  publish_path: "user/reference/triggers"
  publish_section_title: "Reference"
  route: "/user/reference/triggers/"
  source_kind: "repo_doc"
  source_path: "skill/references/TRIGGERS.md"
  summary: "When to invoke the Tusker skill, and when to skip it."
  tags:
    - "reference"
  updated: "2026-04-28"
---

# Triggers

When to invoke the Tusker skill, and when to skip it.

## Invoke when the user says

- "log this", "track this", "file this", "capture this"
- "what's next", "what's ready", "resume", "pick up where we left off"
- "plan", "backlog", "scope this"
- "epic", "story", "bug", "doc"
- "spec", "RFC", "design doc", "PRD"
- "document this project", "write docs as we build", "project docs", "docs site"
- "user guide", "support doc", "release note", "runbook", "knowledge base"
- "ship this", "mark done", "close this", "attest"
- "tusker"

## Invoke when the work pattern is

- Multi-session work that needs a persistent home (anything that won't fit one conversation)
- Deliverables with a review/attestation step (features, refactors, migrations)
- Follow-ups discovered during execution that shouldn't get lost
- A PRD or RFC that needs to become executable stories
- Project documentation should evolve with implementation instead of living in a side chat
- A repo needs canonical docs, companion docs, user/support/release docs, or a static docs site
- Agent queue operations (pickup, claim, release)

## Skip when

- A one-off code edit with no tracking value
- Direct GitHub issue/PR comments (repo contract handles that layer)
- Pure research that won't produce a deliverable
- Greenfield project with no epics yet *and* no intent to plan work
- The user is asking about Tusker itself — answer from the skill, don't invoke the CLI

## Trigger tests

Invoke:
- "Can you use this to log stuff in?" → yes
- "File a follow-up: we need to fix the cache key" → yes, `discover` or `new-story`
- "What stories are open for PLC?" → yes, `list --epic PLC`
- "I have an RFC, turn it into stories" → yes, create D-note + stories
- "Start this new app and keep docs updated as we build" → yes, create/choose epic, canon doc, and story stack
- "Create a user guide for this feature" → yes, `new-doc --audience user`
- "Publish the docs site" → yes, use `docs export/dev/build`
- "Close HIT-S-0001, I've merged it" → yes, `set-status done` + attestation

Don't invoke:
- "What does this function do?" → no, unrelated code question
- "Write me a regex for emails" → no, pure coding task
- "Explain how the vault works" → answer from skill content, don't run CLI
