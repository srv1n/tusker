# Tusker Skill Package

This package teaches coding agents how to operate Tusker in any repository. It
conforms to the Agent Skills package layout at `skills/tusker/` and uses
progressive disclosure: discovery loads only `name` and `description`,
activation loads the bounded `SKILL.md`, and execution reads only the directly
routed reference or asset needed for the current task.

The primary routes are deliberately terminal:

- `references/PLAN.md` — requirements, delivery review/import, Start;
- `references/WORK.md` — interactive/dispatched work, review, proof, human wait;
- `references/OPERATE.md` — daemon, waves, integration, fleet repair, recovery.

Rare routes stay one hop from `SKILL.md`. Legacy reference filenames are small
non-normative redirects for old packets. Compatibility lives in
`assets/compatibility.yaml` and `tusker capabilities --json`, never in skill
frontmatter.
