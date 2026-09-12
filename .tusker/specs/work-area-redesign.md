---
subject: work-area-redesign
title: Tusker work experience — implementation specification
keywords: [work, project navigation, project rail, section rail, full height, pins, sidebar, waves, DAG, results, task panel, board, tags]
part_of: overview
describes: [internal/serve/ui/src]
status: canonical
created: 2026-09-07
read_when: "Building or reviewing the agreed human-facing work experience."
skip_when: "Changing execution policy, runner adapters, or task storage; those belong to the execution team."
sources: [decisions-2026-09-07-work-area-redesign-grill.md, work-area-build-packets.md, assets/work-area-redesign/sidebar-reference.png, assets/work-area-redesign/work.png, assets/work-area-redesign/wave.png, assets/work-area-redesign/ticket.png]
decisions_locked: false
updates: [docs/system/serve-ui.md]
capsule:
  what: "Approved UX structure with bounded implementation defaults, parallel component contracts and explicit backend gaps."
  use_when: "Implementing the Work experience or reviewing its rendered behavior."
  skip_when: "Implementing execution orchestration or researching unselected product directions."
---

# Tusker work experience

## 1. Purpose and authority

Tusker is the human management layer over existing coding-agent harnesses. It connects what was agreed, what agents are doing, what needs a person, and what proves the result. Planning conversations happen in external agent/chat applications. Tusker presents the durable specs, decisions, tasks, waves and evidence. No embedded chat or new coding harness is in this release.

This specification replaces the accumulated proposals in this file. The decision log preserves their rationale. The user approved the navigation model and requested detailed tickets that other agents can implement while design discussion continues. This session authors the specification and tickets; it does not dispatch implementation or enable automation.

Approved decisions are binding. Numeric dimensions, ordering tie-breaks and interaction details explicitly called implementation defaults below are reversible design judgments, ready to build and evaluate visually. The whole-product decisions_locked flag remains false because backend-dependent tags/model settings and later Documents/onboarding work remain open. Do not block the approved component tickets on those unrelated questions.

### Confirmed product structure

- Projects appear in a narrow left rail beside a labeled Waves / Board / Docs section rail. Section 4 and [[full-height-workspace]] supersede the September 10 horizontal strip.
- Docs and wave detail may open a third contextual pane. Main content starts below one compact feature toolbar; project settings stays in the section rail.
- Work has Waves and Board views. Wave overview is grouped, not a set of status tabs.
- Running wave detail leads with a full readable dependency graph.
- Selecting a task opens a temporary side panel; full task reading remains available.
- Completed wave detail leads with results; the graph is one click away.
- Remove Epics from the human workflow. Optional multiple task tags support cross-cutting filtering.
- Remove factory Operations from the UI. Diagnostics belongs under troubleshooting, outside primary Work navigation.
- Work, Documents and Settings are the main project destinations.
- Independent agent review is normal. Human involvement requires an explicit gate or declared outcome review, never routine code approval.

Trains removal and Plan/intake navigation still need a purpose check. The current tickets may remove their primary clutter but must preserve essential reviewed-plan/start/recovery actions in context. No backend endpoint, authorization check or stored work is deleted as part of UI cleanup.

## 2. Human model

| Object | Meaning | Not its job |
|---|---|---|
| Spec | Intended outcome, constraints and links to recorded decisions | Duplicating every run event |
| Task | One bounded contract with acceptance and evidence | A loose unchecked note |
| Wave | Tasks executed together under an authorization boundary | A permanent topic category |
| Result | Evidence and review supporting a claimed outcome | A worker's unsupported success statement |
| Tag | Optional topic, such as auth or security | Status, permission, model routing or required hierarchy |

A spec links to related tasks/waves/results; work links to its governing spec and decisions when those links are available. Do not synthesize relationships from matching titles. One task can occur in historical waves, but current runtime permits at most one open wave per task.

## 3. Interaction grammar

