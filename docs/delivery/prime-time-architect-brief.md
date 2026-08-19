# Prime-time architect brief

Audit date: 2026-08-18. Scope: the spec-gap implementation plus the tracker
state it exposes. This is an evidence handoff, not a release declaration.

## Executive position

The seven fatal validator findings are repaired. The current source validator
exits successfully with zero errors, but reports 136 warnings. The project is
therefore a strong implementation candidate, not yet prime-time or release
ready: human gates, tracker debt, documentation adoption, runtime dogfood, and
source/install promotion remain open.

## Candidate proof

These are source or focused checks. They do not prove release, live-provider
behavior, human acceptance, or production operation.

| Check | Result |
|---|---|
| `go run ./cmd/tusker validate --json` | Exit 0; 0 errors, 136 warnings |
| `/private/tmp/tusker-prime-candidate validate --json` | Exit 0; 0 errors, 136 warnings |
| `go run ./cmd/tusker skill doctor --strict --json` | Exit 0; `ok=true`, 0 errors, 98 warnings |
| `go vet ./cmd/tusker` | PASS |
| `go test ./internal/docgraph ./skills/...` | PASS |
| Serve UI `bun test` | 129 tests, 750 assertions, 27 files passed |
| Serve UI typecheck and production build | PASS |
| `go test ./cmd/tusker -count=1 -timeout 25m -json` | PASS; final package elapsed 1,039.455s, 2,353 unique tests, 0 failures |
| `go build -o /private/tmp/tusker-prime-candidate ./cmd/tusker` | PASS; candidate SHA-256 `c8368adb406c6159bb34a2ab746b66b60f01d02702515d29775929e2815cf146`, revision `a407d776`, marked dirty |
| `git diff --check` | PASS |

The implemented surface includes documentation freshness/status/verification,
doc adoption proposals, init scaffolding and the distinct spec skill,
wave-boundary review grouping, wave brief honesty, reviewer-independence
warnings, demanding-task spec traceability, inert operator help, wave
re-fingerprinting, and scratch-retention closure.

## Seven-fatal repair map

These were durable task-contract repairs, not new product behavior:

1. `ORC-T-0083`: added an exact focused readiness-test verification row so its
   satisfied proof status matches `proof_required: focused_test`.
2. `FAC-T-0007`: added exact focused and broad test rows so both required proof
   classes are represented.
3. `AGX-T-0001`: added the repository-onboarding `spec_refs` link.
4. `SRV-T-0007`: added the Serve UI `spec_refs` link.

The two spec-link changes were applied through accepted proposals
`AGX-P-0001` and `SRV-P-0002`. The validator’s previous seven fatal findings
were the two stale proof-status findings, three missing proof-mode requirements,
and two demanding-ready tasks without `spec_refs`.

Proposal acceptance used the independent `human:sarav` actor. Application used
Tusker’s explicit `--local` control path for this interactive dirty checkout;
the task records were not hand-edited, and this was not a landing or release
transaction.

## Current blockers and debt

### Human gates

`go run ./cmd/tusker gate list --open --json` reports exactly three open,
blocking gates, all owned by `human:sarav`:

- `ACP-G-0001`: authorize the authenticated Codex ACP live smoke;
- `ACP-G-0002`: authorize the authenticated Claude ACP live smoke;
- `ACP-G-0003`: authorize ACP defaults and direct-runner compatibility deletion.

No agent may satisfy these gates or claim their runtime receipts.

### Tracker and documentation debt

- 12 tasks are `review` / `waiting_on_review`.
- `KNW-T-0009`, `KNW-T-0010`, and `ORC-T-0092` through `ORC-T-0097` remain
  backlog/held with generic A1-only placeholder contracts.
