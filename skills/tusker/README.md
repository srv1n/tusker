# Tusker Skill Package

This package teaches coding agents how to track and manage Tusker tasks. It
conforms to the Agent Skills package layout at `skills/tusker/` and uses
progressive disclosure: discovery loads only `name` and `description`,
activation loads the bounded `SKILL.md`, and execution reads only the directly
routed reference or asset needed for the current task.

The primary routes are deliberately terminal:

- `references/TRACK.md` — task creation, lifecycle, proof, and gates;
- `references/KNOWLEDGE.md` — repo knowledge reads and writes;
- `references/SPECS.md` — documentation and spec authoring contracts;
- `references/RUN.md` — deliberate runs, gates, and run watching;
- `references/OPERATE.md` — read-only tracker diagnosis.

Rare routes stay one hop from `SKILL.md`. Current command support comes from
`tusker capabilities --json`, not skill frontmatter.