| Pattern | Responsibility |
|---|---|
| Project strip | Switch projects directly; scroll to the rest; pin frequent projects |
| Project navigation | Work, Documents and project settings for the selected project only |
| Tabs | Alternate representations of the same subject: Waves/Board or Results/Flow |
| Side panel | Inspect one task without losing graph/board context |
| Full page | Read a long spec, complete task contract or substantial evidence |
| Disclosure | Reveal supporting metadata, exact verification and diagnostics |
| Modal | Short consequential decision only; never routine navigation |

No nested drawers, permanent empty inspector, duplicate Open column, decorative dashboard statistics, status jargon explanations, or card inside card inside card. A real failure or necessary user action is never hidden for aesthetic reasons.

## 4. Navigation and persistence

September 11 authority: [[full-height-workspace]] is the detailed navigation and full-height layout contract. Read sections 2–5 for geometry, project/checkout routing, visibility, shared feature layout and saved state; sections 6–9 define Docs, Waves, Board and responsive integration. It replaces the September 10 horizontal project strip and Work/Documents top row, including placement requirements in compact-project-navigation.plan.yaml and older shell packets. Historical task IDs/links below remain history, not authorization to rebuild the strip.

Preserve pins, stable mounted ordering, validated route restoration, hidden-project recovery, logical-repository grouping and meaningful checkout selection. All Projects visibility semantics remain governed by [[project-registration-and-visibility]]. Existing registration, Search, Attention, settings scopes, secondary routes and embedded /panel remain available. The new plan changes navigation placement and content geometry, not backend execution authority.

The implementation handoff is docs/plans/full-height-workspace/README.md and full-height-workspace.plan.yaml. Numerical layout defaults, bounded view state, owned source seams, scenario checks and operational limitations are specified there. In sections 5–10 below, the newer full-height document wins only for placement/scroll/width: status truth, safe actions, editor semantics and domain contracts remain intact. In particular, this layout wave preserves the current top-down graph rather than reinstating older horizontal/pan/zoom proposals.

## 5. Work overview

### Layout

Project context, heading Work, Waves/Board tabs, compact search/filter controls, then sections. No explanatory essay below the heading. One row per wave; no repeated full ticket inventory underneath. Empty sections do not render.

Each row contains a linked full wave title, a meaningful description (up to two lines), one primary state and one compact progress/action fact. Description comes from authored outcome data when available; do not turn an execution count summary into invented product intent. Missing descriptions leave space unused rather than adding filler.

### Grouping rules

These are implementation defaults for a total, nonduplicating partition:

1. Fresh actionable human decision → Needs you, even if another branch is running; retain a secondary Running indicator.
2. Fresh executing or independently reviewing task → Running. Queued is labeled Queued, not running; do not infer liveness from durable task status.
3. Explicit current readiness permits start and no active run → Ready to start.
4. Remaining valid nonterminal waves → Planned, with truthful Paused, Queued, Blocked or Not authorized state/reason as applicable.
5. Authoritatively completed waves → completed filter, not default list.
6. Waves whose overall state cannot be determined because required reads failed or are stale → Status unavailable, a compact exceptional section shown only when needed. Unknown startability alone does not erase a fresh running/human-action fact; show its unavailable start reason in detail.

Unknown, failed-to-load or stale observations stay visibly unavailable and never qualify as ready/completed. If an explicit Needs you or running fact itself is stale, show freshness loss. Cancellation/supersession is history, not successful completion. Stable tie-break: existing source order then ID until a canonical timestamp exists. Do not reorder rows on every event merely for animation.

Ready must use authoritative readiness. Authorization=armed, all task.readiness=ready, or wave.status=open alone cannot prove runnable. If current API lacks a complete predicate, show Status unavailable with the relevant detail; report the missing contract. Do not implement a second scheduler in the browser.

### Interaction

Title/row navigation opens the wave. Embedded action buttons remain separate semantic controls, never nested inside a link. No separate Open column. Search matches title/description; filter state survives detail/back. Completed filter reveals completed waves, with direct links to results. An optional Unassigned tasks link is visible when tasks exist outside waves, leading to Board with that filter.

## 6. Wave flow

