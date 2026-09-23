# Run-session continuity qualification (TSK-T-0054)

This report is the qualification record for W-0039's final integration task.
Checkout `bb697a9e` with local, uncommitted W-0039 edits was used. Focused
Go and UI checks, both source builds, and an isolated built-browser journey
passed.

## Proof boundaries

| Boundary | Scope | Status |
| --- | --- | --- |
| Source | `cmd/tusker/run_session_journey_test.go`, checkout `bb697a9e` plus local edits and existing ACP/EventLog/RuntimeStore seams | PASS — focused Go suite and Go source build passed |
| Fixture adapter | Provider-free `cmd/tusker/testdata/fake_acp` with disposable temp state and session identities | PASS — journey and activity checks ran against fake ACP |
| Built browser | `internal/serve/ui/test/run-session.browser.mjs` against an explicitly declared disposable state root and built Serve | PASS — 5/5 checks, reopen retained activity, five recovery controls rendered, and Stop reached settled canonical readback |
| Installed application | Mac app or installed CLI identity | NOT RUN — no install, restart or resident state mutation is authorized by this task |
| Live provider | Provider account, spend, native live session and real worker | NOT RUN — fixture proof cannot qualify a live provider |
| Human acceptance | Operator confirms recovery controls and retained screenshots | NOT RUN — no human walkthrough is claimed |

The browser journey refuses to start unless the caller supplies
`TUSKER_RUN_SESSION_DISPOSABLE=1`, an absolute
`TUSKER_RUN_SESSION_STATE_ROOT`, a project, a task and a Serve URL. It never
defaults to the resident database or a live project. Mutating controls require
the separate `TUSKER_RUN_SESSION_ALLOW_ACTIONS=1` opt-in.

## Scenario matrix

| Scenario | Acceptance | Result | Evidence / observation |
| --- | --- | --- | --- |
| `messages_survive_reopen_without_private_content` | A1, A2 | PASS, fixture | Fake ACP `activity` mode, public message/tool projection, secret/reasoning exclusion, closed and reopened RuntimeStore, explicit generic ACP fresh-only resume. |
| `crash_restart_preserves_delivery_unknown` | A1 | PASS, fixture | Fake ACP exits after accepting the prompt; durable status is `delivery_unknown`, session uncertainty survives store reopen, and redrive is refused until an operator decision. |
| `duplicate_actions_are_fenced_after_restart` | A1 | PASS, fixture | Two claim attempts use the same unclaimed snapshot; exactly one lease/attempt wins, then the canonical run and native session are read from a cold store reopen. Fixture project `isolated-project`, job `TSK-T-0054-journey`, attempt `attempt-first`, native session `native-session-1`; second attempt refused. |
| browser activity and recovery facts | A2 | PASS, isolated built Serve | Project `app`, task `TSK-T-0054-browser`, attempt `attempt-browser`; two readable activity events survived reload, private fixture content stayed hidden, five controls rendered, and the authorized Stop action settled the attempt without launching a provider. Screenshots: [`before reopen`](run-session-continuity-ui/run-session-before-reopen.png), [`after reopen`](run-session-continuity-ui/run-session-after-reopen.png). |
| report boundary and retained scenario names | A3 | PASS, source | This report is linked by `TestRunSessionJourney`; installed/live-provider rows remain explicit NOT RUN boundaries. |
| installed application qualification | A3 | NOT RUN | Requires a separately identified installed build, safe disposable runtime and restart authority. |
| live-provider qualification | A3 | NOT RUN | Requires separately scoped provider authority, budget, identity and live-session evidence. |

## Verification run

`GOCACHE=/private/tmp/tusker-go-cache go test ./cmd/tusker -run '^(TestRunActivity|TestRunSession|TestResolveResumeSessionHonorsFreshSessionDecisions)' -count=1 -timeout 120s`: PASS. `bun test tests/runs-detail.test.ts tests/run-session-controls.test.ts`: PASS, 16 tests. `bun run build`: PASS. `GOCACHE=/private/tmp/tusker-go-cache go build -o /private/tmp/tusker-run-session-build ./cmd/tusker`: PASS. The isolated browser command below: PASS, 5/5 checks. The repository-wide UI suite was also attempted and is not used as W-0039 proof because unrelated pre-existing source-contract failures remain.

```text
command: go test ./cmd/tusker -run '^TestRunSessionJourney' -count=1
command: node internal/serve/ui/test/run-session.browser.mjs
manual proof: inspect this report against the command output, disposable state identity, API/UI readback, screenshots and launch/effect counts
```

The passing browser command used:

```text
TUSKER_RUN_SESSION_BASE_URL=http://127.0.0.1:17420
TUSKER_RUN_SESSION_PROJECT=app
TUSKER_RUN_SESSION_TASK=TSK-T-0054-browser
TUSKER_RUN_SESSION_STATE_ROOT=/private/tmp/tusker-browser-aw6dFx/state
TUSKER_RUN_SESSION_DISPOSABLE=1
TUSKER_RUN_SESSION_ALLOW_ACTIONS=1
TUSKER_RUN_SESSION_OUT=/private/tmp/tusker-browser-aw6dFx/screenshots
node internal/serve/ui/test/run-session.browser.mjs
```

Fixture proof records the identities above and one successful lease claim with
zero duplicate claims. The browser journey performed one durable Stop effect,
observed it after settlement, and launched zero provider processes. The retained
screenshots are source-tree evidence for the isolated browser boundary only. No
installed application or live provider action was attempted.
