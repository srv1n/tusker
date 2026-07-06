# Tusker Serve — UX Packet

Audience: a designer (human or design-focused AI session) producing layouts,
component designs, and visual direction for the `tusker serve` web UI.
This document is the design brief. The engineering spec lives at
`docs/specs/10-tusker-serve.md` (SRV-T-0001); where the two disagree on
engineering constraints, the spec wins. Where they disagree on UX intent,
this packet wins.

---

## 1. Product context

Tusker is a repo-local task tracker and agent-orchestration harness. Tasks
are markdown contracts in a git repo. A daemon dispatches coding agents
(Codex, Claude Code) to execute them in isolated git worktrees, a separate
reviewer agent checks the work, and most tasks close without a human ever
looking at code.

`tusker serve` is the control room the single human operator opens to answer
one question: **"what needs me, and what is the machine doing?"** It is
served by the tusker binary itself on localhost, as an embedded single-page
app. There is no auth, no multi-user, no mobile requirement (desktop-first;
usable on a narrow window is a bonus).

The organizing principle is **attention routing, not chronology**. The
operator does not want a feed of everything that happened; they want their
own short to-do list first, live machine status second, and browse/read/edit
tools third.

## 2. The user

One person: the repo owner. Technical, terminal-fluent, checks the UI a few
times a day between other work. Agents outnumber the human ~10:1 in activity
volume. The human's jobs, in priority order:

1. **Unblock**: answer clarify questions, provision credentials, approve
   specs. These are "human gates" — the only things that stop the machine.
2. **Monitor**: see active runs, notice stalls or failures early (a run that
   died silently while showing "running" is the exact failure this UI must
   make impossible).
3. **Read**: task contracts, specs, decisions, evidence — rendered well.
4. **Edit**: fix a spec paragraph, tighten an acceptance criterion, write a
   new doc — without dropping to the terminal or Obsidian. Editing is a
   first-class capability, not an afterthought. The operator wants maximum
   control over the project from this one surface.
5. **Review & close**: skim a review packet, accept or send back with a note.

## 3. Information architecture / routes

| Route | View | Purpose |
|---|---|---|
| `/` | Needs-me queue | The human to-do list. Default view. |
| `/runs` | Runs board | Active + recent agent runs, live. |
| `/runs/:taskId` | Run detail | One run: attempts, events, log tail, tokens. |
| `/work` | Work browser | Epics and tasks, filterable board/table. |
| `/work/:id` | Task detail | Rendered contract + evidence + runs + actions. |
| `/docs` | Library | Specs, decisions, knowledge docs, dashboards. |
| `/docs/*path` | Reader/editor | TipTap surface for any vault/docs markdown. |

Global chrome: a slim left nav (5 items above), a persistent "attention"
badge on Needs-me showing the count of items waiting on the human, and a
global search (tasks, docs, decisions by id/title). No top bar clutter.

## 4. View-by-view

### 4.1 Needs-me queue (`/`)

A ranked card list. Each card is one human gate or human-owned state, with
its **kind** as the primary visual differentiator:

- `clarify` — the agent is blocked on an ambiguous spec. Card shows the
  question, the task id/title, and an inline answer box (answer → agent
  resumes).
