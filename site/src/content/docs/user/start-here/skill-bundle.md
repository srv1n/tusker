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
  updated: "2026-04-29"
---

# Tusker Skill Bundle

This directory is the installable skill payload copied by the install script and refreshed by `tusker update`.

What lives here:

- `SKILL.md` is the entrypoint agents should load first
- `references/` holds the operational docs
- `docs/` holds deeper implementation notes
- `agents/` holds marketplace metadata
- `assets/` holds templates, bases, snippets, icons, and repo-contract scaffolds

If you're using the installed skill, the CLI is the execution surface:

```sh
tusker --help
tusker init
tusker init --vault /path/to/vault --yes
tusker update
```

For documentation work, start with `SKILL.md`, then read `references/DOCS_PUBLICATION.md`. Published sites expose `site/public/canon-manifest.json` and `site/public/llms.txt` so agents can find current docs without spelunking stale files. Canon is explicit: `approved` is current, `draft` needs checking, and `deprecated`/`historical` is archaeology.

Run `tusker update` after pulling or rebuilding Tusker. It refreshes the installed CLI link and the installed Tusker skill bundle from the currently running binary.

If you're hacking on Tusker itself, go back to the repo root `README.md`.
