# Live-harness qualification runbook (D6)

Manual end-to-end checks for W-0042 (run sessions) and W-0043 (agent mailbox) against real Claude Code, Codex, Devin and Muse. Decision D6 in `.tusker/specs/decisions/2026-09-23-harness-sessions-grill.md` requires this before a real project is promoted. The existing fixture proof, `docs/reports/run-session-continuity-qualification.md`, leaves "live provider" as NOT RUN. This runbook covers that gap.

The operator runs every step by hand from a human terminal. Agents must not run it: it needs the resident daemon, provider spend and `human:` actor authority.

## Where the e2e repository comes from

The existing e2e test repository is the disposable real-work fixture created by `tusker demo seed --scenario parallel-waves`. Its contract is `.tusker/specs/real-work-test-repository.md`, and `docs/reports/real-work/fixture/report.md` records how it was built. No persistent checkout exists. The runtime registration named `e2e` points to a scratch directory that has since been deleted. Each pass therefore seeds a new directory.

`seed.sh` (in this folder) runs `demo seed` and then adds epic `QLH`. For each harness it creates one small task and a wave containing only that task. `new task --execute-profile` pins the task's execute lane to that harness's global profile, and `--review-profile` pins review to a profile on a different harness (Codex tasks are reviewed by `devin-swe-2-high`, the others by `codex_exec-gpt-6-sol-low`; `--review-profile <name>` overrides all of them). Nothing is enabled, armed or launched.

## Gaps found at HEAD (`fad658aa` + working tree)

| # | Gap | Effect on this runbook |
| --- | --- | --- |
| G1 | Fixed. `demo seed` used to write `automation.profiles` (`execute-fast`, `review-independent`, `gpt-5.6-luna`) into `.tusker/config.local.yaml`, which HEAD rejects. The seeded overlay now holds only concurrency, the completion reactor and validation, and routes through the global `automation.model_levels`. The live `permission-deny` session driver now reports `unsupported`, because forcing a denial would mean editing the global config. | None. |
| G2 | Fixed. `new task --execute-profile/--review-profile` are accepted and must name a resolvable global profile. `task update` gained `--execute-profile`, `--review-profile`, `--clear-execute-profile` and `--clear-review-profile` on the CAS path; pins do not change the contract fingerprint. | `seed.sh` pins each lane per task. The reviewer runs on a different harness. |
| G3 | Fixed. `execution` commands map the vault to its runtime-registered project (the ULID) and accept `--project <id\|key\|name>`. `--contact-role` registration works: `validateExternalContactSubject` (`agent_contacts.go`) accepts a subject note whose `project` frontmatter is the vault's config `project_id` (for example `tusker-live-qual`) as well as the runtime ULID. | None. |
| G4 | Fixed. `tusker message --help` now prints the full usage (ask, send, reply, list, show, consume, apply, inbox, hook print) with address syntax and worker rules. | None. |
| G5 | Fixed. The global config has `muse-spark-1.3-high` (harness `muse`, model `muse-spark-1.3`, effort `high`, eligible tier `standard`, same access shape as `codex_exec-gpt-6-sol-low`). It is not in `model_levels`; tasks reach it only through a pin. | `seed.sh` uses it by default (`--muse-profile` overrides it). |
| G6 | Muse has no MCP overlay (`runner_muse.go`; Muse 1.3 `exec` has no per-run MCP flag and Tusker does not edit Muse settings), so its worker asks through the CLI. `PATH` is not a risk: the prompt uses `"$TUSKER_BIN"`, the absolute path of the daemon binary. The open question is whether the Muse shell sandbox lets that command write Tusker's state root (`~/Library/Application Support/tusker`). Muse exposes no flag to add a writable directory. | The Muse task tells the worker to use `"$TUSKER_BIN" message ask`. A permission or read-only-database error means the sandbox blocks the state root: record it as a finding, not a pass. |

## Cost estimate

