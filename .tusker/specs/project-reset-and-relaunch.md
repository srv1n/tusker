---
title: "Project reset and relaunch"
subject: project-reset-and-relaunch
keywords: [reset, relaunch, purge, preserve specs]
part_of: storage-and-runtime
describes: [cmd/tusker/project_reset.go, cmd/tusker/spec_snapshot.go]
status: canonical
created: 2026-08-22
last_verified:
read_when: "You need to discard stale repo-local Tusker work and start a clean V7 tracker."
skip_when: "You need to remove only selected records or repair an existing tracker."
---

# Project reset and relaunch

`tusker reset --yes` is the destructive, repo-local recovery path for a
project whose tracker state no longer matches the current Tusker API. It plans
and applies the same known-state deletion boundary as `tusker purge`, then
runs a clean V7 `init` with vault-only wiring.

The reset removes tickets, epics, gates, evidence, attempts, events, generated
indexes, scratch, repo-local generated Tusker skills, managed pointers, and
matching workspace registrations. It never deletes source files or ordinary
documentation. Files under `.tusker/specs/**` are snapshotted, the tracker is
removed, and those specs are restored into the fresh vault. Existing
`docs/specs/**` content is outside the purge boundary and remains untouched.

Use `--dry-run` to inspect the deletion plan. `--repo <path>` targets another
checkout without changing the command's normal current-project behavior.
`tusker relaunch` is an alias for the same operation.
