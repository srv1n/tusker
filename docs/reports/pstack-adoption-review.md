# pstack adoption review

Research review and recommendations, 11 September 2026. This is a proposal, not a changed product contract or an implementation plan approved for execution.

## Recommendation

Keep Tusker's product direction: turn an agreed outcome into reviewed, integrated work, and bring back the decisions that require the user's attention. Concentrate the next effort on a usable daily workflow, clearer instructions, and the existing agent-coordination implementation.

The handoff correctly favors pstack's verification recipes, writing discipline, and focused experiments. Its recommendation is too narrow for the user's present objective, however. A documentation experiment around the synthetic demo does not establish that Tusker removes manual message relaying or coordinates real workers. Current local evidence identifies those as the more consequential next steps.

The strongest change to the design process is to finish the contract for the next useful slice before elaborating the rest. Preserve the human–architect conversation about product intent, alternatives, constraints, and acceptance. Leave reversible implementation choices to the worker. Resolve empirical uncertainty with a bounded experiment when further discussion would only produce guesses.

## What the local review established

The requested handoff base exists locally: `4b5d20769b6cf6bd221752bdfb946e5242d98349`. This review inspected the current dirty checkout, which includes newer, uncommitted coordination and model-profile work. It is not a certification of the committed base, the installed app, or one frozen candidate binary. Existing changes were preserved.

