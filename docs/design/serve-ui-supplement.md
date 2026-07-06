# Serve UI supplement: data contract, attention routing, review batching

Audience: whoever is building `internal/serve/ui` and the serve backend. Companion to SRV-T-0001 (serve design spec) and the adoption plan workstream 5. Sources: the 2026-07-06 mock-build screenshot review, `docs/design/aie2026-mining/serve-config.md` (Howie Liu, Talk 31), `docs/design/aie2026-mining/factories-leftovers.md`, and the operator's stated review policy (batch review at wave boundaries; no per-task human gates).

## 1. Verdict on the current mock build

The 2026-07-06 screenshot has the right shape and the wrong data. Layout regions (stat strip, epic chips, status columns, blockers rail, active-runs rail, daemon footer) all map cleanly onto real Tusker data sources — nothing structural needs to change. But every card on screen was fabricated, and the fabrications diverge from reality in ways that will mislead design decisions:

| Mock showed | Reality (same moment) |
|---|---|
| AGX-T-0003 "Workflow runner lease + liveness protocol", in progress | AGX-T-0003 is "Cap changelog and knowledge-delta churn by risk tier", status done |
| RUN-T-0009, RUN-T-0012 | Do not exist; RUN ends at RUN-T-0008 |
| FBK-T-0004 "Feedback digest generator", blocked/rework | FBK-T-0004 is "Add Tusker and ChatGPT handoff onboarding doctor", ready |
| Epic chips AGX 12 / CLN 10 / RUN 7, no TRC | AGX 6 / CLN 7 / RUN 8 / FBK 5 / SRV 4 / TRC 3 — the TRC epic is missing entirely |
| "4 needs you" | Zero items genuinely need the operator; the one review-state task (CLN-T-0001, low risk) belongs to the reviewer agent |
| Active runs 3 | Active runs 4, queued 3 |

**Requirement: generate mock fixtures from a real vault snapshot** (`.tusker/work/**` frontmatter plus a `tusker automation queue --json` capture), never hand-write them. Hand-written fixtures encode wrong title lengths, wrong status distributions (real boards are ready-heavy, not in-progress-heavy), missing epics, and — worst — a fictional model of what "needs you" means. A `tusker serve fixtures --snapshot` subcommand (or a checked-in snapshot JSON) keeps the mock honest as the vault evolves.

## 2. Data contract: every region has exactly one source of truth

| UI region | Source | Notes |
|---|---|---|
| Task cards, statuses, epics, priority, risk | Vault frontmatter (`.tusker/work/tasks/*.md`) | Typed frontmatter is source of truth; tags are projections |
| Epic chips + counts | Vault, grouped by `epic` | Must include every epic present in the vault — no hardcoded list |
| Active runs, lease states, attempt counts | Daemon runtime store via daemon API | Never inferred from task status; `active` is not a durable task status |
| Blockers / readiness reasons | `tusker automation queue --json` `blockers[]` strings | Render verbatim; they are already agent-legible |
| "Needs me" panel | Derived — see section 3 | Computed, never manually flagged |
| Token/stat strip | Daemon store (attempts, turns) | AGX-T-0004 instruments this; show "—" until real |
| Daemon footer | Daemon liveness (pid + last poll timestamp) | Show last-poll age, not just a green dot — a live pid with a stale poll is a wedge |
| Settings surfaces | Config resolver with provenance (RUN-T-0002 A6) | Every setting labeled with its winning source; writes go to user-global or `tusker.local.yaml`, never the committed `tusker.yaml` |

The mock's "dead / stale / fresh" per-run health labels are a good invention — keep them, backed by last-heartbeat age from the runtime store (mining N7: roster needs a "last heartbeat / next wake" field to distinguish idle-alive from dead).

## 3. "Needs me": a derived signal with a closed definition

The operator's review policy: low/medium-risk tasks are auto-closed by the reviewer agent; there are no per-task human gates by default; human review happens in batches at wave boundaries. So "needs me" is narrow and computable. An item enters the panel iff at least one of:

