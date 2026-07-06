# AIE World's Fair 2026 — Learning-Loop Alpha Mining

Tactical extraction for Tusker's feedback → recurring-lesson → durable-canon design and the future eval-over-traces harness (TRC epic).

**Provenance / caution:** All quotes are VERBATIM from YouTube auto-caption transcripts in
`/Users/sarav/Downloads/AIE Talks/AI_WF2026_Agentic_Tools_Obsidian_Vault/Transcripts/`.
Auto-captions mangle proper nouns and can garble numbers. Notably GEPA is transcribed as
"Japa / Jeppa / JPA / JPEA / Jeppo / Japar" throughout — same system. Numbers are quoted exactly
as the caption rendered them; where a caption is visibly corrupted (e.g. "4.25% 25%") it is flagged
inline. Every load-bearing claim carries its own timestamp link. Nothing here is paraphrased into an
attributed number without a timestamp.

Sources:
- Talk 17 — **GEPA: Reflective Optimization**, Lakshmi Agrawal (GEPA). ~23 min.
- Talk 32 — **Weights & Biases: Arya Auto Research Agent**, Tim Sweeney (W&B / CoreWeave). ~21 min.
- Talk 05 — **Arize: Agent as Judge**, Aparna Dinakaran (Arize). ~2 min (opening of the talk is cut off; file starts mid-sentence at 1:41:11).

---

## Talk 17 — GEPA: Reflective Optimization (Lakshmi Agrawal)

The single most on-point talk for Tusker: it IS a feedback→reflection→durable-text-artifact loop,
with a Pareto promotion pool, negative lessons, and an explicit "learn a repo skill from a trajectory"
mode that mirrors `.tusker/SKILL.md`.

### (a) Numbers & thresholds

