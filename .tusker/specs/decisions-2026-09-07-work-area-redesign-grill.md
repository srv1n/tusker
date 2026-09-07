---
subject: decisions/2026-09-07-work-area-redesign-grill
title: Work-area redesign discussion record
keywords: [work UI decisions, redesign rationale]
part_of: work-area-redesign
describes: []
status: canonical
created: 2026-09-07
read_when: "Tracing user constraints and unresolved work-area design decisions."
skip_when: "Reading the proposed screen contracts; use work-area-redesign."
sources: [work-area-redesign.md]
capsule:
  what: "User constraints, live inspection and unresolved work-area choices."
  use_when: "Discussing the work-area redesign."
  skip_when: "Looking up current shipped behavior."
decisions_locked: false
---

# Work-area redesign discussion record

## Scope and constraints supplied by the user

The user described the UI as cluttered and redundant, calling out waves, tickets and the right-hand open area. They requested a spec before application code changes. The desired result is intuitive, minimal and enjoyable for daily use.

Locked from explicit instructions: spec work only; remove redundant content; explore industrial control panel and cardboard references without tacky skeuomorphism; final direction must feel minimalist and Apple-native; no gradients, glows or unnecessary containers. Generate a private random alphanumeric seed. Each visual iteration receives a fresh screenshot-only critic assessment, including score and specific studio-level gaps.

## Inspection method

The user said they would be using the machine and did not want overlapping computer input. They suggested screenshots or Playwright. When asked for the web URL, they instructed the assistant to discover it from the running daemon and inspect it independently.

Locked: independently discover environmental facts; use isolated headless inspection. The daemon was found at 127.0.0.1:7420. Work, wave and task screenshots were captured without operating execution controls.

## Baseline critic

A fresh agent received only the Work screenshot and the requested critique rubric. Score: 5.8/10. It described a restrained operations dashboard whose repetition, vague status pills, sidebar weight, tiny metadata and abrupt rules weaken composition. This evaluates only the baseline and does not approve the proposal.

## Initial proposal — superseded by user clarification

Recommendation: outcomes/waves first, then their tasks, with explicit All tasks browsing. Alternative: tasks first with waves as grouping/filtering. The next user response chose waves first and corrected the sidebar and DAG proposals. Palette and exact interactions remain open.

See [[work-area-redesign]] for proposed screen contracts and acceptance evidence.


## User clarification — project entry, waves and execution visibility

The user wants the existing left project list retained, drag-reordered with saved order, and the active project restored and expanded on relaunch. Work should begin with waves, organized by readiness/running and other states, each with a meaningful high-level description. Opening a wave should show a full readable DAG with task flow and completed/running states. Selecting a task should quickly reveal its intended achievement and detail; slide-out versus another interaction is still open.

They said the task board may be reusable. Trains may be removable. Factory operations should be deleted as a UI surface. Diagnostics should remain for troubleshooting outside the main view. Plan and Epics were mentioned as existing surfaces, not approved for deletion.

They described multiple model providers and tiers, good global defaults with project overrides, task-declared work level, separate worker/reviewer/approver concepts, and visible actual models and ACP versus CLI exec at each stage. Exact levels, approval ownership and fallback policy remain questions; examples of model names are not verified provider availability.

Locked: wave-first organization; persistent reorderable project sidebar and active-project restoration; central readable stateful DAG; meaningful wave descriptions; quick task inspection; remove factory Operations UI; move diagnostics out of primary work; design model defaults/overrides and execution identity visibility. Proposed Waves/Board tabs and temporary task panel await discussion. Do not preserve the earlier project-switcher-only or hidden-DAG recommendations.

Next recommendation: automatic independent review after implementation, with human approval only at a declared outcome/decision boundary. The user has not yet answered this question.


## User clarification — automation and cross-project navigation

The user supplied a screenshot of expandable projects with nested recent work and Show more. They want quick access across multiple projects. The earlier switcher-only proposal is rejected; saved order, visible projects and restored expansion remain agreed. Whether nested entries are waves, tasks or both is still open.

The user wants agent work reviewed by agents and routine flow automated. Human review is for larger outcomes and evaluating artifacts such as screenshot comparisons and performance results, not routine code review. Explicit human gates still require a human. The question of mandatory manual wave acceptance is resolved against it; permission to automatically start subsequent waves is a separate, still-open decision.

They explicitly requested grilling and wayfinder, multiple independent questions per round, and continuously documented tickets and screenshot assets so implementation can start once the spec session finishes. This overrides the earlier one-question cadence. No application code changes are requested.


## Confirmed — prove a task, then a wave; defer automatic wave starts

The user explicitly separated stages: first manually start an individual task through a configured coding agent and see execution/progress work; second manually start a wave and observe its full sequence; only after these work consider multiple waves and parallel streams in one codebase. They rejected automatic starting for now and emphasized that writing a large amount of unusable code is the failure to avoid.

Locked: no automatic cross-wave initiation in the current scope. Agent review and progression through tasks inside the manually started wave remain automated. Future multi-stream work is deferred rather than silently included. This resolves the cross-wave-start question; the first four experience questions remain open.


## Confirmed — navigation round

