# AIE World's Fair 2026 — Routing & Evals Mining

Mined for Tusker's model-routing design: task contracts carry risk/size/priority fields
that a router maps to model tiers, with a deterministic-first gate so trivial work never
hits an LLM. Each nugget is a VERBATIM auto-caption quote + timestamp link + one-line
Tusker anchor. Auto-captions contain transcription errors; `[bracketed]` text = my
correction of an obvious mis-transcription, never a paraphrase of the claim.

Sources:
- `29 - Artificial Analysis - Cost of Intelligence.md` (Micah Hill-Smith & George Cameron)
- `30 - Arena - Real-World Agent Evaluation.md` (Wayin Chiang, Arena / ex-LMSYS)

---

## Talk 29 — Artificial Analysis: Cost of Intelligence

Two AI-benchmarking co-founders. Directly relevant: how to source model-quality numbers
without building your own evals, and the exact archetype logic Tusker's router should copy
(ceiling vs no-ceiling tasks).

### (a) Concrete numbers — cost ratios, thresholds

**N1 — Not using the smartest model saves 10–1000x**
> "there exists a set of tasks that are inside the frontier and that that set of tasks is growing every month as new models come out. For that set of tasks, playing the intelligence cost trade-off is incredibly important because by choosing to not use the smartest model for every single thing, you can spend 10, 100, a thousand times less to get the same work done by the AI."
[8:08:12](https://youtu.be/4sX_He5c4sI?t=29292)
**Tusker anchor:** The whole economic case for the router in one sentence. The savings band (10–1000x) is the ROI ceiling; frame the router's success metric as "% of tasks demoted off the frontier tier without quality loss," not raw spend.

**N2 — Open-weights lag the frontier by a consistent ~3–9 months**
> "we see a consistent 3 to nmon [3-to-9-month] gap that's held surprisingly consistent over all of the last 3 years... within 9 months of Mythos being announced, we are predicting that someone's going to give away a copy of a model as smart as Mythos."
[8:10:50](https://youtu.be/4sX_He5c4sI?t=29450)
**Tusker anchor:** Router tier definitions age fast but predictably. A cheap open-weights tier reaches any given quality bar ~3–9 months after the closed frontier does — schedule tier re-benchmarking on that cadence rather than reacting per-release.

**N3 — Token prices fall 5–10x/year at fixed intelligence**
> "Token prices have continued to fall by 5 to 10x every year for each fixed level of intelligence. Each of the lines here is a band of 10 points of intelligence index."
[8:11:53](https://youtu.be/4sX_He5c4sI?t=29513)
**Tusker anchor:** Any hard-coded price threshold in the router decays 5–10x/year. Store tier boundaries as "intelligence-index band," not dollar figures, and let a price feed float underneath.

**N4 — A 10-point intelligence-index gap is near-total dominance**
> "if you ever have to pick between a model that's 10 points higher on our intelligence index than another model, it's incredibly hard to find any task at all in the full distribution of tasks that the model that is 10 points dumber will outperform the better model"
[8:11:53](https://youtu.be/4sX_He5c4sI?t=29513) / continues [8:12:23](https://youtu.be/4sX_He5c4sI?t=29543)
**Tusker anchor:** Gives a concrete quantization for tier gaps. If the router's candidate models are within ~10 index points, treat them as quality-equivalent and decide purely on cost/latency; only >10-point gaps justify paying up.

**N5 — GPQA (non-agentic reasoning) costs fractions-of-a-cent to ~50c/answer**
> "GBQA [GPQA] diamond famous important open source evaluation data set... It's a reasoning evaluation. We don't let the models work as agents. It's largely solved... We see from fractions of a scent [cent] per answer for each model up to about 50 cents."
[8:12:53](https://youtu.be/4sX_He5c4sI?t=29573)
**Tusker anchor:** Single-shot reasoning (no tool loop) tops out under $0.50/task even at the frontier. This is the cost floor for the "small, bounded" size class — anything costing dollars is being driven by the agent loop, not the model's raw price.

**N6 — Agentic tasks run past $20, some several times that**
> "In our coding agent index and in our new AA briefcase agent [k]nowledge work eval. We see up to beyond $20 being spent on a single task. The most expensive task in a briefcase is actually several times that."
[8:13:23](https://youtu.be/4sX_He5c4sI?t=29603)
**Tusker anchor:** Two-plus orders of magnitude between a bounded reasoning task (N5) and a long-horizon agentic task ($20–$100+). Task size class must gate model tier: a "large/agentic" contract on the frontier tier is where the money actually goes — that's where routing pays off most.

**N7 — 100x token-price spread, frontier vs workhorse**
> "there's two orders of magnitude difference in terms of the token price between Frontier models like Claude Fable 5 and still good very usable workhorse models like Deep Seek V4 Flash and GPT OSS120B."
[8:18:42](https://youtu.be/4sX_He5c4sI?t=29922)
**Tusker anchor:** The price delta between tiers is ~100x on tokens alone. Even a modest demotion rate to workhorse tier moves the total-cost needle hard. Name concrete workhorse candidates: DeepSeek V4 Flash, GPT-OSS-120B.

**N8 — Sonnet 5 burned >200K output tokens per briefcase task**
> "Claude Sonnet 5 released only yesterday used over 200,000 output tokens per task. Compare that to your chat[G]bt query... couple of hundred tokens, couple of thousand tokens, maybe 200,000 tokens to complete a task. And you can see here that models vary orders of magnitude."
[8:20:48](https://youtu.be/4sX_He5c4sI?t=30048)
**Tusker anchor:** Same model can be cheap or ruinous depending on how many turns/how verbose. Per-model list price is a weak router signal; you need per-model observed token-consumption on YOUR task type (see N19/Arena N21).

**N9 — Cache discount is 80–99%, varies by model/provider**
> "the cash [cache] discount for a cash hit of an input token. It's usually around 90% here, but it's also different for models and providers whereby some models here are 99% and others are around 80%... this can change by uh multiples a difference in a cash discount or a cash hit rate the total amount of an agentic task."
[8:22:22](https://youtu.be/4sX_He5c4sI?t=30142)
**Tusker anchor:** Cache-hit economics (80–99% off, model-dependent) can flip which tier is cheapest for a repeated/long-context task. Router cost model must include cache-hit rate as a first-class term, not just list price.

### (b) Named mechanisms / methodologies

**N10 — Intelligence Index = synthesis of 9 evals, v4.1**
> "we publish a metric called artificial analysis intelligence index... What this metric actually is is a synthesis across nine different emails [evals] that we run. We're at version 4.1 of our index. It includes a bunch of agentic stuff. It includes a bunch of hard reasoning Q&A type stuff."
[8:08:44](https://youtu.be/4sX_He5c4sI?t=29324) / [8:09:15](https://youtu.be/4sX_He5c4sI?t=29355)
**Tusker anchor:** Off-the-shelf model-quality data source. Don't build evals from scratch — pull Artificial Analysis Intelligence Index (single composite number, versioned) as the router's quality axis. It already blends agentic + reasoning.

**N11 — "Intelligence frontier" framing**
> "at any given moment in AI we've got this concept that we think of as the intelligence frontier what today's smartest models can do."
[8:07:42](https://youtu.be/4sX_He5c4sI?t=29262)
**Tusker anchor:** Vocabulary for tier design: the router's top tier = "frontier"; the interesting routing decisions live strictly inside the frontier (tasks the workhorse can already do).

**N12 — AA Briefcase = agentic knowledge-work benchmark, 3 grading dimensions**
> "Our AA briefcase benchmark is our new agentic knowledge work benchmark... There's four private scenarios, each representing weeks of human equivalent work... we grade models on the outputs of those tasks across three dimensions. Rubric correctness, analytical quality, and presentation."
[8:14:58](https://youtu.be/4sX_He5c4sI?t=29698) / [8:15:32](https://youtu.be/4sX_He5c4sI?t=29732)
**Tusker anchor:** A rubric template Tusker could reuse for evidence grading: correctness (rubric) + analytical quality + presentation. "Private scenarios" = held-out to avoid contamination; if Tusker ever self-evaluates runner output, keep the grading set private.

**N13 — Realism via messy multi-source environments**
> "you're not given it on a platter... You need to go out and find it. You need to troll through emails, pick up on the latest Slack messages... The environments... are thousands of files, messy Excel files, unstructured documents, structured documents and reports with hundreds of pages, emails, Slack messages."
[8:15:32](https://youtu.be/4sX_He5c4sI?t=29732) / [8:16:03](https://youtu.be/4sX_He5c4sI?t=29763)
**Tusker anchor:** Benchmarks that predict real agent performance must include the discovery cost (finding the inputs), not just the reasoning. A Tusker task contract's difficulty should account for input-discovery burden, not only the transformation.

**N14 — Four cost drivers of an agentic task**
> "Four drivers... the key drivers here are token price, the number of turns in the agent trajectory, the token efficiency and usage of models, and last but potentially most important, the impact of prompt caching."
[8:18:11](https://youtu.be/4sX_He5c4sI?t=29891)
**Tusker anchor:** Direct spec for the router's cost estimator. Cost = f(token_price, num_turns, token_efficiency, cache_hit_rate). Turns and cache are what per-call pricing misses.

**N15 — Almost all agentic-task tokens are INPUT tokens**
> "AA briefcase token breakdown answer tokens, reasoning tokens, input tokens. Can anybody see any output tokens here? They're all input tokens. The vast majority of tokens to complete longrunning agentic tasks are input tokens. You can barely see any output tokens there."
[8:21:52](https://youtu.be/4sX_He5c4sI?t=30112)
**Tusker anchor:** Counterintuitive and load-bearing: cost is dominated by input tokens (each turn's output re-enters as input next turn). Router cost model should weight input-token price × cache-hit rate far above output price.

### (c) Failure anecdotes / benchmark gotchas

**N16 — Average cost-per-task hides the extremes**
> "we look at cost per task across all of the emails and tasks... the number is going up. This is the average across every task which includes some agentic stuff, some non-agentic stuff. So it's actually hiding how extreme cost per task gets in some situations today."
[8:12:23](https://youtu.be/4sX_He5c4sI?t=29543)
**Tusker anchor:** Never budget or route off mean cost-per-task — the distribution is heavy-tailed. Tusker should track p95/max cost per task class, not just averages, or the frontier-tier tail will blow the budget.

**N17 — Verbose/expensive ≠ smart (Sonnet 5 nearly as costly as Fable 5)**
> "claude sonnet 5 actually uses an enormous number of tokens and so it's nearly [as] expensive in our AA briefcase tasks... even though that cost per token for each fixed level of intelligence is falling by 5 to 10x every year."
[8:13:23](https://youtu.be/4sX_He5c4sI?t=29603) / [8:13:54](https://youtu.be/4sX_He5c4sI?t=29634)
**Tusker anchor:** A cheaper-per-token model can cost more per task via verbosity. Rank router candidates by observed $/task on the task class, never $/token.

### (d) Asides / contrarian takes

**N18 — Start cost reasoning from the CACHE-HIT input price, not output tokens**
> "I think we're used to thinking about output tokens, but I'd ask us, let's start with the cash [cache] hit price when thinking about the cost of an angentic [agentic] task and tokens."
[8:22:56](https://youtu.be/4sX_He5c4sI?t=30176)
**Tusker anchor:** Contrarian ordering for the cost estimator: the primary term is cached-input price, output tokens are a rounding error for long agentic runs. Invert the naive "output tokens are expensive" intuition.

**N19 — Most knowledge work has NO intelligence ceiling**
> "The first archetype is a task whereby there's not a ceiling on how much intelligence you could want to complete the task. More intelligent equals better outputs. And this is the case for most knowledge work today in prof[essional] tasks... It can always be better... you want to look at the paro [Pareto] line here in making that decision."
[8:23:58](https://youtu.be/4sX_He5c4sI?t=30238) / [8:24:31](https://youtu.be/4sX_He5c4sI?t=30271)
**Tusker anchor:** For open-ended contracts (design, analysis, writing), there is no "good enough" floor — the router must expose a cost/quality knob (priority field → willingness-to-pay), not auto-pick cheapest. This is the risk/priority field's real job.

### (e) Practical model-selection workflow

**N20 — Two archetypes: ceiling vs no-ceiling → the routing rule**
> "The second archetype of task is whereby there's a ceiling. An example is how much did I spend on Stripe fees last month. A smarter model doesn't necessarily give you a different or a better answer... you want to think about what is the level of intelligence, the minimum level of intelligence that can complete the task. And then you want to choose the cheapest model that which is to the left on this chart."
[8:25:06](https://youtu.be/4sX_He5c4sI?t=30306)
**Tusker anchor:** THE routing algorithm, verbatim from a benchmarking firm. Ceiling tasks (verifiable, single correct answer — Stripe total, lookups, deterministic transforms) → pick the CHEAPEST model above a minimum-intelligence floor. No-ceiling tasks → walk the Pareto line by willingness-to-pay. Maps 1:1 onto Tusker: a "ceiling" flag on the contract + a risk-tier floor = pick-cheapest-that-clears-the-floor; deterministic-first gate is the degenerate case (floor = 0 model needed).

**N21 — 2026's key chart is intelligence-vs-cost-per-task**
> "the most important chart for understanding the AI landscape in 2026. In 2025, it was simpler. It was our intelligence index bar chart. Now we start with the intelligence versus cost per task as we are now wrestling with these trade-offs."
[8:23:27](https://youtu.be/4sX_He5c4sI?t=30207)
**Tusker anchor:** The router's decision surface should literally be a 2D intelligence×cost-per-task plot per task class; picking a model = picking a point on/under the Pareto frontier of that plot.

---

## Talk 30 — Arena: Real-World Agent Evaluation

Wayin Chiang, CTO of Arena (ex-LMSYS / Chatbot Arena / "LLM-as-a-judge" 2023). Directly
relevant: how to rank models from production traces (no hand-built eval set), the signal
taxonomy Tusker could mine from its own runs, and real-world vs list-price cost.

### (a) Concrete numbers

**N22 — Scale: 10M visitors, 700M conversations, $100M ARR in 8 months**
> "We now see 10 million monthly visitor going to uh our product... we have collected 700 million conversations across all the modalities... we just recently announced we hit 100 million um annualized revenue in just eight months after we first released our evaluation product."
[8:29:04](https://youtu.be/4sX_He5c4sI?t=30544) / [8:29:37](https://youtu.be/4sX_He5c4sI?t=30577)
**Tusker anchor:** Context on how much data underlies these leaderboards — Tusker can consume Arena rankings as an external quality feed rather than reproducing them.

**N23 — Agent share of output tokens: ~100% inside OpenAI, >60% for others**
> "if you look at the openi's data on codeex traffic the share of the output token coming from agent has just skyrocketed... inside openai essentially 100% of the uh output tokens from agent from codeex and for other organizations... average is like above 60% now and individual also climbing very fast."
[8:31:44](https://youtu.be/4sX_He5c4sI?t=30704) / [8:32:14](https://youtu.be/4sX_He5c4sI?t=30734)
**Tusker anchor:** Validates that agentic (multi-turn, tool-loop) traffic — exactly Tusker's runner workload — is now the dominant token consumer, so trajectory-level cost accounting is the right unit.

**N24 — Codex adoption ~90% across non-eng departments**
> "agents are not just for engineers... if you look at codeex adoptions by department at... openai engineering obviously 99% but also finance recruiting legal and so on they are all like almost like 90%."
[8:32:14](https://youtu.be/4sX_He5c4sI?t=30734)
**Tusker anchor:** Peripheral to routing, but supports generalizing task contracts beyond code to knowledge-work classes (finance/legal/recruiting) each with their own best-model.

**N25 — Token usage headed to ~60 quadrillion/month; AI spend ≈ half an engineer's salary**
> "the monthly token usage is also skyrocketing towards like you know 60 quadr[illion] quad trillion tokens in the next couple years... the top 1% of the company's monthly AI spend is per employee is actually already like 7 4K [$7.4K] um roughly half of the salary [of a] software engineer."
[8:32:47](https://youtu.be/4sX_He5c4sI?t=30767) / [8:33:19](https://youtu.be/4sX_He5c4sI?t=30799)
**Tusker anchor:** Spend is large enough that routing savings are a real budget line, not a rounding error — justifies investing in the deterministic-first gate + tier router.

**N26 — Agentic tasks: 100+ tool calls before any success/fail signal**
> "a task may take 100 to [tool] calls to to finish right before you know if it's succeeding or failing or you give any feedback of a chance to steer it."
[8:34:21](https://youtu.be/4sX_He5c4sI?t=30861)
**Tusker anchor:** Sparse, late reward. Tusker's per-task cost/quality signal only arrives after a long trajectory — reinforces trajectory-level (whole-run) accounting over per-call, and argues for cost caps that can abort a runaway run before the 100th call.

**N27 — 5.7M tool calls/week; bash = 46%**
> "this plot is the... usage of these tools uh in a... oneweek time frame you see 5.7 million to [tool] calls... bash is was the... number one used That's around 46%."
[8:37:28](https://youtu.be/4sX_He5c4sI?t=31048) / [8:37:58](https://youtu.be/4sX_He5c4sI?t=31078)
**Tusker anchor:** Bash/terminal dominates agent tool use — matches Tusker runners. Tool-call mix is itself a routable signal (a bash-heavy task profile ≠ a research/read-heavy one).

**N28 — 1M+ traces in first month; 50M+ lines of code**
> "in the first months over uh we collected over a million agentic traces... we see more than the half of these... traces fall into work related category more like towards professional use... we have seen [agents] also written um more than 50 million lines of code uh on arena."
[8:39:03](https://youtu.be/4sX_He5c4sI?t=31143) / [8:39:33](https://youtu.be/4sX_He5c4sI?t=31173)
**Tusker anchor:** Confirms production traces (not curated evals) as the data substrate — Tusker's own `.tusker/events` + evidence are a comparable trace store it could mine for its own model-selection signal.

**N29 — Fable 5 tops agent leaderboard by +14% over average**
> "right now fable five is the number one models that was... the net improvement of like 14% over the average which is the... average of all the models followed by call [Claude] opus [and] GP[T] fivei high."
[8:41:36](https://youtu.be/4sX_He5c4sI?t=31296)
**Tusker anchor:** Concrete external ranking point. "Net improvement over average of all models" is a clean normalization Tusker could adopt for comparing runner models on its own signals.

**N30 — Best model depends on task distribution (GDP vs consumer)**
> "you can slice it into any task distribution you care about... let's say you care about... GDP tasks this more like economically valuable professional work versus consumer use cases... GPD5i is actually pretty good... in terms of like GDP tasks... [and] Gemini tends to do better in consumer use cases... the best model generally depends on... what you're doing what you care [about], the distribution."
[8:43:11](https://youtu.be/4sX_He5c4sI?t=31391) / [8:43:43](https://youtu.be/4sX_He5c4sI?t=31423)
**Tusker anchor:** No global "best model" — routing must be per-task-class. Tusker should maintain per-category best-model tables (code vs research vs doc-gen), not one router table.

**N31 — Cost per session and per-token efficiency spread**
> "we basically can plot these... net improvement... against the average cost to see... the parto [Pareto] frontier here you can see fable is the one that's the best uh cost about $10 per session and 5ifi is still very bit cheaper and... GLM 5.2 [and] Gimme [Gemini] is like the most efficient one."
[8:44:14](https://youtu.be/4sX_He5c4sI?t=31454) / [8:44:45](https://youtu.be/4sX_He5c4sI?t=31485)
**Tusker anchor:** ~$10/session at the top tier is a concrete anchor for the "large agentic" cost class. Same Pareto-frontier framing as Artificial Analysis (N20/N21) — two independent firms converge on intelligence-vs-cost-per-session as the selection surface.

### (b) Named mechanisms / methodologies

**N32 — Three signal sources: explicit, implicit, environment**
> "we primarily... mine the signals from three type of... signals. One is like explicit... user will tell us directly like which task succeeded or failed... the other one is some implicit... if user is actually... downloading the file or... complaining about the output... or praising it... and also there's environment feedback where... what actually happened when the code run whether the command succeeded or failed."
[8:40:04](https://youtu.be/4sX_He5c4sI?t=31204) / [8:40:34](https://youtu.be/4sX_He5c4sI?t=31234)
**Tusker anchor:** Directly portable signal taxonomy for Tusker: (1) explicit — human gate PASS/FAIL; (2) implicit — did the reviewer accept, re-run, or complain; (3) environment — did the build/test/vet gate go green. Tusker already emits all three; aggregate them into a per-model score.

**N33 — Rich derived signals: success rate, compliance, durability, bash-recovery, hallucination**
> "aggregate them into... some of these signals like success rate praise over compliance durability bash recovery to [rate] hallucination and each of these signal can produce the ranking right you can measure precisely... which model performs better than other in this particular signal and we combine that into the final... leaderboard."
[8:41:06](https://youtu.be/4sX_He5c4sI?t=31266)
**Tusker anchor:** Multi-signal leaderboard, not one score. "Bash recovery" (does the model recover after a failed command) and "durability" are exactly the reliability traits a risk-tier floor should gate on — a high-risk contract should require a model with strong recovery/durability, not just high raw success.

**N34 — Methodology = randomized controlled trial over agent components**
> "methodologically the core idea is basically a randomized control trial where we intervene on agent component. We measure the causal effect of... any given component on the task outcome... this framework is general enough so we can also measure the interaction effect between different... components for example... different harness... or different system prompt."
[8:42:08](https://youtu.be/4sX_He5c4sI?t=31328) / [8:42:39](https://youtu.be/4sX_He5c4sI?t=31359)
**Tusker anchor:** To prove a router change helps, A/B it as an RCT holding harness/prompt/tools fixed and swapping only the model. Tusker could randomize model tier on equivalent tasks and measure causal effect on gate-pass rate — cleaner than before/after comparisons across drifting task mixes.

**N35 — Agents are multi-component; any piece can break the system**
> "First agents are multi-component systems, right? You got the model, the agent take [agentic] loop, um the tool, the harness... any of these pieces can break the system."
[8:33:49](https://youtu.be/4sX_He5c4sI?t=30829)
**Tusker anchor:** A failed run doesn't imply a bad model — could be harness/tool/loop. Tusker's failure attribution must separate model failure from harness failure before penalizing a model tier in the router's stats.

### (c) Failure anecdotes / gotchas

**N36 — List price lies; real-world token consumption differs at equal price**
> "if you only look at the list price you may see... some of the model is like same price but if you actually put it in the real world some of the model would use more tokens to... for the same task... for example GBD5i [GPT-5i] although it has similar... price... as OPUS but in the... real world it use less token fewer tokens to achieve the same task uh which is more efficient than the others."
[8:44:45](https://youtu.be/4sX_He5c4sI?t=31485) / [8:45:17](https://youtu.be/4sX_He5c4sI?t=31517)
**Tusker anchor:** THE trajectory-cost gotcha. Two models at identical list price can differ multiples in real cost because one completes the task in fewer tokens/turns. Tusker's router MUST use observed $/task-completed from its own traces, never provider list price. This is the single most decision-changing finding for the cost model.

**N37 — Best model varies signal-by-signal (good at success, weak at control)**
> "you can look at the signal by signal... the model may be really really good at test success but sometimes weaker in terms of... stability in terms how do you control the model and you can see exactly like where the model is failing."
[8:41:36](https://youtu.be/4sX_He5c4sI?t=31296)
**Tusker anchor:** A model can top raw success while being hard to steer/unstable. For high-risk contracts, weight controllability/stability, not just pass rate — the risk tier should select on the failure-mode signal that matters for that task, not the headline number.

### (d) / (e) Practical workflow

**N38 — Ground evaluation in real product use, not static benchmarks**
> "to deeply understand the problem at Arena we decided to actually firsthand building real world... agentic product and app to actually source the organic traces and feedback from the actual users... we believe the best evaluation should be... grounded and measured in real world use cases."
[8:34:53](https://youtu.be/4sX_He5c4sI?t=30893) / [8:38:30](https://youtu.be/4sX_He5c4sI?t=31110)
**Tusker anchor:** Endorses Tusker's own posture: its production task-completion evidence is a better model-quality signal than any static benchmark. Mine `.tusker` traces for routing decisions rather than importing only third-party leaderboards.

**N39 — Practical closing workflow: log traces → mine → link to business metric → pick model**
> "if you are building an agentic app... you should definitely be logging your agentic traces... log all the interactions between agent and the user... look into the data mind for insights and measure the outcome links to whatever business metrics you care and use that... real world data to choose the best model for you."
[8:45:49](https://youtu.be/4sX_He5c4sI?t=31549)
**Tusker anchor:** Four-step loop = Tusker's router feedback loop: (1) log every trajectory, (2) mine signals, (3) tie outcome to a business metric (gate-pass, human-accept, cost), (4) feed model selection. The router should be a closed loop over Tusker's own trace store, not a static config table.

**N40 — Tool-similarity: Arena's harness mirrors Claude Code**
> "we give model set of tools um file system tools rewrite edit and so on and search web fetching image... generation speech... just really giving the model tools similar to like a cloud co-work [Claude Code] like harness and also terminal access to run code."
[8:36:58](https://youtu.be/4sX_He5c4sI?t=31018) / [8:37:28](https://youtu.be/4sX_He5c4sI?t=31048)
**Tusker anchor:** Their leaderboard is measured on a Claude-Code-like tool harness — so their rankings transfer reasonably to Tusker's runner environment (file edit + bash + web), making Arena a usable external quality feed for Tusker's tiers.

---

## Cross-talk synthesis for Tusker's router

- **Convergent Pareto framing (N20, N21, N31):** Two independent benchmarking firms both land on
  *intelligence × cost-per-task/session* as the decision surface. Tusker's router should model
  exactly this 2D plot per task class and pick a point on/under the frontier.
- **Ceiling-vs-no-ceiling is the master rule (N20, N19):** ceiling/verifiable tasks → cheapest model
  above an intelligence floor (deterministic-first gate = floor of zero); open-ended tasks → walk the
  Pareto line by the contract's priority/willingness-to-pay field.
- **Cost model must be trajectory-level, cache-aware, input-dominated (N14, N15, N9, N18, N36):**
  list price and output-token price are both misleading. The real cost = input tokens × cache-hit
  price × number of turns × per-model token-efficiency observed on the task class.
- **Source quality data externally, but calibrate on your own traces (N10, N38, N39, N36):** pull
  Artificial Analysis Intelligence Index / Arena leaderboards as the starting quality axis; refine with
  observed $/task-completed and multi-signal reliability (N32–N34) from Tusker's own run history.
- **Risk tier should gate on failure-mode signals, not headline success (N33, N37, N35):** high-risk
  contracts need controllability/durability/bash-recovery, and failure attribution must separate model
  from harness before the router penalizes a tier.
