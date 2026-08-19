---
title: "V7 task model: schema, lifecycle, validation"
subject: tasks-and-proof
keywords: [task, epic, frontmatter, schema, lifecycle, status, readiness, validation, capsule, dependencies, spec_refs, acceptance]
part_of: overview
status: canonical
read_when: "Authoring, promoting, validating, or debugging a Tusker task or epic record — you need the exact frontmatter fields, status/readiness enums, transition rules, or the validator rule that is failing."
skip_when: "You need proof recording, evidence cards, or close/accept mechanics ([[proof-and-closeout]]), gate semantics ([[gates]]), or just the day-to-day command surface (the `tusker` skill)."
sources:
  - cmd/tusker/commands_v7.go
  - cmd/tusker/v7_validation.go
  - cmd/tusker/v7_control_cmd.go
  - cmd/tusker/v7_dependencies.go
  - cmd/tusker/v7_capsule.go
  - cmd/tusker/capsule.go
  - cmd/tusker/v7_traceability.go
  - cmd/tusker/v7_plain_top_layer_lint.go
  - cmd/tusker/v7_document_write.go
  - cmd/tusker/namespace_lint.go
  - cmd/tusker/frontmatter.go
  - cmd/tusker/schema.go
  - cmd/tusker/commands_show.go
  - cmd/tusker/upstream_hold.go
  - internal/v7schema/schema.go
---

# V7 task model

A task is one Markdown file: YAML frontmatter (machine state) + body (the
contract). Enums and field order live in `internal/v7schema/schema.go`;
every rule below is enforced in `cmd/tusker/v7_validation.go` unless noted.

## Records and paths

| Kind | `schema` | ID pattern | Required path |
|---|---|---|---|
| task | `tusker.task/v7` | `ABC-T-0001` | `.tusker/work/tasks/<ID>.md` |
| epic | `tusker.epic/v7` | `ABC` (3 uppercase letters) | `.tusker/work/epics/<ID>.md` |
| gate | `tusker.gate/v1` | `ABC-G-0001` | see [[gates]] |
| decision | `tusker.decision/v1` | `ABC-D-0001` | `.tusker/work/decisions/` |
| evidence | `tusker.evidence/v1` | `ABC-T-0001-E-0001` | see [[proof-and-closeout]] |
| attempt | `tusker.attempt/v1` | `ABC-T-0001-A-0001` | see [[proof-and-closeout]] |

Path and ID mismatches are hard errors (`PATH_MISMATCH`, `ID_SCHEME`) in
`validateV7Task` / `validateV7Epic`. `cmd/tusker/schema.go` also carries a
legacy pre-V7 status set (`taskStatuses`); it does not apply to V7 records —
`validateNote` routes anything with a `tusker.*/v7` schema to `validateV7Note`
and rejects `/v5` and `/v6` schemas outright.

Create with `tusker new task --epic ABC --title "…"` (`newV7Task`) and
`tusker new epic ABC --title "…"` (`newV7Epic`), both in `commands_v7.go`.
Creation refuses an ID that collides with an existing file and suggests the
next safe ID.

## Task frontmatter

Field order is `v7schema.FrontmatterOrder["task"]`; unlisted keys are appended
alphabetically (`stringifyFrontmatter`, `cmd/tusker/frontmatter.go`).