1. **High/critical review** — `status=review` and `risk ∈ {high, critical}` (reviewer cannot auto-close).
2. **Human-owned gate** — an unsatisfied gate whose owner is human.
3. **Rework ping-pong** — a task that has bounced runner→reviewer→rework ≥ 2 times (the loop is not converging; a human decision is cheaper than a third lap).
4. **Terminal run failure** — a run that exhausted retry attempts with no lease able to continue.
5. **Wave boundary reached** — the batch-review trigger itself (section 4), presented as one card, not N.

Explicit non-signals: low/medium tasks in review (reviewer's job), dep-blocked tasks (the dependency's job), capacity-blocked ready tasks (the queue's job), and interrupted leases that were deliberately parked. The mock's "4 needs you" would have been 0 under this definition — that gap is the difference between a control plane the operator trusts and one they learn to ignore. False "needs you" counts train the operator to skip the panel, which defeats attention routing entirely (mining N4: the UI's job-to-be-done is "route the operator to the next agent that needs unblocking").

Every needs-me card carries its one-click resolution action (section 6) and names what it blocks — the mock's "blocks 4" affordance is right, keep it.

## 4. Review batching: the wave boundary

A **wave boundary** fires when either (a) the set of runs that were active together has fully drained to review/done/failed, or (b) an epic's dispatched tasks have all left in-progress. At the boundary, serve assembles one batch card:

- Grouped by epic; per task: capsule frontmatter, verification-row results (command + PASS/FAIL, as recorded), risk tier, and what the reviewer agent already auto-closed vs what waits.
- The operator's batch actions: accept-all-remaining, per-task rework with a one-line reason, or open the drill-down.
- Auto-closed low/medium tasks appear as a collapsed "closed by reviewer: N" line with expansion — visible for audit, not for re-review.

This is the operator's primary review surface; per-task detail is the zoom, not the landing page (mining N3, "Sim City": macro fleet map default, micro drill-down as the exception).

## 5. Roster: three first-class columns plus handoff edges

From Howie Liu (Talk 31, N1/N2): the roster's first-class fields are **working-on**, **blocked-on**, and **handing-off-to** — stored/served as fields, not derived in the client. Blocked-on gets its own filter; the operator scans for blocked agents, not running ones. Handoffs (runner → reviewer, task → dependent task) render as edges between roster rows (N10) — they are the connective tissue of the fleet view, and the chain intake → runner → reviewer → human-approval node is the canonical shape (N16): the approval node is where attention routes.

## 6. Unblock actions: every panel item maps to a CLI verb

The UI must never invent a mutation path; every button is a thin wrapper over an existing verb, and every write goes through the setter-with-readback rule (RUN-T-0002 A5 — `ok:true` no-ops are forbidden):

| Situation | Action | Verb |
|---|---|---|
| High/critical review accepted | Close | `tusker close <id>` (close policy enforced) |
| Review rejected | Rework + reason | `tusker status <id> rework` with note |
| Human gate | Satisfy + evidence | `tusker gate satisfy --evidence "<stmt>"` |
| Rework ping-pong | Amend contract, requeue | Edit task + `tusker reconcile` |
| Terminal failure | Requeue or park | Supervisor continue / explicit park |
| Friction observed | File it | `tusker feedback add` |

A conversational approval to the planner is a valid gate-satisfaction path (established policy: the operator's statement is recorded as evidence). The UI and the planner write through the same verbs, so neither surface drifts.

## 7. Build order implication

Sections 2–3 are the accuracy core: real vault + queue data behind the existing layout, and the needs-me derivation. That is SRV-T-0002 (read-only MVP) territory and is worth pulling forward — the board layout is already right; what's missing is truth. Batching (4) and roster edges (5) layer on the daemon's run/attempt data. Actions (6) come last and depend on the settings write-path rules in RUN-T-0002/SRV-T-0004.