| Finding | Evidence | Consequence |
| --- | --- | --- |
| The desired separation between design and operation is already specified. | [Planning contract](../../.tusker/specs/planning-handoff-and-agent-entry.md#confirmed-user-decisions) assigns discussion to external design skills and task conversion/operation to Tusker. | Sharpen and implement the existing direction; another vision rewrite is unnecessary. |
| Agents receive contradictory model and planning instructions. | [.agents spec skill](../../.agents/skills/spec/SKILL.md#house-rules-that-bind-every-spec-session-here) mandates Opus for implementation/review; the [planning contract](../../.tusker/specs/planning-handoff-and-agent-entry.md#confirmed-package-one-tusker-skill) requires configured models and external planning methods. | Reconcile the active instructions at their source, then refresh installed copies. A shorter skill still wastes tokens if it contradicts another active skill. |
| Work and review already have separate configurable levels. | [modelLevelForNote/modelLevelProfiles](../../cmd/tusker/model_levels.go#L78) resolve `work_level` and `review_level`, including separate execution/review mappings and explicit fallback candidates. | Use these mappings. Do not add a second pstack model-role store. |
| Most task-contract enforcement already exists. | [Delivery validation rules](../../cmd/tusker/delivery_cmd.go#L167) cover requirement mapping, acceptance checks, dependencies, ownership conflicts, resources, capacity, profiles, and labeled assumptions. | Improve the authoring and error-recovery route before adding fields or new commands. Structural coverage does not establish that acceptance captures the user's intent. |
| Both concrete defects from the handoff remain in current source. | The [proof guide](../system/proof-and-closeout.md#record-a-result) says to store pass/fail; [verify add](../../cmd/tusker/v7_proof_cmd.go#L371) permits only pending command rows. [demoTestBinary](../../cmd/tusker/demo_cmd_test.go#L94) skips its E2E tests on build failure. | Correct the procedure and make qualification distinguish a skipped scenario from an executed pass. These are bounded repairs, not a reason for new infrastructure. |
| The real conversation loop is not yet qualified. | The [independent coordination report](agent-coordination/independent-acceptance.md#current-independent-recheck) records 11/11 public mailbox checks and explicitly marks live M1–M6 unrun. | Use the existing M1 pilot, then the branching-wave and next-wave scenarios, to determine what remains broken. These are saved results, not tests rerun in this review. |
| Automatic continuation still has source-level integration gaps. | Repository-wide caller search found `SetArchitectProposal` and `ApplyArchitectContinuation` called only by tests; production polling records/routes reports. [Continuation code](../../cmd/tusker/agent_coordination.go#L339). Execution-address delivery requires a current live Codex handle; an unavailable handle is held. [Wakeup code](../../cmd/tusker/agent_coordination.go#L85). | Demonstrate the idle original architect, proposal receipt, and validated next-wave application through public operations. Persistence helpers and live-worker steering do not by themselves provide that journey. A planning agent could invoke existing delivery commands, but that still needs correlation, limits, and replay qualification. |

Read-only diagnostics run during this review:

- `tusker docs check --json`: exit 0, 39 managed documents, no metadata/link issues.
- `tusker skill doctor --strict --json`: exit 1, 39 errors and 65 warnings. The output includes existing tracker/proof problems; these counts are not 104 skill defects. It also flags ordinary `small/medium/large` and `10/100/1000-document` wording as code.
- Targeted source and caller inspection: completed. One background research agent inspected pstack primary sources. No product tests, Tusker-managed workers, daemon, installation, or UI session were launched for this review.

The structural document check passing alongside contradictory procedures is a useful limit: more lint cannot substitute for checking meaning and following the actual instructions.

## Preserve the design conversation, change its stopping rule

“Get the spec absolutely right” is useful when it means a shared understanding of the promised behavior. It becomes expensive when it means resolving every program detail before receiving implementation feedback.

For each next slice, settle the user outcome, important failure cases, irreversible or shared interfaces, explicit exclusions, observable acceptance, and who decides unresolved product questions. Preserve the chosen alternative and its reason. That is enough to ask whether independent workers can start.

Use three routes for the remaining unknowns:

| Unknown | Next action |
| --- | --- |
| Product preference, acceptable trade-off, or authority | The architect brings the decision to the user with its recommendation and the consequence. |
| Behavior, performance, feasibility, or an interaction that must be experienced | Run a bounded prototype with a question and a decision criterion; record the observation and its limits. |
| Reversible implementation detail under an adequate contract | Let the worker choose and verify it. Escalate if the choice changes the contract. |

The [existing planning spec](../../.tusker/specs/planning-handoff-and-agent-entry.md#external-planning-guidance--outside-the-tusker-skill) already recommends incremental capture and acknowledges that no mechanism guarantees semantic fidelity. Keep a concise current decision record and continuation section. Move superseded directions out of the active reading path while retaining their history in the decision log. Do not make each new worker replay the design conversation.

## What to own in skills

Own one Tusker operating package: documentation format/discovery, task conversion, wave preparation, worker/reviewer procedures, configuration, and recovery. Reuse external grilling, prototyping, and writing methods where useful. The current [skill router](../../skills/tusker/SKILL.md) and [source/install rules](../system/skills.md) already provide the packaging foundation.

Compose them by stage: a design skill develops the decision; a prototype resolves a concrete uncertainty when needed; writing guidance improves the spec; Tusker turns the supplied intent into checked task contracts. During execution, workers load the required task and operating instructions. Do not load every methodology on every task or spawn a separate reviewer merely because two skills apply.

For external skills used routinely, use an explicit installed version through existing skill/plugin tooling, with a source link and a small compatibility check. A GitHub link is useful attribution and optional reading; it is a weak sole runtime dependency for required instructions. Keep Tusker-specific constraints in its existing references. Do not import a sticky pstack mode whose default fan-out, permissions, models, or implementation behavior conflicts with the current workflow. No new marketplace or skill-management service is justified here.

## Make the documentation useful to a tired reader

Treat writing as part of delivery. Apply technical-writing/unslop guidance when writing or reviewing changed documents. Adopt the reader-purpose distinction from [Diátaxis](https://diataxis.fr/); do not split every existing page into four files.

Start with the everyday journey: preparing a wave, understanding why work is waiting, answering an agent, and checking whether the result is ready to try. Each procedure should tell the reader what they can accomplish, what they need, the exact action, the expected result, and what to do when it fails. Keep the implementation detail available in the owning reference. Introduce necessary terms where used and keep their names stable.

For example, the proof guide currently starts its review explanation with a list of revision and fingerprint types. A proposed reader-facing opening is:

> A review applies to a specific version of the work and its evidence. If the relevant code, evidence, or approval state changes, Tusker requires a fresh review.

Keep the exact binding fields and exceptions in the following reference detail. Plain language must preserve those conditions; removing uncertainty or a negation to make prose shorter would change the contract.

Use runnable examples and review for clarity. Keep mechanical checks for broken links, invalid fields, stale commands, and necessary technical constraints. Investigate the existing slash-pattern false positives before extending prose lint. Do not make a banned-word list or sentence-length score the acceptance test for useful writing.

## Divide the work by responsibility

| Part | Responsibility |
| --- | --- |
| Human and architect | Product direction, trade-offs, contract formation, and substantive questions or changed outcomes. |
| Tusker CLI and daemon | Validate contracts, choose configured profiles, schedule eligible work, preserve ownership, route messages, track attempts, enforce checks/review, and apply permitted transitions. Ordinary coordination needs zero model calls. |
| Workers | Implement bounded outcomes using complete relevant acceptance criteria, interfaces, source pointers, and verification recipes. |
| Independent reviewer | Judge the implementation and promised outcome against the relevant evidence. Reuse one review when its scope adequately covers both; require a stronger reviewer for difficult judgment rather than every administrative event. |
| Existing UI | Show the promised result, what is happening, why work is waiting, what needs a decision, and how to try accepted work. Put questions and replies in the task inspector; retain technical details one level deeper. |

Use the current Light/Standard/Demanding mappings as policy, not universal claims about a model's competence. Budget retries and escalation. Measure the whole cost of an accepted outcome, including planning, failed attempts, reviews, and repair. Lower price per call is not a saving if it creates more repair work. Missing usage remains unknown.

Parallelism should follow independent outputs and file/resource ownership. Agree on shared interfaces early so independent implementation can proceed. Serialize shared Git operations and scarce build resources where required. Fixed ten-worker fan-out and multiple competing implementations on routine tasks are poor defaults for this goal.

## Recommended sequence

1. **Repair the instructions and a few frequently used documents.** Reconcile the fixed-model `/spec` guidance, fix command-proof authoring, and rewrite the normal wave/clarification/completion route with concrete examples. Match this to existing FLW-T-0011 and FLW-T-0026 after checking their current contracts; avoid another general skill-redesign epic. Success means a fresh agent performs the intended steps without inventing a command or omitting a condition.
2. **Finish one real question-and-answer journey.** Continue the existing ACO work and [prepared M1 pilot](agent-coordination/m1-pilot.md). A worker asks its recorded architect, yields, receives the correlated answer, and produces the agreed result without manual relaying. Include an idle architect and one available execution slot. Inspect actual provider turns and ownership.
3. **Prove a useful branching wave and continuation.** Use ACO-T-0004 through ACO-T-0007. Independent tasks continue while one waits; acceptance/review/integration release the dependent work; the architect receives the actual result and creates the permitted next wave once. Test duplicates, restart, and stop. The public journey must exercise the production connections identified above.
4. **Use that loop on normal work.** A small wave improving two separate everyday guides is a reasonable first low-risk candidate. Validate factual changes against behavior and have a fresh reader follow the result. UI improvements to question/wait/result visibility can follow the established message contract in parallel with backend work, under disjoint ownership. Keep a minimal literal transport test first so writing-quality judgment does not obscure a routing failure.

For the first normal wave, record manual copy/paste relays, human interventions by reason, discovery attempts, time to an accepted result, retries, actual model calls by role, and available usage. Mechanical status/routing calls should use zero model turns. Compare a few similar tasks before claiming token or time savings. The handoff's six paired runs and 25% threshold may be useful later; they are proposed experimental choices, not prerequisites or established benefits.

This sequence uses the work already underway. Reconcile existing task contracts with source and proof before changing their status. No new tracker, scheduler, generic verification CLI, chat product, or cloud migration is needed to answer the user's current question.

## Source boundary

The primary source is pstack at commit [`7366ac1`](https://github.com/cursor/plugins/tree/7366ac128bdf95f45e6734f412b49a4031800169/pstack), dated 2026-09-10. I compared the focal files with [`current main`](https://github.com/cursor/plugins/tree/main/pstack) at `f5bdd6826fd0a0d9cbc4347134c3a74a200b9d9d`, dated 2026-09-11. The blobs for technical writing, unslop, how, why, teach, recall, architect, arena, setup, swarm, and both verification skills are identical. The only relevant post-pin change is wording in the multi-phase plan. These sources establish written mechanisms, not evidence that the mechanisms improve defect rates or throughput.

The pstack tree contains no `grill` or `spec` skill. Its README explicitly says planning skills are not bundled because Cursor's plan mode is considered sufficient, and says the author's personal default is that “the best spec is code.” That is a design choice in pstack, not evidence that durable product decisions or human authority are unnecessary. See the [pstack README](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/README.md) and [pinned skills tree](https://github.com/cursor/plugins/tree/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills).

## Findings worth testing for Tusker

### Verification ergonomics are the strongest transferable idea

`create-verification-skill` makes a verification route answer five concrete questions. What surface does a user touch? How does it start and become ready? How does an agent drive it? What action and resulting state, including side effects, prove the behavior? How is the instance cleaned up without deleting evidence? It requires a read-only “doctor” check before driving, stable selectors or command handles, and executable helper scripts whose invocation is documented. It also requires one end-to-end run of the generated skill before calling it a deliverable. See [`create-verification-skill/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/create-verification-skill/SKILL.md).

The feature map is user-facing rather than source-file-facing. Each feature records how to reach it, how to drive it, and what observable end state proves it. A convenient entry point is explicitly incomplete when the map names other user paths. This is a useful documentation shape to test against existing Tusker routes, without adding another proof authority. See the same [feature-map instructions](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/create-verification-skill/SKILL.md).

`maintain-verification-skill` adds a disciplined upkeep loop. It returns exactly one of `clean`, `changed`, or `blocked`; keeps edits inside the verification skill; runs one source reader per feature; requires a live pass even when source looks clean; and distinguishes doc drift, harness gaps, and product gaps. Its live-pass invariants are concrete: health-check before driving after surprises, preserve evidence through cleanup, and leave no drive residue alive after it is useful. See [`maintain-verification-skill/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/maintain-verification-skill/SKILL.md).

The adoption candidate is therefore the recipe and its proof vocabulary: doctor, user action, resulting state, side effect, cleanup, evidence location, and blocked coverage. A new verification CLI or second truth store is not implied by these sources.

### Composability reduces copy-paste when contracts stay narrow

`poteto-mode` is a sticky entry point. It classifies a task, opens the matched playbook's steps, routes to skills as steps fire, and applies `unslop` to prose. The skill index names stable composition points: `how` and `why` for investigation, `architect` for cross-boundary design, `arena` for competing attempts, `interrogate` for adversarial review, and the prototype playbook for empirical forks. See [`poteto-mode/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/poteto-mode/SKILL.md).

`how` chooses a cheap direct pass for a narrow question and two to four parallel explorers plus one explainer for a complex subsystem. It fixes the output shape as `Overview`, `Key Concepts`, `How It Works`, `Where Things Live`, and `Gotchas`. [`how/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/how/SKILL.md)

`why` anchors the target in paths, symbols, recent commits, and PRs, then maps available evidence sources and records null results. It keeps “what we found,” “what we can infer,” and “what we do not know” separate. Its default is broad parallel investigation, but it is explicitly Cursor-MCP-specific: investigators run without readonly mode because readonly strips MCP access. [`why/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/why/SKILL.md)

`teach` composes `how` and `why` while preserving `why`'s confidence language. `recall` locks topic, workspace, and time window before searching history, then verifies surfaced branches, PRs, and tickets against live state. The useful general mechanism is scoped, confidence-preserving resumption, not transcript mining as authority. [`teach/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/teach/SKILL.md), [`recall/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/recall/SKILL.md)

### Technical writing and unslop are useful as review constraints

The technical-writing skill starts by choosing one Diátaxis mode per document: tutorial, how-to, reference, or explanation. It then asks for active actors, command-style instructions, short unambiguous sentences, real symbols and paths, and links at the boundary between modes. It explicitly says the rules serve the reader and must not make prose sound machine-written. It applies `unslop` to every document it touches. [`technical-writing/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/technical-writing/SKILL.md)

`unslop` is a small process, not a second authoring system. Scan, rewrite while preserving meaning and tone, then self-audit. Its catalog catches filler, vague attribution, synonym cycling, over-compression, passive voice, and abstract metaphors. Technical-writing supplies the `only`/`not` placement checks. For Tusker, preserving meaning must include permission boundaries, negation, uncertainty, identifiers, and exact commands. [`unslop/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/unslop/SKILL.md)

### Architect and arena provide a concrete review protocol, with important defaults

`architect` grounds the problem with `how` and, when ownership or layering changes, `why`. It then requires at least two structurally distinct sketches through `arena`, screens them for shallow interfaces, leaked representations, temporal decomposition, and pass-through methods, and compares interface depth. Implementation deviations are treated as evidence that the sketch may be wrong. However, its default is to implement immediately after synthesis. A human checkpoint is opt-in only when the invoker asks for one. [`architect/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/architect/SKILL.md), [`architect design red flags`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/architect/references/design-red-flags.md)

`arena` makes the selection reproducible on paper. The prompt is the candidate contract. The picker declares a three-to-six-criterion rubric. Candidates write to isolated paths. A cross-judge scores each criterion. The owner reads every candidate, records the base, grafts only selected ideas, records rejections and dropouts, and verifies the synthesized artifact. Convergent candidates are recorded as agreement; wild divergence triggers reframing instead of averaging. [`arena/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/arena/SKILL.md)

This is a useful bounded experiment for design questions. It does not make cross-model agreement a correctness proof. The synthesized artifact still needs the repository's own verification and authority gates.

### Prototype is for empirical forks, not durable product authority

The prototype playbook requires a decision before building, isolates throwaway work from production, puts variants behind one labeled switcher, observes the question on the matching surface, and hands the chosen direction to Feature or Architect. It expressly treats the observation as the test and the artifact as throwaway. [`prototype.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/poteto-mode/playbooks/prototype.md)

The clean boundary is useful. A prototype can settle behavior, timing, or layout. It cannot authorize a product decision, amend a task contract, satisfy a human acceptance gate, or replace a durable rationale that later agents must rely on.

### Explicit model tiers and wave sizes are composable, but Cursor-specific

`setup-pstack` validates detected model slugs, stores one idempotent role-to-model rule, and uses list-valued roles to set panel fan-out. It separates code, judgment, prose, explorer, synthesizer, arena, architect, interrogate, and swarm roles. Arena's cross-judge pool prefers a different model family from the parent when possible. [`setup-pstack/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/setup-pstack/SKILL.md)

`swarm` makes the done predicate, slice or race shape, worker count, model, isolated output, and report statuses (`PASS`, `ISSUES`, `BLOCKED`) explicit before fan-out. It aggregates coverage without pasting raw worker dumps. [`swarm/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/swarm/SKILL.md)

The transferable idea is declarative role configuration plus explicit wave contracts. The model names, `Task` fields, cloud environment, MCP behavior, and `~/.cursor/rules` storage are implementation details of Cursor and cannot be treated as portable Tusker semantics.

## Limits and cautions

- The README's quality and throughput claims are author reports, not controlled measurements. They do not justify a larger fan-out, weaker review, or a universal live-verification tax. [`README.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/README.md)
- The pinned README says `control-cli`, `control-ui`, and `deslop` come from the separate `cursor-team-kit` plugin. The verification skills therefore describe a workflow that depends on external Cursor capabilities. [`README.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/README.md)
- `why` deliberately asks investigators and its synthesizer to run without readonly mode to retain MCP access, while saying they should not write. That permission workaround is not a general authority model. [`why/SKILL.md`](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/why/SKILL.md)
- Current main's post-pin change is wording-only in the multi-phase plan. Recheck the pinned files before implementation if pstack changes again. [`current main commit`](https://github.com/cursor/plugins/commit/f5bdd6826fd0a0d9cbc4347134c3a74a200b9d9d)

## External adoption hypothesis

Test these as small workflow changes, not as a pstack import:

1. A route that names doctor, user action, resulting state, side effects, cleanup, evidence location, and proof tier.
2. A composition contract that routes `how`, `why`, architect, prototype, arena, and review only when their trigger matches, with fixed compact return shapes.
3. A declarative role and wave table that names model tier, worker count, isolation, owner, and review handoff.
4. A writing pass that selects document mode and removes slop while preserving authority and uncertainty.

These are hypotheses from pstack's source. No effect size, local usability result, or Tusker acceptance result is established here.
