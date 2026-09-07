---
subject: work-experience-map
title: Find the everyday Tusker work experience
part_of: work-area-redesign
status: canonical
created: 2026-09-07
read_when: "Resuming the work-experience decision frontier."
skip_when: "Reading detailed screen requirements; use work-area-redesign."
sources: [work-area-redesign.md, ../work/epics/WUX.md]
decisions_locked: false
capsule:
  what: "Decision frontier linked to the inert Tusker planning epic."
  use_when: "Continuing the redesign discussion."
  skip_when: "Implementing an already agreed contract."
---

# Find the everyday Tusker work experience

## Destination

An agreed everyday work experience with a complete spec, reviewed visual evidence and implementation-ready work contracts. Keep the application unchanged during design.

## Notes

Tracker parent: [Find the everyday Tusker work experience](../work/epics/WUX.md). Use grilling, wayfinder, domain-modeling and Tusker. Ask independent frontier questions together; never guess the user's answers. Explicit session request permits continual map updates across rounds. Decision records are proposed and inert; none grants execution permission. This document carries the rich planning context where the installed decision CLI creates only a title and placeholder body.

User constraints are recorded in [Work-area redesign discussion record](decisions-2026-09-07-work-area-redesign-grill.md). Screen contracts live in [A quieter work area](work-area-redesign.md). The supplied screenshot is [Sidebar reference](assets/work-area-redesign/sidebar-reference.png); baseline screenshots remain beside it.

## Decisions so far

[Set the boundary for automatically starting subsequent waves](../work/decisions/WUX-D-0005.md) — user resolved: manual task first, manual wave second; automatic cross-wave initiation and multiple streams deferred. Resolution evidence is in the discussion record. The installed CLI cannot amend/close this proposed decision record; tracker status remains proposed, not falsely reported closed.

## Earlier navigation round — answered

The remaining four questions have no dependencies on each other's answers. Their proposed decision records are children of the same epic.

| Decision record | Question and recommendation |
|---|---|
| [Choose which work appears beneath each project](../work/decisions/WUX-D-0001.md) | Waves, individual tasks, or both? Recommend recent/active waves, a small visible subset with Show more; keep tasks in the wave to avoid another huge tree. |
| [Choose where reopening a project takes you](../work/decisions/WUX-D-0002.md) | Restore the exact last screen or always return to Work? Recommend restore the last screen and expanded projects across relaunch; clicking the project name always opens Work. |
| [Choose how Work groups the waves](../work/decisions/WUX-D-0003.md) | One overview with state sections or separate state tabs? Recommend one overview with Needs you, Running, Ready to start and Planned; completed work behind a filter. Waves/Board are view tabs, not status tabs. |
| [Choose how a graph task opens without losing context](../work/decisions/WUX-D-0004.md) | Temporary side panel or navigate away? Recommend temporary panel retaining graph position, with full-task navigation for deep inspection. |

## Not yet specified

Exact role-level vocabulary and inherited model configuration; human outcome-gate triggers and timing; run/review/approval stage display; wave descriptions and honest readiness; graph scale and task-detail content; board reuse and Plan/Epics/Trains overlap; artifact comparison; visual alternatives and screenshot critic rounds; persistence scope between web and Mac; migration and runnable implementation acceptance.

## Out of scope

Application implementation during this session, dispatching workers, enabling automation, automatic cross-wave starting, multi-stream scheduling for the initial rollout, rebuilding the scheduler, deleting project data, and decorative theme machinery. Future execution behavior is being specified, not activated.


## Tracker limitations and build follow-through

Tusker decision creation supports titles and a decision paragraph but no supported body amendment, assignee, labels or native decision dependencies. Do not invent claims or closed states. Rich open questions live above; user answers live in the discussion record until a supported resolution route exists. Task records do support native dependencies at creation; wire the executable plan after contracts are settled.

The first two rollout outcomes have backlog placeholders linked from the spec. They are not implementation-ready and may overlap existing pilot tasks; reconcile before promotion. This avoids disguising a design conversation as executable work.


## Latest session resolution

The user confirmed all four navigation decisions in the current round. Their resolution is recorded in the discussion record and incorporated in the main spec. Proposed tracker decision statuses have not been changed through an unsupported mutation. The next frontier is rendered screen composition, mixed-state ordering, task panel content, graph readability and visual treatment. Execution mechanics and pilot rollout belong to another team and are outside this map's UX ownership.


## Product-model frontier

- [Choose whether a separate epic concept earns a place in the human experience](../work/decisions/WUX-D-0006.md): remove redundant grouping or identify an independent long-lived outcome job.
- [Choose how the human follows intent through work to verified results](../work/decisions/WUX-D-0007.md): connect Work and Documents through contextual links or justify a separate journey surface.

Both remain open. Detailed proposals are in the main spec; the current runtime fact investigation is read-only. Resolve the intent model before designing new grouping filters or task-authoring forms.


## Latest boundary decision

Planning conversation stays in external coding-agent/chat applications; Tusker manages durable specs, work and results using existing harnesses. Embedded chat and a new harness are outside current scope. The user's epic explanation supports removing duplicate grouping UI, but underlying schema deletion is not authorized. The next design frontier is onboarding/empty states and the first sidebar-plus-Work preview.


## Latest decisions — completion and grouping

The user approved completed-wave results as the default, with the DAG available, and explicitly approved removing epics from the product model. Optional multiple tags and quick filters replace loose topic grouping. The next frontier is tag scope/matching and rendered sidebar-plus-Work composition; another wholesale navigation rethink is not required without evidence from that flow.


## Implementation contract handoff

The current build plan is [Parallel build packets](work-area-build-packets.md), governed by [Tusker work experience](work-area-redesign.md). Seven rich task records were imported through the supported delivery-plan path: six separate UI components and one dependent live integration. The old execution-pilot placeholders were discarded with history preserved because execution belongs to the other team. The imported wave remains disarmed. Earlier map notes describing build records as unfilled placeholders no longer describe the new UI tickets.

Remaining discussion: Documents, broader onboarding, Plan/Trains placement, durable tags and model configuration. It may continue while the bounded leaf components are built. No silent revisions of a running worker's acceptance contract.


## Active continuation — knowledge and lightweight evidence

User confirms workers have been assigned externally. Continue design in [[work-knowledge-and-retention]] without amending their running contracts. The next round resolves retention boundaries and Documents entry composition. CLI preparation, broad project knowledge, model levels and optional evidence inspection are recorded in the discussion record. Mandatory duplicate scratch journaling is rejected as a product requirement; inspect current protocols before defining the migration.


## Scope expansion — product experience

The user expands the destination to coherent Tusker UI, CLI and backend experience. This supersedes the earlier UI-only design boundary for future specifications, while retaining the ownership of existing implementation packets and the prohibition on implicit execution. [[repeatable-work-testing]] specifies CLI-driven seeded parallel waves and safe reset. [[work-knowledge-and-retention]] records the approved existing Documents file tree, explicit-only model fallbacks, metadata helpers and retention. Remaining work is translating these new contracts into separately owned implementation tasks and evaluating the rendered Documents polish.
