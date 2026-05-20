---
title: "Skill Bundle"
description: "Overview of the installable Tusker skill bundle and how operators use it."
tusker:
  audience: "user"
  publish_path: "user/start-here/skill-bundle"
  route: "/user/start-here/skill-bundle/"
  source_kind: "repo_doc"
  source_path: "skill/README.md"
  summary: "Overview of the installable Tusker skill bundle and how operators use it."
  tags:
    - "start-here"
    - "user"
  updated: "2026-05-18"
---

# Tusker Skill Bundle

This directory is the installable skill payload copied by the install script and refreshed by `tusker install` and `tusker update`.

What lives here:

- `SKILL.md` is the entrypoint agents should load first
- `references/` holds the operational docs
- `docs/` holds deeper implementation notes
- `agents/` holds marketplace metadata
- `assets/` holds templates, snippets, icons, and repo-contract scaffolds

If you're using the installed skill, the CLI is the execution surface:

```sh
tusker --help
tusker init
tusker init --vault /path/to/vault --yes
tusker search "duplicate clue" --type task
tusker update
```

Use `tusker search` as the default tracker lookup before broad filesystem search. It searches first-party task, epic, and doc notes while skipping attachments, generated indexes, runtime state, and raw logs.

For non-trivial implementation, bugs, tests, or refactors, start with `SKILL.md`, then read `references/ENGINEERING_DISCIPLINE.md` for the behavior-first testing, diagnosis, slicing, and surgical-diff checklist.

For documentation work, start with `SKILL.md`, then read `references/DOCS_PUBLICATION.md`. Published sites expose `site/public/canon-manifest.json` and `site/public/llms.txt` so agents can find current docs without spelunking stale files. Canon is explicit: `approved` is current, `draft` needs checking, and `deprecated`/`historical` is archaeology.

Run `tusker update` after pulling or rebuilding Tusker. It refreshes the installed CLI link and the installed Tusker skill bundle from the currently running binary. `tusker install` without `--repo`, `make install`, and `tusker update` refresh existing user skill installs by replacing the installed payload directory from the embedded bundle, so stale files are removed. `tusker install --repo <path>` installs repo-local skill bundles by default; add `--refresh-existing-user-skills` or explicit user flags only when user-level writes are intended.

If you're hacking on Tusker itself, go back to the repo root `README.md`.