| Field | Required | Notes |
|---|---|---|
| `schema` | yes | must be `tusker.task/v7` |
| `kind` | yes | must be `task` |
| `id` `project` `title` | yes | `project` from `tusker.yaml` / `_system/project.yaml` |
| `epic` | on create | 3-letter acronym; matches the ID prefix |
| `status` | yes | see lifecycle |
| `readiness` | projected | see readiness; must not contradict reality |
| `priority` | yes | `p0`–`p3`, default `p2` |
| `risk` | yes | `low` `medium` `high` `critical`, default `medium` |
| `size` | default `m` | `s` `m` `l` `xl` |
| `next_owner` `next_action` | yes while open | `TASK_MISSING_NEXT_ACTION` if either is empty on a non-terminal task |
| `next_source` `next_ref` | projected | reconcile rewrites these |
| `proof_mode` `proof_status` | yes unless `cancelled`/`superseded` | see [[proof-and-closeout]] |
| `proof_required` | yes unless `proof_mode: none` | `TASK_PROOF_REQUIRED_MISSING` |
| `evidence_budget` | default per mode | exceeding it warns (`EVIDENCE_BUDGET_EXCEEDED`), errors under strict proof policy |
| `dependencies` | no | `ID` or `ID:hard` / `ID:soft` |
| `gates` | no | gate IDs; see [[gates]] |
| `spec_refs` | no while drafting; required to resolve for a demanding ready task at tier 2–5/default (tier 1 warns) | repo-relative doc path or a V7 decision ID; at least one entry must resolve before strict ready/dispatch |
| `domains` | no | >5 warns (`TASK_TOO_MANY_DOMAINS`) |
| `wave` | no | must match `W-0001` |
| `complexity` | no | `routine` `standard` `complex` `frontier` |
| `work_kind` | no | `implementation` or `integrator` only |
| `artifact_contract` | no | if present: valid `kind`, `summary`, durable repo-relative `path`, `acceptance_ids` that exist in the body |
| `accepted_by` `accepted_at` `closed_at` `close_authority` | at close | stripped by `tusker status` on any non-`done` move |
| `discarded_by` `discarded_at` `discard_reason` | at discard | stripped on any non-`cancelled` move |
| `created_at` `created_by` `updated_at` `updated_by` | yes | RFC3339 UTC |
| `state_rev` | yes | `sha256:` over frontmatter+body; the CAS token (`v7schema.StateRev`) |

Epic frontmatter requires `schema kind id project title status owner priority
created_at updated_at state_rev`; creation also writes the `next_task_number`,
`next_gate_number`, `next_decision_number` counters, and a missing capsule warns
(`CAPSULE_MISSING`).

Frontmatter with more fields than the configured warn limit raises
`FRONTMATTER_LONG` (`validateV7FrontmatterSize`).

## Task body: two layers

`v7TaskBody` (`cmd/tusker/commands_v7.go`) generates the template.

| Section | Layer | Purpose |
|---|---|---|
| `# ID · Title` | top | heading |
| `## Intent` | top | plain sentences; no paths, symbols, or commands |
| `## Acceptance` | top | `ID / Outcome / Proof` table, one row per outcome |
| `## Non-goals` | top | explicit out-of-scope |
| `## Implementation notes` | appendix | file map, moving parts, exact commands |
| `## Verification` | appendix | proof rows; each Check starts `command:`, `manual proof:`, or `ledger:` (a frozen legacy bare-command prefix list still validates) |
| `## Evidence` | appendix | Accepted / Pending links only |
| `## Knowledge delta` | appendix | what was learned |

`## Implementation notes` is the split point (`v7TaskLayers`,
`v7_plain_top_layer_lint.go`). A task with no appendix heading is treated as
legacy: only `## Intent` counts as the top layer.

Body rules (`validateV7TaskBodyPolicy`, `validateV7BodyBudget`):

| Rule | Severity |
|---|---|
| `## Work log` / `## Execution diary` present | error `TASK_WORK_LOG_SECTION` |
| `## Verification log` present | error `TASK_VERIFICATION_LOG_SECTION` |
| Evidence section >10 non-empty lines | error (>8 warns) |
| ≥5 raw-log-looking lines outside the Verification table | error `TASK_RAW_LOG_IN_BODY` |
| Body >220 lines (>120 warns) | error `TASK_BODY_TOO_LONG` |
| Knowledge delta >10 non-empty lines (>8 warns) | error `KNOWLEDGE_DELTA_TOO_LONG` |
| Obvious secret in frontmatter or body | error `SECRET_IN_RECORD` |

Limits are configurable under `validation:` in `tusker.yaml`
(`v7BodyLineLimitsFor`). Non-task warn/fail budgets (`defaultV7BodyLineLimitsFor`):
gate 80/160, domain index 180/300, canon 220/400, runbook 240/500, evidence
summary 80/160.

Acceptance IDs are parsed from table rows or `- [ ] A1: …` bullets and must
normalize to `A<digits>` (`v7AcceptanceIDs`, `normalizeV7AcceptanceID`).
Acceptance that reads `works`, `tests pass`, `tbd`, `todo`, `placeholder`, or
the scaffold default warns `ACCEPTANCE_TOO_VAGUE`.

## Status lifecycle

`v7schema.TaskStatuses`: `idea`, `backlog`, `ready`, `review`, `rework`,
`done`, `cancelled`, `superseded`.

