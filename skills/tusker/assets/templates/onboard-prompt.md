# Repository onboarding prompt

Inspect the repository before you write tracker records.

## Goal

Prepare current project canon and a small set of new task candidates. Ground
every statement in source, schemas, tests, build files, or current runtime
checks.

## Rules

1. Read user instructions and Git status first.
2. Do not change repository files during the inspection phase.
3. Do not use old tickets, plans, reports, or runtime logs as product authority.
4. Name the exact source path for each product fact.
5. Mark an unknown fact as unknown. Do not invent it.
6. Keep machine runtime state separate from repository state.
7. Do not enable automation or start a daemon.

## Output

Produce:

- a source authority map;
- current build and test commands;
- current platform and runtime limits;
- proposed project canon text;
- a short glossary; and
- new task candidates for real gaps that the source shows.

Use simple technical English. Use short sentences and active voice.