1. **~3 data points, one reflection round beats 25,000 RL rollouts.**
   > "Japa in just one round of reflection using just three data points is already able to get twice the performance gains that gpo [GRPO] got after 25,000 rollouts. Continuing to run Japa for a few more steps further increases that gap itself by another 2x." — [5:39:56](https://youtu.be/4sX_He5c4sI?t=20396)
   *Tusker anchor:* Extreme sample efficiency of text-space learning. A durable lesson can be justified from a handful of examples — supports a LOW recurrence threshold N for promotion (you don't need dozens of repeats to trust a lesson).

2. **~50 human-annotated trajectories are enough to learn a judge.**
   > "jpa can actually learn evals for your task from production traces. The way to do that is you collect a bunch of production traces from your agent. Get a human to annotate just about 50 of those trajectories giving very detailed feedback... you can use Japa to optimize an LLM as a judge prompt. And you can use that LLM as a judge prompt then to go back and optimize your agent and deploy that agent. And this becomes a data flywheel." — [5:53:53](https://youtu.be/4sX_He5c4sI?t=21233)
   *Tusker anchor:* DIRECT blueprint for the reviewer rubric + TRC harness — bootstrap the Tusker judge/reviewer from ~50 human-annotated run traces, then use that judge to optimize agents. Sets a concrete "how many annotated traces to collect" target.

3. **AMD NPU XDNA2: 4.25% → 30.52% (7x) on a zero-public-data API.**
   > "an existing agent which was getting 4.25% 25% on this task and apply Japa without any other change to the agent itself and we got this prompt and pushed this performance 7x to 30.52%." — [5:42:33](https://youtu.be/4sX_He5c4sI?t=20553) (caption shows garbled "4.25% 25%"; the 7x → 30.52% figure is clean)
   *Tusker anchor:* The gain is largest exactly where public training data is absent — i.e. proprietary/internal repos, Tusker's home turf. Canon matters most for private domains.

4. **ARC-AGI: 32.5% → 89.5% in 16 reflection rounds (4-line seed → 6-step agent).**
   > "we started with a simple four-line Python program... Within just 16 rounds of reflection, Jeppa within optimize anything was able to find this sophisticated six-step agent that took RKGI [ARC-AGI] accuracy of Gemini flash from 32.5% to 89.5%." — [5:48:47](https://youtu.be/4sX_He5c4sI?t=20927)

5. **MATH 500: +20% for GPT-4.1 nano via a discovered 2-step agent.**
   > "applying the same approach of discovering agent harnesses to math 500 we are able to push its accuracy of GPT 4.1 nano by 20% by simply creating a two-step agent." — [5:49:18](https://youtu.be/4sX_He5c4sI?t=20958)

6. **GSkill: repo-issue resolution 24% → 93% (~3x) on cheap model, transfers to 100% on strong model, ~50% less time/tokens.**
   > "we started with miniu agent with GPT5 mini... and we were able to take its performance from 24% to 93%. An almost 3x jump on go repository issue resolution but more importantly the skills that were optimized very cheaply on a GPT5 mini agent we are able to take that and apply to the latest claude sonnet... pushing its accuracy to 100% issue resolution while more importantly cutting down the execution time or issue resolution time by almost 50%." — [5:50:18](https://youtu.be/4sX_He5c4sI?t=21018)
   *Tusker anchor:* This is the flagship result for Tusker's thesis. A durable skill learned from run trajectories, consumed by future agents on the same repo, transfers across models AND cuts run cost ~50%. Learn cheap, deploy everywhere.

7. **Runtime / other deltas:** GEPA run takes "about half an hour to 1 hour" [5:41:29](https://youtu.be/4sX_He5c4sI?t=20489); +10% on QA/instruction-following/claim-verification/math benchmarks [5:44:40](https://youtu.be/4sX_He5c4sI?t=20680); cloud scheduling costs cut "almost 40%" [5:52:21](https://youtu.be/4sX_He5c4sI?t=21141); OCR error rates cut "almost 35%" [5:52:52](https://youtu.be/4sX_He5c4sI?t=21172); Databricks "90x cost reduction... GPT OSS 120B to outperform Claude Opus while being 90x cheaper" [5:52:52](https://youtu.be/4sX_He5c4sI?t=21172); snorkel improved internal benchmarks "within just 20 hours of releasing it" [5:52:21](https://youtu.be/4sX_He5c4sI?t=21141).

### (b) Named mechanisms

8. **Pareto-based candidate selection / Pareto pool** (the core promotion mechanism).
   > "it keeps a parto pool where it keeps every single candidate that wins on even one training example and not just the top scorer." — [5:43:37](https://youtu.be/4sX_He5c4sI?t=20617)
   *Tusker anchor:* CONTRARIAN to naive "promote only the most-frequent / globally-best lesson." Keep any lesson that wins on even one case; don't collapse the memory set to a single champion. Reframes utility-scoring of memories: score to preserve a diverse Pareto front, not to pick one winner.

9. **"Actionable side information"** — the evaluator returns rich diagnostic context, not just a score.
   > "an evaluator looks at this piece of code, maybe compiles it, profiles it, generates a bunch of related information that we call as actionable side information which is then provided to an LLM which proposes a better candidate." — [5:46:13](https://youtu.be/4sX_He5c4sI?t=20773)
   *Tusker anchor:* Tusker feedback notes / evidence = "actionable side information." The reflection step that generates a promotable lesson should be fed compiler errors, test output, tool-call errors — an open-ended bag, not a scalar.

10. **"Optimize anything" — three modes: single-problem, multitask search, build-a-skill/generalization.**
    > "If you have just a single problem... If you have any number of related problems... you can use what we call as the multitask search mode and finally build a skill which is if you want to optimize on a set number of problems but your deployment can actually come up with many new problems... So we care about generalization mode." — [5:51:21](https://youtu.be/4sX_He5c4sI?t=21081)
    *Tusker anchor:* Tusker promotion targets the "build a skill / generalization" mode — lessons learned on past tasks must apply to unseen future tasks in the same repo.

11. **GSkill** — open-source "learn a skill from a trajectory" in the GEPA repo.
    > "learn a skill from the trajectory. When the coding agent is presented with similar problem, the skill should be helpful... This is a feature called GSkill. You can find it in the Japar [GEPA] repository and it's fully open source as well." — [5:49:48](https://youtu.be/4sX_He5c4sI?t=20988) / [5:51:21](https://youtu.be/4sX_He5c4sI?t=21081)
    *Tusker anchor:* Prior art to study directly — the exact trajectory→durable-skill mechanism Tusker wants. Worth reading their prompt for HOW they distill a trajectory into a reusable skill.

### (c) Failure anecdotes

12. **The ADF.h negative lesson (negative lessons as first-class).**
    > "I want to highlight the sentence saying avoid including ADF.h... AMD actually ships a library called ADF.h for programming NPUs but that did not work with this latest generation of hardware that we were working with and Jeppo was able to discover that in just one step." — [5:42:33](https://youtu.be/4sX_He5c4sI?t=20553)
    *Tusker anchor:* A concrete, high-value NEGATIVE lesson ("avoid X — the obvious thing is wrong here"). Strongly validates "negative lessons as first-class." The single most valuable promoted canon was a prohibition, discovered in one step.

13. **Loop-in-a-loop gets stuck in local optima (why a single champion is a failure mode).**
    > "a loop keeps only the best and gets stuck in a local optima... it kept doing this and it exhausted all of the search budget. On the other hand, with Japa's parto based candidate selection strategy... it maintains a much more balanced search process... more than half of the gains seen with Japa actually account for this and it gets almost twice the performance gains that you would get with just applying the model in a loop." — [5:44:10](https://youtu.be/4sX_He5c4sI?t=20650)
    *Tusker anchor:* If Tusker greedily keeps only the "best" canon and iterates on it, it will plateau. Retaining diverse winning lessons is worth ~2x — over half the value came from diversity, not the optimizer.

### (d) Asides, Q&A, contrarian takes

14. **CONTRARIAN: prompt/canon optimization gets MORE valuable as models improve, not less.**
    > "as models get better the importance of prompt optimization will go down. I argue the opposite which is as models get better they will get better at instruction following and the more precise instruction about your task that you have to give to a very smart model the better that model will be at solving your task." — [5:53:22](https://youtu.be/4sX_He5c4sI?t=21202)
    *Tusker anchor:* Rebuts "we won't need a learning loop once frontier models are smart enough." Strategic justification to invest in Tusker's canon layer now.

15. **What good canon looks like (not prompt tricks).**
    > "Unlike prior prompt optimizers... which would use model idiosyncrasies like my grandmother will be really angry... Here Jpai is actually giving a very detailed problem specification which includes how to make sense of the input. What is the purpose and context of this particular part of the pipeline? What are some key observations and lessons from the data?" — [5:40:26](https://youtu.be/4sX_He5c4sI?t=20426)
    *Tusker anchor:* Template for a promoted lesson's SHAPE: (i) how to read the input, (ii) purpose/context of this step, (iii) key observations + lessons from data. Not sycophancy hacks. Use this as the schema for a canon entry.

16. **What a repo skill should contain (mirrors `.tusker/SKILL.md`).**
    > "skills contain information about how the repository is organized, how to invoke the test cases, where a particular feature is implemented, what are the build system used by this repository and so on." — [5:50:50](https://youtu.be/4sX_He5c4sI?t=21050)
    *Tusker anchor:* This is a content spec for Tusker project canon: repo layout, how to run tests, where features live, build system. Validates the `.tusker/SKILL.md` concept and gives a concrete field list.

17. **Self-improvement with no external teacher.**
    > "the model Quen 38B [Qwen 3 8B] is optimizing itself here. There is no external expert teacher involved whatsoever." — [5:40:26](https://youtu.be/4sX_He5c4sI?t=20426)
    *Tusker anchor:* The agent can generate its own promotable canon from its own traces; no stronger teacher model required.

### (e) Step-by-step pipeline

18. **The GEPA algorithm, three steps.**
    > "it simply runs your systems on a few examples and collects domain specific feedback. whatever information your environment contains is observed. Second, it runs reflection with an LLM or agent that reads the feedback and proposes a better prompt. Finally, and most importantly, it keeps a parto pool where it keeps every single candidate that wins on even one training example." — [5:43:05](https://youtu.be/4sX_He5c4sI?t=20585)
    *Tusker anchor:* Maps 1:1 to Tusker: (1) run tasks, collect feedback/evidence; (2) reflection pass proposes a lesson/canon edit; (3) promote into a Pareto pool of retained lessons.

19. **Learn only an O(1) score and you throw away the diagnostics.**
    > "there is chains of thought. The tool calls made to the environment, the environment's responses to those tool calls which could potentially contain error messages which also provide diagnostic value and we learned almost nothing from all of that." — [5:37:18](https://youtu.be/4sX_He5c4sI?t=20238)
    *Tusker anchor:* TRC epic must record the full trace (chain-of-thought, tool calls, tool errors), not just PASS/FAIL — the diagnostic gold is in the intermediate errors.

20. **A single natural-language edit = a huge behavior change (why canon-in-prompt beats fine-tuning).**
    > "instead of only updating weights with small deltas, we can instead update a prompt where a single natural language update can give a very large behavior change... 'generate a one-line summary'... 'generate a 10-line summary', we can all agree that the behavior of the system would change quite significantly with that just one word change." — [5:38:21](https://youtu.be/4sX_He5c4sI?t=20301)
    *Tusker anchor:* Justifies canon-as-prompt-text: promoting one durable lesson is a high-leverage single edit vs. thousands of gradient steps.

21. **Learn evals from traces → judge → optimize agent → flywheel** (also counted as a number in #2).
    > "once you get those human annotations, you can use Japa to optimize an LLM as a judge prompt. And you can use that LLM as a judge prompt then to go back and optimize your agent and deploy that agent. And this becomes a data flywheel where you can keep improving it." — [5:53:53](https://youtu.be/4sX_He5c4sI?t=21233)

22. **"Learning fast and slow" — co-optimize weights + prompt harness (continual learning).**
    > "we recently had this paper called learning fast and slow where we propose fast slow learning where we can co-optimize model weights and prompt harnesses and this shows some very strong properties that one would want in a continual learning algorithm." — [5:54:24](https://youtu.be/4sX_He5c4sI?t=21264)
    *Tusker anchor:* Names the fast-loop (per-task feedback) / slow-loop (promoted canon) split Tusker is designing.

23. **Anything text-and-scorable is optimizable (incl. the harness/rubric itself).**
    > "prompts are just text artifacts that determine AI system behavior, the same algorithm can improve anything that you can express as a piece of text and you can score... your entire agent harness is eventually just a Python or a JavaScript file... So if you can write it as text and score it, JPA can optimize it." — [5:45:11](https://youtu.be/4sX_He5c4sI?t=20711)
    *Tusker anchor:* Tusker's reviewer rubric, task templates, and canon are all text-and-scorable — candidates for the same optimization treatment once an eval score exists.

---

## Talk 32 — Weights & Biases: Arya Auto Research Agent (Tim Sweeney)

The most operationally concrete trace→eval→promote pipeline in the corpus, with real promotion
thresholds, eval-suite sizing, a multi-judge task schema, and explicit "behavioral bug" framing.

### (a) Numbers & thresholds

24. **Promotion gate: candidate 73% vs incumbent 72% on the eval suite → promote.**
    > "This is literally two nights ago, the evaluation for our candidate model got 73% on our production or on our eval suite against the 72% that our prod model got, which means we're definitely going to push that forward uh this Friday." — [4:57:06](https://youtu.be/4sX_He5c4sI?t=17826)
    *Tusker anchor:* CONCRETE promotion rule — candidate beats the incumbent on the shared eval suite (even +1 point) → promote. Tusker canon promotion = A/B the candidate lesson-set vs. current canon on the trace eval suite; promote iff it doesn't regress / wins. Decision-changing: promotion is an incumbent-vs-candidate comparison, not an absolute bar.

25. **Eval-suite sizing & structure: ~200 tasks, each = 1+ judges, runs nightly.**
    > "These are all then clustered together into we have about like 200 of these. They're all clustered together into an eval suite that runs nightly." — [4:56:34](https://youtu.be/4sX_He5c4sI?t=17794)
    *Tusker anchor:* Target scale for the TRC eval harness — ~200 trace-derived tasks, run nightly as CI.

26. **Rule-based expediency judge: "≤ 6 tool calls."**
    > "we've defined a third rule-based judge that says were you able to actually generate a result within just six tool calls meaning it got there with some degree of expediency." — [4:56:34](https://youtu.be/4sX_He5c4sI?t=17794)
    *Tusker anchor:* Reviewer rubric should include DETERMINISTIC budget judges (tool-call count / step count), alongside semantic LLM judges. Concrete threshold example.

27. **Log 100% of traces.**
    > "We have a product called Weights and Biases Weave that we log 100% of our traces to where us and our team can learn from." — [4:51:24](https://youtu.be/4sX_He5c4sI?t=17484)
    *Tusker anchor:* TRC epic — record every run trace, not a sample; offline learning depends on full coverage.

### (b) Named mechanisms

28. **Two complementary-yet-adversarial offline loops → promote via registry → close flywheel.**
    > "we emit our evaluation results to weave where we have a common dashboard that we can make go no-go decisions on various prompt changes or architectural changes that then feeds into a research loop which we call our improvement loop where we form hypotheses implement candidate agents and analyze the evals. So we have two sort of complimentary yet adversarial research loops going on offline feeding data from weave ultimately to identify the best model so that we can promote that to production through our registry and close the data flywheel." — [4:51:55](https://youtu.be/4sX_He5c4sI?t=17515)
    *Tusker anchor:* Skeleton of Tusker's promotion loop — traces → insights → candidate → go/no-go on dashboard → promote via registry → close flywheel. "Promote to production through our registry" = Tusker's canon store as the registry.

29. **Task = YAML unit test with a stack of judges (correctness LLM + "interesting" LLM + rule-based budget).**
    > "our tasks are all described as YAML files. You can think of a task as essentially a unit test for your model... we've defined an LLM judge... what correctness means in the context of that question. And... a second LLM judge that determines if the insights are actually interesting. And then we've defined a third rule-based judge..." — [4:56:04](https://youtu.be/4sX_He5c4sI?t=17764)
    *Tusker anchor:* Multi-judge task schema for the reviewer rubric — declarative (YAML), composing semantic judges (did it do the right thing / was the result interesting) with deterministic judges (budget). Tusker task contracts could carry an analogous judge block.

30. **Signals — live LLM judges that cluster behavior into next-iteration fixes.**
    > "you'll see that I have a user frustration signal, a lowquality response signal, ask user signal, etc. These are LLM judges that run live against our live traffic... These help our team identify these clusters of behavior for us to go fix in next week's iteration." — [4:55:03](https://youtu.be/4sX_He5c4sI?t=17703)
    *Tusker anchor:* Recurring-lesson DETECTION mechanism — named LLM judges flag events on live traces; recurring clusters become the next fix. Maps to Tusker "cluster recurring feedback → candidate lesson." Judge examples to seed: frustration, low-quality, "should have asked the user."

### (c) Failure anecdotes / failure classes

31. **"Behavioral bugs" — a new bug class invisible to normal CI.**
    > "This introduces an ability to catch a new class of bugs in our world called behavioral bugs. Not exceptions, not performance, but behavioral bugs." — [4:58:38](https://youtu.be/4sX_He5c4sI?t=17918)
    *Tusker anchor:* Names exactly what Tusker feedback notes capture — the agent did the wrong thing though nothing crashed. Justifies feedback-as-first-class (these never surface in build/vet/test).

32. **Concrete frustration signal example.**
    > "this says the user explicitly states that I'm not satisfied with the loss curve. It looks bad and it apparently that indicates frustration. So here we can see an LLM judges live reasoning for why that particular flag was indicated." — [4:55:34](https://youtu.be/4sX_He5c4sI?t=17734)
    *Tusker anchor:* Judges should emit reasoning for each flag — the "why" becomes the seed text of a candidate lesson.

### (d) Asides, contrarian takes

33. **CONTRARIAN / anti-gold-plating: don't over-engineer memory; feed domain context first.**
    > "It can be really tempting to try to overengineer the harness and do a bunch of creative stuff around memory and things like this. We found that a lot of lowhanging fruit can be ascertained through simply giving your agent context about your business domain, the underlying primitives that you have available and your particular business data." — [4:59:40](https://youtu.be/4sX_He5c4sI?t=17980)
    *Tusker anchor:* Direct warning to Tusker — before building the elaborate promotion/utility-scoring machinery, exhaust the simple win of injecting domain context/primitives (i.e. a good `.tusker/SKILL.md`). Sequence the roadmap: context injection before clever memory.

34. **Human is a NECESSARY judge; weekly best/worst trace review.**
    > "you must use humans as a necessary judge. There are behavioral nuances that LLM will not catch. You must be using your product and you must be manually reviewing these traces as a team at the end of the week on a board looking at the best and worst traces to understand how your model is performing." — [4:59:09](https://youtu.be/4sX_He5c4sI?t=17949)
    *Tusker anchor:* Keep a human gate in the promotion loop; LLM judge alone is insufficient. Ritual: weekly review of best AND worst traces — a promotion cadence, not continuous auto-promote.

35. **Tasks & evals are the new CI; metrics are true go/no-go.**
    > "tasks and evals are the new world of CI... you must develop a practice where your researchers are sitting on the same scrum team as you developing tasks and you're viewing the performance metrics as true go no-go decisions." — [4:58:38](https://youtu.be/4sX_He5c4sI?t=17918)
    *Tusker anchor:* Frame the TRC eval suite as CI that gates canon promotion.

### (e) Step-by-step pipeline

36. **Humans annotate traces (notes/feedback/emojis) → turn into tasks.**
    > "This is where my research lead, myself and my PM go to add notes, add feedback, add emojis, and talk about and discover those insights and those behavioral nuances we spoke about earlier so that we can turn them into tasks." — [4:54:01](https://youtu.be/4sX_He5c4sI?t=17641)
    *Tusker anchor:* `tusker feedback add` = the "add notes/feedback" step; the promotion step = "turn them into tasks/canon." Confirms the two-stage note→durable-artifact shape.

37. **Agent analyzes its own traces to recommend self-improvements.**
    > "using Arya to analyze Arya's own conversations to then make recommendations about how to improve Arya all within the UI." — [4:54:31](https://youtu.be/4sX_He5c4sI?t=17671)
    *Tusker anchor:* The reflection step can be run by the same agent over its own recorded runs.

38. **Full loop recap.**
    > "what we use weave to do is collect production traffic... generate insights both as humans... and we use LLM judges to identify those behavioral nuances. We then enrich our tasks. We implement models and we evaluate using weave as a shared dashboard where we can make decisions together as a team that then ultimately allows us to promote the best model forward with confidence." — [4:57:36](https://youtu.be/4sX_He5c4sI?t=17856)

---

## Talk 05 — Arize: Agent as Judge (Aparna Dinakaran)

Short segment (opening cut off). Its whole thrust — a long-running agent that reads recorded traces
and discovers recurring failure patterns a fixed rubric never could — is precisely Tusker's
recurring-lesson detection over traces.

### (b) Named mechanisms

39. **Three eval types; agent-as-judge = adaptive over trajectories (not a fixed rubric).**
    > "Agent as a judge is about adaptive dynamic analysis. LM as a judge just gives you a fixed rubric with these fixed scores. It's what everyone's doing. But when your agent's doing completely different trajectories every time a user puts in data, it just means that you need a fundamentally different type of eval. My take is that most teams today are doing the first two, but the future of eval is actually having all three." — [1:41:11](https://youtu.be/4sX_He5c4sI?t=6071)
    *Tusker anchor:* Because Tusker runs vary per task, the reviewer/eval can't be only a fixed rubric — it needs an agent-as-judge tier that reads the whole trajectory. Layer the TRC harness: code checks + fixed-rubric LLM judge + adaptive agent-judge.

40. **"Signal" — a long-running agent that reads traces and discovers recurring issue patterns.**
    > "We've released signal. Signal is actually a longunning agent that can read traces sent in discover patterns of issues... it can figure out types of problems that a classical LLM as a judge eval just would never be able to do with these deterministic rubrics." — [1:41:42](https://youtu.be/4sX_He5c4sI?t=6102)
    *Tusker anchor:* This IS Tusker's recurring-lesson detector — an agent over the recorded trace corpus that surfaces patterns a static rubric misses. Prior art for the detection stage.

### (c) Failure anecdotes

41. **Detected failure class: repeated same-tool loop / inefficient trajectory.**
    > "It's helped us figure out very subtle failures that you wouldn't even think of doing such as something going on in a loop for multiple times. It was calling the same tool for repeatedly long time. The trajectory was inefficient." — [1:42:13](https://youtu.be/4sX_He5c4sI?t=6133)
    *Tusker anchor:* Concrete recurring-failure signature to seed Tusker's detector — same-tool loop / long inefficient trajectory. Pairs with W&B's "≤6 tool calls" budget judge (#26) as the deterministic counterpart.

### (d) Asides — detection → auto-remediation

42. **Detection closes to an actual PR/fix.**
    > "because it has all that analysis, it can go put up a PR and put up a fix." — [1:42:13](https://youtu.be/4sX_He5c4sI?t=6133)
    *Tusker anchor:* Promotion needn't stop at a note — a recurring-lesson detection can drive an automated fix/PR. Ambition-setting for where Tusker's loop could end.

---

## Cross-talk synthesis (decision-changing)

- **Promotion is an incumbent-vs-candidate A/B on a shared eval suite, not an absolute bar.**
  W&B promotes on candidate 73% > incumbent 72% (#24). Tusker canon promotion should be: does the
  candidate lesson-set beat / not-regress the current canon on the TRC trace suite? Not "did we hit X%."

- **Bootstrap the judge/reviewer from ~50 human-annotated traces; suite scale ~200 tasks nightly.**
  GEPA (#2) + W&B (#25) give concrete sizing for the TRC harness and reviewer-rubric bootstrap.

- **Keep a Pareto pool of lessons; do NOT collapse to one champion.** GEPA (#8, #13): >half the value
  came from retaining diverse winners; greedy "best only" plateaus. Reframes Tusker's utility-scoring
  from "rank and keep the top lesson" to "preserve the Pareto front of lessons that each win somewhere."

- **Negative lessons are the highest-value canon.** GEPA's single best artifact was a prohibition —
  "avoid ADF.h" (#12). Makes "negative lessons as first-class" a headline feature, not a nicety.

- **Record the full trace, incl. intermediate tool errors — that's where the signal is.** GEPA (#19) +
  W&B 100%-logging (#27). TRC must not reduce runs to PASS/FAIL.

- **Sequence the roadmap: domain-context injection BEFORE elaborate memory machinery.** W&B (#33) warns
  the low-hanging fruit is a good context/skill file; don't over-build promotion/scoring first.

- **The flagship proof for Tusker's thesis already exists:** GEPA's GSkill (#6) — a skill learned from
  run trajectories (repo layout, how to run tests, where features live, build system — i.e. exactly
  `.tusker/SKILL.md`) took issue resolution 24%→93% cheaply, transferred to a strong model at 100%,
  and cut run time ~50%. This is the outcome Tusker's learning loop is aiming at, demonstrated.