- `provision` — the agent needs something only the human can supply
  (credentials, accounts). Card shows the concrete ask ("set S3/R2 keys at
  `<path>`"), a done-checkbox, and a resume button.
- `approve-spec` — a spec awaits approval before expensive work starts.
  Card shows spec title, links into the reader, approve / request-changes
  actions.
- `review` — a finished task explicitly routed to human review. Card shows
  the proof summary (acceptance table with pass/fail), approve / rework
  actions with an optional note.
- `failed` — a run exhausted retries. Card shows last error, retry /
  inspect actions.

Ranking: blocking-the-most-work first (a gate holding up a dependency chain
outranks a lone task), then priority, then age. Each card must be actionable
*on the card* — the operator should clear most items without navigating.

Empty state matters: "Nothing needs you. N runs active, M tasks queued."
with a subtle link to `/runs`. This is the good state; make it feel calm,
not empty.

### 4.2 Runs board (`/runs`)

Live table/cards of runs, active first. Per run: task id + title, runner
(codex / claude badge), model, lane (execute/review), lease state, elapsed
time, token totals, and a **liveness indicator** — time since last event,
turning amber then red as it grows stale. A run showing "running" with a
dead process must be visually alarming within a minute. Recent finished runs
below with outcome chips (succeeded / failed / interrupted / retry-queued).

### 4.3 Run detail (`/runs/:taskId`)

Header: task capsule + run state + actions (interrupt, retry). Body:
attempts timeline (attempt N, outcome, duration, tokens), then a live event
tail (compact, monospace, auto-follow toggle) — filtered protocol events,
not the raw JSONL firehose. Link to the workspace path for terminal
spelunking.

### 4.4 Work browser (`/work`)

Two presentations, one filter bar: a **board** grouped by status
(ready / in progress / review / done) and a **table** (sortable by priority,
risk, epic, updated). Cards/rows show the task capsule: id, title, epic,
priority chip, risk chip, readiness. Epics get header rows with rollup
counts. Filters: epic, status, risk, "has human gate". Clicking anything →
task detail.

### 4.5 Task detail (`/work/:id`)

The contract, rendered beautifully: Intent as prose, the Acceptance table
with per-row proof status (pending / pass / fail as chips), Non-goals,
Verification table with command + result, Evidence cards, Knowledge delta.
Sidebar: frontmatter facts (status, readiness, priority, risk, deps as
links, gates), run history, and actions appropriate to state (promote to
ready, send to rework with note, accept/close, open in editor).

Body sections are editable in place via the TipTap surface (see 4.6);
frontmatter state fields are read-only here, always.

### 4.6 Reader/editor (`/docs`, `/docs/*path`, and task bodies)

This is a headline feature, not a settings page. Requirements:

**Reading first.** Clean typographic scale, comfortable measure (~70ch),
rendered tables, code blocks with syntax highlight, resolved wikilinks
(`[[CLN-D-0001]]` renders as a link with hover preview of the target's
capsule). A left-hand outline (h2/h3) for long specs. Fast — instant
navigation between docs.

**Editing with guardrails.** One toggle (or click-to-edit) switches the
TipTap editor on. Markdown-native: what's saved is markdown, round-tripped
faithfully. Slash-menu for blocks (heading, table, code, list). The
frontmatter is shown as a locked property panel — state fields
(`status`, `state_rev`, `proof_status`, …) are visibly non-editable;
free fields (title, priority where allowed) may be edited via controls,
never as raw YAML.

**Conflict UX (critical).** Saves are CAS-guarded: if an agent changed the
note since the human opened it, the save is rejected. The UI must handle
this gracefully: a non-destructive conflict banner — "This note changed
while you were editing" — with a side-by-side diff (yours vs. theirs) and
choices: take theirs / keep editing on top of theirs / copy my text. Never
silently lose either side.

**Validation inline.** After save, note-level validation runs; errors and
warnings appear as inline annotations (like lint squiggles) plus a summary
strip. An invalid save is rejected with the reasons shown; the vault never
ends up corrupt.

## 5. States the design must cover

For every view: loading (skeletons, not spinners), empty (with a helpful
next action), error (API down — the daemon may not be running: say so and
show the command to start it), and live-update (data changes underneath the
user; updates slide in without stealing focus or scroll).

Specific must-design moments:

1. Needs-me card resolving (answered/approved) — satisfying, quick exit
   animation; count badge decrements.
2. Run going stale → amber → red (liveness indicator states).
3. CAS conflict banner + diff view in the editor.
4. Validation errors inline after a save attempt.
5. Approve-spec flow end-to-end: card → reader → approve → gate cleared.

## 6. Visual direction (suggestion, designer may push back)

A calm, dense control room. Closer to Linear/Height than to a marketing
site: generous type for reading surfaces, compact data-dense tables for
runs/work. Dark and light themes (system-following, toggleable). Color
carries meaning and little else: gate kinds, risk tiers, run outcomes each
get one consistent hue family. No decorative illustration. Monospace only
for ids, commands, and logs.

Hard constraints from engineering: Tailwind CSS for styling; all assets
embedded (no external fonts/CDNs — pick from system font stacks or a font
we can embed); everything must work in a plain browser on localhost.

## 7. Non-goals for v1 design

- No auth/user management, no multi-user presence.
- No kanban drag-and-drop (status changes go through explicit actions).
- No mobile layouts (don't break at 1024px, that's all).
- No editing of evidence, attempts, events, or generated files.
- No charts/analytics dashboards (token rollups render as plain numbers).

## 8. Deliverables requested from the designer

1. Layout per route (desktop, ~1440px reference; note behavior at 1024px).
2. Component inventory: card (per gate kind), run row, task capsule chip
   set (status/risk/priority/readiness), liveness indicator, conflict
   banner + diff, property panel, outline nav, empty/error states.
3. Reading-surface typography spec (scale, measure, table/code treatment).
4. Theme tokens (light/dark) mapped to Tailwind config values.
5. The five must-design moments from §5 as explicit frames.

Deliver as whatever the design tool exports best (Figma frames, HTML
mockups, or annotated images). They will be attached as evidence to Tusker
task SRV-T-0002 and gate the start of implementation.