Wave title, concise outcome, one accurate summary, current relevant action, and Flow/Results view controls. Running/planned waves initially show Flow. Authoritatively completed waves initially show Results. An explicit user-selected tab or deep link wins. Do not yank an actively used graph away when a background event marks completion; indicate Results available and apply the new default on the next ordinary entry.

### Graph requirements

- Real prerequisite relationships, directed edges and readable task nodes. Use existing Mermaid rendering/layout facilities before adding a graph dependency. Do not rebuild a general graph library.
- Implementation default: horizontal dependency flow, roughly 220–280px node text widths, at least 14px title text at normal scale, state icon+label, and compact model name on active nodes where verified.
- Pan, zoom in/out, Fit and Reset. Initial fit cannot shrink text below practical legibility; for large graphs begin at readable scale with scroll/pan, not a postage stamp. Fit may explicitly show the whole graph, with zoom controls remaining usable.
- Preserve graph transform and selection across task inspection. Refresh state in place without relayout unless topology changes; keep selection anchored when topology changes.
- Show completed, executing, reviewing, queued, blocked, failed and unknown distinctly; color is supplementary. Worker success is not accepted task completion.
- Known outside-wave prerequisites appear as labeled external nodes/links. Use explicit member IDs to distinguish an unloaded member from a reference outside the wave. Without a canonical existence result, label that dependency unresolved rather than claiming it is missing or a valid external task. Confirmed missing references and cycles produce an honest graph warning and usable list, not invented edges or a blank screen.
- Keyboard users can reach nodes, select a task, and use controls. Provide a list representation with the same states/dependencies. On narrow screens offer Flow/List while preserving node reachability.
- A current human gate is visible above the graph as one concise action; do not repeat its full request inside another section.

Validation fixtures: 5-task chain, two parallel branches plus join, external prerequisite, cycle, 30-node long-title graph, live state update, no dependencies. Measure render behavior at 30 and 100 nodes; do not claim a performance budget without recording host/browser and results.

## 7. Task inspection

Implementation default: 420px temporary right panel above 1100px viewport width; below that use a full-width overlay. It opens only on selection. Escape/close restores focus to the originating node/card and leaves graph transform intact. Opening full task preserves a return destination. Query/search-state encoding must be allowlisted and scoped to the current project.

Content order:

1. Title and actual stage; current blocker or required decision.
2. What this task achieves, rendered from the contract intent without rewriting it into invented facts.
3. Active attempt: model, execution/review role, transport, last meaningful event. A missing value is Unavailable, not guessed.
4. Accepted result/evidence when present; failing acceptance/check when actionable.
5. Disclosures for full acceptance, non-goals, metadata and exact verification; logs and full task links.

No mandatory priority/risk/readiness badge pile. No silent collapse of the worker, reviewer and human-gate stages. Do not display a past worker's model as the current reviewer's model. Rapid selection A→B must not render late A data inside B's panel. Loading and unavailable task details have distinct states.

Use existing sanitized Markdown, evidence links and human-action components. Do not put raw HTML or unvalidated provider URLs into the view. Selecting a task does not launch it. Reuse only existing supported commands for Start/Retry/Cancel; no optimistic completion or unauthorized fallback.

## 8. Completed results

Results is an outcome reading surface, not a transcript. Lead with the delivered outcome, task acceptance/review summary, and evidence appropriate to the work. Link the governing spec and complete task where available. Flow remains one click away.

- Visual change: before/after of the same named state with clear labels; single image is labeled single, never claimed as a comparison. Broken/unavailable assets show an explicit state.
- Performance: before/after values with units, workload and environment if provided; absent context is visible, never filled in by inference.
- Behavior, migration or backend work: result summary, exercised scenario and relevant evidence; don't pretend a command recipe is a measured result.
- Evidence eligibility follows canonical acceptance/review data. Uploaded but unaccepted artifacts do not become verified merely by appearing on screen.
- No automatic global Verified stamp derived from process exit, wave drained, or task count. Completed-with-missing-evidence shows the inconsistency.
- Logs and full verification remain accessible but secondary. No empty celebratory proof cards.

Do not download arbitrary remote media or implement a new media proxy. Use existing safe artifact rendering; report unsupported kinds as links with honest labels.

