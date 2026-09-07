# Manual walkthrough — real-work UI acceptance

Use this walkthrough only after the machine lanes in `[[completion-and-integrated-acceptance#canonical-end-to-end-execution-plan]]`; required browser skips remain incomplete.

Operator runbook. No tickets to author: follow top to bottom against the seeded project. Read-only until step 4; steps 4–6 queue real daemon work — run them deliberately, one at a time.

## 0. Prerequisites

- Fixture owner's seeded project: reset and seed with the exact command from `docs/reports/real-work/fixture/report.md` (pending publication; planned entry point `scripts/test-real-work-project.sh`). Note the printed project ID — use it below as `<PID>`.
- Test Serve backend on a free port (never on the resident's port), with the repo vault as working directory:
  ```sh
  export TUSKER_STATE_ROOT=/tmp/tusker-realwork-state  # chmod 700
  tusker serve --addr 127.0.0.1:7431 --by human:<your-name>
  ```
- Candidate UI (this report's code) with `/api` pointed at the test backend. `vite.config.ts` proxies to `127.0.0.1:7420`; for another backend port, run vite with a one-off override config or retarget temporarily and revert:
  ```sh
  cd internal/serve/ui
  bunx vite --port 5193   # open http://127.0.0.1:5193
  ```
- The resident daemon must be running: the UI Run button records a one-shot `tusker task run` directive that only the daemon consumes. If the directive is refused with "daemon is not running", start it first.
- Headless Chrome available (`channel: "chrome"` via the existing Playwright install).

## 1. Open the project

Open `http://127.0.0.1:5193/p/<PID>/waves`. Expect the Waves overview with the standalone-task entry, Alpha, Beta and Follow-up, and the `Live updates` note in the Work header. If it reads `Reconnecting — showing last known state`, stop: the stream is down and nothing below proves liveness.

## 2. Individual task first (A1)

1. Go to Board (`/p/<PID>/tasks`). Click the standalone task card.
2. Inspector shows: stage chip (`Ready to start`), `What this task achieves` (intent), `Active attempt` (Identity `Unavailable` before any run — a past worker's model must never appear), acceptance rows and exact verification commands.
3. Click `Open full task →`. Expect either `Execute once` (ready, no live run) or a truthful readiness reason (`Blocked by prerequisites`, `task is not runnable`, `daemon is not running`).
4. Press `Execute once` for the standalone task. Watch the inspector/board: stage moves to building with a live-run fact, progress events arrive without reload.
5. CLI cross-check (same backend state): `tusker runs --json` shows the attempt; after review/close the UI and CLI agree on `done`. A clean process exit with no accepted review must NOT read `Delivered`.

## 3. Waves Alpha then Beta (A2/A3)

1. Open `/p/<PID>/waves/<ALPHA>` Flow. Expect two parallel branches joining into A4 (edges visible, readable labels, model names only on live runs).
2. Start Alpha, then Beta, via the normal wave Run controls (not a demo route).
3. Stay on the DAG while they run: nodes refresh without reload, your selection and zoom stay put (zoom % in the toolbar must not change on selection or event), and the open view never jumps to Results on its own.
4. Joins (A4, B4) stay waiting until both prerequisites meet native rules. Follow-up becomes ready but does NOT auto-start — start it explicitly.
5. CLI cross-check: attempt intervals for Alpha and Beta overlap; `tusker wave show` (or the serve `GET /api/waves/<id>`) agrees with the DAG on member states.

## 4. Disconnect and retry (A4/A5)

1. Stop the test Serve backend briefly (or block `/api/stream`): the header must flip to `Reconnecting — showing last known state`. Restart: counts converge without duplicate nodes or regressed states.
2. If a run fails or is interrupted, its detail offers a distinct retry; unrelated waves keep running. Retry creates a new truthful attempt — prior history stays.

## 5. Completed wave entry (A6)

Open a landed/closed wave fresh (paste its URL in a new tab). Results lead: delivered outcome, independent review facts, accepted evidence. Flow is one click away. Evidence listed is labeled available, never accepted, unless its proof status records acceptance.

## 6. Documents journey (A7)

1. Open `/p/<PID>/knowledge`. Use the folder introduction to pick the product-intent note; open it.
2. Expect: title leads, content readable, routine front matter inside Details, Mermaid renders with source/error fallback, wiki-link takes you to the decision record, backlinks and the superseded→current link work.
3. CLI parity (once the owner ships the verbs): `tusker docs browse`, `docs read <subject> --json`, `docs read <subject> --section '<exact heading>' --json`, `docs backlinks <subject> --limit 20 --json`, `docs check --json` must agree with what the UI showed. Today these verbs do not exist (`unknown command`) — record the mismatch, do not work around it in the UI.

## 7. Narrow screen and keyboard (A8)

At 390×844: Board → task inspector opens, Escape closes it, focus returns sanely, no horizontal scrolling, long task names remain reachable. In the DAG, the List toggle gives the same members as text buttons.

## 8. Automated journey (A9/A10 support)

```sh
cd internal/serve/ui
TUSKER_REALWORK_BASE_URL=http://127.0.0.1:5193 \
TUSKER_REALWORK_PROJECT=<PID> \
node test/real-work.browser.mjs
```

Pass = named `PASS` lines plus honest `SKIP`s (e.g. no failed runs for A5, no completed wave for A6). Exit 2 with `READINESS WIN2` means the fixture shape is absent — reseed, do not debug the UI. Screenshots land in `docs/reports/real-work/ui/`; hand each to a fresh critic with the A1–A10 rubric before calling the journey green.
