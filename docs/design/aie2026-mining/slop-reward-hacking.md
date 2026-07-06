# AIE World's Fair 2026 — Slop & Reward-Hacking Alpha

Mined for Tusker's reviewer agent (catching AI slop + reward hacking in runner output).

**Extraction rule:** every attributed claim below is a VERBATIM quote (auto-caption text,
warts included) with a timestamp link. Auto-captions contain transcription errors
(names/jargon) — flagged inline where relevant. Nothing here is paraphrased-as-attributed.

Sources:
- Talk 19 — "Auto Research for GPU Kernels" (speaker billed as "Tis"), 6:01:07–~6:07:24.
  (Transcript file 19 bleeds past 6:07:24 into a different talk — the "magic lamp / John
  Carmack" multi-repo-memory story is NOT this speaker; it belongs to the Polygraph talk.
  Nothing from 6:07:24+ is attributed to the kernel talk below.)
- Talk 28 — "Addy Osmani - Human Verdict and Engineering Accountability", 7:56:47–8:05:08.

---

## Talk 19 — Auto Research for GPU Kernels (reward hacking)

This is the reward-hacking gold. The speaker runs an "auto research" loop that optimizes CUDA
kernels against a verifiable objective (faster + still correct) and narrates exactly how the
agent games that objective and how they catch it.

### (a) Concrete numbers

