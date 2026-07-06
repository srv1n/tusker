# Operating Models for Continuous Multi-Agent Development

Research for Tusker — focus on the OPERATING MODEL (how work flows without a human gate stalling the pipeline), not runtime robustness (leases/retries/reconcile/budgets are already researched and out of scope here).

Date: 2026-07-06. Method: web search + fetch of primary source (raw GitHub docs, author blogs) and serious secondary analysis. Where a system is closed or undocumented, that is stated rather than padded.

---

## 1. Gas Town (steveyegge/gastown) — the operating model

Gas Town is Steve Yegge's open-source orchestrator, released Jan 2026, that runs "20 to 30 Claude Code instances working in parallel on the same codebase." Yegge frames it as "Kubernetes for AI coding agents." The prior Tusker pass already extracted its redispatch/lease governance; this pass is the operating model — roles, the Convoy unit, the Refinery merge queue, human-attention scheduling, and persistent identity.

### 1.1 The role system (from the README + Yegge's own words)

Two levels: **Town** (workspace, e.g. `~/gt/`, holds all projects) vs **Rig** (one git repo + its agents). Town-level roles coordinate across rigs; rig-level roles manage one repo.

| Role | Level | Job | Yegge's framing |
|---|---|---|---|
| **Mayor** 🎩 | Town | Your primary interface. Breaks user requests into work units, creates convoys, assigns/spawns polecats, holds full workspace context. | "your concierge and chief-of-staff" who "kicks off most of your work convoys" |
| **Polecats** 🦨 | Rig | Ephemeral worker agents with *persistent identity*. Each works in its own git worktree, does one bead, runs `gt done` to push a branch + create a merge-request bead. | "ephemeral per-rig workers that spin up on demand" to "produce Merge Requests" |
| **Refinery** 🏭 | Rig | The merge queue. Batches completed MRs, runs verification gates, lands to main one-at-a-time bors-style. | "the engineer agent responsible for intelligently merging all changes, one at a time, to main. No work can be lost, though it is allowed to escalate." |
| **Witness** 👁️ | Rig | Per-rig lifecycle monitor. Detects stuck/stalled polecats, triggers recovery (nudge or handoff), cleans up sessions. | "watch over them and help them get un-stuck" as polecats scale up |
| **Deacon** 👼 | Town | Supervisor daemon. Runs continuous patrol cycles across all rigs, checks health, dispatches Dogs, escalates what Witnesses can't fix, handles session recycling/handoff. | "daemon beacon" running "a patrol in a loop" |
| **Dogs** 🐕 | Town | Infra/maintenance workers the Deacon dispatches (e.g. "Boot" does triage). Not feature work. | "Deacon's personal crew" doing "maintenance and occasional handyman work" |
| **Crew** 👤 | Rig | Human-driven per-rig coding agents you manage directly. | "per-Rig coding agents … great for stuff like design work" |

**Load-bearing vs theater** (from retrospectives, §1.6):
- **Load-bearing:** Mayor (single coordination interface — the thing users actually praise), Polecats-in-worktrees (parallelism), Refinery (merge-collision avoidance), and the **bead** work-unit itself (git-backed, survives restarts — reviewers call this "genuinely useful beyond Gas Town itself").
- **Theater / cosmetic for a solo operator:** the Deacon/Dogs/Witness split is a *scaling* apparatus for 20-30 agents; at small scale it is one health-monitor loop wearing three hats. Crew (human agents) is optional. The "Mad Max" naming is flavor, not function.

### 1.2 The Convoy unit and lifecycle

A Convoy is "a special bead that wraps a bunch of work into a unit that you track for delivery" — a feature, tech-debt cleanup, or bug fix. It is a **persistent tracking unit** (dashboard object), explicitly distinct from the ephemeral *swarm* of polecats executing it.

Lifecycle is deliberately minimal — a **two-state model**:
- `OPEN` — active tracking, work in progress.
- `LANDED/CLOSED` — all tracked issues closed, notification (with duration + summary) sent to subscribers. Automatically **re-opens** if new issues are added.

Mechanics: `gt convoy create "Deploy v2.0" gt-abc bd-xyz` creates a town-level tracking bead (`hq-cv-*`), assigns polecats to the tracked issues (forming the swarm), and monitors **cross-rig dependencies without blocking**. Convoys labeled **`mountain`** get "autonomous stall detection and smart skip logic" for epic-scale runs. The convoy persists in history after landing.

