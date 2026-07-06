# Tusker Serve — settings & configuration addendum

Follow-up brief to `tusker-serve-ux-packet.md`, written after the owner
reviewed the first round of frames (2026-07-06). The frames are approved in
direction; this note carries three corrections and the settings surfaces the
first round didn't cover. Same visual system, same restraint.

## 1. Corrections to the reviewed frames

1. **Draft-contract task view.** A task whose contract is unwritten can never
   be status `Ready`, and "Start run" can never be live for it. Keep the
   affordance but make it honest: status shows `Backlog`, the primary action
   reads **Draft contract** (it will hand the epic brief to a planning
   agent), and "Start run" appears only once acceptance criteria exist.
2. **Board columns.** "In progress" is not a stored status — it's a ready
   task that currently has an active run. Treat the column as a live
   projection (a task moves there when a run starts, back when it ends), and
   render rework as a badge on the card in the Ready column, not a fifth
   column. Durable statuses are backlog / ready / review / done.
3. **Provenance on every settings row.** Each value shows a small source
   chip: `default`, `global`, `project`, or `local` (machine-only). Editing
   a row never edits a shared committed file — app-level edits save to the
   user's global config, project-level edits to a machine-local override —
   so the chip also tells the operator whether teammates see this value.
   Include a quiet affordance on overridden rows: "reset to inherited."

## 2. New settings surfaces to design

### 2.1 Runner profiles (app-level, projects can override)

A profile is a named bundle the daemon uses to launch an agent. The list
view shows each profile as a card: name, harness (codex / claude-code),
model, reasoning effort, permission preset, and whether it may spawn
subagents (with a max). Ships with built-ins (e.g. `default`, `docs-fast`,
`review-frontier`, `guarded-yolo`); built-ins are editable-by-copy, not
destructively. Detail view is a short form — harness, model (dropdown per
harness), effort, permission preset, subagent policy.

### 2.2 Permission presets and the denylist

Three presets, presented as a radio with one-line consequences:

- **Full access** — no sandbox, no approvals; the operator's usual mode.
- **Guarded full access** — full filesystem and network, but a denylist
  blocks destructive commands. Show the denylist as an editable list of
  patterns with the built-ins visible and non-deletable (force-push,
  recursive delete outside the workspace, hard reset, credential-file
  writes); operator entries append below.
- **Workspace-only** — writes confined to the workspace; a separate toggle
  for network access (on lets agents fetch docs and search the web).

This screen matters: it's where the operator decides how much rope every
agent gets. Copy should be plain about consequences, never scary.

### 2.3 Routing rules (project-level)

An ordered list — "for this kind of task, use this profile." Each rule row:
match criteria (epic, risk, size, keywords) → profile name, drag-to-reorder,
first match wins. A muted footer row shows the fallthrough: lane mapping →
project default. Every run's detail view names which rule (or fallback)
picked its profile, so routing is never a mystery.

### 2.4 Workspace lifecycle (project-level)

For projects that run parallel worktrees. Three fields, each with helper
copy:

- **Setup script** — runs once when a workspace is created (install deps,
  copy env). Multiline command editor; failures fail the dispatch and show
  the output tail.
- **Files to copy** — glob list for local-only files new workspaces need
  (`.env*` default). One pattern per line.
- **Archive script** — cleanup before a workspace is removed.

Plus a read-only line: the port range each workspace gets (e.g. "each
workspace reserves 10 ports from a base"), so dev servers never collide.

### 2.5 Landing & parallelism (project-level)

Extends the existing Details card: concurrent runs (already in the frames),
auto-merge on green (already there), plus **conflict assist** (off by
default — "let an agent attempt conflict resolution before asking you") and
**after landing** (keep branch / delete branch). Overlapping-work protection
(file claims) is automatic; if a task is queued because its files overlap a
running task, the queue reason appears on the task card, not in settings.

### 2.6 Notifications (app-level)

The two toggles in the frames (human gate, stale run) plus delivery method:
macOS notification, in-app only, or both. Nothing else — no digest
schedules, no email.

## 3. Priority

2.1–2.3 gate the first end-to-end configuration story and should be the next
frames. 2.4–2.5 can follow; their backend lands later. 2.6 is a single small
card. Everything here writes through the same settings API with the same
provenance chips as §1.3.
