---
capsule:
  what: "Defines the Plan-17-last, default-off program cutover that composes independent shadow producers and Plan-18 exact-proof authority into one human-gated dogfood and promotion handoff."
  use_when:
    - "Reviewing or importing the cross-scope cutover program after plans 12, 14, 15, and 18."
  skip_when:
    - "Implementing an individual producer plan without changing program-level authority."
---

# Program cutover convergence

Status: proposed implementation contract
Date: 2026-07-26

## Outcome

Plans 12, 14, and 15 remain independently implementable and default-off, and
Plan 18 owns the generic exact-verification authority frontier. The workflow
reviews and imports all four before Plan 17, but Plan 17 resolves only their
exact current imported producer contracts through scope-qualified hard
dependencies; it does not retain a producer-review attestation. It then makes
the only program-level authority path explicit:

```text
scheduled-promotion/v1/scheduled-promotion-shadow-ready ─┐
factory-execution-control/v1/factory-control-shadow-ready ├─> shadow-convergence
full-gate-lifecycle-provider/v1/provider-cutover-doctor ──┘          │
                                                                    v
exact-verification-authority/v1/
exact-verification-authority-e2e ──────────────────────────────────┤
                                                                    v
                         program-cutover-transaction-contract
                                                                    │
full-gate-lifecycle-provider/v1/provider-live-smoke ───────────────┤
                                                                    v
                  low-risk-authoritative-dogfood [human auth]
                                                                    │
                                                                    v
                  production-promotion-handoff [human release]
```

`provider-cutover-doctor` is a read-only shadow-readiness producer: it can
report an unavailable or unconfigured host runtime without starting, fixing,
or requiring one. `provider-live-smoke` is deliberately different. It remains
promote readiness behind Plan 15's human `setup` gate and is a hard dependency
of dogfood, not a shadow-ready alias.

The transaction contract is the anti-false-completion boundary. It builds the
default-off program-cutover handler that records one typed, locked,
expiry-bound human grant for an exact gate, task, project, wave, and current
revisions. The handler consumes that grant once, observes the actual staging
and factory dogfood transaction, and writes immutable before/after/rollback
receipt facts. Read-only status verifies these bindings. A human gate creates
authority; it does not claim or execute dogfood.

The handler also owns the cutover-specific fence in the serialized close path:
before dogfood or handoff proof can close, it resolves the exact typed grant,
dogfood receipt, and (for handoff) release receipt against the current task,
wave, revision, and expiry tuple. Generic gate evidence, replacement test rows,
missing or stale receipts, and status prose are not substitutes. This is not a
claim that Plan 17 solves generic V2 exact proof: its transaction is
structurally blocked on Plan 18's imported `exact-verification-authority-e2e`
frontier, which remains responsible for that system-wide concern.

## Migration and ordering

1. Review and import Plan 12, Plan 14, Plan 15, and Plan 18 independently.
   Their shadow and exact-verification producers are objective and grant no
   automation, daemon, runtime, release, spending, ref, or production
   authority.
2. Review Plan 17 after all four producer imports, then import Plan 17 last.
   It never imports a producer implicitly. Importer validation proves only
   exact *current imported producer contracts*; it is not a durable attestation
   that those producers received an independent review.
3. This reviewed migration obsoletes the former Plan-12 release gate and
   Plan-14 auth gate. They are not moved, removed, or bypassed by producer
   work; the Plan-17 auth and release gates replace them as the only authority
   gates in the composed program.
4. Shadow convergence is still non-authorizing. The transaction contract
   cannot be built until every shadow producer and the current Plan-18
   exact-verification-authority-e2e frontier are integrated, and dogfood cannot
   proceed until that contract plus a current Plan-15 live-smoke receipt are
   integrated. A human must then create the narrow typed auth grant for one
   exact project and wave.
5. Production promotion remains blocked until dogfood has an immutable current
   receipt and the accountable production authority creates a typed,
   receipt-bound release-handoff record. Neither record moves a ref or releases.

The resulting import topology is strictly producer-first and acyclic. It uses
only V2 `(scope, source_key)` hard dependencies; durable task IDs remain
Tusker-allocated import output and are never authored here.

## Authority boundary

Planning, doctor, context, review, and dry-run are inert. The future handler
also rejects main/ref promotion, runtime start, global enablement, unrelated
wave work, release, spending, and free-form evidence as proof. Its close fence
is deliberately limited to the Plan-17 cutover receipt tuple. This contract does
not enable automation, arm waves, dispatch a daemon, choose or start Docker or
Podman, perform a live smoke, release, spend, move any ref, or authorize a
production project. The two human gates name the external authority that only
an accountable operator can supply; neither is a proxy for objective code
review or proof.

## Acceptance

| ID | Observable requirement |
| --- | --- |
| R1 | Plans 12 and 14 expose independent default-off shadow producers, while Plan 15 exposes read-only provider-doctor shadow readiness without conflating it with setup-gated live smoke. |
| R2 | Plan 17, imported after its shadow producers and Plan-18 exact-verification-authority-e2e frontier, converges their qualified hard edges before a locked transaction contract and a low-risk dogfood task that is hard-gated by provider live smoke and the only human auth gate. |
| R3 | Dogfood and production handoff require typed current receipts rather than topology proof, former producer authority gates are obsolete only in this reviewed migration, and all plan checks remain inert and clean. |