The user said “Agreed on all four”: waves nested under projects with a few active/recent entries and Show more; per-project last-screen restoration including selected wave and graph position; one grouped wave overview with Waves/Board tabs and completed filter; temporary contextual task side panel with full-task navigation.

They redirected ownership to final UX/UI because another team owns execution mechanics. They then asked whether visual screen-by-screen iteration with a coding subagent would be more effective than completing the whole written spec first. That is a process question, not yet an instruction to implement production changes.


## New frontier — coherent product model

The user asked how sidebar, tabs and panels work together judiciously across onboarding, documents, specs/decisions, bounded task contracts, waves, model tiers and evidence. They questioned whether epics still serve a purpose and explicitly permit rethinking without backward compatibility. They want proposals and grilling, while execution details remain the other team's ownership.

Not decided: removing Epics, using specs as the intent grouping, adding a persistent outcome entity, evidence placement, and exact onboarding. Do not interpret exploratory remarks as approval of a replacement architecture. Their cheaper-model-with-retries example is a hypothesis, not a measured cost/speed claim.


## Confirmed — external planning and existing harnesses

The user wants a management/factory UI layer over coding-agent harnesses, not a new harness or embedded planning chat now. Specs originate in coding-agent or chat applications; Tusker manages the durable work. An all-in-one experience is a possible future direction, not current scope.

They explained that epics came from traditional product management and loosely grouped tasks such as auth. They do not identify a separate epic lifecycle they need and are open to removal. Record this as openness and rationale, not an explicit instruction to delete the schema. Recommendation remains to remove mandatory epic bookkeeping and the Epics destination, using linked specs/search for related tasks.


## Confirmed — completed results and epic removal

The user approved leading with completed results for a finished wave, and explicitly agreed to get rid of epics. They proposed multiple optional tags, with auth as an example, and quick filters. Tagging is a lightweight grouping direction; exact scope and matching behavior remain unresolved. The user asked whether another whole-UX rethink is needed; recommendation is to validate the agreed structure through rendered screens rather than restart it.


## User request — detailed specification and parallel build tickets

The user asked for a detailed spec and tickets they can assign to Luna, Terra or Muse while design discussion continues. Created six independent component contracts and one integration contract with native hard dependencies. Leaf ownership excludes shared production screens/API/router/styles and backend files; one integration owner composes those later. The user has not asked this session to dispatch implementation. Durable tags and missing execution identity/readiness fields remain explicit backend handoffs rather than fabricated UI behavior.


## Continuation — project knowledge and lightweight evidence

Question round: agent-prepared work through CLI; Documents reader scope; three model levels; location and obligation of human outcome review.

User answer: agents prepare work through the CLI, which should help and enforce contracts through linting and placeholders without wasting agent tokens. Documents must explain the whole project months later, including how ACP/waves work and why choices were made. Agreed Light/Standard/Demanding with implementation/review model mappings and inherited defaults. Human evidence inspection is optional: the operator may simply run the app on a device. Screenshots/performance output should be accessible without compulsory approval. The user additionally requested reconsidering mandatory Tusker scratchpads and repeated ticket-writing overhead, since harnesses already have their own notes, and automatic expiry of accumulated output, suggesting one week while retaining high-level tickets.

Locked: CLI preparation; broad durable knowledge purpose; three model levels; evidence availability alone does not create a human gate; remove compulsory duplicate scratch journaling as a design goal. Open: retention timer/protections/final evidence, exact minimal durable handoff, Documents composition. Proposed artifact badges are derived from attachments rather than overloaded topic tags. Details live in [[work-knowledge-and-retention]]. The assigned component contracts are not silently revised.


## Continuation — progressive discovery and prepared work

The user approved the proposed seven-day retention boundary and compact runtime handoff. Documents was clarified as progressive, bounded metadata navigation for both humans and agents: topic summaries and read/skip guidance at each level, exact current answers in leaf docs, rationale/research/alternatives in linked decision records. Their examples concern detailed indexing/quantization questions six months later; these are retrieval scenarios, not repository facts. Meaningful backlinks and coherent front matter should let CLI discovery avoid agents reading entire corpora. Audience-specific depth may be product, technical or domain-focused as warranted; downstream publishing is a potential use, not current implementation scope.

The user further clarified that initial task preparation should be detailed enough to execute: context, approach, file pointers, acceptance, review and one of three work levels. Tusker resolves that level to configured runner/model profiles when execution is authorized. Minimal runtime handoff must not erase preparation detail. They also requested repeatable seed/clear of fictional tasks and waves in a test repository to evaluate the now-built UI. Current facility discovery is read-only; no real execution or deletion authorized by a fictional seed.


## Continuation — repeatable testing and Documents refinement

User requests a repeatable seeded test project entirely controllable by CLI: two or three waves, two manually startable in parallel, multiple simple timer-like tasks with observable progress and reset. They require CLI parity for UI capabilities so agents do not need computer access. Recorded in [[repeatable-work-testing]], including genuine deterministic attempts versus provider conformance, CLI contracts, cleanup boundaries and acceptance scenarios.

User approves explicit configured fallback only. They prefer the existing Documents folder/file tree and clickable browsing shown in their screenshot; functional structure is good and needs visual polish, front-matter improvements and better CLI helpers. They explicitly expand this session to generic product/backend/CLI/UI design. No runtime implementation or cleanup is performed by this decision.
