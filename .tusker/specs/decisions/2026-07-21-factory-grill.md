---
title: "Decision log: the factory grill session"
status: canonical
created: 2026-07-21
read_when: "You want to know WHY a locked decision in [[software-factory]] was made, what the operator actually said, or what alternatives were weighed and rejected."
skip_when: "You only need the decisions themselves — read [[software-factory]]; it states them without the discussion."
decides_for: ".tusker/specs/software-factory.md"
---

# Decision log: the factory grill session (2026-07-21)

This is the record of the grill session that produced the locked decisions in
[[software-factory]]. Format per decision: the question asked, the options put
to the operator with a recommendation, what the operator actually said
(condensed but faithful — these were spoken answers), and what got locked.
Research inputs that framed the questions: the AIE software-factories
conference vault mining and the loops-vs-graphs field research (both run the
same day, summarized in the spec's sources).

---

## D1 — Who is the customer?

**Asked:** Who is Tusker's customer for the next 6-12 months? Options: private
harness for our own portfolio (recommended); us-first but product-shaped;
product from day one.

**Operator said:** Picked the recommendation as offered.

**Locked:** Private harness. Every design decision optimizes for our own
portfolio (this repo, the rzn backend, Rust and Xcode projects). Productizing
is a later fork decision made from a working system.

---

## D2 — What does the human review, forever?

**Asked:** What do you commit to personally reviewing as the system scales?
Options: specs + slop-free zones (recommended); specs only; specs +
risk-tiered diffs.

**Operator said:** "I want to spend a lot of time on high-level things:
specifications, schemas, API contracts, SQL schemas, storage decisions. Not
the actual plumbing code — I never want to read the code, hopefully. If it's
any high-risk thing, then let's have a discussion on it between me and the
agents."

**Locked:** Operator owns specs, schemas, API contracts, and storage
decisions. High-risk changes become a discussion, not a diff review. Routine
diffs are never human-reviewed; agents own all code review.

---

## D3 — Where do builds and tests run?

**Asked:** Build/test substrate for Rust/Xcode/Go over the next quarter.
Options: selective-local now + sandbox spike (recommended); commit to cloud
sandboxes now; stay fully local.

**Operator said (the richest answer of the session):** Already experimenting
with Crabbox spinning up cloud containers on Hetzner with sccache backed by a
Hetzner bucket — wants that structured properly for nightly runs. Daytime work
stays on the Mac and must not grind to a halt. Xcode/iPhone/Mac apps are best
built locally rather than fighting macOS cloud sandboxes. For multi-day Rust
pushes, use Crabbox VMs (one or two) plus local, torn down when done. Wants
timers: escalate off the laptop when local gates get slow.

The motivating incident: multiple worktrees each cold-building without shared
cache filled the disk; clearing and rebuilding cost roughly two days to land a
batch of worktrees. Conclusion drawn together: per-change testing only for
what changed, nightly full suite to catch the rest.

Also asked directly: is optimistic apply across 8-9 stories with one
collective check at the end acceptable, "I know this is typically frowned
on"? Ruling: yes — it is the sanctioned pattern when file ownership is
disjoint; the two-day disaster was a caching failure, not a batching failure.

**Locked:** Three-stage cadence (per-change focused gates; one collective
wave-end pass; nightly full suite on Hetzner via Crabbox + sccache). Xcode
stays local. Escalation timers and measured disk floors. Optimistic apply
with disjoint ownership is sanctioned. Full policy: [[build-and-test-economics]].

---

## D4 — How do spec discussions become work?

**Asked:** How should GPT-5.6/Claude spec discussions turn into tickets?
Options: vault specs + emit tasks (recommended); specs required for
everything; specs stay conversational.

**Operator said:** Described the existing practice to preserve: an
"architecture notes" folder of Obsidian-style backlinked documents with
minimal backlinks, built up over days of back-and-forth. Structure: intro/why
first, then a PM-ish common write-up, then branches per audience (domain
expert, dev team, marketing); the dev branch holds specs and JSON contracts.
One canonical document, no version numbers, updated in place with
front-matter. After the doc settles: "what is done, what is not done, what do
we need new Tusker tasks for" — then tasks get cut with acceptance criteria
and dependency order.

On acceptance: moving toward humans approving ARTIFACTS — UI change → before/
after screenshots; performance change → benchmark before/after; security →
posture artifact. On human gates: reserved for blockers only (missing
credentials like R2/S3 keys, unclear spec, something replacing something
else) — "not meant for code reviews; the skill needs to clearly specify
that."

Complaint registered about task quality: as a former PM, opening a Tusker
ticket felt overwhelming — function names sprinkled everywhere, sometimes
instructions too brief, proof appendices burning tokens. Tension named:
tickets must carry full working context yet stay readable and token-lean.

**Locked:** Canonical backlinked specs in the vault with front-matter; spec
sessions end by emitting epic + dependency-ordered tasks linking back to the
spec; artifact-first acceptance; human gates for blockers/unclear-spec/
replacement only; two-layer task format (plain top for the PM, implementation
appendix for the builder) with a lint holding the line.

---

## D5 — Who orchestrates, and how is work routed?

**Asked:** Who runs the factory day-to-day? Options: daemon routes with
cross-vendor review (recommended); single-vendor tiered; a manager-agent
orchestrating dynamically.

**Operator said:** The recommendation is the ideal end state, but today he
could not see what was happening — the UI was rough, so he fell back to using
Tusker for task management only while orchestrating manually through Claude
Code/Codex with subagents. The requirement that matters: work must register
in Tusker no matter where it starts. Spawning quick work directly through
Codex or Claude Code must remain possible, with the skill flagging "this task
was picked up outside the daemon" so the UI stays truthful. Long-term,
everything routes through Tusker.

**Locked:** Daemon is the default orchestrator; interactive sessions register
their claims through the skill (the hand-run origin stamp); the UI/stream
board/logbook must always reflect reality regardless of who drives.
Visibility was named the adoption blocker → Stream A got first priority.

---

## D6 — What lands first, and how fast?

**Asked:** Which two streams land first? Recommended order: session-attach +
honest UI, then reviewer lane + PM tasks, then build policy, then graph
hardening.

**Operator said:** "I like your order... but should we also work on some of
the plumbing, the graph? If we can work in parallel by burning more tokens,
nothing like it. I can pause using Tusker for a while till we get the whole
thing right."

**Locked:** All four streams in parallel with disjoint file ownership; token
spend is not the constraint; operator pauses daily Tusker use during the
rebuild. (Executed same day as waves 1-3 of the FAC epic.)

---

## D7 — Plain language everywhere (standing directive, pre-grill)

**Operator said (verbatim intent):** Agents use "very highly technical terms"
in discussions; everything should be as humanly understandable as possible —
not just discussions but variable names, function names, "as simple as
possible."

**Locked:** Operating principle 4 in [[software-factory]]: plain language in
contracts, logbooks, discussions, AND identifiers. Enforced for task top
layers by the plain-language lint (FAC-T-0004); enforced for code by
instruction in every implementer dispatch.

---

## Standing constraints restated during the session

- Never any AI attribution in commits; author is always `srv1n <sarav@gmx.com>`.
- Implementation and review work runs on Opus; spec writing, grilling, and
  adjudication stay with the planner session (Fable). Model override must be
  explicit on every dispatch.
- Interactive sessions never start daemons or dispatch automation; the
  resident daemon is operator-territory.
