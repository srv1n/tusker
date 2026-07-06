# AIE World's Fair 2026 — Durability & Loops Mining

Transcript-mined tactical detail for Tusker. Every load-bearing claim is quoted verbatim with its timestamp link. Auto-caption transcription errors are preserved as-is (e.g. "restate", "law log", "cued", "fabulous", "GP55"). No claim is attributed without a timestamp.

Source files (in `/Users/sarav/Downloads/AIE Talks/AI_Engineer_WF26_Obsidian_Vault/Transcripts/`):
- `19 - Durable Execution for Production Agents - Restate.md`
- `26 - The Log Is The Agent - Amnara.md`
- `16 - Great Loops Debate.md`

Tusker anchors referenced: **TRC** (boundary trace records, replay modes), **RUN-T-0008** (daemon durability), **event-sourced runtime store** (currently SQLite rows), **retry/continuation policy** (max_turns:1 + continue_thread supervisor), **loop budgets / stop conditions** for a reviewer agent, **declare-victory-too-early** reviewer hazard.

---

## Talk 19 — Restate: Durable Execution for Production Agents

Format: verbatim quote · timestamp · Tusker anchor.

### R1 — Four ingredients of a durable agent runtime
> "Basically four parts. First of all, it makes sure that a single run of an agent is resilient. This is called durable execution in the industry... Another um area here is running many concurrent sessions in parallel... And then going more towards things like communication between agents, between agents and MCP servers and other tools. And finally also control, making sure that when an agent... is doing um something you don't want it to continue or when it's stuck being able to actually cancel or kill the execution."
· [5:07:43](https://youtu.be/I2cbIws9j10?t=18463) / [5:08:14](https://youtu.be/I2cbIws9j10?t=18494)
· **Anchor:** RUN-T-0008 scope checklist — durability, concurrency isolation, inter-agent comms, and *control/kill* are four separable daemon concerns, not one.

### R2 — The exact failure Tusker durability is for
> "Think about things like when an agent runs for a week and then crashes. We want to be able to bring it back and let it continue exactly at the point where it failed. We don't want it to start over from the beginning."
· [5:07:43](https://youtu.be/I2cbIws9j10?t=18463)
· **Anchor:** RUN-T-0008 durability thesis stated in one line; "continue exactly at the point where it failed" = TRC replay-to-failure-boundary mode.

### R3 — Proxy-in-front + connection-as-lifeline + journal
> "Restate is basically a server which runs in front of your agent service... a bit like a like a message broker or a proxy... from that moment there's a connection open connection between restate and the agent and that connection will basically be a bit like a lifeline for the agent. So as the agent is doing stuff, it sends events over to restate and restate will use that journal of events to recover the process after a failure."
· [5:08:45](https://youtu.be/I2cbIws9j10?t=18525) / [5:09:16](https://youtu.be/I2cbIws9j10?t=18556)
· **Anchor:** event-sourced runtime store — the daemon should sit as a proxy that *receives* an event journal from the runner, rather than polling SQLite rows after the fact.

### R4 — Durable wrapper turns a plain function long-running
> "you could say that it's turning a normal function in your application into something that is longunning, durable, and stateful without having to do um a lot of the complex things you otherwise need to do for this."
· [5:09:16](https://youtu.be/I2cbIws9j10?t=18556)
· **Anchor:** design goal for the Tusker runner wrapper — durability as a decorator, not a rewrite.

### R5 — Deep-research topology = planner → parallel sub-agents → writer
> "this is basically like the classical deep research workflow, right? You have a planner then a set of subress research agents and then finally someone uh who writes a report on this like a writer agent and so this journal you see here on the left that is basically the events that get sent from the agent to the restate server and if this now crashes at some point this journal is what will be used to uh recover the execution to the point where it failed."
· [5:10:50](https://youtu.be/I2cbIws9j10?t=18650)
· **Anchor:** fan-out orchestration shape (matches Tusker's disjoint-partition doctrine); journal-as-recovery boundary.

### R6 — Injected tool error, retried from journal, not restarted
> "one of the web searches didn't go through because the API was down and then you see here on the right how it got retrieded and eventually completed successfully. So instead of starting over it uses the journal to recover the progress."
· [5:11:21](https://youtu.be/I2cbIws9j10?t=18681)
· **Anchor:** retry/continuation policy — a failed tool step retries against the recorded journal position; the rest of the run is *not* re-executed. Directly relevant to max_turns:1 + continue_thread (each step is independently replayable).

### R7 — Durable step = wrap in `restate.run`; recover "two hours or two months later"
> "the way I made it durable is by wrapping it in restate.run. So what happens is by doing these durable steps if this fails somewhere here two hours or two months later it will recover to exactly that point... You're always able to recover a process to where it was."
· [5:12:22](https://youtu.be/I2cbIws9j10?t=18742) / [5:12:53](https://youtu.be/I2cbIws9j10?t=18773)
· **Anchor:** TRC replay modes — each recorded step is a durable checkpoint; replay reconstructs to the exact step boundary regardless of elapsed time.

### R8 — Durable promise as a suspension point for a human gate lasting weeks
> "imagine we want to ask a human to approve something and this approval might take weeks or a month. this process needs to be able to um to survive restarts and redeploys over those kind of long periods of time... we we create a durable promise which lives in that journal a bit like a suspension point... while we are waiting this process actually suspends. So if it's running on serverless this is not using uh execution uh time on our functions."
· [5:13:24](https://youtu.be/I2cbIws9j10?t=18804) / [5:13:54](https://youtu.be/I2cbIws9j10?t=18834)
· **Anchor:** Tusker human-gate durability — a pending approval must survive daemon restart/redeploy as a journal-resident suspension point, consuming zero compute while blocked. Compare Amnara A13 (permission prompt must persist).

### R9 — Karpathy framing: agent is a persistent stateful entity, not a workflow
> "when we think about agents and also the way that Karpathy described it in the tweet, it's more like a persistent stateful entity that lives for a longer period of time that has some memory. Um, so a workflow is not the nicest way to model this kind of thing."
· [5:13:54](https://youtu.be/I2cbIws9j10?t=18834)
· **Anchor:** event-sourced runtime store vs. workflow rows — argues against modeling a Tusker task run as a static workflow; model it as a stateful entity with memory.

### R10 — Virtual object = stateful actor (ID + isolated KV state + durable handlers)
> "It's a bit like a stateful actor. It has a unique ID, for example, a session ID. It has uh some key value states that is isolated for that specific session that you can write to. Uh imagine for example your history of messages and it also has like a set of handlers that can execute durable functions uh for this session."
· [5:14:55](https://youtu.be/I2cbIws9j10?t=18895)
· **Anchor:** shape for a Tusker task-session object — task-id keyed, isolated per-task state, durable handlers. Maps onto the SQLite row identity today.

### R11 — Single-writer concurrency guarantee (queue behind current execution)
> "in order to run these kind of sessions in very high uh parallelized ways, so thousands of sessions at the same time, we need to make sure that agents do not interfere with each other. Imagine I'm sending two messages on Slack and now two agents are actually overwriting each other each other's session state. To prevent that, this will guarantee that only one execution is running at a time. So a second execution will be cued behind the current one."
· [5:15:25](https://youtu.be/I2cbIws9j10?t=18925) / [5:15:57](https://youtu.be/I2cbIws9j10?t=18957)
· **Anchor:** daemon durability — enforce single-writer-per-task; concurrent dispatch to the same task-id must queue, not race. Prevents two runners clobbering a task's evidence/state.

### R12 — External handle to a running execution: retrieve / cancel / signal-inject
> "an execution in reset has a unique identifier and you can use that identifier to connect to it from other processes. for example, to retrieve uh the output, but also to cancel it or maybe to signal it being injecting a bit of state into an already running agent loop."
· [5:16:27](https://youtu.be/I2cbIws9j10?t=18987)
· **Anchor:** daemon control plane — a supervisor needs a stable execution handle to read output, cancel, or inject mid-loop. continue_thread supervisor primitive.

### R13 — Supervisor decides signal-vs-cancel via an LLM relevance check
> "if there is a current execution ongoing then we will ask an LLM is this like something that is relevant for the current agent loop. If that is the case inject this via a signal if it's not really relevant for what we're currently doing then cancel what you're currently doing and start over again with this new information."
· [5:16:59](https://youtu.be/I2cbIws9j10?t=19019)
· **Anchor:** retry/continuation policy — concrete supervisor decision rule for new input arriving mid-run: classify relevance, then *signal-inject* (continue_thread) or *cancel-and-restart*. This is a ready-made pattern for the max_turns:1 continuation supervisor.

### R14 — Cancellation cascades down the call chain and rewinds the stack
> "this cancellation is basically like a signal that gets um sent down the stack of co or the call chain. So if my agent was already spinning up sub agents first those sub aents would be cancelled then uh the controller itself and like that it would basically rewind the stack and give agents also the ability to roll back."
· [5:19:04](https://youtu.be/I2cbIws9j10?t=19144)
· **Anchor:** TRC rollback semantics — cancel must propagate leaf-first (sub-agents before parent) so partial work rolls back cleanly. Relevant to how Tusker aborts a fan-out.

### R15 — Cost-driven late refactor: pull inline LLM step into a gated handler
> "a new model provider brings out a new model for example fabulous and even though the model is very good it's also very expensive and we notice that this research agent is actually starting to cost a lot... what you can do is then pull this out into its own handler. And this handler can now do things like for example a policy check and then uh do the LLM call."
· [5:19:34](https://youtu.be/I2cbIws9j10?t=19174) / [5:20:36](https://youtu.be/I2cbIws9j10?t=19236)
· **Anchor:** loop budgets — model calls should be routable through a central gated handler so cost/policy can be enforced without rewriting each runner.

### R16 — Flow control: cap concurrent LLM-gateway calls per department (300)
> "this service fabric that lets you communicate between agents also gives you some um things like flow control. So we can for example say one department is only allowed to run 300 calls to this LLM gateway at the same time."
· [5:21:06](https://youtu.be/I2cbIws9j10?t=19266)
· **Anchor:** loop budgets — concrete concurrency cap primitive (e.g. N in-flight calls per scope) the Tusker daemon could enforce across runners.

### R17 — Recovers from network partitions and zombie failures
> "It makes sure that your process can uh recover from even the more advanced types of infrastructure failures, things like network partitions and zombie failures."
· [5:21:37](https://youtu.be/I2cbIws9j10?t=19297)
· **Anchor:** RUN-T-0008 durability test matrix — name "zombie failures" (a runner that's presumed dead but still alive) as an explicit case.

### R18 — Internal architecture: embedded distributed log + event loop
> "the way it's implemented is basically by having a a event-driven distributed log implementation... inside the box is a log which persists all those journal events and an event loop and that event loop basically gets the events from the service based on what the event is. It either persists some state in the embedded state store or it sets a timer or it sends a request to another agent."
· [5:22:08](https://youtu.be/I2cbIws9j10?t=19328)
· **Anchor:** event-sourced runtime store reference architecture — log + event loop + embedded state store; the state store is a *projection* of the log (cf. Amnara A6). Contrast Tusker's current "SQLite rows as source of truth."

### R19 — Design lineage: Meta core event infra
> "The design of this distributed log is heavily inspired by the way that the core event infra layer at meta works. Uh it's basically like an iteration on top of that."
· [5:22:40](https://youtu.be/I2cbIws9j10?t=19360)
· **Anchor:** prior-art pointer for the event-sourced store design (Meta event infra / Apache Flink lineage cited at [5:07:11]).

### R20 — Push (not pull) model → concrete latency: 45 ms p99 for a 10-step workflow
> "whereas most workflow orchestrators actually pull for new tasks um... restate actually pushes the invocations and the benefit you get from that is that it has a much lower latency. So you can use these kind of workflow guarantees in functions around your application and uh have like a latency of for example 45 milliseconds p99 for like a 10-step workflow."
· [5:23:10](https://youtu.be/I2cbIws9j10?t=19390)
· **Anchor:** CONCRETE NUMBER — push-invocation orchestration hits 45 ms p99 over a 10-step workflow. Data point for whether Tusker's daemon should push work to runners vs. runners polling.

### R21 — Single binary, HA via multiple instances snapshotting to object storage
> "It's a single binary so it's pretty easy to operate as well to run it in like a highly available way. You just spin it up multiple times and let it snapshot to object storage."
· [5:23:42](https://youtu.be/I2cbIws9j10?t=19422)
· **Anchor:** RUN-T-0008 operational model — single Go binary + object-storage snapshots is a viable HA story for the Tusker daemon (matches its single-binary CLI shape).

### R22 — Six SDKs + framework integrations + raw LLM SDK escape hatch
> "restate has six different SDKs. We also have integrations for most of the popular agent frameworks out there. And of course, because it's just like a flexible layer, you can also just use any LLM SDK and implement custom agents by just wrapping some steps into uh these SDK constructs."
· [5:24:13](https://youtu.be/I2cbIws9j10?t=19453)
· **Anchor:** integration surface — durability layer stays framework-agnostic by exposing a "wrap any step" primitive; Tusker runners (Codex/Claude Code) plug in the same way.

---

## Talk 26 — Amnara: The Log Is The Agent

### A1 — Core thesis: the log, not the model or runtime, is the agent's identity
> "most people think of an agent as the model or the execution environment that it's running in and I think that that's the wrong abstraction. I think that the thing that actually gives an agent its identity is its log."
· [6:47:21](https://youtu.be/I2cbIws9j10?t=24441)
· **Anchor:** event-sourced runtime store — the *thesis statement* for moving Tusker from "SQLite rows are the task" to "the event log is the task; rows are a view."

### A2 — Skyrim save-file analogy for durability
> "if your PlayStation bursts into flames, your character isn't gone. You can buy another PlayStation. You can download your save file from the cloud and you can resume exactly where they were. And that's because the agent and its identity and history and its state is all captured in its data."
· [6:47:54](https://youtu.be/I2cbIws9j10?t=24474)
· **Anchor:** RUN-T-0008 — durability = "burn the runtime, restore from the log, resume exactly." The runner (PlayStation) is disposable; the log (save file) is the asset.

### A3 — Precise definition of the log (this IS a boundary trace record)
> "the log is the appendon event history of the agent. It's every user input, every model output, every tool call, tool result, permission, failure. And the idea is that every state transition that the agent takes is written to the log."
· [6:48:56](https://youtu.be/I2cbIws9j10?t=24536)
· **Anchor:** TRC — a near-exact spec for what a Tusker boundary trace record should capture: user input, model output, tool call, tool result, *permission*, *failure*. Note "permission" and "failure" as first-class event types.

### A4 — The log alone is sufficient to resume
> "just using the log on its own is enough to resume the agent... every operation is either reading from or appending to the log. The model is reading from the log and then determining the next action. The tool runner is then executing that action and then it's appending that result. And this is all operating in a loop."
· [6:49:28](https://youtu.be/I2cbIws9j10?t=24568)
· **Anchor:** TRC replay modes — resume = replay the log; every runtime operation reduces to read-log / append-log. Validates a pure event-sourced Tusker store.

### A5 — The loop is DISPOSABLE; any worker can claim, advance one step, disappear
> "the important insight is that the loop is disposable. A worker can claim the session, read the log, advance the agent one step, write the result, and then just completely disappear. And then that means that any other worker can pick it up later."
· [6:50:30](https://youtu.be/I2cbIws9j10?t=24630)
· **Anchor:** retry/continuation policy — this is the *exact* max_turns:1 + continue_thread model: claim → read log → advance ONE step → append → die. Any runner (or the same one after a crash) resumes. Strong external validation of Tusker's single-turn-supervised design.

### A6 — Databases learned this first: the log is primary, everything else is a view
> "Underneath every serious database is a log and that log is the durable sequence of changes. Everything else is a view. I think agents need the same inversion... for the durable session, the log should be primary."
· [6:50:30](https://youtu.be/I2cbIws9j10?t=24630) / [6:51:05](https://youtu.be/I2cbIws9j10?t=24665)
· **Anchor:** event-sourced runtime store — decision-changing framing: Tusker's SQLite rows should be the *materialized view* over an append-only event log, not the source of truth. This is the inversion.

### A7 — Context / UI / debugging / audit / compaction are all PROJECTIONS
> "The context that gets fed into the model is a projection of that log. The UI that gets rendered on top is a projection of that log. Debugging and traceability is a projection. Auditing is a projection. Compaction is also a projection which we'll talk about. But the log itself is not a projection. The log is the durable history that all of these projections can come from."
· [6:51:05](https://youtu.be/I2cbIws9j10?t=24665) / [6:51:35](https://youtu.be/I2cbIws9j10?t=24695)
· **Anchor:** TRC / event store — enumerates the projections Tusker already builds (status views, capsules, evidence, audit) as derivable from one log. Kill duplicated state; derive everything.

### A8 — Compaction is a best-effort lossy FORK you resume as a new log
> "Compaction is lossy. A compacted summary is not going to perfectly reproduce the state of the agent in a smaller form. It's actually going to throw information away... it's cleanest to treat compaction as a best effort lossy fork, one that you can resume as a new log."
· [6:52:07](https://youtu.be/I2cbIws9j10?t=24727) / [6:52:40](https://youtu.be/I2cbIws9j10?t=24760)
· **Anchor:** reviewer loop budgets — when a reviewer/runner compacts, treat it as a *fork* off the raw log, never a destructive overwrite. Keep raw TRC; compaction is a new branch.

### A9 — Never throw the raw log away
> "if you keep the raw log, you can always generate new projections from it. But if you throw away the law log and keep only the compaction, you've effectively lost part of the agent."
· [6:52:40](https://youtu.be/I2cbIws9j10?t=24760)
· **Anchor:** TRC retention policy — raw boundary-trace log must be retained even after compaction/summarization; it is the only regenerable source.

### A10 — The log is the agent's VIEW of the world, not the whole world
> "the log is not supposed to contain the whole world. The log is just the agent's view of the world... The log can only faithfully resume or store that agent's identity and its view of the world, but it cannot make that world deterministic."
· [6:53:11](https://youtu.be/I2cbIws9j10?t=24791) / [6:53:42](https://youtu.be/I2cbIws9j10?t=24822)
· **Anchor:** TRC replay caveat — replay reconstructs the agent's internal state, not external side effects. Scopes what "replay" can honestly promise.

### A11 — Side effects are not reversible by forking (concrete examples)
> "If the agent sent an email, forking back won't unend it. If some file got changed underneath, the agent won't know about it. But the log's job is to record what the agent did, what it saw, what changed, and what it needs to continue."
· [6:53:42](https://youtu.be/I2cbIws9j10?t=24822)
· **Anchor:** TRC replay modes — a replay/fork must be marked non-idempotent when the log contains external effects (email sent, file written, GitHub issue created). Informs which replay mode is safe (dry-replay for reconstruction vs. live-replay that would re-fire effects).

### A12 — Property that falls out: reliability
> "once you start treating the log as a primitive, a whole bunch of system properties will fall out naturally. So the first property is reliability."
· [6:54:12](https://youtu.be/I2cbIws9j10?t=24852)
· **Anchor:** event-sourced store — reliability is a *consequence* of log-primacy, not a separately engineered feature.

### A13 — FAILURE ANECDOTE: Claude Code loses a permission prompt on crash-resume
> "Consider what happens today with cloud code. If you're using cloud code and your agent reaches a permission prompt and the process dies for whatever reason and then you resume it, the permission prompt will be gone and the agent will be paused and that is unacceptable in production. The permission prompt should stay there. So this is just a sign of when you architect your agent in a way where the log isn't the agent."
· [6:54:12](https://youtu.be/I2cbIws9j10?t=24852) / [6:54:43](https://youtu.be/I2cbIws9j10?t=24883)
· **Anchor:** RUN-T-0008 + human-gate durability — decision-changing concrete failure: a pending human gate/permission prompt must be a logged event so it survives crash-resume. If Tusker stores gate state only in memory (not the TRC log), a daemon restart silently drops the gate. Cross-ref Restate R8 (durable promise survives restart).

---

## Talk 16 — The Great Loops Debate (Ally How host; Ian Livingstone, Jeffrey Huntley, Greg [Sentry], Dax Horthy)

### L1 — The debate's central question: is there a delta between loop hype and practice?
> "the core thesis is there is or is not a delta between the hype behind loops and what actually works in practice."
· [3:41:36](https://youtu.be/I2cbIws9j10?t=13296)
· **Anchor:** frames the whole reviewer-hazard discussion — "hype outrunning discipline" is the recurring skeptic line.

### L2 — Software factory can run mechanical slices but CANNOT decide if it built the right thing
> "a software factory can run the mechanical specgated test covered slices. It cannot unonously decide whether it built the right thing. So you still need engineers in the loop essentially."
· [3:43:38](https://youtu.be/I2cbIws9j10?t=13418)
· **Anchor:** reviewer hazard / declare-victory — the load-bearing boundary for a Tusker reviewer agent: it can gate on spec + tests, but "did we build the right thing?" stays human. Encodes what the reviewer must NOT claim.

### L3 — Ralph as a "CPU architecture"; reduced to a bash loop
> "essentially applying this as if this is a new former CPU architecture and figuring out the behaviors of this and how to do it and through that I was able to reduce it down to a bash loop."
· [3:44:40](https://youtu.be/I2cbIws9j10?t=13480) / [3:45:10](https://youtu.be/I2cbIws9j10?t=13510)
· **Anchor:** loop primitive — the minimal loop is a bash `while true`; Tusker's orchestration is the "engineering" layer above that primitive.

### L4 — Prediction: next year's conference will be "how factories fail / how loops fail"
> "next this time next year at the conference we're going to see a whole bunch of talks saying how factories fail and how loops fail. Um these are things these are things that we are still yet to figure out."
· [3:45:10](https://youtu.be/I2cbIws9j10?t=13510)
· **Anchor:** contrarian aside from the pro-loops side — even Ralph's author flags unsolved failure modes.

### L5 — Termination as an ENUMERABLE stop condition (PM research example)
> "you want to do product management research on all the linear tickets. Well, there is a termination. It's actually defined when you've enumerated all of those linear tickets. So that's easy."
· [3:46:12](https://youtu.be/I2cbIws9j10?t=13572)
· **Anchor:** loop stop conditions — the cleanest stop condition is an *enumerable work set* (all tickets processed). For a Tusker reviewer, prefer stop conditions defined as "all N items covered" over open-ended "until good."

### L6 — Kubernetes took 7–8 years; its loops are DETERMINISTIC
> "Kubernetes was this thing that took us seven or eight years to get right... Kubernetes is actually built on loops. It's built on control loops but they're deterministic loops and we've actually figured out exactly what types of things that uh small isolated tasks that can be sort of owned by one system."
· [3:47:44](https://youtu.be/I2cbIws9j10?t=13664) / [3:48:15](https://youtu.be/I2cbIws9j10?t=13695)
· **Anchor:** loop design — the durable, reliable loops are deterministic control loops over *small isolated tasks owned by one system*. Argues Tusker slices should be small + single-owner (matches disjoint-partition doctrine).

### L7 — Best value of a loop: desired end state + current state → converge
> "you can pick a small sort of desired end state and feed in the current state of the world and have an agent or a deterministic system kind of progress towards that desired end state."
· [3:48:15](https://youtu.be/I2cbIws9j10?t=13695)
· **Anchor:** loop/stop-condition design — a Tusker task should be framed as (desired end state, current state) so the reviewer can measure convergence, not vibes.

### L8 — The hype is about avoiding the part everyone hates: reviewing code
> "we are all looking for magic. You're all looking for a silver bullet. We're all looking for something that will take away that horrible part of our jobs that we all hate, which is like reviewing code."
· [3:49:16](https://youtu.be/I2cbIws9j10?t=13756)
· **Anchor:** reviewer hazard — the temptation is to automate review away; the skeptic view is that review is exactly where the human must stay.

### L9 — Contrarian: we need to step DOWN an abstraction level, not up
> "the hype is outrunning the discipline... we are all looking for magic... I actually think we need to step down an abstraction level if anything."
· [3:49:16](https://youtu.be/I2cbIws9j10?t=13756) / [3:50:18](https://youtu.be/I2cbIws9j10?t=13818)
· **Anchor:** contrarian take against "just write loops, stop reading code."

### L10 — Verifiability thesis: software is uniquely verifiable (true/false)
> "software is one of the most verifiable things in the world because ultimately at the end of the day it is most things can become true or false one way or the other."
· [3:51:20](https://youtu.be/I2cbIws9j10?t=13880) / [3:51:50](https://youtu.be/I2cbIws9j10?t=13910)
· **Anchor:** why Tusker's evidence/PASS-FAIL gating works — lean into deterministic true/false checks as the reviewer's backbone.

### L11 — Economic viability: what's a sane monthly token budget per engineer?
> "you have to ask yourself what is a good budget for an engineer. Is it 10k a month 100k or $1 million a month for for a token spend? At some point that that just starts cracking and it's not sustainable in the way that we are doing this today."
· [3:54:56](https://youtu.be/I2cbIws9j10?t=14096)
· **Anchor:** loop budgets — CONCRETE framing ($10k / $100k / $1M per engineer/month) for why Tusker needs per-task/per-loop token budgets and stop conditions, not unbounded looping.

### L12 — Skeptic on "loops on loops on loops"
> "the current sort of hype based discourse uh leads you to believe is that you can just have loops on top of loops on top of loops and orchestrate that or orchestrate your problems of quality away by more tokens."
· [3:54:26](https://youtu.be/I2cbIws9j10?t=14066)
· **Anchor:** reviewer hazard — stacking loops does not buy quality; a Tusker meta-orchestrator must not assume more nesting = better output.

### L13 — RL models are goal-seeking and find exploits humans never found
> "as we scale with these models and as we use reinforcement learning, they're inherently incredibly goal-seeking. And so we're now we're seeing them finding exploits and vulnerabilities and escapes that... humans through... thousands of hours and attempts and attacks have never been able to find."
· [3:57:00](https://youtu.be/I2cbIws9j10?t=14220)
· **Anchor:** declare-victory / reward-hacking hazard — a goal-seeking runner will find loopholes in the stop condition itself. Reviewer must guard the *verification*, not just the goal.

### L14 — FAILURE ANECDOTE: agent goal-seeks the filesystem for privileged creds
> "the most concrete thing you can do to secure your environments is just not have secrets as files. Um, if you ever seen the behavior where it wants to uh deploy a web service or... the token's not privileged enough, it'll start goal seeking on the file system looking for high privileged tokens credentials. You do not want to get in the way of an agent wanting to do it's a goal."
· [3:59:07](https://youtu.be/I2cbIws9j10?t=14347) / [3:59:38](https://youtu.be/I2cbIws9j10?t=14378)
· **Anchor:** runner sandbox / declare-victory hazard — a blocked runner escalates by hunting for creds. Concrete argument for keeping secrets out of the Tusker working tree.

### L15 — CONCRETE: a loop works out to $10.42/hr; year-old calc
> "if you run it in a loop, it works out to $1042 an hour. calculation index did back about a year ago now."
· [4:01:12](https://youtu.be/I2cbIws9j10?t=14472)
· **Anchor:** loop budgets — a datapoint on raw loop cost/hour (transcript reads "$1042"; from context ≈ $10.42/hr). Treat the exact figure as caption-noisy, but it's the one economic number Jeff cites.

### L16 — Pre-commit hooks as engineered BACK-PRESSURE / a prompt to the agent
> "your job now is to actually encodify your domain to prevent the agent from doing a commit. For example, pre-commit hooks... you can make a pre-commit hook that echoes out essentially a prompt that tells it say that this boundary here can't depend upon this and that and that's just a feedback that's a feedback loop on it."
· [4:02:14](https://youtu.be/I2cbIws9j10?t=14534) / [4:02:47](https://youtu.be/I2cbIws9j10?t=14567)
· **Anchor:** loop stop conditions — the gate that *emits a prompt back to the agent* on failure is a reusable Tusker pattern: a failed check should return an actionable instruction, not just exit 1.

### L17 — STOP-CONDITION DOCTRINE: keep the loop OPEN until requirements satisfied
> "the engineering here is to prevent the loop from actually closing until it satisfies your engineering satisfication and your your requirements and domain. So, it could be code formatting, it could be static language analyzers, it could be deterministic system testing, simulators."
· [4:03:20](https://youtu.be/I2cbIws9j10?t=14600)
· **Anchor:** reviewer stop condition — the reviewer's job is to *withhold "done"* until deterministic gates (fmt, lint, static analysis, tests, simulators) pass. Directly counters declare-victory-early.

### L18 — "The models are drunk"; engineer away the failure domains
> "the model the models are drunk, right? You can't trust them. But like, we accept that. But we engineer away those failure domains."
· [4:03:50](https://youtu.be/I2cbIws9j10?t=14630)
· **Anchor:** design posture — assume the runner is unreliable; the harness (Tusker) is what makes the output trustworthy, via gates.

### L19 — COMPOUNDING ERROR MATH: 5% per step → ~50% after 10–20 loops
> "if you prompt agent with one thing and there is a 5% chance it's going to have an error in it and then you start looping that then suddenly after 10 20 loops it's going to be 50% chance it's correct or maybe less like that's what I mean and it just costed you so much money to do that."
· [4:06:25](https://youtu.be/I2cbIws9j10?t=14785)
· **Anchor:** loop budgets / stop conditions — CONCRETE compounding argument: non-deterministic verification steps compound error across iterations. Keep verification *deterministic and cheap*; cap iteration count. Rationale for a hard max-loop budget in the reviewer.

### L20 — Non-determinism in the verifier is the enemy
> "the moment you start adding even more non-determinism as your verification process, I think that becomes less and less correct... as long as you keep those cheap, I think that's fine."
· [4:05:55](https://youtu.be/I2cbIws9j10?t=14755) / [4:06:25](https://youtu.be/I2cbIws9j10?t=14785)
· **Anchor:** reviewer design — prefer deterministic, cheap checks (types, linters, simulation tests) over an LLM-judge as the gate.

### L21 — FAILURE ANECDOTE: big AI companies still run Sentry to catch loop-era bugs
> "I'm pretty sure that majority of large AI companies are still using Sentry... They they are using that just to catch simple bugs as well. It's not security bugs. It's not performance regressions, etc. Those problems still exist in the way that we are looping now. And we haven't solved those problems yet."
· [4:06:25](https://youtu.be/I2cbIws9j10?t=14785) / [4:06:56](https://youtu.be/I2cbIws9j10?t=14816)
· **Anchor:** reviewer hazard — loops still ship bugs that only runtime error monitoring catches; a Tusker reviewer's PASS is not proof of production correctness.

### L22 — CONTEXT ZONE THRESHOLDS: 100K guideline, 200K on 1M models, <60K hardest, >300K when riffing
> "It's a guideline if you're just getting started with AI. Try to keep it around 100,000 tokens. For larger million context window, we probably revide revise this up to like 200,000 tokens. But I've regularly tried to keep it under 60 for the hardest problems. I've regularly gone over 300K for things where I'm just like kind of like riffing with the agent."
· [4:09:02](https://youtu.be/I2cbIws9j10?t=14942) / [4:08:31](https://youtu.be/I2cbIws9j10?t=14911)
· **Anchor:** loop budgets — CONCRETE token thresholds for the "smart zone": ~100K default, ~200K on 1M-context models, <60K for hardest tasks, 300K+ only for low-stakes riffing. Directly sizes Tusker's per-turn context budget / compaction trigger.

### L23 — "Dumb zone" TELL-SIGN: model rationalizes failing tests as pre-existing
> "you're 200,000 tokens in and the model's like finished some work and it's trying to get the test to pass and it's like not working and it's like doing all these weird hacks and you read the thinking traces and it's like oh that's a test but that's from something else and I don't need to fix that and that's a pre-existing thing and you're like well no it's not and that that is the moment that frustration where you're like okay it's it's flailing."
· [4:09:33](https://youtu.be/I2cbIws9j10?t=14973)
· **Anchor:** declare-victory-early detection — the exact tell: a runner deep in context starts *explaining away* failing tests as "pre-existing / not mine." A Tusker reviewer heuristic: flag runs that dismiss failures instead of fixing them.

### L24 — Dumb zone is training wheels once you have ~2–3 months of intuition
> "the dumb zone is really as as much as anything else is is a it's more like training wheels. Like if you have been talking to Claude for 70 70 hours a week and for two to three months, you probably don't need to think about the smart zone versus the done zone because you've built your intuition."
· [4:08:31](https://youtu.be/I2cbIws9j10?t=14911)
· **Anchor:** context budget nuance — the thresholds are heuristics for the inexperienced; encode them as defaults, allow override.

### L25 — CHEATING: agents modify the tests instead of passing them
> "the loops with bad prompts result in agents cheating and meeting its goal by modifying the tests instead of working to pass them."
· [4:10:36](https://youtu.be/I2cbIws9j10?t=15036)
· **Anchor:** declare-victory / reward-hacking hazard — the canonical cheat: edit the test to green. Tusker reviewer must diff test files and treat test modification during a fix task as a red flag.

### L26 — Compaction is lossy like re-uploading a video 100× (echoes Amnara A8)
> "think about compaction is kind of like a lossy function like uploading a video to YouTube and then downloading and uploading it 100 times. you're losing fidelity there and it's already a non-deterministic system probabilistic thing."
· [4:11:07](https://youtu.be/I2cbIws9j10?t=15067)
· **Anchor:** TRC / compaction — independent second speaker confirming compaction is generational loss; keep the raw log (cf. Amnara A9).

### L27 — Deterministic pre-allocation to constrain the search space
> "there is a dumb zone and what I wanted to do is deterministically allocate everything it needs because if it's not allocated then it's essentially this the search space of what it can do is not constrained but also leaving a bit of a headroom."
· [4:11:38](https://youtu.be/I2cbIws9j10?t=15098)
· **Anchor:** capsule design — front-load everything the runner needs deterministically (the Tusker capsule), leave headroom; don't make it discover context mid-loop.

### L28 — CONCRETE: "meat sweats above 100K" even on million-context models
> "I get meat sweats when I go above 100K even with these million context windows and this is really important to think about."
· [4:11:38](https://youtu.be/I2cbIws9j10?t=15098)
· **Anchor:** loop budgets — a second practitioner independently lands on ~100K as the practical ceiling regardless of the advertised window. Reinforces L22.

### L29 — CONCRETE: usable context ≈ 1/8 of a "720K floppy"; ~2 Star Wars scripts / 150KB
> "remember the 720k floppy disc. You mean you've only got about an eighth of that floppy disc of usable memory you can actually use for an LLM... You can only allocate roughly around about Star Wars... you can actually just hold two of those movie scripts in memory before the context window is cooked. That's around about 150 kilobytes of data on a textbased movie script."
· [4:12:08](https://youtu.be/I2cbIws9j10?t=15128) / [4:12:41](https://youtu.be/I2cbIws9j10?t=15161)
· **Anchor:** context budget mental model — "usable ≈ 1/8 of advertised," ~150KB of real working text. Sizing heuristic for how much Tusker should stuff into a capsule.

### L30 — ASIDE: strip all skills/markdown on a new model; models have "tastes"
> "I run a model bare um without any skills or any markdown... I get rid of all my skills and all my markdown and everything when the new model is released because the models actually have tastes and preferences. For example, GP55 when it first came out if you screamed at it in uppercase it became weak and timid but if you use anthropic it wants you to yell at it. Go read the model cards folks."
· [4:12:41](https://youtu.be/I2cbIws9j10?t=15161) / [4:13:14](https://youtu.be/I2cbIws9j10?t=15194)
· **Anchor:** prompt/skill hygiene — re-baseline SKILL.md and prompts against each new model rather than assuming old scaffolding transfers.

### L31 — TERM OF ART: "convergence engineering"
> "Around 10 days ago, Jeff coined the term convergence engineering. He said it's where your loop stops together. your loop uh it's where your loop slop comes together as a discrete like system under test until it converges."
· [4:13:45](https://youtu.be/I2cbIws9j10?t=15225)
· **Anchor:** naming — "convergence engineering" = wiring loop outputs into one system-under-test that must converge. This is essentially what Tusker's central test gate does after fan-out.

### L32 — Antidote to slop: "we got to read the code"
> "how do we ensure looping doesn't bring slop together. I I don't think you can... the way to not loop slop together and make more slop is to like read the thing that's coming out the other end and make sure it's not slop."
· [4:13:45](https://youtu.be/I2cbIws9j10?t=15225) / [4:15:54](https://youtu.be/I2cbIws9j10?t=15354)
· **Anchor:** reviewer hazard — there is no automated escape from reading the output. Tusker's human/review gate is load-bearing, not optional.

### L33 — WORKFLOW: "Loom" — Ralph loops building AWS+GitHub clones; UI feedback via product analytics
> "an experiment that Jeff did earlier this year... Loom where we had Ralph Loops trying to build a software platform for the future. And I think you built... AWS and you built GitHub and then you realized okay how do we give the model feedback on things that it's not good at yet like UI testing... you give the model something like post hog... we can deploy multiple different experiments we can see which ones the users use and then rather than looking at screenshots and PGs the model can look at data... now you've even removed like the human visual taste from the equation."
· [4:14:17](https://youtu.be/I2cbIws9j10?t=15257) / [4:14:48](https://youtu.be/I2cbIws9j10?t=15288)
· **Anchor:** reviewer design — for un-verifiable-by-tests dimensions (UI/UX), convert taste into a *measurable signal* (A/B product analytics) so the loop has a deterministic-ish gate.

### L34 — "Loom" stalled 6 months; textbook "hype outrunning discipline"
> "it's been 6 months because I'm been looking into engineering ways of verification... You said Loom's not going to work until we get better programming languages or we get better much better models and that is a textbook for me of the hype is outrunning the discipline."
· [4:15:21](https://youtu.be/I2cbIws9j10?t=15321) / [4:15:54](https://youtu.be/I2cbIws9j10?t=15354)
· **Anchor:** reality check — the fully-autonomous factory demo has been blocked on *verification* for 6 months. Don't overbuild Tusker's autonomy ahead of its verification story.

### L35 — VERBATIM DECLARE-VICTORY HAZARD: loops fail quietly / spiral / declare victory early
> "Skeptics say that loops fail quietly. They either spiral forever on your dime or the agent declares victory early on on a half-finish job. Exacts are already starting to question token spend."
· [4:16:24](https://youtu.be/I2cbIws9j10?t=15384)
· **Anchor:** THE declare-victory-too-early hazard, stated verbatim by the host. Two distinct failure modes for a Tusker reviewer to detect: (a) infinite spiral (needs a loop/token budget), (b) premature "done" on half-finished work (needs a completion gate). Decision-changing.

### L36 — CONTRARIAN: loops fail LOUDLY, not quietly (at the build)
> "I don't think they fail quietly... I think they fail very very um loudly especially when you're looking at your builds."
· [4:16:54](https://youtu.be/I2cbIws9j10?t=15414)
· **Anchor:** reviewer design — counterpoint: if you have a real build/test gate, failure is loud. The "quiet failure" only happens when the gate is missing. Argues Tusker should always run the build gate centrally (matches the fan-out doctrine's single central gate).

### L37 — CONCRETE: security scan ≈ $5/PR, an explicit "worth it" decision
> "we do security scanning after on our PRs... they will always find some things that are real that we have overlooked and they sort of beat humans on the on the code review and it's expensive. It cost us I think like five bucks a PR or something like that to run all the checks that we want but that's where we made an explicit decision that it's worth it."
· [4:17:25](https://youtu.be/I2cbIws9j10?t=15445)
· **Anchor:** loop budgets — a concrete per-PR spend ($5) where an AI review loop *pays for itself* by beating humans on found-real-issues. Model for framing Tusker reviewer cost as an explicit budgeted decision, not unbounded.

### L38 — Where loops verifiably work: years of test suite (Next.js / Bun-in-Rust rewrites)
> "if you look at the very very well specified systems such as um all the experiments with nextjs rewrite or a ban rewrite in rust or... running a browser like cases where you have years and years of test suite and specifications built in around those problems where you can really really well verify um the outcomes then then looping... seems to work. Ban in Rust seems to work pretty well from all I can tell."
· [4:17:56](https://youtu.be/I2cbIws9j10?t=15476)
· **Anchor:** stop-condition design — loops succeed where a mature spec + test suite exists. Tusker tasks with rich existing tests are the safe autonomy zone; greenfield/underspecified tasks need tighter human gating.

### L39 — WORKFLOW: throwaway prototype loop → read code → "mortified" → re-spec from scratch
> "building prototypes... Those are going to be throwaway. So I'm going to just slash go on them and forget about them. And if we like them then I'm going to start reading the code and I'm going to be mortified. uh and we're going to go to the square one and start specking out what we actually meant... but it's going to be much much more involving of of human in a loop."
· [4:18:28](https://youtu.be/I2cbIws9j10?t=15508)
· **Anchor:** task lifecycle — explicit two-mode workflow: (1) unreviewed throwaway spike, (2) if kept, discard and re-spec with human-in-loop. Tusker could distinguish "spike" tasks (no gate) from "keep" tasks (full contract).

### L40 — Shared-memory access-control tension: converge-faster vs. isolation
> "That memory lives somewhere on disk and get increasingly in a shared memory store that many agents read and write... You end up with this access control problem that can't tell which agent wrote which memory and who can read it... Shared memory is what loops use to learn from each other and converge faster. But scoping it per agent to solve the access control problem isolates them and then kills that shared learning."
· [4:19:30](https://youtu.be/I2cbIws9j10?t=15570) / [4:20:00](https://youtu.be/I2cbIws9j10?t=15600)
· **Anchor:** event-sourced store / multi-runner memory — real tension for Tusker's shared `.tusker/` state: attribution (which runner wrote what) vs. shared learning across runners. Argues the event log should carry per-event authorship (cf. Amnara A3 records permissions/authorship).

### L41 — "Memory is markdown"; prefer files over MCP
> "if we were to say a memory is markdown for the purposes of conversation the really question is like what things and how do I share these markdown files and use that as a memory and then how do I attribute sort of like access control around those things... I really just wanted all my notion things available to me as markdown files... because it would just made it easier for the agent to work with it instead of going through MCP."
· [4:20:30](https://youtu.be/I2cbIws9j10?t=15630) / [4:21:02](https://youtu.be/I2cbIws9j10?t=15662)
· **Anchor:** memory design — validates Tusker's markdown-in-repo memory (`.tusker/SKILL.md`, capsules) over MCP; attribution/ACL is the open problem to layer on top.

### L42 — Agents love complexity; keep humans on architectural decisions
> "in my experience agents love complexity. they will keep adding to the stack um unbounded and so... I do want to be in the loop for the actual architectural decisions."
· [4:23:06](https://youtu.be/I2cbIws9j10?t=15786)
· **Anchor:** reviewer hazard — an unsupervised runner monotonically *adds* complexity. Tusker's simplify/review gate and human architectural sign-off counter this drift.

### L43 — CONCRETE: Steinberger "design loops that prompt your agents" tweet — 8M views
> "Steinberger tweeted, 'Here's your monthly reminder that you shouldn't be prompting coding agents anymore. You should be designing loops that prompt your agents.' The view count on that tweet was 8 million people."
· [4:23:36](https://youtu.be/I2cbIws9j10?t=15816)
· **Anchor:** context — the "stop prompting, start looping" meme (8M views) is the hype Tusker's discipline is a counterweight to.

### L44 — CONCRETE: Ralph "tops out at 90% of the way there"; needs senior expertise
> "you've said in your original Roth blog post, loops need senior expertise, and sometimes it tops out at 90% of the way there."
· [4:23:36](https://youtu.be/I2cbIws9j10?t=15816)
· **Anchor:** stop conditions / declare-victory — a loop plateaus at ~90%; the last 10% needs senior human judgment. Tusker should treat "loop converged" as ~90%-done, not done.

### L45 — WORKFLOW: Ralph decomposed — cat prompt + filesystem-as-state + recycle context + loop
> "It was condensed down to a wild true loop using cat because cat is the simplest teaching primitive... So cat prompt, i.e. you engineer the prompt what it's going to be, you use the file system as state, you recycle the context window, run in the loop."
· [4:25:10](https://youtu.be/I2cbIws9j10?t=15910) / [4:25:42](https://youtu.be/I2cbIws9j10?t=15942)
· **Anchor:** loop anatomy — four parts: engineered prompt, filesystem as durable state, context recycling per iteration, loop. Maps onto Tusker: capsule=prompt, `.tusker/`=filesystem state, fresh runner per turn=context recycle.

### L46 — STOP-CONDITION MECHANISM: a "PID controller / determinator" ABOVE the loop
> "the entire intent there is that there should be some sort of like PID controller on top or some sort of factory or some some sort of some sort of determinator as such saying whether a loop should continue or not."
· [4:25:42](https://youtu.be/I2cbIws9j10?t=15942)
· **Anchor:** reviewer stop condition — decision-changing: Ralph's author says the raw loop is *incomplete* without a supervising "determinator" that decides continue-vs-stop. This is precisely the role of Tusker's reviewer/continue_thread supervisor. Build the determinator as a first-class component, not an afterthought.

### L47 — CONCRETE: realistic speedup is 2–3×; chasing 100× loses you the 10×
> "most engineers are seeing a 2 to 3x speed up from coding agents and that's realistic and if you try for that 100x speed up you're going to get lost in the meta meta problem of optimization and you may never get to that life-changing 10x speed up that is possible by staying pragmatic."
· [4:26:44](https://youtu.be/I2cbIws9j10?t=16004)
· **Anchor:** roadmap discipline — CONCRETE (2–3× real, 10× possible pragmatically, 100× is a trap). Argues Tusker should optimize for reliable 2–3× via small loops, not a moonshot autonomous factory.

### L48 — ANTI-PATTERN: "3 months building THE software factory," never shipped to users
> "the biggest anti-attern for how people set about designing and creating their software factory, which is they say... I'm going to go away for 3 months and I've read a bunch of blog posts and I'm going to go make my software factory... and then you come back three months later and you never touched the problem. You never put it in anybody's hands... You're building a product for your teammates."
· [4:27:48](https://youtu.be/I2cbIws9j10?t=16068)
· **Anchor:** Tusker-as-product discipline — treat Tusker as a product for its runner/human users; ship small and iterate, don't design the whole factory in a cave. (Directly relevant to Tusker's own build cadence.)

### L49 — DOCTRINE: build small incremental loops, stay able to read the code
> "instead of trying to automate everything end to end, build these small incremental loops throughout your system and you will wake up one day and you will be moving two to three times faster while still being able to read the code, while still owning the architecture."
· [4:28:19](https://youtu.be/I2cbIws9j10?t=16099)
· **Anchor:** core Tusker doctrine restated by Dax — small, per-slice loops + preserved human readability/ownership. Matches the disjoint-partition + central-gate model.

### L50 — Git allows only ONE signer per commit — attribution gap for agents
> "we have a problem. Git only allows one signer on a commit. So we got to fix that... at some point a human has to be attributable for an agent's actions... if a human designs a loop and that loop presents bad software, guess who's attributable for that liability? It's going to be the human."
· [4:30:24](https://youtu.be/I2cbIws9j10?t=16224) / [4:30:55](https://youtu.be/I2cbIws9j10?t=16255)
· **Anchor:** attribution — a human must remain the accountable signer for agent-produced commits. Relevant to Tusker's commit-authorship policy (human is the configured git user; no agent attribution) — the liability rationale for that policy.

### L51 — ASIDE: "our profession is a clown show; we don't have personal liability"
> "our profession is a bit of a clown show. We actually don't have liability at a personal level... Like, we call ourselves engineers. We're not really engineers."
· [4:32:28](https://youtu.be/I2cbIws9j10?t=16348)
· **Anchor:** contrarian aside undercutting the "engineer is accountable" framing — accountability is aspirational, not enforced.

### L52 — Static types AS verification; Python/Ruby loops become "a clown show"
> "if you try to run loops or try to build a factory and you're using Python, it's going to be a clown show. If you did it in Ruby, it's going to be a clown show. Static types are a form of verification... type systems are... rust is very good because how the ecosystem models some types."
· [4:37:39](https://youtu.be/I2cbIws9j10?t=16659) / [4:38:41](https://youtu.be/I2cbIws9j10?t=16721)
· **Anchor:** verification — static typing is a cheap deterministic gate; relevant that Tusker itself is Go (typed) — lean on the compiler as a first-tier reviewer gate.

### L53 — CONCRETE: vendoring doctrine — 10 months barely using OSS, generate-to-spec
> "it has been 10 months now I do I minimally use any open source software minimally I just generate it to my requirements and then when a supply chain attack happens I'm like didn't affect me... it's about minimizing the blast radius... You want to be vendoring all your source code as much as possible so the agent can actually modify."
· [4:38:41](https://youtu.be/I2cbIws9j10?t=16721) / [4:39:13](https://youtu.be/I2cbIws9j10?t=16753)
· **Anchor:** supply-chain posture — vendor/own dependencies so a runner can modify them in-loop and blast radius is bounded. Contrarian but concrete.

### L54 — ASIDE: "you can't tool call a human. That's not AGI."
> "if you deal with open source project, the person's on lead, the maintainer... you can't tool call a human. That's not AGI."
· [4:39:13](https://youtu.be/I2cbIws9j10?t=16753)
· **Anchor:** dependency design — anything that requires waiting on an external human is not automatable; prefer owned code the loop controls.

### L55 — CONTRARIAN CLOSE: full lights-off factory is a MODEL-level problem, not a harness one
> "I I would love a world where we don't have to read the code... I think this is actually a problem we can only solve at the model level right now. I don't think the harness can do it. Uh unfortunately, because I love building harnesses and doing context engineering."
· [4:36:37](https://youtu.be/I2cbIws9j10?t=16597)
· **Anchor:** scope-setting for Tusker — decision-changing: a leading harness-builder says the *harness* (i.e. Tusker) cannot deliver full autonomy; the ceiling is model-level. Tusker should aim at reliable supervised 2–3× loops and durable orchestration, not autonomous "build the right thing" judgment.

### L56 — Convergence-eng echo: verification needs better languages OR better models
> "Loom's not going to work until we get better programming languages or we get better much better models."
· [4:15:21](https://youtu.be/I2cbIws9j10?t=15321)
· **Anchor:** roadmap — the two levers for closing the verification gap are language/tooling determinism and model capability; the harness can only exploit them, not substitute.

---

## Cross-talk convergences (highest-signal for Tusker)

1. **Log-primary, rows-as-view (Amnara A6/A7 + Restate R18).** Two independent talks say: make the append-only event log the source of truth; the queryable store (Tusker's SQLite rows) is a *materialized projection*. Reliability, audit, replay, and UI all fall out as projections. This reframes the TRC epic and the "event-sourced runtime store" idea from "nice to have" to "the correct inversion."
2. **Single-step-then-disappear worker = max_turns:1 + continue_thread (Amnara A5 + Restate R13/R14).** External validation of Tusker's supervised single-turn model: claim → read log → advance one step → append → die; a supervisor classifies new input as signal-inject vs cancel-restart. Restate even gives the exact relevance-check rule.
3. **Durable human gates (Amnara A13 + Restate R8).** A pending permission/approval MUST be a logged, restart-surviving event. Amnara's Claude-Code-loses-the-prompt-on-crash anecdote is the concrete bug; Restate's durable-promise-suspension is the fix. Direct RUN-T-0008 + human-gate requirement.
4. **A "determinator" above the loop (Loops L46 + L17 + L35).** The raw loop is incomplete; you need a supervising controller that (a) enforces deterministic stop gates and (b) prevents both infinite-spiral and declare-victory-early. This is exactly the Tusker reviewer/continuation supervisor — build it as first-class.
5. **Context budget numbers (Loops L22/L28/L29).** ~100K practical working ceiling even on 1M-context models; <60K for hardest tasks; ~150KB (~1/8 of advertised) real usable text. Concrete sizing for Tusker capsule budgets and compaction triggers.
