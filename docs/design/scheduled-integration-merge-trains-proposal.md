# Proposal: Scheduled Integration ("Merge Trains") as a Tusker Daemon Capability

Status: historical proposal, superseded as implementation direction by
`docs/specs/12-opt-in-scheduled-promotion.md`. Origin: proven in production
shape in the rzn backend repo on 2026-07-24 as a shell conductor + launchd
schedule. Keep this document as problem/pilot context; use the binding spec for
current architecture and accepted production follow-ups.

## 0. The idea in plain English

When several agent lanes work a repo in parallel, the thing that actually burns human days is integration: finished work sits on branches waiting for someone to merge it, and every "should we merge now?" conversation costs a morning. The fix that worked: main integrates on a **fixed schedule, like a train timetable**. Lanes work at their own pace in their own worktrees; a lane that is ready **boards the next train**; a lane that is not ready catches the one after. Nobody waits on anybody, main never lags, and every day starts from an already-merged, already-tested main.

The second half of the idea is cost discipline for a solo developer paying per token: the **scheduler must be free**. A dumb deterministic check (no LLM) runs at each departure time and answers "is there any cargo?" If no lane is ready, it logs one line and exits — a day off costs zero. Only when there is real work does it start a paid model session to act as the integrator. Guardrails let the human hold all departures (day off), hold releases only, or exclude branches, all without waking a model.

Today this exists as ~120 lines of zsh plus a launchd plist, hard-wired to one repo on one Mac. It works, but every piece of it is generic, and Tusker already owns the state that would make it much better. Hence this proposal.

## 1. What is already proven (the rzn pilot)

- Two departures per day: a **light train** (merge ready lanes, focused seam checks, push) and a **full train** (same, plus the full test harvest on an ephemeral cloud box, a benchmark-ledger row, and a deploy only on green).
- A **boarding checklist** defines "ready": clean worktree, branch ahead of origin/main, green focused proof recorded, report filed, no open blocker, no live worker mid-flight on the lane.
- The **conductor** (free shell) censuses worktrees against origin/main, subtracts an exclusion list, and fires a paid integrator session only when cargo exists. The full train also departs when main advanced since the last full departure, so self-merged changes always get gated.
- Operator switches, all checked before any spend: a hold file (skip everything), a deploy-hold file (gate but never release), an exclusion pattern file, and a `check` mode that prints the decision without firing.
- One rule survived every iteration and must survive generalization: **one integrator per departure; lanes never merge to main themselves.** Self-merge exists only as a rare, explicit per-lane grant.

## 2. Why Tusker is the right home

The shell conductor infers readiness from git alone (clean + ahead of main). That is a heuristic, and it has already produced one near-miss: partition branches owned by a running multi-worktree lane looked "ready" to git and had to be hand-excluded. Tusker knows the truth the heuristic approximates:

- **Task state is the boarding signal.** A lane is ready when its task is in review/done with proof recorded — not when its branch merely looks quiet. Boarding becomes a query, not an inference.
- **Live-owner detection is first-class.** Tusker knows which attempts are running; the conductor currently greps process tables.
- **The integrator becomes an audited run.** Today the fired session is an orphaned background process; as a Tusker attempt it gets a journal, evidence, and reconciliation like any other work.
- **Cross-project resource arbitration needs a daemon anyway.** On one machine, multiple projects must not run heavy gates concurrently (e.g. only one Cargo stream per host). Independent per-repo cron jobs cannot see each other; a single daemon owning the timetable can serialize expensive departures across all projects — it becomes the natural owner of the build-slot ledger.
- **Self-merge grants become task attributes** instead of tribal knowledge in prompt text.

## 3. Proposed architecture

Three layers, strictly separated by cost:

**Layer 1 — Scheduler (free, daemon).** The Tusker daemon holds a timetable of departures across all opted-in projects. At each departure time it evaluates that project's departure predicate. No model, no network beyond `git fetch`. Skips are logged with reasons so spend is auditable by reading one log.

**Layer 2 — Conductor predicate (free, deterministic).** Per project, compute the boarding list: tasks in a boardable state whose branch has a clean worktree ahead of the project's main, minus exclusions, minus lanes with a live attempt. Apply guardrails in order: global hold → project hold → quiet hours → budget ledger → minimum-cargo threshold. Empty list (and, for full trains, main unmoved since last gate) → skip. The predicate's output is the census: branch, ahead-count, task, proof pointer.

**Layer 3 — Departure (paid, dispatched).** The daemon dispatches one integrator worker as a normal Tusker attempt, with a generated prompt containing the census, the project's merge policy pointer, and the gate/deploy commands from config. Model and reasoning tier come from config (integration is mid-tier work; do not burn the top tier on merges). The integrator merges per the boarding checklist, runs the configured gate, pushes, records transitions, writes the train report. Red gate → freeze, file a repair-batch proposal, stop. Never bypass a red gate.

