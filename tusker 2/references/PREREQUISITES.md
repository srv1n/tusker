# Prerequisites and setup notes

## Required

### 1) Tusker CLI

The primary execution surface is the `tusker` CLI. It must be available on `PATH` or run from the repository build output.

### 2) Local filesystem access

Tusker works against a repo-local or repo-adjacent markdown vault. The vault can be:

- inside the repository;
- adjacent to the repository;
- in a synced folder;
- mounted into the repository for local work.

The markdown format is the contract. No specific editor is required.

### 3) Git recommended

Use git for:

- versioning task/proof/docs changes;
- comparing state fingerprints;
- syncing text changes between machines;
- reviewing agent edits.

## Suggested filesystem layouts

Repo-contained vault:

```text
repo/
  tusker/
  .agents/skills/tusker/
  .claude/skills/tusker/
```

Repo-adjacent vault:

```text
repo/
vault/
```

Repo-contained is simpler for agents. Repo-adjacent can be cleaner when the vault has private planning material.

## Operational split

Use the code repo for:

- source code;
- public issues and PRs;
- repo-local docs;
- short `AGENTS.md`.

Use the Tusker vault for:

- work tracking;
- plans and task contracts;
- review evidence;
- gates and decisions;
- documentation drafts;
- session summaries;
- internal cross-linking.

## Optional editor/UI

Any markdown editor can view the vault. Editor-specific views, plugins, bases, templates, or sync tools are optional and outside the root skill contract.

Do not make agent behavior depend on editor UI features. Agents should use the CLI and plain markdown paths.

## Binary artifacts

Keep large demo videos, screenshots, and traces out of git unless the project explicitly wants them. Prefer stable file paths or URLs in evidence cards.

Raw logs and debug output belong in `.tusker/scratch/<TASK-ID>/`, not canonical evidence.
