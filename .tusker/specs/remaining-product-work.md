---
subject: remaining-product-work
title: Remaining product work after the three real-work streams
keywords: [handoff, remaining work, coverage, wave outcome, scratch, retention]
part_of: planning-handoff-and-agent-entry
status: canonical
created: 2026-09-07
read_when: "Assigning remaining product development after the real-work lifecycle, fixture and UI streams."
skip_when: "Executing the already assigned three real-work testing streams."
sources: [planning-handoff-and-agent-entry.md, work-knowledge-and-retention.md, real-work-test-packets.md]
decisions_locked: false
---

# Remaining product work

These contracts authorize preparation only. Import is blocked; no new task IDs or wave were allocated. No implementation, dispatch, tests of product behavior, cleanup or lifecycle repair has been performed by this authoring session.

## Existing work: reuse, do not duplicate

- Assigned streams: [[real-work-lifecycle-cli]], [[real-work-test-repository]], [[real-work-ui-acceptance]]. Their lifecycle, fixture and SSE acceptance remain prerequisites for final integration. Completion-state discrepancies belong to the lifecycle stream.
- Model defaults/backend/API/UI: WUX-T-0010, WUX-T-0011, WUX-T-0012. Reported implementation is not fresh end-to-end proof; resume these IDs for missing acceptance only.
- Documents UI and helpers: WUX-T-0013, WUX-T-0014. Folder introductions, metadata, navigation and live acceptance must be checked against [[documents-experience]] and the assigned UI stream; patch gaps there rather than creating a competing Documents implementation.
- Complete runtime packets: FLW-T-0010 (and its existing dependencies). The coverage task below concerns authoring coverage, not replacing that contract or its dependency chain.
- Skill organization: completed in this session. No new skill-redesign task.

## Sequencing and ownership

After the three assigned streams land, run two lanes: coverage → wave-outcome; optional-scratch → artifact-retention. Then evidence-view depends on both lanes. The dependencies serialize shared delivery and Work UI files. Existing lifecycle work must land before optional-scratch; existing UI work before wave-outcome. These external handoffs have Markdown packets rather than new task IDs, so their prerequisites are explicit here, not falsely encoded as resolved tracker edges. Model/Documents acceptance can proceed on their existing IDs with separate ownership, after the same integration baseline. Do not run a broad legacy FLW wave merely to execute these focused follow-ups.

Use the complete task packet, not just the title. First check current source: another stream may already have satisfied part of acceptance. Preserve those changes and deliver only missing behavior. Suggested Standard/Demanding levels are model-neutral; older importer stores standard/complex. No model is selected or launched here.

## Verification contract

Each new task names a future focused Go acceptance suite to create as part of implementation. Those commands are intended checks, not existing proof. UI suites must run browser assertions through that suite or record a separate exact browser command. Review actual behavior and source identity; source-only inspection does not establish live harness or human acceptance. Record remaining prerequisites honestly.

## coverage — Expose requirement coverage when preparing tasks and waves

Outcome: an agent can convert an externally authored specification into complete Tusker work and see exactly which requirements are covered or deliberately deferred before import. This is authoring assistance, not a Tusker interviewing skill. Existing FLW-T-0010 owns complete runtime packet preservation; do not redo it.

Implementation scope and approach:
Inspect direct_authoring_cmd.go and direct_wave_authority.go — the wave authoring validation and review projection that replaced the delivery doctor/review surface. Reuse requirements, requirement_refs and acceptance IDs. Extend the existing review projection with a bounded coverage report; explicit deferrals need a reason, not pretend tasks. Prefer existing schema support; add the smallest compatible field only if needed. Preserve legacy waves. Validate missing/unknown IDs and dependency cycles; expose the same result in CLI JSON and the wave review projection, without another review wizard. Retain detail in the canonical task contract, not a duplicated spec blob.

Ownership: cmd/tusker/direct_authoring_cmd.go, cmd/tusker/direct_wave_authority.go. Inspect named files and callers before editing; file hints are starting points, not permission to replace sibling work. Shared CLI registration, capabilities and system-doc sections require narrow edits after rereading the latest tree.

