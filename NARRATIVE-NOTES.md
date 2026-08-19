# Tusker — narrative notes

Drafted 2026-08-18 from Sarav's product-session interview. These are story and motivation notes, not the internal fact sheet.

## The itch

Coding-agent work kept collapsing back into chat. Sarav would spend time shaping a specification with Codex or Claude, then lose the durable reasoning as the conversation moved on. Agents produced plenty of words, but those words were difficult to scan and often landed as scattered Markdown in arbitrary parts of the repository. The question “what is the state of the code?” became harder, not easier.

The original dream combined an OpenAI Symphony-style orchestration blueprint with Obsidian and Bases: keep specifications canonical, readable, linked, and close to the repository, then use a small Go binary to impose enough structure for agents. Tusker eventually outgrew Obsidian and acquired its own lightweight client.

## The product thesis

The human should spend serious time on the specification, acceptance criteria, and engineering constraints before execution. Agents should receive the exact relevant knowledge rather than ingesting a traditional documentation tree or rediscovering decisions in every session. After execution, the human should review proof—before/after UI artifacts, performance comparisons, verification evidence—not another wall of agent prose.

Documentation is therefore part of the agent interface. A document carries routing information about when it should and should not be read, connects through backlinks, and remains pleasant for a human to browse and edit. Tickets add a constrained, parseable structure that supports epics, waves, status, gates, and proof without becoming arbitrary Markdown.

## Who Sarav has in mind

The center is a solo builder, or a very small technical team, using multiple coding agents on a real Git repository. The collaboration model is intentionally Git-shaped: commit, push, and pull. It is not a real-time shared workspace and is not trying to become general-purpose project management.

## What feels worth defending

- Documentation treated as an agent skill rather than a passive human manual.
- Canonical knowledge that remains easy for a human to read, navigate, and edit at any point.
- A ticket specification refined for both machine enforcement and human comprehension.

## The uncomfortable part

Tusker went through multiple attempts at orchestrating agent harnesses before landing on ACP—the direction Sarav believes it should have taken earlier. The protocol has not yet been exercised enough in daily work. That is the next several weeks of dogfooding, and the reason the product is not ready for other users despite the task, wave, knowledge, and editing foundations working well.

## Lines deliberately not crossed

Tusker is not aiming at hosted canonical state, real-time collaboration, large organizations, non-code task management, universal harness compatibility, ungated autonomy, a web SaaS, or a mobile client. Those exclusions are likely long-term: the point is a local, Git-tracked, agent-native work system for a builder or tiny team, not another cloud project-management platform.