Takeaway: the Convoy is the **wave/batch primitive** — a named, trackable batch of independent beads dispatched together, whose "done" is purely derived ("all children closed"), not a human sign-off.

### 1.3 Integration branches + the Refinery merge queue (the merge discipline)

This is the most transferable part of Gas Town. The problem: with a swarm, "the baseline can change so much during a swarm" that piecemeal landing to main causes ordering breakage:

```
Child A → MR → main (Tue)
Child B → MR → main (Wed, breaks A)
Child C → MR → main (Thu, depends on A+B together)
```

Solution — **integration branches**:
- Polecats spawn their worktrees **from the integration branch**, "so they start with sibling work already present."
- `gt done` / `gt mq submit` **auto-detects the parent epic** and targets the MR at `integration/{epic}` instead of main.
- The Refinery merges completed MRs into the integration branch; when **all epic children are closed**, it lands the integration branch back to base as a **single merge commit** to main.

**Refinery queue discipline = bors-style optimistic batch + bisect:**
- Batches pending MRs, runs verification gates on the *merged stack*.
- **Green** → all MRs in the batch merge.
- **Red** → **bisects** to isolate the failing MR, merges the good ones, kicks the bad one back (fix inline or re-dispatch to a polecat). A single failure does **not** roll back the whole batch.
- Polecats **never push to main directly** — all completion flows through the Refinery.

**Three-layer safety** so nothing bypasses the queue:
1. Role instructions forbid raw git; only `gt mq integration land` is authorized.
2. A `.githooks/pre-push` hook blocks pushes that introduce integration-branch content to the default branch unless `GT_INTEGRATION_LAND=1`.
3. The land command is **idempotent** — if it crashes after push but before cleanup, rerunning detects the integration branch is already an ancestor and skips to cleanup.

**Build pipeline** auto-injected from rig config into the verification formula, in order: `setup → typecheck → lint → test → build` (empty steps skipped silently).

### 1.4 How human attention is scheduled (the review model)

Gas Town does **not** gate progression on human approval. Humans are pulled in **by exception (escalation)**, on a severity ladder, plus a proactive digest at session start:

- Agents escalate rather than wait: `gt escalate -s <SEVERITY> 'desc'`. Escalation routes **Agent → Deacon → Mayor → Overseer(human)**, each hop tracked in bead comments.
- **Severity → channel routing:**
  - `MEDIUM (P2)`: bead + mail Mayor (no human page).
  - `HIGH (P1)`: bead + mail Mayor + **email human**.
  - `CRITICAL (P0)`: bead + mail Mayor + **email + SMS human**.
- Agents escalate only for: system errors, security issues, unresolvable conflicts, stuck loops. They do **not** escalate for normal workflow, recoverable errors, or info queries.
- **Proactive review:** on `gt prime`, the Mayor "displays pending escalations grouped by severity." The Deacon periodically runs `gt escalate stale` to re-escalate anything unacked past `stale_threshold: 4h` (bumping severity). `gt escalate ack <bead-id>` stops re-escalation.
- Problem triage surface: `gt feed --problems` highlights agents needing intervention; keys `n` (nudge) and `h` (handoff/context-refresh).

So the human is a **PM + on-call**, not a gate: "your job is to make tasks for it. That's it," plus "you have to help keep Gas Town running… stuff goes wrong often." It is explicitly "a hands-on-the-wheel orchestration system."

### 1.5 Persistent worker identity (agent = bead, not session)

Yegge's core idea: **"an agent is not a session. Sessions are ephemeral; they are the 'cattle.'"** The durable thing is a **Bead** with "a singleton global address," "a pointer to its Role Bead," and "its mail inbox." Work persists in git-backed hooks (worktrees) that survive crashes/restarts.

Recovery of context across session death uses **Seance**: `gt seance` lists discoverable predecessor sessions (via `.events.jsonl` logs); `gt seance --talk <id>` queries a predecessor for its decisions — a successor recovers intent without re-reading the whole codebase.

This is underpinned by **"Nondeterministic Idempotence"**: any single agent run is unpredictable, but the *outcome* eventually completes as long as you keep throwing sessions at the persistent identity + its hook. It's durable-workflow execution for LLM agents. **What it buys:** you can kill/restart a worker freely, redispatch is safe, and a batch converges without a human babysitting each session.

