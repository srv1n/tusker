---
subject: ui-reset
keywords: [serve ui, declutter, progressive disclosure, project rail, inbox, work, wave graph, status vocabulary]
part_of: work-area-redesign
describes: [internal/serve/ui/src/routes/__root.tsx, internal/serve/ui/src/components/Sidebar.tsx, internal/serve/ui/src/features/workbench]
status: canonical
created: 2026-09-23
last_verified:
read_when: "Changing Serve UI navigation, the Work area (waves, wave graph, board, task drawer), status labels, or deciding what a screen shows by default."
skip_when: "Changing task execution, daemon behavior, document saving, or API contracts."
sources: ["https://claude.ai/artifact/JHjC5i6gx9H65kYg6C6sT8", full-height-workspace.md]
decisions_locked: true
---

# UI reset: three places, one status language, details on demand

The control room answers three questions per project: what needs me (Inbox), where is the plan (Work), what did we decide (Docs). Everything else is one level down. Mockups: the design canvas linked in `sources`.

## 1. Principles

- One question per screen.
- Say each fact once. If a column, chip or header states it, no other text repeats it.
- Show the next action; keep routing, provenance, logs and raw plans one level down.
- Empty is quiet: hide empty groups, one-line empty states, and hide a feature the API cannot back instead of explaining the gap.
- Stable places: nav items and project order never move on their own.

## 2. Navigation rail

Keeps the flat project rail from full-height-workspace §3 and narrows it.

- Every visible project is listed flat with its icon. The active project expands in place to Inbox, Work, Docs. One project expanded at a time; any destination is one click.
- Order is pins and manual order only. Selecting a project never reorders the list.
- A project row carries at most one dot: red = needs you; green = work finished since the last visit (only where an existing signal supports it).
- Only the active project's Inbox shows a count badge (needs-you items only).
- Bottom: runner status row (dot, short label, links to Diagnostics; turns red when the invariant circuit is open) and app Settings. ⌘\ collapses the rail to a 56px icon strip; the active project keeps its three section icons stacked under it.
- Removed: per-project Settings and More rows, the app-level More, Minimize, the notification bell, the full-width circuit banner (its fault moves to the status row). Project settings, pin and refresh move to a hover "…" on the project row.

## 3. Information architecture

| Place | Routes | Content |
| --- | --- | --- |
| Inbox | `/p/$projectId/` | Needs you, Running, Waiting on agents, Done recently; toggle this project / all projects |
| Work | `/waves`, `/waves/$waveId`, `/tasks`, `/tasks/$taskId`, `/runs/$taskId` | Waves and Board as two views of one place |
| Docs | `/docs`, `/knowledge*` | Specs, Decisions, Knowledge; graph is a view option |
| Settings | `/settings`, `/p/$projectId/settings` | Diagnostics becomes Runtime; Trains hidden until the API supplies promotion data |

## 4. Work

- One 52px toolbar per screen: title or breadcrumb left, actions right, Waves | Board segmented control. No eyebrow labels, no subtitles.
- Wave detail is graph-first and full-bleed: compact header (title, one-line goal, status chip, "N of M accepted", progress), at most one collapsed callout, view switch Graph (default) | Tasks | Results. The graph fills the remaining viewport; nodes needing the human are marked; earlier-wave dependencies collapse into a light "Earlier waves" lane. Clicking a node opens the task drawer.
- Waves list: 44px rows (ID, title, progress, count, chip only when unusual), grouped Running / Up next / Done, empty groups hidden, one auto-run line instead of per-row authorization text.
- Board: cards show ID and title only; "Needs you" marker; Planned and Done collapsed by default; columns scroll independently; no Topics bar.
- Task drawer: title, status phrase, decision box, goal, then collapsed Contract, Routing, Proof, Activity with one-line summaries.

## 5. Disclosure layers

| Layer | Shows | Never shows |
| --- | --- | --- |
| 0 Glance | Project dots, Inbox badge, status dot | Any other count |
| 1 List row | ID, title, progress, one chip when unusual | Descriptions, repeated state sentences, dependency IDs |
| 2 Detail | Title, goal, status, progress, one callout, primary action | Routing forms, provenance, raw plans |
| 3 Sections | Contract, Routing, Proof, Activity, collapsed with summaries | Sections open by default |
| 4 Raw | Source, event log, JSON behind View source | Anything above |

## 6. Status vocabulary

| Show | Replaces |
| --- | --- |
| Planned | Backlog, Planned |
| Waiting | Queued behind dependencies, dep-blocked |
| Ready | Ready to start |
| Running | Executing, leased, in progress |
| Review | Reviewing, awaiting review, checking verification |
| Done / Failed | Accepted, landed / terminal run failure |
| Auto-run on / off | Armed, Not armed, authorization disarmed, Not authorized |

## 7. Copy rules

- Sentence case; secondary row text at most eight words.
- IDs in muted mono, first column or drawer header only.
- Empty state: one line, at most one link.

## 8. Rollout

1. Rail and Work area (this change).
2. Inbox screen (replaces Today; absorbs the needs popover).
3. Docs merge (Knowledge into Docs, tree + reader, Info on demand).
4. Settings cleanup (provenance on hover, remove "coming soon" rows, automation plan behind View plan, Diagnostics as Runtime).
