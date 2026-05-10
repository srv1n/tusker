# Triggers

When to invoke the Tusker skill, and when to skip it.

## Invoke when the user says

- "log this", "track this", "file this", "capture this"
- "what's next", "what's ready", "resume", "pick up where we left off"
- "plan", "backlog", "scope this"
- "epic", "task", "bug", "doc"
- "spec", "RFC", "design doc", "PRD"
- "document this project", "write docs as we build", "project docs", "docs site"
- "user guide", "support doc", "release note", "runbook", "knowledge base"
- "ship this", "mark done", "close this"
- "tusker"

## Invoke when the work pattern is

- Multi-session work needs a persistent home.
- Deliverables need review, verification, or evidence.
- Follow-ups discovered during execution should not get lost.
- A PRD/RFC/spec needs to become executable tasks.
- Project documentation should evolve with implementation.
- A repo needs canonical docs, companion docs, user/support/release docs, or a static docs site.
- Agent queue operations need durable task state.

## Skip when

- The user wants a one-off code edit with no tracking value.
- The user is asking for a direct explanation of code.
- The work is pure research with no deliverable.
- There is no intent to plan, track, publish docs, or close work.
- The user asks about Tusker itself; answer from the skill content instead of running the CLI.

## Trigger tests

Invoke:

- "Can you use this to log stuff in?" -> yes.
- "File a follow-up: we need to fix the cache key" -> yes, create a task.
- "What tasks are open for PLC?" -> yes, `list --epic PLC --type task --open`.
- "I have an RFC, turn it into tasks" -> yes, create canon/docs and a task stack.
- "Start this new app and keep docs updated as we build" -> yes, create/choose epic, docs, and tasks with `doc_nodes`.
- "Create a user guide for this feature" -> yes, create a V5 doc.
- "Publish the docs site" -> yes, use `docs export/dev/build`.
- "Close HIT-T-0001, I've merged it" -> yes, evidence/docs check/verify/close.

Do not invoke:

- "What does this function do?" -> no, unrelated code question.
- "Write me a regex for emails" -> no, pure one-off task.
- "Explain how the vault works" -> answer from skill content.
