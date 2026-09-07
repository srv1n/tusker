---
subject: work-knowledge-and-retention
title: Project knowledge, model choices and lightweight evidence
keywords: [documents, models, scratch, retention, evidence, human review]
part_of: work-area-redesign
status: canonical
created: 2026-09-07
read_when: "Continuing the UX decisions for Documents, model settings and evidence retention."
skip_when: "Implementing the already assigned Work component packets or looking up current cleanup commands."
sources: [work-area-redesign.md, decisions-2026-09-07-work-area-redesign-grill.md]
decisions_locked: false
capsule:
  what: "Agreed knowledge, model-level and optional-review direction with open retention decisions."
  use_when: "Continuing the UX spec beyond the assigned Work components."
  skip_when: "Implementing current Work packets or executing cleanup."
---

# Project knowledge, model choices and lightweight evidence

## Purpose and delivery boundary

Tusker should preserve what a person or subsequent agent needs to understand the project without requiring everyone to maintain another working journal. This continuation records the user's next decisions while the Work UI tickets are being implemented. It does not silently change their input interfaces, acceptance contracts or delivery-plan fingerprint. Any resulting component changes require a named follow-up or explicit handoff to the owner. No cleanup is activated by this specification.

## Confirmed decisions

### Prepared work arrives through the CLI

External agents prepare specs, tasks and waves through Tusker's CLI. The human should not repeat that preparation in a UI wizard. CLI helpers should supply structure/placeholders and validate required contracts, references and readiness, with concise actionable errors. Placeholders are authoring aids, never proof that a task is ready. Do not make agents reproduce machine-derivable metadata or read broad documentation to discover a missing field. Starting work remains manual in the current rollout.

### Documents is the project's long-term reference

Documents answers: what is this system, how does it work, and why did we choose this? It includes current system explanations, concepts such as ACP and waves, specs and the decision rationale behind them. It is not merely a list of specs or a run-log archive. The user's concrete scenario is returning months later and recovering that understanding.

Proposed composition, pending user review: searchable document list and readable document body; current explanations lead, with direct links to relevant decisions and work. Historical or superseded decisions remain available but visibly distinct from current guidance. Start here, topical browsing and search result layout are not yet settled. Do not introduce another independently maintained documentation copy. Rich authoring scope remains open; this answer established knowledge retrieval as the primary job.

### Model selection uses work levels

Confirmed labels: Light, Standard, Demanding. Each level maps independently to an implementation model and a review model. Global defaults apply, projects may override, and a task defaults to Standard unless its author selects another level. Execution views show actual model identity. Human approval policy is separate from model selection. Settings layout, inheritance presentation and override interaction remain to be designed; configured provider availability must come from the real system.

### Evidence does not imply a human gate

Human outcome review is optional. Screenshots or performance reports can be opened without requiring an approval action or blocking completion merely because they exist. The user may instead run an Xcode app on their device and be satisfied without recording a formal review. Agent review remains the normal workflow. Explicit human gates retain their existing authority and remain the reason to show Needs you.

Proposed presentation: derive compact Screenshots and Performance report links or badges from actual attached artifacts. These describe available evidence, not manually maintained topic tags, lifecycle status or permissions. They must not activate gates. A missing or expired artifact must not appear downloadable. Formal Approve/Request changes actions appear only when an actual review request or gate exists. No compulsory mark-as-viewed ceremony.

### Agent working notes are optional

The user rejects enforcing a parallel scratchpad when the coding harness already has its own working context. Target: no mandatory Tusker scratch journal or repeated prose updates. Durable task contracts, accepted outcomes and the minimum structured resume/handoff facts still belong to Tusker. Transient reasoning and routine progress chatter do not. CLI-derived status should replace repeated agent-authored summaries wherever possible.

Exact current mandatory writes and the minimum safe handoff contract require source inspection. This is a requested redesign, not a claim that existing worker protocols can already be skipped.

### Transient output should expire

The user wants automatic cleanup of accumulating screenshots, logs and other temporary output, suggesting roughly a week. High-level task records and durable project knowledge should remain. The subsequent round approved the seven-day boundary below, including protections and optional retention of final evidence. No files are purged in this planning session.

## Confirmed retention boundary

| Material | Proposed rule |
|---|---|
| Specs, current system docs, decisions, task contract and compact outcome | Retain as project history |
| Raw logs, intermediate screenshots, temporary reports and optional scratch files | Expire seven days after the task reaches a terminal outcome |
| Work still active or awaiting a human decision | Do not expire evidence needed to finish it |
| Final screenshots and full benchmark output | Same default expiry, with Keep available for explicit retention; keep a concise result summary |
| Artifacts deliberately published as documentation | Treat as durable documentation assets |