Non-goals: no new scheduler, model harness, mandatory human code review, automatic start, formal active-spec revision locking, or implementation of the three assigned real-work test streams. Preserve existing task IDs and contracts. If behavior already exists, verify it and implement only the gap.

Work level: Standard; use configured execution/review profiles at start, never hard-coded model names.

Review: independent agent checks acceptance against changed material and actual test output. Human inspection is optional unless explicitly gated. Completion report leads with observed result, exact commands/PASS-FAIL, limitations and how to try it; no self-certified proof. Update affected current system documentation and shipped CLI help with the implementation.

Acceptance:
- A1: A plan with multiple requirements and tasks reports each requirement with covering task and acceptance IDs; valid shared coverage works.
- A2: An omitted requirement is visibly uncovered; an explicit deferral retains its reason; unknown IDs fail with actionable paths. No semantic-completeness claim is made from structural lint.
- A3: Task non-goals, guidance, acceptance, verification, level and spec links survive import and readback; reuse existing packet tests rather than inventing a packet format.
- A4: Cycles and missing prerequisite references refuse safely; repeat import retains stable IDs and no command arms or starts work.
- A5: Existing plans without the new optional projection remain readable; focused positive/negative tests and CLI documentation describe the exact supported contract.

Verification: `command: go test ./cmd/tusker -run TestRemainingCoverage -count=1` (future suite).

## wave-outcome — Show the promised wave outcome separately from the completed result

Outcome: before starting a wave the operator sees one or two plain-language sentences describing the capability they will gain; after completion they see what actually shipped. Example: You can sign in with Google and return to your original page. An implementation task list is not the summary.

Implementation scope and approach:
Inspect v7_wave_cmd.go, v7_wave_brief.go, direct_wave_authority.go, serve_actions.go and Work wave components. First determine whether an existing authored summary can serve this purpose; reuse it, otherwise add a backward-compatible expected_outcome field. Wire create/import/read/update through supported CLI and the existing API. Render near the wave title and in detail with minimal typography, no extra decorative card. Actual completion remains derived from reviewed work/results; failed or partial waves cannot display the promise as achieved. Preserve manually written summaries through reconciliation and readback.

Ownership: cmd/tusker/v7_wave_cmd.go, cmd/tusker/v7_wave_brief.go, cmd/tusker/serve_actions.go, internal/serve/ui/src/features/work. Inspect named files and callers before editing; file hints are starting points, not permission to replace sibling work. Shared CLI registration, capabilities and system-doc sections require narrow edits after rereading the latest tree.

Non-goals: no new scheduler, model harness, mandatory human code review, automatic start, formal active-spec revision locking, or implementation of the three assigned real-work test streams. Preserve existing task IDs and contracts. If behavior already exists, verify it and implement only the gap.

Work level: Standard; use configured execution/review profiles at start, never hard-coded model names.

Review: independent agent checks acceptance against changed material and actual test output. Human inspection is optional unless explicitly gated. Completion report leads with observed result, exact commands/PASS-FAIL, limitations and how to try it; no self-certified proof. Update affected current system documentation and shipped CLI help with the implementation.

Acceptance:
- A1: CLI authored outcome survives import, reload and reconciliation and appears in wave show/brief structured output.
- A2: Overview and wave detail show the authored promise before start; older waves without a summary remain usable without invented text.
- A3: Actual results, limitations and partial/failed delivery are distinguishable from intended outcome; process exit alone cannot generate a success claim.
- A4: Supported CLI edits and UI reads share validation; empty/oversized text is handled explicitly with no silent destructive truncation.
- A5: Tests cover old/new wave serialization and reconciliation; browser proof covers before-start, complete and partial states with readable keyboard-accessible presentation.

Verification: `command: go test ./cmd/tusker -run TestRemainingWaveOutcome -count=1` (future suite).

## optional-scratch — Remove mandatory scratch journaling while preserving safe resume

Outcome: workers can use their coding harness scratchpad and do not have to repeatedly write a Tusker PLAN.md. Tusker retains only the structured facts necessary for ownership, progress, handoff and truthful recovery.

