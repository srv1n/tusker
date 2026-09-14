# W-0023: close the authoring-to-execution gaps

## Result of the audit

The product has most of the underlying authorities, but the authoring and start paths do not enforce one complete contract. More ticket front matter alone will not fix it. The architect must make task decisions; Tusker must carry them through import, preview, admission, execution and review. A ticket repaired after a user reminder is a failed first-pass authoring result.

This document and FLW-T-0037 through FLW-T-0041 began as the audit handoff. Their source changes are now integrated in this checkout. The original [recovery bundle](handoff/README.md) remains historical evidence of the starting point; current source and the owner reports below are authoritative. Product qualification still requires the separately reported browser and installed-runtime lanes.

## Evidence and accountable fixes

These are audit observations from this checkout/session, not claims about every installed build. Recheck shared files before editing.

| Gap observed | Existing authority / evidence | Required change | Owner |
|---|---|---|---|
| Tier choice required a user correction | ACO author supplied named-model advice; all ten now contain levels, but only after prompting. Existing work_level already expresses the choice. | Make explicit classification part of fresh authoring, typed intake and packet checks. Do not make the architect select a vendor model. | FLW-T-0038 |
| Play did not identify a usable selected profile | All 20 ACO execute/review route previews were blocked; all six tier lane mappings were empty. Generic wave runner availability could still look green. `runner_profiles.go` already resolves precedence. | Resolve both lanes per task at preview and start; expose exact blockers and actual admitted identity. | FLW-T-0037 / 0040 |
| Architect identity was prose, not an operational route | Execution list empty; ACO native conversation ID in notes. `agent_contacts.go` owns roles; execution ledger owns native endpoint state. | Capture host-reported author/origin once, project it into tasks, bind through existing authority, display unbound state honestly. | FLW-T-0039 |
| Architect doing its own ticket was ambiguous | Current conversation, design contact, chosen Play profile and implementation claim are different facts. | Explicit current-conversation claim with actual executor, preserved author/origin and independent review. | FLW-T-0039 / 0040 |
| Authoring did not finish the visible wave handoff | User had to ask for the original wave. W-0023 import itself wrote only an import receipt into its visible body (`delivery_cmd.go` waveBody rendering). | Batch intake creates a readable wave brief with outcome, ordered linked members, blockers and closure criteria. Preserve it on eligible re-import. | FLW-T-0038 / 0040 |
| Persisted metadata obscured the actual authoring contract | ACO records approximately 42-43 keys versus 16-18 typed input keys. IDs, revision/proof/lifecycle fields are machine responsibilities. | Simplify authored input and omit verified empty optional output. Keep useful filters/provenance and historical compatibility. | FLW-T-0038 |
| Naive trimming could change policy | Absence of evidence_budget removes a cap; zero is meaningful. Explicit false policy and revisions also matter. | Per-field consumer/default audit; never blanket-delete false/zero or rewrite historical records. | FLW-T-0038 |
| Verification projections disagreed | waveMaterialTable used raw pipe splitting; existing v7MarkdownTableCells preserves escaped regex alternatives used by ACO checks. | Reuse the existing parser and prove literal commands survive all projections. | FLW-T-0037 |
| UI surfaces exposed different capabilities | TaskRouting/RouteFact already exist; inspector has contacts absent from full detail; API supports yield input not passed by UI. | Reuse existing facts/components; align task, inspector and wave controls with real supported backend operations. | FLW-T-0040 |
| Passing prerequisites could be mistaken for product proof | Some ACO browser/live outcomes mapped build/offline prerequisites with live procedure only in notes. | Require actual browser/live evidence for those outcomes and fail on zero matched tests. | FLW-T-0041; preserve ACO qualification ownership |

## Product decisions for implementation

- The architect selects **Tier 1 / Light, Tier 2 / Standard, or Tier 3 / Demanding** through `work_level`. Configuration resolves harness, model and effort. Review inherits unless deliberately overridden with a reason.
- Human responsibility is a named human action/gate with owner and required action. It is not a fourth model tier. Mixed work contains agent tickets and the human gates that block them.
- Author once: outcome, decisions, meaningful scope/dependencies, acceptance/checks and real architect provenance. Inherit shared context; generate machine bookkeeping. Keep explicit per-task overrides.
- **Standalone tickets remain valid.** Batches must include a wave without another prompt. A one-ticket wave is optional when useful.
- Before Play, show actual worker and reviewer profiles and blockers. Revalidate at start; retain historical attempt identity when Settings changes.
- Self implementation is an explicit authorized work claim, separate from configured-harness Play. It does not bypass independent review.

## Implementation order and handoffs

| Order | Ticket | Execution tier | Deliverable passed onward |
|---|---|---|---|
| 1 | [FLW-T-0037](../../../.tusker/work/tasks/FLW-T-0037.md) | Tier 3 | Shared task/wave route and admission behavior, literal proof preservation, failure matrix. |
| 2 | [FLW-T-0038](../../../.tusker/work/tasks/FLW-T-0038.md) | Tier 2 | Enforced fresh authoring, compact metadata, named human gates, complete wave brief, matching skill/help. |
| 3 | [FLW-T-0039](../../../.tusker/work/tasks/FLW-T-0039.md) | Tier 3 | Provenance/contact binding and self-claim facts, with ownership/replacement checks. |
| 4 | [FLW-T-0040](../../../.tusker/work/tasks/FLW-T-0040.md) | Tier 2 | Consistent task/inspector/wave experience and a built-browser walkthrough. |
| 5 | [FLW-T-0041](../../../.tusker/work/tasks/FLW-T-0041.md) | Tier 2 | Fresh unprompted authoring experiment and end-to-end product evidence; defects returned to their owner. |

Reviews are explicitly Demanding because routing, schema and identity claims affect execution authority. The serial dependency order is deliberate: shared backend contracts settle before UI and trial. Each ticket contains the source entry points, implementation steps, proof mapping and output report. No named model is a routing instruction.

## Surrounding ownership and escalation

W-0017 / ACO-T-0001..0010 owns persistent architect messaging, yield/resume, corrections, stalled-wave reports and automatic continuation. This wave connects authoring and presentation to those authorities; it does not build another coordinator. FLW-T-0031/0033/0034 own overlapping skill/documentation work. Shared runner/daemon/ACP/UI files already contain other work: inspect current diffs and agree on owned edits before proceeding.

Author/origin reported by the host for this handoff: native Codex conversation `01a08f6f-7bc8-7551-a17b-4e506b62bf87`, host `local`. This is descriptive provenance, not an authenticated identity or registered Tusker execution address. Return material decisions through an authorized supported host route or the operator; never invent a transport binding.

## When this wave is actually done

A fresh frontier author receives settled intent once and produces classified work, a human action, real author provenance and a readable wave without corrective reminders. Task and wave preview identify the same profiles that actually execute/review. Missing mappings and human gates prevent inappropriate starts. A failed check is corrected and independently reviewed. Self implementation preserves both executor and author identity. A standalone task works without a synthetic wave. Evidence names the installed build, configuration, task/attempt identities and actual UI/runtime observations.

The real-project audit had empty mappings and daemon/project/workflow/setup blockers. Import validity is not readiness. Fixture and browser results do not close live qualification; an unavailable installed trial stays NOT RUN. Keep the wave disarmed until the separate execution authority authorizes it.
