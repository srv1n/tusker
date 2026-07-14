# Tusker Serve — backend gaps & wiring checklist

The UI is built **front-end-first** against a typed in-browser mock (`src/mock/`
+ `src/lib/api.ts`). This file is the convergence checklist and the hand-off to
the serve-backend team. It is aligned with **`docs/design/serve-ui-supplement.md`**
(the data contract + attention-routing spec) and **SRV-T-0001/0002**.

**The return types in `src/types/domain.ts` are the contract** — they should not
change when mock → real. Where a type is missing a field the UI now needs, it is
called out under [Missing fields](#3-missing-domain-fields).

---

## 0. Gap #1 — fixtures must come from a real vault snapshot

Every card in the current mock was **hand-written**, and hand-written fixtures
drift from reality (wrong titles, wrong status distribution — real boards are
ready-heavy, not in-progress-heavy — missing epics, and a fictional "needs you"
count). Per supplement §1:

> **Requirement:** generate mock fixtures from a real vault snapshot
> (`.tusker/work/**` frontmatter + a `tusker automation queue --json` capture),
> never hand-write them.

**Ask of backend/tooling:** a `tusker serve fixtures --snapshot` subcommand (or a
checked-in snapshot JSON) that emits the exact shapes in `src/types/domain.ts`
from live vault + queue state. Until it exists, the mock is illustrative of
*layout and logic only* — its task IDs, counts, and titles are not real. This is
the single highest-value gap: it makes the difference between a control plane the
operator trusts and one they learn to ignore.

---

## 1. Data contract — one source of truth per region (supplement §2)

Each region maps to exactly one authoritative source. The layout is already
right; what's missing is truth behind it.

| UI region | Source of truth | Status in UI | Gap |
|---|---|---|---|
| Task cards / status / epic / priority / risk | Vault frontmatter `.tusker/work/tasks/*.md` | mock `taskCapsules`/`taskDetails` | needs snapshot (gap #1) |
| Epic chips + counts | Vault, grouped by `epic` | mock `epics` (hardcoded) | **derive from vault; include *every* epic** — no hardcoded list. Real set: AGX/CLN/RUN/FBK/SRV/**TRC** |
| Active runs / lease / attempts | **Daemon runtime store** (not task status) | mock `runs` | needs daemon run API; `active` is not a durable task status |
| Blockers / readiness reasons | `tusker automation queue --json` `blockers[]` | not surfaced | render the strings verbatim (already agent-legible) |
| "Needs me" panel | **Derived** (see §2) | implemented client-side | move server-side; several inputs missing |
| Token / stat strip | Daemon store (attempts, turns) | mock totals | show `—` until AGX-T-0004 instruments it |
| Daemon footer | Daemon liveness (pid + **last-poll age**) | connected bool + addr | add `lastPollAt`; a live pid with a stale poll is a wedge — show the age, not just a green dot |
| Settings surfaces | Config resolver **with provenance** (RUN-T-0002 A6) | mock local config | every setting labeled with winning source; writes → user-global or `tusker.local.yaml`, **never committed `tusker.yaml`** |

Keep the mock's per-run **dead/stale/fresh** health labels — good invention, back
them with last-heartbeat age from the runtime store.

---

## 2. "Needs me" is derived — the closed five-signal rule (supplement §3)

Implemented in **`src/features/inbox/deriveNeeds.ts`** and wired through
`api.needs()` / `api.projects().needsCount`. The panel is now **computed, never
hand-flagged** — but two of five signals can't fire yet because the daemon/vault
don't expose the inputs. This whole derivation should move **server-side**
(SRV-T-0002); the client should ultimately just render `/api/needs`.

| Signal | Rule | Implemented? | Missing input |
|---|---|---|---|
| 1 Explicit human gate | unsatisfied gate with a human capability, authority, unresolved-intent, or subjective-acceptance boundary | ✅ | serve API must emit the gate owner, action, verification, and boundary |
| 2 Human-owned gate | unsatisfied gate, human owner | ⚠️ partial | gate needs a **`satisfied` flag** + a **payload** (clarify→`question`, provision→`ask`+`path`, approve-spec→`specTitle`+`specPath`); today only `{id,kind,owner}` |
| 3 Rework ping-pong | bounced runner→reviewer→rework ≥ 2× | ❌ | a **rework/bounce counter** per task (or transition history) |
| 4 Terminal run failure | exhausted retries, no lease to continue | ✅ | daemon should mark **terminal vs retry-queued** explicitly + surface the terminal error text + attempt count |
| 5 Wave boundary | batch-review trigger, as ONE card | ❌ | a **cohort-drain signal** from the daemon (see §4) |

**Explicit non-signals** (encoded, never enter the panel): review at any risk tier without an explicit human gate,
dep-blocked tasks, capacity-blocked ready tasks, deliberately parked leases.

---

## 3. Missing domain fields

Concrete additions the backend must supply (types in `src/types/domain.ts`):

- `Risk`: ✅ `"critical"` added — confirmed real in the Go schema
  (`cmd/tusker/schema.go`: `low\|medium\|high\|critical`); the serve API must
  emit it. Risk controls proof and landing safeguards, not human routing.
- Gate objects (`TaskDetail.gates[]`): add `satisfied: boolean` and a
  kind-specific payload (`question` / `ask` + `path` / `specTitle` + `specPath`).
- Task: a **rework/bounce count** (signal 3) and per-**project ownership**
  (`projectId`) — fixtures are currently single-project, so cross-project needs
  and per-project boards can't be derived.
- `RunSummary`/`RunDetail`: `terminal: boolean` (exhausted vs retry-queued),
  terminal `error` text, and `lastHeartbeatAt` / `nextWakeAt` for liveness
  (supplement N7: distinguish idle-alive from dead).
- `DaemonStatus`: `lastPollAt` (footer shows poll age).
- Roster fields (supplement §5): **`workingOn` / `blockedOn` / `handingOffTo`**
  as first-class served fields (not client-derived), plus handoff edges
  (runner→reviewer, task→dependent) for the fleet view.
- Epics: served from vault so **counts are derived** and the set is complete
  (the TRC epic is currently missing entirely).

---

## 4. Read endpoints (seam in `api.ts`)

`USE_MOCK = true` today; each method either resolves the mock or calls `real()`
(`fetch('/api…')`). Flip per-method as endpoints land.

| UI hook | Method | Endpoint (proposed) | Returns |
|---|---|---|---|
| `useDaemon` | `api.daemon` | `GET /api/daemon` | `DaemonStatus` (+ `lastPollAt`) |
| `useProjects` | `api.projects` | `GET /api/projects` | `ProjectSummary[]` (`needsCount` derived) |
| `useNeeds` | `api.needs` | `GET /api/needs?project=` | `NeedItem[]` (server derives §2 rule) |
| `useRuns` | `api.runs` | `GET /api/runs?project=` | `RunSummary[]` (daemon store) |
| `useRun` | `api.run` | `GET /api/runs/:taskId` | `RunDetail` |
| `useEpics` | `api.epics` | `GET /api/epics?project=` | `EpicSummary[]` (all epics, counts derived) |
| `useTasks` | `api.tasks` | `GET /api/tasks?project=` | `TaskCapsule[]` |
| `useTask` | `api.task` | `GET /api/tasks/:id` | `TaskDetail` |
| `useDocList` | `api.docs` | `GET /api/docs?project=` | `DocListEntry[]` |
| `useDoc` | `api.doc` | `GET /api/docs/*path` | `DocContent` |
| — (new) | — | `GET /api/roster?project=` | roster rows (§3 fields) + handoff edges |
| — (new) | — | `GET /api/review/batch` | wave-boundary batch card (§5 below) |

### Recommended flip order (per-method, via the `api.ts` seam)

Each method flips independently, so land them in dependency order and demo truth
incrementally (supersedes/expands the suggested sequence):

1. **daemon** — standalone footer; derisks the transport/seam first.
2. **projects** — sidebar rail. `needsCount` is the *same* server capability as
   `/needs` (both are the §2 derivation), so serve it as `null` here until step 8
   — the UI hides the badge on null — rather than a computed-but-partial count.
3. **epics** — chips; counts derived from the vault, include **TRC**.
4. **tasks** — board capsules; foundational for most screens.
5. **task detail** — the zoom.
6. **docs / doc** — library + reader-editor; daemon-independent, easy to make
   truthful early.
7. **runs** — daemon store (+ liveness).
8. **needs** — derived, moved server-side (SRV-T-0002); backfill
   `projects.needsCount` in the same step.
9. **roster / review-batch** — build on run/attempt data; the wave-boundary
   review surface (§6).

---

## 5. Write surface — every action is a thin wrapper over a CLI verb (supplement §6)

The UI must never invent a mutation path; every write goes through the
**setter-with-readback** rule (RUN-T-0002 A5 — `ok:true` no-ops are forbidden).

| Situation | Action | Verb |
|---|---|---|
| Objective review accepted | Close | `tusker close <id>` (proof and explicit gates enforced) |
| Review rejected | Rework + reason | `tusker status <id> rework` with note |
| Human gate | Satisfy + evidence | `tusker gate satisfy --evidence "<stmt>"` |
| Rework ping-pong | Amend contract, requeue | edit task + `tusker reconcile` |
| Terminal failure | Requeue or park | supervisor continue / explicit park |
| Friction observed | File it | `tusker feedback add` |
| Doc save (CAS) | Save body | `PUT /api/docs/*path` w/ base `rev` → 409 + both sides on conflict; 422 + issues on validation |

A conversational approval to the planner is a valid gate-satisfaction path
(the operator's statement is recorded as evidence); the UI and planner write
through the same verbs so neither surface drifts.

---

## 6. Review batching — the wave boundary (supplement §4)

The operator's **primary review surface** (per-task detail is the zoom, not the
landing page). Fires when a cohort of runs fully drains to review/done/failed, or
an epic's dispatched tasks all leave in-progress. Backend must expose the
cohort-drain trigger and assemble the batch: grouped by epic; per task the
capsule frontmatter, verification-row results (command + PASS/FAIL), risk tier,
and what the reviewer auto-closed vs what waits. Auto-closed low/medium tasks show
as a collapsed "closed by reviewer: N" line (audit, not re-review).

---

## 7. Live updates & embedding

- **Live:** views poll every 4s (`LIVE_REFETCH_MS` in `queries.ts`). Replace with
  `GET /api/stream` (SSE/ws) → invalidate the relevant `qk` query keys on event.
  Liveness thresholds (fresh <60s, stale 60–120s, dead ≥120s) are computed
  server-side and delivered on each `RunSummary`; the client only renders them.
- **Embed:** `bun run build` → `internal/serve/ui/dist`; the Go serve package
  `//go:embed all:dist`, serves the SPA under `/` (SPA fallback to `index.html`)
  and the JSON API under `/api`. Dev uses Vite on `:5173` against the mock.

## 8. Build-order implication (supplement §7)

Sections 1–3 are the **accuracy core**: real vault + queue data behind the
existing layout, plus the needs-me derivation. That is **SRV-T-0002** (read-only
MVP) and is worth pulling forward — the board layout is already right; what's
missing is truth. Batching (§6) and roster edges layer on the daemon's
run/attempt data. Actions (§5) come last and depend on the settings write-path
rules in RUN-T-0002 / SRV-T-0004.

## 9. Screen-specific `TODO(api)` markers

```
rg "TODO\(api\)" internal/serve/ui/src
```

Notable stand-ins: the doc editor now uses a real TipTap surface (markdown
round-trip, wiki-links, mermaid) but saves through the mock CAS machine; settings
screens render local mock config; run-detail is synthesized for tasks without an
explicit fixture.
