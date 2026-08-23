---
title: "Skills"
subject: skills
part_of: overview
status: canonical
---

# Skills

The operator skill explains Tusker commands and safety rules. The project skill
routes an agent to repository facts.

## Current sources

- `skills/tusker/SKILL.md` is the operator skill source.
- `.tusker/SKILL.md` is the project skill for this repository.
- `skills/tusker/references/` contains one-hop operator guides.
- `skills/tusker/assets/` contains templates and repo-contract files.

The installed skill can be a copy or a symlink. The source tree remains the
authority for this repository.

## Install and refresh

`make install-user` installs the binary and the Codex and Claude user skills.
`tusker skill sync --repo . --mode symlink --source .` refreshes a repository
skill link. Use copy mode or a bundle when the target cannot follow a symlink.

## Agent reading rule

1. Read `skills/tusker/SKILL.md`.
2. Read one routed reference for the request.
3. Read `.tusker/SKILL.md` only when repository canon is needed.
4. Read the narrowest domain index and canon.

Do not load raw events, attempts, evidence directories, or generated packets
unless the task requires them.

## Validation

Run `tusker skill doctor --strict --json` after a skill change. Run
`tusker validate --json` after a project-skill or canon change.

## Code sources

- `skills/tusker/`
- `cmd/tusker/skill_*.go`
- `cmd/tusker/install.go`
- `.tusker/SKILL.md`
