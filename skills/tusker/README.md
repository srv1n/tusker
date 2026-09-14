# Tusker Skill Package

This package teaches coding agents how to track and manage Tusker tasks. It
conforms to the Agent Skills package layout at `skills/tusker/` and uses
progressive disclosure: discovery loads only `name` and `description`,
activation loads the bounded `SKILL.md`, and execution reads only the directly
routed reference or asset needed for the current task.

Start with the route for the current stage:

- `references/TRACK.md` — task creation, lifecycle, proof, and gates;
- `references/KNOWLEDGE.md` — finding and explaining repo knowledge;
- `references/SPECS.md` — documentation and spec authoring contracts;
- `references/RUN.md` — deliberate runs, gates, and run watching;
- `references/OPERATE.md` — read-only tracker diagnosis.

Follow conditional references when the task needs them: task authoring reads
`HANDOFF.md`, while a status lookup can finish in the entrypoint. Current
command support comes from `tusker capabilities --json`, not skill frontmatter.

Edit canonical skill text in this package. Repository and user installations
are materializations, refreshed through the normal install/sync flow.