Implementation scope and approach:
Trace daemon.go ensureTaskPlanFile and its prompt/read callers, work-session submit/resume, packet generation and scratch cleanup. Identify which PLAN.md facts are actually consumed. Move necessary resume facts into existing run/task handoff fields; do not create another journal. Make an absent plan normal on new and old tasks. Preserve old plan files until ordinary retention policy handles them. A restarted worker receives current task contract, claim/workspace, prior outcome/blocker and evidence pointers; omit transcript and repeated prose. Compare before/after prompt bytes and mandatory write counts for the same fixture.

Ownership: cmd/tusker/daemon.go, cmd/tusker/work_session_cmd.go, cmd/tusker/scratch_retention.go. Inspect named files and callers before editing; file hints are starting points, not permission to replace sibling work. Shared CLI registration, capabilities and system-doc sections require narrow edits after rereading the latest tree.

Non-goals: no new scheduler, model harness, mandatory human code review, automatic start, formal active-spec revision locking, or implementation of the three assigned real-work test streams. Preserve existing task IDs and contracts. If behavior already exists, verify it and implement only the gap.

Work level: Demanding; use configured execution/review profiles at start, never hard-coded model names.

Review: independent agent checks acceptance against changed material and actual test output. Human inspection is optional unless explicitly gated. Completion report leads with observed result, exact commands/PASS-FAIL, limitations and how to try it; no self-certified proof. Update affected current system documentation and shipped CLI help with the implementation.

Acceptance:
- A1: A worker can prepare, execute and submit without PLAN.md or a replacement mandatory freeform journal.
- A2: Crash/resume and explicit handoff preserve claim/workspace, next actionable state, blockers and relevant evidence without replaying the full conversation.
- A3: Existing tasks with or without PLAN.md continue safely; this change does not delete old notes or weaken leases, proof, independent review or human gates.
- A4: Focused tests exercise prompt construction, missing notes and resume; report measured prompt bytes and mandatory writes before/after, with no fabricated token estimate.
- A5: Generated guidance and actual packet instructions agree that harness scratch is sufficient; optional Tusker scratch remains supported.

Verification: `command: go test ./cmd/tusker -run TestRemainingOptionalScratch -count=1` (future suite).

## artifact-retention — Implement safe seven-day retention with Keep and durable evidence receipts

Outcome: Tusker automatically removes owned transient output seven days after terminal completion while retaining task results, provenance and deliberately kept artifacts. Active work and human waits retain the evidence needed to finish. This implements the approved policy, not immediate close-time deletion.

Implementation scope and approach:
Inspect scratch_retention.go, scratch_gc.go, v7_control_cmd.go, v7_proof_cmd.go and existing artifact metadata. Reconcile current close-time reap, fourteen-day GC and link-only protections through one eligibility predicate. Eligible artifacts are explicitly Tusker-owned scratch/log/intermediate and final report assets; never delete arbitrary linked source or external harness storage. Reuse existing resident maintenance scheduling; interactive sessions do not launch it. CLI supports dry-run, scoped apply, policy inspection and Keep/unkeep with the same API used by UI. Retain metadata after deleting bytes. Reopen before expiry protects artifacts again; a later terminal transition starts a new seven-day window. Reopen after expiry keeps the receipt expired and must not pretend bytes were restored. Keep has no automatic expiry until explicitly removed; unkeep reevaluates the existing terminal timestamp. Serialize deletion with Keep/reopen decisions and check paths/symlinks at deletion time.

Ownership: cmd/tusker/scratch_retention.go, cmd/tusker/scratch_gc.go, cmd/tusker/v7_proof_cmd.go, cmd/tusker/v7_control_cmd.go. Inspect named files and callers before editing; file hints are starting points, not permission to replace sibling work. Shared CLI registration, capabilities and system-doc sections require narrow edits after rereading the latest tree.

Non-goals: no new scheduler, model harness, mandatory human code review, automatic start, formal active-spec revision locking, or implementation of the three assigned real-work test streams. Preserve existing task IDs and contracts. If behavior already exists, verify it and implement only the gap.

Work level: Demanding; use configured execution/review profiles at start, never hard-coded model names.

Review: independent agent checks acceptance against changed material and actual test output. Human inspection is optional unless explicitly gated. Completion report leads with observed result, exact commands/PASS-FAIL, limitations and how to try it; no self-certified proof. Update affected current system documentation and shipped CLI help with the implementation.