Each qualification task is tiny: two progress pauses (90 s + 60 s), one file and one question. The main spend is model turns spent re-reading the harness system prompt, most of which should be cache hits.

| Harness | Profile (execute / review) | Turns expected | Approx. tokens | Billing |
| --- | --- | --- | --- | --- |
| Codex | `codex_exec-gpt-6-luna` (xhigh) / `devin-swe-2-high` | 4-5 execute + 1 review | 100-200k input (mostly cached), under 10k output | ChatGPT login quota; smallest of the four |
| Claude Code | `claude-opus-high` / `codex_exec-gpt-6-sol-low` | 3-4 execute + 1 review (soft Say adds no turn) | 150-300k input (mostly cached), under 10k output | Claude subscription quota (D5: your own login); largest of the four |
| Devin | `devin-swe-2-high` / `codex_exec-gpt-6-sol-low` | 4-5 execute + 1 review | similar to Codex | Devin account usage (ACUs); pricing not verified here |
| Muse | `muse-spark-1.3-high` / `codex_exec-gpt-6-sol-low` | 4-5 execute + 1 review | similar to Codex | Muse account |
| Forced-failure tasks | `qual-bad-<harness>` | 0-1 (should fail at launch or admission) | about 0 | none |

Dollar figures are not given because GPT-6, Devin and Muse pricing could not be verified. Record the real totals from `tusker runs inspect <TASK> --json` → `.token_totals` in the results table. Stop and record the result if any run exceeds about 10 turns.

## Step 0: setup (once per pass)

Run everything from a normal terminal, not inside an agent session. Replace `sarav` with your name.

```sh
ME=human:sarav
BIN=$(command -v tusker); "$BIN" version        # record the binary sha256
QUAL=/tmp/tusker-live-qual                      # must not exist yet
TUSKER_CHECKOUT=/Users/sarav/Downloads/side/tusker
```

**0.1 The resident daemon and Serve are up.** Start them yourself in an independent shell or with launchd (`tusker daemon service start`). Never start them from an agent.

```sh
tusker daemon status --json          # expect running; note the active-run count
curl -s http://127.0.0.1:7420/api/daemon | head -c 300
tusker projects list --json          # note which OTHER projects are enabled; leave them alone
```

**0.2 Harnesses are installed and logged in.** This check does not start a model.

```sh
tusker runner catalog --refresh --json   # claude-code, codex_exec, devin, muse: available / authenticated
tusker models show --json | grep -c muse-spark-1.3-high   # G5 profile present
```

**0.3 Create the forced-failure profiles** used by step (g). Each one names a model that does not exist.

```sh
for p in codex:codex_exec claude:claude-code devin:devin muse:muse; do
  REV=$(tusker models show --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["revision"])')
  tusker models profile-set --scope global --name "qual-bad-${p%%:*}" --harness "${p#*:}" \
    --model qual-no-such-model --effort low --if-revision "$REV" --json >/dev/null
done
```

**0.4 Seed the project.** This is inert and never enables or arms anything.

```sh
sh "$TUSKER_CHECKOUT/docs/qualification/live-harness/seed.sh" --repo "$QUAL" --with-failure
. "$QUAL.proof/ids.env"; cd "$REPO"
cat "$QUAL.proof/ids.env"   # PROJECT, <H>_TASK/_WAVE/_PROFILE/_REVIEW_PROFILE and <H>_BAD_TASK/_WAVE
```

Expect eight tasks, `QLH-T-0001`…`0008`, and waves `W-0005`…`W-0012`. The IDs alternate good and bad per harness.

**0.5 Routes resolve to the intended harness.** This is read-only.

```sh
for t in $CODEX_TASK $CLAUDE_TASK $DEVIN_TASK $MUSE_TASK; do
  for lane in execute review; do tusker runner route "$t" --lane $lane --vault "$VAULT" --json |
    python3 -c 'import json,sys;d=json.load(sys.stdin);p=d["profile_definition"];print(d["task"],d["lane"],d.get("profile"),p["harness"],p["model"],d["blockers"])'; done; done
```

