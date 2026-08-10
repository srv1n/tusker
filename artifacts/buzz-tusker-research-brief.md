# Buzz → Tusker comparative architecture research

## Objective

Study the current `block/buzz` repository and identify what Tusker should learn,
adapt, prototype, or explicitly reject. This is not a request to turn Tusker
into Buzz. The goal is to improve Tusker as a simple, repo-local, higher-level
orchestrator for documentation, task contracts, truthful work tracking, proof,
and coordination of Codex/Claude/cloud-agent work.

Upstream repository: https://github.com/block/buzz

Use current primary-source material from the upstream repository. Cite exact
Buzz files, symbols, tests, or documentation links for consequential claims.
Separate capabilities that ship today from vision/design material and work in
progress. Call out anything you cannot verify.

## Tusker's current product boundary

Tusker's deliberately small canonical model is:

```text
Task Markdown   = executable contract
Runner adapter  = execution mechanism
Evidence        = curated proof, not raw logs
Gate            = explicit human/external blocker
Project skill   = routed repository canon
Runtime store   = leases, attempts, sessions, and logs
Generated views = disposable UI surfaces
```

Portable project truth lives inside each repository. Machine-local SQLite owns
runtime coordination. Generated views, UI state, and provider observations are
projections rather than authority. An interactive user-opened Codex/Claude
session implements work directly. Only an independently running resident daemon
may dispatch unattended local runners, and automation is opt-in plus exact-wave
authorized.

Current adapters include Claude Code, Codex, Codex app-server/cloud observation,
and `codex exec`. Provider-native children can be observed and later bound, but
observation alone cannot grant task ownership, proof, review, or landing
authority.

The repository has recently received broad hardening: scoped run identity,
private runtime files, bounded histories and process output, replayable SSE,
typed mutation refusals, atomic runner status, scratch/path confinement,
fail-closed leases and end states, and substantial UI/native honesty work. Local
evidence reports 2,060 backend tests, 128 UI tests, 18 Swift tests, and 15 E2E
scenarios passing. However, the integrated working tree is still materially
dirty, the installed CLI does not match the current source, automation and
fanout are disabled by default, and the simple product story is buried under an
approximately 80-command software-factory surface.

Public artifact distribution and signing are explicitly deferred and outside
this research. Do not optimize for releases, GPG, Minisign, or turning Tusker
into a hosted multi-tenant service.

## Buzz themes to investigate

Investigate, rather than assume, the value of these Buzz ideas:

1. Humans and agents as first-class identities with the same auditable action
   model.
2. A unified append-only event vocabulary for messages, jobs, workflow steps,
   git activity, approvals, and search receipts.
3. Agent-first JSON-in/JSON-out CLI design.
4. ACP between clients and agents, MCP between agents and tools, and whether
   those standards could reduce Tusker's runner-adapter surface.
5. Small, auditable agent/tool binaries; strict bounded process lifetime,
   output, session history, concurrency, cancellation, and isolation.
6. Branch/project/channel as a durable collaboration room, and history search
   that answers with receipts rather than summaries without provenance.
7. YAML workflow triggers, approval events, idempotent execution traces, and
   loop prevention.
8. Capability/kind registries, forward-compatible event envelopes, protocol
   version reporting, and typed acceptance/refusal responses.
9. Community/tenant scoping, signed events, and the Nostr relay: identify what
   is genuinely reusable as a principle versus needless complexity for
   Tusker's current single-user repo-local model.
10. Buzz's testing strategy, live-agent smoke tests, lazy agent startup,
    cancellation behavior, slow-client handling, backpressure, and operational
    honesty about what works versus what is still being wired up.

Also identify architectural mistakes, costs, or premature abstractions in Buzz
that Tusker should avoid.

## Questions to answer

1. What are the 5–10 most valuable transferable patterns, and why do they fit
   Tusker's goals?
2. Which Buzz concepts duplicate capabilities Tusker already has under another
   name?
3. Which concepts should Tusker explicitly reject because they would break its
   repo-local, low-ceremony, single-user default?
4. Would adopting ACP/MCP at selected seams materially simplify Tusker, or add
   another protocol layer without enough benefit? Propose the smallest viable
   interoperability experiment.
5. Can Tusker gain the useful properties of Buzz's signed/unified event model
   without Nostr, public-key identity, a relay, Postgres, Redis, or S3? Give a
   concrete minimal event-envelope design if appropriate.
6. How should Tusker expose agent identity, delegation ancestry, work receipts,
   and cross-session history without making raw chat logs authoritative?
7. What would make Tusker's ordinary golden path feel dramatically simpler?
   Assume the default user should understand roughly five concepts and should
   not need the daemon, waves, or most of the CLI.
8. What robustness, observability, testing, or failure-containment lessons from
   Buzz remain missing in Tusker despite the recent hardening?
9. Which ideas would improve the Serve UI/TuskerBar without turning them into a
   Slack clone or a second source of truth?
10. What should the next staged roadmap be, given the current dirty integrated
    tree and installed-binary drift?

## Required output

Return a decision-oriented research packet with:

- an executive recommendation;
- a fact-checked Buzz architecture and capability summary, clearly separating
  shipped, in-progress, and aspirational pieces;
- a Buzz ↔ Tusker concept map;
- recommendations grouped as **adopt now**, **prototype behind a seam**,
  **defer**, and **reject**;
- for each adopted/prototyped recommendation: Tusker problem solved, smallest
  design, likely repository areas affected, migration/compatibility risk,
  usability impact, and deterministic acceptance evidence;
- a two-week consolidation plan, a six-week product experiment plan, and a
  later optional horizon;
- the three highest-risk ways this comparison could lead Tusker astray;
- a final prioritized list capped at ten items.

Prefer boring, comprehensible mechanisms. Preserve Markdown task/spec authority,
curated proof, fail-closed ownership, and the distinction between execution
visibility and dispatch authority. Be candid when Buzz's architecture solves a
different problem.
