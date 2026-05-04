# Prerequisites and setup notes

## Required

### 1) Go

The primary `tusker` CLI is now built and run with Go.

Minimum expectation:

- Go 1.26+ installed and available on `PATH`

### 2) Local filesystem access

You need a path where the vault lives. This can be:

- a normal local Obsidian vault
- a repo-adjacent vault
- a synced folder
- a server-side vault mirrored through Obsidian Headless or another file sync system

## Recommended

### Obsidian Desktop

Recommended core features:

- Properties
- Templates
- Bases
- Backlinks
- Search
- Outgoing links
- File explorer

These give you the human interface without changing the underlying file format.

### Git

Use git for:

- versioning the vault
- syncing text changes between machines
- tracking structured updates to plans, reviews, and docs

If you keep large demo videos in git, consider Git LFS. If you do not want binary churn in git, keep videos outside the repo and store the stable file path or URL in the demo note.

## Optional

### Obsidian CLI

Use Obsidian CLI when you want the desktop app itself to be controlled from the terminal.

Good for:

- live search
- note creation through the app
- screenshots
- automated UI-oriented workflows

Caveat:

- it operates against the running Obsidian desktop app

### Obsidian Headless

Use Obsidian Headless when you want sync or publish automation without the desktop app.

Good for:

- CI-style sync
- server-side vault mirrors
- publish automation
- giving agents access to a synced vault without desktop access

Caveats:

- it is still beta
- it is a separate toolchain from Tusker itself
- do not use desktop Sync and Headless Sync on the same device

## Suggested filesystem layout

```text
repo/
vault/
```

or:

```text
repo/
  .agents/skills/
  .claude/skills/
vault/
```

Keeping the vault adjacent to the repo is often cleaner than nesting the entire vault inside the repo root.

## Suggested operational split

Use the code repo for:

- source code
- public GitHub issues and PRs
- repo-local docs
- short `AGENTS.md`

Use the vault for:

- deeper work tracking
- planning
- review evidence
- demo scripts
- documentation drafts
- session summaries
- internal cross-linking

## Mobile and multi-device use

If you want phone access, the biggest win is that the canonical artifacts are still plain Markdown. That means:

- readable without the helper scripts
- searchable without a database
- portable even if you stop using this skill later

That portability is the whole point.