Pass: every execute line shows `<H>_PROFILE`, every review line shows `<H>_REVIEW_PROFILE` on a different harness, and blockers are `[]`. □

**0.6 Enable automation for this project only.**

```sh
tusker projects enable --repo "$REPO" --dry-run --json   # scope: no armed waves yet
tusker projects enable --repo "$REPO" --json
tusker automation explain "$CODEX_TASK" --vault "$VAULT" # only blocker left: wave ... disarmed
```

Pass: the only remaining blocker is wave authorization. □

**0.7 Helpers used in every step:**

```sh
st() { tusker runs inspect "$1" --json | python3 -c 'import json,sys
d=json.load(sys.stdin); r=d["run"]; o=d["operator_state"]
print("state:",o["state"],"reason:",(o.get("reason") or {}).get("code",""),"session:",r.get("session_ref",""))
for a in d["attempts"]: print("  attempt",a["AttemptID"],a.get("Outcome",""),"session",a.get("SessionRef",""))
print("tokens:",d["token_totals"])'; }
run_url() { echo "http://127.0.0.1:7420/p/$PROJECT/runs/$1"; }
```

## Per-harness procedure

Run the full procedure once per harness. Order: Codex, Claude Code, Devin, Muse (cheapest first). Before each run, set:

```sh
H=codex  TASK=$CODEX_TASK  WAVE=$CODEX_WAVE  PROFILE=$CODEX_PROFILE  BAD_TASK=$CODEX_BAD_TASK  BAD_WAVE=$CODEX_BAD_WAVE
# claude: H=claude TASK=$CLAUDE_TASK ... ; devin: H=devin ... ; muse: H=muse ...
```

What each harness should do:

| Harness | Say route (`runs say` → `.route`) | Question route | Question wait | Continue resumes via |
| --- | --- | --- | --- | --- |
| Codex | `hard`: the turn is interrupted and the same thread is resumed | MCP `ask` | 300 s inline | `codex exec resume <thread>` |
| Claude Code | `soft`: delivered by the PostToolUse hook between tool calls, with no new attempt | MCP `ask` | 300 s inline | `claude --resume <session>` |
| Devin | `hard` | MCP `ask` over ACP `mcpServers` | capped at 50 s, then the worker yields | ACP `session/load` of the same session |
| Muse | `hard` | CLI `tusker message ask --yield` (no MCP overlay) | none; the worker yields | `muse exec --session-id <same>` |

### (a) Live preflight

```sh
tusker runner test "$PROFILE" --live --json | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["ready"],d["live"],d["model"],d["transport"],d.get("next_step",""))'
```

Expect `True True <model> <transport>`. The two-minute disposable turn is removed afterwards. Look at the JSON `cases` if it fails. Pass □ Fail □

### (b) Arm the one-task wave; the resident daemon picks it up

```sh
tusker wave start "$WAVE" --mode background --by "$ME" --json
tusker wave show "$WAVE"
st "$TASK"          # repeat until state is working
```

Expect the wave to be authorized and the task to reach `queued` and then `working` within one daemon poll. The run shows `runner_profile`/`runner_harness`/`runner_model` equal to the route from step 0.5. Only this project's work starts (`tusker automation status --json`). Pass □ Fail □

### (c) Observe the run in the CLI and in Serve

```sh
st "$TASK"
tusker runs events "$TASK" --lines 20 --follow     # Ctrl-C to stop; expect qual-<h>-pause-1 progress lines
run_url "$TASK"                                     # open in the browser
```

Expect `state: working`, a non-empty native `session`, and one attempt. Serve shows the same state label, session ID, attempt, live transcript/progress, and Say / Stop controls. Pass □ Fail □

### (d) Say mid-run (during pause 1)