The user approved this boundary, including seven days after completion, Keep for selected final evidence and protection for active work or pending human decisions. Define restart/reopen behavior and ownership before implementation. Cleanup must target known Tusker-owned disposable artifacts, not arbitrary referenced files or external harness scratchpads. Reference metadata remains after expiry: show Evidence expired and when, preserve the recorded result and its provenance, and do not claim old results were freshly reverified. Retention scope and scheduling belong in a backend handoff, not the current UI leaves.

## Next decision frontier

1. Review the progressive Documents navigation and exact screen composition.
2. Specify model-setting inheritance and unavailable-profile behavior.
3. Define repeatable demo scenarios and distinguish UI simulation from actual runner proof.

## Implementation follow-through

After the remaining choices are resolved, emit bounded tickets for Documents navigation/reading, model inheritance settings, optional evidence access and efficient task records/retention. Include updates to affected current system docs with their implementation; do not rewrite current-behavior docs to claim this future design already runs. Backend/CLI work goes to that team's owner. Existing Work tickets continue under their assigned contracts.

## Current-source findings — 2026-09-07

Read-only inspection for this session found existing compact mechanisms to reuse rather than replacing the whole workflow:

- Capsule guidance already bounds task context; `cmd/tusker/v7_capsule.go` defaults to 80 tokens, warning above that and failing above 160. Inline verification with no evidence file is already supported (`.tusker/WORKFLOW.md`, proof-mode guidance).
- The daemon Ralph prompt does enforce a disposable `.tusker/scratch/<task>/PLAN.md` through `ensureTaskPlanFile` in `cmd/tusker/daemon.go`. This is a specific cross-attempt continuity mechanism, not a universal interactive-agent scratch requirement. Follow-up should eliminate duplicate journaling while preserving a minimal resume handoff.
- Closing/discarding already attempts to reap task scratch (`cmd/tusker/v7_control_cmd.go`, `cmd/tusker/scratch_retention.go`); accepted link-only evidence can protect referenced scratch from deletion.
- `tusker gc` is already dry-run by default with a 14-day default TTL (`cmd/tusker/scratch_gc.go`). This does not establish a periodic automatic seven-day policy for every screenshot, log or report. Existing size guidance is 200 MiB (`cmd/tusker/setup_doctor.go`).
- Evidence promotion already separates durable artifacts from scratch. Evidence prune currently classifies rather than deletes (`cmd/tusker/v7_proof_cmd.go`).

Implementation should reuse these mechanisms. Before changing retention, reconcile existing close-time deletion, accepted-evidence protections and new expiry semantics; neither current reference protection nor historical proof may be silently invalidated. No historical task logs were opened and no cleanup commands were run for this investigation.

## Subsequent user decisions — discovery and executable contracts

### Progressive knowledge discovery

Confirmed: one coherent Markdown corpus serves humans and agents. At the root and every lower topic level, expose a bounded directory of child topics and direct documents with meaningful summaries, read_when and skip_when. The CLI parses and presents that metadata; agents should not open every document or manually scrape front matter. A topic overview explains purpose and boundaries. Deeper levels reveal enough detail to choose the next relevant branch. Search may jump directly to a matching leaf; do not force hierarchy traversal when the answer is already identifiable.

Use semantic topics, such as authentication, LLM proxy and knowledge indexing, with finer subdivisions only where the material warrants them. Reuse subject/part_of rather than requiring a physical folder migration. A single document may present purpose and product behavior first, then technical or domain-specific detail; split only when independent retrieval helps. Do not require a PM document, technical document and customer document for every subject.

The user's six-to-seven hops of roughly a hundred tokens describes the desired economy, not a mandatory depth or an established performance measurement. Measure actual navigation on realistic questions and keep exact technical identifiers searchable. Example questions supplied by the user: what goes to LanceDB; which quantization setting is used; why one precision or indexing approach was selected. These are retrieval examples, not asserted facts about this repository.

Current behavior and rationale are separate linked reads: the current leaf states the answer and its authority/freshness; its decision record preserves alternatives, hypotheses, supporting research, trade-offs and the actual choice. Backlinks must explain a useful relationship, not form a decorative web. Superseded proposals should resolve to the current answer without deleting decision history. Public docs, customer explanations and technical articles can later derive from this source; no publishing pipeline is required now.

Existing source already supports subject/part_of relationships, bounded find results, read_when/skip_when and supersession (`internal/docgraph/search.go`, `resolver.go`, `cmd/tusker/docs_cmd.go`). Level-by-level metadata browsing is a proposed addition, not an existing documented CLI command. Reuse the current parser/resolver and corpus for both the UI and CLI.

### Detailed preparation, compact runtime handoff