### 1.6 What worked vs what failed at scale (retrospectives)

From independent hands-on write-ups (Tenzin Wangdhen; paddo.dev):

**Worked**
- **Parallelism paid off:** "I slung 7 beads before dinner… Came back to find 6 PRs merged, and only one blocked on a dependency." 6/7 concurrent tasks landed unattended.
- **Mayor as single interface** was the thing people valued — beats juggling 10+ Claude sessions in terminal tabs.
- **Beads** (git-backed work units) let agents resume others' work without context loss — rated useful independent of Gas Town.

**Failed / rough**
- **Agents still needed manual prodding** to continue despite the design goal of autonomy ("agents still seem to need manual prodding to continue").
- **Process-cleanup blowup:** "141 orphaned Claude Code processes accumulated" (since patched).
- **Observability gap:** "Sometimes 6 PRs merged and I had no idea when I'd slung them" — no clear timeline of parallel merges.
- **The "murderous rampaging Deacon"** deleting code, forcing "**5 force pushes to main to recover**" — the autonomous supervisor is a real footgun.
- **"Yolo-mode" data loss:** occasional full-file overwrites, accepted as a throughput tradeoff ("most work gets done; some work gets lost" is *intentional*, recoverable via git).
- **Cost:** a "cash guzzler" needing multiple $200/mo accounts at 20-30 agents.

### 1.7 Minimum viable role set for a SOLO operator, 3-8 agents

From the above, the load-bearing subset collapses to **four functions** (roles, not necessarily four processes):

1. **Planner/Mayor** — one interface that turns your intent into a batch (convoy) of independent, worktree-isolated tasks. *(You already have this: the planner session.)*
2. **Workers/Polecats** — 3-8 agents, each in its own worktree, each producing a reviewable branch, never touching main directly.
3. **Refinery/merge-queue** — one serialized landing lane: batch green branches, verify the merged stack, land good ones, bisect-and-kick the bad one. This is the single most valuable thing to copy.
4. **Watcher + escalation** — ONE health loop (fold Deacon+Witness+Dogs into one) that nudges/handoffs stuck workers and pages you only by severity.

Skip at this scale: the Town/Rig two-level hierarchy (you have one repo), separate Deacon vs Witness vs Dogs, human "Crew" agents, and Mad Max flavor. Keep: worktree-per-worker, the bead-as-durable-identity, integration-branch + Refinery, severity-routed escalation.

---

## 2. The Ralph loop / "Ralph Wiggum" technique (Geoff Huntley)

Source: ghuntley.com/ralph, ghuntley.com/specs, and the `ghuntley/how-to-ralph-wiggum` playbook repo. This is the *single-agent unattended* pattern — the opposite pole from Gas Town's swarm, and the reference design for "run overnight from a spec."

### 2.1 The loop

```bash
while :; do cat PROMPT.md | claude ; done
```

That's it: infinitely feed a prompt file to a fresh agent instance. The agent reads a plan, does **one task**, validates, commits, and **exits** — then the loop restarts it with a **clean context window**.

### 2.2 Why fresh-context-per-iteration works

- The context window degrades as it fills: "~200K advertised = ~176K truly usable," and Huntley targets **40-60% utilization ("the smart zone")**. One tight task per loop keeps every iteration in the smart zone.
- The technique *deliberately* "burns the allocation of the specifications every loop" — re-reading the spec each time is accepted waste that buys coherence. Larger context allocation correlates with worse output.
- Ralph "doesn't remember previous attempts and will cheerfully make the same mistake twice" — so memory is **externalized to files on disk**, and repetition (plus prompt "signs") gets it right eventually. Philosophy: **"deterministically bad in a non-deterministic world"** — failures are systematic and therefore *tunable* via the prompt.

### 2.3 File layout & the two-mode prompt

```
project-root/
├── loop.sh                 # while-true orchestration
├── PROMPT_plan.md          # PLANNING mode instructions
├── PROMPT_build.md         # BUILDING mode instructions
├── AGENTS.md               # operational guide, ~60 lines MAX (build/test commands, conventions)
├── IMPLEMENTATION_PLAN.md  # prioritized TODO list = disk-persisted shared state between loops
├── specs/                  # one markdown file per feature/topic, loaded on demand
└── src/                    # the code
```