```sh
tusker runs say "$TASK" --message "SAY-$H-1: record this token in say.txt" --by "$ME" --key "qual-$H-say-1" --json |
  python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["route"],d["operator_state"]["state"],d["continuation"])'
st "$TASK"
```

Expect `.route` to be `soft` for Claude Code and `hard` for the others.

- **Soft:** no new attempt. The token appears after pause 1 ends (at the next tool boundary).
- **Hard:** the current turn settles and a second attempt starts with the **same** `SessionRef` as the first.

In both cases the transcript in Serve and `runs events` shows the message once, and `$(tusker runs inspect "$TASK" --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["run"]["workspace_path"])')/sample/qual/qual-$H/say.txt` contains `SAY-$H-1`. Pass □ Fail □

### (e) Worker asks the operator; the reply reaches the same worker

Once pause 1 has finished, the worker asks its question.

```sh
tusker message list --project "$PROJECT" --recipient-kind operator --recipient operator |
  python3 -c 'import json,sys;[print(m["id"],m["sender"],m["body"],m.get("answered_at","")) for m in json.load(sys.stdin)["messages"] if m["kind"]=="question"]'
curl -s "http://127.0.0.1:7420/api/needs?project=$PROJECT" | head -c 600
st "$TASK"
QID=<question id from above>
tusker message reply --project "$PROJECT" --reply-to "$QID" --sender "$ME" --key "qual-$H-answer-1" --body teal
st "$TASK"
```

Expect a question from `task:$TASK` with body `qual-$H: which word goes on line 2 of result.txt?`. The same item appears in Needs you (Serve project home / `api/needs`), and the run shows `waiting_on_you`.

After the reply:

- **Codex/Claude:** the inline MCP wait returns the answer in the same attempt.
- **Devin** (if more than 50 s passed) **and Muse:** the answer wakes the run, which continues with the same `SessionRef`. If the run stays `waiting_on_you`/`stopped` for more than 2 minutes, run `tusker runs continue "$TASK" --by "$ME"` and record the missing automatic wake as a finding.

The final `result.txt` line 2 must be `teal`. Pass □ Fail □

### (f) Interrupt, then Continue (during pause 2)

```sh
tusker runs interrupt "$TASK" --json
st "$TASK"                                    # expect stopped
tusker runs continue "$TASK" --message "Continue: rerun pause 2 once, then finish." --by "$ME" --json
st "$TASK"                                    # expect working, new attempt, SAME session
```

Expect `stopped`, then `working`. The new attempt ID has the same `SessionRef` as every earlier attempt. The old process is gone and Serve shows Stopped, then Working. Pass □ Fail □

Then let the run finish:

```sh
tusker runs events "$TASK" --lines 20 --follow   # until submit, then review
tusker wave show "$WAVE"                          # expect the member done and the wave complete
st "$TASK"                                        # final tokens → results table
```

Expect review on the pinned review profile (a different harness) to accept and the task to close. Pass □ Fail □

### (g) Typed failure reason on a forced failure

```sh
tusker automation explain "$BAD_TASK" --vault "$VAULT"    # profile qual-bad-$H, model qual-no-such-model
tusker wave start "$BAD_WAVE" --mode background --by "$ME" --json
st "$BAD_TASK"                                           # repeat for up to 2 minutes
tusker runs inspect "$BAD_TASK" --json | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d.get("reason_code"),d.get("reason_source"),d["operator_state"])'
```

Either outcome is acceptable. Record which one happened:

1. **Admission refusal before claim.** There is no attempt, and `automation explain` or Serve shows an infrastructure block that names the model. Devin is likely to do this because unknown ACP models are not offered.
2. **A run that ends `failed` or `blocked`** with `operator_state.reason.code` from the typed set: `provider_error`, `config_invalid`, `missing_access`, `auth_expired`, and so on. `reason_source` is `driver`, and Serve shows the reason's guidance text.