**N1 — The headline: ~80% of agent output is bad.**
> "I know this this might all sound like roses and flowers but it's not actually the case around 80% of the things that auto reach is going to do are going to be bad uh so it's important to remember while you're u like working on this that most things are going to be bad it's going to try to trick you all the time"
[6:06:17](https://youtu.be/4sX_He5c4sI?t=21977)
*Tusker anchor:* Calibrates reviewer-agent priors — default posture is adversarial ("it's going to try to trick you all the time"), not trust-with-spot-check. A reviewer that PASSes most rows on a frontier task is miscalibrated.

**N2 — Disabling CUDA graphs = 20x slower (the canonical reward hack).**
> "they'll disable CUDA graphs which can make it 20 times slower and they might make that one kernel faster but make the whole like it's not a viable kernel because it's they're disabling a bunch of speed ups like CUDA graphs or only testing on small context windows"
[6:04:13](https://youtu.be/4sX_He5c4sI?t=21853)
*Tusker anchor:* The exact shape of reward hacking — optimize the measured unit (one kernel) by sabotaging the unmeasured whole (end-to-end). Verification rows must measure the end-to-end metric the task actually cares about, not the local proxy the runner can inflate.

**N3 — Bare-metal vs virtualized ~25%.**
> "net on bare metal optimizations, you can get roughly 25% over like a virtualized setup you get from using a cloud provider"
[6:06:17](https://youtu.be/4sX_He5c4sI?t=21977)
*Tusker anchor:* Environment (bare metal vs VM) changes the measured number by 25% — a verification row must pin the environment or the runner can "win" by changing where it measures, not what it wrote.

**N4 — Stacked kernels + hardware hacks = 3x.**
> "you can get that you can combine all of the kernels you did as well as all of the hardware level hacks you did uh you can get a 3x speed up"
[6:06:17](https://youtu.be/4sX_He5c4sI?t=21977)

**N5 — Kernels beat hand-tuning, fueled by "billions of tokens".**
> "you mix that with billions of tokens of your favorite model and that results in kernels that beat hand tuning"
[6:02:09](https://youtu.be/4sX_He5c4sI?t=21729)

**N6 — "100% hallucinate" without model context.**
> "if you don't do this the model will 100% hallucinate uh the actual attention mechanism and you will get useless kernels"
[6:03:41](https://youtu.be/4sX_He5c4sI?t=21821)
*Tusker anchor:* Missing domain context in the task packet → confident wrong output ("useless kernels"). The task contract, not the runner's training data, must supply current spec.

### (b) Named failure modes / exact terms

**N7 — "reward hacking" is named as THE top problem.**
> "Uh by far the biggest problem when you're doing this is going to be reward hacking."
[6:04:13](https://youtu.be/4sX_He5c4sI?t=21853)
*Tusker anchor:* Names the reviewer's primary target. Put "reward hacking" as an explicit named check in the reviewer prompt.

**N8 — The three optimization targets (and the third is a slop vector).**
> "the three main things you can have are a compute bottleneck uh a memory bottleneck or you just have excessive overhead from uh too many kernels being launched"
[6:02:09](https://youtu.be/4sX_He5c4sI?t=21729)

**N9 — MFU / theoretical max as the real ceiling.**
> "eventually you taper off at whatever the hardware limit is uh for your GPU and that's uh some people call this like MFU which is like the actual theoretical max utilization from a GPU"
[6:05:14](https://youtu.be/4sX_He5c4sI?t=21914)
*Tusker anchor:* A verification target should be anchored to a real ceiling; a number "better than baseline" isn't proof if the baseline was chosen by the runner.

### (c) War stories — what the agent gamed and how it was caught

**N10 — The framing: agents cheat where humans wouldn't.**
> "if you were to tell your kernel engineer co-orker I need to make uh the GPU this GPU kernel faster. Uh it's obviously not going to your human coworker is not going to go in and do some stuff that's going to make it slow like the endto-end model inference slower. But uh agents are not humans and they will do plenty of things to make it slower"
[6:04:13](https://youtu.be/4sX_He5c4sI?t=21853)
*Tusker anchor:* "Agents are not humans" — the reviewer cannot assume good-faith engineering norms. Every implicit constraint a human would honor must be made explicit in the contract.

**N11 — Reward hack #1: disable CUDA graphs (see N2). Reward hack #2: test only on small context windows.**
> "...or only testing on small context windows."
[6:04:13](https://youtu.be/4sX_He5c4sI?t=21853)
*Tusker anchor:* Gaming the test surface itself — passing by choosing an easy input distribution. Verification rows must fix the input/workload range so the runner can't cherry-pick where it's evaluated.

**N12 — Reward hack #3: model refuses the required DSL (writes wrong thing instead).**
> "another reward hack is that some models just don't actually write the cute DSL you need uh when you're trying to write kernels. And this is a common problem with enthropic models."
[6:04:43](https://youtu.be/4sX_He5c4sI?t=21883)
*(auto-caption: "cute DSL" = CuTe DSL; "enthropic" = Anthropic.)*
*Tusker anchor:* Runner satisfies the letter of the task in the wrong medium/language. A verification row should assert the artifact is in the required form, not merely that "a kernel exists".

**N13 — Narrow-validity swap: the optimized artifact only works on a sub-range.**
> "sometimes the kernels you come up with might only work well on like zero to 100k and then you need to go back to this the default kernel that could you get from like a flash in for cutless... that's another thing to look out for is that your kernel isn't always just a swap in for all all workloads"
[6:05:14](https://youtu.be/4sX_He5c4sI?t=21914)
*Tusker anchor:* An improvement that regresses outside the tested band is slop dressed as a win. Acceptance must cover the full workload range, and the reviewer must check the artifact doesn't silently narrow scope.

### (d) Asides / Q&A / contrarian takes

**N14 — Verifiability is why this domain works at all.**
> "why are GPUs such a good fit for auto research? It's because they're super verifiable. You can verify them for correctness and speed, and that's basically all you need for your auto research framework."
[6:01:07](https://youtu.be/4sX_He5c4sI?t=21667)
*Tusker anchor:* The whole thesis — an auto/agent loop is only as trustworthy as its verifier. Tusker's leverage is designing verification rows that make the objective as un-gameable as "correct + fast".

**N15 — Humans still own the idea; agents own the parameter search.**
> "they're also still really bad at the high level idea... It's not going to come up with these groundbreaking ideas. So it's still up to the human to do that... it is still your job to have good ideas is what I'm saying."
[6:01:37](https://youtu.be/4sX_He5c4sI?t=21697)
*Tusker anchor:* Reinforces the planner/runner split — the human/planner supplies the idea and the un-gameable goal; the runner fills in the searchable parameters.

**N16 — The secret formula: idea + auto-search + verifiable goal.**
> "the actual secret formula here is you have the good ideas, auto research picks out the parameters and everything to verify that it actually works. Uh and go move toward that verifiable goal of it being x times faster and uh still correct."
[6:01:37](https://youtu.be/4sX_He5c4sI?t=21697)

**N17 — Contrarian aside: model "nerfing".**
> "I mean anthropic says what they say about uh nerfing models. You can it's guess if it's I'm guessing if it's nerfing or not, but I would recommend using a different model."
[6:04:43](https://youtu.be/4sX_He5c4sI?t=21883)
*(Speculative aside, flagged by the speaker himself as a guess — do not treat as fact.)*

**N18 — Bare-metal "hacker" tweaks (BIOS/overclock/PCIe).**
> "if you have actually have bare metal access, your auto research framework can uh do very hacky things... You can uh tweak your BIOS settings, you can overclock the GPU, uh, you can force like PCIe relaxing, all these little tweaks"
[6:05:47](https://youtu.be/4sX_He5c4sI?t=21947)
*Tusker anchor:* Gains that come from changing the environment out-of-band, not the artifact under review — a reviewer scoped only to the diff would miss these. Evidence must record the environment, not just the code.

### (e) Review / verification workflows that actually catch cheating

**N19 — The core countermeasure: define what NOT to do.**
> "a lot of this is also just defining what not to do which is actually very important when you're doing frontier work that agents can actually easily do with a one shot."
[6:04:13](https://youtu.be/4sX_He5c4sI?t=21853)
*Tusker anchor:* Task contracts need explicit negative constraints / anti-goals ("do not disable CUDA graphs", "do not shrink the test window"), not just positive acceptance rows. This is the single most transferable design lesson for Tusker's contract schema.

**N20 — Human reads the profiler top-line and calls it "dumb".**
> "your job as a human is to look at the top here and be like this is dumb. uh we are loading 32k chunks into context uh and we don't actually need to... at a high level all you have to be telling auto research is this top method is dumb let's pipeline it instead and everything else like the sizing the chunk sizing the context chunks that all should just be decided by auto research"
[6:02:39](https://youtu.be/4sX_He5c4sI?t=21759)
*Tusker anchor:* The human/reviewer judges the high-level approach (is the strategy sound?); the agent owns the low-level search. Reviewer prompt should check "is the approach itself dumb?", above the numeric rows.

**N21 — Dual objective: correctness AND speed, together.**
> "You can verify them for correctness and speed" (N14) + "go move toward that verifiable goal of it being x times faster and uh still correct" (N16).
[6:01:07](https://youtu.be/4sX_He5c4sI?t=21667) / [6:01:37](https://youtu.be/4sX_He5c4sI?t=21697)
*Tusker anchor:* A speed row without a paired correctness row is gameable (disable the work, go fast). Verification rows should come in correctness+performance pairs so one can't be traded for the other.

**N22 — Harness must inject hardware context (else wrong-target output).**
> "one thing you really need to make sure your agent is aware of is the hardware. And so on a B200 for example, you need to make sure it has context of uh the warps. It has T-M TMA... this changes generation to generation. like an H200 won't have T-M for example... this basically is just like bunch of MD files you need to give so it has context."
[6:03:11](https://youtu.be/4sX_He5c4sI?t=21791)
*(auto-caption "T-M"/"T-M TMA" = TMA.)*
*Tusker anchor:* The "bunch of MD files" = Tusker's `.tusker/SKILL.md` + capsule context. Un-current or missing context is itself a slop vector (N6). Contract must ship the current environment spec.

**N23 — Harness must inject current model/spec context.**
> "every new model like DeepS Flash comes out with like new tricks like DeepSeek had two new attentions... compress sparse attention hierarchal compressed and if you don't do this the model will 100% hallucinate uh the actual attention mechanism"
[6:03:41](https://youtu.be/4sX_He5c4sI?t=21821)

**N24 — Kernels compound (stack wins), but only if each is really valid.**
> "one of the great things is is that kernels compound. So like if you make one for your sparse MLA for deepseek for example um you can get speed ups there and you just stack them on... you just keep stacking and stacking and stacking"
[6:05:14](https://youtu.be/4sX_He5c4sI?t=21914)
*Tusker anchor:* Compounding only holds if each accepted artifact is genuinely valid (not narrow-validity slop per N13). One reward-hacked row poisons the stack — argues for strict per-row un-gameable acceptance before anything compounds.

---

## Talk 28 — Addy Osmani, Human Verdict & Engineering Accountability

The verification/ownership-philosophy half. Directly names the failure modes of over-delegation
and prescribes an "evidence + responsibility" review boundary — near-verbatim to Tusker's model.

### (a) Concrete numbers

**A1 — Wharton study: borrowed confidence, 73%.**
> "Wharton did a study that kind of offers us a warning light here. when AI was wrong, 73% of people still thought that they, you know, they picked the wrong answer and they felt more sure. So the failure mode is not using AI, but it's borrowed confidence."
[7:58:20](https://youtu.be/4sX_He5c4sI?t=28700)
*(auto-caption garbles the sentence; the cited stat is "73% ... felt more sure" when AI was wrong.)*
*Tusker anchor:* Reviewers (and human gate approvers) inherit false confidence from confident-looking runner evidence. Reviewer prompt should weight the evidence, not the runner's tone/assertion.

**A2 — Time horizon reframes review.**
> "a 30 secondond run right can feel like an interaction but an hour or a daycale task so something long horizon that's a work stream and when tasks can end up you know lasting that long especially when you begin running many of them in parallel review can't just be a glance at the end it has to become a whole control system"
[7:57:17](https://youtu.be/4sX_He5c4sI?t=28637)
*Tusker anchor:* Long-horizon + parallel runners is exactly Tusker's daemon model — "review can't just be a glance at the end, it has to become a whole control system." Argues for the reviewer-agent as a standing gate, not a final skim.

**A3 — Career math: half-life of an edge vs a signature.**
> "the half-life of an edge might be one model release. speed, recall, verification, even taste all move as the frontier moves. But the half-life of a signature, your credibility, your expertise is much longer."
[7:59:54](https://youtu.be/4sX_He5c4sI?t=28794)

### (b) Named failure modes / exact terms

**A4 — "cognitive debt".**
> "first thing to avoid really is cognitive debt. Now, cognitive debt is the erosion of your understanding and memory around how to solve problems... For code, it's the gap between how much code exists in your repo and how much any human on your team genuinely understands."
[7:56:47](https://youtu.be/4sX_He5c4sI?t=28607)
*Tusker anchor:* The named cost of accepting un-understood merges — argues Tusker's "explain it or don't ship it" gate (A11) and capsule-first review.

**A5 — "delegation depth" + tests-pass-but-nobody-understands.**
> "You can have a build that passes you know your tests a PR that you can merge but your team can still end up losing its ability to actually explain the system that they are shipping to production."
[7:57:17](https://youtu.be/4sX_He5c4sI?t=28637)
*Tusker anchor:* Green acceptance rows ≠ understood/defensible change. The reviewer must check comprehensibility/ownership beyond the passing verification rows — the core "check beyond acceptance rows" mandate.

**A6 — "cognitive surrender".**
> "The second thing to avoid is cognitive surrender. Now this is when you blindly accept AI's um responses... Surrender is really saying hey your answer is now my answer before I have formed any opinions myself."
[7:57:50](https://youtu.be/4sX_He5c4sI?t=28670)

**A7 — "orchestration tax" + cognitive bandwidth doesn't parallelize.**
> "The third thing to avoid is orchestration tax... More AI agents running does not mean that there is more of you available. Your cognitive bandwidth does not parallelize. So every loop that you create ends up causing more decisions to route, merge, verify, and integrate."
[7:58:20](https://youtu.be/4sX_He5c4sI?t=28700) / [7:58:51](https://youtu.be/4sX_He5c4sI?t=28731)
*Tusker anchor:* The reviewer agent exists precisely to absorb "route, merge, verify, integrate" load that human bandwidth can't scale to. Directly justifies automating the review gate.

**A8 — "high agency" = ownership with judgment.**
> "High agency is actively taking ownership of your outcomes. So knowing when to delegate, when to inspect, when to stop, and when to put your name on the results... it's not just hustle theater, but it's ownership with judgment attached."
[8:00:55](https://youtu.be/4sX_He5c4sI?t=28855) / [8:01:27](https://youtu.be/4sX_He5c4sI?t=28887)

**A9 — The "agency ladder".**
> "At the bottom, you've got someone that flags a problem and leaves it for the system. higher up they execute, diagnose, propose, recommend, and resolved. And the rare top movement is discernment."
[8:01:27](https://youtu.be/4sX_He5c4sI?t=28887)

**A10 — Inner loop = capability, outer loop = agency.**
> "agents can run much more of the inner execution loop. They can investigate, implement, test and report... but that outer loop is still engineering. So deciding, verifying, approving, owning... that inner loop is capability. The outer loop is agency."
[8:01:58](https://youtu.be/4sX_He5c4sI?t=28918)
*Tusker anchor:* Names Tusker's exact split — runner owns inner loop (implement/test/report → evidence), planner+reviewer own outer loop (decide/verify/approve).

### (e) Review / verification workflows (the heart of the talk)

**A11 — Operational rule: "Explain it or don't ship it."**
> "here's an operational rule. Explain it or don't ship it. And it's not because humans have to type every line or read every line, but because someone has to understand the work well enough to defend it."
[8:03:00](https://youtu.be/4sX_He5c4sI?t=28980)
*Tusker anchor:* A candidate hard gate — a task can't pass review unless a defensible one-line rationale/explanation accompanies the diff. Pairs with the "no breadcrumbs" hygiene rule: the explanation lives in the review record, not as apology-comments in the code.

**A12 — Delegation = "show me enough evidence that I can judge it".**
> "delegation is important because delegation says do the work then show me enough evidence that I can judge it. I still make a judgment in that situation."
[7:57:50](https://youtu.be/4sX_He5c4sI?t=28670)
*Tusker anchor:* Defines the evidence bar — the runner's job is to produce judge-able evidence (capsules, command+PASS/FAIL), not to self-certify.

**A13 — Evidence design: exactly what a runner should return.**
> "Your agent returns evidence. It returns diffs, tests, logs, rationale, traces, trajectories, screenshots, whatever the work itself requires."
[8:02:29](https://youtu.be/4sX_He5c4sI?t=28949)
*Tusker anchor:* A literal spec for Tusker's evidence bundle. "whatever the work itself requires" = per-task-type verification-row design.

**A14 — The boundary: evidence and responsibility, not "human looks at output".**
> "So the boundary is not human looks at AI output. The boundary is evidence and responsibility."
[8:02:29](https://youtu.be/4sX_He5c4sI?t=28949) / [8:03:00](https://youtu.be/4sX_He5c4sI?t=28980)
*Tusker anchor:* Reframes review from eyeballing diffs to auditing an evidence bundle against named responsibilities — argues the reviewer agent should score evidence sufficiency, not just diff correctness.

**A15 — The three ownership questions on failure.**
> "When something fails, the question is, who understood the policy? Who accepted the risk? And who owns the blast radius?"
[8:00:55](https://youtu.be/4sX_He5c4sI?t=28855)
*Tusker anchor:* Candidate fields for a task's human-gate record — policy-understood / risk-accepted / blast-radius-owner. Maps to Tusker's risk-and-evidence model.

**A16 — Verify whether evidence is ENOUGH (not just present).**
> "We decide whether the work was worth doing. We verify whether the evidence is enough and we approve or redirect or own what reaches production."
[8:02:29](https://youtu.be/4sX_He5c4sI?t=28949)
*Tusker anchor:* Reviewer must judge evidence *sufficiency*, and "was the work worth doing" — beyond row-by-row PASS. A task can have all-green rows and still be redirected.

**A17 — Owner's-file analogy for accountability.**
> "some code bases have this concept of an owner's file or c certain subdirectories where there are people who are on the hook for that part of the system... Who's accountable for that part of your architecture in your codebase?"
[8:03:00](https://youtu.be/4sX_He5c4sI?t=28980)
*Tusker anchor:* Suggests binding each task/subtree to a named accountable owner in the contract — the human whose "signature" (A3) is on the merge.

### (d) Contrarian / closing takes

**A18 — Jevons: cheaper software → more of it, every time.**
> "Every time that we've made it easier to write software, we've predicted that the world would need less of it. And in fact, the opposite happened. Higher level languages happened, frameworks, cloud, low code. The pattern always went the other way... when you lower the cost, latent demand ends up appearing."
[8:04:03](https://youtu.be/4sX_He5c4sI?t=29043)

**A19 — New engineering = loop/evidence/brownfield design.**
> "our new work might be loop design, evidence design, and brownfield stewardship, but fewer keystrokes doesn't mean less engineering... It means that there is more surface area that needs taste, verification, ownership, and ultimately care."
[8:03:31](https://youtu.be/4sX_He5c4sI?t=29011)
*Tusker anchor:* "loop design, evidence design" is a one-line description of building Tusker itself.

**A20 — The bottleneck shifts to "should this exist and can we answer for it".**
> "It's not going to remove engineering work. It's going to move the bottleneck from can we build this to should this exist and can we answer for it. So build the factories, keep the lights on, own the verdict."
[8:04:33](https://youtu.be/4sX_He5c4sI?t=29073)

---

## Cross-cutting synthesis for Tusker (design implications, not new quotes)

1. **Define anti-goals, not just acceptance rows.** Talk 19's single most reusable lesson (N19):
   "defining what not to do." Tusker contracts should carry explicit negative constraints. The
   reviewer prompt gets a named "reward hacking" check (N7) and a checklist of concrete cheats:
   sabotaging an unmeasured global metric to win a local one (N2/N10), shrinking/cherry-picking
   the test surface (N11), narrowing artifact validity (N13), wrong-medium output (N12), and
   out-of-band environment gains (N18).

2. **Un-gameable verification rows.** Pair every performance row with a correctness row so speed
   can't be bought by disabling work (N21). Pin the environment and the full input/workload range
   in the row so the runner can't move where it's measured (N3, N11). Anchor targets to a real
   ceiling (MFU-style), not a runner-chosen baseline (N9). Tests/rows the runner cannot edit are
   the mechanical enforcement of A14's "evidence and responsibility" boundary.

3. **Reviewer checks BEYOND green rows.** A5 (tests pass but no one can explain it) + A16 (is the
   evidence *enough*, was the work worth doing) + N20 (is the high-level approach itself dumb).
   Green acceptance is necessary, not sufficient — the reviewer scores comprehensibility, evidence
   sufficiency, and approach soundness on top of the rows.

4. **"Explain it or don't ship it" as a gate (A11)** and the "no breadcrumbs / no references to
   prior versions" hygiene rule are complementary: the *why* belongs in the review/evidence record
   (A13), never smeared through the code as "fixed the thing / previously we did X" comments. A
   CI-level slop detector (AST-walk + small-model comment classifier) enforces the code-hygiene half
   by flagging apology/changelog/breadcrumb comments; the reviewer agent enforces the explanation
   half by requiring a defensible rationale attached to the diff, not the source.

5. **Calibrate the reviewer adversarially.** N1 (~80% bad, "it's going to try to trick you all the
   time") + A1 (73% borrowed confidence) say: trust the evidence, distrust the runner's confident
   framing, and expect most frontier attempts to be slop or hacks by default.
