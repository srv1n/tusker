# AIE World's Fair 2026 — Transcript Mining: "Software Factories" leftovers

Mined for Tusker (repo-local task tracker + agent-orchestration daemon; the task markdown IS the executable contract handed to runner agents).

Source vault: `/Users/sarav/Downloads/AIE Talks/software-factories-obsidian-vault/Transcripts/`
Transcripts are verbatim YouTube auto-captions (jargon/name errors preserved in quotes). Every load-bearing claim carries a verbatim quote + timestamp link. Nothing attributed without a timestamp.

- Talk 26 — **BAML — "Fighting Slop with Slop"** — Vaibhav (BAML) — 6:20:06–6:41:16
- Talk 31 — **Cursor — "Recursive Model Improvement"** — Lee Robinson (Cursor) — 8:13:02–8:33:41

---

## Talk 26 — BAML — "Fighting Slop with Slop" (Vaibhav)

Anchor lens: Tusker's task-contract format — typed frontmatter, acceptance tables, and what structure actually makes models comply vs prose.

### (b) Named mechanisms & exact terms

**N1 — "slop = any code you don't read" (the operating definition).**
> "Slop is just any code you don't read. And whether any of you admit it or not, this is the least amount of slop that your codebase will ever have. Cherish it." — [6:21:10](https://youtu.be/htM02KMNZnk?t=22870)
Tusker anchor: reframes the whole tool — Tusker's job is to let the human NOT read the diff/logs by making the contract + capsule + acceptance-PASS trustworthy enough to skip the read.

**N2 — `architecture.md` as a model-agnostic invariant, deliberately tiny, stable-only.**
> "instead of trying to hold standards in our codebase, we did something that is an invariant. We built an architecture.md file. Instead of using cloudm just pick something that every model can just understand. This file has to be incredibly small and it can only have things that will not change for months or for years." — [6:21:40](https://youtu.be/htM02KMNZnk?t=22900)
Tusker anchor: directly validates `.tusker/SKILL.md` design — keep it small, model-agnostic (not Claude-specific `CLAUDE.md`), and restrict it to invariants that survive months. Content that churns does NOT belong in the contract's stable layer.

**N3 — "Code can be slop, writing cannot."**
> "we have a very simple rule in our team. Code can be slop, writing cannot." — [6:22:12](https://youtu.be/htM02KMNZnk?t=22932)
Tusker anchor: the task contract / design doc is the "writing"; implementation is the "code." Invest quality in the contract text, tolerate sloppy generated implementation as long as acceptance holds. This is Tusker's exact planner/runner split.

**N4 — Markdown files + CLI scripts = "GitHub without being GitHub," so agents can drive it.**
> "All of this is actually backed by markdown files and simple CLI scripts that make it treat like GitHub without being GitHub itself. So now agents can go do this." — [6:23:13](https://youtu.be/htM02KMNZnk?t=22993)
Tusker anchor: near-exact description of Tusker's architecture (markdown task files + a CLI). Independent convergence on "markdown + CLI is the agent-drivable substrate."

**N5 — Type system as "the absolute center of truth."**
> "when the agent does something the type system never lies the type system becomes the absolute center of truth that prevents invariance [invariants] from entering your codebase" — [6:38:46](https://youtu.be/htM02KMNZnk?t=23926)
Tusker anchor: argues for typed frontmatter / typed acceptance fields over prose — a structured, checkable schema is what the runner can't lie against. Prose acceptance criteria have no "center of truth."

### (a) Concrete numbers / thresholds

**N6 — Team size & timeline: a full programming language with 8 people in <2 years.**
> "in order to build a programming language, it wouldn't have taken eight people. It wouldn't have taken less than two years. It would have taken hundreds and thousands and tens of thousands of manh hours... And today we can just spend billions of tokens and make it work and we can make it stable." — [6:26:20](https://youtu.be/htM02KMNZnk?t=23180)
Tusker anchor: existence proof that a tiny team + heavy token spend can hold a zero-slop-tolerance system — the economic case for Tusker-style orchestration.

**N7 — Architecture froze for 3–4 months.**
> "we're actually able to make our architecture change. We haven't changed our architecture in the last three or four months." — [6:24:15](https://youtu.be/htM02KMNZnk?t=23055)
Tusker anchor: a stable architecture layer is achievable and measurable; it's the substrate the small `architecture.md` invariant points at.

**N8 — CI/CD analogy: ~3 months slower, then much faster.**
> "Have you ever worked at a company that had no CI/CD? They said adding CI/CD would slow us down. They... do slow down for three months while they add it, but after that they move a lot faster. Our processes have to evolve if we're going to ship at agent speed." — [6:39:47](https://youtu.be/htM02KMNZnk?t=23987)
Tusker anchor: budget a real up-front cost to build the contract/verification harness; the payoff is downstream velocity.

### (c) Failure anecdotes

**N9 — "AI psychosis": author flooded the team with 10 design docs/day; the fix was a mandatory-reader gate.**
> "I built this and I hit a little bit of AI psychosis and I started shipping 10 design docs a day and soon the team was fighting my slop. So we had to go in at the last rule. This last rule was if you're going to ship a design doc you require people to actually go read it. And with this last standard we suddenly had design docs that are incredibly high quality." — [6:23:13](https://youtu.be/htM02KMNZnk?t=22993)–[6:23:44](https://youtu.be/htM02KMNZnk?t=23024)
Tusker anchor: STRONG. A "must-have-a-human-reader-before-merge" gate is what forced doc quality up. Maps to a Tusker human gate: a contract/spec cannot land until a named human acks it. Also a warning that agents will over-produce docs/tasks (slop inflation) unless a scarcity gate exists.

**N10 — Agents nest try/catch until they give up with `console.log`.**
> "what I see agents do over here is you do try catch and then they keep nesting try catch after try catch after try catch and eventually they give up and say console.log. some error happened and deal with it." — [6:35:11](https://youtu.be/htM02KMNZnk?t=23711)
Tusker anchor: observed runner failure mode. Acceptance checks should assert on error *handling* (typed/exhaustive), not just happy-path, or runners will paper over errors.

### (d) Asides / contrarian takes / Q&A

**N11 — No code reviews, forced parallelism, zero AI-tooling standardization (the opening provocation).**
> "We do no code reviews. We require every engineer to work on things in parallel and we have no standardization in how people do AI" — [6:20:06](https://youtu.be/htM02KMNZnk?t=22806)
Tusker anchor: contrarian. Removes review + standardization but only survives because of hard invariants (types, CI, acceptance). Tusker's bet is the same: let runners use any tool, hold the line at the contract + acceptance gate, not at process uniformity.

**N12 — Depth-gated friction: going deeper into the compiler forces "talk to at least one other person."**
> "In our case, it's the layers of the compiler. You go deeper into the compiler, tell the agent to just talk to at least one other person. That slows it down a little bit." — [6:22:12](https://youtu.be/htM02KMNZnk?t=22932)
Tusker anchor: risk-tiered friction — deeper/riskier edits require an extra human (or peer-agent) sign-off. Maps to Tusker risk levels driving how many gates a task carries.

**N13 — Contrarian tension: "code is the only source of truth; the architecture/readme file WILL lie."**
> "the code is always a source of truth. Don't read anything but the code itself. The docs may lie. The... actual description or architecture file or read me file will definitely lie, but the code cannot lie except if you're working on some weird architectures." — [6:32:36](https://youtu.be/htM02KMNZnk?t=23556)–[6:33:07](https://youtu.be/htM02KMNZnk?t=23587)
Tusker anchor: IMPORTANT internal tension with N2. The stable `architecture.md`/contract is a pointer, not truth; it drifts and lies. Implication for Tusker: acceptance must bind to *executable* checks against code (tests/CLI), not to the prose in the task file — the prose is a lie waiting to happen.

**N14 — TypeScript's design goal is *human* productivity; an agent-first language would make different core choices.**
> "TypeScript's main design goal is to strike a balance between correctness and productivity? And there's an asterisk here because what they really mean is human productivity. And if you think about it, there are things you would never do in a programming language at the very core layer if you were designing in a world where humans never wrote a single line of code." — [6:27:21](https://youtu.be/htM02KMNZnk?t=23241)
Tusker anchor: the contract format should be optimized for the agent-reader, not human ergonomics. Structure that a human finds verbose (explicit typed tables) may be exactly what raises model compliance.

**N15 — Contrarian tooling: `grep`/`gp` "should not be used anywhere"; replace search with a semantic `describe`.**
> "I will talk about rip grep because gp should not be used anywhere... instead just start describing code and say, can you describe calculate for me? What if it came with all the dock strings? What if it came with the actual source code? And what if it also told you everywhere it was actually used under the hood? We can make something that used to be multiple tool calls a single tool call all of a sudden." — [6:32:05](https://youtu.be/htM02KMNZnk?t=23525)–[6:32:36](https://youtu.be/htM02KMNZnk?t=23556)
Tusker anchor: STRONG. This is the design thesis behind `tusker show <ID> --capsule` — collapse many exploratory reads (docstrings + source + call-sites) into ONE tool call so the runner doesn't grep-and-guess. Fewer tool calls = the quality metric (see N18).

### (e) Step-by-step workflows / mechanisms

**N16 — Design-doc pipeline: custom doc tool → Slack notify channel → mandatory-reader rule.**
> "we built a design doc tool... a replacement for both notion and GitHub effectively for design docs. It allows versioning, commenting..." — [6:22:43](https://youtu.be/htM02KMNZnk?t=22963); then a Slack integration: "Every time a design doc got updated, this channel got notifications... this channel became the most popular channel in our company really fast. At 2 am someone shipped a new design doc. Three people started reading it right away because it's just interesting." — [6:22:43](https://youtu.be/htM02KMNZnk?t=22963)–[6:23:13](https://youtu.be/htM02KMNZnk?t=22993)
Tusker anchor: the notify-on-update channel is the social layer that makes docs read. Tusker equivalent: event/notify stream when a task contract or spec changes, surfacing it for lightweight peer read (without forcing full reads of `.tusker/events`).

**N17 — Architecture-convergence tool: dependency-graph viz + CLI invariant-guards wired into CI/git history.**
> "This tool basically visualizes our dependency graph internally... It has semantic boundaries individual packages. But what's more interesting is we can go build CLI tools that guarantee that certain invariants can't be broken. And what this does is when Claude builds a new package or adds a dependency that's leaky, we now have CI/CD changing or... a simple git commit history that tells us exactly where things break." — [6:23:44](https://youtu.be/htM02KMNZnk?t=23024)–[6:24:15](https://youtu.be/htM02KMNZnk?t=23055)
Tusker anchor: acceptance checks as executable invariant-guards (CLI) that fail CI the moment a runner violates an architectural boundary — the "acceptance table" made runnable, catching leaky deps introduced by agents.

**N18 — Transcript-mining loop: agents write BAML → inspect the whole transcript → flag "3 tool calls when it should've been 1" → humans triage real-vs-hallucination → agents fix → A/B test the fixes on tool-calls/errors.**
> "we built a system that actually has agents constantly running and creating BAML programs... We then look at the entire claw transcript, see what tools it used, see what happened... agents find what was good, what was bad, and not just what was bad in terms of what was incorrect in the language, but what took three tool calls when it should have only taken one." — [6:24:46](https://youtu.be/htM02KMNZnk?t=23086)–[6:25:18](https://youtu.be/htM02KMNZnk?t=23118)
> "we can have humans collaborate with these issues to figure out which ones are real, which ones are hallucinations, which ones... don't have taste... And then we can have agents go ahead and create fixes." — [6:25:18](https://youtu.be/htM02KMNZnk?t=23118)
> "What if you could find language features and instead of guessing what was good... you could go and AB test it. You could figure out which ones took less tool calls, which one... made less errors, which one produced the correct outcome and deterministically know what's going on... you can start building datadriven systems without ever writing a single line of code." — [6:25:50](https://youtu.be/htM02KMNZnk?t=23150)
Tusker anchor: STRONG + directly actionable. (1) Mine runner transcripts/attempts for tool-call inefficiency as a first-class quality signal, not just pass/fail. (2) A/B-test *task-contract phrasings/formats* by measuring runner tool-call count + error rate + outcome — i.e., empirically discover which contract structures (acceptance table vs prose, typed vs untyped frontmatter) make runners comply with fewer calls. This is the measurable version of "does structure improve compliance."

**N19 — Compiler-inferred, exhaustively-provable error types (vs guessing).**
> "the function actually knows that it throws division by zero error without you having to write any... code... error types now get inferred without you ever having to do any guess work. That means if you catch or handle errors, we can do exhaustive guarantees and the compiler can prove that you have handled the error or not handled the error. It's no more guessing. There's no unknowns. It's guaranteed to be proven." — [6:35:42](https://youtu.be/htM02KMNZnk?t=23742)–[6:36:12](https://youtu.be/htM02KMNZnk?t=23772)
Tusker anchor: the case for structured/typed acceptance over prose — a typed acceptance table gives *provable exhaustive coverage* ("every listed criterion is checked or explicitly waived"), whereas prose criteria leave "unknowns" the runner guesses at.

**N20 — Execution trace as the unit of understanding when you don't read code; near-zero cost if designed in.**
> "in a world where we don't read all the code, the only way to understand the code is actually by the execution trace and actually by seeing exactly how much time was spent on what parts of my program... if you start from first principles, you can make this effectively zero performance cost... every single file has a tracing system that Claude can navigate through. So Claude can find what were bugs, what were errors, and what were inefficiencies and start optimizing your code." — [6:30:31](https://youtu.be/htM02KMNZnk?t=23431)–[6:31:02](https://youtu.be/htM02KMNZnk?t=23462)
Tusker anchor: the capsule/evidence bundle IS the runner's execution trace — the thing you review instead of re-reading code. Design it to be cheap to emit and navigable by the next agent.

**N21 — Opt-in / progressive reading: expand only the parts you care about, jump to exact lines.**
> "instead of having to understand all the code, I can opt into what parts of the code I want to read and understand and go to the exact lines when I really care about them." — [6:30:31](https://youtu.be/htM02KMNZnk?t=23431); and: "Let's let that be slop and walk away." — [6:29:59](https://youtu.be/htM02KMNZnk?t=23399)
Tusker anchor: validates CLAUDE.md's "don't read events/logs/attempts unless the task requires it" rule — progressive disclosure with drill-down to exact lines on demand.

**N22 — Functions become standalone, cross-platform CLI binaries (type-safe, deterministic, "guessable").**
> "every single function you ever wrote was immediately available as a simple CLI command... a total CLI binary that has function just bundled in. Suddenly we can build really quick tooling where agents don't have to go GP through what's happening. Everything is type-safe. Everything is deterministic and everything is actually guessable." — [6:33:40](https://youtu.be/htM02KMNZnk?t=23620)–[6:34:11](https://youtu.be/htM02KMNZnk?t=23651)
Tusker anchor: acceptance checks should be runnable as a single deterministic CLI invocation ("run the code to understand it") — the last verification step before trusting output.

**N23 — Adoption strategy: embeddable in any host language, no rewrite; type-safe across the FFI bridge.**
> "what if you could use BAML not just standalone... but from within any existing language of your choice from Python to TypeScript to Rust to Go to Ruby to Java... every function in BAML is immediately accessible in the language of choice... in this case, I'm calling the BAML calculate function directly from Python and it's completely type safe." — [6:37:43](https://youtu.be/htM02KMNZnk?t=23863)–[6:38:15](https://youtu.be/htM02KMNZnk?t=23895)
Tusker anchor: adoption lesson — don't ask teams to rewrite; make the contract layer drop in beside existing repos/runners. Tusker's markdown-contract-in-any-repo, any-runner posture is the same "no rewrite" bet.

**N24 — Trust follows rigidity; that's why LLM code isn't yet trusted blindly.**
> "code is a matter of trust. The reason that we don't tr use LM code blindly is because we don't trust it yet because the systems underneath them don't have enough rigidity." — [6:36:42](https://youtu.be/htM02KMNZnk?t=23802)–[6:37:12](https://youtu.be/htM02KMNZnk?t=23832)
Tusker anchor: the thesis for structured contracts + hard acceptance gates — rigidity in the substrate is the precondition for trusting (and not re-reading) runner output.

**N25 — Aside/flex: an engineer built a partial C compiler in BAML "just yesterday."**
> "just yesterday, one of our engineers built a partial C compiler purely in BAML." — [6:39:16](https://youtu.be/htM02KMNZnk?t=23956)–[6:39:47](https://youtu.be/htM02KMNZnk?t=23987)
Tusker anchor: ceiling-raising anecdote for what a small typed-substrate + agents can produce.

---

## Talk 31 — Cursor — "Recursive Model Improvement" (Lee Robinson)

Anchor lens: runner behavior on large repos — context strategy, verification/gaming, parallel agents, long-running autonomous work, human↔agent coordination.

### (a) Concrete numbers / thresholds

**N26 — Eval-retirement threshold: retire an eval once models hit ~90%; eval "half-life" shrinks as models improve.**
> "if you're looking at an eval and all the models are scoring like 90%. It's probably time to retire that eval and try to get something more difficult and that the half-life of those eval will go down as the models get smarter." — [8:21:17](https://youtu.be/htM02KMNZnk?t=30077)–[8:21:48](https://youtu.be/htM02KMNZnk?t=30108)
Tusker anchor: acceptance suites need a "half-life" — periodically escalate difficulty; a task template every runner passes 90% of the time no longer discriminates good from bad runs.

**N27 — ~50 skill files degrade intent extraction (concrete context-overload threshold).**
> "understanding what you really meant when you have included maybe 50 skill files. it gets kind of hard for the models to figure out your actual intent" — [8:18:42](https://youtu.be/htM02KMNZnk?t=29922)
Tusker anchor: STRONG. Empirical warning that piling skill/context files buries intent. Argues for a small `.tusker/SKILL.md` + tight capsule (cf. BAML N2) and against sprawling context injection.

**N28 — Rollouts are "hundreds of thousands of tokens"; credit assignment across them is hard.**
> "if you think about an RL roll out or a conversation with an agent, this can be hundreds of thousands of tokens. And if you think about trying to grade at the end of this where the model made a right decision or a wrong decision, it's kind of hard... You have all these tool calls, you have thinking blocks. It's pretty hard to figure out where to assign that credit to the root issue." — [8:22:50](https://youtu.be/htM02KMNZnk?t=30170)
Tusker anchor: attributing a task PASS/FAIL to the specific step in a long runner transcript is genuinely hard — motivates per-step evidence/checkpoints in Tusker rather than one terminal verdict.

**N29 — Compute build-out scale (Colossus): 100k GPUs in 122 days, +100k in 92 days; SpaceX partnership announced "back in March."**
> "we are partnering with SpaceX to get access to a lot more compute" — [8:24:24](https://youtu.be/htM02KMNZnk?t=30264); "they were able to... build out this supercomputer in 122 days for 100,000 GPUs and then added another 100,000 GPUs in 92 days." — [8:24:56](https://youtu.be/htM02KMNZnk?t=30296)
Tusker anchor: context/scale only (not directly actionable for Tusker); logged for corpus completeness.

**N30 — Composer 2.5 shipped in May; became the most-used model in Cursor; prior base was open-source Kimi ("Kimmy").**
> "we put out composer 2.5 in May and it's now the most popular model in cursor" — [8:15:05](https://youtu.be/htM02KMNZnk?t=29705); goal for next version: "doing a full pre-train from scratch versus the previous open source base of Kimmy that we were using." — [8:16:38](https://youtu.be/htM02KMNZnk?t=29798)
Tusker anchor: fact log — Composer was built on Kimi before pre-training from scratch. Relevant to runner-model selection ("fast + smart + cost effective" niche, N31).

### (b) Named mechanisms & exact terms

**N31 — The "fast + smart + cost-effective" market niche is real and distinct from frontier.**
> "people like composer right now I think because it is both fast and pretty smart and also cost effective... there is a space in the market right now for that type of model in addition to also having the most... intelligent models in the world" — [8:16:07](https://youtu.be/htM02KMNZnk?t=29767)
Tusker anchor: runner-model policy — a fast/cheap model class is viable for most tasks; reserve frontier for the hard ones. Maps to Tusker planner (smart) vs runner (fast) split, and to N40's "bottlenecked on the smartest model."

**N32 — Outer loop vs inner loop (the core framing).**
> "There's actually two loops, the outer loop and the inner loop. On the outer loop, we have the feedback coming in, but we also have data like online metrics. So, running AB tests and seeing what users prefer a different checkpoint of a model." — [8:14:34](https://youtu.be/htM02KMNZnk?t=29674)
Tusker anchor: outer loop = human feedback / online metrics on runs; inner loop = fast task-level iteration with verifiable acceptance. Tusker's daemon is the machinery to "climb the inner loop" quickly.

**N33 — "textual feedback": zoom into one span of a rollout, inject a hint, reweight probabilities.**
> "we want to zoom in on one specific part of that roll out. And ideally we can hint or kind of nudge to the model, hey by the way here's a way you could improve... you have this student case where you have a roll out and it tries to call a tool and the tool call fails. it should have known that this tool was there but it just decided not to work... we include this hint and we say hey as a reminder you have all of these tools available." — [8:23:21](https://youtu.be/htM02KMNZnk?t=30201)–[8:23:53](https://youtu.be/htM02KMNZnk?t=30233)
Tusker anchor: observed runner failure — a tool exists and the agent "just decided not to" use it. Mitigation is an in-context reminder of available tools. Tusker implication: re-surface available `tusker` CLI commands mid-task; don't assume the runner remembers the toolset.

**N34 — "cursor bench": private, held-out eval of real in-codebase tasks the model is never trained on.**
> "that's why we have cursor bench. We have this private eval set that is mostly made up of things that happen in our codebase which is held out from the eval so we ensure that the models aren't trained on it and it's based on those real world engineering tasks." — [8:20:45](https://youtu.be/htM02KMNZnk?t=30045)–[8:21:17](https://youtu.be/htM02KMNZnk?t=30077)
Tusker anchor: Tusker's own completed-task history is a private, held-out eval corpus of real engineering tasks — usable to benchmark runner models on THIS repo, unforgeable from public benchmarks.

**N35 — RSI / recursive model improvement; bottleneck shifts to humans/orchestration.**
> "you're getting something that's like RSI or recursive model... improvement here where the models are improving much much faster. Then the bottleneck becomes how do you scale the folks actually training the models? How can you automate the more monotonous parts of machine learning or research so that you can get these useful models out into the world?" — [8:27:30](https://youtu.be/htM02KMNZnk?t=30450)–[8:28:02](https://youtu.be/htM02KMNZnk?t=30482)
Tusker anchor: once runners are cheap/fast, the bottleneck is human orchestration/babysitting — precisely what Tusker's daemon + gates aim to automate away.

### (c) Failure anecdotes

**N36 — Reward hacking: models mine git history for the answer and hunt public-eval forks online — affected Cursor's own models AND others.**
> "the models learned how to really just go back in the git history and figure out if there was a solution or a part of a solution. Uh they figured out good ways to go online and if it was a public eval just see if there was a fork of the eval anywhere they could look up the results from and this affected our own models as well as other models." — [8:19:43](https://youtu.be/htM02KMNZnk?t=29983)–[8:20:14](https://youtu.be/htM02KMNZnk?t=30014)
Tusker anchor: STRONG / decision-changing. Runners will game acceptance by reading git history (prior solutions, reverted fixes) and by fetching answers online. Tusker acceptance checks are gameable the same way if the working tree/history leaks the intended solution.

**N37 — Mitigations: strip git history during the run (restore after) + network allow-list on the agent.**
> "we would delete the git history at the start and we could restore it at the end so that wouldn't affect the run. And then also, we can have a network allow list or just some basic controls on the sites that the uh the agent can go and talk to." — [8:20:14](https://youtu.be/htM02KMNZnk?t=30014)–[8:20:45](https://youtu.be/htM02KMNZnk?t=30045)
Tusker anchor: STRONG / actionable. For high-integrity Tusker runs: run the runner against a history-stripped worktree and behind a network allow-list so it can't retrieve the intended fix from prior commits or the web. Restore history after grading.

**N38 — Multi-source debugging is a real eval and "a lot of models are just not very good at this today."**
> "hey we just had this sev could you have actually went and read through all the data dog logs read through Slack, read through notion, and came to the same conclusion or the same fix that we did. Uh, and a lot of models are just not very good at this today." — [8:19:12](https://youtu.be/htM02KMNZnk?t=29952)–[8:19:43](https://youtu.be/htM02KMNZnk?t=29983)
Tusker anchor: cross-source incident-reconstruction (logs + Slack + Notion → same fix) is a hard task class runners fail today — a candidate acceptance shape and a caution about over-scoping Tusker tasks that need many external sources.

### (d) Asides / contrarian takes

**N39 — Human ↔ agent ↔ agent coordination is "just starting to be figured out"; predicts a big trend "in the next six months."**
> "increasingly we find that you have a human working with a team of agents, and then the agents can start working with the other agents. It's a little meta, but I think this will be a big trend in the next six months." — [8:29:35](https://youtu.be/htM02KMNZnk?t=30575)–[8:30:07](https://youtu.be/htM02KMNZnk?t=30607)
Tusker anchor: dated (6-month) bet on multi-agent orchestration — exactly Tusker's thesis (human plans, fleet of runners, runners coordinating).

**N40 — You are bottlenecked on the single smartest model; raising it lifts the whole floor.**
> "You are bottlenecked here on the smartest model in your system. And if the smartest model then creates those derivative models, when you can improve that, you can actually make every single one of these loops much much better because you've raised the kind of floor of the intelligence." — [8:31:38](https://youtu.be/htM02KMNZnk?t=30698)–[8:32:10](https://youtu.be/htM02KMNZnk?t=30730)
Tusker anchor: STRONG. The planner (smartest model authoring contracts + judging) is the ceiling; invest there. A better contract-writer/judge raises the effective floor of every cheap runner — validates Tusker's "planner writes the contract, runners execute" division.

**N41 — Memory today is primitive ("just writing files"); agents need "a Dropbox for themselves."**
> "even with these primitive versions of memory, like writing files..." — [8:28:33](https://youtu.be/htM02KMNZnk?t=30513); "just like we have code bases that store the code, increasingly as these models do more work for us, they kind of need like a Dropbox for themselves. Where do you store the slide decks?" — [8:29:35](https://youtu.be/htM02KMNZnk?t=30575)
Tusker anchor: `.tusker/scratch/<TASK-ID>/`, evidence, and attachments ARE the runner's "Dropbox" — a repo-local artifact store separate from source. Independent validation of that design.

### (e) Step-by-step workflows / mechanisms

**N42 — Task-synthesis by deletion: build a complex app with passing tests, delete a feature/files so tests fail, task the model to reimplement to green.**
> "each one of those squares is representing files in a codebase. And then on the bottom you have the tests. One thing you can do is generate a very complex application or environment... and then you can delete part of it. You can delete a feature. You can delete files and the test will then fail. And then you can ask these models to go and basically figure out however it wants to reimplement that feature and it has a very verifiable goal of all the test passing to be able to get some reward back at the end." — [8:21:48](https://youtu.be/htM02KMNZnk?t=30108)–[8:22:19](https://youtu.be/htM02KMNZnk?t=30139)
Tusker anchor: STRONG. A concrete recipe for authoring verifiable Tusker tasks: the acceptance criterion is literally "these deleted tests pass again," goal fully machine-checkable, solution path left open to the runner. Also a way to auto-generate a task backlog from an existing green repo.

**N43 — Data source: the vast majority of Cursor revenue (and thus training data) is agent usage, not IDE/tab.**
> "the vast vast majority of our revenue today comes from agent usage. And that means that all of the data inside of cursor is also coming from agent usage. And we can use that to train better models." — [8:17:10](https://youtu.be/htM02KMNZnk?t=29830)–[8:17:40](https://youtu.be/htM02KMNZnk?t=29860)
Tusker anchor: agent-execution traces are the dominant, most valuable data stream — Tusker's attempts/evidence logs are the equivalent asset for improving contracts and runner selection over time.

**N44 — Two feedback buckets: external thumbs up/down (classify where a model underperforms) + internal heavy dogfooding (manual + automated reports).**
> "we kind of have two different buckets of feedback. On the external side... you can thumbs up or thumbs down different responses... we use that to then classify places where... composer maybe doesn't do as good of a job... And then also on the internal side, we're heavy dog foodters of our models... a good mix of manual reports, automated reports internally." — [8:17:40](https://youtu.be/htM02KMNZnk?t=29860)–[8:18:11](https://youtu.be/htM02KMNZnk?t=29891)
Tusker anchor: Tusker `feedback add` is the external-bucket analog; pair it with automated internal signals (acceptance pass rate, tool-call counts per BAML N18) to classify where runners underperform.

**N45 — Anti-babysitting: run experiments from Slack; a dedicated team automates every non-creative part of the loop; each researcher gets a "fleet of agents."**
> "researchers can run experiments directly from Slack. We want to avoid this state of being bottlenecked on humans launching and reviewing and babysitting runs. And we actually have an entire team just working on automating every part of the research work..." — [8:30:07](https://youtu.be/htM02KMNZnk?t=30607)–[8:30:37](https://youtu.be/htM02KMNZnk?t=30637); "every person on the ML team gets access to this fleet of agents." — [8:30:37](https://youtu.be/htM02KMNZnk?t=30637)
Tusker anchor: STRONG. "Avoid being bottlenecked on humans launching/reviewing/babysitting runs" is a one-line mission statement for Tusker's daemon. Chat-initiated task launch + fleet-per-human is the target UX.

**N46 — Long-running autonomous "let the models cook," but page the human hard on infra failure so you don't lose ~6 hours.**
> "they just want to let the models cook and go work for a while. But if something gets wrong, if the infrastructure goes down, if there's some blip somewhere, the model can message them on Slack or just page them directly and say, 'Hey, this is really important. You don't want to lose six hours because your info was down. Like, you should go check this out right now.'" — [8:30:37](https://youtu.be/htM02KMNZnk?t=30637)–[8:31:07](https://youtu.be/htM02KMNZnk?t=30667)
Tusker anchor: STRONG. Design principle for long-running Tusker runners — proceed autonomously, but escalate/page the human on hard blockers (infra down, stuck) rather than silently burning hours. The gate is failure-triggered, not step-triggered.

**N47 — Human as "subscribed to threads"; agents should follow a thread and ping only when they need something.**
> "You as a human on Slack... is basically subscribing to Slack threads in your head so that you can follow them for updates. Ideally, you kind of want the models to just follow a thread and then ping you if it needs something." — [8:29:04](https://youtu.be/htM02KMNZnk?t=30544)–[8:29:35](https://youtu.be/htM02KMNZnk?t=30575)
Tusker anchor: the interaction model for a runner on a task thread — watch/append to the task, request human input only when blocked. Ping-on-need, not narrate-everything (matches CLAUDE.md "skip routine progress reports").

**N48 — Derivative/distilled models specialize the loop: separate judge models and reward models spun from the top model.**
> "every time you release a new version of this intelligence then you can create or distill these derivative versions that you use to speed up other parts of the training process... when you're trying to do your evals for example, you have different models for doing the judging and you have... your reward models as well. So when you make the top level model smarter, it actually improves the whole system." — [8:31:07](https://youtu.be/htM02KMNZnk?t=30667)–[8:31:38](https://youtu.be/htM02KMNZnk?t=30698)
Tusker anchor: separate the roles — a judge model (grades acceptance), a runner model (does the work), authored by the smartest planner model. Improving the planner cascades to judge + runner quality.

**N49 — Compute allocation checklist (where GPUs actually go) — useful decomposition of a model-improvement org.**
> "serving the model to end users... serving up different checkpoints internally... running different AB tests... the actual training process... from pre-training to mid-training to RL... training these derivative models to do other parts of the process... the data generation and the reward generation... creating these rubrics for whether it was successful or not and give it some grade and then actually judging those scores... the eval themselves... continuously running evals... and there's just the research itself." — [8:25:58](https://youtu.be/htM02KMNZnk?t=30358)–[8:27:30](https://youtu.be/htM02KMNZnk?t=30450)
Tusker anchor: maps the "rubric generation → grading → judging" pipeline — the acceptance-authoring + evidence-judging spine Tusker needs, stated as distinct compute/roles.

**N50 — Clarify-vs-comply is a trained behavior with per-user preference variance.**
> "trying to figure out the line between when you push back and ask the user to clarify a question versus when you trust their judgment and they said no I really wanted to do this there's a kind of a fine line and people have different preferences" — [8:18:42](https://youtu.be/htM02KMNZnk?t=29922)–[8:19:12](https://youtu.be/htM02KMNZnk?t=29952)
Tusker anchor: runner behavior on ambiguous contracts — whether to block-and-ask (human gate) or proceed is a tunable, preference-dependent policy; Tusker should let a task/repo declare its clarify-vs-comply posture rather than hard-code one.

---

### Cross-talk convergence (both speakers, independently)

- **Small, stable, model-agnostic context beats sprawl.** BAML: "incredibly small... only... things that will not change" (N2). Cursor: "~50 skill files... hard for the models to figure out your actual intent" (N27). → Keep `.tusker/SKILL.md` + capsules tight.
- **Verification must be un-gameable / executable, not prose.** BAML: type system as "center of truth," compiler-proven exhaustiveness (N5, N19). Cursor: models game git history + eval forks; strip history + allow-list to fix (N36, N37). → Bind Tusker acceptance to runnable checks against a sanitized worktree.
- **Fewer tool calls = the quality metric; A/B-test the substrate.** BAML measures "3 tool calls when it should've been 1" and A/B-tests features by tool-calls/errors (N18). → A/B-test Tusker contract formats by runner tool-call count.
- **Automate the babysitting; page humans only on hard blockers.** Cursor N45/N46/N47; BAML's mandatory-reader gate N9. → Tusker daemon target: autonomous runs, failure-triggered human gates.