Fail if the code is empty or `unknown`, or if Serve shows Working or Quiet for a dead process. Afterwards run `tusker wave pause "$BAD_WAVE" --by "$ME"` and, if a run record remains, `tusker runs retire "$BAD_TASK" --reason "qualification forced failure" --by "$ME"`. Pass □ Fail □

### (h) Architect inbox hook shows the wave report

Do the setup **before** step (b), so the wave has an architect when it completes.

```sh
mkdir -p "$QUAL.proof/architect/.claude"
cat > "$QUAL.proof/architect/.claude/settings.json" <<EOF
{"hooks":{
 "UserPromptSubmit":[{"hooks":[{"type":"command","command":"$BIN message inbox --project $PROJECT --format hook"}]}],
 "Stop":[{"hooks":[{"type":"command","command":"$BIN message inbox --project $PROJECT --format hook"}]}]}}
EOF
cd "$QUAL.proof/architect" && claude     # interactive architect session; say "hello" once
ARCH_SESSION=<this session's ID: /status, or the newest ~/.claude/projects/*architect*/<id>.jsonl name>
cd "$REPO"
tusker execution register --vault "$VAULT" --wave "$WAVE" --contact-role architect \
  --harness claude-code --provider anthropic --connection-id local --source direct_claude \
  --conversation-id "$ARCH_SESSION" --by "$ME" --if-generation 0 --json
```

Project resolution finds the runtime project; from outside the repo, use `--project "$PROJECT"` instead of `--vault`.

Expect `created: true` and a binding in state `inbox`. After the wave completes (end of step f):

```sh
EXEC=<execution id from the register output: .contact.address.id>
tusker message inbox --project "$PROJECT" --for "execution:$EXEC" --format text   # read-only preview
```

Expect one `wave_result` message from `execution:tusker-wave-$WAVE`. Its JSON body contains `"outcome":"completed"` and the member evidence. Then type any prompt in the architect session: the same message is injected once, and it is not injected again on the next prompt. Optional: register the architect on `$BAD_WAVE` too. About 10 minutes after the failure parks, a `stalled` report with blockers should arrive. Pass □ Fail □ Blocked □

## Cleanup

```sh
tusker projects disable --repo "$REPO" --json
tusker projects remove "$PROJECT" --json
for n in qual-bad-codex qual-bad-claude qual-bad-devin qual-bad-muse; do
  REV=$(tusker models show --json | python3 -c 'import json,sys;print(json.load(sys.stdin)["revision"])')
  tusker models profile-remove --scope global --name "$n" --if-revision "$REV" --json >/dev/null
done
# then delete $QUAL and $QUAL.proof yourself (copy evidence out first)
```

Remove the project before removing the profiles. HEAD lets `profile-remove` delete a profile that a task pin still references, and that task's route then blocks.

## Results template

Copy this section into `docs/reports/live-harness-qualification-<date>.md`. Use `PASS`, `FAIL`, `BLOCKED` (a gap prevents the step) or `NOT RUN`. Put IDs and one line of evidence in the notes.

```text
Date:            Operator:            Binary: tusker <version> sha256:<...>
Daemon/Serve:    <status>             Project: <PROJECT>  Repo: <REPO>
```

| Step | Codex | Claude Code | Devin | Muse |
| --- | --- | --- | --- | --- |
| Profile / model | | | | |
| (a) live preflight ready | | | | |
| (b) wave armed → daemon claim | | | | |
| (c) CLI + Serve state and session | | | | |
| (d) Say route (soft/hard), same session | | | | |
| (e) ask → Needs you → reply → received | | | | |
| (f) interrupt → Continue, same session | | | | |
| close via review | | | | |
| (g) failure: admission block or reason code | | | | |
| (h) architect wave report | | | | |
| Attempts (count) / native session ID | | | | |
| Tokens (`token_totals`) | | | | |

Notes per harness: task, wave, attempt IDs, question/answer message IDs, reason code and guidance text, screenshots path. List findings separately as new gaps (G7…), each with the exact command and output.