## 9. Board and tags

Reuse the existing board/list interaction and durable task state actions. Remove the Epic selector from the human-facing view. Cards show title and the most useful state; model name only for active work with trustworthy identity. Details open the same task panel.

Keep task status, live execution and review distinct. Do not invent another lifecycle merely to get cleaner columns. A compact board/list may label existing columns in plain language; remove repeated Empty messages and redundant card metadata. Dragging task cards, if supported, must retain server transition checks; never imitate a status mutation locally.

Tags: optional multiple task tags; quick filters in Board/List, no required tag on creation, no nested taxonomy, no new top-level Tags screen. Implementation default for preview: match ALL selected tags and show a clear reset action. The preview may demonstrate tag chips/filtering using labeled fixtures. Production editing/filtering is capability-gated until canonical tag read/write APIs exist. Do not use local-only tags as durable truth or repurpose runtime routing domains. The full durable tag contract belongs to the backend handoff below.

## 10. Visual and accessibility contract

Minimalist, Apple-native in feel. Quiet instrument direction for the first working preview: system sans, graphite text, warm flat background, one restrained blue/petrol accent, semantic warning/error colors. The private random seed inspired a regular rhythm with rare accents; it is not shown. Paper/cardboard is a later comparison on the same information structure, not a second theme framework.

Reuse current tokens/components initially; local component styles may refine hierarchy. No worker rewrites global styles independently. Integrator applies any final shared token changes once. No gradients, glows, fake hardware, torn paper, nested decorative containers or compulsory sounds. Sound is deferred.

Validate 1440×1000, 1024×768 and 390×844, light/dark contrast, keyboard traversal, visible focus, reduced motion, long names and enlarged text. Meet ordinary text contrast of 4.5:1 and use readable text instead of low-contrast microcopy. No interaction requires hover or color alone. Motion communicates a state change, not activity theatre.

## 11. Existing contracts and backend gaps

Source inspected on 2026-09-07; revalidate before integration because another team is changing it.

| Needed | Existing source | UI boundary |
|---|---|---|
| Projects, waves, tasks | ProjectSummary, WaveSummary, TaskCapsule and query hooks | Use real IDs and source errors; no duplicate store |
| Dependencies | TaskDetail.deps | Lazy-load/cache necessary details; summaries do not contain complete edges |
| Actual run | RunSummary.model/lane/liveness; RunDetail.identity.runner | Runner union is currently narrow; never infer provider or ACP from model name |
| Results | WaveBrief.seeIt, outcome.tasks, landed, humanAction | Use bound proof and review; fullyDrained is not success |
| Wave intent | WaveBrief.outcome.summary | May be execution summary rather than authored purpose; missing author-intent field is a handoff |
| Start readiness | Current wave authorization and guarded actions | Incomplete composite readiness cannot be fabricated client-side |
| Tags | No tag field in current TaskCapsule/TaskDetail | Fixture preview only until durable API exists |
| Epic-free tasks | Current task creation requires epic | Remove display/selector; backend team must remove required association safely |
| Models/defaults | Existing project-local profiles and partly projected identity | Global defaults/project override editor is later; no pretend saved settings |

Backend handoff requests: authoritative wave startability with reason and revision; authored wave intent; provider/harness/model/effort/transport identity plus freshness/provenance for each stage; accepted completion/evidence binding; task tags read/write and validation; epic-free task creation. These are contracts to agree with the other team, not permission for UI workers to edit Go, schemas or daemon behavior.

## 12. Parallel implementation boundaries

Six leaf components can be built now against the exact input contracts in [[work-area-build-packets]]. They live in separate directories, use existing domain types and callbacks, and render in isolated Vite previews. One integration owner subsequently wires them to live queries/routes and reconciles current dirty work. This is real component code with fixture preview, not a fake production dataset.

No parallel worker edits router.tsx, Sidebar.tsx, DeliveryScreens.tsx, TaskScreens.tsx, global CSS, domain.ts, api.ts, queries.ts, package manifests, generated dist, macOS shell or backend files. Only the integration ticket owns designated shared UI files after prerequisites complete. Shared contract changes go through the lead before sibling work depends on them.