| Status | Meaning |
|---|---|
| `idea` | raw, not yet committed backlog |
| `backlog` | accepted, not promoted |
| `ready` | pickable; contract must be dispatchable |
| `review` | submitted, awaiting verdict |
| `rework` | returned after a reviewer finding |
| `done` | closed; terminal |
| `cancelled` | discarded; terminal |
| `superseded` | replaced; terminal |

`tusker status <id> <status>` (`statusV7Cmd`, `cmd/tusker/v7_control_cmd.go`)
sets any of these **except**:

- `done` — refused; use `tusker close` so close policy runs. Only at adoption
  `tier: 1` may `status` set `done` directly (see [[cli]]).
- `cancelled` — refused; use `tusker discard` so dependents, gates, and
  discard metadata are handled.
- `active` — not a V7 status at all; the error points at the implementation flow.

Moving to `review` additionally requires no placeholder acceptance items unless
an explicit `acceptance_waivers` entry covers them (`errorEvidenceGate`), and —
for an `agent:` actor in a vault that has `WORKFLOW.md` and is not in
single-user local mutation mode — a fresh claimed/running lease owned by that
actor (`requireAgentWorkSession`, `cmd/tusker/commands_v7.go`). A `human:`
actor with `--break-glass --reason` bypasses the session check.

Every status write is a compare-and-swap on `state_rev`
(`saveV7DocumentCAS`), emits a `status_changed` event, and then runs
`reconcileV7ControlProjections` over the affected task set.

## Readiness (projected, not authored)

`v7schema.Readiness`: `ready`, `blocked_by_gate`, `blocked_by_dependency`,
`waiting_on_review`, `waiting_on_human`, `waiting_on_ci`, `held`, `done`,
`cancelled`, `superseded`. Readiness is a *derived* field —
`v7ProjectedTaskState` (`commands_v7.go`) recomputes it along with
`next_owner` / `next_source` / `next_ref` / `next_action`, in this precedence:

1. terminal status → readiness mirrors status, `next_owner: none`
2. valid terminal closeout → `waiting_on_human`
3. open blocking gate → `blocked_by_gate` (human-owned gate → `waiting_on_human`)
4. `status: review` → `waiting_on_review`, owner `reviewer`
5. failed upstream build → `held`
6. blocking dependency → `blocked_by_dependency`
7. otherwise → `ready`, owner `agent`

`validateV7TaskReadiness` errors `READINESS_STALE` when the authored value
contradicts reality (e.g. `ready` with an open blocking gate, or
`blocked_by_dependency` with none). The fix is `tusker reconcile`, not editing
the field. New tasks default to `held` unless created `ready`/`rework`.

## Dependencies

`cmd/tusker/v7_dependencies.go`. Each entry is `ID`, `ID:hard`, or `ID:soft`;
a wiki-link form (`[[ID]]`) is accepted. Without an explicit suffix the
hardness defaults from the *dependency's* risk: low/medium → soft,
high/critical/unknown → hard (`v7DefaultDependencyHardness`).

| Hardness | Unblocks when |
|---|---|
| hard | dependency `status: done` |
| soft | dependency `done`, **or** `review` with `proof_status: satisfied` |

A dependency carrying `build_failed: true` never satisfies (`v7BuildFailed`,
`cmd/tusker/upstream_hold.go`). The separate `held` projection
(`v7HeldByFailedUpstream`) fires on a red dependency *or* a `build_failed_command`
on the task's own wave, and ignores cancelled/superseded dependencies so a dead
marker cannot pin a dependent forever. Close uses
the stricter `v7UnclosedDependency`: every dependency must be `done`,
regardless of hardness. Tasks carrying `delivery_plan_scope` or
`delivery_cross_scope_dependencies` run an extra cross-scope integrity check
that parks consumers when the hard-dependency closure fails validation
(`v7CrossScopeIntegrityBlocker`).

## Traceability

`cmd/tusker/v7_traceability.go`. `spec_refs` entries resolve as either a V7
decision ID (`ABC-D-0001` → `work/decisions/<ID>.md`) or a repo-relative path
(also tried under `.tusker/` for `work/` prefixes). Unresolvable refs warn
`SPEC_REF_DANGLING`; absolute paths and `..` escapes never resolve. A demanding
task entering ready must have at least one resolvable entry at tier 2–5/default;
tier 1 emits `TASK_SPEC_REF_REQUIRED` as a warning. Refs are surfaced to workers
as required reads in capsules and packets
(`v7SpecRefsCapsuleLine`, `v7SpecRefsPacketSection`).