Acceptance:
- A1: At just before seven days artifacts remain; at/after seven days eligible bytes are removed by maintenance or explicit scoped GC; fake-clock tests need no real waiting.
- A2: Active/reopened work, pending human gates, Keep and published documentation assets are protected; final screenshot/benchmark bytes expire by default but concise results persist.
- A3: Dry-run reports each candidate and reason without deleting; external paths, symlink escapes and unknown ownership are refused.
- A4: Close, discard and periodic/manual cleanup obey one policy; duplicate runs are idempotent and partial filesystem failures retain an accurate receipt and retry path.
- A5: Expired receipts retain artifact type, source/provenance, recorded result and expiry time. Reopening does not restore deleted bytes or mark evidence freshly verified.
- A6: Tests cover Keep-versus-GC and reopen-versus-GC races, missing files, cancellation, restart and terminal timestamp resets. CLI/API expose availability without changing historical proof verdicts.

Verification: `command: go test ./cmd/tusker -run TestRemainingArtifactRetention -count=1` (future suite).

## evidence-view — Show optional result artifacts, Keep and expired evidence in task detail

Outcome: task detail leads with what was achieved, verification and how to try it. Available screenshots/performance reports are easy to open; an expired attachment stays visible as a truthful receipt. Merely attaching a screenshot never creates a human approval gate.

Implementation scope and approach:
Use the artifact-retention task API and existing task inspector/Work components. Show compact artifact-type labels such as Screenshot and Performance report, availability, and Keep control where relevant. Use artifact metadata rather than freeform tags to determine opening/expiry behavior; ordinary project tags remain independent. Read-only viewers cannot mutate retention. Preserve existing preview behavior, focus/keyboard navigation and explicit human-gate actions. Avoid a new top-level dashboard. On expiry or stale link refresh, replace the open action with Evidence expired and date while keeping recorded outcome/provenance accessible. Render limitations separately from success. Add a bounded browser fixture for available, kept, expired, missing, failed and human-wait cases.

Ownership: internal/serve/ui/src/features/work. Inspect named files and callers before editing; file hints are starting points, not permission to replace sibling work. Shared CLI registration, capabilities and system-doc sections require narrow edits after rereading the latest tree.

Non-goals: no new scheduler, model harness, mandatory human code review, automatic start, formal active-spec revision locking, or implementation of the three assigned real-work test streams. Preserve existing task IDs and contracts. If behavior already exists, verify it and implement only the gap.

Work level: Standard; use configured execution/review profiles at start, never hard-coded model names.

Review: independent agent checks acceptance against changed material and actual test output. Human inspection is optional unless explicitly gated. Completion report leads with observed result, exact commands/PASS-FAIL, limitations and how to try it; no self-certified proof. Update affected current system documentation and shipped CLI help with the implementation.

Acceptance:
- A1: Available screenshots/reports can be opened from task detail with keyboard and accessible labels; Keep state round-trips through CLI/API and survives reload.
- A2: Expired/missing artifacts do not present broken active preview actions or erase the summary/provenance; the UI distinguishes expiry from unknown load failure.
- A3: Artifacts alone do not request human approval; a genuine configured human gate remains explicit and cannot be satisfied by merely viewing evidence.
- A4: Completed, partial and failed tasks show truthful result/verification/limitations without requiring the human to perform code review.
- A5: Browser assertions cover focus, Keep permissions, API failures and stale-state refresh; source-to-API tests cover the same availability contract used by CLI.

Verification: `command: go test ./cmd/tusker -run TestRemainingEvidenceView -count=1` (future suite).

## Import status and handoff

Wave W-0026 carries this work: it was imported atomically and its durable task records plus the wave's migration receipt are the authority (the original plan input was removed during the direct-authoring cutover). Current project capacity is one; the wave preserves concurrency=1. The two logical lanes above are independent, but do not raise runtime capacity implicitly.

The wave stays disarmed until a separate authorization decision. Until then, give each implementation agent the common boundaries plus its complete task section in this document. Existing task contracts remain the authority for the already assigned work.
