# AIE WF2026 → Tusker adoption plan

Status: draft for owner review · 2026-07-06
Author: planner session · Sources: three Obsidian vaults under `~/Downloads/AIE Talks/` (97 verbatim per-talk transcripts, 3 curated-note layers), 8 deep transcript mines, 66 additional talks triaged.
Method: curated notes used for triage only; every load-bearing claim below cites a talk + timestamp into the verbatim transcript. Timestamps are clickable in the vault transcript files.

## Provenance rule (read first)

Three separate "alpha" items in the curated summaries turned out to be **fabricated by the summarizer** — never said by the speaker:

1. Meta talk: the "95% safe completion / 0 unauthorized writes / replayable within 24h" SLO targets and the six-metric list (curator's hypothetical).
2. Starite talk: the `retrieval_outcome` table schema (curator's proposal).
3. Notion talk: the "a cheap model needing six retries isn't cheap" quote (curator's paraphrase; the real mechanism is trajectory-level evals).

Rule for everything derived from this corpus: **cite transcript timestamp or mark it as our own synthesis.** No spec may attribute a number to a speaker without a timestamp. (This is RUN-T-0002's provenance principle applied to research.)

## Priority order — why reliability comes before parallelism

Both the software-factories playbook layer and the harness-engineering track order the build the same way: trace capture + validation contracts + one bounded loop first; parallel orchestration and model routing last. Tusker's roadmap currently schedules parallel runs (RUN-T-0005) with no tracing story. Today's operations reinforced the ordering empirically: a retry self-block deadlock (RUN-T-0007), a silent daemon double-start crash (SQLITE_BUSY, no instance guard), and a config-source split earlier this week — all reliability failures, none parallelism failures.

Adopted sequence:

1. **Runtime durability** (extends RUN epic) — fixes for the failure classes we hit this week.
2. **Trace & replay** (new epic) — the missing substrate everything else builds on.
3. **RUN-T-0005 flow governor** — parallel runs, now with the control-theory governor it lacked.
4. **Learning loop** (FBK epic) — feedback → canon promotion mechanics.
5. **Serve control room** (SRV epic, already in flight) — supplement the UX packet.
6. **Routing & cost** — last, exactly as every source prescribes.

---

## Workstream 1 — Runtime durability (RUN epic, immediate)

Live evidence: RUN-T-0007 (retry self-block), feedback notes `2026-07-06-agent-sarav-retry-queued-self-block` and `2026-07-06-agent-sarav-daemon-sqlite-busy-no-instance-guard`.

Corpus support (round-2 mining will deepen all three):

- **Restate — Durable Execution** (harness vault, talk 19): event journal + resume-at-failure-point rather than restart; human approval as a durable *suspension* (no burned compute while waiting); per-session serialized execution; idempotent steps; durable retries/cancellation/locks. Direct blueprint for the daemon's lease/attempt machinery.
- **Amnara — The Log Is The Agent** (both vaults, talk 26/24): the run's identity is an append-only event log; workers are disposable — read log, advance one step, append, exit. Idempotent dispatch by construction; UI/debug/audit/compaction are projections of the log, and "compaction is lossy and never replaces the raw log." Tusker's V7 events design is already log-shaped; this argues for making the runtime store event-sourced too, so a crashed daemon reconstructs state from the journal instead of trusting mutable lease rows.
- **Meta — Production Evals** [6:43:54](https://youtu.be/htM02KMNZnk?t=24234): treat the harness like an SRE system — "reliability, availability, latency, cost, recovery" — reliability is the north-star metric. Today's incidents each map to a failure class in Meta's hierarchy [6:43:24](https://youtu.be/htM02KMNZnk?t=24204).

Candidate tasks (to author after round-2 mining):

- Single-instance guard + SQLITE busy_timeout/retry (from feedback note; small, dispatch soon).
- Scheduling fairness: parked continuations currently lose the slot to fresh dispatches (observed 2026-07-06, RUN-T-0001 vs FBK-T-0002) — starvation risk once RUN-T-0007's fix lands.
- Event-sourced runtime-store evaluation (design decision, needs Amnara + Restate transcripts first).

## Workstream 2 — Trace & replay (new epic)

The corpus' clearest composite spec, from Chronicle (talk 09) + the Evaluation & Replayability playbook:

- **Unit of instrumentation**: a *boundary* — "a bounding box around any node in your agentic workflow… a tool call… a call to an LLM or a retrieval… as long as it's a method" [2:09:03](https://youtu.be/htM02KMNZnk?t=7743). Record the typed input/output pair, model version, code version, state — "the entire state during which the agent run happened gets frozen and saved as a trace" [2:09:34](https://youtu.be/htM02KMNZnk?t=7774). Record in-process at the semantic boundary, not the network layer — "half your agent will never touch the network" [2:07:31](https://youtu.be/htM02KMNZnk?t=7651).
- **Field list**: Chronicle gives no schema; adopt the playbook's 17-field boundary schema (trace_id, work_item_id, node_id, node_type, input/output/error JSON, model provider/name/params, prompt_version, skill_versions, code_sha, tool_schema_version, permission_scope, retrieved_chunk_ids, created_at) as the starting DDL. *(Playbook synthesis, not speaker-attributed.)*
- **Replay modes**: live / frozen-model / frozen-external / full replay. Full replay = "a deterministic CI where you stop the model, you'll rerun the exact failure offline with zero model calls" [2:08:03](https://youtu.be/htM02KMNZnk?t=7683).
- **Equality target**: state transitions, not text — "we just need our system to execute the exact same state transition" [2:06:26](https://youtu.be/htM02KMNZnk?t=7586). Replay verification diffs state, never string-matches model output.
- **New verification-row type**: `replay:<trace-id>` — a regression gate proving a specific recorded failure can no longer recur. Slots into existing proof modes.
- **Why not determinism**: four first-principles reasons live re-runs can't reproduce failures (sampling ≠ system determinism; non-associative floating point; server-side batch grouping; MoE capacity routing) [2:05:24](https://youtu.be/htM02KMNZnk?t=7524)–[2:06:26](https://youtu.be/htM02KMNZnk?t=7586). Use verbatim in the epic's intent section to preempt the "just use temperature 0" objection.
- **SLO layer** (our synthesis, per provenance rule): task completion rate, tool correctness, cost per successful task, escalation rate, replay pass rate — grounded in Meta's spoken list [6:44:56](https://youtu.be/htM02KMNZnk?t=24296) and SRE dimensions [6:43:54](https://youtu.be/htM02KMNZnk?t=24234); the formulas are ours to define.

## Workstream 3 — RUN-T-0005 flow governor (contract addendum)

HumanLayer's control-loops talk (talk 28/29) supplies the exact governor RUN-T-0005 lacked:

- **Invariant**: "exactly one PR at most open per loop at a time. No stacking, no duplication" [7:02:15](https://youtu.be/htM02KMNZnk?t=25335). Contract field: `max_open_prs` (default 1) per loop/contract.
- **Placement**: the open-PR check runs *before* checkout and dependency install, so a blocked iteration costs nearly nothing [7:01:44](https://youtu.be/htM02KMNZnk?t=25304).
- **Backpressure semantics**: keyed to review state, not a timer — "no human reviewed the last output, so there's no reason to stack up even more work" [7:02:15](https://youtu.be/htM02KMNZnk?t=25335). Tusker translation: don't auto-land or open a new run for a contract whose prior output sits at an unresolved human gate.
- **Identity**: per-loop PR labels route feedback and scope the governor [7:00:41](https://youtu.be/htM02KMNZnk?t=25241) — maps to a per-contract run-label alongside file claims.
- **Batch knobs**: controller picks 3–5 items, but each is actuated "in its own context window, which will be both cheaper and more reliable" [7:02:45](https://youtu.be/htM02KMNZnk?t=25365). Contract fields: `batch_size`, `isolate_context_per_item`.
- **Companion ratchet**: before draining a backlog, land a CI "disturbance dampener" that blocks *new* violations against a committed baseline — "now that we've stopped the bleeding, we can actually design our controller" [6:57:06](https://youtu.be/htM02KMNZnk?t=25026).
- **Failure anecdote to cite in the contract**: unbounded loops during a travel week stacked duplicate, conflicting PRs nobody reviewed [7:01:13](https://youtu.be/htM02KMNZnk?t=25273).
- **Known gap the talk does not solve**: true concurrent auto-landing into the same base ("hopefully no conflicts"). Tusker's file-claims design goes beyond the state of the art here; Resonate's method applies — build a simulated implementation to discover the algorithm "under partial order and partial failure" before production code (agentic vault, talk 10).

Also fold in the sensor/controller/actuator template for recurring-migration contracts: deterministic sensor (ast-grep, "out of band" so agents can't disable it inline [6:56:05](https://youtu.be/htM02KMNZnk?t=24965)), deterministic controller (smallest-diff-first [6:57:36](https://youtu.be/htM02KMNZnk?t=25056)), actuator = agent + skill + hand-written golden patterns [6:58:38](https://youtu.be/htM02KMNZnk?t=25118), and the principle "I don't think you should ever send an agent to do deterministic code's job" [6:57:36](https://youtu.be/htM02KMNZnk?t=25056).

## Workstream 4 — Learning loop (FBK epic)

Composite mechanics from four independent sources that agree:

- **Feedback file** (HumanLayer): one version-controlled markdown file per loop, deterministically injected into the actuator context *after* controller selection, *before* actuation [7:00:10](https://youtu.be/htM02KMNZnk?t=25210). A `/iterate` comment on the PR loads full PR context and instructs the agent to fix the code **and** update the feedback file [7:00:41](https://youtu.be/htM02KMNZnk?t=25241) — the human correction is the promotion event.
- **Observer agent** (Warp): factory agents run skills; a *separate* observer watches how skills are applied — signal = senior engineers correcting the review agent's comments — and "improve[s] the code review agent for the next run" [5:09:19](https://youtu.be/htM02KMNZnk?t=18559). Reviewer must not improve itself inline; the observer is a distinct role.
- **Promotion threshold** (Starite): lessons stored as *reasoning*, not facts; negative lessons ("this column no longer exists") first-class [5:48:00](https://youtu.be/htM02KMNZnk?t=20880); at ~10 recurrences, bake the lesson into an always-loaded skill [5:47:28](https://youtu.be/htM02KMNZnk?t=20848) (their ablation: memory 66→76%, +skills →80%).
- **Retrieval weighting** (Starite): "retrieve by semantic similarity… weighted by whether those memories have historically helped or hurt the execution" [5:45:55](https://youtu.be/htM02KMNZnk?t=20755). Formula/weights/decay unspecified — ours to design.
- **Engines to evaluate in round 2**: GEPA (reflective text-space optimization over prompts/judges/harness files, Pareto candidate pool) and W&B Arya (production traces → tasks → evals → candidate agents → promote) — two different implementations of the same loop.

Tiering: per-loop feedback file = fast-changing staging; domain canon / golden patterns = durable tier; `feedback promote` graduates entries from the first to the second. Never let unreviewed content enter the durable tier (memory playbook rule).

## Workstream 5 — Serve control room (SRV epic supplement, hand to UX)

From AgentCraft (talk 23/24) — a working demo, mechanics verbatim:

- **Attention routing**: "you would click spacebar and it would just take you to the nearest thing that actually needed your attention… answer questions or approve plans" [5:31:10](https://youtu.be/htM02KMNZnk?t=19870). One global hotkey cycling through agents needing a human. Improve on their spatial "nearest" with ranked order: blocked > awaiting-approval > awaiting-answer.
- **Roster panel columns**: task / last action / current action / needs-attention flag — "everything I need to quickly understand who needs my attention and who doesn't" [5:30:08](https://youtu.be/htM02KMNZnk?t=19808).
- **Review kit contents**: full diff; file-by-file list; *videos* of what changed; screenshots; parallel implementations side-by-side, "pick the best" [5:33:46](https://youtu.be/htM02KMNZnk?t=20026). Justified at 5–20 concurrent reviews [5:33:16](https://youtu.be/htM02KMNZnk?t=19996). Visual-evidence-first so a reviewer can approve without reading logs.
- **File map**: files as "runes" carrying (agent, action, timestamp) [5:30:40](https://youtu.be/htM02KMNZnk?t=19840); heatmap as derived contention layer [5:31:10](https://youtu.be/htM02KMNZnk?t=19870). Speaker's own caveat: per-file granularity is "exhausting" [5:31:43](https://youtu.be/htM02KMNZnk?t=19903) — heatmap/aggregate is the default lens, runes the drill-down. This is the file-claims visualization for parallel runs.
- **Notice board**: one activity stream for humans and agents together [5:35:50](https://youtu.be/htM02KMNZnk?t=20150).

From Howie Liu (harness vault, talk 31): the fleet dashboard field list — goal, owner, role, step, blocked-on, cost, risk, next approval, artifacts, trace link — is the serve status-page requirements row.

## Workstream 6 — Routing & cost (last, per every source)

- **Pre-router deterministic gate** (Notion): "you don't need an LLM to turn a CSV into a PDF… to talk to tool calls if we have a CLI… to do deterministic SQL queries" [6:06:12](https://youtu.be/htM02KMNZnk?t=21972); LLMing plumbing is "where people become token poor." Highest-ROI single change for a frontier-only harness; zero routing infrastructure required.
- **Router shape** (Factory, four stages): per-role defaults → difficulty classifier (features verbatim: "structure of the prompt, of the codebase, how difficult the task is, what tools are being used" [2:17:34](https://youtu.be/htM02KMNZnk?t=8254)) → capability threshold → "choose the cheapest model above the threshold" [2:18:04](https://youtu.be/htM02KMNZnk?t=8284), upgrade-on-failure. Tusker advantage: the classifier's features already live in the task contract (risk tier, acceptance-table size, verification rows, tools) — route by reading the contract.
- **Reference points**: Notion's auto-router carries ~75% of traffic [6:02:31](https://youtu.be/htM02KMNZnk?t=21751); Factory claims a "very conservative" 25% cost saving [2:17:02](https://youtu.be/htM02KMNZnk?t=8222); validation consumed ~40% of a real 16-hour mission [2:22:11](https://youtu.be/htM02KMNZnk?t=8531).
- **Accounting**: evaluate whole trajectories, not single calls (Notion's Parallel eval [6:03:03](https://youtu.be/htM02KMNZnk?t=21783)); headline metric = cost per successful task; route at task granularity because mid-thread switches "kill the cache" [6:03:03](https://youtu.be/htM02KMNZnk?t=21783). Cost-metric schema drafted in the Notion mining report (task_class, model, tokens, retries, trajectory_success, wall_latency, usd_cost, derived cost-per-successful-task).
- **Risk mapping** (ours): risk tier as threshold floor — low/medium eligible for cheap tier; high/critical keep a frontier floor; aligns router with the reviewer auto-close boundary.

## Cross-cutting: slop control additions

- "No breadcrumbs" rule (community, 2026-07-06, dbreunig/dexhorthy thread): never reference what *was*, only what *is* — add the phrase to skill references (complements AGX-T-0003's terse-proof style and the no-attribution policy).
- Candidate validator: AST-walk comments → cheap-model classify → fail build on narrative/breadcrumb comments (_lopopolo's CI gate). Perfect first non-frontier-tier workload once routing exists.
- Reward-hacking defenses for proof modes (GPU-kernels talk, agentic vault 22): always evaluate end-to-end, never a single sub-metric; catalog of gaming modes worth a round-2 mine.
- Reviewer hazard named by the Loops Debate (harness vault 16): agents that "declare victory too early" — design the auto-close path to demand evidence the reviewer did not author (validator ≠ worker).

## Round-2 transcript mining shortlist

Harness vault: 19 Restate (durable execution), 26 Amnara (log-is-the-agent), 16 Great Loops Debate, 31 Howie Liu (fleet control plane), 30 Garry Tan (resolver table — feeds RUN-T-0002).
Agentic vault: 20 GEPA, 16 W&B Arya, 05 Arize (agent-as-judge reviewer), 31 Artificial Analysis (routing economics), 32 Arena (component RCT evals), 30 Addy Osmani, 22 GPU-kernel reward hacking.
Software-factories vault (round 1 leftovers): 26 BAML (dependency-graph CI enforcement), 31 Cursor (eval hardening: delete git history, network allow-lists).

Noted negative: no talk in 97 addresses config resolution with provenance — RUN-T-0002 proceeds on our own design, by analogy from event-sourcing (Amnara) and Garry Tan's resolver table.

## Immediate actions

1. Owner review of this plan (priority order + workstream scoping).
2. Dispatch round-2 miners (cheap, parallel, read-only).
3. Author the trace & replay epic + tasks from Workstream 2 (planner, after round-2 lands).
4. Amend RUN-T-0005's contract with the Workstream 3 governor fields before it dispatches.
5. Hand the Workstream 5 section to the UX team alongside the existing serve addendum.
6. Author the single-instance-guard/SQLITE_BUSY task (small, independent, can dispatch immediately).
