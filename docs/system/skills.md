---
title: "Skills"
subject: skills
part_of: overview
status: canonical
read_when: "Choosing the operator skill source, installed provenance, or project canon route."
skip_when: "You need product behavior, task proof, or runtime storage details."
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

## External methods

Tusker owns task capture, decision links, task contracts, proof, and review.
It composes an external design method only for unresolved choices. On this host,
`grilling` is available through the skill catalog; it returns settled decisions
or open questions. A supplied adequate specification skips that discussion and
goes straight to preserved spec and decision links plus Tusker task conversion.

Writing guidance is optional. The pinned references are pstack
[`technical-writing`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/technical-writing/SKILL.md)
and [`unslop`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/unslop/SKILL.md),
both at `7366ac128bdf95f45e6734f412b49a4031800169` under the repository's MIT
license. Neither is installed in this host catalog. The Codex `skill-installer`
supports a GitHub skill path, but do not install it automatically: it is
Cursor-oriented and a user must choose the installation. To install a pinned
copy, use its documented GitHub-URL option; to update, choose a new reviewed
commit and repeat that explicit install. Keep the source URL and commit here;
do not copy either external rule catalog into Tusker.

When an optional method is missing, say so. Apply Tusker's local prose rule:
lead with the outcome, name the actor, retain exact commands, identifiers,
permission boundaries, and uncertainty, then state the expected result and
failure path. No skill changes permissions or configured work/review levels.

Documentation follows the same current-only placement rule: current chapters
in `docs/system/` (per domain under `docs/system/domains/<domain>/`), change
specifications in `docs/system/proposals/`, and product decisions in
`docs/system/decisions/`. `.tusker/` holds tracker state and thin pointers
only; legacy `.tusker/specs/` paths still resolve during transition.
`docs/system/INDEX.md` and `docs/system/graph.json` are generated views;
update their source documents and run `tusker docs map` rather than editing
those files.

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

The source package is authoritative. `tusker skill sync --repo . --source
<canonical-tusker-checkout>` refreshes managed repository copies only when the
operator asks; user-modified installed copies are preserved, never overwritten
by a refresh. `tusker init` preserves existing user-managed external skills;
it does not refresh active or global installations. `tusker init` and skill
refresh never migrate documents implicitly.

## Code sources

- `skills/tusker/`
- `cmd/tusker/skill_*.go`
- `cmd/tusker/install.go`
- `.tusker/SKILL.md`