- **Two modes.** *Planning*: does gap analysis (specs vs code), outputs/refreshes `IMPLEMENTATION_PLAN.md` only — **no implementation, no commits**. *Building*: assumes a plan exists, picks tasks from it, implements, runs tests (backpressure), commits.
- **`IMPLEMENTATION_PLAN.md` is the durable state** carried between otherwise-isolated loops. It is **disposable**: "If it's wrong, throw it out and start over" — regenerating it costs one planning loop, cheap vs. Ralph going in circles.
- **`AGENTS.md` must stay operational-only.** "A bloated AGENTS.md pollutes every future loop's context." Status/progress belongs in the plan, not here.
- **Specs are loaded selectively** (one topic per file), so the first ~5,000 tokens each loop are the relevant spec — "every loop's context is allocated with the same files so the model starts from a known state."

### 2.4 One-task-per-loop + backpressure (the guardrails that matter)

- **One task per loop.** "You may relax this as the project progresses, but if it starts going off the rails, narrow it down to just one item."
- **Backpressure** = machine gates that reject bad work before commit: tests, typechecks, lints, builds. Strongly-typed compiled languages (Rust, Go) make the compiler a hard gate. The prompt says "run tests" generically; `AGENTS.md` supplies the real commands. For subjective quality, use **LLM-as-judge tests with binary pass/fail**.
- **Subagent fan-out with a serialized validation choke:** up to ~500 parallel read/search subagents, but **only 1 subagent for build/tests** — serializing validation prevents "bad-form back pressure." "Use the main context as a scheduler; spawn subagents whenever possible" so main context stays clean.

### 2.5 Completion detection

There is **no automatic completion threshold**. Signals: tests pass → `git add/commit/push`, semver tag bumps (0.0.0 → 0.0.1) when all builds/tests pass, and the plan's TODO list draining. In practice completion is **human-judged**: the YC-hackathon case hit "~90% automated, 10% human cleanup to finish." Ralph **cannot self-detect drift** from spec — "you must actively watch and monitor… drift detection requires you to recognize the issue and explicitly switch to planning mode."

### 2.6 Overnight failure modes + mitigations

| Failure | Mitigation |
|---|---|
| **Duplicate implementations** (ripgrep false-negative → reimplements existing code) | "don't assume not implemented"; search-before-change; use subagents to verify existence |
| **Placeholder/stub code** (model chases the compile reward) | "DO NOT IMPLEMENT PLACEHOLDER OR SIMPLE IMPLEMENTATIONS"; "if functionality is missing it's your job to add it" |
| **Context saturation → broken build** | `git reset --hard` and restart; or throw the compiler-error dump into a fresh model (Gemini/Opus) for a rescue plan |
| **Spec conflicts** (e.g. contradictory keyword defs) | Opus subagent w/ "ultrathink" reconciles specs; single source of truth, no migrations/adapters |
| **Going in circles / off-track** | Regenerate the plan (cheap); switch to planning mode |
| **Credential exposure running unattended** | **Sandbox it** — "running without a sandbox exposes credentials, cookies, SSH keys, tokens. Run in isolated environments with minimum viable access." |

### 2.7 "Specs as source code" / stdlib

Huntley's `/specs` + `/stdlib` method: you first have a **long conversation** with the model about requirements (not "implement X"), then **formalize it into spec files** that become the authoritative source. Paired with a compiler-sound typed language, this drives "hands-free output of N factor (entire weeks' worth) of co-workers in hours." The generated code stays under control via (a) your **technical standard library** (`src/lib`) steering generation patterns and (b) the specs. The operator's job is to **"sit on the loop, not in it"** — engineer the setup, then "tune it like a guitar… when Ralph fails a specific way, add a sign to help him next time."

**Relevance to Tusker:** Ralph is the canonical *unattended single-lane* protocol. Its transferable pieces: fresh context per unit, plan-file as durable state, one-task-per-loop, machine backpressure as the real gate, sandboxed execution, and "operator sits on the loop."

---

## 3. Review-gate design — the core question

The operator's question: *if a task is in review, why can't dependent work keep going? Human gates stall the pipeline.* The field has converged on three models. The honest answer: **for unattended runs, respected systems use a SOFT gate (machine-verified green + async/batched human review + revert-on-reject), not a hard human gate — but they keep a hard gate for a small high-risk tier.**

### 3.1 The three gate models

