# AIE World's Fair 2026 — Transcript Mining: Serve UI + Config Resolution

Source corpus: `/Users/sarav/Downloads/AIE Talks/AI_Engineer_WF26_Obsidian_Vault/Transcripts/`
Extraction date: 2026-07-06

**Method:** Every attributed claim is a VERBATIM quote (auto-caption text, transcription errors preserved) with its exact timestamp link. Numbers/thresholds/mechanisms without a timestamp are marked as such and are NOT attributed. Anchors tie each nugget to Tusker work: Howie Liu → `tusker serve` UI; Garry Tan → RUN-T-0002 (config resolution with provenance).

---

## Talk 31 — Howie Liu (Airtable / Hyper Agent): "Employable Agents and Agent Fleets"

Video: https://youtu.be/I2cbIws9j10?t=29966 · Anchor domain: the serve control-room UI (roster, attention routing, review batching, handoff visualization).

### (b) Named mechanisms + exact product terms

**N1 — "orchestration control plane" (the primary UI concept)**
> "the way we've thought about that is you need to see this kind of orchestration control plane where you can see at a glance what all of your agents are working on, what they're blocked on, who's handing off work to each other, and your job becomes almost like, you know, zooming out to see the entire Sim City landscape"
[8:34:35](https://youtu.be/I2cbIws9j10?t=30875)
*Tusker anchor:* This is the exact spec for the serve dashboard's top-level view. Three named columns fall straight out: **(1) what each agent is working on, (2) what it's blocked on, (3) who's handing off to whom.** Make these the roster's first-class fields, not derived.

**N2 — "at a glance" + "blocked on" as an explicit surfaced state**
> "you can see at a glance what all of your agents are working on, what they're blocked on, who's handing off work to each other"
[8:34:35](https://youtu.be/I2cbIws9j10?t=30875)
*Tusker anchor:* "Blocked on" is a distinct roster status deserving its own column/filter in serve — the operator scans for blocked agents to unblock, not for running ones.

**N3 — "Sim City landscape" / macro-vs-micro framing**
> "zooming out to see the entire Sim City landscape and orchestrating across everything going on at a macro level between your different agents rather than going and having to play micro and zoom into each individual project or each individual task"
[8:35:05](https://youtu.be/I2cbIws9j10?t=30905)
*Tusker anchor:* Default serve view = macro fleet map; drill-into-one-task is the exception path, not the home screen. The operator's stated primary workflow is macro orchestration; per-task detail is a zoom, not the landing page.

**N4 — Human role reframed as "unblocking"**
> "I think the job of the human becomes increasingly about unblocking agents to be able to do work effectively."
[8:33:32](https://youtu.be/I2cbIws9j10?t=30812)
*Tusker anchor:* The serve UI's job-to-be-done is "route the operator to the next agent that needs unblocking," i.e. attention routing = a prioritized queue of blocked/gated agents.

**N5 — Heartbeat / always-on wake-up mechanism (named)**
> "they can even have a mechanism like the heartbeat mechanism within openclaw um and now in other products like hyper agent that enable them to wake up on their own and almost like um exhibit this always on behavior."
[8:23:04](https://youtu.be/I2cbIws9j10?t=30184)
*Tusker anchor:* Named prior art ("heartbeat") for the V7 daemon's wake/resume loop. If serve shows always-on agents, the roster needs a "last heartbeat / next wake" field to distinguish idle-alive from dead.

**N6 — Wake-up decomposes into many turns/tool-calls**
> "each wake up basically results in many different turns. So the agent loops upon itself. It's performing tool calls. Each tool call response then invokes the the LLM again until it reaches a stopping point, but then that stopping point gets reinvoked or or kind of restarted with a heartbeat mechanism"
[8:23:34](https://youtu.be/I2cbIws9j10?t=30214)
*Tusker anchor:* A serve agent-detail view should collapse a wake cycle into one reviewable unit ("this heartbeat did N turns"), not stream every raw tool call to the operator.

**N7 — Named agent roles + handoff chain (triager → surveyor → human)**
> "This basically gets routed into a um in into one agent that is the triager agent... it hands off then the work to another agent, a surveyor agent, to actually go and then look at the um the physical landscape"
[8:28:17](https://youtu.be/I2cbIws9j10?t=30497)
*Tusker anchor:* Handoffs are first-class edges in the fleet graph. Serve should render the handoff chain (who passed to whom) as the connective tissue between roster rows — directly matches N1's "who's handing off work to each other."

### (a) Concrete numbers / thresholds

**N8 — Reactive-agent task duration ("20 minutes maybe up to an hour")**
> "You would have an agent go and perform a task. It might spend 20 minutes maybe up to an hour to perform the task and then it completes."
[8:23:04](https://youtu.be/I2cbIws9j10?t=30184)
*Tusker anchor:* Design the serve timeline for tasks that run 20–60 min (not seconds). Progress/heartbeat indicators matter at this timescale; a spinner does not.

**N9 — Inference credit / model list (product offer, dated)**
> "hyperaggent.com/ie and you get $1,000 of inference credit... including uh you know the latest uh Opus 4.8, Fable 5, you can use it on GLM 5.2 uh on GBD 5.5"
[8:36:06](https://youtu.be/I2cbIws9j10?t=30966)
*Tusker anchor:* Contemporary frontier model set operators expect to pick from (Opus 4.8, Fable 5, GLM 5.2, "GBD 5.5"/GPT-5.5). Serve's per-agent model selector should assume a multi-vendor list, not a single default.

**N10 — Startup competition scale/prizes (asides, dated context)**
> "the Hyper Asian team has kindly put up $100,000 in prizes... 50k to the winners um 30k to runner up 20k to second runner up"
[8:39:43](https://youtu.be/I2cbIws9j10?t=31183)
*Tusker anchor:* Low relevance to serve; kept for corpus completeness (Founding 500 / 500 founders program, 20 competed, 3 finalists).

### (c) Failure anecdotes

**N11 — Prior autonomous agents drifted / rabbit-holed**
> "if you recall back to the days of um uh baby AGI and autogbt those were really kind of fun cool science experiments but ultimately... they would all inevitably you know kind of end up drifting off to something not super useful, right? It would go off into a rabbit hole and kind of get stuck."
[8:24:37](https://youtu.be/I2cbIws9j10?t=30277)
*Tusker anchor:* The serve UI's value is catching drift early — surfacing "this agent has looped N times without progress" is a control-room feature, not a nice-to-have. Drift is the historical failure mode.

**N12 — Static agents are a failure mode (learn-every-interaction)**
> "agents should not be static. They should not just be you know either static LLM calls or static even agents that just have a prompt and you know some static skills. They really need to learn with every interaction"
[8:31:26](https://youtu.be/I2cbIws9j10?t=30686)
*Tusker anchor:* Feedback captured in serve (operator corrections) should write back to the agent's memory/skills, not evaporate. Ties to `tusker feedback` — corrections during review are training signal.

### (d) Asides / contrarian takes / Q&A

**N13 — Form-factor spectrum: completion → chatbot → agent → "claw" → orchestrator**
> "we went from basically completions... to a chat form factor... The next level I call an agent... which I'm using the term claw here because openclaw obviously has has popularized uh this concept."
[8:21:31](https://youtu.be/I2cbIws9j10?t=30091) through [8:23:04](https://youtu.be/I2cbIws9j10?t=30184)
*Tusker anchor:* Tusker agents are "claw"-tier (long-running, self-waking) trending to orchestrator-tier (agent-to-agent). Serve must be built for the orchestrator tier, above single-agent chat UIs.

**N14 — Agent definition (matches Anthropic's): recursion + open-ended tools, NOT a workflow**
> "an agent is really a model that recurses upon itself... it's not prescripted to a linear workflow, but it can kind of make an open-ended set of choices."
[8:22:33](https://youtu.be/I2cbIws9j10?t=30153)
*Tusker anchor:* Serve should not render agent runs as fixed DAGs/workflow steps; the path is emergent. Show actual trajectory, not a predetermined pipeline.

**N15 — "Not setting off agents overnight feels like a loss" (contrarian ops take)**
> "in fact going to sleep without setting off your agents to perform useful work overnight feels like you're you're taking this huge loss like your your team is not working because you're not unblocking"
[8:34:35](https://youtu.be/I2cbIws9j10?t=30875)
*Tusker anchor:* Serve needs an overnight/async mode: queue work, agents run unattended, operator reviews a batch in the morning. Reinforces "review large chunks" batching over real-time babysitting.

**N16 — Human gates high-impact / binding actions (approval before external action)**
> "the ability for agents to get unblocked by humans that still own policies and decisions and really importantly just kind of gate, you know, really critical um high impact actions like in this case actually making a pitch to a customer that has a binding quote"
[8:30:22](https://youtu.be/I2cbIws9j10?t=30622)
*Tusker anchor:* Serve's human-gate is specifically for irreversible/outward-facing actions (matches Tusker's existing human-gate concept). Gate the binding action, let everything upstream run autonomously.

**N17 — "Upwards reports from the team" management metaphor**
> "just like a great manager who's getting upwards uh reports or you know upwards work updates from their team to then unblock."
[8:30:22](https://youtu.be/I2cbIws9j10?t=30622)
*Tusker anchor:* The review packet an agent hands up should read like a status report to a manager (what I did, what I need approved), not a raw log. Design the review-batch card around this.

**N18 — Coding-workflow evolution as the analogy for the UX shift**
> "the best frontier agentic developers I know... are are really just kind of overseeing a fleet of agents"
[8:34:04](https://youtu.be/I2cbIws9j10?t=30844)
*Tusker anchor:* Tusker's own dispatch-runners model IS this fleet-oversight pattern; serve is the oversight surface. The evolution completions→copilot→cursor-chat→fleet-oversight is the exact arc Tusker sits at the end of.

### (e) Step-by-step workflow

**N19 — End-to-end handoff workflow (intake → triage → survey → human approve → send)**
> "Somebody submits this inquiry... gets routed into... the triager agent... it hands off then the work to another agent, a surveyor agent... the surveyor agent it it went and created this proposal and pitch and now the intervention has to go back to the human like there is a step where humans still need to unblock the agents"
[8:28:17](https://youtu.be/I2cbIws9j10?t=30497) → [8:29:52](https://youtu.be/I2cbIws9j10?t=30592)
*Tusker anchor:* Canonical multi-agent pipeline with a human gate at the end. Serve should render this as: intake node → agent nodes with handoff edges → highlighted human-approval node. The approval node is where operator attention routes.

**N20 — Correction loop happens in the operator's own channel (Slack/email)**
> "within Slack or you know in our case within uh either Slack or email or other um invocation methods you can basically go back and forth with the agent just like a real human and say hey like you know remember to take this into account next time... you missed kind of um this additional cost lever"
[8:32:29](https://youtu.be/I2cbIws9j10?t=30749)
*Tusker anchor:* Invocation/correction doesn't have to live only in the web UI — serve could accept operator feedback from Slack/email and reflect it in the roster. Multiple invocation methods, one fleet state.

---

## Talk 30 — Garry Tan (Y Combinator): "AI Native Company Brain"

Video: https://youtu.be/I2cbIws9j10?t=28692 · Anchor domain: RUN-T-0002 (config resolution with provenance). Live incident being mined against: three config sources for one concurrency limit, a CLI setter that returns `ok:true` but no-ops, and no way to see which source won.

### (b) Named mechanisms — THE RESOLVER TABLE (direct RUN-T-0002 hit)

**N21 — "Resolver table" defined + why it exists (context-too-big → offload to a table)**
> "Uh a resolver table. Uh the thing that many of you, you know, when you run into cloud code and it says your context is too big in cloud. MD, you run off and create a resolver table. Well, you know it. And if you don't know what that is, it's literally like whenever you need to uh alter a test, load tests.m MD. You have a whole table of these things."
[8:02:54](https://youtu.be/I2cbIws9j10?t=28974)
*Tusker anchor:* **This is the mental model for RUN-T-0002's output.** A resolver table maps a condition ("altering a test") → the source-of-truth to load ("tests.md"). For config: condition (which knob) → which of the three sources is authoritative. The resolver table IS the provenance artifact.

**N22 — Resolver = org chart; "a task comes in and the resolver decides who handles it and where it goes"**
> "Um that's an orchart. A task comes in and the resolver decides who handles it and where it goes."
[8:03:25](https://youtu.be/I2cbIws9j10?t=29005)
*Tusker anchor:* For config resolution, restate as: a *read* comes in and the resolver decides which source answers it. RUN-T-0002 must expose the resolver's decision ("source X won") as a queryable, not bury it. "Where it goes" = provenance out.

**N23 — Filing rules = internal process; "whether or not the resolver is actually working... is it actually in compliance"**
> "uh filing rules are your internal process. So this can be um you know whether or not the resolver is actually working and uh is you know is there is it actually in compliance"
[8:03:25](https://youtu.be/I2cbIws9j10?t=29005)
*Tusker anchor:* The exact framing of the RUN-T-0002 bug: the resolver returning `ok:true` while no-opping means it is NOT "actually working / in compliance." Need a compliance check that the write actually changed the winning source.

**N24 — "Trigger eval" — a TEST that verifies the right source actually got loaded (the fix)**
> "and uh trigger eval. So going in and actually having a test that says when I need to alter a test file does test.md actually get loaded those are performance reviews."
[8:03:25](https://youtu.be/I2cbIws9j10?t=29005)
*Tusker anchor:* **Highest-value nugget for RUN-T-0002.** Garry's "trigger eval" is the precise antidote to the `ok:true` no-op: an assertion that after a set, the intended source is the one actually loaded/won. Ship a config trigger-eval: set knob → assert which source now answers the read. This catches the silent no-op that manual inspection missed.

### (b) Named mechanisms — HYGIENE / PROVENANCE (direct RUN-T-0002 hit)

**N25 — "Provenance on every fact" + contradiction checks + pruning librarian**
> "the primitive is not memory. It's memory plus hygiene. Provenence on every fact. Contradition. contradiction checks when new information collides with the old and a librarian human plus agent whose actual job is pruning."
[8:13:12](https://youtu.be/I2cbIws9j10?t=29592)
*Tusker anchor:* Names the three primitives RUN-T-0002 needs: **(1) provenance on every fact** (which source set this value), **(2) contradiction checks** (three sources disagree on the concurrency limit — flag it), **(3) a pruning owner**. Config resolution is exactly "provenance on every fact" applied to settings.

**N26 — "Confident and wrong in ways nobody can trace" — the literal RUN-T-0002 symptom**
> "Treat the brain like a production infrastructure and it compounds. Treat it like a dumping ground and you get a very confident agent that is wrong in ways nobody can trace."
[8:13:12](https://youtu.be/I2cbIws9j10?t=29592)
*Tusker anchor:* This is the CLI setter returning `ok:true` but no-opping, verbatim in spirit: confidently reports success, silently wrong, untraceable. The fix per Garry = provenance + trigger eval so the wrong value IS traceable.

**N27 — "Who arbitrates when two facts disagree" — the multi-source tiebreak**
> "How it gets enriched and linked. What gets promoted to hot memory versus filed as cold reference. Who arbitrates when two facts disagree. Retrieval is easy. Being worth retrieving from is the product."
[8:11:10](https://youtu.be/I2cbIws9j10?t=29470)
*Tusker anchor:* "Who arbitrates when two facts disagree" = the precedence policy across three config sources. RUN-T-0002 must make the arbitration rule explicit and inspectable (which source outranks which), plus a hot/cold distinction (effective value vs. overridden reference).

### (d) Contrarian / architectural takes — COMPUTATION PLACEMENT (RUN-T-0002 relevant)

**N28 — "Be really careful about where the computation is happening" — bugs come from the wrong side**
> "you actually have to be really really careful about where the computation is actually happening. Uh it's happening almost always in two different places and all of the bugs all of the AI engineering that we run into that's a problem it's usually because uh something is happening in one side of the equation that should be in the other."
[8:07:04](https://youtu.be/I2cbIws9j10?t=29224)
*Tusker anchor:* Config precedence must live in **deterministic space** (code that computes the winning source), NOT be inferred by an LLM/agent at read time. The RUN-T-0002 bug class is exactly "computation on the wrong side." Resolution = pure deterministic function of the sources.

**N29 — Latent space (taste/judgment) vs deterministic space (code), defined**
> "the first area I would say is latent space. So the actual LLM... Taste, judgment, understanding what a human actually wants... The non-deterministic calls... and then deterministic space is what uh engineers know like your code"
[8:07:35](https://youtu.be/I2cbIws9j10?t=29255)
*Tusker anchor:* Which source wins = deterministic. Explaining *why a config value looks wrong to a human* = latent. Keep RUN-T-0002's resolver deterministic and reproducible; reserve any natural-language explanation for a separate layer.

**N30 — Deterministic storage "must not live in the context window" (the 800-seat example)**
> "this actual storage of like where everyone is inside like you know this multi-dimensional array of 800 seats um it actually must not live in the context window."
[8:08:05](https://youtu.be/I2cbIws9j10?t=29285)
*Tusker anchor:* The authoritative config state (three sources + winner) must be materialized in a real store/table, not reconstructed in an agent's context each time. Provenance is durable data, not a prompt.

### (b/e) Skillify + never-do-one-off (workflow discipline)

**N31 — "Never do one-off work" / skillify it / "if you have to ask for something twice, you failed"**
> "never do one-off work... at the end of that task uh skillify it... turn whatever you just did into a skill that you can reuse because if you have to ask for something twice, you failed."
[8:13:43](https://youtu.be/I2cbIws9j10?t=29623) → [8:14:13](https://youtu.be/I2cbIws9j10?t=29653)
*Tusker anchor:* The RUN-T-0002 trigger-eval and resolver table should be captured as a reusable Tusker skill/check, not a one-off debug. Also validates Tusker's whole skill-file model.

**N32 — "Model quality is rented, but the brain you own" (build vs. depend)**
> "Model quality is rented, but if you build your brain, you're you own that brain."
[8:14:45](https://youtu.be/I2cbIws9j10?t=29685)
*Tusker anchor:* Tusker's repo-local task/provenance store is the "owned brain." Config provenance is part of that durable, model-independent layer.

### (a) Concrete numbers (context, mostly non-config)

**N33 — Productivity multiplier: "about 400x," floor "8x," and the deflation method**
> "I did the math on my output, and it's about 400x... Take the most pathological verbosity penalty you can stomach... It's still 8x at the floor and ADX ['80x'] in the middle."
[8:00:50](https://youtu.be/I2cbIws9j10?t=28850)
*Tusker anchor:* Context/motivation only. Note the epistemic move (state number, then self-deflate to a defensible floor) — useful framing for how Tusker reports its own leverage claims honestly.

**N34 — "2x people and 100x people use the exact same claude" — leverage is in the wiring**
> "The 2x people and the 100x people are using the exact same claude. Same weights, same context window, same API. So the leverage is not in the weights, it's in how you wire the work."
[8:01:22](https://youtu.be/I2cbIws9j10?t=28882)
*Tusker anchor:* Core thesis validating Tusker's existence: the orchestration/wiring layer (task contracts, resolver tables, gates) is where leverage lives, not the model. Sell serve on wiring, not model choice.

**N35 — Baseline hand-coding rate: "about 14 usable logical lines of code a day... median 15"**
> "I could maybe do about 14 usable logical lines of code a day... that's about median 15. That was me at full effort at that time."
[8:00:16](https://youtu.be/I2cbIws9j10?t=28816)
*Tusker anchor:* Context only (2013 baseline for the 400x claim).

**N36 — YC batch stat: "a quarter of the companies had code bases that were 95% AI generated"**
> "In the winter 25 batch, a quarter of the companies had code base code bases that were 95% AI generated... 94 companies total have now crossed a hundred million in dollars in revenue from a seed check"
[8:01:53](https://youtu.be/I2cbIws9j10?t=28913)
*Tusker anchor:* Context/market-sizing only.

**N37 — Working-memory contrast: "seven things" (7±2) vs "a million tokens... about a thousand pages"**
> "you and I human beings, we only hold about seven things in our head at once. Uh 7 plus or minus two... an AI agent holds a million tokens. That's about a thousand pages."
[8:09:37](https://youtu.be/I2cbIws9j10?t=29377)
*Tusker anchor:* Justifies why config provenance must be externalized into a resolver table (a "prosthetic for that limit") — the operator can't hold three-source precedence in their head; the UI/table must.

### (c) Failure anecdotes (company-brain, transferable to config store)

**N38 — "A brain nobody curates becomes a garbage dump with great search"**
> "a brain nobody curates becomes a garbage dump with great search. Retrieval will surface a stale fact with total confidence... a bad skill file encodes a bad process forever."
[8:12:42](https://youtu.be/I2cbIws9j10?t=29562)
*Tusker anchor:* "Stale fact with total confidence" = a stale/overridden config value reported as authoritative. RUN-T-0002 provenance must show freshness/override state, not just a value. Curation (pruning dead sources) is required or the config store rots.

**N39 — GBrain named + framing: "Postgres for agents," a retrieval layer that picks "which three books"**
> "It's called Gbrain. It works with any harness but it loves openclaw Hermes agent. It's effectively Postgress for agents a retrieval layer whose job is to figure out for uh for any task what three books should be loaded"
[8:11:41](https://youtu.be/I2cbIws9j10?t=29501)
*Tusker anchor:* Named comparable (GBrain, MIT open source per [8:15:49](https://youtu.be/I2cbIws9j10?t=29749)). The "figure out which N to load for a task" pattern = the resolver deciding which config source applies. Same primitive at a different layer.

---

## Cross-talk synthesis (top decision-changing items)

- **RUN-T-0002 fix is named prior art: Garry's "trigger eval" (N24).** A test that asserts the intended source is the one actually loaded/won after a set — the direct antidote to the `ok:true` no-op setter. Plus "provenance on every fact" + "contradiction checks" + explicit arbitration (N25/N27) give the full resolver-with-provenance spec.
- **Config precedence belongs in deterministic space (N28/N29/N30).** The bug class ("confident, wrong, untraceable," N26) is computation on the wrong side. Resolution must be a pure deterministic function over durable stored sources — never reconstructed in an agent's context.
- **Serve's home screen is the "orchestration control plane" (N1/N3):** three named roster columns — working-on / blocked-on / handing-off-to — with macro fleet map as default and per-task drill-down as the exception. Operator's job is attention-routing to whatever needs unblocking (N4), reviewing overnight batches (N15/N17), gating only binding/outward actions (N16).
