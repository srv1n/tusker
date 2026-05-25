# Agent Feedback Review - 2026-05-24

Status: processed

This review marks all feedback notes present in the tracked repos as processed into product stories as of this pass.

## Source Coverage

| Repo | Feedback notes | Disposition |
|---|---:|---|
| `tusker` | 2 | Tusker platform stories |
| `rzn/backend` | 11 | Tusker platform stories plus backend/RZN product stories |
| `guppie/cinta_wt_phase1_test` | 10 | Tusker platform stories plus Cinta product stories |
| `rzn/rznapp` | 11 | Tusker platform stories plus rznapp product stories |
| `rzn/rzn-browser` | 0 | No feedback beyond template |
| `careless/CarelessWhisper` | 0 | No feedback beyond template |

Existing story file reviewed:

- `tusker/feedback/stories/2026-05-21-agent-feedback-product-stories.md`

The May 21 platform stories remain valid. New feedback adds several missing stories and sharpens priority around proof gaps, packet quality, task routing, and downstream product follow-ups.

## Review Of Existing Platform Stories

| Story | Decision | Notes |
|---|---|---|
| AFS-001 Prevent Legacy/V7 Task ID Collisions | Keep P0 | Reinforced by SBL and FNR collisions. |
| AFS-002 Explain Protected-Branch Implementation Flow | Keep P0 | Reinforced by multiple protected branch handoff notes. |
| AFS-003 Make Same-Task Proof Writes Retryable | Keep P0 | Still valid; add retry-command detail. |
| AFS-004 Add a First-Class Feedback Command | Keep P1 | More urgent because several notes drifted into status reports. |
| AFS-005 Generate Feedback Digests Across Repos | Keep P1 | This review was manual; the command should exist. |
| AFS-006 Install and Update Feedback Templates Automatically | Keep P1 | Add bootstrap feedback line and README generation. |
| AFS-007 Provide Scoped Verification Recipes | Keep P1 | Reinforced by rustfmt/cargo check shared-workspace noise. |
| AFS-008 Attribute Shared-Workspace Compile Blockers | Keep P1 | Reinforced by H-022/H-023 compile blocker note. |
| AFS-009 Migrate Critical Root Guidance Into Project Skill | Keep P2 | Still needed where `tusker/SKILL.md` is missing. |

## New Tusker Platform Stories

### AFS-010 - Print Remaining Proof Gaps After Verification

Priority: P0

As an agent adding proof, I need `verify add` and `proof status` to tell me the next missing proof requirement immediately, so I do not loop on finish attempts after adding only part of the required proof.

Acceptance criteria:

- After `tusker verify add`, output includes `proof_status` and a concise list of remaining gaps by owner/class.
- If proof mode requires evidence after inline rows, output names the missing evidence class, for example `missing evidence_required: automated_test`.
- `tusker finish` repeats the same missing-proof summary and suggests the shortest corrective command.
- Regression covers a medium-risk/card-proof task where inline verification passes but evidence remains missing.

Source notes:

- `backend/2026-05-21-codex-trace-api-proof.md`

### AFS-011 - Make Missing Attempts Recoverable From Finish

Priority: P1

As an agent finishing a small task, I need `tusker finish` to give the direct recovery command when no attempt exists, so I do not stop to inspect help for routine closeout.

Acceptance criteria:

- If `tusker finish <TASK-ID>` fails because no attempt exists, the error prints `tusker attempt start <TASK-ID>` as the next command.
- The message explains that attempts are runtime/session state, not durable task status.
- A `--start-attempt-if-missing` option is considered but defaults off unless policy allows it.
- Regression covers a task with satisfied proof rows and no attempt.

Source notes:

- `cinta/2026-05-22-codex-title-rename-cache.md`

### AFS-012 - Harden Agent Packets Against Missing Routes And Stub Acceptance

Priority: P1

As an agent receiving a task packet, I need packet generation to omit dead project-skill/domain links and flag stub acceptance, so I do not waste time chasing missing files or proving vague work.

Acceptance criteria:

- `tusker packet --for agent` checks referenced project-skill and domain route files before printing them.
- Missing project routes are omitted or clearly marked as missing, with a fallback to `tusker/README.md`.
- Stub or placeholder acceptance is surfaced as a packet warning and as a validation warning.
- Proof cannot be marked satisfied when acceptance remains only a stub unless an explicit waiver exists.
- Regression covers a repo with no `tusker/SKILL.md` and a task with placeholder acceptance.

Source notes:

- `cinta/2026-05-21-codex-vne-task-packet-stubs.md`

### AFS-013 - Add Domain And Lane Aware Work Routing

Priority: P1

As an agent picking up architect-directed work, I need `tusker next` and search to route by domain/lane/workstream, so direct hardening lanes do not become ambiguous mega-tasks or unrelated queue picks.