The reverse direction: any `## Work streams` section in `docs/specs/**`,
`docs/design/**`, or a decision record is scanned for task IDs and epic
acronyms; references to records that do not exist warn
`WORK_STREAM_REF_DANGLING`.

`validateCollisionProneNamespaces` (`cmd/tusker/namespace_lint.go`) applies
`WORKFLOW.md`-configured glob + capture-regex rules and flags two files
claiming the same sequence key (`NAMESPACE_COLLISION`).

## Capsules

Two distinct things share the name.

**Frontmatter capsule** — a `capsule:` mapping with `what`, `use_when`,
`skip_when` (`v7_capsule.go`, `capsule.go`). Required (warns when missing) on
domain, domain canon, knowledge, project skill, epic, doc, and spec records;
*not* required on tasks. Budget is 80 whitespace-delimited tokens by default
(`validation.capsule_token_budget`); over budget warns, over 2× errors.

**Rendered capsule** — `tusker show <ID>` output, which defaults to capsule
mode (`showCmd`, `cmd/tusker/commands_show.go`). It prints the frontmatter
capsule if present, else a `## Agent capsule` body section, else a synthesized
one; for tasks it appends status, readiness, `proof_mode/proof_status`,
`spec_refs`, next owner/action, and attempt runtime lines. Other slices:
`--acceptance`, `--evidence`, `--verification`, `--section <heading>`,
`--full`, `--json`.

## Plain-language lint

`lintV7PlainTopLayer` scans the top layer for code words: backticked spans,
dotted filenames, slash paths with an extension or ≥2 slashes, and mixed-case
tokens that carry corroborating evidence (lowerCamelCase, code-adjacent
punctuation, a `Cmd`/`Fn`/`Ctx`/`Err`/`ID` suffix, or a verbatim match in the
appendix). Table rows, headings, and fenced code are exempt. It **warns** by
default and **errors** when the task is demanding (`p0`/`p1`, or risk
`medium`+) *and* `readiness: ready`.

## Dispatchability

`v7TaskDispatchBlockers` (`commands_v7.go`) is the gate on handing work to a
runner, and creating a task directly as `ready` runs it as a preflight
(bypass with `--force-ready`). Blockers: kind not `task`, status not
`ready`/`rework`, readiness not `ready`, `next_owner` not `agent`/`agent:<n>`,
placeholder acceptance, acceptance
with no proof mapping, missing or non-exact `## Verification`, missing
`proof_mode`/`proof_required`, unresolved or unauthorized `wave`, unroutable
domains, and a failed upstream dependency. `validateV7DispatchableTasks`
re-runs this over every task already marked `ready`/`rework` **and**
`readiness: ready`, and errors `TASK_NOT_DISPATCHABLE`.

All of that is tier-gated. At adoption `tier: 1` (`.tusker/config.yaml`; see
[[cli]]) `tusker next`, `validate --dispatchable`, and the `create --status ready`
preflight fall back to `v7TierOneNextBlockers` — status in the trigger states and
`readiness: ready`, nothing about contracts or proof. Tiers 2+ (and the default,
5) run the full blocker list above.

## Writes

Canonical records are written through an flock'd atomic
temp-write-fsync-rename with a parent-directory fsync
(`cmd/tusker/v7_document_write.go`): symlinked, hard-linked, and non-regular
targets are refused, and the lock returns `CAS_BUSY` after 5s. With the
`state_rev` CAS, a concurrent writer loses rather than clobbers.

Protected fields (`v7ProtectedFieldsByKind`, `v7_validation.go`) — task
`status`, `readiness`, `wave`, `next_*`, `accepted_*`, `closed_at`,
`close_authority`, `superseded_by`, `discarded_*` — cannot be changed by hand
on an implementation branch; `tusker validate --branch-policy` diffs them
against the base ref and rejects the change.

## Related

- Proof rows, evidence cards, close and accept: [[proof-and-closeout]]
- Gates and human blockers: [[gates]]
- Dispatch, waves, and the daemon: [[orchestration]]
