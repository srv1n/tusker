# Tusker Skill Package

This package teaches coding agents how to track and manage Tusker tasks. It
conforms to the Agent Skills package layout at `skills/tusker/` and uses
progressive disclosure: discovery loads only `name` and `description`,
activation loads the bounded `SKILL.md`, and execution reads only the directly
routed reference or asset needed for the current task.

The primary routes are deliberately terminal:

- `references/PLAN.md` — requirements and task creation;
- `references/WORK.md` — lifecycle, proof, gates, and human wait;
- `references/OPERATE.md` — read-only tracker diagnosis.

Rare routes stay one hop from `SKILL.md`. Legacy reference filenames are small
non-normative redirects for old packets. Compatibility lives in
`assets/compatibility.yaml` and `tusker capabilities --json`, never in skill
frontmatter.