Acceptance criteria:

- Tasks can be tagged or indexed by domain/lane in a way `tusker next --domain <name>` and `tusker next --lane <name>` understand.
- `tusker next` explains why it chose or skipped a task when a domain/lane filter is used.
- The command can detect when no matching task exists and suggest creating a focused task with the likely domain/lane.
- A task-split helper can turn a review pack into parallel lane candidates with expected overlap warnings.
- Regression covers an eval-harness request where default `next` surfaces unrelated CTX work but `--domain evals` finds or proposes the right lane.

Source notes:

- `backend/2026-05-21-codex-eval-harness-routing.md`
- `backend/2026-05-21-codex-storage-cleanup-lane.md`
- `backend/2026-05-21-sarav-source-qa-readiness-task-split.md`
- `backend/2026-05-22-codex-dss-file-identity.md`
- `rznapp/2026-05-21-codex-hardening-task-split.md`

### AFS-014 - Keep Compact Command And Proof Discipline Upstream

Priority: P1

As a Tusker operator maintaining many downstream repos, I need compact-command and proof-discipline guidance to live in the installed skill and managed bootstrap contract, so users do not hand-edit every repo to fix token burn.

Acceptance criteria:

- Installed skill, repo bootstrap block, project-skill command policy, workflow prompt, and audit repair path all use the same concise context-budget policy.
- `tusker audit --write` can repair old verbose guidance into the managed compact form.
- The managed guidance discourages process/status scans, transcript-like proof, raw logs, and broad task-file reads by default.
- Regression covers an update from old AGENTS/CLAUDE boilerplate to the compact bootstrap plus feedback hook.

Source notes:

- `tusker/2026-05-21-codex-keep-compact-command-proof-discipline-in-the-ins.md`
- `tusker/2026-05-21-codex-feedback-loop.md`

## Downstream Product Stories

These are not necessarily Tusker platform work. They should be copied or promoted into the owning repo's tracker when Sarav wants implementation.

### DPS-001 - Cinta TipTap Theme Source Of Truth

Owning repo: `guppie/cinta_wt_phase1_test`

As a Cinta user, I need TipTap body text, links, selections, highlights, and placeholders to follow the active SwiftUI theme, so WebView editor content does not fall back to stale hardcoded colors.

Acceptance criteria:

- Host-injected TipTap CSS uses `--bd-*` variables for ink, link, highlight, selection, caret, placeholder, and background roles.
- Later host styles cannot override theme variables with hardcoded `!important` color values.
- Theme variable injection updates on preset and light/dark changes.
- Regression covers host CSS using theme variables and pixel/DOM checks for `--bd-tx` in WebView root scope.

Source notes:

- `cinta/2026-05-21-claude-theme-tiptap-handoff.md`
- `cinta/2026-05-21-codex-tiptap-theme-css.md`

### DPS-002 - Cinta iPhone Source History And Dictation Scroll UX

Owning repo: `guppie/cinta_wt_phase1_test`

As an iPhone Cinta user, I need source-history actions and dictation scrolling to match actual host capabilities, so invalid restore actions do not show false failures and live dictation stays visible without fighting user scroll.

Acceptance criteria:

- iPhone source-history actions are filtered using restore preconditions before rendering buttons.
- Long restore actions show visible progress before blocking work begins.
- Success and failure outcomes can be surfaced by the sheet without relying only on dismissal or modal alerts.
- Dictation auto-scroll ticks when content grows during recording, even when insertion uses full `setMarkdown` replacement.
- A user-scroll suppression window is either implemented or explicitly rejected after field testing.

Source notes:

- `cinta/2026-05-22-claude-source-history-iphone-restore.md`
- `cinta/2026-05-22-claude-iphone-overflow-and-autoscroll.md`

### DPS-003 - Backend Domain Packs Contract Completion

Owning repo: `rzn/backend`

As an RZN admin building Domain Packs and Source Setup flows, I need backend contracts to match the new frontend IA, so the UI does not ship against stubs or dishonest lifecycle states.

Acceptance criteria:

- Domain Pack manifest/list/detail/install APIs support version pinning and tenant-owned drafts.
- Source Setup has an explicit table/contract rather than overloading catalog binding.
- Ingestion mode is modeled as `cards_only | cards_bm25 | full`, with `bm25_only` kept only as a legacy alias.
- Review queue lifecycle states are reconciled between UI buckets and backend schema.
- Extraction-run creation either dispatches a worker or clearly represents queued control-plane state.
- Open review findings are tracked as tasks or gates, not loose handoff prose.

Source notes:

- `backend/2026-05-22-claude-domain-packs-frontend.md`
- `backend/2026-05-22-codex-domain-packs-backend-wiring.md`
- `backend/2026-05-22-claude-domain-packs-review-wire.md`
- `backend/2026-05-22-codex-domain-packs-review-rework.md`
- `backend/2026-05-23-claude-domain-packs-redux-fixes.md`

### DPS-004 - Backend Indexing Lifecycle Observability

Owning repo: `rzn/backend`

As an RZN operator, I need indexing lifecycle and batch-size behavior to be visible and safe, so slow BM25/vector jobs are diagnosable without reading repeated progress logs.

Acceptance criteria:

- BM25 lifecycle records expose version, status, cutover, source scope, counts, and failure state.
- Rollback semantics are explicit: metadata-only or atomic directory swap.
- Runtime diagnostics report effective ready-fetch, embed, vector-write, and BM25-write batch sizes.
- A smoke test catches accidental single-row BM25/indexing writes.
- Cleanup scheduling/API ownership is captured as a tracked task or runbook.

Source notes:

- `backend/2026-05-21-codex-bm25-index-lifecycle.md`
- `backend/2026-05-21-codex-storage-cleanup-lane.md`
- `rznapp/2026-05-22-codex-cdc-bm25-batch-size.md`

### DPS-005 - Rznapp Product-First Canonical Projection UI

Owning repo: `rzn/rznapp`

As an RZN user chatting with local or remote models, I need canonical projection internals hidden by default, so reducer/event/debug machinery does not leak into the product transcript.

Acceptance criteria:

- `CanonicalThreadProjectionView` renders user-facing text first.
- Diagnostic fields require an explicit debug flag or developer route.
- Regression covers `data.text` content blocks, duplicate seed event rejection, and waiting-for-response states.
- Failure states show a product-safe message plus a debug affordance, not raw IDs or zero-usage JSON.

Source notes:

- `rznapp/2026-05-22-codex-canonical-projection-debug-leak.md`

### DPS-006 - Rznapp Local LLM Profile Semantics

Owning repo: `rzn/rznapp`

As a user configuring a local LLM, I need profile tests and credential handling to reflect real local inference, so path probes do not masquerade as successful assistant responses.

Acceptance criteria:

- Local profiles are treated as no-credential configurations even when legacy rows contain stale credential fields.
- Profile test success requires an actual assistant response.
- Path/model existence checks are labeled as preflight, not successful inference.
- UI copy and tests distinguish preflight pass, model load pass, and response pass.

Source notes:

- `rznapp/2026-05-22-local-llm-profile-credential-feedback.md`

### DPS-007 - Rznapp Runtime Configuration Single Source

Owning repo: `rzn/rznapp`

As an RZN developer swapping local/cloud backends, I need one canonical API base helper and one analytics endpoint contract, so endpoint changes do not require chasing fallbacks across Rust, frontend, and env files.

Acceptance criteria:

- Main app backend base URL has one canonical helper/env path.
- Service-specific endpoints remain separate but documented.
- Analytics uses `RZN_ANALYTICS_ENDPOINT` with the current local backend fallback.
- Tests or static checks reject stale literal `api.rzn.ai`, `rzn.ai`, or `localhost:8002` defaults in runtime paths.

Source notes:

- `rznapp/2026-05-22-codex-backend-url-config.md`

### DPS-008 - Rznapp Execution Policy Context For Tool Batches

Owning repo: `rzn/rznapp`

As the runtime enforces tool execution policy, I need tool batches and resumes to carry explicit backend and credential context, so policy decisions are auditable once cloud/tool-gateway credentials become first-class.

Acceptance criteria:

- `ToolExecutionBatchCommand` or its successor carries requested backend and credential intent.
- Resume paths preserve the same execution-policy context.
- Denials record the concrete decision input, not only the desktop/no-credential default.
- Tests cover cloud-credential, local-no-credential, and denied-tool cases.

Source notes:

- `rznapp/2026-05-21-worker-execgate-policy-context.md`

### DPS-009 - Rznapp Design Handoff Promotion

Owning repo: `rzn/rznapp`

As a product owner reviewing a design handoff, I need the design pack converted into reviewable implementation tasks, so strategy docs do not sit outside the execution queue.

Acceptance criteria:

- The handoff design pack is copied into repo docs with source provenance.
- Open questions are recorded as decisions or gates.
- The first implementation slice is selected and filed as Tusker tasks.
- The design-token/atom foundation tasks are explicitly upstream of screen rewrites.
- A preview route or artifact exists for reviewing atoms before broad screen work.

Source notes:

- `rznapp/2026-05-22-claude-design-handoff-synthesis.md`

## Processing Decision

All current feedback notes are now processed into one or more stories above or into the existing May 21 platform stories. Raw notes should remain in their source repos. Do not delete them.