Before each worker starts: read AGENTS.md, the owned packet and referenced spec sections; inventory dirty state and reread owned files. Work in a separately assigned checkout/branch if needed, preserving the relevant current UI changes. Never reset, clean, stash or replace the other team's dirty baseline. Commit only owned paths when requested; no push/deploy/daemon start is implicit.

## 13. Evidence and acceptance

Every leaf delivers a rendered preview, focused runnable behavior checks and screenshots of normal/edge states. Source-string assertions are not interaction proof. Preview data is labeled as sample data; live integration is a separate gate and cannot be claimed from screenshots of fixtures.

At every visual iteration: capture the actual rendered screenshot; give only that screenshot and the critique rubric to a fresh context. Ask it to infer the aesthetic, evaluate composition/fine details against a top studio bar, penalize excessive/obviously generated elements, give tight specific gaps and a score out of 10. It receives no source or prior critique. Save critique alongside the screenshot and log what changed. Baseline Work scored 5.8/10; no new design score is claimed here. A score alone does not substitute for user acceptance or behavioral checks.

Human acceptance concerns subjective result quality, not routine code review. Agent review verifies contracts, accessibility basics, state truth and owned-path boundaries. Exact commands, pending outcomes and per-ticket screenshot filenames are in the build packets. Integration must exercise real queries and artifact links without starting real work merely to produce proof.

## 14. Deferred discussion

Detailed Documents redesign; first-run onboarding beyond existing Add project; richer Plan/intake placement; removing Trains rather than relocating essential actions; durable tag API semantics; global model-default editing; cost/retry policy UI; embedded chat; automatic cross-wave starts; custom sound; multi-device preference sync. These do not prevent the six bounded UI components from being built.

<!-- tusker:delivery-import:02edf868de8ca802:begin -->

## Work streams

- `[[WUX-T-0008]]` implements delivery source `board`.
- `[[WUX-T-0005]]` implements delivery source `flow`.
- `[[WUX-T-0006]]` implements delivery source `inspector`.
- `[[WUX-T-0009]]` implements delivery source `integration`.
- `[[WUX-T-0003]]` implements delivery source `navigation`.
- `[[WUX-T-0004]]` implements delivery source `overview`.
- `[[WUX-T-0007]]` implements delivery source `results`.

- `[[W-0013]]` is the imported delivery wave.

<!-- tusker:delivery-import:02edf868de8ca802:end -->


## September 9: restrained Work overview refinement

The operator requested a less overwhelming Work overview and standalone smaller-agent implementation handoffs. This supersedes earlier card-density guidance: use plain wave rows with title, at most one useful secondary line (action/blocker, progress, or short authored outcome), and one truthful primary action. Move full descriptions and technical diagnostics to reachable detail surfaces. Never trade status/authorization truth for visual simplicity. Remove raw project IDs, redundant badges, zero-progress filler, oversized unassigned banner and permanently expanded unrelated factory diagnostics. Preserve project persistence, full DAG navigation, Waves/Board, search, historical distinctions and visible degraded live-update state.

Standalone packets: docs/plans/work-overview-ux-handoffs/01-status-and-action-contract.md; 02-quiet-wave-list.md; 03-shell-and-sidebar.md; 04-integrated-visual-acceptance.md. Each includes full context and acceptance. These are handoff files, not new tracker IDs. 01 establishes grouping contract; 02 consumes it; 03 owns surrounding shell and can run independently subject to existing sidebar ownership; 04 performs integrated acceptance and shipped system documentation updates. No product implementation was started by writing these packets.

<!-- tusker:delivery-import:145461374ff0a123:begin -->

- `[[WUX-T-0017]]` implements delivery source `project-qualification`.
- `[[WUX-T-0016]]` implements delivery source `project-shell`.
- `[[WUX-T-0015]]` implements delivery source `project-state`.

- `[[W-0016]]` is the imported delivery wave.

<!-- tusker:delivery-import:145461374ff0a123:end -->