- Validation warnings are 136, split by who can resolve them safely:

  | Owner | Count | Findings | What can happen next |
  |---|---:|---|---|
  | Mechanical cleanup | 16 | 14 document-opening code-word findings; 2 proposal-rationale findings | Rewrite only the opening prose, or copy the already-recorded reviewer rationale into the proposal body. Preserve technical details and decisions. |
  | Task owner/reviewer | 98 | 65 knowledge deltas; 13 task-top-layer code-word findings; 9 missing verification-proof findings; 6 incomplete review-proof findings; 2 missing epic capsules; plus `ORC-T-0050` acceptance/verification and `SRV-T-0006` evidence-budget findings | Add truthful contracts, proof, review decisions, and bounded task metadata. Do not invent receipts or human acceptance. |
  | Architect/product/human | 22 | 20 dangling work-stream references; 2 ACP gate secret-policy findings | Decide whether to relink, remove, archive, or create replacements; define the security/credential policy. Do not auto-delete locked references or weaken the gates. |

  Only the 16 mechanical findings are safe to batch-clean without a new
  product decision. The other 120 change task authority, proof ownership,
  historical traceability, security policy, or acceptance state and therefore
  require an accountable owner before mutation. The three groups sum to 136.
- Current source `tusker docs status --json`: 28 parsed documents, 0 freshness
  stamps, 0 `describes` declarations, and coverage gaps `cmd`, `e2e`,
  `internal`, and `skills`.
- Current source `tusker docs adopt --dry-run --json`: 1,069 proposals,
  `approved=false`; adoption remains a reviewed migration, not an automatic
  write.
- Tracker reconciliation is still separate: shipped ORC/ACP work remains in
  historical backlog states, and one wave reached landed while disarmed.
- Domains retirement remains a separate migration because packet routing,
  validation, skill doctor, and fallback behavior still depend on
  `.tusker/knowledge/domains`.

### Runtime and release boundaries

The read-only runtime database at
`/Users/sarav/Library/Application Support/tusker/daemon.db` contains 165
execution records, but zero provider observations, execution edges, timeline
events, lifecycle evidence, or cancellation evidence. Current source has
provider adapter call sites in the Codex/Claude live, ACP, daemon, and
codex-exec ingress paths; the earlier handoff claim that constructors had zero
callers is stale. This is a missing live-provider dogfood receipt, not proof
that the adapters are unreachable. Synthetic fixtures do not close that gap.

The checkout is `main` at `a407d776`, ahead of `origin/main` by 35 commits and
was dirty across 148 paths at audit time (this handoff is an additional
untracked path). The current candidate reports version
`v0.0.0-20260818043958-a407d7768648+dirty`, revision `a407d776`, and SHA
`c8368adb406c6159bb34a2ab746b66b60f01d02702515d29775929e2815cf146`. The
installed binary remains `archive/pre-convergence-main-20260727-288-g04e7ed6d`
(SHA `c33d4b7b…`) and lacks the new `docs status|verify|adopt` and wave
`refingerprint` capabilities. No intentional commit, install promotion,
version parity check, or release artifact exists yet. Existing unrelated user
work in `CLAUDE.md`, `DOSSIER.md`, and `NARRATIVE-NOTES.md` must remain scoped
and preserved.

## Decisions required from the architect

1. Authorize or formally defer the three ACP gates.
2. Build or defer VM escalation, the nightly remote suite, and shared build
   cache policy.
3. Measure and set the worktree cap and disk floor; do not guess values.
4. Choose the reviewer auto-return default (currently disabled).
5. Schedule domains retirement and define its compatibility boundary.
6. Assign ownership and acceptance criteria for `describes` coverage,
   freshness stamps, and the 1,069-document adoption proposals.
7. Define the tracker reconciliation policy for shipped work and placeholder
   task contracts.
8. Define the live runtime-observability dogfood scope and acceptable provider
   receipts.
9. Decide whether zero warnings, rather than zero errors, is a release gate.

## Exact next sequence

1. Record the decisions above; do not silently waive human or product-owned
   work.
2. Run a dedicated tracker-reconciliation wave: fill or retire placeholder
   contracts, resolve review tasks, and classify historical warnings without
   weakening validator rules.
3. Review and promote documentation adoption proposals; add `describes` paths
   and verification stamps only after reading the corresponding source.
4. If authorized, run bounded live ACP/provider dogfood and retain the typed
   observation, timeline, lifecycle, and cancellation receipts.
5. Re-run source validation, strict skill doctor, focused checks, UI checks, and
   the full Go suite; record exact counts and keep candidate proof separate from
   human acceptance.
6. Commit the intended scope, build/install the current binary, verify version
   and capability parity, then perform the architect/operator release and human
   acceptance review.