The user approved the minimal handoff proposed in the previous round: changed work, verification status, remaining work/blocker and available resume reference, written at meaningful handoff/completion boundaries. This does not shrink the initial task contract. When asked to turn the planning conversation into tasks and waves, the preparing agent must preserve enough detail to execute without repeating the planning session:

- Intended outcome and relevant context, with direct references to governing specs and decisions.
- Implementation guidance and likely files to inspect, as specific as the conversation permits; distinguish suggestions from verified source facts.
- Scope, ownership, dependencies and wave membership.
- Acceptance criteria, verification/evidence requirements and independent review criteria.
- Work level (Light, Standard or Demanding) and explicit human gates only where requested or required.

Do not pad simple tasks, duplicate entire specs across tickets, or replace useful guidance with arbitrary tiny token limits. Compact indexes and handoffs are different artifacts from detailed executable contracts.

At a user-authorized start, Tusker resolves the task's work level through project overrides and global defaults to the configured implementation and review profiles. A profile supplies the actual harness/runner, model, effort and transport as supported. Model names in the conversation are examples, not hard-coded mappings or verified available models. Show the resolved actual identities on the run; task authors normally choose a level. Resolving a level does not authorize automatic starts or imply permission to substitute another model when the configured profile is unavailable. Missing-profile behavior remains to be designed.

### Repeatable UI exercise

User requests a fictional seeded test project with enough tasks and waves to inspect the assembled experience, clear the scenario and rerun it. Proposed scenario covers planned, ready, running, explicit-human-gate and completed waves; a branching/joining dependency graph; independent tasks; mixed work levels; available and expired evidence; linked documents and rationale. Include a second project to exercise switching and persisted navigation.

Reuse an existing test repository/seed facility if available. Reset must target only owned demo state and must refuse a real project. Simulated run states must be visibly identified as sample data and must not be reported as real coding-agent execution. Actual provider/runner execution is a separate integration proof owned with the runtime team. Inspect current facilities before choosing the smallest implementation. A seed fixture is useful review infrastructure, not an additional production navigation surface.

Demo discovery found reusable `e2e/agent_journey/fixture` and isolated WUX Vite previews, but no executable demo seed/reset command. The journey fixture models only two tasks. The integration preview currently combines overview and board; wave/task selection callbacks are no-ops, so it is not full navigation proof. Proposed seed work should reuse that fixture and native CLI in a disposable repository with automation disabled; use explicit simulated states for UI-only scenarios that cannot truthfully be created as live runs. Do not rely on the stale VITE_USE_MOCK README flag; no corresponding runtime was found.

## Confirmed continuation — file navigation, fallback and broader scope

The user approved fallback only when explicitly configured; otherwise show the unavailable profile and require a choice. Record actual fallback identity and reason on the attempt. No silent change in model quality or cost.

Documents retains its existing clickable folder/file tree, which the user finds functionally good. Refine that interface instead of replacing it with a new conceptual-topic navigation system. Semantic subject/part_of discovery remains useful for agent CLI routing alongside the actual file tree. Preserve folder expansion and file selection, keyboard tree navigation, clear selected state, and useful long-name access. Keep filtering and the optional Graph view. Folder/file click behavior should remain familiar; do not require a new hierarchy migration.

Visual proposals grounded in the supplied screenshot: reduce the oversized front-matter card's prominence, move editable metadata behind a compact Details disclosure in reading mode, show document title/current-status context clearly, and improve tree spacing/contrast/truncation without decorative containers. Render supported Mermaid diagrams in reading mode using the existing renderer; retain explicit source access and truthful rendering-error fallback. The screenshot displays Mermaid source, so diagram rendering is a concrete verification item, not proof of a diagnosed implementation bug. Preserve existing document editing functionality rather than silently turning Documents read-only. No new theme or document-editor framework is needed.

CLI helper contract: bounded child-folder/direct-file metadata listing; targeted search; exact document/section reads; front-matter validation and safe template/default assistance; missing-parent/duplicate-subject/broken-link diagnostics; meaningful backlink inspection and supersession/freshness visibility. Reuse the existing docgraph parser/resolver and expose documented structured output. Do not invent decision rationale, update verification stamps without checking, or overwrite handwritten metadata while inserting defaults. Read and write operations exposed in the UI need corresponding supported CLI operations with the same validation. Specific command spellings and absent capabilities must be inventoried before implementation.

The user explicitly broadened this effort from UI-only design to the whole Tusker product experience, including CLI/backend contracts. Existing assigned Work packets retain their ownership and acceptance boundaries. New implementation belongs in explicit follow-up slices rather than silently expanding those workers' scope. Repeatable testing has its own canonical spec: [[repeatable-work-testing]].