## 4. Opt-in config (the whole contract in one file)

No `train.toml`, no trains — the capability is invisible to projects that don't ask for it. Sketch:

```toml
# .tusker/train.toml
[project]
main_branch = "main"
merge_policy = ".tusker/knowledge/.../merge-train.md"   # canon the integrator must read

[[departure]]
kind = "light"
schedule = "18:00"
gate = ["make check-fast"]                    # cheap seam checks

[[departure]]
kind = "full"
schedule = "00:05"
gate = ["make test-full PROFILE=nightly"]     # may declare a remote/ephemeral runner
gate_runner = "ephemeral-box"                 # or "local", or "ci" (see §6)
deploy = "scripts/ops/deploy.sh"              # optional; honors deploy-hold

[boarding]
task_states = ["review", "done"]
require_proof = true
exclude_branches = ["w0003/*", "crd-*"]

[integrator]
dispatch = "codex"                            # or "claude"
model = "gpt-5.6-terra"
effort = "high"

[budget]
max_paid_departures_per_day = 2
monthly_spend_cap_usd = 100                   # daemon-tracked, skip + notify past cap
min_ready_lanes = 1
```

Global operator switches stay daemon-level and instant: `tusker train hold [--project X] [--deploys-only]`, `tusker train resume`, `tusker train check --project X` (print the decision, spend nothing), `tusker train log`.

## 5. The readiness contract (the generic core)

This is the piece worth standardizing beyond any one repo. A lane boards a train when ALL hold:

1. Its Tusker task is in a boardable state (config: usually review/done) with proof recorded per criterion.
2. Its branch has a clean worktree and is ahead of the project main.
3. It is not excluded and has no live attempt running.
4. No open blocker artifact exists for the task.

And the two standing invariants: lanes never merge to main (self-merge = explicit per-task grant, recorded on the task), and the train never finishes or reworks a lane's content — it merges, repairs seams, gates, and reports. A lane that fails boarding is skipped with a recorded reason, never "helped."

## 6. Not every project has a painful compile — the gate is pluggable

The Rust repo needs an hour-long harvest on a rented box; a TypeScript app needs minutes of tests; a docs site needs a build and a link check. The train model is identical in all three — only the gate config differs:

| Project class | Light gate | Full gate | Runner |
| --- | --- | --- | --- |
| Heavy (Rust backend) | package-scoped checks | full nightly harvest | ephemeral cloud box |
| Medium (app/service) | typecheck + affected tests | full suite + e2e | local or CI |
| Light (docs, sites, tools) | lint + build | same as light | local |

A `gate_runner = "ci"` mode covers projects whose truth already lives in GitHub Actions: the train merges, pushes, and watches the CI result instead of running anything itself — the cheapest possible full train. The daemon serializes departures whose runner contends for a shared resource (one heavy local gate at a time machine-wide); light gates can overlap freely.

## 7. Cost model, stated bluntly

- Evaluating "should a train run" costs zero, forever, on every project, every day.
- A paid session starts only when the census found cargo (or main moved unGated). Its tier is config-pinned to mid-tier by default.
- Budget ledger is daemon-owned and hard: past the daily-departure or monthly-spend cap, trains skip and notify instead of firing.
- Every skip and every firing is one log line with the reason and the census, so "where did the money go" is a grep, not an investigation.

## 8. Rollout

1. **Absorb the pilot.** Daemon runs the existing rzn timetable via `train.toml`, git-heuristic boarding (parity with the shell conductor). launchd keeps a fallback entry until parity is proven; the shell conductor is then retired.
2. **Task-state boarding.** Swap the git heuristic for the readiness contract in §5. This deletes the exclusion-file workaround for partition branches — lanes without boardable tasks simply never count.
3. **Multi-project + budget ledger.** Second and third projects opt in with their own gates; daemon-level serialization of heavy runners and the spend ledger land here.

## 9. Open questions for this thread

- Scheduler residence: is the existing daemon the right host, or does the runtime want a separate scheduler process supervising it?
- Notification surface for red trains and budget-cap skips (the human must hear about these without polling a log).
- Should the train report + census become first-class Tusker artifacts with their own schema, so cross-project morning review is one query?
- Secrets for deploy hooks: presumably the same store discipline as e2e credentials — worth stating as a contract requirement.
- Timetable semantics on a laptop that sleeps: fire-on-wake for missed departures, or skip silently? (launchd coalesces; the daemon should choose deliberately.)
