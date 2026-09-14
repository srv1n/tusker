# Direct-authoring guidance (FLW-T-0048)

Canonical agent guidance now teaches direct durable authoring only: the task
body is the implementation contract, one `task start`/`wave start` is the
scoped authorization, and daemon polling advances wave frontiers
automatically. Delivery plans are not taught as the canonical path.

## Command and help matrix

| Surface | Canonical wording |
| --- | --- |
| `tusker new task` | `--work-level light\|standard\|demanding` and `--body-file <path\|->` required; `--epic`/`--spec-refs` optional, refs must resolve |
| `tusker task update` | CAS `--if-revision` mutation of mutable authoring fields; rework on material change to done/review tasks |
| `tusker wave create` | `--file <request.yaml> --request-key <key>` atomic `tusker.wave-authoring/v1`; inert and idempotent via the immutable receipt |
| `tusker wave review` | read-only state/authorization/frontier/blocker/controls projection |
| `tusker task start` | `--mode interactive --by <agent> --current-workspace` claims this workspace; `--mode background` persists a task-scoped directive |
| `tusker wave start` | `--mode background --by human:<name>\|operator:<name>` arms exact material; daemon advances frontiers automatically |
| `tusker wave pause` / `wave resume` | `--by human:<name>\|operator:<name>`; pause blocks new admissions preserving fingerprint/actor/time; resume restores armed only for unchanged material |
| `tusker capabilities --json` | matching purposes for new/task start/task update/wave create/review/pause/resume/start |

No canonical help, capability purpose, or skill reference recommends
delivery plan/import, marking ready, wave arming, preflight, or Play as a
prerequisite.

## Packet shape

Agent and reviewer packets project `## Wave context` (wave ID, outcome, and
the exact `## Shared context` body) before `## Task contract` for wave
members; standalone tasks omit it and stay self-contained. Agent packets add
`## Execution entry` after canonical metadata and ownership with the exact
one-action Start commands and pause/resume/task-scoped semantics. Literal
body and check strings — pipes, Markdown escapes — survive byte-for-byte;
receipt/admin fields are never projected.

## Specimens

- Ad hoc: `skills/tusker/references/HANDOFF.md` "Ad hoc example" — a light
  single-task body authored through `new task --body-file -`.
- Substantial DAG: same file, "Substantial DAG example" — an atomic
  `tusker.wave-authoring/v1` request covering edge direction
  (dependency -> dependent), `DEPENDENCY_CYCLE`/`DEPENDENCY_DANGLING`
  authoring refusals, readiness, failure propagation, `task update` rework
  semantics, verified source/type seams, locked-versus-proposed names,
  ownership, and exact checks.

## Cold-reader review outcome

The terse specimen `Implement a DAG` round-trips through a fresh packet
verbatim and visibly terse — no structural validator rejects or pads it.
Classified semantically insufficient by source comparison: it names no
verified source seam, no failure case, and no exact check, which the
cold-reader checklist in HANDOFF.md requires a human review to catch. That
is a judgment step, not a validator claim.

## Focused verification

```bash
go test ./cmd/tusker -run '^TestDirectWavePacket' -count=1 -v
```

Five tests, all passing: standalone level/metadata retention, wave context
plus scoped bodies with literal-check fidelity, execution-entry commands and
superseded-choreography exclusion, the rich DAG specimen versus terse
contrast, and help/capability-to-dispatch correspondence.

## Proof labels

All coverage is local executed fixture proof: canonical records in temporary
vaults plus packet/capability rendering in-process. No daemon process,
provider, or live runner was exercised — **NOT RUN**.