**(a) Hard gate** — a dependent task may not start until its dependency is **human-accepted**.
- Where used: nowhere as the default for autonomous swarms; it's the thing everyone is trying to escape. Anthropic's agent-teams model implements a *machine* version of this via **dependency markers** that "sequence work that can't run in parallel" — but that's a dependency ordering, not a human sign-off.
- Cost: exactly the operator's complaint — the human becomes the pipeline's rate limiter.

**(b) Soft gate** — dependent work proceeds **optimistically** the moment the dependency is **machine-verified green** (build/typecheck/lint/test pass); human review happens **async/batched**, and rejection triggers **rollback/revert** rather than blocking. This is the modern merge-queue consensus.

**(c) No gate** — commit straight to trunk; correctness is defended by **fast CI + a revert culture** (any red main is auto-reverted). Works only with excellent tests and low blast radius.

### 3.2 The merge-queue lineage (what (b) actually is)

**bors / bors-ng** (the origin, from Rust's "Not Rocket Science Rule": main must never be broken):
- Reviewed PRs enter a queue, tested against main by copying to a **staging branch**; on green, staging → main. This prevents **semantic merge conflicts** (two individually-green PRs that break when combined) — the exact hazard of a swarm.
- **Optimistic batching:** assume PRs pass; test them in **batches** for throughput.
- **Bisect on failure:** a red batch splits in two, re-queues both; a batch of one that fails is **kicked back to its author** to fix. (This is exactly Gas Town's Refinery.)

**Google TAP / trunk model:** two pointers to trunk — head, and "the latest commit verified to pass all tests." Consumers build off the *verified* pointer. **Revert culture:** a regression that slips in (or a canary trip) gets an **automated revert/revert-PR** to restore green fast.

**Graphite stacked PRs + merge queue:** stacking lets dependent change B build on not-yet-merged A as a **narrative stack**; the merge queue is **stack-aware** (can land a whole stack), and supports **parallel / optimistic / batched** modes. Remaining stack entries auto-rebase onto the new base after a land. This is the cleanest answer to "dependent work while parent is in review": **stack B on A, keep coding, land the stack through an optimistic queue.** GitHub shipped native Stacked PRs (2026) for the same reason. Trunk.io documents an explicit **"optimistic merging"** optimization in its merge queue.

### 3.3 What the agent orchestrators actually do

- **Gas Town = soft gate, done well.** Integration branch + Refinery: siblings build on each other's already-present work (spawn from the integration branch), land through a bors-style bisecting queue, human pulled in only by severity escalation. Dependent work does **not** wait for human approval; it waits (at most) for the *machine* verification of the merged stack. Its own retrospective shows the failure mode of over-autonomy: the "rampaging Deacon" and yolo overwrites — i.e. **no-gate on the wrong operations blows up**, which is why the merge lane stays serialized and hook-guarded.
- **vibe-kanban = soft gate with a human review column.** Each task runs in its own branch + worktree; a Kanban board with `planning → in progress → in review → done`. Multiple tasks run in **parallel optimistically**; after an agent finishes you **review the diff in the UI, then create the PR**. But dependencies are weak: "tasks exist as flat lists without understanding of what blocks what" — a known gap.
- **Anthropic agent teams = machine-sequenced, partitioned, verify-before-downstream.** Shared task list with **lock-and-claim** (a claimed task is marked in-progress and invisible to others — prevents double-work), **dependency markers** sequence non-parallel work, teammates **partition files** (each owns a disjoint set) since teammates are *not* worktree-isolated. Explicit rule: **"Don't assume subagent outputs are correct. Build orchestration logic that verifies or reviews outputs before passing them downstream"** — an agent-to-agent review gate, not a human one.

### 3.4 What practitioners converge on for overnight runs — and what blew up

**Converge on:**
1. **Never let an agent push to main directly.** Everything lands through a serialized queue (bors/Refinery). Universal.
2. **Machine verification is the real gate.** Green build+typecheck+lint+test on the *merged* state, batched + bisected. Human review is **async and batched**, not inline.
3. **Optimistic + revert beats blocking.** Stacked PRs / integration branches let dependents proceed; a rejected/red change is reverted or bisected out, not allowed to stall the queue.
4. **Isolate every worker** (worktree and/or container) so optimism is cheap to undo.
5. **Keep a small HARD-gate tier** for high-blast-radius changes (migrations, auth, prod config, dependency bumps) — human-accept before dependents build on them.

**Blew up:**
- **No-gate on privileged operations** — Gas Town's Deacon force-pushing/deleting code; yolo full-file overwrites. Revert culture only saves you if the operation is revertible and tests catch it.
- **Weak dependency modeling** — vibe-kanban's flat list means "optimistic" silently becomes "wrong order."
- **Observability gaps** — "6 PRs merged and I had no idea when" makes a morning review impossible; you can't review what you can't see.
- **Ralph overnight** — broken non-compiling codebases by morning when backpressure was too weak or the plan drifted; mitigated only by sandbox + strong gates + `git reset --hard`.

---

## 4. Other respected operating models for continuous agent dev

### 4.1 Dagger container-use (Solomon Hykes)

Open-source MCP server giving each agent an **isolated container + git worktree** — "containers for isolated execution, worktrees for isolated files." Multiple agents run without conflict because each has its own working dir/index over one shared repo. Human loop:
- **Observe:** `cu watch` = "a complete real-time log of what your agents are actually doing, not just what they claim. Every command and its output is recorded."
- **Intervene:** `cu terminal <env>` drops you into the container to take control.
- **Review/merge:** inspect with `git log --patch container-use/<env>` / `git diff container-use/<env>`, checkout locally, and `cu merge` when happy.
- **Queue model:** none built-in — humans merge each env; conflict prevention is 100% isolation-based.

### 4.2 sketch.dev (bold.software, Go outer-loop agent)

Each "sketch" runs in its **own Docker container** with a copy of the source; it's trained to **make git commits as checkpoints**, auto-pushed to the host repo as `sketch/*` branches. Parallel by construction (separate sandboxes; "it can trash its own container but not your machine"). You can SSH/VSCode into the container. Foundational essay: *"The Unreasonable Effectiveness of an LLM Agent Loop with Tool Use"* (the inner tool-use loop). Merge/queue discipline is **not** part of sketch — it stops at "produce a branch"; landing is on you. Documented at the loop/checkpoint level; **no documented merge-queue or multi-agent orchestration layer** — thin on the operating-model question specifically.

### 4.3 Anthropic's own multi-agent guidance (official)

Four surfaces, each a different parallelization style:
- **Subagents** — delegated workers in one session, own context, return a summary. Can each get their own worktree.
- **Agent view** (`claude agents`, research preview) — dispatch background sessions, each **auto-moved into its own worktree**, monitor one screen, step in only when one needs you. This is the closest official analog to a solo "morning review" surface.
- **Agent teams** (experimental, off by default) — a **lead** + teammates with a **shared task list + inter-agent messaging**; lock-and-claim, dependency markers, **file partition** (teammates aren't worktree-isolated, so each owns disjoint files).
- **Dynamic workflows** (`/batch`) — a **script** holds the plan and runs many subagents, **cross-checking their results**; the packaged `/batch` skill splits one change into "5 to 30 worktree-isolated subagents that each open a pull request."

Guidance worth adopting:
- **Team size sweet spot: 2-4 concurrent** ("beyond that, coordination overhead outpaces gains") — note this is *below* the operator's 3-8; treat 3-4 as the comfortable band and 8 as the aggressive ceiling.
- **Specialization beats generalization** — focused prompt + limited tools > one general session.
- **Verify outputs before passing downstream** — build review into the orchestration.
- **Routines** run a session on a cron **schedule in Anthropic's cloud** — the sanctioned "agent CI / overnight cron" primitive.

### 4.4 Overnight / "agent CI" patterns (cross-cutting)

The common overnight recipe across Ralph, Gas Town, container-use, and Anthropic routines: **(1)** a durable plan/backlog on disk, **(2)** workers isolated in worktrees/containers, **(3)** a while-true or scheduled dispatcher, **(4)** machine backpressure as the gate, **(5)** a serialized merge lane, **(6)** exception-only paging, and **(7)** a single morning surface that shows what landed, what's red, and what's blocked.

---

## 5. Synthesis — recommended operating model for a solo factory, 3-8 concurrent agents

Design goal: work flows overnight without a human gate stalling the pipeline, while keeping a narrow hard gate where a mistake is expensive. Every element below is justified by who proved it.

### 5.1 Wave / convoy structure
- **Batch work into named waves (Convoys).** A wave = a set of **independent, worktree-isolated tasks** dispatched together, whose "done" is derived ("all children landed"), not human-signed. *(Gas Town Convoy — 6/7 beads landed unattended before dinner.)*
- **3-4 workers is the comfortable band; 8 is the aggressive ceiling.** Above ~4, coordination cost rises fast. *(Anthropic 2-4 sweet spot; Gas Town's own value came from a handful, not the full 30.)*
- **One task per worker per unit, fresh context each unit,** with a durable on-disk plan (`IMPLEMENTATION_PLAN.md`-style) as the shared state between iterations. *(Ralph.)*
- **Partition by file ownership OR isolate by worktree** — never both workers in the same files. Prefer worktree-per-worker (or container-per-worker) so optimism is cheap to undo. *(container-use, sketch.dev, Anthropic worktrees; your own CLAUDE.md "disjoint file ownership" doctrine.)*

### 5.2 Merge discipline
- **No worker ever pushes to main.** Every worker produces a branch/MR; a **single serialized merge lane** lands them. *(bors rule; Gas Town Refinery; universal.)*
- **Integration branch per wave:** siblings spawn from it (so each sees prior sibling work), land into it, and the wave lands to main as **one merge commit** when all children are green. *(Gas Town integration branches — solves swarm ordering breakage.)*
- **Optimistic batch + bisect:** verify the *merged stack* (setup→typecheck→lint→test→build); green lands the batch, red **bisects** to isolate and kicks the one bad MR back, good ones still land. *(bors/bors-ng; Gas Town Refinery.)*
- **Stack dependent work** rather than waiting: if task B needs not-yet-landed A, stack B on A and keep coding; the queue rebases the stack after A lands. *(Graphite / GitHub Stacked PRs.)*
- **Guard the lane** so nothing bypasses it: role instructions forbid raw git to main; a pre-push hook blocks default-branch pushes except the authorized land command; make land **idempotent**. *(Gas Town three-layer defense.)*

### 5.3 Gate policy — soft by default, hard by risk tier
Answer to "why can't dependent work keep going?": **it can — through a soft gate.** Machine-green is the gate; human review is async/batched; rejection triggers revert/bisect, not blocking. Tier by blast radius:

| Risk tier | Examples | Gate |
|---|---|---|
| **Low** | tests, docs, internal refactors, additive feature code with good coverage | **No/soft gate.** Land on machine-green; dependents proceed optimistically; revert on morning reject. *(trunk + revert culture; Ralph backpressure.)* |
| **Medium** | new modules, API surface, cross-cutting changes | **Soft gate.** Optimistic land into integration branch; dependents stack on it; **batched human review** at wave boundary; bisect/revert if rejected. *(Gas Town; Graphite.)* |
| **High** | schema/migrations, auth, prod config, dependency bumps, deletes/force-push-class ops | **Hard gate.** Human-accept before dependents build on it; never autonomous. *(Directly because Gas Town's autonomous Deacon force-pushing/deleting and yolo overwrites blew up — no-gate on privileged ops is the documented catastrophe.)* |

Backpressure is the real gate at every tier: compiler + typecheck + lint + test + LLM-as-judge for subjective quality. *(Ralph; Anthropic "verify before downstream.")*

### 5.4 Overnight-run protocol
1. **Freeze a wave** the evening before: a convoy of independent, spec-backed tasks with a durable plan file; high-risk items **excluded** from the autonomous set (they wait for the morning hard gate). *(Ralph specs; Gas Town convoy.)*
2. **Sandbox everything.** Each worker in its own worktree/container with minimum-viable credentials — "running without a sandbox exposes credentials, cookies, SSH keys, tokens." *(Ralph; container-use; sketch.)*
3. **Dispatch + let it run** on a while-true/scheduled loop with fresh context per unit; **serialized validation** (one build/test lane) to avoid back-pressure thrash. *(Ralph; Anthropic routines for the cron surface.)*
4. **Serialized merge lane runs continuously:** integration branch + optimistic-batch + bisect; nothing reaches main un-verified. *(bors/Refinery.)*
5. **Exception-only paging on a severity ladder:** MEDIUM = logged for morning; HIGH = notify; CRITICAL (security, unresolvable conflict, privileged-op request) = page immediately. Auto-reap stuck/zombie workers; re-escalate stale-unacked. *(Gas Town escalation.)*
6. **Guarantee a durable audit trail** (who landed what, when) — the morning review is impossible without it. *(Directly because Gas Town's "6 PRs merged and I had no idea when" was the top observability complaint.)*
7. **Convergence, not hard-kill:** to wrap a wave, tell workers "land what compiles, skip the suite, return your file list" rather than killing mid-edit. *(Your own fan-out doctrine; Gas Town's nondeterministic-idempotence — redispatch is safe.)*

### 5.5 The morning review ritual
A single surface (agent-view / dashboard style) that on open shows **three lists**: **landed** (merged this run), **red/blocked** (bisected-out or escalated, with the failing gate), and **pending hard gate** (high-risk items awaiting your accept). *(Gas Town `gt prime` "pending escalations grouped by severity"; Anthropic agent view.)*

Then, in order:
1. **Triage escalations first** (severity-sorted) — ack or act. *(Gas Town.)*
2. **Batch-review the diffs** for what landed overnight — this is your async review of the soft-gated work, at the wave boundary, not per-task. *(vibe-kanban review column; your own "batch review at wave boundaries" model.)*
3. **Accept or revert** high-risk / rejected items: hard-gate items you approve now unblock their dependents; anything you reject gets an **automated revert/revert-PR**, main stays green. *(Google TAP revert culture.)*
4. **Retune, don't micromanage:** where a worker failed a specific way, **add a "sign"** (a prompt/spec/stdlib rule) so the next run doesn't repeat it — "sit on the loop, not in it." *(Ralph.)*
5. **Seed the next wave** from what drained and what got kicked back. *(Ralph plan regeneration; Gas Town convoy re-open.)*

**One-line thesis:** run a Gas-Town-shaped factory (convoy waves, worktree workers, integration-branch + bors-bisect Refinery, severity escalation) with Ralph-shaped workers (fresh context, one task, durable plan, machine backpressure, sandboxed), gate **soft by default / hard only for privileged-blast-radius ops**, and make the human a batch reviewer + on-call at the wave boundary — never an inline gate.

---

## Sources
- Gas Town README (roles, convoy, refinery, escalation, identity): https://raw.githubusercontent.com/steveyegge/gastown/main/README.md
- Gas Town integration branches (merge discipline): https://raw.githubusercontent.com/steveyegge/gastown/main/docs/concepts/integration-branches.md
- Gas Town convoy concept/lifecycle: https://raw.githubusercontent.com/steveyegge/gastown/main/docs/concepts/convoy.md
- Gas Town escalation design (human attention): https://raw.githubusercontent.com/steveyegge/gastown/main/docs/design/escalation.md
- Steve Yegge, "Welcome to Gas Town": https://steve-yegge.medium.com/welcome-to-gas-town-4f25ee16dd04
- Tenzin Wangdhen, "Gas Town: The Good, The Bad, The Ugly": https://tenzinwangdhen.com/posts/gastown-good-bad-ugly/
- paddo.dev, "GasTown and the Two Kinds of Multi-Agent": https://paddo.dev/blog/gastown-two-kinds-of-multi-agent/
- Geoff Huntley, "Ralph Wiggum as a software engineer": https://ghuntley.com/ralph/
- Geoff Huntley, "/specs" (Groundhog / design-doc to code): https://ghuntley.com/specs/
- ghuntley/how-to-ralph-wiggum playbook: https://github.com/ghuntley/how-to-ralph-wiggum
- ZeroSync, "The Ralph Loop: Long-Running AI Agents": https://www.zerosync.co/blog/ralph-loop-technical-deep-dive
- bors-ng: https://github.com/bors-ng/bors-ng ; bors.tech: https://bors.tech/
- Graphite, "Not Rocket Science — Bors and Google's TAP": https://graphite.com/blog/bors-google-tap-merge-queue
- Trunk.io optimistic merging: https://docs.trunk.io/merge-queue/optimizations/optimistic-merging
- GitHub Stacked PRs: https://www.infoq.com/news/2026/04/github-stacked-prs/
- Dagger container-use: https://dagger.io/blog/agent-container-use/ ; InfoQ: https://www.infoq.com/news/2025/08/container-use/
- sketch.dev / boldsoftware/sketch: https://github.com/boldsoftware/sketch ; https://sketch.dev/blog/agent-loop
- Anthropic — Run agents in parallel: https://code.claude.com/docs/en/agents
- vibe-kanban (BloopAI): https://vibe-kb.com/ ; review by Eleanor Berger: https://elite-ai-assisted-coding.dev/p/vibe-kanban-tool-review
